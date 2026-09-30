package com.clawbench.app.tunnel;

import java.io.Closeable;

/**
 * The long-lived {@code -R} control plane: write one NDJSON line, receive them
 * by line.
 *
 * <p>The request body carries {@code bind}/{@code unbind}/{@code ping}; the
 * response body carries {@code bound}/{@code bind_err}/{@code incoming}/
 * {@code unbound}/{@code pong}. Reads are delivered on the control stream's own
 * reader thread, so a handler must not block: a stalled handler would stall
 * every reverse port on that control stream.
 */
public interface TunnelControlStream extends Closeable {

    /**
     * Send one message. Returns false when the stream is already closed.
     *
     * <p>Never throws: a control write races the stream's teardown on every
     * disconnect, and a bind request losing that race must not crash the
     * caller. {@link #onClose(Runnable)} reports the stream's death instead.
     */
    boolean send(ControlMessage msg);

    /** Register the handler invoked for each parsed line. */
    void onMessage(MessageHandler handler);

    /** Register the handler invoked once when the stream ends (EOF or error). */
    void onClose(Runnable handler);

    /**
     * Half-close the request direction, then tear the stream down. Idempotent.
     *
     * <p>Declared without {@code throws} so callers do not need to handle an
     * exception a best-effort teardown can never usefully report.
     */
    @Override
    void close();

    /** True once the stream has ended. */
    boolean isClosed();

    /** Why the stream ended, or {@code null} while it is healthy. */
    TunnelErrorKind lastErrorKind();

    /** Reason the stream ended, or {@code null} while it is healthy. */
    String lastErrorMessage();

    interface MessageHandler {
        void onMessage(ControlMessage msg);
    }
}
