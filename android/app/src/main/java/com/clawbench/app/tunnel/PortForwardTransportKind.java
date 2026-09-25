package com.clawbench.app.tunnel;

/**
 * Which transport port forwarding uses, mirroring the server's
 * {@code port_forward.transport} setting (design doc §11).
 *
 * <p>{@link #SSH} is the default, so an install that has not opted into the h2
 * tunnel behaves exactly as it did before the tunnel existed. {@link #BOTH}
 * probes h2 first and falls back to SSH, matching the desktop transport's
 * candidate order.
 */
public enum PortForwardTransportKind {
    /** SSH channels only (JSch). The default. */
    SSH("ssh"),
    /** HTTP/2 streams only. */
    H2("h2"),
    /** Prefer h2, fall back to SSH when the h2 session cannot be established. */
    BOTH("both");

    private final String wireName;

    PortForwardTransportKind(String wireName) {
        this.wireName = wireName;
    }

    public String wireName() {
        return wireName;
    }

    /**
     * Parse a wire name. Unknown or {@code null} values fall back to
     * {@link #SSH}: the setting arrives from the server/JS bridge, and a value
     * this build does not understand must never silently switch transports.
     */
    public static PortForwardTransportKind fromWire(String name) {
        if (name != null) {
            for (PortForwardTransportKind kind : values()) {
                if (kind.wireName.equals(name)) return kind;
            }
        }
        return SSH;
    }

    @Override
    public String toString() {
        return wireName;
    }
}
