package com.clawbench.app;

import android.content.Context;
import android.os.Looper;
import android.view.View;
import android.view.ViewGroup;
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
    // hasRunningSession: parse /api/sessions response for running sessions
    // =====================================================

    @Test
    public void hasRunningSession_withRunningSession_true() throws Exception {
        String json = "{\"sessions\":[{\"id\":\"s1\",\"running\":true},{\"id\":\"s2\",\"running\":false}]}";
        assertTrue(FloatingStatusController.hasRunningSession(new org.json.JSONObject(json)));
    }

    @Test
    public void hasRunningSession_noRunning_false() throws Exception {
        String json = "{\"sessions\":[{\"id\":\"s1\",\"running\":false},{\"id\":\"s2\",\"running\":false}]}";
        assertFalse(FloatingStatusController.hasRunningSession(new org.json.JSONObject(json)));
    }

    @Test
    public void hasRunningSession_emptySessions_false() throws Exception {
        assertFalse(FloatingStatusController.hasRunningSession(new org.json.JSONObject("{\"sessions\":[]}")));
    }

    @Test
    public void hasRunningSession_missingSessionsKey_false() throws Exception {
        assertFalse(FloatingStatusController.hasRunningSession(new org.json.JSONObject("{}")));
    }

    @Test
    public void hasRunningSession_runningOmitted_false() throws Exception {
        // "running" is omitempty server-side; an entry without the field is not running
        String json = "{\"sessions\":[{\"id\":\"s1\"}]}";
        assertFalse(FloatingStatusController.hasRunningSession(new org.json.JSONObject(json)));
    }

    // =====================================================
    // decideCapsuleClick: pure tap-decision function
    // =====================================================

    @Test
    public void decideCapsuleClick_singleRunning_opensSession() {
        assertEquals(FloatingStatusController.CLICK_OPEN_SESSION,
                FloatingStatusController.decideCapsuleClick(1));
    }

    @Test
    public void decideCapsuleClick_multipleRunning_expandsPanel() {
        assertEquals(FloatingStatusController.CLICK_EXPAND_PANEL,
                FloatingStatusController.decideCapsuleClick(2));
    }

    @Test
    public void decideCapsuleClick_zeroRunning_expandsPanel() {
        // No running sessions (e.g. only unread) -> expand panel to view the list.
        assertEquals(FloatingStatusController.CLICK_EXPAND_PANEL,
                FloatingStatusController.decideCapsuleClick(0));
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
    public void notifyRunningSession_addsToRunningCount() {
        FloatingStatusController controller = newController();
        controller.notifyRunningSession("s1", "会话标题");
        assertEquals("WS poll-discovered session must be tracked", 1,
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

    @Test
    public void shouldOpenSessionOnCapsuleTap_singleRunning_true() throws Exception {
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        assertTrue(controller.shouldOpenSessionOnCapsuleTap());
        controller.destroy();
    }

    @Test
    public void shouldOpenSessionOnCapsuleTap_multipleRunning_false() throws Exception {
        FloatingStatusController controller = newController();
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));
        assertFalse(controller.shouldOpenSessionOnCapsuleTap());
        controller.destroy();
    }

    // =====================================================
    // notifyRunningSession: sets hasActive and shows the window when backgrounded
    // =====================================================

    @Test
    public void notifyRunningSession_background_showsWindow() {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(false); // backgrounded
        controller.notifyRunningSession("s1", "会话标题");

        ShadowLooper.runUiThreadTasks();
        assertTrue("window should show for a running session while backgrounded",
                controller.isWindowShowing());
        controller.destroy();
    }

    @Test
    public void notifyRunningSession_foreground_doesNotShow() {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        ShadowSettings.setCanDrawOverlays(true);
        controller.setAppForeground(true); // foreground
        controller.notifyRunningSession("s1", "会话标题");

        ShadowLooper.runUiThreadTasks();
        assertFalse("window must not show while app is foreground",
                controller.isWindowShowing());
        controller.destroy();
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

    // =====================================================
    // onCapsuleTap: capsule tap decision wiring
    // =====================================================

    /** Track whether the onTap Runnable was invoked. */
    private static final class TapRecorder {
        boolean tapped;
    }

    @Test
    public void capsuleTap_singleRunning_invokesOnTap() throws Exception {
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));

        controller.onCapsuleTap();

        assertTrue("single running session tap must open the session (invoke onTap)",
                recorder.tapped);
        controller.destroy();
    }

    @Test
    public void capsuleTap_multipleRunning_doesNotInvokeOnTap() throws Exception {
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.handleEvent("session_update", sessionEvent("running", "s2"));

        controller.onCapsuleTap();

        assertFalse("multiple running sessions tap must expand the panel, not open a session",
                recorder.tapped);
        controller.destroy();
    }

    @Test
    public void onCapsuleTap_zeroRunning_expandsPanel() throws Exception {
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);

        controller.onCapsuleTap();

        assertFalse("no running sessions tap must expand the panel, not open a session",
                recorder.tapped);
        assertTrue(controller.isExpanded());
        controller.destroy();
    }

    @Test
    public void onCapsuleTap_singleRunning_whenExpanded_stillOpensSession() throws Exception {
        // A tap while the panel is already expanded should still respect the
        // single-session decision (open the session) rather than collapsing.
        TapRecorder recorder = new TapRecorder();
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> recorder.tapped = true);
        controller.handleEvent("session_update", sessionEvent("running", "s1"));
        controller.setExpanded(true);
        assertTrue(controller.isExpanded());

        controller.onCapsuleTap();

        assertTrue(recorder.tapped);
        controller.destroy();
    }

    @Test
    public void capsuleTapTouchEvent_multipleRunning_expandsPanel() throws Exception {
        // Real touch path: ACTION_UP on the capsule view must route through
        // onCapsuleTap (decideCapsuleClick), so multiple running sessions
        // expand the panel instead of opening a session.
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

        assertFalse("multi-session capsule tap must not open a session", recorder.tapped);
        assertTrue("multi-session capsule tap must expand the panel", controller.isExpanded());
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
        requests[0] = 0; // reset the initial expand request

        controller.handleEvent("session_update", sessionEvent("running", "s1"));

        assertEquals("events while expanded must trigger an overview refresh", 1, requests[0]);
        controller.destroy();
    }

    // =====================================================
    // Session click: panel row -> onSessionClick callback
    // =====================================================

    @Test
    public void setOnSessionClick_rowClickInvokesCallbackWithSessionId() throws Exception {
        final String[] clicked = {null};
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        controller.setOnSessionClick(sid -> clicked[0] = sid);
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
        controller.destroy();
    }

    @Test
    public void setOnSessionClick_rowClick_collapsesPanel() throws Exception {
        FloatingStatusController controller = new FloatingStatusController(
                RuntimeEnvironment.getApplication(), () -> {});
        controller.setOnSessionClick(sid -> {});
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
}
