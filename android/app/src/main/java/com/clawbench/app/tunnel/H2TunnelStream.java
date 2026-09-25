package com.clawbench.app.tunnel;

import androidx.annotation.Nullable;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.security.cert.X509Certificate;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;

import javax.net.ssl.SSLContext;
import javax.net.ssl.X509TrustManager;

import okhttp3.Call;
import okhttp3.HttpUrl;
import okhttp3.OkHttpClient;
import okhttp3.Protocol;
import okhttp3.Request;
import okhttp3.Response;
import okhttp3.ResponseBody;
import okio.BufferedSink;
import okio.BufferedSource;

/**
 * The HTTP/2 stream tunnel transport, over OkHttp.
 *
 * <p>Wire protocol (server: {@code internal/handler/tunnel_stream.go} and
 * {@code tunnel_control.go}): one forwarded TCP connection is one h2 stream;
 * the request body carries client→server bytes and the response body carries
 * server→client bytes, both streaming at once. The whole tunnel therefore needs
 * only the main port.
 *
 * <h3>Why each of the four hard constraints is implemented the way it is</h3>
 *
 * <ol>
 *   <li><b>{@code isDuplex() == true}</b> — see {@link DuplexRequestBody}. The
 *       body publishes the request sink and returns; anything else makes the
 *       response direction unreachable while the request direction is open.</li>
 *   <li><b>Blocking {@code Call.execute()}, one dedicated thread per stream</b> —
 *       OkHttp's {@code Dispatcher} caps <em>async</em> calls at
 *       {@code maxRequestsPerHost = 5}, keyed on host name alone (not path or
 *       port), so the sixth concurrent {@code enqueue()} sits queued forever.
 *       {@code execute()} adds the call to {@code runningSyncCalls} with no
 *       count check at all, so a dedicated thread per stream scales. The pool
 *       is bounded (see {@link #STREAM_POOL_SIZE}) because each stream costs a
 *       thread; {@code -L} forwards are many-connections-per-port, so an
 *       unbounded cached pool would be a thread bomb on a busy device.</li>
 *   <li><b>{@code readTimeout(0)} and {@code writeTimeout(0)}</b> — the default
 *       10s would fire on an idle stream, and OkHttp's h2 stream timeout
 *       responds by RST_STREAM(CANCEL), killing a healthy tunnel whenever the
 *       forwarded target is simply quiet. A connect/headers budget is applied
 *       per call instead (see {@link #HEADERS_TIMEOUT_MS}) and cleared once the
 *       response headers are in, so it can never fire mid-stream.</li>
 *   <li><b>One shared {@code OkHttpClient}</b> — connection pools are
 *       per-client, so control and data streams must come from the same
 *       instance to share one TCP connection. That is essential for {@code -R}:
 *       a claim token is scoped to the h2 connection that minted it, so the
 *       claim stream must ride the control stream's connection.</li>
 * </ol>
 *
 * <p>The client is intentionally separate from {@code connectNativeWs}'s
 * (which builds a new client per connection): a tunnel failure must not disturb
 * the native event WebSocket, and vice versa.
 *
 * <p>All methods block and must be called off the main thread.
 */
public final class H2TunnelStream implements TunnelStream {

    private static final String TAG = "H2Tunnel";

    private static final String STREAM_PATH = "/api/tunnel/stream";
    private static final String CONTROL_PATH = "/api/tunnel/control";
    /** The liveness probe. Public (no auth), so a 401/403 cannot masquerade as a protocol failure. */
    private static final String PROBE_PATH = "/api/ssh/info";

    /**
     * Budget for establishing a stream: TCP connect, TLS handshake and response
     * headers. Generous enough for a slow phone network, short enough that a
     * black-holed server does not pin a stream thread for minutes.
     */
    static final int HEADERS_TIMEOUT_MS = 20_000;

    /** Budget for the probe round-trip during {@link #connect}. */
    static final int PROBE_TIMEOUT_MS = 8_000;

    /**
     * Dedicated thread budget. Every open stream holds one thread for its whole
     * life, so this is simultaneously the maximum number of concurrent streams.
     * It sits below the server's 250-stream h2 limit on purpose: exceeding the
     * local pool surfaces as a clean {@link TunnelErrorKind#LIMIT} instead of
     * silently queueing behind the server's MAX_CONCURRENT_STREAMS.
     */
    static final int STREAM_POOL_SIZE = 128;

    /** Time a caller waits for a pool slot before giving up. */
    static final int STREAM_SLOT_WAIT_MS = 10_000;

    /**
     * Bound on a control line. The server's scanner caps lines at 64 KiB;
     * matching that here means a garbled peer cannot grow the client buffer
     * without limit, and the overflow surfaces as an error instead of a hang.
     */
    static final int MAX_CONTROL_LINE = 64 * 1024;

    /** Upper bound on the direct-sink write path, so one huge write cannot pin a thread. */
    private static final int WRITE_CHUNK = 64 * 1024;

    private final TunnelPlatform platform;

    /**
     * Test seam: when non-null, every call is created through it instead of the
     * real client, so unit tests can capture requests and feed canned
     * responses without a network. Null in production.
     */
    @Nullable
    private final CallFactory callFactoryOverride;

    /** Effective stream limit; equals {@link #STREAM_POOL_SIZE} in production. */
    private final int streamPoolSize;

