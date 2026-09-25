package com.clawbench.app.tunnel;

/**
 * The tunnel transport abstraction (design doc §8).
 *
 * <p>BackgroundService's port forwarding used to be hard-wired to JSch: every
 * forwarded TCP connection was an SSH channel. The HTTP/2 stream tunnel carries
 * the same traffic as one h2 stream per connection, over the main port only.
 * Both are driven through this shape so the port bookkeeping, persistence and
 * reconnect monitor stay exactly as they were (the port-forward transports
 * replace the {@code setPortForwardingL/R} calls; this interface is what they
 * call).
 *
 * <p>The interface is deliberately thin — it opens and owns streams and the
 * session, and knows nothing about {@code forwardedPorts}, listeners or
 * reconnects. Keeping the SSH implementation out of this package is the point:
 * {@link H2TunnelStream} is the only implementation today, and the SSH path
 * lives in BackgroundService because it is a thin adapter over JSch's own state.
 *
 * <p>All methods are blocking. Callers must invoke them off the main thread.
 */
public interface TunnelStream {

    /**
     * Establish (or reuse) the underlying h2 session against {@code serverUrl}.
     *
     * <p>Probes the transports in the design doc's order (TLS then h2c), or the
     * remembered one first when {@code preferred} is set, and reports which one
     * actually worked so the caller can remember it — a plaintext deployment
     * otherwise pays a wasted TLS rejection on every reconnect.
     *
     * <p>Success is decided by a real request round-trip, never by "the socket
     * connected": the h2 session is not proven to speak HTTP/2 until a response
     * comes back over it (see {@code H2TunnelStream.probe()}).
     *
     * @param serverUrl the same URL the WebView is pointed at, e.g.
     *                  {@code http://127.0.0.1:20000}. Its host and port are the
     *                  tunnel endpoint; its scheme is ignored, because the
     *                  transport chain probes TLS first regardless.
     * @return the transport that carried the session, or {@code null} on failure
     *         (the reason is then available from {@link #getLastError()}).
     */
    TransportKind connect(String serverUrl, TransportKind preferred);

    /** Reuse an already-established session if it is still healthy. */
    boolean isConnected();

    /** The transport carrying the live session, or {@code null} if not connected. */
    TransportKind getKind();

    /**
     * {@code -L}: dial one stream to {@code host:port} on the server side.
     *
     * <p>The returned connection is already relaying bytes in both directions.
     * Throws {@link TunnelException} when the server could not establish the
     * TCP connection (dial failure, disallowed port, unauthenticated), so the
     * caller can close the local socket instead of piping it into nothing.
     */
    TunnelConnection openStream(String host, int port) throws TunnelException;

    /**
     * {@code -R}: open the data stream that redeems an {@code incoming} claim
     * token. The token is single-use and bound to this h2 connection.
     */
    TunnelConnection openClaimStream(String token) throws TunnelException;

    /**
     * {@code -R}: open (or reuse) the long-lived NDJSON control stream.
     *
     * <p>Only one control stream may exist per session: two would mean two
     * competing sets of bind bookkeeping, so this is idempotent and returns the
     * live stream.
     */
    TunnelControlStream openControlStream() throws TunnelException;

    /**
     * {@code -R}: ask the server to bind {@code serverPort} and wait for the
     * reply.
     *
     * @return the actual bound port (differs from the request when
     *         {@code serverPort == 0}), or {@code null} on {@code bind_err} or
     *         when the control stream dies first.
     */
    Integer bind(int serverPort);

    /**
     * {@code -R}: release a server-side bind. Best-effort and fire-and-forget,
     * mirroring JSch's {@code delPortForwardingR}: the server's {@code unbound}
     * acknowledgement carries nothing the caller needs.
     */
    void unbind(int serverPort);

    /**
     * Tear the session down. Every stream on it dies with it, by design.
     * Idempotent.
     */
    void close();

    /** Human-readable reason the last {@link #connect} failed ("" on success). */
    String getLastError();

    /** How the last failure should be classified. */
    TunnelErrorKind getLastErrorKind();

    /** True while any tunnel stream is open (drives WifiLock retention, T12). */
    boolean hasActiveStreams();

    /** Number of streams currently open. Diagnostics and tests. */
    int activeStreamCount();
}
