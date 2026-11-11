package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicBoolean;

import static org.mockito.Mockito.*;
import static org.junit.Assert.assertNull;

import android.content.SharedPreferences;
import android.view.View;
import android.widget.TextView;
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
    public void browserActivity_pauseWebView_callsWebViewPauseOnly() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(browserActivity, "webView", mockWebView);

        invokeMethod(browserActivity, "pauseWebView");

        verify(mockWebView).onPause();
        // pauseTimers() is intentionally NOT called — it globally freezes JS
        // timers and breaks the frontend's reconnect state management.
        verify(mockWebView, never()).pauseTimers();
    }

    @Test
    public void browserActivity_resumeWebView_callsWebViewResumeOnly() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(browserActivity, "webView", mockWebView);

        invokeMethod(browserActivity, "resumeWebView");

        verify(mockWebView).onResume();
        // No resumeTimers() since pauseTimers() is no longer called.
        verify(mockWebView, never()).resumeTimers();
    }

    // =====================================================
    // MainActivity.pauseWebView / resumeWebView tests
    // =====================================================

    @Test
    public void mainActivity_pauseWebView_callsWebViewPauseOnly() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(mainActivity, "webView", mockWebView);

        invokeMethod(mainActivity, "pauseWebView");

        verify(mockWebView).onPause();
        // pauseTimers() is intentionally NOT called — it globally freezes JS
        // timers and breaks the frontend's reconnect state management.
        verify(mockWebView, never()).pauseTimers();
    }

    @Test
    public void mainActivity_resumeWebView_callsWebViewResumeOnly() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(mainActivity, "webView", mockWebView);

        invokeMethod(mainActivity, "resumeWebView");

        verify(mockWebView).onResume();
        // No resumeTimers() since pauseTimers() is no longer called.
        verify(mockWebView, never()).resumeTimers();
    }

    // =====================================================
    // MainActivity.destroyWebView — the single-tab lock depends on it
    // =====================================================

    /**
     * A dropped WebView keeps its document (and the frontend's Web Lock) alive
     * for the life of the process, and this process outlives the Activity
     * because BackgroundService is stopWithTask="false". The next launch then
     * finds the lock held by a page nobody can see and shows the blocking
     * screen — issue #666. Clearing the field alone is NOT enough: only
     * destroy() releases the document, so that is what these tests pin.
     */
    @Test
    public void mainActivity_destroyWebView_detachesAndDestroysTheView() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        android.view.ViewGroup mockParent = mock(android.view.ViewGroup.class);
        when(mockWebView.getParent()).thenReturn(mockParent);
        setField(mainActivity, "webView", mockWebView);

        invokeMethod(mainActivity, "destroyWebView");

        verify(mockWebView).loadUrl("about:blank");
        verify(mockParent).removeView(mockWebView);
        verify(mockWebView).destroy();
        assertNull("the field must be cleared so no late JS lands on a dead view",
                getField(mainActivity.getClass(), "webView").get(mainActivity));
    }

    @Test
    public void mainActivity_destroyWebView_isSafeWhenAlreadyDestroyed() throws Exception {
        setField(mainActivity, "webView", null);

        // Must not throw — onDestroy can run after a renderer crash already
        // replaced or dropped the view.
        invokeMethod(mainActivity, "destroyWebView");

        assertNull(getField(mainActivity.getClass(), "webView").get(mainActivity));
    }

    /**
     * The lifecycle hook must actually call it — testing destroyWebView() alone
     * would still pass if onDestroy stopped calling it, which is the exact
     * regression that produced #666.
     */
    @Test
    public void mainActivity_onDestroy_releasesTheWebView() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(mainActivity, "webView", mockWebView);

        MainActivity spyActivity = spy(mainActivity);
        // super.onDestroy() walks the fragment/ lifecycle machinery, which an
        // Unsafe-allocated Activity has no real instances of.
        setField(spyActivity, "mFragments", mock(FragmentController.class));
        setField(spyActivity, "mFragmentLifecycleRegistry", mock(androidx.lifecycle.LifecycleRegistry.class));

        invokeMethod(spyActivity, "onDestroy");

        verify(mockWebView).destroy();
    }

    @Test
    public void mainActivity_destroyWebView_keepsGoingWhenTeardownStepsThrow() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        android.view.ViewGroup mockParent = mock(android.view.ViewGroup.class);
        when(mockWebView.getParent()).thenReturn(mockParent);
        doThrow(new IllegalStateException("view already detached")).when(mockWebView).loadUrl("about:blank");
        doThrow(new IllegalStateException("wrong thread")).when(mockParent).removeView(mockWebView);
        setField(mainActivity, "webView", mockWebView);

        // Each step is best-effort: a throwing early step must not skip destroy().
        invokeMethod(mainActivity, "destroyWebView");

        verify(mockWebView).destroy();
        assertNull(getField(mainActivity.getClass(), "webView").get(mainActivity));
    }

    // =====================================================
    // MainActivity foreground / resume JS bridge tests
    // =====================================================

    /**
     * The resume hook must be dispatched unconditionally, with no argument.
     *
     * It is deliberately separate from __setAppForeground(true): that reports a
     * foreground STATE and is a no-op in the frontend when it already believes
     * it is in the foreground — which is the normal Android case, because
     * onPause's JS call is dropped while the WebView is frozen. The resume hook
     * is what the frontend's current-session re-sync hangs off.
     */
    @Test
    public void mainActivity_notifyFrontendAppResume_callsResumeHookUnconditionally() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(mainActivity, "webView", mockWebView);
        setField(mainActivity, "webViewConnected", true);

        invokeMethod(mainActivity, "notifyFrontendAppResume");

        verify(mockWebView).evaluateJavascript(
                contains("__clawbenchAppResume()"), any());
    }

    @Test
    public void mainActivity_notifyFrontendAppForeground_callsStateHookWithArgument() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(mainActivity, "webView", mockWebView);
        setField(mainActivity, "webViewConnected", true);

        invokeMethod(mainActivity, "notifyFrontendAppForeground", boolean.class, true);

        verify(mockWebView).evaluateJavascript(
                contains("__setAppForeground(true)"), any());
    }

    @Test
    public void mainActivity_notifyFrontendAppResume_noopWhenWebViewNotConnected() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(mainActivity, "webView", mockWebView);
        setField(mainActivity, "webViewConnected", false);

        invokeMethod(mainActivity, "notifyFrontendAppResume");

        verify(mockWebView, never()).evaluateJavascript(anyString(), any());
    }

    /**
     * onResume() must actually dispatch the resume hook — testing the method
     * body alone would not catch the hook being dropped from the lifecycle.
     *
     * The real onResume() runs its full sequence on a spy: the siblings that
     * would touch the Android framework (resumeWebView, the native-WS stop,
     * intent handling) are stubbed out, while notifyFrontendAppResume is left
     * real so its WebView call is observable.
     */
    @Test
    public void mainActivity_onResume_dispatchesFrontendResumeHook() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(mainActivity, "webView", mockWebView);
        setField(mainActivity, "webViewConnected", true);

        MainActivity spyActivity = spy(mainActivity);
        // super.onResume() (FragmentActivity) dereferences mFragments.
        setField(spyActivity, "mFragments", mock(FragmentController.class));
        doNothing().when(spyActivity).resumeWebView();
        // stopNativeEventWs(this) calls context.startService(), which returns a
        // ComponentName — doReturn, not doNothing (that is void-only).
        doReturn(null).when(spyActivity).startService(any());
        doNothing().when(spyActivity).handleResumeIntent();

        invokeMethod(spyActivity, "onResume");

        verify(mockWebView).evaluateJavascript(contains("__clawbenchAppResume()"), any());
    }

    // =====================================================
    // BrowserActivity.onNewIntent tests
    // =====================================================

    /**
     * Initialize framework fields that onNewIntent() and waitForTunnelAndLoad()
     * need but are null with Unsafe.allocateInstance():
     * - FragmentController.mFragments, mOnNewIntentListeners (super.onNewIntent)
     * - tunnelWaitingOverlay, tunnelWaitingText (waitForTunnelAndLoad)
     * - tunnelWaitRunning AtomicBoolean (waitForTunnelAndLoad guard)
     * - sessionCreds (onNewIntent reads credentials)
     * - SharedPreferences stub (BrowserSessionCredentials.getAndClear)
     */
    private void initFragmentController() throws Exception {
        FragmentController mockController = mock(FragmentController.class);
        setField(browserActivity, "mFragments", mockController);
        // ComponentActivity.mOnNewIntentListeners — needed by super.onNewIntent()
        setField(browserActivity, "mOnNewIntentListeners", new CopyOnWriteArrayList<>());

        // waitForTunnelAndLoad() accesses these views
        View mockOverlay = mock(View.class);
        TextView mockOverlayText = mock(TextView.class);
        setField(browserActivity, "tunnelWaitingOverlay", mockOverlay);
        setField(browserActivity, "tunnelWaitingText", mockOverlayText);

        // AtomicBoolean guard for tunnel-wait thread
        setField(browserActivity, "tunnelWaitRunning", new AtomicBoolean(false));

        // onNewIntent calls BrowserSessionCredentials.getAndClear(this) which
        // calls getSharedPreferences(). Since the Activity is Unsafe-allocated,
        // context methods don't work. Use a spy to intercept the call.
        browserActivity = spy(browserActivity);
        SharedPreferences mockPrefs = mock(SharedPreferences.class);
        when(mockPrefs.getString(anyString(), anyString())).thenReturn("");
        doReturn(mockPrefs).when(browserActivity).getSharedPreferences(anyString(), anyInt());
    }

    @Test
    public void browserActivity_onNewIntent_loadsNewUrl() throws Exception {
        initFragmentController();
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        when(mockWebView.getVisibility()).thenReturn(View.INVISIBLE);
        setField(browserActivity, "webView", mockWebView);
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
        when(intent.getStringExtra("path")).thenReturn(null);

        invokeMethod(browserActivity, "onNewIntent", android.content.Intent.class, intent);

        // Verify pendingUrl and localPort were set correctly
        assert "http://localhost:8080/".equals(getField(browserActivity.getClass(), "pendingUrl").get(browserActivity));
        assert 8080 == getField(browserActivity.getClass(), "localPort").getInt(browserActivity);
    }

    @Test
    public void browserActivity_onNewIntent_withHost_setsUrlFields() throws Exception {
        initFragmentController();
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        when(mockWebView.getVisibility()).thenReturn(View.INVISIBLE);
        setField(browserActivity, "webView", mockWebView);
        setField(browserActivity, "pendingUrl", null);

        android.widget.EditText mockUrlBar = mock(android.widget.EditText.class);
        setField(browserActivity, "urlBar", mockUrlBar);

        android.content.Intent intent = mock(android.content.Intent.class);
        when(intent.getIntExtra("port", 0)).thenReturn(9090);
        when(intent.getStringExtra("protocol")).thenReturn("https");
        when(intent.getStringExtra("host")).thenReturn("192.168.1.1");
        when(intent.getStringExtra("path")).thenReturn(null);

        invokeMethod(browserActivity, "onNewIntent", android.content.Intent.class, intent);

        // Verify pendingUrl was set
        assert "https://localhost:9090/".equals(getField(browserActivity.getClass(), "pendingUrl").get(browserActivity));
        assert 9090 == getField(browserActivity.getClass(), "localPort").getInt(browserActivity);
    }

    @Test
    public void browserActivity_onNewIntent_withCustomPath() throws Exception {
        initFragmentController();
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        when(mockWebView.getVisibility()).thenReturn(View.INVISIBLE);
        setField(browserActivity, "webView", mockWebView);
        setField(browserActivity, "pendingUrl", null);

        android.widget.EditText mockUrlBar = mock(android.widget.EditText.class);
        setField(browserActivity, "urlBar", mockUrlBar);

        android.content.Intent intent = mock(android.content.Intent.class);
        when(intent.getIntExtra("port", 0)).thenReturn(8080);
        when(intent.getStringExtra("protocol")).thenReturn("http");
        when(intent.getStringExtra("host")).thenReturn("example.com:80");
        when(intent.getStringExtra("path")).thenReturn("/dashboard");

        invokeMethod(browserActivity, "onNewIntent", android.content.Intent.class, intent);

        // Custom path should appear in the URL
        assert "http://localhost:8080/dashboard".equals(getField(browserActivity.getClass(), "pendingUrl").get(browserActivity));
        assert 8080 == getField(browserActivity.getClass(), "localPort").getInt(browserActivity);
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
