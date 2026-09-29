import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the bottom dock hides while the soft keyboard is open, on every
 * platform.
 *
 * App.vue has no mount test (it is the entire application), so this wiring is
 * asserted at the source level — same pattern as waveBackgroundWiring.test.ts.
 *
 * Why it must not regress to a CSS offset: the earlier attempt pushed the dock
 * up with `bottom: <keyboardHeight>px` (still used for chat/terminal content),
 * but on a mobile browser the measurement came up short and left the dock ~20%
 * overlapped by the keyboard. Android WebView never showed the bug because
 * `adjustResize` shrinks the layout viewport there. Hiding is platform-agnostic
 * and is the behaviour the user asked for, Android included.
 */
describe('bottom dock keyboard hiding', () => {
  const APP = 'src/App.vue'

  function dockWrapperTag(src: string): string {
    const m = src.match(/<div[^>]*class="bottom-dock-wrapper"[^>]*>/)
    if (!m) throw new Error('bottom-dock-wrapper opening tag not found')
    return m[0]
  }

  it('gates the dock wrapper on the soft-keyboard state', () => {
    const tag = dockWrapperTag(readWebFile(APP))
    expect(tag).toMatch(/v-show="[^"]*!isSoftKeyboardOpen[^"]*"/)
  })

  it('does not gate the dock on the narrow-screen check alone', () => {
    // The regression: `v-show="!isWideScreen"` with no keyboard term means the
    // dock stays behind the IME on mobile browsers.
    const tag = dockWrapperTag(readWebFile(APP))
    expect(tag).not.toMatch(/v-show="!isWideScreen"\s*>/)
  })

  it('installs the detector from the app root', () => {
    const src = readWebFile(APP)
    expect(src).toMatch(/useSoftKeyboard\(\)/)
    expect(src).toMatch(/installSoftKeyboard\(\)/)
  })

  it('tears the detector down on unmount', () => {
    const src = readWebFile(APP)
    expect(src).toMatch(/uninstallSoftKeyboard\(\)/)
  })

  it('re-measures the dock when it reappears after the keyboard closes', () => {
    // Hiding the dock is a display:none toggle, and Android WebView may not
    // deliver the ResizeObserver callback for the display:none → visible
    // transition. Without an explicit re-measure the overflow layout is
    // computed from the width measured while hidden.
    const src = readWebFile(APP)
    const watchBlock = src.match(/watch\(isSoftKeyboardOpen,[\s\S]*?\n\}\)\n/)
    if (!watchBlock) throw new Error('isSoftKeyboardOpen watcher not found')
    expect(watchBlock[0]).toMatch(/startDockResize\(\)/)
    expect(watchBlock[0]).toMatch(/nextTick/)
  })

  it('zeroes --dock-height while the dock is hidden', () => {
    // Bottom-sheet drawers position with `bottom: var(--dock-height)`. A hidden
    // dock must leave them flush with the screen bottom rather than floating
    // above an invisible bar — the same rule the wide-screen branch follows.
    const src = readWebFile(APP)
    const watchBlock = src.match(/watch\(isSoftKeyboardOpen,[\s\S]*?\n\}\)\n/)
    if (!watchBlock) throw new Error('isSoftKeyboardOpen watcher not found')
    expect(watchBlock[0]).toMatch(/--dock-height',\s*'0px'/)
  })

  it('reports the keyboard from any focused editable, not just chat/terminal', () => {
    // The composable is global by design; if it were gated to the chat tab the
    // file-manager search, settings, task forms etc. would keep the overlap.
    const src = readWebFile('src/composables/useSoftKeyboard.ts')
    expect(src).toMatch(/addEventListener\('focusin'/)
    expect(src).toMatch(/isEditableElement/)
  })
})
