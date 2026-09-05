package com.clawbench.app;

import android.content.Context;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for LiveUpdateManager.
 *
 * The pure decision functions (computeStats / chipText / cardSummary /
 * shouldRefresh) have no Android framework dependency and are tested with
 * plain JUnit. computeStats delegates to FloatingStatusController.computeStats,
 * which is already covered by FloatingStatusControllerTest; here we assert the
 * Live Update wrapper returns the same three-tuple for representative
 * overviews. chipText / cardSummary / shouldRefresh are the Live Update
 * specific rendering + throttle logic.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28, qualifiers = "zh")
public class LiveUpdateManagerTest {

    private static JSONObject overview(String... sessionJson) throws Exception {
        StringBuilder sb = new StringBuilder("{\"projects\":[{\"name\":\"/p\",\"sessions\":[");
        for (int i = 0; i < sessionJson.length; i++) {
            if (i > 0) sb.append(',');
            sb.append(sessionJson[i]);
        }
        sb.append("]}],\"total\":").append(sessionJson.length).append('}');
        return new JSONObject(sb.toString());
    }

    private static String session(String id, boolean running, boolean pending, int unread) {
        return "{\"id\":\"" + id + "\",\"running\":" + running
                + ",\"pendingApproval\":" + pending + ",\"unreadCount\":" + unread + "}";
    }

    // =====================================================
    // computeStats: overview -> {running, pending, unread}
    // =====================================================

    @Test
    public void computeStats_runningAndUnread() throws Exception {
        JSONObject o = overview(
                session("s1", true, false, 0),
                session("s2", false, false, 2));
        int[] stats = LiveUpdateManager.computeStats(o);
        assertEquals(1, stats[0]); // running
        assertEquals(0, stats[1]); // pending
        assertEquals(1, stats[2]); // one session with unread messages
    }

    @Test
    public void computeStats_pendingWinsOverRunning() throws Exception {
        // A session that is both running and pending counts as pending only.
        JSONObject o = overview(session("s1", true, true, 0));
        int[] stats = LiveUpdateManager.computeStats(o);
        assertEquals(0, stats[0]);
        assertEquals(1, stats[1]);
        assertEquals(0, stats[2]);
    }

    @Test
    public void computeStats_emptyOverview() throws Exception {
        int[] stats = LiveUpdateManager.computeStats(new JSONObject("{\"projects\":[],\"total\":0}"));
        assertEquals(0, stats[0]);
        assertEquals(0, stats[1]);
        assertEquals(0, stats[2]);
    }

    @Test
    public void computeStats_nullSafe() {
        int[] stats = LiveUpdateManager.computeStats(null);
        assertEquals(0, stats[0]);
        assertEquals(0, stats[1]);
        assertEquals(0, stats[2]);
    }

    // =====================================================
    // chipText: single-line status-bar summary
    // =====================================================

    private static final String L_RUNNING = "执行中";
    private static final String L_PENDING = "待审批";
    private static final String L_UNREAD = "未读";
    private static final String L_JOINER = " · ";

    @Test
    public void chipText_pendingFirst() {
        assertEquals("🟡 待审批 1", LiveUpdateManager.chipText(2, 1, 3,
                L_RUNNING, L_PENDING, L_UNREAD));
    }

    @Test
    public void chipText_unreadOutranksRunning() {
        // Unread finished sessions outrank running ones on the chip.
        assertEquals("🔵 未读 3", LiveUpdateManager.chipText(2, 0, 3,
                L_RUNNING, L_PENDING, L_UNREAD));
    }

    @Test
    public void chipText_runningWhenNoPendingOrUnread() {
        assertEquals("🟢 执行中 2", LiveUpdateManager.chipText(2, 0, 0,
                L_RUNNING, L_PENDING, L_UNREAD));
    }

    @Test
    public void chipText_emptyWhenNothing() {
        // The chip is removed entirely before chipText is consulted when every
        // count is 0, so the function returns "" (never rendered).
        assertEquals("", LiveUpdateManager.chipText(0, 0, 0,
                L_RUNNING, L_PENDING, L_UNREAD));
        assertEquals("", LiveUpdateManager.chipText(0, 0, 0,
                "Running", "Pending", "Unread"));
    }

