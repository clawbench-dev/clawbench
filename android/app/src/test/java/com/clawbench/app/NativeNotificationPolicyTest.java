package com.clawbench.app;

import org.junit.Test;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for the native notification policy.
 *
 * These cover the three reported Android notification defects:
 *
 * 1. Already-read replies re-notified on recovery — the server now marks them
 *    with suppress_notification, and the policy must honor it.
 * 2. A batch of historical completions re-notified after the native channel
 *    reconnects — the server tags replayed messages with replayed:true, and the
 *    policy must suppress them.
 * 3. The same backlog re-notified on a fresh client — an empty cursor makes the
 *    server return its whole TTL-bounded backlog, so the policy must notify none
 *    of it.
 *
 * The tests are pure (no Android runtime) because the logic was extracted into
 * NativeNotificationPolicy precisely so it can be exercised here.
 */
public class NativeNotificationPolicyTest {

    // ── Baseline: only terminal states notify ────────────────────────────────

    @Test
    public void terminalSessionStatusesAreNotifiable() {
        assertTrue(NativeNotificationPolicy.isNotifiableSessionStatus("completed"));
        assertTrue(NativeNotificationPolicy.isNotifiableSessionStatus("cancelled"));
        assertTrue(NativeNotificationPolicy.isNotifiableSessionStatus("permission_pending"));
    }

    @Test
    public void nonTerminalSessionStatusesAreNot() {
        assertFalse(NativeNotificationPolicy.isNotifiableSessionStatus("running"));
        assertFalse(NativeNotificationPolicy.isNotifiableSessionStatus("read"));
        assertFalse(NativeNotificationPolicy.isNotifiableSessionStatus(""));
    }

    @Test
    public void terminalTaskStatusesAreNotifiable() {
        assertTrue(NativeNotificationPolicy.isNotifiableTaskStatus("running"));
        assertTrue(NativeNotificationPolicy.isNotifiableTaskStatus("completed"));
        assertTrue(NativeNotificationPolicy.isNotifiableTaskStatus("failed"));
        assertTrue(NativeNotificationPolicy.isNotifiableTaskStatus("cancelled"));
    }

    @Test
    public void unknownEventTypesAreNotNotifiable() {
        assertFalse(NativeNotificationPolicy.isNotifiableEvent("summary_update", "completed"));
        assertFalse(NativeNotificationPolicy.isNotifiableEvent("chat_stream", "completed"));
        assertFalse(NativeNotificationPolicy.isNotifiableEvent("", "completed"));
    }

    // ── Live WS path ─────────────────────────────────────────────────────────

    @Test
    public void liveCompletionNotifies() {
        assertTrue(NativeNotificationPolicy.shouldNotifyLive(
                "session_update", "completed", false, false));
    }

    /** Defect 2: a replayed (caught-up) completion must not notify. */
    @Test
    public void replayedCompletionDoesNotNotify() {
        assertFalse("replayed events are caught-up history, not live completions",
                NativeNotificationPolicy.shouldNotifyLive("session_update", "completed", true, false));
    }

    /** Defect 1: a completion the user already read must not notify. */
    @Test
    public void alreadyReadCompletionDoesNotNotify() {
        assertFalse("an already-read completion must not re-notify",
                NativeNotificationPolicy.shouldNotifyLive("session_update", "completed", false, true));
    }

    @Test
    public void replayedAndReadAlsoDoesNotNotify() {
        assertFalse(NativeNotificationPolicy.shouldNotifyLive(
                "session_update", "completed", true, true));
    }

    /**
     * permission_pending is exempt from BOTH suppression flags.
     *
     * It is an approval request still blocking the session, not a reply to read:
     * the server's read gate exempts it, and a replayed one is at most ~10s old
     * (the reconnect buffer window) so it is almost certainly still pending.
     * Dropping the prompt would block the session with no signal to the user;
     * a duplicate prompt only costs a tap. The asymmetry decides it.
     */
    @Test
    public void permissionPendingAlwaysNotifies() {
        assertTrue("approval prompt must survive the read gate",
                NativeNotificationPolicy.shouldNotifyLive(
                        "session_update", "permission_pending", false, true));
        assertTrue("a ~10s-old replayed approval prompt is still pending",
                NativeNotificationPolicy.shouldNotifyLive(
                        "session_update", "permission_pending", true, false));
        assertTrue("and with no flags at all",
                NativeNotificationPolicy.shouldNotifyLive(
                        "session_update", "permission_pending", false, false));
    }

    @Test
    public void permissionPendingNotifiesFromBacklogWithCursor() {
        assertTrue(NativeNotificationPolicy.shouldNotifyFromBacklog(
                "evt_123", "session_update", "permission_pending", false));
    }

