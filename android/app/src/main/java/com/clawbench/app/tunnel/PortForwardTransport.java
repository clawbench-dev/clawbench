package com.clawbench.app.tunnel;

/**
 * The port-forward transport abstraction (design doc §8).
 *
 * <p>{@code BackgroundService}'s port forwarding used to be hard-wired to JSch:
 * every forwarded local port was a {@code setPortForwardingL} on the SSH
 * session. The h2 tunnel carries the same traffic as one HTTP/2 stream per
 * accepted connection, so the local listener and the per-connection stream pump
 * live on the client instead of on the server.
 *
 * <p>Both are driven through this shape so the port bookkeeping, persistence,
 * notification count and reconnect monitor in {@code BackgroundService} stay
 * exactly as they were. The two implementations are:
 *
 * <ul>
 *   <li>{@link SshPortForwardTransport} — a thin adapter over JSch, byte-for-byte
 *       the calls the service used to make inline;</li>
 *   <li>{@link H2PortForwardTransport} — a local {@code ServerSocket} per port
 *       plus one h2 stream per accepted connection.</li>
 * </ul>
 *
 * <p>All methods are blocking (they do network I/O) and must be called off the
 * main thread. The target host/port passed to {@link #addLocal} are
 * <em>already resolved</em> by the caller: the non-localhost case is routed
 * through the server-side reverse proxy ({@code 127.0.0.1:localPort}) by
 * {@code BackgroundService.resolveForwardTarget}, so both transports cannot
 * disagree about what a target means.
 */
public interface PortForwardTransport {

    /**
     * {@code -L}: start forwarding {@code 127.0.0.1:localPort} to the resolved
     * {@code targetHost:targetPort}. Idempotent — a port that is already
     * registered/ listening is treated as success.
     *
     * <p>When {@code localPort} is already taken on this device the transport
     * binds the next free port instead of failing, and returns the port it
     * ACTUALLY bound. The caller must re-key its bookkeeping (and the server
     * registry) to that port, or the UI URL and the server's key would point at
     * a port nothing listens on. A successful return always names a bound port.
     *
     * @return the local port the listener actually bound ({@code localPort}
     *         when it was free).
     * @throws Exception when the forward could not be established, so the
     *                   caller can drop the port from its bookkeeping.
     */
    int addLocal(int localPort, int targetPort, String targetHost) throws Exception;

    /**
     * {@code -L}: stop forwarding {@code localPort} and release its listener.
     * Best-effort: an unknown or already-released port is a no-op.
     */
    void removeLocal(int localPort) throws Exception;

    /**
     * {@code -R}: ask the far side to bind {@code serverPort} and hand accepted
     * connections back to {@code targetHost:targetPort} on this device.
     *
     * <p>SSH pushes the connections down the session; h2 cannot open a
     * server-initiated stream, so the h2 implementation parks them behind a
     * control-stream {@code incoming} notification and claims each one back.
     * Both are blocking and idempotent in the "already bound is success" sense.
     *
     * @throws Exception when the server refused the bind or the control plane
     *                   could not be established, so the caller can drop the
     *                   port from its bookkeeping.
     */
    void addReverse(int serverPort, int targetPort, String targetHost) throws Exception;

    /**
     * {@code -R}: release a server-side bind. Best-effort, mirroring JSch's
     * {@code delPortForwardingR}.
     */
    void removeReverse(int serverPort) throws Exception;

    /** True while the underlying session is up and usable. */
    boolean isConnected();

    /**
     * True when {@code localPort} is actually accepting connections.
     *
     * <p>This is the transport-specific half of the service's post-registration
     * verification: SSH probes the JSch-bound port with a real socket, h2 can
     * answer from the listener it just bound. Keeping it here (rather than
     * probing unconditionally in the service) is also what lets the h2 unit
     * tests avoid opening real sockets, which hang the Gradle worker under
     * Robolectric.
     */
    boolean isLocalReachable(int localPort);

    /**
     * Release everything this transport owns. Idempotent.
     *
     * <p>For h2 that means every local listener, every in-flight connection,
     * every server-side reverse bind and the h2 session itself. For SSH it is a
     * no-op: the JSch session's lifecycle stays with
     * {@code BackgroundService.disconnectInternal()}.
     */
    void close();
}
