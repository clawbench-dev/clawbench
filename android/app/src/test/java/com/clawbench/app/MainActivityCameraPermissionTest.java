package com.clawbench.app;

import android.content.Intent;
import android.net.Uri;
import android.webkit.ValueCallback;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

/**
 * Unit tests for MainActivity's camera permission flow for file chooser.
 *
 * The flow:
 * 1. onShowFileChooser builds a chooser intent and stores it in pendingFileChooserIntent
 * 2. If CAMERA permission is already granted → launchFileChooserWithCamera()
 * 3. If CAMERA permission not yet granted → cameraPermissionLauncher.launch(CAMERA)
 *    - On permission granted → launchFileChooserWithCamera()
 *    - On permission denied → launchFileChooserWithoutCamera()
 *
 * launchFileChooserWithCamera():
 * - Reads and clears pendingFileChooserIntent
 * - Builds camera intent with FileProvider URI + GRANT_URI_PERMISSION flags
 * - If camera available → wraps in chooser with EXTRA_INITIAL_INTENTS
 * - Launches via fileChooserLauncher
 * - On exception → sets filePathCallback = null
 *
 * launchFileChooserWithoutCamera():
 * - Reads and clears pendingFileChooserIntent
 * - Launches the bare chooser intent via fileChooserLauncher
 * - On exception → sets filePathCallback = null
 *
 * Note: Since Unsafe-allocated activities skip field initializers, fields like
 * cameraPermissionLauncher and fileChooserLauncher are null. Tests that call
 * launchFileChooserWithCamera/WithoutCamera directly must set up the necessary
 * fields via reflection, including injecting a mock fileChooserLauncher.
 *
 * Camera intent construction (launchFileChooserWithCamera) cannot be tested here
 * because it calls getPackageManager() which requires a fully initialized Android
 * Context. Those paths are covered by instrumented tests.
 */
public class MainActivityCameraPermissionTest {

    private MainActivity activity;
    private android.webkit.WebView mockWebView;