    /**
     * Created lazily per transport kind, because OkHttp bakes the protocol list
     * into the client: {@code H2_PRIOR_KNOWLEDGE} for h2c and {@code HTTP_2}
     * for TLS. Exactly one of these is live per session; the map exists so a
     * re-probe after a transport switch does not leak clients.
     */
    private final Map<TransportKind, OkHttpClient> clients = new ConcurrentHashMap<>();

    private final ThreadPoolExecutor streamPool;
    private final Set<Call> liveCalls = Collections.newSetFromMap(new ConcurrentHashMap<Call, Boolean>());

    /**
     * Connections handed out and not yet closed. Tracked alongside the calls so
     * a session teardown can flip each connection's own closed flag: cancelling
     * the call stops the I/O, but a caller still holding the connection must
     * also observe {@code isClosed() == true} and stop using it.
     */
    private final Set<H2Connection> liveConnections =
            Collections.newSetFromMap(new ConcurrentHashMap<H2Connection, Boolean>());

    /** Guards session-level state transitions (connect/close). */
    private final Object sessionLock = new Object();

    /**
     * Serializes connect attempts. Separate from {@link #sessionLock} because a
     * connect performs network I/O with a multi-second budget, and holding the
     * session lock across it would stall {@link #close()} (and therefore every
     * stream teardown) behind a slow probe.
     */
    private final Object connectLock = new Object();

    private volatile ServerTarget target;
    private volatile TransportKind kind;
    private volatile boolean closed = false;
    private volatile String lastError = "";
    private volatile TunnelErrorKind lastErrorKind = TunnelErrorKind.UNKNOWN;

    @Nullable
    private volatile H2ControlStream controlStream;

    public H2TunnelStream(TunnelPlatform platform) {
        this(platform, null);
    }

    /**
     * @param callFactoryOverride test seam; {@code null} in production. Lets a
     *                            unit test observe the exact {@link Request}
     *                            each stream builds and drive the stream
     *                            lifecycle against a canned {@link Response}.
     */
    H2TunnelStream(TunnelPlatform platform, @Nullable CallFactory callFactoryOverride) {
        this(platform, callFactoryOverride, STREAM_POOL_SIZE);
    }

    /**
     * @param streamPoolSize maximum concurrent streams; a test seam so the
     *                       saturation path is reachable without opening
     *                       {@link #STREAM_POOL_SIZE} real streams.
     */
    H2TunnelStream(TunnelPlatform platform, @Nullable CallFactory callFactoryOverride, int streamPoolSize) {
        this.platform = platform;
        this.callFactoryOverride = callFactoryOverride;
        this.streamPoolSize = streamPoolSize;
        this.streamPool = new ThreadPoolExecutor(
                // Core size 0: an idle tunnel keeps no threads alive. Every
                // stream still gets a fresh thread up to the max, because the
                // queue is a SynchronousQueue.
                0, streamPoolSize,
                30, TimeUnit.SECONDS,
                new java.util.concurrent.SynchronousQueue<Runnable>(),
                new StreamThreadFactory());
        // Do not let an idle tunnel thread keep the process alive.
        streamPool.allowCoreThreadTimeOut(true);
    }

    // ------------------------------------------------------------------
    // connect / lifecycle
    // ------------------------------------------------------------------

    /**
     * Connect to {@code serverUrl}, trying {@code preferred} first.
     *
     * <p>The order is the design doc's priority chain with the remembered
     * transport promoted: a plaintext deployment fails the TLS probe at the
     * handshake (fast, not a timeout) and then succeeds on h2c, and the caller
     * remembers h2c so subsequent reconnects skip the wasted probe.
     */
    @Override
    @Nullable
    public TransportKind connect(String serverUrl, @Nullable TransportKind preferred) {
        ServerTarget serverTarget = ServerTarget.parse(serverUrl);
        if (serverTarget == null) {
            recordFailure(TunnelErrorKind.UNKNOWN, "invalid server url");
            return null;
        }
        // Serialize attempts, but do not hold the session lock across the
        // probes: they block for up to PROBE_TIMEOUT_MS each.
        synchronized (connectLock) {
            synchronized (sessionLock) {
                // Reuse a healthy session for the same server regardless of the
                // preference: reconnecting just to switch transports would drop
                // every live forwarded connection.
                if (isConnected() && target != null && target.host.equals(serverTarget.host)
                        && target.port == serverTarget.port) {
                    return kind;
                }
                closeInternal();
                closed = false;
                target = serverTarget;
                lastError = "";
                lastErrorKind = TunnelErrorKind.UNKNOWN;
            }

            List<TransportKind> order = probeOrder(preferred);
            String lastMessage = "";
            TunnelErrorKind lastKind = TunnelErrorKind.UNKNOWN;
            for (TransportKind candidate : order) {
                try {
                    probe(candidate, serverTarget);
                    synchronized (sessionLock) {
                        // A close() during the probe wins: the session it tore
                        // down must not be resurrected by this late success.
                        if (closed || target != serverTarget) {
                            return null;
                        }
                        kind = candidate;
                        log("i", "connected via " + candidate.wireName() + " to " + serverTarget);
                        return candidate;
                    }
                } catch (TunnelException e) {
                    lastMessage = e.getMessage();
                    lastKind = e.kind();
                    log("w", candidate.wireName() + " probe failed: " + lastMessage);
                }
            }
            // Every transport failed: release whatever the last probe left behind.
            synchronized (sessionLock) {
                closeInternal();
                recordFailure(lastKind, lastMessage.isEmpty() ? "no transport available" : lastMessage);
            }
            return null;
        }
    }

