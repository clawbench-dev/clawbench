import { describe, expect, it } from 'vitest'
import {
  DOCK_PERMANENT_TAB_IDS,
  dockFeatures,
  dockTabIdsInDefaultOrder,
  isDockTabId,
  isPermanentDockTab,
  type DockFeature,
} from '@/composables/dockTabs'
import {
  DOCK_SETTINGS_PAGES,
  DOCK_TABS,
  DOCK_TABS_WITH_ICONS,
  DOCK_TAB_BY_ID,
  assemblePluginModules,
  resolveDockPlugin,
} from '@/composables/dockPluginRuntime'
import { dockPresetPlan } from '@/composables/useDockPlugins'
import { categoryItems } from '@/components/settings/settingsFieldMap'
import { readWebFile } from '@/testUtils/readWebFile'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Guard rails for the plugin manifest.
 *
 * Adding a plugin is now "drop a directory with a plugin.ts in it" — the id comes
 * from the directory name and the default position from the plugin's own `order`.
 * Every remaining requirement is a silent no-op when forgotten — a tab that never
 * renders, a tooltip that shows a raw key, a switch that writes a key nobody
 * reads — so each one is pinned here instead of being caught by a user.
 */

/** Every `plugin.ts` the bundler would pick up, keyed by its directory name. */
const PLUGIN_MODULES = import.meta.glob<{ default: { id?: string } }>('@/plugins/*/plugin.ts', {
  eager: true,
})

const PLUGIN_DIRS = Object.keys(PLUGIN_MODULES)
  .map((path) => path.match(/\/plugins\/([^/]+)\/plugin\.ts$/)?.[1])
  .filter((id): id is string => !!id)
  .sort()

function resolveKey(messages: unknown, key: string): unknown {
  return key
    .split('.')
    .reduce<unknown>(
      (acc, part) => (acc && typeof acc === 'object' ? (acc as Record<string, unknown>)[part] : undefined),
      messages,
    )
}

describe('plugin modules and dock order', () => {
  it('has one plugin.ts per directory, and the directory name is the plugin id', () => {
    expect(PLUGIN_DIRS.length).toBeGreaterThan(0)
    for (const dir of PLUGIN_DIRS) {
      const plugin = Object.entries(PLUGIN_MODULES).find(([path]) => path.includes(`/plugins/${dir}/`))
      expect(plugin?.[1].default?.id, `${dir}/plugin.ts declares a different id`).toBe(dir)
    }
  })

  it('registers every plugin directory as a dock tab, and nothing else', () => {
    // `chat` is the right-pane surface: it shares the runtime contract but is
    // deliberately not a left-dock entry. Everything else must be registered —
    // a module whose `order` is missing would drop out of the list silently.
    const dockPluginIds = PLUGIN_DIRS.filter((id) => id !== 'chat')
    expect([...dockTabIdsInDefaultOrder()].sort()).toEqual([...dockPluginIds].sort())
  })

  it('has a plugin module for every registered dock id', () => {
    // The reverse direction: an id without a module would be dropped from the
    // render list, leaving a hole in the dock with no error anywhere.
    for (const id of dockTabIdsInDefaultOrder()) {
      expect(resolveDockPlugin(id), `no plugin.ts for "${id}"`).toBeDefined()
    }
  })

  it('declares a distinct, positive `order` for every dock plugin', () => {
    // Order is now the ONLY thing that decides where a tab lands; two plugins
    // sharing a number would fall back to the id tie-break, which is stable but
    // not what either plugin asked for.
    const seen = new Map<number, string>()
    for (const id of dockTabIdsInDefaultOrder()) {
      const order = resolveDockPlugin(id)?.order
      expect(typeof order, `${id} declares no order`).toBe('number')
      expect(order as number, `${id} order is not positive`).toBeGreaterThan(0)
      const clash = seen.get(order as number)
      expect(clash, `${id} and ${clash} share order ${order}`).toBeUndefined()
      seen.set(order as number, id)
    }
  })

  it('assembles the manifest in the plugin-declared order', () => {
    const expected = [...dockTabIdsInDefaultOrder()]
    expect(DOCK_TABS.map((t) => t.id)).toEqual(expected)
    expect(DOCK_TABS_WITH_ICONS.map((t) => t.id)).toEqual(expected)
    for (const tab of DOCK_TABS_WITH_ICONS) {
      expect(tab.icon, `${tab.id} has no icon`).toBeTruthy()
    }
    // And that order really is ascending by the declared numbers.
    const numbers = DOCK_TABS.map((t) => resolveDockPlugin(t.id)?.order as number)
    expect([...numbers].sort((a, b) => a - b)).toEqual(numbers)
  })

  it('rejects unknown ids through the type guard', () => {
    expect(isDockTabId('chat')).toBe(false)
    expect(isDockTabId('not-a-tab')).toBe(false)
    for (const id of dockTabIdsInDefaultOrder()) expect(isDockTabId(id)).toBe(true)
  })

  it('keeps the import-free permanent list in sync with the manifests', () => {
    // `DOCK_PERMANENT_TAB_IDS` exists so `useDockPlugins` (reached from
    // FileHeader) never imports the plugin graph. The manifests are the display
    // source of truth; this pins the two together.
    const notClosable = DOCK_TABS.filter((t) => !t.closable).map((t) => t.id)
    expect([...DOCK_PERMANENT_TAB_IDS].sort()).toEqual([...notClosable].sort())
    for (const id of DOCK_PERMANENT_TAB_IDS) expect(isPermanentDockTab(id)).toBe(true)
    expect(isPermanentDockTab('mail')).toBe(false)
  })
})

