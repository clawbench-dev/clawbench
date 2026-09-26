import { BrowserWindow, WebContentsView } from 'electron'
import path from 'node:path'
import { record } from './clientLog'
import {
  CONNECTION_TIMEOUT_MS,
  SPLASH_FAILSAFE_MS,
  isForwardStage,
  shouldShowSplash,
  splashBackgroundColor,
  stageForEvent,
  type SplashStage,
} from './splashPolicy'
import { getStore } from './store'
import { isDarkThemeId, normalizeThemeId } from '../shared/theme'

const TAG = 'Splash'

/** Matches the fade-out in the overlay's own CSS, so the two cannot drift. */
const FADE_OUT_MS = 200

/**
 * The native loading overlay, shown while the app connects to a server and boots.
 *
 * Desktop used to show a blank window for this whole period: the login page was
 * navigated away from, and the server page rendered nothing until `/api/me` and
 * the app's own initialization resolved. Android covers the same gap with a
 * native splash overlay; this is the desktop equivalent.
 *
 * The overlay is a `WebContentsView` parented to the window's `contentView`, so
 * it floats ABOVE the window's own page and survives the navigation from the
 * login page to the server page. Verified on Electron 44: a child view renders
 * over the window's page, `setVisible(false)` reveals it, and re-adding the
 * child view brings the overlay back on top.
 *
 * Its content is `login.html?splash=1` rather than a new file, because that page
 * already inlines all 36 theme palettes, the i18n table and the `ClawBenchNative`
 * preload. A separate splash page would need a third copy of the palette —
 * exactly the duplication `theme.test.ts` exists to police.
 */
export interface SplashController {
  /**
   * Show the overlay for a navigation to `url`, if that navigation warrants it.
   * Returns whether the overlay was shown.
   */
  show(url: string): boolean
  /** Hide the overlay. Idempotent, and safe after the window is gone. */
  dismiss(): void
  /** Stop loading, hide the overlay and hand control back to the caller. */
  cancel(): void
  /** Release the view and timers. Call on window close. */
  destroy(): void
  /** Whether the overlay is currently shown. */
  isVisible(): boolean
}

export interface SplashOptions {
  /**
   * Called when the user cancels, or when the connection times out. The caller
   * navigates back to the login page (and, for a timeout, reports the error).
   */
  onAbort: (reason: 'cancel' | 'timeout') => void
}

/** Absolute path to the login page, which doubles as the overlay's content. */
function loginPagePath(): string {
  return path.join(process.resourcesPath, 'login.html')
}

function preloadPath(): string {
  return path.join(__dirname, '../preload/index.js')
}

/**
 * Create the overlay for `win`. The view is created hidden; `show()` reveals it.
 *
 * Bounds track the window's content area, so a resize (or a maximise) while the
 * overlay is up does not leave it covering only part of the window.
 */