    private static List<TransportKind> probeOrder(@Nullable TransportKind preferred) {
        // TLS first by default (design doc §2.3). The remembered kind, when
        // given, is promoted to the front.
        List<TransportKind> order = new ArrayList<>(Arrays.asList(TransportKind.TLS, TransportKind.H2C));
        if (preferred != null && order.remove(preferred)) {
            order.add(0, preferred);
        }
        return order;
    }

    /**
     * Prove that the server really speaks HTTP/2 on {@code candidate}.
     *
     * <p>A completed TCP connect proves nothing: OkHttp's connection is only
     * known to be h2 once a response arrives over it. Against a plain HTTP/1.1
     * server an h2c client fails with {@code ConnectionShutdownException} — but
     * only when the first request is attempted, which is exactly this probe.
     * Skipping it would leave the h2c fallback unreachable and the tunnel
     * hanging on a half-dead session.
     *
     * <p>Any real HTTP status is accepted, including 401: the server answered
     * over h2, so framing works, and the cookie is a separate concern.
     */
    private void probe(TransportKind candidate, ServerTarget serverTarget) throws TunnelException {
        HttpUrl url = serverTarget.base(candidate).encodedPath(PROBE_PATH).build();
        Request request = new Request.Builder().url(url).get().build();
        Call call = newCall(candidate, request);
        call.timeout().timeout(PROBE_TIMEOUT_MS, TimeUnit.MILLISECONDS);
        try (Response response = call.execute()) {
            // execute() returning at all means the h2 session framed a
            // response; a protocol mismatch fails here.
            if (response.protocol() != Protocol.HTTP_2
                    && response.protocol() != Protocol.H2_PRIOR_KNOWLEDGE) {
                throw new TunnelException(TunnelErrorKind.PROTOCOL,
                        "server negotiated " + response.protocol() + " instead of HTTP/2");
            }
            int code = response.code();
            if (code >= 100 && code <= 599) return;
            throw new TunnelException(TunnelErrorKind.HTTP_ERROR,
                    "unexpected probe status " + code, code, null);
        } catch (IOException e) {
            throw asTunnelException(e, "probe failed");
        }
    }

    @Override
    public boolean isConnected() {
        if (closed) return false;
        ServerTarget t = target;
        return t != null && kind != null;
    }

    @Override
    @Nullable
    public TransportKind getKind() {
        return isConnected() ? kind : null;
    }

    @Override
    public void close() {
        synchronized (sessionLock) {
            closeInternal();
            closed = true;
        }
    }

    private void closeInternal() {
        H2ControlStream control = controlStream;
        controlStream = null;
        if (control != null) {
            control.close();
        }
        // Cancel every live stream. Call.cancel() on h2 is an RST_STREAM scoped
        // to that stream, so this drops exactly the tunnel's streams and leaves
        // any other traffic on the shared connection untouched.
        for (Call call : new ArrayList<>(liveCalls)) {
            try {
                call.cancel();
            } catch (RuntimeException ignored) {
                // Already finished.
            }
        }
        liveCalls.clear();
        // Flip each connection's own flag too: cancelling the call stops the
        // I/O, but a caller still holding the connection would otherwise see
        // isClosed() == false and keep using a dead stream.
        for (H2Connection connection : new ArrayList<>(liveConnections)) {
            connection.markClosedBySession();
        }
        liveConnections.clear();
        for (OkHttpClient client : clients.values()) {
            try {
                client.connectionPool().evictAll();
            } catch (RuntimeException ignored) {
                // Best-effort.
            }
            try {
                client.dispatcher().executorService().shutdown();
            } catch (RuntimeException ignored) {
                // Best-effort.
            }
        }
        clients.clear();
        kind = null;
        target = null;
    }

    @Override
    public String getLastError() {
        return lastError;
    }

    @Override
    public TunnelErrorKind getLastErrorKind() {
        return lastErrorKind;
    }

    @Override
    public boolean hasActiveStreams() {
        return !liveCalls.isEmpty();
    }

    @Override
    public int activeStreamCount() {
        return liveCalls.size();
    }

    // ------------------------------------------------------------------
    // streams
    // ------------------------------------------------------------------

    @Override
    public TunnelConnection openStream(String host, int port) throws TunnelException {
        HttpUrl url = target().base(kind())
                .encodedPath(STREAM_PATH)
                .addQueryParameter("host", host == null ? "" : host)
                .addQueryParameter("port", Integer.toString(port))
                .build();
        return openConnection(url, "open stream " + host + ":" + port);
    }

    @Override
    public TunnelConnection openClaimStream(String token) throws TunnelException {
        if (token == null || token.isEmpty()) {
            throw new TunnelException(TunnelErrorKind.AUTH, "missing claim token");
        }
        HttpUrl url = target().base(kind())
                .encodedPath(STREAM_PATH)
                .addQueryParameter("claim", token)
                .build();
        return openConnection(url, "claim stream");
    }

