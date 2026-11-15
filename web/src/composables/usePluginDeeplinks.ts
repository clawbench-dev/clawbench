import { dockDeepLinks } from './dockPluginRuntime'
import type { DockDeepLinkContext } from './dockPluginTypes'
import { appLog } from '@/utils/appLog'

const TAG = 'PluginDeeplinks'

/**
 * Installs the window-event handlers the plugins declare.
 *
 * The host no longer has one `handleOpenXxx` per plugin: a plugin says which
 * events it wants (`deeplinks` in `plugin.ts`) and gets the host's navigation
 * primitives through the context. Registration stays here so the shell keeps
 * ownership of add/remove symmetry and of the duplicate check.
 *
 * Returns the uninstaller; the SAME listener reference is removed, otherwise a
 * hot reload / remount would leave the previous handler attached and every deep
 * link would run twice.
 */
export function installPluginDeeplinks(ctx: DockDeepLinkContext): () => void {
  const bound: { event: string; listener: EventListener }[] = []
  const seen = new Map<string, string>()

  for (const { pluginId, link } of dockDeepLinks()) {
    const owner = seen.get(link.event)
    if (owner) {
      // Two plugins claiming one event means one of them would silently never
      // run (or both would, doubling the navigation). The first declaration
      // wins and the collision is reported — a plugin bug, not a runtime state.
      appLog.w(TAG, `event "${link.event}" is declared by both ${owner} and ${pluginId}`)
      continue
    }
    seen.set(link.event, pluginId)

    const listener: EventListener = (event) => {
      link.handler((event as CustomEvent<unknown>).detail, ctx)
    }
    window.addEventListener(link.event, listener)
    bound.push({ event: link.event, listener })
  }

  return () => {
    for (const { event, listener } of bound) window.removeEventListener(event, listener)
    bound.length = 0
  }
}
