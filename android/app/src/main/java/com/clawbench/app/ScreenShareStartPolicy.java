package com.clawbench.app;

/**
 * Decides whether a second "start sharing" request may begin a new capture.
 *
 * <p>Two paths can ask while a session is already live: the page (a WebView
 * reload clears its own state, so the user clicks "share screen" again) and a
 * duplicate {@code ACTION_START} reaching the service. Starting a second
 * projection on top of the first overwrites {@code projection},
 * {@code imageReader} and {@code virtualDisplay} without releasing any of them,
 * and the abandoned {@link android.hardware.display.VirtualDisplay} can never be
 * released afterwards: the system's "screen is being shared" indicator stays on
 * and the foreground service cannot be stopped without killing the process.
 *
 * <p>The activity refuses the duplicate outright — the running session is
 * already what the page is asking for. Kept free of Android imports so the
 * decision table is unit-testable on the JVM.
 */
public final class ScreenShareStartPolicy {

    private ScreenShareStartPolicy() {
    }

    /**
     * Whether a start request must be ignored because a session already exists.
     *
     * <p>{@code requesting} counts as busy: the consent dialog is up, and
     * launching a second one would leave the first result to be delivered to a
     * callback whose options have been overwritten.
     */
    public static boolean shouldIgnoreStart(String state) {
        return ScreenShareStatus.RUNNING.equals(state) || ScreenShareStatus.REQUESTING.equals(state);
    }

    /**
     * Whether a start request has to release an existing projection first.
     *
     * <p>The service cannot simply refuse: it has no way to tell a duplicate
     * from a legitimate restart, and refusing would leave the new consent
     * result unused. Releasing first is the safe side — the previous display is
     * torn down deterministically instead of being leaked.
     */
    public static boolean shouldReleaseBeforeStart(boolean hasProjection, boolean hasDisplay) {
        return hasProjection || hasDisplay;
    }
}
