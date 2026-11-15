/**
 * Dock tab identities and default order — the import-free half of the dock
 * registry.
 *
 * This module deliberately imports NOTHING (no Vue, no lucide, and — critically
 * — no plugin module) so that it stays safe to pull into composables that tests
 * import with narrow module mocks. In particular `useWideScreenLayout` depends
 * on this file, and that composable is reached from many components
 * (BottomSheet, AppHeader, FileViewer, …) whose tests mock `lucide-vue-next`
 * with only a handful of icons. Importing icon components here would make those
 * tests throw at module load with
 * `No "FolderOpen" export is defined on the "lucide-vue-next" mock`.
 *
 * Everything else about a tab (title key, description key, group, closable,
 * feature, capability, settings category, icon, runtime resources) is declared
 * by the plugin itself in `web/src/plugins/<id>/plugin.ts` and assembled by
 * `composables/dockPluginRuntime.ts`. Adding a plugin therefore means adding
 * that file plus ONE line in `DOCK_TAB_ORDER` below — never editing this file's
 * data.
 *
 * The order stays hand-written on purpose: it is a product decision (the file
 * manager is first, settings is the exit affordance at the end). Deriving it
 * from the glob would let the filesystem's alphabetical order decide where a
 * new tab lands, and would drag the plugin modules into this file.
 */

/**
 * Every left-column tab id, in DEFAULT order.
 *
 * Order is the default order and the "developer" preset's order. The user can
 * drag every entry (including settings) anywhere afterwards.
 *
 * Runtime-gated tabs (terminal, proxy) are declared here too; the gates are
 * applied when the render list is built.
 */
export const DOCK_TAB_ORDER = [
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
  // Notifications sits immediately before settings in the DEFAULT order:
  // settings is the "exit" affordance at the end of the dock, and the
  // notification log is the last stop before leaving the tool tabs.
  'notifications',
  'settings',
] as const

/** Left-column tab ids (everything except chat). */
export type DockTabId = (typeof DOCK_TAB_ORDER)[number]

/**
 * Every tab id the app can navigate to.
 *
 * `chat` is the right-hand pane on wide screens and a normal tab on narrow
 * ones; the rest are left-column tabs. It is not part of `DOCK_TAB_ORDER`
 * because it is not a left-dock entry, but it shares the plugin runtime
 * contract (badge, onEnter) — see `plugins/chat/plugin.ts`.
 */
export type TabId = 'chat' | DockTabId

/** Where a tab sits in the settings manager's grouping (presentational only). */
export type DockGroup = 'workspace' | 'project' | 'personal' | 'system'

/** A condition the host/environment must satisfy before the tab is usable. */
export type DockCapability = 'terminal' | 'portForward'

/**
 * Every server master switch a dock plugin may gate on.
 *
 * Declared here (not only in the manifests) so the preset rules in
 * `useDockPlugins` — a leaf module — can say "switch everything off" without
 * importing the plugin graph. `dockPluginManifest.test.ts` asserts this list
 * equals the set the manifests actually declare.
 *
 * The TYPE is derived from this array, so adding a switch is one line here —
 * not the two hand-maintained lists that used to drift apart.
 */
export const DOCK_FEATURES = [
  'forge.enabled',
  'mail.enabled',
  'terminal.enabled',
  'kb.enabled',
  'tasks.enabled',
  'share.enabled',
] as const

/**
 * A server-side master switch. Turning one of these off stops the server's
 * background work as well as hiding the tab, so the manager's switch writes
 * the server config rather than a per-device preference.
 */
export type DockFeature = (typeof DOCK_FEATURES)[number]

/**
 * The assembled view of a dock tab: manifest fields declared by the plugin,
 * resolved in `dockPluginRuntime.ts`.
 *
 * Plugins do not write this shape directly — they write `DockPlugin`
 * (`dockPluginTypes.ts`), which adds the runtime half.
 */
export interface DockTabDescriptor {
  id: DockTabId
  /** i18n key for the tab label / tooltip. */
  titleKey: string
  /** i18n key for the one-line description in the manager. */
  descriptionKey: string
  /** Settings-manager grouping; does not change dock order. */
  group: DockGroup
  /**
   * Whether the user may hide this tab. Permanent tabs (browse, view,
   * settings) are always visible; everything else can be hidden — including
   * notifications, whose switch only hides the entry (the log itself keeps
   * being written and the unread/notification mechanism is untouched).
   */
  closable: boolean
  /**
   * Server master switch this plugin depends on. Present ⇒ the manager's
   * switch writes the server config and the tab disappears when it is off.
   * Absent ⇒ the switch only hides the entry locally.
   */
  feature?: DockFeature
  /** Host/environment capability. When it is missing the entry is hidden and
   *  the manager greys the switch out with a reason. */
  capability?: DockCapability
  /** Settings category that owns this plugin's own configuration. */
  settingsCategory?: string
}

/** All left-column tab ids, in default order. Derived from the order above. */
export const DOCK_TAB_IDS: readonly DockTabId[] = DOCK_TAB_ORDER

/**
 * Tabs the user can never hide.
 *
 * Each plugin's manifest still carries `closable` — that is what the manager
 * renders — but the persisted `dockHidden` set is read by leaf composables
 * (`useDockPlugins`, reached from FileHeader) that must NOT import the plugin
 * graph. Keeping the three permanent ids here keeps that path free of lucide and
 * of every plugin module. `dockPluginManifest.test.ts` asserts this list equals
 * the manifest's `closable: false` set, so the two cannot drift.
 */
export const DOCK_PERMANENT_TAB_IDS: readonly DockTabId[] = ['browse', 'view', 'settings']

const DOCK_PERMANENT_TAB_SET: ReadonlySet<string> = new Set(DOCK_PERMANENT_TAB_IDS)

/** True when the tab cannot be hidden by the user. */
export function isPermanentDockTab(id: string): boolean {
  return DOCK_PERMANENT_TAB_SET.has(id)
}

const DOCK_TAB_ID_SET: ReadonlySet<string> = new Set(DOCK_TAB_ORDER)

/** True when `tab` is a known left-column tab id. */
export function isDockTabId(tab: string): tab is DockTabId {
  return DOCK_TAB_ID_SET.has(tab)
}

/**
 * Merge a stored order with the registry's default order.
 *
 * The rule is "default positions, stored relative order": every id the stored
 * array mentions keeps the SLOT it occupies in the default order, and those
 * slots are filled in the stored sequence. Ids the stored array does not
 * mention stay where the default puts them.
 *
 * This is what makes the upgrade from the old format safe. Before every tab
 * became draggable, `dockOrder` only ever contained the movable tail (forge …
 * proxy). Appending those to the end — or starting the list from them — would
 * push the permanent tabs (file manager first of all) out of place. With slot
 * preservation the old array only reorders the movable slots, exactly as it
 * did before, and every permanent tab keeps its default position.
 *
 * A complete array (what the manager writes after a drag) is reproduced
 * verbatim: all ids are mentioned, so all slots are refilled in its order.
 * Unknown and duplicate ids are dropped.
 */
export function mergeDockOrder(order: readonly DockTabId[] = []): DockTabId[] {
  const stored: DockTabId[] = []
  const seen = new Set<DockTabId>()
  for (const id of order) {
    if (!isDockTabId(id) || seen.has(id)) continue
    seen.add(id)
    stored.push(id)
  }
  if (stored.length === 0) return [...DOCK_TAB_IDS]
  let next = 0
  return DOCK_TAB_IDS.map((id) => (seen.has(id) ? stored[next++] : id))
}