    @Override
    public TunnelControlStream openControlStream() throws TunnelException {
        H2ControlStream existing = controlStream;
        if (existing != null && !existing.isClosed()) {
            return existing;
        }
        // Build outside sessionLock: the constructor blocks until the response
        // headers arrive, and holding the session lock for that long would
        // stall close() (and therefore every stream teardown) behind it.
        HttpUrl url = target().base(kind()).encodedPath(CONTROL_PATH).build();
        H2ControlStream stream = new H2ControlStream(url);
        synchronized (sessionLock) {
            // Two concurrent callers would otherwise each open a control
            // stream, and two control streams mean two competing sets of bind
            // bookkeeping. The loser closes the one it just built.
            if (closed || !isConnected()) {
                stream.close();
                throw new TunnelException(TunnelErrorKind.CLOSED, "tunnel not connected");
            }
            H2ControlStream raced = controlStream;
            if (raced != null && !raced.isClosed()) {
                stream.close();
                return raced;
            }
            controlStream = stream;
            return stream;
        }
    }

    @Override
    public Integer bind(int serverPort) {
        H2ControlStream control;
        try {
            control = (H2ControlStream) openControlStream();
        } catch (TunnelException e) {
            recordFailure(e.kind(), e.getMessage());
            return null;
        }
        return control.bind(serverPort);
    }

    @Override
    public void unbind(int serverPort) {
        H2ControlStream control = controlStream;
        // Fire-and-forget, mirroring JSch's delPortForwardingR: the caller is
        // synchronous and the server's `unbound` acknowledgement carries
        // nothing it needs to wait for.
        if (control != null && !control.isClosed()) {
            control.send(ControlMessage.unbind(serverPort));
        }
    }

    /**
     * Open one data stream and wait for its response headers.
     *
     * <p>The call runs on a dedicated pool thread because it blocks until the
     * response headers arrive (the server writes 200 only after it has dialed
     * the target, and 502 when it could not). The caller thread waits on a
     * latch rather than executing it, so a stuck dial cannot pin the caller
     * (which is the local accept loop in T10).
     */
    private TunnelConnection openConnection(HttpUrl url, String what) throws TunnelException {
        if (!isConnected()) {
            throw new TunnelException(TunnelErrorKind.CLOSED, "tunnel not connected");
        }
        ExecutorService pool = streamPool;
        StreamCall streamCall = new StreamCall(url, what);
        try {
            // The Future is deliberately not retained: the task publishes its
            // own result through the latch, and cancellation goes through the
            // Call (RST_STREAM), not through interrupt.
            pool.submit(streamCall);
        } catch (RejectedExecutionException e) {
            throw new TunnelException(TunnelErrorKind.LIMIT,
                    "stream pool saturated (" + streamPoolSize + " streams)", e);
        }
        if (!streamCall.await()) {
            throw new TunnelException(TunnelErrorKind.TIMEOUT,
                    what + " timed out after " + STREAM_SLOT_WAIT_MS + "ms");
        }
        if (streamCall.failure != null) throw streamCall.failure;
        if (streamCall.connection == null) {
            throw new TunnelException(TunnelErrorKind.UNKNOWN, what + " produced no stream");
        }
        return streamCall.connection;
    }

    /** The blocking body of {@link #openConnection}, run on a pool thread. */
    private final class StreamCall implements Runnable {
        private final HttpUrl url;
        private final String what;
        private final java.util.concurrent.CountDownLatch done = new java.util.concurrent.CountDownLatch(1);

        volatile TunnelConnection connection;
        volatile TunnelException failure;

        StreamCall(HttpUrl url, String what) {
            this.url = url;
            this.what = what;
        }

        boolean await() {
            try {
                return done.await(STREAM_SLOT_WAIT_MS, TimeUnit.MILLISECONDS);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return false;
            }
        }

        @Override
        public void run() {
            DuplexRequestBody body = new DuplexRequestBody();
            Call call;
            try {
                call = newCall(kind(), duplexRequest(url, body));
            } catch (TunnelException e) {
                failure = e;
                return; // the finally block releases the latch
            }
            call.timeout().timeout(HEADERS_TIMEOUT_MS, TimeUnit.MILLISECONDS);
            liveCalls.add(call);
            try {
                Response response = call.execute();
                int code = response.code();
                if (code != 200) {
                    TunnelErrorKind errorKind = TunnelErrorKind.fromStatus(code);
                    response.close();
                    call.cancel();
                    throw new TunnelException(errorKind,
                            what + " rejected with HTTP " + code, code, null);
                }
                // Headers are in: drop the budget so an idle stream is never
                // killed by it (readTimeout/writeTimeout are already 0).
                call.timeout().timeout(0, TimeUnit.MILLISECONDS);
                BufferedSink sink = body.sink();
                if (sink == null) {
                    response.close();
                    call.cancel();
                    throw new TunnelException(TunnelErrorKind.PROTOCOL,
                            what + ": request body never received its sink");
                }
                H2Connection opened = new H2Connection(call, response, sink);
                liveConnections.add(opened);
                connection = opened;
            } catch (TunnelException e) {
                failure = e;
                liveCalls.remove(call);
            } catch (IOException e) {
                failure = asTunnelException(e, what + " failed");
                liveCalls.remove(call);
            } catch (RuntimeException e) {
                failure = new TunnelException(TunnelErrorKind.UNKNOWN, what + " failed: " + e, e);
                liveCalls.remove(call);
            } finally {
                done.countDown();
            }
        }
    }

