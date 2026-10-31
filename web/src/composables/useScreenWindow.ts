/**
 * The member's shared screen in its own OS window.
 *
 * Uses Document Picture-in-Picture rather than a BrowserWindow: the track lives
 * in this renderer's RTCPeerConnection, and a separate Electron window is a
 * separate JS context that cannot receive it. PiP hands back a real window
 * whose document we can render into (the panel Teleports into `target`), so the
 * picture and its controls stay on one object graph.
 *
 * Hosts without the API (Android WebView, Firefox) get `'unsupported'` and the
 * caller falls back to an in-app overlay — the picture stays viewable, just not
 * detachable.
 */
import { shallowRef, type ShallowRef } from 'vue'
import { appLog } from '@/utils/appLog'

const TAG = 'ScreenWindow'

/** Result of trying to open the window. */
export type ScreenWindowOpenResult = 'opened' | 'unsupported' | 'failed'

interface PictureInPictureApi {
  requestWindow?: (opts?: { width?: number; height?: number }) => Promise<Window>
}

export interface ScreenWindow {
  /** Body of the open window, or null while it is closed. */
  target: ShallowRef<HTMLElement | null>
  open: (opts?: { width?: number; height?: number }) => Promise<ScreenWindowOpenResult>
  close: () => void
}

export function useScreenWindow(): ScreenWindow {
  const target = shallowRef<HTMLElement | null>(null)
  const win = shallowRef<Window | null>(null)

  async function open(opts: { width?: number; height?: number } = {}): Promise<ScreenWindowOpenResult> {
    if (target.value) {
      // Already open: bring it forward instead of stacking a second one.
      try {
        win.value?.focus()
      } catch {
        /* the window is going away */
      }
      return 'opened'
    }
    const api = (globalThis as unknown as { documentPictureInPicture?: PictureInPictureApi })
      .documentPictureInPicture
    if (!api?.requestWindow) return 'unsupported'
    try {
      const w = await api.requestWindow({ width: opts.width ?? 1024, height: opts.height ?? 640 })
      copyDocumentStyles(document, w.document)
      // Fires for the user's own close button as well as our close().
      w.addEventListener('pagehide', () => {
        target.value = null
        win.value = null
      })
      win.value = w
      target.value = w.document.body
      return 'opened'
    } catch (e) {
      // A denied/blocked popup must not look like a dead button.
      appLog.w(TAG, 'requestWindow failed', e)
      return 'failed'
    }
  }

  function close(): void {
    const w = win.value
    target.value = null
    win.value = null
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
