package com.clawbench.app.tunnel;

import com.clawbench.app.AppLog;

import androidx.annotation.Nullable;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.util.ArrayList;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * The HTTP/2 stream tunnel's port-forward transport.
 *
 * <h2>Shape</h2>
 * {@code -L} is the mirror image of SSH: where the server owned the listener,
 * the h2 tunnel makes the client listen. Every accepted local connection
 * becomes one {@code POST /api/tunnel/stream?host=&port=} — which is the whole
 * reason the tunnel only needs the main port.
 *
 * <p>{@code -R} inverts the roles again. h2 cannot open a server-initiated
 * stream (RFC 9113 §8.4), so the server parks each accepted connection, mints a
 * single-use token and announces it as {@code incoming} on the long-lived
 * control stream; the client dials its own target and redeems the token with a
 * claim stream. Both directions end up as the same relay.
 *
 * <h2>Threading</h2>
 * Nothing here runs on {@code BackgroundService.networkExecutor}. That executor
 * is a single thread that also serialises {@code ensureConnection},
 * {@code disconnect} and the WebSocket reconnect path; an accept loop parked on
 * it would starve every one of them (design doc §8). This transport therefore
 * owns a dedicated daemon pool, lazily (re)created so it survives a
 * disconnect/reconnect cycle:
 *
 * <ul>
 *   <li>one thread per listener, blocked in {@code accept()};</li>
 *   <li>one thread per accepted connection to open the stream, then two more to
 *       pump it — the two directions block independently, so a single thread
 *       could not carry both;</li>
 *   <li>one thread per {@code incoming} notification to dial the target and
 *       claim the stream (never the control stream's own reader thread, which
 *       must stay free to receive the next line).</li>
 * </ul>
 *
 * {@link #close()} shuts the pool down; the next {@link #addLocal} builds a new
 * one, which is what makes the service's screen-off/reconnect replay work.
 *
 * <h2>Backpressure</h2>
 * There is none to implement. {@link TunnelConnection} blocks a write once the
 * h2 flow-control window fills, and not reading its input simply withholds
 * WINDOW_UPDATE. The pumps below are plain blocking copy loops on purpose — an
 * extra buffer or a sleep would only add latency.
 *
 * <h2>Half-close</h2>
 * When the local socket reaches EOF, the request direction is ended with
 * {@link TunnelConnection#closeWrite()} (END_STREAM) rather than closing the
 * stream, so the peer may keep replying. Only the response direction ending
 * tears the pair down. This is the same asymmetry the server's bounded-grace
 * half-close implements on the other end.
 */
public final class H2PortForwardTransport implements PortForwardTransport {

    private static final String TAG = "ClawBench";
    /** Listen backlog. Small: the accept loop is never the bottleneck. */
    private static final int BACKLOG = 50;
    private static final int BUFFER_SIZE = 16 * 1024;
    private static final String LOOPBACK = "127.0.0.1";

    /** Supplies the shared {@link TunnelStream} (process-wide in production). */
    public interface TunnelStreamProvider {
        TunnelStream get();
    }

    private final TunnelStreamProvider tunnels;
    private final ServerSocketFactory socketFactory;
    private final LocalDialer dialer;

    /**
     * Live listeners keyed by the device-side port. Deliberately separate from
     * {@code BackgroundService.PortInfo} (whose persisted format must not
     * change) and never persisted: a listener is runtime state, rebuilt on
     * reconnect.
     */
    private final Map<Integer, ServerSocket> listeners = new ConcurrentHashMap<>();

    /**
     * Desired reverse forwards, keyed by server port (and, for a {@code bind(0)},
     * by the request key too — both entries share one instance). The server
     * reports the port it bound, and {@code incoming} names that port, so the
     * mapping has to be indexed the way the control plane talks.
     */
    private final Map<Integer, ReverseForward> reverseForwards = new ConcurrentHashMap<>();

    /** The control stream whose {@code incoming} handler is currently wired. */
    private volatile TunnelControlStream wiredControl;

    /** In-flight connections, so a port removal can close them, not just unbind. */
    private final Set<Relay> relays = ConcurrentHashMap.newKeySet();

    private final Object executorLock = new Object();
    private ExecutorService executor;
    private final AtomicInteger threadSeq = new AtomicInteger();

    /**
     * Test seam: run when the accept loop deregisters a listener after an
     * {@code accept()} failure. Null in production. Lets a test await that
     * teardown deterministically instead of sleeping.
     */
    @Nullable
    private volatile Runnable onListenerClosedForTesting;

    /** Test seam; see {@link #onListenerClosedForTesting}. */
    void setOnListenerClosedForTesting(@Nullable Runnable hook) {
        this.onListenerClosedForTesting = hook;
    }

    public H2PortForwardTransport(TunnelStreamProvider tunnels) {
        this(tunnels, ServerSocketFactory.DEFAULT, LocalDialer.DEFAULT);
    }

    /** @param socketFactory test seam; production passes {@link ServerSocketFactory#DEFAULT}. */
    public H2PortForwardTransport(TunnelStreamProvider tunnels, ServerSocketFactory socketFactory) {
        this(tunnels, socketFactory, LocalDialer.DEFAULT);
    }

    /**
     * @param dialer test seam for the {@code -R} target dial; production passes
     *               {@link LocalDialer#DEFAULT}.
     */
    public H2PortForwardTransport(TunnelStreamProvider tunnels, ServerSocketFactory socketFactory,
                                  LocalDialer dialer) {
        this.tunnels = tunnels;
        this.socketFactory = socketFactory;
        this.dialer = dialer;
    }

    // ------------------------------------------------------------------
    // -L
    // ------------------------------------------------------------------

    @Override
    public void addLocal(int localPort, int targetPort, String targetHost) throws Exception {
        // Idempotent: a reconnect replay re-adds ports whose listener survived
        // (the listener is independent of the h2 session — it opens a stream per
        // connection), and re-binding would fail with EADDRINUSE.
        ServerSocket existing = listeners.get(localPort);
        if (existing != null && !existing.isClosed()) {
            AppLog.d(TAG, "H2: listener for " + localPort + " already bound, reusing");
            return;
        }

        TunnelStream tunnel = tunnels.get();
        if (tunnel == null || !tunnel.isConnected()) {
            throw new TunnelException(TunnelErrorKind.CLOSED, "tunnel not connected");
        }

        ServerSocket server = socketFactory.create();
        server.setReuseAddress(true);
        try {
            server.bind(new InetSocketAddress(LOOPBACK, localPort), BACKLOG);
        } catch (IOException e) {
            closeQuietly(server);
            throw e;
        }
        listeners.put(localPort, server);
        AppLog.i(TAG, "H2: listening on " + LOOPBACK + ":" + localPort + " -> " + targetHost + ":" + targetPort);

        try {
            executor().execute(() -> acceptLoop(localPort, server, targetHost, targetPort));
        } catch (RejectedExecutionException e) {
            listeners.remove(localPort, server);
            closeQuietly(server);
            throw new IOException("h2 tunnel executor rejected accept loop", e);
        }
    }

    @Override
    public void removeLocal(int localPort) throws Exception {
        ServerSocket server = listeners.remove(localPort);
        if (server != null) {
            closeQuietly(server);
        }
        // Closing the listener stops new connections; the live ones must be torn
        // down explicitly or they would keep relaying on a port the user just
        // deleted (and keep their h2 streams open).
        for (Relay relay : new ArrayList<>(relays)) {
            if (!relay.reverse && relay.portKey == localPort) {
                relay.closeBoth();
            }
        }
        AppLog.i(TAG, "H2: stopped listening on " + localPort);
    }

    // ------------------------------------------------------------------
    // -R
    // ------------------------------------------------------------------

    /**
     * {@code -R}: ask the server to bind {@code serverPort}, then answer every
     * accepted connection by claiming it back over a data stream.
     *
     * <p>The control stream is opened (and its {@code incoming} handler wired)
     * <em>before</em> the bind is requested: a connection that arrives right
     * after {@code bound} is parked by the server and announced with a
     * single-use token, and a token nobody is listening for is simply lost.
     *
     * <p>The port the server actually bound is remembered for {@code incoming}
     * routing, because a {@code bind(0)} is answered with the OS-assigned port.
     *
     * @throws Exception when the tunnel is down or the server refused the bind,
     *                   so the caller can drop the mapping and report failure.
     */
    @Override
    public void addReverse(int serverPort, int targetPort, String targetHost) throws Exception {
        TunnelStream tunnel = tunnels.get();
        if (tunnel == null || !tunnel.isConnected()) {
            throw new TunnelException(TunnelErrorKind.CLOSED, "tunnel not connected");
        }

        // Idempotent, mirroring the SSH adapter's "already registered means
        // success": ensureConnection's replay loop may have bound this port
        // before the explicit add ran, and re-binding would be answered with
        // bind_err (the server's listener already holds the port).
        if (reverseForwards.containsKey(serverPort)) {
            AppLog.d(TAG, "H2: reverse forward " + serverPort + " already bound, reusing");
            return;
        }

        // Open the control plane first: see the method doc. openControlStream
        // is idempotent, so several reverse ports share one stream (and one set
        // of bind bookkeeping, which is what the server expects).
        TunnelControlStream control = tunnel.openControlStream();
        wireControlStream(control);

        Integer bound = tunnel.bind(serverPort);
        if (bound == null) {
            // The control stream died before answering (no server verdict to
            // report). A server refusal throws instead, carrying its code and
            // message, and propagates to the caller unchanged.
            throw new TunnelException(TunnelErrorKind.UNAVAILABLE,
                    "server refused to bind port " + serverPort);
        }

        ReverseForward forward = new ReverseForward(bound, targetHost, targetPort);
        reverseForwards.put(bound, forward);
        // A bind(0) is answered with the OS-assigned port, so index the request
        // key too: removeReverse() is called with the key the service recorded.
        if (bound != serverPort) {
            reverseForwards.put(serverPort, forward);
        }
        AppLog.i(TAG, "H2: reverse forward bound " + bound + " -> " + targetHost + ":" + targetPort);
    }

    @Override
    public void removeReverse(int serverPort) throws Exception {
        ReverseForward forward = reverseForwards.remove(serverPort);
        if (forward != null) {
            // Both keys may point at the same mapping (bind(0)); drop the twin.
            reverseForwards.values().remove(forward);
        }
        TunnelStream tunnel = tunnels.get();
        if (tunnel != null) {
            // Best-effort: the server releases the listener and closes every
            // parked connection when the control stream dies anyway.
            tunnel.unbind(serverPort);
        }
        // The data streams outlive the unbind, so tear them down explicitly —
        // otherwise the target keeps talking to a port the user just deleted.
        for (Relay relay : new ArrayList<>(relays)) {
            if (relay.reverse && relay.portKey == serverPort) {
                relay.closeBoth();
            }
        }
        AppLog.i(TAG, "H2: reverse forward removed for " + serverPort);
    }

    /**
     * Wire the shared control stream once: route {@code incoming} notifications
     * and drop every reverse mapping when the stream dies.
     *
     * <p>Registered exactly once per stream: re-registering per bind would
     * deliver every {@code incoming} several times, and each duplicate would
     * race for the same single-use token.
     */
    private void wireControlStream(TunnelControlStream control) {
        if (control == wiredControl) return;
        wiredControl = control;
        control.onMessage(this::handleControlMessage);
        control.onClose(() -> {
            if (wiredControl == control) wiredControl = null;
            // The mappings are server-side state that dies with the stream;
            // keeping them would advertise reverse ports nothing can serve.
            reverseForwards.clear();
        });
    }

    /** Control-stream callback, on the stream's own reader thread. */
    private void handleControlMessage(ControlMessage msg) {
        if (msg == null || !ControlMessage.INCOMING.equals(msg.type)) return;
        if (msg.token == null || msg.token.isEmpty()) return;
        ReverseForward forward = reverseForwards.get(msg.port);
        if (forward == null) {
            // No matching forward: the token simply expires (the server closes
            // the parked connection on timeout). There is nothing to reject.
            AppLog.d(TAG, "H2: incoming for unknown reverse port " + msg.port + ", ignoring");
            return;
        }
        String token = msg.token;
        // Never block the reader thread: dialing the target and opening the
        // claim stream both block, and a stalled handler would stall every
        // reverse port on this control stream.
        try {
            executor().execute(() -> claimIncoming(forward, msg.port, token));
        } catch (RejectedExecutionException e) {
            // close() raced the notification; the token expires server-side.
            AppLog.d(TAG, "H2: dropping incoming for " + msg.port + ", transport is closing");
        }
    }

    /**
     * Redeem one {@code incoming} token.
     *
     * <p>The target is dialed <em>before</em> the claim stream is opened: a
     * claim token is single-use, so opening the stream first would spend it on
     * a connection that cannot be served if the local target is down.
     */
    private void claimIncoming(ReverseForward forward, int serverPort, String token) {
        Socket target;
        try {
            target = dialer.dial(forward.targetHost, forward.targetPort);
        } catch (IOException e) {
            // Target unreachable: deliberately do NOT claim, so the token is
            // never spent and the server reclaims it on its own timeout.
            AppLog.w(TAG, "H2: reverse target " + forward.targetHost + ":" + forward.targetPort
                    + " unreachable, not claiming: " + e.getMessage());
            return;
        }

        TunnelStream tunnel = tunnels.get();
        if (tunnel == null || !tunnel.isConnected()) {
            closeQuietly(target);
            return;
        }

        TunnelConnection connection;
        try {
            connection = tunnel.openClaimStream(token);
        } catch (TunnelException e) {
            AppLog.w(TAG, "H2: claim for " + serverPort + " failed: " + e.kind() + " " + e.getMessage());
            closeQuietly(target);
            return;
        } catch (RuntimeException e) {
            AppLog.e(TAG, "H2: claim for " + serverPort + " threw", e);
            closeQuietly(target);
            return;
        }

        Relay relay = new Relay(serverPort, true, target);
        relay.connection = connection;
        relays.add(relay);
        if (relay.isClosed()) {
            try {
                connection.close();
            } catch (RuntimeException ignored) {
                // Already gone.
            }
            return;
        }
        try {
            executor().execute(() -> pumpLocalToTunnel(relay));
        } catch (RejectedExecutionException e) {
            relay.closeBoth();
            return;
        }
        pumpTunnelToLocal(relay);
    }

    // ------------------------------------------------------------------
    // state
    // ------------------------------------------------------------------

    @Override
    public boolean isConnected() {
        TunnelStream tunnel = tunnels.get();
        return tunnel != null && tunnel.isConnected();
    }

    /** Number of live local listeners. Diagnostics and tests. */
    int listenerCount() {
        return listeners.size();
    }

    /** Number of in-flight connections. Diagnostics and tests. */
    int activeRelayCount() {
        return relays.size();
    }

    /** Number of desired reverse forwards. Diagnostics and tests. */
    int reverseForwardCount() {
        return reverseForwards.size();
    }

    @Override
    public boolean isLocalReachable(int localPort) {
        // A listener only enters the map after a successful bind, so presence is
        // the answer. Answering from the map (instead of dialing 127.0.0.1) is
        // also what keeps the unit tests free of real sockets, which hang the
        // Gradle worker under Robolectric.
        ServerSocket server = listeners.get(localPort);
        return server != null && !server.isClosed();
    }

    @Override
    public void close() {
        for (ServerSocket server : listeners.values()) {
            closeQuietly(server);
        }
        listeners.clear();
        for (Relay relay : new ArrayList<>(relays)) {
            relay.closeBoth();
        }
        relays.clear();
        reverseForwards.clear();
        wiredControl = null;
        TunnelStream tunnel = tunnels.get();
        if (tunnel != null) {
            // Closing the session tears the control stream down too, which
            // releases every server-side bind and parked connection.
            tunnel.close();
        }
        ExecutorService current;
        synchronized (executorLock) {
            current = executor;
            executor = null;
        }
        if (current != null) {
            current.shutdownNow();
        }
    }

    // ------------------------------------------------------------------
    // accept loop
    // ------------------------------------------------------------------

    private void acceptLoop(int localPort, ServerSocket server, String targetHost, int targetPort) {
        while (!server.isClosed()) {
            Socket socket;
            try {
                socket = server.accept();
            } catch (IOException e) {
                if (!server.isClosed()) {
                    AppLog.w(TAG, "H2: accept on " + localPort + " failed: " + e.getMessage());
                }
                // The loop is dead, so the listener must stop being advertised:
                // isLocalReachable() answers from this map, and leaving a
                // non-closed socket behind would report a port that can no
                // longer accept. Conditional remove so a concurrent rebind on
                // the same port is not evicted.
                listeners.remove(localPort, server);
                closeQuietly(server);
                Runnable hook = onListenerClosedForTesting;
                if (hook != null) {
                    try {
                        hook.run();
                    } catch (RuntimeException ignored) {
                        // Test hook only.
                    }
                }
                break;
            }
            Relay relay = new Relay(localPort, false, socket);
            relays.add(relay);
            try {
                executor().execute(() -> serve(relay, targetHost, targetPort));
            } catch (RejectedExecutionException e) {
                // close() raced the accept; drop the connection rather than leak it.
                relay.closeBoth();
            }
        }
    }

    /**
     * Dial the tunnel target for one accepted connection, then pump it.
     *
     * <p>The response direction runs on this thread and the request direction on
     * another, because both are blocking reads. A failed dial closes the local
     * socket instead of leaving the client waiting on a connection that will
     * never carry anything.
     */
    private void serve(Relay relay, String targetHost, int targetPort) {
        TunnelStream tunnel = tunnels.get();
        if (tunnel == null || !tunnel.isConnected()) {
            AppLog.w(TAG, "H2: dropping connection on " + relay.portKey + ", tunnel is down");
            relay.closeBoth();
            return;
        }

        TunnelConnection connection;
        try {
            connection = tunnel.openStream(targetHost, targetPort);
        } catch (TunnelException e) {
            AppLog.w(TAG, "H2: openStream " + targetHost + ":" + targetPort + " failed: "
                    + e.kind() + " " + e.getMessage());
            relay.closeBoth();
            return;
        } catch (RuntimeException e) {
            AppLog.e(TAG, "H2: openStream " + targetHost + ":" + targetPort + " threw", e);
            relay.closeBoth();
            return;
        }

        relay.connection = connection;
        if (relay.isClosed()) {
            // removeLocal()/close() landed while the stream was opening.
            try {
                connection.close();
            } catch (RuntimeException ignored) {
            }
            return;
        }

        try {
            executor().execute(() -> pumpLocalToTunnel(relay));
        } catch (RejectedExecutionException e) {
            relay.closeBoth();
            return;
        }
        pumpTunnelToLocal(relay);
    }

    /** Local socket -> h2 request body. EOF is a half-close, not a teardown. */
    private void pumpLocalToTunnel(Relay relay) {
        TunnelConnection connection = relay.connection;
        if (connection == null) return;
        try {
            InputStream in = relay.socket.getInputStream();
            OutputStream out = connection.getOutputStream();
            byte[] buffer = new byte[BUFFER_SIZE];
            int read;
            while ((read = in.read(buffer)) != -1) {
                out.write(buffer, 0, read);
                out.flush();
            }
            // The local peer is done sending: END_STREAM, keep the response
            // direction readable (TCP half-close semantics).
            connection.closeWrite();
        } catch (IOException e) {
            AppLog.d(TAG, "H2: local->tunnel pump for " + relay.portKey + " ended: " + e.getMessage());
            relay.closeBoth();
        } catch (RuntimeException e) {
            AppLog.w(TAG, "H2: local->tunnel pump for " + relay.portKey + " failed", e);
            relay.closeBoth();
        }
    }

    /** h2 response body -> local socket. Any end tears the pair down. */
    private void pumpTunnelToLocal(Relay relay) {
        TunnelConnection connection = relay.connection;
        if (connection == null) {
            relay.closeBoth();
            return;
        }
        try {
            InputStream in = connection.getInputStream();
            OutputStream out = relay.socket.getOutputStream();
            byte[] buffer = new byte[BUFFER_SIZE];
            int read;
            while ((read = in.read(buffer)) != -1) {
                out.write(buffer, 0, read);
                out.flush();
            }
        } catch (IOException e) {
            AppLog.d(TAG, "H2: tunnel->local pump for " + relay.portKey + " ended: " + e.getMessage());
        } catch (RuntimeException e) {
            AppLog.w(TAG, "H2: tunnel->local pump for " + relay.portKey + " failed", e);
        } finally {
            relay.closeBoth();
        }
    }

    // ------------------------------------------------------------------
    // helpers
    // ------------------------------------------------------------------

    private ExecutorService executor() {
        synchronized (executorLock) {
            if (executor == null || executor.isShutdown()) {
                executor = Executors.newCachedThreadPool(new TunnelThreadFactory(threadSeq));
            }
            return executor;
        }
    }

    private void closeQuietly(ServerSocket server) {
        try {
            server.close();
        } catch (IOException ignored) {
            // Already closed.
        }
    }

    private void closeQuietly(Socket socket) {
        try {
            socket.close();
        } catch (IOException ignored) {
            // Already closed.
        }
    }

    /** One desired reverse forward, as announced by the server's {@code bound}. */
    private static final class ReverseForward {
        final int serverPort;
        final String targetHost;
        final int targetPort;

        ReverseForward(int serverPort, String targetHost, int targetPort) {
            this.serverPort = serverPort;
            this.targetHost = targetHost;
            this.targetPort = targetPort;
        }
    }

    /**
     * One live connection: its socket and the h2 stream carrying it.
     *
     * <p>Used by both directions. {@code portKey} is the device-side local port
     * for a {@code -L} relay and the server-side port for a {@code -R} one;
     * {@code reverse} says which, so a removal only tears down its own kind.
     */
    private final class Relay {
        final int portKey;
        final boolean reverse;
        final Socket socket;
        volatile TunnelConnection connection;
        private final AtomicBoolean closed = new AtomicBoolean(false);

        Relay(int portKey, boolean reverse, Socket socket) {
            this.portKey = portKey;
            this.reverse = reverse;
            this.socket = socket;
        }

        boolean isClosed() {
            return closed.get();
        }

        /** Tear down both ends exactly once, then stop tracking. */
        void closeBoth() {
            if (!closed.compareAndSet(false, true)) return;
            // Untrack first: a concurrent removeLocal()/close() iterating the
            // set must not be handed a relay that is already being torn down.
            relays.remove(this);
            TunnelConnection current = connection;
            if (current != null) {
                try {
                    current.close();
                } catch (RuntimeException ignored) {
                    // Already gone.
                }
            }
            try {
                socket.close();
            } catch (IOException ignored) {
                // Already closed.
            }
        }
    }

    /** Daemon threads so an idle tunnel never keeps the process alive. */
    private static final class TunnelThreadFactory implements ThreadFactory {
        private final AtomicInteger seq;

        TunnelThreadFactory(AtomicInteger seq) {
            this.seq = seq;
        }

        @Override
        public Thread newThread(Runnable r) {
            Thread thread = new Thread(r, "H2Tunnel-" + seq.incrementAndGet());
            thread.setDaemon(true);
            return thread;
        }
    }
}
