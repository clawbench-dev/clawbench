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

    @Override
    public void addLocal(int localPort, int targetPort, String targetHost) throws Exception {
        Session session = sessions.get();
        if (session == null) throw new JSchException("no SSH session");
        try {
            session.setPortForwardingL("127.0.0.1", localPort, targetHost, targetPort);
        } catch (JSchException e) {
            if (e.getMessage() != null && e.getMessage().contains("already registered")) {
                AppLog.d(TAG, "SSH: port " + localPort + " already registered in JSch, treating as success");
                return;
            }
            throw e;
        }
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
