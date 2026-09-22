package com.clawbench.app;

import android.app.Notification;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;

import androidx.core.app.NotificationCompat;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.lang.reflect.Field;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;

import static org.junit.Assert.*;

/**
 * Regression tests for the "stale forwarded ports resurrect a dead service"
 * defect.
 *
 * Symptom: a permanent foreground notification reading "后台服务即将停止"
 * appears even though the user enabled no floating window, no Live Updates chip
 * and no native push (e.g. push_mode = dingtalk). The service is running but
 * doing nothing and never stops.
 *
 * Root cause chain (three independent defects, one test class each):
 *  1. BackgroundService.start() used an ACTION-LESS intent. onCreate() had
 *     already repopulated forwardedPorts from SharedPreferences, so the
 *     "stop when idle" check saw a non-empty port map and kept the service
 *     alive — but no onStartCommand branch ran, so ensureConnection() was never
 *     called and the SSH session never came up. The service sat forever in the
 *     state that renders as "后台服务即将停止".
 *  2. stopBackgroundService() (invoked when the server reports zero enabled
 *     ports) cleared only the Activity's cache map. The Service's own map and
 *     the persisted "forwarded_ports" list survived, so the next cold start
 *     restored them again.
 *  3. buildCurrentNotification() counted ports only on a LIVE SSH session, so
 *     a just-restored-but-not-yet-connected port set rendered as "stopping".
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class BackgroundServiceStalePortsTest {

    private BackgroundService service;
    private Context appContext;

    @Before
    public void setUp() throws Exception {
        service = Robolectric.buildService(BackgroundService.class).get();
        appContext = RuntimeEnvironment.getApplication();
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .edit().clear().commit();

        setStaticField("instance", service);
        setStaticField("isRunning", false);
        setStaticField("nativeWsNeeded", false);
        setInstanceField("nativeWsActive", false);
        setStaticField("lastError", null);
    }

    @After
    public void tearDown() throws Exception {
        try {
            setStaticField("instance", null);
            setStaticField("isRunning", false);
            setStaticField("nativeWsNeeded", false);
            setInstanceField("nativeWsActive", false);
            setStaticField("lastError", null);
        } catch (Exception ignored) {}
    }

    // =====================================================
    // Defect 1: start() must ask for a restore
    // =====================================================

    /**
     * The action-less intent was the trigger. Asserting the action (not merely
     * that a service was started) is what makes this a real regression guard:
     * an action-less start still "starts the service", it just never connects.
     */
    @Test
    public void start_sendsRestorePortsAction() {
        BackgroundService.start(appContext);

        Intent started = org.robolectric.Shadows.shadowOf(
                (android.app.Application) appContext).getNextStartedService();
        assertNotNull("start() must launch the service", started);
        assertEquals("start() must carry RESTORE_PORTS so onStartCommand "
                        + "re-establishes the tunnel instead of idling forever",
                "RESTORE_PORTS", started.getAction());
    }

    /** RESTORE_PORTS is only reachable if onStartCommand actually honours it. */
    @Test
    public void start_whileRunning_doesNotStartAgain() throws Exception {
        setStaticField("isRunning", true);

        BackgroundService.start(appContext);

        Intent started = org.robolectric.Shadows.shadowOf(
                (android.app.Application) appContext).getNextStartedService();
        assertNull("start() must be a no-op while the service is already running",
                started);
    }

    // =====================================================
    // Defect 2: forgetting ports must clear memory AND prefs
    // =====================================================

    @Test
    public void forgetForwardedPorts_clearsMemoryAndPrefs() throws Exception {
        seedForwardedPortsPrefs("8080:20000", "3000:3000");
        Map inMemory = forwardedPortsOf(service);
        inMemory.put(8080, newPortInfo(20000, ""));
        inMemory.put(3000, newPortInfo(3000, ""));

        BackgroundService.forgetForwardedPorts(appContext);

        assertTrue("in-memory forwardedPorts must be cleared", inMemory.isEmpty());
        Set<String> persisted = appContext
                .getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .getStringSet("forwarded_ports", null);
        assertTrue("persisted forwarded_ports must be removed so a cold start "
                        + "cannot resurrect a service that can never connect",
                persisted == null || persisted.isEmpty());
    }

    /**
     * The service may be gone while its prefs survive (e.g. the Activity calls
     * this after the process was restarted). Must not NPE.
     */
    @Test
    public void forgetForwardedPorts_serviceNotRunning_stillClearsPrefs() throws Exception {
        setStaticField("instance", null);
        seedForwardedPortsPrefs("8080:20000");

        BackgroundService.forgetForwardedPorts(appContext);

        Set<String> persisted = appContext
                .getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .getStringSet("forwarded_ports", null);
        assertTrue("prefs must be cleared even with no live service instance",
                persisted == null || persisted.isEmpty());
    }

    /**
     * After forgetting, the cold-start restore path must find nothing — this is
     * the end-to-end shape of the fix (what restoreBackgroundServiceIfNeeded()
     * reads).
     */
    @Test
    public void forgetForwardedPorts_preventsColdStartRestore() {
        seedForwardedPortsPrefs("8080:20000");
        Set<String> beforeRestore = appContext
                .getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .getStringSet("forwarded_ports", null);
        assertNotNull("precondition: a stale port list exists", beforeRestore);

        BackgroundService.forgetForwardedPorts(appContext);

        Set<String> savedPorts = appContext
                .getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .getStringSet("forwarded_ports", null);
        assertTrue("restoreBackgroundServiceIfNeeded() must not see saved ports",
                savedPorts == null || savedPorts.isEmpty());
    }

    // =====================================================
    // Defect 3: restored ports must not render as "stopping"
    // =====================================================

    /**
     * The user-visible bug. Ports restored from prefs, SSH not yet connected:
     * the notification must describe the pending reconnect, never the "stopping"
     * fallback, and must NOT claim the ports are live (nothing is forwarding).
     */
    @Test
    public void buildCurrentNotification_restoredPortsNoSession_reportsReconnectingNotStopping()
            throws Exception {
        Map inMemory = forwardedPortsOf(service);
        inMemory.put(8080, newPortInfo(20000, ""));
        inMemory.put(3000, newPortInfo(3000, ""));

        Notification n = buildCurrentNotification(service);
        CharSequence text = NotificationCompat.getExtras(n)
                .getCharSequence(Notification.EXTRA_TEXT);

        assertNotNull(text);
        assertNotEquals("restored ports are real work — the notification must not "
                        + "claim the service is stopping",
                appContext.getString(R.string.notif_service_stopping),
                text.toString());
        assertEquals("must describe the pending reconnect",
                appContext.getString(R.string.ssh_notification_reconnecting),
                text.toString());
        assertNotEquals("must not claim ports are live before the session connects",
                appContext.getString(R.string.notif_ports_mapped, 2),
                text.toString());
    }

    /**
     * Guard the other direction: with genuinely nothing to do the fallback must
     * still be reachable, otherwise the fix would mask real idleness.
     */
    @Test
    public void buildCurrentNotification_noPortsNoWs_stillReportsStopping() throws Exception {
        Notification n = buildCurrentNotification(service);
        CharSequence text = NotificationCompat.getExtras(n)
                .getCharSequence(Notification.EXTRA_TEXT);

        assertEquals("a truly idle service must still be able to say it is stopping",
                appContext.getString(R.string.notif_service_stopping), text.toString());
    }

    /** Native WS active with no ports keeps the "listening" wording, unchanged. */
    @Test
    public void buildCurrentNotification_nativeWsActive_reportsListening() throws Exception {
        setInstanceField("nativeWsActive", true);

        Notification n = buildCurrentNotification(service);
        CharSequence text = NotificationCompat.getExtras(n)
                .getCharSequence(Notification.EXTRA_TEXT);

        assertEquals(appContext.getString(R.string.notif_listening), text.toString());
    }

    // =====================================================
    // Defect 4: a failed cold-start restore must arm the retry loop
    // =====================================================

    /**
     * restoreAndReconnect() claimed "Connection monitor will handle reconnect",
     * but startConnectionMonitor() is otherwise only reached at the END of a
     * SUCCESSFUL ensureConnection(). On a first-attempt failure nothing retried,
     * so the service sat with a port list it could never forward. Assert the
     * monitor actually starts — a pure "did not throw" assertion would pass even
     * with the bug present.
     */
    @Test
    public void restoreAndReconnect_connectionFailure_armsConnectionMonitor() throws Exception {
        setInstanceField("screenOn", true);
        Map inMemory = forwardedPortsOf(service);
        inMemory.put(8080, newPortInfo(20000, ""));
        // No server URL configured -> ensureConnection() throws.

        assertFalse("precondition: monitor must not already be running",
                monitorActiveOf(service));

        java.lang.reflect.Method m = BackgroundService.class
                .getDeclaredMethod("restoreAndReconnect");
        m.setAccessible(true);
        m.invoke(service);

        assertTrue("a failed restore must arm the monitor, otherwise no retry "
                        + "ever runs and the ports stay unforwarded forever",
                monitorActiveOf(service));

        // Clean up the monitor thread this test started.
        java.lang.reflect.Method stop = BackgroundService.class
                .getDeclaredMethod("stopConnectionMonitor");
        stop.setAccessible(true);
        stop.invoke(service);
    }

    /** The successful path must keep arming the monitor too (no regression). */
    @Test
    public void restoreAndReconnect_noPorts_doesNotArmMonitor() throws Exception {
        setInstanceField("screenOn", true);

        java.lang.reflect.Method m = BackgroundService.class
                .getDeclaredMethod("restoreAndReconnect");
        m.setAccessible(true);
        m.invoke(service);

        assertFalse("with no ports there is nothing to monitor",
                monitorActiveOf(service));
    }

    /**
     * onCreate() must not flash "stopping" before restoreForwardedPorts() has
     * run — at that point the service cannot know whether it has work.
     */
    @Test
    public void onCreate_placeholderIsNotStoppingText() throws Exception {
        java.lang.reflect.Method onCreate = BackgroundService.class
                .getDeclaredMethod("onCreate");
        onCreate.setAccessible(true);
        onCreate.invoke(service);

        Notification n = org.robolectric.Shadows.shadowOf(service)
                .getLastForegroundNotification();
        assertNotNull("onCreate must post a foreground notification", n);
        CharSequence text = NotificationCompat.getExtras(n)
                .getCharSequence(Notification.EXTRA_TEXT);
        assertNotNull(text);
        assertNotEquals("the onCreate placeholder must not read as 'stopping'",
                appContext.getString(R.string.notif_service_stopping), text.toString());
        assertEquals(appContext.getString(R.string.notif_service_starting),
                text.toString());
    }

    // =====================================================
    // Helpers
    // =====================================================

    private void seedForwardedPortsPrefs(String... entries) {
        Set<String> ports = new java.util.HashSet<>();
        java.util.Collections.addAll(ports, entries);
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .edit().putStringSet("forwarded_ports", ports).commit();
    }

    @SuppressWarnings({"unchecked", "rawtypes"})
    private Map forwardedPortsOf(BackgroundService svc) throws Exception {
        Field f = BackgroundService.class.getDeclaredField("forwardedPorts");
        f.setAccessible(true);
        return (Map) f.get(svc);
    }

    private Object newPortInfo(int targetPort, String host) throws Exception {
        Class<?> portInfoClass = Class.forName("com.clawbench.app.BackgroundService$PortInfo");
        java.lang.reflect.Constructor<?> ctor =
                portInfoClass.getDeclaredConstructor(int.class, String.class);
        ctor.setAccessible(true);
        return ctor.newInstance(targetPort, host);
    }

    private Notification buildCurrentNotification(BackgroundService svc) throws Exception {
        java.lang.reflect.Method m = BackgroundService.class
                .getDeclaredMethod("buildCurrentNotification");
        m.setAccessible(true);
        return (Notification) m.invoke(svc);
    }

    private void setStaticField(String name, Object value) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(name);
        field.setAccessible(true);
        field.set(null, value);
    }

    private void setInstanceField(String name, Object value) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(name);
        field.setAccessible(true);
        field.set(service, value);
    }

    private boolean monitorActiveOf(BackgroundService svc) throws Exception {
        Field f = BackgroundService.class.getDeclaredField("monitorActive");
        f.setAccessible(true);
        return (boolean) f.get(svc);
    }
}
