package com.clawbench.app;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

import org.json.JSONObject;
import org.junit.Before;
import org.junit.Test;

/**
 * The hand-off buffers between the capture service and the WebView bridge.
 *
 * The two rules under test are the ones a user would notice as "the picture
 * lags" or "the audio is choppy": video keeps only the newest frame, and audio
 * is a bounded queue that drops its oldest chunk rather than growing forever.
 */
public class ScreenShareStatusTest {

    @Before
    public void reset() {
        ScreenShareStatus.setTickListener(null);
        ScreenShareStatus.reset();
    }

    @Test
    public void beginRequestingClearsLeftovers() {
        ScreenShareStatus.setRunning(100, 200, true);
        ScreenShareStatus.putFrame(new byte[]{1, 2, 3});
        ScreenShareStatus.putAudio(new byte[]{9});

        ScreenShareStatus.beginRequesting();

        assertEquals(ScreenShareStatus.REQUESTING, ScreenShareStatus.getState());
        assertNull(ScreenShareStatus.takeFrame());
        assertNull(ScreenShareStatus.takeAudio());
        assertEquals(0, ScreenShareStatus.queuedAudioChunks());
    }

    @Test
    public void snapshotCarriesSizeAndAudioFlag() throws Exception {
        ScreenShareStatus.setRunning(1080, 2400, true);
        JSONObject json = new JSONObject(ScreenShareStatus.snapshotJson());
        assertEquals("running", json.getString("state"));
        assertEquals(1080, json.getInt("width"));
        assertEquals(2400, json.getInt("height"));
        assertTrue(json.getBoolean("systemAudio"));
        assertEquals("", json.getString("error"));
    }

    @Test
    public void frameIsTakeOnceAndNewestWins() {
        ScreenShareStatus.setRunning(10, 10, false);
        ScreenShareStatus.putFrame(new byte[]{1});
        ScreenShareStatus.putFrame(new byte[]{2});

        assertArrayEquals(new byte[]{2}, ScreenShareStatus.takeFrame());
        // Taking twice must not hand the same frame to the page again.
        assertNull(ScreenShareStatus.takeFrame());
    }

    @Test
    public void emptyFrameIsIgnored() {
        ScreenShareStatus.setRunning(10, 10, false);
        ScreenShareStatus.putFrame(new byte[0]);
        assertNull(ScreenShareStatus.takeFrame());
    }

    @Test
    public void audioIsConcatenatedInArrivalOrder() {
        ScreenShareStatus.setRunning(10, 10, false);
        ScreenShareStatus.putAudio(new byte[]{1, 2});
        ScreenShareStatus.putAudio(new byte[]{3});
        assertArrayEquals(new byte[]{1, 2, 3}, ScreenShareStatus.takeAudio());
        assertNull(ScreenShareStatus.takeAudio());
    }

    @Test
    public void audioQueueIsBoundedAndDropsOldest() {
        ScreenShareStatus.setRunning(10, 10, false);
        for (int i = 0; i < ScreenShareStatus.MAX_AUDIO_CHUNKS + 5; i++) {
            ScreenShareStatus.putAudio(new byte[]{(byte) i});
        }
        assertEquals(ScreenShareStatus.MAX_AUDIO_CHUNKS, ScreenShareStatus.queuedAudioChunks());

        byte[] drained = ScreenShareStatus.takeAudio();
        // Chunks 0..4 were dropped; the queue starts at chunk 5.
        assertEquals(ScreenShareStatus.MAX_AUDIO_CHUNKS, drained.length);
        assertEquals((byte) 5, drained[0]);
    }

    @Test
    public void resetReturnsToIdleAndDropsBuffers() {
        ScreenShareStatus.setRunning(10, 10, true);
        ScreenShareStatus.putFrame(new byte[]{1});
        ScreenShareStatus.putAudio(new byte[]{1});

        ScreenShareStatus.reset();

        assertEquals(ScreenShareStatus.IDLE, ScreenShareStatus.getState());
        assertFalse(ScreenShareStatus.isRunning());
        assertNull(ScreenShareStatus.takeFrame());
        assertNull(ScreenShareStatus.takeAudio());
    }

    @Test
    public void tickListenerFiresForFramesAndAudio() {
        int[] ticks = {0};
        ScreenShareStatus.setTickListener(() -> ticks[0]++);
        ScreenShareStatus.setRunning(10, 10, false);

        ScreenShareStatus.putFrame(new byte[]{1});
        ScreenShareStatus.putAudio(new byte[]{2});
        ScreenShareStatus.putFrame(new byte[0]); // empty: no tick
        ScreenShareStatus.putAudio(null);        // null: no tick

        assertEquals(2, ticks[0]);
    }

    @Test
    public void throwingTickListenerDoesNotBreakProducers() {
        ScreenShareStatus.setTickListener(() -> {
            throw new IllegalStateException("webview gone");
        });
        ScreenShareStatus.setRunning(10, 10, false);
        // Must not propagate: the capture thread has to survive a dead WebView.
        ScreenShareStatus.putFrame(new byte[]{1});
        assertArrayEquals(new byte[]{1}, ScreenShareStatus.takeFrame());
    }
}
