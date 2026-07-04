package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

import androidx.annotation.NonNull;
import androidx.work.Worker;
import androidx.work.WorkerParameters;

import org.json.JSONArray;
import org.json.JSONObject;

import java.net.URLEncoder;
import java.util.concurrent.TimeUnit;

import javax.net.ssl.SSLContext;
import javax.net.ssl.X509TrustManager;

import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;

/**
 * WorkManager worker that periodically fetches pending (missed) events from
 * the server when the foreground service WebSocket is not active.
 *
 * This is a fallback for Chinese ROMs that aggressively kill foreground
 * services during Doze. WorkManager survives process kills because it's
 * managed by the system and respects battery constraints.
 *
 * Schedule: PeriodicWorkRequest every 15 minutes (WorkManager minimum).
 * Constraints: requires battery not low (avoids excessive polling on low battery).
 *
 * The worker:
 * 1. Checks if native WS is active (if so, skips — WS is more efficient)
 * 2. Fetches /api/ai/events/pending with cursor
 * 3. Posts Android notifications for terminal events
 * 4. Updates cursor in SharedPreferences
 *
 * SSL trust: Uses trust-all SSL context for self-signed certs.
 * Security model: The user explicitly accepted the self-signed cert in the
 * Activity login flow. The stored server URL is user-configured and persisted
 * in SharedPreferences. The WorkManager worker runs in the same app sandbox.
 */
public class PendingEventsWorker extends Worker {

    private static final String TAG = "ClawBench";
    private static final String PREFS_NAME = "clawbench_prefs";
    private static final String KEY_SERVER_URL = "server_url";
    private static final String KEY_LAST_SEEN_EVENT_ID = "last_seen_event_id";

    /** Lazily-initialized shared OkHttpClient for connection pool reuse. */
    private static volatile OkHttpClient sharedClient;

    public PendingEventsWorker(@NonNull Context context, @NonNull WorkerParameters params) {
        super(context, params);
    }

    @NonNull
    @Override
    public Result doWork() {
        Context ctx = getApplicationContext();

        // Skip if foreground service WS is active — it's more efficient
        if (BackgroundService.isNativeWsActive()) {
            AppLog.d(TAG, "PendingEventsWorker: native WS active, skipping");
            return Result.success();
        }

        SharedPreferences prefs = ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
        String serverUrl = prefs.getString(KEY_SERVER_URL, "");
        if (serverUrl.isEmpty()) {
            AppLog.d(TAG, "PendingEventsWorker: no server URL configured, skipping");
            return Result.success();
        }

        // Need auth cookie to make authenticated request
        String sessionCookie = getSessionCookie(serverUrl);
        if (sessionCookie == null) {
            AppLog.d(TAG, "PendingEventsWorker: no session cookie available, skipping");
            return Result.success();
        }

        try {
            fetchAndNotify(ctx, serverUrl, sessionCookie, prefs);
            return Result.success();
        } catch (Exception e) {
            AppLog.w(TAG, "PendingEventsWorker: fetch failed: " + e.getMessage());
            return Result.retry();
        }
    }

