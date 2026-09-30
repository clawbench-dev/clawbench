package com.clawbench.app.tunnel;

import java.io.IOException;
import java.net.ServerSocket;

/**
 * Creates the {@link ServerSocket} a local forward listens on.
 *
 * <p>A seam, not a configuration knob: production always binds a real socket
 * ({@link #DEFAULT}), while unit tests inject a fake so the listener lifecycle
 * and the accept loop can be driven without opening a real port. That matters
 * more than usual here — a bare {@code new Socket()}/{@code ServerSocket()} in
 * a Robolectric test leaves the Gradle worker un-exitable (measured while
 * building T9's transport).
 */
public interface ServerSocketFactory {

    /** A fresh, unbound server socket. The caller binds it. */
    ServerSocket create() throws IOException;

    /** Production factory: a plain unbound {@link ServerSocket}. */
    ServerSocketFactory DEFAULT = ServerSocket::new;
}
