package com.clawbench.app;

import android.app.Application;
import android.content.Context;
import android.content.SharedPreferences;

import com.clawbench.app.tunnel.PortForwardTransport;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.lang.reflect.Field;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

/**
 * The real-preference half of the h2 tunnel-toggle contract, under Robolectric.
 *
 * <p>{@code BackgroundServiceTransportTest} drives the toggle through a
 * Mockito-stubbed {@link SharedPreferences} (fast, but it can only prove the
 * code consulted the store it was handed). This class uses a real Robolectric
 * {@link Context}, so it proves the value genuinely lands in the
 * {@code clawbench_prefs} store under the documented key and survives a fresh
 * service/read handle — which is the whole point of making the preference
 * (not a process-wide field) the source of truth.
 *
 * <p>Follows {@code BackgroundServiceFloatingTest}: {@code Robolectric
 * .buildService}, a per-test {@code clear().commit()}, and the static reset in
 * {@code tearDown}.
 *
 * <p>Deliberately does NOT exercise {@code ensureConnection()} here. Its
 * transport dispatch needs a connected fake session plus a fake h2 adapter
 * (a real one binds sockets, which hangs the Gradle worker under Robolectric),
 * and {@code BackgroundServiceTransportTest} already covers both branches with
 * exactly those fakes. This class stays on the persistence contract.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class BackgroundServiceTunnelTransportPrefsTest {

    private static final String PREFS_NAME = "clawbench_prefs";
    private static final String KEY = "tunnel_transport_h2_enabled";

    private BackgroundService service;
    private Application appContext;

    @Before
    public void setUp() throws Exception {
        service = Robolectric.buildService(BackgroundService.class).get();
        appContext = RuntimeEnvironment.getApplication();

        // Fresh prefs per test so "default" really means "never set".
        appContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .edit().clear().commit();

        setStaticField("instance", service);
        setStaticField("isRunning", true);
    }

    @After
    public void tearDown() throws Exception {
        try {
            setStaticField("instance", null);
            setStaticField("isRunning", false);
        } catch (Exception ignored) {
        }
    }

    // =====================================================
    // Default (compatibility story)
    // =====================================================

    @Test
    public void defaultsToSsh() {
        // An install that never opted in must behave exactly as before the h2
        // tunnel existed: the absent key answers false.
        assertFalse("the h2 toggle must default to off (SSH)",
                BackgroundService.isTunnelTransportH2Enabled(appContext));
    }

    @Test
    public void missingKey_isNotPresentInTheStore() {
        // The default is produced by the getter's fallback, not by a seeded
        // value — otherwise "never set" and "explicitly off" would be
        // indistinguishable and the default could silently drift.
        assertFalse(appContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                .contains(KEY));
    }

    // =====================================================
    // Setter round-trip
    // =====================================================

    @Test
    public void setter_persistsTrueThenFalse() {
        BackgroundService.setTunnelTransportH2Enabled(appContext, true);
        assertTrue("set(true) must read back true",
                BackgroundService.isTunnelTransportH2Enabled(appContext));

        BackgroundService.setTunnelTransportH2Enabled(appContext, false);
        assertFalse("set(false) must read back false",
                BackgroundService.isTunnelTransportH2Enabled(appContext));
    }

    @Test
    public void setter_writesTheDocumentedKey() {
        BackgroundService.setTunnelTransportH2Enabled(appContext, true);

        // Pin the exact key the frontend toggles: a rename would silently
        // desync the JS bridge (which sends a boolean, not a key) from the
        // connect path.
        assertTrue("the value must land under " + KEY,
                appContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
                        .getBoolean(KEY, false));
    }

    // =====================================================
    // Cross-instance persistence
    // =====================================================

    @Test
    public void value_survivesAFreshServiceAndReadHandle() {
        BackgroundService.setTunnelTransportH2Enabled(appContext, true);

        // Drop every process-wide static a cold start would not have. If the
        // toggle had been cached in a field (the pre-T1 design), this would
        // read false.
        try {
            setStaticField("instance", null);
            setStaticField("isRunning", false);
        } catch (Exception e) {
            throw new AssertionError(e);
        }

        BackgroundService recreated = Robolectric.buildService(BackgroundService.class).get();
        assertTrue("a recreated service must read the persisted value",
                BackgroundService.isTunnelTransportH2Enabled(recreated));

        // A separate read handle (not the service's own context) agrees, so the
        // value is in the shared store rather than on the service instance.
        SharedPreferences freshHandle =
                appContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
        assertTrue("a fresh prefs handle must see the same value",
                freshHandle.getBoolean(KEY, false));
    }

    // =====================================================
    // Null safety (T1 guard)
    // =====================================================

    @Test
    public void nullContext_returnsFalseWithoutThrowing() {
        // The guard exists because ensureConnection() is a hot path: a null
        // context must degrade to SSH, never NPE.
        assertFalse("a null context must answer the SSH default",
                BackgroundService.isTunnelTransportH2Enabled(null));
    }

    @Test
    public void nullPreferences_returnsFalseWithoutThrowing() throws Exception {
        // The second half of the guard: a context whose getSharedPreferences
        // returns null (stub/misbehaving) must also answer false, not crash.
        Context context = mock(Context.class);
        org.mockito.Mockito.when(context.getSharedPreferences(
                org.mockito.ArgumentMatchers.anyString(),
                org.mockito.ArgumentMatchers.anyInt())).thenReturn(null);

        assertFalse("a null prefs handle must answer the SSH default",
                BackgroundService.isTunnelTransportH2Enabled(context));
    }

    // =====================================================
    // Toggling does not touch a live transport (design §7.4)
    // =====================================================

    @Test
    public void setter_leavesTheLiveTransportUntouched() throws Exception {
        // The switch takes effect on the next reconnect (documented at
        // MainActivity:2714-2716). The setter must not close or swap the
        // transport a running tunnel is using.
        PortForwardTransport live = mock(PortForwardTransport.class);
        setInstanceField("activeTransport", live);

        BackgroundService.setTunnelTransportH2Enabled(appContext, false);

        assertSame("the live transport must survive a preference change",
                live, getInstanceField("activeTransport"));
        verify(live, never()).close();
    }

    // --- Helpers -------------------------------------------------------

    private static void setStaticField(String name, Object value) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(null, value);
    }

    private void setInstanceField(String name, Object value) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(service, value);
    }

    private Object getInstanceField(String name) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        return f.get(service);
    }
}
