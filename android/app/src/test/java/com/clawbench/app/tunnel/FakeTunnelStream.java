package com.clawbench.app.tunnel;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * A fake {@link TunnelStream} for the port-forward transport tests.
 *
 * <p>Public because several layers use it: {@code H2PortForwardTransportTest}
 * (the listener and the pumps) and {@code BackgroundServiceTransportTest} (the
 * service routing add/remove/disconnect through the abstraction). Sharing the
 * fixture is what keeps both from drifting.
 */
public final class FakeTunnelStream implements TunnelStream {

    public static final long WAIT_MS = 5000;

    public final List<String> openedHosts = Collections.synchronizedList(new ArrayList<>());
    public final List<Integer> openedPorts = Collections.synchronizedList(new ArrayList<>());
    public final List<FakeConnection> connections =
            Collections.synchronizedList(new ArrayList<>());
    public volatile boolean connected = true;
    public volatile boolean failNextOpen = false;
    public volatile boolean failConnect = false;
    public volatile boolean closed = false;
    /**
     * The {@code preferred} argument of every {@link #connect} call, oldest
     * first — lets a test prove the remembered transport is handed back.
     */
    public final List<TransportKind> connectPreferred = Collections.synchronizedList(new ArrayList<>());
    /**
     * The kind {@link #connect} reports when it succeeds. Null echoes the
     * preference (or {@link TransportKind#H2C} when there is none), which is the
     * pre-existing behaviour.
     */
    public volatile TransportKind connectResult = null;
    /**
     * When set, {@link #openStream} throws this instead of handing out a
     * connection. Unlike {@link #failNextOpen} it is sticky, so a test can make
     * every stream on a port fail (the accept-loop-survival case).
     */
    public volatile TunnelException streamFailure = null;

    // --- -R control plane -------------------------------------------------

    /** Ports passed to {@link #bind}; null-result binds are marked by {@link #failNextBind}. */
    public final List<Integer> boundPorts = Collections.synchronizedList(new ArrayList<>());
    /** Ports passed to {@link #unbind}. */
    public final List<Integer> unboundPorts = Collections.synchronizedList(new ArrayList<>());
    /** Tokens passed to {@link #openClaimStream}. */
    public final List<String> claimedTokens = Collections.synchronizedList(new ArrayList<>());
    /** Port the next bind answers with; defaults to echoing the request. */
    public volatile Integer nextBoundPort = null;
    public volatile boolean failNextBind = false;
    public volatile boolean failNextClaim = false;

    public final FakeControlStream control = new FakeControlStream();
    /** Number of times {@link #openControlStream} was called. */
    public final AtomicInteger controlOpens = new AtomicInteger();
    /** When set, the next claim stream returns this instead of a fresh one. */
    public volatile FakeConnection queuedClaimConnection = null;

    private final AtomicInteger handedOut = new AtomicInteger();

    /**
     * Pre-create the connection the next {@code openStream} returns, so a test
     * can hold a reference before the accept loop runs.
     */
    public FakeConnection queueConnection() {
        FakeConnection connection = new FakeConnection();
        connections.add(connection);
        return connection;
    }

    public void awaitOpenedStreams(int count) throws InterruptedException {
        long deadline = System.currentTimeMillis() + WAIT_MS;
        while (openedHosts.size() < count && System.currentTimeMillis() < deadline) {
            Thread.sleep(10);
        }
    }

    /** Wait for {@code count} claim streams to be opened. */
    public boolean awaitClaims(int count) throws InterruptedException {
        long deadline = System.currentTimeMillis() + WAIT_MS;
        while (claimedTokens.size() < count && System.currentTimeMillis() < deadline) {
            Thread.sleep(10);
        }
        return claimedTokens.size() >= count;
    }

