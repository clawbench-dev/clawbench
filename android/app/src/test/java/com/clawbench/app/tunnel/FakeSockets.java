package com.clawbench.app.tunnel;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * Fake local sockets for {@link H2PortForwardTransport}'s tests.
 *
 * <p>A real {@code ServerSocket} in a Robolectric test leaves the Gradle worker
 * un-exitable (measured while building T9), so the listener lifecycle is driven
 * entirely through these. Public so both the transport test and the service test
 * share one fixture.
 */
public final class FakeSockets {

    private FakeSockets() {
    }

    /** Supplies {@link FakeServerSocket}s and can fail a bind on demand. */
    public static final class Factory implements ServerSocketFactory {
        public final List<FakeServerSocket> created =
                Collections.synchronizedList(new ArrayList<>());
        public volatile boolean failNextBind = false;
        /** Dials the reverse ({@code -R}) targets; shared with the transport. */
        public final FakeDialer dialer = new FakeDialer();

        @Override
        public ServerSocket create() throws IOException {
            FakeServerSocket socket = new FakeServerSocket(this);
            created.add(socket);
            return socket;
        }
    }

    /**
     * Records the reverse ({@code -R}) target dials and can fail on demand.
     *
     * <p>{@link #nextSocket} lets a test hand the claim path a socket it holds
     * a reference to, so it can drive the pump.
     */
    public static final class FakeDialer implements LocalDialer {
        public final List<String> dialedHosts = Collections.synchronizedList(new ArrayList<>());
        public final List<Integer> dialedPorts = Collections.synchronizedList(new ArrayList<>());
        public volatile boolean failNextDial = false;
        public volatile FakeSocket nextSocket = null;
        /**
         * Runs after a dial is recorded but before the socket is returned, so a
         * test can deterministically change tunnel state "while the dial is in
         * flight" without racing the claim task.
         */
        public volatile Runnable afterDial = null;

        @Override
        public Socket dial(String host, int port) throws IOException {
            dialedHosts.add(host);
            dialedPorts.add(port);
            if (failNextDial) {
                failNextDial = false;
                throw new IOException("connection refused");
            }
            Runnable hook = afterDial;
            if (hook != null) hook.run();
            FakeSocket queued = nextSocket;
            nextSocket = null;
            return queued != null ? queued : new FakeSocket("");
        }

        public int dialCount() {
            return dialedHosts.size();
        }

        /**
         * Wait until {@code count} dials have been attempted, with a deadline.
         *
         * <p>Lets a test replace a {@code Thread.sleep} + "nothing happened"
         * assertion with a real wait: the sleep could expire before the claim
         * task ran, so the negative assertion passed vacuously. Awaiting the
         * dial proves the task actually reached the dialer before asserting
         * what it did (or did not) do next.
         */
        public boolean awaitDials(int count) throws InterruptedException {
            long deadline = System.currentTimeMillis() + FakeTunnelStream.WAIT_MS;
            while (dialedHosts.size() < count && System.currentTimeMillis() < deadline) {
                Thread.sleep(5);
            }
            return dialedHosts.size() >= count;
        }
    }

    /** A fake {@link ServerSocket} whose {@code accept()} the test triggers. */
    public static final class FakeServerSocket extends ServerSocket {
        public final AtomicBoolean closed = new AtomicBoolean(false);
        public volatile boolean bound = false;
        public volatile InetSocketAddress bindAddress;
        /** The thread blocked in {@code accept()} — proves whose pool it is. */
        public volatile Thread acceptThread;
        private final Factory factory;
        private final BlockingQueue<Socket> pending = new ArrayBlockingQueue<>(16);

        FakeServerSocket(Factory factory) throws IOException {
            // An unbound ServerSocket allocates no descriptor, and both bind()
            // and accept() are overridden below.
            super();
            this.factory = factory;
        }

        @Override
        public void bind(java.net.SocketAddress endpoint, int backlog) throws IOException {
            if (factory.failNextBind) {
                factory.failNextBind = false;
                throw new IOException("bind refused");
            }
            bound = true;
            bindAddress = (InetSocketAddress) endpoint;
        }

        @Override
        public void setReuseAddress(boolean on) {
        }

