package com.clawbench.app.tunnel;

import java.io.Closeable;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;

/**
 * One forwarded TCP connection, carried as one HTTP/2 stream.
 *
 * <p>Read = bytes coming back from the tunnel target, write = bytes going to
 * it. The two directions are independent: closing the writable side
 * ({@link #closeWrite()}) sends END_STREAM and leaves the readable side live,
 * which is exactly TCP half-close — the remote may keep replying after the
 * local side is done sending.
 *
 * <p>Both streams block. Backpressure needs no extra API: a write blocks once
 * the h2 flow-control window is full (the peer is not reading), and simply not
 * calling {@code read()} stops OkHttp from sending WINDOW_UPDATE, which stalls
 * the sender.
 *
 * <p>Not thread-safe for concurrent writers: a connection has one direction
 * per thread, so callers must not write from two threads at once.
 */
public interface TunnelConnection extends Closeable {

    /** Bytes from the tunnel target. {@code -1} on EOF. */
    InputStream getInputStream();

    /** Bytes to the tunnel target. Flushed per write so latency stays low. */
    OutputStream getOutputStream();

    /**
     * Half-close: send END_STREAM on the request direction, keeping the
     * response direction readable. Idempotent.
     *
     * <p>Implemented by closing the request-body sink; on h2 that ends the
     * stream's local half only, so the server can still write its remaining
     * bytes (and a DONE/END_STREAM back) on the same stream.
     */
    void closeWrite() throws IOException;

    /**
     * Tear the whole stream down. Cancels the call, which on h2 is an
     * RST_STREAM scoped to this stream only — every other stream on the same
     * connection keeps running. Idempotent.
     */
    @Override
    void close();

    /** True once {@link #close()} has run. */
    boolean isClosed();
}
