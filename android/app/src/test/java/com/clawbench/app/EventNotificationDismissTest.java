package com.clawbench.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.content.Context;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.Shadows;
import org.robolectric.annotation.Config;
import org.robolectric.shadows.ShadowNotificationManager;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import okhttp3.WebSocket;

import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.mockito.Mockito.mock;

/**
 * Regression tests for dismissing an event notification when its subject is
 * read.
 *
 * The reported defect: the notification only auto-cancels on tap, so opening
 * the app and reading the message left a stale notification in the shade that
 * re-dispatched the deep link when tapped later. These tests drive the REAL
 * NativeEventListener.onMessage path (not just the id helper) and assert the
 * notification is actually removed — the helper alone cannot prove a caller
 * wired it up.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class EventNotificationDismissTest {

    private static final String PREFS = "clawbench_prefs";
    private static final String CHANNEL = "clawbench_events";

    private BackgroundService service;
    private Context appContext;
    private NotificationManager nm;

    @Before
    public void setUp() throws Exception {
        service = Robolectric.buildService(BackgroundService.class).get();
        appContext = RuntimeEnvironment.getApplication();
        appContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().clear().commit();

        nm = (NotificationManager) appContext.getSystemService(Context.NOTIFICATION_SERVICE);
        nm.createNotificationChannel(new NotificationChannel(CHANNEL, "events",
                NotificationManager.IMPORTANCE_HIGH));

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
        } catch (Exception ignored) {
        }
        appContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().clear().commit();
    }

    /** Post a bare notification under a given id so the cancel has something to remove. */
    private void postNotification(int id) {
        Notification n = new Notification.Builder(appContext, CHANNEL)
                .setSmallIcon(android.R.drawable.ic_dialog_info)
                .setContentTitle("t")
                .build();
        nm.notify(id, n);
    }

    private ShadowNotificationManager shadow() {
        return Shadows.shadowOf(nm);
    }

    @Test
    public void sessionReadCancelsTheSessionNotification() throws Exception {
        String sid = "s-read-1";
        int id = EventNotificationIds.forSession(sid);
        // Pin the concrete id so this round-trip test also anchors the value
        // the POST path writes (not just "cancel matches post").
        org.junit.Assert.assertEquals(599, id);
        postNotification(id);
        assertNotNull("precondition: the notification must be in the shade",
                shadow().getNotification(id));

        invokeOnMessage("{\"type\":\"event\",\"id\":\"evt-read-1\",\"event\":\"session_update\","
                + "\"data\":{\"session_id\":\"" + sid + "\",\"status\":\"read\"}}");

        assertNull("a session marked read must have its notification cancelled",
                shadow().getNotification(id));
    }

    @Test
    public void taskReadCancelsTheTaskNotification() throws Exception {
        String tid = "7";
        int id = EventNotificationIds.forTask(tid);
        postNotification(id);
        assertNotNull("precondition: the notification must be in the shade",
                shadow().getNotification(id));

        invokeOnMessage("{\"type\":\"event\",\"id\":\"evt-read-2\",\"event\":\"task_update\","
                + "\"data\":{\"task_id\":\"" + tid + "\",\"status\":\"read\"}}");

        assertNull("a task marked read must have its notification cancelled",
                shadow().getNotification(id));
    }

    /** A read for one subject must not cancel another subject's notification. */
    @Test
    public void readDoesNotCancelOtherNotifications() throws Exception {
        String other = "s-other";
        int otherId = EventNotificationIds.forSession(other);
        postNotification(otherId);

        invokeOnMessage("{\"type\":\"event\",\"id\":\"evt-read-3\",\"event\":\"session_update\","
                + "\"data\":{\"session_id\":\"s-target\",\"status\":\"read\"}}");

        assertNotNull("a different session's notification must survive",
                shadow().getNotification(otherId));
    }

    /** A non-read status must not trigger a cancel. */
    @Test
    public void completionDoesNotCancel() throws Exception {
        String sid = "s-complete";
        int id = EventNotificationIds.forSession(sid);
        postNotification(id);

        invokeOnMessage("{\"type\":\"event\",\"id\":\"evt-complete\",\"event\":\"session_update\","
                + "\"data\":{\"session_id\":\"" + sid + "\",\"status\":\"completed\"}}");

        assertNotNull("a completion must not dismiss the notification (it was just posted)",
                shadow().getNotification(id));
    }

    /** The bridge method must cancel through the same formula. */
    @Test
    public void bridgeDismissCancelsNotification() throws Exception {
        String sid = "s-bridge";
        int id = EventNotificationIds.forSession(sid);
        postNotification(id);

        Class<?> waiClass = Class.forName("com.clawbench.app.MainActivity$WebAppInterface");
        Constructor<?> ctor = waiClass.getDeclaredConstructor(MainActivity.class);
        ctor.setAccessible(true);
        // WebAppInterface only needs its `activity` field for the delegation.
        // Unsafe allocation leaves ContextWrapper's base null, so attach a real
        // application context — otherwise getSystemService() returns null and
        // the cancel is a silent no-op.
        MainActivity activity = allocate(MainActivity.class);
        Method attach = android.content.ContextWrapper.class
                .getDeclaredMethod("attachBaseContext", android.content.Context.class);
        attach.setAccessible(true);
        attach.invoke(activity, appContext);
        Field activityField = MainActivity.class.getDeclaredField("instance");
        activityField.setAccessible(true);
        activityField.set(null, activity);
        Object bridge = ctor.newInstance(activity);

        Method m = waiClass.getDeclaredMethod("dismissEventNotification", String.class, String.class);
        m.setAccessible(true);
        m.invoke(bridge, "", sid);

        assertNull("the bridge must cancel via the shared id formula",
                shadow().getNotification(id));
    }

    // ── Helpers ──────────────────────────────────────────────────────────────

    private void invokeOnMessage(String text) throws Exception {
        Class<?> listenerClazz = Class.forName("com.clawbench.app.BackgroundService$NativeEventListener");
        Constructor<?> ctor = listenerClazz.getDeclaredConstructor(BackgroundService.class);
        ctor.setAccessible(true);
        Object listener = ctor.newInstance(service);

        WebSocket ws = mock(WebSocket.class);
        Method m = listenerClazz.getDeclaredMethod("onMessage", WebSocket.class, String.class);
        m.setAccessible(true);
        m.invoke(listener, ws, text);
    }

    @SuppressWarnings("unchecked")
    private static <T> T allocate(Class<T> clazz) throws Exception {
        var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        Object unsafe = unsafeField.get(null);
        var allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
        allocate.setAccessible(true);
        return (T) allocate.invoke(unsafe, clazz);
    }

    private void setStaticField(String name, Object value) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(null, value);
    }
}
