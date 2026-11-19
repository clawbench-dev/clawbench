package com.clawbench.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.provider.Settings;

import androidx.core.app.NotificationCompat;

import org.json.JSONObject;

/**
 * Android 16 Live Updates — a promoted ongoing notification that surfaces the
 * ClawBench session state as a status-bar chip (the Android analogue of the
 * iOS Dynamic Island) plus an expanded-by-default, uncollapsible card on the
 * lock screen and at the top of the notification drawer.
 *
 * The chip shows a single, mutually-exclusive summary — pending approvals
 * first (most urgent: user action required), then unread finished sessions,
 * then running sessions. When nothing is left the notification is removed
 * so the status bar stays clean until the next session becomes active. The
 * expanded card always shows the counts for all three groups — "执行中 2 ·
 * 待审批 0 · 未读 3" — with the status-bar chip rendering the short
 * single-group line (contentTitle) and the expanded-by-default card showing
 * the full breakdown (contentText / BigTextStyle). Labels are loaded from
 * string resources for i18n.
 *
 * Data source parity with the floating window: the service feeds this manager
 * the same /api/ai/sessions/overview snapshots (on WS connect and on events)
 * that drive {@link FloatingStatusController}. {@link #computeStats} delegates
 * to {@link FloatingStatusController#computeStats(JSONObject)}, which itself
 * reuses the three pure overview parsers in {@link FloatingStatusView}, so the
 * Live Update chip and the floating capsule always agree on the numbers.
 *
 * Throttling: a {@link #THROTTLE_MS} merge window coalesces event-driven
 * refreshes, so a burst of session_update events triggers a single notify().
 * The system refreshes the `when`-driven elapsed time itself, so the chip
 * stays alive between refreshes without any app work.
 *
 * Promotion requirements (official docs): standard style (BigTextStyle here),
 * POST_PROMOTED_NOTIFICATIONS in the manifest, a request to be promoted, the
 * ongoing flag, a content title, no custom RemoteViews / group summary /
 * colorized, and a channel above IMPORTANCE_MIN. {@link #notifyInternal}
 * checks {@link NotificationManager#canPostPromotedNotifications()} and falls
 * back to a plain ongoing notification when promotion is unavailable (older
 * system, user disabled Live Updates, or the app lost the banner permission).
 *
 * All public methods are safe to call from any thread; notification posting is
 * marshalled to the main looper.
 */
public class LiveUpdateManager {

    private static final String TAG = "LiveUpdate";

    /** Notification channel for the live update card. */
    public static final String CHANNEL_ID = "clawbench_live_update";
    /** Single notification id — one chip summarizing the whole workspace.
     *  Reserved for this manager alone (the background service uses 2, screen
     *  sharing 0x5C01): the promoted path posts under this id with no tag, and
     *  tearing the chip down cancels that combination outright. */
    public static final int NOTIFICATION_ID = 100;

    /** How long a burst of event-driven refreshes is merged into one notify(). */
    static final long THROTTLE_MS = 5000;

    /** Extra key requesting promoted treatment (EXTRA_REQUEST_PROMOTED_ONGOING
     *  value; not a public constant in the locally shipped android-36 jar). */
    private static final String EXTRA_REQUEST_PROMOTED_ONGOING = "android.requestPromotedOngoing";

    private final Context appContext;
    private final Handler handler;
    private final NotificationManager notificationManager;

    /**
     * Fired when the manager needs a fresh overview to reconcile counts that
     * events alone cannot carry (unread). Wired by BackgroundService to
     * fetchOverviewSessions. Same contract as FloatingStatusController's
     * listener of the same name.
     */
    public interface OverviewRequestListener {
        void onRequestOverview();
    }

    private volatile OverviewRequestListener overviewRequestListener;

    /** Latest computed counts (pending > running > unread), for throttled coalescing. */
    private volatile int pendingCount;
    private volatile int runningCount;
    private volatile int unreadCount;
    /** Last time a notification was actually posted (throttle anchor). */
    private volatile long lastPostedMs;
    /** Last time an overview was requested from an event (throttle anchor). */
    private volatile long lastOverviewRequestMs;
    /**
     * Monotonically increasing tag for the pre-Android 16 fallback posting
     * path. Some pre-16 OEM chips (ColorOS fluid cloud) cache the chip text
     * per notification key and do not refresh the title on same-key updates,
     * so that path rotates the tag. The promoted path (API 36+) uses a stable
     * key instead — see notifyInternal.
     */
    private int currentTag;
    /** Whether a refresh is already scheduled within the throttle window. */
    private volatile boolean refreshScheduled;
    /** Whether a notification is currently visible. */
    private volatile boolean visible;

