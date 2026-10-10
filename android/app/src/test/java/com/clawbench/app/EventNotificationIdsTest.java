package com.clawbench.app;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotEquals;

/**
 * Unit tests for the shared event-notification id formula.
 *
 * These pin the EXACT integer values the POST path writes, because the CANCEL
 * path must recompute the same id or the cancel silently no-ops (the reported
 * stale notification). The values are literal — NOT re-derived from the same
 * constants — so a coordinated change to the formula (or to BASE/BAND) is
 * caught here instead of passing tautologically. The formula was extracted
 * verbatim from BackgroundService; drift here would make the two paths
 * disagree with no compile error.
 */
public class EventNotificationIdsTest {

    @Test
    public void sessionIdUsesSessionBand() {
        // 3 + |"session-abc".hashCode() % 1000|
        assertEquals(16, EventNotificationIds.forSession("session-abc"));
        assertEquals(617, EventNotificationIds.forSession("s1"));
    }

    @Test
    public void taskIdUsesTaskBand() {
        // 3 + 1000 + |"task-abc".hashCode() % 1000|
        assertEquals(1153, EventNotificationIds.forTask("task-abc"));
        assertEquals(1058, EventNotificationIds.forTask("7"));
    }

    /** The exact values the dismiss end-to-end test relies on. */
    @Test
    public void knownSubjectsResolveToStableIds() {
        assertEquals(599, EventNotificationIds.forSession("s-read-1"));
        assertEquals(64, EventNotificationIds.forSession("s-bridge"));
        assertEquals(1599, EventNotificationIds.forTask("s-read-1"));
    }

    /** Session and task bands must not overlap — the +1000 offset guarantees it. */
    @Test
    public void sessionAndTaskBandsAreDisjoint() {
        for (int i = 0; i < 2000; i++) {
            String id = "id-" + i;
            int session = EventNotificationIds.forSession(id);
            int task = EventNotificationIds.forTask(id);
            assertNotEquals("session and task ids must never collide for the same subject id",
                    session, task);
            org.junit.Assert.assertTrue("session id stays below the task band",
                    session < EventNotificationIds.BASE + EventNotificationIds.BAND);
            org.junit.Assert.assertTrue("task id starts at the task band",
                    task >= EventNotificationIds.BASE + EventNotificationIds.BAND);
        }
    }

    /** The event entry point prefers the task id, matching the post branch order. */
    @Test
    public void forEventPrefersTaskId() {
        assertEquals(EventNotificationIds.forTask("t1"),
                EventNotificationIds.forEvent("t1", "s1"));
    }

    @Test
    public void forEventFallsBackToSession() {
        assertEquals(EventNotificationIds.forSession("s1"),
                EventNotificationIds.forEvent("", "s1"));
        assertEquals(EventNotificationIds.forSession("s1"),
                EventNotificationIds.forEvent(null, "s1"));
    }

    @Test
    public void blankIdsFallBackToBase() {
        assertEquals(EventNotificationIds.BASE, EventNotificationIds.forSession(""));
        assertEquals(EventNotificationIds.BASE, EventNotificationIds.forSession(null));
        assertEquals(EventNotificationIds.BASE, EventNotificationIds.forTask(""));
        assertEquals(EventNotificationIds.BASE, EventNotificationIds.forEvent(null, null));
    }

    /**
     * Math.abs is applied AFTER the modulo, so the Integer.MIN_VALUE trap
     * (Math.abs(Integer.MIN_VALUE) == Integer.MIN_VALUE) cannot produce a
     * negative id.
     */
    @Test
    public void idsAreAlwaysNonNegative() {
        String[] tricky = {"polygenelubricants", "", "Aa", "BB", "\u0000", "task-x", "session-y"};
        for (String s : tricky) {
            org.junit.Assert.assertTrue(EventNotificationIds.forSession(s) >= EventNotificationIds.BASE);
            org.junit.Assert.assertTrue(EventNotificationIds.forTask(s) >= EventNotificationIds.BASE);
        }
    }

    /** The base constant must stay at the historical value (ids in the shade). */
    @Test
    public void baseMatchesServiceConstant() {
        assertEquals(3, EventNotificationIds.BASE);
    }
}
