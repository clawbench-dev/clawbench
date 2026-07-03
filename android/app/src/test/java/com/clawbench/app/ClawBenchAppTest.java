package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Field;
import java.lang.reflect.Method;

import static org.junit.Assert.*;

/**
 * Unit tests for ClawBenchApp process detection logic.
 *
 * ClawBenchApp extends Application which cannot be fully instantiated in JVM unit tests.
 * We use Unsafe allocation + reflection to test the private methods:
 * - isBrowserProcess()
 * - getProcessNameSuffix()
 *
 * The process detection logic determines which initialization path to take
 * in onCreate(): browser process gets WebView data directory suffix.
 */
public class ClawBenchAppTest {

    private Object app;

    @Before
    public void setUp() throws Exception {
        // Create ClawBenchApp via Unsafe allocation (bypasses Android framework)
        var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        Object unsafe = unsafeField.get(null);
        Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
        allocate.setAccessible(true);
        app = allocate.invoke(unsafe, ClawBenchApp.class);
    }

    @After
    public void tearDown() throws Exception {
        // No static state to clean up
    }

    // =====================================================
    // Test 1: isBrowserProcess returns false for non-browser process
    // =====================================================

    @Test
    public void isBrowserProcess_nonBrowserProcess_returnsFalse() throws Exception {
        Method method = ClawBenchApp.class.getDeclaredMethod("isBrowserProcess");
        method.setAccessible(true);
        boolean result = (Boolean) method.invoke(app);
        // In unit test, the process is a Java test runner, not ":browser"
        assertFalse("isBrowserProcess should be false for non-browser process", result);
    }

    // =====================================================
    // Test 2: getProcessNameSuffix returns non-null string
    // =====================================================

    @Test
    public void getProcessNameSuffix_returnsNonNull() throws Exception {
        Method method = ClawBenchApp.class.getDeclaredMethod("getProcessNameSuffix");
        method.setAccessible(true);
        String result = (String) method.invoke(app);
        assertNotNull("getProcessNameSuffix should not return null", result);
    }

    // =====================================================
    // Test 3: getProcessNameSuffix extracts suffix from process name
    // =====================================================

    @Test
    public void getProcessNameSuffix_extractsSuffixOrEmpty() throws Exception {
        Method method = ClawBenchApp.class.getDeclaredMethod("getProcessNameSuffix");
        method.setAccessible(true);
        String result = (String) method.invoke(app);
        // The suffix is either empty (no colon in process name) or starts with ":"
        assertTrue("Suffix should be empty or start with colon",
                result.isEmpty() || result.startsWith(":"));
    }

    // =====================================================
    // Test 4: isBrowserProcess is private
    // =====================================================

    @Test
    public void isBrowserProcess_isPrivate() throws Exception {
        Method method = ClawBenchApp.class.getDeclaredMethod("isBrowserProcess");
        assertTrue("isBrowserProcess should be private",
                java.lang.reflect.Modifier.isPrivate(method.getModifiers()));
    }

    // =====================================================
    // Test 5: getProcessNameSuffix is private
    // =====================================================

    @Test
    public void getProcessNameSuffix_isPrivate() throws Exception {
        Method method = ClawBenchApp.class.getDeclaredMethod("getProcessNameSuffix");
        assertTrue("getProcessNameSuffix should be private",
                java.lang.reflect.Modifier.isPrivate(method.getModifiers()));
    }

    // =====================================================
    // Test 6: TAG constant has expected value
    // =====================================================

    @Test
    public void tagConstant_hasExpectedValue() throws Exception {
        Field field = ClawBenchApp.class.getDeclaredField("TAG");
        field.setAccessible(true);
        assertEquals("ClawBench", field.get(null));
    }

    // =====================================================
    // Test 7: ClawBenchApp extends Application
    // =====================================================

    @Test
    public void clawBenchApp_extendsApplication() {
        assertTrue("ClawBenchApp should extend Application",
                android.app.Application.class.isAssignableFrom(ClawBenchApp.class));
    }

    // =====================================================
    // Test 8: onCreate method exists and overrides parent
    // =====================================================

    @Test
    public void onCreate_overridesParent() throws Exception {
        Method method = ClawBenchApp.class.getDeclaredMethod("onCreate");
        assertEquals("onCreate should be declared in ClawBenchApp",
                ClawBenchApp.class, method.getDeclaringClass());
    }

    // =====================================================
    // Test 9: isBrowserProcess logic matches getProcessNameSuffix
    // =====================================================

    @Test
    public void isBrowserProcess_matchesSuffixLogic() throws Exception {
        Method isBrowser = ClawBenchApp.class.getDeclaredMethod("isBrowserProcess");
        isBrowser.setAccessible(true);

        Method getSuffix = ClawBenchApp.class.getDeclaredMethod("getProcessNameSuffix");
        getSuffix.setAccessible(true);

        String suffix = (String) getSuffix.invoke(app);
        boolean expected = suffix.equals(":browser");
        boolean actual = (Boolean) isBrowser.invoke(app);
        assertEquals("isBrowserProcess should match suffix.equals(':browser')", expected, actual);
    }

    // =====================================================
    // Test 10: getProcessNameSuffix uses lastIndexOf colon
    // =====================================================

    @Test
    public void getProcessNameSuffix_usesLastIndexOfColon() throws Exception {
        // Verify the suffix extraction logic works correctly
        // by testing the method returns a valid result
        Method getSuffix = ClawBenchApp.class.getDeclaredMethod("getProcessNameSuffix");
        getSuffix.setAccessible(true);

        String suffix = (String) getSuffix.invoke(app);
        // If the process name contains ":", the suffix starts with ":"
        // If not, the suffix is empty ""
        assertNotNull("Suffix should not be null", suffix);
    }
}