    public LiveUpdateManager(Context context) {
        this.appContext = context.getApplicationContext();
        this.handler = new Handler(Looper.getMainLooper());
        this.notificationManager =
                (NotificationManager) appContext.getSystemService(Context.NOTIFICATION_SERVICE);
    }

    /**
     * Feed a fresh /api/ai/sessions/overview snapshot. Recomputes the three
     * counts and schedules a (possibly throttled) refresh. Any thread.
     */
    public void onOverviewLoaded(JSONObject overview) {
        if (overview == null) {
            return;
        }
        int[] stats = computeStats(overview);
        updateCounts(stats[0], stats[1], stats[2]);
    }

    /**
     * Register the overview request callback. The service wires this to
     * fetchOverviewSessions so unread counts (which events do not carry) can
     * be reconciled while the app is backgrounded. Any thread.
     */
    public void setOverviewRequestListener(OverviewRequestListener listener) {
        this.overviewRequestListener = listener;
    }

    /**
     * Feed a session_update / task_update event for count tracking. The event
     * alone does not carry unread counts, so only running/pending state is
     * adjusted here; the overview path reconciles all three. Any thread.
     *
     * Mirrors FloatingStatusController.trackSessionState semantics: active
     * states add the session, terminal states remove it. task_update is
     * ignored (its session_id is often empty and would desync the count).
     *
     * Because the event path cannot see unread, each event also requests a
     * fresh overview via {@link OverviewRequestListener} — throttled on the
     * service side — so the chip's unread count stays current in the
     * background without waiting for a WS (re)connect.
     */
    public void onEvent(String eventType, String status, String sessionId) {
        if (!"session_update".equals(eventType)
                || sessionId == null || sessionId.isEmpty()) {
            return;
        }
        boolean active = FloatingStatusController.isActiveStatus(eventType, status);
        boolean pending = "permission_pending".equals(status);
        boolean terminal = "completed".equals(status) || "cancelled".equals(status)
                || "failed".equals(status);
        if (active) {
            runningSessions.add(sessionId);
            if (pending) {
                pendingSessions.add(sessionId);
            }
        } else if (terminal) {
            runningSessions.remove(sessionId);
            pendingSessions.remove(sessionId);
        } else if ("permission_resolved".equals(status)) {
            pendingSessions.remove(sessionId);
        }
        // Event-driven path can only update running/pending; unread stays as
        // the last overview reported it. Ask for a fresh overview so the chip
        // reconciles all three (unread included) while backgrounded — but only
        // at most once per THROTTLE_MS, so an event burst cannot spam the API.
        updateCounts(runningSessions.size() - pendingSessions.size(),
                pendingSessions.size(), unreadCount);
        long now = System.currentTimeMillis();
        if (now - lastOverviewRequestMs >= THROTTLE_MS) {
            lastOverviewRequestMs = now;
            OverviewRequestListener listener = overviewRequestListener;
            if (listener != null) {
                listener.onRequestOverview();
            }
        }
    }

    /**
     * Remove the live update notification and reset local state. Called on
     * service shutdown / feature toggle-off. Any thread.
     */
    public void destroy() {
        handler.post(() -> {
            refreshScheduled = false;
            visible = false;
            cancelChip();
        });
        runningSessions.clear();
        pendingSessions.clear();
    }

    /** True while a live update notification is currently visible. */
    public boolean isVisible() {
        return visible;
    }

    /** Latest pending count (most urgent group). */
    public int getPendingCount() {
        return pendingCount;
    }

    /** Latest running count. */
    public int getRunningCount() {
        return runningCount;
    }

    /** Latest unread count. */
    public int getUnreadCount() {
        return unreadCount;
    }

    // ------------------------------------------------------------------
    // Pure helpers (org.json + plain fields only — unit-testable without
    // an Android framework under plain JUnit).
    // ------------------------------------------------------------------