    private void fetchAndNotify(Context ctx, String serverUrl, String sessionCookie,
                                SharedPreferences prefs) throws Exception {
        String lastSeenId = prefs.getString(KEY_LAST_SEEN_EVENT_ID, "");

        StringBuilder urlBuilder = new StringBuilder(serverUrl)
                .append("/api/ai/events/pending");
        if (!lastSeenId.isEmpty()) {
            urlBuilder.append("?after=").append(URLEncoder.encode(lastSeenId, "UTF-8"));
        }

        OkHttpClient client = getOrCreateClient(serverUrl);

        Request.Builder reqBuilder = new Request.Builder()
                .url(urlBuilder.toString())
                .get();
        if (sessionCookie != null) {
            reqBuilder.header("Cookie", sessionCookie);
        }

        try (Response response = client.newCall(reqBuilder.build()).execute()) {
            if (response.code() != 200) {
                AppLog.w(TAG, "PendingEventsWorker: HTTP " + response.code());
                return;
            }

            String body = response.body().string();
            JSONObject json = new JSONObject(body);
            JSONArray events = json.optJSONArray("events");
            if (events == null || events.length() == 0) {
                AppLog.d(TAG, "PendingEventsWorker: no missed events");
                return;
            }

            AppLog.i(TAG, "PendingEventsWorker: processing " + events.length() + " missed events");
            String latestId = lastSeenId;

            for (int i = 0; i < events.length(); i++) {
                JSONObject eventObj = events.getJSONObject(i);
                String payloadStr = eventObj.optString("payload", "");
                String eventId = eventObj.optString("event_id", "");

                JSONObject msg = new JSONObject(payloadStr);
                String eventType = msg.optString("event", "");
                JSONObject data = msg.optJSONObject("data");
                if (data == null) continue;

                // Post notification for terminal events
                String status = data.optString("status", "");
                boolean shouldNotify = false;
                if ("session_update".equals(eventType)
                        && ("completed".equals(status) || "cancelled".equals(status) || "permission_pending".equals(status))) {
                    shouldNotify = true;
                } else if ("task_update".equals(eventType)
                        && ("completed".equals(status) || "failed".equals(status) || "cancelled".equals(status))) {
                    shouldNotify = true;
                }
                if (shouldNotify) {
                    BackgroundService.postEventNotificationFromWorker(ctx, eventType, data);
                }

                if (!eventId.isEmpty()) {
                    latestId = eventId;
                }
            }

            // Update cursor
            if (!latestId.equals(lastSeenId)) {
                prefs.edit().putString(KEY_LAST_SEEN_EVENT_ID, latestId).apply();
            }
        }
    }

    /**
     * Get or create the shared OkHttpClient. Lazily initialized for connection
     * pool reuse across WorkManager invocations.
     */
    private static OkHttpClient getOrCreateClient(String serverUrl) {
        if (sharedClient != null) return sharedClient;
        synchronized (PendingEventsWorker.class) {
            if (sharedClient != null) return sharedClient;
            OkHttpClient.Builder builder = new OkHttpClient.Builder()
                    .connectTimeout(5, TimeUnit.SECONDS)
                    .readTimeout(10, TimeUnit.SECONDS);
            SSLContext ssl = BackgroundService.getTrustAllSSLContext();
            if (ssl != null && serverUrl.startsWith("https://")) {
                builder.sslSocketFactory(ssl.getSocketFactory(), new X509TrustManager() {
                    public java.security.cert.X509Certificate[] getAcceptedIssuers() { return new java.security.cert.X509Certificate[0]; }
                    public void checkClientTrusted(java.security.cert.X509Certificate[] c, String a) {}
                    public void checkServerTrusted(java.security.cert.X509Certificate[] c, String a) {}
                });
                builder.hostnameVerifier((hostname, session) -> true);
            }
            sharedClient = builder.build();
            return sharedClient;
        }
    }

    /**
     * Read session cookie from WebView CookieManager.
     * Returns null if no suitable cookie found.
     */
    private static String getSessionCookie(String serverUrl) {
        try {
            String cookies = android.webkit.CookieManager.getInstance().getCookie(serverUrl);
            if (cookies == null) return null;
            for (String cookie : cookies.split(";")) {
                String trimmed = cookie.trim();
                int eqIdx = trimmed.indexOf('=');
                if (eqIdx > 0) {
                    String name = trimmed.substring(0, eqIdx);
                    if (name.equals("clawbench_session") ||
                            (name.startsWith("cb") && name.endsWith("_clawbench_session"))) {
                        return trimmed;
                    }
                }
            }
        } catch (Exception e) {
            AppLog.w(TAG, "PendingEventsWorker: failed to read cookie", e);
        }
        return null;
    }
}
