/**
 * Dock tab ids and layout facts — the icon-free half of the dock tab registry.
 *
 * This module deliberately imports NOTHING (no Vue, no lucide) so that it stays
 * safe to pull into composables that tests import with narrow module mocks. In
 * particular `useWideScreenLayout` depends on this file, and that composable is
 * reached from many components (BottomSheet, AppHeader, FileViewer, …) whose
 * tests mock `lucide-vue-next` with only a handful of icons. Importing icon
 * components here would make those tests throw at module load with
 * `No "FolderOpen" export is defined on the "lucide-vue-next" mock`.
 *
 * The icon half lives in `dockTabMeta.ts`, which only the dock-rendering code
 * imports.
 */

/**
 * Every tab id the app can navigate to.
 *
 * `chat` is the right-hand pane on wide screens and a normal tab on narrow
 * ones; the rest are left-column tabs.
 */
export type TabId =
  | 'chat'
  | 'browse'
  | 'view'
  | 'history'
  | 'forge'
  | 'tasks'
  | 'terminal'
  | 'proxy'
  | 'stats'
  | 'settings'

/** Left-column tab ids (everything except chat). */
export type DockTabId = Exclude<TabId, 'chat'>

export interface DockTabDescriptor {
  id: DockTabId
  /** i18n key for the tab label / tooltip. */
  titleKey: string
  /**
   * Always rendered in the wide-screen dock's fixed head, in this order.
   * Non-primary tabs follow as the dock's secondary (overflow) tabs.
   */
  primary?: boolean
}

/**
 * The single source of truth for dock tabs: order, ids, i18n keys and the
 * primary/secondary split. Everything the dock renders and switches derives
 * from this array.
 *
 * This exists because the dock previously kept the *same fact* in two places
 * that had to be hand-synced: what the dock renders (`overflowTabs` +
 * `wideScreenTabMeta` in App.vue) and what switching accepts
 * (`WIDE_SCREEN_DOCK_TABS` in useWideScreenLayout.ts). Nothing derived one from
 * the other, so adding a tab and forgetting the whitelist produced a button
 * that rendered but silently did nothing on click — exactly what happened to
 * the forge tab.
 *
 * Now the render list, the switch whitelist and the dock order all derive from
 * this array, so that class of divergence cannot recur: adding a tab here makes
 * it renderable *and* switchable at once. (Its icon is added in dockTabMeta.ts.)
 *
 * Order is the dock's display order. Runtime-gated tabs (terminal, proxy) are
 * still declared here; App.vue filters them out when the feature is disabled.
 */
export const DOCK_TABS: readonly DockTabDescriptor[] = [
  { id: 'browse', titleKey: 'nav.fileManager', primary: true },
  { id: 'view', titleKey: 'nav.fileView', primary: true },
  { id: 'history', titleKey: 'git.history.projectHistory', primary: true },
  { id: 'forge', titleKey: 'nav.forge' },
  { id: 'tasks', titleKey: 'nav.tasks' },
  { id: 'terminal', titleKey: 'terminal.title' },
  { id: 'proxy', titleKey: 'nav.portForward' },
  { id: 'stats', titleKey: 'nav.stats' },
  { id: 'settings', titleKey: 'nav.settings' },
]

/** All left-column tab ids, in dock order. Derived — never hand-maintained. */
export const DOCK_TAB_IDS: readonly DockTabId[] = DOCK_TABS.map((t) => t.id)

/** Tabs always rendered in the wide-screen dock's fixed head, in order. */
export const WIDE_SCREEN_PRIMARY_TABS: readonly DockTabId[] = DOCK_TABS.filter(
  (t) => t.primary,
).map((t) => t.id)

/** Descriptor lookup by id. */
export const DOCK_TAB_BY_ID: ReadonlyMap<DockTabId, DockTabDescriptor> = new Map(
  DOCK_TABS.map((t) => [t.id, t]),
)

/** True when `tab` is a known left-column tab id. */
export function isDockTabId(tab: string): tab is DockTabId {
  return DOCK_TAB_BY_ID.has(tab as DockTabId)
}
