package com.clawbench.app;

import android.app.Application;
import android.content.Context;
import android.content.Intent;
import android.provider.Settings;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.Shadows;
import org.robolectric.annotation.Config;
import org.robolectric.shadows.ShadowSettings;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.contains;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

/**
 * Unit tests for the desktop floating-window overlay permission flow in MainActivity.
 *
 * Covers (Task 5 of the Android floating status window feature):
 * 1. requestOverlayPermission() launches Settings.ACTION_MANAGE_OVERLAY_PERMISSION
 *    when Settings.canDrawOverlays(context) is false (and does nothing when granted).
 * 2. The WebAppInterface JS bridge exposes setFloatingWindowEnabled(boolean) which
 *    persists the opt-in flag through BackgroundService.
 * 3. launchFromFloatingWindow(String[, String]) — the static entry point the
 *    floating capsule/panel uses to bring the main activity to the front with a
 *    session deep link (optionally carrying the owning project path).
 * 4. The activity consumes a session_id intent extra and hands it to the frontend
 *    via handleNotificationIntent → webView.evaluateJavascript
 *    (clawbench-open-session CustomEvent).
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class MainActivityOverlayPermissionTest {

    private Application appContext;
    private MainActivity activity;

    @Before
    public void setUp() throws Exception {
        appContext = RuntimeEnvironment.getApplication();
        // Fresh prefs per test so the floating-window toggle starts at its default.
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .edit().clear().commit();
        ShadowSettings.setCanDrawOverlays(false);

        // A real attached activity so static launchFromFloatingWindow() and
        // requestOverlayPermission() have a working context.
        activity = Robolectric.buildActivity(MainActivity.class).get();
        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);
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
    // requestOverlayPermission: launches the overlay settings screen
    // =====================================================

    @Test
    public void requestOverlayPermission_denied_launchesOverlaySettings() throws Exception {
        ShadowSettings.setCanDrawOverlays(false);

        invokeMethod(activity, "requestOverlayPermission");

        Intent next = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull("requestOverlayPermission must start an activity when overlay not granted", next);
        assertEquals("Must open the manage-overlay-permission settings screen",
                Settings.ACTION_MANAGE_OVERLAY_PERMISSION, next.getAction());
        assertEquals("Intent must target this package",
                "package:" + appContext.getPackageName(), next.getDataString());
    }

    @Test
    public void requestOverlayPermission_granted_noIntent() throws Exception {
        ShadowSettings.setCanDrawOverlays(true);

        invokeMethod(activity, "requestOverlayPermission");

        assertNull("No settings intent when overlay permission is already granted",
                Shadows.shadowOf(appContext).getNextStartedActivity());
    }

    // =====================================================
    // JS bridge: setFloatingWindowEnabled
    // =====================================================

    @Test
    public void webAppInterface_setFloatingWindowEnabled_persistsToggle() throws Exception {
        MainActivity.WebAppInterface bridge = allocate(MainActivity.WebAppInterface.class);
        setField(bridge, "activity", activity);

        bridge.setFloatingWindowEnabled(true);
        assertTrue("bridge on(true) should enable the floating window",
                BackgroundService.isFloatingWindowEnabled(appContext));

        bridge.setFloatingWindowEnabled(false);
        assertFalse("bridge off(false) should disable the floating window",
                BackgroundService.isFloatingWindowEnabled(appContext));
    }

    @Test
    public void webAppInterface_setFloatingWindowEnabled_hasJavascriptAnnotation() throws Exception {
        Method m = MainActivity.WebAppInterface.class.getDeclaredMethod("setFloatingWindowEnabled", boolean.class);
        assertNotNull("setFloatingWindowEnabled must be annotated @JavascriptInterface",
                m.getAnnotation(android.webkit.JavascriptInterface.class));
    }

    // =====================================================
    // launchFromFloatingWindow: static entry point for the capsule tap
    // =====================================================

    @Test
    public void launchFromFloatingWindow_withSessionId_buildsDeepLinkIntent() throws Exception {
        Method m = MainActivity.class.getDeclaredMethod("launchFromFloatingWindow", String.class);
        assertTrue("launchFromFloatingWindow must be static",
                java.lang.reflect.Modifier.isStatic(m.getModifiers()));

        MainActivity.launchFromFloatingWindow("s-float-1");

        Intent next = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull("launchFromFloatingWindow must start the activity", next);
        assertEquals("Intent must target MainActivity",
                new android.content.ComponentName(appContext, MainActivity.class), next.getComponent());
        assertEquals("session_id extra must be carried into the intent",
                "s-float-1", next.getStringExtra("session_id"));
        int flags = next.getFlags();
        assertTrue("must include FLAG_ACTIVITY_REORDER_TO_FRONT",
                (flags & Intent.FLAG_ACTIVITY_REORDER_TO_FRONT) != 0);
        assertTrue("must include FLAG_ACTIVITY_SINGLE_TOP",
                (flags & Intent.FLAG_ACTIVITY_SINGLE_TOP) != 0);
        assertTrue("must include FLAG_ACTIVITY_NEW_TASK",
                (flags & Intent.FLAG_ACTIVITY_NEW_TASK) != 0);
    }

    @Test
    public void launchFromFloatingWindow_nullSessionId_stillLaunches() {
        MainActivity.launchFromFloatingWindow(null);

        Intent next = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull("null sessionId must still bring the activity to the front", next);
        assertNull("no session_id extra when sessionId is null", next.getStringExtra("session_id"));
    }

    @Test
    public void launchFromFloatingWindow_emptySessionId_stillLaunches() {
        MainActivity.launchFromFloatingWindow("");

        Intent next = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull("empty sessionId must still bring the activity to the front", next);
        assertNull("no session_id extra when sessionId is empty", next.getStringExtra("session_id"));
    }

    @Test
    public void launchFromFloatingWindow_withProjectPath_carriesProjectPathExtra() throws Exception {
        // The two-arg overload exists for panel session rows that belong to a
        // different project than the current cookie.
        Method m = MainActivity.class.getDeclaredMethod("launchFromFloatingWindow", String.class, String.class);
        assertTrue("two-arg launchFromFloatingWindow must be static",
                java.lang.reflect.Modifier.isStatic(m.getModifiers()));

        MainActivity.launchFromFloatingWindow("s-float-x", "/projB");

        Intent next = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull("launchFromFloatingWindow must start the activity", next);
        assertEquals("session_id extra must be carried into the intent",
                "s-float-x", next.getStringExtra("session_id"));
        assertEquals("project_path extra must be carried into the intent",
                "/projB", next.getStringExtra("project_path"));
    }

    @Test
    public void launchFromFloatingWindow_emptyProjectPath_omitsExtra() {
        // Empty / null project path (capsule tap path) must not add a
        // project_path extra — an empty value would be treated as absent.
        MainActivity.launchFromFloatingWindow("s-float-y", "");

        Intent next = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull(next);
        assertEquals("s-float-y", next.getStringExtra("session_id"));
        assertNull("empty project_path must not be put into the intent",
                next.getStringExtra("project_path"));
    }

    // =====================================================
    // session_id deep link: routed through handleNotificationIntent → frontend JS
    // (clawbench-open-session CustomEvent)
    // =====================================================

    @Test
    public void handleNotificationIntent_withSessionId_dispatchesOpenSessionEvent() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(activity, "webView", mockWebView);
        Intent intent = new Intent().putExtra("session_id", "s-float-2");

        invokeMethod(activity, "handleNotificationIntent", Intent.class, intent);

        // A bare session_id intent (floating capsule deep link, no task_id /
        // event_type) must reach the frontend as a clawbench-open-session event
        // so it can navigate to the right chat session.
        verify(mockWebView).evaluateJavascript(contains("clawbench-open-session"), any());
        verify(mockWebView).evaluateJavascript(contains("s-float-2"), any());
        // The consumed extra must be removed to prevent re-dispatch.
        assertNull("session_id extra must be removed after consumption",
                intent.getStringExtra("session_id"));
    }

    @Test
    public void handleNotificationIntent_withProjectPath_dispatchesProjectPathInDetail() throws Exception {
        // Regression: a cross-project session deep link carries project_path in
        // the intent. handleNotificationIntent must forward it into the
        // clawbench-open-session detail so the frontend can switch the project
        // cookie before opening the session (otherwise a cross-project open 403s).
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(activity, "webView", mockWebView);
        Intent intent = new Intent()
                .putExtra("session_id", "s-cross")
                .putExtra("project_path", "/projB");

        invokeMethod(activity, "handleNotificationIntent", Intent.class, intent);

        verify(mockWebView).evaluateJavascript(contains("clawbench-open-session"), any());
        verify(mockWebView).evaluateJavascript(contains("s-cross"), any());
        verify(mockWebView).evaluateJavascript(contains("projectPath"), any());
        verify(mockWebView).evaluateJavascript(contains("/projB"), any());
        // The consumed extras must be removed to prevent re-dispatch.
        assertNull("session_id extra must be removed after consumption",
                intent.getStringExtra("session_id"));
        assertNull("project_path extra must be removed after consumption",
                intent.getStringExtra("project_path"));
    }

    @Test
    public void handleNotificationIntent_nullWebView_doesNotThrow() throws Exception {
        setField(activity, "webView", null);
        Intent intent = new Intent().putExtra("session_id", "s-float-3");

        invokeMethod(activity, "handleNotificationIntent", Intent.class, intent);
        // Should not throw.
    }

    @Test
    public void handleNotificationIntent_emptySessionId_doesNotDispatch() throws Exception {
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(activity, "webView", mockWebView);
        Intent intent = new Intent().putExtra("session_id", "");

        invokeMethod(activity, "handleNotificationIntent", Intent.class, intent);

        verify(mockWebView, org.mockito.Mockito.never()).evaluateJavascript(any(), any());
    }

    @Test
    public void handleNotificationIntent_taskIntent_dispatchesOpenTaskEvent() throws Exception {
        // Task intents carry task_id and are handled by the task branch — they must
        // dispatch clawbench-open-task, never clawbench-open-session.
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(activity, "webView", mockWebView);
        Intent intent = new Intent()
                .putExtra("session_id", "s-notif")
                .putExtra("task_id", "t-notif");

        invokeMethod(activity, "handleNotificationIntent", Intent.class, intent);

        verify(mockWebView).evaluateJavascript(contains("clawbench-open-task"), any());
        verify(mockWebView, org.mockito.Mockito.never())
                .evaluateJavascript(contains("clawbench-open-session"), any());
    }

    @Test
    public void onNewIntent_withSessionId_dispatchesToWebView() throws Exception {
        // MainActivity is singleTask; a floating-capsule deep link with
        // FLAG_ACTIVITY_REORDER_TO_FRONT lands in onNewIntent when the activity is
        // already alive. The session id must reach the frontend via the shared
        // handleNotificationIntent channel (clawbench-open-session), not be dropped.
        android.webkit.WebView mockWebView = mock(android.webkit.WebView.class);
        setField(activity, "webView", mockWebView);
        Intent intent = new Intent().putExtra("session_id", "s-onnewintent");

        invokeMethod(activity, "onNewIntent", Intent.class, intent);

        verify(mockWebView).evaluateJavascript(contains("clawbench-open-session"), any());
        verify(mockWebView).evaluateJavascript(contains("s-onnewintent"), any());
        // onNewIntent calls setIntent(intent), so handleNotificationIntent's
        // removeExtra targets the very intent delivered here and prevents
        // re-dispatch on the next onResume.
        assertNull("session_id extra must be removed from the onNewIntent intent",
                intent.getStringExtra("session_id"));
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

    private static Object invokeMethod(Object target, String methodName, Class<?> paramType, Object param) throws Exception {
        Method method = null;
        Class<?> clazz = target.getClass();
        while (clazz != null) {
            try {
                method = clazz.getDeclaredMethod(methodName, paramType);
                break;
            } catch (NoSuchMethodException e) {
                clazz = clazz.getSuperclass();
            }
        }
        if (method == null) throw new NoSuchMethodException(methodName);
        method.setAccessible(true);
        return method.invoke(target, param);
    }

    private static Object invokeMethod(Object target, String methodName) throws Exception {
        Method method = null;
        Class<?> clazz = target.getClass();
        while (clazz != null) {
            try {
                method = clazz.getDeclaredMethod(methodName);
                break;
            } catch (NoSuchMethodException e) {
                clazz = clazz.getSuperclass();
            }
        }
        if (method == null) throw new NoSuchMethodException(methodName);
        method.setAccessible(true);
        return method.invoke(target);
    }
}
