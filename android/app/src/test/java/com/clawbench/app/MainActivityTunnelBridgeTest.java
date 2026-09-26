package com.clawbench.app;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.concurrent.ConcurrentHashMap;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.spy;

/**
 * Unit tests for the new WebAppInterface bridge methods:
 * - testPortReachable(int port): TCP Socket connect to localhost
 * - reconnectTunnel(): calls BackgroundService.forceReconnect()
 *
 * Since these are @JavascriptInterface methods on an inner class,
 * we test them via reflection to avoid needing a full Android Activity.
 *
 * <p>The activity is a Mockito spy whose {@code runOnUiThread} executes the
 * Runnable inline, so the bridge delegation tests can observe what the posted
 * lambda actually did without an Android main looper.
 */
public class MainActivityTunnelBridgeTest {

    private Object webAppInterface;
    private MainActivity activity;

    @Before
    public void setUp() throws Exception {
        // Allocate a minimal MainActivity instance and spy it so runOnUiThread
        // can be made to run inline.
        activity = spy(allocate(MainActivity.class));
        doAnswer(inv -> {
            Runnable r = inv.getArgument(0);
            if (r != null) r.run();
            return null;
        }).when(activity).runOnUiThread(any(Runnable.class));

        // Unsafe allocation skips field initializers; the bridge reads/writes
        // this map.
        Field forwarded = MainActivity.class.getDeclaredField("forwardedPorts");
        forwarded.setAccessible(true);
        forwarded.set(activity, new ConcurrentHashMap<Integer, String>());

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
        try {
            Field bsInstance = BackgroundService.class.getDeclaredField("instance");
            bsInstance.setAccessible(true);
            bsInstance.set(null, null);
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

    // =====================================================
    // isTunnelConnected bridge
    // =====================================================

    @Test
    public void isTunnelConnected_returnsFalseWhenServiceNotRunning() throws Exception {
        // MainActivity:2674 delegates straight to the static, which answers
        // false when the service is not running (BackgroundService:558). The
        // frontend uses this to decide whether to show the tunnel banner.
        setStatic("BackgroundService", "isRunning", false);
        setStatic("BackgroundService", "instance", null);

        assertFalse((Boolean) invoke("isTunnelConnected"));
    }

    // =====================================================
    // getForwardedPorts reverse entries
    // =====================================================

    @Test
    public void getForwardedPorts_includesReverseDirectionEntries() throws Exception {
        // MainActivity:2856-2912: reverse mappings share the returned list with
        // direction="reverse" and `port` = the SERVER-side bind port, which is
        // the key the frontend reconciliation matches on. Without this a
        // reverse-only deployment would look like it has no forwards.
        BackgroundService service = allocate(BackgroundService.class);
        setField(service, "forwardedPorts",
                new ConcurrentHashMap<Integer, BackgroundService.PortInfo>());
        java.util.Map<Integer, BackgroundService.PortInfo> reverse =
                new ConcurrentHashMap<>();
        reverse.put(9000, new BackgroundService.PortInfo(3000, "10.0.0.7", true));
        setField(service, "reversePorts", reverse);
        setStatic("BackgroundService", "instance", service);

        String json = (String) invoke("getForwardedPorts");
        org.json.JSONArray arr = new org.json.JSONArray(json);
        assertEquals(1, arr.length());
        org.json.JSONObject obj = arr.getJSONObject(0);
        assertEquals("the server-side bind port is the key", 9000, obj.getInt("port"));
        assertEquals("reverse", obj.getString("direction"));
        assertEquals("10.0.0.7", obj.getString("host"));
    }

    @Test
    public void getForwardedPorts_listsBothDirections() throws Exception {
        BackgroundService service = allocate(BackgroundService.class);
        java.util.Map<Integer, BackgroundService.PortInfo> forward =
                new ConcurrentHashMap<>();
        forward.put(3080, new BackgroundService.PortInfo(80, ""));
        setField(service, "forwardedPorts", forward);
        java.util.Map<Integer, BackgroundService.PortInfo> reverse =
                new ConcurrentHashMap<>();
        reverse.put(9000, new BackgroundService.PortInfo(3000, "", true));
        setField(service, "reversePorts", reverse);
        setStatic("BackgroundService", "instance", service);

        String json = (String) invoke("getForwardedPorts");
        org.json.JSONArray arr = new org.json.JSONArray(json);
        assertEquals(2, arr.length());
        boolean sawForward = false;
        boolean sawReverse = false;
        for (int i = 0; i < arr.length(); i++) {
            String direction = arr.getJSONObject(i).getString("direction");
            if ("forward".equals(direction)) sawForward = true;
            if ("reverse".equals(direction)) sawReverse = true;
        }
        assertTrue("the forward mapping must be listed", sawForward);
        assertTrue("the reverse mapping must be listed", sawReverse);
    }

    // =====================================================
    // Delegation to BackgroundService
    //
    // The Intent action/extras assertions live in
    // MainActivityTunnelBridgeDelegationTest: with returnDefaultValues=true an
    // Intent's accessors return defaults here, so they can only be read under
    // Robolectric.
    // =====================================================

    @Test
    public void removeForwardedPort_removesFromTheActivityCache() throws Exception {
        // MainActivity:2795-2800: drop the activity cache entry AND ask the
        // service to release the listener. Skipping either would leave a stale
        // forward behind.
        doReturn(null).when(activity).startService(any(android.content.Intent.class));
        activity.forwardedPorts.put(3080, "10.0.0.1");

        invoke("removeForwardedPort", 3080);

        assertFalse("the activity cache must drop the port",
                activity.forwardedPorts.containsKey(3080));
        org.mockito.Mockito.verify(activity).startService(any(android.content.Intent.class));
    }

    @Test
    public void removeReverseForwardedPort_delegatesToTheService() throws Exception {
        // MainActivity:2831-2833.
        doReturn(null).when(activity).startService(any(android.content.Intent.class));

        invoke("removeReverseForwardedPort", 9000);

        org.mockito.Mockito.verify(activity).startService(any(android.content.Intent.class));
    }

    // --- Helper methods ---

    private static void setStatic(String clazz, String name, Object value) throws Exception {
        Field field = Class.forName("com.clawbench.app." + clazz).getDeclaredField(name);
        field.setAccessible(true);
        field.set(null, value);
    }

    private static void setField(Object target, String name, Object value) throws Exception {
        Field field = target.getClass().getDeclaredField(name);
        field.setAccessible(true);
        field.set(target, value);
    }

    private Object invoke(String method, Object... args) throws Exception {
        Method m = webAppInterface.getClass().getDeclaredMethod(
                method, method.equals("setTunnelTransport") ? new Class<?>[]{String.class}
                        : method.equals("removeForwardedPort")
                        || method.equals("removeReverseForwardedPort")
                        ? new Class<?>[]{int.class}
                        : method.equals("addReverseForwardedPort")
                        ? new Class<?>[]{int.class, int.class, String.class}
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
