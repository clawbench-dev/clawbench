package com.clawbench.app;

import android.app.Application;
import android.app.DownloadManager;
import android.content.Context;
import android.content.Intent;
import android.database.MatrixCursor;
import android.net.Uri;
import android.provider.Settings;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.Shadows;
import org.robolectric.annotation.Config;
import org.robolectric.shadows.ShadowPackageManager;
import org.robolectric.shadows.ShadowToast;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.concurrent.atomic.AtomicBoolean;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;

/**
 * Unit tests for the APK download → install path in MainActivity.
 *
 * <p>Regression focus: the previous implementation located the downloaded APK
 * with the {@code File} API ({@code COLUMN_LOCAL_URI} then a direct lookup in the
 * public Downloads directory). Under scoped storage (targetSdk 34) neither path
 * is readable, so {@code exists()} was always false and the installer was never
 * launched — silently. The fix hands DownloadManager's own content URI to the
 * installer, which is the only supported way to reach the file.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class MainActivityApkInstallTest {

    private Application appContext;
    private MainActivity activity;

    @Before
    public void setUp() throws Exception {
        appContext = RuntimeEnvironment.getApplication();
        appContext.getSharedPreferences("clawbench_prefs", Context.MODE_PRIVATE)
                .edit().clear().commit();

        activity = Robolectric.buildActivity(MainActivity.class).get();
        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);
    }

    @After
    public void tearDown() throws Exception {
        try {
            Field instanceField = MainActivity.class.getDeclaredField("instance");
            instanceField.setAccessible(true);
            instanceField.set(null, null);
        } catch (Exception ignored) {
        }
    }

    // =====================================================
    // awaitApkDownload: terminal-state classification
    // =====================================================

    @Test
    public void awaitApkDownload_successful() {
        DownloadManager dm = mock(DownloadManager.class);
        doReturn(statusCursor(DownloadManager.STATUS_SUCCESSFUL)).when(dm).query(any());

        MainActivity.ApkDownloadOutcome outcome =
                MainActivity.awaitApkDownload(dm, 1L, 1000, 1);

        assertEquals(MainActivity.ApkDownloadStatus.SUCCESS, outcome.status);
    }

    @Test
    public void awaitApkDownload_failed_carriesReason() {
        DownloadManager dm = mock(DownloadManager.class);
        // 404 is DownloadManager.ERROR_HTTP_CODE_NOT_FOUND, the reason a
        // missing /api/apk produces.
        doReturn(failedCursor(404)).when(dm).query(any());

        MainActivity.ApkDownloadOutcome outcome =
                MainActivity.awaitApkDownload(dm, 1L, 1000, 1);

        assertEquals(MainActivity.ApkDownloadStatus.FAILED, outcome.status);
        assertEquals("the DownloadManager reason must be surfaced for logging", 404, outcome.reason);
    }

    @Test
    public void awaitApkDownload_noResult_whenQueryReturnsNull() {
        DownloadManager dm = mock(DownloadManager.class);
        doReturn(null).when(dm).query(any());

        MainActivity.ApkDownloadOutcome outcome =
                MainActivity.awaitApkDownload(dm, 1L, 1000, 1);

        assertEquals(MainActivity.ApkDownloadStatus.NO_RESULT, outcome.status);
    }

    @Test
    public void awaitApkDownload_noResult_whenCursorEmpty() {
        DownloadManager dm = mock(DownloadManager.class);
        MatrixCursor empty = new MatrixCursor(new String[]{DownloadManager.COLUMN_STATUS});
        doReturn(empty).when(dm).query(any());

        MainActivity.ApkDownloadOutcome outcome =
                MainActivity.awaitApkDownload(dm, 1L, 1000, 1);

        assertEquals(MainActivity.ApkDownloadStatus.NO_RESULT, outcome.status);
    }

    @Test
    public void awaitApkDownload_timeout_whenStillRunning() {
        DownloadManager dm = mock(DownloadManager.class);
        doReturn(statusCursor(DownloadManager.STATUS_RUNNING)).when(dm).query(any());

        // A zero deadline must time out immediately rather than poll for 10 minutes.
        MainActivity.ApkDownloadOutcome outcome =
                MainActivity.awaitApkDownload(dm, 1L, 0, 1);

        assertEquals(MainActivity.ApkDownloadStatus.TIMEOUT, outcome.status);
    }

    // =====================================================
    // resolveApkInstallUri: the scoped-storage fix
    // =====================================================

    @Test
    public void resolveApkInstallUri_returnsDownloadManagerUri() {
        DownloadManager dm = mock(DownloadManager.class);
        Uri contentUri = Uri.parse("content://downloads/my_downloads/42");
        doReturn(contentUri).when(dm).getUriForDownloadedFile(42L);

        Uri resolved = MainActivity.resolveApkInstallUri(dm, 42L);

        assertEquals("must use DownloadManager's own content URI, not a File path",
                contentUri, resolved);
    }

    @Test
    public void resolveApkInstallUri_nullWhenDownloadManagerFails() {
        DownloadManager dm = mock(DownloadManager.class);
        doThrow(new IllegalStateException("download not found"))
                .when(dm).getUriForDownloadedFile(7L);

        assertNull("a failing lookup must return null rather than throw",
                MainActivity.resolveApkInstallUri(dm, 7L));
    }

    // =====================================================
    // launchApkInstaller
    // =====================================================

    @Test
    public void launchApkInstaller_contentUri_intentGrantsReadPermission() {
        grantInstallPermission(true);
        Uri apkUri = Uri.parse("content://downloads/my_downloads/9");

        activity.launchApkInstaller(apkUri);

        Intent started = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull("the installer must be launched", started);
        assertEquals(Intent.ACTION_VIEW, started.getAction());
        assertEquals("the APK content URI must be handed to the installer",
                apkUri, started.getData());
        assertEquals("application/vnd.android.package-archive", started.getType());
        int flags = started.getFlags();
        assertTrue("the installer runs in another process — it needs a read grant",
                (flags & Intent.FLAG_GRANT_READ_URI_PERMISSION) != 0);
        assertTrue("must be launched from a non-activity context",
                (flags & Intent.FLAG_ACTIVITY_NEW_TASK) != 0);
    }

    @Test
    public void launchApkInstaller_withoutInstallPermission_opensSettingsOnly() {
        grantInstallPermission(false);
        Uri apkUri = Uri.parse("content://downloads/my_downloads/9");

        activity.launchApkInstaller(apkUri);

        Intent started = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull(started);
        assertEquals("must ask for the unknown-sources permission first",
                Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, started.getAction());
        assertEquals("package:" + appContext.getPackageName(), started.getDataString());
        assertFalse("the installer must not be launched before permission is granted",
                Intent.ACTION_VIEW.equals(started.getAction()));
    }

    // =====================================================
    // waitForApkInstall: failure paths must not be silent
    // =====================================================

    @Test
    public void waitForApkInstall_downloadFailed_toastsInsteadOfSilentlyReturning() throws Exception {
        DownloadManager dm = mock(DownloadManager.class);
        doReturn(failedCursor(404)).when(dm).query(any());

        runWaitForApkInstall(dm, 1L);

        // Previously this path only wrote a log line; the user saw nothing at all.
        String toast = ShadowToast.getTextOfLatestToast();
        assertNotNull("a failed download must tell the user", toast);
        assertEquals(appContext.getString(R.string.apk_download_failed), toast);
    }

    @Test
    public void waitForApkInstall_noDownloadRecord_toasts() throws Exception {
        DownloadManager dm = mock(DownloadManager.class);
        doReturn(null).when(dm).query(any());

        runWaitForApkInstall(dm, 1L);

        assertEquals(appContext.getString(R.string.apk_download_failed),
                ShadowToast.getTextOfLatestToast());
    }

    @Test
    public void waitForApkInstall_noInstallableUri_toasts() throws Exception {
        DownloadManager dm = mock(DownloadManager.class);
        doReturn(statusCursor(DownloadManager.STATUS_SUCCESSFUL)).when(dm).query(any());
        doReturn(null).when(dm).getUriForDownloadedFile(5L);

        runWaitForApkInstall(dm, 5L);

        assertEquals("a completed download with no readable URI must not be silent",
                appContext.getString(R.string.apk_install_failed),
                ShadowToast.getTextOfLatestToast());
    }

    @Test
    public void waitForApkInstall_success_launchesInstaller() throws Exception {
        grantInstallPermission(true);
        DownloadManager dm = mock(DownloadManager.class);
        doReturn(statusCursor(DownloadManager.STATUS_SUCCESSFUL)).when(dm).query(any());
        Uri apkUri = Uri.parse("content://downloads/my_downloads/11");
        doReturn(apkUri).when(dm).getUriForDownloadedFile(11L);

        runWaitForApkInstall(dm, 11L);

        Intent started = Shadows.shadowOf(appContext).getNextStartedActivity();
        assertNotNull("a successful download must open the installer", started);
        assertEquals(apkUri, started.getData());
        assertNull("no failure toast on the happy path", ShadowToast.getTextOfLatestToast());
    }

    /**
     * Run waitForApkInstall on its worker thread and wait for it to finish, then
     * drain the main looper so any posted toast/activity calls have executed.
     */
    private void runWaitForApkInstall(DownloadManager dm, long downloadId) throws Exception {
        Method m = findMethod(MainActivity.class, "waitForApkInstall",
                DownloadManager.class, long.class, String.class);
        m.setAccessible(true);

        // Join via the thread the method starts: find it by name after invoking.
        m.invoke(activity, dm, downloadId, "clawbench-android.apk");
        for (Thread t : Thread.getAllStackTraces().keySet()) {
            if ("APK-Install-Wait".equals(t.getName())) {
                t.join(10_000);
            }
        }
        Shadows.shadowOf(android.os.Looper.getMainLooper()).idle();
    }

    private MatrixCursor statusCursor(int status) {
        MatrixCursor cursor = new MatrixCursor(new String[]{DownloadManager.COLUMN_STATUS});
        cursor.addRow(new Object[]{status});
        return cursor;
    }

    private MatrixCursor failedCursor(int reason) {
        MatrixCursor cursor = new MatrixCursor(
                new String[]{DownloadManager.COLUMN_STATUS, DownloadManager.COLUMN_REASON});
        cursor.addRow(new Object[]{DownloadManager.STATUS_FAILED, reason});
        return cursor;
    }

    private void grantInstallPermission(boolean granted) {
        ShadowPackageManager spm = Shadows.shadowOf(appContext.getPackageManager());
        spm.setCanRequestPackageInstalls(granted);
    }

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

    private static Method findMethod(Class<?> clazz, String methodName, Class<?>... paramTypes)
            throws Exception {
        Class<?> c = clazz;
        while (c != null) {
            try {
                return c.getDeclaredMethod(methodName, paramTypes);
            } catch (NoSuchMethodException e) {
                c = c.getSuperclass();
            }
        }
        throw new NoSuchMethodException(methodName);
    }
}
