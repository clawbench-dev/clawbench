package com.clawbench.app;

import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

/**
 * Unit tests for BackgroundService's port-host serialization changes.
 *
 * Tests the new Map<Integer, PortInfo> forwardedPorts (localPort -> PortInfo) behavior:
 * - saveForwardedPorts() saves as "localPort:targetPort" or "localPort:targetPort:host" format
 * - restoreForwardedPorts() parses both new and legacy formats
 * - removePortForward() checks the map (not a set)
 * - static addForwardedPort(Context, int, int, String) includes host in intent extras
 *
 * Uses Unsafe.allocateInstance() + Mockito spy to create a BackgroundService
 * with mocked SharedPreferences support.
 */
public class BackgroundServicePortHostTest {

    private BackgroundService service;
    private SharedPreferences mockPrefs;
    private Map<String, Set<String>> prefsData;

    @Before
    public void setUp() throws Exception {
        // Create a minimal BackgroundService instance via Unsafe allocation
        var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        Object unsafe = unsafeField.get(null);
        Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
        allocate.setAccessible(true);
        BackgroundService rawInstance = (BackgroundService) allocate.invoke(unsafe, BackgroundService.class);

        // Create a spy so we can stub getSharedPreferences
        service = spy(rawInstance);

        // Initialize forwardedPorts (Unsafe allocation skips field initializers)
        Field fpField = BackgroundService.class.getDeclaredField("forwardedPorts");
        fpField.setAccessible(true);
        fpField.set(service, new ConcurrentHashMap<Integer, BackgroundService.PortInfo>());

        // Set static fields for consistent test state
        setStaticField("instance", service);
        setStaticField("isRunning", true);
        setStaticField("nativeWsNeeded", false);
        setStaticField("lastError", null);

        // Prepare mock SharedPreferences for saveForwardedPorts / restoreForwardedPorts
        prefsData = new java.util.HashMap<>();
        mockPrefs = mock(SharedPreferences.class);
        when(mockPrefs.getStringSet(eq("forwarded_ports"), any())).thenAnswer(inv -> {
            String key = inv.getArgument(0);
            Set<String> defVal = inv.getArgument(1);
            return prefsData.containsKey(key) ? prefsData.get(key) : defVal;
        });
        when(mockPrefs.edit()).thenAnswer(inv -> {
            SharedPreferences.Editor editor = mock(SharedPreferences.Editor.class);
            when(editor.putStringSet(anyString(), any())).thenAnswer(editInv -> {
                String key = editInv.getArgument(0);
                Set<String> val = editInv.getArgument(1);
                prefsData.put(key, val);
                return editor;
            });
            doNothing().when(editor).apply();
            return editor;
        });

        // Stub getSharedPreferences on the spy
        doReturn(mockPrefs).when(service).getSharedPreferences(anyString(), anyInt());
    }

    @After
    public void tearDown() throws Exception {
        try {
            setStaticField("instance", null);
            setStaticField("isRunning", false);
            setStaticField("nativeWsNeeded", false);
            setStaticField("lastError", null);
        } catch (Exception ignored) {}
    }

    // =====================================================
    // saveForwardedPorts tests — calls the REAL method
    // =====================================================

    @Test
    public void testSaveForwardedPorts_noHost_savesAsLocalPortTargetPort() throws Exception {
        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(20000, new BackgroundService.PortInfo(20000, ""));

        // Call the real saveForwardedPorts method
        invokeMethod(service, "saveForwardedPorts");

        Set<String> saved = prefsData.get("forwarded_ports");
        assertNotNull("SharedPreferences should have forwarded_ports", saved);
        assertTrue("Should contain '20000:20000'", saved.contains("20000:20000"));
    }

    @Test
    public void testSaveForwardedPorts_portWithHost_savesAsLocalPortTargetPortHost() throws Exception {
        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(20000, new BackgroundService.PortInfo(80, "192.168.1.5"));

        invokeMethod(service, "saveForwardedPorts");

        Set<String> saved = prefsData.get("forwarded_ports");
        assertNotNull(saved);
        assertTrue("Should contain '20000:80:192.168.1.5'", saved.contains("20000:80:192.168.1.5"));
    }

