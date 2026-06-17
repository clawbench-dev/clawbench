package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.concurrent.CopyOnWriteArrayList;

import static org.mockito.Mockito.*;

import androidx.fragment.app.FragmentController;

/**
 * Unit tests for BrowserActivity and MainActivity WebView lifecycle methods
 * (pauseWebView/resumeWebView) that pause/resume WebView to save resources,
 * and BrowserActivity keep-alive behavior (onNewIntent, moveTaskToBack).
 *
 * Uses Unsafe.allocateInstance() to create Activities without triggering
 * AppCompatActivity's constructor. Uses Mockito to mock WebView.
 * With returnDefaultValues = true, android.jar stubs are no-ops.
 */
public class WebViewLifecycleTest {

    private MainActivity mainActivity;
    private BrowserActivity browserActivity;

    @Before
    public void setUp() throws Exception {
        mainActivity = allocate(MainActivity.class);
        // Set the static instance field
        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, mainActivity);

        browserActivity = allocate(BrowserActivity.class);
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
    // BrowserActivity.pauseWebView / resumeWebView tests
    // =====================================================

    @Test
    public void browserActivity_pauseWebView_callsWebViewPauseAndPauseTimers() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(browserActivity, "webView", mockWebView);

        invokeMethod(browserActivity, "pauseWebView");

