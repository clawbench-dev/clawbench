package com.clawbench.app.tunnel;

import org.junit.After;
import org.junit.Test;

import java.io.IOException;
import java.net.BindException;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.util.HashSet;
import java.util.Set;
import java.util.function.BooleanSupplier;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

/**
 * Real-socket regression guard for {@link H2PortForwardTransport#close()}.
 *
 * <h2>Why this class exists (and why it is separate)</h2>
 * {@code H2PortForwardTransportTest} deliberately drives the listener through
 * {@link FakeSockets} and only ever asserts the fake's {@code closed} flag. That
 * proves the transport <em>decided</em> to close, but not that a real OS port was
 * released: if {@code close()} regressed to "set the flag, never call
 * {@code ServerSocket.close()}", those tests stay green while the bug (a leaked
 * listener, and a same-port rebind failing with EADDRINUSE) survives.
 *
 * <p>This class closes that gap by using real sockets and observing the OS: a
 * port is "occupied" iff a second bind of it throws {@link BindException}, and
 * "free" iff the bind succeeds. It is a <em>separate</em> class on purpose — the
 * sibling class documents "no real sockets anywhere", and a distinct class makes
 * the deviation explicit and localised.
 *
 * <h2>Why real sockets are safe here (unlike Robolectric)</h2>
 * This is a <b>pure JVM</b> test: it has no {@code @RunWith(RobolectricTestRunner)}
 * and the test JVM is configured with {@code returnDefaultValues = true}
 * ({@code app/build.gradle}), so the {@code android.util.Log} calls that
 * {@link com.clawbench.app.AppLog} makes become no-ops. A bare
 * {@code new ServerSocket()} only wedges the Gradle worker under Robolectric
 * (measured while building T9); a plain-JVM socket is already used by
 * {@code BackgroundServicePortHostTest} (a real {@code ServerSocket(0)}) in the
 * same suite, which is the precedent this class follows.
 *
 * <p>{@code H2PortForwardTransport} itself depends on no Android runtime: it
 * imports only {@code java.net.*} / {@code java.util.concurrent.*} plus
 * {@code AppLog} (whose Android calls no-op under {@code returnDefaultValues}).
 */
public class H2PortForwardTransportRealSocketTest {

    private static final String LOOPBACK = "127.0.0.1";
    private static final String THREAD_PREFIX = "H2Tunnel-";
    /** Upper bound for "close() must not hang"; the real figure is ~0 ms. */
    private static final long CLOSE_BUDGET_MS = 5_000;
    private static final long WAIT_MS = 5_000;
    /**
     * How long a released port is allowed to take to become rebindable again.
     * The real figure is ~1 ms (see {@link #assertPortFree}); the budget is
     * generous only so a loaded CI box cannot make the wait itself flaky.
     */
    private static final long PORT_FREE_TIMEOUT_MS = 2_000;
    /** Poll interval while waiting for a released port to become rebindable. */
    private static final long PORT_FREE_POLL_MS = 10;

    private final FakeTunnelStream tunnel = new FakeTunnelStream();
    private H2PortForwardTransport transport;

    @After
    public void tearDown() {
        if (transport != null) {
            transport.close();
        }
    }

    // ==================================================================
    // A. close() really releases the port
    // ==================================================================

    /**
     * Positive control for every "the port is free again" assertion below: right
     * after {@code addLocal} the port is genuinely held by the OS. Without this,
     * a "free after close()" test could pass vacuously if {@code addLocal} never
     * bound anything. This is also the root mechanism behind "switching to SSH,
     * {@code setPortForwardingL} fails": the surviving h2 listener still owns the
     * port.
     */
    @Test
    public void addLocal_occupiesThePortForReal() throws Exception {
        int port = freePort();
        transport = new H2PortForwardTransport(() -> tunnel);

        transport.addLocal(port, 80, LOOPBACK);

        assertPortOccupied(port);
        assertTrue("the transport must report the listener", transport.isLocalReachable(port));
    }