    /** Wait until {@code count} target dials have been recorded. */
    public boolean awaitBinds(int count) throws InterruptedException {
        long deadline = System.currentTimeMillis() + WAIT_MS;
        while (boundPorts.size() < count && System.currentTimeMillis() < deadline) {
            Thread.sleep(10);
        }
        return boundPorts.size() >= count;
    }

    @Override
    public TransportKind connect(String serverUrl, TransportKind preferred) {
        connectPreferred.add(preferred);
        if (failConnect) {
            connected = false;
            return null;
        }
        connected = true;
        if (connectResult != null) return connectResult;
        return preferred != null ? preferred : TransportKind.H2C;
    }

    @Override
    public boolean isConnected() {
        return connected && !closed;
    }

    @Override
    public TransportKind getKind() {
        if (!isConnected()) return null;
        return connectResult != null ? connectResult : TransportKind.H2C;
    }

    @Override
    public TunnelConnection openStream(String host, int port) throws TunnelException {
        openedHosts.add(host);
        openedPorts.add(port);
        if (streamFailure != null) {
            throw streamFailure;
        }
        if (failNextOpen) {
            failNextOpen = false;
            throw new TunnelException(TunnelErrorKind.TARGET_UNREACHABLE, "dial failed");
        }
        return nextConnection();
    }

    @Override
    public TunnelConnection openClaimStream(String token) throws TunnelException {
        claimedTokens.add(token);
        if (failNextClaim) {
            failNextClaim = false;
            throw new TunnelException(TunnelErrorKind.AUTH, "claim rejected");
        }
        FakeConnection queued = queuedClaimConnection;
        if (queued != null) {
            queuedClaimConnection = null;
            return queued;
        }
        return nextConnection();
    }

    private FakeConnection nextConnection() {
        synchronized (connections) {
            int index = handedOut.getAndIncrement();
            if (index < connections.size()) return connections.get(index);
            FakeConnection fresh = new FakeConnection();
            connections.add(fresh);
            return fresh;
        }
    }

    @Override
    public TunnelControlStream openControlStream() {
        controlOpens.incrementAndGet();
        return control;
    }

    @Override
    public Integer bind(int serverPort) {
        boundPorts.add(serverPort);
        if (failNextBind) {
            failNextBind = false;
            return null;
        }
        Integer override = nextBoundPort;
        nextBoundPort = null;
        return override != null ? override : serverPort;
    }

    @Override
    public void unbind(int serverPort) {
        unboundPorts.add(serverPort);
    }

    @Override
    public void close() {
        closed = true;
        control.close();
    }

    @Override
    public String getLastError() {
        return "";
    }

    @Override
    public TunnelErrorKind getLastErrorKind() {
        return TunnelErrorKind.UNKNOWN;
    }

    @Override
    public boolean hasActiveStreams() {
        return false;
    }

    @Override
    public int activeStreamCount() {
        return 0;
    }

    /**
     * A fake control stream whose {@code incoming} notifications the test
     * pushes, and whose sends the test inspects.
     */
    public static final class FakeControlStream implements TunnelControlStream {
        public final List<ControlMessage> sent = Collections.synchronizedList(new ArrayList<>());
        private final List<MessageHandler> handlers = new CopyOnWriteArrayList<>();
        private final List<Runnable> closeHandlers = new CopyOnWriteArrayList<>();
        private final AtomicBoolean closed = new AtomicBoolean(false);

        @Override
        public boolean send(ControlMessage msg) {
            if (closed.get()) return false;
            sent.add(msg);
            return true;
        }

        @Override
        public void onMessage(MessageHandler handler) {
            handlers.add(handler);
        }

        @Override
        public void onClose(Runnable handler) {
            closeHandlers.add(handler);
        }

        @Override
        public void close() {
            if (closed.compareAndSet(false, true)) {
                for (Runnable handler : closeHandlers) handler.run();
            }
        }

        @Override
        public boolean isClosed() {
            return closed.get();
        }

        @Override
        public TunnelErrorKind lastErrorKind() {
            return null;
        }

