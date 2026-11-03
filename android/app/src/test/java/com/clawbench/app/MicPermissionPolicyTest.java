package com.clawbench.app;

import org.junit.Test;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Pure JUnit tests for {@link MicPermissionPolicy}.
 *
 * <p>No Android dependencies — the class under test only inspects the resource
 * strings a WebView passes to {@code onPermissionRequest} plus the two OS-grant
 * flags the caller supplies.
 *
 * <p>The interesting cases are the pairs: what the page asked for, and what the
 * app is actually allowed to hand over. Granting a resource the OS refused just
 * moves the failure one step later, with a worse message.
 */
public class MicPermissionPolicyTest {

    private static final String AUDIO = MicPermissionPolicy.RESOURCE_AUDIO_CAPTURE;
    private static final String VIDEO = MicPermissionPolicy.RESOURCE_VIDEO_CAPTURE;

    @Test
    public void audioOnlyIsGrantedWhenTheOsGrantExists() {
        assertArrayEquals(new String[]{AUDIO},
                MicPermissionPolicy.grantableResources(new String[]{AUDIO}, true, false));
    }

    @Test
    public void audioIsRefusedWhenTheMicrophonePermissionIsMissing() {
        // The OS grant is the outer gate: without it the page must not be told
        // the microphone is available.
        assertEquals(0, MicPermissionPolicy.grantableResources(new String[]{AUDIO}, false, true).length);
    }

    @Test
    public void videoIsGrantedForAVideoMeeting() {
        // Video meetings capture the camera, so this resource is supported now.
        assertArrayEquals(new String[]{VIDEO},
                MicPermissionPolicy.grantableResources(new String[]{VIDEO}, false, true));
    }

    @Test
    public void videoIsRefusedWhenTheCameraPermissionIsMissing() {
        assertEquals(0, MicPermissionPolicy.grantableResources(new String[]{VIDEO}, true, false).length);
    }

    @Test
    public void mixedRequestGrantsBothWhenBothAreHeld() {
        // A mixed getUserMedia({audio,video}) request that is granted only half
        // of it fails as a whole, so both halves have to survive together.
        String[] granted = MicPermissionPolicy.grantableResources(
                new String[]{VIDEO, AUDIO, "android.webkit.resource.PROTECTED_MEDIA_ID"}, true, true);
        assertArrayEquals(new String[]{VIDEO, AUDIO}, granted);
    }

    @Test
    public void mixedRequestDropsOnlyTheHalfTheOsRefused() {
        String[] granted = MicPermissionPolicy.grantableResources(new String[]{VIDEO, AUDIO}, true, false);
        assertArrayEquals(new String[]{AUDIO}, granted);
    }

    @Test
    public void nullAndEmptyRequestsGrantNothing() {
        assertEquals(0, MicPermissionPolicy.grantableResources(null, true, true).length);
        assertEquals(0, MicPermissionPolicy.grantableResources(new String[0], true, true).length);
    }

    @Test
    public void unknownResourcesGrantNothing() {
        assertEquals(0, MicPermissionPolicy.grantableResources(
                new String[]{"android.webkit.resource.MIDI_SYSEX"}, true, true).length);
    }

    @Test
    public void wantedResourcesAreReportedSeparately() {
        // MainActivity uses these to decide WHICH OS permission to ask for; a
        // wrong answer here means a dialog for a resource the page never wanted.
        assertTrue(MicPermissionPolicy.wantsAudio(new String[]{AUDIO, VIDEO}));
        assertTrue(MicPermissionPolicy.wantsVideo(new String[]{AUDIO, VIDEO}));
        assertFalse(MicPermissionPolicy.wantsVideo(new String[]{AUDIO}));
        assertFalse(MicPermissionPolicy.wantsAudio(new String[]{VIDEO}));
        assertFalse(MicPermissionPolicy.wantsAudio(null));
    }
}
