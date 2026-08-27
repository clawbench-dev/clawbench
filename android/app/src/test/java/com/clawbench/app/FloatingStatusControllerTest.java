package com.clawbench.app;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Unit tests for FloatingStatusController's pure static decision functions.
 *
 * isActiveStatus / shouldShow are pure functions with no Android framework
 * dependencies, so they can be tested with plain JUnit (no Robolectric).
 */
public class FloatingStatusControllerTest {

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
}
