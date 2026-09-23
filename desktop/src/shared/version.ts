/**
 * Version-string normalization shared by the desktop shell.
 *
 * The two sides of the upgrade do not spell versions the same way: the server
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
 *   - `compareVersions` parses segments with `parseInt`, and `parseInt('v1')`
 *     is NaN (coerced to 0). `compareVersions('v1.0.0', '0.99.1')` therefore
 *     returns a negative number, and the update check goes permanently silent
 *     the moment the project ships v1.0.0.
 *
 * Lives in `shared/` rather than `install.ts` because `updater.test.ts` mocks
 * `./install` wholesale (`vi.mock('./install', () => ({ httpGetBuffer: vi.fn() }))`),
 * so importing from there would yield `undefined`.
 */
export function normalizeVersion(v: string): string {
  return v.trim().replace(/^[vV]/, '')
}
