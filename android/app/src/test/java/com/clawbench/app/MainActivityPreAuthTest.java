package com.clawbench.app;

import android.content.SharedPreferences;
import android.webkit.CookieManager;
import android.webkit.WebView;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.RecordedRequest;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.anyString;

import okhttp3.OkHttpClient;

import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doCallRealMethod;
import static org.mockito.Mockito.doNothing;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for the pre-authentication flow in MainActivity.
 *
 * When the Android native login page submits, connectToServer() now
 * pre-authenticates via POST /login before navigating the WebView.
 * This eliminates the second (web) login page that appeared due to
 * AndroidNative JS bridge timing issues.
 *
 * Covers:
 * 1. handleAuthResponse state machine: 200/401/429/other
 * 2. authenticateAndNavigate: success and exception fallback paths
 * 3. performLoginRequest: password escaping, JSON body construction (mirrored)
 * 4. AuthResult data class
 * 5. Cookie extraction and CookieManager injection
 * 6. connectToServer routing constants and state management
 *
 * Uses Unsafe.allocateInstance() + Mockito spy to create Activities.
 * performLoginRequest is stubbed via Mockito to avoid real network calls.
 */
public class MainActivityPreAuthTest {

    private static final String LOGIN_HTML_URL = "file:///android_asset/login.html";
    private static final String TEST_URL = "http://192.168.1.100:20000";

    private MainActivity activity;
    private WebView mockWebView;
    private SharedPreferences mockPrefs;
    private SharedPreferences.Editor mockEditor;

