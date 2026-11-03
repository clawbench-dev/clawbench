/**
 * The member's shared screen in its own OS window.
 *
 * Two mechanisms, one contract: the picture is a live MediaStream that belongs
 * to THIS renderer, so the window must be a second document we can render into
 * (the surface Teleports into `target`). A real navigation would get its own
 * process and could never see the track.
 *
 * - Desktop shell → a `window.open` popup, i.e. a genuine Electron window with
 *   a title bar, resize handles and an entry in the OS window stack. The shell
 *   has to allow that popup (`setWindowOpenHandler`), which is why the app's own
 *   `about:blank` popups are exempted there.
 * - Everywhere else → Document Picture-in-Picture, which needs no popup
 *   permission and floats above other windows.
 *
 * Either can fail (blocked popup, no PiP on Firefox/Android). The caller then
 * keeps the in-app fullscreen picture rather than leaving a dead button.
 */
import { shallowRef, type ShallowRef } from 'vue'
import { appLog } from '@/utils/appLog'
import { isDesktopApp } from '@/utils/clawbenchNative'

const TAG = 'ScreenWindow'

/**
 * A DOMException serialises to `{}`, which made "requestWindow failed {}" the
 * only clue we had when the picture refused to leave the app. Keep the name and
 * the message.
 */
function describeError(e: unknown): string {
  if (e instanceof Error) return `${e.name}: ${e.message}`
  return String(e)
}

/**
 * Window name, so re-opening reuses the same popup instead of stacking.
 *
 * The name is per SURFACE, not per app: `window.open` returns the existing
 * window for a name it already knows, so two surfaces sharing one name would
 * render into the same document — and closing either would take the other one
 * down with it. Each caller therefore passes its own (the member's picture vs
 * the drawing surface).
 */
export const SCREEN_WINDOW_NAME = 'clawbench-screen-window'
export const ANNOTATE_WINDOW_NAME = 'clawbench-annotate-window'

/** Result of trying to open the window. */
export type ScreenWindowOpenResult = 'opened' | 'failed'

interface PictureInPictureApi {
  requestWindow?: (opts?: { width?: number; height?: number }) => Promise<Window>
}

export interface ScreenWindow {
  /** Body of the open window, or null while it is closed. */
  target: ShallowRef<HTMLElement | null>
  open: (opts?: { width?: number; height?: number }) => Promise<ScreenWindowOpenResult>
  close: () => void
}