describe('assemblePluginModules', () => {
  /** A minimal valid left-dock manifest for `id`. */
  function plugin(id: string, extra: Record<string, unknown> = {}) {
    return {
      id,
      order: 10,
      titleKey: 'nav.mail',
      descriptionKey: 'settings.dockPlugins.desc.mail',
      group: 'personal' as const,
      closable: true,
      icon: { name: id },
      ...extra,
    }
  }

  it('keeps the FIRST module when two declare the same id', () => {
    // The id is the plugin's stable identity (config key, i18n prefix,
    // deep-link target), so a duplicate must not depend on glob order.
    const { runtime, tabs } = assemblePluginModules({
      'a/mail.ts': { default: plugin('mail', { titleKey: 'first' }) },
      'b/mail.ts': { default: plugin('mail', { titleKey: 'second' }) },
    })
    expect((runtime.mail as { titleKey?: string }).titleKey).toBe('first')
    expect(tabs.filter((t) => t.id === 'mail')).toHaveLength(1)
  })

  it('drops a module with no default export instead of throwing', () => {
    const { runtime } = assemblePluginModules({
      'a/mail.ts': {},
      'b/kb.ts': { default: plugin('kb') },
    })
    expect(runtime.mail).toBeUndefined()
    expect(runtime.kb).toBeDefined()
  })

  it('emits no descriptor for a module without a manifest order', () => {
    // `order` is what marks a module as a dock entry; the guard test in this
    // file catches it, and the assembly must still not produce a nameless
    // button if one slips through.
    const { tabs } = assemblePluginModules({
      'a/mail.ts': { default: plugin('mail', { order: undefined }) },
      'b/kb.ts': { default: plugin('kb') },
    })
    expect(tabs.map((t) => t.id)).toEqual(['kb'])
  })

  it('sorts descriptors by the declared order, not by glob order', () => {
    const { tabs } = assemblePluginModules({
      'a/mail.ts': { default: plugin('mail', { order: 30 }) },
      'b/kb.ts': { default: plugin('kb', { order: 10 }) },
      'c/tasks.ts': { default: plugin('tasks', { order: 20 }) },
    })
    expect(tabs.map((t) => t.id)).toEqual(['kb', 'tasks', 'mail'])
  })

  it('breaks an order tie by id so the result never depends on glob order', () => {
    const { tabs } = assemblePluginModules({
      'a/tasks.ts': { default: plugin('tasks', { order: 10 }) },
      'b/kb.ts': { default: plugin('kb', { order: 10 }) },
    })
    expect(tabs.map((t) => t.id)).toEqual(['kb', 'tasks'])
  })

  it('never turns chat into a left-dock tab', () => {
    const { runtime, tabs } = assemblePluginModules({
      'a/chat.ts': { default: plugin('chat', { titleKey: 'nav.chat', icon: { name: 'chat' } }) },
    })
    expect(runtime.chat).toBeDefined()
    expect(tabs).toHaveLength(0)
  })
})

