package com.clawbench.app;

import android.content.ContentResolver;
import android.content.ContentValues;
import android.net.Uri;
import android.provider.MediaStore;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import java.io.IOException;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertThrows;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.ArgumentMatchers.isNull;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for the MediaStore destination used by streamed downloads.
 *
 * <p>Regression focus: the previous implementation opened
 * {@code Download/ClawBench/<name>} with {@code FileOutputStream}. Under scoped
 * storage (targetSdk 34) that call throws {@code EACCES} on every device, so
 * every file-manager download failed with "下载失败" while the same file
 * downloaded fine through DownloadManager. The MediaStore row below is the only
 * supported way to write into public Downloads, so these assertions pin the
 * columns it needs: without {@code RELATIVE_PATH} the file lands in the wrong
 * place, and without {@code IS_PENDING} a half-written file becomes visible.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 34)
public class DownloadSinkTest {

    private static final Uri ROW = Uri.parse("content://media/external/downloads/42");

    @Test
    public void pendingRow_targetsTheDownloadSubdirectory() {
        ContentValues values = MainActivity.pendingDownloadValues("a.apk", "application/vnd.android.package-archive");

        assertEquals("Download/ClawBench", values.getAsString(MediaStore.MediaColumns.RELATIVE_PATH));
    }

    @Test
    public void pendingRow_carriesNameAndMimeType() {
        ContentValues values = MainActivity.pendingDownloadValues("a.apk", "application/vnd.android.package-archive");

        assertEquals("a.apk", values.getAsString(MediaStore.MediaColumns.DISPLAY_NAME));
        assertEquals("application/vnd.android.package-archive",
                values.getAsString(MediaStore.MediaColumns.MIME_TYPE));
    }

    @Test
    public void pendingRow_isInvisibleUntilPublished() {
        // A pending row keeps a partially written file out of the Downloads app
        // and out of other apps' reach; clearing it is what "completes" the save.
        ContentValues values = MainActivity.pendingDownloadValues("a.apk", "application/octet-stream");

        assertEquals(Integer.valueOf(1), values.getAsInteger(MediaStore.MediaColumns.IS_PENDING));
    }

    @Test
    public void createDownloadFile_insertsIntoTheDownloadsCollection() throws IOException {
        ContentResolver cr = mock(ContentResolver.class);
        when(cr.insert(eq(MediaStore.Downloads.EXTERNAL_CONTENT_URI), any(ContentValues.class)))
                .thenReturn(ROW);

        Uri uri = MainActivity.createDownloadFile(cr, "a.apk", "application/octet-stream");

        assertEquals(ROW, uri);
    }

    @Test
    public void createDownloadFile_failsLoudWhenTheProviderRefuses() {
        ContentResolver cr = mock(ContentResolver.class);
        when(cr.insert(any(Uri.class), any(ContentValues.class))).thenReturn(null);

        // Returning null here would leave the caller writing into nothing; the
        // download must surface as a failure instead.
        assertThrows(IOException.class,
                () -> MainActivity.createDownloadFile(cr, "a.apk", "application/octet-stream"));
    }

    @Test
    public void publishDownloadFile_clearsThePendingFlag() {
        ContentResolver cr = mock(ContentResolver.class);

        MainActivity.publishDownloadFile(cr, ROW);

        verify(cr).update(eq(ROW), any(ContentValues.class), isNull(), isNull());
    }

    @Test
    public void deleteDownloadFile_dropsTheRow() {
        ContentResolver cr = mock(ContentResolver.class);

        MainActivity.deleteDownloadFile(cr, ROW);

        verify(cr).delete(eq(ROW), isNull(), isNull());
    }

    @Test
    public void mimeTypeFor_keepsTheApkTypeSoTheInstallerOpensIt() {
        // The installer is launched with this MIME type; a generic octet-stream
        // makes some OEM installers refuse the URI.
        assertEquals("application/vnd.android.package-archive",
                MainActivity.mimeTypeFor("clawbench-android.apk"));
    }

    @Test
    public void mimeTypeFor_fallsBackToOctetStream() {
        assertEquals("application/octet-stream", MainActivity.mimeTypeFor("no-extension"));
    }
}
