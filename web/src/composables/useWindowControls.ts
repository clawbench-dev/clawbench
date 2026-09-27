import { onBeforeUnmount, onMounted, ref } from 'vue'
import { getNative } from '@/utils/clawbenchNative'

/**
 * Maximize state event dispatched by the native hosts.
 *
 * Mirrors `WINDOW_STATE_CHANNEL` in `desktop/src/shared/types.ts`. The desktop
 * preload forwards the IPC message as a `CustomEvent` of this name so the
 * renderer listens to the window rather than to IPC, keeping the two hosts
 * interchangeable.
 */
const WINDOW_STATE_EVENT = 'clawbench-window-state'

/**
 * App-drawn window controls for the frameless desktop shell.
 *
 * Gated on the HOST's answer (`hasCustomWindowControls()`), not on
 * `isDesktopApp` plus a user-agent guess: only the main process knows whether
 * the window actually has a frame, and the platform table lives there so it
 * cannot drift into a second copy.
 *
 * The whole cluster is conditional on that one flag, so the buttons cannot
 * appear over a window that still has a native title bar (which would show two
 * sets of controls) or disappear from a frameless one (which would leave the
 * window impossible to close).
 */
export function useWindowControls() {
  const native = getNative()
  const hasCustomControls = native?.hasCustomWindowControls?.() === true
  const isMaximized = ref(false)

  /**
   * Whether a pushed state has arrived. The initial read is a snapshot taken at
   * mount, so it must not overwrite a push that lands while it is in flight —
   * the window can change state (an OS snap, a double-click on the drag region)
   * between the two, and applying the stale read last would show the wrong glyph
   * until the next change.
   */
  let sawPushedState = false

  // Ask once on mount rather than relying on a push during load: the main
  // process cannot know when the renderer's listener is attached, so an initial
  // push would race Vue's mount and could be dropped.
  if (hasCustomControls) {
    void Promise.resolve(native?.isWindowMaximized?.())
      .then((value) => {
        if (sawPushedState) return
        if (typeof value === 'boolean') isMaximized.value = value
      })
      .catch(() => { /* keep the default glyph rather than break the header */ })
  }

  const onWindowState = (event: Event) => {
    const detail = (event as CustomEvent<{ maximized?: unknown }>).detail
    if (typeof detail?.maximized === 'boolean') {
      sawPushedState = true
      isMaximized.value = detail.maximized
    }
  }

  onMounted(() => {
    if (hasCustomControls) window.addEventListener(WINDOW_STATE_EVENT, onWindowState)
  })
  onBeforeUnmount(() => {
    window.removeEventListener(WINDOW_STATE_EVENT, onWindowState)
  })

  return {
    hasCustomControls,
    isMaximized,
    minimize: () => native?.windowMinimize?.(),
    toggleMaximize: () => native?.windowToggleMaximize?.(),
    close: () => native?.windowClose?.(),
  }
}
