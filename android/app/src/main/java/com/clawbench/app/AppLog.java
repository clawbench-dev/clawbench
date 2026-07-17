package com.clawbench.app;

import android.app.ActivityManager;
import android.content.Context;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.util.ArrayList;
import java.util.List;

import javax.net.ssl.HttpsURLConnection;
import javax.net.ssl.SSLContext;

/**
 * Drop-in replacement for android.util.Log that also buffers entries and
 * periodically POSTs them to the ClawBench server's /api/android-log endpoint.
 *
 * Usage: replace Log.d(TAG, msg) with AppLog.d(TAG, msg) etc.
 *
 * When log capture is off (default), AppLog simply delegates to android.util.Log
 * with zero overhead. When capture is enabled via {@link #startCapture(String)},
 * entries are buffered in memory and flushed every 3 seconds (or when the buffer
 * reaches 200 entries) via HTTP POST.
 *
 * Error callback: set via {@link #setOnErrorCallback(OnErrorCallback)} to receive
 * relay failure notifications (HTTP errors, connection failures). The callback runs
 * on a background thread. This enables callers to detect and react to log relay
 * issues without polling.
 */
public class AppLog {

    private static final String TAG = "ClawBench-AppLog";
    private static final int BUFFER_CAPACITY = 500;
    private static final int FLUSH_THRESHOLD = 200;
    private static final long FLUSH_INTERVAL_MS = 3000;

    /**
     * Callback interface for log relay errors.
     * Implementations should be lightweight as they run on a background thread.
     */
    public interface OnErrorCallback {
        /**
         * Called when a log relay attempt fails.
         *
         * @param message human-readable error description (e.g. "HTTP 503", "Connection refused")
         * @param cause    the underlying exception, or null if it was an HTTP-level error
         */
        void onError(String message, Exception cause);
    }

    // Log entry buffer
    private static final List<LogEntry> buffer = new ArrayList<>();
    private static volatile boolean capturing = false;
    private static String serverBaseUrl = null;
    private static Handler flushHandler;
    private static Runnable flushRunnable;
    private static volatile OnErrorCallback errorCallback = null;

    /** Last relay error message, or null if the last flush succeeded. */
    private static volatile String lastError = null;
    /** Timestamp of the last successful flush (epoch millis), or 0 if never succeeded. */
    private static volatile long lastFlushSuccessTs = 0;

    // SSL context that trusts all certs (for self-signed server certs)
    private static SSLContext trustAllSSL;

    static {
        try {
            trustAllSSL = SSLContext.getInstance("TLS");
            trustAllSSL.init(null, new javax.net.ssl.TrustManager[]{
                new javax.net.ssl.X509TrustManager() {
                    public java.security.cert.X509Certificate[] getAcceptedIssuers() { return new java.security.cert.X509Certificate[0]; }
                    public void checkClientTrusted(java.security.cert.X509Certificate[] c, String a) {}
                    public void checkServerTrusted(java.security.cert.X509Certificate[] c, String a) {}
                }
            }, new java.security.SecureRandom());
        } catch (Exception e) {
            // Should never happen
        }
    }

    // --- Public API ---

    public static void d(String tag, String msg) { log('D', tag, msg); }
    public static void d(String tag, String msg, Throwable t) {
        log('D', tag, msg + "\n" + Log.getStackTraceString(t));
    }
    public static void i(String tag, String msg) { log('I', tag, msg); }
    public static void i(String tag, String msg, Throwable t) {
        log('I', tag, msg + "\n" + Log.getStackTraceString(t));
    }
    public static void w(String tag, String msg) { log('W', tag, msg); }
    public static void w(String tag, String msg, Throwable t) {
        log('W', tag, msg + "\n" + Log.getStackTraceString(t));
    }
    public static void e(String tag, String msg) { log('E', tag, msg); }
    public static void e(String tag, String msg, Throwable t) {
        log('E', tag, msg + "\n" + Log.getStackTraceString(t));
    }

    /**
     * Start capturing logs. Entries will be buffered and periodically flushed
     * to the server's /api/android-log endpoint.
     *
     * @param baseUrl the server base URL (e.g. "https://localhost:20000")
     */
    public static synchronized void startCapture(String baseUrl) {
        if (capturing) return;
        serverBaseUrl = baseUrl;
        capturing = true;
        flushHandler = new Handler(Looper.getMainLooper());
        flushRunnable = new Runnable() {
            @Override
            public void run() {
                if (!capturing) return;
                flushToServer();
                flushHandler.postDelayed(this, FLUSH_INTERVAL_MS);
            }
        };
        flushHandler.postDelayed(flushRunnable, FLUSH_INTERVAL_MS);
        i(TAG, "Log capture started");
    }

    /** Stop capturing and flush remaining entries. */
    public static synchronized void stopCapture() {
        if (!capturing) return;
        capturing = false;
        if (flushHandler != null) {
            flushHandler.removeCallbacks(flushRunnable);
            flushHandler = null;
        }
        flushToServer();
        i(TAG, "Log capture stopped");
        // The "stopped" entry itself won't be sent, but that's fine.
    }

    /** Returns whether log capture is currently active. */
    public static boolean isCapturing() {
        return capturing;
    }

    /**
     * Set a callback to be notified when log relay to the server fails.
     * The callback runs on a background thread — do not perform heavy work.
     * Pass null to remove a previously set callback.
     */
    public static void setOnErrorCallback(OnErrorCallback callback) {
        errorCallback = callback;
    }

