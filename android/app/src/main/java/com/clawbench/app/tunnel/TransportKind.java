package com.clawbench.app.tunnel;

/**
 * Which wire form of HTTP/2 a tunnel session uses.
 *
 * <p>Both carry the same protocol (one h2 stream per forwarded TCP connection);
 * they differ only in how the connection is established. The priority chain is
 * {@link #TLS} then {@link #H2C} (design doc §2.3) — h2-over-TLS is preferred
 * because it is indistinguishable from ordinary HTTPS to a middlebox, and h2c
 * is the default for the plaintext deployment ClawBench ships by default.
 *
 * <p>{@link #wireName()} is the string reported across the JS bridge and shown
 * in the UI, and matches the Electron transport's {@code H2TransportKind}.
 */
public enum TransportKind {
    /** HTTP/2 over TLS (ALPN {@code h2}), self-signed certificates accepted. */
    TLS("tls"),
    /** HTTP/2 cleartext with prior knowledge (no Upgrade dance). */
    H2C("h2c");

    private final String wireName;

    TransportKind(String wireName) {
        this.wireName = wireName;
    }

    public String wireName() {
        return wireName;
    }

    /** The URL scheme a stream on this transport uses. */
    public String scheme() {
        return this == TLS ? "https" : "http";
    }

    /** Parse a wire name, returning {@code null} for anything unknown. */
    public static TransportKind fromWire(String name) {
        if (name == null) return null;
        for (TransportKind k : values()) {
            if (k.wireName.equals(name)) return k;
        }
        return null;
    }

    @Override
    public String toString() {
        return wireName;
    }
}