    /**
     * One live h2 stream, exposed as a pair of blocking Java streams.
     *
     * <p>Both adapters delegate straight to the OkHttp response source and the
     * request sink, which is what makes backpressure automatic: a
     * {@code write()} blocks in OkHttp's h2 framing sink once the flow-control
     * window is exhausted, and a {@code read()} not being called stops
     * WINDOW_UPDATEs, so the server-side pump blocks in its own write.
     */
    private final class H2Connection implements TunnelConnection {

        private final Call call;
        private final Response response;
        private final BufferedSink sink;
        private final BufferedSource source;
        private final AtomicBoolean closedWrite = new AtomicBoolean(false);
        private final AtomicBoolean closed = new AtomicBoolean(false);

        H2Connection(Call call, Response response, BufferedSink sink) {
            this.call = call;
            this.response = response;
            this.sink = sink;
            ResponseBody body = response.body();
            if (body == null) throw new IllegalStateException("stream response has no body");
            this.source = body.source();
        }

        @Override
        public InputStream getInputStream() {
            return new InputStream() {
                @Override
                public int read() throws IOException {
                    try {
                        int b = source.readByte() & 0xFF;
                        return b;
                    } catch (java.io.EOFException e) {
                        return -1;
                    } catch (IOException e) {
                        throw translate(e);
                    }
                }

                @Override
                public int read(byte[] b, int off, int len) throws IOException {
                    if (len == 0) return 0;
                    try {
                        int n = source.read(b, off, len);
                        return n;
                    } catch (IOException e) {
                        throw translate(e);
                    }
                }

                @Override
                public void close() {
                    H2Connection.this.close();
                }
            };
        }

        @Override
        public OutputStream getOutputStream() {
            return new OutputStream() {
                @Override
                public void write(int b) throws IOException {
                    write(new byte[]{(byte) b}, 0, 1);
                }

                @Override
                public void write(byte[] b, int off, int len) throws IOException {
                    if (closedWrite.get()) {
                        // A late write after a legitimate half-close (the peer
                        // closed while bytes were in flight) must be dropped,
                        // not surface as a stream error.
                        return;
                    }
                    int remaining = len;
                    int position = off;
                    try {
                        while (remaining > 0) {
                            int n = Math.min(remaining, WRITE_CHUNK);
                            sink.write(b, position, n);
                            sink.flush();
                            position += n;
                            remaining -= n;
                        }
                    } catch (IOException e) {
                        throw translate(e);
                    } catch (IllegalStateException e) {
                        // Lost the race with closeWrite()/close(): the sink is
                        // gone and the bytes can no longer be delivered.
                        closedWrite.set(true);
                    }
                }

                @Override
                public void flush() throws IOException {
                    // After a half-close the sink is closed, and flushing it
                    // throws IllegalStateException from okio rather than an
                    // IOException. A flush that races the local EOF must be a
                    // no-op, not a crash in a caller that only handles
                    // IOException.
                    if (closedWrite.get() || closed.get()) return;
                    try {
                        sink.flush();
                    } catch (IOException e) {
                        throw translate(e);
                    } catch (IllegalStateException e) {
                        // Lost the race with closeWrite(): nothing left to flush.
                    }
                }

                @Override
                public void close() throws IOException {
                    H2Connection.this.closeWrite();
                }
            };
        }

        @Override
        public void closeWrite() throws IOException {
            if (!closedWrite.compareAndSet(false, true)) return;
            // Closing the request-body sink sends END_STREAM, which ends only
            // the local half of the h2 stream: the response direction stays
            // readable so the remote can finish replying (TCP half-close).
            try {
                sink.close();
            } catch (IOException e) {
                throw translate(e);
            }
        }

        @Override
        public void close() {
            if (!closed.compareAndSet(false, true)) return;
            // Half-close first when possible so a clean shutdown still sends
            // END_STREAM; then cancel, which is the RST_STREAM that actually
            // frees the stream and its thread.
            try {
                closeWrite();
            } catch (IOException ignored) {
                // The stream may already be gone.
            }
            try {
                response.close();
            } catch (RuntimeException ignored) {
                // Already closed.
            }
            try {
                call.cancel();
            } catch (RuntimeException ignored) {
                // Already finished.
            }
            liveCalls.remove(call);
            liveConnections.remove(this);
        }

        /** Mark closed without re-cancelling; the session already did that. */
        void markClosedBySession() {
            closed.set(true);
            closedWrite.set(true);
        }

        @Override
        public boolean isClosed() {
            return closed.get();
        }

        private IOException translate(IOException e) {
            TunnelErrorKind errorKind = TunnelErrorKind.of(e);
            if (e instanceof TunnelException) return e;
            // A locally-closed stream reports a cancelled/EOF IOException; the
            // caller asked for that, so it is not a transport failure.
            if (closed.get()) return new TunnelException(TunnelErrorKind.CLOSED, "stream closed");
            return new TunnelException(errorKind, e.getMessage() == null ? errorKind.name() : e.getMessage(), e);
        }
    }

    // ------------------------------------------------------------------
    // control stream
    // ------------------------------------------------------------------

    /**
     * The {@code -R} control plane over one h2 stream.
     *
     * <p>Runs its own blocking {@code execute()} on a pool thread, publishes
     * the request sink the same way a data stream does, and reads NDJSON lines
     * on that thread. Bind replies are correlated through a queue, not a
     * port-keyed map: a {@code bind(0)} is answered with the OS-assigned port,
     * so the reply carries no key to match on.
     */
    private final class H2ControlStream implements TunnelControlStream {