    /** The three workspace stats from an overview: {running, pending, unread}. */
    public static int[] computeStats(JSONObject overview) {
        return FloatingStatusController.computeStats(overview);
    }

    /**
     * Which group the status-bar chip highlights, most urgent first: pending
     * approvals (user action required), then unread finished sessions, then
     * running — so a finished-but-unread result outranks an in-flight session.
     * "" when every count is 0. Pure: only primitives.
     */
    public static String chipGroup(int running, int pending, int unread) {
        if (pending > 0) {
            return "pending";
        }
        if (unread > 0) {
            return "unread";
        }
        if (running > 0) {
            return "running";
        }
        return "";
    }

    /**
     * The single summary line shown on the status-bar chip. Shows only the
     * most urgent group (see {@link #chipGroup}), each group carrying the same
     * colored dot emoji as the expanded card (🟡 pending, 🟢 running,
     * 🔵 unread), so the chip and the card read consistently. The caller
     * removes the chip before this when every count is 0, so "" here is never
     * rendered. Labels are injected for i18n. Pure: only primitives.
     */
    public static String chipText(int running, int pending, int unread,
                                  String runningLabel, String pendingLabel,
                                  String unreadLabel) {
        String group = chipGroup(running, pending, unread);
        if ("pending".equals(group)) {
            return "🟡 " + pendingLabel + " " + pending;
        }
        if ("running".equals(group)) {
            return "🟢 " + runningLabel + " " + running;
        }
        if ("unread".equals(group)) {
            return "🔵 " + unreadLabel + " " + unread;
        }
        return "";
    }

    /**
     * Expanded-card summary. It shows the groups that the status-bar chip does
     * not already highlight — whatever the chip shows in its title is not
     * repeated here. The remaining groups are always shown (including zeros),
     * each prefixed with a colored dot emoji mirroring the floating capsule's
     * colored dots: 🟢 running, 🟡 pending approval, 🔵 unread. E.g. with a
     * chip of "🟢 执行中 2" the card reads "🟡 待审批 0 · 🔵 未读 3". The caller
     * removes the chip before this when every count is 0. Labels and the
     * joiner are injected for i18n. Pure: only primitives.
     */
    public static String cardSummary(int running, int pending, int unread,
                                     String runningLabel, String pendingLabel,
                                     String unreadLabel, String joiner) {
        String chip = chipGroup(running, pending, unread);
        StringBuilder sb = new StringBuilder();
        if (!"running".equals(chip)) {
            sb.append("🟢 ").append(runningLabel).append(" ").append(running);
        }
        if (!"pending".equals(chip)) {
            if (sb.length() > 0) sb.append(joiner);
            sb.append("🟡 ").append(pendingLabel).append(" ").append(pending);
        }
        if (!"unread".equals(chip)) {
            if (sb.length() > 0) sb.append(joiner);
            sb.append("🔵 ").append(unreadLabel).append(" ").append(unread);
        }
        return sb.toString();
    }

    /**
     * Whether a refresh should be posted immediately. Pure: only primitives.
     * An empty workspace refreshes immediately (so the chip can be removed
     * right away); otherwise the first event is immediate and subsequent ones
     * within THROTTLE_MS are merged.
     */
    static boolean shouldRefresh(int running, int pending, int unread,
                                 boolean visible, long nowMs, long lastPostedMs) {
        if (pending == 0 && running == 0 && unread == 0) {
            return true; // idle — remove the chip right away
        }
        if (!visible) {
            return true; // first appearance is always immediate
        }
        return nowMs - lastPostedMs >= THROTTLE_MS;
    }

    // ------------------------------------------------------------------
    // Internal state + posting
    // ------------------------------------------------------------------

    private final java.util.Set<String> runningSessions =
            java.util.concurrent.ConcurrentHashMap.newKeySet();
    private final java.util.Set<String> pendingSessions =
            java.util.concurrent.ConcurrentHashMap.newKeySet();

    private void updateCounts(int running, int pending, int unread) {
        boolean changed = running != runningCount || pending != pendingCount
                || unread != unreadCount;
        runningCount = running;
        pendingCount = pending;
        unreadCount = unread;
        if (!changed && visible) {
            return;
        }
        handler.post(this::scheduleRefresh);
    }

