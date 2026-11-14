import type { Component } from 'vue'

/**
 * Runtime resources a Dock plugin can contribute.
 *
 * This file is intentionally type-only so feature `dock.ts` modules can import
 * it without creating a runtime cycle with the aggregator
 * (`dockPluginRuntime.ts`), which imports those modules.
 */
export interface DockBadge {
  /** Numeric badge. Use 0 with `live: true` for a dot-only indicator. */
  count: number
  /** Ongoing-activity dot (meetings), not a count. */
  live?: boolean
}

/**
 * Host callbacks a panel may need. Passed from App.vue to DockPanelHost, then
 * forwarded to each plugin's `panel.props` / `panel.on`.
 *
 * Keep this narrow: it is the contract between the shell and every panel. A
 * panel that needs more should receive it as a normal prop from its own
 * `dock.ts`, not by importing App.vue.
 */
export interface DockPanelHost {
  projectPath: string
  terminalRequestedCwd: string | null
  platformUnsupported: boolean
  openFile(path: string): void
  openTaskFile(path: string, lineStart?: number): void
  quoteMeeting(token: string): void
  openMailSettings(): void
  requestProject(): void
  openMeetingRelaySettings(): void
  clearTerminalCwd(): void
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
  props?: (host: DockPanelHost) => Record<string, unknown>
  /** Event listeners, derived from the host context. */
  on?: (host: DockPanelHost) => Record<string, (...args: never[]) => void>
}

export interface DockPluginRuntime {
  /**
   * Dynamic icon. Only needed when the icon changes with state (Forge switches
   * between GitHub and GitLab); everything else stays in dockTabMeta.ts.
   */
  icon?: () => Component
  /** Dynamic title; falls back to the manifest's titleKey when absent. */
  title?: () => string
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
}