        private final java.util.concurrent.CountDownLatch ready = new java.util.concurrent.CountDownLatch(1);
        /**
         * Pending bind requests, oldest first. The server's control loop is
         * sequential and writes {@code bound}/{@code bind_err} before reading
         * the next command, so replies arrive in request order. A queue of
         * waiters rather than a port-keyed map is mandatory: a {@code bind(0)}
         * is answered with the OS-assigned port, so that reply carries no key
         * to correlate on.
         *
         * <p>The waiter is enqueued <em>before</em> the command is sent, so a
         * reply can never arrive before there is something to complete.
         */
        private final java.util.Queue<BindWaiter> bindWaiters = new java.util.concurrent.ConcurrentLinkedQueue<>();

        private final AtomicBoolean closed = new AtomicBoolean(false);
        private final List<MessageHandler> messageHandlers = new java.util.concurrent.CopyOnWriteArrayList<>();
        private final List<Runnable> closeHandlers = new java.util.concurrent.CopyOnWriteArrayList<>();

        private volatile BufferedSink sink;
        private volatile TunnelException failure;
        private volatile TunnelErrorKind lastErrorKind;
        private volatile String lastErrorMessage;

        H2ControlStream(HttpUrl url) throws TunnelException {
            DuplexRequestBody body = new DuplexRequestBody();
            Call call = newCall(kind(), duplexRequest(url, body));
            call.timeout().timeout(HEADERS_TIMEOUT_MS, TimeUnit.MILLISECONDS);
            liveCalls.add(call);

            try {
                streamPool.execute(() -> runControl(call, body));
            } catch (RejectedExecutionException e) {
                liveCalls.remove(call);
                throw new TunnelException(TunnelErrorKind.LIMIT,
                        "stream pool saturated opening control stream", e);
            }

            // Wait for the response headers (or the failure) before returning:
            // a control stream that never opened must not look usable.
            try {
                if (!ready.await(HEADERS_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
                    throw new TunnelException(TunnelErrorKind.TIMEOUT, "control stream timed out");
                }
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new TunnelException(TunnelErrorKind.TIMEOUT, "control stream interrupted", e);
            }
            if (failure != null) throw failure;
            if (sink == null) {
                throw new TunnelException(TunnelErrorKind.PROTOCOL, "control stream has no sink");
            }
        }

        private void runControl(Call call, DuplexRequestBody body) {
            try {
                Response response = call.execute();
                int code = response.code();
                if (code != 200) {
                    response.close();
                    call.cancel();
                    fail(new TunnelException(TunnelErrorKind.fromStatus(code),
                            "control stream rejected with HTTP " + code, code, null));
                    return;
                }
                call.timeout().timeout(0, TimeUnit.MILLISECONDS);
                BufferedSink controlSink = body.sink();
                if (controlSink == null) {
                    response.close();
                    call.cancel();
                    fail(new TunnelException(TunnelErrorKind.PROTOCOL, "control stream has no sink"));
                    return;
                }
                sink = controlSink;
                ready.countDown();

                ResponseBody responseBody = response.body();
                if (responseBody == null) {
                    fail(new TunnelException(TunnelErrorKind.PROTOCOL, "control stream has no body"));
                    return;
                }
                readLines(responseBody.source());
                end(null, null);
            } catch (IOException e) {
                fail(asTunnelException(e, "control stream failed"));
            } catch (RuntimeException e) {
                fail(new TunnelException(TunnelErrorKind.UNKNOWN, "control stream failed: " + e, e));
            } finally {
                // Unblock the constructor if the failure happened before the
                // headers arrived.
                ready.countDown();
                liveCalls.remove(call);
            }
        }

        private void readLines(BufferedSource source) throws IOException {
            while (true) {
                String line;
                try {
                    line = source.readUtf8LineStrict(MAX_CONTROL_LINE);
                } catch (java.io.EOFException e) {
                    // Clean EOF: the server half-closed (or the session ended).
                    return;
                }
                ControlMessage msg = ControlMessage.parse(line);
                if (msg == null) {
                    // Malformed line: skip it, mirroring the server's
                    // readControlLoop. One bad byte must not drop every reverse
                    // port.
                    log("w", "dropping malformed control line");
                    continue;
                }
                if (ControlMessage.BOUND.equals(msg.type)) {
                    settleBind(msg.hasPort() ? Integer.valueOf(msg.port) : null);
                } else if (ControlMessage.BIND_ERR.equals(msg.type)) {
                    settleBind(null);
                }
                for (MessageHandler handler : messageHandlers) {
                    try {
                        handler.onMessage(msg);
                    } catch (RuntimeException e) {
                        // A handler must not be able to kill the control stream.
                        log("e", "control message handler threw", e);
                    }
                }
            }
        }

        private void fail(TunnelException e) {
            failure = e;
            end(e.kind(), e.getMessage());
        }

        /** Complete the oldest pending bind, if any. */
        private void settleBind(@Nullable Integer port) {
            BindWaiter waiter = bindWaiters.poll();
            if (waiter != null) waiter.settle(port);
        }

        private void end(@Nullable TunnelErrorKind errorKind, @Nullable String message) {
            lastErrorKind = errorKind;
            lastErrorMessage = message;
            if (!closed.compareAndSet(false, true)) return;
            // A control stream that ended can never answer a pending bind.
            // Settling every waiter keeps bind() from hanging on a dead tunnel
            // (which would also poison the caller's pending-bind bookkeeping
            // forever).
            BindWaiter waiter;
            while ((waiter = bindWaiters.poll()) != null) {
                waiter.settle(null);
            }
            for (Runnable handler : closeHandlers) {
                try {
                    handler.run();
                } catch (RuntimeException e) {
                    log("e", "control close handler threw", e);
                }
            }
        }

        @Override
        public boolean send(ControlMessage msg) {
            if (msg == null) return false;
            BufferedSink current = sink;
            if (current == null || closed.get()) return false;
            String line = msg.encode();
            if (line == null) return false;
            try {
                current.writeUtf8(line);
                current.flush();
                return true;
            } catch (IOException e) {
                // The stream is gone; onClose has fired or will.
                fail(asTunnelException(e, "control write failed"));
                return false;
            }
        }

        /**
         * Send {@code bind} and wait for {@code bound}/{@code bind_err}.
         *
         * <p>The waiter is registered before the command goes out, so a reply
         * that arrives immediately cannot be missed. The wait is bounded by the
         * stream's own lifetime rather than a fixed timeout: a {@code bind} on
         * a dead control stream is settled by {@link #end}, and a live one is
         * answered promptly because the server writes the reply before reading
         * the next command. A fixed timeout here would convert a slow-but-live
         * server into a spurious failure.
         *
         * <p>Blocks the caller's own thread (never a pool thread).
         */
        Integer bind(int serverPort) {
            BindWaiter waiter = new BindWaiter();
            bindWaiters.add(waiter);
            if (!send(ControlMessage.bind(serverPort))) {
                bindWaiters.remove(waiter);
                return null;
            }
            return waiter.await();
        }

        @Override
        public void onMessage(MessageHandler handler) {
            if (handler != null) messageHandlers.add(handler);
        }

        @Override
        public void onClose(Runnable handler) {
            if (handler == null) return;
            closeHandlers.add(handler);
            // Already ended: report immediately so a subscriber that attaches
            // after the fact is not left waiting forever.
            if (closed.get()) {
                try {
                    handler.run();
                } catch (RuntimeException e) {
                    log("e", "control close handler threw", e);
                }
            }
        }

        @Override
        public void close() {
            if (!closed.get()) {
                BufferedSink current = sink;
                if (current != null) {
                    try {
                        current.close(); // END_STREAM on the request direction
                    } catch (IOException ignored) {
                        // Already gone.
                    }
                }
            }
            end(null, null);
        }

        @Override
        public boolean isClosed() {
            return closed.get();
        }

        @Override
        public TunnelErrorKind lastErrorKind() {
            return lastErrorKind;
        }

        @Override
        public String lastErrorMessage() {
            return lastErrorMessage;
        }
    }

