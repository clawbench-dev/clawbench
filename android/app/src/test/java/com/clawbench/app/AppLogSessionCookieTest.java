package com.clawbench.app;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import static org.junit.Assert.*;

/**
 * Tests for the session-cookie extraction used by the /api/client-log relay.
 *
 * That endpoint is auth-protected, so the relay must send a valid session
 * cookie. The cookie name is port-scoped by the server, which is the part most
 * likely to break silently: matching only the bare name would send nothing on a
 * non-default port and the relay would 401 with no obvious cause.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class AppLogSessionCookieTest {

    @Test
    public void extractsBareSessionCookieOnDefaultPort() {
        String raw = "clawbench_session=abc123; other=xyz";
        assertEquals("clawbench_session=abc123", AppLog.extractSessionCookie(raw));
    }

    @Test
    public void extractsPortScopedSessionCookieOnCustomPort() {
        // Server prefixes the name with cb<port>_ when the port is non-default.
        String raw = "cb20300_clawbench_session=abc123; clawbench_project=%2Ftmp";
        assertEquals("cb20300_clawbench_session=abc123", AppLog.extractSessionCookie(raw));
    }

    @Test
    public void ignoresUnrelatedCookies() {
        String raw = "clawbench_project=%2Ftmp; chat_session_id=sess-1";
        assertNull(AppLog.extractSessionCookie(raw));
    }

    @Test
    public void returnsNullWhenNoCookies() {
        assertNull(AppLog.extractSessionCookie(null));
        assertNull(AppLog.extractSessionCookie(""));
    }

    @Test
    public void doesNotMatchSimilarNames() {
        // A prefix/suffix near-miss must not be mistaken for the session cookie.
        assertNull(AppLog.extractSessionCookie("clawbench_session_extra=abc"));
        assertNull(AppLog.extractSessionCookie("xxclawbench_session=abc"));
        assertNull(AppLog.extractSessionCookie("cb20300_clawbench_session_old=abc"));
    }

    @Test
    public void picksSessionCookieWhenProjectCookieComesFirst() {
        // Cookie order is not guaranteed; the project cookie must not shadow it.
        String raw = "clawbench_project=%2Fp; cb20999_clawbench_session=tok; clawbench-locale=zh";
        assertEquals("cb20999_clawbench_session=tok", AppLog.extractSessionCookie(raw));
    }

    @Test
    public void toleratesSurroundingWhitespace() {
        String raw = "  clawbench_session=abc123  ;  other=1";
        assertEquals("clawbench_session=abc123", AppLog.extractSessionCookie(raw));
    }

    @Test
    public void readWebViewCookiesDoesNotThrowWithoutWebView() {
        // Robolectric provides a CookieManager; the point is that a failure is
        // swallowed and surfaced as null rather than crashing the log relay.
        String result = AppLog.readWebViewCookies("http://localhost:20000");
        // Either null or a string — must not throw.
        assertTrue(result == null || result instanceof String);
    }
}
