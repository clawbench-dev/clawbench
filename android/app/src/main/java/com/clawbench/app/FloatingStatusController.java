package com.clawbench.app;

import android.content.Context;
import android.content.SharedPreferences;
import android.graphics.PixelFormat;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.provider.Settings;
import android.view.Gravity;
import android.view.MotionEvent;
import android.view.View;
import android.view.ViewConfiguration;
import android.view.WindowManager;

import org.json.JSONArray;
import org.json.JSONObject;

/**
 * Controller for the desktop floating status window.
 *
 * Owns the WindowManager / FloatingStatusView / LayoutParams triple: decides
 * when to show or hide the capsule based on incoming session_update /
 * task_update events, app foreground state, and user dismissal. Handles
 * drag-to-snap positioning (persisted to SharedPreferences) and tap-to-open.
 *
 * handleEvent / setAppForeground / setUserDismissed are safe to call from any
 * thread; all WindowManager and View mutations are marshalled to the UI
 * thread via view.post(Runnable). The static pure functions isActiveStatus /
 * shouldShow have no framework dependency and are unit-tested with plain JUnit.
 */
public class FloatingStatusController {

    private static final String TAG = "FloatingStatusCtrl";

    private static final String PREFS_NAME = "floating_window_prefs";
    private static final String KEY_X = "floating_window_x";
    private static final String KEY_Y = "floating_window_y";
    private static final String KEY_RATIO_X = "floating_window_ratio_x";
    private static final String KEY_RATIO_Y = "floating_window_ratio_y";

    /** How long the "done" terminal state stays visible before fading out. */
    private static final long TERMINAL_SHOW_MS = 3000;
    private static final long FADE_MS = 300;
    private static final int EDGE_MARGIN_DP = 8;
    /** Fallback capsule width estimate (dp) used before the view is measured. */
    private static final int DEFAULT_CAPSULE_WIDTH_DP = 120;
    /** Drag opacity while moving. */
    private static final float DRAG_ALPHA = 0.85f;

    /** Capsule tap opens the single running session directly. */
    public static final int CLICK_OPEN_SESSION = 0;
    /** Capsule tap expands the panel to show the session list. */
    public static final int CLICK_EXPAND_PANEL = 1;

    private final Context context;
    private final Runnable onTap;
    private final WindowManager windowManager;
    private final Handler handler;
    private final SharedPreferences prefs;
    private final int edgeMarginPx;
    private final int capsuleWidthPx;

    private FloatingStatusView view;
    private WindowManager.LayoutParams params;
    private volatile boolean windowShowing;
    private volatile boolean destroyed;
    private volatile boolean hasActive;
    private volatile boolean appForeground;
    private volatile boolean userDismissed;
    private Runnable fadeHideRunnable;

    /** Session ids currently running, tracked from events. Thread-safe set. */
    private final java.util.Set<String> runningSessions =
            java.util.concurrent.ConcurrentHashMap.newKeySet();

    // Drag bookkeeping.
    private float downX;
    private float downY;
    private int dragStartX;
    private int dragStartY;
    private boolean dragging;
    private final int touchSlop;

    /**
     * Whether an event/status pair represents an "active" (in-progress) state
     * that should keep the floating window visible. Pure: no framework deps.
     */
    public static boolean isActiveStatus(String eventType, String status) {
        if ("session_update".equals(eventType)) {
            return "running".equals(status) || "permission_pending".equals(status);
        }
        if ("task_update".equals(eventType)) {
            return "running".equals(status);
        }
        return false;
    }

    /**
     * Whether the floating window should be shown right now. Pure: no framework
     * deps. The window is only meaningful while the app is in the background,
     * there is an active task, and the user has not dismissed it.
     */
    public static boolean shouldShow(boolean appForeground, boolean hasActive, boolean userDismissed) {
        return !appForeground && hasActive && !userDismissed;
    }

