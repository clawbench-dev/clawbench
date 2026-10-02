package com.clawbench.app.tunnel;

import com.clawbench.app.AppLog;
import com.jcraft.jsch.JSchException;
import com.jcraft.jsch.Session;

/**
 * The SSH (JSch) port-forward transport.
 *
 * <p>A thin adapter over the {@code com.jcraft.jsch.Session} the service owns:
 * every method is one JSch call with exactly the error handling the service
 * used to perform inline. In particular {@code "already registered"} is still
 * treated as success, because {@code ensureConnection()}'s replay loop may have
 * set the forward up before the explicit add ran.
 *
 * <p>It deliberately does <em>not</em> own the session: the session field, the
 * connection monitor and {@code disconnectInternal()} all stay in
 * {@code BackgroundService}. That is what keeps the SSH path unchanged — the
 * only thing that moved is which object issues the JSch call.
 */
public final class SshPortForwardTransport implements PortForwardTransport {

    private static final String TAG = "ClawBench";

    /** Supplies the live JSch session, or {@code null} when there is none. */
    public interface SessionProvider {
        Session get();
    }

    /** Probes whether a local port is actually accepting connections. */
    public interface LocalProbe {
        boolean isReachable(int port);
    }

    private final SessionProvider sessions;
    private final LocalProbe probe;

    public SshPortForwardTransport(SessionProvider sessions, LocalProbe probe) {
        this.sessions = sessions;
        this.probe = probe;
    }

    /**
     * How many ports above the requested one to try before giving up. JSch's
     * {@code setPortForwardingL} returns the port it actually bound, and passing
     * {@code 0} asks the OS for one — but only the requested/next-neighbour walk
     * is used here (matching the h2 transport) so a rebind stays predictable.
     */
    private static final int LOCAL_PORT_SCAN_LIMIT = 50;

    @Override
    public int addLocal(int localPort, int targetPort, String targetHost) throws Exception {
        Session session = sessions.get();
        if (session == null) throw new JSchException("no SSH session");
        // 0 is not a usable forward key (the frontend reaches the forward at
        // localhost:{localPort}); reject it before walking candidates.
        if (localPort <= 0 || localPort > 65535) {
            throw new JSchException("invalid local port: " + localPort);
        }
        JSchException lastConflict = null;
        for (int candidate : localPortCandidates(localPort)) {
            try {
                // The return value is the local port JSch actually bound, which
                // equals `candidate` for an explicit port. Capturing it (rather
                // than discarding it) keeps this correct if a 0 candidate is ever
                // added, and is what the caller re-keys the registry to.
                return session.setPortForwardingL("127.0.0.1", candidate, targetHost, targetPort);
            } catch (JSchException e) {
                String msg = e.getMessage();
                if (msg != null && msg.contains("already registered")) {
                    AppLog.d(TAG, "SSH: port " + candidate + " already registered in JSch, treating as success");
                    return candidate;
                }
                if (msg != null && msg.contains("cannot be bound")) {
                    // Occupied on this device — try the next candidate.
                    lastConflict = e;
                    continue;
                }
                throw e;
            }
        }
        throw lastConflict != null ? lastConflict : new JSchException("no free local port");
    }

    /** Requested port, then its next neighbours (see the desktop shell's scan). */
    private static int[] localPortCandidates(int requested) {
        int start = Math.max(1, requested);
        int count = Math.min(start + LOCAL_PORT_SCAN_LIMIT, 65535) - start + 1;
        int[] out = new int[Math.max(0, count)];
        for (int i = 0; i < out.length; i++) {
            out[i] = start + i;
        }
        return out;
    }

    @Override
    public void removeLocal(int localPort) throws Exception {
        Session session = sessions.get();
        // Mirrors the pre-tunnel guard: a disconnected/absent session has
        // nothing to unregister, and JSch throws "Session is down" if asked.
        if (session == null || !session.isConnected()) {
            AppLog.i(TAG, "SSH: removePortForward skipping delPortForwardingL (session "
                    + (session == null ? "null" : "disconnected") + ") for port " + localPort);
            return;
        }
        session.delPortForwardingL(localPort);
    }

    @Override
    public void addReverse(int serverPort, int targetPort, String targetHost) throws Exception {
        Session session = sessions.get();
        if (session == null) throw new JSchException("no SSH session");
        try {
            session.setPortForwardingR("127.0.0.1", serverPort, targetHost, targetPort);
        } catch (JSchException e) {
            if (e.getMessage() != null && e.getMessage().contains("already registered")) {
                AppLog.d(TAG, "SSH: reverse forward " + serverPort + " already registered");
                return;
            }
            throw e;
        }
    }

    @Override
    public void removeReverse(int serverPort) throws Exception {
        Session session = sessions.get();
        if (session == null || !session.isConnected()) {
            AppLog.i(TAG, "SSH: removeReversePortForward skipping delPortForwardingR (session "
                    + (session == null ? "null" : "disconnected") + ") for port " + serverPort);
            return;
        }
        session.delPortForwardingR(serverPort);
    }

    @Override
    public boolean isConnected() {
        Session session = sessions.get();
        return session != null && session.isConnected();
    }

    @Override
    public boolean isLocalReachable(int localPort) {
        return probe.isReachable(localPort);
    }

    @Override
    public void close() {
        // The JSch session is owned by BackgroundService (disconnectInternal);
        // disconnecting here would fight the reconnect monitor and the session
        // field's single owner.
    }
}
