package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;

import com.clawbench.app.tunnel.FakeTunnelStream;
import com.clawbench.app.tunnel.PortForwardTransport;
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
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyBoolean;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.doNothing;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for {@code BackgroundService}'s port-forward transport routing.
 *
 * <p>These cover the h2 side of T10/T11/T12 at the service level: which
 * transport the port operations go through, that the SSH default is untouched,
 * that the single-threaded {@code networkExecutor} is never parked, and that
 * the WifiLock/screen-off policy counts an h2 session as active.
 *
 * <p>Uses {@code Unsafe.allocateInstance()} + Mockito spy, mirroring
 * {@code BackgroundServicePortHostTest}. A fake {@link PortForwardTransport}
 * records the calls instead of a real {@code ServerSocket} — a real socket in a
 * Robolectric test leaves the Gradle worker un-exitable.
 */
public class BackgroundServiceTransportTest {

    /** Records every call the service makes through the transport abstraction. */
    static final class RecordingTransport implements PortForwardTransport {
        final AtomicInteger addLocalCalls = new AtomicInteger();
        final AtomicInteger removeLocalCalls = new AtomicInteger();
        final AtomicInteger addReverseCalls = new AtomicInteger();
        final AtomicInteger removeReverseCalls = new AtomicInteger();
        final AtomicInteger closeCalls = new AtomicInteger();
        volatile int lastLocalPort = -1;
        volatile int lastTargetPort = -1;
        volatile String lastTargetHost;
        volatile int lastReverseServerPort = -1;
        volatile int lastReverseTargetPort = -1;
        volatile String lastReverseTargetHost;
        volatile boolean connected = true;
        volatile boolean localReachable = true;
        volatile Exception addLocalFailure;

        @Override
        public void addLocal(int localPort, int targetPort, String targetHost) throws Exception {
            addLocalCalls.incrementAndGet();
            lastLocalPort = localPort;
            lastTargetPort = targetPort;
            lastTargetHost = targetHost;
            if (addLocalFailure != null) throw addLocalFailure;
        }

        @Override
        public void removeLocal(int localPort) {
            removeLocalCalls.incrementAndGet();
        }

        @Override
        public void addReverse(int serverPort, int targetPort, String targetHost) {
            addReverseCalls.incrementAndGet();
            lastReverseServerPort = serverPort;
            lastReverseTargetPort = targetPort;
            lastReverseTargetHost = targetHost;
        }

        @Override
        public void removeReverse(int serverPort) {
            removeReverseCalls.incrementAndGet();
        }

        @Override
        public boolean isConnected() {
            return connected;
        }

        @Override
        public boolean isLocalReachable(int localPort) {
            return localReachable;
        }

        @Override
        public void close() {
            closeCalls.incrementAndGet();
        }
    }

    private BackgroundService service;
    private SharedPreferences mockPrefs;
    private Map<String, Set<String>> prefsData;
    /** Boolean preferences written through {@link #enableH2Preference} or the setter. */
    private Map<String, Boolean> prefsBools;
    private ExecutorService testExecutor;

    /** The fake h2 adapter the service routes through in these tests. */
    private RecordingTransport h2;

    /** The fake h2 session, connected by default. */
    private FakeTunnelStream tunnel;

    /** Server URL the mocked prefs return; a test may blank it to fail fast. */
    private String serverUrl = "http://127.0.0.1:20000";

