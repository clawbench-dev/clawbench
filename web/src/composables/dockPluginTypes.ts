import type { Component } from 'vue'
import type { DockCapability, DockFeature, DockGroup, DockTabId, TabId } from './dockTabs'

/**
 * The self-description of one dock plugin.
 *
 * This file is intentionally type-only so a `plugins/<id>/plugin.ts` module can
 * import it without creating a runtime cycle with the aggregator
 * (`dockPluginRuntime.ts`), which imports those modules.
 */
export interface DockBadge {
  /** Numeric badge. Use 0 with `live: true` for a dot-only indicator. */
  count: number
  /** Ongoing-activity dot (meetings), not a count. */
  live?: boolean
}

/**
 * Host callbacks ANY panel may need. Passed from App.vue to DockPanelHost, then
 * forwarded to each plugin's `panel.props` / `panel.on`.
 *
 * This is the GENERIC layer: adding a plugin must never widen it. A panel that
 * needs something plugin-specific takes it from `DockPanelHostExtras` (its own
 * key below) — that keeps one shell-wide contract and one per-plugin contract
 * apart, instead of one interface where `openMailSettings` sits next to
 * `projectPath`.
 */
export interface DockPanelHost {
  /** Active project root, for panels scoped per project. */
  projectPath: string
  /** Open a file in the viewer. */
  openFile(path: string): void
  /** Open any settings category (replaces per-plugin openXxxSettings). */
  openSettingsCategory(id: string): void
  /** Switch to a tab (replaces requestProject). */
  openTab(id: string): void
  /** Whether the left column is the wide-screen variant. */
  isWideScreen: boolean
}

/**
 * Plugin-specific host capabilities, keyed by plugin.
 *
 * A panel reaches its own group through `panel.props(host, extras)`; another
 * plugin's group is none of its business. A new capability gets a new key here —
 * never a member on the generic interface.
 */
export interface DockPanelHostExtras {
  terminal: {
    requestedCwd: string | null
    clearCwd(): void
    platformUnsupported: boolean
  }
  task: {
    openTaskFile(path: string, lineStart?: number): void
  }
  meeting: {
    quote(token: string): void
  }
}

/**
 * How a plugin supplies its panel. `component` is for a synchronous core
 * panel; `load` is the normal path (a dynamic import, wrapped with the shared
 * async-component retry/error handling).
 */
export interface DockPanelDescriptor {
  component?: Component
  load?: () => Promise<unknown>
  /** Extra props beyond `active`, derived from the host context. */
  props?: (host: DockPanelHost, extras: DockPanelHostExtras) => Record<string, unknown>
  /** Event listeners, derived from the host context. */
  on?: (host: DockPanelHost, extras: DockPanelHostExtras) => Record<string, (...args: never[]) => void>
}

/**
 * Button appearance states, declared by the plugin.
 *
 * The host no longer knows any concrete plugin: each predicate answers "is this
 * state true right now" and the shared dock button attaches the matching class
 * (`has-unread` / `has-running` / `just-completed`). What it LOOKS like stays in
 * CSS — the plugin only declares WHEN.
 */
export interface DockAppearance {
  unread?: () => boolean
  running?: () => boolean
  justCompleted?: () => boolean
  /** Ongoing-activity dot (a meeting that is running), no number. */
  liveDot?: () => boolean
}

/**
 * A settings page contributed by the plugin.
 *
 * Declared by the plugin so the settings index and the category dispatcher can
 * both derive from the registry instead of hand-maintaining a list.
 */
export interface DockSettingsPage {
  /** Category id; defaults to the plugin id. */
  categoryId?: string
  /** Group on the settings index this page hangs under. */
  group: string
  icon: Component
  /** Flat field list / grouped panel / a custom component. */
  kind: 'flat' | 'group' | 'custom'
  /** Required for `kind: 'custom'`; a dynamic import, never a static one. */
  component?: () => Promise<unknown>
}

/**
 * What a deep link handler may ask the host to do.
 *
 * Deliberately NOT a generic `{followsProject, apply}` descriptor: the forge
 * jump has an ORDER that matters (refuse while blocked and clear the pending
 * target, then publish the target, then switch — possibly across projects, and
 * only clear on failure), and a template that cannot express it would silently
 * change behaviour. A handler that receives these five operations can keep the
 * original sequence verbatim while the host keeps ownership of the primitives.
 */
export interface DockDeepLinkContext {
  /** Switch to a tab (the host's guarded navigation). */
  switchTab(id: TabId): void
  /** Rebuild the app for another project; resolves even when the switch is refused. */
  hotSwitchProject(path: string): Promise<void>
  /** True when the tab has no entry right now (hidden / off / unavailable). */
  tabBlocked(id: DockTabId): boolean
  /** Record the notification log as the origin of this jump (for Back). */
  recordNotificationOrigin(detail: unknown): void
  /** The project currently loaded. */
  currentProject(): string
}