    /** The core claim: after close() the OS port is released, not just flagged. */
    @Test
    public void close_releasesTheRealPort() throws Exception {
        int port = freePort();
        transport = new H2PortForwardTransport(() -> tunnel);
        transport.addLocal(port, 80, LOOPBACK);
        assertPortOccupied(port);

        transport.close();

        // A successful rebind is the only proof that ServerSocket.close() ran:
        // a flag-only regression leaves the port held and this throws.
        assertPortFree(port);
    }

    /** Every listener is released, not just the first one in the map. */
    @Test
    public void close_releasesEveryListener() throws Exception {
        int first = freePort();
        int second = freePort();
        int third = freePort();
        transport = new H2PortForwardTransport(() -> tunnel);
        transport.addLocal(first, 80, LOOPBACK);
        transport.addLocal(second, 81, LOOPBACK);
        transport.addLocal(third, 82, LOOPBACK);
        assertPortOccupied(first);
        assertPortOccupied(second);
        assertPortOccupied(third);

        transport.close();

        assertPortFree(first);
        assertPortFree(second);
        assertPortFree(third);
    }

    /** removeLocal() must release the real port too (same primitive as close()). */
    @Test
    public void removeLocal_releasesTheRealPort() throws Exception {
        int port = freePort();
        transport = new H2PortForwardTransport(() -> tunnel);
        transport.addLocal(port, 80, LOOPBACK);
        assertPortOccupied(port);

        transport.removeLocal(port);

        assertPortFree(port);
    }

    // ==================================================================
    // B. close() boundaries
    // ==================================================================

    /**
     * close() must not block on a live relay: the accept thread and both pump
     * threads are parked on blocking reads, and close() has to unblock them by
     * closing the socket rather than waiting for them.
     */
    @Test
    public void close_withAnActiveRelay_returnsPromptlyAndReleasesThePort() throws Exception {
        int port = freePort();
        transport = new H2PortForwardTransport(() -> tunnel);
        transport.addLocal(port, 80, LOOPBACK);

        try (Socket client = new Socket()) {
            client.connect(new InetSocketAddress(LOOPBACK, port), (int) WAIT_MS);
            // The connection must actually be accepted and tracked, or the
            // "did not hang" assertion would be about an idle transport.
            assertTrue("the accepted connection must become an active relay",
                    waitFor(() -> transport.activeRelayCount() == 1));

            long start = System.nanoTime();
            transport.close();
            long elapsedMs = (System.nanoTime() - start) / 1_000_000;

            assertTrue("close() must not hang on an active relay (took "
                    + elapsedMs + " ms)", elapsedMs < CLOSE_BUDGET_MS);
            assertEquals("every relay must be torn down", 0, transport.activeRelayCount());
        }
        assertPortFree(port);
    }

    /**
     * The accept thread is blocked in {@code accept()}; closing the listener must
     * end it. Asserted on the specific thread instance (thread names restart at
     * {@code H2Tunnel-1} per transport, so a name count would be ambiguous).
     */
    @Test
    public void close_stopsTheAcceptThread() throws Exception {
        int port = freePort();
        Set<Thread> before = tunnelThreads();
        transport = new H2PortForwardTransport(() -> tunnel);
        transport.addLocal(port, 80, LOOPBACK);

        assertTrue("the accept loop must be running on its own thread",
                waitFor(() -> tunnelThreads().size() > before.size()));
        Set<Thread> spawned = tunnelThreads();
        spawned.removeAll(before);
        assertEquals("addLocal must spawn exactly the accept thread", 1, spawned.size());
        Thread acceptThread = spawned.iterator().next();

        transport.close();

        assertTrue("the accept thread must exit after close()",
                waitFor(() -> !acceptThread.isAlive()));
    }

    /** A second close() (double tap, disconnect after close) must not throw. */
    @Test
    public void close_isIdempotent() throws Exception {
        int port = freePort();
        transport = new H2PortForwardTransport(() -> tunnel);
        transport.addLocal(port, 80, LOOPBACK);

        transport.close();
        transport.close();

        assertPortFree(port);
    }