    @Test
    public void chipText_usesInjectedLabels() {
        assertEquals("🟢 Running 2", LiveUpdateManager.chipText(2, 0, 0,
                "Running", "Pending", "Unread"));
        assertEquals("🟡 Pending 1", LiveUpdateManager.chipText(0, 1, 0,
                "Running", "Pending", "Unread"));
        assertEquals("🔵 Unread 3", LiveUpdateManager.chipText(0, 0, 3,
                "Running", "Pending", "Unread"));
    }

    // =====================================================
    // cardSummary: expanded-card breakdown (omits the chip's group)
    // =====================================================

    @Test
    public void cardSummary_omitsChipGroup_allThreeNonZero() {
        // Chip shows pending (most urgent) → card shows running + unread only.
        assertEquals("🟢 执行中 2 · 🔵 未读 3",
                LiveUpdateManager.cardSummary(2, 1, 3,
                        L_RUNNING, L_PENDING, L_UNREAD, L_JOINER));
    }

    @Test
    public void cardSummary_omitsChipGroup_runningChip() {
        // Chip shows running → card shows pending + unread.
        assertEquals("🟡 待审批 0 · 🔵 未读 0",
                LiveUpdateManager.cardSummary(1, 0, 0,
                        L_RUNNING, L_PENDING, L_UNREAD, L_JOINER));
    }

    @Test
    public void cardSummary_omitsChipGroup_pendingChip() {
        // Chip shows pending → card shows running + unread.
        assertEquals("🟢 执行中 0 · 🔵 未读 0",
                LiveUpdateManager.cardSummary(0, 2, 0,
                        L_RUNNING, L_PENDING, L_UNREAD, L_JOINER));
    }

    @Test
    public void cardSummary_omitsChipGroup_unreadChip() {
        // Chip shows unread → card shows running + pending.
        assertEquals("🟢 执行中 0 · 🟡 待审批 0",
                LiveUpdateManager.cardSummary(0, 0, 5,
                        L_RUNNING, L_PENDING, L_UNREAD, L_JOINER));
    }

    @Test
    public void cardSummary_allZeroCounts() {
        // Never rendered (the chip is removed first); with no chip group the
        // function still produces a deterministic three-group line.
        assertEquals("🟢 执行中 0 · 🟡 待审批 0 · 🔵 未读 0",
                LiveUpdateManager.cardSummary(0, 0, 0,
                        L_RUNNING, L_PENDING, L_UNREAD, L_JOINER));
    }

    @Test
    public void cardSummary_usesInjectedLabelsAndJoiner() {
        // Chip shows unread (outranks running) → card shows running + pending.
        assertEquals("🟢 Running 2 | 🟡 Pending 0",
                LiveUpdateManager.cardSummary(2, 0, 3,
                        "Running", "Pending", "Unread", " | "));
    }

    @Test
    public void chipGroup_priorityOrder() {
        // Most urgent first: pending approval > unread > running.
        assertEquals("pending", LiveUpdateManager.chipGroup(2, 1, 3));
        assertEquals("unread", LiveUpdateManager.chipGroup(2, 0, 3));
        assertEquals("unread", LiveUpdateManager.chipGroup(0, 0, 3));
        assertEquals("running", LiveUpdateManager.chipGroup(2, 0, 0));
        assertEquals("", LiveUpdateManager.chipGroup(0, 0, 0));
    }

    // =====================================================
    // shouldRefresh: throttle + empty-workspace semantics
    // =====================================================

    @Test
    public void shouldRefresh_emptyWorkspace_alwaysImmediate() {
        assertTrue(LiveUpdateManager.shouldRefresh(0, 0, 0, true, 1000, 100));
    }

    @Test
    public void shouldRefresh_notVisible_immediate() {
        assertTrue(LiveUpdateManager.shouldRefresh(1, 0, 0, false, 1000, 100));
    }

    @Test
    public void shouldRefresh_visibleAfterThrottleWindow_immediate() {
        long now = 1000;
        long last = now - LiveUpdateManager.THROTTLE_MS; // exactly at the edge
        assertTrue(LiveUpdateManager.shouldRefresh(1, 0, 0, true, now, last));
    }

    @Test
    public void shouldRefresh_visibleWithinWindow_throttled() {
        assertFalse(LiveUpdateManager.shouldRefresh(1, 0, 0, true, 1000, 950));
    }

    // =====================================================
    // canPostPromoted: promotion availability gate
    // =====================================================

