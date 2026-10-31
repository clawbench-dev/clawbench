package com.clawbench.app;

import org.junit.Test;

import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;

/**
 * Pure JUnit tests for {@link MicPermissionPolicy}.
 *
 * <p>No Android dependencies — the class under test only inspects the resource
 * strings a WebView passes to {@code onPermissionRequest}.
 */
public class MicPermissionPolicyTest {

    private static final String AUDIO = MicPermissionPolicy.RESOURCE_AUDIO_CAPTURE;

    @Test
    public void audioOnlyIsGranted() {
        assertArrayEquals(new String[]{AUDIO}, MicPermissionPolicy.grantableResources(new String[]{AUDIO}));
    }

    @Test
    public void videoIsRefused() {
        // ClawBench has no camera-for-WebRTC feature; granting video would let
        // any page open the camera without its own permission flow.
        String[] granted = MicPermissionPolicy.grantableResources(
                new String[]{"android.webkit.resource.VIDEO_CAPTURE"});
        assertEquals(0, granted.length);
    }

    @Test
    public void mixedRequestGrantsOnlyAudio() {
        String[] granted = MicPermissionPolicy.grantableResources(new String[]{
                "android.webkit.resource.VIDEO_CAPTURE",
                AUDIO,
                "android.webkit.resource.PROTECTED_MEDIA_ID",
        });
        assertArrayEquals(new String[]{AUDIO}, granted);
    }

    @Test
    public void nullAndEmptyRequestsGrantNothing() {
        assertEquals(0, MicPermissionPolicy.grantableResources(null).length);
        assertEquals(0, MicPermissionPolicy.grantableResources(new String[0]).length);
    }

    @Test
    public void unknownResourcesGrantNothing() {
        assertEquals(0, MicPermissionPolicy.grantableResources(
                new String[]{"android.webkit.resource.MIDI_SYSEX"}).length);
    }
}
