package com.clawbench.app;

/**
 * Pure decision logic for native (Android) event notifications.
 *
 * Extracted from BackgroundService so the rules can be unit-tested without an
 * Android runtime. BackgroundService had three independent notification paths
 * (live WS, WS replay after reconnect, HTTP pending-event recovery) that each
 * re-implemented "should this notify?" inline. They drifted: the replay path
 * ignored the server's {@code replayed} marker and the HTTP path had no
 * empty-cursor guard, so both re-notified replies the user had already read.
 *
 * Three rules are enforced here:
 *
 * 1. Only terminal session/task states notify (unchanged behavior).
 * 2. Replayed events never notify. The server tags messages it replays from the
 *    reconnect buffer with {@code replayed:true}; they are caught-up history the
 *    user did not watch happen. The web frontend already honors this
 *    (useGlobalEvents.ts → isReplayingEvents); the native client did not.
 * 3. Events whose subject is already read never notify, when the server says so
 *    via {@code suppress_notification}. The pending-event log is not pruned on
 *    read, so without this an already-read reply re-notifies on every recovery.
 *
 * Cursor advancement is deliberately a SEPARATE question from notification
 * (see {@link #isNotifiableEvent}): a suppressed or replayed event must still
 * advance the client cursor, or the next fetch would re-read it forever.
 */
public final class NativeNotificationPolicy {

    private NativeNotificationPolicy() {
    }

    /** Terminal session_update statuses that are worth a notification. */
    public static boolean isNotifiableSessionStatus(String status) {
        return "completed".equals(status)
                || "cancelled".equals(status)
                || "permission_pending".equals(status);
    }

    /** task_update statuses that are worth a notification. */
    public static boolean isNotifiableTaskStatus(String status) {
        return "running".equals(status)
                || "completed".equals(status)
                || "failed".equals(status)
                || "cancelled".equals(status);
    }

    /**
     * Whether the event is a notifiable terminal state.
     *
     * This governs CURSOR ADVANCEMENT, not notification: every event the server
     * persisted as notifiable must advance the cursor even when its notification
     * is suppressed, otherwise the client would fetch it again on the next
     * reconnect and suppress it again forever.
     */
    public static boolean isNotifiableEvent(String eventType, String status) {
        if ("session_update".equals(eventType)) {
            return isNotifiableSessionStatus(status);
        }
        if ("task_update".equals(eventType)) {
            return isNotifiableTaskStatus(status);
        }
        return false;
    }

    /**
     * Whether a live WS event should raise a notification.
     *
     * permission_pending is never suppressible by the server's read/replay
     * flags. The server's read gate already exempts it (it is an approval
     * request, not a reply to read), so the flags should never be set for it —
     * but if that ever regressed, suppressing the prompt would block the
     * session with no signal to the user, whereas notifying a stale prompt only
     * costs a tap. The asymmetry justifies not trusting the flag here.
     *
     * @param replayed             server tagged the message as reconnect replay
     * @param suppressNotification server says the subject is already read
     */
    public static boolean shouldNotifyLive(String eventType, String status,
                                           boolean replayed, boolean suppressNotification) {
        if (!isNotifiableEvent(eventType, status)) {
            return false;
        }
        if ("session_update".equals(eventType) && "permission_pending".equals(status)) {
            return true;
        }
        if (replayed) {
            return false;
        }
        return !suppressNotification;
    }

    /**
     * Whether an event from the HTTP pending-event recovery path should notify.
     *
     * On a fresh client (empty cursor) the server returns up to 24h of backlog
     * with no {@code after} filter. Notifying that backlog floods the user with
     * completions they already saw — the reported "切到后台后补弹一批历史通知".
     * The web client already refuses to replay an empty-cursor fetch
     * (useGlobalEvents.fetchPendingEvents advances the cursor and returns); this
     * mirrors it. The caller must still adopt the newest event id as cursor.
     *
     * @param lastSeenId empty when this client has no cursor yet
     */
    public static boolean shouldNotifyFromBacklog(String lastSeenId, String eventType,
                                                  String status, boolean suppressNotification) {
        if (isFreshBacklog(lastSeenId)) {
            return false;
        }
        return shouldNotifyLive(eventType, status, false, suppressNotification);
    }

    /**
     * True when the client has no cursor, so a pending fetch returns the whole
     * (TTL-bounded) backlog rather than the events missed since last seen.
     */
    public static boolean isFreshBacklog(String lastSeenId) {
        return lastSeenId == null || lastSeenId.isEmpty();
    }
}
