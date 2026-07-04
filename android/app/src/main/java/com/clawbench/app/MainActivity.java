package com.clawbench.app;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.app.AlertDialog;
import android.app.DownloadManager;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.ConnectivityManager;
import android.net.NetworkInfo;
import android.net.Uri;
import android.net.http.SslError;
import android.os.Build;
import android.os.Bundle;
import android.os.Environment;
import android.os.PowerManager;
import android.util.Log;
import android.provider.MediaStore;
import android.provider.Settings;
import android.view.KeyEvent;
import android.view.View;
import android.view.WindowManager;
import android.webkit.CookieManager;
import android.widget.FrameLayout;
import android.webkit.DownloadListener;
import android.webkit.JavascriptInterface;
import android.webkit.SslErrorHandler;
import android.webkit.ValueCallback;
import android.webkit.ConsoleMessage;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.EditText;
import android.widget.ProgressBar;
import android.widget.RadioButton;
import android.widget.RadioGroup;
import android.widget.Toast;

import android.content.pm.PackageManager;
import android.Manifest;



import androidx.activity.result.ActivityResultLauncher;
import androidx.activity.result.contract.ActivityResultContracts;
import androidx.appcompat.app.AppCompatActivity;
import androidx.core.content.ContextCompat;

import org.json.JSONArray;

import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.security.cert.X509Certificate;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.UUID;

import javax.net.ssl.HttpsURLConnection;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509TrustManager;

import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;

/**
 * Main Activity: hosts a fullscreen WebView that connects to the ClawBench server.
 *
 * Key features:
 * - Static HTML login page on first launch (matches web UI style)
 * - WebView hidden during connection attempts — no ugly error pages shown
 * - WebView with JS, DOM storage, and media autoplay enabled
 * - JavaScript interface for native bridge (port forwarding, SSH password)
 * - Port forwarding via SSH tunnels (BackgroundService) — transparent localhost access
 * - Proper back navigation within WebView
 * - SSL error handling with user confirmation
 */
public class MainActivity extends AppCompatActivity {

    private static final String PREFS_NAME = "clawbench_prefs";
    private static final String KEY_SERVER_URL = "server_url";
    private static final String KEY_SSH_PASSWORD = "ssh_password";
    private static final String KEY_SERVER_LIST = "server_list";
    private static final String TAG = "ClawBench";
    private static final String LOGIN_HTML_URL = "file:///android_asset/login.html";

    static MainActivity instance;

    /** Whether the app is currently in the foreground (between onResume and onPause). */
    static volatile boolean isForeground = false;

    WebView webView;
    private ProgressBar progressBar;
    private SharedPreferences prefs;

    // Tracks whether the WebView is showing a successfully loaded remote page.
    // When false (login page or load error), the WebView is hidden behind the dark background.
    private boolean webViewConnected = false;

    // Set to true in onReceivedError() — prevents onPageFinished() from
    // showing the WebView until the login page is displayed.
    // Android WebView calls onPageFinished() even for failed loads, so
    // without this guard the browser error page would flash briefly.
    private boolean loadErrorPending = false;

    // Connection timeout runnable: if the remote page hasn't loaded within
    // TIMEOUT_MS, navigate back to the login page. Prevents the user from
    // being stuck on a black screen when the server is unreachable or slow.
    private Runnable connectionTimeoutRunnable;
    private static final int CONNECTION_TIMEOUT_MS = 15_000;

    // Pending error message to deliver to the login page once it finishes loading.
    // Replaces the old fixed 300ms delay — see showLoginPage() and onPageFinished().
    private String pendingLoginErrorMessage = null;

    // Set to true when the user has confirmed a self-signed SSL certificate at the OkHttp level.
    // When true, onReceivedSslError will auto-accept SSL errors for the current server,
    // because the user has already explicitly trusted the certificate.
    private boolean sslCertTrustedByUser = false;

    // File chooser state for WebView <input type="file"> support
    private ValueCallback<Uri[]> filePathCallback;
    private Uri cameraImageUri; // URI for camera capture image
    private Intent pendingFileChooserIntent; // Stored chooser intent while waiting for camera permission

    // Camera permission launcher — requests CAMERA runtime permission before
    // launching the camera intent. On many OEM ROMs (Xiaomi, Huawei, etc.),
    // ACTION_IMAGE_CAPTURE fails silently without the CAMERA permission granted,
    // even though AOSP doesn't strictly require it.
    private final ActivityResultLauncher<String> cameraPermissionLauncher =
            registerForActivityResult(new ActivityResultContracts.RequestPermission(), isGranted -> {
                if (isGranted) {
                    launchFileChooserWithCamera();
                } else {
                    AppLog.w(TAG, "CAMERA permission denied — launching file chooser without camera option");
                    launchFileChooserWithoutCamera();
                }
            });

    // Notification permission launcher (Android 13+) — required for foreground service notification
    private final ActivityResultLauncher<String> notificationPermissionLauncher =
            registerForActivityResult(new ActivityResultContracts.RequestPermission(), isGranted -> {
                if (!isGranted) {
                    AppLog.w(TAG, "POST_NOTIFICATIONS permission denied — tunnel notification will not show");
                }
            });

    // Activity result launcher for file chooser (replaces deprecated onActivityResult)
    private final ActivityResultLauncher<Intent> fileChooserLauncher =
            registerForActivityResult(new ActivityResultContracts.StartActivityForResult(), result -> {
                if (filePathCallback == null) return;
                Uri[] results = null;
                if (result.getResultCode() == Activity.RESULT_OK && result.getData() != null) {
                    Intent data = result.getData();
                    String dataString = data.getDataString();
                    if (dataString != null) {
                        results = new Uri[]{ Uri.parse(dataString) };
                    } else if (data.getClipData() != null) {
                        // Multiple files selected
                        int count = data.getClipData().getItemCount();
                        results = new Uri[count];
                        for (int i = 0; i < count; i++) {
                            results[i] = data.getClipData().getItemAt(i).getUri();
                        }
                    }
                    // Only use camera URI as fallback when RESULT_OK — this means
                    // the user actively chose the camera option and took a photo.
                    // Without this guard, cancelling the picker would falsely report
                    // the pre-created camera temp file as the "selected" file.
                    if (results == null && cameraImageUri != null) {
                        results = new Uri[]{ cameraImageUri };
                    }
                }
                // Use empty array instead of null for cancellation — some WebView implementations
                // fire the JS change event with stale file data when onReceiveValue(null) is called.
                // An empty array explicitly means "0 files selected".
                filePathCallback.onReceiveValue(results != null ? results : new Uri[0]);
                filePathCallback = null;
                // Clean up unused camera temp file if user didn't take a photo
                if (cameraImageUri != null && (results == null || !cameraImageUri.equals(results[0]))) {
                    new File(cameraImageUri.getPath()).delete();
                }
                cameraImageUri = null;
            });

    // Map of ports currently being forwarded: port -> host (thread-safe for access from WebView background threads)
    final Map<Integer, String> forwardedPorts = new java.util.concurrent.ConcurrentHashMap<>();

    // Volume key interception mode: when true, volume up/down are forwarded to WebView
    // as JS calls instead of adjusting system volume. Controlled by the terminal panel.
    private volatile boolean volumeKeyMode = false;

    // Pending navigation from a notification tap that occurred before the WebView
    // was loaded (cold start). Consumed by WebAppInterface.getPendingNavigation().
    public org.json.JSONObject pendingNavigation = null;

    // Fullscreen video state: managed by WebChromeClient.onShowCustomView/onHideCustomView
    private View customView;
    private WebChromeClient.CustomViewCallback customViewCallback;
    private int originalOrientation;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        instance = this;

        // Check if launched from notification
        logLaunchIntent(getIntent());

        // Initialize trust-all SSL for self-signed HTTPS servers (used by BackgroundService)
        BackgroundService.initTrustAllSSL();

        setContentView(R.layout.activity_main);

        webView = findViewById(R.id.webView);
        progressBar = findViewById(R.id.progressBar);

        prefs = getSharedPreferences(PREFS_NAME, MODE_PRIVATE);

        // Request notification permission (Android 13+) — required for foreground service notification
        requestNotificationPermission();

        // Auto-restore BackgroundService if there are previously saved ports.
        // This ensures the SSH tunnel and its notification are active immediately on cold start,
        // without waiting for the WebView to load and syncToNative() to fire.
        restoreBackgroundServiceIfNeeded();

        setupWebView();

        // Auto-connect if there's a saved URL (user has configured before).
        // This preserves the original behavior: returning users go straight
        // to the app. Only first-time users see the login page.

        // Load saved URL or show configuration dialog
        String savedUrl = prefs.getString(KEY_SERVER_URL, null);
        if (savedUrl != null) {
            // Auto-reconnect: use pre-authentication to verify server is reachable
            // before loading the WebView. This prevents Chrome's built-in error page.
            String savedPassword = prefs.getString(KEY_SSH_PASSWORD, null);
            if (savedPassword != null && !savedPassword.isEmpty()) {
                webView.setVisibility(View.INVISIBLE);
                authenticateAndNavigate(savedUrl, savedPassword);
            } else {
                webView.setVisibility(View.INVISIBLE);
                checkConnectivityAndNavigate(savedUrl);
            }
        } else {
            webView.loadUrl(LOGIN_HTML_URL);
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

        // Critical: allow audio.play() without user gesture (enables TTS in lock screen)
        settings.setMediaPlaybackRequiresUserGesture(false);

        // Allow mixed content (HTTP API calls from HTTPS page, or vice versa)
        settings.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);

        // File access for uploads
        settings.setAllowFileAccess(true);
        settings.setAllowContentAccess(true);

        // Responsive layout
        settings.setUseWideViewPort(true);
        settings.setLoadWithOverviewMode(true);

        // Enable smooth scrolling
        webView.setOverScrollMode(WebView.OVER_SCROLL_NEVER);

        // Set custom user agent
        String ua = settings.getUserAgentString();
        settings.setUserAgentString(ua + " ClawBench-Android/1.0");

        // JavaScript interface for native bridge
        webView.addJavascriptInterface(new WebAppInterface(this), "AndroidNative");

        // WebView client for navigation and error handling
        webView.setWebViewClient(new ClawBenchWebViewClient());