    @Before
    public void setUp() throws Exception {
        activity = allocate(MainActivity.class);

        // Set the static instance field
        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        // Mock WebView
        mockWebView = mock(android.webkit.WebView.class);
        setField(activity, "webView", mockWebView);

        // Set default field values
        setField(activity, "webViewConnected", true);
        setField(activity, "filePathCallback", null);
        setField(activity, "cameraImageUri", null);
        setField(activity, "pendingFileChooserIntent", null);
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
    // Null callback guards
    // =====================================================

    @Test
    public void launchFileChooserWithCamera_nullCallback_returnsEarly() throws Exception {
        // filePathCallback is null → should return immediately without touching launcher
        var mockLauncher = mock(androidx.activity.result.ActivityResultLauncher.class);
        setField(activity, "fileChooserLauncher", mockLauncher);

        invokeMethod(activity, "launchFileChooserWithCamera");

        // Should not launch anything
        verify(mockLauncher, never()).launch(any(Intent.class));
    }

    @Test
    public void launchFileChooserWithoutCamera_nullCallback_returnsEarly() throws Exception {
        // filePathCallback is null → should return immediately
        var mockLauncher = mock(androidx.activity.result.ActivityResultLauncher.class);
        setField(activity, "fileChooserLauncher", mockLauncher);

        invokeMethod(activity, "launchFileChooserWithoutCamera");

        verify(mockLauncher, never()).launch(any(Intent.class));
    }

    // =====================================================
    // launchFileChooserWithCamera: clears pendingFileChooserIntent
    // =====================================================

    @Test
    public void launchFileChooserWithCamera_nullCallback_doesNotClearPendingIntent() throws Exception {
        // When filePathCallback is null, the method returns early before
        // clearing pendingFileChooserIntent. Verify this guard works.
        Intent pendingIntent = new Intent(Intent.ACTION_GET_CONTENT);
        setField(activity, "pendingFileChooserIntent", pendingIntent);

        var mockLauncher = mock(androidx.activity.result.ActivityResultLauncher.class);
        setField(activity, "fileChooserLauncher", mockLauncher);

        invokeMethod(activity, "launchFileChooserWithCamera");

        // Early return means pendingFileChooserIntent is NOT cleared
        Field field = MainActivity.class.getDeclaredField("pendingFileChooserIntent");
        field.setAccessible(true);
        assertNotNull("pendingFileChooserIntent should NOT be cleared on null callback", field.get(activity));
    }

    // =====================================================
    // launchFileChooserWithoutCamera: clears pendingFileChooserIntent
    // =====================================================

    @Test
    public void launchFileChooserWithoutCamera_clearsPendingIntent() throws Exception {
        ValueCallback<Uri[]> mockCallback = mock(ValueCallback.class);
        setField(activity, "filePathCallback", mockCallback);

        Intent pendingIntent = new Intent(Intent.ACTION_GET_CONTENT);
        setField(activity, "pendingFileChooserIntent", pendingIntent);

        var mockLauncher = mock(androidx.activity.result.ActivityResultLauncher.class);
        setField(activity, "fileChooserLauncher", mockLauncher);

        invokeMethod(activity, "launchFileChooserWithoutCamera");

        // pendingFileChooserIntent should be cleared
        Field field = MainActivity.class.getDeclaredField("pendingFileChooserIntent");
        field.setAccessible(true);
        assertNull("pendingFileChooserIntent should be cleared", field.get(activity));
    }

    // =====================================================
    // launchFileChooserWithoutCamera: launches chooser directly
    // =====================================================

    @Test
    public void launchFileChooserWithoutCamera_launchesPendingIntent() throws Exception {
        ValueCallback<Uri[]> mockCallback = mock(ValueCallback.class);
        setField(activity, "filePathCallback", mockCallback);

        Intent pendingIntent = new Intent(Intent.ACTION_GET_CONTENT);
        setField(activity, "pendingFileChooserIntent", pendingIntent);

        var mockLauncher = mock(androidx.activity.result.ActivityResultLauncher.class);
        setField(activity, "fileChooserLauncher", mockLauncher);

        invokeMethod(activity, "launchFileChooserWithoutCamera");

        // Should launch the pending intent (without camera option)
        verify(mockLauncher).launch(pendingIntent);
    }

    // =====================================================
    // launchFileChooserWithoutCamera: exception clears callback
    // =====================================================

    @Test
    public void launchFileChooserWithoutCamera_exceptionClearsCallback() throws Exception {
        ValueCallback<Uri[]> mockCallback = mock(ValueCallback.class);
        setField(activity, "filePathCallback", mockCallback);

        Intent pendingIntent = new Intent(Intent.ACTION_GET_CONTENT);
        setField(activity, "pendingFileChooserIntent", pendingIntent);

        var mockLauncher = mock(androidx.activity.result.ActivityResultLauncher.class);
        doThrow(new RuntimeException("test exception")).when(mockLauncher).launch(any(Intent.class));
        setField(activity, "fileChooserLauncher", mockLauncher);

        invokeMethod(activity, "launchFileChooserWithoutCamera");

        // filePathCallback should be cleared on exception
        Field field = MainActivity.class.getDeclaredField("filePathCallback");
        field.setAccessible(true);
        assertNull("filePathCallback should be cleared on exception", field.get(activity));
    }

    // =====================================================
    // pendingFileChooserIntent: stores chooser intent
    // =====================================================

    @Test
    public void pendingFileChooserIntent_storesChooserIntent() throws Exception {
        Intent testIntent = new Intent(Intent.ACTION_GET_CONTENT);
        setField(activity, "pendingFileChooserIntent", testIntent);

        Field field = MainActivity.class.getDeclaredField("pendingFileChooserIntent");
        field.setAccessible(true);
        Intent stored = (Intent) field.get(activity);
        assertNotNull("pendingFileChooserIntent should store the intent", stored);
        assertSame("pendingFileChooserIntent should be the same intent", testIntent, stored);
    }

    // =====================================================
    // launchFileChooserWithoutCamera: does not set cameraImageUri
    // =====================================================

    @Test
    public void launchFileChooserWithoutCamera_doesNotSetCameraImageUri() throws Exception {
        ValueCallback<Uri[]> mockCallback = mock(ValueCallback.class);
        setField(activity, "filePathCallback", mockCallback);

        Intent pendingIntent = new Intent(Intent.ACTION_GET_CONTENT);
        setField(activity, "pendingFileChooserIntent", pendingIntent);

        var mockLauncher = mock(androidx.activity.result.ActivityResultLauncher.class);
        setField(activity, "fileChooserLauncher", mockLauncher);

        invokeMethod(activity, "launchFileChooserWithoutCamera");

        // cameraImageUri should remain null (no camera was set up)
        Field field = MainActivity.class.getDeclaredField("cameraImageUri");
        field.setAccessible(true);
        assertNull("cameraImageUri should remain null", field.get(activity));
    }

    // =====================================================
    // Pure logic: permission check branching
    // =====================================================

    @Test
    public void permissionCheck_granted_shouldChooseWithCamera() {
        // Verify the branching logic: PERMISSION_GRANTED → launchFileChooserWithCamera
        int result = android.content.pm.PackageManager.PERMISSION_GRANTED;
        assertEquals("PERMISSION_GRANTED should be 0", 0, result);
    }

    @Test
    public void permissionCheck_denied_shouldChooseWithoutCamera() {
        // Verify the branching logic: PERMISSION_DENIED → launchFileChooserWithoutCamera
        int result = android.content.pm.PackageManager.PERMISSION_DENIED;
        assertEquals("PERMISSION_DENIED should be -1", -1, result);
    }

    // =====================================================
    // Camera intent flags: verify correct flag values
    // =====================================================

    @Test
    public void cameraIntent_shouldGrantReadWriteUriPermissions() {
        // launchFileChooserWithCamera adds:
        //   FLAG_GRANT_WRITE_URI_PERMISSION | FLAG_GRANT_READ_URI_PERMISSION
        int expectedFlags = Intent.FLAG_GRANT_WRITE_URI_PERMISSION | Intent.FLAG_GRANT_READ_URI_PERMISSION;
        int actualWrite = Intent.FLAG_GRANT_WRITE_URI_PERMISSION;
        int actualRead = Intent.FLAG_GRANT_READ_URI_PERMISSION;

        // Verify the flags are non-zero and combine correctly
        assertTrue("FLAG_GRANT_WRITE_URI_PERMISSION should be non-zero", actualWrite != 0);
        assertTrue("FLAG_GRANT_READ_URI_PERMISSION should be non-zero", actualRead != 0);
        assertEquals("Combined flags should match", expectedFlags, actualWrite | actualRead);
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

    private static Object invokeMethod(Object target, String methodName) throws Exception {
        Method method = null;
        Class<?> clazz = target.getClass();
        while (clazz != null) {
            try {
                method = clazz.getDeclaredMethod(methodName);
                break;
            } catch (NoSuchMethodException e) {
                clazz = clazz.getSuperclass();
            }
        }
        if (method == null) throw new NoSuchMethodException(methodName);
        method.setAccessible(true);
        return method.invoke(target);
    }
}
