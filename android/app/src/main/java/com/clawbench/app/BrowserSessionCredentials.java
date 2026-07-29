package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

/**
 * Cross-process session credentials holder for BrowserActivity.
 *
 * Uses SharedPreferences to pass session credentials from the main process
 * to the :browser process. Credentials are cleared after being read to
 * minimize exposure.
 *
 * Security note: Session cookies are NOT passed via Intent extras (which can
 * be intercepted by other apps or leaked to logcat). Instead, MainActivity
 * writes credentials here before launching BrowserActivity, and BrowserActivity
 * reads them on creation.
 */
public class BrowserSessionCredentials {

    private static final String PREFS_NAME = "browser_session_creds";
    private static final String KEY_SESSION_ID = "session_id";
    private static final String KEY_SERVER_URL = "server_url";
    private static final String KEY_SESSION_COOKIE = "session_cookie";

    public static class Creds {
        public final String sessionId;
        public final String serverUrl;
        public final String sessionCookie;

        public Creds(String sessionId, String serverUrl, String sessionCookie) {
            this.sessionId = sessionId;
            this.serverUrl = serverUrl;
            this.sessionCookie = sessionCookie;
        }
    }

    /** Write credentials from the main process before launching BrowserActivity. */
    public static void set(Context context, String sessionId, String serverUrl, String sessionCookie) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit()
                .putString(KEY_SESSION_ID, sessionId != null ? sessionId : "")
                .putString(KEY_SERVER_URL, serverUrl != null ? serverUrl : "")
                .putString(KEY_SESSION_COOKIE, sessionCookie != null ? sessionCookie : "")
                .apply();
    }

    /** Read credentials from the :browser process. Returns null if missing. */
    public static Creds get(Context context) {
        SharedPreferences prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
        String sid = prefs.getString(KEY_SESSION_ID, "");
        String url = prefs.getString(KEY_SERVER_URL, "");
        String cookie = prefs.getString(KEY_SESSION_COOKIE, "");
        if (sid.isEmpty() && url.isEmpty()) return null;
        return new Creds(sid, url, cookie);
    }

    /** Read credentials and clear them (minimize exposure window). */
    public static Creds getAndClear(Context context) {
        Creds c = get(context);
        if (c != null) {
            context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                    .edit().clear().apply();
        }
        return c;
    }

    public static void clear(Context context) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit().clear().apply();
    }
}
