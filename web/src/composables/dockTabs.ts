/**
 * Dock tab identities — the import-free half of the dock registry.
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
 * EVERYTHING about a tab — id, default order, title key, description key,
 * group, closable, feature, capability, settings category, icon, runtime
 * resources — is declared by the plugin itself in
 * `web/src/plugins/<id>/plugin.ts`. `composables/dockPluginRuntime.ts` globs
 * those modules, sorts them by their declared `order` and publishes the result
 * here through `installDockTabIds`. Adding a plugin therefore means adding its
 * directory and nothing else.
 *
 * The plugin runtime fills in `DOCK_TAB_ID_SET` at module load (it imports this
 * file, not the other way round), so the id helpers below answer for free while
 * this file keeps its "imports nothing" property. Before the runtime has
 * loaded, every `isDockTabId` answer is false — which is exactly what the first
 * paint sees today, and the runtime loads eagerly with the app shell.
 */

/**
 * Left-column tab ids (everything except chat).
 *
 * A plain `string`: the set is derived from the plugin modules, so there is no
 * literal union left to derive it from. The compiler can no longer catch a
 * mistyped id — the plugin runtime logs the installed set once at startup
 * instead, and `dockPluginManifest.test.ts` pins that every id has a plugin.
 */
export type DockTabId = string

/**
 * Every tab id the app can navigate to.
 *
 * `chat` is the right-hand pane on wide screens and a normal tab on narrow
 * ones; the rest are left-column tabs. It is not part of `DOCK_TAB_IDS`
 * because it is not a left-dock entry, but it shares the plugin runtime
 * contract (badge, onEnter) — see `plugins/chat/plugin.ts`.
 */
export type TabId = 'chat' | DockTabId

/** Where a tab sits in the settings manager's grouping (presentational only). */
export type DockGroup = 'workspace' | 'project' | 'personal' | 'system'

/** A condition the host/environment must satisfy before the tab is usable. */
export type DockCapability = 'terminal' | 'portForward'

/**
 * A server-side master switch. Turning one of these off stops the server's
 * background work as well as hiding the tab, so the manager's switch writes
 * the server config rather than a per-device preference.
 *
 * A plain `string`: the SET of switches is derived from the plugins that declare
 * one (see `dockFeatures()` below), so there is no literal array left here to
 * derive it from — and no second list to forget to extend.
 */
export type DockFeature = string

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

/**
 * Plugin ids, in default order, and the master switches they gate on.
 *
 * BOTH are pushed in by `dockPluginRuntime.ts`, never read from the plugin
 * modules here: this file must stay import-free (see the header — a batch of
 * component tests mock `lucide-vue-next` with only the icons they use, and
 * anything pulled in here makes them throw at module load). The runtime imports
 * THIS file, so it does the globbing and pushing; the direction never reverses.
 *
 * Both are also installed explicitly by tests that exercise a leaf module
 * (`useProjectPanel`, `useDockPlugins`, …) without loading the runtime.
 */
let dockTabIds: readonly DockTabId[] = []
let installedFeatures: readonly DockFeature[] = []

/**
 * The default order.
 *
 * Always read through this accessor rather than capturing the array at module
 * scope: the value is only final once the plugin modules have been assembled.
 */
export function dockTabIdsInDefaultOrder(): readonly DockTabId[] {
  return dockTabIds
}

/**
 * The master switches the plugins declare.
 *
 * Read through this accessor by the preset rules in `useDockPlugins` — a leaf
 * module that must not import the plugin graph.
 */
export function dockFeatures(): readonly DockFeature[] {
  return installedFeatures
}

/**
 * Install the plugin-declared default order. Called once by the runtime (and by
 * tests that need the real ids without a full plugin load).
 *
 * Not exported as a mutable binding on purpose: this installer is the only
 * writer, so "who changed the default order" has exactly one answer.
 */
export function installDockTabIds(ids: readonly DockTabId[]): void {
  dockTabIds = Object.freeze([...ids]) as readonly DockTabId[]
  DOCK_TAB_ID_SET = new Set(ids)
}

/** Install the plugin-declared master switches. Called once by the runtime. */
export function installDockFeatures(features: readonly DockFeature[]): void {
  installedFeatures = Object.freeze([...features]) as readonly DockFeature[]
}


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

let DOCK_TAB_ID_SET: ReadonlySet<string> = new Set()

/**
 * True when `tab` is a known left-column tab id.
 *
 * Answers from the installed set, so it is `false` for everything until
 * `installDockTabIds` has run. The runtime loads eagerly with the app shell, and
 * tests that need real answers install the ids themselves.
 */
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
  const defaults = dockTabIdsInDefaultOrder()
  if (stored.length === 0) return [...defaults]
  let next = 0
  return defaults.map((id) => (seen.has(id) ? stored[next++] : id))
}