    @Before
    public void setUp() throws Exception {
        var unsafeField = Class.forName("sun.misc.Unsafe").getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        Object unsafe = unsafeField.get(null);
        Method allocate = unsafe.getClass().getDeclaredMethod("allocateInstance", Class.class);
        allocate.setAccessible(true);
        BackgroundService raw = (BackgroundService) allocate.invoke(unsafe, BackgroundService.class);
        service = spy(raw);

        // Unsafe allocation skips field initializers, so every collection the
        // code touches needs seeding.
        setField("forwardedPorts", new ConcurrentHashMap<Integer, BackgroundService.PortInfo>());
        setField("reversePorts", new ConcurrentHashMap<Integer, BackgroundService.PortInfo>());
        setField("isShuttingDown", false);
        setField("intentionalDisconnect", false);
        setField("monitorActive", false);
        setField("nativeWsActive", false);
        setField("sshScreenSuspended", false);
        setField("activeTransport", null);
        setField("sshTransport", null);
        setField("h2PortForwardTransport", null);
        setField("h2TransportOverride", null);
        setField("lastH2Kind", null);
        setField("lastError", null);

        // A connected fake session: ensureConnection() short-circuits on it
        // instead of dialing the network (which a real H2TunnelStream would).
        tunnel = new FakeTunnelStream();
        setField("tunnelStreamOverride", tunnel);

        // A fake h2 adapter: the real one would bind real ServerSockets, which
        // hang the Gradle worker under Robolectric.
        h2 = new RecordingTransport();
        setField("h2TransportOverride", h2);

        testExecutor = Executors.newSingleThreadExecutor();
        setField("networkExecutor", testExecutor);

        setStaticField("instance", service);
        setStaticField("isRunning", true);
        setStaticField("nativeWsNeeded", false);
        setStaticField("lastError", null);

        prefsData = new HashMap<>();
        prefsBools = new HashMap<>();
        mockPrefs = mock(SharedPreferences.class);
        when(mockPrefs.getStringSet(eq("forwarded_ports"), any())).thenAnswer(inv ->
                prefsData.containsKey("forwarded_ports") ? prefsData.get("forwarded_ports") : inv.getArgument(1));
        when(mockPrefs.getStringSet(eq("reverse_forwarded_ports"), any())).thenAnswer(inv ->
                prefsData.containsKey("reverse_forwarded_ports")
                        ? prefsData.get("reverse_forwarded_ports") : inv.getArgument(1));
        when(mockPrefs.getString(eq("server_url"), anyString())).thenAnswer(inv ->
                serverUrl != null ? serverUrl : inv.getArgument(1));
        // Boolean reads honour the written value first, then the stored default
        // (false for the h2 toggle). An absent key therefore answers the
        // pre-tunnel default = SSH.
        when(mockPrefs.getBoolean(anyString(), anyBoolean())).thenAnswer(inv -> {
            String key = inv.getArgument(0);
            return prefsBools.containsKey(key) ? prefsBools.get(key) : inv.getArgument(1);
        });
        when(mockPrefs.edit()).thenAnswer(inv -> {
            SharedPreferences.Editor editor = mock(SharedPreferences.Editor.class);
            when(editor.putStringSet(anyString(), any())).thenAnswer(editInv -> {
                prefsData.put(editInv.getArgument(0), editInv.getArgument(1));
                return editor;
            });
            when(editor.putBoolean(anyString(), anyBoolean())).thenAnswer(editInv -> {
                prefsBools.put(editInv.getArgument(0), editInv.getArgument(1));
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
        } catch (Exception ignored) {
        }
        if (testExecutor != null) testExecutor.shutdownNow();
    }

    /**
     * Make the service read {@code tunnel_transport_h2_enabled = true}.
     *
     * <p>Replaces the old {@code setField("transportPreference", H2)}: the
     * preference is no longer a field, so it has to be driven through the
     * SharedPreferences the service actually consults.
     */
    private void enableH2Preference() {
        prefsBools.put("tunnel_transport_h2_enabled", true);
    }

    // ==================================================================
    // Transport selection
    // ==================================================================

    @Test
    public void defaultPreferenceIsSsh() {
        // The whole compatibility story: an install that never opts in must
        // keep behaving exactly as before the tunnel existed. The preference is
        // now persisted, so the default is what an unstubbed SharedPreferences
        // hands back for an absent key.
        assertFalse(BackgroundService.isTunnelTransportH2Enabled(service));
    }

    @Test
    public void setTunnelTransportH2Enabled_persistsTheBoolean() {
        // The setter is the only writer of the preference; it must round-trip
        // through the same prefs the connect path reads.
        BackgroundService.setTunnelTransportH2Enabled(service, true);
        assertTrue(BackgroundService.isTunnelTransportH2Enabled(service));
        BackgroundService.setTunnelTransportH2Enabled(service, false);
        assertFalse(BackgroundService.isTunnelTransportH2Enabled(service));
    }

    @Test
    public void addPortForward_underH2_doesNotTouchTheSshSession() throws Exception {
        enableH2Preference();
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);
        setField("activeTransport", null);

        invoke("addPortForward", 3080, 80, "127.0.0.1");

        // The h2 path must not reach into JSch at all. Without the h2 adapter
        // seam this test would also fail on the real ServerSocket bind.
        verify(session, never()).setPortForwardingL(anyString(), anyInt(), anyString(), anyInt());
        verify(session, never()).delPortForwardingL(anyInt());
        // addLocal is called twice on a fresh add: once by ensureConnection's
        // replay (the port was recorded before connecting) and once by the
        // explicit add. The h2 adapter is idempotent, exactly as JSch's
        // "already registered" handling is. Assert the exact count, not >= 1:
        // the latter would pass if the explicit add were deleted, which is the
        // call that actually binds the listener.
        assertEquals("the replay add plus the explicit add, exactly",
                2, h2.addLocalCalls.get());
        assertEquals(3080, h2.lastLocalPort);
        assertEquals(80, h2.lastTargetPort);
        assertEquals("127.0.0.1", h2.lastTargetHost);
    }

    @Test
    public void addPortForward_underSsh_stillCallsSetPortForwardingL() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        doReturn(0).when(session).setPortForwardingL(anyString(), anyInt(), anyString(), anyInt());
        setField("sshSession", session);
        // Force the SSH adapter to be built from the session.
        setField("activeTransport", null);

        invoke("addPortForward", 3080, 80, "127.0.0.1");

        verify(session).setPortForwardingL("127.0.0.1", 3080, "127.0.0.1", 80);
    }

    @Test
    public void addPortForward_nonLocalhostUnderH2_routesThroughTheServerProxy() throws Exception {
        enableH2Preference();
        setField("activeTransport", null);

        invoke("addPortForward", 3080, 80, "10.0.0.1");

        // Non-localhost targets are only reachable via the server-side reverse
        // proxy on 127.0.0.1:{localPort}; the tunnel is TCP-level and cannot
        // rewrite the Host header itself.
        assertEquals("127.0.0.1", h2.lastTargetHost);
        assertEquals(3080, h2.lastTargetPort);
    }

    @Test
    public void removePortForward_underH2_releasesTheListenerWithoutJsch() throws Exception {
        enableH2Preference();
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);
        setField("activeTransport", null);
        forwardedPorts().put(3080, new BackgroundService.PortInfo(80, ""));

        invoke("removePortForward", 3080);

        assertEquals(1, h2.removeLocalCalls.get());
        verify(session, never()).delPortForwardingL(anyInt());
        assertFalse(forwardedPorts().containsKey(3080));
    }