    /**
     * Returns the last relay error message, or null if the last flush succeeded.
     * Useful for health-checking the log relay without a callback.
     */
    public static String getLastError() {
        return lastError;
    }

    /**
     * Returns the timestamp (epoch millis) of the last successful flush,
     * or 0 if no flush has ever succeeded.
     */
    public static long getLastFlushSuccessTs() {
        return lastFlushSuccessTs;
    }

    /**
     * Log current memory status at INFO level. Useful for diagnosing OOM kills —
     * the last memory log before a crash gap reveals whether memory pressure was the cause.
     *
     * @param context Application context (for ActivityManager access)
     * @param tag     Log tag
     * @param label   Description of the checkpoint (e.g. "addPortForward", "screenOn", "WebViewLoaded")
     */
    public static void logMemory(Context context, String tag, String label) {
        if (context == null) return;
        try {
            ActivityManager am = (ActivityManager) context.getSystemService(Context.ACTIVITY_SERVICE);
            if (am == null) return;
            ActivityManager.MemoryInfo mi = new ActivityManager.MemoryInfo();
            am.getMemoryInfo(mi);
            Runtime rt = Runtime.getRuntime();
            long usedMb = (rt.totalMemory() - rt.freeMemory()) / 1048576;
            long totalMb = rt.totalMemory() / 1048576;
            long maxMb = rt.maxMemory() / 1048576;
            long availMb = mi.availMem / 1048576;
            boolean lowMem = mi.lowMemory;
            i(tag, "Memory[" + label + "]: used=" + usedMb + "M total=" + totalMb + "M max=" + maxMb + "M systemAvail=" + availMb + "M low=" + lowMem);
        } catch (Exception ignored) {}
    }

    // --- Internal ---

    private static void log(char level, String tag, String msg) {
        // Always write to logcat
        switch (level) {
            case 'D': Log.d(tag, msg); break;
            case 'I': Log.i(tag, msg); break;
            case 'W': Log.w(tag, msg); break;
            case 'E': Log.e(tag, msg); break;
        }
        // Buffer if capturing
        if (capturing) {
            synchronized (buffer) {
                if (buffer.size() >= BUFFER_CAPACITY) {
                    buffer.remove(0); // drop oldest
                }
                buffer.add(new LogEntry(level, tag, msg, System.currentTimeMillis()));
                if (buffer.size() >= FLUSH_THRESHOLD) {
                    flushToServer();
                }
            }
        }
    }

    /** Flush all buffered entries to the server via HTTP POST. */
    static void flushToServer() {
        List<LogEntry> toSend;
        synchronized (buffer) {
            if (buffer.isEmpty()) return;
            toSend = new ArrayList<>(buffer);
            buffer.clear();
        }

        if (serverBaseUrl == null) return;

        // Build JSON payload
        try {
            JSONArray entries = new JSONArray();
            for (LogEntry e : toSend) {
                JSONObject obj = new JSONObject();
                obj.put("level", String.valueOf(e.level));
                obj.put("tag", e.tag);
                obj.put("msg", e.msg);
                obj.put("ts", e.ts);
                obj.put("source", "android");
                entries.put(obj);
            }
            JSONObject payload = new JSONObject();
            payload.put("entries", entries);

            // POST in background thread
            new Thread(() -> {
                try {
                    postLogPayload(payload.toString());
                    lastError = null;
                    lastFlushSuccessTs = System.currentTimeMillis();
                } catch (Exception e) {
                    String msg = "Log relay failed: " + e.getMessage();
                    lastError = msg;
                    notifyError(msg, e);
                }
            }).start();
        } catch (Exception e) {
            String msg = "Log relay JSON build failed: " + e.getMessage();
            lastError = msg;
            notifyError(msg, e);
        }
    }

    private static void notifyError(String message, Exception cause) {
        OnErrorCallback cb = errorCallback;
        if (cb != null) {
            try {
                cb.onError(message, cause);
            } catch (Exception ignored) {
                // Callback must not throw
            }
        }
    }

    private static void postLogPayload(String json) throws Exception {
        String urlStr = serverBaseUrl + "/api/android-log";
        URL url = new URL(urlStr);
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        try {
            conn.setRequestMethod("POST");
            conn.setRequestProperty("Content-Type", "application/json");
            conn.setConnectTimeout(5000);
            conn.setReadTimeout(5000);
            conn.setDoOutput(true);

            // Trust self-signed certs for HTTPS connections
            if (conn instanceof HttpsURLConnection) {
                ((HttpsURLConnection) conn).setSSLSocketFactory(trustAllSSL.getSocketFactory());
                ((HttpsURLConnection) conn).setHostnameVerifier((hostname, session) -> true);
            }

            // Write request body
            byte[] data = json.getBytes("UTF-8");
            conn.setFixedLengthStreamingMode(data.length);
            OutputStream os = conn.getOutputStream();
            os.write(data);
            os.flush();
            os.close();

            int code = conn.getResponseCode();
            if (code != 200) {
                throw new Exception("HTTP " + code);
            }
        } finally {
            conn.disconnect();
        }
    }

    // --- Data class ---

    private static class LogEntry {
        final char level;
        final String tag;
        final String msg;
        final long ts; // epoch millis

        LogEntry(char level, String tag, String msg, long ts) {
            this.level = level;
            this.tag = tag;
            this.msg = msg;
            this.ts = ts;
        }
    }
}
