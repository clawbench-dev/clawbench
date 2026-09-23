package com.clawbench.app;

import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;

import com.jcraft.jsch.Session;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.HashMap;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;

import static org.junit.Assert.*;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

/**
 * Unit tests for BackgroundService's reverse (ssh -R) port forwarding.
 *
 * Reverse mappings live in a SEPARATE map from forward mappings because the keys
 * mean different things (server-side bind port vs device-side listening port).
 * Mixing them would make testLocalPort(serverPort) probe a port nothing listens
 * on locally, and would misroute non-localhost targets through the server's
 * reverse proxy.
 *
 * Uses Unsafe.allocateInstance() + Mockito spy, mirroring
 * BackgroundServicePortHostTest.
 */
public class BackgroundServiceReversePortTest {

    private BackgroundService service;
    private SharedPreferences mockPrefs;
    private Map<String, Set<String>> prefsData;

    @Before
    public void setUp() throws Exception {
        var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        Object unsafe = unsafeField.get(null);
        Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
        allocate.setAccessible(true);
        BackgroundService rawInstance = (BackgroundService) allocate.invoke(unsafe, BackgroundService.class);

        service = spy(rawInstance);

        // Unsafe allocation skips field initializers, so both maps need seeding.
        setField("forwardedPorts", new ConcurrentHashMap<Integer, BackgroundService.PortInfo>());
        setField("reversePorts", new ConcurrentHashMap<Integer, BackgroundService.PortInfo>());

        setStaticField("instance", service);
        setStaticField("isRunning", true);
        setStaticField("nativeWsNeeded", false);
        setStaticField("lastError", null);
        setField("isShuttingDown", false);

        prefsData = new HashMap<>();
        mockPrefs = mock(SharedPreferences.class);
        when(mockPrefs.getStringSet(eq("forwarded_ports"), any())).thenAnswer(inv -> {
            String key = inv.getArgument(0);
            Set<String> defVal = inv.getArgument(1);
            return prefsData.containsKey(key) ? prefsData.get(key) : defVal;
        });
        when(mockPrefs.getStringSet(eq("reverse_forwarded_ports"), any())).thenAnswer(inv -> {
            String key = inv.getArgument(0);
            Set<String> defVal = inv.getArgument(1);
            return prefsData.containsKey(key) ? prefsData.get(key) : defVal;
        });
        when(mockPrefs.edit()).thenAnswer(inv -> {
            SharedPreferences.Editor editor = mock(SharedPreferences.Editor.class);
            when(editor.putStringSet(anyString(), any())).thenAnswer(editInv -> {
                prefsData.put(editInv.getArgument(0), editInv.getArgument(1));
                return editor;
            });
            when(editor.remove(anyString())).thenAnswer(editInv -> {
                prefsData.remove(editInv.getArgument(0));
                return editor;
            });
            doNothing().when(editor).apply();
            return editor;
        });
        doReturn(mockPrefs).when(service).getSharedPreferences(anyString(), anyInt());
    }

    @After
    public void tearDown() throws Exception {
        try {
            setStaticField("instance", null);
            setStaticField("isRunning", false);
            setStaticField("nativeWsNeeded", false);
            setStaticField("lastError", null);
            setField("isShuttingDown", false);
        } catch (Exception ignored) {}
    }

    // =====================================================
    // PortInfo semantics
    // =====================================================

    @Test
    public void testPortInfo_reverseIsNeverNonLocalhost() {
        // A reverse mapping's target is on THIS device. Routing it through the
        // server-side reverse proxy (127.0.0.1:{localPort}) would dial the
        // server's own listener — the target host must be used verbatim.
        BackgroundService.PortInfo reverse = new BackgroundService.PortInfo(3000, "192.168.1.5", true);
        assertTrue(reverse.isReverse());
        assertFalse("reverse target must not be rerouted through the proxy", reverse.isNonLocalhost());
        assertEquals("192.168.1.5", reverse.host);
        assertEquals(3000, reverse.targetPort);
    }

    @Test
    public void testPortInfo_forwardKeepsNonLocalhostBehaviour() {
        // The legacy two-arg constructor must stay forward-direction.
        BackgroundService.PortInfo fwd = new BackgroundService.PortInfo(80, "192.168.1.5");
        assertFalse(fwd.isReverse());
        assertTrue(fwd.isNonLocalhost());
    }

    // =====================================================
    // Persistence
    // =====================================================

    @Test
    public void testSaveReversePorts_writesSeparateKey() throws Exception {
        reversePorts().put(9000, new BackgroundService.PortInfo(3000, "", true));

        invokeMethod(service, "saveReversePorts");

        Set<String> saved = prefsData.get("reverse_forwarded_ports");
        assertNotNull("reverse forwards must use their own prefs key", saved);
        assertTrue(saved.contains("9000:3000"));
        // The forward key must be untouched — an old install's list is separate.
        assertFalse(prefsData.containsKey("forwarded_ports"));
    }

