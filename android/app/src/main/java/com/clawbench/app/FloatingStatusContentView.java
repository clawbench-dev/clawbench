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
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.TextView;

/**
 * The shared stats content row used by both the collapsed capsule
 * (FloatingStatusView) and the expanded panel's title bar
 * (FloatingStatusPanelView).
 *
 * Layout: {@code logo | (green dot + "执行中 N") | (yellow dot + "待审批 N") |
 * (blue dot + "未读 N")}. Each group is an item (dot + label) and is hidden
 * entirely when its count is 0; the running group's dot breathes while any
 * session is running. Session titles are intentionally not shown.
 *
 * Both the capsule and the panel title bar own their own instance, so the
 * breathing animation is managed independently per instance (a capsule and a
 * panel are never attached at the same time, and the title-bar instance stays
 * stable across panel renders — only the session rows are rebuilt).
 *
 * The row's intrinsic height is the logo size (24dp), so a host with 7dp
 * vertical padding produces the 38dp capsule / title-bar height.
 */
public class FloatingStatusContentView extends LinearLayout {

    // Status dot colors as inline ARGB literals.
    private static final int COLOR_RUNNING = 0xFF00CC00; // green
    private static final int COLOR_PERMISSION_PENDING = 0xFFE6A23C; // yellow
    private static final int COLOR_UNREAD = 0xFF3B82F6; // blue

    // Layout / animation constants.
    static final int LOGO_SIZE_DP = 24;
    static final int DOT_SIZE_DP = 12;
    static final int DOT_MARGIN_END_DP = 6;
    static final int TEXT_SIZE_SP = 14;
    static final int LOGO_MARGIN_END_DP = 10;
    // Breathing animation: the running dot pulses between 30% and full opacity.
    private static final float BREATH_ALPHA_MIN = 0.3f;
    private static final float BREATH_ALPHA_MAX = 1.0f;
    private static final long BREATH_MS = 800;

    private final View runningDot;
    private final LinearLayout runningItem;
    private final LinearLayout pendingItem;
    private final LinearLayout unreadItem;
    private final ObjectAnimator breathAnim;
    private final float density;

    public FloatingStatusContentView(Context context) {
        super(context);
        density = getResources().getDisplayMetrics().density;

        setOrientation(HORIZONTAL);
        setGravity(Gravity.CENTER_VERTICAL);

        // App logo as a circle at the row's leading edge.
        ImageView logo = new ImageView(context);
        logo.setImageDrawable(circularLogo(context));
        logo.setScaleType(ImageView.ScaleType.FIT_CENTER);
        LinearLayout.LayoutParams logoLp = new LinearLayout.LayoutParams(dp(LOGO_SIZE_DP), dp(LOGO_SIZE_DP));
        logoLp.setMargins(0, 0, dp(LOGO_MARGIN_END_DP), 0);
        addView(logo, logoLp);

        runningDot = new View(context);
        GradientDrawable runningDotDrawable = new GradientDrawable();
        runningDotDrawable.setShape(GradientDrawable.OVAL);
        runningDotDrawable.setColor(COLOR_RUNNING);
        runningDot.setBackground(runningDotDrawable);
        runningItem = buildStatItem(runningDot, "执行中");

        pendingItem = buildStatItem(dot(COLOR_PERMISSION_PENDING), "待审批");
        unreadItem = buildStatItem(dot(COLOR_UNREAD), "未读");

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
     * Render the three stats into the row. Groups with a count of 0 are
     * hidden entirely (dot + label). UI thread only.
     *
     * The running dot breathes (alpha 0.3 ↔ 1.0 loop) while the running count
     * is above 0; on zero it stops and the dot returns to full opacity. The
     * pending and unread dots never breathe.
     */
    public void renderStats(int running, int pending, int unread) {
        AppLog.d("FloatingStatusContent", "renderStats running=" + running
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
     * Called on host teardown so an infinite animator cannot keep posting
     * frame callbacks after the window is removed. UI thread only.
     */
    public void stopBreathing() {
        if (breathAnim.isRunning()) {
            breathAnim.cancel();
        }
        runningDot.setAlpha(BREATH_ALPHA_MAX);
    }

    /** Build one dot+label item, added to the row with dot leading the label. */
    private LinearLayout buildStatItem(View dot, String label) {
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
        addView(item, itemLp);
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
            // down; the row works fine without a logo.
            AppLog.w("FloatingStatusContent", "circularLogo failed", e);
            return null;
        }
    }

    private int dp(int value) {
        return Math.round(value * density);
    }
}
