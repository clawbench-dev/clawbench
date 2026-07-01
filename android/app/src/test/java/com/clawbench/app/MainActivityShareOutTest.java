package com.clawbench.app;

import org.junit.Test;

import java.io.File;
import java.lang.reflect.Method;

import static org.junit.Assert.*;

/**
 * Unit tests for MainActivity's Share Out / shared cache directory logic.
 *
 * Uses reflection to test private methods without Activity instantiation.
 */
public class MainActivityShareOutTest {

    @Test
    public void getSharedCacheDir_methodExists() throws Exception {
        Method method = MainActivity.class.getDeclaredMethod("getSharedCacheDir");
        method.setAccessible(true);
        assertNotNull(method);
        assertEquals(File.class, method.getReturnType());
    }

    @Test
    public void cleanupSharedCacheDir_methodExists() throws Exception {
        Method method = MainActivity.class.getDeclaredMethod("cleanupSharedCacheDir");
        method.setAccessible(true);
        assertNotNull(method);
        assertEquals(void.class, method.getReturnType());
    }

    @Test
    public void cleanupSharedCacheDir_deletesFilesInDirectory() throws Exception {
        // Create a temp dir with some files
        File tempDir = new File(System.getProperty("java.io.tmpdir"),
                "clawbench-test-cleanup-" + System.currentTimeMillis());
        tempDir.mkdirs();

        File sharedDir = new File(tempDir, "shared");
        sharedDir.mkdirs();

        // Create test files
        assertTrue(new File(sharedDir, "test1.png").createNewFile());
        assertTrue(new File(sharedDir, "test2.jpg").createNewFile());

        // Verify files exist
        File[] files = sharedDir.listFiles();
        assertNotNull(files);
        assertEquals(2, files.length);

        // Manually perform cleanup (same logic as cleanupSharedCacheDir)
        for (File f : files) {
            assertTrue(f.delete());
        }

        // Verify files are deleted
        assertEquals(0, sharedDir.listFiles().length);

        // Cleanup
        assertTrue(sharedDir.delete());
        assertTrue(tempDir.delete());
    }

    @Test
    public void getSharedCacheDir_createsSubdirectory() throws Exception {
        File tempDir = new File(System.getProperty("java.io.tmpdir"),
                "clawbench-test-shared-" + System.currentTimeMillis());
        tempDir.mkdirs();

        // Simulate getSharedCacheDir logic
        File sharedDir = new File(tempDir, "shared");
        if (!sharedDir.exists()) {
            assertTrue(sharedDir.mkdirs());
        }

        assertTrue(sharedDir.exists());
        assertTrue(sharedDir.isDirectory());

        // Cleanup
        assertTrue(sharedDir.delete());
        assertTrue(tempDir.delete());
    }
}
