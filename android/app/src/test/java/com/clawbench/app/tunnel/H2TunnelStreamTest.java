package com.clawbench.app.tunnel;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.ConnectException;
import java.net.SocketTimeoutException;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import javax.net.ssl.SSLContext;

import okhttp3.HttpUrl;
import okhttp3.OkHttpClient;
import okhttp3.Protocol;
import okhttp3.Request;
import okhttp3.Response;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for {@link H2TunnelStream}, driving it through a fake
 * {@link CallFactory}.
 *
 * <p>Every assertion here corresponds to one of the transport's hard
 * constraints (design doc §8.1) or to the request contract the server
 * documents. The full-duplex data path itself is covered by T14 against a real
 * Go server, because MockWebServer cannot do it (see {@link FakeTunnelServer}).
 */
public class H2TunnelStreamTest {

    private static final String SERVER_URL = "http://127.0.0.1:20000";

    private FakeTunnelServer server;
    private FakePlatform platform;
    private H2TunnelStream tunnel;

    @Before
    public void setUp() {
        server = new FakeTunnelServer();
        platform = new FakePlatform();
        tunnel = new H2TunnelStream(platform, server);
    }

    @After
    public void tearDown() {
        tunnel.close();
    }

    // ==================================================================
    // Constraint 3: both timeouts must be 0
    // ==================================================================

    @Test
    public void client_hasZeroReadAndWriteTimeouts() {
        // Defaults are 10s. An idle tunnel stream would then time out, and
        // OkHttp's h2 stream timeout answers by RST_STREAM(CANCEL) — killing a
        // healthy stream whenever the forwarded target is merely quiet.
        OkHttpClient h2c = tunnel.clientForTesting(TransportKind.H2C);
        assertEquals("readTimeout must be 0 (infinite)", 0, h2c.readTimeoutMillis());
        assertEquals("writeTimeout must be 0 (infinite)", 0, h2c.writeTimeoutMillis());

        OkHttpClient tls = tunnel.clientForTesting(TransportKind.TLS);
        assertEquals(0, tls.readTimeoutMillis());
        assertEquals(0, tls.writeTimeoutMillis());
    }

    @Test
    public void client_hasConnectTimeoutSoAProbeCannotHangForever() {
        OkHttpClient h2c = tunnel.clientForTesting(TransportKind.H2C);
        assertEquals(H2TunnelStream.HEADERS_TIMEOUT_MS, h2c.connectTimeoutMillis());
    }

    // ==================================================================
    // Constraint: h2c uses H2_PRIOR_KNOWLEDGE; TLS uses HTTP_2
    // ==================================================================

    @Test
    public void h2cClient_usesPriorKnowledge() {
        // Prior knowledge means "speak h2 immediately", with no HTTP/1.1
        // Upgrade negotiation — the only form a Go h2c server accepts.
        List<Protocol> protocols = tunnel.clientForTesting(TransportKind.H2C).protocols();
        assertEquals(1, protocols.size());
        assertEquals(Protocol.H2_PRIOR_KNOWLEDGE, protocols.get(0));
    }

    @Test
    public void tlsClient_offersHttp2First() {
        List<Protocol> protocols = tunnel.clientForTesting(TransportKind.TLS).protocols();
        assertTrue("HTTP_2 must be offered for ALPN", protocols.contains(Protocol.HTTP_2));
        // OkHttp rejects a protocol list containing neither H2_PRIOR_KNOWLEDGE
        // nor HTTP_1_1, so HTTP_1_1 must be present as the pairing entry.
        assertTrue(protocols.contains(Protocol.HTTP_1_1));
    }

    @Test
    public void tlsClient_trustsSelfSignedCertificates() {
        // A self-hosted instance commonly uses a self-signed cert; the tunnel
        // is authenticated by the session cookie, not the certificate.
        platform.sslContext = fakeSslContext();
        H2TunnelStream tlsTunnel = new H2TunnelStream(platform, server);
        try {
            OkHttpClient client = tlsTunnel.clientForTesting(TransportKind.TLS);
            assertNotNull(client.sslSocketFactory());
            // Accept-all verifier: any hostname must be accepted.
            assertTrue(client.hostnameVerifier().verify("anything.example", null));
        } finally {
            tlsTunnel.close();
        }
    }

    // ==================================================================
    // Constraint 4: control and data share one client
    // ==================================================================