/**
 * A window event this plugin receives (notification / quote deep links).
 *
 * The host owns registration, unregistration and the context; the plugin owns
 * the handler, so a new deep link needs only a `plugin.ts` change.
 */
export interface DockDeepLink {
  /** Window event name, e.g. `clawbench-open-mail`. */
  event: string
  handler: (detail: unknown, ctx: DockDeepLinkContext) => void
}

/**
 * Runtime resources a Dock plugin contributes.
 *
 * Everything here is optional: a plugin that only renders a panel declares only
 * `panel`.
 */
export interface DockPluginRuntime {
  /**
   * Dynamic icon, for the rare plugin whose glyph depends on state (Forge
   * switches between GitHub and GitLab). Everything else uses the static
   * `icon` from the manifest.
   */
  dynamicIcon?: () => Component
  /** Dynamic title; falls back to the manifest's titleKey when absent. */
  title?: () => string
  /**
   * Dynamic i18n KEY for the label, for a plugin whose name depends on state
   * (Forge names the bound service). Every surface that shows the feature's
   * name — dock, settings manager, settings page, panel header — resolves it
   * through `dockTabTitleKey`, so they cannot drift to different keys.
   * Falls back to the manifest's titleKey when absent.
   */
  dynamicTitleKey?: () => string
  /** Current badge; null/absent means no badge. */
  badge?: () => DockBadge | null
  /** The panel this plugin renders in the left dock. */
  panel?: DockPanelDescriptor
  /**
   * Whether the plugin is usable right now. Absent = always usable.
   * The dock hides the entry when this returns false; the settings manager can
   * show `unavailableReasonKey`.
   */
  available?: () => boolean
  /**
   * Whether the host should leave the tab when it becomes unavailable.
   * Defaults to true. Terminal returns false on platform-unsupported so the
   * panel can show its dedicated "unsupported" empty state instead of being
   * navigated away from.
   */
  leaveWhenUnavailable?: () => boolean
  /** i18n key explaining why the plugin is unavailable. */
  unavailableReasonKey?: string
  /** Called when the tab becomes active. */
  onEnter?: () => void
  /** Called when the tab is left. */
  onLeave?: () => void
  /**
   * Background resource that must run even while the tab is closed (badge
   * polling). Returns a cleanup function; the host starts/stops it with
   * visibility and availability.
   */
  startBackground?: () => () => void
  /** Button appearance states. See DockAppearance. */
  appearance?: DockAppearance
  /**
   * What clicking the dock entry does beyond switching to the tab.
   *
   * Absent ⇒ the host just switches tabs. Runs BEFORE the switch (the terminal
   * clears the "open this directory" request so a plain click opens a plain
   * terminal, while the file manager's dedicated entry sets one).
   */
  onClick?: (host: DockPanelHost, extras: DockPanelHostExtras) => void
  /** Window events this plugin wants to receive. */
  deeplinks?: readonly DockDeepLink[]
}

/**
 * Manifest half: what the dock, the settings manager and the settings index
 * need to render an entry. Written by the plugin, assembled into
 * `DockTabDescriptor` by the runtime.
 */
export interface DockTabPluginManifest {
  /** i18n key for the tab label / tooltip. */
  titleKey: string
  /** i18n key for the one-line description in the manager. */
  descriptionKey: string
  /** Settings-manager grouping; does not change dock order. */
  group: DockGroup
  /**
   * Whether the user may hide this tab. Permanent tabs (browse, view,
   * settings) are always visible.
   */
  closable: boolean
  /**
   * Server master switch this plugin depends on. Present ⇒ the manager's
   * switch writes the server config and the tab disappears when it is off.
   */
  feature?: DockFeature
  /** Host/environment capability the tab needs. */
  capability?: DockCapability
  /** Settings category that owns this plugin's own configuration. */
  settingsCategory?: string
  /** Settings page this plugin contributes to the settings index. */
  settingsPage?: DockSettingsPage
  /** Static brand glyph for the dock entry. */
  icon: Component
}

/**
 * A plugin's complete self-description.
 *
 * Left-dock plugins must carry the manifest (title, description, group,
 * closable, icon) — a missing field is a type error. `chat` is the right-pane
 * surface: it is not a left-dock entry (no description / group / closable), but
 * its narrow-dock button is rendered by the same component, so it still names
 * its icon and title.
 */
export type DockPlugin =
  | ({ id: DockTabId } & DockTabPluginManifest & DockPluginRuntime)
  | ({ id: 'chat' } & Pick<DockTabPluginManifest, 'icon' | 'titleKey'> & DockPluginRuntime)
