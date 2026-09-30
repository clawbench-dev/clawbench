package com.clawbench.app.tunnel;

import java.io.IOException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;

import okhttp3.Call;
import okhttp3.Callback;
import okhttp3.MediaType;
import okhttp3.Protocol;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;
import okhttp3.ResponseBody;
import okio.Buffer;
import okio.BufferedSink;
import okio.BufferedSource;
import okio.Okio;
import okio.Pipe;
import okio.Timeout;

/**
 * A fake {@link CallFactory} that stands in for a live h2 server.
 *
 * <p>Why a fake rather than MockWebServer: MockWebServer's h2 handler is
 * half-duplex — {@code Http2SocketHandler.onStream} reads the entire request
 * body before it writes any response, so a full-duplex test against it hangs
 * until the timeout (measured in this project: EXIT=124). Real full-duplex
 * coverage therefore belongs to T14's integration test against a real Go
 * server; these unit tests drive the transport's own logic instead.
 *
 * <p>The fake models the one part of OkHttp that matters here: the duplex
 * branch of {@code CallServerInterceptor}. When a request body reports {@link
 * RequestBody#isDuplex()}, the interceptor creates a sink, calls {@code
 * writeTo(sink)} and — because the body returned — proceeds to read the
 * response headers. {@link FakeCall#execute()} reproduces exactly that, so the
 * request sink the transport publishes is the same object the fake lets the
 * test read from.
 */
final class FakeTunnelServer implements CallFactory {

    private static final MediaType OCTET = MediaType.parse("application/octet-stream");

    /** How each request is answered. */
    enum Mode {
        /** Publish a duplex pipe; the test drives both directions. */
        DUPLEX,
        /** Answer with a status and an empty body (probe, 403, 502, ...). */
        STATUS,
        /** Fail {@code execute()} with this IOException. */
        FAIL,
        /** Block inside {@code execute()} until {@link #releaseHang()}. */
        HANG
    }

    final List<Request> requests = Collections.synchronizedList(new ArrayList<>());

    /** Set before a request arrives to choose its behaviour. */
    volatile Mode mode = Mode.DUPLEX;
    volatile int status = 200;
    volatile Protocol protocol = Protocol.H2_PRIOR_KNOWLEDGE;
    volatile IOException failure;
    /** Protocol reported by the probe response; HTTP/1.1 simulates a non-h2 peer. */
    volatile Protocol probeProtocol = Protocol.HTTP_2;
    /** Probe status; 401 simulates "answered over h2 but unauthenticated". */
    volatile int probeStatus = 200;
    /** When set, every request on this URL scheme fails (simulates plaintext TLS). */
    volatile String failScheme = null;
    /** When true, only the probe fails; streams still work. */
    volatile boolean failProbeOnly = false;
    /** When true, the probe blocks until {@link #releaseHang()}. */
    volatile boolean hangProbe = false;

    /** Streams the fake handed out, oldest first. */
    final List<FakeStream> streams = Collections.synchronizedList(new ArrayList<>());

    /** Request bodies observed on duplex streams, for isDuplex assertions. */
    final List<RequestBody> duplexBodies = Collections.synchronizedList(new ArrayList<>());

    private final AtomicInteger executions = new AtomicInteger();
    private final AtomicInteger enqueues = new AtomicInteger();
    /** Number of {@code Call.cancel()} invocations across every fake call. */
    private final AtomicInteger cancels = new AtomicInteger();

    /** Gate for {@link Mode#HANG}: released by {@link #releaseHang()}. */
    private volatile java.util.concurrent.CountDownLatch hangGate = new java.util.concurrent.CountDownLatch(0);

    /** Requests that have reached the hang gate, oldest first — a real signal
     *  for "these workers occupy their slots", so a test does not have to sleep
     *  a fixed interval and hope the threads were scheduled in time. */
    private final AtomicInteger hangEntries = new AtomicInteger();
    private final Object hangMonitor = new Object();

    /** Make the next {@code execute()} block until {@link #releaseHang()}. */
    void armHang() {
        hangGate = new java.util.concurrent.CountDownLatch(1);
        synchronized (hangMonitor) {
            hangEntries.set(0);
        }
    }

    void releaseHang() {
        hangGate.countDown();
    }

