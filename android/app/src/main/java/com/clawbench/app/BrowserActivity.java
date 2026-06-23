package com.clawbench.app;

import android.annotation.SuppressLint;
import android.app.AlertDialog;
import android.content.Intent;
import android.net.Uri;
import android.net.http.SslError;
import android.os.Bundle;
import android.util.Log;
import android.view.View;
import android.view.inputmethod.EditorInfo;
import android.webkit.CookieManager;
import android.webkit.SslErrorHandler;
import android.webkit.ConsoleMessage;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebStorage;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.EditText;
import android.widget.ProgressBar;
import android.widget.Toast;

import androidx.appcompat.app.AppCompatActivity;

import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.security.SecureRandom;
import java.security.cert.X509Certificate;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

import javax.net.ssl.HttpsURLConnection;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509TrustManager;

/**
 * Sandbox browser Activity for testing forwarded ports.
 *
 * Runs in an independent process (":browser") to provide full Cookie/Storage
 * isolation from the main app. This allows login/authentication testing
 * without sharing session state with the main ClawBench WebView.
 *
 * Key features:
 * - Back/forward navigation within WebView
 * - URL bar with localhost-only navigation (external URLs → system browser)
 * - Refresh current page
 * - Clear browsing data (manual, with confirmation dialog)
 * - Data persists across sessions (not cleared on exit)
 * - Auto-accept SSL for localhost, prompt for others
 * - No AndroidNative bridge injected (clean browser environment)
 *
 * Lifecycle:
 * - Back button navigates back to MainActivity (preserves WebView state)
 *   instead of destroying it, so users can return to the same page.
 * - Explicit close button (X) truly finishes the Activity.
 * - When reopened via openInSandbox, existing instance is reused via
 *   FLAG_ACTIVITY_CLEAR_TOP | FLAG_ACTIVITY_SINGLE_TOP + onNewIntent.
 */
public class BrowserActivity extends AppCompatActivity {

    private static final String TAG = "ClawBench-Browser";

    private WebView webView;
    private EditText urlBar;
    private ProgressBar progressBar;

    private int tunnelRetryCount = 0;
    private static final int MAX_TUNNEL_RETRIES = 5;
    private static final long TUNNEL_RETRY_DELAY_MS = 1000;
    private String pendingUrl = null;

    /** Target host:port for Host header rewriting (e.g. "192.168.100.1"). Empty if localhost. */
    private String targetHost = "";

    /** The local port that the SSH tunnel listens on. */
    private int localPort = 0;

