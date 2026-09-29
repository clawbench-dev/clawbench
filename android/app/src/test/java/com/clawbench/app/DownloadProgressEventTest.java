package com.clawbench.app;

import org.junit.Test;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for the download progress event the native side pushes into the
 * WebView.
 *
 * <p>The renderer's progress bar reads this payload by field name and event
 * name; a rename on either side would compile fine on both and silently freeze
 * the bar. These assertions pin the contract.
 */
public class DownloadProgressEventTest {

    @Test
    public void script_usesTheEventNameTheRendererListensFor() {
        String script = MainActivity.buildDownloadEventScript(1, 0, 0, false, false, false);
        assertTrue("the renderer subscribes to clawbench-download-progress",
                script.contains("'clawbench-download-progress'"));
    }

    @Test
    public void payload_carriesEveryFieldTheRendererReads() {
        String script = MainActivity.buildDownloadEventScript(7, 512, 2048, true, false, true);

        assertTrue(script.contains("\"id\":7"));
        assertTrue(script.contains("\"received\":512"));
        assertTrue(script.contains("\"total\":2048"));
        assertTrue(script.contains("\"done\":true"));
        assertTrue(script.contains("\"error\":false"));
        assertTrue(script.contains("\"cancelled\":true"));
    }

    @Test
    public void payload_marksUnknownTotalAsZero() {
        // 0 is the renderer's "unknown size" signal (no Content-Length), which
        // switches the bar to its indeterminate animation.
        String script = MainActivity.buildDownloadEventScript(2, 10, 0, false, false, false);
        assertTrue(script.contains("\"total\":0"));
    }

    @Test
    public void payload_doesNotClaimDoneForInFlightProgress() {
        String script = MainActivity.buildDownloadEventScript(3, 10, 100, false, false, false);
        assertTrue(script.contains("\"done\":false"));
        assertTrue(script.contains("\"error\":false"));
        assertTrue(script.contains("\"cancelled\":false"));
    }

    @Test
    public void cancelledAndErrorAreIndependentFlags() {
        // A user cancel is not a failure: the renderer must suppress the error
        // toast, so error stays false while cancelled is true.
        String cancelled = MainActivity.buildDownloadEventScript(4, 0, 100, true, false, true);
        assertTrue(cancelled.contains("\"cancelled\":true"));
        assertTrue(cancelled.contains("\"error\":false"));

        String failed = MainActivity.buildDownloadEventScript(4, 0, 100, true, true, false);
        assertTrue(failed.contains("\"error\":true"));
        assertFalse(failed.contains("\"cancelled\":true"));
    }
}
