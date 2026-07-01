package com.clawbench.app;

import java.io.File;

/**
 * Utility for managing the shared cache directory used by Share Out.
 * Extracted from MainActivity for testability without Activity dependency.
 */
public final class SharedCacheUtils {

    private SharedCacheUtils() {} // utility class

    /** Subdirectory name under the app's cacheDir for shared temp files. */
    static final String SHARED_DIR_NAME = "shared";

    /** Get or create the shared temp files directory under the given cache dir. */
    public static File getSharedCacheDir(File cacheDir) {
        File dir = new File(cacheDir, SHARED_DIR_NAME);
        if (!dir.exists()) dir.mkdirs();
        return dir;
    }

    /**
     * Clean up all files in the shared cache directory.
     * @return number of files deleted
     */
    public static int cleanupSharedCacheDir(File cacheDir) {
        int deleted = 0;
        try {
            File dir = getSharedCacheDir(cacheDir);
            File[] files = dir.listFiles();
            if (files != null) {
                for (File f : files) {
                    if (f.delete()) deleted++;
                }
            }
        } catch (Exception e) {
            AppLog.w("SharedCacheUtils", "Failed to cleanup shared cache dir", e);
        }
        return deleted;
    }
}
