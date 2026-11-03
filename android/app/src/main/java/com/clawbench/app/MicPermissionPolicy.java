package com.clawbench.app;

/**
 * Pure decision logic for WebView {@code onPermissionRequest} — which of the
 * resources a page asked for may be granted.
 *
 * Extracted from {@link MainActivity} so the rules are unit-testable without an
 * Android runtime (same reason {@link NativeNotificationPolicy} exists). The
 * class deliberately avoids {@code android.webkit} imports: the resource names
 * are stable protocol strings, and duplicating them keeps this file loadable by
 * a plain JVM test.
 *
 * Background: a WebView denies {@code getUserMedia} by default — the page sees
 * {@code NotAllowedError} even when the app holds the OS permission. The only
 * hook is {@code WebChromeClient.onPermissionRequest}, so neither voice input
 * nor a video meeting's camera can work until the app answers that callback.
 *
 * The OS grant and the WebView grant are two separate gates and both must be
 * open: {@code canAudio}/{@code canVideo} are what the app holds at the system
 * level, and anything not granted there is refused here as well. Granting a
 * resource the OS refused would just move the failure one step later, with a
 * worse message.
 */
public final class MicPermissionPolicy {

    /** Value of {@code PermissionRequest.RESOURCE_AUDIO_CAPTURE}. */
    public static final String RESOURCE_AUDIO_CAPTURE = "android.webkit.resource.AUDIO_CAPTURE";
    /** Value of {@code PermissionRequest.RESOURCE_VIDEO_CAPTURE}. */
    public static final String RESOURCE_VIDEO_CAPTURE = "android.webkit.resource.VIDEO_CAPTURE";

    private MicPermissionPolicy() {
    }

    /**
     * Returns the subset of {@code requested} that may be granted, given which
     * OS permissions the app currently holds.
     *
     * An empty result means "deny": the page asked for something this app never
     * grants (screen capture arrives through MediaProjection, not this hook, and
     * anything else is unknown to us).
     */
    public static String[] grantableResources(String[] requested, boolean canAudio, boolean canVideo) {
        if (requested == null || requested.length == 0) {
            return new String[0];
        }
        int count = 0;
        for (String resource : requested) {
            if (isAllowed(resource, canAudio, canVideo)) {
                count++;
            }
        }
        String[] granted = new String[count];
        int index = 0;
        for (String resource : requested) {
            if (isAllowed(resource, canAudio, canVideo)) {
                granted[index++] = resource;
            }
        }
        return granted;
    }

    /** Whether the request contains an audio-capture resource we could grant. */
    public static boolean wantsAudio(String[] requested) {
        return contains(requested, RESOURCE_AUDIO_CAPTURE);
    }

    /** Whether the request contains a video-capture resource we could grant. */
    public static boolean wantsVideo(String[] requested) {
        return contains(requested, RESOURCE_VIDEO_CAPTURE);
    }

    private static boolean isAllowed(String resource, boolean canAudio, boolean canVideo) {
        if (RESOURCE_AUDIO_CAPTURE.equals(resource)) return canAudio;
        if (RESOURCE_VIDEO_CAPTURE.equals(resource)) return canVideo;
        return false;
    }

    private static boolean contains(String[] requested, String resource) {
        if (requested == null) return false;
        for (String candidate : requested) {
            if (resource.equals(candidate)) return true;
        }
        return false;
    }
}