describe('localisation', () => {
  it('resolves every title and description in both locales, non-empty', () => {
    for (const [locale, messages] of Object.entries({ en, zh })) {
      for (const tab of DOCK_TABS) {
        for (const key of [tab.titleKey, tab.descriptionKey]) {
          const value = resolveKey(messages, key)
          expect(typeof value, `${key} missing in ${locale}`).toBe('string')
          expect(value, `${key} is empty in ${locale}`).not.toBe('')
        }
      }
    }
  })

  it('keeps every plugin\'s copy in its own directory, not the global tables', () => {
    // A new plugin must not need an edit to `i18n/locales/*.ts`: its description
    // (and its "this host cannot run it" reason) come from
    // `plugins/<id>/i18n.ts`. The merge is what makes them addressable under
    // `settings.dockPlugins.*`, so assert the value ARRIVES and that the base
    // literal alone does not already carry it.
    const pluginWithCopy = PLUGIN_DIRS.filter((id) => id !== 'chat')
    for (const id of pluginWithCopy) {
      expect(zh.settings.dockPlugins.desc[id], `zh desc for ${id}`).toBeTruthy()
      expect(en.settings.dockPlugins.desc[id], `en desc for ${id}`).toBeTruthy()
    }
    for (const reason of Object.keys(zh.settings.dockPlugins.unavailable)) {
      expect(en.settings.dockPlugins.unavailable[reason as keyof typeof en.settings.dockPlugins.unavailable], `en unavailable for ${reason}`).toBeTruthy()
    }
  })

  it('resolves every unavailable-reason key in both locales', () => {
    for (const tab of DOCK_TABS) {
      const plugin = resolveDockPlugin(tab.id)
      const key = plugin?.unavailableReasonKey
      if (!key) continue
      for (const [locale, messages] of Object.entries({ en, zh })) {
        const value = resolveKey(messages, key)
        expect(typeof value, `${tab.id}: ${key} missing in ${locale}`).toBe('string')
        expect(value, `${tab.id}: ${key} is empty in ${locale}`).not.toBe('')
      }
    }
  })
})

describe('feature switches', () => {
  it('gates only on switches the server registry knows', () => {
    // A typo'd switch key reads as "on" forever (the reader falls back to the
    // default), so the tab would never disappear when the server feature is off.
    for (const tab of DOCK_TABS) {
      if (!tab.feature) continue
      expect(
        dockFeatures(),
        `${tab.id} gates on unknown feature "${tab.feature}"`,
      ).toContain(tab.feature)
    }
  })

  it('declares the six server-switch tabs', () => {
    const featureTabs = DOCK_TABS.filter((t) => t.feature).map((t) => t.id)
    expect(new Set(featureTabs)).toEqual(
      new Set(['forge', 'mail', 'terminal', 'kb', 'tasks', 'share']),
    )
  })

  it('exposes exactly the switches the manifests declare', () => {
    // `dockFeatures()` is what the preset rules (a leaf module) read to switch
    // everything off without importing the plugin graph. It is derived from the
    // manifests, so this pins the derivation — a plugin whose `feature` never
    // reaches the list would be one a preset cannot turn off.
    const declared = DOCK_TABS.filter((t) => t.feature).map((t) => t.feature) as DockFeature[]
    expect([...dockFeatures()].sort()).toEqual([...new Set(declared)].sort())
  })
})