    /**
     * After close() the transport must be usable again (the service reconnects
     * first, then replays its forwards): the executor is rebuilt lazily and the
     * real port is re-bound.
     */
    @Test
    public void addLocal_rebindsTheRealPortAfterClose() throws Exception {
        int port = freePort();
        transport = new H2PortForwardTransport(() -> tunnel);
        transport.addLocal(port, 80, LOOPBACK);
        transport.close();
        assertPortFree(port);

        // close() tore the h2 session down, so a bare re-add is correctly
        // refused; the service reconnects first.
        tunnel.closed = false;
        transport.addLocal(port, 80, LOOPBACK);

        assertPortOccupied(port);
        assertTrue(transport.isLocalReachable(port));
    }

    // ==================================================================
    // helpers
    // ==================================================================

    /** A port the OS reports as free right now. */
    private static int freePort() throws IOException {
        try (ServerSocket probe = new ServerSocket(0)) {
            return probe.getLocalPort();
        }
    }

    /**
     * Assert {@code port} is genuinely held: a fresh bind of the same
     * address:port must fail with {@link BindException}. (SO_REUSEADDR does not
     * let a second live listener share the port — only TIME_WAIT sockets.)
     */
    private static void assertPortOccupied(int port) throws IOException {
        ServerSocket probe = new ServerSocket();
        probe.setReuseAddress(false);
        try {
            probe.bind(new InetSocketAddress(LOOPBACK, port));
        } catch (BindException expected) {
            probe.close();
            return;
        }
        probe.close();
        fail("expected port " + port + " to be occupied, but a rebind succeeded");
    }

    /**
     * Assert {@code port} is genuinely released: a fresh bind must eventually
     * succeed.
     *
     * <p>Polled rather than attempted once: {@code ServerSocket.close()} is not
     * synchronous with the kernel dropping the listener. The closing socket's
     * accept thread is parked in {@code accept()} and must be woken and exit
     * before the bound address is fully released, so an immediate rebind can
     * still race it and throw {@link BindException} (measured: ~7/3000 attempts,
     * resolving within ~1 ms). The assertion is unchanged in meaning — the port
     * <em>must</em> become bindable — we simply give the kernel that moment
     * instead of failing on the first, racy attempt.
     */
    private static void assertPortFree(int port) throws IOException, InterruptedException {
        long deadline = System.currentTimeMillis() + PORT_FREE_TIMEOUT_MS;
        BindException lastFailure = null;
        while (true) {
            try (ServerSocket probe = new ServerSocket()) {
                probe.setReuseAddress(true);
                probe.bind(new InetSocketAddress(LOOPBACK, port));
                return;
            } catch (BindException e) {
                lastFailure = e;
            }
            if (System.currentTimeMillis() >= deadline) {
                fail("port " + port + " was still not bindable after "
                        + PORT_FREE_TIMEOUT_MS + " ms of polling (last error: "
                        + lastFailure + ")");
            }
            Thread.sleep(PORT_FREE_POLL_MS);
        }
    }

    /**
     * Every live thread whose name starts with the transport's prefix. Mutable
     * on purpose: the caller diffs it against another snapshot with
     * {@code removeAll}.
     */
    private static Set<Thread> tunnelThreads() {
        Set<Thread> threads = new HashSet<>();
        for (Thread thread : Thread.getAllStackTraces().keySet()) {
            if (thread.getName().startsWith(THREAD_PREFIX)) {
                threads.add(thread);
            }
        }
        return threads;
    }

    private static boolean waitFor(BooleanSupplier condition) throws InterruptedException {
        long deadline = System.currentTimeMillis() + WAIT_MS;
        while (System.currentTimeMillis() < deadline) {
            if (condition.getAsBoolean()) {
                return true;
            }
            Thread.sleep(10);
        }
        return condition.getAsBoolean();
    }
}
