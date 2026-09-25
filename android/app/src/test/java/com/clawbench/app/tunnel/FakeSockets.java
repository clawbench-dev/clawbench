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

        @Override
        public Socket dial(String host, int port) throws IOException {
            dialedHosts.add(host);
            dialedPorts.add(port);
            if (failNextDial) {
                failNextDial = false;
                throw new IOException("connection refused");
            }
            FakeSocket queued = nextSocket;
            nextSocket = null;
            return queued != null ? queued : new FakeSocket("");
        }

        public int dialCount() {
            return dialedHosts.size();
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

        public boolean awaitLocalBytes() throws InterruptedException {
            long deadline = System.currentTimeMillis() + FakeTunnelStream.WAIT_MS;
            while (written.isEmpty() && System.currentTimeMillis() < deadline) {
                Thread.sleep(10);
            }
            return !written.isEmpty();
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
