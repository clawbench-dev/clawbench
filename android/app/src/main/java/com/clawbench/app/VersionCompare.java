package com.clawbench.app;

import java.util.regex.Pattern;

/**
 * Pure-Java version comparison for the native APK-vs-server mismatch gate.
 *
 * <p>Mirrors the authoritative semantics in {@code web/src/utils/version.ts},
 * which in turn matches {@code internal/version/compare.go}. Kept free of Android
 * imports so it is trivially unit-testable on the JVM.
 *
 * <p>All comparisons are <b>fail-open</b>: a version that cannot be parsed as a
 * {@code vX.Y.Z} build (short git hash, {@code "dev"}, empty) never triggers a
 * mismatch prompt.
 */
final class VersionCompare {

    private VersionCompare() {
    }

    /** A build with a parseable {@code vX.Y.Z} base: a clean release or a dev build. */
    private static final Pattern VERSIONED_BUILD = Pattern.compile("^v\\d+\\.\\d+\\.\\d+(-|$)");

    /** The build-time suffix format appended by build.sh: mmddHHMM. */
    private static final Pattern BUILD_TIME_SUFFIX = Pattern.compile("\\d{8}");

    /**
     * Strips a trailing build-time suffix ({@code -MMDDHHMM}, exactly 8 digits after
     * the last {@code -}). Mirrors {@code normalizeVersion} in version.ts.
     *
     * <p>Examples: {@code "v1.0.0-07291030" -> "v1.0.0"},
     * {@code "v0.30.0-30-g830bb6c-05211030" -> "v0.30.0-30-g830bb6c"}.
     */
    static String normalizeVersion(String v) {
        if (v == null) return "";
        int idx = v.lastIndexOf('-');
        if (idx < 0) return v;
        String suffix = v.substring(idx + 1);
        if (BUILD_TIME_SUFFIX.matcher(suffix).matches()) {
            return v.substring(0, idx);
        }
        return v;
    }

    /**
     * Whether the version has a parseable {@code vX.Y.Z} base. Accepts clean releases
     * ("v1.0.0") and dev builds ("v0.66.0-5-g7702c473"); rejects short hashes, plain
     * "dev", empty strings, versions without the "v" prefix, and garbage appended to
     * the patch number ("v1.0.0garbage").
     */
    static boolean isVersionedBuild(String v) {
        if (v == null || v.isEmpty()) return false;
        return VERSIONED_BUILD.matcher(v).find();
    }

    /**
     * Compares two semver-like versions. Returns -1 if {@code a < b}, 0 if equal,
     * 1 if {@code a > b}.
     *
     * <p>Strips the optional "v" prefix and the build-time suffix, splits off a
     * pre-release suffix at the first "-", then compares the dotted cores
     * numerically (missing segments count as 0). When the cores are equal, a version
     * <b>with</b> a pre-release suffix is considered <b>newer</b> — dev builds like
     * "0.66.0-5-gabc" are commits made after the "0.66.0" tag.
     */
    static int compareVersions(String a, String b) {
        String aClean = normalizeVersion(stripLeadingV(a));
        String bClean = normalizeVersion(stripLeadingV(b));

        String[] aSplit = splitPreRelease(aClean);
        String[] bSplit = splitPreRelease(bClean);
        String aCore = aSplit[0];
        String aPre = aSplit[1];
        String bCore = bSplit[0];
        String bPre = bSplit[1];

        String[] aParts = aCore.split("\\.", -1);
        String[] bParts = bCore.split("\\.", -1);
        int maxLen = Math.max(aParts.length, bParts.length);

        for (int i = 0; i < maxLen; i++) {
            int aNum = i < aParts.length ? parseVersionPart(aParts[i]) : 0;
            int bNum = i < bParts.length ? parseVersionPart(bParts[i]) : 0;
            if (aNum < bNum) return -1;
            if (aNum > bNum) return 1;
        }

        // Cores equal — a pre-release suffix means commits after the release tag.
        if (!aPre.isEmpty() && bPre.isEmpty()) return 1;
        if (aPre.isEmpty() && !bPre.isEmpty()) return -1;
        return 0;
    }

    /**
     * Whether the installed APK should be flagged as older than the server.
     *
     * <p>Fail-open: returns false when either side is missing or is not a versioned
     * build, so servers/APKs without a comparable version never block login.
     */
    static boolean shouldShowMismatch(String appVersion, String serverVersion) {
        if (appVersion == null || appVersion.isEmpty()) return false;
        if (serverVersion == null || serverVersion.isEmpty()) return false;

        String normalizedApp = normalizeVersion(appVersion);
        String normalizedServer = normalizeVersion(serverVersion);

        if (!isVersionedBuild(normalizedApp) || !isVersionedBuild(normalizedServer)) return false;

        return compareVersions(normalizedApp, normalizedServer) < 0;
    }

    private static String stripLeadingV(String v) {
        if (v == null) return "";
        return v.startsWith("v") ? v.substring(1) : v;
    }

    /**
     * Splits a version into {@code [core, preRelease]} at the first "-".
     * Mirrors {@code splitPreRelease} in compare.go.
     */
    private static String[] splitPreRelease(String v) {
        int idx = v.indexOf('-');
        if (idx >= 0) {
            return new String[]{v.substring(0, idx), v.substring(idx + 1)};
        }
        return new String[]{v, ""};
    }

    /**
     * Parses a single numeric segment, taking leading digits only.
     * "3" -> 3, "beta" -> 0, "1a" -> 1. Deliberately avoids {@code Integer.parseInt},
     * which would throw on non-numeric segments. Mirrors {@code parseVersionPart}.
     */
    private static int parseVersionPart(String s) {
        int num = 0;
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c >= '0' && c <= '9') {
                num = num * 10 + (c - '0');
            } else {
                break;
            }
        }
        return num;
    }
}
