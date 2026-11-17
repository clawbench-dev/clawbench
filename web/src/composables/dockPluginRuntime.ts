import type { Component } from 'vue'
import {
  DOCK_TAB_IDS,
  isPermanentDockTab,
  mergeDockOrder,
  type DockTabDescriptor,
  type DockTabId,
  type TabId,
} from './dockTabs'
import type {
  DockBadge,
  DockDeepLink,
  DockPanelHost,
  DockPanelHostExtras,
  DockPlugin,
} from './dockPluginTypes'
import { appLog } from '@/utils/appLog'

const TAG = 'DockPlugins'

/**
 * The ONE place that knows which dock plugins exist.
 *
 * Every `web/src/plugins/<id>/plugin.ts` is pulled in here at build time by
 * `import.meta.glob`, so adding a plugin never touches this file. The glob is
 * eager (equivalent to the static imports it replaced — the same modules end up
 * in the main bundle), but each plugin's PANEL stays a dynamic `import()`, so
 * no panel code is loaded until its tab is opened.
 *
 * Order is NOT taken from the glob: it comes from `DOCK_TAB_ORDER`
 * (`dockTabs.ts`), because default order is a product decision and must not be
 * decided by the filesystem's alphabetical order.
 */
/** A plugin module as the bundler hands it over. */
export type PluginModuleMap = Record<string, { default?: DockPlugin }>

export interface AssembledPlugins {
  /** Plugin by id; a duplicate id keeps the FIRST module and drops the rest. */
  runtime: Partial<Record<TabId, DockPlugin>>
  /** One descriptor per left-dock id, in `DOCK_TAB_ORDER`. */
  tabs: (DockTabDescriptor & { icon: Component })[]
}

/**
 * Turn the globbed modules into the runtime map + the left-dock manifest.
 *
 * Exported (and pure) so the aggregation rules — duplicate ids, a missing
 * `plugin.ts`, `chat` not being a dock tab — can be tested by calling it with
 * a fake module map. The real call site is the only place that touches the
 * filesystem, so those rules would otherwise be unreachable from a test.
 */
export function assemblePluginModules(modules: PluginModuleMap): AssembledPlugins {
  const runtime: Partial<Record<TabId, DockPlugin>> = {}

  for (const [path, mod] of Object.entries(modules)) {
    const plugin = mod.default
    if (!plugin?.id) {
      appLog.w(TAG, `plugin module ${path} has no default export with an id`)
      continue
    }
    if (runtime[plugin.id]) {
      // A duplicate id would silently shadow a plugin; the id is the stable
      // identity (config key, i18n prefix, deep-link target), so it must be
      // unique. First wins so the outcome does not depend on glob order.
      appLog.w(TAG, `duplicate plugin id "${plugin.id}" in ${path} — ignored`)
      continue
    }
    runtime[plugin.id] = plugin
  }

  const tabs: (DockTabDescriptor & { icon: Component })[] = []
  for (const id of DOCK_TAB_IDS) {
    const plugin = runtime[id]
    // `chat` is not a left-dock entry; a missing module means the id was added
    // to DOCK_TAB_ORDER without its plugin.ts. Both are dropped here (and
    // caught by the manifest guard test) rather than rendering a nameless
    // button.
    if (!plugin || plugin.id === 'chat') {
      if (!plugin) appLog.w(TAG, `no plugin.ts for dock tab "${id}"`)
      continue
    }
    tabs.push({
      id,
      titleKey: plugin.titleKey,
      descriptionKey: plugin.descriptionKey,
      group: plugin.group,
      closable: plugin.closable,
      feature: plugin.feature,
      capability: plugin.capability,
      settingsCategory: plugin.settingsCategory,
      icon: plugin.icon,
    })
  }

  return { runtime, tabs }
}

const { runtime: RUNTIME, tabs: assembled } = assemblePluginModules(
  import.meta.glob<{ default: DockPlugin }>('@/plugins/*/plugin.ts', { eager: true }),
)

/** Every left-dock tab, in default order, with its manifest resolved. */
export const DOCK_TABS: readonly DockTabDescriptor[] = assembled

/** Manifest + static icon, in default order. */
export const DOCK_TABS_WITH_ICONS: readonly (DockTabDescriptor & { icon: Component })[] = assembled

/** Descriptor lookup by id. */
export const DOCK_TAB_BY_ID: ReadonlyMap<DockTabId, DockTabDescriptor> = new Map(
  assembled.map((t) => [t.id, t]),
)