        @Override
        public String lastErrorMessage() {
            return null;
        }

        /** Deliver a server -> client line to every registered handler. */
        public void deliver(ControlMessage msg) {
            for (MessageHandler handler : handlers) handler.onMessage(msg);
        }

        /** How many message handlers are registered (one per wiring). */
        public int handlerCount() {
            return handlers.size();
        }

        /** Build and deliver an {@code incoming} notification. */
        public void incoming(int port, String token) {
            deliver(ControlMessage.parse(
                    "{\"type\":\"incoming\",\"port\":" + port + ",\"token\":\"" + token + "\"}"));
        }
    }

    /** A fake connection whose two directions the test drives directly. */
    public static final class FakeConnection implements TunnelConnection {
        private final List<Byte> requestBytes = Collections.synchronizedList(new ArrayList<>());
        private final BlockingQueue<Integer> responseBytes = new ArrayBlockingQueue<>(64);
        private final CountDownLatch closeWriteCalled = new CountDownLatch(1);
        private final AtomicBoolean closed = new AtomicBoolean(false);
        private volatile IOException responseFailure = null;
        private volatile IOException writeFailure = null;

        @Override
        public InputStream getInputStream() {
            return new InputStream() {
                // Sticky EOF, like a real socket: the pump's bulk read consumes
                // the terminator, so a one-shot marker would leave the next read
                // blocking instead of ending the pump.
                private boolean eof = false;

                @Override
                public int read() throws IOException {
                    IOException failure = responseFailure;
                    if (failure != null) throw failure;
                    if (eof) return -1;
                    try {
                        Integer next = responseBytes.poll(WAIT_MS, TimeUnit.MILLISECONDS);
                        if (next == null) throw new IOException("no response bytes");
                        if (next == -1) {
                            eof = true;
                            return -1;
                        }
                        return next;
                    } catch (InterruptedException e) {
                        Thread.currentThread().interrupt();
                        throw new IOException("interrupted", e);
                    }
                }
            };
        }

        @Override
        public OutputStream getOutputStream() {
            return new OutputStream() {
                @Override
                public void write(int b) throws IOException {
                    IOException failure = writeFailure;
                    if (failure != null) throw failure;
                    requestBytes.add((byte) b);
                }

                @Override
                public void flush() throws IOException {
                    IOException failure = writeFailure;
                    if (failure != null) throw failure;
                }
            };
        }

        /** Make the response direction fail with {@code e} on the next read. */
        public void failResponse(IOException e) {
            responseFailure = e;
        }

        /** Make the request direction fail with a reset on every write. */
        public void failWrites() {
            writeFailure = new IOException("stream reset");
        }

        @Override
        public void closeWrite() {
            closeWriteCalled.countDown();
        }

        @Override
        public void close() {
            if (closed.compareAndSet(false, true)) {
                responseBytes.add(-1);
            }
        }

        @Override
        public boolean isClosed() {
            return closed.get();
        }

        /** Push server->client bytes; the pump copies them to the local socket. */
        public void pushResponse(String data) {
            for (byte b : data.getBytes(StandardCharsets.UTF_8)) {
                responseBytes.add((int) b);
            }
        }

        /** End the response direction. */
        public void endResponse() {
            responseBytes.add(-1);
        }

        public boolean awaitCloseWrite() throws InterruptedException {
            return closeWriteCalled.await(WAIT_MS, TimeUnit.MILLISECONDS);
        }

        public boolean awaitRequestBytes() throws InterruptedException {
            long deadline = System.currentTimeMillis() + WAIT_MS;
            while (requestBytes.isEmpty() && System.currentTimeMillis() < deadline) {
                Thread.sleep(10);
            }
            return !requestBytes.isEmpty();
        }

        public String requestBytes() {
            StringBuilder sb = new StringBuilder();
            synchronized (requestBytes) {
                for (byte b : requestBytes) sb.append((char) b);
            }
            return sb.toString();
        }
    }
}
