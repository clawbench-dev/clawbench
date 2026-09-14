package com.clawbench.app;

import android.content.SharedPreferences;
import android.view.View;
import android.webkit.WebView;
import android.widget.TextView;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for the native splash fail-safe.
 *
 * Background (issue #449): on some devices the app stayed on "正在初始化应用…"
 * forever. The splash is normally dismissed by the JS app via
 * ClawBenchNative.dismissSplash(), but if JS init throws before reaching that
 * call, nothing else can remove the overlay — the connection-timeout guard is
 * already disarmed once the page loads successfully.
 *
 * The fail-safe arms a timer in onPageFinished() (remote page loaded) and
 * force-hides the splash when JS never checks in. These tests cover:
 *  - arming on successful remote page load
 *  - NOT arming on login page / errored loads
 *  - the timer body force-showing the WebView and dismissing the splash
 *  - dismissal and page failures cancelling the timer
 */
public class MainActivitySplashFailSafeTest {

    private static final String LOGIN_HTML_URL = "file:///android_asset/login.html";
    private static final String REMOTE_URL = "https://192.168.1.100:20000";

    private MainActivity activity;
    private WebView mockWebView;
    private SharedPreferences mockPrefs;
    private SharedPreferences.Editor mockEditor;
    private View mockSplashScreen;
    /** Runnable passed to ViewPropertyAnimator.withEndAction() — runs when the fade-out completes. */
    private final Runnable[] splashFadeEndAction = new Runnable[1];

    @Before
    public void setUp() throws Exception {
        activity = allocateAndSpy(MainActivity.class);

        stubGetString();

        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        mockWebView = mock(WebView.class);
        setField(activity, "webView", mockWebView);

        mockPrefs = mock(SharedPreferences.class);
        mockEditor = mock(SharedPreferences.Editor.class);
        when(mockPrefs.edit()).thenReturn(mockEditor);
        when(mockEditor.putString(any(), any())).thenReturn(mockEditor);
        setField(activity, "prefs", mockPrefs);

        // Splash must look "visible" for dismissSplash() to do any work.
        // animate() is a real View method returning a ViewPropertyAnimator; a bare
        // mock returns null and the chained .alpha()/.setDuration() calls NPE, so
        // stub the whole fluent chain. The real animator invokes withEndAction()'s
        // runnable on completion — capture it so tests can finish the fade.
        mockSplashScreen = mock(View.class);
        when(mockSplashScreen.getVisibility()).thenReturn(View.VISIBLE);
        android.view.ViewPropertyAnimator mockAnimator = mock(android.view.ViewPropertyAnimator.class);
        when(mockAnimator.alpha(org.mockito.ArgumentMatchers.anyFloat())).thenReturn(mockAnimator);
        when(mockAnimator.setDuration(anyLong())).thenReturn(mockAnimator);
        when(mockAnimator.withEndAction(any(Runnable.class))).thenAnswer(invocation -> {
            splashFadeEndAction[0] = invocation.getArgument(0);
            return mockAnimator;
        });
        when(mockSplashScreen.animate()).thenReturn(mockAnimator);
        setField(activity, "splashScreen", mockSplashScreen);
        setField(activity, "splashProgress", mock(TextView.class));
        setField(activity, "splashCancelButton", mock(View.class));

        setField(activity, "webViewConnected", false);
        setField(activity, "loadErrorPending", false);
        setField(activity, "connectionTimeoutRunnable", null);
        setField(activity, "splashFailSafeRunnable", null);
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
    // Arming: onPageFinished(remote page, success)
    // =====================================================

    @Test
    public void onPageFinished_remotePageSuccess_armsSplashFailSafe() throws Exception {
        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", WebView.class, mockWebView, String.class, REMOTE_URL);

        // A delayed runnable should be posted with the fail-safe delay.
        verify(mockWebView).postDelayed(any(Runnable.class), eq(15_000L));
        assertNotNull("fail-safe runnable should be stored", getField(activity, "splashFailSafeRunnable"));
    }

    @Test
    public void onPageFinished_loginPage_doesNotArmFailSafe() throws Exception {
        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", WebView.class, mockWebView, String.class, LOGIN_HTML_URL);

        verify(mockWebView, never()).postDelayed(any(Runnable.class), eq(15_000L));
        assertNull(getField(activity, "splashFailSafeRunnable"));
    }

    @Test
    public void onPageFinished_remotePageErrorPending_doesNotArmFailSafe() throws Exception {
        setField(activity, "loadErrorPending", true);

        Object client = createWebViewClient();
        invokeMethod(client, "onPageFinished", WebView.class, mockWebView, String.class, REMOTE_URL);

        verify(mockWebView, never()).postDelayed(any(Runnable.class), eq(15_000L));
        assertNull(getField(activity, "splashFailSafeRunnable"));
    }

    @Test
    public void startSplashFailSafe_cancelsExistingTimer() throws Exception {
        Runnable stale = mock(Runnable.class);
        setField(activity, "splashFailSafeRunnable", stale);

        invokeMethod(activity, "startSplashFailSafe");

        // The stale timer must be removed so it can't fire against a newer page.
        verify(mockWebView).removeCallbacks(stale);
        assertNotNull(getField(activity, "splashFailSafeRunnable"));
    }

    // =====================================================
    // Timer body: force-hide the splash
    // =====================================================

    @Test
    public void failSafeFires_showsWebViewAndHidesSplash() throws Exception {
        final Runnable[] captured = new Runnable[1];
        doAnswer(invocation -> {
            captured[0] = invocation.getArgument(0);
            return true;
        }).when(mockWebView).postDelayed(any(Runnable.class), anyLong());

        invokeMethod(activity, "startSplashFailSafe");
        assertNotNull(captured[0]);

        captured[0].run();

        // The WebView must become reachable — otherwise the user still sees nothing.
        verify(mockWebView).setVisibility(View.VISIBLE);
        // Splash fade-out is started, and completing it actually hides the overlay.
        assertNotNull("fade-out end action should be registered", splashFadeEndAction[0]);
        splashFadeEndAction[0].run();
        verify(mockSplashScreen).setVisibility(View.GONE);
    }

    @Test
    public void failSafeFires_splashAlreadyGone_doesNothing() throws Exception {
        // JS dismissed the splash but the timer somehow survived — the timer must
        // not fight the already-completed dismissal.
        when(mockSplashScreen.getVisibility()).thenReturn(View.GONE);

        final Runnable[] captured = new Runnable[1];
        doAnswer(invocation -> {
            captured[0] = invocation.getArgument(0);
            return true;
        }).when(mockWebView).postDelayed(any(Runnable.class), anyLong());

        invokeMethod(activity, "startSplashFailSafe");
        captured[0].run();

        verify(mockWebView, never()).setVisibility(View.VISIBLE);
    }

    // =====================================================
    // Cancellation
    // =====================================================

    @Test
    public void dismissSplash_cancelsFailSafe() throws Exception {
        Runnable pending = mock(Runnable.class);
        setField(activity, "splashFailSafeRunnable", pending);

        invokeMethod(activity, "dismissSplash");

        verify(mockWebView).removeCallbacks(pending);
        assertNull(getField(activity, "splashFailSafeRunnable"));
    }

    @Test
    public void showLoginPage_cancelsFailSafe() throws Exception {
        Runnable pending = mock(Runnable.class);
        setField(activity, "splashFailSafeRunnable", pending);

        invokeMethod(activity, "showLoginPage", String.class, "error");

        verify(mockWebView).removeCallbacks(pending);
        assertNull(getField(activity, "splashFailSafeRunnable"));
    }

    @Test
    public void cancelSplashFailSafe_nullTimer_noException() throws Exception {
        setField(activity, "splashFailSafeRunnable", null);

        invokeMethod(activity, "cancelSplashFailSafe");

        verify(mockWebView, never()).removeCallbacks(any(Runnable.class));
    }

    @Test
    public void onRenderProcessGone_cancelsFailSafe() throws Exception {
        Runnable pending = mock(Runnable.class);
        setField(activity, "splashFailSafeRunnable", pending);

        Object client = createWebViewClient();
        Method method = findMethod(client.getClass(), "onRenderProcessGone",
                WebView.class, android.webkit.RenderProcessGoneDetail.class);
        method.setAccessible(true);

        android.webkit.RenderProcessGoneDetail detail = mock(android.webkit.RenderProcessGoneDetail.class);
        when(detail.didCrash()).thenReturn(true);

        try {
            method.invoke(client, mockWebView, detail);
        } catch (java.lang.reflect.InvocationTargetException e) {
            // Expected: runOnUiThread needs a real Activity on an Unsafe-allocated one.
        }

        verify(mockWebView).removeCallbacks(pending);
        assertNull(getField(activity, "splashFailSafeRunnable"));
    }

    @Test
    public void splashFailSafeConstant_is15Seconds() throws Exception {
        Field field = MainActivity.class.getDeclaredField("SPLASH_FAILSAFE_MS");
        field.setAccessible(true);
        assertEquals(15_000, field.getInt(null));
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

    @SuppressWarnings("unchecked")
    private static <T> T allocateAndSpy(Class<T> clazz) throws Exception {
        return org.mockito.Mockito.spy(allocate(clazz));
    }

    private void stubGetString() {
        doAnswer(inv -> {
            int resId = inv.getArgument(0);
            String v = MainActivityZhText.get(resId);
            return v != null ? v : "";
        }).when(activity).getString(org.mockito.ArgumentMatchers.anyInt());
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
        Object[] args = new Object[paramTypes.length];
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

    private Object createWebViewClient() throws Exception {
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