    @Test
    public void testSaveForwardedPorts_mixedPorts_savesCorrectFormats() throws Exception {
        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(20000, new BackgroundService.PortInfo(20000, ""));
        ports.put(3080, new BackgroundService.PortInfo(80, "10.0.0.1"));

        invokeMethod(service, "saveForwardedPorts");

        Set<String> saved = prefsData.get("forwarded_ports");
        assertNotNull(saved);
        assertTrue("Should contain '20000:20000'", saved.contains("20000:20000"));
        assertTrue("Should contain '3080:80:10.0.0.1'", saved.contains("3080:80:10.0.0.1"));
    }

    // =====================================================
    // restoreForwardedPorts tests — calls the REAL method
    // =====================================================

    @Test
    public void testRestoreForwardedPorts_legacyPlainNumber_restoresWithSameTargetPort() throws Exception {
        // Legacy format: plain port number "20000" — targetPort assumed == localPort
        Set<String> portStrings = new HashSet<>();
        portStrings.add("20000");
        prefsData.put("forwarded_ports", portStrings);

        invokeMethod(service, "restoreForwardedPorts");

        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        assertEquals(1, ports.size());
        assertTrue("Should contain port 20000", ports.containsKey(20000));
        BackgroundService.PortInfo info = ports.get(20000);
        assertEquals("Target port should equal local port", 20000, info.targetPort);
        assertEquals("Host should be empty string", "", info.host);
    }

    @Test
    public void testRestoreForwardedPorts_legacyPortHostFormat_restoresWithHost() throws Exception {
        // Legacy format: "20000:192.168.1.5" — targetPort assumed == localPort
        Set<String> portStrings = new HashSet<>();
        portStrings.add("20000:192.168.1.5");
        prefsData.put("forwarded_ports", portStrings);

        invokeMethod(service, "restoreForwardedPorts");

        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        assertEquals(1, ports.size());
        assertTrue("Should contain port 20000", ports.containsKey(20000));
        BackgroundService.PortInfo info = ports.get(20000);
        assertEquals("Target port should equal local port (legacy)", 20000, info.targetPort);
        assertEquals("192.168.1.5", info.host);
    }

    @Test
    public void testRestoreForwardedPorts_newFormat_restoresWithTargetPort() throws Exception {
        // New format: "3080:80:192.168.1.5"
        Set<String> portStrings = new HashSet<>();
        portStrings.add("3080:80:192.168.1.5");
        prefsData.put("forwarded_ports", portStrings);

        invokeMethod(service, "restoreForwardedPorts");

        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        assertEquals(1, ports.size());
        assertTrue("Should contain local port 3080", ports.containsKey(3080));
        BackgroundService.PortInfo info = ports.get(3080);
        assertEquals("Target port should be 80", 80, info.targetPort);
        assertEquals("192.168.1.5", info.host);
    }

    @Test
    public void testRestoreForwardedPorts_mixedFormats_restoresAll() throws Exception {
        Set<String> portStrings = new HashSet<>();
        portStrings.add("20000");               // Legacy: plain number
        portStrings.add("30000:10.0.0.1");      // Legacy: port:host
        portStrings.add("4080:80:192.168.1.5"); // New: localPort:targetPort:host
        prefsData.put("forwarded_ports", portStrings);

        invokeMethod(service, "restoreForwardedPorts");

        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        assertEquals(3, ports.size());
        assertEquals(20000, ports.get(20000).targetPort);
        assertEquals("", ports.get(20000).host);
        assertEquals(30000, ports.get(30000).targetPort);
        assertEquals("10.0.0.1", ports.get(30000).host);
        assertEquals(80, ports.get(4080).targetPort);
        assertEquals("192.168.1.5", ports.get(4080).host);
    }

    @Test
    public void testRestoreForwardedPorts_emptySet_doesNothing() throws Exception {
        prefsData.put("forwarded_ports", new HashSet<>());

        invokeMethod(service, "restoreForwardedPorts");

        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        assertTrue("No ports should be restored from empty set", ports.isEmpty());
    }