        @Override
        public Socket accept() throws IOException {
            acceptThread = Thread.currentThread();
            // Poll rather than block forever so close() is observed promptly.
            long deadline = System.currentTimeMillis() + FakeTunnelStream.WAIT_MS;
            while (true) {
                if (closed.get()) throw new IOException("Socket closed");
                try {
                    Socket socket = pending.poll(20, TimeUnit.MILLISECONDS);
                    if (socket != null) return socket;
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                    throw new IOException("interrupted", e);
                }
                if (System.currentTimeMillis() > deadline) {
                    throw new IOException("no pending connection");
                }
            }
        }

        @Override
        public void close() {
            closed.set(true);
        }

        @Override
        public boolean isClosed() {
            return closed.get();
        }

        /** Hand the accept loop a connection. */
        public void accept(Socket socket) {
            pending.add(socket);
        }
    }

    /** A fake local socket whose reads and writes the test drives. */
    public static final class FakeSocket extends Socket {
        private final BlockingQueue<Integer> localBytes = new ArrayBlockingQueue<>(64);
        private final List<Byte> written = Collections.synchronizedList(new ArrayList<>());
        public final AtomicBoolean closed = new AtomicBoolean(false);

        public FakeSocket(String bytesToRead) {
            for (byte b : bytesToRead.getBytes(StandardCharsets.UTF_8)) {
                localBytes.add((int) b);
            }
            localBytes.add(-1);
        }

        @Override
        public InputStream getInputStream() {
            return new InputStream() {
                // Sticky EOF, like a real socket: the pump's bulk read consumes
                // the terminator, so a one-shot marker would leave the next read
                // blocking instead of ending the pump.
                private boolean eof = false;

                @Override
                public int read() throws IOException {
                    if (eof) return -1;
                    try {
                        Integer next = localBytes.poll(FakeTunnelStream.WAIT_MS, TimeUnit.MILLISECONDS);
                        if (next == null) throw new IOException("no local bytes");
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

                @Override
                public int read(byte[] b, int off, int len) throws IOException {
                    // Return as soon as data is available, like a real socket.
                    // The inherited InputStream.read(byte[]) calls read() once
                    // more after the last queued byte, blocks for the full
                    // WAIT_MS and then *swallows* the timeout IOException — so
                    // the pump's write landed exactly on the tests' WAIT_MS
                    // deadline and raced it under load. Block for the first byte
                    // (that timeout is the fake's stand-in for an unreadable
                    // socket), then drain only what is already queued.
                    if (len == 0) return 0;
                    int first = read();
                    if (first == -1) return -1;
                    b[off] = (byte) first;
                    int count = 1;
                    Integer next;
                    while (count < len && (next = localBytes.poll()) != null) {
                        if (next == -1) {
                            eof = true;
                            break;
                        }
                        b[off + count++] = next.byteValue();
                    }
                    return count;
                }
            };
        }

        @Override
        public OutputStream getOutputStream() {
            return new OutputStream() {
                @Override
                public void write(int b) {
                    written.add((byte) b);
                }

                @Override
                public void flush() {
                }
            };
        }

        @Override
        public void close() {
            closed.set(true);
        }

        public boolean awaitClosed() throws InterruptedException {
            long deadline = System.currentTimeMillis() + FakeTunnelStream.WAIT_MS;
            while (!closed.get() && System.currentTimeMillis() < deadline) {
                Thread.sleep(10);
            }
            return closed.get();
        }

        /**
         * Wait until {@code expected} bytes have been written by the pump.
         *
         * <p>Waits for the exact payload rather than merely "something
         * arrived": a socket read may hand the pump a partial chunk, so a
         * non-empty wait followed by an exact-string assert could observe a
         * prefix. Waiting for the byte count is the pump's real completion
         * signal and is immune to chunking.
         */
        public boolean awaitLocalBytes(int expected) throws InterruptedException {
            long deadline = System.currentTimeMillis() + FakeTunnelStream.WAIT_MS;
            while (written.size() < expected && System.currentTimeMillis() < deadline) {
                Thread.sleep(5);
            }
            return written.size() >= expected;
        }

        public String localBytes() {
            StringBuilder sb = new StringBuilder();
            synchronized (written) {
                for (byte b : written) sb.append((char) b);
            }
            return sb.toString();
        }
    }
}
