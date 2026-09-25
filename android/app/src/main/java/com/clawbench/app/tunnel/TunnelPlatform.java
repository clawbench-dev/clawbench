package com.clawbench.app.tunnel;

import javax.net.ssl.SSLContext;

/**
 * The Android-specific inputs {@link H2TunnelStream} needs, injected so the
 * transport can be unit-tested on a plain JVM without Robolectric.
 *
 * <p>Everything here is a static call into the app (WebView's CookieManager,
 * the trust-all context built by {@code BackgroundService.initTrustAllSSL()},
 * the logger). Faking them is what lets the tests drive the real request
 * construction and stream lifecycle.
 */
public interface TunnelPlatform {

    /**
     * The session cookie for {@code serverUrl}, or {@code null} when the user
     * is not logged in.
     *
     * <p>The server scopes the cookie name by port ({@code clawbench_session}
     * on 20000, {@code cb<port>_clawbench_session} elsewhere —
     * {@code model.ScopedCookieName}), so the implementation matches both,
     * exactly like {@code BackgroundService.connectNativeWs} does for the
     * native event WebSocket.
     */
    String sessionCookie(String serverUrl);

    /**
     * The trust-all SSL context, or {@code null} when it has not been
     * initialized. A self-hosted instance commonly uses a self-signed
     * certificate; the tunnel is authenticated by the session cookie, not by
     * the certificate, so refusing a self-signed cert would break the tunnel on
     * exactly those installs.
     */
    SSLContext trustAllSslContext();

    void log(String level, String message);

    void log(String level, String message, Throwable error);
}
