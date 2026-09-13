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
 * entirely when its count is 0; the running group's arc spins while any
 * session is running. Session titles are intentionally not shown.
 *
 * With all three counts at 0 the row holds only the logo. That state is not a
 * resting state for the window: the controller hides the floating window when
 * there is nothing worth showing, so a zero-count capsule is only ever built
 * transiently (e.g. between a terminal event and the overview that hides it).
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
    // Spin animation: the running arc rotates 0 → 360° forever while any
    // session is running.
    private static final long SPIN_MS = 900;

    private final View runningDot;
    private final LinearLayout runningItem;
    private final LinearLayout pendingItem;
    private final LinearLayout unreadItem;
    private final ObjectAnimator spinAnim;
    private final float density;
    /** Last rendered counts, kept so refreshLocaleText() can re-render after a system locale change. */
    private int lastRunning;
    private int lastPending;
    private int lastUnread;

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

        // Running indicator: a rotating ring-arc (spinner-like) instead of a
        // static dot. The drawable renders a 270° arc; spinAnim rotates the
        // view while any session is running.
        runningDot = new View(context);
        runningDot.setBackground(new ArcProgressDrawable(COLOR_RUNNING, density));
        runningItem = buildStatItem(runningDot, R.string.floating_stat_running);

        pendingItem = buildStatItem(dot(COLOR_PERMISSION_PENDING), R.string.floating_stat_pending);
        unreadItem = buildStatItem(dot(COLOR_UNREAD), R.string.floating_stat_unread);

        // Spin animation on the running arc. Loops forever while any session
        // is running; renderStats starts/stops it with the running count.
        // A linear interpolator keeps the rotation perfectly constant-speed —
        // the default AccelerateDecelerateInterpolator makes each lap start
        // slow and end slow, which reads as a visible hiccup per revolution.
        spinAnim = ObjectAnimator.ofFloat(runningDot, "rotation", 0f, 360f);
        spinAnim.setDuration(SPIN_MS);
        spinAnim.setRepeatCount(ObjectAnimator.INFINITE);
        spinAnim.setInterpolator(new android.view.animation.LinearInterpolator());

        // Initial state: all groups hidden until the first renderStats.
        renderStats(0, 0, 0);
    }

    /**
     * Render the three stats into the row. Groups with a count of 0 are
     * hidden entirely (dot + label); with every count at 0 the row holds only
     * the logo. UI thread only.
     *
     * The running arc spins (rotation 0 → 360° loop) while the running count
     * is above 0; on zero it stops and the rotation resets. The pending and
     * unread dots never spin.
     */
    public void renderStats(int running, int pending, int unread) {
        AppLog.d("FloatingStatusContent", "renderStats running=" + running
                + " pending=" + pending + " unread=" + unread);
        lastRunning = running;
        lastPending = pending;
        lastUnread = unread;
        setStat(runningItem, running, R.string.floating_stat_running);
        setStat(pendingItem, pending, R.string.floating_stat_pending);
        setStat(unreadItem, unread, R.string.floating_stat_unread);
        if (running > 0) {
            if (!spinAnim.isRunning()) {
                spinAnim.start();
            }
        } else if (spinAnim.isRunning()) {
            spinAnim.cancel();
            runningDot.setRotation(0f);
        }
    }

    /**
     * Re-resolve strings after a language change (in-app switch or system
     * locale change) and re-render the last counts. Stat labels are re-read
     * from resources so the floating capsule follows the language immediately.
     * UI thread only.
     */
    public void refreshLocaleText() {
        renderStats(lastRunning, lastPending, lastUnread);
    }

    /**
     * Stop the spin animation and reset the running arc's rotation.
     * Called on host teardown so an infinite animator cannot keep posting
     * frame callbacks after the window is removed. UI thread only.
     */
    public void stopBreathing() {
        if (spinAnim.isRunning()) {
            spinAnim.cancel();
        }
        runningDot.setRotation(0f);
    }

    /** Build one dot+label item, added to the row with dot leading the label. */
    private LinearLayout buildStatItem(View dot, int labelResId) {
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
        text.setText(UserLanguage.resolve(getContext(), labelResId));
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
    private void setStat(LinearLayout item, int count, int labelResId) {
        if (count > 0) {
            item.setVisibility(VISIBLE);
            ((TextView) item.getChildAt(1)).setText(
                    UserLanguage.resolve(getContext(), labelResId, count));
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
