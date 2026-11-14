package com.clawbench.app;

import java.net.URI;
import java.util.HashSet;
import java.util.Locale;
import java.util.Set;

/**
 * Server addresses whose self-signed certificate the user has already accepted.
 *
 * <p>Android used to keep this answer in a single boolean field
 * ({@code sslCertTrustedByUser}) that was cleared on every connect attempt and
 * on every return to the login page. The user therefore had to confirm the same
 * self-signed certificate again on every cold start and after every failed
 * connection — "trust and continue" never lasted longer than one attempt.
 *
 * <p>The answer is now remembered per ORIGIN (scheme + host + port), so a
 * different port is a different server and the same server is asked only once.
 * It is deliberately keyed by address rather than by certificate fingerprint:
 * a self-hosted server that regenerates its certificate (the HTTPS panel does
 * this whenever the machine's addresses change) is still the same machine, and
 * re-asking on every regeneration is exactly the nagging this replaces. The
 * explicit way out is removing the server from the list, which drops its entry
 * (see {@link #withoutTrusted}).
 *
 * <p>Kept free of Android imports so the decision logic is unit-testable on the
 * JVM; the {@code SharedPreferences} plumbing lives in {@code MainActivity}.
 */
public final class CertificateTrust {

    private CertificateTrust() {}

    /**
     * The key this URL is remembered under, or '' when it cannot be parsed.
     *
     * <p>WebSocket URLs fold onto their HTTP equivalent, mirroring the desktop
     * shell: the socket of a page reaches the same server and presents the same
     * certificate, so one entry has to cover both.
     */
    public static String originOf(String url) {
        if (url == null) return "";
        String trimmed = url.trim();
        if (trimmed.isEmpty()) return "";
        try {
            URI uri = new URI(trimmed);
            String scheme = uri.getScheme();
            String host = uri.getHost();
            if (scheme == null || host == null) return "";
            scheme = scheme.toLowerCase(Locale.ROOT);
            if ("wss".equals(scheme)) {
                scheme = "https";
            } else if ("ws".equals(scheme)) {
                scheme = "http";
            }
            if (!"https".equals(scheme) && !"http".equals(scheme)) return "";
            int port = uri.getPort();
            int defaultPort = "https".equals(scheme) ? 443 : 80;
            String authority = host.toLowerCase(Locale.ROOT);
            if (port == -1 || port == defaultPort) {
                return scheme + "://" + authority;
            }
            return scheme + "://" + authority + ":" + port;
        } catch (Exception e) {
            return "";
        }
    }

    /** Whether the user already accepted a certificate for this URL's origin. */
    public static boolean isTrusted(Set<String> trustedOrigins, String url) {
        String origin = originOf(url);
        if (origin.isEmpty() || trustedOrigins == null) return false;
        return trustedOrigins.contains(origin);
    }

    /**
     * A copy of {@code trustedOrigins} with this URL's origin added.
     *
     * <p>Always returns a new set: {@code SharedPreferences.getStringSet} hands
     * out a read-only view, and mutating it in place throws at runtime.
     */
    public static Set<String> withTrusted(Set<String> trustedOrigins, String url) {
        Set<String> next = new HashSet<>();
        if (trustedOrigins != null) next.addAll(trustedOrigins);
        String origin = originOf(url);
        if (!origin.isEmpty()) next.add(origin);
        return next;
    }

    /**
     * A copy of {@code trustedOrigins} with this URL's origin removed.
     *
     * <p>Used when the user deletes a server: forgetting the address means the
     * next time it is added back (possibly a different machine behind the same
     * address) the certificate is confirmed again.
     */
    public static Set<String> withoutTrusted(Set<String> trustedOrigins, String url) {
        Set<String> next = new HashSet<>();
        if (trustedOrigins != null) next.addAll(trustedOrigins);
        String origin = originOf(url);
        if (!origin.isEmpty()) next.remove(origin);
        return next;
    }
}
