package com.clawbench.app;

import android.content.Intent;
import android.webkit.WebChromeClient;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import static org.junit.Assert.*;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

/**
 * Unit tests for the capture= handling in MainActivity's file chooser.
 *
 * Regression: the 拍照 button (<input capture="environment">) and the 照片
 * button (<input accept="image/*">) both opened the SAME three-way sheet
 * (相机 / 照片和视频 / 更多), because onShowFileChooser ignored the capture
 * hint and unconditionally wrapped every request in Intent.createChooser with
 * the camera appended as an EXTRA_INITIAL_INTENT. A dedicated camera entry is
 * pointless if it cannot skip that sheet.
 *
 * The fix has two halves, both covered here:
 *   1. isCaptureRequested() reads FileChooserParams.isCaptureEnabled()
 *   2. buildLaunchIntent() launches the camera alone when capture was asked
 *
 * Runs under Robolectric: Intent.createChooser() is a stub returning null on
 * the plain android.jar, so the chooser assertions would be vacuously broken
 * without a real implementation.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class MainActivityFileChooserCaptureTest {

    // =====================================================
    // isCaptureRequested: reads the params flag
    // =====================================================

    @Test
    public void isCaptureRequested_trueWhenParamsReportCapture() {
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.isCaptureEnabled()).thenReturn(true);

        assertTrue(MainActivity.isCaptureRequested(params));
    }

    @Test
    public void isCaptureRequested_falseWhenParamsDoNotReportCapture() {
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.isCaptureEnabled()).thenReturn(false);

        assertFalse("a plain accept=\"image/*\" input must keep the chooser",
                MainActivity.isCaptureRequested(params));
    }

    @Test
    public void isCaptureRequested_falseForNullParams() {
        // Some WebView implementations hand us null; that must not crash the
        // picker — it just means "no capture".
        assertFalse(MainActivity.isCaptureRequested(null));
    }

    @Test
    public void isCaptureRequested_falseWhenOemAccessorThrows() {
        // OEM WebViews have been observed throwing from this accessor. The
        // picker must still open (as a normal file chooser), not die.
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.isCaptureEnabled()).thenThrow(new RuntimeException("oem webview"));

        assertFalse(MainActivity.isCaptureRequested(params));
    }

    // =====================================================
    // buildLaunchIntent: the actual launched intent
    // =====================================================

    @Test
    public void buildLaunchIntent_captureOnly_launchesCameraDirectly() {
        Intent picker = new Intent(Intent.ACTION_GET_CONTENT);
        Intent camera = new Intent("android.media.action.IMAGE_CAPTURE");

        Intent launched = MainActivity.buildLaunchIntent(picker, camera, true, "choose");

        // Must be the camera itself — NOT a chooser wrapping it. This is the
        // assertion that fails on the old code (which always chose).
        assertSame("capture must bypass the chooser entirely", camera, launched);
        assertNull("a direct camera launch carries no EXTRA_INITIAL_INTENTS",
                launched.getParcelableArrayExtra(Intent.EXTRA_INITIAL_INTENTS));
    }

    @Test
    public void buildLaunchIntent_notCapture_wrapsInChooserWithCamera() {
        Intent picker = new Intent(Intent.ACTION_GET_CONTENT);
        Intent camera = new Intent("android.media.action.IMAGE_CAPTURE");

        Intent launched = MainActivity.buildLaunchIntent(picker, camera, false, "choose");

        assertNotNull(launched);
        assertEquals(Intent.ACTION_CHOOSER, launched.getAction());
        Intent[] initial = (Intent[]) launched.getParcelableArrayExtra(Intent.EXTRA_INITIAL_INTENTS);
        assertNotNull("the camera stays reachable as an initial intent", initial);
        assertEquals(1, initial.length);
        assertSame(camera, initial[0]);
    }

    @Test
    public void buildLaunchIntent_noCamera_launchesPickerAsIs() {
        // CAMERA permission denied, or no camera app installed: degrade to the
        // plain file picker instead of launching an intent nobody can handle.
        // Checked with captureOnly=true as well, because that is the path where
        // launching the camera unconditionally would throw
        // ActivityNotFoundException and lose the callback.
        Intent picker = new Intent(Intent.ACTION_GET_CONTENT);

        assertSame(picker, MainActivity.buildLaunchIntent(picker, null, false, "choose"));
        assertSame(picker, MainActivity.buildLaunchIntent(picker, null, true, "choose"));
    }
}