    /**
     * A fresh client suppresses the whole backlog — including permission_pending.
     *
     * This mirrors the web client exactly (useGlobalEvents.fetchPendingEvents
     * advances the cursor and returns on an empty cursor). It is safe because the
     * app's normal foreground load syncs session state directly, so a genuinely
     * pending approval surfaces in the UI. Notifying instead would flood the user
     * with up to 7 days of prompts, most long since answered — the reported bug.
     */
    @Test
    public void freshClientSuppressesPermissionPendingBacklogToo() {
        assertFalse(NativeNotificationPolicy.shouldNotifyFromBacklog(
                "", "session_update", "permission_pending", false));
        assertFalse(NativeNotificationPolicy.shouldNotifyFromBacklog(
                null, "session_update", "permission_pending", false));
    }

    @Test
    public void nonTerminalLiveEventDoesNotNotify() {
        assertFalse(NativeNotificationPolicy.shouldNotifyLive(
                "session_update", "running", false, false));
    }

    // ── HTTP pending-event recovery path ─────────────────────────────────────

    /** Defect 3: an empty cursor means the response is the whole backlog. */
    @Test
    public void emptyCursorSuppressesWholeBacklog() {
        assertTrue(NativeNotificationPolicy.isFreshBacklog(""));
        assertTrue(NativeNotificationPolicy.isFreshBacklog(null));
        assertFalse("a non-empty cursor means the fetch is a genuine catch-up",
                NativeNotificationPolicy.isFreshBacklog("evt_123"));

        assertFalse("a fresh client must not notify the 24h backlog",
                NativeNotificationPolicy.shouldNotifyFromBacklog(
                        "", "session_update", "completed", false));
    }

    /** With a cursor the fetch really is "what I missed" — notify it. */
    @Test
    public void withCursorMissedCompletionNotifies() {
        assertTrue(NativeNotificationPolicy.shouldNotifyFromBacklog(
                "evt_123", "session_update", "completed", false));
    }

    /** The read gate applies on the HTTP path too. */
    @Test
    public void withCursorAlreadyReadCompletionDoesNotNotify() {
        assertFalse(NativeNotificationPolicy.shouldNotifyFromBacklog(
                "evt_123", "session_update", "completed", true));
    }

    @Test
    public void withCursorNonTerminalDoesNotNotify() {
        assertFalse(NativeNotificationPolicy.shouldNotifyFromBacklog(
                "evt_123", "session_update", "running", false));
    }

    /**
     * Cursor advancement is a SEPARATE question from notification, and the two
     * deliberately differ for `task_update running`: it notifies ("task started")
     * but is never persisted server-side, so it must not move the cursor.
     *
     * The behavioural proof that a caller actually advances the cursor lives in
     * NativeCursorAdvancementTest (this predicate alone cannot detect a caller
     * that stops writing it — a review mutation did exactly that and the whole
     * suite stayed green).
     */
    @Test
    public void cursorPredicateMirrorsServerPersistence() {
        // Persisted → may advance.
        assertTrue(NativeNotificationPolicy.advancesCursor("session_update", "completed"));
        assertTrue(NativeNotificationPolicy.advancesCursor("session_update", "cancelled"));
        assertTrue(NativeNotificationPolicy.advancesCursor("session_update", "permission_pending"));
        assertTrue(NativeNotificationPolicy.advancesCursor("task_update", "completed"));
        assertTrue(NativeNotificationPolicy.advancesCursor("task_update", "failed"));
        assertTrue(NativeNotificationPolicy.advancesCursor("task_update", "cancelled"));

        // NOT persisted → must not advance, or the cursor becomes unresolvable
        // and GetPendingEvents returns empty forever.
        assertFalse("task_update running is notified but never stored",
                NativeNotificationPolicy.advancesCursor("task_update", "running"));
        assertFalse(NativeNotificationPolicy.advancesCursor("session_update", "running"));
        assertFalse(NativeNotificationPolicy.advancesCursor("summary_update", "completed"));
    }

    /** The notify predicate and the cursor predicate must actually differ here. */
    @Test
    public void taskRunningNotifiesButDoesNotAdvanceCursor() {
        assertTrue("task started should notify",
                NativeNotificationPolicy.isNotifiableEvent("task_update", "running"));
        assertFalse("...but must not advance the cursor",
                NativeNotificationPolicy.advancesCursor("task_update", "running"));
    }

    @Test
    public void taskRunningNotifiesOnLivePath() {
        assertTrue(NativeNotificationPolicy.shouldNotifyLive(
                "task_update", "running", false, false));
    }

    @Test
    public void taskFailedNotifiesOnLivePath() {
        assertTrue(NativeNotificationPolicy.shouldNotifyLive(
                "task_update", "failed", false, false));
    }
}
