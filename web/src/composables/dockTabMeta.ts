import type { Component } from 'vue'
import {
  FolderOpen,
  FileText,
  GitBranch,
  Inbox,
  Network,
  SquareTerminal as TerminalIcon,
  Clock,
  BarChart3,
  Settings,
  Github,
} from 'lucide-vue-next'
import { DOCK_TABS, type DockTabId, type DockTabDescriptor } from '@/composables/dockTabs'

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
  overview: Inbox,
  forge: Github,
  tasks: Clock,
  terminal: TerminalIcon,
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
