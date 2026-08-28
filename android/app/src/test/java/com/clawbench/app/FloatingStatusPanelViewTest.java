package com.clawbench.app;

import android.animation.ObjectAnimator;
import android.view.View;
import android.view.ViewGroup;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

import java.util.ArrayList;
import java.util.List;

import static org.junit.Assert.*;

/**
 * Unit tests for FloatingStatusPanelView.
 *
 * buildGroups / statusDotKind are pure functions (org.json + plain model
 * fields, no Android framework dependency) so they are unit-testable with
 * plain JUnit. The content-height measurement (measureContentHeight /
 * constrainListHeight) drives the panel's height-follows-content sizing, so
 * those run under Robolectric against the real View measure machinery.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28)
public class FloatingStatusPanelViewTest {

    private static final int WIDTH_PX = 280; // density 1.0 under Robolectric

    // =====================================================
    // buildGroups: pure overview -> model builder
    // =====================================================

    @Test
    public void buildGroups_groupsByProject() throws Exception {
        String json = "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]},{\"name\":\"/projB\",\"sessions\":[{\"id\":\"s2\",\"title\":\"t2\",\"running\":false,\"pendingApproval\":true,\"unreadCount\":3}]}],\"total\":2}";
        List<FloatingStatusPanelView.ProjectGroup> groups =
                FloatingStatusPanelView.buildGroups(new JSONObject(json));

        assertEquals(2, groups.size());

        FloatingStatusPanelView.ProjectGroup groupA = groups.get(0);
        assertEquals("/projA", groupA.name);
        assertEquals(1, groupA.sessions.size());
        FloatingStatusPanelView.SessionItem a = groupA.sessions.get(0);
        assertEquals("s1", a.id);
        assertTrue(a.running);
        assertFalse(a.pendingApproval);
        assertEquals(0, a.unreadCount);

        FloatingStatusPanelView.ProjectGroup groupB = groups.get(1);
        assertEquals("/projB", groupB.name);
        assertEquals(1, groupB.sessions.size());
        FloatingStatusPanelView.SessionItem b = groupB.sessions.get(0);
        assertEquals("s2", b.id);
        assertFalse(b.running);
        assertTrue(b.pendingApproval);
        assertEquals(3, b.unreadCount);
    }

    @Test
    public void buildGroups_emptyOverview_returnsEmpty() throws Exception {
        String json = "{\"projects\":[],\"total\":0}";
        List<FloatingStatusPanelView.ProjectGroup> groups =
                FloatingStatusPanelView.buildGroups(new JSONObject(json));
        assertNotNull(groups);
        assertTrue(groups.isEmpty());
    }

    @Test
    public void buildGroups_nullOverview_returnsEmpty() {
        List<FloatingStatusPanelView.ProjectGroup> groups =
                FloatingStatusPanelView.buildGroups(null);
        assertNotNull(groups);
        assertTrue(groups.isEmpty());
    }

    @Test
    public void buildGroups_missingProjectsKey_returnsEmpty() throws Exception {
        String json = "{\"total\":0}";
        List<FloatingStatusPanelView.ProjectGroup> groups =
                FloatingStatusPanelView.buildGroups(new JSONObject(json));
        assertNotNull(groups);
        assertTrue(groups.isEmpty());
    }

    @Test
    public void buildGroups_sessionNoRunning_preservesFields() throws Exception {
        String json = "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s7\",\"title\":\"静默会话\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}]}],\"total\":1}";
        List<FloatingStatusPanelView.ProjectGroup> groups =
                FloatingStatusPanelView.buildGroups(new JSONObject(json));

        assertEquals(1, groups.size());
        List<FloatingStatusPanelView.SessionItem> sessions = groups.get(0).sessions;
        assertEquals(1, sessions.size());
        FloatingStatusPanelView.SessionItem item = sessions.get(0);
        assertEquals("s7", item.id);
        assertEquals("静默会话", item.title);
        assertFalse(item.running);
        assertFalse(item.pendingApproval);
        assertEquals(2, item.unreadCount);
    }

    @Test
    public void buildGroups_nullElements_areSkipped() throws Exception {
        // null project and null session entries must be skipped, not crash.
        String json = "{\"projects\":[null,{\"name\":\"/projA\",\"sessions\":[null,{\"id\":\"s5\",\"title\":\"t5\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":2}";
        List<FloatingStatusPanelView.ProjectGroup> groups =
                FloatingStatusPanelView.buildGroups(new JSONObject(json));

        assertEquals(1, groups.size());
        assertEquals("/projA", groups.get(0).name);
        assertEquals(1, groups.get(0).sessions.size());
        assertEquals("s5", groups.get(0).sessions.get(0).id);
    }

    @Test
    public void buildGroups_missingOptionalFields_defaults() throws Exception {
        // running / pendingApproval / unreadCount omitted -> sensible defaults.
        String json = "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s9\",\"title\":\"t9\"}]}],\"total\":1}";
        List<FloatingStatusPanelView.ProjectGroup> groups =
                FloatingStatusPanelView.buildGroups(new JSONObject(json));

        FloatingStatusPanelView.SessionItem item = groups.get(0).sessions.get(0);
        assertFalse(item.running);
        assertFalse(item.pendingApproval);
        assertEquals(0, item.unreadCount);
    }

    // =====================================================
    // statusDotKind: pure status-dot decision (Task 3)
    // =====================================================

    private FloatingStatusPanelView.SessionItem item(boolean running,
                                                     boolean pendingApproval,
                                                     int unreadCount) {
        return new FloatingStatusPanelView.SessionItem(
                "s", "t", running, pendingApproval, unreadCount);
    }

    @Test
    public void statusDotKind_pendingWinsOverRunning() {
        assertEquals(FloatingStatusPanelView.StatusDotKind.PENDING,
                FloatingStatusPanelView.statusDotKind(item(true, true, 0)));
    }

    @Test
    public void statusDotKind_pendingWithUnread_isPending() {
        // Pending (yellow) wins over unread (blue).
        assertEquals(FloatingStatusPanelView.StatusDotKind.PENDING,
                FloatingStatusPanelView.statusDotKind(item(false, true, 5)));
    }

    @Test
    public void statusDotKind_runningWinsOverUnread() {
        // Running (green) wins over unread (blue).
        assertEquals(FloatingStatusPanelView.StatusDotKind.RUNNING,
                FloatingStatusPanelView.statusDotKind(item(true, false, 2)));
    }

    @Test
    public void statusDotKind_runningOnly_isRunning() {
        assertEquals(FloatingStatusPanelView.StatusDotKind.RUNNING,
                FloatingStatusPanelView.statusDotKind(item(true, false, 0)));
    }

    @Test
    public void statusDotKind_idleWithUnread_isUnread() {
        assertEquals(FloatingStatusPanelView.StatusDotKind.UNREAD,
                FloatingStatusPanelView.statusDotKind(item(false, false, 3)));
    }

    @Test
    public void statusDotKind_idleNoUnread_isNone() {
        assertEquals(FloatingStatusPanelView.StatusDotKind.NONE,
                FloatingStatusPanelView.statusDotKind(item(false, false, 0)));
    }

    // =====================================================
    // Content-height measurement (Robolectric)
    // =====================================================

    private static String overviewWith(int sessionCount) {
        StringBuilder sb = new StringBuilder("{\"projects\":[{\"name\":\"/projA\",\"sessions\":[");
        for (int i = 0; i < sessionCount; i++) {
            if (i > 0) {
                sb.append(",");
            }
            sb.append("{\"id\":\"s").append(i).append("\",\"title\":\"t").append(i)
                    .append("\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}");
        }
        sb.append("]}]}");
        return sb.toString();
    }

    private static FloatingStatusPanelView newPanel() throws Exception {
        return new FloatingStatusPanelView(RuntimeEnvironment.getApplication(), null);
    }

    @Test
    public void measureContentHeight_growsWithSessionCount() throws Exception {
        FloatingStatusPanelView panel = newPanel();
        panel.render(new JSONObject(overviewWith(1)), null);
        int one = panel.measureContentHeight(WIDTH_PX);
        panel.render(new JSONObject(overviewWith(20)), null);
        int many = panel.measureContentHeight(WIDTH_PX);
        assertTrue("more sessions must measure taller, one=" + one + " many=" + many,
                many > one);
    }

    @Test
    public void constrainListHeight_shrinksPanelToTarget() throws Exception {
        FloatingStatusPanelView panel = newPanel();
        panel.render(new JSONObject(overviewWith(20)), null);
        panel.measureContentHeight(WIDTH_PX); // lay out so the header height is known

        panel.constrainListHeight(100);
        int measured = panel.measureContentHeight(WIDTH_PX);
        // measureContentHeight measures CONTENT, temporarily restoring the
        // scroll area to WRAP_CONTENT, so the report stays at the true content
        // height even after the scroll area was capped. constrainListHeight
        // separately caps the scroll area for the final window size.
        assertTrue("content measure must exceed the 100px cap (it reports real content, "
                + "one=" + measured + ")", measured > 100);
        assertTrue("content must stay under the unconstrained content height",
                measured <= panel.measureContentHeight(WIDTH_PX));
    }

    @Test
    public void constrainListHeight_capsScrollViewHeight() throws Exception {
        FloatingStatusPanelView panel = newPanel();
        panel.render(new JSONObject(overviewWith(20)), null);
        panel.measureContentHeight(WIDTH_PX);

        panel.constrainListHeight(100);
        ScrollView sv = (ScrollView) getField(panel, "scrollView");
        LinearLayout.LayoutParams lp = (LinearLayout.LayoutParams) sv.getLayoutParams();
        assertTrue("scroll area must fit within the constrained panel, got " + lp.height,
                lp.height > 0 && lp.height <= 100);
    }

    @Test
    public void measureContentHeight_afterConstrain_stillReportsContent() throws Exception {
        // Regression: a previous constrainListHeight set a fixed scroll-area
        // height; measureContentHeight must still see the session rows, not a
        // height clamped by the old (possibly 0) scroll area.
        FloatingStatusPanelView panel = newPanel();
        panel.render(new JSONObject(overviewWith(1)), null);
        int unconstrained = panel.measureContentHeight(WIDTH_PX);

        panel.constrainListHeight(40); // very small cap
        int afterConstrain = panel.measureContentHeight(WIDTH_PX);

        assertTrue("content height must survive a prior constraint, unconstrained="
                        + unconstrained + " after=" + afterConstrain,
                afterConstrain == unconstrained);
    }

    // =====================================================
    // Title bar: shared capsule content row + stats
    // =====================================================

    private FloatingStatusContentView headerContent(FloatingStatusPanelView panel) throws Exception {
        LinearLayout header = (LinearLayout) getField(panel, "headerLayout");
        return (FloatingStatusContentView) header.getChildAt(0);
    }

    /** The running dot inside the header's shared content row. */
    private View headerRunningDot(FloatingStatusPanelView panel) throws Exception {
        FloatingStatusContentView content = headerContent(panel);
        LinearLayout runningItem = (LinearLayout) content.getChildAt(1);
        return runningItem.getChildAt(0);
    }

    private ObjectAnimator headerBreathAnimator(FloatingStatusPanelView panel) throws Exception {
        return (ObjectAnimator) getField(headerContent(panel), "breathAnim");
    }

    @Test
    public void renderHeaderStats_withRunning_startsBreathing() throws Exception {
        FloatingStatusPanelView panel = newPanel();
        panel.renderHeaderStats(2, 0, 0);

        ObjectAnimator anim = headerBreathAnimator(panel);
        assertTrue("header running dot must breathe while sessions run", anim.isRunning());
        assertEquals("header breathing must loop forever", ObjectAnimator.INFINITE,
                org.robolectric.Shadows.shadowOf(anim).getActualRepeatCount());
        panel.stopBreathing();
    }

    @Test
    public void renderHeaderStats_zeroRunning_stopsBreathingAndRestoresAlpha() throws Exception {
        FloatingStatusPanelView panel = newPanel();
        panel.renderHeaderStats(1, 0, 0);
        assertTrue(headerBreathAnimator(panel).isRunning());

        panel.renderHeaderStats(0, 0, 0);

        assertFalse("header breathing must stop when the running count drops to 0",
                headerBreathAnimator(panel).isRunning());
        assertEquals("the header running dot must return to full opacity", 1.0f,
                headerRunningDot(panel).getAlpha(), 0.001f);
        panel.stopBreathing();
    }

    @Test
    public void render_paintsTitleBarStatsFromOverview() throws Exception {
        // The title bar reuses the capsule content row: render() must feed it
        // the same mutually-exclusive stats as the capsule (pending wins over
        // running; unread only counts idle sessions). No separate
        // "N 个会话运行中" header text remains.
        FloatingStatusPanelView panel = newPanel();
        String json = "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                + "{\"id\":\"r\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0},"
                + "{\"id\":\"p\",\"running\":false,\"pendingApproval\":true,\"unreadCount\":0},"
                + "{\"id\":\"b\",\"running\":true,\"pendingApproval\":true,\"unreadCount\":0},"
                + "{\"id\":\"u\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":3}"
                + "]}]}";
        panel.render(new JSONObject(json), null);

        List<String> texts = collectAllTexts(panel);
        assertTrue("title bar must show the running count, got: " + texts,
                texts.contains("执行中 1"));
        assertTrue("title bar must show the pending count, got: " + texts,
                texts.contains("待审批 2"));
        assertTrue("title bar must show the unread count, got: " + texts,
                texts.contains("未读 1"));
        panel.stopBreathing();
    }

    @Test
    public void render_unreadSession_noNumericBadge_keepsBlueDot() throws Exception {
        // The unread session row must NOT carry a red numeric badge (the blue
        // unread dot is the only unread signal) — regression for the badge
        // removal. The blue dot must still be present.
        FloatingStatusPanelView panel = newPanel();
        String json = "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                + "{\"id\":\"u\",\"title\":\"未读会话\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":7}"
                + "]}]}";
        panel.render(new JSONObject(json), null);

        List<String> texts = collectAllTexts(panel);
        assertTrue("session title must be present", texts.contains("未读会话"));
        assertFalse("unread count must NOT appear as a numeric badge, got: " + texts,
                texts.contains("7"));
        ViewGroup list = (ViewGroup) getField(panel, "listContainer");
        assertTrue("the blue unread status dot must still be drawn in the session row",
                sessionRowHasStatusDot(list));
        panel.stopBreathing();
    }

    /** True if a session row's first child is a plain View with an OVAL background (the status dot). */
    private boolean sessionRowHasStatusDot(ViewGroup list) {
        for (int i = 0; i < list.getChildCount(); i++) {
            View child = list.getChildAt(i);
            if (!(child instanceof LinearLayout)) {
                continue; // skip project headers
            }
            LinearLayout row = (LinearLayout) child;
            if (row.getChildCount() == 0) {
                continue;
            }
            View first = row.getChildAt(0);
            if (first instanceof TextView) {
                return false; // no dot — a bare TextView as first child means no status dot
            }
            if (first.getBackground() instanceof android.graphics.drawable.GradientDrawable) {
                return true;
            }
        }
        return false;
    }

    @Test
    public void headerContent_staysStableAcrossRenders() throws Exception {
        // The title bar's content row is built once at construction; render()
        // only rebuilds the session list, so the header keeps its own state
        // (and breathing animation) across refreshes.
        FloatingStatusPanelView panel = newPanel();
        FloatingStatusContentView first = headerContent(panel);

        panel.render(new JSONObject(overviewWith(1)), null);
        panel.render(new JSONObject(overviewWith(3)), null);

        assertEquals("header content row must not be rebuilt on render",
                first, headerContent(panel));
        panel.stopBreathing();
    }

    // =====================================================
    // Skeleton loading placeholder
    // =====================================================

    @Test
    public void showSkeleton_fillsListUntilRender() throws Exception {
        FloatingStatusPanelView panel = newPanel();

        panel.showSkeleton();

        ViewGroup list = (ViewGroup) getField(panel, "listContainer");
        assertTrue("skeleton must populate the list area, got " + list.getChildCount(),
                list.getChildCount() > 0);
        assertTrue("skeleton must be reported as showing", panel.isSkeletonShowing());

        // Real data replaces the skeleton rows.
        panel.render(new JSONObject(overviewWith(2)), null);
        assertFalse("render must clear the skeleton", panel.isSkeletonShowing());
        assertTrue("real session rows must be present",
                list.getChildCount() >= 2);
        panel.stopBreathing();
    }

    @Test
    public void showSkeleton_emptyOverview_stillClears() throws Exception {
        // A no-session overview is still real content: the placeholder must
        // not linger after a load that returned zero sessions.
        FloatingStatusPanelView panel = newPanel();
        panel.showSkeleton();

        panel.render(new JSONObject("{\"projects\":[],\"total\":0}"), null);

        assertFalse("empty overview must clear the skeleton", panel.isSkeletonShowing());
        assertEquals("empty overview renders an empty list", 0,
                ((ViewGroup) getField(panel, "listContainer")).getChildCount());
        panel.stopBreathing();
    }

    @Test
    public void showSkeleton_hideSkeleton_clearsImmediately() throws Exception {
        FloatingStatusPanelView panel = newPanel();
        panel.showSkeleton();
        assertTrue(panel.isSkeletonShowing());

        panel.hideSkeleton();

        assertFalse("hideSkeleton must clear the placeholder", panel.isSkeletonShowing());
        assertEquals("list must be empty after hideSkeleton", 0,
                ((ViewGroup) getField(panel, "listContainer")).getChildCount());
    }

    @Test
    public void showSkeleton_stopBreathing_restoresFullOpacity() throws Exception {
        FloatingStatusPanelView panel = newPanel();
        panel.showSkeleton();

        ViewGroup list = (ViewGroup) getField(panel, "listContainer");
        panel.stopBreathing();

        assertEquals("skeleton container must return to full opacity",
                1.0f, list.getAlpha(), 0.001f);
    }

    @Test
    public void mixArgb_blendsTowardBackground() {
        // textSecondary (#8B949E) mixed 50% toward the dark bg (#161B22) must
        // land between the two (a faint gray placeholder).
        int mixed = FloatingStatusPanelView.mixArgb(0xFF8B949E, 0xFF161B22, 0.5f);
        int r = (mixed >> 16) & 0xFF;
        int g = (mixed >> 8) & 0xFF;
        int b = mixed & 0xFF;
        assertTrue("red must blend between inputs", r >= 0x16 && r <= 0x8B);
        assertTrue("green must blend between inputs", g >= 0x1B && g <= 0x94);
        assertTrue("blue must blend between inputs", b >= 0x22 && b <= 0x9E);
        // The placeholder must be visibly fainter than real text color.
        assertTrue("skeleton gray must be dimmer than the text color",
                (r + g + b) < (0x8B + 0x94 + 0x9E));
    }

    // --- helpers ---

    private static Object getField(Object target, String name) throws Exception {
        java.lang.reflect.Field f = target.getClass().getDeclaredField(name);
        f.setAccessible(true);
        return f.get(target);
    }

    /** All visible TextView texts in the hierarchy, in order. */
    private List<String> collectAllTexts(ViewGroup root) {
        List<String> out = new ArrayList<>();
        for (TextView tv : collectVisibleTextViews(root, 0)) {
            String text = tv.getText().toString();
            if (!text.isEmpty()) {
                out.add(text);
            }
        }
        return out;
    }

    private List<TextView> collectVisibleTextViews(ViewGroup root, int depth) {
        List<TextView> out = new ArrayList<>();
        if (depth > 6) {
            return out;
        }
        for (int i = 0; i < root.getChildCount(); i++) {
            View child = root.getChildAt(i);
            if (child instanceof TextView
                    && child.getVisibility() == View.VISIBLE
                    && isVisibleInHierarchy(child)) {
                out.add((TextView) child);
            }
            if (child instanceof ViewGroup) {
                out.addAll(collectVisibleTextViews((ViewGroup) child, depth + 1));
            }
        }
        return out;
    }

    /** A GONE parent hides its children even when their own flag is VISIBLE. */
    private static boolean isVisibleInHierarchy(View v) {
        View current = v;
        while (current != null) {
            if (current.getVisibility() != View.VISIBLE) {
                return false;
            }
            if (!(current.getParent() instanceof View)) {
                break;
            }
            current = (View) current.getParent();
        }
        return true;
    }
}
