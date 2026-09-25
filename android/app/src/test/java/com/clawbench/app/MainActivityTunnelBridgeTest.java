package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import static org.junit.Assert.*;

/**
 * Unit tests for the new WebAppInterface bridge methods:
 * - testPortReachable(int port): TCP Socket connect to localhost
 * - reconnectTunnel(): calls BackgroundService.forceReconnect()
 *
 * Since these are @JavascriptInterface methods on an inner class,
 * we test them via reflection to avoid needing a full Android Activity.
 */
public class MainActivityTunnelBridgeTest {

    private Object webAppInterface;
    private MainActivity activity;

    @Before
    public void setUp() throws Exception {
        // Allocate a minimal MainActivity instance
        activity = allocate(MainActivity.class);

        // Set the static instance field
        Field instanceField = MainActivity.class.getDeclaredField("instance");
        instanceField.setAccessible(true);
        instanceField.set(null, activity);

        // Create a WebAppInterface instance via reflection
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

    // =====================================================
    // testPortReachable tests
    // =====================================================

    @Test
    public void testPortReachable_invalidPortZero_returnsFalse() throws Exception {
        boolean result = invokeTestPortReachable(0);
        assertFalse("Port 0 should be invalid", result);
    }

    @Test
    public void testPortReachable_negativePort_returnsFalse() throws Exception {
        boolean result = invokeTestPortReachable(-1);
        assertFalse("Negative port should be invalid", result);
    }

    @Test
    public void testPortReachable_portTooLarge_returnsFalse() throws Exception {
        boolean result = invokeTestPortReachable(65536);
        assertFalse("Port > 65535 should be invalid", result);
    }

    @Test
    public void testPortReachable_unusedPort_returnsFalse() throws Exception {
        // Port 1 is almost certainly not listening on localhost
        boolean result = invokeTestPortReachable(1);
        assertFalse("Port 1 should not be reachable on localhost", result);
    }

    // =====================================================
    // reconnectTunnel tests
    // =====================================================

    @Test
    public void reconnectTunnel_serviceNotRunning_returnsFalse() throws Exception {
        // Ensure BackgroundService is not running
        Field isRunningField = BackgroundService.class.getDeclaredField("isRunning");
        isRunningField.setAccessible(true);
        isRunningField.set(null, false);

        boolean result = invokeReconnectTunnel();
        assertFalse("Should return false when BackgroundService is not running", result);
    }

    // =====================================================
    // Method signature verification
    // =====================================================

    @Test
    public void testPortReachable_methodExists() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("testPortReachable", int.class);
        assertNotNull("testPortReachable method should exist", method);
    }

    @Test
    public void reconnectTunnel_methodExists() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("reconnectTunnel");
        assertNotNull("reconnectTunnel method should exist", method);
    }

    @Test
    public void testPortReachable_hasJavascriptInterfaceAnnotation() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("testPortReachable", int.class);
        assertNotNull("Should have @JavascriptInterface annotation",
                method.getAnnotation(android.webkit.JavascriptInterface.class));
    }

    @Test
    public void reconnectTunnel_hasJavascriptInterfaceAnnotation() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("reconnectTunnel");
        assertNotNull("Should have @JavascriptInterface annotation",
                method.getAnnotation(android.webkit.JavascriptInterface.class));
    }

    // =====================================================
    // setTunnelTransport / getTunnelTransport bridge
    // =====================================================

    @Test
    public void setTunnelTransport_methodExists() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("setTunnelTransport", String.class);
        assertNotNull("setTunnelTransport method should exist", method);
        assertNotNull("Should have @JavascriptInterface annotation",
                method.getAnnotation(android.webkit.JavascriptInterface.class));
    }

    @Test
    public void setTunnelTransport_appliesThePreference() throws Exception {
        // Reset to the default so a previous test class cannot leak in.
        BackgroundService.setTransportPreference("ssh");
        try {
            invoke("setTunnelTransport", "h2");
            assertEquals(com.clawbench.app.tunnel.PortForwardTransportKind.H2,
                    BackgroundService.getTransportPreference());
        } finally {
            BackgroundService.setTransportPreference("ssh");
        }
    }

    @Test
    public void setTunnelTransport_unknownValueKeepsTheDefault() throws Exception {
        BackgroundService.setTransportPreference("ssh");
        try {
            invoke("setTunnelTransport", "gopher");
            assertEquals("an unknown value must not switch transports",
                    com.clawbench.app.tunnel.PortForwardTransportKind.SSH,
                    BackgroundService.getTransportPreference());
        } finally {
            BackgroundService.setTransportPreference("ssh");
        }
    }

    @Test
    public void setTunnelTransport_acceptsNullWithoutThrowing() throws Exception {
        BackgroundService.setTransportPreference("ssh");
        try {
            invoke("setTunnelTransport", (Object) null);
            assertEquals(com.clawbench.app.tunnel.PortForwardTransportKind.SSH,
                    BackgroundService.getTransportPreference());
        } finally {
            BackgroundService.setTransportPreference("ssh");
        }
    }

    @Test
    public void getTunnelTransport_reportsThePreference() throws Exception {
        BackgroundService.setTransportPreference("both");
        try {
            assertEquals("both", invoke("getTunnelTransport"));
        } finally {
            BackgroundService.setTransportPreference("ssh");
        }
    }

    @Test
    public void getActiveTunnelTransport_methodExists() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("getActiveTunnelTransport");
        assertNotNull("getActiveTunnelTransport method should exist", method);
        assertNotNull("Should have @JavascriptInterface annotation",
                method.getAnnotation(android.webkit.JavascriptInterface.class));
    }

    // --- Helper methods ---

    private Object invoke(String method, Object... args) throws Exception {
        Method m = webAppInterface.getClass().getDeclaredMethod(
                method, method.equals("setTunnelTransport") ? new Class<?>[]{String.class}
                        : new Class<?>[0]);
        m.setAccessible(true);
        return m.invoke(webAppInterface, args);
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

    private boolean invokeTestPortReachable(int port) throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("testPortReachable", int.class);
        method.setAccessible(true);
        return (boolean) method.invoke(webAppInterface, port);
    }

    private boolean invokeReconnectTunnel() throws Exception {
        Method method = webAppInterface.getClass().getDeclaredMethod("reconnectTunnel");
        method.setAccessible(true);
        return (boolean) method.invoke(webAppInterface);
    }
}
