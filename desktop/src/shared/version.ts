/**
 * Version-string normalization and comparison shared by the desktop shell.
 *
 * The two sides of an upgrade do not spell versions the same way: the server
 * reports `version.Get()`, which for a release build is `git describe` output
 * and therefore carries a leading `v` (`v0.99.1`), while `app.getVersion()`
 * reads `desktop/package.json`, which the release workflow rewrites to the BARE
 * version (`0.99.1`).
 *
 * Comparing the two raw forms reports "different" for what is the same version.
 * Two call sites care:
 *
 *   - `selectStartupVersion` decides whether to hand off to the pointed
 *     version. A false difference sends it into `handOffToPointedVersion`'s
 *     self-reference guard, which DELETES the pointer — so the upgrade is
 *     silently reverted on the second cold start.
 *   - `compareVersions` parses segments, and `parseInt('v1')` is NaN (coerced
 *     to 0). `compareVersions('v1.0.0', '0.99.1')` would therefore be negative,
 *     and the update check would go permanently silent the moment the project
 *     ships v1.0.0.
 *
 * Lives in `shared/` rather than `install.ts` because `updater.test.ts` mocks
 * `./install` wholesale (`vi.mock('./install', () => ({ httpGetBuffer: vi.fn() }))`),
 * so importing from there would yield `undefined`.
 */

/**
 * Strip a leading `v`/`V` and surrounding whitespace.
 *
 * NOTE this is deliberately NOT the build-time-suffix stripper of the same name
 * in `web/src/utils/version.ts`. Here it exists so the two spellings of one
 * version compare equal for IDENTITY checks (the pointer file holds the
 * server's `v0.99.1` while `app.getVersion()` reports `0.99.1`). `compareVersions`
 * below strips the build-time suffix itself.
 */
export function normalizeVersion(v: string): string {
  return v.trim().replace(/^[vV]/, '')
}

/** A build-time suffix appended by build.sh: `-MMDDHHMM` (exactly 8 digits). */
const BUILD_TIME_SUFFIX = /^\d{8}$/

/**
 * Remove a trailing `-MMDDHHMM` build-time suffix, if present.
 *
 * Mirrors `stripBuildTimeSuffix` in `internal/version/compare.go`. The suffix is
 * build metadata, not part of the version: `v0.99.1-07291030` is the v0.99.1
 * release, and a dev build's `v0.66.0-5-gabc-07291030` is `v0.66.0-5-gabc`.
 */
function stripBuildTimeSuffix(v: string): string {
  const idx = v.lastIndexOf('-')
  if (idx < 0) return v
  return BUILD_TIME_SUFFIX.test(v.slice(idx + 1)) ? v.slice(0, idx) : v
}

/**
 * Whether a version string has a parseable `vX.Y.Z` base.
 *
 * Accepts clean releases (`v1.0.0` / `1.0.0`) and dev builds with a base
 * (`v0.66.0-5-g7702c473`); rejects short git hashes, plain `dev`, empty strings,
 * and garbage appended to the patch number (`v1.0.0garbage`).
 *
 * The leading `v` is OPTIONAL here (unlike the web/Android mirrors, which only
 * ever see `git describe` output): the desktop client reports its own version
 * without the prefix, and both sides must pass this check for the gate to fire.
 */
export function isVersionedBuild(v: string): boolean {
  return /^v?\d+\.\d+\.\d+(-|$)/.test(v)
}

/**
 * Parse one numeric version segment, taking leading digits only.
 * `"3" -> 3`, `"beta" -> 0`, `"1a" -> 1`. Mirrors `parseVersionPart` in
 * `internal/version/compare.go` (and avoids `parseInt`, which returns NaN for
 * a non-numeric segment and would poison the whole comparison).
 */
function parseVersionPart(s: string): number {
  let num = 0
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i)
    if (c < 48 || c > 57) break
    num = num * 10 + (c - 48)
  }
  return num
}

/**
 * Compare two semver-like version strings.
 *
 * Returns a negative number when a < b, 0 when equal, positive when a > b.
 * A plain `!==` check would report "mismatch" when the two sides spell the same
 * version differently (`v0.99.1` vs `0.99.1`, or a trailing build-time suffix).
 *
 * This is the AUTHORITATIVE implementation, mirroring
 * `internal/version/compare.go`, `web/src/utils/version.ts` and
 * `android/.../VersionCompare.java`. It replaced a naive per-segment `parseInt`
 * compare that ignored the build-time suffix and pre-release ordering — which
 * made the desktop client disagree with Android about the same pair of
 * versions. Semantics:
 *
 *   - strips an optional leading `v` and a `-MMDDHHMM` build-time suffix;
 *   - splits off a pre-release suffix at the first `-` and compares the dotted
 *     cores numerically (missing segments count as 0);
 *   - when the cores are equal, a version WITH a pre-release suffix is NEWER,
 *     because dev builds like `0.66.0-5-gabc` are commits made after the
 *     `0.66.0` tag.
 */
export function compareVersions(a: string, b: string): number {
  const aClean = stripBuildTimeSuffix(normalizeVersion(a))
  const bClean = stripBuildTimeSuffix(normalizeVersion(b))

  const splitPre = (v: string): [string, string] => {
    const idx = v.indexOf('-')
    return idx >= 0 ? [v.slice(0, idx), v.slice(idx + 1)] : [v, '']
  }

  const [aCore, aPre] = splitPre(aClean)
  const [bCore, bPre] = splitPre(bClean)

  const aParts = aCore.split('.')
  const bParts = bCore.split('.')
  const len = Math.max(aParts.length, bParts.length)
  for (let i = 0; i < len; i++) {
    const d = parseVersionPart(aParts[i] ?? '') - parseVersionPart(bParts[i] ?? '')
    if (d !== 0) return d
  }

  // Cores equal — a pre-release suffix means commits after the release tag.
  if (aPre !== '' && bPre === '') return 1
  if (aPre === '' && bPre !== '') return -1
  return 0
}

export interface GateOptions {
  /**
   * Whether this is a packaged (release) desktop build. The repo keeps
   * `desktop/package.json` at a placeholder version, so an unpackaged dev shell
   * would otherwise be flagged against every server it connects to.
   */
  packaged: boolean
}

/**
 * Whether the desktop client and server versions warrant the blocking
 * "version mismatch" gate.
 *
 * Mirrors Android's `VersionCompare.shouldShowMismatch`, with one deliberate
 * difference: the mismatch is checked in BOTH directions (`!== 0`), so a client
 * newer than its server is flagged too. Android only flagged an older client
 * until this feature aligned them.
 *
 * Fail-open: any side that is not a parseable versioned build (`dev`, a short
 * git hash, an empty string) never gates. An unpackaged dev build never gates.
 * This is what keeps a dev server or a dev shell from blocking login.
 */
export function shouldGate(client: string, server: string, opts: GateOptions): boolean {
  if (!opts.packaged) return false
  if (!client || !server) return false
  if (!isVersionedBuild(client) || !isVersionedBuild(server)) return false
  return compareVersions(client, server) !== 0
}
