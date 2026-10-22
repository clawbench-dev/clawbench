package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import java.io.File;
import java.lang.reflect.Field;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Regression test for camera temp-file cleanup.
 *
 * The cleanup used `new File(cameraImageUri.getPath()).delete()`. The URI is a
 * FileProvider `content://` URI, so `getPath()` yields a synthetic
 * "/external_files/..." string that no File can address — the delete returned
 * false every time and each cancelled capture left a full-size JPEG behind in
 * getExternalFilesDir(Pictures), forever, with nothing in the log.
 *
 * The fix keeps the File that was handed to FileProvider and deletes that. This
 * test drives the real filesystem: a temp file is created, the field is
 * populated the way launchFileChooserWithCamera does, and the cleanup must
 * actually remove it.
 *
 * Runs under Robolectric: instantiating MainActivity touches the lifecycle
 * registry, which needs a main Looper.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 34)
public class MainActivityCameraTempFileTest {

    private MainActivity activity;
    private File tempFile;

    @Before
    public void setUp() throws Exception {
        activity = allocate(MainActivity.class);
        tempFile = File.createTempFile("IMG_test_", ".jpg");
        assertTrue("fixture must exist before the test", tempFile.exists());
    }

    @After
    public void tearDown() throws Exception {
        if (tempFile != null && tempFile.exists()) {
            tempFile.delete();
        }
        try {
            Field instanceField = MainActivity.class.getDeclaredField("instance");
            instanceField.setAccessible(true);
            instanceField.set(null, null);
        } catch (Exception ignored) {}
    }

    @Test
    public void deleteCameraTempFile_removesTheFileBehindTheUri() throws Exception {
        setField(activity, "cameraImageFile", tempFile);

        activity.deleteCameraTempFile();

        assertFalse("the temp JPEG must be gone after a cancelled capture", tempFile.exists());
    }

    @Test
    public void deleteCameraTempFile_isSafeWhenNoCaptureWasStarted() {
        // No camera request was made — the field is null and the cleanup must
        // be a no-op, not an NPE on every picker result.
        activity.deleteCameraTempFile();
    }

    @Test
    public void deleteCameraTempFile_isIdempotent() throws Exception {
        setField(activity, "cameraImageFile", tempFile);

        activity.deleteCameraTempFile();
        // The second call has nothing to delete; it must not throw or recreate
        // anything.
        activity.deleteCameraTempFile();

        assertFalse(tempFile.exists());
    }

    @Test
    public void deleteCameraTempFile_clearsTheFieldSoALaterCaptureIsNotAffected() throws Exception {
        setField(activity, "cameraImageFile", tempFile);

        activity.deleteCameraTempFile();

        Field field = MainActivity.class.getDeclaredField("cameraImageFile");
        field.setAccessible(true);
        // A stale reference here would make the NEXT cancelled capture delete
        // the file that the current one just reported to the WebView.
        assertFalse("cameraImageFile must be cleared after cleanup", field.get(activity) != null);
    }

    // --- Helpers ---

    @SuppressWarnings("unchecked")
    private static <T> T allocate(Class<T> clazz) throws Exception {
        var ctor = clazz.getDeclaredConstructor();
        ctor.setAccessible(true);
        return ctor.newInstance();
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
}
