package com.clawbench.app.tunnel;

import android.webkit.CookieManager;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.mockito.MockedStatic;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.mockStatic;
import static org.mockito.Mockito.when;

/**
 * Tests for {@link AndroidTunnelPlatform#sessionCookie(String)}.
 *
 * <p>This is the single point that decides whether every port-scoped
 * deployment authenticates. The server scopes the session cookie name by port
 * ({@code clawbench_session} on 20000, {@code cb<port>_clawbench_session}
 * elsewhere), and the class doc says it must keep mirroring
 * {@code BackgroundService.connectNativeWs} — a tunnel that failed to match the
 * scoped name would 401 on every port but the default one, with no obvious
 * cause. Until now the class had no test at all.
 *
 * <p>The cookie jar is stubbed with a mocked {@link CookieManager} rather than
 * Robolectric's {@code RoboCookieManager}, so each test hands the production
 * parser the exact raw string it wants to exercise: the shadow's own cookie
 * parser drops malformed segments (a leading {@code =}, a bare name), which
 * would stop the {@code eqIdx > 0} guard from ever running.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class AndroidTunnelPlatformTest {

    private AndroidTunnelPlatform platform;
    private CookieManager cookieManager;
    private MockedStatic<CookieManager> staticCookies;

    @Before
    public void setUp() {
        platform = new AndroidTunnelPlatform();
        cookieManager = mock(CookieManager.class);
        staticCookies = mockStatic(CookieManager.class);
        staticCookies.when(CookieManager::getInstance).thenReturn(cookieManager);
    }

    @After
    public void tearDown() {
        staticCookies.close();
    }

    @Test
    public void nullCookieJar_returnsNull() {
        // The unauthenticated path: CookieManager answers null before any login
        // has happened. Must be null (not an empty string), because the caller
        // omits the Cookie header entirely on null.
        when(cookieManager.getCookie("http://127.0.0.1:20000")).thenReturn(null);
        assertNull(platform.sessionCookie("http://127.0.0.1:20000"));
    }

    @Test
    public void unscopedName_isMatched() {
        // Default port 20000: the server emits the bare name.
        when(cookieManager.getCookie("http://127.0.0.1:20000"))
                .thenReturn("clawbench_session=s");
        assertEquals("clawbench_session=s",
                platform.sessionCookie("http://127.0.0.1:20000"));
    }

    @Test
    public void portScopedName_isMatched() {
        // Non-default port: the server prefixes cb<port>_. Missing this is the
        // silent-401 bug the class doc warns about.
        when(cookieManager.getCookie("http://127.0.0.1:20300"))
                .thenReturn("cb20300_clawbench_session=s");
        assertEquals("cb20300_clawbench_session=s",
                platform.sessionCookie("http://127.0.0.1:20300"));
    }

    @Test
    public void multiCookieJar_picksOnlyTheSessionToken() {
        // Cookie order is not guaranteed and the jar carries unrelated cookies;
        // only the session token may be returned (the whole trimmed pair, so the
        // server can read name=value).
        when(cookieManager.getCookie("http://127.0.0.1:20300"))
                .thenReturn("theme=dark; cb20300_clawbench_session=s; x=1");
        assertEquals("cb20300_clawbench_session=s",
                platform.sessionCookie("http://127.0.0.1:20300"));
    }

    @Test
    public void unrelatedCookieWithTheCbPrefix_isNotMatched() {
        // The predicate requires the full _clawbench_session suffix; a bare
        // "cbfoo" must not be mistaken for the session cookie.
        when(cookieManager.getCookie("http://127.0.0.1:20300")).thenReturn("cbfoo=1");
        assertNull(platform.sessionCookie("http://127.0.0.1:20300"));
    }

    @Test
    public void cookieWithTheSuffixButNoCbPrefix_isNotMatched() {
        // The port-scoped branch is (startsWith("cb") && endsWith(...)); a name
        // that only satisfies the suffix half must not match.
        when(cookieManager.getCookie("http://127.0.0.1:20300"))
                .thenReturn("xx_clawbench_session=1");
        assertNull(platform.sessionCookie("http://127.0.0.1:20300"));
    }

    @Test
    public void segmentWithoutAName_isIgnored() {
        // The eqIdx > 0 guard: a leading '=' (empty name) or a bare token with
        // no '=' at all must be skipped without throwing, and the real session
        // cookie after them must still be found.
        when(cookieManager.getCookie("http://127.0.0.1:20000"))
                .thenReturn("=orphan; naked; clawbench_session=real");
        assertEquals("clawbench_session=real",
                platform.sessionCookie("http://127.0.0.1:20000"));
    }

    @Test
    public void cookieManagerThrowing_isSwallowedAndReturnsNull() {
        // A broken WebView cookie jar must degrade to "not authenticated" (the
        // caller then sends no Cookie header) rather than crash the tunnel.
        // getCookie() raising a RuntimeException is the shape the production
        // catch (RuntimeException) exists for.
        when(cookieManager.getCookie("http://127.0.0.1:20000"))
                .thenThrow(new RuntimeException("cookie manager unavailable"));

        assertNull(platform.sessionCookie("http://127.0.0.1:20000"));
    }
}
