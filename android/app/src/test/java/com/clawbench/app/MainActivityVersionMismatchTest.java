package com.clawbench.app;

import android.content.SharedPreferences;
import android.webkit.WebView;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.Collections;
import java.util.concurrent.atomic.AtomicBoolean;

import okhttp3.OkHttpClient;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doNothing;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for the native pre-WebView version-mismatch gate in MainActivity.
 *
 * <p>The gate blocks navigation when the installed APK is older than the server, showing
 * a modal dialog with "Download APK" / "Force skip". The dialog itself cannot be rendered
 * in unit tests — {@code new AlertDialog.Builder(this)} needs an attached Context, and
 * these tests use {@code Unsafe.allocateInstance} (same constraint as
 * {@link MainActivityPreAuthTest}, whose SSL test likewise only asserts on navigation).
 * So the dialog-showing method is stubbed and the tests assert on the <em>decision</em>
 * and the button <em>actions</em>.
 */
public class MainActivityVersionMismatchTest {

    private static final String LOGIN_HTML_URL = "file:///android_asset/login.html";
    private static final String TEST_URL = "http://192.168.1.100:20000";

    private MainActivity activity;
    private WebView mockWebView;
    private SharedPreferences mockPrefs;
    private SharedPreferences.Editor mockEditor;

    @Before
    public void setUp() throws Exception {
        activity = allocateAndSpy(MainActivity.class);

        // The spy has no attached Context, so getString() would return null.
        doAnswer(inv -> {
            int resId = inv.getArgument(0);
            String v = MainActivityZhText.get(resId);
            return v != null ? v : "";
        }).when(activity).getString(anyInt());

        // Execute UI-thread Runnables synchronously.
        doAnswer(inv -> {
            Runnable r = inv.getArgument(0);
            if (r != null) r.run();
            return null;
        }).when(activity).runOnUiThread(any(Runnable.class));

        doReturn(true).when(activity).isNetworkAvailable();
        doReturn(mockPrefs).when(activity).getSharedPreferences(anyString(), anyInt());

        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        mockWebView = mock(WebView.class);
        setField(activity, "webView", mockWebView);
        doAnswer(inv -> true).when(mockWebView).postDelayed(any(Runnable.class), anyLong());

        mockPrefs = mock(SharedPreferences.class);
        mockEditor = mock(SharedPreferences.Editor.class);
        when(mockPrefs.edit()).thenReturn(mockEditor);
        when(mockEditor.putString(any(), any())).thenReturn(mockEditor);
        setField(activity, "prefs", mockPrefs);

        setField(activity, "webViewConnected", false);
        setField(activity, "loadErrorPending", false);
        setField(activity, "connectionTimeoutRunnable", null);
        setField(activity, "pendingLoginErrorMessage", null);
    }

    @After
    public void tearDown() throws Exception {
        try {
            Field instanceField = MainActivity.class.getDeclaredField("instance");
            instanceField.setAccessible(true);
            instanceField.set(null, null);
        } catch (Exception ignored) {
        }
    }

    // =====================================================
    // gateVersionMismatchAndProceed: decision
    // =====================================================

