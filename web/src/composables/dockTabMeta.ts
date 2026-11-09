import type { Component } from 'vue'
import {
  FolderOpen,
  FileText,
  GitBranch,
  Network,
  SquareTerminal as TerminalIcon,
  Clock,
  BarChart3,
  Settings,
  Github,
  Inbox,
  Mail,
  Share2,
} from 'lucide-vue-next'
import { DOCK_TABS, type DockTabId, type DockTabDescriptor } from '@/composables/dockTabs'
import { MEETING_ICON } from '@/utils/meetingIcon'

/**
 * Icon half of the dock tab registry.
 *
 * Kept separate from `dockTabs.ts` so that the icon-free half (ids, order,
 * i18n keys) can be imported by composables without dragging `lucide-vue-next`
 * into their module graph — see the header of dockTabs.ts for why that matters.
 *
 * `DOCK_TABS` remains the single source of truth for *which* tabs exist and in
 * what order; this file only attaches a brand icon to each of them.
 */
const DOCK_TAB_ICONS: Record<DockTabId, Component> = {
  browse: FolderOpen,
  view: FileText,
  history: GitBranch,
  forge: Github,
  tasks: Clock,
  // Inbox, not Bell: this tab is a browsable, append-only log of what happened,
  // not an alert channel. A bell would read as "something needs your attention"
  // and imply the unread state the log deliberately does not have.
  notifications: Inbox,
  terminal: TerminalIcon,
  // A monitor with an outgoing arrow: the picture half of a meeting. See
  // utils/meetingIcon.ts — the list header, the quote chip and this tab share
  // one constant so they cannot drift apart.
  live: MEETING_ICON,
  // Share2 (a node with two arrows leaving it), not a link glyph: this tab is
  // the OUTGOING side — what has been handed to other people — and the meeting
  // icon next to it already carries the "screen" half of that idea.
  share: Share2,
  mail: Mail,
  proxy: Network,
  stats: BarChart3,
  settings: Settings,
}

/** Dock tab metadata with its icon attached, in registry order. */
export const DOCK_TABS_WITH_ICONS: readonly (DockTabDescriptor & { icon: Component })[] =
  DOCK_TABS.map((tab) => ({ ...tab, icon: DOCK_TAB_ICONS[tab.id] }))

const BY_ID: ReadonlyMap<DockTabId, DockTabDescriptor & { icon: Component }> = new Map(
  DOCK_TABS_WITH_ICONS.map((t) => [t.id, t]),
)

/** Icon-aware descriptor lookup. Returns undefined for unknown ids. */
export function dockTabWithIcon(tab: string): (DockTabDescriptor & { icon: Component }) | undefined {
  return BY_ID.get(tab as DockTabId)
}