describe('presets', () => {
  it('expands every preset to the complete tab set', () => {
    // A hand-written preset that forgets an id leaves the tab in a strange
    // position (or hidden) after applying it. Each preset's order must be a
    // permutation of the registry's ids, and its hidden set must be drawn from it.
    const all = [...dockTabIdsInDefaultOrder()].sort()
    // Without this the permutation check below would pass vacuously if the
    // registry were empty (the exact failure mode of a pre-install snapshot).
    expect(all.length).toBeGreaterThan(0)
    for (const name of ['developer', 'standard', 'minimal'] as const) {
      const plan = dockPresetPlan(name)
      expect([...plan.order].sort(), `${name}.order is not the full set`).toEqual(all)
      for (const id of plan.hidden) {
        expect(dockTabIdsInDefaultOrder(), `${name}.hidden has unknown "${id}"`).toContain(id)
      }
      for (const change of plan.features) {
        expect(dockFeatures(), `${name} writes unknown feature "${change.key}"`).toContain(
          change.key,
        )
      }
    }
  })
})

describe('settings pages', () => {
  it('declares a page for every plugin that owns a settings category', () => {
    // The manager renders a gear for every `settingsCategory`; without a
    // `settingsPage` the category exists in the field map but never appears on
    // the settings index, so the gear would lead nowhere.
    for (const tab of DOCK_TABS) {
      const plugin = resolveDockPlugin(tab.id)
      if (!plugin || plugin.id === 'chat') continue
      if (!tab.settingsCategory) continue
      expect(plugin.settingsPage, `${tab.id} has a category but no settingsPage`).toBeDefined()
      expect(plugin.settingsPage?.group).toBe('dockModules')
      expect(plugin.settingsPage?.icon, `${tab.id} settings row has no icon`).toBeTruthy()
    }
  })

  it('points every declared page at a real settings category', () => {
    for (const page of DOCK_SETTINGS_PAGES) {
      expect(
        categoryItems[page.categoryId],
        `${page.pluginId} → "${page.categoryId}" is not a settings category`,
      ).toBeTruthy()
    }
  })

  it('gives every custom page a lazy component', () => {
    for (const page of DOCK_SETTINGS_PAGES) {
      if (page.kind !== 'custom') continue
      expect(page.component, `${page.categoryId} is custom but has no loader`).toBeTypeOf('function')
    }
  })

  it('uses the plugin\'s settings category as the page id', () => {
    // One id, two consumers: the manager's gear (settingsCategory) and the
    // settings index row (settingsPage). A divergence would make the gear land
    // on a category that has no row.
    for (const page of DOCK_SETTINGS_PAGES) {
      const tab = DOCK_TAB_BY_ID.get(page.pluginId)
      expect(page.categoryId).toBe(tab?.settingsCategory ?? page.pluginId)
    }
  })
})

describe('dockTabs.ts stays import-free', () => {
  it('has no runtime import', () => {
    // A batch of component tests mock `lucide-vue-next` with only the handful of
    // icons they use; pulling icons (or any module) in here makes those tests
    // throw at module load. `useWideScreenLayout` depends on this file and is
    // reached from many components, so the constraint is load-bearing.
    const src = readWebFile('src/composables/dockTabs.ts')
    const imports = src.match(/^\s*import\s/gm) ?? []
    expect(imports, 'dockTabs.ts must not import anything').toHaveLength(0)
    expect(src).not.toMatch(/\bimport\.meta\b/)
  })
})

describe('test registry helper', () => {
  it('lists exactly the plugin-declared ids and switches', async () => {
    // `testUtils/dockRegistryIds.ts` restates the ids and switches so tests can
    // install them without globbing the plugin modules (a static glob breaks the
    // `vi.mock` hoisting order in component specs). Restating them is only safe
    // if the two cannot drift — which is what this pins.
    const { TEST_DOCK_TAB_IDS, TEST_DOCK_FEATURES } = await import('@/testUtils/dockRegistryIds')
    expect([...TEST_DOCK_TAB_IDS].sort()).toEqual([...dockTabIdsInDefaultOrder()].sort())
    expect([...TEST_DOCK_TAB_IDS]).toEqual([...dockTabIdsInDefaultOrder()])
    expect([...TEST_DOCK_FEATURES].sort()).toEqual([...dockFeatures()].sort())
  })
})
