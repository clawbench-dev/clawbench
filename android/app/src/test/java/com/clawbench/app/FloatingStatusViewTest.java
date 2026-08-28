package com.clawbench.app;

import android.animation.ObjectAnimator;
import android.graphics.drawable.GradientDrawable;
import android.view.View;
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

    /** The shared content row embedded in the capsule. */
    private FloatingStatusContentView content(FloatingStatusView capsule) {
        return (FloatingStatusContentView) capsule.getChildAt(0);
    }

    /** The running dot: first child of the running item (dot, then label). */
    private View runningDot(FloatingStatusView capsule) {
        FloatingStatusContentView row = content(capsule);
        LinearLayout runningItem = (LinearLayout) row.getChildAt(1);
        return runningItem.getChildAt(0);
    }

    private ObjectAnimator breathAnimator(FloatingStatusView capsule) throws Exception {
        return (ObjectAnimator) getField(content(capsule), "breathAnim");
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

    // =====================================================
    // Capsule sizing: compact 38dp pill (7dp padding + 24dp logo)
    // =====================================================

    private static int constantInt(String name) throws Exception {
        java.lang.reflect.Field f = FloatingStatusView.class.getDeclaredField(name);
        f.setAccessible(true);
        return f.getInt(null);
    }

    private static int contentConstantInt(String name) throws Exception {
        java.lang.reflect.Field f = FloatingStatusContentView.class.getDeclaredField(name);
        f.setAccessible(true);
        return f.getInt(null);
    }

    @Test
    public void sizeConstants_areCompact() throws Exception {
        // The capsule is a compact 38dp pill: 7dp vertical padding around a
        // 24dp logo. Constants specific to the content row (logo, dots, text)
        // live in FloatingStatusContentView, shared with the panel title bar.
        assertEquals(7, constantInt("PADDING_V_DP"));
        assertEquals(14, constantInt("PADDING_H_DP"));
        assertEquals(8, constantInt("PADDING_H_START_DP"));
        assertEquals(24, contentConstantInt("LOGO_SIZE_DP"));
        assertEquals(12, contentConstantInt("DOT_SIZE_DP"));
        assertEquals(6, contentConstantInt("DOT_MARGIN_END_DP"));
        assertEquals(14, contentConstantInt("TEXT_SIZE_SP"));
        assertEquals(10, contentConstantInt("LOGO_MARGIN_END_DP"));
    }

    @Test
    public void cornerRadius_isHalfTheCapsuleHeight() throws Exception {
        // The capsule renders as a pill: CORNER_RADIUS 19dp equals half the
        // 38dp target height (7dp vertical padding + 24dp logo), so both
        // ends are full semicircles rather than rounded-rectangle corners.
        assertEquals(19, constantInt("CORNER_RADIUS_DP"));
        assertEquals("logo height + 2 * vertical padding",
                38, 2 * constantInt("PADDING_V_DP") + contentConstantInt("LOGO_SIZE_DP"));
        assertEquals("corner radius must be half the 38dp height",
                38 / 2, constantInt("CORNER_RADIUS_DP"));
    }

    @Test
    public void background_borderFromBackgroundLuminance() throws Exception {
        // The capsule gets a 1dp border derived from the background color via a
        // luminance nudge (lighter on dark themes, darker on light themes) so it
        // is visible while staying in the background's hue family. The stroke
        // getters are API 29+ on the real class, so read them through
        // Robolectric's shadow.
        FloatingStatusView capsule = newCapsule();
        GradientDrawable bg = (GradientDrawable) capsule.getBackground();
        org.robolectric.shadows.ShadowGradientDrawable shadow =
                Shadows.shadowOf(bg);
        assertEquals("border must be 1dp at density 1.0", 1,
                shadow.getStrokeWidth());
        int bgColor = FloatingThemeColors.get(RuntimeEnvironment.getApplication())[0];
        assertEquals("border must be the background-luminance-derived color",
                FloatingThemeColors.borderColorFromBackground(bgColor),
                shadow.getStrokeColor());
        capsule.stopBreathing();
    }

    @Test
    public void capsuleMeasuredHeight_meetsTarget() throws Exception {
        // Target ≈ 38dp (7dp vertical padding + 24dp logo, i.e. the pill
        // height whose half drives the full-semicircle corner radius).
        // Robolectric's default font metrics inflate the 14sp text line
        // height well above on-device values (~19dp), so it measures ~45px
        // here while a real device renders ~38dp. The lower bound is the real
        // guard against a "stingy" capsule; the upper bound catches gross
        // regressions. Robolectric runs at density=1, so dp == px here.
        FloatingStatusView capsule = newCapsule();
        capsule.renderStats(1, 1, 1);

        int widthSpec = View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED);
        int heightSpec = View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED);
        capsule.measure(widthSpec, heightSpec);

        int height = capsule.getMeasuredHeight();
        assertTrue("capsule height " + height + "px must be >= 38dp (target)",
                height >= 38);
        assertTrue("capsule height " + height + "px must be <= 60dp (Robolectric inflates text)",
                height <= 60);
        capsule.stopBreathing();
    }
}
