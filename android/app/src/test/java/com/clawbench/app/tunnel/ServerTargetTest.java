package com.clawbench.app.tunnel;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;

/**
 * Tests for the server-URL parsing the transport depends on.
 *
 * <p>The scheme matters for the cookie lookup ({@code originalUrl} is passed to
 * the WebView verbatim) but deliberately NOT for transport selection: the
 * priority chain probes TLS first regardless, so an {@code http://} instance
 * still pays one fast TLS rejection and then remembers h2c.
 */
public class ServerTargetTest {

    @Test
    public void parse_readsHostAndPort() {
        ServerTarget target = ServerTarget.parse("http://127.0.0.1:20000");
        assertNotNull(target);
        assertEquals("127.0.0.1", target.host);
        assertEquals(20000, target.port);
    }

    @Test
    public void parse_keepsTheOriginalUrlForTheCookieLookup() {
        // The cookie jar is keyed by the URL the WebView loaded, so it must be
        // passed through unchanged rather than rebuilt.
        ServerTarget target = ServerTarget.parse("https://example.com:20300/app");
        assertNotNull(target);
        assertEquals("https://example.com:20300/app", target.originalUrl);
    }

    @Test
    public void parse_defaultsThePortFromTheScheme() {
        assertEquals(80, ServerTarget.parse("http://example.com").port);
        assertEquals(443, ServerTarget.parse("https://example.com").port);
    }

    @Test
    public void parse_handlesIPv6Literals() {
        ServerTarget target = ServerTarget.parse("http://[::1]:20000");
        assertNotNull(target);
        assertEquals("::1", target.host);
        assertEquals(20000, target.port);
    }

    @Test
    public void parse_rejectsUnusableInput() {
        assertNull(ServerTarget.parse(null));
        assertNull(ServerTarget.parse(""));
        assertNull(ServerTarget.parse("   "));
        assertNull(ServerTarget.parse("not a url"));
        assertNull(ServerTarget.parse("ftp://example.com"));
    }

    @Test
    public void base_usesTheTransportsScheme() {
        ServerTarget target = ServerTarget.parse("http://127.0.0.1:20000");
        assertNotNull(target);
        // The stored scheme is ignored on purpose: h2c is always probed over
        // http and TLS always over https, whatever the page loaded over.
        assertEquals("https", target.base(TransportKind.TLS).build().scheme());
        assertEquals("http", target.base(TransportKind.H2C).build().scheme());
        assertEquals(20000, target.base(TransportKind.H2C).build().port());
        assertEquals("127.0.0.1", target.base(TransportKind.H2C).build().host());
    }

    @Test
    public void transportKind_roundTripsItsWireName() {
        for (TransportKind kind : TransportKind.values()) {
            assertEquals(kind, TransportKind.fromWire(kind.wireName()));
        }
        // The names are the cross-platform contract (Electron's
        // H2TransportKind, and the value the JS bridge reports).
        assertEquals("tls", TransportKind.TLS.wireName());
        assertEquals("h2c", TransportKind.H2C.wireName());
        assertNull(TransportKind.fromWire(null));
        assertNull(TransportKind.fromWire("quic"));
    }
}
