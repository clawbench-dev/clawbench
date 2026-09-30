package com.clawbench.app.tunnel;

import org.json.JSONException;
import org.json.JSONObject;

/**
 * One NDJSON line on the {@code -R} control stream.
 *
 * <p>Wire shape and field names are frozen by {@code internal/tunnel/ndjson.go}
 * ({@code ControlMessage}): {@code type} is mandatory, everything else is
 * optional. The server omits zero-valued fields, so a missing {@code port} on a
 * {@code bind} is the documented "let the OS choose" request — hence {@link
 * #hasPort()} rather than a sentinel value.
 *
 * <p>Types (server -> client types must never be sent by the client; the server
 * ignores them): {@code bind}, {@code bound}, {@code bind_err}, {@code incoming},
 * {@code unbind}, {@code unbound}, {@code ping}, {@code pong}.
 */
public final class ControlMessage {

    public static final String BIND = "bind";
    public static final String BOUND = "bound";
    public static final String BIND_ERR = "bind_err";
    public static final String INCOMING = "incoming";
    public static final String UNBIND = "unbind";
    public static final String UNBOUND = "unbound";
    public static final String PING = "ping";
    public static final String PONG = "pong";

    /** {@code bind_err} codes, aligned with the server's mapping. */
    public static final int BIND_ERR_NOT_ALLOWED = 2;
    public static final int BIND_ERR_RESERVED_OR_TAKEN = 3;
    public static final int BIND_ERR_LISTEN_FAILED = 4;
    public static final int BIND_ERR_INTERNAL = 6;

    public final String type;
    public final int port;
    public final int code;
    public final String msg;
    public final String token;

    private final boolean portPresent;

    private ControlMessage(String type, int port, boolean portPresent,
                           int code, String msg, String token) {
        this.type = type;
        this.port = port;
        this.portPresent = portPresent;
        this.code = code;
        this.msg = msg;
        this.token = token;
    }

    public static ControlMessage bind(int port) {
        return new ControlMessage(BIND, port, true, 0, null, null);
    }

    public static ControlMessage unbind(int port) {
        return new ControlMessage(UNBIND, port, true, 0, null, null);
    }

    public static ControlMessage ping() {
        return new ControlMessage(PING, 0, false, 0, null, null);
    }

    /** True when the wire message carried a {@code port} field. */
    public boolean hasPort() {
        return portPresent;
    }

    /** True for a server -> client type (never legal in the client's direction). */
    public boolean isServerToClient() {
        return BOUND.equals(type) || BIND_ERR.equals(type) || INCOMING.equals(type)
                || UNBOUND.equals(type) || PONG.equals(type);
    }

    /**
     * Parse one control line. Returns {@code null} for anything that is not a
     * well-formed known message — mirroring the server's
     * {@code readControlLoop}, where a single bad byte must not drop every
     * reverse port.
     */
    public static ControlMessage parse(String line) {
        if (line == null) return null;
        String trimmed = line.trim();
        if (trimmed.isEmpty()) return null;
        try {
            JSONObject o = new JSONObject(trimmed);
            String type = o.optString("type", "");
            if (!knownType(type)) return null;
            boolean portPresent = o.has("port") && !o.isNull("port");
            int port = portPresent ? o.optInt("port", 0) : 0;
            int code = o.optInt("code", 0);
            String msg = o.isNull("msg") ? null : o.optString("msg", null);
            String token = o.isNull("token") ? null : o.optString("token", null);
            return new ControlMessage(type, port, portPresent, code, msg, token);
        } catch (JSONException e) {
            return null;
        }
    }

    /**
     * Render as one NDJSON line, newline included.
     *
     * <p>Fields are added only when meaningful so the JSON matches what the
     * server's {@code omitempty} would produce; a {@code bind(0)} deliberately
     * omits {@code port} because zero means "OS-assigned" and the server reads
     * a missing port exactly the same way, so emitting it would be a needless
     * wire difference.
     */
    public String encode() {
        JSONObject o = new JSONObject();
        try {
            o.put("type", type);
            if (portPresent && port != 0) o.put("port", port);
            if (code != 0) o.put("code", code);
            if (msg != null) o.put("msg", msg);
            if (token != null) o.put("token", token);
        } catch (JSONException e) {
            // Every value is a String or int, so this is unreachable.
            return null;
        }
        return o.toString() + "\n";
    }

    private static boolean knownType(String t) {
        switch (t) {
            case BIND:
            case BOUND:
            case BIND_ERR:
            case INCOMING:
            case UNBIND:
            case UNBOUND:
            case PING:
            case PONG:
                return true;
            default:
                return false;
        }
    }

    @Override
    public String toString() {
        return "ControlMessage{" + type + " port=" + (portPresent ? port : "-")
                + " code=" + code + " msg=" + msg + " token=" + (token != null) + "}";
    }
}