    @Test
    public void removePortForward_underH2_releasesEvenWhenTheTunnelIsDown() throws Exception {
        enableH2Preference();
        // The listener outlives the session, so a removal must still run while
        // the tunnel is down — otherwise the local port stays bound forever.
        setField("activeTransport", null);
        h2.connected = false;
        forwardedPorts().put(3080, new BackgroundService.PortInfo(80, ""));

        invoke("removePortForward", 3080);

        assertEquals(1, h2.removeLocalCalls.get());
    }

    @Test
    public void addPortForward_h2Failure_removesTheMappingAndReports() throws Exception {
        enableH2Preference();
        setField("activeTransport", null);
        h2.addLocalFailure = new Exception("bind refused");

        invoke("addPortForward", 3080, 80, "127.0.0.1");

        assertFalse("a failed add must not linger in the bookkeeping",
                forwardedPorts().containsKey(3080));
        assertEquals("bind refused", BackgroundService.getLastError());
    }

    @Test
    public void addPortForward_invalidPort_reachesTheTransportUnvalidated() throws Exception {
        enableH2Preference();
        // Pins the validation asymmetry: addReversePortForward rejects
        // serverPort<=0 or >65535 (BackgroundService:2111) BEFORE touching the
        // transport, but addPortForward (:1866) does not — the bad port reaches
        // the transport, which for h2 means ServerSocket.bind. This test records
        // that the transport is asked with the raw value; the report flags the
        // asymmetry and whether port 0 is legitimate.
        setField("activeTransport", null);

        invoke("addPortForward", -1, 80, "127.0.0.1");

        assertTrue("the forward path must NOT pre-validate (unlike the reverse path)",
                h2.addLocalCalls.get() >= 1);
        assertEquals("the raw invalid port must reach the transport", -1, h2.lastLocalPort);
    }