    @SuppressLint("SetJavaScriptEnabled")
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_browser);

        webView = findViewById(R.id.browserWebView);
        urlBar = findViewById(R.id.urlBar);
        progressBar = findViewById(R.id.progressBar);

        setupWebView();
        setupToolbar();

        // Load initial URL from Intent
        int port = getIntent().getIntExtra("port", 0);
        String protocol = getIntent().getStringExtra("protocol");
        String host = getIntent().getStringExtra("host");
        localPort = port;
        // Build targetHost for Host header rewriting: strip default ports per HTTP spec
        if (host != null && !host.isEmpty()) {
            // Strip default port from host:port for Host header
            String hostPart = host;
            if (host.contains(":")) {
                String[] parts = host.split(":", 2);
                try {
                    int targetPort = Integer.parseInt(parts[1]);
                    boolean isDefault = ("http".equals(protocol) && targetPort == 80) ||
                            ("https".equals(protocol) && targetPort == 443);
                    hostPart = isDefault ? parts[0] : host;
                } catch (NumberFormatException e) {
                    hostPart = host;
                }
            }
            targetHost = hostPart;
        }
        if (port > 0 && protocol != null) {
            String initialUrl = protocol + "://localhost:" + port + "/";
            pendingUrl = initialUrl;
            AppLog.i(TAG, "BrowserActivity: loading " + initialUrl + " (tunnel target: " + (host != null && !host.isEmpty() ? host : "localhost") + ":" + port + ")");
            webView.loadUrl(initialUrl);
            urlBar.setText(initialUrl);
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    private void setupWebView() {
        WebSettings settings = webView.getSettings();

        // Core web features
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setDatabaseEnabled(true);
        settings.setCacheMode(WebSettings.LOAD_DEFAULT);

        // Allow mixed content (HTTP/HTTPS)
        settings.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);

        // Responsive layout
        settings.setUseWideViewPort(true);
        settings.setLoadWithOverviewMode(true);

        // Smooth scrolling
        webView.setOverScrollMode(WebView.OVER_SCROLL_NEVER);

        // Custom user agent to identify sandbox browser
        String ua = settings.getUserAgentString();
        settings.setUserAgentString(ua + " ClawBench-Browser/1.0");

        // NO AndroidNative bridge — this is a clean browser environment

        // WebView client with URL restriction and SSL handling
        webView.setWebViewClient(new SandboxWebViewClient());

        // Chrome client for progress bar and console logging
        webView.setWebChromeClient(new WebChromeClient() {
            @Override
            public boolean onConsoleMessage(ConsoleMessage consoleMessage) {
                String tag = "WebView:" + consoleMessage.messageLevel();
                String msg = consoleMessage.message() + " (" + consoleMessage.sourceId() + ":" + consoleMessage.lineNumber() + ")";
                switch (consoleMessage.messageLevel()) {
                    case ERROR:
                        Log.e(tag, msg);
                        break;
                    case WARNING:
                        Log.w(tag, msg);
                        break;
                    default:
                        Log.d(tag, msg);
                        break;
                }
                return true;
            }

            @Override
            public void onProgressChanged(WebView view, int newProgress) {
                if (newProgress < 100) {
                    progressBar.setVisibility(View.VISIBLE);
                    progressBar.setProgress(newProgress);
                } else {
                    progressBar.setVisibility(View.GONE);
                }
            }
        });

        // Accept third-party cookies
        CookieManager.getInstance().setAcceptThirdPartyCookies(webView, true);
    }

    private void setupToolbar() {
        // Back button: WebView history back, or navigate back to main app
        findViewById(R.id.btnBack).setOnClickListener(v -> {
            if (webView.canGoBack()) {
                webView.goBack();
            } else {
                navigateBackToMain();
            }
        });

        // Close button: truly finish the Activity (destroy WebView)
        findViewById(R.id.btnClose).setOnClickListener(v -> finish());

        // Refresh button
        findViewById(R.id.btnRefresh).setOnClickListener(v -> webView.reload());

        // Clear data button
        findViewById(R.id.btnClearData).setOnClickListener(v -> showClearDataDialog());

        // URL bar: navigate on Enter/Go
        urlBar.setOnEditorActionListener((v, actionId, event) -> {
            if (actionId == EditorInfo.IME_ACTION_GO || actionId == EditorInfo.IME_ACTION_DONE) {
                navigateToUrl();
                return true;
            }
            return false;
        });

        // Also navigate on focus lost (if user taps away from URL bar)
        urlBar.setOnFocusChangeListener((v, hasFocus) -> {
            if (!hasFocus) {
                // Update URL bar with current page URL if user didn't edit
                // (prevents stale URL display after navigation)
            }
        });
    }

    /**
     * Navigate to the URL entered in the URL bar.
     * Only localhost URLs are loaded in the sandbox;
     * external URLs are opened in the system browser.
     */
    private void navigateToUrl() {
        String input = urlBar.getText().toString().trim();
        if (input.isEmpty()) return;

        // Ensure it has a scheme
        if (!input.startsWith("http://") && !input.startsWith("https://")) {
            input = "http://" + input;
        }

        try {
            Uri uri = Uri.parse(input);
            String host = uri.getHost();

            if ("localhost".equals(host) || "127.0.0.1".equals(host)) {
                webView.loadUrl(input);
            } else {
                // External URL: open in system browser, not in sandbox
                startActivity(new Intent(Intent.ACTION_VIEW, uri));
            }
        } catch (Exception e) {
            AppLog.w(TAG, "Invalid URL: " + input, e);
        }

        // Hide keyboard
        urlBar.clearFocus();
    }

    /**
     * Show confirmation dialog for clearing browsing data.
     * Data is preserved by default; user must explicitly clear it.
     */
    private void showClearDataDialog() {
        new AlertDialog.Builder(this)
                .setTitle(R.string.browser_clear_title)
                .setMessage(R.string.browser_clear_message)
                .setPositiveButton(R.string.browser_clear_positive, (dialog, which) -> clearBrowsingData())
                .setNegativeButton(R.string.browser_clear_negative, null)
                .show();
    }

    /**
     * Clear all browsing data: cookies, WebStorage, cache, form data.
     * Then reload the current page to reflect the clean state.
     */
    private void clearBrowsingData() {
        CookieManager.getInstance().removeAllCookies(null);
        WebStorage.getInstance().deleteAllData();
        webView.clearCache(true);
        webView.clearFormData();
        webView.clearHistory();

        Toast.makeText(this, R.string.browser_clear_done, Toast.LENGTH_SHORT).show();

        // Reload current page to show logged-out state
        if (webView.getUrl() != null) {
            webView.reload();
        }
    }

    @Override
    protected void onPause() {
        super.onPause();
        pauseWebView();
    }

    @Override
    protected void onResume() {
        super.onResume();
        resumeWebView();
    }

    /** Pause WebView rendering and JS timers to release CPU/GPU resources. */
    void pauseWebView() {
        webView.onPause();
        webView.pauseTimers();
    }

    /** Resume WebView rendering and JS timers when returning to foreground. */
    void resumeWebView() {
        webView.onResume();
        webView.resumeTimers();
    }

    /**
     * Navigate back to MainActivity instead of destroying this Activity.
     * Since BrowserActivity runs in a separate task (taskAffinity="" + :browser process),
     * moveTaskToBack would push the whole task to background with no way back.
     * Starting MainActivity brings the main app to the foreground while this
     * Activity stays alive in the background, preserving the WebView state.
     */
    @Override
    public void onBackPressed() {
        if (webView.canGoBack()) {
            webView.goBack();
        } else {
            navigateBackToMain();
        }
    }

    /**
     * Navigate back to the main app while keeping this Activity alive.
     * Instead of starting a new MainActivity (which can reset tab state),
     * we move the main app's task to the foreground using ActivityManager.
     * This preserves the exact UI state the user had before opening the sandbox.
     */
    private void navigateBackToMain() {
        // Move this task (BrowserActivity) to background first
        moveTaskToBack(true);
        // Bring the main app's task to the foreground
        try {
            android.app.ActivityManager am = (android.app.ActivityManager) getSystemService(ACTIVITY_SERVICE);
            for (android.app.ActivityManager.AppTask appTask : am.getAppTasks()) {
                android.content.ComponentName comp = appTask.getTaskInfo().topActivity;
                if (comp != null && comp.getPackageName().equals(getPackageName())
                        && !getClass().getName().equals(comp.getClassName())) {
                    appTask.moveToFront();
                    break;
                }
            }
        } catch (Exception e) {
            AppLog.w(TAG, "BrowserActivity: failed to bring main task to front", e);
        }
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);

        int port = intent.getIntExtra("port", 0);
        String protocol = intent.getStringExtra("protocol");
        String host = intent.getStringExtra("host");

        if (port <= 0 || protocol == null) return;

        String newUrl = protocol + "://localhost:" + port + "/";

        // If reopening the same URL, just bring to foreground without reloading
        if (newUrl.equals(pendingUrl)) {
            AppLog.i(TAG, "BrowserActivity: onNewIntent same URL, skip reload: " + newUrl);
            return;
        }

        localPort = port;

        // Reset and recalculate targetHost
        targetHost = "";
        if (host != null && !host.isEmpty()) {
            String hostPart = host;
            if (host.contains(":")) {
                String[] parts = host.split(":", 2);
                try {
                    int targetPort = Integer.parseInt(parts[1]);
                    boolean isDefault = ("http".equals(protocol) && targetPort == 80) ||
                            ("https".equals(protocol) && targetPort == 443);
                    hostPart = isDefault ? parts[0] : host;
                } catch (NumberFormatException e) {
                    hostPart = host;
                }
            }
            targetHost = hostPart;
        }

        tunnelRetryCount = 0;
        pendingUrl = newUrl;
        AppLog.i(TAG, "BrowserActivity: onNewIntent loading " + newUrl + " (tunnel target: " + (host != null && !host.isEmpty() ? host : "localhost") + ":" + port + ")");
        webView.loadUrl(newUrl);
        urlBar.setText(newUrl);
    }

    @Override
    protected void onDestroy() {
        // Do NOT clear data here — it should persist across sessions.
        // Only release WebView resources.
        webView.loadUrl("about:blank");
        webView.destroy();
        super.onDestroy();
    }

    // --- WebView Client ---

    private class SandboxWebViewClient extends WebViewClient {

        /**
         * Intercept requests to localhost:localPort and rewrite the Host header
         * when forwarding to a non-localhost target (e.g. 192.168.100.1).
         *
         * Without this, the browser sends "Host: localhost:port" which the target
         * server doesn't recognize, causing 404 errors on virtual-host-based servers.
         *
         * When targetHost is empty (forwarding to localhost itself), we skip
         * interception and let WebView handle the request normally via the SSH tunnel.
         */
        @Override
        public WebResourceResponse shouldInterceptRequest(WebView view, WebResourceRequest request) {
            Uri uri = request.getUrl();
            String host = uri.getHost();

            // Only intercept localhost requests when we have a target host to rewrite to
            if (targetHost.isEmpty() || !("localhost".equals(host) || "127.0.0.1".equals(host))) {
                return super.shouldInterceptRequest(view, request);
            }

            try {
                URL url = new URL(uri.toString());
                HttpURLConnection conn = (HttpURLConnection) url.openConnection();

                // Trust all certs for localhost (SSH tunnel is plaintext, self-signed HTTPS)
                if (conn instanceof HttpsURLConnection && ("localhost".equals(host) || "127.0.0.1".equals(host))) {
                    HttpsURLConnection httpsConn = (HttpsURLConnection) conn;
                    SSLContext sc = SSLContext.getInstance("TLS");
                    sc.init(null, new TrustManager[]{new X509TrustManager() {
                        public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
                        public void checkClientTrusted(X509Certificate[] certs, String authType) {}
                        public void checkServerTrusted(X509Certificate[] certs, String authType) {}
                    }}, new SecureRandom());
                    httpsConn.setSSLSocketFactory(sc.getSocketFactory());
                    httpsConn.setHostnameVerifier((hostname, session) -> true);
                }

                // Set method
                String method = request.getMethod();
                conn.setRequestMethod(method);

                // Rewrite Host header to the target host (default port already stripped)
                conn.setRequestProperty("Host", targetHost);

                AppLog.i(TAG, "BrowserActivity: intercept " + method + " " + uri + " → Host: " + targetHost);

                // Copy other request headers (except Host which we already set)
                Map<String, String> reqHeaders = request.getRequestHeaders();
                for (Map.Entry<String, String> entry : reqHeaders.entrySet()) {
                    String key = entry.getKey();
                    if ("Host".equalsIgnoreCase(key)) continue;  // already set
                    conn.setRequestProperty(key, entry.getValue());
                }

                // Get response
                int statusCode = conn.getResponseCode();
                String reason = conn.getResponseMessage();
                String contentType = conn.getContentType();
                String encoding = conn.getContentEncoding();

                AppLog.i(TAG, "BrowserActivity: response " + statusCode + " " + reason + " contentType=" + contentType);

                // Log error response body preview for diagnostics
                if (statusCode >= 400) {
                    try {
                        InputStream errStream = conn.getErrorStream();
                        if (errStream != null) {
                            byte[] preview = new byte[Math.min(256, errStream.available() > 0 ? errStream.available() : 256)];
                            int read = errStream.read(preview);
                            if (read > 0) {
                                AppLog.w(TAG, "BrowserActivity: error response body: " + new String(preview, 0, read, "UTF-8"));
                            }
                        }
                    } catch (Exception ignored) {}
                }

                // Collect response headers
                Map<String, String> respHeaders = new HashMap<>();
                for (Map.Entry<String, List<String>> entry : conn.getHeaderFields().entrySet()) {
                    if (entry.getKey() != null && !entry.getValue().isEmpty()) {
                        respHeaders.put(entry.getKey(), entry.getValue().get(0));
                    }
                }

                InputStream inputStream = conn.getErrorStream();
                if (inputStream == null) {
                    inputStream = conn.getInputStream();
                }

                // Determine MIME type
                String mime = contentType;
                if (mime == null || mime.isEmpty()) {
                    mime = "application/octet-stream";
                } else {
                    int semiIdx = mime.indexOf(';');
                    if (semiIdx > 0) {
                        mime = mime.substring(0, semiIdx).trim();
                    }
                }

                return new WebResourceResponse(
                        mime,
                        encoding != null ? encoding : "utf-8",
                        statusCode,
                        reason != null ? reason : "OK",
                        respHeaders,
                        inputStream
                );

            } catch (Exception e) {
                AppLog.w(TAG, "BrowserActivity: shouldInterceptRequest failed for " + uri, e);
                return super.shouldInterceptRequest(view, request);
            }
        }

        @Override
        public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
            Uri url = request.getUrl();
            String host = url.getHost();

            // Only allow localhost URLs in the sandbox
            if ("localhost".equals(host) || "127.0.0.1".equals(host)) {
                return false; // Load in sandbox WebView
            }

            // External URLs → system browser
            startActivity(new Intent(Intent.ACTION_VIEW, url));
            return true;
        }

        @Override
        public void onPageFinished(WebView view, String url) {
            // Update URL bar to reflect actual page URL
            urlBar.setText(url);
        }

        @Override
        public void onReceivedSslError(WebView view, SslErrorHandler handler, SslError error) {
            String host = null;
            String currentUrl = view.getUrl();
            if (currentUrl != null) {
                host = Uri.parse(currentUrl).getHost();
            }

            // Auto-accept SSL for localhost (self-signed certs on forwarded ports)
            if ("localhost".equals(host) || "127.0.0.1".equals(host)) {
                handler.proceed();
                return;
            }

            // Non-localhost: prompt user before accepting
            new AlertDialog.Builder(BrowserActivity.this)
                    .setTitle(R.string.browser_ssl_title)
                    .setMessage(R.string.browser_ssl_message)
                    .setPositiveButton(R.string.browser_ssl_positive, (dialog, which) -> handler.proceed())
                    .setNegativeButton(R.string.browser_ssl_negative, (dialog, which) -> handler.cancel())
                    .setCancelable(false)
                    .show();
        }

        @Override
        public void onReceivedError(WebView view, WebResourceRequest request, WebResourceError error) {
            super.onReceivedError(view, request, error);
            if (request.isForMainFrame()) {
                if (tunnelRetryCount < MAX_TUNNEL_RETRIES && pendingUrl != null) {
                    tunnelRetryCount++;
                    AppLog.i(TAG, "BrowserActivity: page load failed (attempt " + tunnelRetryCount + "/" + MAX_TUNNEL_RETRIES + "), retrying in " + TUNNEL_RETRY_DELAY_MS + "ms");
                    webView.postDelayed(() -> webView.loadUrl(pendingUrl), TUNNEL_RETRY_DELAY_MS);
                } else {
                    Toast.makeText(BrowserActivity.this, R.string.error_connection_failed, Toast.LENGTH_SHORT).show();
                }
            }
        }
    }
}
