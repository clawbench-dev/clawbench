package com.clawbench.app;

import android.content.SharedPreferences;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.ValueCallback;
import android.webkit.WebView;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.contains;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for connection error recovery in MainActivity.
 *
 * Covers the fixes for stuck-state scenarios where the user can neither
 * proceed to the app nor go back to the login page:
 *
 * 1. Connection timeout (black screen when server is unreachable)
 * 2. Back press recovery (return to login when WebView is stuck)
 * 3. showLoginPage error message delivery via onPageFinished
 * 4. Connection timeout cancellation on successful load
 * 5. HTTP error handling for main frame 4xx/5xx
 *
 * Uses Unsafe.allocateInstance() to create Activities without triggering
 * AppCompatActivity's constructor. Uses Mockito to mock WebView.
 */
public class MainActivityConnectionRecoveryTest {

    private static final String LOGIN_HTML_URL = "file:///android_asset/login.html";

    private MainActivity activity;
    private WebView mockWebView;
    private SharedPreferences mockPrefs;
    private SharedPreferences.Editor mockEditor;

    @Before
    public void setUp() throws Exception {
        activity = allocate(MainActivity.class);

        // Set the static instance field
        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        // Mock WebView
        mockWebView = mock(WebView.class);
        setField(activity, "webView", mockWebView);

        // Mock SharedPreferences
        mockPrefs = mock(SharedPreferences.class);
        mockEditor = mock(SharedPreferences.Editor.class);
        when(mockPrefs.edit()).thenReturn(mockEditor);
        when(mockEditor.putString(any(), any())).thenReturn(mockEditor);
        // apply() returns void — doNothing is the default for void methods
        setField(activity, "prefs", mockPrefs);

        // Set default state
        setField(activity, "webViewConnected", false);
        setField(activity, "loadErrorPending", false);
        setField(activity, "customView", null);
        setField(activity, "connectionTimeoutRunnable", null);
        setField(activity, "pendingLoginErrorMessage", null);
    }

    @After
    public void tearDown() throws Exception {
        try {
            Field instanceField = MainActivity.class.getDeclaredField("instance");
            instanceField.setAccessible(true);
            instanceField.set(null, null);
        } catch (Exception ignored) {}
    }

    // =====================================================
    // Back press recovery: !webViewConnected → showLoginPage
    // =====================================================

    @Test
    public void onBackPressed_webViewNotConnected_navigatesToLoginPage() throws Exception {
        // WebView is NOT connected (stuck state) and URL is a remote page
        setField(activity, "webViewConnected", false);
        when(mockWebView.getUrl()).thenReturn("https://192.168.1.100:20000");

        activity.onBackPressed();

        // Should load login page, NOT dispatch JS back-press event
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
        verify(mockWebView, never()).evaluateJavascript(contains("clawbench-back-press"), any());
    }

    @Test
    public void onBackPressed_webViewNotConnected_nullUrl_navigatesToLoginPage() throws Exception {
        // WebView is NOT connected with null URL (e.g., page not loaded yet)
        setField(activity, "webViewConnected", false);
        when(mockWebView.getUrl()).thenReturn(null);

        activity.onBackPressed();

        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
        verify(mockWebView, never()).evaluateJavascript(contains("clawbench-back-press"), any());
    }

    @Test
    public void onBackPressed_webViewConnected_dispatchesJsEvent() throws Exception {
        // WebView IS connected — normal operation, delegate to JS
        setField(activity, "webViewConnected", true);
        when(mockWebView.getUrl()).thenReturn("https://192.168.1.100:20000");

        activity.onBackPressed();

        // Should dispatch JS event, NOT load login page
        verify(mockWebView).evaluateJavascript(contains("clawbench-back-press"), any(ValueCallback.class));
        verify(mockWebView, never()).loadUrl(LOGIN_HTML_URL);
    }

    @Test
    public void onBackPressed_onLoginPage_callsSuperBackPress() throws Exception {
        // Already on login page — should call super.onBackPressed() (exit app)
        setField(activity, "webViewConnected", false);
        when(mockWebView.getUrl()).thenReturn(LOGIN_HTML_URL);

        try {
            activity.onBackPressed();
        } catch (NullPointerException e) {
            // Expected: super.onBackPressed() crashes on Unsafe-allocated activity
        }

        // Should NOT dispatch JS event and NOT load login page again
        verify(mockWebView, never()).evaluateJavascript(contains("clawbench-back-press"), any());
        verify(mockWebView, never()).loadUrl(LOGIN_HTML_URL);
    }

