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
@Config(sdk = 28)
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
        assertTrue(FloatingStatusController.shouldShow(false, true, false));
    }

    @Test
    public void shouldShow_foreground_false() {
        assertFalse(FloatingStatusController.shouldShow(true, true, false));
    }

    @Test
    public void shouldShow_noActive_false() {
        assertFalse(FloatingStatusController.shouldShow(false, false, false));
    }

    @Test
    public void shouldShow_userDismissed_false() {
        assertFalse(FloatingStatusController.shouldShow(false, true, true));
    }

    @Test
    public void shouldShow_foregroundNoActiveDismissed_false() {
        assertFalse(FloatingStatusController.shouldShow(true, false, true));
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
        return new FloatingStatusController(ctx, null);
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
    public void trackSessionState_taskUpdate_isIgnored() throws Exception {
        // task_update is a scheduled-task status (session_id omitempty, often
        // empty); it must not feed the running session set.
        FloatingStatusController controller = newController();
        controller.trackSessionState("task_update", "running", "t1");
        assertEquals("task_update must not be tracked as a running session", 0,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    // =====================================================
    // onCapsuleTap: unified expand-panel tap (Task 3)
    // =====================================================

    /** Track whether the onTap Runnable was invoked. */
    private static final class TapRecorder {
        boolean tapped;
    }

    @Test
    public void onCapsuleTap_singleRunning_expandsPanelNotSession() throws Exception {
        // Capsule tap is now a unified "expand the panel" gesture: even with a
        // single running session it must NOT open the session directly.
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));

        controller.onCapsuleTap();

        assertFalse("single running session tap must not open the session",
                recorder.tapped);
        assertTrue("single running session tap must expand the panel",
                controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void onCapsuleTap_multipleRunning_expandsPanel() throws Exception {
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));

        controller.onCapsuleTap();

        assertFalse("multiple running sessions tap must not open a session",
                recorder.tapped);
        assertTrue(controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void onCapsuleTap_zeroRunning_expandsPanel() throws Exception {
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);

        controller.onCapsuleTap();

        assertFalse("no running sessions tap must not open a session",
                recorder.tapped);
        assertTrue(controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void onCapsuleTap_whenAlreadyExpanded_staysExpanded() throws Exception {
        // A tap while the panel is already expanded keeps it expanded rather
        // than collapsing (the capsule is not attached in that state anyway).
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.setExpanded(true);
        assertTrue(controller.isExpanded());

        controller.onCapsuleTap();

        assertFalse(recorder.tapped);
        assertTrue(controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void capsuleTapTouchEvent_expandsPanel() throws Exception {
        // Real touch path: ACTION_UP on the capsule view must route through
        // onCapsuleTap and always expand the panel, never open a session.
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);
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

        assertFalse("capsule tap must not open a session", recorder.tapped);
        assertTrue("capsule tap must expand the panel", controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void onCapsuleTap_ignoresOnTapRunnable() throws Exception {
        // The onTap runnable (legacy "open most recent session") must never fire
        // from a capsule tap now that taps always expand the panel.
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));

        controller.onCapsuleTap();

        assertFalse("onTap must not be invoked by a capsule tap", recorder.tapped);
        controller.destroy();
    }

    // =====================================================
    // setExpanded / collapse: panel visibility lifecycle
    // =====================================================

    @Test
    public void setExpanded_true_showsPanelWindow() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
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
    public void collapse_restoresCapsuleView() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
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
        // No active session (e.g. panel showed only unread sessions): collapsing
        // must hide the floating window entirely rather than restore the capsule.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue("panel can expand even with no running session", controller.isWindowShowing());

        controller.setExpanded(false);
        ShadowLooper.runUiThreadTasks();

        assertFalse("collapse with no active session must hide the window",
                controller.isWindowShowing());
        assertFalse(controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void setAppForeground_true_resetsExpanded() throws Exception {
        // Regression: returning to the foreground hid the window but kept the
        // expanded flag, so a later background rebuilt the stale panel instead
        // of the capsule.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
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
                RuntimeEnvironment.getApplication(), () -> {});
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
                RuntimeEnvironment.getApplication(), () -> {});
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
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview);
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
                RuntimeEnvironment.getApplication(), () -> {});
        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");

        controller.onOverviewLoaded(overview);
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
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false); // backgrounded

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview);
        ShadowLooper.runUiThreadTasks();

        assertTrue("a running session from the overview must show the capsule while backgrounded",
                controller.isWindowShowing());
        assertEquals("overview running session must be tracked", 1,
                controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_noRunning_doesNotShowWindow() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview);
        ShadowLooper.runUiThreadTasks();

        assertFalse("no running session must not show the capsule", controller.isWindowShowing());
        assertEquals(0, controller.getRunningSessionCount());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_noRunningAndTotalZero_hidesLingeringWindow() throws Exception {
        // Regression: the overview had no running sessions but did not reset
        // hasActive, so a session that ended while the WS was down left the
        // capsule stuck on screen. With total == 0 nothing is worth showing.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
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
        controller.onOverviewLoaded(overview);
        ShadowLooper.runUiThreadTasks();

        assertFalse("empty overview must reset hasActive and hide the window",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_noRunningButTotalPositive_keepsWindow() throws Exception {
        // total > 0 means unread / pending-approval sessions remain, which are
        // still "worth showing" — the window must not be hidden.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"t1\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":2}]}],\"total\":1}");
        controller.onOverviewLoaded(overview);
        ShadowLooper.runUiThreadTasks();

        assertTrue("unread sessions must keep the window visible",
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
                RuntimeEnvironment.getApplication(), () -> {});
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
        controller.onOverviewLoaded(overview);
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
    public void onOverviewLoaded_zeroCounts_hideStatItems() throws Exception {
        // Zero-count groups (dot + label) must be hidden: only the logo shows.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[],\"total\":0}");
        controller.onOverviewLoaded(overview);
        ShadowLooper.runUiThreadTasks();

        FloatingStatusView capsule = (FloatingStatusView) getPrivateField(controller, "view");
        assertNotNull(capsule);
        List<String> texts = collectAllTexts(capsule);
        assertTrue("zero-count stats must be hidden, got: " + texts, texts.isEmpty());
        controller.destroy();
    }

    @Test
    public void onOverviewLoaded_whenPanelExpanded_stillUpdatesCapsuleStats() throws Exception {
        // The capsule is swapped out while the panel is expanded, but a later
        // onOverviewLoaded must still keep the (unattached) capsule stats fresh
        // so collapsing back shows correct counts immediately.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
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
        controller.onOverviewLoaded(overview);
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
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false); // backgrounded, no prior events

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s1\",\"title\":\"T1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview);
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
        // Bug 2: a terminal event for one session set hasActive=false, so the
        // 3s hide fired even though another session was still running.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        controller.handleEvent("session_update", sessionEvent("completed", "s1"));
        ShadowLooper.runUiThreadTasks();

        assertTrue("another session still running must keep the window",
                controller.isWindowShowing());
        assertNull("no terminal hide may be scheduled while other sessions run",
                getPrivateField(controller, "fadeHideRunnable"));
        controller.destroy();
    }

    @Test
    public void terminalEvent_lastSession_endsWindow() throws Exception {
        // Regression guard: when the last running session ends, the "done"
        // capsule must still be shown briefly and then hidden.
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isWindowShowing());

        controller.handleEvent("session_update", sessionEvent("completed", "s1"));
        ShadowLooper.runUiThreadTasks();

        assertNotNull("last session ending must schedule the terminal hide",
                getPrivateField(controller, "fadeHideRunnable"));
        ShadowLooper.runUiThreadTasksIncludingDelayedTasks();
        assertFalse("the window must hide after the terminal delay",
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
                RuntimeEnvironment.getApplication(), () -> {});
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
                RuntimeEnvironment.getApplication(), () -> {});
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
    public void handleEvent_whenExpanded_throttlesRapidRefreshRequests() throws Exception {
        final int[] requests = {0};
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
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
                RuntimeEnvironment.getApplication(), () -> {});
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
                RuntimeEnvironment.getApplication(), () -> {});
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
                RuntimeEnvironment.getApplication(), () -> {});
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
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        ShadowLooper.runUiThreadTasks();

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":["
                        + "{\"id\":\"s1\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0},"
                        + "{\"id\":\"u\",\"running\":false,\"pendingApproval\":false,\"unreadCount\":3}"
                        + "]}]}");
        controller.onOverviewLoaded(overview);
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
                RuntimeEnvironment.getApplication(), () -> {});
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
                RuntimeEnvironment.getApplication(), () -> {});
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
        controller.onOverviewLoaded(overview);
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
                RuntimeEnvironment.getApplication(), () -> {});
        controller.setOnSessionClick((sid, projectPath) -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false);
        controller.setExpanded(true);
        ShadowLooper.runUiThreadTasks();
        assertTrue(controller.isExpanded());

        org.json.JSONObject overview = new org.json.JSONObject(
                "{\"projects\":[{\"name\":\"/projA\",\"sessions\":[{\"id\":\"s-click\",\"title\":\"t\",\"running\":true,\"pendingApproval\":false,\"unreadCount\":0}]}],\"total\":1}");
        controller.onOverviewLoaded(overview);
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
