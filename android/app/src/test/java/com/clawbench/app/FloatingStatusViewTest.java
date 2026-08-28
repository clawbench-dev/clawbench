package com.clawbench.app;

import org.json.JSONObject;
import org.junit.Test;

import static org.junit.Assert.*;

/**
 * Unit tests for FloatingStatusView's stats capsule counting functions.
 *
 * countRunning / countPending / countUnread are pure static functions that
 * derive the three mutually-exclusive capsule counts from a
 * /api/ai/sessions/overview JSON object. They only depend on org.json (plain
 * JUnit, no Robolectric needed).
 *
 * Grouping priority matches the panel's status dot (yellow pending > green
 * running): a session with both running and pendingApproval is counted as
 * pending. Unread only counts sessions that are neither running nor pending.
 */
public class FloatingStatusViewTest {

    private static JSONObject overview(String json) throws Exception {
        return new JSONObject(json);
    }

    /** Overview with one session per combination of flags. */
    private static final String MIXED = "{"
            + "\"projects\":[{"
            + "\"name\":\"/projA\",\"sessions\":["
            + "{\"id\":\"s-running\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0},"
            + "{\"id\":\"s-pending\",\"running\":false,\"pendingApproval\":true,\"unreadCount\":0},"
            + "{\"id\":\"s-both\",\"running\":true,\"pendingApproval\":true,\"unreadCount\":0},"
            + "{\"id\":\"s-unread\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":3},"
            + "{\"id\":\"s-idle\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":0}"
            + "]}]}";

    // --- countRunning: running && !pendingApproval ---

    @Test
    public void countRunning_onlyPureRunningSessions() throws Exception {
        assertEquals(1, FloatingStatusView.countRunning(overview(MIXED)));
    }

    @Test
    public void countRunning_excludesPendingEvenWhenAlsoRunning() throws Exception {
        // s-both has running=true: without the pending-exclusion it would be
        // counted as running; pendingApproval must win (yellow > green).
        JSONObject overview = overview(
                "{\"projects\":[{\"name\":\"/p\",\"sessions\":["
                        + "{\"id\":\"a\",\"running\":true,\"pendingApproval\":true,\"unreadCount\":0}"
                        + "]}]}");
        assertEquals(0, FloatingStatusView.countRunning(overview));
    }

    // --- countPending: pendingApproval wins over running ---

    @Test
    public void countPending_includesPendingOnlyAndBothFlagSessions() throws Exception {
        assertEquals(2, FloatingStatusView.countPending(overview(MIXED)));
    }

    // --- countUnread: neither running nor pending, unreadCount > 0 ---

    @Test
    public void countUnread_onlyIdleSessionsWithUnread() throws Exception {
        assertEquals(1, FloatingStatusView.countUnread(overview(MIXED)));
    }

    @Test
    public void countUnread_excludesRunningAndPendingSessions() throws Exception {
        // An unread session that is also running or pending must not count as
        // unread (groups must not overlap).
        JSONObject overview = overview(
                "{\"projects\":[{\"name\":\"/p\",\"sessions\":["
                        + "{\"id\":\"r\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":5},"
                        + "{\"id\":\"p\",\"running\":false,\"pendingApproval\":true,\"unreadCount\":5},"
                        + "{\"id\":\"u\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}"
                        + "]}]}");
        assertEquals(1, FloatingStatusView.countUnread(overview));
    }

    @Test
    public void countUnread_zeroUnreadCount_notCounted() throws Exception {
        JSONObject overview = overview(
                "{\"projects\":[{\"name\":\"/p\",\"sessions\":["
                        + "{\"id\":\"a\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":0}"
                        + "]}]}");
        assertEquals(0, FloatingStatusView.countUnread(overview));
    }

    // --- grouping: the three counts partition the session set ---

    @Test
    public void mixedGroupsAreMutuallyExclusive() throws Exception {
        JSONObject overview = overview(MIXED);
        int running = FloatingStatusView.countRunning(overview);
        int pending = FloatingStatusView.countPending(overview);
        int unread = FloatingStatusView.countUnread(overview);
        // 5 sessions total: 1 running, 2 pending (incl. both), 1 unread, 1 idle.
        assertEquals(1, running);
        assertEquals(2, pending);
        assertEquals(1, unread);
        // Each session lands in exactly one group: running+pending+unread + 1 idle.
        assertEquals(4, running + pending + unread);
    }

    @Test
    public void emptyOverview_returnsZeroCounts() throws Exception {
        JSONObject overview = overview("{\"projects\":[],\"total\":0}");
        assertEquals(0, FloatingStatusView.countRunning(overview));
        assertEquals(0, FloatingStatusView.countPending(overview));
        assertEquals(0, FloatingStatusView.countUnread(overview));
    }

    // --- defensive parsing ---

    @Test
    public void nullOverview_returnsZeroCounts() {
        assertEquals(0, FloatingStatusView.countRunning(null));
        assertEquals(0, FloatingStatusView.countPending(null));
        assertEquals(0, FloatingStatusView.countUnread(null));
    }

    @Test
    public void malformedOverview_returnsZeroCounts() throws Exception {
        JSONObject overview = overview("{\"projects\":[{\"name\":\"/p\",\"sessions\":[null]}]}");
        assertEquals(0, FloatingStatusView.countRunning(overview));
        assertEquals(0, FloatingStatusView.countPending(overview));
        assertEquals(0, FloatingStatusView.countUnread(overview));
    }

    @Test
    public void multiProjectOverview_countsAcrossAllProjects() throws Exception {
        JSONObject overview = overview(
                "{\"projects\":["
                        + "{\"name\":\"/p1\",\"sessions\":["
                        + "{\"id\":\"a\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]},"
                        + "{\"name\":\"/p2\",\"sessions\":["
                        + "{\"id\":\"b\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0},"
                        + "{\"id\":\"c\",\"running\":false,\"pendingApproval\":true,\"unreadCount\":0}]}"
                        + "]}");
        assertEquals(2, FloatingStatusView.countRunning(overview));
        assertEquals(1, FloatingStatusView.countPending(overview));
        assertEquals(0, FloatingStatusView.countUnread(overview));
    }
}
