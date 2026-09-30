package com.clawbench.app.tunnel;

import android.webkit.CookieManager;

import com.clawbench.app.AppLog;
import com.clawbench.app.BackgroundService;

import javax.net.ssl.SSLContext;

/**
 * The production {@link TunnelPlatform}: reads the WebView cookie jar and the
 * trust-all SSL context, and logs through {@link AppLog}.
 *
 * <p>The cookie-matching logic mirrors {@code BackgroundService.connectNativeWs}
 * exactly, and must keep mirroring it: the server scopes the session cookie
 * name by port ({@code clawbench_session} on 20000, {@code
 * cb<port>_clawbench_session} elsewhere — {@code model.ScopedCookieName}), and
 * a tunnel that failed to match the scoped name would 401 on every port but the
 * default one.
 */
public final class AndroidTunnelPlatform implements TunnelPlatform {

    private static final String TAG = "H2Tunnel";

    @Override
    public String sessionCookie(String serverUrl) {
        try {
            String cookies = CookieManager.getInstance().getCookie(serverUrl);
            if (cookies == null) return null;
            for (String cookie : cookies.split(";")) {
                String trimmed = cookie.trim();
                // Match both "clawbench_session=..." and "cb<port>_clawbench_session=..."
                int eqIdx = trimmed.indexOf('=');
                if (eqIdx > 0) {
                    String name = trimmed.substring(0, eqIdx);
                    if (name.equals("clawbench_session")
                            || (name.startsWith("cb") && name.endsWith("_clawbench_session"))) {
                        return trimmed;
                    }
                }
            }
        } catch (RuntimeException e) {
            AppLog.w(TAG, "failed to read session cookie", e);
        }
        return null;
    }

    @Override
    public SSLContext trustAllSslContext() {
        // Initialized once from MainActivity.onCreate(); null before that.
        return BackgroundService.getTrustAllSSLContext();
    }

    @Override
    public void log(String level, String message) {
        switch (level) {
            case "e": AppLog.e(TAG, message); break;
            case "w": AppLog.w(TAG, message); break;
            case "i": AppLog.i(TAG, message); break;
            default: AppLog.d(TAG, message); break;
        }
    }

    @Override
    public void log(String level, String message, Throwable error) {
        switch (level) {
            case "e": AppLog.e(TAG, message, error); break;
            case "w": AppLog.w(TAG, message, error); break;
            case "i": AppLog.i(TAG, message, error); break;
            default: AppLog.d(TAG, message, error); break;
        }
    }
}
