import { describe, expect, it, vi } from 'vitest'

/**
 * Smoke-imports every manifest and resolves the hooks that take no arguments,
 * so the "declarative" side of each plugin is executed at least once.
 *
 * Why this exists: these files are read by the registry at import time and
 * never mounted by any component test, so a manifest whose callbacks are never
 * called is the one place a runtime typo (a wrong i18n key, an undefined
 * constant) survives the whole suite. The heavier behaviour (deeplinks, badges,
 * lifecycle) is covered by the focused tests next to this file; this one only
 * proves each manifest can answer without an app.
 */

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => ({ getCapabilities: () => ({ tunnel: true }) }),
}))

// Partially mocked: other modules read STORAGE_KEY etc. from this module, so
// only the translation call is stubbed.
vi.mock('@/i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/i18n')>()
  return { ...actual, default: { global: { t: (key: string) => key } } }
})

const modules = import.meta.glob<{ default: Record<string, unknown> }>('@/plugins/*/plugin.ts', {
  eager: true,
})

const ARGLESS_HOOKS = [
  'available',
  'leaveWhenUnavailable',
  'badge',
  'title',
  'dynamicIcon',
  'startBackground',
] as const

describe('every plugin manifest answers its host', () => {
  const entries = Object.entries(modules).map(([path, mod]) => [path.split('/').at(-2) ?? '', mod.default] as const)

  it.each(entries)('%s resolves its argless hooks', (_dir, manifest) => {
    for (const hook of ARGLESS_HOOKS) {
      const fn = manifest[hook]
      if (typeof fn !== 'function') continue
      // Each of these is called by the dock while rendering the rail. A throw
      // here takes the rail down, so the value only has to be produced — the
      // semantic checks live in the dedicated tests.
      expect(() => (fn as () => unknown)(), `${String(hook)} threw`).not.toThrow()
      if (hook === 'startBackground') {
        // Lifecycle hooks return a stopper; call it so the import cannot leak a
        // timer into the rest of the run.
        const stop = (fn as () => unknown)()
        if (typeof stop === 'function') (stop as () => void)()
      }
    }
  })

  // The three built-in surfaces whose (large) prop surfaces still live in
  // App.vue instead of a plugin panel. They are the only manifests allowed to
  // declare no panel; a NEW tab must bring its own, or it would open nothing.
  const HOST_RENDERED = new Set(['browse', 'view', 'chat'])

  it.each(entries)('%s is renderable by some channel', (dir, manifest) => {
    const hasPanel = typeof manifest.panel === 'object' && manifest.panel !== null
    const hasOnClick = typeof manifest.onClick === 'function'
    expect(
      hasPanel || hasOnClick || HOST_RENDERED.has(dir),
      `${dir} declares no panel and is not one of the App.vue-rendered surfaces`,
    ).toBe(true)
  })
})