    private void scheduleRefresh() {
        if (refreshScheduled) {
            return;
        }
        long now = System.currentTimeMillis();
        if (shouldRefresh(runningCount, pendingCount, unreadCount,
                visible, now, lastPostedMs)) {
            refreshScheduled = false;
            doRefresh();
            return;
        }
        refreshScheduled = true;
        long delay = lastPostedMs + THROTTLE_MS - now;
        handler.postDelayed(() -> {
            refreshScheduled = false;
            doRefresh();
        }, Math.max(0, delay));
    }

    private void doRefresh() {
        lastPostedMs = System.currentTimeMillis();
        // An empty workspace removes the chip — nothing is worth showing, so
        // the status bar stays clean until the next session becomes active.
        if (pendingCount == 0 && runningCount == 0 && unreadCount == 0) {
            visible = false;
            cancelChip();
            return;
        }
        visible = true;
        notifyInternal(
                chipText(runningCount, pendingCount, unreadCount,
                        appContext.getString(R.string.live_update_running),
                        appContext.getString(R.string.live_update_pending),
                        appContext.getString(R.string.live_update_unread)),
                cardSummary(runningCount, pendingCount, unreadCount,
                        appContext.getString(R.string.live_update_running),
                        appContext.getString(R.string.live_update_pending),
                        appContext.getString(R.string.live_update_unread),
                        appContext.getString(R.string.live_update_joiner)));
    }

    /**
     * Build and post the live update notification. On API 36+ with promotion
     * available this is a promoted ongoing notification (status-bar chip); on
     * older systems or when promotion is denied it degrades to a plain ongoing
     * notification. Main thread only.
     */
    private void notifyInternal(String chip, String summary) {
        if (notificationManager == null) {
            return;
        }
        ensureChannel();

        NotificationCompat.Builder builder = new NotificationCompat.Builder(appContext, CHANNEL_ID)
                // The launcher icon (auto-rendered as a monochrome silhouette
                // by the system for status-bar small icons) shows the app's own
                // mark on the chip instead of a generic notification glyph.
                .setSmallIcon(R.mipmap.ic_launcher)
                // ColorOS fluid cloud (and most OEM chips) render the status
                // text from contentTitle, so the live status goes there. The
                // expanded card shows the full breakdown via contentText /
                // BigTextStyle.
                .setContentTitle(chip)
                .setContentText(summary)
                .setStyle(new NotificationCompat.BigTextStyle().bigText(summary))
                .setOngoing(true)
                .setOnlyAlertOnce(true)
                .setContentIntent(buildTapIntent());

        if (Build.VERSION.SDK_INT >= 36) {
            // Request promotion to a Live Update (status-bar chip). The extra
            // key matches EXTRA_REQUEST_PROMOTED_ONGOING; the locally shipped
            // android-36 jar predates the 36.1 constant, hence the literal.
            builder.setPriority(NotificationCompat.PRIORITY_LOW);
            builder.getExtras().putBoolean(EXTRA_REQUEST_PROMOTED_ONGOING, true);
        }

        Notification notification = builder.build();
        AppLog.d(TAG, "posting live update: chip=\"" + chip + "\" summary=\"" + summary
                + " promoted=" + canPostPromoted(appContext) + " (sdk=" + Build.VERSION.SDK_INT + ")");

        postChip(notification, Build.VERSION.SDK_INT);
    }

    /**
     * Post the chip under the key its OS generation expects.
     *
     * Split from {@link #notifyInternal} so both branches are reachable from a
     * unit test: Robolectric's offline SDK cache tops out below 36, so a test
     * cannot drive the promoted branch through {@link Build.VERSION#SDK_INT}.
     */
    void postChip(Notification notification, int sdkInt) {
        if (sdkInt >= 36) {
            // Keep the chip's user-visible identity stable. OriginOS (vivo
            // 原子岛) ties status-bar / lock-screen / notification-center chips
            // to the notification key; posting a fresh tag on every refresh
            // makes each refresh look like a brand-new notification there, so
            // no chip ever shows up (issue #694). A stable key (tag=null) lets
            // the system update the existing entry in place — and because the
            // update is in place, other ROMs (ColorOS fluid cloud) can no
            // longer serve a stale cached chip title either.
            notificationManager.notify(NOTIFICATION_ID, notification);
            return;
        }
        // Pre-Android 16 fallback (promotion unavailable): keep the historical
        // tag rotation. Some pre-16 OEM chips (ColorOS fluid cloud) cache the
        // chip text per notification key and do not refresh it on same-key
        // updates, so a fresh tag forces a rebuild. Cancel the previous tag
        // first (before the new one is posted) so the drawer never accumulates
        // stale entries and never holds two chips at once.
        String previous = currentTag > 0 ? "live-" + currentTag : null;
        String tag = "live-" + (++currentTag);
        if (previous != null) {
            notificationManager.cancel(previous, NOTIFICATION_ID);
        }
        notificationManager.notify(tag, NOTIFICATION_ID, notification);
    }