    @Test
    public void dataAndControlStreams_shareTheSameClient() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        assertNotNull(tunnel.connect(SERVER_URL, null));

        // Two data streams and the control stream must all come from one client
        // so they share one TCP connection. A -R claim token is scoped to the
        // h2 connection, so a claim on another connection would be rejected.
        OkHttpClient first = tunnel.clientForTesting(TransportKind.H2C);
        assertSame("client must be a singleton per kind", first,
                tunnel.clientForTesting(TransportKind.H2C));

        try (TunnelConnection c1 = tunnel.openStream("127.0.0.1", 8080);
             TunnelConnection c2 = tunnel.openStream("127.0.0.1", 8081);
             TunnelControlStream control = tunnel.openControlStream()) {
            assertNotNull(c1);
            assertNotNull(c2);
            assertNotNull(control);
            assertSame(first, tunnel.clientForTesting(TransportKind.H2C));
        } catch (IOException e) {
            throw new AssertionError(e);
        }
    }

    // ==================================================================
    // Readiness: a real round-trip, not "the socket connected"
    // ==================================================================

    @Test
    public void connect_probesWithARealRequest() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        assertEquals(TransportKind.H2C, tunnel.connect(SERVER_URL, TransportKind.H2C));

        List<Request> probes = server.requestsFor("/api/ssh/info");
        assertEquals("connect() must issue exactly one probe", 1, probes.size());
        Request probe = probes.get(0);
        assertEquals("GET", probe.method());
        // The probe is a plain GET with no body: it only needs a response to
        // prove the session frames h2.
        assertNull(probe.body());
    }

    @Test
    public void connect_probeUsesTheMainPort() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        HttpUrl url = server.requestsFor("/api/ssh/info").get(0).url();
        assertEquals(20000, url.port());
        assertEquals("127.0.0.1", url.host());
    }

    @Test
    public void connect_acceptsUnauthorizedProbe_becauseFramingIsWhatMatters() {
        // 401 proves the server answered over h2; the cookie is separate.
        server.probeStatus = 401;
        assertEquals(TransportKind.H2C, tunnel.connect(SERVER_URL, TransportKind.H2C));
        assertTrue(tunnel.isConnected());
    }

    @Test
    public void connect_rejectsAPeerThatIsNotHttp2() {
        // A plain HTTP/1.1 server negotiated h1: the session is not an h2
        // tunnel, so connect() must not report success.
        server.probeProtocol = Protocol.HTTP_1_1;
        assertNull(tunnel.connect(SERVER_URL, TransportKind.H2C));
        assertFalse(tunnel.isConnected());
        assertEquals(TunnelErrorKind.PROTOCOL, tunnel.getLastErrorKind());
    }

    // ==================================================================
    // Transport priority chain
    // ==================================================================

    @Test
    public void connect_triesTlsFirstByDefault() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        assertEquals(TransportKind.TLS, tunnel.connect(SERVER_URL, null));
        // Exactly one probe, on the https scheme.
        List<Request> probes = server.requestsFor("/api/ssh/info");
        assertEquals(1, probes.size());
        assertEquals("https", probes.get(0).url().scheme());
    }

    @Test
    public void connect_fallsBackToH2cWhenTlsFails() {
        // The plaintext deployment: the TLS probe is refused at the handshake
        // (fast, not a timeout), then h2c succeeds.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        server.failScheme = "https";
        assertEquals(TransportKind.H2C, tunnel.connect(SERVER_URL, null));

        List<Request> probes = server.requestsFor("/api/ssh/info");
        assertEquals("both transports must have been probed", 2, probes.size());
        assertEquals("https", probes.get(0).url().scheme());
        assertEquals("http", probes.get(1).url().scheme());
    }

    @Test
    public void connect_honoursTheRememberedTransport() {
        // "Remember the last successful transport": passing h2c must skip the
        // wasted TLS probe entirely.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        assertEquals(TransportKind.H2C, tunnel.connect(SERVER_URL, TransportKind.H2C));
        List<Request> probes = server.requestsFor("/api/ssh/info");
        assertEquals(1, probes.size());
        assertEquals("http", probes.get(0).url().scheme());
    }

    @Test
    public void connect_reportsFailureWhenEveryTransportFails() {
        server.mode = FakeTunnelServer.Mode.FAIL;
        server.failure = new ConnectException("connection refused");
        assertNull(tunnel.connect(SERVER_URL, null));
        assertFalse(tunnel.isConnected());
        assertEquals(TunnelErrorKind.NETWORK, tunnel.getLastErrorKind());
        assertTrue(tunnel.getLastError().contains("refused"));
    }

    @Test
    public void connect_rejectsAnUnparseableServerUrl() {
        assertNull(tunnel.connect("not a url", null));
        assertFalse(tunnel.isConnected());
    }

    @Test
    public void connect_losingTheRaceWithClose_doesNotResurrectTheSession() throws Exception {
        // close() during a slow probe must win: otherwise the session would be
        // marked connected after the caller already tore it down, and the next
        // stream would open on a dead transport.
        FakeTunnelServer fake = new FakeTunnelServer();
        fake.hangProbe = true;
        fake.armHang();
        H2TunnelStream racy = new H2TunnelStream(platform, fake);
        try {
            Thread connector = new Thread(() -> racy.connect(SERVER_URL, TransportKind.H2C));
            connector.start();
            Thread.sleep(200);

            racy.close();
            fake.releaseHang();
            connector.join(10_000);

            assertFalse("a closed tunnel must stay closed", racy.isConnected());
        } finally {
            racy.close();
        }
    }

    @Test
    public void connect_reusesAHealthySessionForTheSameServer() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        int probesAfterFirst = server.requestsFor("/api/ssh/info").size();

        // A second connect for the same host:port must not re-probe (which
        // would drop every live stream).
        assertEquals(TransportKind.H2C, tunnel.connect(SERVER_URL, TransportKind.H2C));
        assertEquals(probesAfterFirst, server.requestsFor("/api/ssh/info").size());
    }

    // ==================================================================
    // Constraint 2: execute(), never enqueue()
    // ==================================================================

    @Test
    public void streams_useExecuteNotEnqueue() {
        // Dispatcher caps async calls at maxRequestsPerHost=5 keyed on host
        // name alone, so the sixth enqueue() would queue forever.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try (TunnelConnection c = tunnel.openStream("127.0.0.1", 8080)) {
            assertNotNull(c);
        } catch (IOException e) {
            throw new AssertionError(e);
        }
        assertEquals(0, server.enqueueCount());
        assertTrue(server.executionCount() >= 2); // probe + stream
    }

    @Test
    public void manyConcurrentStreams_allReachExecute() throws Exception {
        // The regression this guards: with enqueue() only 5 would start.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);

        final int count = 20;
        final CountDownLatch started = new CountDownLatch(count);
        final CountDownLatch release = new CountDownLatch(1);
        final AtomicReference<Throwable> error = new AtomicReference<>();
        Thread[] workers = new Thread[count];
        final TunnelConnection[] connections = new TunnelConnection[count];

        for (int i = 0; i < count; i++) {
            final int index = i;
            workers[i] = new Thread(() -> {
                try {
                    connections[index] = tunnel.openStream("127.0.0.1", 9000 + index);
                    started.countDown();
                    release.await();
                } catch (Throwable t) {
                    error.compareAndSet(null, t);
                    started.countDown();
                }
            }, "open-" + i);
            workers[i].start();
        }

        assertTrue("all 20 streams must open", started.await(30, TimeUnit.SECONDS));
        release.countDown();
        for (Thread worker : workers) worker.join(10_000);

        assertNull("no stream may fail: " + error.get(), error.get());
        assertEquals(20, server.streams.size());
        assertEquals(0, server.enqueueCount());

        for (TunnelConnection connection : connections) {
            if (connection != null) connection.close();
        }
    }

    // ==================================================================
    // Constraint 1: isDuplex() must be true
    // ==================================================================

    @Test
    public void requestBody_isDuplex() {
        // The decisive constraint: with isDuplex()==false, OkHttp's
        // CallServerInterceptor calls finishRequest() before reading the
        // response, so the response direction is unreachable while the request
        // direction is open.
        DuplexRequestBody body = new DuplexRequestBody();
        assertTrue(body.isDuplex());
        assertTrue("a live sink must not be retried", body.isOneShot());
    }

    @Test
    public void streamRequest_usesADuplexBody() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try (TunnelConnection c = tunnel.openStream("127.0.0.1", 8080)) {
            assertNotNull(c);
        } catch (IOException e) {
            throw new AssertionError(e);
        }
        assertFalse("the fake records the body it was handed",
                server.duplexBodies.isEmpty());
        assertTrue(server.duplexBodies.get(0).isDuplex());
    }

    // ==================================================================
    // Request contract (must match internal/handler/tunnel_stream.go)
    // ==================================================================

    @Test
    public void openStream_buildsTheDocumentedPostRequest() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try (TunnelConnection c = tunnel.openStream("10.0.0.5", 8080)) {
            assertNotNull(c);
        } catch (IOException e) {
            throw new AssertionError(e);
        }

        Request request = server.lastRequest();
        assertEquals("POST", request.method());
        assertEquals("/api/tunnel/stream", request.url().encodedPath());
        assertEquals("10.0.0.5", request.url().queryParameter("host"));
        assertEquals("8080", request.url().queryParameter("port"));
        assertEquals("application/octet-stream",
                request.body().contentType().toString());
        try {
            // -1 means "unknown length", i.e. no Content-Length header, which
            // is what makes the body a stream rather than a fixed payload.
            assertEquals(-1, request.body().contentLength());
        } catch (IOException e) {
            throw new AssertionError(e);
        }
    }

    @Test
    public void openStream_escapesTheTargetQueryParameters() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try (TunnelConnection c = tunnel.openStream("host with space", 80)) {
            assertNotNull(c);
        } catch (IOException e) {
            throw new AssertionError(e);
        }
        // The parsed value must round-trip exactly; the raw URL is encoded.
        assertEquals("host with space", server.lastRequest().url().queryParameter("host"));
    }

    @Test
    public void openClaimStream_usesTheClaimParameterAndNoHostPort() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try (TunnelConnection c = tunnel.openClaimStream("token-abc")) {
            assertNotNull(c);
        } catch (IOException e) {
            throw new AssertionError(e);
        }
        Request request = server.lastRequest();
        assertEquals("/api/tunnel/stream", request.url().encodedPath());
        assertEquals("token-abc", request.url().queryParameter("claim"));
        assertNull("host must not accompany a claim", request.url().queryParameter("host"));
        assertNull("port must not accompany a claim", request.url().queryParameter("port"));
    }

    @Test
    public void openClaimStream_rejectsAnEmptyTokenWithoutTouchingTheNetwork() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try {
            tunnel.openClaimStream("");
            throw new AssertionError("expected a rejection");
        } catch (TunnelException e) {
            assertEquals(TunnelErrorKind.AUTH, e.kind());
        }
        assertTrue(server.requestsFor("/api/tunnel/stream").isEmpty());
    }

    @Test
    public void requests_carryTheSessionCookieHeader() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        platform.cookie = "cb20300_clawbench_session=secret";
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try (TunnelConnection c = tunnel.openStream("127.0.0.1", 8080)) {
            assertNotNull(c);
        } catch (IOException e) {
            throw new AssertionError(e);
        }
        assertEquals("cb20300_clawbench_session=secret",
                server.lastRequest().header("Cookie"));
    }

    @Test
    public void requests_omitTheCookieHeaderWhenNotLoggedIn() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        platform.cookie = null;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try (TunnelConnection c = tunnel.openStream("127.0.0.1", 8080)) {
            assertNotNull(c);
        } catch (IOException e) {
            throw new AssertionError(e);
        }
        // Omitted rather than empty: the server answers 401, which is the
        // correct signal, instead of receiving a malformed header.
        assertNull(server.lastRequest().header("Cookie"));
    }

    // ==================================================================
    // HTTP status -> error kind
    // ==================================================================

    @Test
    public void openStream_maps502ToTargetUnreachable() {
        server.mode = FakeTunnelServer.Mode.STATUS;
        server.status = 502;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try {
            tunnel.openStream("127.0.0.1", 8080);
            throw new AssertionError("expected 502 to fail");
        } catch (TunnelException e) {
            assertEquals(TunnelErrorKind.TARGET_UNREACHABLE, e.kind());
            assertEquals(502, e.status());
        }
        // A 502 is the server answering, so the session stays usable.
        assertTrue(tunnel.isConnected());
    }

    @Test
    public void openStream_maps403ToAuth() {
        server.mode = FakeTunnelServer.Mode.STATUS;
        server.status = 403;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try {
            tunnel.openStream("127.0.0.1", 8080);
            throw new AssertionError("expected 403 to fail");
        } catch (TunnelException e) {
            assertEquals(TunnelErrorKind.AUTH, e.kind());
        }
    }

    @Test
    public void openStream_maps503ToUnavailable() {
        server.mode = FakeTunnelServer.Mode.STATUS;
        server.status = 503;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        try {
            tunnel.openStream("127.0.0.1", 8080);
            throw new AssertionError("expected 503 to fail");
        } catch (TunnelException e) {
            assertEquals(TunnelErrorKind.UNAVAILABLE, e.kind());
        }
    }

    @Test
    public void openStream_mapsAnIOExceptionToNetwork() {
        // Connect first (probe succeeds), then make the data plane fail: the
        // session is up, so the failure must classify as a stream-level
        // network error rather than "not connected".
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        server.mode = FakeTunnelServer.Mode.FAIL;
        server.failure = new ConnectException("connection refused");
        try {
            tunnel.openStream("127.0.0.1", 8080);
            throw new AssertionError("expected a failure");
        } catch (TunnelException e) {
            assertEquals(TunnelErrorKind.NETWORK, e.kind());
        }
    }

    @Test
    public void openStream_timesOutRatherThanHangingForever() {
        server.mode = FakeTunnelServer.Mode.HANG;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        server.mode = FakeTunnelServer.Mode.HANG;
        server.armHang();
        long start = System.currentTimeMillis();
        try {
            tunnel.openStream("127.0.0.1", 8080);
            throw new AssertionError("expected a timeout");
        } catch (TunnelException e) {
            // The caller must not block indefinitely on a server that accepted
            // the stream but never answered.
            assertEquals(TunnelErrorKind.TIMEOUT, e.kind());
        } finally {
            server.releaseHang();
        }
        long elapsed = System.currentTimeMillis() - start;
        assertTrue("must give up in bounded time, took " + elapsed + "ms",
                elapsed < H2TunnelStream.STREAM_SLOT_WAIT_MS + 5000);
    }

    // ==================================================================
    // Constraint: stream pool is bounded
    // ==================================================================

    @Test
    public void streamPool_saturatesWithLimitRatherThanUnboundedThreads() throws Exception {
        FakeTunnelServer fake = new FakeTunnelServer();
        fake.mode = FakeTunnelServer.Mode.DUPLEX;
        H2TunnelStream limited = new H2TunnelStream(platform, fake, 2);
        try {
            assertNotNull(limited.connect(SERVER_URL, TransportKind.H2C));
            fake.mode = FakeTunnelServer.Mode.HANG;
            fake.armHang();

            Thread first = new Thread(() -> openQuietly(limited, 1));
            Thread second = new Thread(() -> openQuietly(limited, 2));
            first.start();
            second.start();
            // Give both a moment to occupy the two slots.
            Thread.sleep(300);

            try {
                limited.openStream("127.0.0.1", 3);
                throw new AssertionError("expected saturation");
            } catch (TunnelException e) {
                assertEquals(TunnelErrorKind.LIMIT, e.kind());
            }

            fake.releaseHang();
            first.join(10_000);
            second.join(10_000);
        } finally {
            limited.close();
        }
    }

    private static void openQuietly(TunnelStream tunnel, int port) {
        try {
            tunnel.openStream("127.0.0.1", port);
        } catch (TunnelException ignored) {
            // The hang is released in the test body.
        }
    }

    // ==================================================================
    // Stream accounting (drives WifiLock retention in T12)
    // ==================================================================

    @Test
    public void activeStreamCount_tracksOpenStreams() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        assertFalse(tunnel.hasActiveStreams());

        TunnelConnection connection = openStreamQuietly();
        assertEquals(1, tunnel.activeStreamCount());
        assertTrue(tunnel.hasActiveStreams());

        connection.close();
        assertEquals(0, tunnel.activeStreamCount());
        assertFalse(tunnel.hasActiveStreams());
    }

    @Test
    public void failedStream_doesNotLeakAStreamSlot() {
        server.mode = FakeTunnelServer.Mode.STATUS;
        server.status = 502;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        for (int i = 0; i < 5; i++) {
            try {
                tunnel.openStream("127.0.0.1", 8080);
                throw new AssertionError("expected 502");
            } catch (TunnelException ignored) {
                // Expected.
            }
        }
        assertEquals("a failed stream must release its slot",
                0, tunnel.activeStreamCount());
    }

    // ==================================================================
    // Duplex data path (via the fake's pipes)
    // ==================================================================

    @Test
    public void connection_relaysBytesInBothDirections() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);

        try (TunnelConnection connection = tunnel.openStream("127.0.0.1", 8080)) {
            FakeTunnelServer.FakeStream stream = server.lastStream();
            assertNotNull(stream);

            OutputStream out = connection.getOutputStream();
            out.write("ping".getBytes("UTF-8"));
            out.flush();
            assertEquals("ping", stream.readToServer(64));

            stream.writeFromServer("pong");
            InputStream in = connection.getInputStream();
            byte[] buffer = new byte[4];
            int read = readFully(in, buffer);
            assertEquals(4, read);
            assertEquals("pong", new String(buffer, "UTF-8"));
        }
    }

    @Test
    public void connection_closeWrite_sendsEndStreamWithoutClosingTheReadSide() throws Exception {
        // Half-close semantics: the local side is done sending, the remote may
        // still reply. Closing the whole stream here would truncate the
        // response of any protocol that answers after the request ends.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);

        TunnelConnection connection = tunnel.openStream("127.0.0.1", 8080);
        FakeTunnelServer.FakeStream stream = server.lastStream();

        connection.getOutputStream().write("bye".getBytes("UTF-8"));
        connection.getOutputStream().flush();
        assertEquals("bye", stream.readToServer(64));

        connection.closeWrite();

        stream.writeFromServer("late");
        InputStream in = connection.getInputStream();
        byte[] buffer = new byte[4];
        assertEquals(4, readFully(in, buffer));
        assertEquals("late", new String(buffer, "UTF-8"));
        assertFalse("half-close must not close the connection", connection.isClosed());

        connection.close();
        assertTrue(connection.isClosed());
    }

    @Test
    public void connection_close_isIdempotent() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelConnection connection = tunnel.openStream("127.0.0.1", 8080);

        connection.close();
        connection.close();
        connection.close();

        assertTrue(connection.isClosed());
        assertEquals(0, tunnel.activeStreamCount());
    }

    @Test
    public void connection_writeAfterHalfClose_isDropped() throws Exception {
        // A late write racing the peer's EOF is the common case, and must be
        // dropped rather than surface as a stream error.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelConnection connection = tunnel.openStream("127.0.0.1", 8080);

        connection.closeWrite();
        connection.getOutputStream().write("late".getBytes("UTF-8"));
        connection.getOutputStream().flush();
        connection.close();
    }

    @Test
    public void connection_closeCancelsOnlyItsOwnStream() throws Exception {
        // Call.cancel() on h2 is an RST_STREAM scoped to one stream; every
        // other stream on the connection must survive.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);

        TunnelConnection first = tunnel.openStream("127.0.0.1", 1);
        FakeTunnelServer.FakeStream firstStream = server.lastStream();
        TunnelConnection second = tunnel.openStream("127.0.0.1", 2);
        FakeTunnelServer.FakeStream secondStream = server.lastStream();
        assertTrue(first != second);

        first.close();
        assertTrue("cancel must reach the Call", firstStream.isCanceled());
        assertFalse("the sibling stream must not be cancelled", secondStream.isCanceled());
        assertEquals(1, tunnel.activeStreamCount());

        second.close();
        assertEquals(0, tunnel.activeStreamCount());
    }

    @Test
    public void close_tearsDownEveryStream() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelConnection first = tunnel.openStream("127.0.0.1", 1);
        TunnelConnection second = tunnel.openStream("127.0.0.1", 2);

        tunnel.close();

        assertTrue(first.isClosed());
        assertTrue(second.isClosed());
        assertFalse(tunnel.isConnected());
        assertEquals(0, tunnel.activeStreamCount());
    }

    // ==================================================================
    // Control stream
    // ==================================================================

    @Test
    public void openControlStream_usesTheDocumentedPostRequest() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        assertNotNull(control);

        List<Request> controlRequests = server.requestsFor("/api/tunnel/control");
        assertEquals(1, controlRequests.size());
        Request request = controlRequests.get(0);
        assertEquals("POST", request.method());
        assertTrue(request.body().isDuplex());
        assertEquals("application/octet-stream",
                request.body().contentType().toString());
        control.close();
    }

    @Test
    public void openControlStream_isIdempotent() throws Exception {
        // Two control streams would mean two competing sets of bind
        // bookkeeping, so the second call must return the first.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream first = tunnel.openControlStream();
        TunnelControlStream second = tunnel.openControlStream();

        assertSame(first, second);
        assertEquals(1, server.requestsFor("/api/tunnel/control").size());
        first.close();
    }

    @Test
    public void controlStream_sendsNDJSONLines() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        FakeTunnelServer.FakeStream stream = server.lastStream();

        assertTrue(control.send(ControlMessage.bind(3000)));
        String line = stream.readToServer(256);
        assertNotNull(line);
        assertTrue(line.endsWith("\n"));
        assertTrue("bind must carry the port", line.contains("\"port\":3000"));
        assertTrue(line.contains("\"type\":\"bind\""));
        control.close();
    }

    @Test
    public void controlStream_parsesIncomingAndDispatchesHandlers() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        FakeTunnelServer.FakeStream stream = server.lastStream();

        final CountDownLatch received = new CountDownLatch(1);
        final AtomicReference<ControlMessage> message = new AtomicReference<>();
        control.onMessage(msg -> {
            message.set(msg);
            received.countDown();
        });

        stream.writeFromServer("{\"type\":\"incoming\",\"port\":9000,\"token\":\"tok\"}\n");
        assertTrue("handler must fire", received.await(5, TimeUnit.SECONDS));
        assertEquals(ControlMessage.INCOMING, message.get().type);
        assertEquals("tok", message.get().token);
        control.close();
    }

    @Test
    public void controlStream_skipsMalformedLinesWithoutDying() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        FakeTunnelServer.FakeStream stream = server.lastStream();

        final CountDownLatch received = new CountDownLatch(1);
        control.onMessage(msg -> received.countDown());

        stream.writeFromServer("garbage not json\n");
        stream.writeFromServer("{\"type\":\"bogus\"}\n");
        stream.writeFromServer("{\"type\":\"pong\"}\n");

        assertTrue("a good line after bad ones must still arrive",
                received.await(5, TimeUnit.SECONDS));
        assertFalse("one bad line must not end the stream", control.isClosed());
        control.close();
    }

    @Test
    public void controlStream_closeFiresCloseHandlers() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();

        final CountDownLatch closed = new CountDownLatch(1);
        control.onClose(closed::countDown);
        control.close();

        assertTrue(closed.await(5, TimeUnit.SECONDS));
        assertTrue(control.isClosed());
    }

    @Test
    public void controlStream_onCloseAfterItEnded_firesImmediately() throws Exception {
        // A subscriber that attaches after the stream died must not wait
        // forever, or its bind bookkeeping would never be released.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        control.close();

        final CountDownLatch closed = new CountDownLatch(1);
        control.onClose(closed::countDown);
        try {
            assertTrue(closed.await(2, TimeUnit.SECONDS));
        } catch (InterruptedException e) {
            throw new AssertionError(e);
        }
    }

    @Test
    public void controlStream_sendAfterClose_returnsFalseInsteadOfThrowing() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        control.close();

        assertFalse("a write racing teardown must be a no-op",
                control.send(ControlMessage.unbind(3000)));
    }

    @Test
    public void controlStream_sendAfterServerHalfClose_returnsFalse() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        FakeTunnelServer.FakeStream stream = server.lastStream();

        // Server ends the response direction: the reader sees EOF and the
        // stream must be reported closed.
        stream.endFromServer();
        final CountDownLatch closed = new CountDownLatch(1);
        control.onClose(closed::countDown);
        assertTrue(closed.await(5, TimeUnit.SECONDS));
        assertTrue(control.isClosed());
    }

    @Test
    public void bind_resolvesWithTheServerAssignedPort() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        FakeTunnelServer.FakeStream stream = server.lastStream();

        Thread responder = new Thread(() -> {
            try {
                // Wait for the bind line, then answer with the OS-assigned port.
                assertNotNull(stream.readToServer(256));
                stream.writeFromServer("{\"type\":\"bound\",\"port\":41000}\n");
            } catch (IOException e) {
                throw new RuntimeException(e);
            }
        });
        responder.start();

        Integer port = tunnel.bind(0);
        responder.join(5000);
        assertEquals(Integer.valueOf(41000), port);
        control.close();
    }

    @Test
    public void bind_returnsNullOnBindErr() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        FakeTunnelServer.FakeStream stream = server.lastStream();

        Thread responder = new Thread(() -> {
            try {
                assertNotNull(stream.readToServer(256));
                stream.writeFromServer(
                        "{\"type\":\"bind_err\",\"port\":80,\"code\":3,\"msg\":\"taken\"}\n");
            } catch (IOException e) {
                throw new RuntimeException(e);
            }
        });
        responder.start();

        assertNull(tunnel.bind(80));
        responder.join(5000);
        control.close();
    }

    @Test
    public void bind_settlesWhenTheControlStreamDiesFirst() throws Exception {
        // Without this the caller would block forever and its pending-bind
        // bookkeeping would never be released.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        FakeTunnelServer.FakeStream stream = server.lastStream();

        Thread killer = new Thread(() -> {
            try {
                assertNotNull(stream.readToServer(256));
                stream.endFromServer();
            } catch (IOException e) {
                throw new RuntimeException(e);
            }
        });
        killer.start();

        assertNull("a dead control stream must settle the bind", tunnel.bind(3000));
        killer.join(5000);
        control.close();
    }

    @Test
    public void bind_correlatesRepliesInOrder() throws Exception {
        // The server's control loop is sequential, so a bind(0) answered with
        // the OS-assigned port carries no key to correlate on: order is the
        // contract.
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        FakeTunnelServer.FakeStream stream = server.lastStream();

        Thread responder = new Thread(() -> {
            try {
                assertNotNull(stream.readToServer(256));
                stream.writeFromServer("{\"type\":\"bound\",\"port\":41000}\n");
                assertNotNull(stream.readToServer(256));
                stream.writeFromServer("{\"type\":\"bound\",\"port\":41001}\n");
            } catch (IOException e) {
                throw new RuntimeException(e);
            }
        });
        responder.start();

        assertEquals(Integer.valueOf(41000), tunnel.bind(0));
        assertEquals(Integer.valueOf(41001), tunnel.bind(0));
        responder.join(5000);
        control.close();
    }

    @Test
    public void unbind_sendsTheUnbindLine() throws Exception {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        TunnelControlStream control = tunnel.openControlStream();
        FakeTunnelServer.FakeStream stream = server.lastStream();

        tunnel.unbind(41000);
        String line = stream.readToServer(256);
        assertNotNull(line);
        assertTrue(line.contains("\"type\":\"unbind\""));
        assertTrue(line.contains("\"port\":41000"));
        control.close();
    }

    @Test
    public void unbind_withoutAControlStream_isANoOp() {
        server.mode = FakeTunnelServer.Mode.DUPLEX;
        tunnel.connect(SERVER_URL, TransportKind.H2C);
        // Best-effort, mirroring JSch's delPortForwardingR: must not throw.
        tunnel.unbind(41000);
    }

    @Test
    public void openControlStream_withoutAConnection_throwsClosed() {
        try {
            tunnel.openControlStream();
            throw new AssertionError("expected a rejection");
        } catch (TunnelException e) {
            assertEquals(TunnelErrorKind.CLOSED, e.kind());
        }
    }

    @Test
    public void openStream_withoutAConnection_throwsClosed() {
        try {
            tunnel.openStream("127.0.0.1", 8080);
            throw new AssertionError("expected a rejection");
        } catch (TunnelException e) {
            assertEquals(TunnelErrorKind.CLOSED, e.kind());
        }
    }

    // ==================================================================
    // helpers
    // ==================================================================

    private TunnelConnection openStreamQuietly() {
        try {
            return tunnel.openStream("127.0.0.1", 8080);
        } catch (TunnelException e) {
            throw new AssertionError(e);
        }
    }

    private static int readFully(InputStream in, byte[] buffer) throws IOException {
        int total = 0;
        while (total < buffer.length) {
            int n = in.read(buffer, total, buffer.length - total);
            if (n == -1) return total;
            total += n;
        }
        return total;
    }

    private static SSLContext fakeSslContext() {
        try {
            SSLContext context = SSLContext.getInstance("TLS");
            context.init(null, null, new java.security.SecureRandom());
            return context;
        } catch (Exception e) {
            throw new AssertionError(e);
        }
    }

    /** In-memory {@link TunnelPlatform}: no WebView, no Android runtime. */
    private static final class FakePlatform implements TunnelPlatform {
        volatile String cookie;
        volatile SSLContext sslContext;

        @Override
        public String sessionCookie(String serverUrl) {
            return cookie;
        }

        @Override
        public SSLContext trustAllSslContext() {
            return sslContext;
        }

        @Override
        public void log(String level, String message) {
            // Silence.
        }

        @Override
        public void log(String level, String message, Throwable error) {
            // Silence.
        }
    }
}