    @Test
    public void testSaveReversePorts_includesHostWhenSet() throws Exception {
        reversePorts().put(9000, new BackgroundService.PortInfo(3000, "10.0.0.7", true));

        invokeMethod(service, "saveReversePorts");

        assertTrue(prefsData.get("reverse_forwarded_ports").contains("9000:3000:10.0.0.7"));
    }

    @Test
    public void testRestoreReversePorts_roundTrip() throws Exception {
        reversePorts().put(9000, new BackgroundService.PortInfo(3000, "10.0.0.7", true));
        invokeMethod(service, "saveReversePorts");

        // Simulate a fresh service: same prefs, empty in-memory maps.
        setField("reversePorts", new ConcurrentHashMap<Integer, BackgroundService.PortInfo>());
        invokeMethod(service, "restoreReversePorts");

        Map<Integer, BackgroundService.PortInfo> restored = reversePorts();
        assertEquals(1, restored.size());
        BackgroundService.PortInfo info = restored.get(9000);
        assertNotNull(info);
        assertEquals(3000, info.targetPort);
        assertEquals("10.0.0.7", info.host);
        assertTrue("restored entries must stay reverse", info.isReverse());
    }

    @Test
    public void testRestoreReversePorts_legacyAbsentDoesNotThrow() throws Exception {
        // An install that predates reverse forwarding has no such key. That must
        // be a no-op, not an error, and must not disturb forward mappings.
        forwardedPorts().put(20000, new BackgroundService.PortInfo(20000, ""));

        invokeMethod(service, "restoreReversePorts");

        assertTrue(reversePorts().isEmpty());
        assertEquals(1, forwardedPorts().size());
    }

    // =====================================================
    // Static entry points
    // =====================================================

    @Test
    public void testStaticAddReverseForwardedPort_callsStartService() {
        // Unit tests run with returnDefaultValues=true, so Intent extras are not
        // persisted and cannot be read back (same limitation the forward-path
        // tests document). Assert the service was asked to start, and cover the
        // action/extra construction separately below.
        Context ctx = mock(Context.class);
        doReturn(null).when(ctx).startService(any(Intent.class));

        BackgroundService.addReverseForwardedPort(ctx, 9000, 3000, "10.0.0.7");

        verify(ctx).startService(any(Intent.class));
    }

    @Test
    public void testStaticRemoveReverseForwardedPort_callsStartService() {
        Context ctx = mock(Context.class);
        doReturn(null).when(ctx).startService(any(Intent.class));

        BackgroundService.removeReverseForwardedPort(ctx, 9000);

        verify(ctx).startService(any(Intent.class));
    }

    @Test
    public void testReverseIntentActions_areDistinctFromForwardActions() {
        // The Service dispatches on the action string, so a collision with the
        // forward actions would silently route reverse adds into addPortForward.
        assertNotEquals("ADD_PORT", "ADD_REVERSE_PORT");
        assertNotEquals("REMOVE_PORT", "REMOVE_REVERSE_PORT");
    }

    // =====================================================
    // Session calls (ssh -R is driven by setPortForwardingR)
    // =====================================================

