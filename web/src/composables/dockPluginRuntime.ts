import type { Component } from 'vue'
import type { TabId } from './dockTabs'
import type { DockBadge, DockPluginRuntime } from './dockPluginTypes'
import { chatDock } from '@/components/chat/dock'
import { browseDock } from '@/components/file/dock'
import { historyDock } from '@/components/git/dock'
import { forgeDock } from '@/components/forge/dock'
import { tasksDock } from '@/components/task/dock'
import { terminalDock } from '@/components/terminal/dock'
import { mailDock } from '@/components/mail/dock'
import { liveDock } from '@/components/live/dock'
import { proxyDock } from '@/components/proxy/dock'
import { kbDock } from '@/components/kb/dock'
import { shareDock } from '@/components/share/dock'
import { notificationsDock } from '@/components/notifications/dock'
import { statsDock } from '@/components/stats/dock'
import { settingsDock } from '@/components/settings/dock'

/**
 * The one place that maps a dock id to its runtime resources.
 *
 * Feature modules export a `dock.ts` descriptor; this file imports them and
 * the host reads everything through the resolvers below. Plugins never import
 * this module, and they never import each other.
 *
 * `chat` is included even though it is not in DOCK_TABS: it is the right-pane
 * surface and uses the same badge/onEnter contract. It has no background
 * resource, so the lifecycle loop (which walks DOCK_TABS) never touches it.
 */
const RUNTIME: Partial<Record<TabId, DockPluginRuntime>> = {
  chat: chatDock,
  browse: browseDock,
  history: historyDock,
  forge: forgeDock,
  tasks: tasksDock,
  terminal: terminalDock,
  mail: mailDock,
  live: liveDock,
  proxy: proxyDock,
  kb: kbDock,
  share: shareDock,
  notifications: notificationsDock,
  stats: statsDock,
  settings: settingsDock,
}

export function getDockPluginRuntime(id: string): DockPluginRuntime | undefined {
  return RUNTIME[id as TabId]
}

export function resolveDockBadge(id: string): DockBadge | null {
  return getDockPluginRuntime(id)?.badge?.() ?? null
}

export function resolveDockTitle(id: string): string | undefined {
  return getDockPluginRuntime(id)?.title?.()
}

export function resolveDockIcon(id: string): Component | undefined {
  return getDockPluginRuntime(id)?.icon?.()
}

/** True when the plugin is usable now; plugins without a check are usable. */
export function isDockPluginAvailable(id: string): boolean {
  return getDockPluginRuntime(id)?.available?.() ?? true
}

/** Whether the host should leave the tab when it becomes unavailable. */
export function shouldLeaveUnavailableDockTab(id: string): boolean {
  return getDockPluginRuntime(id)?.leaveWhenUnavailable?.() ?? true
}

export function dockUnavailableReasonKey(id: string): string | undefined {
  return getDockPluginRuntime(id)?.unavailableReasonKey
}

export function runDockOnEnter(id: string): void {
  getDockPluginRuntime(id)?.onEnter?.()
}

export function runDockOnLeave(id: string): void {
  getDockPluginRuntime(id)?.onLeave?.()
}

/** Start a plugin's background resource; returns its cleanup, or null. */
export function startDockBackground(id: string): (() => void) | null {
  const start = getDockPluginRuntime(id)?.startBackground
  return start ? start() : null
}
