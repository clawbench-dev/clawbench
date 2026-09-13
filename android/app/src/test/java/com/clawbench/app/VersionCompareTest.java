package com.clawbench.app;

import org.junit.Test;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

/**
 * Pure JUnit tests for {@link VersionCompare}, mirroring the web-layer suite in
 * {@code web/src/utils/__tests__/version.test.ts} so the native pre-WebView gate
 * behaves identically to the version logic the frontend used to apply.
 *
 * <p>No Android dependencies — the class under test is deliberately free of them.
 */
public class VersionCompareTest {

    // ── normalizeVersion ──────────────────────────────────────────────

    @Test
    public void normalizeVersion_stripsBuildTimeSuffix() {
        assertEquals("v1.0.0", VersionCompare.normalizeVersion("v1.0.0-07291030"));
    }

    @Test
    public void normalizeVersion_stripsBuildTimeSuffixFromDevVersion() {
        assertEquals("v0.30.0-30-g830bb6c",
                VersionCompare.normalizeVersion("v0.30.0-30-g830bb6c-07291030"));
    }

    @Test
    public void normalizeVersion_returnsUnchangedWhenNoSuffix() {
        assertEquals("v1.0.0", VersionCompare.normalizeVersion("v1.0.0"));
    }

    @Test
    public void normalizeVersion_doesNotStripNonEightDigitSuffix() {
        // "-5" is a commit count, not a build-time suffix.
        assertEquals("v1.0.0-5", VersionCompare.normalizeVersion("v1.0.0-5"));
    }

    @Test
    public void normalizeVersion_handlesShortHash() {
        assertEquals("a0f87a96", VersionCompare.normalizeVersion("a0f87a96"));
    }

    @Test
    public void normalizeVersion_handlesEmptyAndNull() {
        assertEquals("", VersionCompare.normalizeVersion(""));
        assertEquals("", VersionCompare.normalizeVersion(null));
    }

    // ── isVersionedBuild ──────────────────────────────────────────────

    @Test
    public void isVersionedBuild_acceptsCleanReleases() {
        assertTrue(VersionCompare.isVersionedBuild("v1.0.0"));
        assertTrue(VersionCompare.isVersionedBuild("v2.3.1"));
    }

    @Test
    public void isVersionedBuild_acceptsDevBuildsWithBase() {
        assertTrue(VersionCompare.isVersionedBuild("v0.30.0-30-g830bb6c"));
        assertTrue(VersionCompare.isVersionedBuild("v0.66.0-5-g7702c473"));
    }

    @Test
    public void isVersionedBuild_acceptsBuildTimeSuffix() {
        assertTrue(VersionCompare.isVersionedBuild("v0.66.0-07291030"));
    }

    @Test
    public void isVersionedBuild_rejectsShortHash() {
        assertFalse(VersionCompare.isVersionedBuild("a0f87a96"));
    }

    @Test
    public void isVersionedBuild_rejectsPlainDev() {
        assertFalse(VersionCompare.isVersionedBuild("dev"));
    }

    @Test
    public void isVersionedBuild_rejectsEmptyAndNull() {
        assertFalse(VersionCompare.isVersionedBuild(""));
        assertFalse(VersionCompare.isVersionedBuild(null));
    }

    @Test
    public void isVersionedBuild_rejectsMissingVPrefix() {
        assertFalse(VersionCompare.isVersionedBuild("1.0.0"));
    }

    @Test
    public void isVersionedBuild_rejectsGarbageAfterPatch() {
        assertFalse(VersionCompare.isVersionedBuild("v1.0.0garbage"));
    }

    // ── compareVersions ───────────────────────────────────────────────

    @Test
    public void compareVersions_returnsZeroForEqual() {
        assertEquals(0, VersionCompare.compareVersions("1.0.0", "1.0.0"));
        assertEquals(0, VersionCompare.compareVersions("v1.0.0", "1.0.0"));
        assertEquals(0, VersionCompare.compareVersions("v1.0.0", "v1.0.0"));
    }

    @Test
    public void compareVersions_returnsMinusOneWhenSmaller() {
        assertEquals(-1, VersionCompare.compareVersions("1.0.0", "1.0.1"));
        assertEquals(-1, VersionCompare.compareVersions("1.0.0", "1.1.0"));
        assertEquals(-1, VersionCompare.compareVersions("1.0.0", "2.0.0"));
        assertEquals(-1, VersionCompare.compareVersions("v1.0.0", "1.0.1"));
    }

    @Test
    public void compareVersions_returnsOneWhenLarger() {
        assertEquals(1, VersionCompare.compareVersions("1.0.1", "1.0.0"));
        assertEquals(1, VersionCompare.compareVersions("1.1.0", "1.0.0"));
        assertEquals(1, VersionCompare.compareVersions("2.0.0", "1.99.99"));
    }

    @Test
    public void compareVersions_handlesDifferentSegmentCounts() {
        assertEquals(0, VersionCompare.compareVersions("1.0", "1.0.0"));
        assertEquals(1, VersionCompare.compareVersions("1.0.1", "1.0"));
        assertEquals(-1, VersionCompare.compareVersions("1.0", "1.0.1"));
    }