    @Test
    public void addReversePortForward_invalidPort_isRejectedBeforeTheTransport() throws Exception {
        enableH2Preference();
        // The contrast to the test above: the reverse path validates up front,
        // so the transport is never asked with an impossible port.
        setField("activeTransport", null);

        invoke("addReversePortForward", 0, 3000, "");
        invoke("addReversePortForward", 9000, 70000, "");

        assertEquals("an invalid reverse port must never reach the transport",
                0, h2.addReverseCalls.get());
        assertTrue(reversePorts().isEmpty());
    }

    // ==================================================================
    // Error-kind mapping is discarded at the service boundary (report item)
    // ==================================================================

    @Test
    public void getErrorType_stringMatchesAndCannotRecoverTheH2Kind() throws Exception {
        // PINS THE DEAD MAPPING: TunnelErrorKind.uiType() maps AUTH -> "auth",
        // but the service never consults it. ensureH2Connection() stores only
        // tunnel.getLastError() (a message), and getErrorType() re-derives the
        // category by substring-matching that message. An h2 AUTH message
        // ("... rejected with HTTP 403") shares none of the auth keywords, so
        // the precise classification is lost and the frontend sees "unknown".
        // Do not "fix" this here — the report asks for it to be reported, not
        // wired up.
        assertEquals("auth", com.clawbench.app.tunnel.TunnelErrorKind.AUTH.uiType());

        setField("lastError", "open stream 127.0.0.1:8080 rejected with HTTP 403");
        assertEquals("the string-matching path loses the h2 AUTH classification",
                "unknown", BackgroundService.getErrorType());
    }

    // ==================================================================
    // -R
    // ==================================================================

    @Test
    public void addReversePortForward_underH2_bindsThroughTheTransport() throws Exception {
        enableH2Preference();
        setField("activeTransport", null);

        invoke("addReversePortForward", 9000, 3000, "192.168.1.5");

        // Like addLocal, addReverse runs once from the ensureConnection replay
        // (the mapping is recorded before connecting) and once from the
        // explicit call; the adapter is idempotent. Assert the exact count:
        // >= 1 would pass if the explicit bind were dropped, leaving the port
        // bound only by the replay.
        assertEquals("the replay bind plus the explicit bind, exactly",
                2, h2.addReverseCalls.get());
        assertEquals(9000, h2.lastReverseServerPort);
        assertEquals(3000, h2.lastReverseTargetPort);
        // A reverse target is on THIS device; it must not be rerouted.
        assertEquals("192.168.1.5", h2.lastReverseTargetHost);
        assertTrue(reversePorts().containsKey(9000));
    }

    @Test
    public void removeReversePortForward_underH2_unbindsThroughTheTransport() throws Exception {
        enableH2Preference();
        setField("activeTransport", null);
        reversePorts().put(9000, new BackgroundService.PortInfo(3000, "", true));

        invoke("removeReversePortForward", 9000);

        assertEquals(1, h2.removeReverseCalls.get());
        assertFalse(reversePorts().containsKey(9000));
    }

