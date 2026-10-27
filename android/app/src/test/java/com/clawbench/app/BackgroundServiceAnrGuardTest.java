package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

import com.jcraft.jsch.Session;

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
import java.util.Set;
import java.util.concurrent.AbstractExecutorService;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Regression tests for the Android "应用无响应" (ANR) defect.
 *
 * <p>Symptom: with the SSH port unreachable, every user action froze the UI for
 * 5–25s. The ANR trace showed the main thread {@code Blocked} in
 * {@code BackgroundService.onDestroy()} waiting for the service object lock,
 * held by a thread stuck in {@code Session.connect()} inside
 * {@code ensureConnection()}.
 *
 * <p>Two independent defects fed it, one group of tests each:
 * <ol>
 *   <li>{@code onDestroy()} called the {@code synchronized}
 *       {@code disconnectInternal()} directly on the main thread;</li>
 *   <li>{@code forceReconnectAsync()} had no in-flight guard, so the frontend's
 *       fixed 5s poll queued a new 15s attempt faster than the single-threaded
 *       executor could drain them — the lock was held almost continuously.</li>
 * </ol>
 *
 * <p>Uses Robolectric like {@code BackgroundServiceFloatingTest}, because
 * {@code onDestroy()} legitimately touches Service lifecycle APIs
 * ({@code stopSelf}, {@code stopForeground}).
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class BackgroundServiceAnrGuardTest {

    private BackgroundService service;
    private ExecutorService testExecutor;

    @Before
    public void setUp() throws Exception {
        service = Robolectric.buildService(BackgroundService.class).get();
        // Fresh prefs: no server URL, so ensureConnection() fails immediately
        // instead of dialing anything.
        Context app = RuntimeEnvironment.getApplication();
        app.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .edit().clear().commit();

        setField("forwardedPorts", new ConcurrentHashMap<Integer, BackgroundService.PortInfo>());
        setField("reversePorts", new ConcurrentHashMap<Integer, BackgroundService.PortInfo>());
        setField("isShuttingDown", false);
        setField("intentionalDisconnect", false);
        setField("monitorActive", false);
        setField("nativeWsActive", false);
        setField("sshScreenSuspended", false);
        setField("activeTransport", null);
        setField("sshTransport", null);
        setField("h2PortForwardTransport", null);
        setField("lastH2Kind", null);

        testExecutor = Executors.newSingleThreadExecutor();
        setField("networkExecutor", testExecutor);

        setStaticField("instance", service);
        setStaticField("isRunning", true);
        setStaticField("lastError", null);
    }

    @After
    public void tearDown() throws Exception {
        try {
            setStaticField("instance", null);
            setStaticField("isRunning", false);
            setStaticField("lastError", null);
        } catch (Exception ignored) {
        }
        if (testExecutor != null) testExecutor.shutdownNow();
    }

    // ==================================================================
    // Defect 2: concurrent reconnect calls must coalesce, not queue
    // ==================================================================

    /**
     * The frontend polls faster than an attempt can finish. Three calls arriving
     * back-to-back must produce exactly ONE queued attempt — otherwise the
     * executor queue (and with it the lock hold time) grows without bound.
     */
    @Test
    public void forceReconnectAsync_whileInFlight_doesNotQueueASecondAttempt() throws Exception {
        ExecutorService gated = newSingleThreadGated();
        AtomicInteger queued = new AtomicInteger();
        setField("networkExecutor", countingExecutor(gated, queued));

        BackgroundService.forceReconnectAsync(15000, "window.cbOne");
        BackgroundService.forceReconnectAsync(15000, "window.cbTwo");
        BackgroundService.forceReconnectAsync(15000, "window.cbThree");

        assertEquals("only one attempt may be queued while another is in flight",
                1, queued.get());
        // Distinct callers are kept (no dedup surprises for real code) and all
        // of them ride the one attempt.
        assertEquals("every distinct callback must be retained exactly once",
                3, callbacks().size());
        assertTrue("the coalesced attempt must still be marked in flight", inFlight());

        releaseGated(gated);
    }

    /**
     * The in-flight flag must be released when the attempt finishes, otherwise
     * the tunnel could never be reconnected again for the life of the service.
     */
    @Test
    public void forceReconnectAsync_afterTheAttemptFinishes_acceptsANewOne() throws Exception {
        assertFalse("precondition", inFlight());

        BackgroundService.forceReconnectAsync(15000, "window.cb");
        testExecutor.shutdown();
        assertTrue("attempt must have run", testExecutor.awaitTermination(5, TimeUnit.SECONDS));

        assertFalse("the in-flight flag must be cleared once the attempt settles", inFlight());
        assertTrue("a finished attempt must leave no callback behind", callbacks().isEmpty());

        // And a later attempt is accepted again (the flag is genuinely reusable).
        ExecutorService second = Executors.newSingleThreadExecutor();
        AtomicInteger queued = new AtomicInteger();
        setField("networkExecutor", countingExecutor(second, queued));
        BackgroundService.forceReconnectAsync(15000, null);
        assertEquals("a settled attempt must not block the next one", 1, queued.get());
        second.shutdown();
        assertTrue(second.awaitTermination(5, TimeUnit.SECONDS));
    }

    /**
     * A call that arrives while the executor is already shut down (service being
     * destroyed) must not leave the in-flight flag stuck true.
     */
    @Test
    public void forceReconnectAsync_rejectedByAShutdownExecutor_releasesTheFlag() throws Exception {
        testExecutor.shutdownNow();

        BackgroundService.forceReconnectAsync(15000, "window.cb");

        assertFalse("a rejected attempt must not leave the service permanently 'reconnecting'",
                inFlight());
        assertTrue(callbacks().isEmpty());
    }

    /**
     * Duplicate expressions are stored once. This is an optimisation, not a
     * correctness requirement: the JS side installs a single global
     * ({@code window.__clawbenchReconnectResult}) whose {@code done} already has
     * a {@code settled} guard, so a second evaluation is a harmless no-op. The
     * set merely avoids doing that work twice.
     */
    @Test
    public void forceReconnectAsync_identicalCallbacks_areDeduplicated() throws Exception {
        ExecutorService gated = newSingleThreadGated();
        setField("networkExecutor", countingExecutor(gated, new AtomicInteger()));

        BackgroundService.forceReconnectAsync(15000, "window.cb");
        BackgroundService.forceReconnectAsync(15000, "window.cb");

        assertEquals("the same expression must be registered once", 1, callbacks().size());
        releaseGated(gated);
    }

    // ==================================================================
    // Defect 1: onDestroy must never block the main thread on the service lock
    // ==================================================================

    /**
     * The ANR itself: with a connect attempt holding the object lock, the old
     * {@code onDestroy()} waited for it on the main thread. It must now return
     * without ever touching that lock.
     *
     * <p>Runs onDestroy on its own thread with a bounded join so a regression
     * fails cleanly instead of hanging the suite (the old code blocked until the
     * holder released, which it never would here).
     */
    @Test
    public void onDestroy_whileAConnectHoldsTheLock_returnsWithoutBlocking() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);

        // Hold the service monitor the way an in-flight ensureConnection() does.
        CountDownLatch locked = new CountDownLatch(1);
        CountDownLatch release = new CountDownLatch(1);
        Thread holder = new Thread(() -> {
            synchronized (service) {
                locked.countDown();
                try {
                    release.await(10, TimeUnit.SECONDS);
                } catch (InterruptedException ignored) {
                }
            }
        }, "lock-holder");
        holder.setDaemon(true);
        holder.start();
        assertTrue(locked.await(5, TimeUnit.SECONDS));

        Thread destroyer = new Thread(() -> {
            try {
                invoke("onDestroy");
            } catch (Exception e) {
                throw new RuntimeException(e);
            }
        }, "fake-main");
        destroyer.setDaemon(true);
        destroyer.start();
        try {
            destroyer.join(3000);
            assertFalse("onDestroy must not wait for the tunnel lock (main-thread ANR)",
                    destroyer.isAlive());
        } finally {
            release.countDown();
            holder.join(5000);
        }
    }

    /**
     * The teardown still has to happen — just off the calling thread. Otherwise
     * the session would outlive the service.
     */
    @Test
    public void onDestroy_stillDisconnectsTheSession_offTheMainThread() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);

        java.util.concurrent.atomic.AtomicReference<String> teardownThread =
                new java.util.concurrent.atomic.AtomicReference<>();
        org.mockito.Mockito.doAnswer(inv -> {
            teardownThread.set(Thread.currentThread().getName());
            return null;
        }).when(session).disconnect();

        String caller = Thread.currentThread().getName();
        java.util.concurrent.atomic.AtomicReference<String> destroyCaller =
                new java.util.concurrent.atomic.AtomicReference<>();
        Thread destroyer = new Thread(() -> {
            destroyCaller.set(Thread.currentThread().getName());
            try {
                invoke("onDestroy");
            } catch (Exception e) {
                throw new RuntimeException(e);
            }
        }, "fake-main");
        // Daemon: if the production change regresses, onDestroy blocks on the
        // service monitor and this thread would otherwise keep the Gradle worker
        // alive after the assertion fails (a hang instead of a clean failure).
        destroyer.setDaemon(true);
        destroyer.start();
        destroyer.join(5000);
        assertFalse("onDestroy itself must return promptly", destroyer.isAlive());

        // The teardown runs on its own daemon thread, so wait for the disconnect
        // itself rather than for the executor.
        org.mockito.Mockito.verify(session, org.mockito.Mockito.timeout(5000)).disconnect();
        // The decisive part: the disconnect ran off the thread that called
        // onDestroy. "Did it run at all" alone passes for the original buggy code
        // too.
        assertNotNull("the teardown must have run", teardownThread.get());
        assertFalse("the teardown must not run on the onDestroy caller (that was the ANR)",
                destroyCaller.get().equals(teardownThread.get()));
        assertFalse("sanity: the destroyer is not the JUnit thread", caller.equals(destroyCaller.get()));
    }

    /**
     * A connect queued just before shutdown must not open a session after the
     * teardown has run — that would leave a live SSH session behind a dead
     * service (a leak the old main-thread disconnect prevented by accident).
     */
    @Test
    public void ensureConnection_afterShutdownStarted_throwsInsteadOfConnecting() throws Exception {
        setField("isShuttingDown", true);
        try {
            service.ensureConnection();
            org.junit.Assert.fail("ensureConnection must refuse to run while shutting down");
        } catch (Exception expected) {
            assertTrue("the refusal must be a distinct type so callers can tell it "
                            + "apart from a real connection failure, got " + expected.getClass().getName(),
                    expected instanceof BackgroundService.ServiceShuttingDownException);
        }
    }

    /**
     * The shutdown abort must not be mistaken for a connection failure by
     * {@code addPortForward}: its catch block deletes the mapping from memory and
     * from {@code forwarded_ports}, which would silently drop a mapping the
     * frontend has already registered on the server.
     *
     * <p>The window is real but narrow: {@code addPortForward} checks
     * {@code isShuttingDown} on entry, then calls {@code saveForwardedPorts()},
     * and only then {@code ensureConnection()} — where the new guard throws.
     * {@code onDestroy()} can set the flag inside that window. The test closes it
     * deterministically by flipping the flag from the prefs stub that
     * {@code saveForwardedPorts()} hits.
     */
    @Test
    public void addPortForward_shutdownRacingTheConnect_keepsTheMapping() throws Exception {
        BackgroundService spied = spyWithShutdownRaceOnSave();

        // Deliberately NOT pre-populated: `saveForwardedPorts()` (which is what
        // closes the race in this fixture) only runs on the not-yet-tracked path.
        invokeOn(spied, "addPortForward", 3080, 80, "127.0.0.1");
        assertTrue("a shutdown abort must keep the desired mapping",
                forwardedPortsOf(spied).containsKey(3080));
        assertFalse("a shutdown abort must not surface as an SSH error",
                lastErrorMentionsShutdown());
    }

    /**
     * The same contract for reverse forwards (their catch also deletes + saves).
     */
    @Test
    public void addReversePortForward_shutdownRacingTheConnect_keepsTheMapping() throws Exception {
        BackgroundService spied = spyWithShutdownRaceOnSave();

        invokeOn(spied, "addReversePortForward", 9000, 3000, "");

        assertTrue("a shutdown abort must keep the desired reverse mapping",
                reversePortsOf(spied).containsKey(9000));
    }

    /**
     * A connect that started before onDestroy() can still succeed after it. It
     * must not re-post the ongoing notification (that leaves a permanent
     * "N 个端口转发" entry with no service behind it) nor re-arm the monitor on a
     * destroyed service.
     */
    @Test
    public void updateNotification_afterShutdownStarted_doesNotPost() throws Exception {
        android.app.NotificationManager nm =
                (android.app.NotificationManager) RuntimeEnvironment.getApplication()
                        .getSystemService(Context.NOTIFICATION_SERVICE);
        nm.cancelAll();

        setField("isShuttingDown", true);
        invoke("updateNotification", 3, null);

        assertEquals("no notification may be posted after shutdown",
                0, org.robolectric.Shadows.shadowOf(nm).getActiveNotifications().length);

        // Control: the same call without the guard DOES post, so the assertion
        // above is discriminating rather than vacuously true.
        setField("isShuttingDown", false);
        invoke("updateNotification", 3, null);
        assertEquals("the guard is what suppresses the post, not the fixture",
                1, org.robolectric.Shadows.shadowOf(nm).getActiveNotifications().length);
    }

    @Test
    public void startConnectionMonitor_afterShutdownStarted_doesNotArm() throws Exception {
        setField("isShuttingDown", true);
        invoke("startConnectionMonitor");
        try {
            assertFalse("the monitor must not be armed on a destroyed service",
                    field("monitorActive", Boolean.class));
        } finally {
            invoke("stopConnectionMonitor");
        }
    }

    // --- helpers ---

    /**
     * A single-thread executor already busy with a blocking task, so anything
     * submitted to it stays queued (which is what "in flight" means here).
     */
    private final CountDownLatch gateRelease = new CountDownLatch(1);

    private ExecutorService newSingleThreadGated() throws Exception {
        ExecutorService gated = Executors.newSingleThreadExecutor();
        CountDownLatch started = new CountDownLatch(1);
        gated.execute(() -> {
            started.countDown();
            try {
                gateRelease.await(10, TimeUnit.SECONDS);
            } catch (InterruptedException ignored) {
            }
        });
        assertTrue("gate task must start", started.await(5, TimeUnit.SECONDS));
        return gated;
    }

    private void releaseGated(ExecutorService gated) throws Exception {
        gateRelease.countDown();
        gated.shutdown();
        assertTrue("gated executor must drain", gated.awaitTermination(5, TimeUnit.SECONDS));
    }

    /** Counts submissions while forwarding to {@code delegate}. */
    private ExecutorService countingExecutor(ExecutorService delegate, AtomicInteger counter) {
        return new AbstractExecutorService() {
            @Override public void execute(Runnable command) {
                counter.incrementAndGet();
                delegate.execute(command);
            }
            @Override public void shutdown() { delegate.shutdown(); }
            @Override public java.util.List<Runnable> shutdownNow() { return delegate.shutdownNow(); }
            @Override public boolean isShutdown() { return delegate.isShutdown(); }
            @Override public boolean isTerminated() { return delegate.isTerminated(); }
            @Override public boolean awaitTermination(long t, TimeUnit u) throws InterruptedException {
                return delegate.awaitTermination(t, u);
            }
        };
    }

    private boolean inFlight() throws Exception {
        return field("reconnectInFlight", Boolean.class);
    }

    /**
     * A spy whose {@code getSharedPreferences} flips {@code isShuttingDown} the
     * first time it is consulted — i.e. exactly between {@code addPortForward}'s
     * entry check and its {@code ensureConnection()} call.
     */
    private BackgroundService spyWithShutdownRaceOnSave() throws Exception {
        BackgroundService spied = org.mockito.Mockito.spy(service);
        SharedPreferences prefs = org.mockito.Mockito.mock(SharedPreferences.class);
        SharedPreferences.Editor editor = org.mockito.Mockito.mock(SharedPreferences.Editor.class);
        org.mockito.Mockito.when(prefs.edit()).thenReturn(editor);
        org.mockito.Mockito.when(editor.putStringSet(
                org.mockito.ArgumentMatchers.anyString(), org.mockito.ArgumentMatchers.any()))
                .thenAnswer(inv -> {
                    setFieldOn(spied, "isShuttingDown", true);
                    return editor;
                });
        org.mockito.Mockito.doNothing().when(editor).apply();
        org.mockito.Mockito.when(prefs.getString(
                org.mockito.ArgumentMatchers.anyString(), org.mockito.ArgumentMatchers.anyString()))
                .thenReturn("");
        org.mockito.Mockito.doReturn(prefs).when(spied)
                .getSharedPreferences(org.mockito.ArgumentMatchers.anyString(),
                        org.mockito.ArgumentMatchers.anyInt());
        return spied;
    }

    private boolean lastErrorMentionsShutdown() {
        String err = BackgroundService.getLastError();
        return err != null && err.contains("shutting down");
    }

    @SuppressWarnings("unchecked")
    private java.util.Map<Integer, BackgroundService.PortInfo> forwardedPortsOf(BackgroundService target)
            throws Exception {
        return fieldOn(target, "forwardedPorts", java.util.Map.class);
    }

    @SuppressWarnings("unchecked")
    private java.util.Map<Integer, BackgroundService.PortInfo> reversePortsOf(BackgroundService target)
            throws Exception {
        return fieldOn(target, "reversePorts", java.util.Map.class);
    }

    private void invokeOn(BackgroundService target, String name, Object... args) throws Exception {
        Class<?>[] types = new Class<?>[args.length];
        for (int i = 0; i < args.length; i++) {
            if (args[i] instanceof Integer) types[i] = int.class;
            else if (args[i] instanceof Boolean) types[i] = boolean.class;
            else types[i] = String.class;
        }
        Method m = BackgroundService.class.getDeclaredMethod(name, types);
        m.setAccessible(true);
        m.invoke(target, args);
    }

    @SuppressWarnings("unchecked")
    private <T> T fieldOn(Object target, String name, Class<T> type) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        return (T) f.get(target);
    }

    private void setFieldOn(Object target, String name, Object value) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(target, value);
    }

    @SuppressWarnings("unchecked")
    private Set<String> callbacks() throws Exception {
        return field("pendingReconnectCallbacks", Set.class);
    }

    @SuppressWarnings("unchecked")
    private <T> T field(String name, Class<T> type) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        return (T) f.get(service);
    }

    private void invoke(String name) throws Exception {
        Method m = BackgroundService.class.getDeclaredMethod(name);
        m.setAccessible(true);
        m.invoke(service);
    }

    private void invoke(String name, Object... args) throws Exception {
        Class<?>[] types = new Class<?>[args.length];
        for (int i = 0; i < args.length; i++) {
            if (args[i] instanceof Integer) types[i] = int.class;
            else if (args[i] instanceof Boolean) types[i] = boolean.class;
            else types[i] = String.class;
        }
        Method m = BackgroundService.class.getDeclaredMethod(name, types);
        m.setAccessible(true);
        m.invoke(service, args);
    }

    private void setField(String name, Object value) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(service, value);
    }

    private void setStaticField(String name, Object value) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(null, value);
    }
}