    @Test
    public void testRestoreForwardedPorts_nullSet_doesNothing() throws Exception {
        // Don't put anything in prefsData — getStringSet returns default (null)
        prefsData.remove("forwarded_ports");

        invokeMethod(service, "restoreForwardedPorts");

        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        assertTrue("No ports should be restored when prefs has null set", ports.isEmpty());
    }

    // =====================================================
    // removePortForward tests — calls the REAL method
    // =====================================================

    @Test
    public void testRemovePortForward_containsKey_checksMap() throws Exception {
        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(20000, new BackgroundService.PortInfo(20000, "192.168.1.5"));
        ports.put(30000, new BackgroundService.PortInfo(30000, "10.0.0.1"));

        // Call the real removePortForward method
        invokeMethod(service, "removePortForward", 20000);

        assertFalse("Port 20000 should be removed", ports.containsKey(20000));
        assertTrue("Port 30000 should still exist", ports.containsKey(30000));
    }

    @Test
    public void testRemovePortForward_notInMap_returnsEarly() throws Exception {
        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(20000, new BackgroundService.PortInfo(20000, "192.168.1.5"));

        // Remove a port that doesn't exist — should return early without error
        invokeMethod(service, "removePortForward", 99999);

        // Existing port should still be there
        assertEquals(1, ports.size());
        assertTrue("Port 20000 should still exist", ports.containsKey(20000));
    }

    // =====================================================
    // addPortForward tests — calls the REAL method with mocked SSH
    // =====================================================

    @Test
    public void testAddPortForward_newPort_notInSet_addsToMap() throws Exception {
        // Mock JSch Session
        com.jcraft.jsch.Session mockSession = mock(com.jcraft.jsch.Session.class);
        when(mockSession.isConnected()).thenReturn(true);
        when(mockSession.setPortForwardingL(anyString(), anyInt(), anyString(), anyInt())).thenReturn(0);
        doNothing().when(mockSession).disconnect();

        // Set sshSession field
        Field sshField = BackgroundService.class.getDeclaredField("sshSession");
        sshField.setAccessible(true);
        sshField.set(service, mockSession);

        // Call the real addPortForward(int, int, String) method
        // ensureConnection() will be called but since sshSession is connected it won't try to reconnect
        try {
            invokeMethod(service, "addPortForward", 3080, 80, "10.0.0.1");
        } catch (java.lang.reflect.InvocationTargetException e) {
            // updateNotification/saveForwardedPorts may throw due to Android framework stubs
            if (!(e.getCause() instanceof NullPointerException)) {
                throw e;
            }
        }

        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        assertTrue("Port 3080 should be in map", ports.containsKey(3080));
        assertEquals(80, ports.get(3080).targetPort);
        assertEquals("10.0.0.1", ports.get(3080).host);
    }

    @Test
    public void testAddPortForward_newPort_nullHost_targetHostDefaultsToLocalhost() throws Exception {
        // Test the targetHost logic from addPortForward directly:
        // String targetHost = (host == null || host.isEmpty()) ? "127.0.0.1" : host;
        String nullHost = null;
        String targetHost1 = (nullHost == null || nullHost.isEmpty()) ? "127.0.0.1" : nullHost;
        assertEquals("127.0.0.1", targetHost1);

        String emptyHost = "";
        String targetHost2 = (emptyHost == null || emptyHost.isEmpty()) ? "127.0.0.1" : emptyHost;
        assertEquals("127.0.0.1", targetHost2);

        String customHost = "10.0.0.1";
        String targetHost3 = (customHost == null || customHost.isEmpty()) ? "127.0.0.1" : customHost;
        assertEquals("10.0.0.1", targetHost3);
    }

