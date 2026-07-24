/**
 * Version comparison utilities for APK vs server version mismatch detection.
 */

/** Strip build time suffix for comparison (e.g. "v1.0.0 (2026-05-21 10:30:00)" → "v1.0.0") */
export function normalizeVersion(v: string): string {
  return v.replace(/ *\([^)]*\)$/, '')
}

/** Check if a version string is a formal release (e.g. "v1.0.0", "v2.3.1"), not dev/pre-release */
export function isReleaseVersion(v: string): boolean {
  return /^v\d+\.\d+\.\d+$/.test(v)
}

/**
 * Check if a version string is a versioned build (has a vX.Y.Z base).
 * Accepts both clean releases ("v1.0.0") and dev builds ("v0.66.0-5-g7702c473").
 * Rejects short hashes ("a0f87a96"), plain "dev", and empty strings.
 */
export function isVersionedBuild(v: string): boolean {
  return /^v\d+\.\d+\.\d+/.test(v)
}

/**
 * Extract the base version (major.minor.patch) from a version string.
 * e.g. "v0.66.0-5-g7702c473" → "v0.66.0", "v1.0.0" → "v1.0.0"
 */
export function extractBaseVersion(v: string): string {
  const match = v.match(/^(v\d+\.\d+\.\d+)/)
  return match ? match[1] : v
}

/**
 * Whether the version mismatch dialog should be shown.
 * Compares base versions and shows when APK is older than server.
 * Works for both release and dev builds with a vX.Y.Z base.
 */
export function shouldShowMismatch(appVersion: string, serverVersion: string): boolean {
  if (!appVersion || !serverVersion) return false
  const normalizedServer = normalizeVersion(serverVersion)
  // Both must have a parseable vX.Y.Z base version
  if (!isVersionedBuild(appVersion) || !isVersionedBuild(normalizedServer)) return false
  // Show only when APK is older than server (needs upgrade)
  return compareVersions(appVersion, normalizedServer) < 0
}

/**
 * Compare two semver-like version strings.
 * Strips optional "v" prefix and pre-release suffix before comparison.
 * Pre-release builds (e.g. "v0.66.0-5-gabc") are considered newer than
 * the same release version ("v0.66.0") since they include commits after the tag.
 * Returns -1 if a < b, 0 if a == b, 1 if a > b.
 */
export function compareVersions(a: string, b: string): number {
  const strip = (v: string) => v.replace(/^v/, '')
  // Strip build-time suffix like " (2026-07-24 10:30:00)"
  const stripBuildTime = (v: string) => v.replace(/ *\([^)]*\)$/, '')

  const aClean = stripBuildTime(strip(a))
  const bClean = stripBuildTime(strip(b))

  // Split off pre-release suffix (after first '-')
  const splitPre = (v: string): [string, string] => {
    const idx = v.indexOf('-')
    return idx >= 0 ? [v.slice(0, idx), v.slice(idx + 1)] : [v, '']
  }

  const [aCore, aPre] = splitPre(aClean)
  const [bCore, bPre] = splitPre(bClean)

  const aParts = aCore.split('.').map(Number)
  const bParts = bCore.split('.').map(Number)
  const maxLen = Math.max(aParts.length, bParts.length)
  for (let i = 0; i < maxLen; i++) {
    const aNum = aParts[i] ?? 0
    const bNum = bParts[i] ?? 0
    if (aNum < bNum) return -1
    if (aNum > bNum) return 1
  }

  // Core versions are equal — compare pre-release
  // A version with a pre-release suffix is newer than the same version without one
  // (dev builds like "0.66.0-5-gabc" are commits after the "0.66.0" release).
  if (aPre !== '' && bPre === '') return 1
  if (aPre === '' && bPre !== '') return -1

  return 0
}
