package com.clawbench.app;

import android.content.Context;

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
        setField("networkExecutor", second);
        AtomicInteger queued = new AtomicInteger();
        setField("networkExecutor", countingExecutor(second, queued));
        BackgroundService.forceReconnectAsync(15000, null);
        assertEquals("a settled attempt must not block the next one", 1, queued.get());
        second.shutdown();
        second.awaitTermination(5, TimeUnit.SECONDS);
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
     * Duplicate expressions must not fire twice: the JS side installs one global
     * callback, and evaluating it twice would resolve an already-settled promise
     * a second time.
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
        destroyer.start();
        destroyer.join(5000);
        assertFalse("onDestroy itself must return promptly", destroyer.isAlive());

        testExecutor.shutdown();
        assertTrue("the queued teardown must run",
                testExecutor.awaitTermination(5, TimeUnit.SECONDS));

        verify(session).disconnect();
        // The decisive part: the disconnect ran on the executor, not on the
        // thread that called onDestroy. "Did it run at all" alone passes for the
        // original buggy code too.
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
            assertTrue("the refusal must be explicit: " + expected.getMessage(),
                    expected.getMessage().contains("shutting down"));
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