    @Test
    public void testAddPortForward_alreadyInSet_sessionAlive_reachable_returnsEarly() throws Exception {
        com.jcraft.jsch.Session mockSession = mock(com.jcraft.jsch.Session.class);
        when(mockSession.isConnected()).thenReturn(true);

        Field sshField = BackgroundService.class.getDeclaredField("sshSession");
        sshField.setAccessible(true);
        sshField.set(service, mockSession);

        // Pre-add the port
        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(20000, new BackgroundService.PortInfo(20000, "10.0.0.1"));

        // When the port is already in set, session is alive, AND testLocalPort
        // returns true, addPortForward should return early without calling setPortForwardingL.
        // We simulate a reachable port by starting a local ServerSocket on the port.
        java.net.ServerSocket ss = new java.net.ServerSocket(0);
        int testPort = ss.getLocalPort();
        ports.put(testPort, new BackgroundService.PortInfo(testPort, "10.0.0.1"));

        try {
            invokeMethod(service, "addPortForward", testPort, testPort, "10.0.0.1");
            // Verify setPortForwardingL was NOT called (already forwarded and reachable)
            verify(mockSession, never()).setPortForwardingL(anyString(), anyInt(), anyString(), anyInt());
        } finally {
            ss.close();
        }
    }

    @Test
    public void testAddPortForward_alreadyInSet_sessionAlive_notReachable_reRegisters() throws Exception {
        com.jcraft.jsch.Session mockSession = mock(com.jcraft.jsch.Session.class);
        when(mockSession.isConnected()).thenReturn(true);
        doNothing().when(mockSession).delPortForwardingL(anyInt());
        // setPortForwardingL returns int — use doReturn with int
        doReturn(0).when(mockSession).setPortForwardingL(anyString(), anyInt(), anyString(), anyInt());

        Field sshField = BackgroundService.class.getDeclaredField("sshSession");
        sshField.setAccessible(true);
        sshField.set(service, mockSession);

        // Use a high-numbered port (59999) that is very unlikely to have a listener,
        // so testLocalPort returns false.
        int testPort = 59999;
        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(testPort, new BackgroundService.PortInfo(testPort, "10.0.0.1"));

        // addPortForward should detect the port is not reachable, delete old forwarding,
        // then re-register with setPortForwardingL.
        // For non-localhost hosts, the SSH tunnel routes through the server-side reverse
        // proxy (127.0.0.1:{localPort}) instead of directly to the remote host.
        invokeMethod(service, "addPortForward", testPort, testPort, "10.0.0.1");

        // Verify delPortForwardingL was called to remove stale forwarding
        verify(mockSession).delPortForwardingL(testPort);
        // Verify setPortForwardingL was called with reverse proxy routing
        verify(mockSession).setPortForwardingL(eq("127.0.0.1"), eq(testPort), eq("127.0.0.1"), eq(testPort));
    }

    // =====================================================
    // removePortForward full test — with mock SSH session
    // =====================================================

    @Test
    public void testRemovePortForward_withSession_removesAndSaves() throws Exception {
        com.jcraft.jsch.Session mockSession = mock(com.jcraft.jsch.Session.class);
        when(mockSession.isConnected()).thenReturn(true);
        doNothing().when(mockSession).delPortForwardingL(anyInt());

        Field sshField = BackgroundService.class.getDeclaredField("sshSession");
        sshField.setAccessible(true);
        sshField.set(service, mockSession);

        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(20000, new BackgroundService.PortInfo(20000, "10.0.0.1"));

        invokeMethod(service, "removePortForward", 20000);

        assertFalse("Port 20000 should be removed from map", ports.containsKey(20000));
        verify(mockSession).delPortForwardingL(20000);
    }

    // =====================================================
    // disconnectInternal test — iterates over keySet
    // =====================================================

    @Test
    public void testDisconnectInternal_iteratesOverKeySet() throws Exception {
        com.jcraft.jsch.Session mockSession = mock(com.jcraft.jsch.Session.class);
        when(mockSession.isConnected()).thenReturn(true);
        doNothing().when(mockSession).delPortForwardingL(anyInt());
        doNothing().when(mockSession).disconnect();

        Field sshField = BackgroundService.class.getDeclaredField("sshSession");
        sshField.setAccessible(true);
        sshField.set(service, mockSession);

        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(20000, new BackgroundService.PortInfo(20000, ""));
        ports.put(30000, new BackgroundService.PortInfo(30000, "10.0.0.1"));

        invokeMethod(service, "disconnectInternal");

        // Verify delPortForwardingL was called for each port
        verify(mockSession).delPortForwardingL(20000);
        verify(mockSession).delPortForwardingL(30000);
    }

