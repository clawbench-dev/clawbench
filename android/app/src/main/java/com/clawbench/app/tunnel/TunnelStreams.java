package com.clawbench.app.tunnel;

import androidx.annotation.Nullable;

/**
 * Process-wide holder for the tunnel transport.
 *
 * <p>There is exactly one tunnel per app: the control stream and every data
 * stream must share one {@code OkHttpClient} so they land on a single TCP
 * connection (OkHttp's connection pool is per-client). That is not an
 * optimization — a {@code -R} claim token is scoped to the h2 connection that
 * minted it, so a claim stream opened from a different client would be
 * rejected as foreign. A single holder makes that impossible to get wrong.
 *
 * <p>T10/T11 call {@link #get()} to obtain the transport; {@link #reset()} is
 * for tests and for a full teardown.
 */
public final class TunnelStreams {

    private static volatile TunnelStream instance;
    private static volatile TunnelPlatform platformOverride;

    private TunnelStreams() {
    }

    /** The shared transport, creating it on first use. */
    public static TunnelStream get() {
        TunnelStream current = instance;
        if (current != null) return current;
        synchronized (TunnelStreams.class) {
            if (instance == null) {
                TunnelPlatform platform = platformOverride;
                if (platform == null) {
                    platform = new AndroidTunnelPlatform();
                }
                instance = new H2TunnelStream(platform);
            }
            return instance;
        }
    }

    /**
     * Tear down and forget the shared transport. The next {@link #get()} builds
     * a fresh one (and therefore a fresh client and connection).
     */
    public static void reset() {
        TunnelStream current;
        synchronized (TunnelStreams.class) {
            current = instance;
            instance = null;
        }
        if (current != null) current.close();
    }

    /**
     * Install a platform implementation. Tests only; production always uses
     * {@link AndroidTunnelPlatform}.
     */
    static void setPlatformForTesting(@Nullable TunnelPlatform platform) {
        synchronized (TunnelStreams.class) {
            platformOverride = platform;
            TunnelStream current = instance;
            instance = null;
            if (current != null) current.close();
        }
    }
}
