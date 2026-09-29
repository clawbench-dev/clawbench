package com.clawbench.app;

import android.content.Context;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;

import okhttp3.WebSocket;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;

/**
 * Regression tests for the native event cursor ("last_seen_event_id").
 *
 * The cursor is what stops the offline-recovery path from re-delivering events
 * the client already saw. It is written from exactly one place —
 * NativeEventListener.onMessage — so these tests drive that real code path via
 * reflection rather than re-asserting the policy predicate.
 *
 * Why this file exists: a review mutation disabled the cursor write entirely and
 * ALL 827 Android tests still passed. The invariant that matters most — "an
 * event whose notification was suppressed must still advance the cursor" — had
 * zero coverage. Without it a suppressed event is re-fetched forever, which is
 * an infinite re-notify loop.
 *
 * Two rules are pinned here:
 * 1. Suppressed (replayed) events still advance the cursor.
 * 2. Events the server never persists (task_update running) do NOT advance it —
 *    an unresolvable cursor makes GetPendingEvents return an empty list, which
 *    would strand any completion that arrived while offline.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class NativeCursorAdvancementTest {

    private static final String PREFS = "clawbench_prefs";
    private static final String KEY_LAST_SEEN = "last_seen_event_id";

    private BackgroundService service;
    private Context appContext;

    @Before
    public void setUp() throws Exception {
        service = Robolectric.buildService(BackgroundService.class).get();
        appContext = RuntimeEnvironment.getApplication();
        appContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().clear().commit();

        setStaticField("instance", service);
        setStaticField("isRunning", true);
        setStaticField("nativeWsNeeded", false);
        setStaticField("lastError", null);
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
        appContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().clear().commit();
    }

    private String readCursor() {
        return appContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                .getString(KEY_LAST_SEEN, null);
    }

    // ── Rule 1: suppressed events must still advance the cursor ──────────────

    @Test
    public void liveCompletionAdvancesCursor() throws Exception {
        invokeOnMessage(sessionEvent("evt_live", "completed", false, false));
        assertEquals("a live completion must advance the cursor", "evt_live", readCursor());
    }

    /**
     * The core invariant. A replayed event is not notified, but the client has
     * now seen it — if the cursor did not move, the next reconnect would fetch
     * and re-suppress it indefinitely.
     */
    @Test
    public void replayedCompletionStillAdvancesCursor() throws Exception {
        invokeOnMessage(sessionEvent("evt_replayed", "completed", true, false));
        assertEquals("a replayed event must still advance the cursor",
                "evt_replayed", readCursor());
    }

    /** Same for an event suppressed by the server's read gate. */
    @Test
    public void readSuppressedCompletionStillAdvancesCursor() throws Exception {
        invokeOnMessage(sessionEvent("evt_read", "completed", false, true));
        assertEquals("a read-suppressed event must still advance the cursor",
                "evt_read", readCursor());
    }

    @Test
    public void permissionPendingAdvancesCursor() throws Exception {
        invokeOnMessage(sessionEvent("evt_perm", "permission_pending", false, false));
        assertEquals("permission_pending is persisted and must advance the cursor",
                "evt_perm", readCursor());
    }

    @Test
    public void taskCompletionAdvancesCursor() throws Exception {
        invokeOnMessage(taskEvent("evt_task", "completed"));
        assertEquals("a task completion must advance the cursor", "evt_task", readCursor());
    }

    // ── Rule 2: non-persisted events must NOT advance the cursor ─────────────

    /**
     * `task_update running` is notified ("task started") but the server never
     * writes it to pending_events (service.IsNotifiableEvent excludes running).
     * Advancing the cursor to it would leave the client holding an id the server
     * cannot resolve; GetPendingEvents then returns an EMPTY list for the unknown
     * cursor, so a completion that landed while offline becomes unreachable.
     */
    @Test
    public void taskRunningDoesNotAdvanceCursor() throws Exception {
        invokeOnMessage(taskEvent("evt_running", "running"));
        assertNull("task_update running is never persisted — the cursor must not move to it",
                readCursor());
    }

    @Test
    public void nonTerminalSessionStatusDoesNotAdvanceCursor() throws Exception {
        invokeOnMessage(sessionEvent("evt_running_s", "running", false, false));
        assertNull("a non-terminal session status must not advance the cursor", readCursor());
    }

    /** A non-persisted event must not clobber an already-good cursor. */
    @Test
    public void taskRunningDoesNotClobberExistingCursor() throws Exception {
        invokeOnMessage(taskEvent("evt_good", "completed"));
        assertEquals("evt_good", readCursor());

        invokeOnMessage(taskEvent("evt_running2", "running"));
        assertEquals("a later non-persisted event must not overwrite a valid cursor",
                "evt_good", readCursor());
    }

    // ── Helpers ──────────────────────────────────────────────────────────────

    private String sessionEvent(String id, String status, boolean replayed, boolean suppress) {
        return "{\"type\":\"event\",\"id\":\"" + id + "\",\"event\":\"session_update\""
                + (replayed ? ",\"replayed\":true" : "")
                + (suppress ? ",\"suppress_notification\":true" : "")
                + ",\"data\":{\"session_id\":\"s1\",\"status\":\"" + status + "\"}}";
    }

    private String taskEvent(String id, String status) {
        return "{\"type\":\"event\",\"id\":\"" + id + "\",\"event\":\"task_update\","
                + "\"data\":{\"task_id\":\"1\",\"execution_id\":\"5\",\"status\":\"" + status + "\"}}";
    }

    /** Build the private NativeEventListener and drive its real onMessage. */
    private void invokeOnMessage(String text) throws Exception {
        Class<?> listenerClazz = Class.forName("com.clawbench.app.BackgroundService$NativeEventListener");
        Constructor<?> ctor = listenerClazz.getDeclaredConstructor(BackgroundService.class);
        ctor.setAccessible(true);
        Object listener = ctor.newInstance(service);

        WebSocket ws = mock(WebSocket.class);
        Method m = listenerClazz.getDeclaredMethod("onMessage", WebSocket.class, String.class);
        m.setAccessible(true);
        m.invoke(listener, ws, text);
    }

    private void setStaticField(String name, Object value) throws Exception {
        Field f = BackgroundService.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(null, value);
    }
}