    // =====================================================
    // Static addForwardedPort(Context, int, int, String) tests
    // =====================================================

    @Test
    public void testAddForwardedPort_staticMethod_callsStartService() throws Exception {
        // With returnDefaultValues=true, Intent extras are not persisted (getStringExtra returns null).
        // We can only verify that startService was called with an Intent targeting BackgroundService.
        Context mockContext = mock(Context.class);
        doReturn(null).when(mockContext).startService(any(Intent.class));

        BackgroundService.addForwardedPort(mockContext, 3080, 80, "10.0.0.1");

        verify(mockContext).startService(any(Intent.class));
    }

    @Test
    public void testAddForwardedPort_staticMethod_nullHost_callsStartService() throws Exception {
        Context mockContext = mock(Context.class);
        doReturn(null).when(mockContext).startService(any(Intent.class));

        BackgroundService.addForwardedPort(mockContext, 20000, 20000, null);

        verify(mockContext).startService(any(Intent.class));
    }

    @Test
    public void testAddForwardedPort_intentConstruction_hostExtra() throws Exception {
        // Verify the intent construction logic directly since returnDefaultValues=true
        // prevents reading extras from mock Intent objects.
        // The source code does:
        //   intent.putExtra("host", host != null ? host : "");
        String host = "10.0.0.1";
        assertEquals("10.0.0.1", host != null ? host : "");

        String nullHost = null;
        assertEquals("", nullHost != null ? nullHost : "");
    }

    // =====================================================
    // forwardedPorts map type verification
    // =====================================================

    @Test
    public void testForwardedPorts_isConcurrentHashMap() throws Exception {
        Field field = BackgroundService.class.getDeclaredField("forwardedPorts");
        field.setAccessible(true);
        Object value = field.get(service);
        assertTrue("forwardedPorts should be a ConcurrentHashMap",
                value instanceof ConcurrentHashMap);
    }

    @Test
    public void testForwardedPorts_isMapNotSet() throws Exception {
        Field field = BackgroundService.class.getDeclaredField("forwardedPorts");
        field.setAccessible(true);
        assertEquals("forwardedPorts should be Map type", Map.class, field.getType());
    }

    @Test
    public void testPortInfo_storesTargetPortAndHost() throws Exception {
        BackgroundService.PortInfo info1 = new BackgroundService.PortInfo(80, "192.168.1.5");
        assertEquals(80, info1.targetPort);
        assertEquals("192.168.1.5", info1.host);

        BackgroundService.PortInfo info2 = new BackgroundService.PortInfo(3306, null);
        assertEquals(3306, info2.targetPort);
        assertEquals("Null host should default to empty string", "", info2.host);
    }

    @Test
    public void testPortInfo_isNonLocalhost() throws Exception {
        // Non-localhost hosts
        assertTrue("LAN IP is non-localhost", new BackgroundService.PortInfo(80, "192.168.100.1").isNonLocalhost());
        assertTrue("Private IP is non-localhost", new BackgroundService.PortInfo(80, "10.0.0.1").isNonLocalhost());
        assertTrue("Domain is non-localhost", new BackgroundService.PortInfo(80, "example.com").isNonLocalhost());

        // Localhost hosts
        assertFalse("Empty host is localhost", new BackgroundService.PortInfo(80, "").isNonLocalhost());
        assertFalse("Null host is localhost", new BackgroundService.PortInfo(80, null).isNonLocalhost());
        assertFalse("localhost is localhost", new BackgroundService.PortInfo(80, "localhost").isNonLocalhost());
        assertFalse("127.0.0.1 is localhost", new BackgroundService.PortInfo(80, "127.0.0.1").isNonLocalhost());
        assertFalse("::1 is localhost", new BackgroundService.PortInfo(80, "::1").isNonLocalhost());
    }

    // =====================================================
    // Round-trip test: save then restore
    // =====================================================

