package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import static org.junit.Assert.*;

/**
 * Unit tests for the ClawBenchNative.readClipboardText bridge method.
 *
 * Uses Unsafe.allocateInstance() to create MainActivity without triggering the
 * Android framework constructor, then invokes the @JavascriptInterface method
 * via reflection (the android.jar stubs return null for getSystemService).
 */
public class MainActivityClipboardBridgeTest {

    private Object webAppInterface;
    private MainActivity activity;

    @Before
    public void setUp() throws Exception {
        activity = allocate(MainActivity.class);
        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        Class<?> waiClass = Class.forName("com.clawbench.app.MainActivity$WebAppInterface");
        Constructor<?> constructor = waiClass.getDeclaredConstructor(MainActivity.class);
        constructor.setAccessible(true);
        webAppInterface = constructor.newInstance(activity);
    }

    @After
    public void tearDown() throws Exception {
        try {
            Field instanceField = MainActivity.class.getDeclaredField("instance");
            instanceField.setAccessible(true);
            instanceField.set(null, null);
        } catch (Exception ignored) {}
    }

    @Test
    public void readClipboardText_methodExists() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("readClipboardText");
        assertNotNull("readClipboardText method should exist", method);
    }

    @Test
    public void readClipboardText_hasJavascriptInterfaceAnnotation() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("readClipboardText");
        assertNotNull("Should have @JavascriptInterface annotation",
                method.getAnnotation(android.webkit.JavascriptInterface.class));
    }

    @Test
    public void readClipboardText_returnsEmptyString_whenClipboardServiceUnavailable() throws Exception {
        // In the JVM unit-test environment, activity.getSystemService() returns null,
        // so the method must gracefully return an empty string (not crash).
        String result = invokeReadClipboardText();
        assertEquals("", result);
    }

    private String invokeReadClipboardText() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("readClipboardText");
        method.setAccessible(true);
        return (String) method.invoke(webAppInterface);
    }

    @SuppressWarnings("unchecked")
    private static <T> T allocate(Class<T> clazz) throws Exception {
        var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        Object unsafe = unsafeField.get(null);
        var allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
        allocate.setAccessible(true);
        return (T) allocate.invoke(unsafe, clazz);
    }
}