    /**
     * Compute the window x coordinate (left edge, gravity TOP|START) snapped to
     * the left or right edge of the screen with the given margin. Clamps so the
     * capsule right edge never exceeds the screen. Pure: no framework deps.
     */
    public static int snapX(int screenWidth, int viewWidth, int margin, boolean right) {
        if (right) {
            int x = screenWidth - viewWidth - margin;
            return x < margin ? margin : x;
        }
        return margin;
    }

    /**
     * Decide what a capsule tap does: open the single running session, or
     * expand the panel. Pure: no framework deps.
     */
    public static int decideCapsuleClick(int runningSessionCount) {
        return runningSessionCount == 1 ? CLICK_OPEN_SESSION : CLICK_EXPAND_PANEL;
    }

    /**
     * Track session running state from events. Adds the session when an event
     * reports it as active, removes it on a terminal status (completed /
     * cancelled / failed). Any thread.
     */
    public void trackSessionState(String eventType, String status, String sessionId) {
        if (sessionId == null || sessionId.isEmpty()) {
            return;
        }
        if (isActiveStatus(eventType, status)) {
            runningSessions.add(sessionId);
        } else if ("completed".equals(status) || "cancelled".equals(status)
                || "failed".equals(status)) {
            runningSessions.remove(sessionId);
        }
    }

    /** Number of currently running sessions. */
    public int getRunningSessionCount() {
        return runningSessions.size();
    }

    /**
     * True when a capsule tap should open the running session directly rather
     * than expand the panel. Backs the Task 5 onTap wiring.
     */
    public boolean shouldOpenSessionOnCapsuleTap() {
        return decideCapsuleClick(getRunningSessionCount()) == CLICK_OPEN_SESSION;
    }

    public FloatingStatusController(Context context, Runnable onTap) {
        this.context = context.getApplicationContext();
        this.onTap = onTap;
        this.windowManager = (WindowManager) context.getSystemService(Context.WINDOW_SERVICE);
        this.handler = new Handler(Looper.getMainLooper());
        this.prefs = this.context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
        this.touchSlop = ViewConfiguration.get(this.context).getScaledTouchSlop();
        this.edgeMarginPx = Math.round(EDGE_MARGIN_DP
                * this.context.getResources().getDisplayMetrics().density);
        this.capsuleWidthPx = Math.round(DEFAULT_CAPSULE_WIDTH_DP
                * this.context.getResources().getDisplayMetrics().density);
    }

    /**
     * Handle an incoming event. Parses status / session_id / session_title /
     * tool_name / response_preview_plain and drives window visibility. Safe to
     * call from any thread; View updates are posted to the UI thread.
     */
    public void handleEvent(String eventType, JSONObject data) {
        if (data == null) {
            return;
        }
        String status = data.optString("status", "");
        String sessionId = data.optString("session_id", "");
        String sessionTitle = data.optString("session_title", "");
        String toolName = data.optString("tool_name", "");
        String preview = data.optString("response_preview_plain", "");

        boolean active = isActiveStatus(eventType, status);
        hasActive = active;
        trackSessionState(eventType, status, sessionId);

        AppLog.d(TAG, "handleEvent event=" + eventType + " status=" + status
                + " sessionId=" + sessionId + " active=" + active);

        final String fEventType = eventType;
        final String fStatus = status;
        final String fSessionTitle = sessionTitle;
        final String fToolName = toolName;
        final String fPreview = preview;
        postToUi(() -> {
            cancelPendingHide();
            if (active) {
                if (shouldShow(appForeground, true, userDismissed)) {
                    ensureWindow();
                    render(fEventType, fStatus, fSessionTitle, fToolName, fPreview);
                }
            } else {
                // Terminal state: show the "done" capsule briefly, then fade out.
                if (windowShowing) {
                    render(fEventType, fStatus, fSessionTitle, fToolName, fPreview);
                    scheduleTerminalHide();
                }
            }
        });
    }

    /** App foreground state changes drive visibility directly. Any thread. */
    public void setAppForeground(boolean foreground) {
        appForeground = foreground;
        postToUi(() -> {
            if (foreground) {
                hideWindow();
            } else if (shouldShow(false, hasActive, userDismissed)) {
                cancelPendingHide();
                ensureWindow();
                if (view != null) {
                    view.pulse();
                }
            }
        });
    }

