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
 * The icon/title half lives in `dockTabMeta.ts`, which only the dock-rendering
 * code imports.
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
  | 'notifications'
  | 'terminal'
  | 'live'
  | 'share'
  | 'mail'
  | 'kb'
  | 'proxy'
  | 'stats'
  | 'settings'

/** Left-column tab ids (everything except chat). */
export type DockTabId = Exclude<TabId, 'chat'>

/** Where a tab sits in the settings manager's grouping (presentational only). */
export type DockGroup = 'workspace' | 'project' | 'personal' | 'system'

/** A condition the host/environment must satisfy before the tab is usable. */
export type DockCapability = 'terminal' | 'portForward'

/**
 * A server-side master switch. Turning one of these off stops the server's
 * background work as well as hiding the tab, so the manager's switch writes
 * the server config rather than a per-device preference.
 */
export type DockFeature =
  | 'forge.enabled'
  | 'mail.enabled'
  | 'terminal.enabled'
  | 'kb.enabled'
  | 'tasks.enabled'
  | 'share.enabled'

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
   * notifications, settings) are always visible; everything else can be hidden.
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
 * The single source of truth for dock tabs: ids, i18n keys, default order and
 * which switch (if any) governs each tab.
 *
 * Everything the dock renders and switches derives from this array, so adding
 * a tab here makes it renderable *and* switchable at once. (Its icon is added
 * in dockTabMeta.ts.)
 *
 * Order is the DEFAULT order. It is also the "developer" preset's order — the
 * user can drag every entry (including settings) into any order afterwards.
 *
 * Runtime-gated tabs (terminal, proxy) are still declared here; `dockTabs()`
 * applies the gate when building the render list.
 */
export const DOCK_TABS: readonly DockTabDescriptor[] = [
  {
    id: 'browse',
    titleKey: 'nav.fileManager',
    descriptionKey: 'settings.dockPlugins.desc.browse',
    group: 'workspace',
    closable: false,
    settingsCategory: 'files',
  },
  {
    id: 'view',
    titleKey: 'nav.fileView',
    descriptionKey: 'settings.dockPlugins.desc.view',
    group: 'workspace',
    closable: false,
    settingsCategory: 'fileView',
  },
  {
    id: 'history',
    titleKey: 'git.history.projectHistory',
    descriptionKey: 'settings.dockPlugins.desc.history',
    group: 'workspace',
    closable: true,
    settingsCategory: 'gitHistory',
  },
  // Forge's registry title is the platform-NEUTRAL label ("issues & change
  // requests"). The dock does not use it: `dockTabTitle` in App.vue resolves
  // `forgeTabTitleKey(platform)` instead, so the tab names the bound service
  // ("GitHub integration" / "GitLab integration"). It stays here because the
  // registry requires a title for every tab (and the guard test checks each one
  // resolves in both locales), and because the forge panel header uses it as
  // its own fallback when no repository is bound.
  {
    id: 'forge',
    titleKey: 'nav.forge',
    descriptionKey: 'settings.dockPlugins.desc.forge',
    group: 'project',
    closable: true,
    feature: 'forge.enabled',
    settingsCategory: 'forgeIntegration',
  },
  {
    id: 'tasks',
    titleKey: 'nav.tasks',
    descriptionKey: 'settings.dockPlugins.desc.tasks',
    group: 'project',
    closable: true,
    feature: 'tasks.enabled',
    settingsCategory: 'tasks',
  },
  {
    id: 'terminal',
    titleKey: 'terminal.title',
    descriptionKey: 'settings.dockPlugins.desc.terminal',
    group: 'project',
    closable: true,
    feature: 'terminal.enabled',
    capability: 'terminal',
    settingsCategory: 'terminal',
  },
  {
    id: 'stats',
    titleKey: 'nav.stats',
    descriptionKey: 'settings.dockPlugins.desc.stats',
    group: 'project',
    closable: true,
    settingsCategory: 'stats',
  },
  // 邮件: a full mail client (accounts, folders, reading, composing). Data is
  // global rather than per-project — a mailbox belongs to the person, not to a
  // repository — so the tab looks the same in every project.
  {
    id: 'mail',
    titleKey: 'nav.mail',
    descriptionKey: 'settings.dockPlugins.desc.mail',
    group: 'personal',
    closable: true,
    feature: 'mail.enabled',
    settingsCategory: 'mail',
  },
  // 知识库: multiple collections (global or per project), each a set of files
  // indexed for keyword + semantic search. It sits next to 邮件 because both
  // are reference material rather than project state.
  {
    id: 'kb',
    titleKey: 'nav.knowledgeBase',
    descriptionKey: 'settings.dockPlugins.desc.kb',
    group: 'personal',
    closable: true,
    feature: 'kb.enabled',
    settingsCategory: 'kb',
  },
  // 视频会议: the meeting records, and the meeting itself — screen/camera or
  // voice. It used to be two tabs (听记 and 桌面共享) even though both drove the
  // same recorder, the same endpoints and the same artifact folder; a record
  // can be either kind, so one tab is the honest shape.
  {
    id: 'live',
    titleKey: 'nav.meeting',
    descriptionKey: 'settings.dockPlugins.desc.live',
    group: 'personal',
    closable: true,
    settingsCategory: 'meeting',
  },
  // 分享: every public link this project has handed out (files, conversations,
  // meetings), in one place. It sits next to 会议 because both are "things this
  // machine offers to other people", and it stays before 端口映射 for the same
  // reason — the outgoing-access pair stays together.
  {
    id: 'share',
    titleKey: 'nav.share',
    descriptionKey: 'settings.dockPlugins.desc.share',
    group: 'project',
    closable: true,
    feature: 'share.enabled',
    settingsCategory: 'share',
  },
  {
    id: 'proxy',
    titleKey: 'nav.portForward',
    descriptionKey: 'settings.dockPlugins.desc.proxy',
    group: 'project',
    closable: true,
    capability: 'portForward',
    settingsCategory: 'portForward',
  },
  // Notifications sits immediately before settings in the DEFAULT order:
  // settings is the "exit" affordance at the end of the dock, and the
  // notification log is the last stop before leaving the tool tabs. Both can
  // be dragged elsewhere now.
  {
    id: 'notifications',
    titleKey: 'nav.notifications',
    descriptionKey: 'settings.dockPlugins.desc.notifications',
    group: 'system',
    closable: false,
    settingsCategory: 'notifications',
  },
  {
    id: 'settings',
    titleKey: 'nav.settings',
    descriptionKey: 'settings.dockPlugins.desc.settings',
    group: 'system',
    closable: false,
  },
]

/** All left-column tab ids, in default order. Derived — never hand-maintained. */
export const DOCK_TAB_IDS: readonly DockTabId[] = DOCK_TABS.map((t) => t.id)

/** Descriptor lookup by id. */
export const DOCK_TAB_BY_ID: ReadonlyMap<DockTabId, DockTabDescriptor> = new Map(
  DOCK_TABS.map((t) => [t.id, t]),
)

/** Tabs whose switch writes a server master switch. */
export const DOCK_FEATURE_TABS: readonly DockTabId[] = DOCK_TABS.filter((t) => t.feature).map(
  (t) => t.id,
)

/** True when `tab` is a known left-column tab id. */
export function isDockTabId(tab: string): tab is DockTabId {
  return DOCK_TAB_BY_ID.has(tab as DockTabId)
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
  return !!DOCK_TAB_BY_ID.get(id)?.closable && hidden.has(id)
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
