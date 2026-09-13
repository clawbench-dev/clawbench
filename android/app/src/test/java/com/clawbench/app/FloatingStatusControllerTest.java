package com.clawbench.app;

import android.content.Context;
import android.os.Looper;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.widget.ScrollView;
import android.widget.TextView;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;
import org.robolectric.shadows.ShadowLooper;

import java.util.ArrayList;
import java.util.List;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for FloatingStatusController.
 *
 * The pure static decision functions (isActiveStatus / shouldShow / snapX)
 * have no Android framework dependencies and are tested with plain JUnit.
 * The destroy() lifecycle behavior (window teardown + post-destroy event
 * drop) requires a real main looper + WindowManager, so those run under
 * Robolectric.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 28, qualifiers = "zh")
public class FloatingStatusControllerTest {

    private static final String TAG = "FloatingStatusCtrl";

    // --- isActiveStatus ---

    @Test
    public void isActiveStatus_sessionRunning_true() {
        assertTrue(FloatingStatusController.isActiveStatus("session_update", "running"));
    }

    @Test
    public void isActiveStatus_sessionPermissionPending_true() {
        assertTrue(FloatingStatusController.isActiveStatus("session_update", "permission_pending"));
    }

    @Test
    public void isActiveStatus_sessionCompleted_false() {
        assertFalse(FloatingStatusController.isActiveStatus("session_update", "completed"));
    }

    @Test
    public void isActiveStatus_sessionCancelled_false() {
        assertFalse(FloatingStatusController.isActiveStatus("session_update", "cancelled"));
    }

    @Test
    public void isActiveStatus_sessionFailed_false() {
        assertFalse(FloatingStatusController.isActiveStatus("session_update", "failed"));
    }

    @Test
    public void isActiveStatus_taskRunning_true() {
        assertTrue(FloatingStatusController.isActiveStatus("task_update", "running"));
    }

    @Test
    public void isActiveStatus_taskCompleted_false() {
        assertFalse(FloatingStatusController.isActiveStatus("task_update", "completed"));
    }

    @Test
    public void isActiveStatus_taskFailed_false() {
        assertFalse(FloatingStatusController.isActiveStatus("task_update", "failed"));
    }

    @Test
    public void isActiveStatus_unknownEvent_false() {
        assertFalse(FloatingStatusController.isActiveStatus("unknown_event", "running"));
        assertFalse(FloatingStatusController.isActiveStatus("", ""));
    }

    // --- shouldShow ---

    @Test
    public void shouldShow_backgroundActiveNotDismissed_true() {
        assertTrue(FloatingStatusController.shouldShow(false, true, false, false));
    }

    @Test
    public void shouldShow_backgroundUnreadNotDismissed_true() {
        // Unread sessions are worth showing even with nothing active.
        assertTrue(FloatingStatusController.shouldShow(false, false, true, false));
    }

    @Test
    public void shouldShow_backgroundIdle_false() {
        // Nothing active and nothing unread: an idle window has nothing worth
        // showing, so it must not be displayed.
        assertFalse("idle (no active, no unread) must hide the window",
                FloatingStatusController.shouldShow(false, false, false, false));
    }

    @Test
    public void shouldShow_foreground_false() {
        assertFalse(FloatingStatusController.shouldShow(true, true, false, false));
        assertFalse(FloatingStatusController.shouldShow(true, false, false, false));
    }

    @Test
    public void shouldShow_userDismissed_false() {
        assertFalse(FloatingStatusController.shouldShow(false, true, false, true));
        assertFalse("dismissal must win over unread too",
                FloatingStatusController.shouldShow(false, false, true, true));
    }

    @Test
    public void shouldShow_foregroundNoActiveDismissed_false() {
        assertFalse(FloatingStatusController.shouldShow(true, false, true, true));
    }

    // --- snapX ---

    @Test
    public void snapX_rightEdge_accountsForViewWidth() {
        assertEquals(300 - 120 - 8, FloatingStatusController.snapX(300, 120, 8, true));
    }

    @Test
    public void snapX_leftEdge_returnsMargin() {
        assertEquals(8, FloatingStatusController.snapX(300, 120, 8, false));
    }

    @Test
    public void snapX_rightEdge_wideViewClampsToMargin() {
        // View wider than screen - margin would push the left edge negative;
        // clamp keeps it on-screen at the margin.
        assertEquals(8, FloatingStatusController.snapX(100, 200, 8, true));
    }

    @Test
    public void snapX_rightEdge_exactlyFits() {
        assertEquals(8, FloatingStatusController.snapX(136, 120, 8, true));
    }

    // --- destroy() lifecycle (Robolectric) ---

    private FloatingStatusController newController() {
        Context ctx = RuntimeEnvironment.getApplication();
        return new FloatingStatusController(ctx);
    }

    @Test
    public void destroy_cleansUpAndDropsQueuedEvents() throws Exception {
        FloatingStatusController controller = newController();
        ShadowSettings.setCanDrawOverlays(true);

        org.json.JSONObject data = new org.json.JSONObject();
        data.put("status", "running");
        controller.handleEvent("session_update", data);
        // Run the posted handleEvent so the window appears.
        ShadowLooper.runUiThreadTasksIncludingDelayedTasks();
        assertTrue("active event should show the floating window",
                controller.isWindowShowing());

        controller.destroy();
        // Destroy runs on the current (main) thread and removes the view.
        assertFalse("destroy() must remove the floating window",
                controller.isWindowShowing());

        // A handleEvent posted while destroyed must be dropped at the guard —
        // it must not resurrect the window.
        data.put("status", "running");
        controller.handleEvent("session_update", data);
        ShadowLooper.runUiThreadTasksIncludingDelayedTasks();
        assertFalse("event after destroy() must not resurrect the window",
                controller.isWindowShowing());
    }

    @Test
    public void postToUi_droppedAfterDestroy_evenWhenPostedFromWorkerThread() throws Exception {
        // Regression: a runnable queued on the main thread BEFORE destroy()
        // executes AFTER destroy()'s synchronous cleanup, and must not rebuild
        // the window (zombie floating window race).
        FloatingStatusController controller = newController();
        ShadowSettings.setCanDrawOverlays(true);
        assertTrue("Looper must be the main looper for this scenario",
                Looper.myLooper() == Looper.getMainLooper());

        org.json.JSONObject data = new org.json.JSONObject();
        data.put("status", "running");

        // Simulate the native-WS thread posting an event just before the
        // service is torn down: the runnable lands on the main handler, then
        // onDestroy (main thread) synchronously destroys the controller.
        final Throwable[] workerError = {null};
        Thread worker = new Thread(() -> {
            try {
                controller.handleEvent("session_update", data);
            } catch (Throwable t) {
                workerError[0] = t;
            }
        });
        worker.start();
        worker.join();
        assertNull("worker thread must not throw", workerError[0]);

        controller.destroy();
        assertFalse(controller.isWindowShowing());

        ShadowLooper.runUiThreadTasksIncludingDelayedTasks();
        assertFalse("pre-destroy queued event must be dropped at execution time",
                controller.isWindowShowing());
    }

