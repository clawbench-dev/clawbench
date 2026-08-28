package com.clawbench.app;

import android.content.Context;
import android.graphics.drawable.GradientDrawable;
import android.view.View;

import org.json.JSONArray;
import org.json.JSONObject;

/**
 * Floating stats capsule for the desktop floating status window.
 *
 * The visual content — logo + three stat item groups + breathing animation —
 * lives in {@link FloatingStatusContentView}; the capsule is a pill-shaped
 * frame around a single content row. Height = 2 * 7dp vertical padding + 24dp
 * logo = 38dp, with a 19dp (half-height) corner radius so both ends render as
 * full semicircles. The expanded panel's title bar reuses the same content row
 * (see FloatingStatusPanelView), so the collapsed capsule and the expanded
 * panel's header stay visually identical.
 *
 * The static pure functions countRunning / countPending / countUnread derive
 * the three mutually-exclusive counts from a /api/ai/sessions/overview JSON
 * object and have no Android framework dependency (only org.json), so they are
 * unit-testable with plain JUnit.
 */
public class FloatingStatusView extends android.widget.FrameLayout {

    // Layout constants.
    // CORNER_RADIUS = half the 38dp capsule height (7dp vertical padding +
    // 24dp logo) so both ends render as full semicircles.
    private static final int CORNER_RADIUS_DP = 19;
    private static final int PADDING_H_DP = 14;
    private static final int PADDING_H_START_DP = 8;
    private static final int PADDING_V_DP = 7;

    private final FloatingStatusContentView contentView;
    private float density = 1f;

    public FloatingStatusView(Context context) {
        super(context);
        density = getResources().getDisplayMetrics().density;

        // Theme palette read once at construction (floating window rebuilds on
        // theme change pick up the new colors).
        int[] palette = FloatingThemeColors.get(context);
        int bgColor = (palette[0] & 0x00FFFFFF) | 0xEE000000; // keep ~93% opacity
        int textColor = palette[1];

        // Background: capsule with full-semicircle ends, translucent theme
        // background, and a thin border in the theme's secondary text color
        // (matches FloatingStatusPanelView's border so the collapsed capsule
        // reads as the panel's companion; the bg-tinted border was invisible).
        GradientDrawable bg = new GradientDrawable();
        bg.setColor(bgColor);
        bg.setCornerRadius(dp(CORNER_RADIUS_DP));
        bg.setStroke(dp(1), palette[2]);
        setBackground(bg);
        // Asymmetric padding: tighter leading edge so the logo sits embedded
        // near the capsule's left end; trailing edge keeps the wider 14dp.
        setPadding(dp(PADDING_H_START_DP), dp(PADDING_V_DP), dp(PADDING_H_DP), dp(PADDING_V_DP));

        // Shared content row: logo + stat items + breathing animation.
        contentView = new FloatingStatusContentView(context);
        addView(contentView, new LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT));
    }

    /**
     * Count sessions currently running (running && !pendingApproval) in an
     * overview. Pending approval wins over running for a session that has both
     * flags (matches the panel's yellow > green status-dot priority), so the
     * three counts stay mutually exclusive. Pure: only org.json.
     */
    public static int countRunning(JSONObject overview) {
        int count = 0;
        for (JSONObject s : sessions(overview)) {
            if (s != null && s.optBoolean("running", false)
                    && !s.optBoolean("pendingApproval", false)) {
                count++;
            }
        }
        return count;
    }

    /**
     * Count sessions awaiting approval (pendingApproval) in an overview.
     * Pending wins over running for both-flag sessions. Pure: only org.json.
     */
    public static int countPending(JSONObject overview) {
        int count = 0;
        for (JSONObject s : sessions(overview)) {
            if (s != null && s.optBoolean("pendingApproval", false)) {
                count++;
            }
        }
        return count;
    }

    /**
     * Count finished sessions with unread messages: neither running nor
     * pending approval and unreadCount > 0. Pure: only org.json.
     */
    public static int countUnread(JSONObject overview) {
        int count = 0;
        for (JSONObject s : sessions(overview)) {
            if (s != null && !s.optBoolean("running", false)
                    && !s.optBoolean("pendingApproval", false)
                    && s.optInt("unreadCount", 0) > 0) {
                count++;
            }
        }
        return count;
    }

    /** All session objects across every project group; null-safe. */
    private static java.util.List<JSONObject> sessions(JSONObject overview) {
        java.util.List<JSONObject> out = new java.util.ArrayList<>();
        if (overview == null) {
            return out;
        }
        JSONArray projects = overview.optJSONArray("projects");
        if (projects == null) {
            return out;
        }
        for (int i = 0; i < projects.length(); i++) {
            JSONObject project = projects.optJSONObject(i);
            if (project == null) {
                continue;
            }
            JSONArray sessions = project.optJSONArray("sessions");
            if (sessions == null) {
                continue;
            }
            for (int j = 0; j < sessions.length(); j++) {
                out.add(sessions.optJSONObject(j));
            }
        }
        return out;
    }

    /**
     * Render the three stats into the capsule's content row. Delegates to the
     * shared {@link FloatingStatusContentView}. UI thread only.
     */
    public void renderStats(int running, int pending, int unread) {
        contentView.renderStats(running, pending, unread);
    }

    /**
     * Stop the breathing animation in the content row. Called on controller
     * teardown so an infinite animator cannot keep posting frame callbacks
     * after the window is removed. UI thread only.
     */
    public void stopBreathing() {
        contentView.stopBreathing();
    }

    private int dp(int value) {
        return Math.round(value * density);
    }
}