    /** Mark the window user-dismissed for the rest of this lifecycle. Any thread. */
    public void setUserDismissed(boolean dismissed) {
        userDismissed = dismissed;
        postToUi(() -> {
            if (dismissed) {
                cancelPendingHide();
                hideWindow();
            } else if (shouldShow(appForeground, hasActive, false)) {
                // Re-evaluate: un-dismissing should restore the window if conditions hold.
                cancelPendingHide();
                ensureWindow();
                if (view != null) {
                    view.pulse();
                }
            }
        });
    }

    public boolean isWindowShowing() {
        return windowShowing;
    }

    /**
     * True when a /api/sessions response contains at least one running session.
     * Pure function (no Android framework deps) so it is unit-testable.
     */
    public static boolean hasRunningSession(JSONObject sessionsResponse) {
        if (sessionsResponse == null) {
            return false;
        }
        JSONArray sessions = sessionsResponse.optJSONArray("sessions");
        if (sessions == null) {
            return false;
        }
        for (int i = 0; i < sessions.length(); i++) {
            JSONObject s = sessions.optJSONObject(i);
            if (s != null && s.optBoolean("running", false)) {
                return true;
            }
        }
        return false;
    }

    /**
     * Notify the controller that a session is running (discovered via the
     * /api/sessions poll on native WS connect). Marks the session active so the
     * capsule appears when the app is backgrounded. Any thread.
     */
    public void notifyRunningSession(String sessionId, String sessionTitle) {
        hasActive = true;
        if (sessionId != null && !sessionId.isEmpty()) {
            runningSessions.add(sessionId);
        }
        final String fSessionId = sessionId;
        final String fSessionTitle = sessionTitle;
        postToUi(() -> {
            cancelPendingHide();
            if (shouldShow(appForeground, true, userDismissed)) {
                ensureWindow();
                render("session_update", "running", fSessionTitle, "", "");
            } else if (appForeground) {
                // Backgrounded later; setAppForeground(false) will re-evaluate.
            }
        });
    }

    /** Remove the window and cancel all pending callbacks. Any thread. */
    public void destroy() {
        destroyed = true;
        runningSessions.clear();
        // Bypass postToUi's destroyed guard here: the guard must drop event
        // runnables, but it must NOT drop our own teardown, otherwise the
        // window is never removed from the WindowManager.
        Runnable cleanup = () -> {
            cancelPendingHide();
            if (view != null) {
                view.animate().cancel();
            }
            hideWindow();
            view = null;
            params = null;
        };
        if (Looper.myLooper() == Looper.getMainLooper()) {
            cleanup.run();
        } else {
            handler.post(cleanup);
        }
    }

    // --- UI-thread window management ---

    private void postToUi(Runnable r) {
        if (destroyed) {
            return;
        }
        Runnable guarded = () -> {
            // Re-check at execution time: a runnable queued before destroy()
            // (e.g. a handleEvent posted from the native WS thread) executes
            // AFTER destroy()'s synchronous cleanup on the main thread, and
            // must not resurrect the window via ensureWindow().
            if (destroyed) {
                return;
            }
            r.run();
        };
        if (Looper.myLooper() == Looper.getMainLooper()) {
            guarded.run();
        } else {
            handler.post(guarded);
        }
    }

    private void ensureWindow() {
        if (windowShowing) {
            // A fade may be in flight (alpha < 1); cancel it and restore full
            // opacity so a fresh active event makes the window reappear.
            view.animate().cancel();
            view.setAlpha(1f);
            return;
        }
        if (!canDrawOverlays()) {
            AppLog.w(TAG, "SYSTEM_ALERT_WINDOW not granted, floating window no-op");
            return;
        }
        try {
            if (view == null) {
                view = new FloatingStatusView(context);
                attachTouchListener(view);
                params = buildLayoutParams();
                restorePosition(params);
            }
            windowManager.addView(view, params);
            windowShowing = true;
            // Ensure right-edge placement accounts for the real capsule width now
            // that the view is laid out (no-op when a saved position is in effect).
            snapRightEdgeIfNeeded();
            view.setAlpha(1f);
            view.pulse();
            AppLog.i(TAG, "floating window shown at x=" + params.x + " y=" + params.y);
        } catch (Exception e) {
            AppLog.w(TAG, "failed to add floating window", e);
        }
    }