/** Tabs whose switch writes a server master switch. */
export const DOCK_FEATURE_TABS: readonly DockTabId[] = assembled
  .filter((t) => t.feature)
  .map((t) => t.id)

/** A settings page contributed by a plugin, resolved and in DOCK order. */
export interface DockSettingsPageEntry {
  pluginId: DockTabId
  /** Settings category this page renders (the plugin's `settingsCategory`). */
  categoryId: string
  /** Index group the row hangs under (`dockModules` today). */
  group: string
  icon: Component
  kind: 'flat' | 'group' | 'custom'
  /** Only for `kind: 'custom'`: the page component's lazy loader. */
  component?: () => Promise<unknown>
}

/**
 * Every settings page the plugins contribute, in dock order.
 *
 * This is what lets the settings index and the category dispatcher stop naming
 * plugins: adding a page means declaring `settingsPage` in `plugin.ts`, not
 * editing `SettingsIndex.vue` / `SettingsCategory.vue`.
 */
export const DOCK_SETTINGS_PAGES: readonly DockSettingsPageEntry[] = assembled.flatMap((tab) => {
  const plugin = RUNTIME[tab.id]
  if (!plugin || plugin.id === 'chat' || !plugin.settingsPage) return []
  const page = plugin.settingsPage
  const categoryId = page.categoryId ?? plugin.settingsCategory ?? plugin.id
  return [{ pluginId: tab.id, categoryId, group: page.group, icon: page.icon, kind: page.kind, component: page.component }]
})

/** Every registered plugin id, `chat` included. */
export const DOCK_PLUGIN_IDS: readonly TabId[] = Object.keys(RUNTIME) as TabId[]

/** A deep link together with the plugin that declared it (for diagnostics). */
export interface RegisteredDockDeepLink {
  pluginId: TabId
  link: DockDeepLink
}

/**
 * Every deep link declared by a plugin, in plugin order.
 *
 * The host installs exactly these; nothing else knows the event names.
 */
export function dockDeepLinks(): RegisteredDockDeepLink[] {
  const out: RegisteredDockDeepLink[] = []
  for (const id of DOCK_PLUGIN_IDS) {
    for (const link of RUNTIME[id]?.deeplinks ?? []) out.push({ pluginId: id, link })
  }
  return out
}

/** The plugin that declares `id`, if any. `chat` included. */
export function resolveDockPlugin(id: string): DockPlugin | undefined {
  return RUNTIME[id as TabId]
}

/** Manifest + icon lookup; undefined for unknown ids. */
export function dockTabWithIcon(
  id: string,
): (DockTabDescriptor & { icon: Component }) | undefined {
  return assembled.find((t) => t.id === id)
}

export function resolveDockBadge(id: string): DockBadge | null {
  return resolveDockPlugin(id)?.badge?.() ?? null
}

export function resolveDockTitle(id: string): string | undefined {
  return resolveDockPlugin(id)?.title?.()
}

/** Dynamic icon override (Forge's platform glyph); static icons live on the manifest. */
export function resolveDockIcon(id: string): Component | undefined {
  return resolveDockPlugin(id)?.dynamicIcon?.()
}

/** Static manifest icon. `chat` included — its narrow-dock button uses it. */
export function dockTabIcon(id: string): Component | undefined {
  return resolveDockPlugin(id)?.icon
}

/** Manifest title key, or the plugin's state-dependent key (Forge). */
export function dockTabTitleKey(id: string): string | undefined {
  const plugin = resolveDockPlugin(id)
  return plugin?.dynamicTitleKey?.() ?? plugin?.titleKey
}

/**
 * Settings category id → the dock plugin that owns that page.
 *
 * The manifests already declare the pairing (`settingsCategory`), so this map
 * is derived rather than hand-written: a plugin that adds or renames its
 * settings page cannot leave the settings index behind.
 */
const SETTINGS_CATEGORY_OWNER: ReadonlyMap<string, DockTabId> = new Map(
  assembled
    .filter((tab) => tab.settingsCategory)
    .map((tab) => [tab.settingsCategory as string, tab.id]),
)