    @Test
    public void compareVersions_handlesNonNumericPartsAsZero() {
        // Mirrors Go's parseVersionPart: leading digits only, else 0.
        assertEquals(-1, VersionCompare.compareVersions("0", "1.0.0"));
        assertEquals(-1, VersionCompare.compareVersions("dev", "1.0.0"));
        assertEquals(-1, VersionCompare.compareVersions("a0f87a96", "1.0.0"));
        assertEquals(1, VersionCompare.compareVersions("1.0.0", "dev"));
    }

    @Test
    public void compareVersions_parsesLeadingDigitsOnly() {
        // "1a" -> 1 (must not throw, unlike Integer.parseInt), so "1a" equals "1".
        assertEquals(0, VersionCompare.compareVersions("1a", "1"));
        assertEquals(1, VersionCompare.compareVersions("2beta", "1.9"));
        // Trailing letters are ignored, not compared.
        assertEquals(0, VersionCompare.compareVersions("1.0.0-rc", "1.0.0-rc2"));
    }

    @Test
    public void compareVersions_stripsBuildTimeSuffixBeforeComparing() {
        assertEquals(0, VersionCompare.compareVersions("v1.0.0-07291030", "1.0.0"));
        assertEquals(0, VersionCompare.compareVersions(
                "v0.70.0-5-g830bb6c-07291030", "0.70.0-5-g830bb6c"));
        assertEquals(1, VersionCompare.compareVersions(
                "v0.70.0-5-g830bb6c-07291030", "0.70.0"));
    }

    @Test
    public void compareVersions_preReleaseIsNewerThanSameRelease() {
        assertEquals(1, VersionCompare.compareVersions("v0.66.0-5-gabc", "v0.66.0"));
        assertEquals(-1, VersionCompare.compareVersions("v0.66.0", "v0.66.0-5-gabc"));
    }

    @Test
    public void compareVersions_sameBaseDifferentPreReleaseAreEqual() {
        assertEquals(0, VersionCompare.compareVersions("v0.66.0-3-gabc", "v0.66.0-5-g7702c473"));
    }

    @Test
    public void compareVersions_preReleaseWithDifferentBasesCompareByBase() {
        assertEquals(-1, VersionCompare.compareVersions("v0.65.0-10-gabc", "v0.66.0-5-g7702c473"));
        assertEquals(1, VersionCompare.compareVersions("v0.66.0-5-gabc", "v0.65.0-10-g7702c473"));
    }

    @Test
    public void compareVersions_devBuildWithBuildTimeSuffixStrips() {
        assertEquals(0, VersionCompare.compareVersions("v0.66.0-5-gabc-07291030", "v0.66.0-5-gabc"));
    }

    @Test
    public void compareVersions_shortHashWithBuildTimeSuffixStrips() {
        assertEquals(0, VersionCompare.compareVersions("a0f87a96-07291030", "a0f87a96"));
    }

    @Test
    public void compareVersions_devWithBuildTimeSuffixStrips() {
        assertEquals(0, VersionCompare.compareVersions("dev-07291030", "dev"));
    }

    // ── shouldShowMismatch ────────────────────────────────────────────

    @Test
    public void shouldShowMismatch_trueWhenApkOlderThanServer() {
        assertTrue(VersionCompare.shouldShowMismatch("v1.0.0", "v2.0.0"));
    }

    @Test
    public void shouldShowMismatch_falseWhenVersionsMatch() {
        assertFalse(VersionCompare.shouldShowMismatch("v1.0.0", "v1.0.0"));
    }

    @Test
    public void shouldShowMismatch_falseWhenApkNewerThanServer() {
        assertFalse(VersionCompare.shouldShowMismatch("v2.0.0", "v1.0.0"));
    }

    @Test
    public void shouldShowMismatch_trueForOlderDevBuild() {
        assertTrue(VersionCompare.shouldShowMismatch("v0.65.0-10-gabc", "v0.66.0-5-g7702c473"));
    }

    @Test
    public void shouldShowMismatch_falseForSameBaseDevBuilds() {
        assertFalse(VersionCompare.shouldShowMismatch("v0.66.0-3-gabc", "v0.66.0-5-g7702c473"));
    }

    @Test
    public void shouldShowMismatch_failsOpenOnEmptyOrNullAppVersion() {
        assertFalse(VersionCompare.shouldShowMismatch("", "v1.0.0"));
        assertFalse(VersionCompare.shouldShowMismatch(null, "v1.0.0"));
    }

    @Test
    public void shouldShowMismatch_failsOpenOnEmptyOrNullServerVersion() {
        assertFalse(VersionCompare.shouldShowMismatch("v1.0.0", ""));
        assertFalse(VersionCompare.shouldShowMismatch("v1.0.0", null));
    }

    @Test
    public void shouldShowMismatch_failsOpenOnNonVersionedBuilds() {
        assertFalse(VersionCompare.shouldShowMismatch("a0f87a96", "v1.0.0"));
        assertFalse(VersionCompare.shouldShowMismatch("v1.0.0", "a0f87a96"));
        assertFalse(VersionCompare.shouldShowMismatch("dev", "v1.0.0"));
        assertFalse(VersionCompare.shouldShowMismatch("v1.0.0", "dev"));
    }

    @Test
    public void shouldShowMismatch_handlesBuildTimeSuffix() {
        assertFalse(VersionCompare.shouldShowMismatch("v1.0.0", "v1.0.0-07291030"));
        assertTrue(VersionCompare.shouldShowMismatch("v1.0.0", "v2.0.0-07291030"));
    }
}
