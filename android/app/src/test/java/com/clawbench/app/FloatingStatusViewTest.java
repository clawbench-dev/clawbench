package com.clawbench.app;

import android.animation.ObjectAnimator;
import android.view.View;
import android.view.ViewGroup;
import android.widget.LinearLayout;

import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.Shadows;
import org.robolectric.annotation.Config;
import org.robolectric.shadows.ShadowValueAnimator;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for FloatingStatusView's stats capsule.
 *
 * The pure static counting functions (countRunning / countPending /
 * countUnread) derive the three mutually-exclusive capsule counts from a
 * /api/ai/sessions/overview JSON object. They only depend on org.json (plain
 * JUnit, no Robolectric needed).
 *
 * The breathing-animation lifecycle tests (renderStats starts/stops the alpha
 * loop on the running dot) need a view, so they run under Robolectric.
 *
 * Grouping priority matches the panel's status dot (yellow pending > green
 * running): a session with both running and pendingApproval is counted as
 * pending. Unread only counts sessions that are neither running nor pending.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
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

    // =====================================================
    // Breathing animation: renderStats drives the alpha loop
    // =====================================================

    private FloatingStatusView newCapsule() {
        return new FloatingStatusView(RuntimeEnvironment.getApplication());
    }

    /** The running dot: first child of the running item (dot, then label). */
    private View runningDot(FloatingStatusView capsule) {
        ViewGroup row = (ViewGroup) capsule.getChildAt(0);
        LinearLayout runningItem = (LinearLayout) row.getChildAt(1);
        return runningItem.getChildAt(0);
    }

    private ObjectAnimator breathAnimator(FloatingStatusView capsule) throws Exception {
        return (ObjectAnimator) getField(capsule, "breathAnim");
    }

    private Object getField(Object target, String name) throws Exception {
        java.lang.reflect.Field f = target.getClass().getDeclaredField(name);
        f.setAccessible(true);
        return f.get(target);
    }

    @Test
    public void renderStats_withRunning_startsBreathingAnimation() throws Exception {
        FloatingStatusView capsule = newCapsule();
        // renderStats(0,0,0) ran at construction; a running count must start
        // the loop.
        capsule.renderStats(1, 0, 0);

        ObjectAnimator anim = breathAnimator(capsule);
        assertTrue("breathing must be running while a session runs",
                anim.isRunning());
        // Robolectric's ShadowValueAnimator maps INFINITE (-1) to 1 on the real
        // animator, so the infinite-ness must be read back from the shadow.
        assertEquals("breathing must loop forever", ObjectAnimator.INFINITE,
                Shadows.shadowOf(anim).getActualRepeatCount());
        assertEquals("breathing must oscillate", ObjectAnimator.REVERSE,
                anim.getRepeatMode());
        assertEquals("breathing must animate alpha", "alpha",
                ((android.animation.PropertyValuesHolder)
                        anim.getValues()[0]).getPropertyName());
        capsule.stopBreathing();
    }

    @Test
    public void renderStats_noRunning_stopsBreathingAndRestoresAlpha() throws Exception {
        FloatingStatusView capsule = newCapsule();
        capsule.renderStats(1, 0, 0);
        assertTrue(breathAnimator(capsule).isRunning());

        capsule.renderStats(0, 0, 0);

        assertFalse("breathing must stop when the running count drops to 0",
                breathAnimator(capsule).isRunning());
        assertEquals("the running dot must return to full opacity", 1.0f,
                runningDot(capsule).getAlpha(), 0.001f);
        capsule.stopBreathing();
    }

    @Test
    public void renderStats_runningTwice_doesNotStackAnimations() throws Exception {
        // A fresh start on an already-running animator must not restart it.
        FloatingStatusView capsule = newCapsule();
        capsule.renderStats(1, 0, 0);
        ObjectAnimator anim = breathAnimator(capsule);
        anim.start();
        assertTrue(anim.isRunning());

        capsule.renderStats(2, 0, 0);

        assertEquals("re-render with running > 0 must not restart the animator",
                anim, breathAnimator(capsule));
        assertTrue("animator must still be running", anim.isRunning());
        capsule.stopBreathing();
    }

    @Test
    public void renderStats_pendingOrUnreadOnly_doesNotBreathe() throws Exception {
        FloatingStatusView capsule = newCapsule();
        capsule.renderStats(0, 1, 0);
        assertFalse("a pending-only capsule must not breathe",
                breathAnimator(capsule).isRunning());

        capsule.renderStats(0, 0, 1);
        assertFalse("an unread-only capsule must not breathe",
                breathAnimator(capsule).isRunning());

        assertEquals("the running dot must stay fully opaque", 1.0f,
                runningDot(capsule).getAlpha(), 0.001f);
        capsule.stopBreathing();
    }

    @Test
    public void stopBreathing_restoresAlpha() throws Exception {
        FloatingStatusView capsule = newCapsule();
        capsule.renderStats(1, 0, 0);
        assertTrue(breathAnimator(capsule).isRunning());

        capsule.stopBreathing();

        assertFalse("stopBreathing must cancel the running animator",
                breathAnimator(capsule).isRunning());
        assertEquals("stopBreathing must restore full opacity", 1.0f,
                runningDot(capsule).getAlpha(), 0.001f);
    }
}