    // =====================================================
    // Panel height sizing: height-follows-content (Task 3)
    // =====================================================

    @Test
    public void panelHeightForContent_clampsToScreen() {
        // Content taller than the screen must be capped at the screen height.
        assertEquals(600, FloatingStatusController.panelHeightForContent(800, 600));
    }

    @Test
    public void panelHeightForContent_smallContent_notClamped() {
        assertEquals(200, FloatingStatusController.panelHeightForContent(200, 600));
    }

    @Test
    public void panelHeightForContent_zeroScreen_clamps() {
        // A zero screen height (unlikely) must still yield a finite height.
        assertEquals(0, FloatingStatusController.panelHeightForContent(200, 0));
    }

    @Test
    public void panelHeightForContent_zeroContent_returnsZero() {
        assertEquals(0, FloatingStatusController.panelHeightForContent(0, 600));
    }

    // =====================================================
    // trackSessionState: event-driven running session collection
    // =====================================================

    private org.json.JSONObject sessionEvent(String status, String sessionId) throws Exception {
        org.json.JSONObject data = new org.json.JSONObject();
        data.put("status", status);
        if (sessionId != null) {
            data.put("session_id", sessionId);
        }
        return data;
    }

    /**
     * A "completed" event as the backend actually broadcasts it: the live
     * terminal broadcast passes has_new_messages=false (see
     * internal/handler/chat.go EmitSessionEventWSOnly). A completion still means
     * new assistant output, so the controller must treat it as unread
     * regardless of the flag — tests use this shape to stay honest.
     */
    private org.json.JSONObject completedEvent(String sessionId) throws Exception {
        return sessionEvent("completed", sessionId);
    }

