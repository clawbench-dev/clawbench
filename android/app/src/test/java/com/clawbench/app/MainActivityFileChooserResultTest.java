package com.clawbench.app;

import android.app.Activity;
import android.content.ClipData;
import android.content.Intent;
import android.net.Uri;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import androidx.activity.result.ActivityResult;

import static org.junit.Assert.*;

/**
 * Unit tests for MainActivity.resolveFileChooserResult().
 *
 * Regression test for the camera-capture bug: after taking a photo via the
 * file chooser's camera option, ACTION_IMAGE_CAPTURE writes the picture to the
 * EXTRA_OUTPUT FileProvider URI and returns RESULT_OK with data==null. The old
 * code gated the whole block on "RESULT_OK && data != null", so the photo was
 * dropped and the WebView reported "0 files selected" — the image never
 * reached the upload flow.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class MainActivityFileChooserResultTest {

    private MainActivity activity;
    private Uri cameraUri;

    @Before
    public void setUp() throws Exception {
        activity = allocate(MainActivity.class);
        cameraUri = Uri.parse("content://com.clawbench.app.fileprovider/camera_images/IMG_20260824_120000.jpg");
        setField(activity, "cameraImageUri", cameraUri);
    }

    @After
    public void tearDown() throws Exception {
        try {
            Field instanceField = MainActivity.class.getDeclaredField("instance");
            instanceField.setAccessible(true);
            instanceField.set(null, null);
        } catch (Exception ignored) {}
    }

    // =====================================================
    // Camera capture path (the regression being fixed)
    // =====================================================

    @Test
    public void cameraCapture_okWithNullData_returnsCameraUri() throws Exception {
        // ACTION_IMAGE_CAPTURE with EXTRA_OUTPUT returns RESULT_OK + null data
        ActivityResult result = new ActivityResult(Activity.RESULT_OK, null);
        Uri[] uris = invokeResolve(activity, result);
        assertNotNull("captured photo must be delivered to the WebView", uris);
        assertEquals(1, uris.length);
        assertEquals("must fall back to the pre-created camera URI", cameraUri, uris[0]);
    }

    @Test
    public void cameraCapture_cancelled_returnsNull() throws Exception {
        // User backed out of the camera app — must NOT report the temp file
        ActivityResult result = new ActivityResult(Activity.RESULT_CANCELED, null);
        assertNull("cancelled capture must not report the camera temp file", invokeResolve(activity, result));
    }

    @Test
    public void cameraCapture_okWithNullData_noCameraUri_returnsNull() throws Exception {
        // No camera URI was set up (e.g. camera app unavailable) — nothing to return
        setField(activity, "cameraImageUri", null);
        ActivityResult result = new ActivityResult(Activity.RESULT_OK, null);
        assertNull(invokeResolve(activity, result));
    }

    // =====================================================
    // Normal picker paths (must keep working)
    // =====================================================

    @Test
    public void picker_singleFile_returnsDataStringUri() throws Exception {
        Intent data = new Intent();
        data.setData(Uri.parse("content://com.android.providers.media.documents/document/image%3A42"));
        ActivityResult result = new ActivityResult(Activity.RESULT_OK, data);
        Uri[] uris = invokeResolve(activity, result);
        assertNotNull(uris);
        assertEquals(1, uris.length);
        assertEquals("content://com.android.providers.media.documents/document/image%3A42", uris[0].toString());
    }

    @Test
    public void picker_multipleFiles_returnsClipDataUris() throws Exception {
        Intent data = new Intent();
        ClipData clip = new ClipData("images",
                new String[]{"image/jpeg"},
                new ClipData.Item(Uri.parse("content://uri/1")));
        clip.addItem(new ClipData.Item(Uri.parse("content://uri/2")));
        data.setClipData(clip);
        ActivityResult result = new ActivityResult(Activity.RESULT_OK, data);
        Uri[] uris = invokeResolve(activity, result);
        assertNotNull(uris);
        assertEquals(2, uris.length);
        assertEquals("content://uri/1", uris[0].toString());
        assertEquals("content://uri/2", uris[1].toString());
    }

    @Test
    public void picker_cancelled_returnsNull() throws Exception {
        ActivityResult result = new ActivityResult(Activity.RESULT_CANCELED, new Intent());
        assertNull(invokeResolve(activity, result));
    }

    // =====================================================
    // Picker with camera fallback interplay
    // =====================================================

    @Test
    public void picker_okWithData_ignoresCameraUri() throws Exception {
        // User picked a file from the picker instead of the camera —
        // the picked URI wins and the camera temp file is left for cleanup.
        Intent data = new Intent();
        data.setData(Uri.parse("content://picker/picked"));
        ActivityResult result = new ActivityResult(Activity.RESULT_OK, data);
        Uri[] uris = invokeResolve(activity, result);
        assertNotNull(uris);
        assertEquals(1, uris.length);
        assertEquals("content://picker/picked", uris[0].toString());
    }

    // --- Helper methods ---

    @SuppressWarnings("unchecked")
    private static <T> T allocate(Class<T> clazz) throws Exception {
        try {
            Constructor<T> ctor = clazz.getDeclaredConstructor();
            ctor.setAccessible(true);
            return ctor.newInstance();
        } catch (Exception e) {
            var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
            unsafeField.setAccessible(true);
            Object unsafe = unsafeField.get(null);
            Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
            allocate.setAccessible(true);
            return (T) allocate.invoke(unsafe, clazz);
        }
    }

    private static void setField(Object target, String fieldName, Object value) throws Exception {
        Field field = null;
        Class<?> clazz = target.getClass();
        while (clazz != null) {
            try {
                field = clazz.getDeclaredField(fieldName);
                break;
            } catch (NoSuchFieldException e) {
                clazz = clazz.getSuperclass();
            }
        }
        if (field == null) throw new NoSuchFieldException(fieldName);
        field.setAccessible(true);
        field.set(target, value);
    }

    private static Uri[] invokeResolve(Object target, ActivityResult result) throws Exception {
        Method method = target.getClass().getDeclaredMethod("resolveFileChooserResult", ActivityResult.class);
        method.setAccessible(true);
        return (Uri[]) method.invoke(target, result);
    }
}
