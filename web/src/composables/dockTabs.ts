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

/** A server-side feature switch that also controls background work. */
export type DockFeature = 'forge.enabled' | 'mail.enabled'

export interface DockTabDescriptor {
  id: DockTabId
  /** i18n key for the tab label / tooltip. */
  titleKey: string
  /** i18n key for the one-line description in the plugin manager. */
  descriptionKey: string
  /** Settings-manager grouping; does not change dock order. */
  group: DockGroup
  /**
   * Always rendered in the wide-screen dock's fixed head, in this order.
   * Non-primary tabs follow as the dock's secondary (overflow) tabs.
   *
   * `primary` is a POSITION, not a lock: a primary tab may still be closable
   * (history is). The wide dock must filter primary tabs by visibility before
   * rendering them.
   */
  primary?: boolean
  /**
   * Whether the user may hide this tab. Core tabs (chat, browse, view,
   * settings) are always visible; everything else can be hidden.
   */
  closable: boolean
  /**
   * Development-related tab. The "standard" preset hides these; the manager
   * shows a "dev" chip. Data-driven so the preset does not hard-code ids.
   */
  devRelated?: boolean
  /** Server feature switch this plugin depends on. Metadata for the manager;
   *  the runtime gate is still wired in App.vue until the availability layer
   *  lands (see docs/plugin-dev/04-runtime.md). */
  feature?: DockFeature
  /** Host/environment capability. Metadata for the manager; same caveat as
   *  `feature` — the actual gate lives in App.vue today. */
  capability?: DockCapability
  /** Settings category that owns this plugin's own configuration. */
  settingsCategory?: string
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
  {
    id: 'browse',
    titleKey: 'nav.fileManager',
    descriptionKey: 'settings.dockPlugins.desc.browse',
    group: 'workspace',
    primary: true,
    closable: false,
    settingsCategory: 'files',
  },
  {
    id: 'view',
    titleKey: 'nav.fileView',
    descriptionKey: 'settings.dockPlugins.desc.view',
    group: 'workspace',
    primary: true,
    closable: false,
    settingsCategory: 'files',
  },
  {
    id: 'history',
    titleKey: 'git.history.projectHistory',
    descriptionKey: 'settings.dockPlugins.desc.history',
    group: 'workspace',
    primary: true,
    closable: true,
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
    devRelated: true,
    feature: 'forge.enabled',
    settingsCategory: 'forgeIntegration',
  },
  {
    id: 'tasks',
    titleKey: 'nav.tasks',
    descriptionKey: 'settings.dockPlugins.desc.tasks',
    group: 'project',
    closable: true,
  },
  {
    id: 'terminal',
    titleKey: 'terminal.title',
    descriptionKey: 'settings.dockPlugins.desc.terminal',
    group: 'project',
    closable: true,
    devRelated: true,
    capability: 'terminal',
    settingsCategory: 'terminal',
  },
  {
    id: 'stats',
    titleKey: 'nav.stats',
    descriptionKey: 'settings.dockPlugins.desc.stats',
    group: 'project',
    closable: true,
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
    settingsCategory: 'rag',
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
  },
  {
    id: 'proxy',
    titleKey: 'nav.portForward',
    descriptionKey: 'settings.dockPlugins.desc.proxy',
    group: 'project',
    closable: true,
    devRelated: true,
    capability: 'portForward',
    settingsCategory: 'portForward',
  },
  // Notifications sits immediately before settings: settings stays the final
  // tab (it is the "exit" affordance at the end of the dock), and the
  // notification log is the last stop before leaving the tool tabs.
  {
    id: 'notifications',
    titleKey: 'nav.notifications',
    descriptionKey: 'settings.dockPlugins.desc.notifications',
    group: 'system',
    closable: true,
    settingsCategory: 'notification',
  },
  {
    id: 'settings',
    titleKey: 'nav.settings',
    descriptionKey: 'settings.dockPlugins.desc.settings',
    group: 'system',
    closable: false,
  },
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

/**
 * The secondary (non-primary) dock tabs that should be rendered, in registry
 * order, with the runtime-gated ones filtered out.
 *
 * This is the *render* list the narrow dock's overflow area and the wide-screen
 * dock's tail both use. It lives here (not inline in App.vue) so a test can
 * assert it is a subset of the switch whitelist: the original forge bug was
 * precisely the render list diverging from what `switchLeftTab` accepts, and an
 * inline computed in a .vue file is not reachable from a unit test.
 *
 * `terminal`/`proxy` are hidden when their feature is unavailable at runtime
 * (terminal: the PTY manager is not up yet or the platform cannot run it; proxy:
 * port forwarding is genuinely unusable — no wire can carry it). Both are still
 * declared in DOCK_TABS — this function is where the runtime gate is applied.
 *
 * `live` (会议) is deliberately NOT gated: a host that cannot capture a screen
 * can still read its meeting records, and a voice meeting only needs a
 * microphone. The create page explains what is missing instead.
 *
 * `forgeHidden` is a USER-facing switch, not a runtime capability: issue #590 asks
 * for a way to turn the integration off when it is not in use. On the server
 * that switch stops polling and clears every project's repository binding, so by
 * the time this flag is true there is nothing left for the panel to show — the
 * dock entry is simply the visible half of a real disable.
 *
 * `mailHidden` is the same kind of switch for the mail client: off stops the
 * server's background IMAP sync (and the email-triggered tasks riding on it) and
 * hides the dock entry. Unlike forge it deletes nothing — the accounts stay, so
 * the switch is trivially reversible.
 *
 * The proxy flag is `portForwardUnavailable`, not `sshDisabled`: an install
 * forwarding over the stream tunnel has no SSH listener yet works fine, so
 * keying the dock off SSH alone hid the whole tab there. Callers compute it with
 * `portForwardUnavailable()` from utils/portForwardUtils — this module stays
 * import-free on purpose (see the header), so it takes the resulting boolean.
 */
export interface DockVisibilityOptions {
  terminalDisabled?: boolean
  portForwardUnavailable?: boolean
  forgeHidden?: boolean
  mailHidden?: boolean
  /** Tabs the user hid in the plugin manager. */
  hidden?: ReadonlySet<DockTabId>
  /** User order for the reorderable secondary tabs; missing ids keep default order. */
  order?: readonly DockTabId[]
}

const EMPTY_HIDDEN: ReadonlySet<DockTabId> = new Set()

/**
 * Non-primary, closable tabs the user can reorder. `settings` is excluded on
 * purpose: it is the exit affordance and must stay last.
 */
export const REORDERABLE_DOCK_TABS: readonly DockTabId[] = DOCK_TABS
  .filter((t) => !t.primary && t.closable && t.id !== 'settings')
  .map((t) => t.id)

function isGated(id: DockTabId, opts: DockVisibilityOptions): boolean {
  return (
    (id === 'terminal' && !!opts.terminalDisabled) ||
    (id === 'proxy' && !!opts.portForwardUnavailable) ||
    (id === 'forge' && !!opts.forgeHidden) ||
    (id === 'mail' && !!opts.mailHidden)
  )
}

/**
 * Only closable tabs can be hidden. A corrupt/stale `dockHidden` value must not
 * hide a core tab (settings in particular would lock the user out of the
 * manager that could fix it).
 */
function isUserHidden(id: DockTabId, hidden: ReadonlySet<DockTabId>): boolean {
  return !!DOCK_TAB_BY_ID.get(id)?.closable && hidden.has(id)
}

/**
 * Merge a stored order with the registry order. Unknown/duplicate ids are
 * dropped; ids added in a later release are appended in their default order so
 * they never disappear just because the stored array predates them.
 */
export function mergeDockOrder(order: readonly DockTabId[] = []): DockTabId[] {
  const seen = new Set<DockTabId>()
  const out: DockTabId[] = []
  for (const id of order) {
    if (!REORDERABLE_DOCK_TABS.includes(id) || seen.has(id)) continue
    seen.add(id)
    out.push(id)
  }
  for (const id of REORDERABLE_DOCK_TABS) {
    if (!seen.has(id)) out.push(id)
  }
  return out
}

/** Visible tabs in the wide dock's fixed head (history may be hidden). */
export function primaryDockTabs(
  opts: Pick<DockVisibilityOptions, 'hidden'> = {},
): DockTabId[] {
  const hidden = opts.hidden ?? EMPTY_HIDDEN
  return WIDE_SCREEN_PRIMARY_TABS.filter((id) => !isUserHidden(id, hidden))
}

/**
 * The secondary (non-primary) dock tabs that should be rendered, in the user's
 * order, with runtime gates and user-hidden tabs filtered out.
 *
 * `settings` is always appended last: it is the only non-primary core tab and
 * the exit affordance. `REORDERABLE_DOCK_TABS` covers everything before it.
 *
 * This is the *render* list the narrow dock's overflow area and the wide-screen
 * dock's tail both use. It lives here (not inline in App.vue) so a test can
 * assert it is a subset of the switch whitelist: the original forge bug was
 * precisely the render list diverging from what `switchLeftTab` accepts, and an
 * inline computed in a .vue file is not reachable from a unit test.
 *
 * `terminal`/`proxy` are hidden when their feature is unavailable at runtime
 * (terminal: the PTY manager is not up yet or the platform cannot run it; proxy:
 * port forwarding is genuinely unusable — no wire can carry it). Both are still
 * declared in DOCK_TABS — this function is where the runtime gate is applied.
 *
 * `live` (会议) is deliberately NOT gated: a host that cannot capture a screen
 * can still read its meeting records, and a voice meeting only needs a
 * microphone. The create page explains what is missing instead.
 *
 * `forgeHidden` / `mailHidden` are server feature switches: off stops the
 * server's work too, so the dock entry is the visible half of a real disable.
 * `hidden` is the per-device preference from the plugin manager — it only
 * removes the entry and never stops background work.
 */
export function secondaryDockTabs(opts: DockVisibilityOptions = {}): DockTabId[] {
  const hidden = opts.hidden ?? EMPTY_HIDDEN
  const order = mergeDockOrder(opts.order)
  const visible = (id: DockTabId) => !isGated(id, opts) && !isUserHidden(id, hidden)

  const reorderable = order.filter(visible)
  const settings = DOCK_TABS.filter((t) => t.id === 'settings' && visible(t.id)).map((t) => t.id)
  // Future non-primary tabs that are neither reorderable nor the fixed tail
  // (none today). Keeping this branch means adding such a tab never silently
  // drops it from the dock.
  const rest = DOCK_TABS.filter(
    (t) => !t.primary && t.id !== 'settings' && !REORDERABLE_DOCK_TABS.includes(t.id),
  )
    .filter((t) => visible(t.id))
    .map((t) => t.id)

  return [...reorderable, ...rest, ...settings]
}