    @Test
    public void canPostPromoted_belowApi36_false() {
        // @Config(sdk=28) → Build.VERSION.SDK_INT < 36, so promotion is
        // always unavailable regardless of the NotificationManager state.
        assertFalse("below API 36 promotion must be unavailable",
                LiveUpdateManager.canPostPromoted(RuntimeEnvironment.getApplication()));
    }

    @Test
    public void canPostPromoted_nullContext_false() {
        assertFalse("null context must not throw and must report unavailable",
                LiveUpdateManager.canPostPromoted(null));
    }

    // =====================================================
    // onEvent: overview reconciliation requests (unread source)
    // =====================================================

    @Test
    public void onEvent_requestsOverview_whenThrottleWindowElapsed() throws Exception {
        LiveUpdateManager manager = new LiveUpdateManager(RuntimeEnvironment.getApplication());
        final int[] calls = {0};
        manager.setOverviewRequestListener(() -> calls[0]++);

        // First event → requests overview immediately.
        manager.onEvent("session_update", "running", "s1");
        assertEquals("first event must request a fresh overview", 1, calls[0]);

        // Second event within THROTTLE_MS → throttled, no second request.
        manager.onEvent("session_update", "completed", "s1");
        assertEquals("events within the throttle window must not re-request", 1, calls[0]);
    }

    @Test
    public void onEvent_ignoresNonSessionEvents_andNullSession() throws Exception {
        LiveUpdateManager manager = new LiveUpdateManager(RuntimeEnvironment.getApplication());
        final int[] calls = {0};
        manager.setOverviewRequestListener(() -> calls[0]++);

        manager.onEvent("task_update", "completed", "t1");
        manager.onEvent("session_update", "completed", "");
        assertEquals("non-session events and empty session ids must not request an overview", 0, calls[0]);
    }

    @Test
    public void onOverviewLoaded_updatesCountsAndPostsNotification() throws Exception {
        Context context = RuntimeEnvironment.getApplication();
        LiveUpdateManager manager = new LiveUpdateManager(context);
        assertFalse(manager.isVisible());

        // 1. null overview is safe
        manager.onOverviewLoaded(null);
        assertEquals(0, manager.getRunningCount());
        assertEquals(0, manager.getPendingCount());
        assertEquals(0, manager.getUnreadCount());

        // 2. overview with active session posts notification
        JSONObject o = overview(
                session("s1", true, false, 0),
                session("s2", false, true, 0),
                session("s3", false, false, 2));
        manager.onOverviewLoaded(o);
        org.robolectric.shadows.ShadowLooper.idleMainLooper();

        assertEquals(1, manager.getRunningCount());
        assertEquals(1, manager.getPendingCount());
        assertEquals(1, manager.getUnreadCount());
        assertTrue(manager.isVisible());

        // 3. overview with zero counts cancels notification
        JSONObject empty = new JSONObject("{\"projects\":[],\"total\":0}");
        manager.onOverviewLoaded(empty);
        org.robolectric.shadows.ShadowLooper.idleMainLooper();

        assertEquals(0, manager.getRunningCount());
        assertEquals(0, manager.getPendingCount());
        assertEquals(0, manager.getUnreadCount());
        assertFalse(manager.isVisible());

        // 4. destroy cleans up
        manager.destroy();
        org.robolectric.shadows.ShadowLooper.idleMainLooper();
        assertFalse(manager.isVisible());
    }

    @Test
    public void onEvent_handlesAllLifecycleStatuses() throws Exception {
        Context context = RuntimeEnvironment.getApplication();
        LiveUpdateManager manager = new LiveUpdateManager(context);

        // permission_pending
        manager.onEvent("session_update", "permission_pending", "s1");
        // permission_resolved
        manager.onEvent("session_update", "permission_resolved", "s1");
        // completed
        manager.onEvent("session_update", "completed", "s1");
        // cancelled
        manager.onEvent("session_update", "cancelled", "s1");
        // failed
        manager.onEvent("session_update", "failed", "s1");

        org.robolectric.shadows.ShadowLooper.idleMainLooper();
    }

    @Test
    public void openPromotedSettings_handlesContextSafely() {
        // Null context safe
        LiveUpdateManager.openPromotedSettings(null);

        // Real Robolectric context safe fallback
        Context context = RuntimeEnvironment.getApplication();
        LiveUpdateManager.openPromotedSettings(context);
    }
}
