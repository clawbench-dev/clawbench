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

/** Whether the version mismatch dialog should be shown */
export function shouldShowMismatch(appVersion: string, serverVersion: string): boolean {
  if (!appVersion || !serverVersion) return false
  const normalized = normalizeVersion(serverVersion)
  if (!isReleaseVersion(normalized) || !isReleaseVersion(appVersion)) return false
  return appVersion !== normalized
}