    private void hideWindow() {
        if (!windowShowing || view == null) {
            return;
        }
        try {
            windowManager.removeView(view);
            windowShowing = false;
            AppLog.i(TAG, "floating window hidden");
        } catch (Exception e) {
            AppLog.w(TAG, "failed to remove floating window", e);
        }
    }

    private void hideWithFade() {
        if (!windowShowing || view == null) {
            return;
        }
        view.animate()
                .alpha(0f)
                .setDuration(FADE_MS)
                .withEndAction(() -> {
                    hideWindow();
                    if (view != null) {
                        view.setAlpha(1f);
                    }
                })
                .start();
    }

    private void render(String eventType, String status, String sessionTitle, String toolName, String preview) {
        if (view != null) {
            view.render(eventType, status, sessionTitle, toolName, preview);
        }
    }

    private void cancelPendingHide() {
        if (fadeHideRunnable != null) {
            handler.removeCallbacks(fadeHideRunnable);
            fadeHideRunnable = null;
        }
    }

    private void scheduleTerminalHide() {
        fadeHideRunnable = () -> {
            fadeHideRunnable = null;
            if (shouldShow(appForeground, hasActive, userDismissed)) {
                // A newer active event superseded the terminal display.
                return;
            }
            hideWithFade();
        };
        handler.postDelayed(fadeHideRunnable, TERMINAL_SHOW_MS);
    }

    private boolean canDrawOverlays() {
        return Build.VERSION.SDK_INT < Build.VERSION_CODES.M || Settings.canDrawOverlays(context);
    }

    private WindowManager.LayoutParams buildLayoutParams() {
        int type = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                ? WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY
                : WindowManager.LayoutParams.TYPE_PHONE;
        int flags = WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE
                | WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL;
        WindowManager.LayoutParams lp = new WindowManager.LayoutParams(
                WindowManager.LayoutParams.WRAP_CONTENT,
                WindowManager.LayoutParams.WRAP_CONTENT,
                type,
                flags,
                PixelFormat.TRANSLUCENT);
        lp.gravity = Gravity.TOP | Gravity.START;
        // Default: right edge (estimate before view is measured), vertically centered.
        lp.x = snapX(screenWidth(), capsuleWidthPx, edgeMarginPx, true);
        lp.y = screenHeight() / 2;
        return lp;
    }

    private void restorePosition(WindowManager.LayoutParams lp) {
        // Ratio-based persistence is robust to screen size changes; clamp to
        // screen bounds so the capsule can never be dragged fully off-screen.
        int maxX = maxCapsuleX();
        float ratioX = prefs.getFloat(KEY_RATIO_X, -1f);
        float ratioY = prefs.getFloat(KEY_RATIO_Y, -1f);
        if (ratioX >= 0f && ratioY >= 0f) {
            lp.x = clamp(Math.round(ratioX * screenWidth()), edgeMarginPx, maxX);
            lp.y = clamp(Math.round(ratioY * screenHeight()), 0, screenHeight() - minCapsuleHeight());
        } else {
            int savedX = prefs.getInt(KEY_X, -1);
            int savedY = prefs.getInt(KEY_Y, -1);
            if (savedX >= 0 && savedY >= 0) {
                lp.x = clamp(savedX, edgeMarginPx, maxX);
                lp.y = clamp(savedY, 0, screenHeight() - minCapsuleHeight());
            }
        }
    }

    private void savePosition(int x, int y) {
        int width = screenWidth();
        int height = screenHeight();
        prefs.edit()
                .putFloat(KEY_RATIO_X, width > 0 ? (float) x / width : 0f)
                .putFloat(KEY_RATIO_Y, height > 0 ? (float) y / height : 0f)
                .putInt(KEY_X, x)
                .putInt(KEY_Y, y)
                .apply();
    }

