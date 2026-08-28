package com.clawbench.app;

import android.content.Context;
import android.os.Looper;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.RuntimeEnvironment;
import org.robolectric.annotation.Config;
import org.robolectric.shadows.ShadowLooper;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
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
}
