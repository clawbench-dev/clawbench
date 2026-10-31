package com.clawbench.app;

import org.json.JSONObject;

import java.util.ArrayDeque;
import java.util.Iterator;

/**
 * Shared state for one screen-share session.
 *
 * <p>The capture service produces frames and PCM chunks on its own threads, the
 * JavaScript bridge consumes them from the WebView's thread, and the UI reads
 * the status. All three live in the same process, so a small static holder is
 * enough — and it keeps the service and the bridge from having to know about
 * each other.
 *
 * <p>Two rules shape the buffers:
 * <ul>
 *   <li><b>Video keeps only the newest frame.</b> The WebView pulls a frame when
 *       it is ready for one; a backlog of stale frames would only add latency,
 *       so a new frame overwrites the previous one.</li>
 *   <li><b>Audio is a bounded queue.</b> It is a continuous stream and every
 *       chunk matters, but an unbounded queue would grow without limit if the
 *       WebView stops draining (page reload, WebView paused). Two seconds of
 *       backlog is the compromise: enough to ride out a hitch, bounded enough
 *       to stay honest about dropping.</li>
 * </ul>
 *
 * <p>Free of Android imports (no {@code Base64}, no {@code SystemClock}) so the
 * queueing rules are unit-testable on the JVM. Encoding to base64 is the
 * bridge's job.
 */
public final class ScreenShareStatus {

    public static final String IDLE = "idle";
    /** Consent dialog is up (or the microphone permission prompt before it). */
    public static final String REQUESTING = "requesting";
    public static final String RUNNING = "running";
    /** The user dismissed the system consent dialog. */
    public static final String DENIED = "denied";
    public static final String ERROR = "error";

    /** 100 ms of 48 kHz stereo PCM per chunk, so this is ~2 s of backlog. */
    static final int MAX_AUDIO_CHUNKS = 20;

    private static final Object LOCK = new Object();

    private static volatile String state = IDLE;
    private static volatile String error = "";
    private static volatile int width = 0;
    private static volatile int height = 0;
    private static volatile boolean systemAudio = false;

    /** Newest encoded JPEG, or null when the WebView already took it. */
    private static byte[] frame = null;
    private static final ArrayDeque<byte[]> audio = new ArrayDeque<>();

    /**
     * Invoked whenever a frame or an audio chunk becomes available.
     *
     * <p>Set by the bridge (it owns the WebView) and cleared when the session
     * ends. Kept as a plain Runnable so this class never references WebView.
     */
    private static volatile Runnable tickListener = null;

    private ScreenShareStatus() {
    }

    public static void setTickListener(Runnable listener) {
        tickListener = listener;
    }

    public static String getState() {
        return state;
    }

    public static boolean isRunning() {
        return RUNNING.equals(state);
    }

    /** Enter the consent flow: clear any leftovers from a previous session. */
    public static void beginRequesting() {
        synchronized (LOCK) {
            frame = null;
            audio.clear();
        }
        width = 0;
        height = 0;
        systemAudio = false;
        error = "";
        state = REQUESTING;
    }

    public static void setRunning(int w, int h, boolean withSystemAudio) {
        width = w;
        height = h;
        systemAudio = withSystemAudio;
        error = "";
        state = RUNNING;
    }

    public static void setDenied() {
        error = "";
        state = DENIED;
    }

    public static void setError(String message) {
        error = message == null ? "" : message;
        state = ERROR;
    }

    /** Back to idle, dropping every buffered frame and chunk. */
    public static void reset() {
        synchronized (LOCK) {
            frame = null;
            audio.clear();
        }
        width = 0;
        height = 0;
        systemAudio = false;
        error = "";
        state = IDLE;
    }

    /** JSON snapshot for the bridge; the frontend polls this while starting. */
    public static String snapshotJson() {
        JSONObject json = new JSONObject();
        try {
            json.put("state", state);
            json.put("error", error);
            json.put("width", width);
            json.put("height", height);
            json.put("systemAudio", systemAudio);
        } catch (Exception ignored) {
            // Every value is a String/int/boolean; put() cannot fail here.
        }
        return json.toString();
    }

    /** Store the newest frame, replacing any frame the WebView has not taken. */
    public static void putFrame(byte[] jpeg) {
        if (jpeg == null || jpeg.length == 0) return;
        synchronized (LOCK) {
            frame = jpeg;
        }
        fireTick();
    }

    /** Take the pending frame, or null when there is none. */
    public static byte[] takeFrame() {
        synchronized (LOCK) {
            byte[] out = frame;
            frame = null;
            return out;
        }
    }

    /** Append one PCM chunk, dropping the oldest when the backlog is full. */
    public static void putAudio(byte[] pcm) {
        if (pcm == null || pcm.length == 0) return;
        synchronized (LOCK) {
            audio.addLast(pcm);
            while (audio.size() > MAX_AUDIO_CHUNKS) {
                audio.removeFirst();
            }
        }
        fireTick();
    }

    /**
     * Drain everything queued, concatenated in arrival order.
     *
     * <p>Returns null when empty. Concatenating here (rather than handing the
     * WebView one chunk per call) keeps the number of bridge crossings equal to
     * the number of ticks instead of the number of chunks.
     */
    public static byte[] takeAudio() {
        synchronized (LOCK) {
            if (audio.isEmpty()) return null;
            int total = 0;
            for (Iterator<byte[]> it = audio.iterator(); it.hasNext(); ) {
                total += it.next().length;
            }
            byte[] out = new byte[total];
            int offset = 0;
            for (Iterator<byte[]> it = audio.iterator(); it.hasNext(); ) {
                byte[] chunk = it.next();
                System.arraycopy(chunk, 0, out, offset, chunk.length);
                offset += chunk.length;
            }
            audio.clear();
            return out;
        }
    }

    /** Visible for tests: how many chunks are waiting. */
    static int queuedAudioChunks() {
        synchronized (LOCK) {
            return audio.size();
        }
    }

    private static void fireTick() {
        Runnable listener = tickListener;
        if (listener == null) return;
        try {
            listener.run();
        } catch (Exception ignored) {
            // The WebView may be gone (activity destroyed mid-share). The
            // capture keeps running until the user stops it; a failed tick only
            // means this frame is not delivered.
        }
    }
}