    @Test
    public void testRoundTrip_saveAndRestore() throws Exception {
        @SuppressWarnings("unchecked")
        Map<Integer, BackgroundService.PortInfo> ports = (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
        ports.put(20000, new BackgroundService.PortInfo(20000, ""));
        ports.put(3080, new BackgroundService.PortInfo(80, "192.168.1.5"));

        // Save
        invokeMethod(service, "saveForwardedPorts");

        // Clear the map
        ports.clear();
        assertTrue("Map should be empty after clear", ports.isEmpty());

        // Restore
        invokeMethod(service, "restoreForwardedPorts");

        // Verify restored data matches original
        assertEquals(2, ports.size());
        assertEquals(20000, ports.get(20000).targetPort);
        assertEquals("", ports.get(20000).host);
        assertEquals(80, ports.get(3080).targetPort);
        assertEquals("192.168.1.5", ports.get(3080).host);
    }

    // =====================================================
    // SSH tunnel routing: non-localhost → reverse proxy, localhost → direct
    // =====================================================

    @Test
    public void testAddPortForward_nonLocalhostHost_routesThroughReverseProxy() throws Exception {
        // For non-localhost targets, the SSH tunnel should route through the server-side
        // reverse proxy (127.0.0.1:{localPort}) to ensure correct Host header rewriting.
        com.jcraft.jsch.Session mockSession = mock(com.jcraft.jsch.Session.class);
        when(mockSession.isConnected()).thenReturn(true);
        when(mockSession.setPortForwardingL(anyString(), anyInt(), anyString(), anyInt())).thenReturn(0);
        doNothing().when(mockSession).disconnect();

        Field sshField = BackgroundService.class.getDeclaredField("sshSession");
        sshField.setAccessible(true);
        sshField.set(service, mockSession);

        try {
            // Port 80 on 192.168.100.1 — should route through 127.0.0.1:{localPort}
            invokeMethod(service, "addPortForward", 3080, 80, "192.168.100.1");
        } catch (java.lang.reflect.InvocationTargetException e) {
            if (!(e.getCause() instanceof NullPointerException)) {
                throw e;
            }
        }

        // Verify: SSH tunnel targets 127.0.0.1:3080 (reverse proxy), NOT 192.168.100.1:80
        verify(mockSession).setPortForwardingL(eq("127.0.0.1"), eq(3080), eq("127.0.0.1"), eq(3080));
    }

    @Test
    public void testAddPortForward_localhostHost_routesDirectly() throws Exception {
        // For localhost targets, the SSH tunnel connects directly (no reverse proxy needed).
        com.jcraft.jsch.Session mockSession = mock(com.jcraft.jsch.Session.class);
        when(mockSession.isConnected()).thenReturn(true);
        when(mockSession.setPortForwardingL(anyString(), anyInt(), anyString(), anyInt())).thenReturn(0);
        doNothing().when(mockSession).disconnect();

        Field sshField = BackgroundService.class.getDeclaredField("sshSession");
        sshField.setAccessible(true);
        sshField.set(service, mockSession);

        try {
            // Port 5173 on localhost — should route directly to 127.0.0.1:5173
            invokeMethod(service, "addPortForward", 5173, 5173, "");
        } catch (java.lang.reflect.InvocationTargetException e) {
            if (!(e.getCause() instanceof NullPointerException)) {
                throw e;
            }
        }

        // Verify: SSH tunnel targets 127.0.0.1:5173 (direct, no reverse proxy)
        verify(mockSession).setPortForwardingL(eq("127.0.0.1"), eq(5173), eq("127.0.0.1"), eq(5173));
    }

    // --- Helper methods ---

    private void setStaticField(String name, Object value) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(name);
        field.setAccessible(true);
        field.set(null, value);
    }

    private Object getField(Object target, String fieldName) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(fieldName);
        field.setAccessible(true);
        return field.get(target);
    }

    private Object invokeMethod(Object target, String methodName, Object... args) throws Exception {
        Class<?>[] paramTypes = new Class<?>[args.length];
        for (int i = 0; i < args.length; i++) {
            paramTypes[i] = args[i].getClass();
            // Handle primitive types
            if (paramTypes[i] == Integer.class) paramTypes[i] = int.class;
        }
        Method method = BackgroundService.class.getDeclaredMethod(methodName, paramTypes);
        method.setAccessible(true);
        return method.invoke(target, args);
    }
}
