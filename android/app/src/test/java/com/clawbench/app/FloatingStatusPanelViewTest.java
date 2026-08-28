package com.clawbench.app;

import org.json.JSONObject;
import org.junit.Test;

import java.util.List;

import static org.junit.Assert.*;

/**
 * Unit tests for FloatingStatusPanelView's pure buildGroups model builder.
 *
 * buildGroups parses the /api/ai/sessions/overview JSON response into grouped
 * project/session models. It has no Android framework dependency (uses
 * org.json + plain lists), so it is unit-testable with plain JUnit.
 */
public class FloatingStatusPanelViewTest {

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
}