export function createSplashController(win: BrowserWindow, opts: SplashOptions): SplashController {
  const themeId = normalizeThemeId(getStore().get('theme'))

  const view = new WebContentsView({
    webPreferences: {
      preload: preloadPath(),
      contextIsolation: true,
      nodeIntegration: false,
    },
  })
  // Painted before the page parses, so a dark theme does not flash white. The
  // page itself applies the real theme from [data-theme] as soon as its <head>
  // runs; this only covers the gap.
  view.setBackgroundColor(splashBackgroundColor(isDarkThemeId(themeId)))
  view.setVisible(false)
  win.contentView.addChildView(view)

  let visible = false
  let destroyed = false
  /** Whether the overlay's own document has parsed and can take commands. */
  let pageReady = false
  /** The stage last handed to the page, replayed once it becomes ready. */
  let currentStage: SplashStage | null = null
  let failSafeTimer: NodeJS.Timeout | null = null
  let connectionTimer: NodeJS.Timeout | null = null
  let fadeTimer: NodeJS.Timeout | null = null

  const isAlive = () => !destroyed && !win.isDestroyed()

  function syncBounds(): void {
    if (!isAlive()) return
    const [width, height] = win.getContentSize()
    view.setBounds({ x: 0, y: 0, width, height })
  }

  function clearTimer(t: NodeJS.Timeout | null): null {
    if (t) clearTimeout(t)
    return null
  }

  function cancelTimers(): void {
    failSafeTimer = clearTimer(failSafeTimer)
    connectionTimer = clearTimer(connectionTimer)
    fadeTimer = clearTimer(fadeTimer)
  }

  /** Push `stage` into the overlay, unless it would move the text backwards. */
  function setStage(stage: SplashStage): void {
    if (!isForwardStage(currentStage, stage)) return
    currentStage = stage
    if (!pageReady || !isAlive()) return
    view.webContents
      .executeJavaScript(`window.__splashSetStage && window.__splashSetStage(${JSON.stringify(stage)})`)
      .catch(() => { /* overlay went away mid-call; nothing to update */ })
  }

  /**
   * Arm the fail-safe: the JS app dismisses the overlay itself once its
   * initialization resolves, but if that never happens (init threw, or the
   * bridge is unavailable) the overlay would cover the app forever.
   */
  function armFailSafe(): void {
    failSafeTimer = clearTimer(failSafeTimer)
    failSafeTimer = setTimeout(() => {
      failSafeTimer = null
      if (!visible || !isAlive()) return
      record('W', TAG, `fail-safe fired after ${SPLASH_FAILSAFE_MS}ms — JS never called dismissSplash(); forcing overlay hidden`)
      dismiss()
    }, SPLASH_FAILSAFE_MS)
  }

  /** Arm the connection deadline; any dismissal path cancels it. */
  function armConnectionTimeout(): void {
    connectionTimer = clearTimer(connectionTimer)
    connectionTimer = setTimeout(() => {
      connectionTimer = null
      if (!visible || !isAlive()) return
      record('W', TAG, `connection timed out after ${CONNECTION_TIMEOUT_MS}ms`)
      hideNow()
      opts.onAbort('timeout')
    }, CONNECTION_TIMEOUT_MS)
  }

  function hideNow(): void {
    cancelTimers()
    visible = false
    currentStage = null
    if (isAlive()) view.setVisible(false)
  }

  function dismiss(): void {
    if (!visible || !isAlive()) return
    cancelTimers()
    visible = false
    currentStage = null
    // Let the page play its own fade, then take the view out of the draw path.
    // The timer is not tracked in `cancelTimers` on purpose: a second dismiss()
    // returns early above, and destroy() clears it directly.
    try {
      void view.webContents.executeJavaScript('window.__splashFadeOut && window.__splashFadeOut()').catch(() => {})
    } catch { /* view already gone */ }
    fadeTimer = setTimeout(() => {
      fadeTimer = null
      if (isAlive()) view.setVisible(false)
    }, FADE_OUT_MS)
  }

  // ── Overlay page lifecycle ────────────────────────────────────────────────
  view.webContents.on('did-finish-load', () => {
    pageReady = true
    // The page may have finished loading after show() already set a stage.
    if (currentStage) {
      const stage = currentStage
      currentStage = null
      setStage(stage)
    }
  })

  // ── Window navigation drives the stage display ────────────────────────────
  // Only advance while the overlay is up: these events also fire for the login
  // page's own load, which has nothing to report.
  const onStageEvent = (event: string) => {
    if (!visible) return
    const stage = stageForEvent(event)
    if (stage) setStage(stage)
  }
  win.webContents.on('did-start-loading', () => onStageEvent('did-start-loading'))
  win.webContents.on('dom-ready', () => onStageEvent('dom-ready'))
  win.webContents.on('did-finish-load', () => {
    onStageEvent('did-finish-load')
    if (visible) armFailSafe()
  })

  const onResize = () => syncBounds()
  win.on('resize', onResize)
  win.on('maximize', onResize)
  win.on('unmaximize', onResize)

  return {
    show(url: string): boolean {
      if (!isAlive()) return false
      // A local file (the first-run login page) has nothing to wait for, so an
      // overlay would be a one-frame flash of the logo.
      if (!shouldShowSplash(url)) return false
      if (visible) return true

      // Drop any fade-out still pending from a previous dismiss, or it would
      // fire mid-way through this show and hide the overlay again.
      cancelTimers()
      visible = true
      syncBounds()
      view.setVisible(true)
      if (!pageReady) {
        // First use: load the overlay content. Subsequent shows reuse the
        // already-loaded document. setStage() below queues the stage, which the
        // did-finish-load handler replays once the page can take commands.
        void view.webContents
          .loadFile(loginPagePath(), { query: { splash: '1' } })
          .catch((err) => record('E', TAG, `overlay failed to load: ${String(err)}`))
      }
      setStage('connecting')
      armConnectionTimeout()
      return true
    },

    dismiss,

    cancel(): void {
      if (!isAlive()) return
      record('I', TAG, 'user cancelled the connection')
      // Stop the in-flight navigation before hiding, or the server page could
      // still replace the login page the caller is about to show.
      try { win.webContents.stop() } catch { /* nothing loading */ }
      hideNow()
      opts.onAbort('cancel')
    },

    destroy(): void {
      if (destroyed) return
      destroyed = true
      cancelTimers()
      win.removeListener('resize', onResize)
      win.removeListener('maximize', onResize)
      win.removeListener('unmaximize', onResize)
      if (!win.isDestroyed()) {
        try { win.contentView.removeChildView(view) } catch { /* already detached */ }
      }
      try { view.webContents.close() } catch { /* already gone */ }
    },

    isVisible(): boolean {
      return visible
    },
  }
}
