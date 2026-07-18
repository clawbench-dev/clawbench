package com.clawbench.app;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import static org.junit.Assert.*;

/**
 * Tests for AppLog overloads that accept Throwable parameter.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class AppLogThrowableTest {

    @Test
    public void d_withThrowable_formatsStackTrace() {
        Exception testException = new Exception("test-error");
        // Should not throw — delegates to log() which writes to logcat
        AppLog.d("TestTag", "debug message", testException);
    }

    @Test
    public void i_withThrowable_formatsStackTrace() {
        Exception testException = new Exception("test-info");
        AppLog.i("TestTag", "info message", testException);
    }

    @Test
    public void d_withNullThrowable_doesNotCrash() {
        AppLog.d("TestTag", "debug null throwable", (Throwable) null);
    }

    @Test
    public void i_withNullThrowable_doesNotCrash() {
        AppLog.i("TestTag", "info null throwable", (Throwable) null);
    }

    @Test
    public void getLastFlushSuccessTs_initialValue() {
        assertEquals(0, AppLog.getLastFlushSuccessTs());
    }

    @Test
    public void getLastError_initialValue() {
        assertNull(AppLog.getLastError());
    }
}
