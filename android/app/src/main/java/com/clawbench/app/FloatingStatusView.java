package com.clawbench.app;

import android.animation.ObjectAnimator;
import android.content.Context;
import android.graphics.Color;
import android.graphics.drawable.GradientDrawable;
import android.view.Gravity;
import android.view.View;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;

/**
 * Floating capsule view for the desktop floating status window.
 *
 * Renders a status dot + single-line label mapping session_update / task_update
 * events to UI text and colors. The static pure functions statusLabel /
 * statusColor have no Android framework dependency (colors are inline int
 * literals), so they are unit-testable with plain JUnit.
 */
public class FloatingStatusView extends FrameLayout {

    // Colors as inline ARGB literals to keep pure functions framework-free.
    private static final int COLOR_RUNNING = 0xFF00CC00; // green
    private static final int COLOR_PERMISSION_PENDING = 0xFFE6A23C; // yellow
    private static final int COLOR_COMPLETED = 0xFF9E9E9E; // gray
    private static final int COLOR_FAILED = 0xFFE53935; // red
    private static final int COLOR_CANCELLED = 0xFF9E9E9E; // gray
    private static final int COLOR_UNKNOWN = 0x00000000; // transparent

    private final View statusDot;
    private final TextView labelView;
    private float density = 1f;

    public FloatingStatusView(Context context) {
        super(context);
        density = getResources().getDisplayMetrics().density;

        // Background: rounded capsule, light translucent.
        GradientDrawable bg = new GradientDrawable();
        bg.setColor(0xEEFFFFFF);
        bg.setCornerRadius(dp(18));
        setBackground(bg);
        setPadding(dp(12), dp(6), dp(12), dp(6));

        LinearLayout row = new LinearLayout(context);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_VERTICAL);

        statusDot = new View(context);
        GradientDrawable dot = new GradientDrawable();
        dot.setShape(GradientDrawable.OVAL);
        dot.setColor(COLOR_UNKNOWN);
        dot.setSize(dp(10), dp(10));
        statusDot.setBackground(dot);
        LinearLayout.LayoutParams dotLp = new LinearLayout.LayoutParams(dp(10), dp(10));
        dotLp.setMargins(0, 0, dp(6), 0);
        row.addView(statusDot, dotLp);

        labelView = new TextView(context);
        labelView.setTextSize(12);
        labelView.setSingleLine(true);
        labelView.setMaxLines(1);
        labelView.setEllipsize(android.text.TextUtils.TruncateAt.END);
        labelView.setTextColor(Color.parseColor("#FF333333"));
        labelView.setIncludeFontPadding(false);
        row.addView(labelView, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT));

        addView(row, new LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT));
    }

    /** Map an event to its capsule label text. Pure: no instance fields, no framework deps. */
    public static String statusLabel(String eventType, String status, String sessionTitle, String toolName) {
        if ("session_update".equals(eventType)) {
            if ("running".equals(status)) {
                return sessionTitle == null || sessionTitle.isEmpty()
                        ? "运行中" : "运行中 · " + sessionTitle;
            }
            if ("completed".equals(status)) {
                return "✓ 完成";
            }
            if ("cancelled".equals(status)) {
                return "已取消";
            }
            if ("permission_pending".equals(status)) {
                return toolName == null || toolName.isEmpty()
                        ? "等待授权" : "等待授权 · " + toolName;
            }
        } else if ("task_update".equals(eventType)) {
            if ("running".equals(status)) {
                return sessionTitle == null || sessionTitle.isEmpty()
                        ? "任务运行中" : "任务运行中 · " + sessionTitle;
            }
            if ("completed".equals(status)) {
                return "✓ 任务完成";
            }
            if ("failed".equals(status)) {
                return "任务失败";
            }
            if ("cancelled".equals(status)) {
                return "任务已取消";
            }
        }
        return "";
    }

    /** Map an event to its status dot color (ARGB int). Pure: no instance fields. */
    public static int statusColor(String eventType, String status) {
        if ("session_update".equals(eventType)) {
            if ("running".equals(status)) {
                return COLOR_RUNNING;
            }
            if ("permission_pending".equals(status)) {
                return COLOR_PERMISSION_PENDING;
            }
            if ("completed".equals(status)) {
                return COLOR_COMPLETED;
            }
            if ("cancelled".equals(status)) {
                return COLOR_CANCELLED;
            }
        } else if ("task_update".equals(eventType)) {
            if ("running".equals(status)) {
                return COLOR_RUNNING;
            }
            if ("completed".equals(status)) {
                return COLOR_COMPLETED;
            }
            if ("failed".equals(status)) {
                return COLOR_FAILED;
            }
            if ("cancelled".equals(status)) {
                return COLOR_CANCELLED;
            }
        }
        return COLOR_UNKNOWN;
    }

    /** Refresh the capsule from an event. Falls back to preview text when no label maps. */
    public void render(String eventType, String status, String sessionTitle, String toolName, String preview) {
        AppLog.d("FloatingStatusView",
                "render eventType=" + eventType + " status=" + status
                        + " sessionTitle=" + sessionTitle + " toolName=" + toolName
                        + " preview=" + preview);

        String label = statusLabel(eventType, status, sessionTitle, toolName);
        String text;
        if (label != null && !label.isEmpty()) {
            text = label;
        } else if (preview != null && !preview.isEmpty()) {
            text = preview;
        } else {
            text = "—";
        }
        labelView.setText(text);

        ((GradientDrawable) statusDot.getBackground()).setColor(
                statusColor(eventType, status));
    }

    /** Pulse the status dot (scale 1 → 1.25 → 1, 200ms each way). */
    public void pulse() {
        ObjectAnimator animX = ObjectAnimator.ofFloat(statusDot, "scaleX", 1f, 1.25f, 1f);
        animX.setDuration(200);
        ObjectAnimator animY = ObjectAnimator.ofFloat(statusDot, "scaleY", 1f, 1.25f, 1f);
        animY.setDuration(200);
        animX.start();
        animY.start();
    }

    private int dp(int value) {
        return Math.round(value * density);
    }
}