    @Test
    public void gate_olderApk_showsDialogAndDoesNotProceed() throws Exception {
        doReturn("v1.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));

        AtomicBoolean proceeded = new AtomicBoolean(false);
        invokeGate(TEST_URL, "v2.0.0", () -> proceeded.set(true));

        verify(activity).showVersionMismatchDialog(eq(TEST_URL), eq("v1.0.0"), eq("v2.0.0"), any(Runnable.class));
        assertFalse("navigation must not run while the dialog is up", proceeded.get());
    }

    @Test
    public void gate_equalVersions_skipsDialogAndProceeds() throws Exception {
        doReturn("v1.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));

        AtomicBoolean proceeded = new AtomicBoolean(false);
        invokeGate(TEST_URL, "v1.0.0", () -> proceeded.set(true));

        verify(activity, never()).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        assertTrue(proceeded.get());
    }

    @Test
    public void gate_apkNewerThanServer_skipsDialogAndProceeds() throws Exception {
        doReturn("v2.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));

        AtomicBoolean proceeded = new AtomicBoolean(false);
        invokeGate(TEST_URL, "v1.0.0", () -> proceeded.set(true));

        verify(activity, never()).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        assertTrue(proceeded.get());
    }

    // =====================================================
    // gateVersionMismatchAndProceed: fail-open cases
    // =====================================================

    @Test
    public void gate_nullServerVersion_failsOpen() throws Exception {
        doReturn("v1.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));

        AtomicBoolean proceeded = new AtomicBoolean(false);
        invokeGate(TEST_URL, null, () -> proceeded.set(true));

        verify(activity, never()).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        assertTrue("a server with no version must not block login", proceeded.get());
    }

    @Test
    public void gate_nonVersionedServer_failsOpen() throws Exception {
        doReturn("v1.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));

        AtomicBoolean proceeded = new AtomicBoolean(false);
        invokeGate(TEST_URL, "a0f87a96", () -> proceeded.set(true));

        verify(activity, never()).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        assertTrue(proceeded.get());
    }

    @Test
    public void gate_unreadableAppVersion_failsOpen() throws Exception {
        // getAppVersionName() returns null when PackageManager is unavailable.
        doReturn(null).when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));

        AtomicBoolean proceeded = new AtomicBoolean(false);
        invokeGate(TEST_URL, "v2.0.0", () -> proceeded.set(true));

        verify(activity, never()).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        assertTrue(proceeded.get());
    }

    @Test
    public void gate_nonVersionedApk_failsOpen() throws Exception {
        doReturn("dev").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));

        AtomicBoolean proceeded = new AtomicBoolean(false);
        invokeGate(TEST_URL, "v2.0.0", () -> proceeded.set(true));

        verify(activity, never()).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        assertTrue(proceeded.get());
    }

    // =====================================================
    // handleAuthResponse 200: the gate sits between health check and navigation
    // =====================================================

    @Test
    public void handleAuthResponse_200_olderApk_doesNotNavigate() throws Exception {
        doReturn("v1.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        doReturn(MainActivity.HealthCheckResult.success("v2.0.0"))
                .when(activity).performHealthCheck(anyString(), any(OkHttpClient.class));

        activity.handleAuthResponse(200, TEST_URL, "testpass", Collections.emptyList(),
                new OkHttpClient());

        verify(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    @Test
    public void handleAuthResponse_200_equalVersions_navigates() throws Exception {
        doReturn("v1.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        doReturn(MainActivity.HealthCheckResult.success("v1.0.0"))
                .when(activity).performHealthCheck(anyString(), any(OkHttpClient.class));

        activity.handleAuthResponse(200, TEST_URL, "testpass", Collections.emptyList(),
                new OkHttpClient());

        verify(activity, never()).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        verify(mockWebView).loadUrl(TEST_URL);
    }

    @Test
    public void handleAuthResponse_200_noServerVersion_navigates() throws Exception {
        // Fail-open: /api/health responded without a version field.
        doReturn("v1.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        doReturn(MainActivity.HealthCheckResult.success(null))
                .when(activity).performHealthCheck(anyString(), any(OkHttpClient.class));

        activity.handleAuthResponse(200, TEST_URL, "testpass", Collections.emptyList(),
                new OkHttpClient());

        verify(mockWebView).loadUrl(TEST_URL);
    }

    // =====================================================
    // checkConnectivityAndNavigate (no-password path)
    // =====================================================

    @Test
    public void checkConnectivity_olderApk_doesNotNavigate() throws Exception {
        doReturn("v1.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        doReturn(MainActivity.HealthCheckResult.success("v2.0.0"))
                .when(activity).performHealthCheck(anyString(), any(OkHttpClient.class));

        invokeCheckConnectivityAndNavigate(TEST_URL);
        Thread.sleep(500);

        verify(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        verify(mockWebView, never()).loadUrl(TEST_URL);
    }

    @Test
    public void checkConnectivity_equalVersions_navigates() throws Exception {
        doReturn("v1.0.0").when(activity).getAppVersionName();
        doNothing().when(activity).showVersionMismatchDialog(anyString(), anyString(), anyString(), any(Runnable.class));
        doReturn(MainActivity.HealthCheckResult.success("v1.0.0"))
                .when(activity).performHealthCheck(anyString(), any(OkHttpClient.class));

        invokeCheckConnectivityAndNavigate(TEST_URL);
        Thread.sleep(500);

        verify(mockWebView).loadUrl(TEST_URL);
    }

    // =====================================================
    // Dialog button actions
    // =====================================================

    @Test
    public void onVersionMismatchDownload_startsDownloadAndReturnsToLoginPage() throws Exception {
        doNothing().when(activity).startApkDownload(anyString());

        activity.onVersionMismatchDownload(TEST_URL);

        verify(activity).startApkDownload(TEST_URL);
        verify(mockWebView).loadUrl(LOGIN_HTML_URL);
    }

    @Test
    public void startApkDownload_usesServerBaseUrl() throws Exception {
        doNothing().when(activity).downloadFileViaManager(anyString(), anyString());

        activity.startApkDownload(TEST_URL);

        verify(activity).downloadFileViaManager(TEST_URL + "/api/apk", "clawbench-android.apk");
    }

    @Test
    public void startApkDownload_ignoresEmptyUrl() throws Exception {
        activity.startApkDownload("");
        activity.startApkDownload(null);

        verify(activity, never()).downloadFileViaManager(anyString(), anyString());
    }

    // =====================================================
    // Helpers
    // =====================================================

    private void invokeGate(String url, String serverVersion, Runnable proceed) throws Exception {
        Method m = findMethod(MainActivity.class, "gateVersionMismatchAndProceed",
                String.class, String.class, Runnable.class);
        m.setAccessible(true);
        m.invoke(activity, url, serverVersion, proceed);
    }

    private void invokeCheckConnectivityAndNavigate(String url) throws Exception {
        Method m = findMethod(MainActivity.class, "checkConnectivityAndNavigate", String.class);
        m.setAccessible(true);
        m.invoke(activity, url);
    }

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
}