    /**
     * Wait until {@code count} requests have parked in the hang gate.
     *
     * <p>Replaces a {@code Thread.sleep} + "the workers must be in place"
     * assertion: under load the sleep could expire before the pool threads
     * were even scheduled, so the assertion that followed ran against an idle
     * pool. Awaiting the hang entries proves the workers actually reached the
     * gate first.
     */
    boolean awaitHangs(int count, long timeoutMs) throws InterruptedException {
        long deadline = System.currentTimeMillis() + timeoutMs;
        synchronized (hangMonitor) {
            while (hangEntries.get() < count) {
                long remaining = deadline - System.currentTimeMillis();
                if (remaining <= 0) return false;
                hangMonitor.wait(remaining);
            }
            return true;
        }
    }

    /** Park in the hang gate, recording that this request occupies its slot. */
    private void enterHang(String message) throws IOException {
        synchronized (hangMonitor) {
            hangEntries.incrementAndGet();
            hangMonitor.notifyAll();
        }
        try {
            hangGate.await();
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IOException(message, e);
        }
    }

    int executionCount() {
        return executions.get();
    }

    /** Total {@code cancel()} calls; lets a test prove a leaked call was closed. */
    int cancelCount() {
        return cancels.get();
    }

    /** Any use of {@code enqueue()} is a bug: it caps at maxRequestsPerHost=5. */
    int enqueueCount() {
        return enqueues.get();
    }

    @Override
    public Call newCall(Request request) {
        requests.add(request);
        return new FakeCall(request);
    }

    Request lastRequest() {
        synchronized (requests) {
            return requests.isEmpty() ? null : requests.get(requests.size() - 1);
        }
    }

    FakeStream lastStream() {
        synchronized (streams) {
            return streams.isEmpty() ? null : streams.get(streams.size() - 1);
        }
    }

    /** Requests whose path matches, oldest first. */
    List<Request> requestsFor(String pathSuffix) {
        List<Request> matched = new ArrayList<>();
        synchronized (requests) {
            for (Request r : requests) {
                if (r.url().encodedPath().endsWith(pathSuffix)) matched.add(r);
            }
        }
        return matched;
    }

    private final class FakeCall implements Call {
        private final Request request;
        private volatile boolean canceled = false;
        private volatile boolean executed = false;
        private final Timeout timeout = new Timeout();
        /**
         * The duplex stream this call handed out, or {@code null} before
         * {@link #duplexResponse()} ran (e.g. a call still parked in
         * {@link Mode#HANG}). Set by the {@link FakeStream} constructor.
         */
        private volatile FakeStream stream;

        FakeCall(Request request) {
            this.request = request;
        }

        @Override
        public Request request() {
            return request;
        }

        @Override
        public Response execute() throws IOException {
            executed = true;
            executions.incrementAndGet();

            // A scheme-wide failure models a plaintext deployment rejecting the
            // TLS probe: the h2c attempt (a different scheme) must then win.
            if (failScheme != null && failScheme.equals(request.url().scheme())) {
                throw new IOException("scheme " + failScheme + " refused");
            }

            if (request.url().encodedPath().endsWith("/api/ssh/info")) {
                if (hangProbe) {
                    enterHang("interrupted while hanging the probe");
                }
                if ((mode == Mode.FAIL || failProbeOnly) && failure != null) throw failure;
                return responseFor(probeStatus, probeProtocol);
            }

            if (mode == Mode.HANG) {
                enterHang("interrupted while hanging");
                if (canceled) throw new IOException("Canceled");
                return duplexResponse();
            }

            switch (mode) {
                case FAIL:
                    if (failure != null) throw failure;
                    throw new IOException("fake failure");
                case STATUS:
                    return responseFor(status, protocol);
                case DUPLEX:
                default:
                    return duplexResponse();
            }
        }

        /**
         * Mirror CallServerInterceptor's duplex branch: hand the request body a
         * sink, let it return, then return the response. A non-duplex body
         * would be finished here before the response existed — which is exactly
         * why the transport must report {@code isDuplex() == true}.
         */
        private Response duplexResponse() throws IOException {
            FakeStream stream = new FakeStream(this);
            streams.add(stream);

            RequestBody body = request.body();
            if (body == null) throw new IOException("duplex request has no body");
            duplexBodies.add(body);
            // Fail loudly rather than hanging if the constraint regresses: a
            // body that writes inline would block here forever, and the test
            // harness would time out instead of pointing at the cause.
            if (!body.isDuplex()) {
                throw new IOException("request body is not duplex; OkHttp would "
                        + "finishRequest() before reading the response");
            }
            BufferedSink sink = Okio.buffer(stream.toServer.sink());
            body.writeTo(sink);
            return stream.response;
        }

