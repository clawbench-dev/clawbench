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
 * The window is only shown while it has something worth showing: the app is in
 * the background, a session is active (running / pending approval) or unread
 * items remain, and the user has not dismissed it. When the last session ends
 * and no unread items are left the window is hidden entirely, so an idle
 * background never leaves a meaningless capsule on screen.
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

    /** Fade-in duration when the window first appears. */
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
    private BiConsumer<String, String> onSessionClick;
    private OverviewRequestListener overviewRequestListener;
    /** Last time an event-triggered overview refresh was requested (throttle). */
    private volatile long lastOverviewRequestMs;
    /**
     * Whether an overview has ever been requested. Tracked separately from
     * lastOverviewRequestMs because 0 is a legitimate timestamp (elapsedRealtime
     * origin, as in tests) and must not be confused with "never requested".
     */
    private volatile boolean overviewRequested;

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
     * Session ids with unread messages, tracked from the last overview and
     * corrected by events between overviews. Thread-safe set. The capsule's
     * unread count is the set size; the set (not a bare counter) is what makes
     * the idle decision exact — a completion adds its session once, a "read"
     * event removes it, and a session that starts running again is dropped so
     * it cannot stay counted as unread.
     */
    private final java.util.Set<String> unreadSessions =
            java.util.concurrent.ConcurrentHashMap.newKeySet();

    /**
     * Scheduled-task ids currently running, tracked from task_update events.
     * Thread-safe set. Kept separate from runningSessions because the overview
     * covers chat sessions only — a running task must keep the window up, and
     * must not be wiped by an overview that knows nothing about it.
     */
    private final java.util.Set<String> runningTasks =
            java.util.concurrent.ConcurrentHashMap.newKeySet();

    /**
     * Guards every mutation of the tracked sets (runningSessions /
     * pendingSessions / unreadSessions / runningTasks / hasActive) together with
     * stateVersion, so an event's mutation and the version bump are atomic and
     * an overview response cannot interleave between a version check and the
     * set update it authorises.
     */
    private final Object stateLock = new Object();

    /**
     * Bumped whenever an event actually changes the tracked sets. An overview is
     * fetched asynchronously, so its response can be older than an event that
     * arrived while the request was in flight; comparing this counter against
     * the value captured when the request was issued tells the response whether
     * it is still current. Without this a stale snapshot could clear a session
     * that started mid-request and hide the window mid-session.
     */
    private final java.util.concurrent.atomic.AtomicLong stateVersion =
            new java.util.concurrent.atomic.AtomicLong();

    /**
     * Capture the tracked-state version before starting an overview fetch. The
     * service calls this at its single fetch choke point and passes the result
     * back to {@link #onOverviewLoaded(JSONObject, long)}, so every response is
     * matched to the state as it was when its own request went out. Any thread.
     */
    public long beginOverviewRequest() {
        return stateVersion.get();
    }

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
     * there is an active task or an unread session (either is worth drawing
     * the user's attention to), and the user has not dismissed it.
     */
    public static boolean shouldShow(boolean appForeground, boolean hasActive,
                                     boolean hasUnread, boolean userDismissed) {
        return !appForeground && (hasActive || hasUnread) && !userDismissed;
    }

    /** Number of sessions currently marked unread (tracked set size). */
    private int unreadCount() {
        return unreadSessions.size();
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
     * Track running state from events. Adds the id when an event reports it as
     * active, removes it on a terminal status (completed / cancelled / failed).
     * For session_update the id is the session id; for task_update it is the
     * task id (a scheduled task never enters the session sets — see below).
     * Any thread.
     *
     * The version bump is applied only when a set actually changes, and both
     * happen under {@link #stateLock} so an overview response can never observe
     * a half-applied event (see onOverviewLoaded).
     */
    public void trackSessionState(String eventType, String status, String id) {
        // Late events arriving after destroy() must not resurrect the cleared
        // running set. This guard runs before any mutation; the postToUi
        // dropped-runnable guard only protects the window, not this set.
        if (destroyed) {
            return;
        }
        if (id == null || id.isEmpty()) {
            return;
        }
        boolean changed = false;
        synchronized (stateLock) {
            if ("task_update".equals(eventType)) {
                // Scheduled tasks are tracked by task_id. They never enter the
                // session sets: the overview covers chat sessions only, so a
                // task id in runningSessions would be wiped by the next overview
                // and wrongly hide the window mid-task.
                if ("running".equals(status)) {
                    changed = runningTasks.add(id);
                } else if (isTerminalStatus(status)) {
                    changed = runningTasks.remove(id);
                }
            } else if ("session_update".equals(eventType)) {
                if (isActiveStatus(eventType, status)) {
                    changed = runningSessions.add(id);
                    // A running / pending session is excluded from the unread
                    // group (pending > running > unread), so it must not stay
                    // counted unread.
                    changed |= unreadSessions.remove(id);
                    if ("permission_pending".equals(status)) {
                        changed |= pendingSessions.add(id);
                    }
                } else if (isTerminalStatus(status)) {
                    changed = runningSessions.remove(id);
                    changed |= pendingSessions.remove(id);
                } else if ("permission_resolved".equals(status)) {
                    // The session is still running, so it must NOT be removed
                    // from runningSessions here.
                    changed = pendingSessions.remove(id);
                } else if ("read".equals(status)) {
                    // The session was read (possibly from another client): it is
                    // no longer unread. Applied here so the idle decision is
                    // correct even before the next overview lands.
                    changed = unreadSessions.remove(id);
                }
            }
            if (changed) {
                stateVersion.incrementAndGet();
            }
        }
    }

    /** A terminal session/task status — the run is no longer in progress. */
    private static boolean isTerminalStatus(String status) {
        return "completed".equals(status) || "cancelled".equals(status)
                || "failed".equals(status);
    }

    /**
     * Mark a session unread from an event. Any thread. Bumps the state version
     * (an in-flight overview response must not overwrite this), so it is
     * skipped for a blank id.
     */
    private void markSessionUnread(String sessionId) {
        if (sessionId == null || sessionId.isEmpty()) {
            return;
        }
        synchronized (stateLock) {
            if (unreadSessions.add(sessionId)) {
                stateVersion.incrementAndGet();
            }
        }
    }

    /**
     * Recompute {@code hasActive} from the tracked sets. Any thread. The
     * overview's apply path writes it directly under the same lock; this is the
     * event path's equivalent.
     */
    private void refreshHasActive(boolean eventActive) {
        synchronized (stateLock) {
            hasActive = eventActive || !runningSessions.isEmpty() || !runningTasks.isEmpty();
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
                    // Show skeleton rows immediately so the panel never sits
                    // blank while the overview round trip is in flight; the
                    // first onOverviewLoaded() render replaces them.
                    if (panelView != null) {
                        panelView.showSkeleton();
                        resizePanelIfNeeded();
                    }
                }
                // Force the overview fetch: the panel's session list depends on
                // it, and the throttled path could swallow the request if a
                // streaming event fired moments before the expand.
                requestOverviewRefresh(true);
            } else {
                // Collapse: back to the capsule if a session is still active or
                // unread items remain, otherwise hide the window entirely —
                // an idle window has nothing worth showing.
                if (shouldShow(appForeground, hasActive, unreadCount() > 0, userDismissed)) {
                    attachView(view != null ? view : buildCapsuleView());
                    renderCapsuleStats();
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
     * The overview also re-seeds the tracked sets: on WS connect it is the
     * fallback for a "running" session_update that was broadcast while the WS
     * was down, so the capsule still appears.
     *
     * @param requestVersion the {@link #beginOverviewRequest() tracked-state
     *                       version} captured when this response's request was
     *                       issued, or -1 when the caller did not record one
     *                       (only tests do that; every production path goes
     *                       through BackgroundService.fetchOverviewSessions,
     *                       which always captures a version). A response whose
     *                       version no longer matches is stale and discarded.
     */
    public void onOverviewLoaded(JSONObject overview, long requestVersion) {
        if (overview == null) {
            return;
        }
        // The overview is a server snapshot fetched asynchronously, so it can be
        // older than an event that arrived while the request was in flight.
        // Applying a stale snapshot is unsafe in BOTH directions: it could
        // resurrect a session an event just cleared (keeping the window up for a
        // finished session, showing a count the user already read) or drop a
        // session an event just started (hiding the window mid-session). So the
        // version is re-checked under the same lock that guards the sets: a
        // current response replaces them wholesale, a stale one is discarded and
        // a fresh fetch is requested instead. A caller that did not record a
        // version (-1) has nothing to compare against and is taken as current.
        boolean current;
        synchronized (stateLock) {
            current = requestVersion < 0 || stateVersion.get() == requestVersion;
            if (current) {
                applyOverviewLocked(overview);
            }
        }
        if (!current) {
            AppLog.d(TAG, "overview response stale (requested v" + requestVersion
                    + ", now v" + stateVersion.get() + "), discarded; refetching");
            // Converge: the discarded snapshot may have carried data the events
            // alone cannot reconstruct (e.g. an unread mark for a session that
            // finished while the WS was down). Forced past the throttle so the
            // discard cannot silently leave the capsule stale; this cannot storm
            // because a discard requires a real state change mid-flight
            // (stateVersion only moves when a tracked set actually changes, not
            // on every streaming event).
            requestOverviewRefresh(true);
            return;
        }
        postToUi(() -> renderOverview(overview));
    }

    /**
     * Replace the tracked sets with an authoritative overview snapshot, and
     * recompute {@code hasActive} from it. Caller must hold {@link #stateLock}.
     *
     * The overview is authoritative: the server lists every non-archived chat
     * session that is running, pending approval, or unread, resolving the flags
     * against the live registries. So the sets are replaced rather than merged —
     * a tracked id absent from the snapshot (its session ended while the WS was
     * down) must be dropped, otherwise a stale id would keep {@code hasActive}
     * true on the next unrelated event and resurrect a window with nothing to
     * show. A running scheduled task is not part of this overview (it covers
     * chat sessions only), so {@code hasActive} additionally honours the tracked
     * task set. No-op when the payload is malformed.
     */
    private void applyOverviewLocked(JSONObject overview) {
        JSONArray projects = overview.optJSONArray("projects");
        if (projects == null) {
            return;
        }
        java.util.Set<String> running = new java.util.HashSet<>();
        java.util.Set<String> pending = new java.util.HashSet<>();
        java.util.Set<String> unread = new java.util.HashSet<>();
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
                if (s.optBoolean("pendingApproval", false)) {
                    // Pending wins over running (yellow > green), matching the
                    // panel's status-dot priority and the capsule's mutual
                    // exclusion between the running and pending groups.
                    running.add(id);
                    pending.add(id);
                } else if (s.optBoolean("running", false)) {
                    running.add(id);
                } else if (s.optInt("unreadCount", 0) > 0) {
                    // Mirrors FloatingStatusView.countUnread: unread only counts
                    // sessions that are neither running nor pending.
                    unread.add(id);
                }
            }
        }
        runningSessions.clear();
        runningSessions.addAll(running);
        pendingSessions.clear();
        pendingSessions.addAll(pending);
        unreadSessions.clear();
        unreadSessions.addAll(unread);
        hasActive = !running.isEmpty() || !runningTasks.isEmpty();
    }

    /**
     * Render an (already applied) overview into the panel and drive window
     * visibility from the current tracked state. UI thread only.
     */
    private void renderOverview(JSONObject overview) {
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
            // Real data (or an empty no-session overview) replaces the
            // skeleton, so the placeholder never lingers after a load —
            // even when the overview carried zero sessions.
            panelView.hideSkeleton();
            // The overview changed the panel's content (group/session
            // count), so re-fit the window height to the new content.
            resizePanelIfNeeded();
            // A session list that emptied out (the last running session
            // finished and nothing is left worth showing) must not leave a
            // hollow panel behind: collapse it, which hides the window
            // because shouldShow is false with no content left.
            if (!shouldShow(appForeground, hasActive, unreadCount() > 0, userDismissed)) {
                setExpanded(false);
            }
        } else if (shouldShow(appForeground, hasActive, unreadCount() > 0, userDismissed)) {
            // Background + not dismissed + content worth showing. On WS
            // connect the overview is the first reliable state signal — a
            // running session discovered here (whose start event was missed
            // while the WS was down) must bring up the capsule.
            if (!windowShowing) {
                ensureWindow();
            }
        } else {
            // Nothing active and no unread items left: hide the window so
            // an idle background leaves no meaningless capsule on screen.
            hideWindow();
        }
        // The stats capsule always reflects the latest overview so its
        // counts stay current on collapse and right after a fallback build.
        if (view != null) {
            int[] stats = computeStats(overview);
            view.renderStats(stats[0], stats[1], stats[2]);
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
        // Scheduled-task events identify the run by task_id; session_id is
        // omitempty there and often empty. Both feed the tracked-state update
        // below, but only sessions reach the session sets.
        String taskId = data.optString("task_id", "");

        boolean active = isActiveStatus(eventType, status);
        // trackSessionState must run before deriving hasActive: a terminal
        // event for one session must not clear hasActive while other sessions
        // are still running in the tracked set.
        trackSessionState(eventType, status,
                "task_update".equals(eventType) ? taskId : sessionId);
        // A completed turn always produced assistant output, which the backend
        // counts as unread (messages newer than last_read_at). The live
        // "completed" broadcast carries has_new_messages=false, so keying off
        // that flag alone would hide the window on every completion — record
        // the session as unread here instead. A cancellation is different: it
        // may have produced nothing, so it only counts when the event itself
        // reports new messages. The overview remains authoritative and corrects
        // this either way.
        if ("session_update".equals(eventType)
                && ("completed".equals(status)
                        || data.optBoolean("has_new_messages", false))) {
            markSessionUnread(sessionId);
        }
        refreshHasActive(active);

        // While the panel is expanded, every event should refresh the overview
        // so the session list stays current without waiting for the next tap.
        // High-frequency streaming events would otherwise pile up requests, so
        // the refresh is throttled to OVERVIEW_REFRESH_MIN_INTERVAL_MS.
        // A "read" event (session marked read from another client) carries no
        // running/pending change, but must also refresh the overview so the
        // capsule's unread count updates even while collapsed.
        if (expanded || "read".equals(status)) {
            requestOverviewRefresh();
        }

        AppLog.d(TAG, "handleEvent event=" + eventType + " status=" + status
                + " sessionId=" + sessionId + " active=" + active);

        postToUi(() -> {
            if (active) {
                if (shouldShow(appForeground, true, unreadCount() > 0, userDismissed)) {
                    ensureWindow();
                    // Render the capsule instantly from locally tracked state
                    // (running/pending sets, last overview's unread count) so a
                    // session start is visible without waiting for the overview
                    // network round trip; the refresh then corrects all counts.
                    renderCapsuleStats();
                    requestOverviewRefresh();
                }
            } else {
                // Terminal state (or an unrelated status). The unread mark was
                // already recorded synchronously above, so the window decision
                // here uses current state.
                // While the panel is expanded the user is reading the list, so
                // never touch the window here — the overview path collapses and
                // hides it once nothing is left worth showing.
                if (!expanded) {
                    if (shouldShow(appForeground, hasActive, unreadCount() > 0, userDismissed)) {
                        // Content remains (another session running, or this one
                        // is now unread): show the capsule with fresh counts.
                        // ensureWindow covers the case where no window is up yet
                        // (e.g. the completion is the first event seen after a
                        // WS reconnect).
                        ensureWindow();
                        renderCapsuleStats();
                    } else {
                        // Nothing active and no unread items left: hide the
                        // window so an idle background leaves no capsule up.
                        hideWindow();
                    }
                }
                // Always re-pull the overview: it is authoritative for the
                // unread count and can reveal a running session whose start
                // event was missed while the WS was down. Forced for a terminal
                // status so the throttled path cannot leave the capsule showing
                // a just-finished session as still running.
                requestOverviewRefresh(isTerminalStatus(status));
            }
        });
    }

    /**
     * Render the capsule stats from locally tracked state, without waiting for
     * an overview round trip. The running count is the tracked running set
     * minus the pending set (pending wins over running, matching the overview
     * grouping); the unread count is the tracked unread set, seeded from the
     * last overview and adjusted by events since. onOverviewLoaded corrects all
     * three. Any thread; marshalled to the UI thread.
     */
    private void renderCapsuleStats() {
        postToUi(() -> {
            if (view != null) {
                int running = Math.max(0, runningSessions.size() - pendingSessions.size());
                view.renderStats(running, pendingSessions.size(), unreadCount());
            }
        });
    }

    /**
     * Re-render text after a system locale change. The capsule re-resolves its
     * resource strings; an expanded panel re-fetches the overview and re-renders
     * so the (Untitled) fallback follows the new locale. Any thread; UI
     * mutations are marshalled.
     */
    public void onLocaleChanged() {
        postToUi(() -> {
            if (view != null) {
                view.refreshLocaleText();
            }
        });
        if (expanded) {
            requestOverviewRefresh(true);
        }
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
            } else if (shouldShow(false, hasActive, unreadCount() > 0, userDismissed)) {
                ensureWindow();
                renderCapsuleStats();
            } else {
                // Backgrounded with nothing active and nothing unread: there is
                // nothing to show, so the window stays hidden.
                hideWindow();
            }
        });
    }

    /** Mark the window user-dismissed for the rest of this lifecycle. Any thread. */
    public void setUserDismissed(boolean dismissed) {
        userDismissed = dismissed;
        postToUi(() -> {
            if (dismissed) {
                hideWindow();
            } else if (shouldShow(appForeground, hasActive, unreadCount() > 0, false)) {
                // Re-evaluate: un-dismissing should restore the window if conditions hold.
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
        unreadSessions.clear();
        runningTasks.clear();
        // Bypass postToUi's destroyed guard here: the guard must drop event
        // runnables, but it must NOT drop our own teardown, otherwise the
        // window is never removed from the WindowManager.
        Runnable cleanup = () -> {
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
        requestOverviewRefresh(false);
    }

    /**
     * Request a fresh overview. When {@code force} is true the throttle is
     * bypassed entirely (used for panel expand, where the overview is required
     * to populate the list). Any thread.
     */
    private void requestOverviewRefresh(boolean force) {
        if (overviewRequestListener == null) {
            return;
        }
        long now = android.os.SystemClock.elapsedRealtime();
        // The first request is never swallowed by the throttle (elapsedRealtime
        // may legitimately be 0, so a timestamp check alone cannot tell "never
        // requested" from "requested at the clock origin").
        if (!force && overviewRequested
                && now - lastOverviewRequestMs < OVERVIEW_REFRESH_MIN_INTERVAL_MS) {
            return;
        }
        overviewRequested = true;
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
            // A hide may be in flight (alpha < 1); cancel it and restore the
            // full alpha so a fresh active event makes the window reappear.
            // cancel() does not run the hide's end action, so everything is
            // restored here.
            if (attachedView != null) {
                attachedView.animate().cancel();
                attachedView.setAlpha(1f);
                // Re-apply role sizing: an expanded panel must keep its fixed
                // 280dp width, a capsule its wrap-content.
                applyViewSizing();
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
            // Fade in on first show (the window appearing from nothing), like
            // the capsule<->panel swap animation in attachView.
            attachedView.setAlpha(0f);
            windowManager.addView(attachedView, params);
            windowShowing = true;
            // Ensure right-edge placement accounts for the real view width now
            // that it is laid out (no-op when a saved position is in effect).
            snapRightEdgeIfNeeded();
            attachedView.animate().alpha(1f).setDuration(FADE_MS).start();
            if (attachedView instanceof FloatingStatusView) {
                // The capsule's stats render on the next event / overview; the
                // spin animation is driven by renderStats, so nothing to
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
                // Fade the new view in so capsule<->panel swaps are not a
                // jarring cut. The old view was removed above; animate only the
                // freshly added view (ensureWindow's other paths set alpha 1f
                // directly and are unaffected).
                newView.setAlpha(0f);
                windowManager.addView(attachedView, params);
                newView.animate().alpha(1f).setDuration(FADE_MS).start();
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
            // The panel's window width is fixed (280dp); never measure it
            // against a WRAP_CONTENT (or animated) width — that would let the
            // panel stretch to the full screen width.
            int measureWidth = Math.round(PANEL_WIDTH_DP * context.getResources().getDisplayMetrics().density);
            int contentHeight = panelView.measureContentHeight(measureWidth);
            int panelHeight = panelHeightForContent(contentHeight, screenHeight());
            if (panelHeight <= 0) {
                return;
            }
            panelView.constrainListHeight(panelHeight);
            params.height = panelHeight;
            params.width = measureWidth;
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
     * Position the expanded panel so it stays on-screen while keeping the
     * capsule's side. The panel is much wider than the capsule, so a capsule
     * parked at the left edge keeps the panel left-aligned (x = margin) and a
     * right-parked capsule keeps the panel right-aligned — never jumping the
     * panel to the opposite side. The capsule's side is read from the current
     * params.x (same rule as snapToEdge: left half of the screen = left). UI
     * thread only.
     */
    private void clampPanelX() {
        if (attachedView == null || params == null || !windowShowing) {
            return;
        }
        int panelWidthPx = Math.round(PANEL_WIDTH_DP * context.getResources().getDisplayMetrics().density);
        boolean toLeft = params.x < screenWidth() / 2;
        int snapped = snapX(screenWidth(), panelWidthPx, edgeMarginPx, !toLeft);
        int clamped = clamp(snapped, edgeMarginPx, screenWidth());
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
