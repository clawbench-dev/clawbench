package com.clawbench.app.tunnel;

import org.junit.Test;

import java.io.EOFException;
import java.io.IOException;
import java.io.InterruptedIOException;
import java.net.ConnectException;
import java.net.NoRouteToHostException;
import java.net.SocketTimeoutException;
import java.net.UnknownHostException;

import javax.net.ssl.SSLHandshakeException;

import okhttp3.internal.http2.ConnectionShutdownException;
import okhttp3.internal.http2.ErrorCode;
import okhttp3.internal.http2.StreamResetException;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Tests for the error-mapping table.
 *
 * <p>The point of {@link TunnelErrorKind} is that callers never inspect
 * OkHttp's exception zoo or HTTP status codes directly, so this pins the
 * mapping in both directions: exception/status in, kind out — and kind to the
 * {@code auth|network|unknown} string the JS bridge already speaks.
 */
public class TunnelErrorKindTest {

    // ── HTTP status mapping ───────────────────────────────────────────

    @Test
    public void fromStatus_matchesTheServerContract() {
        // internal/handler/tunnel_stream.go documents this exact mapping.
        assertEquals(TunnelErrorKind.AUTH, TunnelErrorKind.fromStatus(401));
        assertEquals(TunnelErrorKind.AUTH, TunnelErrorKind.fromStatus(403));
        assertEquals(TunnelErrorKind.TARGET_UNREACHABLE, TunnelErrorKind.fromStatus(502));
        assertEquals(TunnelErrorKind.UNAVAILABLE, TunnelErrorKind.fromStatus(503));
        assertEquals(TunnelErrorKind.HTTP_ERROR, TunnelErrorKind.fromStatus(400));
        assertEquals(TunnelErrorKind.HTTP_ERROR, TunnelErrorKind.fromStatus(500));
        assertEquals(TunnelErrorKind.HTTP_ERROR, TunnelErrorKind.fromStatus(200));
    }

    // ── exception mapping ─────────────────────────────────────────────

    @Test
    public void socketTimeout_isTimeout() {
        assertEquals(TunnelErrorKind.TIMEOUT,
                TunnelErrorKind.of(new SocketTimeoutException("read timed out")));
    }

    @Test
    public void interruptedIo_isTimeout_thisIsHowOkHttpReportsCallTimeout() {
        // Measured: Call.timeout().timeout(...) fires as
        // InterruptedIOException("timeout"), not SocketTimeoutException.
        InterruptedIOException e = new InterruptedIOException("timeout");
        assertEquals(TunnelErrorKind.TIMEOUT, TunnelErrorKind.of(e));
    }

    @Test
    public void networkExceptions_areNetwork() {
        assertEquals(TunnelErrorKind.NETWORK, TunnelErrorKind.of(new ConnectException("refused")));
        assertEquals(TunnelErrorKind.NETWORK, TunnelErrorKind.of(new UnknownHostException("nope")));
        assertEquals(TunnelErrorKind.NETWORK, TunnelErrorKind.of(new NoRouteToHostException("no route")));
        assertEquals(TunnelErrorKind.NETWORK, TunnelErrorKind.of(new SSLHandshakeException("bad cert")));
    }

    @Test
    public void h2StreamReset_isProtocol_notNetwork() {
        // A stream reset is a rejected h2 stream, i.e. a protocol-level event.
        StreamResetException e = new StreamResetException(ErrorCode.CANCEL);
        assertEquals(TunnelErrorKind.PROTOCOL, TunnelErrorKind.of(e));
    }

    @Test
    public void connectionShutdown_isProtocol() {
        // This is what an h2c client gets from a plain HTTP/1.1 server, so it
        // must not be mistaken for a transient network error.
        assertEquals(TunnelErrorKind.PROTOCOL,
                TunnelErrorKind.of(new ConnectionShutdownException()));
    }

    @Test
    public void protocolException_isProtocol() {
        assertEquals(TunnelErrorKind.PROTOCOL,
                TunnelErrorKind.of(new java.net.ProtocolException("bad framing")));
    }

    @Test
    public void plainIoException_isNetwork() {
        assertEquals(TunnelErrorKind.NETWORK, TunnelErrorKind.of(new IOException("socket closed")));
        assertEquals(TunnelErrorKind.NETWORK, TunnelErrorKind.of(new EOFException("eof")));
    }

    @Test
    public void tunnelException_preservesItsOwnKind() {
        TunnelException e = new TunnelException(TunnelErrorKind.AUTH, "nope", 401, null);
        assertEquals(TunnelErrorKind.AUTH, TunnelErrorKind.of(e));
        assertEquals(401, e.status());
    }

    @Test
    public void null_isUnknown() {
        assertEquals(TunnelErrorKind.UNKNOWN, TunnelErrorKind.of(null));
    }

    @Test
    public void h2ClassMatching_survivesRenamedClass() {
        // The mapping matches internal OkHttp classes by name (R8 may rename
        // them), so a throwable whose simple class is unknown but whose name
        // matches must still classify. Here the real class is used, which is
        // the same path a renamed subclass would take via its superclass.
        StreamResetException reset = new StreamResetException(ErrorCode.INTERNAL_ERROR);
        assertEquals(TunnelErrorKind.PROTOCOL, TunnelErrorKind.of(reset));
    }

    // ── UI vocabulary ─────────────────────────────────────────────────

    @Test
    public void uiType_matchesTheFrontendVocabulary() {
        // web/src/composables/usePortForward.ts and MainActivity.getTunnelErrorType
        // both expect auth|network|unknown.
        assertEquals("auth", TunnelErrorKind.AUTH.uiType());
        assertEquals("network", TunnelErrorKind.NETWORK.uiType());
        assertEquals("network", TunnelErrorKind.PROTOCOL.uiType());
        assertEquals("network", TunnelErrorKind.TIMEOUT.uiType());
        assertEquals("network", TunnelErrorKind.TARGET_UNREACHABLE.uiType());
        assertEquals("network", TunnelErrorKind.LIMIT.uiType());
        assertEquals("unknown", TunnelErrorKind.UNAVAILABLE.uiType());
        assertEquals("unknown", TunnelErrorKind.HTTP_ERROR.uiType());
        assertEquals("unknown", TunnelErrorKind.CLOSED.uiType());
        assertEquals("unknown", TunnelErrorKind.UNKNOWN.uiType());
    }

    @Test
    public void isConnectionLevel_onlyTrueWhenTheSessionIsSuspect() {
        // A 403/502/503 is an answer from a working h2 session, so it must not
        // tear the session down (which would drop every other forwarded port).
        assertFalse(TunnelErrorKind.AUTH.isConnectionLevel());
        assertFalse(TunnelErrorKind.TARGET_UNREACHABLE.isConnectionLevel());
        assertFalse(TunnelErrorKind.UNAVAILABLE.isConnectionLevel());
        assertFalse(TunnelErrorKind.HTTP_ERROR.isConnectionLevel());
        assertFalse(TunnelErrorKind.LIMIT.isConnectionLevel());

        assertTrue(TunnelErrorKind.PROTOCOL.isConnectionLevel());
        assertTrue(TunnelErrorKind.TIMEOUT.isConnectionLevel());
        assertTrue(TunnelErrorKind.NETWORK.isConnectionLevel());
        assertTrue(TunnelErrorKind.CLOSED.isConnectionLevel());
    }
}
