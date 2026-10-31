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
 * hook is {@code WebChromeClient.onPermissionRequest}, so voice input cannot
 * work until the app answers that callback.
 */
public final class MicPermissionPolicy {

    /** Value of {@code PermissionRequest.RESOURCE_AUDIO_CAPTURE}. */
    public static final String RESOURCE_AUDIO_CAPTURE = "android.webkit.resource.AUDIO_CAPTURE";

    private MicPermissionPolicy() {
    }

    /**
     * Returns the subset of {@code requested} that may be granted: audio capture
     * only.
     *
     * Video capture is intentionally refused — ClawBench has no WebRTC call
     * feature, and granting it would hand any page in the WebView the camera
     * without its own permission flow. An empty result means "deny".
     */
    public static String[] grantableResources(String[] requested) {
        if (requested == null || requested.length == 0) {
            return new String[0];
        }
        int count = 0;
        for (String resource : requested) {
            if (RESOURCE_AUDIO_CAPTURE.equals(resource)) {
                count++;
            }
        }
        String[] granted = new String[count];
        int index = 0;
        for (String resource : requested) {
            if (RESOURCE_AUDIO_CAPTURE.equals(resource)) {
                granted[index++] = resource;
            }
        }
        return granted;
    }
}
