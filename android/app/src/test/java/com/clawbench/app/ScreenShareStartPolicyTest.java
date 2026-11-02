package com.clawbench.app;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

/**
 * The duplicate-start guard for screen sharing.
 *
 * <p>Its failure mode is silent and severe: a second {@code ACTION_START}
 * overwrites {@code projection} / {@code imageReader} / {@code virtualDisplay}
 * without releasing the old ones, and the abandoned virtual display can never
 * be released afterwards — the system keeps showing "screen is being shared"
 * until the process is killed.
 */
public class ScreenShareStartPolicyTest {

    @Test
    public void ignoresAStartWhileAlreadyRunning() {
        assertTrue(ScreenShareStartPolicy.shouldIgnoreStart(ScreenShareStatus.RUNNING));
    }

    @Test
    public void ignoresAStartWhileTheConsentDialogIsUp() {
        // A second consent dialog would deliver its result to a callback whose
        // options were already replaced.
        assertTrue(ScreenShareStartPolicy.shouldIgnoreStart(ScreenShareStatus.REQUESTING));
    }

    @Test
    public void allowsAStartFromAnInactiveState() {
        assertFalse(ScreenShareStartPolicy.shouldIgnoreStart(ScreenShareStatus.IDLE));
        assertFalse(ScreenShareStartPolicy.shouldIgnoreStart(ScreenShareStatus.DENIED));
        assertFalse(ScreenShareStartPolicy.shouldIgnoreStart(ScreenShareStatus.ERROR));
        // An unknown/absent state must not block sharing forever.
        assertFalse(ScreenShareStartPolicy.shouldIgnoreStart(""));
        assertFalse(ScreenShareStartPolicy.shouldIgnoreStart(null));
    }

    @Test
    public void releasesAnExistingSessionBeforeStartingAnother() {
        assertTrue(ScreenShareStartPolicy.shouldReleaseBeforeStart(true, false));
        assertTrue(ScreenShareStartPolicy.shouldReleaseBeforeStart(false, true));
        assertTrue(ScreenShareStartPolicy.shouldReleaseBeforeStart(true, true));
        assertFalse(ScreenShareStartPolicy.shouldReleaseBeforeStart(false, false));
    }
}
