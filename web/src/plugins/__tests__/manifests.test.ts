import { describe, expect, it, vi } from 'vitest'

/**
 * Contract test for the dock plugin manifests.
 *
 * Every `plugin.ts` is loaded by the registry at import time (via
 * `import.meta.glob`), which makes the manifest files themselves the one part
 * of the plugin system that no component test ever exercises directly: a typo
 * in an icon, a `settingsCategory` that names no settings page, or an
 * `available()` predicate that throws would only show up in the app.
 *
 * The assertions here are deliberately structural, not snapshot: each manifest
 * must answer the questions the registry asks it (a boolean availability, a
 * stable id that matches its directory), and the predicates must be callable
 * without a mounted component — if one of them starts reading a store that only
 * exists after login, this test fails before the dock does.
 */

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => ({ getCapabilities: () => ({ tunnel: true }) }),
}))

vi.mock('@/stores/app', async () => {
  const { reactive } = await import('vue')
  return {
    store: reactive({
      state: {
        portForwardEnabledCount: 0,
        currentSession: null,
        agentCount: 0,
        taskUnreadCount: 0,
        forgeUnreadCount: 0,
        meetingActive: false,
        mailUnreadCount: 0,
        shareCount: 0,
        sessions: [],
      },
    }),
  }
})

interface Manifest {
  id: string
  titleKey: string
  descriptionKey?: string
  group?: string
  closable?: boolean
  icon?: unknown
  capabilities?: unknown
  settingsCategory?: string
  badge?: unknown
  available?: () => boolean
  onEnter?: (...args: unknown[]) => unknown
  onLeave?: (...args: unknown[]) => unknown
}

// Every manifest module in the tree. `eager` mirrors the registry, so a file
// that fails to load fails here too.
const modules = import.meta.glob<{ default: Manifest }>('@/plugins/*/plugin.ts', { eager: true })

function manifests(): Array<{ dir: string; manifest: Manifest }> {
  return Object.entries(modules).map(([path, mod]) => {
    const dir = path.split('/').at(-2) ?? ''
    return { dir, manifest: mod.default }
  })
}

describe('dock plugin manifests', () => {
  it('loads every plugin file', () => {
    // A guard against the glob silently matching nothing (renamed directory or
    // changed import base), which would make every assertion below vacuous.
    expect(manifests().length).toBeGreaterThanOrEqual(14)
  })

  it.each(manifests().map(({ dir, manifest }) => [dir, manifest] as const))(
    '%s declares an id that matches its directory and a translatable title',
    (dir, manifest) => {
      // The registry keys availability, order and settings by this id, so a
      // copy-pasted manifest with the wrong id would make a tab vanish and
      // another appear — with no error anywhere.
      expect(manifest.id).toBe(dir)
      // The title is a locale path, not the tab's name (`nav.fileManager` for
      // the browse tab), so it is checked for shape and resolvability rather
      // than for containing the id.
      expect(manifest.titleKey).toMatch(/^[a-z][A-Za-z0-9]*(\.[A-Za-z0-9]+)+$/)
    },
  )

  it.each(manifests().map(({ dir, manifest }) => [dir, manifest] as const))(
    '%s answers "available" with a boolean and does not throw',
    (dir, manifest) => {
      if (!manifest.available) return
      // The dock calls this while rendering the rail — before any panel mounts.
      // It must be total: a predicate that throws takes the whole dock down.
      let verdict: unknown
      expect(() => {
        verdict = manifest.available!()
      }, `${dir}.available() threw`).not.toThrow()
      expect(typeof verdict, `${dir}.available() must return a boolean`).toBe('boolean')
    },
  )

  it('declares settings categories that exist in the settings field map', async () => {
    const { categoryItems } = await import('@/components/settings/settingsFieldMap')
    for (const { dir, manifest } of manifests()) {
      if (!manifest.settingsCategory) continue
      // A category id with no entry renders an empty page, so the gear would
      // look live and do nothing.
      expect(
        Object.prototype.hasOwnProperty.call(categoryItems, manifest.settingsCategory),
        `${dir} points at settings category "${manifest.settingsCategory}", which does not exist`,
      ).toBe(true)
    }
  })

  it('gives every closable tab the fields the plugin manager renders', () => {
    // Hiding a tab is display-only, so a hidden tab must still be listed in the
    // plugin manager — which needs a description line and a group heading. The
    // chat tab is exempt from the description because the manager never hides
    // it (it is the default surface), but any OTHER closable tab needs one.
    const groups = new Set(manifests().map(({ manifest }) => manifest.group))
    for (const { dir, manifest } of manifests()) {
      if (manifest.closable === false || manifest.id === 'chat') continue
      expect(manifest.descriptionKey, `${dir} is closable but has no description`).toBeTruthy()
      expect(manifest.group, `${dir} is closable but declares no group`).toBeTruthy()
    }
    expect(groups.size, 'the manager groups rows by this value').toBeGreaterThan(1)
  })
})
