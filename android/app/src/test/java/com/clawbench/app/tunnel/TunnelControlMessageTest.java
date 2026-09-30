package com.clawbench.app.tunnel;

import org.json.JSONObject;
import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

/**
 * Pure JUnit tests for the {@code -R} control-plane NDJSON codec.
 *
 * <p>The wire format is frozen by the server's {@code internal/tunnel/ndjson.go}
 * ({@code ControlMessage} with {@code omitempty}), so these tests pin the field
 * names and the omission rules rather than just round-tripping.
 *
 * <p>No Android dependencies: {@code ControlMessage} uses only org.json, which
 * the test source set pulls in as a real implementation (the android.jar stub
 * returns defaults under {@code returnDefaultValues}).
 */
public class TunnelControlMessageTest {

    // ── encode ────────────────────────────────────────────────────────

    @Test
    public void bind_encodesTypeAndPort_withTrailingNewline() throws Exception {
        String line = ControlMessage.bind(3000).encode();
        assertNotNull(line);
        assertTrue("NDJSON line must end with a newline", line.endsWith("\n"));

        JSONObject o = new JSONObject(line.trim());
        assertEquals("bind", o.getString("type"));
        assertEquals(3000, o.getInt("port"));
        assertFalse("a bind must not carry code/msg/token",
                o.has("code") || o.has("msg") || o.has("token"));
    }

    @Test
    public void bindZero_omitsPort_soServerReadsOsAssigned() throws Exception {
        // Zero means "let the OS choose". The server treats a missing port the
        // same way, so emitting "port":0 would be a needless wire difference.
        String line = ControlMessage.bind(0).encode();
        assertNotNull(line);
        JSONObject o = new JSONObject(line.trim());
        assertEquals("bind", o.getString("type"));
        assertFalse("port must be omitted for bind(0)", o.has("port"));
    }

    @Test
    public void unbind_encodesPort() throws Exception {
        JSONObject o = new JSONObject(ControlMessage.unbind(8080).encode().trim());
        assertEquals("unbind", o.getString("type"));
        assertEquals(8080, o.getInt("port"));
    }

    @Test
    public void ping_carriesOnlyType() throws Exception {
        JSONObject o = new JSONObject(ControlMessage.ping().encode().trim());
        assertEquals("ping", o.getString("type"));
        assertEquals(1, o.length());
    }

    // ── parse ─────────────────────────────────────────────────────────

    @Test
    public void parse_bound_keepsPort() {
        ControlMessage m = ControlMessage.parse("{\"type\":\"bound\",\"port\":41000}");
        assertNotNull(m);
        assertEquals(ControlMessage.BOUND, m.type);
        assertTrue(m.hasPort());
        assertEquals(41000, m.port);
    }

    @Test
    public void parse_incoming_keepsTokenAndPort() {
        ControlMessage m = ControlMessage.parse(
                "{\"type\":\"incoming\",\"port\":9000,\"token\":\"a1b2c3\"}");
        assertNotNull(m);
        assertEquals(ControlMessage.INCOMING, m.type);
        assertEquals(9000, m.port);
        assertEquals("a1b2c3", m.token);
    }

    @Test
    public void parse_bindErr_keepsCodeAndMsg() {
        ControlMessage m = ControlMessage.parse(
                "{\"type\":\"bind_err\",\"port\":80,\"code\":2,\"msg\":\"not allowed\"}");
        assertNotNull(m);
        assertEquals(ControlMessage.BIND_ERR, m.type);
        assertEquals(2, m.code);
        assertEquals("not allowed", m.msg);
    }

    @Test
    public void parse_toleratesTrailingNewlineAndCr() {
        assertNotNull(ControlMessage.parse("{\"type\":\"pong\"}\n"));
        assertNotNull(ControlMessage.parse("{\"type\":\"pong\"}\r\n"));
    }

    @Test
    public void parse_boundWithoutPort_reportsNoPort() {
        // A malformed-but-typed `bound` must not look like "bound to port 0".
        ControlMessage m = ControlMessage.parse("{\"type\":\"bound\"}");
        assertNotNull(m);
        assertFalse(m.hasPort());
    }

    @Test
    public void parse_rejectsMalformedAndUnknownTypes() {
        // Mirrors the server's readControlLoop: one bad line is skipped, never
        // fatal, so it must be reported as null rather than throwing.
        assertNull(ControlMessage.parse(null));
        assertNull(ControlMessage.parse(""));
        assertNull(ControlMessage.parse("   "));
        assertNull(ControlMessage.parse("not json"));
        assertNull(ControlMessage.parse("{\"port\":1}"));
        assertNull(ControlMessage.parse("{\"type\":\"bogus\"}"));
        assertNull(ControlMessage.parse("[1,2,3]"));
    }

    @Test
    public void parse_acceptsAllEightProtocolTypes() {
        String[] types = {"bind", "bound", "bind_err", "incoming",
                "unbind", "unbound", "ping", "pong"};
        for (String type : types) {
            assertNotNull("type " + type + " must parse",
                    ControlMessage.parse("{\"type\":\"" + type + "\"}"));
        }
    }

    @Test
    public void isServerToClient_matchesTheProtocolDirectionTable() {
        assertTrue(ControlMessage.parse("{\"type\":\"bound\"}").isServerToClient());
        assertTrue(ControlMessage.parse("{\"type\":\"bind_err\"}").isServerToClient());
        assertTrue(ControlMessage.parse("{\"type\":\"incoming\"}").isServerToClient());
        assertTrue(ControlMessage.parse("{\"type\":\"unbound\"}").isServerToClient());
        assertTrue(ControlMessage.parse("{\"type\":\"pong\"}").isServerToClient());

        assertFalse(ControlMessage.parse("{\"type\":\"bind\"}").isServerToClient());
        assertFalse(ControlMessage.parse("{\"type\":\"unbind\"}").isServerToClient());
        assertFalse(ControlMessage.parse("{\"type\":\"ping\"}").isServerToClient());
    }

    @Test
    public void bindErrCodes_matchTheServerMapping() {
        // internal/tunnel/ndjson.go: 2=not allowed, 3=reserved/taken,
        // 4=listen failed, 6=internal.
        assertEquals(2, ControlMessage.BIND_ERR_NOT_ALLOWED);
        assertEquals(3, ControlMessage.BIND_ERR_RESERVED_OR_TAKEN);
        assertEquals(4, ControlMessage.BIND_ERR_LISTEN_FAILED);
        assertEquals(6, ControlMessage.BIND_ERR_INTERNAL);
    }

    @Test
    public void roundTrip_preservesEveryField() {
        ControlMessage original = ControlMessage.parse(
                "{\"type\":\"incoming\",\"port\":1234,\"code\":3,\"msg\":\"taken\",\"token\":\"tok\"}");
        assertNotNull(original);
        ControlMessage reparsed = ControlMessage.parse(original.encode().trim());
        assertNotNull(reparsed);
        assertEquals(original.type, reparsed.type);
        assertEquals(original.port, reparsed.port);
        assertEquals(original.code, reparsed.code);
        assertEquals(original.msg, reparsed.msg);
        assertEquals(original.token, reparsed.token);
    }
}
