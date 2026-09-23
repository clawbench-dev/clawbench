/**
 * App-level keyboard shortcuts, decided in the main process.
 *
 * These are dispatched from `webContents.on('before-input-event')` rather than
 * `globalShortcut`. A global shortcut is captured by the OS and never reaches
 * the renderer, which would break the page's own use of the same keys: the
 * terminal sends F5/F12 as escape sequences to the running TUI, and the file
 * manager uses F5 to refresh its listing. `before-input-event` runs in the
 * window's own event path, so this module can claim only the chords it wants
 * and leave everything else to the page.
 *
 * Kept free of electron imports so the decision table is unit-testable.
 */

/** The subset of Electron's Input we read. */
export interface KeyInput {
  type: string
  key: string
  /**
   * Physical key code (e.g. 'Equal', 'Numpad0'). Required for the zoom chords:
   * the numpad keys do not carry their own `key` — Numpad0 reports `Insert`,
   * and NumpadAdd reports `+` on some layouts but not others — so matching on
   * `key` alone would either miss the numpad or hit the wrong key entirely.
   */
  code?: string
  control: boolean
  shift: boolean
  alt: boolean
  meta: boolean
}

export interface ShortcutHandlers {
  /** Clear cache + hard reload. */
  onHardReload: () => void
  /** Show or hide DevTools. */
  onToggleDevTools: () => void
  /**
   * Change the page zoom. The shell applies it via `webContents.setZoomFactor`
   * (see `zoom.ts`); this module only decides whether a chord claims the key.
   */
  onZoom: (action: 'in' | 'out' | 'reset') => void
}

/**
 * Decide whether a key event is an app shortcut.
 *
 * Returns true when the event was claimed, meaning the page must NOT see it.
 * Anything unrecognised returns false and falls through to the renderer, which
 * is what keeps the terminal's F5/F12 working.
 */
export function handleShortcut(
  input: KeyInput,
  isMac: boolean,
  handlers: ShortcutHandlers,
): boolean {
  // Auto-repeat must not re-trigger a reload or flap DevTools.
  if (input.type !== 'keyDown') return false

  const lower = input.key.toLowerCase()
  const mod = isMac ? input.meta : input.control

  // Ctrl/Cmd+Shift+R — hard reload. Checked before the DevTools chord so a
  // later change to either cannot silently shadow this one.
  if (mod && input.shift && lower === 'r') {
    handlers.onHardReload()
    return true
  }

  // F12 — the conventional DevTools key on Windows/Linux.
  if (input.key === 'F12') {
    handlers.onToggleDevTools()
    return true
  }

  // Ctrl+Shift+I (Cmd+Option+I on macOS) — the other conventional DevTools
  // chord, kept because F12 is not universal on macOS keyboards.
  const devToolsChord = isMac
    ? input.meta && input.alt && lower === 'i'
    : input.control && input.shift && lower === 'i'
  if (devToolsChord) {
    handlers.onToggleDevTools()
    return true
  }

  // ── Page zoom ──
  // Chromium's own Ctrl+=/Ctrl+-/Ctrl+0 accelerators are gone (the app has no
  // menu bar), so the shell reproduces them here.
  //
  // Matched on `code`, not `key`: the numpad has no distinct `key` of its own
  // (Numpad0 reports 'Insert'), so a `key`-based table would silently miss it.
  //
  // `alt` is required to be absent because AltGr is delivered as
  // Ctrl+Alt on Windows — without this guard, typing AltGr+0 (a brace on many
  // European layouts) would reset the zoom instead of inserting a character.
  if (mod && !input.alt) {
    const code = input.code
    if (code === 'Equal' || code === 'NumpadAdd') {
      handlers.onZoom('in')
      return true
    }
    // Shift is deliberately NOT allowed here: Ctrl+Shift+Minus is Ctrl+_
    // (0x1f), which readline binds to undo, so claiming it would break the
    // terminal. This also matches browser behaviour — only unshifted Ctrl+-
    // zooms out.
    if (!input.shift && (code === 'Minus' || code === 'NumpadSubtract')) {
      handlers.onZoom('out')
      return true
    }
    if (!input.shift && (code === 'Digit0' || code === 'Numpad0')) {
      handlers.onZoom('reset')
      return true
    }
  }

  return false
}
