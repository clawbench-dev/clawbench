package com.clawbench.app;

import android.widget.LinearLayout;
import android.widget.ScrollView;

import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;

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
        assertEquals("a constrained panel must measure to the target height", 100, measured);
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

    // --- helpers ---

    private static Object getField(Object target, String name) throws Exception {
        java.lang.reflect.Field f = target.getClass().getDeclaredField(name);
        f.setAccessible(true);
        return f.get(target);
    }
}
