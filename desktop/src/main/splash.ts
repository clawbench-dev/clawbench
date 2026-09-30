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
   * Returns whether the overlay was shown. Never clobbers an active version gate.
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
  /**
   * Replace the loading overlay with the blocking version-mismatch gate. The
   * page renders the client/server versions and two actions (download /
   * continue); the main process owns what those actions do.
   */
  showVersionMismatch(info: GateOverlayInfo): void
  /** Dismiss the gate because the user chose to continue on the current version. */
  continueGate(): void
}

/** The version details the gate overlay renders. */
export interface GateOverlayInfo {
  /** The running desktop version. */
  clientVersion: string
  /** The version the server reports for itself. */
  serverVersion: string
  /**
   * Which side is newer, so the overlay can word itself correctly. `newer`
   * means the client is AHEAD of the server, where "download" aligns DOWN.
   */
  direction: 'older' | 'newer'
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

  /**
   * What the overlay is currently doing.
   *
   *  - `idle`  — hidden, nothing pending.
   *  - `splash`— the loading overlay (stages, connection timeout, fail-safe).
   *  - `gate`  — the blocking version-mismatch gate. It must survive the app's
   *              own `dismissSplash()` call (the app finishes booting BEHIND the
   *              gate), so `dismiss()` defers instead of hiding, and the boot
   *              fail-safe is disabled — the gate's buttons are the only exit.
   */
  type Mode = 'idle' | 'splash' | 'gate'
  let mode: Mode = 'idle'
  /**
   * Set by EVERY `dismiss()` call, including ones that arrive while the gate is
   * up. Without it, a dismiss that lands during the gate would be lost: after
   * the gate closes the overlay would stay hidden only by accident, and a
   * subsequent `show()` could not tell whether the app was already done.
   */
  let dismissRequested = false
  /** A gate requested before the overlay page was ready, replayed on load. */
  let pendingMismatch: GateOverlayInfo | null = null

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
    // The gate is a different screen; a stale loading stage must not repaint
    // over it (the overlay page is reused, so this is a real path).
    if (mode !== 'splash') return
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
   *
   * Never armed for the gate: its buttons are the deliberate exit, and a timer
   * that revealed the app underneath would defeat the whole point of blocking.
   */
  function armFailSafe(): void {
    if (mode !== 'splash') return
    failSafeTimer = clearTimer(failSafeTimer)
    failSafeTimer = setTimeout(() => {
      failSafeTimer = null
      if (mode !== 'splash' || !visible || !isAlive()) return
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
    mode = 'idle'
    visible = false
    currentStage = null
    pendingMismatch = null
    if (isAlive()) view.setVisible(false)
  }

  /** Fade the overlay out (used by the normal dismiss path). */
  function fadeOut(): void {
    cancelTimers()
    visible = false
    currentStage = null
    // Let the page play its own fade, then take the view out of the draw path.
    // `cancelTimers()` above already dropped any previous fade timer, so this
    // one is the only pending hide; show() cancels it if a new connect starts
    // mid-fade, and destroy() cancels it on window close.
    try {
      void view.webContents.executeJavaScript('window.__splashFadeOut && window.__splashFadeOut()').catch(() => {})
    } catch { /* view already gone */ }
    fadeTimer = setTimeout(() => {
      fadeTimer = null
      if (isAlive()) view.setVisible(false)
    }, FADE_OUT_MS)
  }

  function dismiss(): void {
    // Record the request even when the gate defers it: the app HAS finished
    // booting, and once the gate closes the overlay must not reappear.
    dismissRequested = true
    // While the gate is up the app finishes booting behind it; hiding here would
    // reveal the very app the gate exists to hold back. `continueGate()` does
    // the hiding instead.
    if (mode === 'gate') return
    if (!visible || !isAlive()) return
    mode = 'idle'
    fadeOut()
  }

  /** Hand the gate details to the overlay page. Assumes the page is ready. */
  function applyMismatch(info: GateOverlayInfo): void {
    if (!pageReady || !isAlive()) return
    // Reset first: the page is reused, so a previous gate's fade class (or a
    // prior splash state) would otherwise leave it transparent.
    view.webContents
      .executeJavaScript('window.__versionMismatchReset && window.__versionMismatchReset()')
      .then(() => view.webContents.executeJavaScript(
        `window.__versionMismatchShow && window.__versionMismatchShow(${JSON.stringify(info)})`,
      ))
      .catch(() => { /* overlay went away mid-call */ })
  }

  /**
   * Show the blocking version-mismatch gate.
   *
   * Called from the main process when the version check resolves — possibly
   * AFTER the app already called `dismiss()`. That is why this re-shows the view
   * and cancels timers rather than assuming the splash is still up: a pending
   * 200ms fade from the app's dismiss would otherwise hide the gate right after
   * it appears.
   */
  function showVersionMismatch(info: GateOverlayInfo): void {
    if (!isAlive()) return
    // Already gating: keep the first one. A second check (e.g. a reconnect) must
    // not restart the animation or drop the user's in-progress download.
    if (mode === 'gate') return

    // Drop any pending fade/hide from a dismiss or a finished splash.
    cancelTimers()
    mode = 'gate'
    visible = true
    currentStage = null
    syncBounds()
    view.setVisible(true)

    if (!pageReady) {
      // First use: load the overlay content; did-finish-load replays the gate.
      pendingMismatch = info
      void view.webContents
        .loadFile(loginPagePath(), { query: { splash: '1' } })
        .catch((err) => record('E', TAG, `overlay failed to load: ${String(err)}`))
      return
    }
    applyMismatch(info)
  }

  // ── Overlay page lifecycle ────────────────────────────────────────────────
  view.webContents.on('did-finish-load', () => {
    pageReady = true
    // A gate may have been requested before the page could take commands (the
    // check races the very first load). Replay it now.
    if (pendingMismatch) {
      const info = pendingMismatch
      pendingMismatch = null
      applyMismatch(info)
      return
    }
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
    if (!visible) return
    // The page loaded, so the CONNECTION succeeded: the connection deadline has
    // done its job and must not fire during a slow app boot. Android does the
    // same (cancelConnectionTimeout() then startSplashFailSafe()), and without
    // this a boot longer than 90s would throw the user back to the login page
    // even though the server answered.
    connectionTimer = clearTimer(connectionTimer)
    armFailSafe()
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
      // Never clobber the blocking version gate with a loading overlay.
      if (mode === 'gate') return false
      if (visible) return true

      // Drop any fade-out still pending from a previous dismiss, or it would
      // fire mid-way through this show and hide the overlay again.
      cancelTimers()
      mode = 'splash'
      dismissRequested = false
      visible = true
      syncBounds()
      view.setVisible(true)
      // The page is reused across connects, so clear the fade class the previous
      // dismissal left behind — otherwise this show renders fully transparent.
      if (pageReady) {
        void view.webContents
          .executeJavaScript('window.__splashReset && window.__splashReset()')
          .catch(() => { /* overlay went away mid-call */ })
      }
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
      // Only the loading overlay has a cancel button; the gate's markup omits it.
      if (mode === 'gate') return
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

    showVersionMismatch,

    continueGate(): void {
      if (mode !== 'gate') return
      mode = 'idle'
      dismissRequested = false
      // Unconditional: the gate is the thing being dismissed, so it must come
      // down even though the app's own dismiss was deferred while it was up.
      if (isAlive()) fadeOut()
      else { visible = false }
    },
  }
}
