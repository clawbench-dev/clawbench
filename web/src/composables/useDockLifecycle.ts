import { onUnmounted, watchEffect } from 'vue'
import { DOCK_TAB_IDS, type DockTabId } from './dockTabs'
import {
  resolveDockPlugin,
  runDockOnEnter,
  runDockOnLeave,
  startDockBackground,
} from './dockPluginRuntime'
import { useDockAvailability } from './useDockAvailability'
import { appLog } from '@/utils/appLog'

const TAG = 'DockLifecycle'

/**
 * Owns the background resources plugins declare via `startBackground`.
 *
 * A resource runs only while its plugin is visible (not user-hidden) and
 * reachable — the same three axes the dock itself applies (server feature,
 * host capability, user preference). The sync is a `watchEffect`, so flipping a
 * switch, losing the terminal runtime or hiding the entry re-evaluates it.
 *
 * `startBackground` is currently only used by mail (the unread poller). The
 * lifecycle is generic so a new plugin can add a poller without touching
 * App.vue.
 */
export function useDockLifecycle() {
  const { availability } = useDockAvailability()
  const cleanups = new Map<DockTabId, () => void>()

  function shouldRun(id: DockTabId): boolean {
    const blocked =
      availability.value.hidden.has(id) ||
      availability.value.featureDisabled.has(id) ||
      availability.value.unavailable.has(id)
    return !!resolveDockPlugin(id)?.startBackground && !blocked
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

  /**
   * The tab whose `onEnter` has run and whose `onLeave` is still owed.
   *
   * Enter is idempotent because the shell reaches it from two places (the
   * narrow dock's switchTab and the wide layout's side effects), and a layout
   * change can replay the same tab. Leaving is what was missing entirely: a
   * plugin could not pause its polling when the user walked away.
   */
  let current: string | null = null

  function enter(id: string): void {
    if (current === id) return
    if (current) runDockOnLeave(current)
    current = id
    runDockOnEnter(id)
  }

  function leave(): void {
    if (!current) return
    runDockOnLeave(current)
    current = null
  }

  // Unmount is a leave too: the panel is going away, so the plugin must be told
  // once — then its background resource is stopped. Without this, `leave()` had
  // no caller outside `enter()`, i.e. the hook was only half wired.
  onUnmounted(() => {
    leave()
    stopAll()
  })

  return {
    sync,
    stopAll,
    enter,
    leave,
  }
}
