package com.clawbench.app.tunnel;

import java.io.IOException;

/**
 * A tunnel failure carrying a machine-readable {@link TunnelErrorKind}.
 *
 * <p>Extends {@link IOException} on purpose: every call site that already
 * handles an OkHttp failure keeps compiling, while {@link
 * TunnelErrorKind#of(Throwable)} can recover the precise cause without
 * re-deriving it from an English message.
 *
 * <p>{@code status} is the HTTP status the server answered with, or 0 when the
 * failure happened before a response existed (connect refused, stream reset,
 * local close).
 */
public class TunnelException extends IOException {

    private final TunnelErrorKind kind;
    private final int status;

    public TunnelException(TunnelErrorKind kind, String message) {
        this(kind, message, 0, null);
    }

    public TunnelException(TunnelErrorKind kind, String message, Throwable cause) {
        this(kind, message, 0, cause);
    }

    public TunnelException(TunnelErrorKind kind, String message, int status, Throwable cause) {
        super(message, cause);
        this.kind = kind;
        this.status = status;
    }

    public TunnelErrorKind kind() {
        return kind;
    }

    /** HTTP status from the server, or 0 when there was no response. */
    public int status() {
        return status;
    }
}
