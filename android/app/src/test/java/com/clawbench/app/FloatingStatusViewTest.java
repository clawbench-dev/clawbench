package com.clawbench.app;

import org.junit.Test;

import static org.junit.Assert.*;

/**
 * Unit tests for FloatingStatusView's pure static state mapping functions.
 *
 * statusLabel / statusColor are pure functions with no Android framework
 * dependencies, so they can be tested with plain JUnit (no Robolectric).
 * Colors are inline int literals (0xAARRGGBB), not android.graphics.Color.
 */
public class FloatingStatusViewTest {

    // --- statusLabel ---

    @Test
    public void statusLabel_sessionRunning_withTitle() {
        assertEquals("运行中 · 修复登录 bug",
                FloatingStatusView.statusLabel("session_update", "running", "修复登录 bug", ""));
    }

    @Test
    public void statusLabel_sessionRunning_emptyTitle() {
        assertEquals("运行中",
                FloatingStatusView.statusLabel("session_update", "running", "", ""));
    }

    @Test
    public void statusLabel_sessionCompleted() {
        assertEquals("✓ 完成",
                FloatingStatusView.statusLabel("session_update", "completed", "", ""));
    }

    @Test
    public void statusLabel_sessionCancelled() {
        assertEquals("已取消",
                FloatingStatusView.statusLabel("session_update", "cancelled", "", ""));
    }

    @Test
    public void statusLabel_sessionPermissionPending_withToolName() {
        assertEquals("等待授权 · 文件写入",
                FloatingStatusView.statusLabel("session_update", "permission_pending", "", "文件写入"));
    }

    @Test
    public void statusLabel_sessionPermissionPending_emptyToolName() {
        assertEquals("等待授权",
                FloatingStatusView.statusLabel("session_update", "permission_pending", "", ""));
    }

    @Test
    public void statusLabel_taskRunning_withTitle() {
        assertEquals("任务运行中 · 跑测试",
                FloatingStatusView.statusLabel("task_update", "running", "跑测试", ""));
    }

    @Test
    public void statusLabel_taskCompleted() {
        assertEquals("✓ 任务完成",
                FloatingStatusView.statusLabel("task_update", "completed", "", ""));
    }

    @Test
    public void statusLabel_taskFailed() {
        assertEquals("任务失败",
                FloatingStatusView.statusLabel("task_update", "failed", "", ""));
    }

    @Test
    public void statusLabel_taskCancelled() {
        assertEquals("任务已取消",
                FloatingStatusView.statusLabel("task_update", "cancelled", "", ""));
    }

    @Test
    public void statusLabel_unknownCombo_returnsEmpty() {
        assertEquals("", FloatingStatusView.statusLabel("session_update", "started", "", ""));
        assertEquals("", FloatingStatusView.statusLabel("unknown_event", "running", "", ""));
        assertEquals("", FloatingStatusView.statusLabel("", "", "", ""));
    }

    // --- statusColor ---

    @Test
    public void statusColor_sessionRunning_differsFromCompleted() {
        int running = FloatingStatusView.statusColor("session_update", "running");
        int completed = FloatingStatusView.statusColor("session_update", "completed");
        assertNotEquals(running, completed);
        assertNotEquals(0, running);
        assertNotEquals(0, completed);
    }

    @Test
    public void statusColor_failed_nonZero() {
        assertNotEquals(0, FloatingStatusView.statusColor("task_update", "failed"));
    }

    @Test
    public void statusColor_permissionPending_isYellow() {
        assertEquals(0xFFE6A23C, FloatingStatusView.statusColor("session_update", "permission_pending"));
    }

    @Test
    public void statusColor_cancelled_isGrayLike() {
        int cancelled = FloatingStatusView.statusColor("session_update", "cancelled");
        int failed = FloatingStatusView.statusColor("task_update", "failed");
        assertNotEquals(0, cancelled);
        // cancelled and completed both gray; failed is red, so they must differ
        assertNotEquals(failed, cancelled);
    }

    @Test
    public void statusColor_unknown_returnsZero() {
        assertEquals(0, FloatingStatusView.statusColor("session_update", "started"));
        assertEquals(0, FloatingStatusView.statusColor("", ""));
        assertEquals(0, FloatingStatusView.statusColor("unknown", "unknown"));
    }
}