export function useScreenWindow(name: string = SCREEN_WINDOW_NAME): ScreenWindow {
  const target = shallowRef<HTMLElement | null>(null)
  const win = shallowRef<Window | null>(null)
  /** Popups have no `pagehide` in the opener; poll `closed` instead. */
  let closePoll: ReturnType<typeof setInterval> | null = null

  function stopPoll(): void {
    if (closePoll === null) return
    clearInterval(closePoll)
    closePoll = null
  }

  function forget(w: Window): void {
    // A late event from an already-replaced window must not clear the current
    // one.
    if (win.value !== w) return
    reset()
  }

  /** Drop our handle on the window without closing it (it may already be gone). */
  function reset(): void {
    stopPoll()
    target.value = null
    win.value = null
  }

  /**
   * Is the window we think we own still there?
   *
   * `target` alone is not an answer: a popup the user closed, or one that
   * navigated away from the document we rendered into, leaves the ref pointing
   * at a body that is no longer on screen. Treating that as "already open" made
   * the caller's button a silent no-op — the surface was gone and nothing could
   * bring it back short of reloading the app.
   */
  function alive(): boolean {
    const w = win.value
    const body = target.value
    if (!w || !body) return false
    if (w.closed) return false
    return body.isConnected
  }

  /** Point the surface at a second document and start watching for its close. */
  function adopt(w: Window, kind: 'pip' | 'popup'): ScreenWindowOpenResult {
    let doc: Document
    try {
      doc = w.document
    } catch (e) {
      // Cross-process popup: its DOM is unreachable, so nothing can be rendered
      // into it. Close it instead of leaving an empty window behind.
      appLog.w(TAG, 'window document is not reachable', describeError(e))
      try {
        w.close()
      } catch {
        /* already gone */
      }
      return 'failed'
    }
    copyDocumentStyles(document, doc)
    doc.title = 'ClawBench'
    appLog.i(TAG, `picture moved to a ${kind} window`)
    win.value = w
    target.value = doc.body
    if (kind === 'pip') {
      // Fires for the user's own close button as well as our close().
      w.addEventListener('pagehide', () => forget(w))
    } else {
      stopPoll()
      closePoll = setInterval(() => {
        if (w.closed) forget(w)
      }, 400)
    }
    return 'opened'
  }

  function openPopup(width: number, height: number): Window | null {
    try {
      const w = window.open('', name, `popup=yes,width=${width},height=${height}`)
      // The desktop shell answers `deny` with null (its popup handler), and so
      // does a browser popup blocker.
      if (!w) appLog.w(TAG, 'window.open was blocked')
      return w
    } catch (e) {
      appLog.w(TAG, 'window.open failed', describeError(e))
      return null
    }
  }

  async function open(opts: { width?: number; height?: number } = {}): Promise<ScreenWindowOpenResult> {
    if (target.value) {
      if (alive()) {
        // Already open: bring it forward instead of stacking a second one.
        try {
          win.value?.focus()
        } catch {
          /* the window is going away */
        }
        return 'opened'
      }
      // The window went away without the close event reaching us (or it left
      // our document). Forget it and open a fresh one — the alternative is a
      // button that never works again.
      appLog.w(TAG, 'previous window is gone, opening a new one')
      reset()
    }
    const width = opts.width ?? 1024
    const height = opts.height ?? 640

    // Desktop shell first: the user asked for an Electron window, and PiP
    // support there depends on the shell's Chromium build.
    if (isDesktopApp()) {
      const popup = openPopup(width, height)
      if (popup) return adopt(popup, 'popup')
    }

    const api = (globalThis as unknown as { documentPictureInPicture?: PictureInPictureApi })
      .documentPictureInPicture
    if (api?.requestWindow) {
      try {
        return adopt(await api.requestWindow({ width, height }), 'pip')
      } catch (e) {
        // Denied/blocked must not look like a dead button; the popup below is
        // the last resort.
        appLog.w(TAG, 'requestWindow failed', describeError(e))
      }
    }

    const popup = openPopup(width, height)
    if (popup) return adopt(popup, 'popup')
    return 'failed'
  }

  function close(): void {
    const w = win.value
    reset()
    try {
      w?.close()
    } catch {
      /* already gone */
    }
  }

  return { target, open, close }
}

/**
 * Copy a document's styling into another document.
 *
 * A separate document inherits nothing — not the sheets, not the theme, not the
 * UI-scale variables on <html> — so a window rendered without this is unstyled.
 * Rules are inlined when readable (same origin), which keeps the window working
 * even if the source tab navigates away.
 */
export function copyDocumentStyles(from: Document, to: Document): void {
  const root = from.documentElement
  to.documentElement.setAttribute('data-theme', root.getAttribute('data-theme') ?? '')
  to.documentElement.setAttribute('style', root.getAttribute('style') ?? '')
  to.body.className = from.body.className
  for (const sheet of Array.from(from.styleSheets)) {
    try {
      const css = Array.from(sheet.cssRules)
        .map((r) => r.cssText)
        .join('\n')
      const style = to.createElement('style')
      style.textContent = css
      to.head.appendChild(style)
    } catch {
      // Cross-origin sheet: rules are unreadable, so re-link instead.
      if (!sheet.href) continue
      const link = to.createElement('link')
      link.rel = 'stylesheet'
      link.href = sheet.href
      to.head.appendChild(link)
    }
  }
}
