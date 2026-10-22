package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import static org.junit.Assert.*;

/**
 * Tests for the capture lifecycle entries.
 *
 * The relay is the only way the server learns what the native side did, so a
 * missing "capture stopped" line is not cosmetic: without it a capture outage
 * looks identical to "the app was simply quiet" when reading client.log after
 * the fact. The entry used to be written after `capturing` was cleared, which
 * dropped it (log() only buffers while capturing) — this pins the ordering.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class AppLogStopCaptureTest {

    @Before
    public void setUp() {
        AppLog.resetStateForTest();
        AppLog.recordPayloadForTest();
    }

    @After
    public void tearDown() {
        if (AppLog.isCapturing()) {
            AppLog.stopCapture();
        }
        // These tests deliberately point the relay at an unreachable URL, so the
        // background POST always fails and writes `lastError`. Wait for it to
        // land before resetting, or the write races the reset and leaks into the
        // next test class (AppLogThrowableTest asserts the "initial" state).
        AppLog.awaitFlushesAndResetForTest();
    }

    @Test
    public void stopCapture_relaysTheStoppedEntry() {
        AppLog.startCapture("http://127.0.0.1:1");
        assertTrue("capture must be armed by startCapture", AppLog.isCapturing());

        AppLog.stopCapture();

        String payload = AppLog.lastPayloadForTest();
        assertNotNull("stopCapture must flush a payload", payload);
        assertTrue(
                "the stopped entry must reach the relay, got: " + payload,
                payload.contains("Log capture stopped"));
    }

    @Test
    public void stopCapture_isIdempotent() {
        AppLog.startCapture("http://127.0.0.1:1");
        AppLog.stopCapture();
        assertFalse(AppLog.isCapturing());

        // A second call must be a no-op rather than an error.
        AppLog.stopCapture();
        assertFalse(AppLog.isCapturing());
    }

    @Test
    public void startCapture_whileAlreadyCapturing_keepsCapturing() {
        AppLog.startCapture("http://127.0.0.1:1");
        AppLog.startCapture("http://127.0.0.1:2");
        assertTrue(AppLog.isCapturing());
    }

    @Test
    public void entriesAreNotBufferedWhileStopped() {
        // Nothing was ever started, so a log line must not be relayed.
        AppLog.i("TestTag", "should not be buffered");
        AppLog.flushToServer();
        assertNull(
                "logging while capture is off must not produce a payload",
                AppLog.lastPayloadForTest());
    }
}