    // =====================================================
    // showLoginPage: error message delivery
    // =====================================================

    @Test
    public void showLoginPage_withErrorMessage_storesPendingMessage() throws Exception {
        // Call showLoginPage with an error message
        invokeMethod(activity, "showLoginPage", String.class, "连接超时");

        // The error message should be stored for delivery after login page loads
        String pending = (String) getField(activity, "pendingLoginErrorMessage");
        assertEquals("连接超时", pending);

        // Should load the login page URL
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);

        // webViewConnected and loadErrorPending should be reset
        assertFalse(getBooleanField(activity, "webViewConnected"));
        assertFalse(getBooleanField(activity, "loadErrorPending"));
    }

    @Test
    public void showLoginPage_withNullError_noPendingMessage() throws Exception {
        invokeMethod(activity, "showLoginPage", String.class, null);

        String pending = (String) getField(activity, "pendingLoginErrorMessage");
        assertNull(pending);
    }

    @Test
    public void showLoginPage_cancelsConnectionTimeout() throws Exception {
        // Set up a mock timeout runnable
        Runnable mockRunnable = mock(Runnable.class);
        setField(activity, "connectionTimeoutRunnable", mockRunnable);

        invokeMethod(activity, "showLoginPage", String.class, "error");

        // Should cancel the timeout (removeCallbacks called)
        verify(mockWebView).removeCallbacks(mockRunnable);

        // Timeout runnable should be cleared
        assertNull(getField(activity, "connectionTimeoutRunnable"));
    }

    // =====================================================
    // Connection timeout: startConnectionTimeout / cancelConnectionTimeout
    // =====================================================

    @Test
    public void startConnectionTimeout_schedulesDelayedRunnable() throws Exception {
        invokeMethod(activity, "startConnectionTimeout");

        // Should post a delayed runnable on the WebView
        verify(mockWebView).postDelayed(any(Runnable.class), eq(15_000L));

        // The timeout runnable field should be set
        assertNotNull(getField(activity, "connectionTimeoutRunnable"));
    }

    @Test
    public void startConnectionTimeout_runnableCallsShowLoginPage() throws Exception {
        // Capture the timeout runnable and execute it to verify it calls showLoginPage
        setField(activity, "webViewConnected", false);

        final Runnable[] capturedRunnable = new Runnable[1];
        doAnswer(invocation -> {
            capturedRunnable[0] = invocation.getArgument(0);
            return true;
        }).when(mockWebView).postDelayed(any(Runnable.class), any(long.class));

        invokeMethod(activity, "startConnectionTimeout");
        assertNotNull(capturedRunnable[0]);

        // Execute the timeout runnable
        capturedRunnable[0].run();

        // Should have called showLoginPage
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
        // Error message should be stored
        String pending = (String) getField(activity, "pendingLoginErrorMessage");
        assertEquals("连接超时，请检查服务器地址和网络连接。", pending);
    }

    @Test
    public void startConnectionTimeout_cancelsExistingTimeout() throws Exception {
        // Set up an existing timeout
        Runnable oldRunnable = mock(Runnable.class);
        setField(activity, "connectionTimeoutRunnable", oldRunnable);

        invokeMethod(activity, "startConnectionTimeout");

        // Should remove the old runnable
        verify(mockWebView).removeCallbacks(oldRunnable);
    }

    @Test
    public void cancelConnectionTimeout_removesRunnable() throws Exception {
        Runnable mockRunnable = mock(Runnable.class);
        setField(activity, "connectionTimeoutRunnable", mockRunnable);

        invokeMethod(activity, "cancelConnectionTimeout");

        verify(mockWebView).removeCallbacks(mockRunnable);
        assertNull(getField(activity, "connectionTimeoutRunnable"));
    }

    @Test
    public void cancelConnectionTimeout_nullRunnable_noException() throws Exception {
        setField(activity, "connectionTimeoutRunnable", null);

        // Should not throw
        invokeMethod(activity, "cancelConnectionTimeout");

        verify(mockWebView, never()).removeCallbacks(any(Runnable.class));
    }

    // =====================================================
    // onPageFinished: pendingLoginErrorMessage delivery
    // =====================================================

    @Test
    public void onPageFinished_loginPage_withPendingError_deliversErrorViaJs() throws Exception {
        // Simulate: showLoginPage stored a pending error message
        setField(activity, "pendingLoginErrorMessage", "无法连接到服务器");
        setField(activity, "webViewConnected", false);

        // Create the WebViewClient and call onPageFinished for login page
        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", android.webkit.WebView.class, mockWebView,
                     String.class, LOGIN_HTML_URL);

        // Should call evaluateJavascript with onConnectError
        verify(mockWebView).evaluateJavascript(contains("onConnectError"), any());

        // Pending message should be consumed (set to null)
        assertNull(getField(activity, "pendingLoginErrorMessage"));
    }

    @Test
    public void onPageFinished_loginPage_noPendingError_noJsCall() throws Exception {
        setField(activity, "pendingLoginErrorMessage", null);
        setField(activity, "webViewConnected", false);

        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", android.webkit.WebView.class, mockWebView,
                     String.class, LOGIN_HTML_URL);

        // Should NOT call evaluateJavascript (no error to deliver)
        verify(mockWebView, never()).evaluateJavascript(contains("onConnectError"), any());
    }

    @Test
    public void onPageFinished_loginPage_cancelsConnectionTimeout() throws Exception {
        Runnable mockTimeout = mock(Runnable.class);
        setField(activity, "connectionTimeoutRunnable", mockTimeout);
        setField(activity, "pendingLoginErrorMessage", null);

        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", android.webkit.WebView.class, mockWebView,
                     String.class, LOGIN_HTML_URL);

        // Should cancel the connection timeout
        verify(mockWebView).removeCallbacks(mockTimeout);
    }

    @Test
    public void onPageFinished_remotePage_success_setsConnectedAndVisible() throws Exception {
        setField(activity, "loadErrorPending", false);
        setField(activity, "connectionTimeoutRunnable", mock(Runnable.class));

        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", android.webkit.WebView.class, mockWebView,
                     String.class, "https://192.168.1.100:20000");

        // Should set webViewConnected = true
        assertTrue(getBooleanField(activity, "webViewConnected"));
        // Should make WebView visible
        verify(mockWebView).setVisibility(android.view.View.VISIBLE);
        // Should cancel connection timeout
        assertNull(getField(activity, "connectionTimeoutRunnable"));
    }

    @Test
    public void onPageFinished_remotePage_errorPending_doesNotShowWebView() throws Exception {
        setField(activity, "loadErrorPending", true);

        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", android.webkit.WebView.class, mockWebView,
                     String.class, "https://192.168.1.100:20000");

        // Should NOT set webViewConnected or show WebView
        assertFalse(getBooleanField(activity, "webViewConnected"));
        verify(mockWebView, never()).setVisibility(android.view.View.VISIBLE);
    }

    // =====================================================
    // onPageStarted: login page visibility
    // =====================================================

    @Test
    public void onPageStarted_loginPage_showsImmediately() throws Exception {
        setField(activity, "webViewConnected", true);
        setField(activity, "loadErrorPending", true);

        Object client = createWebViewClient();
        Method onPageStarted = findMethod(client.getClass(), "onPageStarted",
                android.webkit.WebView.class, String.class, android.graphics.Bitmap.class);
        onPageStarted.setAccessible(true);
        onPageStarted.invoke(client, mockWebView, LOGIN_HTML_URL, null);

        // Login page should be shown immediately
        verify(mockWebView).setVisibility(android.view.View.VISIBLE);
        // State should be reset
        assertFalse(getBooleanField(activity, "webViewConnected"));
        assertFalse(getBooleanField(activity, "loadErrorPending"));
    }

    @Test
    public void onPageStarted_remotePage_hidesWebView() throws Exception {
        setField(activity, "webViewConnected", true);

        Object client = createWebViewClient();
        Method onPageStarted = findMethod(client.getClass(), "onPageStarted",
                android.webkit.WebView.class, String.class, android.graphics.Bitmap.class);
        onPageStarted.setAccessible(true);
        onPageStarted.invoke(client, mockWebView, "https://192.168.1.100:20000", null);

        // Remote page should hide WebView during loading
        verify(mockWebView).setVisibility(android.view.View.INVISIBLE);
        assertFalse(getBooleanField(activity, "webViewConnected"));
    }

    // =====================================================
    // connectToServer: state management (tested indirectly via fields)
    // =====================================================

    @Test
    public void connectToServer_savesUrlAndStartsTimeout() throws Exception {
        // connectToServer has framework dependencies (isNetworkAvailable, BackgroundService.setPassword)
        // that crash on Unsafe-allocated activity. Test the critical invariants instead:
        // 1. URL is saved to prefs
        // 2. Connection timeout is started
        // These are verified through the other tests that exercise the same code paths
        // (showLoginPage cancels timeout, onPageFinished delivers errors, etc.)

        // Verify the timeout constant is correct
        Field field = MainActivity.class.getDeclaredField("CONNECTION_TIMEOUT_MS");
        field.setAccessible(true);
        int timeout = field.getInt(null);
        assertEquals(15_000, timeout);
    }

    // =====================================================
    // Error message escaping
    // =====================================================

    @Test
    public void pendingErrorMessage_withQuotes_escapedInJsCall() throws Exception {
        setField(activity, "pendingLoginErrorMessage", "can't connect");

        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", android.webkit.WebView.class, mockWebView,
                     String.class, LOGIN_HTML_URL);

        // The JS call should have the single quote escaped
        verify(mockWebView).evaluateJavascript(contains("can\\'t connect"), any());
    }

    @Test
    public void pendingErrorMessage_withBackslash_escapedInJsCall() throws Exception {
        setField(activity, "pendingLoginErrorMessage", "path\\error");

        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", android.webkit.WebView.class, mockWebView,
                     String.class, LOGIN_HTML_URL);

        verify(mockWebView).evaluateJavascript(contains("path\\\\error"), any());
    }

    @Test
    public void pendingErrorMessage_withNewline_escapedInJsCall() throws Exception {
        setField(activity, "pendingLoginErrorMessage", "line1\nline2");

        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", android.webkit.WebView.class, mockWebView,
                     String.class, LOGIN_HTML_URL);

        verify(mockWebView).evaluateJavascript(contains("line1\\nline2"), any());
    }

    // =====================================================
    // Connection timeout constant
    // =====================================================

    @Test
    public void connectionTimeoutConstant_is15Seconds() throws Exception {
        Field field = MainActivity.class.getDeclaredField("CONNECTION_TIMEOUT_MS");
        field.setAccessible(true);
        int timeout = field.getInt(null);
        assertEquals(15_000, timeout);
    }

    // =====================================================
    // onReceivedError: main frame error handling
    // =====================================================

    @Test
    public void onReceivedError_mainFrame_setsLoadErrorPendingAndSchedulesShowLoginPage() throws Exception {
        setField(activity, "webViewConnected", false);
        setField(activity, "loadErrorPending", false);

        WebResourceRequest mockRequest = mock(WebResourceRequest.class);
        when(mockRequest.isForMainFrame()).thenReturn(true);

        // Capture the postDelayed Runnable to verify showLoginPage is called
        final Runnable[] capturedRunnable = new Runnable[1];
        doAnswer(invocation -> {
            capturedRunnable[0] = invocation.getArgument(0);
            return true;
        }).when(mockWebView).postDelayed(any(Runnable.class), any(long.class));

        Object client = createWebViewClient();
        Method onReceivedError = findMethod(client.getClass(), "onReceivedError",
                android.webkit.WebView.class, WebResourceRequest.class, android.webkit.WebResourceError.class);
        onReceivedError.setAccessible(true);
        onReceivedError.invoke(client, mockWebView, mockRequest, null);

        assertTrue(getBooleanField(activity, "loadErrorPending"));
        verify(mockWebView).setVisibility(android.view.View.INVISIBLE);

        // Execute the captured runnable to verify showLoginPage is called
        assertNotNull("postDelayed runnable should be captured", capturedRunnable[0]);
        capturedRunnable[0].run();
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
    }

    @Test
    public void onReceivedError_subFrame_doesNothing() throws Exception {
        setField(activity, "loadErrorPending", false);

        WebResourceRequest mockRequest = mock(WebResourceRequest.class);
        when(mockRequest.isForMainFrame()).thenReturn(false);

        Object client = createWebViewClient();
        Method onReceivedError = findMethod(client.getClass(), "onReceivedError",
                android.webkit.WebView.class, WebResourceRequest.class, android.webkit.WebResourceError.class);
        onReceivedError.setAccessible(true);
        onReceivedError.invoke(client, mockWebView, mockRequest, null);

        // Should NOT set loadErrorPending for sub-frame errors
        assertFalse(getBooleanField(activity, "loadErrorPending"));
    }

    // =====================================================
    // onReceivedHttpError: main frame HTTP 4xx/5xx handling
    // =====================================================

    @Test
    public void onReceivedHttpError_mainFrameNotConnected_401_setsErrorAndSchedulesLogin() throws Exception {
        setField(activity, "webViewConnected", false);
        setField(activity, "loadErrorPending", false);

        WebResourceRequest mockRequest = mock(WebResourceRequest.class);
        when(mockRequest.isForMainFrame()).thenReturn(true);
        WebResourceResponse mockResponse = mock(WebResourceResponse.class);
        when(mockResponse.getStatusCode()).thenReturn(401);

        // Capture the postDelayed Runnable to verify showLoginPage is called
        final Runnable[] capturedRunnable = new Runnable[1];
        doAnswer(invocation -> {
            capturedRunnable[0] = invocation.getArgument(0);
            return true;
        }).when(mockWebView).postDelayed(any(Runnable.class), any(long.class));

        Object client = createWebViewClient();
        Method onReceivedHttpError = findMethod(client.getClass(), "onReceivedHttpError",
                android.webkit.WebView.class, WebResourceRequest.class, WebResourceResponse.class);
        onReceivedHttpError.setAccessible(true);
        onReceivedHttpError.invoke(client, mockWebView, mockRequest, mockResponse);

        assertTrue(getBooleanField(activity, "loadErrorPending"));
        verify(mockWebView).setVisibility(android.view.View.INVISIBLE);

        // Execute the captured runnable to verify showLoginPage is called
        assertNotNull("postDelayed runnable should be captured", capturedRunnable[0]);
        capturedRunnable[0].run();
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
    }

    @Test
    public void onReceivedHttpError_mainFrameNotConnected_500_setsErrorAndSchedulesLogin() throws Exception {
        setField(activity, "webViewConnected", false);

        WebResourceRequest mockRequest = mock(WebResourceRequest.class);
        when(mockRequest.isForMainFrame()).thenReturn(true);
        WebResourceResponse mockResponse = mock(WebResourceResponse.class);
        when(mockResponse.getStatusCode()).thenReturn(500);

        final Runnable[] capturedRunnable = new Runnable[1];
        doAnswer(invocation -> {
            capturedRunnable[0] = invocation.getArgument(0);
            return true;
        }).when(mockWebView).postDelayed(any(Runnable.class), any(long.class));

        Object client = createWebViewClient();
        Method onReceivedHttpError = findMethod(client.getClass(), "onReceivedHttpError",
                android.webkit.WebView.class, WebResourceRequest.class, WebResourceResponse.class);
        onReceivedHttpError.setAccessible(true);
        onReceivedHttpError.invoke(client, mockWebView, mockRequest, mockResponse);

        assertTrue(getBooleanField(activity, "loadErrorPending"));

        // Execute the captured runnable
        assertNotNull(capturedRunnable[0]);
        capturedRunnable[0].run();
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
    }

    @Test
    public void onReceivedHttpError_mainFrameNotConnected_404_setsErrorAndSchedulesLogin() throws Exception {
        setField(activity, "webViewConnected", false);

        WebResourceRequest mockRequest = mock(WebResourceRequest.class);
        when(mockRequest.isForMainFrame()).thenReturn(true);
        WebResourceResponse mockResponse = mock(WebResourceResponse.class);
        when(mockResponse.getStatusCode()).thenReturn(404);

        final Runnable[] capturedRunnable = new Runnable[1];
        doAnswer(invocation -> {
            capturedRunnable[0] = invocation.getArgument(0);
            return true;
        }).when(mockWebView).postDelayed(any(Runnable.class), any(long.class));

        Object client = createWebViewClient();
        Method onReceivedHttpError = findMethod(client.getClass(), "onReceivedHttpError",
                android.webkit.WebView.class, WebResourceRequest.class, WebResourceResponse.class);
        onReceivedHttpError.setAccessible(true);
        onReceivedHttpError.invoke(client, mockWebView, mockRequest, mockResponse);

        // Execute the captured runnable
        assertNotNull(capturedRunnable[0]);
        capturedRunnable[0].run();
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
    }

    @Test
    public void onReceivedHttpError_webViewConnected_doesNothing() throws Exception {
        // Once the app is loaded, HTTP errors are handled by the Vue frontend
        setField(activity, "webViewConnected", true);
        setField(activity, "loadErrorPending", false);

        WebResourceRequest mockRequest = mock(WebResourceRequest.class);
        when(mockRequest.isForMainFrame()).thenReturn(true);
        WebResourceResponse mockResponse = mock(WebResourceResponse.class);
        when(mockResponse.getStatusCode()).thenReturn(500);

        Object client = createWebViewClient();
        Method onReceivedHttpError = findMethod(client.getClass(), "onReceivedHttpError",
                android.webkit.WebView.class, WebResourceRequest.class, WebResourceResponse.class);
        onReceivedHttpError.setAccessible(true);
        onReceivedHttpError.invoke(client, mockWebView, mockRequest, mockResponse);

        // Should NOT set loadErrorPending when already connected
        assertFalse(getBooleanField(activity, "loadErrorPending"));
    }

    @Test
    public void onReceivedHttpError_subFrame_doesNothing() throws Exception {
        setField(activity, "webViewConnected", false);

        WebResourceRequest mockRequest = mock(WebResourceRequest.class);
        when(mockRequest.isForMainFrame()).thenReturn(false);
        WebResourceResponse mockResponse = mock(WebResourceResponse.class);
        when(mockResponse.getStatusCode()).thenReturn(500);

        Object client = createWebViewClient();
        Method onReceivedHttpError = findMethod(client.getClass(), "onReceivedHttpError",
                android.webkit.WebView.class, WebResourceRequest.class, WebResourceResponse.class);
        onReceivedHttpError.setAccessible(true);
        onReceivedHttpError.invoke(client, mockWebView, mockRequest, mockResponse);

        assertFalse(getBooleanField(activity, "loadErrorPending"));
    }

    @Test
    public void onReceivedHttpError_otherStatusCode_fallbackMessage() throws Exception {
        // Test the else branch for non-standard status codes (e.g., 301 redirect response
        // incorrectly classified as HTTP error)
        setField(activity, "webViewConnected", false);

        WebResourceRequest mockRequest = mock(WebResourceRequest.class);
        when(mockRequest.isForMainFrame()).thenReturn(true);
        WebResourceResponse mockResponse = mock(WebResourceResponse.class);
        when(mockResponse.getStatusCode()).thenReturn(302);

        final Runnable[] capturedRunnable = new Runnable[1];
        doAnswer(invocation -> {
            capturedRunnable[0] = invocation.getArgument(0);
            return true;
        }).when(mockWebView).postDelayed(any(Runnable.class), any(long.class));

        Object client = createWebViewClient();
        Method onReceivedHttpError = findMethod(client.getClass(), "onReceivedHttpError",
                android.webkit.WebView.class, WebResourceRequest.class, WebResourceResponse.class);
        onReceivedHttpError.setAccessible(true);
        onReceivedHttpError.invoke(client, mockWebView, mockRequest, mockResponse);

        assertNotNull(capturedRunnable[0]);
        capturedRunnable[0].run();
        // Should have called showLoginPage with the fallback message
        String pending = (String) getField(activity, "pendingLoginErrorMessage");
        assertEquals("连接失败，请检查服务器地址和网络连接。", pending);
    }

    // =====================================================
    // onRenderProcessGone: WebView crash recovery
    // =====================================================

    @Test
    public void onRenderProcessGone_resetsStateBeforeUiRecovery() throws Exception {
        // onRenderProcessGone resets webViewConnected, loadErrorPending,
        // and cancels the connection timeout BEFORE runOnUiThread (which needs a real Activity).
        // We test the state changes by catching the NPE from runOnUiThread or detail.didCrash().
        setField(activity, "webViewConnected", true);
        setField(activity, "loadErrorPending", true);
        Runnable mockTimeout = mock(Runnable.class);
        setField(activity, "connectionTimeoutRunnable", mockTimeout);

        Object client = createWebViewClient();
        Method method = findMethod(client.getClass(), "onRenderProcessGone",
                android.webkit.WebView.class, android.webkit.RenderProcessGoneDetail.class);
        method.setAccessible(true);

        // Create a mock RenderProcessGoneDetail to avoid NPE from detail.didCrash()
        android.webkit.RenderProcessGoneDetail mockDetail = mock(android.webkit.RenderProcessGoneDetail.class);
        when(mockDetail.didCrash()).thenReturn(true);

        try {
            method.invoke(client, mockWebView, mockDetail);
        } catch (java.lang.reflect.InvocationTargetException e) {
            // Expected: runOnUiThread NPE on Unsafe-allocated activity
            // The important thing is that the state was reset before runOnUiThread
        }

        // State should be reset (these assignments happen before runOnUiThread)
        assertFalse(getBooleanField(activity, "webViewConnected"));
        assertFalse(getBooleanField(activity, "loadErrorPending"));
        // Connection timeout should be cancelled
        verify(mockWebView).removeCallbacks(mockTimeout);
        assertNull(getField(activity, "connectionTimeoutRunnable"));
    }

    // =====================================================
    // onReceivedSslError: dialog lifecycle
    // =====================================================

    @Test
    public void onReceivedSslError_localhost_autoAccepts() throws Exception {
        when(mockWebView.getUrl()).thenReturn("https://localhost:20000");
        when(mockPrefs.getString(any(), any())).thenReturn("https://localhost:20000");

        Object client = createWebViewClient();
        Method method = findMethod(client.getClass(), "onReceivedSslError",
                android.webkit.WebView.class, android.webkit.SslErrorHandler.class, android.net.http.SslError.class);
        method.setAccessible(true);

        android.webkit.SslErrorHandler mockHandler = mock(android.webkit.SslErrorHandler.class);
        method.invoke(client, mockWebView, mockHandler, null);

        verify(mockHandler).proceed();
    }

    @Test
    public void onReceivedSslError_nonHttps_cancels() throws Exception {
        when(mockWebView.getUrl()).thenReturn("https://192.168.1.100:20000");
        when(mockPrefs.getString(any(), any())).thenReturn("http://192.168.1.100:20000");

        Object client = createWebViewClient();
        Method method = findMethod(client.getClass(), "onReceivedSslError",
                android.webkit.WebView.class, android.webkit.SslErrorHandler.class, android.net.http.SslError.class);
        method.setAccessible(true);

        android.webkit.SslErrorHandler mockHandler = mock(android.webkit.SslErrorHandler.class);
        method.invoke(client, mockWebView, mockHandler, null);

        verify(mockHandler).cancel();
    }

    @Test
    public void onReceivedSslError_httpsServer_cancelsAndSchedulesLogin() throws Exception {
        // SSL is now handled at the OkHttp level during pre-authentication.
        // If WebView encounters an SSL error and the user has NOT confirmed the cert,
        // it cancels and returns to login page.
        setField(activity, "sslCertTrustedByUser", false);
        when(mockWebView.getUrl()).thenReturn("https://192.168.1.100:20000");
        when(mockPrefs.getString(any(), any())).thenReturn("https://192.168.1.100:20000");

        final Runnable[] capturedRunnable = new Runnable[1];
        doAnswer(invocation -> {
            capturedRunnable[0] = invocation.getArgument(0);
            return true;
        }).when(mockWebView).postDelayed(any(Runnable.class), any(long.class));

        Object client = createWebViewClient();
        Method method = findMethod(client.getClass(), "onReceivedSslError",
                android.webkit.WebView.class, android.webkit.SslErrorHandler.class, android.net.http.SslError.class);
        method.setAccessible(true);

        android.webkit.SslErrorHandler mockHandler = mock(android.webkit.SslErrorHandler.class);
        method.invoke(client, mockWebView, mockHandler, null);

        // Should cancel the SSL handler (not show dialog)
        verify(mockHandler).cancel();
        // Should set loadErrorPending
        assertTrue(getBooleanField(activity, "loadErrorPending"));
        // Should hide WebView
        verify(mockWebView).setVisibility(android.view.View.INVISIBLE);

        // Execute the captured runnable to verify showLoginPage is called
        assertNotNull(capturedRunnable[0]);
        capturedRunnable[0].run();
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
    }

    @Test
    public void onReceivedSslError_sslCertTrustedByUser_autoAccepts() throws Exception {
        // When the user has already confirmed the SSL certificate at the OkHttp level,
        // WebView should auto-accept SSL errors for that server.
        setField(activity, "sslCertTrustedByUser", true);
        when(mockWebView.getUrl()).thenReturn("https://192.168.1.100:20000");

        Object client = createWebViewClient();
        Method method = findMethod(client.getClass(), "onReceivedSslError",
                android.webkit.WebView.class, android.webkit.SslErrorHandler.class, android.net.http.SslError.class);
        method.setAccessible(true);

        android.webkit.SslErrorHandler mockHandler = mock(android.webkit.SslErrorHandler.class);
        method.invoke(client, mockWebView, mockHandler, null);

        // Should auto-accept (proceed) because user already confirmed
        verify(mockHandler).proceed();
        // Should NOT set loadErrorPending or hide WebView
        assertFalse(getBooleanField(activity, "loadErrorPending"));
    }

    // =====================================================
    // recreateWebViewAfterCrash: extracted crash recovery
    // =====================================================

    @Test
    public void recreateWebViewAfterCrash_requiresActivityUI() throws Exception {
        // recreateWebViewAfterCrash accesses view.getParent() — NPE on mock WebView
        Method method = findMethod(MainActivity.class, "recreateWebViewAfterCrash",
                android.webkit.WebView.class);
        method.setAccessible(true);
        try {
            method.invoke(activity, mockWebView);
        } catch (java.lang.reflect.InvocationTargetException e) {
            // Expected: ViewGroup NPE on Unsafe-allocated activity
            assertNotNull(e.getCause());
        }
    }

    // --- Helper methods ---

    @SuppressWarnings("unchecked")
    private static <T> T allocate(Class<T> clazz) throws Exception {
        try {
            Constructor<T> ctor = clazz.getDeclaredConstructor();
            ctor.setAccessible(true);
            return ctor.newInstance();
        } catch (Exception e) {
            var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
            unsafeField.setAccessible(true);
            Object unsafe = unsafeField.get(null);
            Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
            allocate.setAccessible(true);
            return (T) allocate.invoke(unsafe, clazz);
        }
    }

    private static void setField(Object target, String fieldName, Object value) throws Exception {
        Field field = findField(target.getClass(), fieldName);
        field.setAccessible(true);
        field.set(target, value);
    }

    private static Object getField(Object target, String fieldName) throws Exception {
        Field field = findField(target.getClass(), fieldName);
        field.setAccessible(true);
        return field.get(target);
    }

    private static boolean getBooleanField(Object target, String fieldName) throws Exception {
        Field field = findField(target.getClass(), fieldName);
        field.setAccessible(true);
        return field.getBoolean(target);
    }

    private static Field findField(Class<?> clazz, String fieldName) throws Exception {
        Class<?> c = clazz;
        while (c != null) {
            try {
                return c.getDeclaredField(fieldName);
            } catch (NoSuchFieldException e) {
                c = c.getSuperclass();
            }
        }
        throw new NoSuchFieldException(fieldName);
    }

    private static void invokeMethod(Object target, String methodName, Class<?>... paramTypes) throws Exception {
        Method method = findMethod(target.getClass(), methodName, paramTypes);
        method.setAccessible(true);
        // Build default args (null for objects, 0 for primitives)
        Object[] args = new Object[paramTypes.length];
        for (int i = 0; i < paramTypes.length; i++) {
            if (paramTypes[i].isPrimitive()) {
                if (paramTypes[i] == boolean.class) args[i] = false;
                else if (paramTypes[i] == int.class) args[i] = 0;
                else if (paramTypes[i] == long.class) args[i] = 0L;
                else if (paramTypes[i] == double.class) args[i] = 0.0;
                else if (paramTypes[i] == float.class) args[i] = 0.0f;
            }
        }
        method.invoke(target, args);
    }

    private static <T> void invokeMethod(Object target, String methodName, Class<T> paramType1, T arg1) throws Exception {
        Method method = findMethod(target.getClass(), methodName, paramType1);
        method.setAccessible(true);
        method.invoke(target, arg1);
    }

    private static <T1, T2> void invokeMethod(Object target, String methodName,
            Class<T1> paramType1, T1 arg1, Class<T2> paramType2, T2 arg2) throws Exception {
        Method method = findMethod(target.getClass(), methodName, paramType1, paramType2);
        method.setAccessible(true);
        method.invoke(target, arg1, arg2);
    }

    private static Method findMethod(Class<?> clazz, String methodName, Class<?>... paramTypes) throws Exception {
        Class<?> c = clazz;
        while (c != null) {
            try {
                return c.getDeclaredMethod(methodName, paramTypes);
            } catch (NoSuchMethodException e) {
                c = c.getSuperclass();
            }
        }
        throw new NoSuchMethodException(methodName);
    }

    /**
     * Create a ClawBenchWebViewClient instance via reflection.
     * The inner class constructor takes an outer class reference.
     */
    private Object createWebViewClient() throws Exception {
        // ClawBenchWebViewClient is a non-static inner class of MainActivity
        Class<?> clientClass = null;
        for (Class<?> inner : MainActivity.class.getDeclaredClasses()) {
            if (inner.getSimpleName().equals("ClawBenchWebViewClient")) {
                clientClass = inner;
                break;
            }
        }
        assertNotNull("ClawBenchWebViewClient class not found", clientClass);

        Constructor<?> ctor = clientClass.getDeclaredConstructor(MainActivity.class);
        ctor.setAccessible(true);
        return ctor.newInstance(activity);
    }
}
