package com.clawbench.app;

import android.animation.ObjectAnimator;
import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.PorterDuff;
import android.graphics.PorterDuffXfermode;
import android.graphics.drawable.BitmapDrawable;
import android.graphics.drawable.Drawable;
import android.graphics.drawable.GradientDrawable;
import android.view.Gravity;
import android.view.View;
import android.widget.FrameLayout;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.TextView;

import org.json.JSONArray;
import org.json.JSONObject;

/**
 * Floating stats capsule for the desktop floating status window.
 *
 * Layout: {@code logo | (green dot + "执行中 N") | (yellow dot + "待审批 N") |
 * (blue dot + "未读 N")}. Each group is an item (dot + label) and is hidden
 * entirely when its count is 0; the running group's dot breathes while any
 * session is running. Session titles are intentionally not shown.
 *
 * The static pure functions countRunning / countPending / countUnread derive
 * the three mutually-exclusive counts from a /api/ai/sessions/overview JSON
 * object and have no Android framework dependency (only org.json), so they are
 * unit-testable with plain JUnit.
 */
public class FloatingStatusView extends FrameLayout {

    // Status dot colors as inline ARGB literals.
    private static final int COLOR_RUNNING = 0xFF00CC00; // green
    private static final int COLOR_PERMISSION_PENDING = 0xFFE6A23C; // yellow
    private static final int COLOR_UNREAD = 0xFF3B82F6; // blue

    // Layout / animation constants.
    // CORNER_RADIUS = half the 48dp capsule height (20dp vertical padding +
    // 28dp logo) so both ends render as full semicircles.
    private static final int CORNER_RADIUS_DP = 24;
    private static final int PADDING_H_DP = 14;
    private static final int PADDING_H_START_DP = 8;
    private static final int PADDING_V_DP = 10;
    private static final int DOT_SIZE_DP = 12;
    private static final int DOT_MARGIN_END_DP = 6;
    private static final int TEXT_SIZE_SP = 14;
    private static final int LOGO_SIZE_DP = 28;
    private static final int LOGO_MARGIN_END_DP = 10;
    // Breathing animation: the running dot pulses between 30% and full opacity.
    private static final float BREATH_ALPHA_MIN = 0.3f;
    private static final float BREATH_ALPHA_MAX = 1.0f;
    private static final long BREATH_MS = 800;

    private final View runningDot;
    private final LinearLayout runningItem;
    private final LinearLayout pendingItem;
    private final LinearLayout unreadItem;
    private final ObjectAnimator breathAnim;
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

