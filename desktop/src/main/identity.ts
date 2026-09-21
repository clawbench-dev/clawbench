/**
 * Windows toast notification identity.
 *
 * Windows addresses toast notifications by Application User Model ID, and it
 * only surfaces one for an AUMID it can resolve to an installed shortcut. With
 * no explicit id Electron falls back to the generic `electron.app.Electron`, so
 * notifications are attributed to "Electron" rather than ClawBench — or do not
 * appear at all.
 *
 * This lives in its own module (rather than in index.ts) so tests can import it
 * without pulling in the whole main-process entry point, which requires the
 * Electron runtime.
 */

/**
 * MUST stay equal to `appId` in desktop/electron-builder.yml.
 *
 * That value is baked into the Start Menu shortcut and the exe's version
 * resource at build time, and Windows resolves the toast through it. The yml is
 * a build-time input that is NOT shipped inside app.asar, so the app cannot read
 * it back — if the two drift, Windows notifications silently stop appearing
 * with no error anywhere. `identity.test.ts` asserts they agree.
 */
export const APP_USER_MODEL_ID = 'com.xulongzhe.clawbench'
