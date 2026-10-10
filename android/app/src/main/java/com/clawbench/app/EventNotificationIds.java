package com.clawbench.app;

/**
 * Deterministic notification IDs for event (session/task) notifications.
 *
 * <p>Extracted from {@code BackgroundService} so the "post" and "cancel" paths
 * share ONE formula. They were duplicated inline at two post sites and had no
 * cancel counterpart at all — the reported defect was that a notification
 * stayed in the shade after the user opened the app and read the message, so a
 * later tap re-dispatched the deep link. Cancelling requires recomputing the
 * exact id that was posted; if the formula drifted between the two paths the
 * cancel would silently no-op.
 *
 * <p><b>Base and layout.</b> Sessions and tasks live in disjoint id bands so a
 * task can never collide with a session:
 * <ul>
 *   <li>session: {@code BASE + |hashCode(sessionId) % 1000|}</li>
 *   <li>task:    {@code BASE + 1000 + |hashCode(taskId) % 1000|}</li>
 * </ul>
 *
 * <p><b>Known limitation (accepted).</b> The {@code % 1000} truncation means two
 * ids whose hash codes differ by a multiple of 1000 map to the same
 * notification: the second post replaces the first, and a cancel for either
 * clears both. Widening the modulus would change the ids of notifications
 * already in the shade (their cancel would then miss), so the formula is kept
 * byte-for-byte identical to the historical one and only centralized. The
 * modulo runs BEFORE {@code abs}, which also sidesteps the classic
 * {@code Math.abs(Integer.MIN_VALUE) == Integer.MIN_VALUE} trap.
 */
public final class EventNotificationIds {

    /** Base id for event notifications. Kept at the historical value 3. */
    public static final int BASE = 3;

    /** Size of each id band. Also the truncation modulus. */
    public static final int BAND = 1000;

    private EventNotificationIds() {
    }

    /** Notification id for a session event, or {@link #BASE} when the id is blank. */
    public static int forSession(String sessionId) {
        if (sessionId == null || sessionId.isEmpty()) {
            return BASE;
        }
        return BASE + Math.abs(sessionId.hashCode() % BAND);
    }

    /** Notification id for a task event, or {@link #BASE} when the id is blank. */
    public static int forTask(String taskId) {
        if (taskId == null || taskId.isEmpty()) {
            return BASE;
        }
        return BASE + BAND + Math.abs(taskId.hashCode() % BAND);
    }

    /**
     * Notification id for an event, preferring the task id when present (matching
     * the post path's branch order: a task event carries both a task_id and a
     * session_id, and must resolve to the task band).
     */
    public static int forEvent(String taskId, String sessionId) {
        if (taskId != null && !taskId.isEmpty()) {
            return forTask(taskId);
        }
        return forSession(sessionId);
    }
}