    @Test
    public void addReversePortForward_underSsh_stillCallsSetPortForwardingR() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);
        setField("activeTransport", null);

        invoke("addReversePortForward", 9000, 3000, "");

        verify(session).setPortForwardingR("127.0.0.1", 9000, "127.0.0.1", 3000);
    }

    // ==================================================================
    // disconnect / reconnect
    // ==================================================================

    @Test
    public void disconnectInternal_dropsTheTransportReference() throws Exception {
        setField("activeTransport", h2);

        invoke("disconnectInternal");

        assertNull("the transport reference must be dropped so a reconnect rebuilds",
                getField("activeTransport"));
    }

    @Test
    public void disconnectInternal_underH2_closesTheTransportAndListeners() throws Exception {
        enableH2Preference();
        // The h2 adapter owns local listeners and in-flight streams the JSch
        // branch knows nothing about, so disconnectInternal must close it. The
        // real adapter is used here (the field is what disconnectInternal
        // closes); it never binds a socket because no listener was added.
        setField("h2TransportOverride", null);
        com.clawbench.app.tunnel.H2PortForwardTransport adapter =
                new com.clawbench.app.tunnel.H2PortForwardTransport(() -> tunnel);
        setField("h2PortForwardTransport", adapter);
        setField("activeTransport", h2);

        invoke("disconnectInternal");

        assertTrue("the h2 session must be torn down", tunnel.closed);
        assertNull(getField("activeTransport"));
    }

    @Test
    public void ensureConnection_underH2_connectsInlineWithoutTheNetworkExecutor() throws Exception {
        enableH2Preference();
        // The h2 accept loops and stream pumps must never run on the
        // single-threaded networkExecutor: parking it would starve
        // ensureConnection/disconnect/WS reconnect. This asserts the connect
        // path completes on the caller's thread.
        setField("activeTransport", null);

        service.ensureConnection();

        assertTrue("the tunnel must have been connected", tunnel.connected);
        assertNotNull("the h2 adapter must become the active transport",
                getField("activeTransport"));
    }

    @Test
    public void ensureH2Connection_passesTheRememberedKindOnReconnect() throws Exception {
        enableH2Preference();
        // BackgroundService:1642 passes lastH2Kind to connect(), and :1648
        // stores the result. This is the whole "remember the last successful
        // transport" wiring: without it a plaintext deployment pays a wasted
        // TLS rejection on every reconnect. It was previously unverified.
        setField("activeTransport", null);
        tunnel.connected = false;
        tunnel.connectResult = com.clawbench.app.tunnel.TransportKind.TLS;

        service.ensureConnection();
        assertEquals("the first connect has nothing remembered yet",
                1, tunnel.connectPreferred.size());
        assertNull("the first connect must pass a null preference",
                tunnel.connectPreferred.get(0));
        assertEquals("the successful kind must be remembered",
                com.clawbench.app.tunnel.TransportKind.TLS, getField("lastH2Kind"));

        // Force a reconnect and assert the remembered kind is handed back.
        tunnel.connected = false;
        service.ensureConnection();

        assertEquals(2, tunnel.connectPreferred.size());
        assertEquals("the reconnect must pass the remembered transport",
                com.clawbench.app.tunnel.TransportKind.TLS,
                tunnel.connectPreferred.get(1));
    }

    @Test
    public void setTunnelTransportH2Enabled_whileConnected_doesNotTearDownTheLiveTransport() throws Exception {
        // The setter only writes the preference; an already-connected tunnel
        // keeps running until the next reconnect (documented at
        // MainActivity:2714-2716, "takes effect on reconnect").
        // transportForPortOps() (BackgroundService:1788-1790) keeps routing
        // through the live activeTransport, so the change must be deferred, not
        // applied eagerly.
        setField("activeTransport", h2);
        h2.connected = true;

        BackgroundService.setTunnelTransportH2Enabled(service, false);

        assertSame("the live transport must survive a preference change",
                h2, getField("activeTransport"));
        assertEquals("the live transport must not be closed", 0, h2.closeCalls.get());

        // Deferred, not dropped: a port op still routes through the live h2
        // transport even though the preference now says SSH.
        forwardedPorts().put(3080, new BackgroundService.PortInfo(80, ""));
        invoke("removePortForward", 3080);
        assertEquals("port ops must keep using the live transport",
                1, h2.removeLocalCalls.get());
    }

    @Test
    public void reconnect_replaysForwardAndReversePortsExactlyOnceOnH2() throws Exception {
        enableH2Preference();
        // replayForwardedPortsOnActiveTransport():1698 and
        // replayReversePortsOnActiveTransport():1671. Pre-populate both maps,
        // drop the transport (a disconnect), then reconnect: each desired
        // mapping must be re-added exactly once. This is the exact-count test
        // that supersedes the >= 1 assertions on the add paths.
        forwardedPorts().put(3080, new BackgroundService.PortInfo(80, ""));
        forwardedPorts().put(3081, new BackgroundService.PortInfo(81, ""));
        reversePorts().put(9000, new BackgroundService.PortInfo(3000, "", true));
        reversePorts().put(9001, new BackgroundService.PortInfo(3001, "", true));

        // Disconnect: the transport reference is dropped, the desired maps stay.
        setField("activeTransport", h2);
        invoke("disconnectInternal");
        assertNull("disconnect must drop the transport", getField("activeTransport"));

        service.ensureConnection();

        assertSame("the h2 adapter must become active again", h2, getField("activeTransport"));
        assertEquals("each forward must be replayed exactly once",
                2, h2.addLocalCalls.get());
        assertEquals("each reverse must be replayed exactly once",
                2, h2.addReverseCalls.get());
    }

    // ==================================================================
    // WifiLock / screen policy (T12)
    // ==================================================================

    @Test
    public void maybeReleaseWifiLock_keepsTheLockWhileH2IsActive() throws Exception {
        // Under h2 sshSession is always null, so the pre-tunnel predicate would
        // release the lock out from under a running tunnel.
        tunnel.connected = true;
        android.net.wifi.WifiManager.WifiLock lock = heldLock();
        setField("wifiLock", lock);

        invoke("maybeReleaseWifiLock");

        verify(lock, never()).release();
    }

    @Test
    public void maybeReleaseWifiLock_releasesWhenNoTransportIsActive() throws Exception {
        tunnel.connected = false;
        android.net.wifi.WifiManager.WifiLock lock = heldLock();
        setField("wifiLock", lock);

        invoke("maybeReleaseWifiLock");

        verify(lock).release();
    }

    @Test
    public void maybeReleaseWifiLock_keepsTheLockWhileTheSshSessionIsActive() throws Exception {
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);
        android.net.wifi.WifiManager.WifiLock lock = heldLock();
        setField("wifiLock", lock);

        invoke("maybeReleaseWifiLock");

        verify(lock, never()).release();
    }

    @Test
    public void maybeReleaseWifiLock_keepsTheLockWhileNativeWsIsActive() throws Exception {
        setField("nativeWsActive", true);
        android.net.wifi.WifiManager.WifiLock lock = heldLock();
        setField("wifiLock", lock);

        invoke("maybeReleaseWifiLock");

        verify(lock, never()).release();
    }

    @Test
    public void maybeReleaseWakeLock_keepsTheLockWhileH2IsActive() throws Exception {
        tunnel.connected = true;
        android.os.PowerManager.WakeLock lock = heldWakeLock();
        setField("wakeLock", lock);

        invoke("maybeReleaseWakeLock");

        verify(lock, never()).release();
    }

    @Test
    public void maybeReleaseWakeLock_releasesWhenNoTransportIsActive() throws Exception {
        tunnel.connected = false;
        android.os.PowerManager.WakeLock lock = heldWakeLock();
        setField("wakeLock", lock);

        invoke("maybeReleaseWakeLock");

        verify(lock).release();
    }

    @Test
    public void isTunnelTransportActive_countsTheH2Session() throws Exception {
        tunnel.connected = true;
        assertTrue((Boolean) invoke("isTunnelTransportActive"));

        tunnel.connected = false;
        assertFalse((Boolean) invoke("isTunnelTransportActive"));
    }

    @Test
    public void isTunnelTransportActive_countsTheSshSession() throws Exception {
        // No h2 session in this test, so the SSH half of the predicate is what
        // is under test.
        tunnel.connected = false;
        Session session = mock(Session.class);
        when(session.isConnected()).thenReturn(true);
        setField("sshSession", session);
        assertTrue((Boolean) invoke("isTunnelTransportActive"));

        when(session.isConnected()).thenReturn(false);
        assertFalse((Boolean) invoke("isTunnelTransportActive"));
    }

    @Test
    public void getActiveTunnelTransport_reportsTheH2WireKind() {
        // Reports the transport *family* of the live h2 session ("h2"), not the
        // raw wire kind ("tls"/"h2c"): the frontend whitelist is
        // ['ssh','h2','both'] (usePortForward.ts), so a raw wire name would be
        // rejected and fall back to the preference.
        assertEquals("h2", BackgroundService.getActiveTunnelTransport());
    }

    @Test
    public void getActiveTunnelTransport_isEmptyWithoutASession() {
        tunnel.connected = false;
        assertEquals("", BackgroundService.getActiveTunnelTransport());
    }

    // ==================================================================
    // "tunnel reconnected" notification wording (T15)
    // ==================================================================

    /**
     * N2 is the only notification that can name the transport: it fires after
     * ensureConnection() succeeded, so the kind is known. h2 -> HTTP/2 wording,
     * SSH (no wire kind) -> SSH wording. The progress strings stay generic
     * because a disconnected h2 is indistinguishable from SSH.
     */
    @Test
    public void recoveringNotificationResId_namesTheLiveTransport() {
        tunnel.connected = true;
        assertEquals("a live h2 session must announce HTTP/2",
                R.string.h2_notification_recovering,
                BackgroundService.recoveringNotificationResId());

        tunnel.connected = false;
        assertEquals("SSH has no wire kind, so an empty transport means SSH",
                R.string.ssh_notification_recovering,
                BackgroundService.recoveringNotificationResId());
    }

    // ==================================================================
    // screen-off suspend (T12)
    // ==================================================================

    @Test
    public void suspendTunnelForScreenOff_setsTheSuspendedFlag() throws Exception {
        tunnel.connected = true;
        setField("activeTransport", h2);

        service.suspendTunnelForScreenOff();

        assertTrue("the reconnect monitor is gated on this flag",
                (Boolean) getField("sshScreenSuspended"));
    }

    @Test
    public void suspendTunnelForScreenOff_closesTheH2Transport() throws Exception {
        enableH2Preference();
        // The h2 adapter owns local listeners and in-flight streams; suspending
        // must release them, not just mark a flag.
        setField("h2TransportOverride", null);
        com.clawbench.app.tunnel.H2PortForwardTransport adapter =
                new com.clawbench.app.tunnel.H2PortForwardTransport(() -> tunnel);
        setField("h2PortForwardTransport", adapter);
        setField("activeTransport", h2);

        service.suspendTunnelForScreenOff();

        assertTrue("the h2 session must be torn down on screen off", tunnel.closed);
        assertNull("the active transport must be dropped", getField("activeTransport"));
    }

    @Test
    public void suspendTunnelForScreenOff_releasesTheWifiLock() throws Exception {
        // The real adapter, so disconnectInternal actually tears the fake
        // session down and maybeRelease* sees nothing active.
        setField("h2TransportOverride", null);
        com.clawbench.app.tunnel.H2PortForwardTransport adapter =
                new com.clawbench.app.tunnel.H2PortForwardTransport(() -> tunnel);
        setField("h2PortForwardTransport", adapter);
        setField("activeTransport", h2);
        android.net.wifi.WifiManager.WifiLock lock = heldLock();
        setField("wifiLock", lock);

        service.suspendTunnelForScreenOff();

        // maybeRelease* runs after the teardown, so nothing is active any more.
        verify(lock).release();
    }

    @Test
    public void suspendTunnelForScreenOff_stopsTheConnectionMonitor() throws Exception {
        setField("activeTransport", h2);
        setField("monitorActive", true);

        service.suspendTunnelForScreenOff();

        assertFalse("the monitor must stop while suspended",
                (Boolean) getField("monitorActive"));
    }

    // ==================================================================
    // helpers
    // ==================================================================

    private android.net.wifi.WifiManager.WifiLock heldLock() {
        android.net.wifi.WifiManager.WifiLock lock =
                mock(android.net.wifi.WifiManager.WifiLock.class);
        when(lock.isHeld()).thenReturn(true);
        return lock;
    }

    private android.os.PowerManager.WakeLock heldWakeLock() {
        android.os.PowerManager.WakeLock lock =
                mock(android.os.PowerManager.WakeLock.class);
        when(lock.isHeld()).thenReturn(true);
        return lock;
    }

    @SuppressWarnings("unchecked")
    private Map<Integer, BackgroundService.PortInfo> forwardedPorts() throws Exception {
        return (Map<Integer, BackgroundService.PortInfo>) getField("forwardedPorts");
    }

    @SuppressWarnings("unchecked")
    private Map<Integer, BackgroundService.PortInfo> reversePorts() throws Exception {
        return (Map<Integer, BackgroundService.PortInfo>) getField("reversePorts");
    }

    private void setField(String name, Object value) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(name);
        field.setAccessible(true);
        // lastError is static; setting it on the instance would silently leave
        // the process-wide value in place.
        if (java.lang.reflect.Modifier.isStatic(field.getModifiers())) {
            field.set(null, value);
        } else {
            field.set(service, value);
        }
    }

    private Object getField(String name) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(name);
        field.setAccessible(true);
        return java.lang.reflect.Modifier.isStatic(field.getModifiers())
                ? field.get(null) : field.get(service);
    }

    private void setStaticField(String name, Object value) throws Exception {
        Field field = BackgroundService.class.getDeclaredField(name);
        field.setAccessible(true);
        field.set(null, value);
    }

    private Object invoke(String methodName, Object... args) throws Exception {
        for (Method m : BackgroundService.class.getDeclaredMethods()) {
            if (m.getName().equals(methodName) && m.getParameterCount() == args.length) {
                m.setAccessible(true);
                return m.invoke(service, args);
            }
        }
        throw new NoSuchMethodException(methodName + " with " + args.length + " args");
    }
}