        verify(mockWebView).onPause();
        verify(mockWebView).pauseTimers();
    }

    @Test
    public void browserActivity_resumeWebView_callsWebViewResumeAndResumeTimers() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(browserActivity, "webView", mockWebView);

        invokeMethod(browserActivity, "resumeWebView");

        verify(mockWebView).onResume();
        verify(mockWebView).resumeTimers();
    }

    // =====================================================
    // MainActivity.pauseWebView / resumeWebView tests
    // =====================================================

    @Test
    public void mainActivity_pauseWebView_callsWebViewPauseAndPauseTimers() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(mainActivity, "webView", mockWebView);

        invokeMethod(mainActivity, "pauseWebView");

        verify(mockWebView).onPause();
        verify(mockWebView).pauseTimers();
    }

    @Test
    public void mainActivity_resumeWebView_callsWebViewResumeAndResumeTimers() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(mainActivity, "webView", mockWebView);

        invokeMethod(mainActivity, "resumeWebView");

        verify(mockWebView).onResume();
        verify(mockWebView).resumeTimers();
    }

    // =====================================================
    // BrowserActivity.onNewIntent tests
    // =====================================================

    /**
     * FragmentActivity.onNewIntent() calls mFragments.noteStateNotSaved().
     * ComponentActivity.onNewIntent() iterates mOnNewIntentListeners.
     * Since we use Unsafe.allocateInstance(), these are null. Initialize them.
     */
    private void initFragmentController() throws Exception {
        FragmentController mockController = mock(FragmentController.class);
        setField(browserActivity, "mFragments", mockController);
        // ComponentActivity.mOnNewIntentListeners — needed by super.onNewIntent()
        setField(browserActivity, "mOnNewIntentListeners", new CopyOnWriteArrayList<>());
    }

    @Test
    public void browserActivity_onNewIntent_loadsNewUrl() throws Exception {
        initFragmentController();
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(browserActivity, "webView", mockWebView);
        setField(browserActivity, "tunnelRetryCount", 5);
        // Set a pre-existing targetHost to verify it gets reset when host is empty
        setField(browserActivity, "targetHost", "old.host.com");
        // Ensure pendingUrl is null so the same-URL early-return doesn't trigger
        setField(browserActivity, "pendingUrl", null);

        android.widget.EditText mockUrlBar = mock(android.widget.EditText.class);
        setField(browserActivity, "urlBar", mockUrlBar);

        // android.content.Intent extras return defaults with returnDefaultValues=true,
        // so we mock the Intent to provide the expected values.
        android.content.Intent intent = mock(android.content.Intent.class);
        when(intent.getIntExtra("port", 0)).thenReturn(8080);
        when(intent.getStringExtra("protocol")).thenReturn("http");
        when(intent.getStringExtra("host")).thenReturn("");

        invokeMethod(browserActivity, "onNewIntent", android.content.Intent.class, intent);

        verify(mockWebView).loadUrl("http://localhost:8080/");
        // tunnelRetryCount should be reset
        assert getField(browserActivity.getClass(), "tunnelRetryCount").getInt(browserActivity) == 0;
        // targetHost should be reset to empty when host is empty
        assert "".equals(getField(browserActivity.getClass(), "targetHost").get(browserActivity));
    }

    @Test
    public void browserActivity_onNewIntent_withHost_setsTargetHost() throws Exception {
        initFragmentController();
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(browserActivity, "webView", mockWebView);
        setField(browserActivity, "pendingUrl", null);

        android.widget.EditText mockUrlBar = mock(android.widget.EditText.class);
        setField(browserActivity, "urlBar", mockUrlBar);

        android.content.Intent intent = mock(android.content.Intent.class);
        when(intent.getIntExtra("port", 0)).thenReturn(9090);
        when(intent.getStringExtra("protocol")).thenReturn("https");
        when(intent.getStringExtra("host")).thenReturn("192.168.1.1");

        invokeMethod(browserActivity, "onNewIntent", android.content.Intent.class, intent);

        verify(mockWebView).loadUrl("https://localhost:9090/");
        assert "192.168.1.1".equals(getField(browserActivity.getClass(), "targetHost").get(browserActivity));
    }

    @Test
    public void browserActivity_onNewIntent_stripsDefaultPort() throws Exception {
        initFragmentController();
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(browserActivity, "webView", mockWebView);
        setField(browserActivity, "pendingUrl", null);

        android.widget.EditText mockUrlBar = mock(android.widget.EditText.class);
        setField(browserActivity, "urlBar", mockUrlBar);

        android.content.Intent intent = mock(android.content.Intent.class);
        when(intent.getIntExtra("port", 0)).thenReturn(8080);
        when(intent.getStringExtra("protocol")).thenReturn("http");
        when(intent.getStringExtra("host")).thenReturn("example.com:80");

        invokeMethod(browserActivity, "onNewIntent", android.content.Intent.class, intent);

        // Port 80 is default for http, so targetHost should be just "example.com"
        assert "example.com".equals(getField(browserActivity.getClass(), "targetHost").get(browserActivity));
    }

    // --- Helper methods ---

    @SuppressWarnings("unchecked")
    private static <T> T allocate(Class<T> clazz) throws Exception {
        // Try default constructor first (works for most classes with returnDefaultValues=true)
        try {
            Constructor<T> ctor = clazz.getDeclaredConstructor();
            ctor.setAccessible(true);
            return ctor.newInstance();
        } catch (Exception e) {
            // Fallback: Unsafe allocation for classes without no-arg constructors
            var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
            unsafeField.setAccessible(true);
            Object unsafe = unsafeField.get(null);
            Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
            allocate.setAccessible(true);
            return (T) allocate.invoke(unsafe, clazz);
        }
    }

    private static void setField(Object target, String fieldName, Object value) throws Exception {
        Field field = null;
        Class<?> clazz = target.getClass();
        while (clazz != null) {
            try {
                field = clazz.getDeclaredField(fieldName);
                break;
            } catch (NoSuchFieldException e) {
                clazz = clazz.getSuperclass();
            }
        }
        if (field == null) throw new NoSuchFieldException(fieldName);
        field.setAccessible(true);
        field.set(target, value);
    }

    private static void invokeMethod(Object target, String methodName) throws Exception {
        Method method = findMethod(target.getClass(), methodName);
        method.setAccessible(true);
        method.invoke(target);
    }

    private static void invokeMethod(Object target, String methodName, Class<?> paramType, Object arg) throws Exception {
        Method method = findMethod(target.getClass(), methodName, paramType);
        method.setAccessible(true);
        method.invoke(target, arg);
    }

    private static Field getField(Class<?> clazz, String fieldName) throws Exception {
        Field field = null;
        Class<?> c = clazz;
        while (c != null) {
            try {
                field = c.getDeclaredField(fieldName);
                break;
            } catch (NoSuchFieldException e) {
                c = c.getSuperclass();
            }
        }
        if (field == null) throw new NoSuchFieldException(fieldName);
        field.setAccessible(true);
        return field;
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