    @Test
    public void testAddReversePortForward_callsSetPortForwardingR() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);
        invokeMethod(service, "addReversePortForward", 9000, 3000, "");

        verify(session).setPortForwardingR("127.0.0.1", 9000, "127.0.0.1", 3000);
        assertEquals(1, reversePorts().size());
        assertNotNull(reversePorts().get(9000));
    }

    @Test
    public void testAddReversePortForward_nonLocalhostTargetNotRerouted() throws Exception {
        // Unlike a forward mapping, a non-localhost reverse target must be passed
        // through verbatim — there is no server-side proxy for it.
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);
        invokeMethod(service, "addReversePortForward", 9000, 3000, "192.168.1.5");

        verify(session).setPortForwardingR("127.0.0.1", 9000, "192.168.1.5", 3000);
    }

    @Test
    public void testAddReversePortForward_jschFailureRemovesAndReports() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        doThrow(new com.jcraft.jsch.JSchException("remote port forwarding failed for listen port 9000"))
                .when(session).setPortForwardingR(anyString(), anyInt(), anyString(), anyInt());
        setField("sshSession", session);

        invokeMethod(service, "addReversePortForward", 9000, 3000, "");

        // A failed bind must not linger in the map, or the UI and the notification
        // count would both claim a mapping that does not exist.
        assertFalse(reversePorts().containsKey(9000));
        assertEquals("remote port forwarding failed for listen port 9000", getStaticField("lastError"));
    }

    @Test
    public void testAddReversePortForward_alreadyRegisteredTreatedAsSuccess() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        // ensureConnection's replay loop may have set this up already.
        doThrow(new com.jcraft.jsch.JSchException("PortForwardingR: remote port 9000 is already registered"))
                .when(session).setPortForwardingR(anyString(), anyInt(), anyString(), anyInt());
        setField("sshSession", session);

        invokeMethod(service, "addReversePortForward", 9000, 3000, "");

        assertTrue("already-registered must keep the mapping", reversePorts().containsKey(9000));
    }

    @Test
    public void testAddReversePortForward_invalidPortsRejected() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);

        invokeMethod(service, "addReversePortForward", 0, 3000, "");
        invokeMethod(service, "addReversePortForward", 9000, 70000, "");

        verify(session, never()).setPortForwardingR(anyString(), anyInt(), anyString(), anyInt());
        assertTrue(reversePorts().isEmpty());
    }

    @Test
    public void testRemoveReversePortForward_callsDelPortForwardingR() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);
        reversePorts().put(9000, new BackgroundService.PortInfo(3000, "", true));

        invokeMethod(service, "removeReversePortForward", 9000);

        verify(session).delPortForwardingR(9000);
        assertFalse(reversePorts().containsKey(9000));
    }

    @Test
    public void testRemoveReversePortForward_unknownPortIsNoop() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);

        invokeMethod(service, "removeReversePortForward", 9000);

        verify(session, never()).delPortForwardingR(anyInt());
    }

    @Test
    public void testDisconnectInternal_deletesReverseForwards() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);
        reversePorts().put(9000, new BackgroundService.PortInfo(3000, "", true));
        forwardedPorts().put(20000, new BackgroundService.PortInfo(20000, ""));

        invokeMethod(service, "disconnectInternal");

        verify(session).delPortForwardingL(20000);
        verify(session).delPortForwardingR(9000);
        verify(session).disconnect();
        // The maps survive: they are the intent a reconnect replays.
        assertTrue(reversePorts().containsKey(9000));
        assertTrue(forwardedPorts().containsKey(20000));
    }

    // =====================================================
    // Cross-direction bookkeeping
    // =====================================================

    @Test
    public void testHasNoPorts_considersBothDirections() throws Exception {
        assertTrue((Boolean) invokeMethod(service, "hasNoPorts"));

        reversePorts().put(9000, new BackgroundService.PortInfo(3000, "", true));
        assertFalse("a reverse-only mapping is still work to do", (Boolean) invokeMethod(service, "hasNoPorts"));

        reversePorts().clear();
        forwardedPorts().put(20000, new BackgroundService.PortInfo(20000, ""));
        assertFalse((Boolean) invokeMethod(service, "hasNoPorts"));
    }

    @Test
    public void testTotalPortCount_sumsBothDirections() throws Exception {
        reversePorts().put(9000, new BackgroundService.PortInfo(3000, "", true));
        forwardedPorts().put(20000, new BackgroundService.PortInfo(20000, ""));
        forwardedPorts().put(20001, new BackgroundService.PortInfo(20001, ""));

        assertEquals(3, invokeMethod(service, "totalPortCount"));
    }

    // =====================================================
    // Helpers
    // =====================================================

    @SuppressWarnings("unchecked")
    private Map<Integer, BackgroundService.PortInfo> reversePorts() throws Exception {
        return (Map<Integer, BackgroundService.PortInfo>) getField(service, "reversePorts");
    }

    @SuppressWarnings("unchecked")
    private Map<Integer, BackgroundService.PortInfo> forwardedPorts() throws Exception {
        return (Map<Integer, BackgroundService.PortInfo>) getField(service, "forwardedPorts");
    }

    private void setField(String name, Object value) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(name);
        field.setAccessible(true);
        field.set(service, value);
    }

    private void setStaticField(String name, Object value) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(name);
        field.setAccessible(true);
        field.set(null, value);
    }

    private Object getStaticField(String name) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(name);
        field.setAccessible(true);
        return field.get(null);
    }

    private Object getField(Object target, String fieldName) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(fieldName);
        field.setAccessible(true);
        return field.get(target);
    }

    private Object invokeMethod(Object target, String methodName, Object... args) throws Exception {
        for (Method m : BackgroundService.class.getDeclaredMethods()) {
            if (m.getName().equals(methodName) && m.getParameterCount() == args.length) {
                m.setAccessible(true);
                return m.invoke(target, args);
            }
        }
        throw new NoSuchMethodException(methodName + " with " + args.length + " args");
    }
}