        @Override
        public void enqueue(Callback responseCallback) {
            enqueues.incrementAndGet();
            throw new AssertionError("enqueue() must never be used for a tunnel stream");
        }

        @Override
        public void cancel() {
            canceled = true;
            cancels.incrementAndGet();
            // Real OkHttp answers cancel() with an RST_STREAM scoped to this
            // stream, which fails the response-direction read of a caller
            // parked in read() — that is the whole point of cancel() for the
            // tunnel (it is what unblocks a parked pump). Model it: without
            // this the fake leaves the reader parked forever and a test cannot
            // observe the failure the production path raises.
            FakeStream current = stream;
            if (current != null) {
                current.failFromServer();
            }
        }

        @Override
        public boolean isExecuted() {
            return executed;
        }

        @Override
        public boolean isCanceled() {
            return canceled;
        }

        @Override
        public Timeout timeout() {
            return timeout;
        }

        @Override
        public Call clone() {
            return new FakeCall(request);
        }
    }

    private Response responseFor(int code, Protocol proto) {
        Request req = lastRequest();
        Request fallback = req != null ? req
                : new Request.Builder().url("http://127.0.0.1:20000/").build();
        return new Response.Builder()
                .request(fallback)
                .protocol(proto)
                .code(code)
                .message(code == 200 ? "OK" : "ERR")
                .body(ResponseBody.create("", OCTET))
                .build();
    }

    /**
     * One fake duplex stream.
     *
     * <p>{@code toServer} is the request direction: the transport writes into
     * its sink (via the duplex body) and the test reads it back. {@code
     * fromServer} is the response direction: the test writes and the transport
     * reads.
     */
    static final class FakeStream {
        /** Client -> server bytes. The transport's duplex body writes here. */
        final Pipe toServer = new Pipe(256 * 1024);
        /** Server -> client bytes. The transport reads these as the response. */
        final Pipe fromServer = new Pipe(256 * 1024);
        final Response response;
        private final FakeCall call;

        FakeStream(FakeCall call) {
            this.call = call;
            // Let Call.cancel() reach this stream's response pipe, the way an
            // RST_STREAM does in real OkHttp.
            call.stream = this;
            BufferedSource source = Okio.buffer(fromServer.source());
            ResponseBody body = ResponseBody.create(source, OCTET, -1L);
            this.response = new Response.Builder()
                    .request(call.request)
                    .protocol(Protocol.H2_PRIOR_KNOWLEDGE)
                    .code(200)
                    .message("OK")
                    .body(body)
                    .build();
        }

        boolean isCanceled() {
            return call.canceled;
        }

        /** Read whatever the transport has written to the request direction. */
        String readToServer(long maxBytes) throws IOException {
            Buffer buffer = new Buffer();
            long read = toServer.source().read(buffer, maxBytes);
            if (read == -1) return null;
            return buffer.readUtf8();
        }

        /**
         * Read exactly {@code expected} request-direction bytes (or until EOF),
         * for a bulk payload test that must compare bytes rather than UTF-8.
         */
        byte[] readToServerBytes(int expected) throws IOException {
            Buffer buffer = new Buffer();
            long total = 0;
            while (total < expected) {
                long read = toServer.source().read(buffer, expected - total);
                if (read == -1) break;
                total += read;
            }
            return buffer.readByteArray();
        }

        /** Push bytes from the server into the response direction. */
        void writeFromServer(String data) throws IOException {
            BufferedSink sink = Okio.buffer(fromServer.sink());
            sink.writeUtf8(data);
            sink.flush();
        }

        /** End the response direction (server half-close). */
        void endFromServer() throws IOException {
            fromServer.sink().close();
        }

        /**
         * Fail the response direction with an {@code IOException} on the next
         * read, the way a reset connection surfaces to the transport. Unlike
         * {@link #endFromServer} (a clean EOF) this is a transport failure.
         */
        void failFromServer() {
            fromServer.cancel();
        }
    }
}
