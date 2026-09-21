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

  return false
}
