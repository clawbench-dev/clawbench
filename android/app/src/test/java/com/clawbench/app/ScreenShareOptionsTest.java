package com.clawbench.app;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

/**
 * Options arrive from JavaScript, so every field is untrusted. These tests pin
 * the clamping (a 0-pixel frame or a 1000 fps request would break the encoder)
 * and the round-trip through JSON that the Intent to the service relies on.
 */
public class ScreenShareOptionsTest {

    @Test
    public void parseNullUsesDefaults() {
        ScreenShareOptions options = ScreenShareOptions.parse(null);
        assertEquals(ScreenShareOptions.DEFAULT_FPS, options.fps);
        assertEquals(ScreenShareOptions.DEFAULT_MAX_WIDTH, options.maxWidth);
        assertEquals(ScreenShareOptions.DEFAULT_QUALITY, options.quality);
        assertFalse(options.systemAudio);
    }

    @Test
    public void parseMalformedJsonUsesDefaults() {
        ScreenShareOptions options = ScreenShareOptions.parse("{not json");
        assertEquals(ScreenShareOptions.DEFAULT_FPS, options.fps);
        assertEquals(ScreenShareOptions.DEFAULT_MAX_WIDTH, options.maxWidth);
        assertEquals(ScreenShareOptions.DEFAULT_QUALITY, options.quality);
        assertFalse(options.systemAudio);
    }

    @Test
    public void parseClampsOutOfRangeValues() {
        ScreenShareOptions tooSmall = ScreenShareOptions.parse(
                "{\"fps\":0,\"maxWidth\":10,\"quality\":1}");
        assertEquals(ScreenShareOptions.MIN_FPS, tooSmall.fps);
        assertEquals(ScreenShareOptions.MIN_MAX_WIDTH, tooSmall.maxWidth);
        assertEquals(ScreenShareOptions.MIN_QUALITY, tooSmall.quality);

        ScreenShareOptions tooLarge = ScreenShareOptions.parse(
                "{\"fps\":1000,\"maxWidth\":99999,\"quality\":100}");
        assertEquals(ScreenShareOptions.MAX_FPS, tooLarge.fps);
        assertEquals(ScreenShareOptions.MAX_MAX_WIDTH, tooLarge.maxWidth);
        assertEquals(ScreenShareOptions.MAX_QUALITY, tooLarge.quality);
    }

    @Test
    public void parseKeepsValidValuesAndSystemAudio() {
        ScreenShareOptions options = ScreenShareOptions.parse(
                "{\"fps\":15,\"maxWidth\":960,\"quality\":70,\"systemAudio\":true}");
        assertEquals(15, options.fps);
        assertEquals(960, options.maxWidth);
        assertEquals(70, options.quality);
        assertTrue(options.systemAudio);
    }

    @Test
    public void toJsonRoundTrips() {
        ScreenShareOptions original = ScreenShareOptions.parse(
                "{\"fps\":12,\"maxWidth\":800,\"quality\":45,\"systemAudio\":true}");
        ScreenShareOptions parsed = ScreenShareOptions.parse(original.toJson());
        assertEquals(original.fps, parsed.fps);
        assertEquals(original.maxWidth, parsed.maxWidth);
        assertEquals(original.quality, parsed.quality);
        assertEquals(original.systemAudio, parsed.systemAudio);
    }

    @Test
    public void withoutSystemAudioKeepsEverythingElse() {
        ScreenShareOptions original = ScreenShareOptions.parse(
                "{\"fps\":12,\"maxWidth\":800,\"quality\":45,\"systemAudio\":true}");
        ScreenShareOptions degraded = original.withoutSystemAudio();
        assertFalse(degraded.systemAudio);
        assertEquals(original.fps, degraded.fps);
        assertEquals(original.maxWidth, degraded.maxWidth);
        assertEquals(original.quality, degraded.quality);
    }
}
