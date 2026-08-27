package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.lang.reflect.Field;
import java.lang.reflect.Method;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

/**
 * Unit tests wiring the FloatingStatusController into BackgroundService.
 *
 * Verifies:
 * - static floating-window enable getter/setter backed by SharedPreferences
 *   ("floating_window_enabled", default false — opt-in feature)
 * - the floatingController instance field exists and is non-static
 * - the controller is created in onCreate() only when the feature is enabled
 * - the controller is destroyed in onDestroy()
 * - onMessage dispatches session_update/task_update events to the controller
 *   (after ack + dedup, so replays never re-render it)
 * - startNativeEventWs/stopNativeEventWs notify the controller of app
 *   foreground state changes
 *
 * Uses Robolectric so the FloatingStatusController constructor (which needs a
 * real Looper, ViewConfiguration and WindowManager) can run inside onCreate.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class BackgroundServiceFloatingTest {

    private BackgroundService service;
    private Context appContext;

    @Before
    public void setUp() throws Exception {
        // Build the service with a real (Robolectric) context attached, but
        // WITHOUT calling onCreate (that is exercised explicitly per test).
        service = Robolectric.buildService(BackgroundService.class).get();

        appContext = RuntimeEnvironment.getApplication();
        // Fresh prefs for each test
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .edit().clear().commit();

        setStaticField("instance", service);
        setStaticField("isRunning", true);
        setStaticField("nativeWsNeeded", false);
        setStaticField("lastError", null);
    }

    @After
    public void tearDown() throws Exception {
        try {
            setStaticField("instance", null);
            setStaticField("isRunning", false);
            setStaticField("nativeWsNeeded", false);
            setStaticField("lastError", null);
        } catch (Exception ignored) {}
    }

    // =====================================================
    // Static enable getter/setter (floating_window_enabled)
    // =====================================================

    @Test
    public void isFloatingWindowEnabled_existsAndIsStaticPublic() throws Exception {
        Method m = BackgroundService.class.getDeclaredMethod("isFloatingWindowEnabled", Context.class);
        assertTrue("isFloatingWindowEnabled should be static",
                java.lang.reflect.Modifier.isStatic(m.getModifiers()));
        assertTrue("isFloatingWindowEnabled should be public",
                java.lang.reflect.Modifier.isPublic(m.getModifiers()));
        assertEquals(boolean.class, m.getReturnType());
    }

    @Test
    public void isFloatingWindowEnabled_defaultsToFalse() {
        assertFalse("Floating window should be disabled by default (opt-in)",
                BackgroundService.isFloatingWindowEnabled(appContext));
    }

    @Test
    public void setFloatingWindowEnabled_persistsToPrefs() {
        BackgroundService.setFloatingWindowEnabled(appContext, true);
        assertTrue("set(true) should persist floating_window_enabled=true",
                BackgroundService.isFloatingWindowEnabled(appContext));

        BackgroundService.setFloatingWindowEnabled(appContext, false);
        assertFalse("set(false) should persist floating_window_enabled=false",
                BackgroundService.isFloatingWindowEnabled(appContext));
    }

    @Test
    public void setFloatingWindowEnabled_true_whileRunning_createsController() throws Exception {
        // Service is "running" (instance + isRunning set in setUp).
        setField(service, "floatingController", null);

        BackgroundService.setFloatingWindowEnabled(appContext, true);

        assertNotNull("toggle on while running should create the controller immediately",
                getField(service, "floatingController"));
    }

    @Test
    public void setFloatingWindowEnabled_false_whileRunning_destroysController() throws Exception {
        FloatingStatusController controller = mock(FloatingStatusController.class);
        setField(service, "floatingController", controller);

        BackgroundService.setFloatingWindowEnabled(appContext, false);

        verify(controller).destroy();
        assertNull("toggle off while running should destroy and null the controller",
                getField(service, "floatingController"));
    }

    @Test
    public void setFloatingWindowEnabled_true_whenServiceNotRunning_doesNotCreateController() throws Exception {
        setStaticField("instance", null);
        setStaticField("isRunning", false);

        BackgroundService.setFloatingWindowEnabled(appContext, true);

        assertNull("toggle on with no running service should only persist prefs",
                getField(service, "floatingController"));
    }

    // =====================================================
    // floatingController field
    // =====================================================

    @Test
    public void floatingController_fieldExistsAndIsNonStatic() throws Exception {
        Field f = BackgroundService.class.getDeclaredField("floatingController");
        assertFalse("floatingController must be a per-instance field, not static",
                java.lang.reflect.Modifier.isStatic(f.getModifiers()));
        assertEquals("floatingController should be typed FloatingStatusController",
                FloatingStatusController.class, f.getType());
    }

    // =====================================================
    // onCreate / onDestroy lifecycle
    // =====================================================

    @Test
    public void onCreate_enabled_createsController() throws Exception {
        BackgroundService.setFloatingWindowEnabled(appContext, true);
        setStaticField("instance", null);

        invokeMethod(service, "onCreate");

        Object controller = getField(service, "floatingController");
        assertNotNull("onCreate should create floatingController when enabled", controller);
        assertTrue(controller instanceof FloatingStatusController);
    }

    @Test
    public void onCreate_disabled_doesNotCreateController() throws Exception {
        BackgroundService.setFloatingWindowEnabled(appContext, false);
        setStaticField("instance", null);

        invokeMethod(service, "onCreate");

        assertNull("onCreate should NOT create floatingController when disabled",
                getField(service, "floatingController"));
    }

    @Test
    public void syncFloatingController_enabledWithForegroundActivity_createsHiddenController() throws Exception {
        // The main activity is foreground: the controller must be created in a
        // state that won't show the capsule (appForeground=true, set via the
        // background-only creation path in syncFloatingController).
        java.lang.reflect.Field fg = MainActivity.class.getDeclaredField("isForeground");
        fg.setAccessible(true);
        fg.set(null, true);
        try {
            setField(service, "floatingController", null);
            BackgroundService.setFloatingWindowEnabled(appContext, true);

            Object controller = getField(service, "floatingController");
            assertNotNull("syncFloatingController should create the controller when enabled",
                    controller);
            assertFalse("controller must not be window-showing while activity is foreground",
                    ((FloatingStatusController) controller).isWindowShowing());
        } finally {
            fg.set(null, false);
        }
    }

    @Test
    public void onDestroy_nullController_noThrowAndStaysNull() throws Exception {
        setField(service, "floatingController", null);

        invokeMethod(service, "onDestroy");

        assertNull("onDestroy with null controller should stay null",
                getField(service, "floatingController"));
    }

    @Test
    public void onDestroy_withController_destroysAndNullsIt() throws Exception {
        FloatingStatusController controller = mock(FloatingStatusController.class);
        setField(service, "floatingController", controller);

        invokeMethod(service, "onDestroy");

        verify(controller).destroy();
        assertNull("onDestroy should null floatingController after destroy()",
                getField(service, "floatingController"));
    }

    // =====================================================
    // onMessage dispatch to controller
    // =====================================================

    @Test
    public void onMessage_sessionUpdate_dispatchesToController() throws Exception {
        FloatingStatusController controller = mock(FloatingStatusController.class);
        setField(service, "floatingController", controller);

        org.json.JSONObject data = new org.json.JSONObject();
        data.put("session_id", "s-fs-1");
        data.put("status", "running");
        data.put("session_title", "T");
        String msg = "{\"type\":\"event\",\"id\":\"e1\",\"event\":\"session_update\",\"data\":" + data + "}";

        invokeListenerMessage(msg);

        verify(controller).handleEvent(eq("session_update"), any(org.json.JSONObject.class));
    }

    @Test
    public void onMessage_taskUpdate_dispatchesToController() throws Exception {
        FloatingStatusController controller = mock(FloatingStatusController.class);
        setField(service, "floatingController", controller);

        org.json.JSONObject data = new org.json.JSONObject();
        data.put("session_id", "s-fs-2");
        data.put("status", "running");
        String msg = "{\"type\":\"event\",\"id\":\"e2\",\"event\":\"task_update\",\"data\":" + data + "}";

        invokeListenerMessage(msg);

        verify(controller).handleEvent(eq("task_update"), any(org.json.JSONObject.class));
    }

    @Test
    public void onMessage_otherEventType_notDispatched() throws Exception {
        FloatingStatusController controller = mock(FloatingStatusController.class);
        setField(service, "floatingController", controller);

        org.json.JSONObject data = new org.json.JSONObject();
        data.put("status", "completed");
        String msg = "{\"type\":\"event\",\"id\":\"e3\",\"event\":\"terminal_update\",\"data\":" + data + "}";

        invokeListenerMessage(msg);

        verify(controller, never()).handleEvent(anyString(), any(org.json.JSONObject.class));
    }

    @Test
    public void onMessage_duplicateEvent_notDispatched() throws Exception {
        FloatingStatusController controller = mock(FloatingStatusController.class);
        setField(service, "floatingController", controller);

        org.json.JSONObject data = new org.json.JSONObject();
        data.put("status", "running");
        String msg = "{\"type\":\"event\",\"id\":\"dup-1\",\"event\":\"session_update\",\"data\":" + data + "}";

        // First delivery marks the event as processed.
        invokeListenerMessage(msg);
        verify(controller).handleEvent(eq("session_update"), any(org.json.JSONObject.class));

        // Re-delivery of the same event id must be dropped before dispatch.
        invokeListenerMessage(msg);

        int calls = org.mockito.Mockito.mockingDetails(controller).getInvocations()
                .stream()
                .filter(i -> i.getMethod().getName().equals("handleEvent"))
                .mapToInt(i -> 1).sum();
        assertEquals("duplicate event must not be re-dispatched", 1, calls);
    }

    @Test
    public void onMessage_sessionUpdate_updatesFloatingSessionId() throws Exception {
        setField(service, "floatingSessionId", "");

        org.json.JSONObject data = new org.json.JSONObject();
        data.put("session_id", "s-track");
        data.put("status", "running");
        String msg = "{\"type\":\"event\",\"id\":\"e4\",\"event\":\"session_update\",\"data\":" + data + "}";

        invokeListenerMessage(msg);

        assertEquals("s-track", getField(service, "floatingSessionId"));
    }

    // =====================================================
    // Foreground/background linkage
    // =====================================================

    @Test
    public void startNativeEventWs_marksAppBackground() throws Exception {
        FloatingStatusController controller = mock(FloatingStatusController.class);
        setField(service, "floatingController", controller);
        setField(service, "nativeWsActive", false);

        invokeMethod(service, "startNativeEventWs", "http://localhost:20000");

        verify(controller).setAppForeground(false);
    }

    @Test
    public void startNativeEventWs_nullController_noThrow() throws Exception {
        setField(service, "floatingController", null);
        setField(service, "nativeWsActive", false);

        invokeMethod(service, "startNativeEventWs", "http://localhost:20000");
        // No exception thrown when controller is null.
    }

    @Test
    public void stopNativeEventWs_marksAppForeground() throws Exception {
        FloatingStatusController controller = mock(FloatingStatusController.class);
        setField(service, "floatingController", controller);

        invokeMethod(service, "stopNativeEventWs");

        verify(controller).setAppForeground(true);
    }

    @Test
    public void stopNativeEventWs_nullController_noThrow() throws Exception {
        setField(service, "floatingController", null);

        invokeMethod(service, "stopNativeEventWs");
        // No exception thrown when controller is null.
    }

    // =====================================================
    // Internal listener invocation + helpers
    // =====================================================

    /** Create the private inner NativeEventListener and invoke its onMessage. */
    private void invokeListenerMessage(String text) throws Exception {
        Class<?> listenerClazz = Class.forName("com.clawbench.app.BackgroundService$NativeEventListener");
        java.lang.reflect.Constructor<?> ctor =
                listenerClazz.getDeclaredConstructor(BackgroundService.class);
        ctor.setAccessible(true);
        Object listener = ctor.newInstance(service);

        okhttp3.WebSocket ws = mock(okhttp3.WebSocket.class);
        Method m = listenerClazz.getDeclaredMethod("onMessage", okhttp3.WebSocket.class, String.class);
        m.setAccessible(true);
        m.invoke(listener, ws, text);
    }

    private void invokeMethod(Object target, String name, Object... args) throws Exception {
        Class<?>[] types = new Class<?>[args.length];
        for (int i = 0; i < args.length; i++) {
            if (args[i] instanceof Integer) types[i] = int.class;
            else if (args[i] instanceof Boolean) types[i] = boolean.class;
            else types[i] = args[i].getClass();
        }
        Method m = BackgroundService.class.getDeclaredMethod(name, types);
        m.setAccessible(true);
        m.invoke(target, args);
    }

    private void setStaticField(String name, Object value) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(null, value);
    }

    private Object getField(Object target, String name) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        return f.get(target);
    }

    private void setField(Object target, String name, Object value) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(target, value);
    }
}