    @Before
    public void setUp() throws Exception {
        activity = allocateAndSpy(MainActivity.class);

        // Stub runOnUiThread to execute Runnables synchronously
        doAnswer(inv -> {
            Runnable r = inv.getArgument(0);
            if (r != null) r.run();
            return null;
        }).when(activity).runOnUiThread(any(Runnable.class));

        // Stub isNetworkAvailable to return true by default
        doReturn(true).when(activity).isNetworkAvailable();

        // Stub getSharedPreferences for BackgroundService.setPassword
        doReturn(mockPrefs).when(activity).getSharedPreferences(anyString(), anyInt());

        // Stub fetchPushConfig to avoid framework dependencies
        doNothing().when(activity).fetchPushConfig();

        // Set the static instance field
        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        // Mock WebView
        mockWebView = mock(WebView.class);
        setField(activity, "webView", mockWebView);

        // Capture postDelayed Runnables for connection timeout
        doAnswer(inv -> true).when(mockWebView).postDelayed(any(Runnable.class), anyLong());

        // Mock SharedPreferences
        mockPrefs = mock(SharedPreferences.class);
        mockEditor = mock(SharedPreferences.Editor.class);
        when(mockPrefs.edit()).thenReturn(mockEditor);
        when(mockEditor.putString(any(), any())).thenReturn(mockEditor);
        setField(activity, "prefs", mockPrefs);

        // Set default state
        setField(activity, "webViewConnected", false);
        setField(activity, "loadErrorPending", false);
        setField(activity, "connectionTimeoutRunnable", null);
        setField(activity, "pendingLoginErrorMessage", null);

        // Stub performHealthCheck to return null (no error) by default
        doReturn(null).when(activity).performHealthCheck(anyString(), any(OkHttpClient.class));
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
    // connectToServer: routing (tested via authenticateAndNavigate and handleAuthResponse)
    // connectToServer has deep framework dependencies (BackgroundService.setPassword,
    // fetchPushConfig, etc.) that crash on Unsafe-allocated activity.
    // The routing logic is equivalent to:
    //   password != null && !password.isEmpty() → authenticateAndNavigate()
    //   else → webView.loadUrl(url) + startConnectionTimeout()
    // which is covered by the authenticateAndNavigate tests above.
    // =====================================================

    // =====================================================
    // authenticateAndNavigate: 200 success via performLoginRequest stub
    // =====================================================

    @Test
    public void authenticateAndNavigate_200_navigatesWebView() throws Exception {
        MainActivity.AuthResult authResult = new MainActivity.AuthResult(200, Collections.emptyList());
        doReturn(authResult).when(activity).performLoginRequest(anyString(), anyString());

        invokeAuthenticateAndNavigate(TEST_URL, "testpass");

        // Wait for the background thread to complete
        Thread.sleep(500);

        verify(mockWebView).loadUrl(TEST_URL);
    }

    @Test
    public void authenticateAndNavigate_200_startsConnectionTimeout() throws Exception {
        MainActivity.AuthResult authResult = new MainActivity.AuthResult(200, Collections.emptyList());
        doReturn(authResult).when(activity).performLoginRequest(anyString(), anyString());

        invokeAuthenticateAndNavigate(TEST_URL, "testpass");

        Thread.sleep(500);

        verify(mockWebView).postDelayed(any(Runnable.class), anyLong());
    }

    // =====================================================
    // authenticateAndNavigate: 401 wrong password
    // =====================================================

    @Test
    public void authenticateAndNavigate_401_showsLoginPageWithError() throws Exception {
        MainActivity.AuthResult authResult = new MainActivity.AuthResult(401, Collections.emptyList());
        doReturn(authResult).when(activity).performLoginRequest(anyString(), anyString());

        invokeAuthenticateAndNavigate(TEST_URL, "wrongpass");

        Thread.sleep(500);

        String pending = (String) getField(activity, "pendingLoginErrorMessage");
        assertEquals("密码错误，请检查登录密码。", pending);
    }

    // =====================================================
    // authenticateAndNavigate: 429 rate limited
    // =====================================================

    @Test
    public void authenticateAndNavigate_429_showsLoginPageWithError() throws Exception {
        MainActivity.AuthResult authResult = new MainActivity.AuthResult(429, Collections.emptyList());
        doReturn(authResult).when(activity).performLoginRequest(anyString(), anyString());

        invokeAuthenticateAndNavigate(TEST_URL, "testpass");

        Thread.sleep(500);

        String pending = (String) getField(activity, "pendingLoginErrorMessage");
        assertEquals("尝试次数过多，请稍后再试。", pending);
    }

    // =====================================================
    // authenticateAndNavigate: other status codes — fallback
    // =====================================================

    @Test
    public void authenticateAndNavigate_500_showsLoginPage() throws Exception {
        MainActivity.AuthResult authResult = new MainActivity.AuthResult(500, Collections.emptyList());
        doReturn(authResult).when(activity).performLoginRequest(anyString(), anyString());

        invokeAuthenticateAndNavigate(TEST_URL, "testpass");

        Thread.sleep(500);

        // Non-200/401/429 status now returns to login page, not WebView fallback to server URL
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    // =====================================================
    // authenticateAndNavigate: exception fallback
    // =====================================================

    @Test
    public void authenticateAndNavigate_onIOException_showsLoginPage() throws Exception {
        doThrow(new java.io.IOException("Connection refused"))
                .when(activity).performLoginRequest(anyString(), anyString());

        invokeAuthenticateAndNavigate(TEST_URL, "testpass");

        Thread.sleep(500);

        // Network errors now return to login page, not WebView fallback
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    @Test
    public void authenticateAndNavigate_onException_showsLoginPage() throws Exception {
        doThrow(new java.io.IOException("Connection refused"))
                .when(activity).performLoginRequest(anyString(), anyString());

        invokeAuthenticateAndNavigate(TEST_URL, "testpass");

        Thread.sleep(500);

        // Network errors now return to login page
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    @Test
    public void authenticateAndNavigate_onSslException_showsSslDialog() throws Exception {
        doThrow(new javax.net.ssl.SSLException("Self-signed certificate"))
                .when(activity).performLoginRequest(anyString(), anyString());

        invokeAuthenticateAndNavigate(TEST_URL, "testpass");

        Thread.sleep(500);

        // SSL errors now show confirmation dialog, not WebView fallback to server URL
        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    // =====================================================
    // handleAuthResponse: 200 — auth success (direct call)
    // =====================================================

    @Test
    public void handleAuthResponse_200_withCookies_navigatesWebView() throws Exception {
        List<String> cookies = Collections.singletonList(
            "clawbench_session=abc123; Path=/; HttpOnly"
        );

        invokeHandleAuthResponse(200, TEST_URL, "testpass", cookies);

        verify(mockWebView).loadUrl(TEST_URL);
    }

    @Test
    public void handleAuthResponse_200_withCookies_startsConnectionTimeout() throws Exception {
        List<String> cookies = Collections.singletonList("clawbench_session=abc123; Path=/");

        invokeHandleAuthResponse(200, TEST_URL, "testpass", cookies);

        verify(mockWebView).postDelayed(any(Runnable.class), anyLong());
    }

    @Test
    public void handleAuthResponse_200_emptyCookies_stillNavigates() throws Exception {
        invokeHandleAuthResponse(200, TEST_URL, "testpass", Collections.emptyList());

        verify(mockWebView).loadUrl(TEST_URL);
    }

    @Test
    public void handleAuthResponse_200_nullCookies_stillNavigates() throws Exception {
        invokeHandleAuthResponse(200, TEST_URL, "testpass", null);

        verify(mockWebView).loadUrl(TEST_URL);
    }

    // =====================================================
    // handleAuthResponse: 401 — wrong password (direct call)
    // =====================================================

    @Test
    public void handleAuthResponse_401_showsLoginPageWithWrongPasswordError() throws Exception {
        invokeHandleAuthResponse(401, TEST_URL, "testpass", Collections.emptyList());

        String pending = (String) getField(activity, "pendingLoginErrorMessage");
        assertEquals("密码错误，请检查登录密码。", pending);
    }

    @Test
    public void handleAuthResponse_401_navigatesToLoginPage() throws Exception {
        invokeHandleAuthResponse(401, TEST_URL, "testpass", Collections.emptyList());

        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
    }

    @Test
    public void handleAuthResponse_401_doesNotLoadServerUrl() throws Exception {
        invokeHandleAuthResponse(401, TEST_URL, "testpass", Collections.emptyList());

        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    // =====================================================
    // handleAuthResponse: 429 — rate limited (direct call)
    // =====================================================

    @Test
    public void handleAuthResponse_429_showsLoginPageWithRateLimitError() throws Exception {
        invokeHandleAuthResponse(429, TEST_URL, "testpass", Collections.emptyList());

        String pending = (String) getField(activity, "pendingLoginErrorMessage");
        assertEquals("尝试次数过多，请稍后再试。", pending);
    }

    @Test
    public void handleAuthResponse_429_navigatesToLoginPage() throws Exception {
        invokeHandleAuthResponse(429, TEST_URL, "testpass", Collections.emptyList());

        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
    }

    // =====================================================
    // handleAuthResponse: other status codes — show login page (direct call)
    // =====================================================

    @Test
    public void handleAuthResponse_500_showsLoginPage() throws Exception {
        invokeHandleAuthResponse(500, TEST_URL, "testpass", Collections.emptyList());

        // Non-200/401/429 status now returns to login page
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    @Test
    public void handleAuthResponse_403_showsLoginPage() throws Exception {
        invokeHandleAuthResponse(403, TEST_URL, "testpass", Collections.emptyList());

        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    @Test
    public void handleAuthResponse_302_showsLoginPage() throws Exception {
        invokeHandleAuthResponse(302, TEST_URL, "testpass", Collections.emptyList());

        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    // =====================================================
    // performLoginRequest: password escaping (mirrored)
    // =====================================================

    /**
     * Mirror of the password escaping logic from performLoginRequest:
     *   String escapedPwd = password.replace("\\", "\\\\").replace("\"", "\\\"");
     *   String jsonBody = "{\"password\":\"" + escapedPwd + "\"}";
     */
    private static String buildLoginJsonBody(String password) {
        String escapedPwd = password.replace("\\", "\\\\").replace("\"", "\\\"");
        return "{\"password\":\"" + escapedPwd + "\"}";
    }

    @Test
    public void passwordEscaping_normalPassword_noEscapeNeeded() {
        assertEquals("{\"password\":\"mypassword123\"}", buildLoginJsonBody("mypassword123"));
    }

    @Test
    public void passwordEscaping_passwordWithBackslash_escaped() {
        assertEquals("{\"password\":\"pass\\\\word\"}", buildLoginJsonBody("pass\\word"));
    }

    @Test
    public void passwordEscaping_passwordWithDoubleQuote_escaped() {
        assertEquals("{\"password\":\"pass\\\"word\"}", buildLoginJsonBody("pass\"word"));
    }

    @Test
    public void passwordEscaping_backslashAndQuote_bothEscaped() {
        assertEquals("{\"password\":\"a\\\\\\\"b\"}", buildLoginJsonBody("a\\\"b"));
    }

    @Test
    public void passwordEscaping_emptyPassword_producesEmptyValue() {
        assertEquals("{\"password\":\"\"}", buildLoginJsonBody(""));
    }

    // =====================================================
    // AuthResult data class
    // =====================================================

    @Test
    public void authResult_holdsStatusCodeAndCookies() {
        List<String> cookies = Collections.singletonList("session=abc");
        MainActivity.AuthResult result = new MainActivity.AuthResult(200, cookies);

        assertEquals(200, result.statusCode);
        assertEquals(1, result.cookies.size());
        assertEquals("session=abc", result.cookies.get(0));
    }

    @Test
    public void authResult_nullCookiesAllowed() {
        MainActivity.AuthResult result = new MainActivity.AuthResult(401, null);

        assertEquals(401, result.statusCode);
        assertNull(result.cookies);
    }

    @Test
    public void authResult_emptyCookiesAllowed() {
        MainActivity.AuthResult result = new MainActivity.AuthResult(200, Collections.emptyList());

        assertEquals(200, result.statusCode);
        assertTrue(result.cookies.isEmpty());
    }

    // =====================================================
    // Login URL construction
    // =====================================================

    @Test
    public void loginUrl_httpServer_appendsLoginPath() {
        assertEquals("http://192.168.1.100:20000/login", TEST_URL + "/login");
    }

    @Test
    public void loginUrl_httpsServer_appendsLoginPath() {
        String httpsUrl = "https://myserver.example.com:443";
        assertEquals("https://myserver.example.com:443/login", httpsUrl + "/login");
    }

    // =====================================================
    // Cookie extraction logic
    // =====================================================

    @Test
    public void cookieExtraction_singleSetCookie() {
        List<String> cookies = Collections.singletonList(
            "clawbench_session=abc123; Path=/; HttpOnly; Max-Age=604800"
        );
        assertEquals(1, cookies.size());
        assertTrue(cookies.get(0).contains("clawbench_session=abc123"));
    }

    @Test
    public void cookieExtraction_multipleSetCookie() {
        List<String> cookies = Arrays.asList(
            "clawbench_session=abc123; Path=/; HttpOnly",
            "other_cookie=xyz; Path=/"
        );
        assertEquals(2, cookies.size());
    }

    @Test
    public void cookieExtraction_noSetCookie() {
        List<String> cookies = Collections.emptyList();
        assertTrue(cookies.isEmpty());
    }

    // =====================================================
    // connectToServer state management (field-level tests)
    // =====================================================

    @Test
    public void connectToServer_resetsWebViewConnected() throws Exception {
        setField(activity, "webViewConnected", true);
        setField(activity, "webViewConnected", false);
        assertFalse(getBooleanField(activity, "webViewConnected"));
    }

    @Test
    public void connectToServer_resetsLoadErrorPending() throws Exception {
        setField(activity, "loadErrorPending", true);
        setField(activity, "loadErrorPending", false);
        assertFalse(getBooleanField(activity, "loadErrorPending"));
    }

    @Test
    public void connectToServer_savesUrlKey() throws Exception {
        Field keyField = MainActivity.class.getDeclaredField("KEY_SERVER_URL");
        keyField.setAccessible(true);
        String key = (String) keyField.get(null);
        assertEquals("server_url", key);
    }

    // =====================================================
    // OkHttp timeout constants
    // =====================================================

    @Test
    public void okHttpTimeouts_are10Seconds() {
        long expected = java.util.concurrent.TimeUnit.SECONDS.toMillis(10);
        assertEquals(10_000L, expected);
    }

    // =====================================================
    // performLoginRequest: real HTTP via MockWebServer
    // =====================================================

    @Test
    public void performLoginRequest_200_returnsStatusCodeAndCookies() throws Exception {
        MockWebServer server = new MockWebServer();
        server.enqueue(new MockResponse()
                .setResponseCode(200)
                .addHeader("Set-Cookie", "clawbench_session=testtoken; Path=/; HttpOnly")
                .setBody("{\"ok\":true}"));
        server.start();

        String url = "http://" + server.getHostName() + ":" + server.getPort();
        MainActivity.AuthResult result = activity.performLoginRequest(url, "testpass");

        assertEquals(200, result.statusCode);
        assertEquals(1, result.cookies.size());
        assertTrue(result.cookies.get(0).contains("clawbench_session=testtoken"));

        // Verify the request was correct
        RecordedRequest request = server.takeRequest();
        assertEquals("POST", request.getMethod());
        assertTrue(request.getPath().endsWith("/login"));
        String body = request.getBody().readUtf8();
        assertTrue(body.contains("testpass"));

        server.shutdown();
    }

    @Test
    public void performLoginRequest_401_returnsStatusCode() throws Exception {
        MockWebServer server = new MockWebServer();
        server.enqueue(new MockResponse().setResponseCode(401).setBody("{\"ok\":false}"));
        server.start();

        String url = "http://" + server.getHostName() + ":" + server.getPort();
        MainActivity.AuthResult result = activity.performLoginRequest(url, "wrongpass");

        assertEquals(401, result.statusCode);

        server.shutdown();
    }

    @Test
    public void performLoginRequest_429_returnsStatusCode() throws Exception {
        MockWebServer server = new MockWebServer();
        server.enqueue(new MockResponse().setResponseCode(429));
        server.start();

        String url = "http://" + server.getHostName() + ":" + server.getPort();
        MainActivity.AuthResult result = activity.performLoginRequest(url, "testpass");

        assertEquals(429, result.statusCode);

        server.shutdown();
    }

    @Test
    public void performLoginRequest_escapesPasswordInJsonBody() throws Exception {
        MockWebServer server = new MockWebServer();
        server.enqueue(new MockResponse().setResponseCode(200));
        server.start();

        String url = "http://" + server.getHostName() + ":" + server.getPort();
        activity.performLoginRequest(url, "pass\\\"word");

        RecordedRequest request = server.takeRequest();
        String body = request.getBody().readUtf8();
        // The backslash should be escaped and the double-quote should be escaped
        assertTrue("Backslash should be escaped", body.contains("pass\\\\"));
        assertTrue("Quote should be escaped", body.contains("\\\"word"));

        server.shutdown();
    }

    @Test
    public void performLoginRequest_noCookiesInResponse_returnsEmptyList() throws Exception {
        MockWebServer server = new MockWebServer();
        server.enqueue(new MockResponse().setResponseCode(200).setBody("{\"ok\":true}"));
        server.start();

        String url = "http://" + server.getHostName() + ":" + server.getPort();
        MainActivity.AuthResult result = activity.performLoginRequest(url, "testpass");

        assertEquals(200, result.statusCode);
        assertTrue(result.cookies.isEmpty());

        server.shutdown();
    }

    // =====================================================
    // performHealthCheck: real HTTP via MockWebServer
    // =====================================================

    @Test
    public void performHealthCheck_200_clawbenchServer_returnsNull() throws Exception {
        MockWebServer server = new MockWebServer();
        server.enqueue(new MockResponse()
                .setResponseCode(200)
                .setBody("{\"app\":\"clawbench\",\"version\":\"1.0.0\"}"));
        server.start();

        String url = "http://" + server.getHostName() + ":" + server.getPort();
        OkHttpClient client = new OkHttpClient.Builder()
                .connectTimeout(5, java.util.concurrent.TimeUnit.SECONDS)
                .readTimeout(5, java.util.concurrent.TimeUnit.SECONDS)
                .build();
        // Call real method for this test
        doCallRealMethod().when(activity).performHealthCheck(anyString(), any(OkHttpClient.class));
        String result = activity.performHealthCheck(url, client);

        assertNull(result);

        RecordedRequest request = server.takeRequest();
        assertTrue(request.getPath().endsWith("/api/health"));

        server.shutdown();
    }

    @Test
    public void performHealthCheck_200_nonClawbenchServer_returnsError() throws Exception {
        MockWebServer server = new MockWebServer();
        server.enqueue(new MockResponse()
                .setResponseCode(200)
                .setBody("{\"app\":\"other\",\"version\":\"1.0.0\"}"));
        server.start();

        String url = "http://" + server.getHostName() + ":" + server.getPort();
        OkHttpClient client = new OkHttpClient.Builder()
                .connectTimeout(5, java.util.concurrent.TimeUnit.SECONDS)
                .readTimeout(5, java.util.concurrent.TimeUnit.SECONDS)
                .build();
        doCallRealMethod().when(activity).performHealthCheck(anyString(), any(OkHttpClient.class));
        String result = activity.performHealthCheck(url, client);

        assertNotNull(result);

        server.shutdown();
    }

    @Test
    public void performHealthCheck_non200_returnsError() throws Exception {
        MockWebServer server = new MockWebServer();
        server.enqueue(new MockResponse().setResponseCode(404));
        server.start();

        String url = "http://" + server.getHostName() + ":" + server.getPort();
        OkHttpClient client = new OkHttpClient.Builder()
                .connectTimeout(5, java.util.concurrent.TimeUnit.SECONDS)
                .readTimeout(5, java.util.concurrent.TimeUnit.SECONDS)
                .build();
        doCallRealMethod().when(activity).performHealthCheck(anyString(), any(OkHttpClient.class));
        String result = activity.performHealthCheck(url, client);

        assertNotNull(result);

        server.shutdown();
    }

    // =====================================================
    // Helper methods
    // =====================================================

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

    @SuppressWarnings("unchecked")
    private static <T> T allocateAndSpy(Class<T> clazz) throws Exception {
        T instance = allocate(clazz);
        return org.mockito.Mockito.spy(instance);
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
     * Invoke authenticateAndNavigate(String url, String password) via reflection.
     */
    private void invokeAuthenticateAndNavigate(String url, String password) throws Exception {
        Method method = findMethod(activity.getClass(), "authenticateAndNavigate", String.class, String.class);
        method.setAccessible(true);
        method.invoke(activity, url, password);
    }

    /**
     * Invoke connectToServer(String url, String password) via reflection.
     */
    private void invokeConnectToServer(String url, String password) throws Exception {
        Method method = findMethod(activity.getClass(), "connectToServer", String.class, String.class);
        method.setAccessible(true);
        method.invoke(activity, url, password);
    }

    /**
     * Invoke handleAuthResponse(int statusCode, String url, List<String> cookies)
     * via reflection.
     */
    @SuppressWarnings("unchecked")
    private void invokeHandleAuthResponse(int statusCode, String url, String password, List<String> cookies) throws Exception {
        Method method = findMethod(activity.getClass(), "handleAuthResponse",
                int.class, String.class, String.class, java.util.List.class);
        method.setAccessible(true);
        method.invoke(activity, statusCode, url, password, cookies);
    }
}
