package com.clawbench.app;

import android.content.Intent;
import android.provider.MediaStore;
import android.webkit.WebChromeClient;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import static org.junit.Assert.*;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

/**
 * Unit tests for the per-entry routing of MainActivity's file chooser.
 *
 * Regression: onShowFileChooser ignored what the <input> actually asked for and
 * wrapped every request in Intent.createChooser with the camera appended as an
 * EXTRA_INITIAL_INTENT. All three entries in the attach drawer — 拍照
 * (capture="environment"), 照片 (accept="image/*") and 上传文件 — therefore
 * opened the SAME "相机 / 照片和视频 / 更多" sheet, so the dedicated entries were
 * indistinguishable from each other.
 *
 * The fix routes each entry straight to its own app:
 *   1. isCaptureRequested() / isImageOnlyRequest() classify the request
 *   2. buildPickerIntent() returns the camera (null → caller builds it), the
 *      photo picker / gallery, or the WebView-built file intent
 *
 * Runs under Robolectric because MediaStore.getPickImagesMaxLimit() is a real
 * method call, not an inlined constant, and the plain android.jar stub would
 * throw on it.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 34)
public class MainActivityFileChooserCaptureTest {

    private static final int MODE_OPEN = WebChromeClient.FileChooserParams.MODE_OPEN;
    private static final int MODE_MULTI = WebChromeClient.FileChooserParams.MODE_OPEN_MULTIPLE;

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

        assertFalse("a plain accept=\"image/*\" input must not be treated as a capture request",
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
        // picker must still open, not die.
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.isCaptureEnabled()).thenThrow(new RuntimeException("oem webview"));

        assertFalse(MainActivity.isCaptureRequested(params));
    }

    // =====================================================
    // isImageOnlyRequest: distinguishes 照片 from 上传文件
    // =====================================================

    @Test
    public void isImageOnlyRequest_trueForImageMimeType() {
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.getAcceptTypes()).thenReturn(new String[]{ "image/*" });

        assertTrue(MainActivity.isImageOnlyRequest(params));
    }

    @Test
    public void isImageOnlyRequest_trueForConcreteImageTypes() {
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.getAcceptTypes()).thenReturn(new String[]{ "image/png", "image/jpeg" });

        assertTrue(MainActivity.isImageOnlyRequest(params));
    }

    @Test
    public void isImageOnlyRequest_trueForExtensionList() {
        // Some OEM WebViews hand back extensions instead of MIME types.
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.getAcceptTypes()).thenReturn(new String[]{ ".jpg", ".png", ".heic" });

        assertTrue(MainActivity.isImageOnlyRequest(params));
    }

    @Test
    public void isImageOnlyRequest_falseForMixedTypes() {
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.getAcceptTypes()).thenReturn(new String[]{ "image/*", "application/pdf" });

        assertFalse("a mixed accept list must keep the file manager",
                MainActivity.isImageOnlyRequest(params));
    }

    @Test
    public void isImageOnlyRequest_falseForEmptyAccept() {
        // No accept attribute means "any file" — the 上传文件 entry. Treating
        // this as images-only would hide every non-image from the user.
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.getAcceptTypes()).thenReturn(new String[0]);

        assertFalse(MainActivity.isImageOnlyRequest(params));
    }

    @Test
    public void isImageOnlyRequest_falseForNullAccept() {
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.getAcceptTypes()).thenReturn(null);

        assertFalse(MainActivity.isImageOnlyRequest(params));
    }

    @Test
    public void isImageOnlyRequest_falseWhenAccessorThrows() {
        WebChromeClient.FileChooserParams params = mock(WebChromeClient.FileChooserParams.class);
        when(params.getAcceptTypes()).thenThrow(new RuntimeException("oem webview"));

        assertFalse(MainActivity.isImageOnlyRequest(params));
    }

    // =====================================================
    // buildPickerIntent: the routing table
    // =====================================================

    @Test
    public void buildPickerIntent_capture_returnsNullSoCallerBuildsCamera() {
        Intent fileIntent = new Intent(Intent.ACTION_GET_CONTENT);

        assertNull("capture is handled by the caller (needs FileProvider + Activity)",
                MainActivity.buildPickerIntent(true, false, MODE_OPEN, true, fileIntent));
    }

    @Test
    public void buildPickerIntent_imagesOnly_opensPhotoPickerOnApi33() {
        Intent fileIntent = new Intent(Intent.ACTION_GET_CONTENT);

        Intent picked = MainActivity.buildPickerIntent(false, true, MODE_OPEN, true, fileIntent);

        assertNotNull(picked);
        assertEquals("the 照片 entry must reach the photo picker, not a chooser",
                MediaStore.ACTION_PICK_IMAGES, picked.getAction());
        assertNull("no chooser wrapper", picked.getParcelableArrayExtra(Intent.EXTRA_INITIAL_INTENTS));
    }

    @Test
    public void buildPickerIntent_imagesOnlyMultiple_carriesMaxLimit() {
        Intent fileIntent = new Intent(Intent.ACTION_GET_CONTENT);

        Intent picked = MainActivity.buildPickerIntent(false, true, MODE_MULTI, true, fileIntent);

        assertEquals(MediaStore.ACTION_PICK_IMAGES, picked.getAction());
        // Multi-select must be requested explicitly; without it the picker
        // returns a single image even though the input said `multiple`.
        assertTrue(picked.hasExtra(MediaStore.EXTRA_PICK_IMAGES_MAX));
    }

    @Test
    public void buildPickerIntent_imagesOnlySingle_doesNotSetMaxLimit() {
        Intent fileIntent = new Intent(Intent.ACTION_GET_CONTENT);

        Intent picked = MainActivity.buildPickerIntent(false, true, MODE_OPEN, true, fileIntent);

        assertFalse("a single-select request must not set EXTRA_PICK_IMAGES_MAX",
                picked.hasExtra(MediaStore.EXTRA_PICK_IMAGES_MAX));
    }

    @Test
    public void buildPickerIntent_imagesOnly_fallsBackToGalleryBeforeApi33() {
        // ACTION_PICK_IMAGES is API 33; older devices must use the OEM gallery
        // instead of launching an activity that does not exist.
        Intent fileIntent = new Intent(Intent.ACTION_GET_CONTENT);

        Intent picked = MainActivity.buildPickerIntent(false, true, MODE_OPEN, false, fileIntent);

        assertEquals(Intent.ACTION_PICK, picked.getAction());
        assertEquals("image/*", picked.getType());
    }

    @Test
    public void buildPickerIntent_files_opensDocumentUiDirectly() {
        // The 上传文件 entry. The WebView intent is ACTION_GET_CONTENT, which the
        // system routes through an app-chooser when several apps can return
        // content — that is why the button still showed the old sheet on some
        // devices. ACTION_OPEN_DOCUMENT is handled by the system document UI,
        // so it opens the file manager directly.
        Intent fileIntent = new Intent(Intent.ACTION_GET_CONTENT).setType("*/*");

        Intent picked = MainActivity.buildPickerIntent(false, false, MODE_OPEN, true, fileIntent);

        assertEquals(Intent.ACTION_OPEN_DOCUMENT, picked.getAction());
        assertTrue(picked.hasCategory(Intent.CATEGORY_OPENABLE));
        assertEquals("*/*", picked.getType());
        assertNull("no chooser wrapper — that was the bug",
                picked.getParcelableArrayExtra(Intent.EXTRA_INITIAL_INTENTS));
    }

    @Test
    public void buildPickerIntent_files_preservesAcceptAndMultiple() {
        // Rebuilding the intent must not silently drop what the <input> asked
        // for: a multiple file input still has to allow selecting several files,
        // and a narrowed accept list must survive as EXTRA_MIME_TYPES.
        Intent fileIntent = new Intent(Intent.ACTION_GET_CONTENT).setType("application/pdf");
        fileIntent.putExtra(Intent.EXTRA_MIME_TYPES, new String[]{ "application/pdf" });

        Intent picked = MainActivity.buildPickerIntent(false, false, MODE_MULTI, true, fileIntent);

        assertEquals("application/pdf", picked.getType());
        assertArrayEquals(new String[]{ "application/pdf" },
                picked.getStringArrayExtra(Intent.EXTRA_MIME_TYPES));
        assertTrue(picked.getBooleanExtra(Intent.EXTRA_ALLOW_MULTIPLE, false));
    }

    @Test
    public void buildPickerIntent_files_fallsBackToAnyType() {
        // WebView can hand back an intent with no type at all. An untyped
        // OPEN_DOCUMENT shows nothing, so it must degrade to */*.
        Intent fileIntent = new Intent(Intent.ACTION_GET_CONTENT);

        Intent picked = MainActivity.buildPickerIntent(false, false, MODE_OPEN, true, fileIntent);

        assertEquals("*/*", picked.getType());
    }

    @Test
    public void buildPickerIntent_allEntriesReachDifferentApps() {
        // The whole point: three entries, three distinct targets. This is the
        // assertion that fails if any two entries collapse back into one.
        Intent fileIntent = new Intent(Intent.ACTION_GET_CONTENT).setType("*/*");

        Intent camera = MainActivity.buildPickerIntent(true, false, MODE_OPEN, true, fileIntent);
        Intent photos = MainActivity.buildPickerIntent(false, true, MODE_OPEN, true, fileIntent);
        Intent files = MainActivity.buildPickerIntent(false, false, MODE_OPEN, true, fileIntent);

        assertNull("拍照 → caller-built camera intent", camera);
        assertEquals(MediaStore.ACTION_PICK_IMAGES, photos.getAction());
        assertEquals(Intent.ACTION_OPEN_DOCUMENT, files.getAction());
        assertNotEquals(photos.getAction(), files.getAction());
    }
}
