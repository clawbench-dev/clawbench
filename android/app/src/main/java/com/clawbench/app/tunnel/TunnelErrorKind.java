package com.clawbench.app.tunnel;

import java.io.IOException;
import java.io.InterruptedIOException;
import java.net.ConnectException;
import java.net.NoRouteToHostException;
import java.net.SocketTimeoutException;
import java.net.UnknownHostException;

import javax.net.ssl.SSLException;

/**
 * Why a tunnel operation failed, in the vocabulary the rest of the app speaks.
 *
 * <p>Every failure the h2 transport can produce is reduced to exactly one of
 * these, so callers never have to know about OkHttp's exception zoo
 * ({@code StreamResetException}, {@code ConnectionShutdownException},
 * {@code InterruptedIOException("timeout")}, ...) or about the HTTP status
 * codes the tunnel endpoints use.
 *
 * <p>{@link #uiType()} maps onto the vocabulary the existing SSH path already
 * reports to the frontend — {@code "auth" | "network" | "unknown"} (see
 * {@code BackgroundService.getErrorType()} and {@code desktop/src/main/tunnel.ts}
 * classifyError). {@code "hostkey"} is deliberately absent: h2 has no host key
 * concept, and mislabelling a TLS problem as one would send the user to an SSH
 * panel that cannot fix it.
 */
public enum TunnelErrorKind {
    /** 401 (unauthenticated) or 403 (port not allowed / claim token rejected). */
    AUTH,
    /** 502: the server could not dial the forwarded target. The session is fine. */
    TARGET_UNREACHABLE,
    /** 503: the server has no port registry (no forwarding enabled at all). */
    UNAVAILABLE,
    /**
     * The peer does not speak HTTP/2, or an h2 stream was reset
     * (RST_STREAM / GOAWAY / connection shutdown).
     */
    PROTOCOL,
    /** A deadline expired while connecting or waiting for response headers. */
    TIMEOUT,
    /** The socket could not be established: refused, unreachable, DNS, TLS. */
    NETWORK,
    /** The transport was closed locally (or never connected). */
    CLOSED,
    /** Any other non-success HTTP status. */
    HTTP_ERROR,
    /**
     * The local stream pool is saturated, so a new stream could not be
     * started. A capacity condition, not a connectivity one: the session is
     * still healthy and the caller may retry once a stream finishes.
     */
    LIMIT,
    /** Nothing matched. */
    UNKNOWN;

    /**
     * Classify a throwable thrown by OkHttp/okio.
     *
     * <p>OkHttp's h2-specific failures live in {@code okhttp3.internal.http2}
     * and {@code okhttp3.internal.connection}. They are matched by class
     * <em>name</em> rather than by importing them, because they are internal
     * types that R8 is free to rename in a release build; a name check degrades
     * to the generic {@link #NETWORK} branch instead of failing to link.
     */
    public static TunnelErrorKind of(Throwable t) {
        if (t == null) return UNKNOWN;
        if (t instanceof TunnelException) return ((TunnelException) t).kind();

        // Order matters: the specific IOException subclasses must be tested
        // before the catch-all IOException branch at the bottom.
        if (t instanceof SocketTimeoutException) return TIMEOUT;
        // OkHttp's call timeout surfaces as InterruptedIOException("timeout").
        if (t instanceof InterruptedIOException) return TIMEOUT;
        if (t instanceof UnknownHostException
                || t instanceof ConnectException
                || t instanceof NoRouteToHostException
                || t instanceof SSLException) {
            return NETWORK;
        }
        if (t instanceof java.net.ProtocolException
                || hasName(t, "okhttp3.internal.http2.StreamResetException")
                || hasName(t, "okhttp3.internal.http2.ConnectionShutdownException")) {
            return PROTOCOL;
        }
        if (t instanceof IOException) return NETWORK;
        return UNKNOWN;
    }

    /** Classify a non-success HTTP status returned by a tunnel endpoint. */
    public static TunnelErrorKind fromStatus(int status) {
        switch (status) {
            case 401:
            case 403:
                return AUTH;
            case 502:
                return TARGET_UNREACHABLE;
            case 503:
                return UNAVAILABLE;
            default:
                return HTTP_ERROR;
        }
    }

    /** The {@code auth|network|unknown} string the JS bridge and UI expect. */
    public String uiType() {
        switch (this) {
            case AUTH:
                return "auth";
            case TARGET_UNREACHABLE:
            case PROTOCOL:
            case TIMEOUT:
            case NETWORK:
            case LIMIT:
                return "network";
            default:
                return "unknown";
        }
    }

    /** True when this failure means the session itself is unusable. */
    public boolean isConnectionLevel() {
        switch (this) {
            case PROTOCOL:
            case TIMEOUT:
            case NETWORK:
            case CLOSED:
                return true;
            default:
                // AUTH / TARGET_UNREACHABLE / UNAVAILABLE / HTTP_ERROR are all
                // answers from a working h2 session — they must not mark it dead.
                return false;
        }
    }

    private static boolean hasName(Throwable t, String className) {
        for (Class<?> c = t.getClass(); c != null; c = c.getSuperclass()) {
            if (c.getName().equals(className)) return true;
        }
        return false;
    }
}
