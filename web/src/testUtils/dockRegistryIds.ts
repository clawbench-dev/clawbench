/**
 * The dock tab ids and master switches, restated as plain lists for the test
 * setup.
 *
 * WHY RESTATED: the real values are declared by the plugin modules, and the test
 * setup runs before any spec's `vi.mock` factories resolve. Globbing the plugin
 * modules there would evaluate them too early — a component spec that mocks
 * something a plugin imports (`useWideScreenLayout`, the store, …) fails at load
 * with "Cannot access 'X' before initialization". Plain data has no such
 * ordering problem.
 *
 * WHY THAT IS SAFE: `dockPluginManifest.test.ts` ("test registry helper") fails
 * if these lists and the plugin declarations disagree, in both directions. A
 * plugin added without updating this file is a test failure, not a silent gap.
 *
 * Only `web/src/test-setup.ts` should import this.
 */
export const TEST_DOCK_TAB_IDS: readonly string[] = [
  'browse',
  'view',
  'history',
  'forge',
  'tasks',
  'terminal',
  'stats',
  'mail',
  'kb',
  'live',
  'share',
  'proxy',
  'notifications',
  'settings',
]

/**
 * The server master switches the plugins declare.
 *
 * Needed by tests that APPLY a preset: a preset writes exactly this list, so an
 * empty one makes "turn every feature off" a no-op that still passes a shallow
 * assertion.
 */
export const TEST_DOCK_FEATURES: readonly string[] = [
  'forge.enabled',
  'kb.enabled',
  'mail.enabled',
  'share.enabled',
  'tasks.enabled',
  'terminal.enabled',
]
