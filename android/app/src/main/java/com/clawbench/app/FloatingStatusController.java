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

import java.util.function.BiConsumer;

/**
 * Controller for the desktop floating status window.
 *
 * Owns the WindowManager / FloatingStatusView / LayoutParams triple: decides
 * when to show or hide the capsule based on incoming session_update /
 * task_update events, app foreground state, and user dismissal. Handles
 * drag-to-snap positioning (persisted to SharedPreferences) and tap-to-open.
 *
 * Capsule taps always expand the grouped panel (the panel's session rows are
 * the single tap-to-open entry point, carrying session id + project path).
 *
 * The panel's height follows its content: after each render the panel is
 * measured and the window height is updated to min(content, screen), so a few
 * sessions show a compact panel and a long list scrolls inside a full-height
 * window.
 *
 * handleEvent / setAppForeground / setUserDismissed are safe to call from any
 * thread; all WindowManager and View mutations are marshalled to the UI
 * thread via view.post(Runnable). The static pure functions isActiveStatus /
 * shouldShow / panelHeightForContent have no framework dependency and are
 * unit-tested with plain JUnit.
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

    /** Panel width in dp (matches FloatingStatusPanelView). */
    private static final int PANEL_WIDTH_DP = 280;
    /** Minimum interval between overview refreshes triggered by events while expanded. */
    private static final long OVERVIEW_REFRESH_MIN_INTERVAL_MS = 2000;

    private final Context context;
    private final WindowManager windowManager;
    private final Handler handler;
    private final SharedPreferences prefs;
    private final int edgeMarginPx;
    private final int capsuleWidthPx;

    private FloatingStatusView view;
    private FloatingStatusPanelView panelView;
    private WindowManager.LayoutParams params;
    /** The view currently attached to the WindowManager (capsule or panel). */
    private View attachedView;
    private volatile boolean windowShowing;
    private volatile boolean destroyed;
    private volatile boolean hasActive;
    private volatile boolean appForeground;
    private volatile boolean userDismissed;
    private volatile boolean expanded;
    private Runnable fadeHideRunnable;
    private BiConsumer<String, String> onSessionClick;
    private OverviewRequestListener overviewRequestListener;
    /** Last time an event-triggered overview refresh was requested (throttle). */
    private volatile long lastOverviewRequestMs;

    /** Session ids currently running, tracked from events. Thread-safe set. */
    private final java.util.Set<String> runningSessions =
            java.util.concurrent.ConcurrentHashMap.newKeySet();

    /**
     * Session ids currently awaiting approval, tracked from events. Thread-safe
     * set. Kept in parallel with runningSessions so the capsule can show a
     * live pending count without waiting for an overview round trip.
     */
    private final java.util.Set<String> pendingSessions =
            java.util.concurrent.ConcurrentHashMap.newKeySet();

    /**
     * Number of sessions with unread messages, as of the last overview. Events
     * carry no unread data, so this is the best the capsule can show between
     * overview refreshes; onOverviewLoaded corrects it.
     */
    private volatile int lastUnreadCount;

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
     * Clamp a panel content height to the screen. Pure: no framework deps.
     * Returns 0 when either input is non-positive so a malformed measure can
     * never drive the window to a negative size.
     */
    public static int panelHeightForContent(int contentHeight, int screenHeight) {
        if (contentHeight <= 0 || screenHeight <= 0) {
            return 0;
        }
        return Math.min(contentHeight, screenHeight);
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
     * Track session running state from events. Adds the session when an event
     * reports it as active, removes it on a terminal status (completed /
     * cancelled / failed). Any thread.
     */
    public void trackSessionState(String eventType, String status, String sessionId) {
        // Late events arriving after destroy() must not resurrect the cleared
        // running set. This guard runs before any mutation; the postToUi
        // dropped-runnable guard only protects the window, not this set.
        if (destroyed) {
            return;
        }
        // Only session_update events feed the running set. task_update is a
        // scheduled-task status, not a session status: its session_id is
        // omitempty (often empty) and would desync the count from hasActive.
        if (!"session_update".equals(eventType)) {
            return;
        }
        if (sessionId == null || sessionId.isEmpty()) {
            return;
        }
        if (isActiveStatus(eventType, status)) {
            runningSessions.add(sessionId);
            if ("permission_pending".equals(status)) {
                pendingSessions.add(sessionId);
            }
        } else if ("completed".equals(status) || "cancelled".equals(status)
                || "failed".equals(status)) {
            runningSessions.remove(sessionId);
            pendingSessions.remove(sessionId);
        } else {
            // permission_resolved leaves the session still running, so it must
            // NOT be removed from the set here.
            if ("permission_resolved".equals(status)) {
                pendingSessions.remove(sessionId);
            }
        }
    }

    /** Number of currently running sessions. */
    public int getRunningSessionCount() {
        return runningSessions.size();
    }

    /** Whether the grouped session panel is currently expanded. Any thread. */
    public boolean isExpanded() {
        return expanded;
    }

    /**
     * Callback the controller invokes when the panel needs a fresh overview
     * (on expand, and on every session/task event while expanded). The service
     * wires this to fetchOverviewSessions on the network executor.
     */
    public interface OverviewRequestListener {
        void onRequestOverview();
    }

    /**
     * Public entry point for a capsule tap: always expand the grouped panel.
     * Session-specific open actions happen through the panel's session rows
     * (which carry the tapped session id + project path). Any thread.
     */
    public void onCapsuleTap() {
        setExpanded(true);
    }

    /**
     * Expand to the grouped session list panel, or collapse back to the
     * capsule. Any thread; View/WindowManager mutations are marshalled to the
     * UI thread.
     */
    public void setExpanded(boolean expand) {
        expanded = expand;
        postToUi(() -> {
            if (expand) {
                // The capsule may be hidden (e.g. no active session, panel
                // showing only unread items): ensure the window exists.
                ensureWindow();
                if (windowShowing) {
                    attachView(panelView != null ? panelView : buildPanelView());
                    // A panel is much wider than the capsule: the capsule's
                    // right-edge placement would push the panel off-screen, so
                    // re-clamp x against the real panel width.
                    clampPanelX();
                    // Fit the (initially empty) panel before the first overview
                    // arrives so the header-only window is compact.
                    resizePanelIfNeeded();
                }
                requestOverviewRefresh();
            } else {
                // Collapse: back to the capsule if a session is still active,
                // otherwise hide the window entirely.
                if (shouldShow(appForeground, hasActive, userDismissed)) {
                    attachView(view != null ? view : buildCapsuleView());
                } else {
                    hideWindow();
                }
                if (panelView != null) {
                    panelView.setOnCollapseClickListener(null);
                    panelView = null;
                }
            }
        });
    }

    /**
     * Render overview data into the expanded panel. Any thread; the render is
     * marshalled to the UI thread and no-ops when the panel is not expanded.
     *
     * The overview also re-seeds the running session set: on WS connect it is
     * the fallback for a "running" session_update that was broadcast while the
     * WS was down, so the capsule still appears. Running ids are added (never
     * removed — the event stream remains authoritative for termination).
     */
    public void onOverviewLoaded(JSONObject overview) {
        if (overview == null) {
            return;
        }
        seedRunningFromOverview(overview);
        lastUnreadCount = FloatingStatusView.countUnread(overview);
        postToUi(() -> {
            if (panelView != null && expanded) {
                panelView.render(overview, (sid, projectPath) -> {
                    if (sid != null && !sid.isEmpty()) {
                        // Opening a specific session: deliver it to the service
                        // deep-link and collapse the panel.
                        if (onSessionClick != null) {
                            onSessionClick.accept(sid, projectPath);
                        }
                        setExpanded(false);
                    }
                });
                // The overview changed the panel's content (group/session
                // count), so re-fit the window height to the new content.
                resizePanelIfNeeded();
            } else if (shouldShow(appForeground, hasActive, userDismissed) && !windowShowing) {
                // WS-connect fallback: a running session discovered via the
                // overview (whose start event was missed while the WS was down)
                // must bring up the capsule even though no event triggered it.
                cancelPendingHide();
                ensureWindow();
            }
            // The stats capsule always reflects the latest overview so its
            // counts stay current on collapse and right after a fallback build.
            if (view != null) {
                int[] stats = computeStats(overview);
                view.renderStats(stats[0], stats[1], stats[2]);
            }
        });
    }

    /**
     * Add sessions flagged running or pending-approval by the overview into
     * their tracked sets. Any thread. No-op when the overview is malformed.
     */
    private void seedRunningFromOverview(JSONObject overview) {
        JSONArray projects = overview.optJSONArray("projects");
        if (projects == null) {
            return;
        }
        boolean anyRunning = false;
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
                JSONObject s = sessions.optJSONObject(j);
                if (s == null) {
                    continue;
                }
                String id = s.optString("id", "");
                if (id.isEmpty()) {
                    continue;
                }
                boolean pending = s.optBoolean("pendingApproval", false);
                boolean running = s.optBoolean("running", false);
                if (pending) {
                    // Pending wins over running (yellow > green), matching the
                    // panel's status-dot priority and the capsule's mutual
                    // exclusion between the running and pending groups.
                    runningSessions.add(id);
                    pendingSessions.add(id);
                    anyRunning = true;
                } else if (running) {
                    runningSessions.add(id);
                    anyRunning = true;
                }
            }
        }
        if (anyRunning) {
            hasActive = true;
        } else if (hasActive && overview.optInt("total", 0) == 0) {
            // No running session anywhere in the overview and nothing left
            // worth showing (total counts unread / pending-approval items too):
            // every session ended while the WS was down, so reset hasActive and
            // hide the lingering window. total > 0 means unread or pending
            // items remain — keep the window.
            hasActive = false;
            postToUi(() -> {
                if (!expanded) {
                    cancelPendingHide();
                    hideWindow();
                }
            });
        }
    }

    /**
     * Compute the capsule's three stats from an overview JSON object:
     * {running, pending, unread}. The groups are mutually exclusive —
     * pendingApproval wins over running for a session that has both flags
     * (matching the panel's yellow > green status-dot priority), and unread
     * only counts sessions that are neither running nor pending. Pure: only
     * org.json, so unit-testable with plain JUnit.
     */
    public static int[] computeStats(JSONObject overview) {
        return new int[]{
                FloatingStatusView.countRunning(overview),
                FloatingStatusView.countPending(overview),
                FloatingStatusView.countUnread(overview)
        };
    }

    /**
     * Callback invoked when a session row is tapped in the expanded panel.
     * Carries the session id and its owning project path so the service can
     * deep-link into it. projectPath may be null/empty for rows without a group.
     */
    public void setOnSessionClick(BiConsumer<String, String> listener) {
        this.onSessionClick = listener;
    }

    /**
     * Callback invoked when the controller needs a fresh overview (expand +
     * every event while expanded). The service pulls /api/ai/sessions/overview
     * on the network executor and feeds the result back via onOverviewLoaded.
     */
    public void setOverviewRequestListener(OverviewRequestListener listener) {
        this.overviewRequestListener = listener;
    }

    public FloatingStatusController(Context context) {
        this.context = context.getApplicationContext();
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

        boolean active = isActiveStatus(eventType, status);
        // trackSessionState must run before deriving hasActive: a terminal
        // event for one session must not clear hasActive while other sessions
        // are still running in the tracked set.
        trackSessionState(eventType, status, sessionId);
        hasActive = active || !runningSessions.isEmpty();

        // While the panel is expanded, every event should refresh the overview
        // so the session list stays current without waiting for the next tap.
        // High-frequency streaming events would otherwise pile up requests, so
        // the refresh is throttled to OVERVIEW_REFRESH_MIN_INTERVAL_MS.
        if (expanded) {
            requestOverviewRefresh();
        }

        AppLog.d(TAG, "handleEvent event=" + eventType + " status=" + status
                + " sessionId=" + sessionId + " active=" + active);

        postToUi(() -> {
            cancelPendingHide();
            if (active) {
                if (shouldShow(appForeground, true, userDismissed)) {
                    ensureWindow();
                    // Render the capsule instantly from locally tracked state
                    // (running/pending sets, last overview's unread count) so a
                    // session start is visible without waiting for the overview
                    // network round trip; the refresh then corrects all counts.
                    renderCapsuleStats();
                    requestOverviewRefresh();
                }
            } else {
                // Terminal state: show the "done" capsule briefly, then fade out.
                // While the panel is expanded the user is looking at the list,
                // so never auto-hide it; the overview refresh keeps it current.
                if (windowShowing && !expanded) {
                    if (!runningSessions.isEmpty()) {
                        // Other sessions are still running: keep the capsule up
                        // with the updated counts (terminal event may have
                        // dropped this session's running/pending state).
                        renderCapsuleStats();
                        // The overview refresh re-seeds the running set and the
                        // fresh overview drives the capsule's stats.
                        requestOverviewRefresh();
                    } else {
                        // Last session ended: reflect the empty counts instantly
                        // (breathing stops, groups hide) before the fade-out.
                        renderCapsuleStats();
                        requestOverviewRefresh();
                        scheduleTerminalHide();
                    }
                }
            }
        });
    }

    /**
     * Render the capsule stats from locally tracked state, without waiting for
     * an overview round trip. The running count is the tracked running set
     * minus the pending set (pending wins over running, matching the overview
     * grouping); the unread count is whatever the last overview reported, since
     * events carry no unread data. onOverviewLoaded corrects all three. Any
     * thread; marshalled to the UI thread.
     */
    private void renderCapsuleStats() {
        postToUi(() -> {
            if (view != null) {
                int running = Math.max(0, runningSessions.size() - pendingSessions.size());
                view.renderStats(running, pendingSessions.size(), lastUnreadCount);
            }
        });
    }

    /** App foreground state changes drive visibility directly. Any thread. */
    public void setAppForeground(boolean foreground) {
        appForeground = foreground;
        if (foreground) {
            // Returning to the foreground hides the window but must also reset
            // the expanded state: otherwise a later background would rebuild the
            // stale panel (with old content) instead of the capsule. The panel
            // view is dropped too so the next expand rebuilds it fresh.
            expanded = false;
            panelView = null;
        }
        postToUi(() -> {
            if (foreground) {
                hideWindow();
            } else if (shouldShow(false, hasActive, userDismissed)) {
                cancelPendingHide();
                ensureWindow();
                renderCapsuleStats();
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
                renderCapsuleStats();
            }
        });
    }

    public boolean isWindowShowing() {
        return windowShowing;
    }

    /** Remove the window and cancel all pending callbacks. Any thread. */
    public void destroy() {
        destroyed = true;
        runningSessions.clear();
        pendingSessions.clear();
        // Bypass postToUi's destroyed guard here: the guard must drop event
        // runnables, but it must NOT drop our own teardown, otherwise the
        // window is never removed from the WindowManager.
        Runnable cleanup = () -> {
            cancelPendingHide();
            if (view != null) {
                view.animate().cancel();
                view.stopBreathing();
            }
            if (panelView != null) {
                panelView.setOnCollapseClickListener(null);
                panelView.stopBreathing();
            }
            hideWindow();
            view = null;
            panelView = null;
            attachedView = null;
            params = null;
        };
        if (Looper.myLooper() == Looper.getMainLooper()) {
            cleanup.run();
        } else {
            handler.post(cleanup);
        }
    }

    // --- UI-thread window management ---

    /**
     * Request a fresh overview. Expand requests always fire; event-triggered
     * requests are throttled so high-frequency streaming events cannot pile up
     * network pulls. Any thread.
     */
    private void requestOverviewRefresh() {
        if (overviewRequestListener == null) {
            return;
        }
        long now = android.os.SystemClock.elapsedRealtime();
        // lastOverviewRequestMs == 0 means "never requested" — always fire so
        // the expand request is never swallowed by the throttle.
        if (lastOverviewRequestMs != 0
                && now - lastOverviewRequestMs < OVERVIEW_REFRESH_MIN_INTERVAL_MS) {
            return;
        }
        lastOverviewRequestMs = now;
        overviewRequestListener.onRequestOverview();
    }

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
            if (attachedView != null) {
                attachedView.animate().cancel();
                attachedView.setAlpha(1f);
            }
            return;
        }
        if (!canDrawOverlays()) {
            AppLog.w(TAG, "SYSTEM_ALERT_WINDOW not granted, floating window no-op");
            return;
        }
        try {
            if (params == null) {
                params = buildLayoutParams();
                restorePosition(params);
            }
            if (attachedView == null) {
                attachView(expanded ? buildPanelView() : buildCapsuleView());
            }
            applyViewSizing();
            windowManager.addView(attachedView, params);
            windowShowing = true;
            // Ensure right-edge placement accounts for the real view width now
            // that it is laid out (no-op when a saved position is in effect).
            snapRightEdgeIfNeeded();
            attachedView.setAlpha(1f);
            if (attachedView instanceof FloatingStatusView) {
                // The capsule's stats render on the next event / overview; the
                // breathing animation is driven by renderStats, so nothing to
                // start here.
                renderCapsuleStats();
            }
            AppLog.i(TAG, "floating window shown at x=" + params.x + " y=" + params.y
                    + (expanded ? " (panel)" : " (capsule)"));
        } catch (Exception e) {
            AppLog.w(TAG, "failed to add floating window", e);
        }
    }

    private void hideWindow() {
        if (!windowShowing || attachedView == null) {
            return;
        }
        try {
            windowManager.removeView(attachedView);
            windowShowing = false;
            attachedView = null;
            AppLog.i(TAG, "floating window hidden");
        } catch (Exception e) {
            AppLog.w(TAG, "failed to remove floating window", e);
        }
    }

    /**
     * Swap the view attached to the WindowManager (capsule <-> panel) while the
     * window is visible. Reuses the existing LayoutParams so drag position is
     * preserved. UI thread only. When the window is not showing, just records
     * which view should be added on the next ensureWindow().
     */
    private void attachView(View newView) {
        if (newView == null) {
            return;
        }
        if (newView == attachedView) {
            return;
        }
        if (windowShowing && attachedView != null) {
            try {
                windowManager.removeView(attachedView);
            } catch (Exception e) {
                AppLog.w(TAG, "failed to remove old floating view", e);
            }
        }
        attachedView = newView;
        applyViewSizing();
        if (windowShowing) {
            try {
                windowManager.addView(attachedView, params);
                if (attachedView instanceof FloatingStatusView) {
                    renderCapsuleStats();
                }
            } catch (Exception e) {
                AppLog.w(TAG, "failed to add swapped floating view", e);
            }
        }
    }

    /**
     * Size the attached view for its role: the panel gets a fixed 280dp-wide
     * window (height is content-driven, see resizePanelIfNeeded), the capsule
     * stays wrap-content. UI thread only.
     */
    private void applyViewSizing() {
        if (params == null || attachedView == null) {
            return;
        }
        float density = context.getResources().getDisplayMetrics().density;
        if (expanded && attachedView == panelView) {
            params.width = Math.round(PANEL_WIDTH_DP * density);
            params.height = WindowManager.LayoutParams.WRAP_CONTENT;
        } else {
            params.width = WindowManager.LayoutParams.WRAP_CONTENT;
            params.height = WindowManager.LayoutParams.WRAP_CONTENT;
        }
    }

    /**
     * Fit the expanded panel's window height to its content: measure the
     * rendered content, clamp it to the screen, cap the inner scroll area, and
     * push the new height to the WindowManager. Called after every panel render
     * so a few sessions show a compact panel and a long list scrolls inside a
     * full-height window. UI thread only.
     */
    private void resizePanelIfNeeded() {
        if (panelView == null || params == null || attachedView != panelView) {
            return;
        }
        try {
            int contentHeight = panelView.measureContentHeight(params.width);
            int panelHeight = panelHeightForContent(contentHeight, screenHeight());
            if (panelHeight <= 0) {
                return;
            }
            panelView.constrainListHeight(panelHeight);
            params.height = panelHeight;
            if (windowShowing) {
                try {
                    windowManager.updateViewLayout(panelView, params);
                } catch (IllegalArgumentException e) {
                    AppLog.w(TAG, "resizePanelIfNeeded updateViewLayout failed", e);
                }
            }
            AppLog.d(TAG, "panel resized to height=" + panelHeight
                    + " (content=" + contentHeight + " screen=" + screenHeight() + ")");
        } catch (Exception e) {
            AppLog.w(TAG, "resizePanelIfNeeded failed", e);
        }
    }

    private FloatingStatusView buildCapsuleView() {
        view = new FloatingStatusView(context);
        attachTouchListener(view);
        return view;
    }

    private FloatingStatusPanelView buildPanelView() {
        // Session-row taps are delivered through the render() callback
        // (setOnSessionClick), so construction only wires the collapse button.
        panelView = new FloatingStatusPanelView(context, null);
        panelView.setOnCollapseClickListener(() -> setExpanded(false));
        // Panel content is dark/translucent; keep the drag alpha subtle.
        attachTouchListener(panelView);
        return panelView;
    }

    private void hideWithFade() {
        if (!windowShowing || attachedView == null) {
            return;
        }
        View fadeView = attachedView;
        fadeView.animate()
                .alpha(0f)
                .setDuration(FADE_MS)
                .withEndAction(() -> {
                    hideWindow();
                    fadeView.setAlpha(1f);
                })
                .start();
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

    private void attachTouchListener(final View v) {
        v.setOnTouchListener((touchedView, event) -> {
            switch (event.getActionMasked()) {
                case MotionEvent.ACTION_DOWN:
                    downX = event.getRawX();
                    downY = event.getRawY();
                    dragStartX = params != null ? params.x : 0;
                    dragStartY = params != null ? params.y : 0;
                    dragging = false;
                    return true;
                case MotionEvent.ACTION_MOVE:
                    float dx = event.getRawX() - downX;
                    float dy = event.getRawY() - downY;
                    if (!dragging && Math.hypot(dx, dy) > touchSlop) {
                        dragging = true;
                        touchedView.setAlpha(DRAG_ALPHA);
                    }
                    if (dragging && params != null) {
                        params.x = dragStartX + Math.round(dx);
                        params.y = dragStartY + Math.round(dy);
                        try {
                            windowManager.updateViewLayout(touchedView, params);
                        } catch (IllegalArgumentException e) {
                            AppLog.w(TAG, "updateViewLayout failed", e);
                        }
                    }
                    return true;
                case MotionEvent.ACTION_UP:
                    if (dragging) {
                        snapToEdge();
                    } else if (v == panelView) {
                        // Tap on empty panel space collapses it back to the capsule.
                        setExpanded(false);
                    } else {
                        // Capsule tap: always expand the panel.
                        onCapsuleTap();
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
        if (params == null || attachedView == null) {
            return;
        }
        attachedView.setAlpha(1f);
        int width = screenWidth();
        // Snap to nearest left/right edge with a small margin.
        boolean toLeft = params.x < width / 2;
        params.x = snapX(width, currentWidth(), edgeMarginPx, !toLeft);
        params.y = clamp(params.y, 0, screenHeight() - minCapsuleHeight());
        try {
            windowManager.updateViewLayout(attachedView, params);
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

    /** Real attached-view width when measured, otherwise the default estimate. */
    private int currentWidth() {
        if (attachedView != null && attachedView.getMeasuredWidth() > 0) {
            return attachedView.getMeasuredWidth();
        }
        if (expanded) {
            return Math.round(PANEL_WIDTH_DP * context.getResources().getDisplayMetrics().density);
        }
        return capsuleWidthPx;
    }

    /**
     * Clamp the panel's left edge so the whole panel stays on-screen. The
     * capsule default sits at the right edge (x = width - capsuleWidth -
     * margin), which would push the wider panel off-screen; re-clamping with
     * the real panel width keeps it fully visible. UI thread only.
     */
    private void clampPanelX() {
        if (attachedView == null || params == null || !windowShowing) {
            return;
        }
        int panelWidthPx = Math.round(PANEL_WIDTH_DP * context.getResources().getDisplayMetrics().density);
        int clamped = clamp(snapX(screenWidth(), panelWidthPx, edgeMarginPx, true),
                edgeMarginPx, screenWidth());
        if (clamped != params.x) {
            params.x = clamped;
            try {
                windowManager.updateViewLayout(attachedView, params);
            } catch (IllegalArgumentException e) {
                AppLog.w(TAG, "clampPanelX updateViewLayout failed", e);
            }
        }
    }

    /** Largest left-edge x that keeps the attached view fully on-screen. */
    private int maxCapsuleX() {
        return Math.max(edgeMarginPx, screenWidth() - currentWidth() - edgeMarginPx);
    }

    /**
     * If the window sits at the right-edge default placement (x == screen - margin
     * with no persisted position), re-snap it so the right edge lands inside the
     * screen using the real measured view width.
     */
    private void snapRightEdgeIfNeeded() {
        if (attachedView == null || params == null || !windowShowing) {
            return;
        }
        int measured = attachedView.getMeasuredWidth();
        if (measured <= 0) {
            return;
        }
        if (params.x >= screenWidth() - capsuleWidthPx - edgeMarginPx) {
            int snapped = snapX(screenWidth(), measured, edgeMarginPx, true);
            if (snapped != params.x) {
                params.x = snapped;
                try {
                    windowManager.updateViewLayout(attachedView, params);
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
