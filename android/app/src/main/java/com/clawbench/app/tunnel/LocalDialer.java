package com.clawbench.app.tunnel;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Socket;

/**
 * Dials the local target of a reverse ({@code -R}) forward.
 *
 * <p>A seam, not a configuration knob: production dials a real socket
 * ({@link #DEFAULT}), while unit tests inject a fake so the claim path can be
 * driven without opening a real port. A bare {@code new Socket()} in a
 * Robolectric test leaves the Gradle worker un-exitable (measured while
 * building T9's transport).
 */
public interface LocalDialer {

    /** Connect to {@code host:port}. The caller owns (and must close) the socket. */
    Socket dial(String host, int port) throws IOException;

    /** How long a reverse-forward target dial may take before giving up. */
    int DIAL_TIMEOUT_MS = 10_000;

    /** Production dialer: a plain blocking TCP connect with a bounded timeout. */
    LocalDialer DEFAULT = (host, port) -> {
        Socket socket = new Socket();
        try {
            socket.connect(new InetSocketAddress(host, port), DIAL_TIMEOUT_MS);
            return socket;
        } catch (IOException e) {
            // connect() failure leaves an unconnected socket behind; close it
            // so a retrying caller cannot leak descriptors.
            try {
                socket.close();
            } catch (IOException ignored) {
                // Nothing useful to report on an unconnected socket.
            }
            throw e;
        }
    };
}
