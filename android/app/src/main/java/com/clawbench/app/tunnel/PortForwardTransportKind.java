package com.clawbench.app.tunnel;

/**
 * Which transport port forwarding uses, mirroring the server's
 * {@code port_forward.transport} setting (design doc §11).
 *
 * <p>{@link #SSH} is the default, so an install that has not opted into the h2
 * tunnel behaves exactly as it did before the tunnel existed. Android's choice
 * is a local boolean toggle, which has only two states, so there is no
 * "prefer h2, fall back to SSH" value here: exactly one transport runs. The
 * desktop client keeps its own {@code 'both'} preference; Android has no
 * equivalent.
 */
public enum PortForwardTransportKind {
    /** SSH channels only (JSch). The default. */
    SSH("ssh"),
    /** HTTP/2 streams only. */
    H2("h2");

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
     *
     * <p>{@code "both"} is now one of those unknown values and therefore maps
     * to {@link #SSH}. That is intentional and harmless: Android's local
     * boolean toggle cannot express "prefer h2, fall back to SSH", no caller
     * on this platform sends {@code "both"} any more, and the fail-safe
     * direction (do not switch transports on a value we cannot honour) is the
     * one we want even if a stale value did arrive.
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