/**
 * The ONE i18n key for a feature's name, given its settings category.
 *
 * Settings pages used to carry their own `settings.categories.*` wording, so
 * the same feature read differently in the manager ("项目历史"), the settings
 * index ("Git 历史") and the panel header ("提交列表"). Returning the dock
 * plugin's key makes every surface say exactly the same thing; `undefined`
 * means "not a dock feature", and callers fall back to their own key.
 */
export function dockCategoryTitleKey(categoryId: string): string | undefined {
  const id = SETTINGS_CATEGORY_OWNER.get(categoryId)
  return id ? dockTabTitleKey(id) : undefined
}

/** Button appearance states, declared by the plugin (never by the host). */
export function resolveDockAppearance(id: string) {
  return resolveDockPlugin(id)?.appearance
}

/** True when the plugin is usable now; plugins without a check are usable. */
export function isDockPluginAvailable(id: string): boolean {
  return resolveDockPlugin(id)?.available?.() ?? true
}

/** Whether the host should leave the tab when it becomes unavailable. */
export function shouldLeaveUnavailableDockTab(id: string): boolean {
  return resolveDockPlugin(id)?.leaveWhenUnavailable?.() ?? true
}

export function dockUnavailableReasonKey(id: string): string | undefined {
  return resolveDockPlugin(id)?.unavailableReasonKey
}

export function runDockOnEnter(id: string): void {
  resolveDockPlugin(id)?.onEnter?.()
}

export function runDockOnLeave(id: string): void {
  resolveDockPlugin(id)?.onLeave?.()
}

/**
 * Extra work a dock click performs before the host switches tabs.
 *
 * The host owns navigation; the plugin only contributes what "clicking my entry"
 * means beyond that (terminal clears the pending directory request).
 */
export function runDockOnClick(id: string, host: DockPanelHost, extras: DockPanelHostExtras): void {
  resolveDockPlugin(id)?.onClick?.(host, extras)
}

/** Start a plugin's background resource; returns its cleanup, or null. */
export function startDockBackground(id: string): (() => void) | null {
  const start = resolveDockPlugin(id)?.startBackground
  return start ? start() : null
}

export interface DockVisibilityOptions {
  /** Tabs whose server master switch is OFF. */
  featureDisabled?: ReadonlySet<DockTabId>
  /** Tabs the user hid in the manager. */
  hidden?: ReadonlySet<DockTabId>
  /**
   * Tabs whose runtime availability check currently fails (terminal runtime
   * down, no wire for port forwarding, …). One set for every host limitation —
   * App.vue derives it from each plugin's `available()`, so a capability never
   * has a second, differently-named gate here.
   */
  unavailable?: ReadonlySet<DockTabId>
  /** User order (any subset of ids; see mergeDockOrder). */
  order?: readonly DockTabId[]
}

const EMPTY_SET: ReadonlySet<DockTabId> = new Set()

function isGated(id: DockTabId, opts: DockVisibilityOptions): boolean {
  return !!opts.unavailable?.has(id) || !!opts.featureDisabled?.has(id)
}

/**
 * Only closable tabs can be hidden. A corrupt/stale `dockHidden` value must not
 * hide a permanent tab (settings in particular would lock the user out of the
 * manager that could fix it).
 */
function isUserHidden(id: DockTabId, hidden: ReadonlySet<DockTabId>): boolean {
  return !isPermanentDockTab(id) && hidden.has(id)
}

/**
 * The left dock's visible tabs, in the user's order, with every gate applied.
 *
 * This is the ONE render list: the wide-screen vertical dock, the narrow
 * bottom dock's inline/overflow split and the settings manager all call it, so
 * a tab can never be rendered in one place and missing from another (the
 * original forge bug was exactly that divergence).
 *
 * Gates:
 *  - `unavailable` — the host cannot run the feature right now (terminal
 *    runtime down, no wire for port forwarding, …). `live` (会议) is
 *    deliberately NOT gated on capture: a host without a screen can still read
 *    its records, and a voice meeting only needs a microphone.
 *  - `featureDisabled` — the server master switch is off. The tab is the
 *    visible half of a real disable, so it disappears with the background work.
 *  - `hidden` — the user turned the entry off in the manager (closable tabs
 *    only). For feature tabs this is the same switch: the manager shows one
 *    switch per tab, not two.
 */
export function dockTabs(opts: DockVisibilityOptions = {}): DockTabId[] {
  const hidden = opts.hidden ?? EMPTY_SET
  return mergeDockOrder(opts.order).filter((id) => !isGated(id, opts) && !isUserHidden(id, hidden))
}