    /**
     * One in-flight {@code bind}, waiting for its reply.
     *
     * <p>Reached by exactly one settler: both the reply path and the teardown
     * path {@code poll()} the waiter off the queue first, and {@code poll()} is
     * atomic, so a reply and a concurrent stream death cannot both claim the
     * same waiter (and a real answer can never be overwritten by a null).
     */
    private static final class BindWaiter {
        private final java.util.concurrent.CountDownLatch done = new java.util.concurrent.CountDownLatch(1);
        private volatile Integer port;

        void settle(@Nullable Integer result) {
            port = result;
            done.countDown();
        }

        @Nullable
        Integer await() {
            try {
                done.await();
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return null;
            }
            return port;
        }
    }

    // ------------------------------------------------------------------
    // helpers
    // ------------------------------------------------------------------

    /**
     * The client for one transport kind, created on first use.
     *
     * <p>Per-kind clients are required because OkHttp validates the protocol
     * list against the scheme: {@code H2_PRIOR_KNOWLEDGE} cannot be combined
     * with HTTPS. Only one kind is live at a time, and it serves both the
     * control stream and every data stream, which is what keeps them on a
     * single TCP connection (and therefore on a single claim-token scope).
     */
    /**
     * Create a call, honouring the test seam.
     *
     * <p>Production always goes through the shared per-kind client; the
     * override exists only so unit tests can observe requests and supply canned
     * responses without a live server.
     */
    private Call newCall(TransportKind transportKind, Request request) {
        CallFactory override = callFactoryOverride;
        if (override != null) return override.newCall(request);
        return client(transportKind).newCall(request);
    }

    /**
     * The shared client for a transport kind. Package-private so unit tests can
     * assert the two settings that must never regress: both timeouts at 0, and
     * the protocol list that makes h2c work.
     */
    OkHttpClient clientForTesting(TransportKind transportKind) {
        return client(transportKind);
    }

    private OkHttpClient client(TransportKind transportKind) {
        OkHttpClient existing = clients.get(transportKind);
        if (existing != null) return existing;
        synchronized (clients) {
            existing = clients.get(transportKind);
            if (existing != null) return existing;
            OkHttpClient created = buildClient(transportKind);
            clients.put(transportKind, created);
            return created;
        }
    }

