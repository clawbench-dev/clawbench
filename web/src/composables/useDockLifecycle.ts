import { onUnmounted, watchEffect } from 'vue'
import { DOCK_TAB_IDS, type DockTabId } from './dockTabs'
import {
  getDockPluginRuntime,
  isDockPluginAvailable,
  runDockOnEnter,
  runDockOnLeave,
  startDockBackground,
} from './dockPluginRuntime'
import { useDockPlugins } from './useDockPlugins'
import { appLog } from '@/utils/appLog'

const TAG = 'DockLifecycle'

/**
 * Owns the background resources plugins declare via `startBackground`.
 *
 * A resource runs only while its plugin is visible (not user-hidden) and
 * available (feature/capability gates). The sync is a `watchEffect`, so any
 * change to the hidden set, server feature config, terminal status or port
 * forwarding capability re-evaluates it.
 *
 * `startBackground` is currently only used by mail (the unread poller). The
 * lifecycle is generic so a new plugin can add a poller without touching
 * App.vue.
 */
export function useDockLifecycle() {
  const { hidden } = useDockPlugins()
  const cleanups = new Map<DockTabId, () => void>()

  function shouldRun(id: DockTabId): boolean {
    return (
      !!getDockPluginRuntime(id)?.startBackground &&
      !hidden.value.has(id) &&
      isDockPluginAvailable(id)
    )
  }

  function sync(): void {
    for (const id of DOCK_TAB_IDS) {
      const wanted = shouldRun(id)
      const running = cleanups.has(id)
      if (wanted && !running) {
        try {
          const cleanup = startDockBackground(id)
          if (cleanup) cleanups.set(id, cleanup)
        } catch (err) {
          appLog.w(TAG, `failed to start background for ${id}`, err)
        }
      } else if (!wanted && running) {
        try {
          cleanups.get(id)!()
        } catch (err) {
          appLog.w(TAG, `failed to stop background for ${id}`, err)
        }
        cleanups.delete(id)
      }
    }
  }

  function stopAll(): void {
    for (const [id, cleanup] of cleanups) {
      try {
        cleanup()
      } catch (err) {
        appLog.w(TAG, `failed to stop background for ${id}`, err)
      }
    }
    cleanups.clear()
  }

  watchEffect(sync)
  onUnmounted(stopAll)

  return {
    sync,
    stopAll,
    onEnter(id: string) {
      runDockOnEnter(id)
    },
    onLeave(id: string) {
      runDockOnLeave(id)
    },
  }
}