        // Chrome client for progress and file chooser
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
                } else {
                    progressBar.setVisibility(View.GONE);
                }
            }

            @Override
            public boolean onShowFileChooser(WebView webView, ValueCallback<Uri[]> callback, FileChooserParams fileChooserParams) {
                // Cancel any previous pending request
                if (filePathCallback != null) {
                    filePathCallback.onReceiveValue(null);
                }
                filePathCallback = callback;

                // Build the file chooser intent from the <input> element's accept/multiple attributes
                Intent chooserIntent;
                try {
                    chooserIntent = fileChooserParams.createIntent();
                } catch (Exception e) {
                    // Fallback: generic file picker
                    chooserIntent = new Intent(Intent.ACTION_GET_CONTENT);
                    chooserIntent.addCategory(Intent.CATEGORY_OPENABLE);
                    chooserIntent.setType("*/*");
                    if (fileChooserParams.getMode() == FileChooserParams.MODE_OPEN_MULTIPLE) {
                        chooserIntent.putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true);
                    }
                }

                // Store the chooser intent for use after permission check
                pendingFileChooserIntent = chooserIntent;

                // Request CAMERA runtime permission before offering the camera option.
                // Many OEM ROMs (Xiaomi, Huawei, Samsung) require this permission to be
                // granted at runtime, even though AOSP's ACTION_IMAGE_CAPTURE should work
                // without it when using FileProvider URI.
                if (ContextCompat.checkSelfPermission(MainActivity.this, Manifest.permission.CAMERA)
                        == PackageManager.PERMISSION_GRANTED) {
                    launchFileChooserWithCamera();
                } else {
                    cameraPermissionLauncher.launch(Manifest.permission.CAMERA);
                }
                return true;
            }

            @Override
            public void onShowCustomView(View view, CustomViewCallback callback) {
                // If already in fullscreen, dismiss the previous one first
                if (customView != null) {
                    onHideCustomView();
                    return;
                }
                customView = view;
                customViewCallback = callback;

                // Save current orientation and switch to landscape for video
                originalOrientation = getRequestedOrientation();
                setRequestedOrientation(android.content.pm.ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE);

                // Hide the WebView and show the custom fullscreen view
                webView.setVisibility(View.GONE);
                FrameLayout container = findViewById(R.id.webView).getParent() instanceof FrameLayout
                        ? (FrameLayout) findViewById(R.id.webView).getParent() : null;
                if (container != null) {
                    container.addView(view, new FrameLayout.LayoutParams(
                            FrameLayout.LayoutParams.MATCH_PARENT,
                            FrameLayout.LayoutParams.MATCH_PARENT));
                }

                // Hide system UI for immersive fullscreen
                getWindow().getDecorView().setSystemUiVisibility(
                        View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                        | View.SYSTEM_UI_FLAG_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_LAYOUT_STABLE);
            }

            @Override
            public void onHideCustomView() {
                if (customView == null) return;

                // Remove the custom view
                FrameLayout container = customView.getParent() instanceof FrameLayout
                        ? (FrameLayout) customView.getParent() : null;
                if (container != null) {
                    container.removeView(customView);
                }
                customView = null;

                // Restore orientation
                setRequestedOrientation(originalOrientation);

                // Show the WebView again
                webView.setVisibility(View.VISIBLE);

                if (customViewCallback != null) {
                    customViewCallback.onCustomViewHidden();
                    customViewCallback = null;
                }

                // Restore system UI
                getWindow().getDecorView().setSystemUiVisibility(View.SYSTEM_UI_FLAG_VISIBLE);
            }
        });

        // Enable cookies (needed for auth session)
        CookieManager.getInstance().setAcceptThirdPartyCookies(webView, true);

        // Handle file downloads via DownloadManager
        webView.setDownloadListener((url, userAgent, contentDisposition, mimetype, contentLength) -> {
            try {
                DownloadManager.Request request = new DownloadManager.Request(Uri.parse(url));
                // Carry auth cookies so the download is authorized
                String cookies = CookieManager.getInstance().getCookie(url);
                if (cookies != null) {
                    request.addRequestHeader("Cookie", cookies);
                }
                request.setMimeType(mimetype);
                String fileName = getDownloadFileName(url, contentDisposition);
                request.setTitle(fileName);
                request.setDescription(getString(R.string.download_description));
                request.allowScanningByMediaScanner();
                request.setNotificationVisibility(
                        DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED);
                request.setDestinationInExternalPublicDir(
                        Environment.DIRECTORY_DOWNLOADS, "ClawBench/" + fileName);

                DownloadManager dm = (DownloadManager) getSystemService(Context.DOWNLOAD_SERVICE);
                dm.enqueue(request);
                Toast.makeText(this, R.string.download_started, Toast.LENGTH_SHORT).show();
            } catch (Exception e) {
                AppLog.e(TAG, "Download failed", e);
                Toast.makeText(this, R.string.download_failed, Toast.LENGTH_SHORT).show();
            }
        });
    }

    /**
     * Determine the download file name.
     * Priority: Content-Disposition header > URL path (without query params) > "download".
     */
    private String getDownloadFileName(String url, String contentDisposition) {
        // 1. Try Content-Disposition header (sent by server with attachment; filename="...")
        if (contentDisposition != null && !contentDisposition.isEmpty()) {
            // Parse filename*= (RFC 5987) first, then filename=
            String name = parseContentDispositionFilename(contentDisposition);
            if (name != null && !name.isEmpty()) return name;
        }
        // 2. Fallback: extract from URL path, stripping query parameters
        String decoded = Uri.decode(url);
        // Remove query string and fragment
        int queryIdx = decoded.indexOf('?');
        if (queryIdx >= 0) decoded = decoded.substring(0, queryIdx);
        int fragmentIdx = decoded.indexOf('#');
        if (fragmentIdx >= 0) decoded = decoded.substring(0, fragmentIdx);
        int lastSlash = decoded.lastIndexOf('/');
        if (lastSlash >= 0 && lastSlash < decoded.length() - 1) {
            return decoded.substring(lastSlash + 1);
        }
        return "download";
    }

    /**
     * Parse filename from Content-Disposition header.
     * Supports: filename="..." and filename*=UTF-8''... (RFC 5987)
     */
    private String parseContentDispositionFilename(String contentDisposition) {
        // Try filename*= (RFC 5987 encoded) first
        java.util.regex.Matcher extMatcher = java.util.regex.Pattern.compile(
                "filename\\*\\s*=\\s*(?:UTF-8|utf-8)''(.+?)(?:\\s*;|$)")
                .matcher(contentDisposition);
        if (extMatcher.find()) {
            try {
                return java.net.URLDecoder.decode(extMatcher.group(1), "UTF-8");
            } catch (Exception ignored) {}
        }
        // Then try filename="..."
        java.util.regex.Matcher matcher = java.util.regex.Pattern.compile(
                "filename\\s*=\\s*\"?([^\";]+)\"?")
                .matcher(contentDisposition);
        if (matcher.find()) {
            return matcher.group(1).trim();
        }
        return null;
    }

    /**
     * Create a temporary image file for camera capture.
     * Used by onShowFileChooser to provide a URI for the camera intent.
     */
    private File createImageFile() {
        String timestamp = new SimpleDateFormat("yyyyMMdd_HHmmss", Locale.US).format(new Date());
        File storageDir = getExternalFilesDir(Environment.DIRECTORY_PICTURES);
        try {
            return File.createTempFile("IMG_" + timestamp, ".jpg", storageDir);
        } catch (IOException e) {
            AppLog.e(TAG, "Failed to create image file", e);
            return null;
        }
    }

    /**
     * Launch file chooser with camera option included.
     * Called after CAMERA runtime permission is confirmed.
     */
    private void launchFileChooserWithCamera() {
        if (filePathCallback == null) return;
        Intent chooserIntent = pendingFileChooserIntent;
        pendingFileChooserIntent = null;

        // Build camera intent with FileProvider URI
        Intent cameraIntent = null;
        try {
            cameraIntent = new Intent(MediaStore.ACTION_IMAGE_CAPTURE);
            if (cameraIntent.resolveActivity(getPackageManager()) != null) {
                File photoFile = createImageFile();
                if (photoFile != null) {
                    cameraImageUri = androidx.core.content.FileProvider.getUriForFile(
                            this,
                            getPackageName() + ".fileprovider",
                            photoFile
                    );
                    cameraIntent.putExtra(MediaStore.EXTRA_OUTPUT, cameraImageUri);
                    // Grant URI permissions so the camera app can write to the FileProvider URI
                    cameraIntent.addFlags(Intent.FLAG_GRANT_WRITE_URI_PERMISSION | Intent.FLAG_GRANT_READ_URI_PERMISSION);
                } else {
                    cameraIntent = null;
                }
            } else {
                cameraIntent = null;
            }
        } catch (Exception e) {
            AppLog.w(TAG, "Camera intent not available", e);
            cameraIntent = null;
        }

        try {
            if (cameraIntent != null) {
                // Show chooser with both file picker and camera options
                chooserIntent = Intent.createChooser(chooserIntent, "选择文件");
                chooserIntent.putExtra(Intent.EXTRA_INITIAL_INTENTS, new Intent[]{ cameraIntent });
            }
            fileChooserLauncher.launch(chooserIntent);
        } catch (Exception e) {
            AppLog.e(TAG, "File chooser failed to launch", e);
            filePathCallback = null;
        }
    }

    /**
     * Launch file chooser without camera option (fallback when CAMERA permission denied).
     */
    private void launchFileChooserWithoutCamera() {
        if (filePathCallback == null) return;
        Intent chooserIntent = pendingFileChooserIntent;
        pendingFileChooserIntent = null;

        try {
            fileChooserLauncher.launch(chooserIntent);
        } catch (Exception e) {
            AppLog.e(TAG, "File chooser failed to launch", e);
            filePathCallback = null;
        }
    }

    /**
     * Show the static login page. Hides the WebView content area so the
     * dark background shows through, and calls onConnectError() on the
     * login page to display the error message inline.
     * @param errorMessage error message to show, or null for a fresh login.
     */
    private void showLoginPage(String errorMessage) {
        webViewConnected = false;
        loadErrorPending = false;
        sslCertTrustedByUser = false;
        cancelConnectionTimeout();
        // Store error message for delivery after login page finishes loading.
        // See onPageFinished() where pendingLoginErrorMessage is consumed.
        pendingLoginErrorMessage = errorMessage;
        // Note: don't set View.INVISIBLE here — onPageStarted will set VISIBLE
        // for the login page URL, which is the correct time to show it.
        webView.loadUrl(LOGIN_HTML_URL);
    }

    /**
     * Start a connection timeout timer. If the remote page hasn't loaded
     * within CONNECTION_TIMEOUT_MS, navigate back to the login page.
     */
    private void startConnectionTimeout() {
        cancelConnectionTimeout();
        connectionTimeoutRunnable = () -> {
            if (!isFinishing() && !isDestroyed() && !webViewConnected) {
                AppLog.w(TAG, "Connection timeout — returning to login page");
                showLoginPage("连接超时，请检查服务器地址和网络连接。");
            }
        };
        webView.postDelayed(connectionTimeoutRunnable, CONNECTION_TIMEOUT_MS);
    }

    /**
     * Cancel any pending connection timeout timer.
     */
    private void cancelConnectionTimeout() {
        if (connectionTimeoutRunnable != null) {
            webView.removeCallbacks(connectionTimeoutRunnable);
            connectionTimeoutRunnable = null;
        }
    }

    /**
     * Attempt to connect to a server URL.
     * Called from the static login page via AndroidNative.connectToServer().
     * Hides WebView content during the connection attempt so error pages don't flash.
     *
     * All connection errors are handled at the OkHttp level — the WebView is only
     * asked to load a URL after pre-authentication succeeds. This prevents Chrome's
     * built-in error page from appearing when the server is unreachable.
     */
    private void connectToServer(String url, String password) {
        webViewConnected = false;
        loadErrorPending = false;
        sslCertTrustedByUser = false;
        webView.setVisibility(View.INVISIBLE);

        // Save URL and password
        prefs.edit().putString(KEY_SERVER_URL, url).apply();
        if (password != null && !password.isEmpty()) {
            BackgroundService.setPassword(this, password);
        }

        if (isNetworkAvailable()) {
            if (password != null && !password.isEmpty()) {
                authenticateAndNavigate(url, password);
            } else {
                // No password — perform connectivity check first, then navigate WebView.
                // This avoids loading an unreachable URL into the WebView.
                checkConnectivityAndNavigate(url);
            }
        } else {
            // No network — go back to login page with error
            showLoginPage("网络不可用，请检查网络连接。");
        }
    }

    /**
     * Pre-authenticate with the server before navigating the WebView.
     * POSTs /login with the password, extracts the Set-Cookie header,
     * injects it into WebView's CookieManager, then loads the URL.
     *
     * SSL errors are handled at the OkHttp level: a native confirmation dialog
     * is shown, and if the user trusts the certificate, the request is retried
     * with a trusting OkHttp client. The WebView is NEVER asked to load a URL
     * that hasn't been pre-verified, preventing Chrome's built-in error page.
     */
    private void authenticateAndNavigate(String url, String password) {
        new Thread(() -> {
            try {
                AuthResult result = performLoginRequest(url, password);
                handleAuthResponse(result.statusCode, url, password, result.cookies);
            } catch (javax.net.ssl.SSLException e) {
                // SSL error (self-signed cert, hostname mismatch, etc.)
                // Show native confirmation dialog on UI thread, then retry with trusting client
                AppLog.w(TAG, "SSL error during pre-auth, showing confirmation dialog", e);
                runOnUiThread(() -> showSslConfirmationDialog(() -> {
                    try {
                        AuthResult result = performLoginRequestWithClient(buildTrustingOkHttpClient(), url, password);
                        handleAuthResponse(result.statusCode, url, password, result.cookies);
                    } catch (Exception retryEx) {
                        AppLog.w(TAG, "SSL retry failed", retryEx);
                        runOnUiThread(() -> showLoginPage(getNetworkErrorMessage(retryEx)));
                    }
                }));
            } catch (Exception e) {
                // Network error (DNS failure, connection refused, timeout, etc.)
                // Go back to login page — never fallback to webView.loadUrl()
                AppLog.w(TAG, "Pre-auth failed, returning to login page", e);
                String msg = getNetworkErrorMessage(e);
                runOnUiThread(() -> showLoginPage(msg));
            }
        }).start();
    }

    /**
     * Show SSL confirmation dialog. If the user trusts the certificate, runs the provided action
     * on a background thread. This is used by both the pre-authentication path and the
     * connectivity-check path to handle self-signed certificates at the OkHttp level.
     */
    private void showSslConfirmationDialog(Runnable onTrustAction) {
        new AlertDialog.Builder(this)
                .setTitle("SSL 证书验证失败")
                .setMessage("服务器使用了自签名证书，连接可能不安全。\n\n仅当您信任该服务器时才继续。")
                .setPositiveButton("信任并继续", (dialog, which) -> {
                    // Mark that the user has trusted this certificate.
                    // This allows onReceivedSslError to auto-accept when the WebView
                    // loads the same URL (since WebView doesn't share OkHttp's trust store).
                    sslCertTrustedByUser = true;
                    new Thread(onTrustAction).start();
                })
                .setNegativeButton("取消连接", (dialog, which) -> {
                    showLoginPage(null);
                })
                .setCancelable(false)
                .show();
    }

    /**
     * Build an OkHttpClient that trusts all SSL certificates (self-signed, hostname mismatch, etc.).
     * Used after the user explicitly confirms they trust the server's certificate.
     */
    private OkHttpClient buildTrustingOkHttpClient() {
        TrustManager[] trustAll = { new X509TrustManager() {
            public void checkClientTrusted(X509Certificate[] c, String a) {}
            public void checkServerTrusted(X509Certificate[] c, String a) {}
            public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[0]; }
        }};

        try {
            SSLContext sc = SSLContext.getInstance("TLS");
            sc.init(null, trustAll, new java.security.SecureRandom());
            return new OkHttpClient.Builder()
                    .connectTimeout(10, java.util.concurrent.TimeUnit.SECONDS)
                    .readTimeout(10, java.util.concurrent.TimeUnit.SECONDS)
                    .sslSocketFactory(sc.getSocketFactory(), (X509TrustManager) trustAll[0])
                    .hostnameVerifier((hostname, session) -> true)
                    .build();
        } catch (Exception e) {
            // Should never happen — TLS is always available
            throw new RuntimeException("Failed to create trusting OkHttpClient", e);
        }
    }

    /**
     * Extract a user-friendly error message from a network exception.
     */
    private String getNetworkErrorMessage(Exception e) {
        if (e instanceof java.net.UnknownHostException) {
            return "无法解析服务器地址，请检查域名是否正确。";
        } else if (e instanceof java.net.ConnectException) {
            return "无法连接到服务器，请检查地址和端口。";
        } else if (e instanceof java.net.SocketTimeoutException) {
            return "连接超时，请检查服务器地址和网络连接。";
        } else if (e instanceof IOException) {
            return "网络错误，请检查网络连接。";
        } else {
            return "无法连接到服务器，请检查地址和网络连接。";
        }
    }

    /**
     * Perform a health check (GET /api/health) before loading the WebView.
     * Used when no password is provided (server may have no auth).
     * Verifies the server is both reachable AND is a ClawBench instance.
     */
    private void checkConnectivityAndNavigate(String url) {
        new Thread(() -> {
            try {
                String healthError = performHealthCheck(url, new OkHttpClient.Builder()
                        .connectTimeout(10, java.util.concurrent.TimeUnit.SECONDS)
                        .readTimeout(10, java.util.concurrent.TimeUnit.SECONDS)
                        .build());
                if (healthError != null) {
                    runOnUiThread(() -> showLoginPage(healthError));
                    return;
                }
                runOnUiThread(() -> {
                    webView.loadUrl(url);
                    startConnectionTimeout();
                });
            } catch (javax.net.ssl.SSLException e) {
                AppLog.w(TAG, "SSL error during health check, showing confirmation dialog", e);
                runOnUiThread(() -> showSslConfirmationDialog(() -> {
                    try {
                        OkHttpClient trustClient = buildTrustingOkHttpClient();
                        String healthError = performHealthCheck(url, trustClient);
                        if (healthError != null) {
                            runOnUiThread(() -> showLoginPage(healthError));
                            return;
                        }
                        runOnUiThread(() -> {
                            webView.loadUrl(url);
                            startConnectionTimeout();
                        });
                    } catch (Exception retryEx) {
                        AppLog.w(TAG, "SSL health check retry failed", retryEx);
                        runOnUiThread(() -> showLoginPage(getNetworkErrorMessage(retryEx)));
                    }
                }));
            } catch (Exception e) {
                AppLog.w(TAG, "Health check failed", e);
                runOnUiThread(() -> showLoginPage(getNetworkErrorMessage(e)));
            }
        }).start();
    }

    /**
     * Perform GET /api/health and verify the response contains {"app":"clawbench"}.
     * Returns null on success, or an error message string on failure.
     */
    String performHealthCheck(String url, OkHttpClient client) throws Exception {
        Request request = new Request.Builder()
                .url(url + "/api/health")
                .get()
                .build();
        try (Response response = client.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                AppLog.w(TAG, "Health check failed: HTTP " + response.code());
                return "该地址不是 ClawBench 服务器。";
            }
            String body = response.body() != null ? response.body().string() : "";
            if (!body.contains("\"app\"") || !body.contains("\"clawbench\"")) {
                AppLog.w(TAG, "Health check failed: response does not identify as clawbench: " + body);
                return "该地址不是 ClawBench 服务器。";
            }
            return null; // success
        }
    }

    /**
     * Perform the HTTP POST /login request.
     * Extracted for testability — can be overridden in tests to mock network calls.
     *
     * @param url      the server base URL
     * @param password the password to authenticate with
     * @return AuthResult with status code and Set-Cookie headers
     */
    AuthResult performLoginRequest(String url, String password) throws Exception {
        return performLoginRequestWithClient(
                new OkHttpClient.Builder()
                        .connectTimeout(10, java.util.concurrent.TimeUnit.SECONDS)
                        .readTimeout(10, java.util.concurrent.TimeUnit.SECONDS)
                        .build(),
                url, password);
    }

    /**
     * Perform the HTTP POST /login request with a pre-built OkHttpClient.
     * Shared by performLoginRequest (standard client) and the SSL retry path (trusting client).
     */
    private AuthResult performLoginRequestWithClient(OkHttpClient client, String url, String password) throws Exception {
        org.json.JSONObject json = new org.json.JSONObject();
        json.put("password", password);
        RequestBody body = RequestBody.create(json.toString(),
                MediaType.parse("application/json; charset=utf-8"));

        Request request = new Request.Builder()
                .url(url + "/login")
                .post(body)
                .build();

        try (Response response = client.newCall(request).execute()) {
            java.util.List<String> cookies = response.headers("Set-Cookie");
            return new AuthResult(response.code(), cookies);
        }
    }

    /**
     * Result of the pre-authentication POST /login request.
     */
    static class AuthResult {
        final int statusCode;
        final java.util.List<String> cookies;

        AuthResult(int statusCode, java.util.List<String> cookies) {
            this.statusCode = statusCode;
            this.cookies = cookies;
        }
    }

    /**
     * Handle the server's response to the pre-authentication POST /login.
     * Extracted from authenticateAndNavigate for testability.
     *
     * @param statusCode HTTP status code from the login response
     * @param url        the server URL to navigate to on success/fallback
     * @param cookies    Set-Cookie headers from the response (may be empty)
     */
    void handleAuthResponse(int statusCode, String url, String password, java.util.List<String> cookies) {
        if (statusCode == 200) {
            // Extract Set-Cookie and inject into WebView CookieManager.
            // Before injecting, clear all ClawBench cookies for the target domain
            // to prevent stale cookies from a previous server instance (e.g., switching
            // from port 20000 which uses unscoped "clawbench_session" to port 20300
            // which uses "cb20300_clawbench_session"). Without this cleanup, the old
            // unscoped cookie would be sent alongside the new scoped cookie, causing
            // confusion on the server side. (Browsers/WebView do not isolate cookies
            // by port — only by domain + path.)
            try {
                CookieManager cm = CookieManager.getInstance();
                clearClawBenchCookies(cm, url);
                if (cookies != null) {
                    for (String cookie : cookies) {
                        cm.setCookie(url, cookie);
                    }
                }
                cm.flush();
            } catch (Exception e) {
                // CookieManager may be unavailable in test environments
                AppLog.w(TAG, "Failed to inject auth cookie", e);
            }
            // Auth success — verify this is a ClawBench server before navigating WebView
            try {
                OkHttpClient healthClient = new OkHttpClient.Builder()
                        .connectTimeout(10, java.util.concurrent.TimeUnit.SECONDS)
                        .readTimeout(10, java.util.concurrent.TimeUnit.SECONDS)
                        .build();
                String healthError = performHealthCheck(url, healthClient);
                if (healthError != null) {
                    runOnUiThread(() -> showLoginPage(healthError));
                    return;
                }
            } catch (Exception e) {
                AppLog.w(TAG, "Health check after auth failed", e);
                runOnUiThread(() -> showLoginPage(getNetworkErrorMessage(e)));
                return;
            }
            // Health check passed — promote server to head of list, then navigate
            runOnUiThread(() -> {
                saveServerInternal(url, password);
                webView.loadUrl(url);
                startConnectionTimeout();
            });
        } else if (statusCode == 401) {
            // Wrong password — go back to login page with error
            runOnUiThread(() -> showLoginPage("密码错误，请检查登录密码。"));
        } else if (statusCode == 429) {
            runOnUiThread(() -> showLoginPage("尝试次数过多，请稍后再试。"));
        } else {
            // Unexpected status — do not navigate WebView, return to login page
            String msg;
            if (statusCode >= 500) {
                msg = "服务器错误 (" + statusCode + ")，请稍后重试。";
            } else {
                msg = "连接失败 (" + statusCode + ")，请检查服务器地址。";
            }
            final String errorMsg = msg;
            runOnUiThread(() -> showLoginPage(errorMsg));
        }
    }

    /**
     * Clear all ClawBench-related cookies for the given URL's domain.
     * This is necessary when switching between server instances on different ports
     * because browsers/WebView do not isolate cookies by port — only by domain + path.
     * Without this cleanup, stale cookies from a previous server (e.g., unscoped
     * "clawbench_session" from port 20000) would be sent alongside the new server's
     * scoped cookies (e.g., "cb20300_clawbench_session"), causing auth failures or
     * incorrect behavior on the new server.
     *
     * The method parses the cookie string from CookieManager.getCookie(), identifies
     * all ClawBench cookies (both unscoped and port-scoped variants), and removes
     * them by setting expired versions.
     */
    private void clearClawBenchCookies(CookieManager cm, String url) {
        String existing = cm.getCookie(url);
        if (existing == null || existing.isEmpty()) return;

        // Parse the URL to build the cookie removal string with correct attributes
        String path = "/";
        boolean secure = url.startsWith("https://");
        try {
            android.net.Uri parsed = android.net.Uri.parse(url);
            String pathPart = parsed.getPath();
            if (pathPart != null && !pathPart.isEmpty() && !pathPart.equals("/")) {
                path = pathPart;
            }
        } catch (Exception ignored) {}

        // Build removal suffix: expires in the past, correct path, and secure flag if HTTPS
        String removeSuffix = "=; Path=" + path + "; Max-Age=0" + (secure ? "; Secure" : "");

        // Known ClawBench cookie name patterns (unscoped + cb{port}_ scoped variants)
        String[] baseNames = {"clawbench_session", "clawbench_project", "clawbench-locale"};
        for (String pair : existing.split(";")) {
            String trimmed = pair.trim();
            int eqIdx = trimmed.indexOf('=');
            if (eqIdx < 0) continue;
            String name = trimmed.substring(0, eqIdx).trim();
            // Remove any ClawBench cookie: unscoped or port-scoped (cb{port}_)
            for (String base : baseNames) {
                if (name.equals(base) || name.matches("cb\\d+_" + java.util.regex.Pattern.quote(base))) {
                    cm.setCookie(url, name + removeSuffix);
                    break;
                }
            }
        }
    }

    @SuppressWarnings("deprecation")
    boolean isNetworkAvailable() {
        ConnectivityManager cm = (ConnectivityManager) getSystemService(Context.CONNECTIVITY_SERVICE);
        if (cm == null) return false;
        NetworkInfo ni = cm.getActiveNetworkInfo();
        return ni != null && ni.isConnected();
    }

    /**
     * Request POST_NOTIFICATIONS runtime permission on Android 13+ (API 33+).
     * Required for the BackgroundService foreground notification to be visible.
     */
    private void requestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                    != PackageManager.PERMISSION_GRANTED) {
                notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS);
            }
        }
    }

    /**
     * Show the login page for server configuration.
     * Replaces the old AlertDialog-based server dialog with the
     * static HTML login page that matches the web UI style.
     */
    private void showServerDialog() {
        showLoginPage(null);
    }

    /**
     * If there are previously saved forwarded ports in SharedPreferences,
     * start the BackgroundService immediately so the SSH tunnel and its
     * notification are active on cold start.
     * This avoids the gap where no notification shows until the WebView
     * finishes loading and syncToNative() fires.
     */
    private void restoreBackgroundServiceIfNeeded() {
        Set<String> savedPorts = prefs.getStringSet("forwarded_ports", null);
        if (savedPorts != null && !savedPorts.isEmpty()) {
            AppLog.i(TAG, "Cold start: restoring BackgroundService with " + savedPorts.size() + " saved ports");
            BackgroundService.start(this);
        }
    }

    /** Timestamp of the last unhandled back press (for double-back-to-exit) */
    private long lastBackPressTime = 0;
    private static final long BACK_PRESS_TIMEOUT = 2000; // ms

    @Override
    public void onBackPressed() {
        // If in fullscreen video mode, exit fullscreen first
        if (customView != null) {
            WebChromeClient client = webView.getWebChromeClient();
            if (client != null) {
                client.onHideCustomView();
            }
            return;
        }
        // If currently on the login page, apply double-back-to-exit
        String currentUrl = webView.getUrl();
        if (currentUrl != null && currentUrl.equals(LOGIN_HTML_URL)) {
            if (System.currentTimeMillis() - lastBackPressTime < BACK_PRESS_TIMEOUT) {
                lastBackPressTime = 0;
                super.onBackPressed();
            } else {
                lastBackPressTime = System.currentTimeMillis();
                Toast.makeText(this, R.string.press_again_to_exit, Toast.LENGTH_SHORT).show();
            }
            return;
        }
        // If the WebView is not connected (stuck on black screen or error),
        // go back to the login page instead of exiting the app.
        if (!webViewConnected) {
            showLoginPage(null);
            return;
        }
        // Delegate to JS: dispatch a clawbench-back-press event.
        // The JS layer checks if any drill-down page can navigate back.
        // If it can, the JS handler calls goBack() and sets __clawbenchBackHandled = true.
        // If not, the JS layer implements double-back-to-exit:
        //   - First press: shows toast tip, sets __clawbenchBackHandled = true (prevents exit)
        //   - Second press within 2s: sets __clawbenchBackHandled = false (allows exit)
        webView.evaluateJavascript(
            "(function() {" +
            "  if (typeof window.__clawbenchBackHandled === 'undefined') window.__clawbenchBackHandled = false;" +
            "  window.__clawbenchBackHandled = false;" +
            "  window.dispatchEvent(new CustomEvent('clawbench-back-press'));" +
            "  return window.__clawbenchBackHandled;" +
            "})()",
            result -> {
                boolean handled = "true".equals(result);
                if (!handled) {
                    // JS confirmed exit — second press within timeout
                    super.onBackPressed();
                }
            }
        );
    }

    @Override
    protected void onDestroy() {
        // Do NOT stop BackgroundService here — it should survive Activity lifecycle
        // so the SSH tunnel continues running when the app is in background.
        cancelConnectionTimeout();
        cleanupSharedCacheDir();
        instance = null; // Clear static reference to prevent memory leak / stale access
        super.onDestroy();
    }

    @Override
    protected void onPause() {
        super.onPause();
        isForeground = false;
        pauseWebView();
        // App going to background — start native WS so we still get
        // notifications when Android kills the WebView process.
        if (webViewConnected) {
            BackgroundService.startNativeEventWs(this);
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        isForeground = true;
        resumeWebView();
        // App returning to foreground — stop native WS (WebView WS handles events)
        BackgroundService.stopNativeEventWs(this);
        // Handle notification tap intent + re-dispatch pending navigation
        handleResumeIntent();
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
     * Handle notification intent and re-dispatch pending navigation on resume.
     * Extracted from onResume() for testability (lifecycle methods call super which
     * requires Android framework, making them untestable in pure JUnit).
     */
    void handleResumeIntent() {
        Intent intent = getIntent();
        AppLog.i(TAG, "MainActivity: onResume intent=" + intent
                + ", action=" + (intent != null ? intent.getAction() : "null")
                + ", extras=" + (intent != null ? intent.getExtras() : "null"));
        handleNotificationIntent(intent);
        redispatchPendingNavigation();
    }

    /**
     * Re-dispatch pending navigation if it wasn't consumed yet.
     * (e.g., CustomEvent was dispatched while WebView was paused/suspended)
     */
    void redispatchPendingNavigation() {
        if (pendingNavigation != null && webView != null) {
            AppLog.i(TAG, "MainActivity: onResume - re-dispatching pendingNavigation=" + pendingNavigation.toString());
            final String jsArg = pendingNavigation.toString();
            // Choose event name based on navigation type: task vs session
            String eventName = pendingNavigation.has("taskId") ? "clawbench-open-task" : "clawbench-open-session";
            AppLog.i(TAG, "MainActivity: onResume - dispatching " + eventName);
            webView.evaluateJavascript(
                "window.dispatchEvent(new CustomEvent('" + eventName + "', { detail: " + jsArg + " }))",
                result -> {
                    AppLog.i(TAG, "MainActivity: onResume re-dispatch evaluateJavascript result=" + result);
                    pendingNavigation = null;
                }
            );
        }
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        handleNotificationIntent(intent);
    }

    /**
     * Handle intent extras from notification taps.
     * For session notifications: dispatches clawbench-open-session event (navigate to chat).
     * For task notifications: dispatches clawbench-open-task event (navigate to task execution detail).
     */
    void handleNotificationIntent(Intent intent) {
        AppLog.i(TAG, "MainActivity: handleNotificationIntent called, intent=" + intent);
        if (intent == null) {
            AppLog.i(TAG, "MainActivity: handleNotificationIntent - intent is null, skipping");
            return;
        }
        String sessionId = intent.getStringExtra("session_id");
        String taskId = intent.getStringExtra("task_id");
        String executionId = intent.getStringExtra("execution_id");
        String eventType = intent.getStringExtra("event_type");
        String projectPath = intent.getStringExtra("project_path");
        AppLog.i(TAG, "MainActivity: handleNotificationIntent - sessionId=" + sessionId
                + ", taskId=" + taskId + ", executionId=" + executionId
                + ", eventType=" + eventType + ", projectPath=" + projectPath);

        // Also dump all intent extras for debugging
        Bundle extras = intent.getExtras();
        if (extras != null) {
            for (String key : extras.keySet()) {
                AppLog.i(TAG, "MainActivity: intent extra: " + key + "=" + extras.get(key));
            }
        }

        // Determine navigation type: task notification vs session notification
        boolean isTaskNotification = taskId != null || "task_update".equals(eventType);

        if (isTaskNotification && taskId != null) {
            // Task notification: navigate to task execution detail
            AppLog.i(TAG, "MainActivity: handleNotificationIntent - task notification, dispatching clawbench-open-task");
            try {
                org.json.JSONObject detail = new org.json.JSONObject();
                detail.put("taskId", taskId);
                if (executionId != null) detail.put("executionId", executionId);
                if (sessionId != null) detail.put("sessionId", sessionId);
                if (projectPath != null) detail.put("projectPath", projectPath);
                // Store as pending navigation for cold-start fallback (getPendingNavigation bridge)
                pendingNavigation = detail;
                AppLog.i(TAG, "MainActivity: stored pendingNavigation=" + detail.toString());
                if (webView != null) {
                    AppLog.i(TAG, "MainActivity: webView available, dispatching clawbench-open-task event");
                    webView.evaluateJavascript(
                        "window.dispatchEvent(new CustomEvent('clawbench-open-task', { detail: " + detail.toString() + " }))",
                        result -> {
                            AppLog.i(TAG, "MainActivity: clawbench-open-task evaluateJavascript result=" + result);
                            pendingNavigation = null;
                        }
                    );
                } else {
                    AppLog.w(TAG, "MainActivity: webView is null, cannot dispatch event (pendingNavigation stored for cold-start)");
                }
            } catch (Exception e) {
                AppLog.w(TAG, "MainActivity: failed to dispatch clawbench-open-task event from notification", e);
            }
            // Clear extras so we don't re-dispatch on subsequent onResume
            intent.removeExtra("task_id");
            intent.removeExtra("execution_id");
            intent.removeExtra("event_type");
            intent.removeExtra("session_id");
            intent.removeExtra("project_path");
            AppLog.i(TAG, "MainActivity: cleared intent extras to prevent re-dispatch");
        } else if (sessionId != null) {
            // Session notification: navigate to chat session
            AppLog.i(TAG, "MainActivity: handleNotificationIntent - session_id found, dispatching navigation");
            try {
                org.json.JSONObject detail = new org.json.JSONObject();
                detail.put("sessionId", sessionId);
                if (projectPath != null) detail.put("projectPath", projectPath);
                // Store as pending navigation for cold-start fallback (getPendingNavigation bridge)
                pendingNavigation = detail;
                AppLog.i(TAG, "MainActivity: stored pendingNavigation=" + detail.toString());
                if (webView != null) {
                    AppLog.i(TAG, "MainActivity: webView available, dispatching clawbench-open-session event");
                    webView.evaluateJavascript(
                        "window.dispatchEvent(new CustomEvent('clawbench-open-session', { detail: " + detail.toString() + " }))",
                        result -> {
                            AppLog.i(TAG, "MainActivity: evaluateJavascript result=" + result);
                            // JS event dispatched successfully — clear pendingNavigation
                            // so onResume re-dispatch won't fire again
                            pendingNavigation = null;
                        }
                    );
                } else {
                    AppLog.w(TAG, "MainActivity: webView is null, cannot dispatch event (pendingNavigation stored for cold-start)");
                }
            } catch (Exception e) {
                AppLog.w(TAG, "MainActivity: failed to dispatch open-session event from notification", e);
            }
            // Clear extras so we don't re-dispatch on subsequent onResume
            intent.removeExtra("session_id");
            intent.removeExtra("project_path");
            intent.removeExtra("event_type");
            AppLog.i(TAG, "MainActivity: cleared intent extras to prevent re-dispatch");
        } else {
            AppLog.i(TAG, "MainActivity: handleNotificationIntent - no session_id or task_id in intent extras");
        }
    }

    // --- Share Out ---

    /** Get or create the shared temp files directory under cacheDir. */
    private File getSharedCacheDir() {
        return SharedCacheUtils.getSharedCacheDir(getCacheDir());
    }

    /** Clean up all files in the shared cache directory. Called in onDestroy. */
    private void cleanupSharedCacheDir() {
        SharedCacheUtils.cleanupSharedCacheDir(getCacheDir());
    }

    /**
     * Log launch intent extras (session_id/project_path from notification).
     * Extracted from onCreate() for testability.
     */
    void logLaunchIntent(Intent launchIntent) {
        if (launchIntent != null) {
            String sid = launchIntent.getStringExtra("session_id");
            String pp = launchIntent.getStringExtra("project_path");
            AppLog.i(TAG, "MainActivity: onCreate intent extras: session_id=" + sid + ", project_path=" + pp);
        }
    }

    /**
     * Intercept volume key events when volumeKeyMode is enabled (terminal panel open).
     * Instead of adjusting system volume, dispatch them to the WebView as JS callbacks.
     * All other keys fall through to the default handling.
     */
    @Override
    public boolean dispatchKeyEvent(KeyEvent event) {
        if (volumeKeyMode) {
            int keyCode = event.getKeyCode();
            if (keyCode == KeyEvent.KEYCODE_VOLUME_UP || keyCode == KeyEvent.KEYCODE_VOLUME_DOWN) {
                // Only act on ACTION_DOWN to avoid double-firing on ACTION_UP
                if (event.getAction() == KeyEvent.ACTION_DOWN) {
                    String direction = keyCode == KeyEvent.KEYCODE_VOLUME_UP ? "up" : "down";
                    webView.evaluateJavascript(
                            "if(typeof __onVolumeKey==='function'){__onVolumeKey('" + direction + "')}", null);
                }
                return true; // consume the event — no system volume change
            }
        }
        return super.dispatchKeyEvent(event);
    }

    // --- WebView Client ---

    private class ClawBenchWebViewClient extends WebViewClient {

        @Override
        public void onPageStarted(WebView view, String url, android.graphics.Bitmap favicon) {
            super.onPageStarted(view, url, favicon);
            if (LOGIN_HTML_URL.equals(url)) {
                // Navigating to the login page — show it immediately.
                // The login page IS the UI, not a transitional state.
                webViewConnected = false;
                loadErrorPending = false;
                view.setVisibility(View.VISIBLE);
            } else {
                // Navigating to a remote page — hide WebView until it loads
                // to prevent flashing ugly browser error pages.
                webViewConnected = false;
                loadErrorPending = false;
                view.setVisibility(View.INVISIBLE);
            }
        }

        @Override
        public void onPageFinished(WebView view, String url) {
            super.onPageFinished(view, url);
            if (LOGIN_HTML_URL.equals(url)) {
                // Login page finished loading — deliver any pending error message.
                // This is more reliable than a fixed delay (the old 300ms approach)
                // because it waits for the page to actually be ready.
                cancelConnectionTimeout();
                if (pendingLoginErrorMessage != null) {
                    String msg = pendingLoginErrorMessage;
                    pendingLoginErrorMessage = null;
                    String escaped = msg.replace("\\", "\\\\").replace("'", "\\'").replace("\n", "\\n");
                    view.evaluateJavascript("if(typeof onConnectError==='function'){onConnectError('" + escaped + "')}", null);
                }
            } else if (loadErrorPending) {
                // Error was received during this page load — don't show the WebView.
                // The delayed showLoginPage() will handle the transition.
                // This prevents the browser's built-in error page from flashing.
            } else {
                // Remote page finished loading successfully — show the WebView.
                webViewConnected = true;
                cancelConnectionTimeout();
                view.setVisibility(View.VISIBLE);
            }
        }

        @Override
        public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
            Uri url = request.getUrl();

            // Allow content:// URIs only from our own FileProvider (camera capture, etc.)
            if ("content".equals(url.getScheme())) {
                if (url.getAuthority().equals(getPackageName() + ".fileprovider")) {
                    return false;
                }
                return true; // Block other content:// URIs
            }

            String host = url.getHost();

            // Allow localhost and the configured server
            String serverUrl = prefs.getString(KEY_SERVER_URL, "");
            String serverHost = Uri.parse(serverUrl).getHost();

            if ("localhost".equals(host) || "127.0.0.1".equals(host) || host.equals(serverHost)) {
                return false; // Load in WebView
            }

            // Open external links in system browser
            Intent intent = new Intent(Intent.ACTION_VIEW, url);
            startActivity(intent);
            return true;
        }

        @Override
        public void onReceivedSslError(WebView view, SslErrorHandler handler, SslError error) {
            String url = view.getUrl();
            // Auto-accept SSL errors for localhost (SSH tunnel forwards — cert won't match localhost)
            if (url != null && (url.startsWith("https://localhost:") || url.startsWith("https://127.0.0.1:"))) {
                handler.proceed();
                return;
            }
            // Auto-accept SSL errors if the user already confirmed the certificate
            // at the OkHttp level during pre-authentication.
            if (sslCertTrustedByUser) {
                AppLog.i(TAG, "Auto-accepting SSL error: user already confirmed certificate");
                handler.proceed();
                return;
            }
            // Unexpected SSL error — WebView should not encounter this because
            // all connections are pre-verified at the OkHttp level.
            AppLog.w(TAG, "Unexpected SSL error in WebView, returning to login");
            handler.cancel();
            loadErrorPending = true;
            view.setVisibility(View.INVISIBLE);
            view.postDelayed(() -> {
                if (!isFinishing() && !isDestroyed() && !webViewConnected && loadErrorPending) {
                    showLoginPage("SSL 连接异常，请重新连接。");
                }
            }, 600);
        }

        @Override
        public void onReceivedError(WebView view, WebResourceRequest request, WebResourceError error) {
            super.onReceivedError(view, request, error);
            // Only handle main frame errors — show login page when the remote page fails to load.
            if (request.isForMainFrame()) {
                // Set flag immediately to block onPageFinished() from showing the WebView.
                // Android WebView calls onPageFinished() even for failed loads, and without
                // this flag the browser's built-in error page would flash before we can
                // navigate back to the login page.
                loadErrorPending = true;
                view.setVisibility(View.INVISIBLE);

                // Defer the navigation to login page: if the connection recovers before
                // the deferred runnable fires (e.g. screen unlock), we avoid showing a
                // stale error. But since loadErrorPending is already set, onPageFinished
                // won't flash the error page even if it fires in the meantime.
                view.postDelayed(() -> {
                    if (!isFinishing() && !isDestroyed() && !webViewConnected && loadErrorPending) {
                        showLoginPage("无法连接到服务器，请检查地址和网络连接。");
                    }
                }, 600);
            }
        }

        @Override
        public void onReceivedHttpError(WebView view, WebResourceRequest request, WebResourceResponse errorResponse) {
            super.onReceivedHttpError(view, request, errorResponse);
            // Only handle main frame HTTP errors (4xx/5xx) during initial connection.
            // Once the app is loaded (webViewConnected), HTTP errors are handled by the
            // Vue frontend (e.g., 401 redirects to login within the SPA).
            if (request.isForMainFrame() && !webViewConnected) {
                int statusCode = errorResponse.getStatusCode();
                AppLog.w(TAG, "Main frame HTTP error during connection: " + statusCode);
                loadErrorPending = true;
                view.setVisibility(View.INVISIBLE);
                String msg;
                if (statusCode == 401 || statusCode == 403) {
                    msg = "认证失败，请检查密码是否正确。";
                } else if (statusCode >= 500) {
                    msg = "服务器错误 (" + statusCode + ")，请稍后重试。";
                } else if (statusCode >= 400) {
                    msg = "请求错误 (" + statusCode + ")，请检查服务器地址。";
                } else {
                    msg = "连接失败，请检查服务器地址和网络连接。";
                }
                view.postDelayed(() -> {
                    if (!isFinishing() && !isDestroyed() && !webViewConnected && loadErrorPending) {
                        showLoginPage(msg);
                    }
                }, 600);
            }
        }

        @Override
        public boolean onRenderProcessGone(WebView view, android.webkit.RenderProcessGoneDetail detail) {
            // WebView renderer crashed (OOM, GPU failure, etc.)
            // Reset state and recover by showing the login page.
            AppLog.e(TAG, "WebView renderer crashed! didCrash=" + detail.didCrash());
            webViewConnected = false;
            loadErrorPending = false;
            cancelConnectionTimeout();
            // The WebView is in an unusable state — destroy and recreate it.
            // Simply showing the login page won't work because the renderer is dead.
            runOnUiThread(() -> recreateWebViewAfterCrash(view));
            return true; // We handled the crash — don't let the default behavior show a blank screen
        }
    }

    /**
     * Recreate the WebView after a renderer crash.
     * Separated for testability — the core state reset happens in onRenderProcessGone
     * before this is called. This method only handles the UI recovery.
     */
    void recreateWebViewAfterCrash(WebView crashedView) {
        try {
            android.view.ViewGroup parent = (android.view.ViewGroup) crashedView.getParent();
            int index = parent.indexOfChild(crashedView);
            parent.removeView(crashedView);
            crashedView.destroy();
            WebView newView = new WebView(this);
            parent.addView(newView, index);
            webView = newView;
            setupWebView();
            showLoginPage("页面渲染异常，请重新连接。");
        } catch (Exception e) {
            AppLog.e(TAG, "Failed to recreate WebView after crash", e);
            finish();
        }
    }

    // --- JavaScript Interface ---

    public static class WebAppInterface {
        private final MainActivity activity;

        public WebAppInterface(MainActivity activity) {
            this.activity = activity;
        }

        @JavascriptInterface
        public String getAppVersion() {
            try {
                return activity.getPackageManager()
                        .getPackageInfo(activity.getPackageName(), 0).versionName;
            } catch (Exception e) {
                return "1.0.0";
            }
        }

        @JavascriptInterface
        public boolean isNativeApp() {
            return true;
        }

        /**
         * Check whether the SSH tunnel is currently connected.
         * Queries the BackgroundService's SSH session state.
         * Returns true if connected, false if disconnected or service not running.
         */
        @JavascriptInterface
        public boolean isTunnelConnected() {
            return BackgroundService.isTunnelConnected();
        }

        /**
         * Get the last SSH connection error message.
         * Returns empty string if no error, or a descriptive error message.
         * Used by the frontend to show specific failure reasons
         * (auth failure, network unreachable, etc.) in the tunnel status banner.
         */
        @JavascriptInterface
        public String getTunnelError() {
            String err = BackgroundService.getLastError();
            return err != null ? err : "";
        }

        /**
         * Get the type of the last SSH connection error.
         * Returns one of: "auth", "network", "hostkey", "unknown", or empty string if no error.
         * Used by the frontend to show localized error messages.
         */
        @JavascriptInterface
        public String getTunnelErrorType() {
            String type = BackgroundService.getErrorType();
            return type != null ? type : "";
        }

        /**
         * Add a port to be forwarded via SSH tunnel.
         * The BackgroundService creates a local port forward: localhost:{port} → server:{port}
         * WebView can then access http://localhost:{port} directly.
         * Also requests battery optimization exemption on first port forward.
         */
        @JavascriptInterface
        public void addForwardedPort(int localPort, int targetPort, String host) {
            AppLog.i(TAG, "addForwardedPort: localPort=" + localPort + ", targetPort=" + targetPort + ", host=" + host);
            activity.runOnUiThread(() -> {
                activity.forwardedPorts.put(localPort, host != null ? host : "");
                BackgroundService.addForwardedPort(activity, localPort, targetPort, host != null ? host : "");

                // Request battery optimization exemption if not already granted.
                // Re-check every time in case the user previously dismissed the dialog.
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                    PowerManager pm = (PowerManager) activity.getSystemService(Context.POWER_SERVICE);
                    if (pm != null && !pm.isIgnoringBatteryOptimizations(activity.getPackageName())) {
                        requestIgnoreBatteryOptimization();
                    }
                }
            });
        }

        /**
         * Request the system to exclude ClawBench from battery optimization.
         * This prevents the OS from aggressively killing the port forward service.
         * Only requested once — tracked via SharedPreferences.
         */
        private void requestIgnoreBatteryOptimization() {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                PowerManager pm = (PowerManager) activity.getSystemService(Context.POWER_SERVICE);
                String packageName = activity.getPackageName();
                if (pm != null && !pm.isIgnoringBatteryOptimizations(packageName)) {
                    try {
                        Intent intent = new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS);
                        intent.setData(Uri.parse("urn:android:pkg:" + packageName));
                        activity.startActivity(intent);
                        BackgroundService.setBatteryOptRequested(activity);
                        AppLog.i(TAG, "Requested battery optimization exemption");
                    } catch (Exception e) {
                        AppLog.w(TAG, "Failed to request battery optimization exemption", e);
                    }
                } else {
                    // Already whitelisted, just mark as requested
                    BackgroundService.setBatteryOptRequested(activity);
                }
            }
        }

        /**
         * Remove a port forward.
         */
        @JavascriptInterface
        public void removeForwardedPort(int port) {
            activity.runOnUiThread(() -> {
                activity.forwardedPorts.remove(port);
                BackgroundService.removeForwardedPort(activity, port);
            });
        }

        /**
         * Stop the BackgroundService and disconnect SSH.
         * Called from WebView when server reports no forwarded ports,
         * to avoid running an idle foreground service with no work to do.
         */
        @JavascriptInterface
        public void stopBackgroundService() {
            activity.runOnUiThread(() -> {
                AppLog.i(TAG, "WebView requested BackgroundService stop (no ports on server)");
                activity.forwardedPorts.clear();
                BackgroundService.stop(activity);
            });
        }

        @JavascriptInterface
        public String getForwardedPorts() {
            try {
                JSONArray arr = new JSONArray();
                for (Map.Entry<Integer, String> entry : activity.forwardedPorts.entrySet()) {
                    org.json.JSONObject obj = new org.json.JSONObject();
                    obj.put("port", entry.getKey());
                    obj.put("host", entry.getValue());
                    arr.put(obj);
                }
                return arr.toString();
            } catch (Exception e) {
                return "[]";
            }
        }

        @JavascriptInterface
        public String getServerUrl() {
            return activity.prefs.getString(KEY_SERVER_URL, "");
        }

        /**
         * Connect to a server URL with the given password.
         * Called from the static login page's "连接" button.
         * Hides the WebView during the connection attempt so error pages don't flash.
         */
        @JavascriptInterface
        public void connectToServer(String url, String password) {
            activity.runOnUiThread(() -> activity.connectToServer(url, password));
        }

        /**
         * Get the saved server configuration as JSON.
         * Used by the static login page to pre-fill the form fields.
         * Returns: {"protocol":"https|http", "host":"...", "port":"...", "password":"..."}
         */
        @JavascriptInterface
        public String getSavedServerConfig() {
            try {
                String savedUrl = activity.prefs.getString(KEY_SERVER_URL, "");
                String savedPassword = activity.prefs.getString(KEY_SSH_PASSWORD, "");
                org.json.JSONObject config = new org.json.JSONObject();
                if (!savedUrl.isEmpty()) {
                    Uri parsed = Uri.parse(savedUrl);
                    config.put("protocol", parsed.getScheme() != null ? parsed.getScheme() : "https");
                    config.put("host", parsed.getHost() != null ? parsed.getHost() : "");
                    config.put("port", parsed.getPort() > 0 ? String.valueOf(parsed.getPort()) : "");
                } else {
                    config.put("protocol", "https");
                    config.put("host", "");
                    config.put("port", "");
                }
                config.put("password", savedPassword);
                return config.toString();
            } catch (Exception e) {
                return "{}";
            }
        }

        /**
         * Show the server configuration dialog (change URL/password).
         * Called from WebView when connection fails or user wants to reconfigure.
         */
        @JavascriptInterface
        public void showServerDialog() {
            activity.runOnUiThread(() -> activity.showServerDialog());
        }

        /**
         * Open a forwarded port in the system browser.
         * Called from the port forwarding panel "open" button.
         */
        @JavascriptInterface
        public void openInBrowser(int port, String protocol, String host) {
            AppLog.i(TAG, "openInBrowser: port=" + port + ", protocol=" + protocol + ", host=" + host);
            activity.runOnUiThread(() -> {
                String scheme = "https".equalsIgnoreCase(protocol) ? "https" : "http";
                // External browser accesses the SSH tunnel on localhost, not the original host
                String url = scheme + "://localhost:" + port;
                Intent intent = new Intent(Intent.ACTION_VIEW, Uri.parse(url));
                intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
                activity.startActivity(intent);
            });
        }

        /**
         * Open a forwarded port in the sandbox browser (BrowserActivity).
         * Runs in a separate process for full Cookie/Storage isolation from the main app.
         * Called from the port forwarding panel "open" button (preferred over openInBrowser).
         *
         * BrowserActivity uses singleTask launchMode, so if an instance already exists,
         * Android brings it to the foreground and calls onNewIntent instead of creating
         * a new Activity (which would reload the WebView).
         */
        @JavascriptInterface
        public void openInSandbox(int port, String protocol, String host) {
            AppLog.i(TAG, "openInSandbox: port=" + port + ", protocol=" + protocol + ", host=" + host);
            activity.runOnUiThread(() -> {
                String scheme = "https".equalsIgnoreCase(protocol) ? "https" : "http";
                Intent intent = new Intent(activity, BrowserActivity.class);
                intent.putExtra("port", port);
                intent.putExtra("protocol", scheme);
                intent.putExtra("host", host != null ? host : "");
                activity.startActivity(intent);
            });
        }

        /**
         * Test whether a local forwarded port is reachable by attempting a TCP connect.
         * Uses a very short timeout (500ms) since we're connecting to localhost.
         * Returns true if the port accepts a TCP connection, false otherwise.
         * Used by the frontend to verify tunnel health before opening a port.
         *
         * IMPORTANT: This runs on the JavaBridge thread. A long timeout blocks ALL
         * JS bridge calls (including subsequent testPortReachable calls), causing
         * the frontend connecting-state polling to stall. 500ms is sufficient for
         * localhost — if the port is listening, the connection is near-instant.
         */
        @JavascriptInterface
        public boolean testPortReachable(int port) {
            if (port <= 0 || port > 65535) return false;
            try (java.net.Socket socket = new java.net.Socket()) {
                socket.connect(new java.net.InetSocketAddress("127.0.0.1", port), 500);
                return true;
            } catch (Exception e) {
                return false;
            }
        }

        /**
         * Force-reconnect the SSH tunnel.
         * Disconnects the current (possibly stale) session and establishes a new one.
         * Returns true if reconnection succeeded (port forwards re-established),
         * false if reconnection failed or timed out.
         * This is a blocking call (up to 15s) — runs on the JavaBridge thread.
         */
        @JavascriptInterface
        public boolean reconnectTunnel() {
            if (!BackgroundService.isRunning()) {
                AppLog.w(TAG, "reconnectTunnel: BackgroundService not running");
                return false;
            }
            try {
                return BackgroundService.forceReconnect(15000);
            } catch (Exception e) {
                AppLog.e(TAG, "reconnectTunnel: failed", e);
                return false;
            }
        }

        /**
         * Get the saved SSH/web password for auto-login.
         * Returns empty string if no password is saved.
         */
        @JavascriptInterface
        public String getPassword() {
            return activity.prefs.getString(KEY_SSH_PASSWORD, "");
        }

        /**
         * Save the SSH password. Called from WebView after successful login.
         * The same password is used for both web auth and SSH auth.
         */
        @JavascriptInterface
        public void setSSHPassword(String pwd) {
            BackgroundService.setPassword(activity, pwd);
        }

        /**
         * Download a file from the ClawBench server to the Downloads directory.
         * @param path Relative file path (as used in /api/local-file/ URL)
         */
        @JavascriptInterface
        public void downloadFile(String path) {
            activity.runOnUiThread(() -> {
                String serverUrl = activity.prefs.getString(KEY_SERVER_URL, "");
                if (serverUrl.isEmpty()) return;
                String url = serverUrl + "/api/local-file/" + Uri.encode(path, "/") + "?download=1";
                // Trigger the DownloadListener by asking WebView to load the URL
                // The ?download=1 param makes the server return Content-Disposition: attachment
                // which forces WebView to trigger the DownloadListener instead of rendering inline
                activity.webView.loadUrl(url);
            });
        }

        /**
         * Download a blob of base64-encoded data to the Downloads directory.
         * Used for archive (zip) downloads which require a POST request
         * and cannot use the WebView loadUrl -> DownloadListener approach.
         * @param base64Data Base64-encoded file content (no data: prefix)
         * @param fileName File name for the download (e.g. "project.zip")
         */
        @JavascriptInterface
        public void downloadBlob(String base64Data, String fileName) {
            new Thread(() -> {
                try {
                    byte[] data = android.util.Base64.decode(base64Data, android.util.Base64.DEFAULT);
                    java.io.File outDir = new java.io.File(
                            Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS), "ClawBench");
                    if (!outDir.exists()) outDir.mkdirs();
                    java.io.File outFile = new java.io.File(outDir, fileName);
                    java.io.FileOutputStream fos = new java.io.FileOutputStream(outFile);
                    fos.write(data);
                    fos.close();

                    // Register with DownloadManager so the system shows a completed
                    // notification and the file appears in the Downloads app.
                    try {
                        DownloadManager dm = (DownloadManager) activity.getSystemService(Context.DOWNLOAD_SERVICE);
                        // Infer MIME type from file extension (simple extraction, not URL parsing)
                        int dot = fileName.lastIndexOf('.');
                        String ext = (dot >= 0 && dot < fileName.length() - 1)
                                ? fileName.substring(dot + 1).toLowerCase() : "";
                        String mimeType = android.webkit.MimeTypeMap.getSingleton()
                                .getMimeTypeFromExtension(ext);
                        if (mimeType == null) mimeType = "application/octet-stream";
                        dm.addCompletedDownload(fileName, activity.getString(R.string.download_description),
                                true, mimeType, outFile.getAbsolutePath(), data.length,
                                true /* show notification */);
                    } catch (Exception e) {
                        // addCompletedDownload may fail on some devices/scopes;
                        // the file is already saved, just skip the notification.
                        AppLog.w(TAG, "addCompletedDownload failed, file already saved", e);
                    }

                    // Notify MediaScanner so the file appears in Downloads app
                    Intent scanIntent = new Intent(Intent.ACTION_MEDIA_SCANNER_SCAN_FILE);
                    scanIntent.setData(Uri.fromFile(outFile));
                    activity.sendBroadcast(scanIntent);
                } catch (Exception e) {
                    AppLog.e(TAG, "downloadBlob failed", e);
                    activity.runOnUiThread(() ->
                            Toast.makeText(activity, R.string.download_failed, Toast.LENGTH_SHORT).show());
                }
            }).start();
        }

        /**
         * Enable or disable volume key interception mode.
         * When enabled, volume up/down keys are forwarded to the WebView JS layer
         * via __onVolumeKey() callback instead of adjusting system volume.
         * Called by the terminal panel on open/close.
         * @param enabled true to intercept volume keys, false to restore default behavior
         */
        @JavascriptInterface
        public void setVolumeKeyMode(boolean enabled) {
            activity.volumeKeyMode = enabled;
        }

        /**
         * Open a chat session by dispatching an event to the WebView.
         * Called when a notification is tapped.
         */
        @JavascriptInterface
        public void openSession(String sessionId) {
            activity.runOnUiThread(() -> {
                activity.webView.evaluateJavascript(
                    "window.dispatchEvent(new CustomEvent('clawbench-open-session', { detail: { sessionId: '" + sessionId + "' } }))",
                    null
                );
            });
        }

        /**
         * Returns pending navigation data from a notification tap that occurred
         * before the WebView was loaded (cold start). Returns null if none pending.
         * Called by the frontend on mount to handle deferred deep links.
         */
        @JavascriptInterface
        public String getPendingNavigation() {
            org.json.JSONObject nav = activity.pendingNavigation;
            activity.pendingNavigation = null;
            String result = nav != null ? nav.toString() : null;
            AppLog.i(TAG, "MainActivity: getPendingNavigation called, returning=" + result);
            return result;
        }

        /**
         * Start capturing Android logs and sending them to the server.
         * The logs are written to .clawbench/logs/android.log on the server
         * and can be viewed in the built-in file browser.
         */
        @JavascriptInterface
        public void startLogCapture() {
            String baseUrl = activity.prefs.getString(KEY_SERVER_URL, "");
            if (!baseUrl.isEmpty()) {
                AppLog.startCapture(baseUrl);
            }
        }

        /**
         * Stop capturing Android logs and flush remaining entries.
         */
        @JavascriptInterface
        public void stopLogCapture() {
            AppLog.stopCapture();
        }

        /**
         * Update the active terminal session count shown in the background notification.
         * Called from the WebView when terminal tabs are created/closed.
         * The notification text shows "N 个终端" alongside port forward count.
         */
        @JavascriptInterface
        public void setTerminalSessionCount(int count) {
            BackgroundService.setTerminalSessionCount(count);
        }

        /**
         * Get the saved server list as a JSON array.
         * Each entry: {"url":"https://host:port", "password":"..."}
         * Returns "[]" when no servers are saved.
         */
        @JavascriptInterface
        public String getServerList() {
            try {
                String json = activity.prefs.getString(KEY_SERVER_LIST, "[]");
                return json;
            } catch (Exception e) {
                return "[]";
            }
        }

        /**
         * Save (add or update) a server entry in the server list.
         * If a server with the same URL already exists, its password is updated
         * and the entry is moved to the head of the list (most recently used).
         * @param url      The server URL (e.g. "https://192.168.1.100:20000")
         * @param password The password for this server
         */
        @JavascriptInterface
        public void saveServer(String url, String password) {
            activity.saveServerInternal(url, password);
        }

        /**
         * Remove a server entry from the server list by URL.
         * @param url The server URL to remove
         */
        @JavascriptInterface
        public void removeServer(String url) {
            try {
                org.json.JSONArray list = new org.json.JSONArray(
                        activity.prefs.getString(KEY_SERVER_LIST, "[]"));
                org.json.JSONArray newList = new org.json.JSONArray();
                for (int i = 0; i < list.length(); i++) {
                    org.json.JSONObject entry = list.getJSONObject(i);
                    if (!url.equals(entry.optString("url", ""))) {
                        newList.put(entry);
                    }
                }
                activity.prefs.edit().putString(KEY_SERVER_LIST, newList.toString()).apply();
            } catch (Exception e) {
                AppLog.e(TAG, "removeServer failed", e);
            }
        }

        /**
         * Set or clear the FLAG_KEEP_SCREEN_ON flag on the activity window.
         * When true, prevents the screen from turning off due to inactivity.
         * Used when preventScreenLock is enabled and AI is streaming or TTS
         * is playing, so the user can watch the output without the screen locking.
         * The flag is automatically cleared if the activity goes to background.
         * @param keepOn true to keep screen on, false to allow normal screen timeout
         */
        @JavascriptInterface
        public void setKeepScreenOn(boolean keepOn) {
            activity.runOnUiThread(() -> {
                if (keepOn) {
                    activity.getWindow().addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
                    AppLog.i(TAG, "setKeepScreenOn: FLAG_KEEP_SCREEN_ON added");
                } else {
                    activity.getWindow().clearFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
                    AppLog.i(TAG, "setKeepScreenOn: FLAG_KEEP_SCREEN_ON cleared");
                }
            });
        }

        /**
         * Share a file from the ClawBench server with another app via ACTION_SEND.
         * Downloads the file to a temp directory, then creates a share intent
         * with a FileProvider content URI.
         * @param path Relative file path (as used in /api/local-file/ URL)
         * @param mimeType MIME type for the share intent (e.g. "image/png", "application/pdf")
         */
        @JavascriptInterface
        public void shareFile(String path, String mimeType) {
            // Path safety: reject traversal attempts (both raw and URL-decoded forms)
            if (path == null || path.isEmpty() || path.contains("..") || path.startsWith("/")) {
                AppLog.w(TAG, "shareFile: invalid path: " + path);
                return;
            }
            try {
                String decoded = java.net.URLDecoder.decode(path, "UTF-8");
                if (decoded.contains("..") || decoded.startsWith("/")) {
                    AppLog.w(TAG, "shareFile: invalid decoded path: " + path);
                    return;
                }
            } catch (Exception e) {
                AppLog.w(TAG, "shareFile: invalid path encoding: " + path);
                return;
            }

            new Thread(() -> {
                try {
                    String serverUrl = activity.prefs.getString(KEY_SERVER_URL, "");
                    if (serverUrl.isEmpty()) {
                        AppLog.w(TAG, "shareFile: no server URL");
                        return;
                    }

                    // Flush cookies and get auth cookie
                    CookieManager.getInstance().flush();
                    String cookie = CookieManager.getInstance().getCookie(serverUrl);
                    if (cookie == null || cookie.isEmpty()) {
                        AppLog.w(TAG, "shareFile: no auth cookie");
                        activity.runOnUiThread(() ->
                                Toast.makeText(activity, R.string.share_file_failed, Toast.LENGTH_SHORT).show());
                        return;
                    }

                    // Download file from server
                    String encodedPath = Uri.encode(path, "/");
                    String downloadUrl = serverUrl + "/api/local-file/" + encodedPath + "?download=1";

                    OkHttpClient client = activity.buildTrustingOkHttpClient();
                    Request request = new Request.Builder()
                            .url(downloadUrl)
                            .addHeader("Cookie", cookie)
                            .build();

                    // Stream to temp file in shared cache dir
                    String fileName = path.contains("/") ? path.substring(path.lastIndexOf("/") + 1) : path;
                    fileName = fileName.replaceAll("[\\\\/:*?\"<>|]", "_");
                    if (fileName.length() > 200) fileName = fileName.substring(0, 200);
                    final String safeFileName = fileName;
                    File tempFile = new File(activity.getSharedCacheDir(), UUID.randomUUID().toString() + "_" + safeFileName);

                    try (Response response = client.newCall(request).execute()) {
                        if (!response.isSuccessful() || response.body() == null) {
                            AppLog.w(TAG, "shareFile: download failed, code=" + response.code());
                            activity.runOnUiThread(() ->
                                    Toast.makeText(activity, R.string.share_file_failed, Toast.LENGTH_SHORT).show());
                            return;
                        }
                        try (InputStream is = response.body().byteStream();
                             FileOutputStream fos = new FileOutputStream(tempFile)) {
                            byte[] buffer = new byte[8192];
                            int len;
                            while ((len = is.read(buffer)) != -1) {
                                fos.write(buffer, 0, len);
                            }
                        }
                    }

                    tempFile.deleteOnExit();

                    // Get FileProvider content URI
                    String authority = activity.getPackageName() + ".fileprovider";
                    Uri contentUri = androidx.core.content.FileProvider.getUriForFile(activity, authority, tempFile);

                    // Build share intent
                    Intent shareIntent = new Intent(Intent.ACTION_SEND);
                    shareIntent.setType(mimeType != null && !mimeType.isEmpty() ? mimeType : "*/*");
                    shareIntent.putExtra(Intent.EXTRA_STREAM, contentUri);
                    shareIntent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);

                    activity.runOnUiThread(() -> {
                        try {
                            activity.startActivity(Intent.createChooser(shareIntent, safeFileName));
                        } catch (Exception e) {
                            AppLog.w(TAG, "shareFile: chooser failed", e);
                            Toast.makeText(activity, R.string.share_file_failed, Toast.LENGTH_SHORT).show();
                        }
                    });

                } catch (Exception e) {
                    AppLog.e(TAG, "shareFile failed", e);
                    activity.runOnUiThread(() ->
                            Toast.makeText(activity, R.string.share_file_failed, Toast.LENGTH_SHORT).show());
                }
            }).start();
        }

        /**
         * Share text with another app via ACTION_SEND.
         * @param text Text content to share
         */
        @JavascriptInterface
        public void shareText(String text) {
            if (text == null || text.isEmpty()) return;
            activity.runOnUiThread(() -> {
                try {
                    Intent shareIntent = new Intent(Intent.ACTION_SEND);
                    shareIntent.setType("text/plain");
                    shareIntent.putExtra(Intent.EXTRA_TEXT, text);
                    activity.startActivity(Intent.createChooser(shareIntent, "Share"));
                } catch (Exception e) {
                    AppLog.w(TAG, "shareText: chooser failed", e);
                }
            });
        }

        /**
         * Control whether the BackgroundService shows a persistent notification.
         * When enabled, the foreground service notification is visible (keeps service alive).
         * When disabled, the service still runs but the notification is minimized/silent.
         */
        @JavascriptInterface
        public void setPushPersistentNotification(boolean enabled) {
            BackgroundService.setPersistentNotificationEnabled(activity, enabled);
        }

        /**
         * Query whether persistent notification is currently enabled.
         */
        @JavascriptInterface
        public boolean isPushPersistentNotification() {
            return BackgroundService.isPersistentNotificationEnabled(activity);
        }

        /**
         * Check if the device is a Chinese OEM with aggressive background process
         * management (Xiaomi, Huawei, OPPO, vivo). The frontend uses this to
         * prompt the user to enable auto-start / battery optimization whitelisting.
         */
        @JavascriptInterface
        public boolean isChineseOem() {
            return OemUtils.isChineseOem();
        }

        /**
         * Get the detected OEM name for display purposes.
         * Returns one of: "xiaomi", "huawei", "oppo", "vivo", "samsung", "other".
         */
        @JavascriptInterface
        public String getOemName() {
            return OemUtils.detectOem().name().toLowerCase();
        }

        /**
         * Check if the auto-start prompt has been shown to the user.
         */
        @JavascriptInterface
        public boolean isOemAutoStartPrompted() {
            return OemUtils.isAutoStartPrompted(activity);
        }

        /**
         * Mark the auto-start prompt as shown (don't prompt again).
         */
        @JavascriptInterface
        public void setOemAutoStartPrompted() {
            OemUtils.setAutoStartPrompted(activity);
        }

        /**
         * Open the OEM-specific auto-start / startup manager settings.
         * Returns true if an intent was launched, false if not supported.
         */
        @JavascriptInterface
        public boolean openOemAutoStartSettings() {
            Intent intent = OemUtils.getAutoStartIntent(activity);
            if (intent != null) {
                try {
                    activity.startActivity(intent);
                    return true;
                } catch (Exception e) {
                    AppLog.w(TAG, "Failed to open OEM auto-start settings", e);
                }
            }
            return false;
        }

        /**
         * Open the OEM-specific battery optimization settings.
         * Returns true if an intent was launched, false if not supported.
         */
        @JavascriptInterface
        public boolean openOemBatterySettings() {
            Intent intent = OemUtils.getBatterySettingsIntent(activity);
            if (intent != null) {
                try {
                    activity.startActivity(intent);
                    return true;
                } catch (Exception e) {
                    AppLog.w(TAG, "Failed to open OEM battery settings", e);
                }
            }
            return false;
        }

        /**
         * Sync the last seen event ID cursor from the WebView WS to
         * SharedPreferences. Called by the frontend when it receives a
         * terminal-state event (completed/cancelled/failed/permission_pending)
         * while the app is in the foreground. This prevents the native WS
         * fetchPendingEvents() from re-delivering already-seen events when
         * the app switches to background.
         */
        @JavascriptInterface
        public void updateLastSeenEventId(String eventId) {
            BackgroundService.updateLastSeenEventId(activity, eventId);
        }
    }

    /**
     * Save (add or update) a server entry in the server list.
     * If the URL already exists, its password is updated and the entry is
     * promoted to the head of the list (most recently used).
     * Thread-safe: only called on the UI thread.
     */
    private void saveServerInternal(String url, String password) {
        try {
            org.json.JSONArray list = new org.json.JSONArray(
                    prefs.getString(KEY_SERVER_LIST, "[]"));
            org.json.JSONArray reordered = new org.json.JSONArray();
            org.json.JSONObject updated = null;

            // Find and update existing entry
            for (int i = 0; i < list.length(); i++) {
                org.json.JSONObject entry = list.getJSONObject(i);
                if (url.equals(entry.optString("url", ""))) {
                    entry.put("password", password);
                    updated = entry;
                } else {
                    reordered.put(entry);
                }
            }

            // If not found, create new entry
            if (updated == null) {
                updated = new org.json.JSONObject();
                updated.put("url", url);
                updated.put("password", password);
            }

            // Insert at head (most recently used)
            org.json.JSONArray result = new org.json.JSONArray();
            result.put(updated);
            for (int i = 0; i < reordered.length(); i++) {
                result.put(reordered.get(i));
            }

            prefs.edit().putString(KEY_SERVER_LIST, result.toString()).apply();
        } catch (Exception e) {
            AppLog.e(TAG, "saveServerInternal failed", e);
        }
    }

    /**
     * Log a message from the WebView JS layer through AppLog.
     * This gives frontend code explicit control over log relay to the server,
     * independent of the implicit onConsoleMessage capture.
     * @param level One of "D", "I", "W", "E"
     * @param tag   Log tag (e.g. "ChatStream", "PortForward")
     * @param msg   Log message
     */
    @JavascriptInterface
    public void log(String level, String tag, String msg) {
        switch (level) {
            case "E": AppLog.e(tag, msg); break;
            case "W": AppLog.w(tag, msg); break;
            case "I": AppLog.i(tag, msg); break;
            default:  AppLog.d(tag, msg); break;
        }
    }
}