    private OkHttpClient buildClient(TransportKind transportKind) {
        OkHttpClient.Builder builder = new OkHttpClient.Builder()
                // The two settings that make a long-lived, quiet stream
                // survivable. Without them the 10s default fires on an idle
                // tunnel and OkHttp answers by RST_STREAM(CANCEL), killing a
                // healthy stream whenever the forwarded target is silent.
                .readTimeout(0, TimeUnit.MILLISECONDS)
                .writeTimeout(0, TimeUnit.MILLISECONDS)
                .connectTimeout(HEADERS_TIMEOUT_MS, TimeUnit.MILLISECONDS)
                // No protocol-level ping: the tunnel has no keepalive
                // requirement (h2 has connection-level PING and stream
                // lifecycle), and an unexpected ping would only add traffic.
                .pingInterval(0, TimeUnit.MILLISECONDS)
                // The data streams carry opaque TCP payloads; the default
                // cookie jar would be both useless and a cross-talk risk.
                .cookieJar(okhttp3.CookieJar.NO_COOKIES)
                .retryOnConnectionFailure(true);

        if (transportKind == TransportKind.H2C) {
            // Prior knowledge: speak h2 immediately, no Upgrade negotiation.
            builder.protocols(Collections.singletonList(Protocol.H2_PRIOR_KNOWLEDGE));
        } else {
            // HTTP_2 must be paired with HTTP_1_1 — OkHttp rejects a list that
            // contains neither H2_PRIOR_KNOWLEDGE nor HTTP_1_1. HTTP/1.1 is
            // never actually selected because the probe demands HTTP_2.
            builder.protocols(Arrays.asList(Protocol.HTTP_2, Protocol.HTTP_1_1));
            SSLContext sslContext = platform.trustAllSslContext();
            if (sslContext != null) {
                builder.sslSocketFactory(sslContext.getSocketFactory(), TRUST_ALL_MANAGER);
                builder.hostnameVerifier((hostname, session) -> true);
            }
        }
        return builder.build();
    }

    /** Trust-all manager for self-signed instances; mirrors the native WS client. */
    private static final X509TrustManager TRUST_ALL_MANAGER = new X509TrustManager() {
        @Override
        public X509Certificate[] getAcceptedIssuers() {
            return new X509Certificate[0];
        }

        @Override
        public void checkClientTrusted(X509Certificate[] chain, String authType) {
        }

        @Override
        public void checkServerTrusted(X509Certificate[] chain, String authType) {
        }
    };

    /**
     * Build the {@code Cookie} header for the live target.
     *
     * <p>h2 carries auth as an ordinary header. A missing cookie is not an
     * error here — the request goes out without it and the server answers 401,
     * which the caller sees as {@link TunnelErrorKind#AUTH}.
     */
    @Nullable
    private String cookieHeader() {
        ServerTarget t = target;
        if (t == null) return null;
        try {
            return platform.sessionCookie(t.originalUrl);
        } catch (RuntimeException e) {
            log("w", "failed to read session cookie: " + e.getMessage());
            return null;
        }
    }

    /**
     * Build the POST for one tunnel stream.
     *
     * <p>No {@code Content-Length} (the body is a live stream with no known
     * end), and the session cookie rides as an ordinary header — h2 has no
     * other auth channel.
     */
    private Request duplexRequest(HttpUrl url, DuplexRequestBody body) throws TunnelException {
        if (kind == null) throw new TunnelException(TunnelErrorKind.CLOSED, "tunnel not connected");
        Request.Builder builder = new Request.Builder().url(url).post(body);
        String cookie = cookieHeader();
        if (cookie != null && !cookie.isEmpty()) {
            builder.header("Cookie", cookie);
        }
        return builder.build();
    }

    private ServerTarget target() throws TunnelException {
        ServerTarget t = target;
        if (t == null || !isConnected()) {
            throw new TunnelException(TunnelErrorKind.CLOSED, "tunnel not connected");
        }
        return t;
    }

    private TransportKind kind() throws TunnelException {
        TransportKind k = kind;
        if (k == null) {
            throw new TunnelException(TunnelErrorKind.CLOSED, "tunnel not connected");
        }
        return k;
    }

    private void recordFailure(TunnelErrorKind errorKind, String message) {
        lastErrorKind = errorKind;
        lastError = message == null ? "" : message;
    }

    private static TunnelException asTunnelException(IOException e, String context) {
        if (e instanceof TunnelException) return (TunnelException) e;
        TunnelErrorKind errorKind = TunnelErrorKind.of(e);
        String detail = e.getMessage() == null ? e.getClass().getSimpleName() : e.getMessage();
        return new TunnelException(errorKind, context + ": " + detail, e);
    }

    private void log(String level, String message) {
        try {
            platform.log(level, message);
        } catch (RuntimeException ignored) {
            // Logging must never break the tunnel.
        }
    }

    private void log(String level, String message, Throwable error) {
        try {
            platform.log(level, message, error);
        } catch (RuntimeException ignored) {
            // Logging must never break the tunnel.
        }
    }

    /** Daemon threads so an idle tunnel never keeps the JVM/process alive. */
    private static final class StreamThreadFactory implements ThreadFactory {
        private final AtomicInteger counter = new AtomicInteger();

        @Override
        public Thread newThread(Runnable r) {
            Thread thread = new Thread(r, "h2-tunnel-" + counter.incrementAndGet());
            thread.setDaemon(true);
            return thread;
        }
    }
}