        LinearLayout row = new LinearLayout(context);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER_VERTICAL);

        // App logo as a circle at the capsule's leading edge.
        ImageView logo = new ImageView(context);
        logo.setImageDrawable(circularLogo(context));
        logo.setScaleType(ImageView.ScaleType.FIT_CENTER);
        LinearLayout.LayoutParams logoLp = new LinearLayout.LayoutParams(dp(LOGO_SIZE_DP), dp(LOGO_SIZE_DP));
        logoLp.setMargins(0, 0, dp(LOGO_MARGIN_END_DP), 0);
        row.addView(logo, logoLp);

        runningDot = new View(context);
        GradientDrawable runningDotDrawable = new GradientDrawable();
        runningDotDrawable.setShape(GradientDrawable.OVAL);
        runningDotDrawable.setColor(COLOR_RUNNING);
        runningDot.setBackground(runningDotDrawable);
        runningItem = buildStatItem(row, runningDot, "执行中");

        pendingItem = buildStatItem(row, dot(COLOR_PERMISSION_PENDING), "待审批");
        unreadItem = buildStatItem(row, dot(COLOR_UNREAD), "未读");

        addView(row, new LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT));

        // Breathing alpha animation on the running dot. Loops forever while any
        // session is running; renderStats starts/stops it with the running count.
        breathAnim = ObjectAnimator.ofFloat(runningDot, "alpha",
                BREATH_ALPHA_MIN, BREATH_ALPHA_MAX);
        breathAnim.setDuration(BREATH_MS);
        breathAnim.setRepeatCount(ObjectAnimator.INFINITE);
        breathAnim.setRepeatMode(ObjectAnimator.REVERSE);

        // Initial state: all groups hidden until the first renderStats.
        renderStats(0, 0, 0);
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
     * Render the three stats into the capsule. Groups with a count of 0 are
     * hidden entirely (dot + label). UI thread only.
     *
     * The running dot breathes (alpha 0.3 ↔ 1.0 loop) while the running count
     * is above 0; on zero it stops and the dot returns to full opacity. The
     * pending and unread dots never breathe.
     */
    public void renderStats(int running, int pending, int unread) {
        AppLog.d("FloatingStatusView", "renderStats running=" + running
                + " pending=" + pending + " unread=" + unread);
        setStat(runningItem, running, "执行中");
        setStat(pendingItem, pending, "待审批");
        setStat(unreadItem, unread, "未读");
        if (running > 0) {
            if (!breathAnim.isRunning()) {
                breathAnim.start();
            }
        } else if (breathAnim.isRunning()) {
            breathAnim.cancel();
            runningDot.setAlpha(BREATH_ALPHA_MAX);
        }
    }

    /**
     * Stop the breathing animation and restore the running dot to full opacity.
     * Called on controller teardown so an infinite animator cannot keep posting
     * frame callbacks after the window is removed. UI thread only.
     */
    public void stopBreathing() {
        if (breathAnim.isRunning()) {
            breathAnim.cancel();
        }
        runningDot.setAlpha(BREATH_ALPHA_MAX);
    }

    /** Build one dot+label item, added to the row with dot leading the label. */
    private LinearLayout buildStatItem(LinearLayout row, View dot, String label) {
        LinearLayout item = new LinearLayout(getContext());
        item.setOrientation(LinearLayout.HORIZONTAL);
        item.setGravity(Gravity.CENTER_VERTICAL);
        item.setVisibility(GONE);

        LinearLayout.LayoutParams dotLp = new LinearLayout.LayoutParams(dp(DOT_SIZE_DP), dp(DOT_SIZE_DP));
        dotLp.setMargins(0, 0, dp(DOT_MARGIN_END_DP), 0);
        item.addView(dot, dotLp);

        TextView text = new TextView(getContext());
        text.setTextSize(TEXT_SIZE_SP);
        text.setSingleLine(true);
        text.setIncludeFontPadding(false);
        text.setTextColor(FloatingThemeColors.get(getContext())[1]);
        text.setText(label);
        item.addView(text, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT));

        LinearLayout.LayoutParams itemLp = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT);
        itemLp.setMargins(0, 0, dp(DOT_MARGIN_END_DP), 0);
        row.addView(item, itemLp);
        return item;
    }

    /** A plain oval dot view with the given ARGB color. */
    private View dot(int color) {
        View dot = new View(getContext());
        GradientDrawable drawable = new GradientDrawable();
        drawable.setShape(GradientDrawable.OVAL);
        drawable.setColor(color);
        dot.setBackground(drawable);
        return dot;
    }

    /** Apply a count to a stat item: show dot+label, or hide the whole group. */
    private void setStat(LinearLayout item, int count, String prefix) {
        if (count > 0) {
            item.setVisibility(VISIBLE);
            ((TextView) item.getChildAt(1)).setText(prefix + " " + count);
        } else {
            item.setVisibility(GONE);
        }
    }

    /**
     * Build a circular app-logo drawable by clipping a square crop of the
     * launcher icon to a circle on a fresh ARGB canvas. This does not rely on
     * ShapeDrawable's shader bounds behavior (which silently failed to render
     * under FIT_XY), so the logo is always visible.
     */
    private static Drawable circularLogo(Context context) {
        try {
            Bitmap bmp = BitmapFactory.decodeResource(context.getResources(), R.mipmap.ic_launcher);
            if (bmp == null) {
                return null;
            }
            int size = Math.min(bmp.getWidth(), bmp.getHeight());
            if (size <= 0) {
                return null;
            }
            int left = (bmp.getWidth() - size) / 2;
            int top = (bmp.getHeight() - size) / 2;
            Bitmap square = Bitmap.createBitmap(bmp, left, top, size, size);

            Bitmap output = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888);
            Canvas canvas = new Canvas(output);
            Paint paint = new Paint(Paint.ANTI_ALIAS_FLAG);
            canvas.drawCircle(size / 2f, size / 2f, size / 2f, paint);
            paint.setXfermode(new PorterDuffXfermode(PorterDuff.Mode.SRC_IN));
            canvas.drawBitmap(square, 0, 0, paint);
            return new BitmapDrawable(context.getResources(), output);
        } catch (Exception e) {
            // Any Bitmap/CANVAS failure must not take the whole floating window
            // down; the capsule works fine without a logo.
            AppLog.w("FloatingStatusView", "circularLogo failed", e);
            return null;
        }
    }

    private int dp(int value) {
        return Math.round(value * density);
    }
}