    @Test
    public void handleEvent_runningIncrementsCount() throws Exception {
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        assertEquals("running event must add the session", 1,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void handleEvent_completedDecrementsCount() throws Exception {
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        assertEquals(1, controller.getRunningSessionCount());
        controller.handleEvent("session_update", sessionEvent("completed", "s1"));
        assertEquals("completed event must remove the session", 0,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void handleEvent_runningSameSessionTwice_countsOnce() throws Exception {
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        assertEquals("duplicate running events must not double-count", 1,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void handleEvent_sessionWithoutId_isIgnored() throws Exception {
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", null));
        assertEquals("session without id must not be tracked", 0,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void handleEvent_unknownStatus_leavesCountUntouched() throws Exception {
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("some_other_state", "s1"));
        assertEquals("unrelated status must not change the count", 1,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void handleEvent_cancelledAndFailed_removeSession() throws Exception {
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));
        controller.handleEvent("session_update", sessionEvent("cancelled", "s1"));
        controller.handleEvent("session_update", sessionEvent("failed", "s2"));
        assertEquals("cancelled/failed must remove the sessions", 0,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void destroy_clearsRunningSessions() throws Exception {
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));
        controller.destroy();
        assertEquals("destroy() must clear the running set", 0,
                controller.getRunningSessionCount());
    }

    @Test
    public void trackSessionState_afterDestroy_doesNotReviveSet() throws Exception {
        // Regression: trackSessionState runs synchronously outside postToUi, so
        // a late event (e.g. a WS event arriving after destroy) would re-add to
        // the cleared running set. The destroyed guard must drop it.
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        assertEquals(1, controller.getRunningSessionCount());
        controller.destroy();
        assertEquals(0, controller.getRunningSessionCount());

        controller.trackSessionState("session_update", "running", "s1");
        assertEquals("trackSessionState after destroy must not revive the set", 0,
                controller.getRunningSessionCount());
    }

    @Test
    public void trackSessionState_taskUpdate_doesNotEnterSessionSets() throws Exception {
        // task_update is tracked in its own runningTasks set, never in
        // runningSessions: the overview covers chat sessions only, so a task id
        // in runningSessions would be wiped by the next overview and wrongly
        // hide the window mid-task. Window visibility for a task is covered by
        // taskRunning_keepsWindowUp.
        FloatingStatusController controller = newController();
        controller.trackSessionState("task_update", "running", "t1");
        assertEquals("task_update must not be tracked as a running session", 0,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    // =====================================================
    // handleEvent status="read": unread-only refresh
    // =====================================================

    @Test
    public void handleEvent_readStatus_triggersOverviewRefresh() throws Exception {
        // A "read" event (session marked read from another client) must refresh
        // the overview even while collapsed, so the capsule's unread count
        // updates without waiting for the next panel expand.
        final int[] requests = {0};
        FloatingStatusController controller = newController();
        controller.setOverviewRequestListener(() -> requests[0]++);

        // Not expanded: previously only expanded events triggered a refresh.
        controller.handleEvent("session_update", sessionEvent("read", "s1"));

        assertEquals("read events must trigger an overview refresh even while collapsed",
                1, requests[0]);
        controller.destroy();
    }

    @Test
    public void handleEvent_readStatus_doesNotChangeRunningSet() throws Exception {
        // "read" is a unread-only refresh: it must not add, remove, or otherwise
        // disturb the tracked running/pending session sets.
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        assertEquals(1, controller.getRunningSessionCount());

        controller.handleEvent("session_update", sessionEvent("read", "s1"));

        assertEquals("read must not remove a running session", 1,
                controller.getRunningSessionCount());

        // And it must not add a session that was never running.
        controller.handleEvent("session_update", sessionEvent("read", "s2"));
        assertEquals("read must not add sessions to the running set", 1,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void handleEvent_readStatus_doesNotHideWindow() throws Exception {
        // "read" is not an active status, but it must not hide the window.
        FloatingStatusController controller = newController();
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        controller.handleEvent("session_update", sessionEvent("read", "s1"));
        ShadowLooper.runUiThreadTasks();

        assertTrue("read must not hide the window", controller.isWindowShowing());
        controller.destroy();
    }

    // =====================================================
    // onCapsuleTap: unified expand-panel tap (Task 3)
    // =====================================================

    @Test
    public void onCapsuleTap_singleRunning_expandsPanelNotSession() throws Exception {
        // Capsule tap is now a unified "expand the panel" gesture: even with a
        // single running session it must NOT open the session directly.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        controller.handleEvent("session_update", sessionEvent("running", "s1"));

        controller.onCapsuleTap();

        assertTrue("single running session tap must expand the panel",
                controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void onCapsuleTap_multipleRunning_expandsPanel() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));

        controller.onCapsuleTap();

        assertTrue(controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void onCapsuleTap_unreadOnly_expandsPanel() throws Exception {
        // An unread session (tracked from an overview) is still content worth
        // showing, so the tap must expand the panel.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"u\",\"title\":\"t\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}"
                        + "]}]}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        controller.onCapsuleTap();

        assertTrue("unread content must expand the panel", controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void onCapsuleTap_whenAlreadyExpanded_staysExpanded() throws Exception {
        // A tap while the panel is already expanded keeps it expanded rather
        // than collapsing (the capsule is not attached in that state anyway).
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.setExpanded(true);
        assertTrue(controller.isExpanded());

        controller.onCapsuleTap();

        assertTrue(controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void capsuleTapTouchEvent_expandsPanel() throws Exception {
        // Real touch path: ACTION_UP on the capsule view must route through
        // onCapsuleTap and always expand the panel, never open a session.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));
        controller.setExpanded(false);
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        FloatingStatusView capsule = (FloatingStatusView) getPrivateField(controller, "view");
        assertNotNull(capsule);
        long now = android.os.SystemClock.uptimeMillis();
        android.view.MotionEvent down = android.view.MotionEvent.obtain(now, now,
                android.view.MotionEvent.ACTION_DOWN, 5f, 5f, 0);
        android.view.MotionEvent up = android.view.MotionEvent.obtain(now, now + 50,
                android.view.MotionEvent.ACTION_UP, 5f, 5f, 0);
        capsule.dispatchTouchEvent(down);
        capsule.dispatchTouchEvent(up);
        ShadowLooper.runUiThreadTasks();

        assertTrue("capsule tap must expand the panel", controller.isExpanded());
        controller.destroy();
    }

    // =====================================================
    // setExpanded / collapse: panel visibility lifecycle
    // =====================================================

    @Test
    public void setExpanded_true_showsPanelWindow() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));

        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();

        assertTrue("panel must be visible after expand", controller.isWindowShowing());
        assertTrue(controller.isExpanded());
        FloatingStatusPanelView panelView =
                (FloatingStatusPanelView) getPrivateField(controller, "panelView");
        assertNotNull("panel view must exist when expanded", panelView);
        assertEquals("the attached view must be the panel, not the capsule",
                panelView, getPrivateField(controller, "attachedView"));
        controller.destroy();
    }

    @Test
    public void setExpanded_true_activeEvent_keepsPanelWidthFixed() throws Exception {
        // Regression: another client starting a session while the panel is
        // expanded fired ensureWindow's cancel branch, which reset params.width
        // to WRAP_CONTENT; the next panel resize then measured at full screen
        // width and the panel stretched across the whole screen. The expanded
        // panel width must stay fixed at 280dp.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isExpanded());

        int panelWidthPx = Math.round(280 * density());
        WindowManager.LayoutParams lp = (WindowManager.LayoutParams)
                getPrivateField(controller, "params");
        assertEquals("panel must start at the fixed 280dp width",
                panelWidthPx, lp.width);

        // Another client starts a second session while the panel is open.
        controller.handleEvent("session_update", sessionEvent("running", "s2"));
        ShadowLooper.runUiThreadTasks();

        // A fresh overview with two sessions re-renders the panel.
        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"s1\",\"title\":\"t1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0},"
                        + "{\"id\":\"s2\",\"title\":\"t2\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}"
                        + "]}],\"total\":2}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        lp = (WindowManager.LayoutParams) getPrivateField(controller, "params");
        assertEquals("the panel width must stay fixed after a new session arrives",
                panelWidthPx, lp.width);
        assertEquals("the panel must stay expanded", true, controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void collapse_restoresCapsuleView() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));

        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());
        assertTrue(controller.isExpanded());

        controller.setExpanded(false);
        ShadowLooper.runUiThreadTasks();

        assertFalse("collapse must reset the expanded flag", controller.isExpanded());
        assertTrue("capsule window should stay visible while a session is active",
                controller.isWindowShowing());
        Object capsuleView = getPrivateField(controller, "view");
        assertNotNull("capsule view must be restored on collapse", capsuleView);
        assertEquals("the attached view must be the capsule after collapse",
                capsuleView, getPrivateField(controller, "attachedView"));
        assertNull("panel view must be detached on collapse",
                getPrivateField(controller, "panelView"));
        controller.destroy();
    }

    @Test
    public void collapse_withNoActive_hidesWindow() throws Exception {
        // No active session and no unread content: collapsing the panel must
        // hide the window entirely — an idle window has nothing worth showing.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue("panel can expand even with no running session", controller.isWindowShowing());

        controller.setExpanded(false);
        ShadowLooper.runUiThreadTasks();

        assertFalse(controller.isExpanded());
        assertFalse("collapse with no content must hide the idle window",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void setAppForeground_true_resetsExpanded() throws Exception {
        // Regression: returning to the foreground hid the window but kept the
        // expanded flag, so a later background rebuilt the stale panel instead
        // of the capsule.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isExpanded());

        controller.setAppForeground(true);
        ShadowLooper.runUiThreadTasks();

        assertFalse("foreground must reset the expanded state",
                controller.isExpanded());
        assertFalse("foreground must hide the window", controller.isWindowShowing());
        assertNull("foreground must drop the stale panel view",
                getPrivateField(controller, "panelView"));
        controller.destroy();
    }

    @Test
    public void setAppForeground_false_afterForegroundReset_showsCapsule() throws Exception {
        // After the foreground reset, backgrounding with an active session must
        // bring back the capsule (not a stale panel).
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isExpanded());

        controller.setAppForeground(true);
        ShadowLooper.runUiThreadTasks();
        assertFalse(controller.isExpanded());

        controller.setAppForeground(false);
        ShadowLooper.runUiThreadTasks();

        assertTrue("background with an active session must show the window again",
                controller.isWindowShowing());
        Object capsuleView = getPrivateField(controller, "view");
        assertNotNull(capsuleView);
        assertEquals("the re-shown window must be the capsule, not a stale panel",
                capsuleView, getPrivateField(controller, "attachedView"));
        controller.destroy();
    }

    // =====================================================
    // setExpanded panel positioning: the panel must stay on-screen
    // =====================================================

    @Test
    public void setExpanded_true_clampsPanelXWithinScreen() throws Exception {
        // Regression: the capsule default sits at the right edge (x = width -
        // capsuleWidth - margin); attaching the wider 280dp panel there pushed
        // it off-screen. After expand, x must be re-clamped to the panel width.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();

        WindowManager.LayoutParams lp = (WindowManager.LayoutParams)
                getPrivateField(controller, "params");
        assertNotNull(lp);
        int screenWidth = contextWidthPx();
        int panelWidthPx = Math.round(280 * density());
        int marginPx = Math.round(8 * density());
        assertTrue("panel left edge must be >= margin", lp.x >= marginPx);
        assertTrue("panel right edge must be within the screen",
                lp.x + panelWidthPx <= screenWidth);
        controller.destroy();
    }

    @Test
    public void setExpanded_true_leftAnchoredCapsule_keepsPanelOnLeft() throws Exception {
        // Regression: the capsule dragged to the left edge expanded to a panel
        // that jumped to the right edge. The panel must stay on the capsule's
        // side (left-aligned at the margin).
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();

        // Park the capsule at the left edge (x = margin).
        WindowManager.LayoutParams lp = (WindowManager.LayoutParams)
                getPrivateField(controller, "params");
        int marginPx = Math.round(8 * density());
        lp.x = marginPx;
        windowManagerFor(controller).updateViewLayout(
                (View) getPrivateField(controller, "attachedView"), lp);

        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();

        lp = (WindowManager.LayoutParams) getPrivateField(controller, "params");
        assertEquals("left-anchored capsule must keep the panel on the left",
                marginPx, lp.x);
        controller.destroy();
    }

    @Test
    public void setExpanded_true_rightAnchoredCapsule_keepsPanelOnRight() throws Exception {
        // A capsule at the right edge expands to a panel that stays right-
        // aligned (panel right edge = screen - margin), not jumping left.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();

        WindowManager.LayoutParams lp = (WindowManager.LayoutParams)
                getPrivateField(controller, "params");
        int screenWidth = contextWidthPx();
        int marginPx = Math.round(8 * density());
        lp.x = screenWidth / 2 + 50; // right half
        windowManagerFor(controller).updateViewLayout(
                (View) getPrivateField(controller, "attachedView"), lp);

        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();

        lp = (WindowManager.LayoutParams) getPrivateField(controller, "params");
        int panelWidthPx = Math.round(280 * density());
        assertEquals("right-anchored capsule must keep the panel right-aligned",
                screenWidth - panelWidthPx - marginPx, lp.x);
        controller.destroy();
    }

    private WindowManager windowManagerFor(FloatingStatusController controller) {
        return (WindowManager) RuntimeEnvironment.getApplication()
                .getSystemService(Context.WINDOW_SERVICE);
    }

    private int contextWidthPx() {
        return RuntimeEnvironment.getApplication().getResources().getDisplayMetrics().widthPixels;
    }

    private float density() {
        return RuntimeEnvironment.getApplication().getResources().getDisplayMetrics().density;
    }

    // =====================================================
    // onOverviewLoaded: overview rendering into the panel
    // =====================================================

    @Test
    public void onOverviewLoaded_rendersSessionsIntoPanel() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        FloatingStatusPanelView panelView = (FloatingStatusPanelView) getPrivateField(controller, "panelView");
        assertNotNull(panelView);
        String headerText = findHeaderText(panelView);
        assertTrue("panel header must show the running count, got: " + headerText,
                headerText != null && headerText.contains("1"));
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_whenNotExpanded_isNoOp() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");

        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertNull("no panel view should exist when not expanded",
                getPrivateField(controller, "panelView"));
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_background_runningSession_showsCapsule() throws Exception {
        // WS-connect fallback: a running session discovered via the overview
        // (start event missed while the WS was down) must bring up the capsule.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false); // backgrounded

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertTrue("a running session from the overview must show the capsule while backgrounded",
                controller.isWindowShowing());
        assertEquals("overview running session must be tracked", 1,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_noRunningNoUnread_hidesWindow() throws Exception {
        // Backgrounded with no running session and no unread content: there is
        // nothing worth showing, so the window must stay hidden.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertFalse("no running session and no unread content must hide the window",
                controller.isWindowShowing());
        assertEquals(0, controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_background_unreadOnly_showsCapsuleWithUnread() throws Exception {
        // Overview fallback must also cover the unread case: with no running
        // session but an unread one, the capsule appears (and shows 未读 N) to
        // remind the user — previously only running/pending triggered it.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"u\",\"title\":\"t1\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":3}"
                        + "]}]}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertTrue("an unread session from the overview must show the capsule",
                controller.isWindowShowing());
        FloatingStatusView capsule = (FloatingStatusView) getPrivateField(controller, "view");
        assertNotNull("capsule view must be built for the unread fallback", capsule);
        List<String> texts = collectAllTexts(capsule);
        assertTrue("capsule must render the unread count, got: " + texts,
                texts.contains("未读 1"));
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_noRunningAndTotalZero_hidesWindow() throws Exception {
        // Regression: the overview had no running sessions but did not reset
        // hasActive, so a session that ended while the WS was down left the
        // capsule stuck on screen. With total == 0 nothing is worth showing —
        // hasActive must reset and the window must be hidden.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue("window must be visible while the session is running",
                controller.isWindowShowing());

        // Session ended while the WS was down; the overview confirms nothing
        // is running and there are no unread/pending sessions left.
        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[],\"total\":0}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertFalse("empty overview must reset hasActive and hide the window",
                controller.isWindowShowing());
        assertEquals("stale running ids must be cleared by the overview", 0,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_noRunningButTotalPositive_keepsWindow() throws Exception {
        // total > 0 means unread / pending-approval sessions remain, which are
        // still "worth showing" — the window must not be hidden.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}]}],\"total\":1}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertTrue("unread sessions must keep the window visible",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_panelEmptied_autoCollapsesAndHidesWindow() throws Exception {
        // Regression: when the last running session finished while the panel
        // was expanded, the overview came back empty — the panel went blank
        // and stayed on screen until the user tapped the × button. A panel
        // with nothing left worth showing must collapse, which hides the
        // window entirely.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue("panel must be expanded before the session ends",
                controller.isExpanded());
        assertTrue(controller.isWindowShowing());

        org.json.JSONObject emptyOverview = new org.json.JSONObject(
                "{\"projects\":[],\"total\":0}");
        controller.onOverviewLoaded(emptyOverview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertFalse("an emptied panel must auto-collapse", controller.isExpanded());
        assertFalse("an emptied panel must hide the idle window",
                controller.isWindowShowing());
        assertNull("the panel view must be dropped after auto-collapse",
                getPrivateField(controller, "panelView"));
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_panelStillHasUnread_keepsPanelOpen() throws Exception {
        // The auto-collapse must only fire when nothing is left worth showing.
        // Unread sessions remaining must keep the panel expanded.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isExpanded());

        org.json.JSONObject unreadOverview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"u\",\"title\":\"t\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}"
                        + "]}],\"total\":1}");
        controller.onOverviewLoaded(unreadOverview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertTrue("unread sessions must keep the panel expanded",
                controller.isExpanded());
        assertTrue(controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_staleResponseAfterRunningEvent_keepsWindowUp() throws Exception {
        // Race: the overview is fetched asynchronously, so its response can be
        // older than a session_update that arrived while the request was in
        // flight. A stale snapshot (taken before the session started) must not
        // wipe the tracked running session and hide the window mid-session.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        // The overview request was issued while the tracked state was still
        // empty; the session then starts before the response lands. The version
        // captured at request time (0) no longer matches the current state.
        long requestVersion = controller.beginOverviewRequest();
        controller.handleEvent("session_update", sessionEvent("running", "s-late"));
        ShadowLooper.runUiThreadTasks();
        assertEquals("the running event must be tracked before the overview lands", 1,
                controller.getRunningSessionCount());

        // The stale response predates the session: it knows nothing about it.
        org.json.JSONObject staleOverview = new org.json.JSONObject(
                "{\"projects\":[],\"total\":0}");
        controller.onOverviewLoaded(staleOverview, requestVersion);
        ShadowLooper.runUiThreadTasks();

        assertEquals("a stale overview must not drop the just-started session", 1,
                controller.getRunningSessionCount());
        assertTrue("the window must stay up for the running session",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_staleResponse_doesNotClearUnreadMark() throws Exception {
        // Same race for unread: a completion marks its session unread locally, a
        // stale response (taken before it) must not clear that mark and hide the
        // window while the user still has something to read.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        long requestVersion = controller.beginOverviewRequest();

        controller.handleEvent("session_update", completedEvent("s-done"));
        ShadowLooper.runUiThreadTasks();

        org.json.JSONObject staleOverview = new org.json.JSONObject(
                "{\"projects\":[],\"total\":0}");
        controller.onOverviewLoaded(staleOverview, requestVersion);
        ShadowLooper.runUiThreadTasks();

        assertTrue("a stale overview must not clear the unread mark",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_staleResponse_doesNotResurrectClearedUnread() throws Exception {
        // Regression (the mirror of the test above): applying a stale snapshot
        // as "add-only" would re-add a session the user just read, resurrecting
        // the window with a count that no longer exists — and with no further
        // events nothing would correct it. A stale response must be discarded
        // outright, not merged.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        // The overview request goes out while s1 is unread.
        long requestVersion = controller.beginOverviewRequest();
        org.json.JSONObject unreadOverview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"s1\",\"title\":\"t\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}"
                        + "]}]}");
        controller.onOverviewLoaded(unreadOverview, requestVersion);
        ShadowLooper.runUiThreadTasks();
        assertTrue("the unread session must show the window", controller.isWindowShowing());

        // The user reads it before the in-flight response lands.
        controller.handleEvent("session_update", sessionEvent("read", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertFalse("reading the only unread session must hide the window",
                controller.isWindowShowing());

        // A stale snapshot (still listing s1 unread) must NOT bring it back.
        org.json.JSONObject staleOverview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"s1\",\"title\":\"t\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}"
                        + "]}]}");
        controller.onOverviewLoaded(staleOverview, requestVersion);
        ShadowLooper.runUiThreadTasks();

        assertFalse("a stale overview must not resurrect a read session",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_staleResponse_requestsFreshOverview() throws Exception {
        // A discarded stale response must trigger a fresh fetch: the snapshot
        // may have carried data the events cannot reconstruct (e.g. an unread
        // mark for a session that finished while the WS was down), so dropping
        // it silently would leave the capsule permanently stale.
        final int[] requests = {0};
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.setOverviewRequestListener(() -> requests[0]++);

        long requestVersion = controller.beginOverviewRequest();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        requests[0] = 0;

        org.json.JSONObject staleOverview = new org.json.JSONObject(
                "{\"projects\":[],\"total\":0}");
        controller.onOverviewLoaded(staleOverview, requestVersion);
        ShadowLooper.runUiThreadTasks();

        assertEquals("a stale response must trigger a fresh overview request",
                1, requests[0]);
        controller.destroy();
    }

    @Test
    public void terminalEvent_completionCountsAsUnreadEvenWithFlagFalse() throws Exception {
        // Regression: the live "completed" broadcast passes has_new_messages
        // =false (internal/handler/chat.go EmitSessionEventWSOnly), yet the turn
        // did produce assistant output the backend counts as unread. Keying the
        // unread mark off that flag hid the window on every completion.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();

        // completedEvent() deliberately omits has_new_messages, matching prod.
        controller.handleEvent("session_update", completedEvent("s1"));
        ShadowLooper.runUiThreadTasks();

        assertTrue("a completion must keep the window up as unread",
                controller.isWindowShowing());
        assertTrue("the capsule must show the unread count, got: "
                        + collectAllTexts(capsuleOf(controller)),
                collectAllTexts(capsuleOf(controller)).contains("未读 1"));
        controller.destroy();
    }

    @Test
    public void taskRunning_keepsWindowUp() throws Exception {
        // A running scheduled task is not part of /api/ai/sessions/overview
        // (chat sessions only), so it must keep the window up on its own.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        org.json.JSONObject task = new org.json.JSONObject();
        task.put("status", "running");
        task.put("task_id", "task-1");
        controller.handleEvent("task_update", task);
        ShadowLooper.runUiThreadTasks();

        assertTrue("a running scheduled task must show the window",
                controller.isWindowShowing());

        // An overview that knows nothing about the task must not hide it.
        org.json.JSONObject emptyOverview = new org.json.JSONObject(
                "{\"projects\":[],\"total\":0}");
        controller.onOverviewLoaded(emptyOverview, -1L);
        ShadowLooper.runUiThreadTasks();
        assertTrue("an overview covering only chat sessions must not hide a running task",
                controller.isWindowShowing());

        // The task finishing leaves nothing to show.
        task.put("status", "completed");
        controller.handleEvent("task_update", task);
        ShadowLooper.runUiThreadTasks();
        assertFalse("a finished task with nothing else must hide the window",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_authoritativeResponseClearsEndedSessions() throws Exception {
        // A response that is still current (no event landed after the request)
        // is authoritative: it clears the tracked sets, so a session that ended
        // while the WS was down does not linger and keep the window up.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        org.json.JSONObject emptyOverview = new org.json.JSONObject(
                "{\"projects\":[],\"total\":0}");
        controller.onOverviewLoaded(emptyOverview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertEquals("an authoritative empty overview must clear running ids", 0,
                controller.getRunningSessionCount());
        assertFalse("nothing left worth showing must hide the window",
                controller.isWindowShowing());
        controller.destroy();
    }

    // =====================================================
    // computeStats: overview -> {running, pending, unread}
    // =====================================================

    @Test
    public void computeStats_mixedOverview_groupsAreMutuallyExclusive() throws Exception {
        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"r\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0},"
                        + "{\"id\":\"p\",\"running\":false,\"pendingApproval\":true,\"unreadCount\":0},"
                        + "{\"id\":\"b\",\"running\":true,\"pendingApproval\":true,\"unreadCount\":0},"
                        + "{\"id\":\"u\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":3}"
                        + "]}]}");
        int[] stats = FloatingStatusController.computeStats(overview);
        assertEquals("pure running only (pending wins for both-flag sessions)",
                1, stats[0]);
        assertEquals("pending counts pending + both-flag sessions",
                2, stats[1]);
        assertEquals("unread only counts idle sessions with unread",
                1, stats[2]);
    }

    @Test
    public void computeStats_emptyOverview_returnsZeros() throws Exception {
        org.json.JSONObject overview = new org.json.JSONObject("{\"projects\":[],\"total\":0}");
        int[] stats = FloatingStatusController.computeStats(overview);
        assertEquals(0, stats[0]);
        assertEquals(0, stats[1]);
        assertEquals(0, stats[2]);
    }

    // =====================================================
    // onOverviewLoaded: capsule stats render (Task 1+2)
    // =====================================================

    @Test
    public void onOverviewLoaded_updatesCapsuleStats() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"r\",\"title\":\"t\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0},"
                        + "{\"id\":\"p\",\"title\":\"t\",\"running\":false,\"pendingApproval\":true,\"unreadCount\":0},"
                        + "{\"id\":\"u\",\"title\":\"t\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":5}"
                        + "]}]}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        FloatingStatusView capsule = (FloatingStatusView) getPrivateField(controller, "view");
        assertNotNull("capsule must exist to show stats", capsule);
        List<String> texts = collectAllTexts(capsule);
        assertTrue("capsule must show the running count, got: " + texts,
                texts.contains("执行中 1"));
        assertTrue("capsule must show the pending count, got: " + texts,
                texts.contains("待审批 1"));
        assertTrue("capsule must show the unread count, got: " + texts,
                texts.contains("未读 1"));
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_zeroCounts_hidesWindow() throws Exception {
        // All counts at zero means nothing is worth showing: the window must
        // be hidden rather than left up as a logo-only capsule.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[],\"total\":0}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertFalse("zero counts must hide the window", controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_whenPanelExpanded_stillUpdatesCapsuleStats() throws Exception {
        // The capsule is swapped out while the panel is expanded, but a later
        // onOverviewLoaded must still keep the (unattached) capsule stats fresh
        // so collapsing back shows correct counts immediately.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isExpanded());

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"r\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}"
                        + "]}]}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        FloatingStatusView capsule = (FloatingStatusView) getPrivateField(controller, "view");
        assertNotNull("capsule view must be kept alive while expanded", capsule);
        List<String> texts = collectAllTexts(capsule);
        assertTrue("capsule stats must update even while the panel is expanded, got: " + texts,
                texts.contains("执行中 1"));
        controller.destroy();
    }

    @Test
    public void overviewFallback_showsCapsuleWithRunningStats() throws Exception {
        // Bug 1: the WS-connect fallback in onOverviewLoaded showed the capsule
        // (ensureWindow) but never rendered overview data into it, so the
        // capsule displayed the initial empty state ("—" label, transparent dot).
        // The stats capsule must render the running count from the overview.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false); // backgrounded, no prior events

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"T1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        assertTrue("a running session from the overview must show the capsule",
                controller.isWindowShowing());
        FloatingStatusView capsule = (FloatingStatusView) getPrivateField(controller, "view");
        assertNotNull("capsule view must be built for the fallback", capsule);
        List<String> texts = collectAllTexts(capsule);
        assertTrue("capsule must render the running count, got: " + texts,
                texts.contains("执行中 1"));
        assertTrue("capsule must not render the session title, got: " + texts,
                !texts.contains("T1"));
        controller.destroy();
    }

    @Test
    public void terminalEvent_otherSessionsRunning_keepsWindow() throws Exception {
        // A terminal event for one session must not hide the window while
        // another session is still running.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        controller.handleEvent("session_update", completedEvent("s1"));
        ShadowLooper.runUiThreadTasks();

        assertTrue("another session still running must keep the window",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void terminalEvent_lastSession_keepsWindowAsUnread() throws Exception {
        // A completion carries new assistant output, so the finished session is
        // unread: the window must stay up showing 未读 rather than hide — the
        // user still has something to read.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        controller.handleEvent("session_update", completedEvent("s1"));
        ShadowLooper.runUiThreadTasks();

        assertTrue("a completed session is unread and must keep the window up",
                controller.isWindowShowing());
        List<String> texts = collectAllTexts(capsuleOf(controller));
        assertTrue("the capsule must show the unread count, got: " + texts,
                texts.contains("未读 1"));
        controller.destroy();
    }

    @Test
    public void terminalEvent_cancelledWithoutNewMessages_hidesWindow() throws Exception {
        // A cancellation carries no new content, so once the last running
        // session is gone with nothing unread there is nothing to show: the
        // window must be hidden.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        controller.handleEvent("session_update", sessionEvent("cancelled", "s1"));
        ShadowLooper.runUiThreadTasks();

        assertFalse("a cancelled session with nothing unread must hide the window",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void terminalEvent_withUnread_keepsWindow() throws Exception {
        // Ending the last running session while unread sessions remain keeps
        // the capsule showing the unread count.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        // Overview with an unread session (not running, unreadCount > 0).
        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"u\",\"title\":\"t\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}"
                        + "]}]}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();
        assertTrue(collectAllTexts(capsuleOf(controller)).contains("未读 1"));

        // Last running session ends with new output: it joins the unread set
        // alongside u, so the capsule must stay up and show both.
        controller.handleEvent("session_update", completedEvent("s1"));
        ShadowLooper.runUiThreadTasks();

        assertTrue("unread sessions must keep the capsule visible",
                controller.isWindowShowing());
        List<String> texts = collectAllTexts(capsuleOf(controller));
        assertTrue("capsule must count the finished session as unread too, got: " + texts,
                texts.contains("未读 2"));
        controller.destroy();
    }

    @Test
    public void terminalEvent_withUnread_afterRead_hidesWindow() throws Exception {
        // The unread state is not sticky: once the user reads everything and
        // the overview confirms zero unread, a later terminal event for the
        // last session (with no new content) must hide the window.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();

        org.json.JSONObject unreadOverview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"u\",\"title\":\"t\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}"
                        + "]}]}");
        controller.onOverviewLoaded(unreadOverview, -1L);
        ShadowLooper.runUiThreadTasks();

        // All unread cleared.
        org.json.JSONObject readOverview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"u\",\"title\":\"t\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":0}"
                        + "]}]}");
        controller.onOverviewLoaded(readOverview, -1L);
        ShadowLooper.runUiThreadTasks();

        // A cancellation carries no new content, so nothing is left to show.
        controller.handleEvent("session_update", sessionEvent("cancelled", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertFalse("with unread cleared and no new content the window must hide",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void terminalEvent_readEvent_hidesWindowWithoutWaitingForOverview() throws Exception {
        // A "read" event (session marked read from another client) must drop
        // the session from the unread set immediately: with no active session
        // left, the window hides on that event rather than lingering until the
        // overview round trip returns.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", completedEvent("s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue("the completed session is unread and must show the window",
                controller.isWindowShowing());

        controller.handleEvent("session_update", sessionEvent("read", "s1"));
        ShadowLooper.runUiThreadTasks();

        assertFalse("reading the last unread session must hide the window",
                controller.isWindowShowing());
        controller.destroy();
    }

    // =====================================================
    // setExpanded + overview request callback
    // =====================================================

    @Test
    public void setExpanded_true_invokesOverviewRequestListener() {
        final int[] requests = {0};
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        controller.setOverviewRequestListener(() -> requests[0]++);

        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertEquals("expanding must request an overview refresh", 1, requests[0]);

        controller.setExpanded(false);
        ShadowLooper.runUiThreadTasks();
        assertEquals("collapsing must not request an overview refresh", 1, requests[0]);
        controller.destroy();
    }

    @Test
    public void handleEvent_whenExpanded_requestsOverviewRefresh() throws Exception {
        final int[] requests = {0};
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        controller.setOverviewRequestListener(() -> requests[0]++);
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        // Fast-forward past the refresh throttle so the expand request does not
        // suppress the event-triggered one.
        org.robolectric.shadows.ShadowSystemClock.advanceBy(3000,
                java.util.concurrent.TimeUnit.MILLISECONDS);
        requests[0] = 0; // reset the initial expand request

        controller.handleEvent("session_update", sessionEvent("running", "s1"));

        assertEquals("events while expanded must trigger an overview refresh", 1, requests[0]);
        controller.destroy();
    }

    @Test
    public void handleEvent_whenExpanded_throttlesRapidRefreshRequests() throws Exception {        final int[] requests = {0};
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        controller.setOverviewRequestListener(() -> requests[0]++);
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        // Fast-forward past the throttle window so the expand request does not
        // suppress the first event-triggered one.
        org.robolectric.shadows.ShadowSystemClock.advanceBy(3000,
                java.util.concurrent.TimeUnit.MILLISECONDS);
        requests[0] = 0;

        // Burst of streaming events inside the throttle window: only the first
        // may fire the listener; the rest must be skipped.
        for (int i = 0; i < 5; i++) {
            controller.handleEvent("session_update", sessionEvent("running", "s1"));
        }
        assertEquals("events inside the throttle window must be coalesced", 1, requests[0]);

        org.robolectric.shadows.ShadowSystemClock.advanceBy(3000,
                java.util.concurrent.TimeUnit.MILLISECONDS);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        assertEquals("a refresh after the throttle window must fire", 2, requests[0]);
        controller.destroy();
    }

    @Test
    public void setExpanded_true_bypassesThrottle_fetchesOverview() throws Exception {
        final int[] requests = {0};
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        controller.setOverviewRequestListener(() -> requests[0]++);
        // Fire one request first to arm the throttle window.
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertEquals("the initial event must fire the listener", 1, requests[0]);

        // Expanding while still inside the throttle window must STILL fetch:
        // the panel's session list depends on the overview.
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertEquals("expand must bypass the throttle and fetch the overview",
                2, requests[0]);
        controller.destroy();
    }

    // =====================================================
    // Instant capsule counts: handleEvent renders from local
    // state without waiting for the overview round trip
    // =====================================================

    /** The FloatingStatusView built by the controller (capsule). */
    private FloatingStatusView capsuleOf(FloatingStatusController controller) throws Exception {
        return (FloatingStatusView) getPrivateField(controller, "view");
    }

    @Test
    public void handleEvent_running_rendersCapsuleCountsImmediately() throws Exception {
        // Regression: after commit 572a5754 the capsule waited for an overview
        // round trip (5s/10s timeouts), so a session start left the capsule
        // showing only the logo until the network returned.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();

        FloatingStatusView capsule = capsuleOf(controller);
        assertNotNull("active event must build the capsule", capsule);
        List<String> texts = collectAllTexts(capsule);
        assertTrue("capsule must show the running count instantly, got: " + texts,
                texts.contains("执行中 1"));
        assertTrue("capsule must not show the session title, got: " + texts,
                !texts.contains("s1"));
        controller.destroy();
    }

    @Test
    public void handleEvent_permissionPending_rendersPendingCountInstantly() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        controller.handleEvent("session_update", sessionEvent("permission_pending", "s1"));
        ShadowLooper.runUiThreadTasks();

        FloatingStatusView capsule = capsuleOf(controller);
        assertNotNull("permission_pending event must build the capsule", capsule);
        List<String> texts = collectAllTexts(capsule);
        assertTrue("capsule must show the pending count instantly, got: " + texts,
                texts.contains("待审批 1"));
        assertTrue("a pending session must not count as running, got: " + texts,
                !texts.contains("执行中"));
        controller.destroy();
    }

    @Test
    public void handleEvent_completed_rendersUpdatedCountsInstantly() throws Exception {
        // A terminal event must drop the finished session from the capsule's
        // running/pending counts immediately (before any overview returns).
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));
        ShadowLooper.runUiThreadTasks();
        assertTrue("capsule must be showing two running sessions",
                collectAllTexts(capsuleOf(controller)).contains("执行中 2"));

        controller.handleEvent("session_update", sessionEvent("completed", "s1"));
        ShadowLooper.runUiThreadTasks();

        List<String> texts = collectAllTexts(capsuleOf(controller));
        assertTrue("capsule must drop the finished session instantly, got: " + texts,
                texts.contains("执行中 1"));
        controller.destroy();
    }

    @Test
    public void handleEvent_keepsUnreadCountFromLastOverview() throws Exception {
        // Events carry no unread data, so the capsule must keep the unread
        // count from the last overview until a fresh one corrects it.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"s1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0},"
                        + "{\"id\":\"u\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":3}"
                        + "]}]}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();
        assertTrue(collectAllTexts(capsuleOf(controller)).contains("未读 1"));

        // A new running event must not zero out the unread count.
        controller.handleEvent("session_update", sessionEvent("running", "s2"));
        ShadowLooper.runUiThreadTasks();

        List<String> texts = collectAllTexts(capsuleOf(controller));
        assertTrue("unread count from the last overview must persist, got: " + texts,
                texts.contains("未读 1"));
        assertTrue(texts.contains("执行中 2"));
        controller.destroy();
    }

    @Test
    public void handleEvent_runningPendingMix_pendingExcludedFromRunning() throws Exception {
        // Pending wins over running (yellow > green): a session that is both
        // running and pending-approval counts as pending, not running.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s-running"));
        controller.handleEvent("session_update", sessionEvent("permission_pending", "s-pending"));
        ShadowLooper.runUiThreadTasks();

        List<String> texts = collectAllTexts(capsuleOf(controller));
        assertTrue("capsule must show one running, got: " + texts, texts.contains("执行中 1"));
        assertTrue("capsule must show one pending, got: " + texts, texts.contains("待审批 1"));
        assertTrue("capsule must not double-count the pending session, got: " + texts,
                !texts.contains("执行中 2"));
        controller.destroy();
    }

    // =====================================================
    // Session click: panel row -> onSessionClick callback
    // =====================================================

    @Test
    public void setOnSessionClick_rowClickInvokesCallbackWithSessionId() throws Exception {
        final String[] clicked = {null};
        final String[] clickedProjectPath = {null};
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        controller.setOnSessionClick((sid, projectPath) -> {
            clicked[0] = sid;
            clickedProjectPath[0] = projectPath;
        });
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s-click\",\"title\":\"t\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        FloatingStatusPanelView panelView = (FloatingStatusPanelView) getPrivateField(controller, "panelView");
        assertNotNull(panelView);
        TextView title = findSessionRow(panelView, "t");
        assertNotNull("session row must be rendered", title);
        // The click listener lives on the row (the title's parent), not the TextView.
        ((View) title.getParent()).performClick();

        assertEquals("clicking a session row must deliver its session id", "s-click", clicked[0]);
        assertEquals("clicking a session row must deliver its owning project path",
                "/projA", clickedProjectPath[0]);
        controller.destroy();
    }

    @Test
    public void setOnSessionClick_rowClick_collapsesPanel() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication());
        controller.setOnSessionClick((sid, projectPath) -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isExpanded());

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s-click\",\"title\":\"t\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview, -1L);
        ShadowLooper.runUiThreadTasks();

        FloatingStatusPanelView panelView = (FloatingStatusPanelView) getPrivateField(controller, "panelView");
        assertNotNull(panelView);
        TextView title = findSessionRow(panelView, "t");
        assertNotNull(title);
        ((View) title.getParent()).performClick();

        ShadowLooper.runUiThreadTasks();
        assertFalse("opening a session from the panel must collapse it", controller.isExpanded());
        controller.destroy();
    }

    // --- test helpers ---

    private Object getPrivateField(Object target, String name) throws Exception {
        java.lang.reflect.Field f = target.getClass().getDeclaredField(name);
        f.setAccessible(true);
        return f.get(target);
    }

    private String findHeaderText(ViewGroup root) {
        return findTextByClass(root, TextView.class, 0);
    }

    private String findTextByClass(ViewGroup root, Class<? extends TextView> clazz, int depth) {
        if (depth > 4) {
            return null;
        }
        for (int i = 0; i < root.getChildCount(); i++) {
            View child = root.getChildAt(i);
            if (clazz.isInstance(child)) {
                return ((TextView) child).getText().toString();
            }
            if (child instanceof ViewGroup) {
                String found = findTextByClass((ViewGroup) child, clazz, depth + 1);
                if (found != null) {
                    return found;
                }
            }
        }
        return null;
    }

    private TextView findSessionRow(ViewGroup root, String titleText) {
        List<TextView> all = new ArrayList<>();
        collectTextViews(root, all, 0);
        for (TextView tv : all) {
            if (titleText.equals(tv.getText().toString())) {
                return tv;
            }
        }
        return null;
    }

    private void collectTextViews(ViewGroup root, List<TextView> out, int depth) {
        if (depth > 6) {
            return;
        }
        for (int i = 0; i < root.getChildCount(); i++) {
            View child = root.getChildAt(i);
            if (child instanceof TextView) {
                out.add((TextView) child);
            }
            if (child instanceof ViewGroup) {
                collectTextViews((ViewGroup) child, out, depth + 1);
            }
        }
    }

    /** All rendered (visible in the view hierarchy) TextView texts, in order. */
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

    /** Robolectric helper to set Settings.canDrawOverlays(true) for SDK 28. */
    private static final class ShadowSettings {
        static void setCanDrawOverlays(boolean value) {
            try {
                Class<?> cls = Class.forName("org.robolectric.shadows.ShadowSettings");
                java.lang.reflect.Method m = cls.getDeclaredMethod("setCanDrawOverlays", boolean.class);
                m.setAccessible(true);
                m.invoke(null, value);
            } catch (Exception e) {
                throw new RuntimeException("failed to set canDrawOverlays", e);
            }
        }
    }
}