    /**
     * Cancel the currently visible chip — the stable promoted key plus the
     * most recent pre-16 fallback tag. Main thread only (called from
     * doRefresh / destroy on the main looper).
     */
    private void cancelChip() {
        if (notificationManager == null) {
            return;
        }
        notificationManager.cancel(NOTIFICATION_ID);
        if (currentTag > 0) {
            notificationManager.cancel("live-" + currentTag, NOTIFICATION_ID);
        }
    }

    private void ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return;
        }
        if (notificationManager.getNotificationChannel(CHANNEL_ID) != null) {
            return;
        }
        NotificationChannel channel = new NotificationChannel(
                CHANNEL_ID,
                appContext.getString(R.string.notif_channel_live_update),
                NotificationManager.IMPORTANCE_LOW);
        channel.setDescription(appContext.getString(R.string.notif_channel_live_update_desc));
        channel.setShowBadge(false);
        notificationManager.createNotificationChannel(channel);
    }

    private PendingIntent buildTapIntent() {
        Intent intent = new Intent(appContext, MainActivity.class);
        intent.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP
                | Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        return PendingIntent.getActivity(
                appContext, 0, intent,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
    }

    // ------------------------------------------------------------------
    // Promotion support / settings navigation
    // ------------------------------------------------------------------

    /**
     * Whether the system will actually promote a Live Updates request right
     * now. On Android 16+ this reflects canPostPromotedNotifications() (which
     * accounts for the user's Live Updates setting and the app's permission);
     * on older systems promotion is unavailable, so this is false.
     */
    public static boolean canPostPromoted(Context context) {
        if (Build.VERSION.SDK_INT < 36) {
            return false;
        }
        NotificationManager nm =
                (NotificationManager) context.getSystemService(Context.NOTIFICATION_SERVICE);
        return nm != null && nm.canPostPromotedNotifications();
    }

    /**
     * Open the system screen where the user can enable Live Updates for this
     * app. Tries the promotion-specific actions in order, falling back to the
     * app notification settings screen (which exists on every device) when a
     * specific action is missing or throws.
     *
     * @param context an Activity context (the settings screen is launched from
     *                it); null-safe and exception-safe.
     */
    public static void openPromotedSettings(Context context) {
        if (context == null) {
            return;
        }
        String pkg = context.getPackageName();
        // Known promotion-settings actions, most specific first. The exact
        // action name varies across Android versions / OEMs (the documented
        // ACTION_MANAGE_APP_PROMOTED_NOTIFICATIONS does not exist on some
        // builds, where ACTION_APP_NOTIFICATION_PROMOTION_SETTINGS is the real
        // one), so probe each and fall through on failure.
        String[] promotionActions = {
                "android.settings.MANAGE_APP_PROMOTED_NOTIFICATIONS",
                "android.settings.APP_NOTIFICATION_PROMOTION_SETTINGS",
        };
        for (String action : promotionActions) {
            try {
                Intent intent = new Intent(action, Uri.parse("package:" + pkg));
                if (context.getPackageManager().resolveActivity(intent, 0) != null) {
                    context.startActivity(intent);
                    return;
                }
            } catch (Exception e) {
                AppLog.w(TAG, "promoted settings action unavailable: " + action, e);
            }
        }
        // Fallback: the per-app notification settings screen exists everywhere.
        try {
            Intent fallback = new Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                    .putExtra(Settings.EXTRA_APP_PACKAGE, pkg);
            context.startActivity(fallback);
        } catch (Exception e) {
            AppLog.w(TAG, "failed to open notification settings", e);
        }
    }
}
