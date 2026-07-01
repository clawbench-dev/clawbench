package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.io.File;

import static org.junit.Assert.*;

/**
 * Unit tests for SharedCacheUtils.
 */
public class SharedCacheUtilsTest {

    private File tempCacheDir;

    @Before
    public void setUp() {
        tempCacheDir = new File(System.getProperty("java.io.tmpdir"),
                "clawbench-test-shared-" + System.currentTimeMillis());
        tempCacheDir.mkdirs();
    }

    @After
    public void tearDown() {
        File sharedDir = new File(tempCacheDir, SharedCacheUtils.SHARED_DIR_NAME);
        if (sharedDir.exists()) {
            for (File f : sharedDir.listFiles()) f.delete();
            sharedDir.delete();
        }
        tempCacheDir.delete();
    }

    @Test
    public void getSharedCacheDir_createsDirectory() {
        File result = SharedCacheUtils.getSharedCacheDir(tempCacheDir);
        assertTrue(result.exists());
        assertTrue(result.isDirectory());
        assertEquals(new File(tempCacheDir, "shared"), result);
    }

    @Test
    public void getSharedCacheDir_returnsExistingDirectory() {
        File first = SharedCacheUtils.getSharedCacheDir(tempCacheDir);
        File second = SharedCacheUtils.getSharedCacheDir(tempCacheDir);
        assertEquals(first, second);
    }

    @Test
    public void cleanupSharedCacheDir_deletesFiles() throws Exception {
        File sharedDir = SharedCacheUtils.getSharedCacheDir(tempCacheDir);
        new File(sharedDir, "a.png").createNewFile();
        new File(sharedDir, "b.jpg").createNewFile();
        assertEquals(2, SharedCacheUtils.cleanupSharedCacheDir(tempCacheDir));
        assertEquals(0, sharedDir.listFiles().length);
    }

    @Test
    public void cleanupSharedCacheDir_returnsZeroWhenEmpty() {
        SharedCacheUtils.getSharedCacheDir(tempCacheDir);
        assertEquals(0, SharedCacheUtils.cleanupSharedCacheDir(tempCacheDir));
    }

    @Test
    public void cleanupSharedCacheDir_handlesNonExistentDir() {
        File nonExistent = new File(System.getProperty("java.io.tmpdir"),
                "clawbench-nonexistent-" + System.currentTimeMillis());
        assertEquals(0, SharedCacheUtils.cleanupSharedCacheDir(nonExistent));
        nonExistent.delete();
    }
}