    private void attachTouchListener(final FloatingStatusView v) {
        v.setOnTouchListener((view, event) -> {
            switch (event.getActionMasked()) {
                case MotionEvent.ACTION_DOWN:
                    downX = event.getRawX();
                    downY = event.getRawY();
                    dragStartX = params.x;
                    dragStartY = params.y;
                    dragging = false;
                    return true;
                case MotionEvent.ACTION_MOVE:
                    float dx = event.getRawX() - downX;
                    float dy = event.getRawY() - downY;
                    if (!dragging && Math.hypot(dx, dy) > touchSlop) {
                        dragging = true;
                        view.setAlpha(DRAG_ALPHA);
                    }
                    if (dragging && params != null) {
                        params.x = dragStartX + Math.round(dx);
                        params.y = dragStartY + Math.round(dy);
                        try {
                            windowManager.updateViewLayout(view, params);
                        } catch (IllegalArgumentException e) {
                            AppLog.w(TAG, "updateViewLayout failed", e);
                        }
                    }
                    return true;
                case MotionEvent.ACTION_UP:
                    if (dragging) {
                        snapToEdge();
                    } else if (onTap != null) {
                        onTap.run();
                    }
                    return true;
                case MotionEvent.ACTION_CANCEL:
                    if (dragging) {
                        snapToEdge();
                    }
                    return true;
            }
            return false;
        });
    }

    private void snapToEdge() {
        if (params == null || view == null) {
            return;
        }
        view.setAlpha(1f);
        int width = screenWidth();
        // Snap to nearest left/right edge with a small margin.
        boolean toLeft = params.x < width / 2;
        params.x = snapX(width, capsuleWidth(), edgeMarginPx, !toLeft);
        params.y = clamp(params.y, 0, screenHeight() - minCapsuleHeight());
        try {
            windowManager.updateViewLayout(view, params);
        } catch (IllegalArgumentException e) {
            AppLog.w(TAG, "snap updateViewLayout failed", e);
        }
        savePosition(params.x, params.y);
        AppLog.d(TAG, "snapped x=" + params.x + " y=" + params.y + " toLeft=" + toLeft);
    }

    private int screenWidth() {
        return context.getResources().getDisplayMetrics().widthPixels;
    }

    private int screenHeight() {
        return context.getResources().getDisplayMetrics().heightPixels;
    }

    private int minCapsuleHeight() {
        return Math.round(36f * context.getResources().getDisplayMetrics().density);
    }

    /** Real capsule width when measured, otherwise the default estimate. */
    private int capsuleWidth() {
        if (view != null && view.getMeasuredWidth() > 0) {
            return view.getMeasuredWidth();
        }
        return capsuleWidthPx;
    }

    /** Largest left-edge x that keeps the capsule fully on-screen. */
    private int maxCapsuleX() {
        return Math.max(edgeMarginPx, screenWidth() - capsuleWidth() - edgeMarginPx);
    }

    /**
     * If the window sits at the right-edge default placement (x == screen - margin
     * with no persisted position), re-snap it so the right edge lands inside the
     * screen using the real measured capsule width.
     */
    private void snapRightEdgeIfNeeded() {
        if (view == null || params == null || !windowShowing) {
            return;
        }
        int measured = view.getMeasuredWidth();
        if (measured <= 0) {
            return;
        }
        if (params.x >= screenWidth() - capsuleWidthPx - edgeMarginPx) {
            int snapped = snapX(screenWidth(), measured, edgeMarginPx, true);
            if (snapped != params.x) {
                params.x = snapped;
                try {
                    windowManager.updateViewLayout(view, params);
                } catch (IllegalArgumentException e) {
                    AppLog.w(TAG, "snapRightEdgeIfNeeded updateViewLayout failed", e);
                }
                AppLog.d(TAG, "right-edge default snapped to x=" + params.x);
            }
        }
    }

    private static int clamp(int value, int min, int max) {
        if (value < min) return min;
        if (value > max) return max;
        return value;
    }
}
