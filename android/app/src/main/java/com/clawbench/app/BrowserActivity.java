package com.clawbench.app;

import android.annotation.SuppressLint;
import android.app.AlertDialog;
import android.content.Intent;
import android.net.Uri;
import android.net.http.SslError;
import android.os.Bundle;
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
import android.widget.TextView;
import android.widget.Toast;

import androidx.appcompat.app.AppCompatActivity;

import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.net.URL;

import java.security.cert.X509Certificate;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;

import javax.net.ssl.HttpsURLConnection;

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
    private BrowserLogBuffer logBuffer;
    private String mobileUserAgent;
    private boolean desktopMode = false;
    private BrowserSessionCredentials.Creds sessionCreds;

    private View tunnelWaitingOverlay;
    private TextView tunnelWaitingText;

    /** Prevents multiple tunnel-wait threads from running simultaneously. */
    private final AtomicBoolean tunnelWaitRunning = new AtomicBoolean(false);

    /** Reference to the tunnel-wait thread so we can interrupt it on onNewIntent. */
    private volatile Thread tunnelWaitThread = null;

    private static final int MAX_TUNNEL_WAIT_MS = 30000;
    private static final int TUNNEL_POLL_INTERVAL_MS = 500;
    private static final int TUNNEL_CONNECT_TIMEOUT_MS = 300;
    private String pendingUrl = null;

    /** Target host:port for Host header rewriting (e.g. "192.168.100.1"). Empty if localhost. */
    private String targetHost = "";

    /** The local port that the SSH tunnel listens on. */
    private int localPort = 0;

    BrowserLogBuffer getLogBuffer() {
        return logBuffer;
    }

    BrowserSessionCredentials.Creds getSessionCredentials() {
        return sessionCreds;
    }

    /**
     * Wait for the SSH tunnel port to become reachable, then load the URL in WebView.
     * Shows the tunnel waiting overlay with a spinner while polling.
     * If the port doesn't become reachable within MAX_TUNNEL_WAIT_MS, shows an error.
     *
     * This replaces the old pattern of immediately calling webView.loadUrl() and
     * retrying on error — instead we proactively wait for the tunnel to be ready.
     */
    private void waitForTunnelAndLoad(String url) {
        if (localPort <= 0) {
            // No tunnel needed (shouldn't happen), just load directly
            showWebViewAndLoad(url);
            return;
        }

        // Interrupt any previous tunnel-wait thread
        Thread prev = tunnelWaitThread;
        if (prev != null && prev.isAlive()) {
            prev.interrupt();
        }

        // Show overlay, hide WebView
        tunnelWaitingOverlay.setVisibility(View.VISIBLE);
        tunnelWaitingText.setText(R.string.browser_tunnel_waiting);
        webView.setVisibility(View.GONE);

        // Force-set the guard — we already interrupted the old thread above,
        // so we own the right to start a new one. The old thread's finally
        // will set false, but tunnelWaitThread already points to the new thread.
        tunnelWaitRunning.set(true);

        Thread t = new Thread(() -> {
            int elapsed = 0;
            try {
                while (elapsed < MAX_TUNNEL_WAIT_MS) {
                    if (Thread.interrupted()) return;
                    if (testLocalPort(localPort)) {
                        AppLog.i(TAG, "BrowserActivity: tunnel ready after " + elapsed + "ms, loading " + url);
                        runOnUiThread(() -> showWebViewAndLoad(url));
                        return;
                    }
                    Thread.sleep(TUNNEL_POLL_INTERVAL_MS);
                    elapsed += TUNNEL_POLL_INTERVAL_MS;
                }
                // Timeout
                AppLog.w(TAG, "BrowserActivity: tunnel wait timed out after " + elapsed + "ms for port " + localPort);
                runOnUiThread(() -> {
                    tunnelWaitingText.setText(R.string.browser_tunnel_failed);
                    // Allow user to tap overlay to retry
                    tunnelWaitingOverlay.setOnClickListener(v -> {
                        tunnelWaitingOverlay.setClickable(false);
                        waitForTunnelAndLoad(url);
                    });
                    tunnelWaitingOverlay.setClickable(true);
                });
            } catch (InterruptedException e) {
                AppLog.i(TAG, "BrowserActivity: tunnel wait interrupted");
            } finally {
                tunnelWaitRunning.set(false);
            }
        }, "tunnel-wait");
        tunnelWaitThread = t;
        t.start();
    }

    /**
     * Hide the waiting overlay, show the WebView, and load the URL.
     */
    private void showWebViewAndLoad(String url) {
        tunnelWaitingOverlay.setVisibility(View.GONE);
        webView.setVisibility(View.VISIBLE);
        webView.loadUrl(url);
    }

    /**
     * Test whether a local port is reachable (TCP connect succeeds).
     * Uses a short 300ms timeout — localhost connections should be near-instant.
     */
    private boolean testLocalPort(int port) {
        try (Socket socket = new Socket()) {
            socket.connect(new InetSocketAddress("127.0.0.1", port), TUNNEL_CONNECT_TIMEOUT_MS);
            return true;
        } catch (Exception e) {
            AppLog.d(TAG, "testLocalPort: port " + port + " not reachable: " + e.getMessage());
            return false;
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_browser);

        webView = findViewById(R.id.browserWebView);
        urlBar = findViewById(R.id.urlBar);
        progressBar = findViewById(R.id.progressBar);
        tunnelWaitingOverlay = findViewById(R.id.tunnelWaitingOverlay);
        tunnelWaitingText = findViewById(R.id.tunnelWaitingText);

        logBuffer = new BrowserLogBuffer(500);
        sessionCreds = BrowserSessionCredentials.getAndClear(this);
        setupWebView();
        setupToolbar();
        setupFindBar();

        // Load initial URL from Intent
        int port = getIntent().getIntExtra("port", 0);
        String protocol = getIntent().getStringExtra("protocol");
        String host = getIntent().getStringExtra("host");
        String path = getIntent().getStringExtra("path");
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
            String urlPath = (path != null && !path.isEmpty()) ? path : "/";
            String initialUrl = protocol + "://localhost:" + port + urlPath;
            pendingUrl = initialUrl;
            urlBar.setText(initialUrl);
            AppLog.i(TAG, "BrowserActivity: waiting for tunnel then loading " + initialUrl + " (tunnel target: " + (host != null && !host.isEmpty() ? host : "localhost") + ":" + port + ")");
            waitForTunnelAndLoad(initialUrl);
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
        mobileUserAgent = ua + " ClawBench-Browser/1.0";
        settings.setUserAgentString(mobileUserAgent);

        // Inject BrowserNative JS interface for error relay + log capture
        webView.addJavascriptInterface(new BrowserJavascriptInterface(logBuffer), "BrowserNative");

        // WebView client with URL restriction and SSL handling
        webView.setWebViewClient(new SandboxWebViewClient());

        // Chrome client for progress bar and console logging
        webView.setWebChromeClient(new WebChromeClient() {
            @Override
            public boolean onConsoleMessage(ConsoleMessage consoleMessage) {
                String tag = "WebView:" + consoleMessage.messageLevel();
                String msg = consoleMessage.message() + " (" + consoleMessage.sourceId() + ":" + consoleMessage.lineNumber() + ")";
                char level;
                switch (consoleMessage.messageLevel()) {
                    case ERROR:
                        AppLog.e(tag, msg);
                        level = 'E';
                        break;
                    case WARNING:
                        AppLog.w(tag, msg);
                        level = 'W';
                        break;
                    default:
                        AppLog.d(tag, msg);
                        level = 'D';
                        break;
                }
                logBuffer.add(level, tag, msg);
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

        // Find in page listener
        webView.setFindListener((activeMatchOrdinal, numberOfMatches, isDoneCounting) -> {
            TextView countView = findViewById(R.id.findResultCount);
            if (isDoneCounting) {
                if (numberOfMatches > 0) {
                    countView.setText((activeMatchOrdinal + 1) + "/" + numberOfMatches);
                    countView.setVisibility(View.VISIBLE);
                } else {
                    countView.setText(R.string.browser_find_no_results);
                    countView.setVisibility(View.VISIBLE);
                }
            }
        });
    }

    private void setupToolbar() {
        // Home button: always navigate back to main app
        findViewById(R.id.btnBack).setOnClickListener(v -> navigateBackToMain());

        // Log button: show console log bottom sheet
        findViewById(R.id.btnLog).setOnClickListener(v -> showLogBottomSheet());

        // Overflow menu button
        findViewById(R.id.btnMore).setOnClickListener(v -> showOverflowMenu());

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
     * Show overflow popup menu with browser actions.
     */
    private void showOverflowMenu() {
        android.widget.PopupMenu popup = new android.widget.PopupMenu(this, findViewById(R.id.btnMore));
        popup.getMenuInflater().inflate(R.menu.browser_menu, popup.getMenu());

        // Force icons to show in PopupMenu (hidden by default).
        // Note: This reflection hack breaks on Android 14+ (API 34) due to
        // restrictions on non-SDK interfaces. The try-catch ensures graceful
        // fallback (icons simply won't show on those devices).
        try {
            java.lang.reflect.Field field = popup.getClass().getDeclaredField("mPopup");
            field.setAccessible(true);
            Object menuPopupHelper = field.get(popup);
            Class<?> classPopupHelper = Class.forName(menuPopupHelper.getClass().getName());
            java.lang.reflect.Method setForceShowIcon = classPopupHelper.getMethod("setForceShowIcon", boolean.class);
            setForceShowIcon.invoke(menuPopupHelper, true);
        } catch (Exception e) {
            AppLog.w(TAG, "BrowserActivity: failed to force show menu icons", e);
        }

        // Update desktop mode checkbox state
        popup.getMenu().findItem(R.id.action_desktop_mode).setChecked(desktopMode);
        popup.setOnMenuItemClickListener(item -> {
            int id = item.getItemId();
            if (id == R.id.action_page_back) {
                if (webView.canGoBack()) {
                    webView.goBack();
                } else {
                    Toast.makeText(this, R.string.browser_no_history, Toast.LENGTH_SHORT).show();
                }
                return true;
            } else if (id == R.id.action_forward) {
                if (webView.canGoForward()) {
                    webView.goForward();
                } else {
                    Toast.makeText(this, R.string.browser_no_history, Toast.LENGTH_SHORT).show();
                }
                return true;
            } else if (id == R.id.action_refresh) {
                webView.reload();
                return true;
            } else if (id == R.id.action_desktop_mode) {
                toggleDesktopMode();
                return true;
            } else if (id == R.id.action_find) {
                showFindBar();
                return true;
            } else if (id == R.id.action_clear_data) {
                showClearDataDialog();
                return true;
            } else if (id == R.id.action_close) {
                finish();
                return true;
            }
            return false;
        });
        popup.show();
    }

    /**
     * Show the find-in-page bar.
     */
    private void showFindBar() {
        findViewById(R.id.findBar).setVisibility(View.VISIBLE);
        EditText findInput = findViewById(R.id.findInput);
        findInput.setText("");
        findInput.requestFocus();
        findViewById(R.id.findResultCount).setVisibility(View.GONE);
    }

    /**
     * Set up the find-in-page bar listeners.
     */
    private void setupFindBar() {
        EditText findInput = findViewById(R.id.findInput);
        findInput.setOnEditorActionListener((v, actionId, event) -> {
            if (actionId == EditorInfo.IME_ACTION_SEARCH) {
                executeFind();
                return true;
            }
            return false;
        });

        findViewById(R.id.btnFindNext).setOnClickListener(v -> webView.findNext(true));
        findViewById(R.id.btnFindPrev).setOnClickListener(v -> webView.findNext(false));
        findViewById(R.id.btnFindClose).setOnClickListener(v -> clearFind());
    }

    private void executeFind() {
        String query = ((EditText) findViewById(R.id.findInput)).getText().toString();
        webView.findAllAsync(query);
    }

    private void clearFind() {
        webView.clearMatches();
        findViewById(R.id.findBar).setVisibility(View.GONE);
        findViewById(R.id.findResultCount).setVisibility(View.GONE);
    }

    /**
     * Toggle desktop mode by switching user agent and viewport settings.
     */
    private void toggleDesktopMode() {
        desktopMode = !desktopMode;
        WebSettings settings = webView.getSettings();
        if (desktopMode) {
            // Desktop user agent
            String desktopUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";
            settings.setUserAgentString(desktopUA);
            settings.setUseWideViewPort(true);
            settings.setLoadWithOverviewMode(false);
        } else {
            settings.setUserAgentString(mobileUserAgent);
            settings.setUseWideViewPort(true);
            settings.setLoadWithOverviewMode(true);
        }
        webView.reload();
    }

    /**
     * Show the console log bottom sheet dialog.
     */
    private void showLogBottomSheet() {
        LogBottomSheet sheet = LogBottomSheet.newInstance();
        sheet.show(getSupportFragmentManager(), "log_bottom_sheet");
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
                // Update localPort if user navigated to a different port
                int uriPort = uri.getPort();
                if (uriPort > 0) {
                    localPort = uriPort;
                }
                waitForTunnelAndLoad(input);
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
        navigateBackToMain();
    }

    /**
     * Navigate back to the main app while keeping this Activity alive.
     * Instead of starting a new MainActivity (which can reset tab state),
     * we move the main app's task to the foreground using ActivityManager.
     * This preserves the exact UI state the user had before opening the sandbox.
     */
    private void navigateBackToMain() {
        navigateBackToMain(null);
    }

    /**
     * Navigate back to the main app, optionally jumping to a specific session.
     * Uses an explicit intent with FLAG_ACTIVITY_SINGLE_TOP so onNewIntent()
     * dispatches the session_id via handleNotificationIntent().
     * BrowserActivity is moved to background (not finished) to preserve WebView state.
     */
    void navigateBackToMain(String sessionId) {
        moveTaskToBack(true);
        try {
            Intent intent = new Intent(this, MainActivity.class);
            intent.setAction(Intent.ACTION_MAIN);
            intent.addCategory(Intent.CATEGORY_LAUNCHER);
            intent.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_NEW_TASK);
            if (sessionId != null && !sessionId.isEmpty()) {
                intent.putExtra("session_id", sessionId);
            }
            startActivity(intent);
        } catch (Exception e) {
            AppLog.w(TAG, "BrowserActivity: failed to bring main task to front", e);
        }
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);

        // Refresh session credentials if provided
        try {
            BrowserSessionCredentials.Creds newCreds = BrowserSessionCredentials.getAndClear(this);
            if (newCreds != null) {
                sessionCreds = newCreds;
            }
        } catch (Exception e) {
            // May fail in test environments with Unsafe-allocated Activities
            AppLog.w(TAG, "BrowserActivity: failed to read session credentials", e);
        }

        int port = intent.getIntExtra("port", 0);
        String protocol = intent.getStringExtra("protocol");
        String host = intent.getStringExtra("host");
        String path = intent.getStringExtra("path");

        if (port <= 0 || protocol == null) return;

        String urlPath = (path != null && !path.isEmpty()) ? path : "/";
        String newUrl = protocol + "://localhost:" + port + urlPath;

        // If reopening the same URL and WebView is already visible, skip reload
        if (newUrl.equals(pendingUrl) && webView.getVisibility() == View.VISIBLE) {
            AppLog.i(TAG, "BrowserActivity: onNewIntent same URL and WebView visible, skip reload: " + newUrl);
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

        pendingUrl = newUrl;
        urlBar.setText(newUrl);
        AppLog.i(TAG, "BrowserActivity: onNewIntent waiting for tunnel then loading " + newUrl + " (tunnel target: " + (host != null && !host.isEmpty() ? host : "localhost") + ":" + port + ")");
        waitForTunnelAndLoad(newUrl);
    }

    @Override
    protected void onDestroy() {
        // Interrupt tunnel-wait thread to prevent leaks and post-after-destroy crashes
        Thread t = tunnelWaitThread;
        if (t != null && t.isAlive()) {
            t.interrupt();
        }
        // Do NOT clear browsing data here — it should persist across sessions.
        // Only release WebView resources and clear session credentials.
        BrowserSessionCredentials.clear(this);
        webView.loadUrl("about:blank");
        webView.destroy();
        super.onDestroy();
    }

    // --- WebView Client ---

    private class SandboxWebViewClient extends WebViewClient {

        @Override
        public void onPageStarted(WebView view, String url, android.graphics.Bitmap favicon) {
            super.onPageStarted(view, url, favicon);
            // Inject global error listeners for uncaught JS exceptions and resource load failures
            view.evaluateJavascript(JSErrorInjector.buildScript("BrowserNative"), null);
        }

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
                if (conn instanceof HttpsURLConnection) {
                    SSLHelper.setupTrustAll((HttpsURLConnection) conn, host);
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
                    InputStream errStream = null;
                    try {
                        errStream = conn.getErrorStream();
                        if (errStream != null) {
                            byte[] preview = new byte[Math.min(256, errStream.available() > 0 ? errStream.available() : 256)];
                            int read = errStream.read(preview);
                            if (read > 0) {
                                AppLog.w(TAG, "BrowserActivity: error response body: " + new String(preview, 0, read, "UTF-8"));
                            }
                            errStream.close();
                        }
                    } catch (Exception ignored) {
                        if (errStream != null) try { errStream.close(); } catch (Exception ignored2) {}
                    }
                }

                // Collect response headers
                Map<String, String> respHeaders = new HashMap<>();
                for (Map.Entry<String, List<String>> entry : conn.getHeaderFields().entrySet()) {
                    if (entry.getKey() != null && !entry.getValue().isEmpty()) {
                        respHeaders.put(entry.getKey(), entry.getValue().get(0));
                    }
                }

                InputStream inputStream;
                try {
                    inputStream = conn.getErrorStream();
                    if (inputStream == null) {
                        inputStream = conn.getInputStream();
                    }
                } catch (Exception e) {
                    AppLog.w(TAG, "BrowserActivity: failed to get response stream for " + uri, e);
                    return super.shouldInterceptRequest(view, request);
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
                AppLog.w(TAG, "BrowserActivity: page load failed for " + request.getUrl());
                Toast.makeText(BrowserActivity.this, R.string.error_connection_failed, Toast.LENGTH_SHORT).show();
            }
        }
    }
}
