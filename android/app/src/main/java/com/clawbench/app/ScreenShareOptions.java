package com.clawbench.app;

import org.json.JSONObject;

/**
 * Parsed and clamped options for one screen-share session.
 *
 * <p>The values arrive from JavaScript as JSON, so nothing here may be trusted:
 * an out-of-range frame rate or a 0-pixel width would either melt the encoder or
 * produce an unusable picture. Clamping here (rather than in the capture loop)
 * keeps the bounds in one place and makes them unit-testable without any
 * Android media classes.
 *
 * <p>Deliberately free of Android imports so it can be exercised by a plain
 * JUnit test.
 */
public final class ScreenShareOptions {

    public static final int DEFAULT_FPS = 10;
    public static final int DEFAULT_MAX_WIDTH = 1280;
    public static final int DEFAULT_QUALITY = 55;

    static final int MIN_FPS = 1;
    static final int MAX_FPS = 30;
    static final int MIN_MAX_WIDTH = 320;
    static final int MAX_MAX_WIDTH = 1920;
    static final int MIN_QUALITY = 20;
    static final int MAX_QUALITY = 90;

    /** Frames per second pushed to the WebView. */
    public final int fps;
    /** Longest side of the encoded frame, in pixels. */
    public final int maxWidth;
    /** JPEG quality, 1-100. */
    public final int quality;
    /** Whether to also capture the device's playback audio. */
    public final boolean systemAudio;

    ScreenShareOptions(int fps, int maxWidth, int quality, boolean systemAudio) {
        this.fps = fps;
        this.maxWidth = maxWidth;
        this.quality = quality;
        this.systemAudio = systemAudio;
    }

    /**
     * Parse the bridge payload, falling back to defaults for anything missing.
     *
     * <p>Never throws: a malformed payload must not break the bridge call, and
     * the defaults are a working configuration.
     */
    public static ScreenShareOptions parse(String raw) {
        int fps = DEFAULT_FPS;
        int maxWidth = DEFAULT_MAX_WIDTH;
        int quality = DEFAULT_QUALITY;
        boolean systemAudio = false;
        if (raw != null && !raw.isEmpty()) {
            try {
                JSONObject json = new JSONObject(raw);
                fps = clamp(json.optInt("fps", DEFAULT_FPS), MIN_FPS, MAX_FPS);
                maxWidth = clamp(json.optInt("maxWidth", DEFAULT_MAX_WIDTH), MIN_MAX_WIDTH, MAX_MAX_WIDTH);
                quality = clamp(json.optInt("quality", DEFAULT_QUALITY), MIN_QUALITY, MAX_QUALITY);
                systemAudio = json.optBoolean("systemAudio", false);
            } catch (Exception ignored) {
                // Malformed JSON: keep the defaults. Logging here would spam the
                // client log for a condition the caller can already see (the
                // capture simply runs with default settings).
            }
        }
        return new ScreenShareOptions(fps, maxWidth, quality, systemAudio);
    }

    static int clamp(int value, int min, int max) {
        if (value < min) return min;
        return Math.min(value, max);
    }

    /** The same session with system audio turned off (used when the mic is denied). */
    public ScreenShareOptions withoutSystemAudio() {
        return new ScreenShareOptions(fps, maxWidth, quality, false);
    }

    /** Serialised form, used to hand the options to the service via an Intent. */
    public String toJson() {        JSONObject json = new JSONObject();
        try {
            json.put("fps", fps);
            json.put("maxWidth", maxWidth);
            json.put("quality", quality);
            json.put("systemAudio", systemAudio);
        } catch (Exception ignored) {
            // JSONObject.put only throws for non-finite doubles / unsupported
            // keys; every value here is an int or a boolean.
        }
        return json.toString();
    }
}
