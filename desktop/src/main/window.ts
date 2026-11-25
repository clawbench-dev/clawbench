import { BrowserWindow, Menu, MenuItem, clipboard, dialog, screen, shell } from 'electron'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { getStore } from './store'
import { contextMenuLabels } from './contextMenu'
import { resolveNativeLanguage, unloadGuardLabels } from './labels'
import { classifyUrl, isAppPopup, shouldBlockPopupNavigation } from './urlPolicy'
import { markRendererLoading } from './navReady'
import { handleShortcut } from './shortcuts'
import { shouldFallBackToLogin, buildConnectErrorScript } from './loadFailure'
import { createSplashController, type SplashController } from './splash'
import {
  beginNavigation,
  checkVersionGate,
  getActiveGate,
  installServerVersion,
  setActiveGate,
} from './versionGate'
import { nextZoomFactor, type ZoomAction } from './zoom'
import { shouldUseFramelessWindow } from './windowChrome'
import { cancelPendingQuit, handleCloseRequest, isAppQuitting, noteOsShutdown } from './closeFlow'
import { isEmbeddedServerActive, isEmbeddedServerUrl } from './localServer'
import { isStartupSettled, whenStartupSettled } from './startupGate'
import { embeddedServerLabels } from './labels'
import { record } from './clientLog'
import { instanceLabel } from './instance'
import { boundsToPersist, resolveStartupBounds, MIN_WINDOW_HEIGHT, MIN_WINDOW_WIDTH, type WindowBounds } from './windowGeometry'
import { WINDOW_STATE_CHANNEL, type WindowState } from '../shared/types'

let mainWindow: BrowserWindow | null = null

/**
 * The native loading overlay for the main window, if one is up.
 *
 * Kept at module scope because the bridge (`native:dismiss-splash` from the
 * app, `native:splash-cancel` from the overlay's cancel button) and the window
 * lifecycle both need it, and they reach it through the exported helpers below
 * rather than each holding their own reference.
 */
let splash: SplashController | null = null

/** Show the loading overlay for a navigation to `url`, if it warrants one. */
export function showSplashFor(url: string): void {
  splash?.show(url)
}

/** Hide the loading overlay. Idempotent; safe when none is up. */
export function dismissSplash(): void {
  splash?.dismiss()
}

/**
 * Abort the in-flight connection from the overlay's cancel button: stop the
 * navigation, hide the overlay, and return to the server-selection page.
 */
export function cancelSplash(): void {
  splash?.cancel()
}

/**
 * Check the desktop/server version consistency for a navigation to `url` and,
 * on a mismatch, raise the blocking gate. Fire-and-forget: the check is
 * asynchronous and its result is applied only if the navigation is still the
 * current one.
 */
export function checkVersionGateFor(url: string): void {
  // The built-in server is part of this package and updates with it, so its
  // version can never be a mismatch this gate could fix — and walking the
  // middle of a download here would only offer to replace the app under itself.
  //
  // Keyed on the ADDRESS, not on "we run a built-in server": an integrated build
  // keeps its own server running while the user is connected to another
  // machine, and that machine's version must still be checked.
  if (isEmbeddedServerUrl(url)) {
    setActiveGate(null)
    return
  }
  const gen = beginNavigation()
  void checkVersionGate(url, gen)
    .then((info) => {
      if (!info) {
        // No mismatch (or nothing to compare): make sure a gate from an earlier
        // navigation cannot be acted on.
        setActiveGate(null)
        return
      }
      setActiveGate(info)
      splash?.showVersionMismatch(info)
    })
    .catch(() => { /* a gate check must never break a connection */ })
}

/** Dismiss the version gate (the overlay's "continue" action). */
export function continueVersionGate(): void {
  setActiveGate(null)
  splash?.continueGate()
}

/**
 * Abandon any version-gate work for the current navigation.
 *
 * Bumps the navigation generation so an in-flight check cannot raise the gate
 * afterwards, and clears any gate already up. Used when the connection fails or
 * the user returns to the login page, where a gate would be meaningless.
 */
export function abortVersionGate(): void {
  beginNavigation()
  setActiveGate(null)
  splash?.continueGate()
}

/**
 * Install the server's version from the gate ("download" action). The gate
 * stays up on failure; on success the app restarts into the new version (or the
 * gate closes when the user defers the restart, so it stops offering a download
 * of the version already on disk).
 */
export async function downloadVersionFromGate(): Promise<void> {
  const info = getActiveGate()
  if (!info) return
  if (await installServerVersion(info, mainWindow)) continueVersionGate()
}

export function getMainWindow(): BrowserWindow | null { return mainWindow }

/**
 * Callbacks run for every main window, including one re-created later (macOS
 * `activate`, the capsule's "open the app" when the window was closed).
 *
 * A listener cannot simply be attached once at startup: the window is replaced
 * when it is closed and re-opened, and a stale listener would leave the new one
 * unobserved. `floating.ts` needs this to follow minimize/hide — it imports this
 * module, so a callback registry keeps the dependency pointing one way.
 */
const mainWindowHooks: ((win: BrowserWindow) => void)[] = []

export function onMainWindowCreated(fn: (win: BrowserWindow) => void): void {
  mainWindowHooks.push(fn)
  // `initFloating` runs after the first `createMainWindow`, so the window that
  // already exists has to be handed over here rather than only on the next one.
  if (mainWindow && !mainWindow.isDestroyed()) fn(mainWindow)
}

function notifyMainWindowCreated(win: BrowserWindow): void {
  for (const fn of mainWindowHooks) {
    try {
      fn(win)
    } catch {
      /* a listener must never break window creation */
    }
  }
}

function loginPagePath(): string {
  return path.join(process.resourcesPath, 'login.html')
}

/**
 * Show the server-selection login page, or explain why there is nothing to show.
 *
 * In the normal build there is always something to pick, so this is the login
 * page plus the connection error it must display. In the integrated build, a failure
 * against the built-in server gets a dialog naming that address first (the
 * overlay may already be gone, so a page change alone would look like a plain
 * failure) — and then the login page anyway, so the user can still reach
 * another machine. A failure against a REMOTE server while the built-in one
 * happens to be running must not be described as "the built-in server stopped".
 */
async function serveLoginOrExplain(reason: string): Promise<void> {
  const target = getStore().get('serverUrl')
  if (target && isEmbeddedServerUrl(target) && !isEmbeddedServerActive()) {
    const labels = embeddedServerLabels(resolveNativeLanguage())
    dialog.showErrorBox(
      labels.stoppedTitle,
      labels.stoppedMessage.replace('{url}', target) + (reason ? `\n\n${reason}` : ''),
    )
  }
  try {
    await mainWindow?.loadFile(loginPagePath())
    const wc = mainWindow?.webContents
    if (wc) void wc.executeJavaScript(buildConnectErrorScript(reason), true)
  } catch {
    /* login page itself failed to load; nothing to report to */
  }
}

/**
 * Claim the app-level shortcuts (page zoom, hard reload, DevTools) on this window.
 *
 * Uses `before-input-event` rather than `globalShortcut`: a global shortcut is
 * captured by the OS and never reaches the renderer, which would break the
 * page's own use of the same keys — the terminal sends F5/F12 to the running
 * TUI and the file manager refreshes on F5. This runs in the window's own
 * event path, so unclaimed keys fall through to the page untouched.
 *
 * Also gives the window Ctrl+Wheel page zoom. `webContents` announces that
 * gesture with `zoom-changed` but does not apply it (measured on Electron 44 —
 * the built-in handling is gone now that the app has no menu bar), so the shell
 * has to set the factor itself.
 *
 * Keys over an element that calls `preventDefault()` on Ctrl+Wheel never reach
 * this event at all, which is what keeps the terminal, PDF and office previews
 * on their own Ctrl+Wheel zoom instead of zooming the whole page. Verified
 * against real input: three events over a plain area, zero over a
 * `preventDefault` region.
 *
 * The zoom mode is intentionally left at Chromium's `default`. `manual` reads
 * as the tidier option but is not: it neither applies the factor live nor
 * persists it, so it would silently break both the visual result and the
 * per-origin memory across restarts.
 */
function registerKeyboardShortcuts(webContents: Electron.WebContents): void {
  const isMac = process.platform === 'darwin'

  const applyZoom = (action: ZoomAction) => {
    webContents.setZoomFactor(nextZoomFactor(webContents.getZoomFactor(), action))
  }

  webContents.on('zoom-changed', (_event, direction) => {
    applyZoom(direction === 'in' ? 'in' : 'out')
  })

  webContents.on('before-input-event', (event, input) => {
    const claimed = handleShortcut(input, isMac, {
      // Imported lazily: session.ts imports getMainWindow() from this module,
      // so a top-level import here would be a cycle.
      onHardReload: () => { void import('./session').then((m) => m.clearCacheAndReload('everything')) },
      onToggleDevTools: () => {
        const wc = webContents
        if (wc.isDevToolsOpened()) wc.closeDevTools()
        else wc.openDevTools({ mode: 'bottom' })
      },
      onZoom: applyZoom,
    })
    // preventDefault stops the renderer from also seeing a key we handled.
    if (claimed) event.preventDefault()
  })
}

/**
 * Make sure a prompt owned by this window can actually be seen.
 *
 * `will-prevent-unload` is also reached from the tray's "Quit", and by then the
 * window is usually hidden or minimized — that is the state the tray exists for.
 * A modal dialog owned by an invisible window is a dialog nobody can answer, and
 * because the handler below is synchronous the main process would stay blocked
 * for the rest of the app's life: the close button and the tray's "Quit" both
 * look dead (the tray menu still opens, since it is drawn by the OS, but the
 * click is queued behind the blocked JS and never runs). Bringing the window
 * back first turns that deadlock into an ordinary question.
 */
function revealForPrompt(win: BrowserWindow): void {
  if (win.isDestroyed()) return
  try {
    if (win.isMinimized()) win.restore()
    if (!win.isVisible()) win.show()
    win.focus()
  } catch {
    /* the window is going away; the dialog below is still worth asking */
  }
}

/**
 * Turn a prevented unload into a real question.
 *
 * The renderer cancels `beforeunload` while a meeting is publishing, because the
 * publisher socket IS the meeting's lifetime (see useLiveShare.ts). Electron's
 * default for that is to drop the unload SILENTLY — no dialog — so F5 and the
 * window's close button look dead rather than dangerous. This handler is what
 * makes the guard visible; choosing "leave" lets the unload through.
 *
 * The meeting guard is the only renderer code that cancels `beforeunload` today,
 * which is why the wording is meeting-specific (see `unloadGuardLabels`).
 */
function registerUnloadGuard(win: BrowserWindow): void {
  win.webContents.on('will-prevent-unload', (event) => {
    record('W', 'Unload', 'the page vetoed the unload — asking whether to leave')
    revealForPrompt(win)
    const labels = unloadGuardLabels(resolveNativeLanguage())
    const choice = dialog.showMessageBoxSync(win, {
      type: 'warning',
      buttons: [labels.leave, labels.stay],
      // Esc / closing the dialog must mean "stay": leaving is destructive.
      defaultId: 1,
      cancelId: 1,
      noLink: true,
      title: labels.title,
      message: labels.message,
      detail: labels.detail,
    })
    if (choice === 0) {
      record('I', 'Unload', 'user chose to leave — the unload proceeds')
      // For `will-prevent-unload`, `preventDefault` means "ignore the
      // renderer's veto and unload anyway" — i.e. the user chose to leave.
      event.preventDefault()
      return
    }
    record('I', 'Unload', 'user chose to stay — the quit is cancelled')
    // The window refused to close, which also aborts whatever quit started this
    // close. Clearing the flag is what makes the NEXT close ask again instead of
    // quitting outright — the very decision the user just declined.
    cancelPendingQuit()
  })
}

/** Register native context menu handlers for text selection, editable fields, links, and images. */
function registerContextMenu(webContents: Electron.WebContents): void {
  webContents.on('context-menu', (_e, params) => {
    // cut/copy/paste use Electron roles (auto-localized by the OS); only the
    // custom items are translated from the current app language.
    //
    // Resolved INSIDE the handler, not once at registration: the labels used to
    // be frozen at window creation, so switching the UI language mid-session
    // kept showing the old wording until a restart. `resolveNativeLanguage()`
    // prefers the web UI's choice (mirrored into the store) and falls back to
    // the OS locale, so it tracks the user's live preference.
    const labels = contextMenuLabels(resolveNativeLanguage())
    const menu = new Menu()

    // 1. Editable inputs (input, textarea, contenteditable)
    if (params.isEditable) {
      const hasSelection = params.selectionText.trim().length > 0
      if (hasSelection) {
        menu.append(new MenuItem({ role: 'cut', enabled: params.editFlags.canCut }))
        menu.append(new MenuItem({ role: 'copy', enabled: params.editFlags.canCopy }))
      }
      menu.append(new MenuItem({ role: 'paste', enabled: params.editFlags.canPaste }))
      if (params.linkURL) {
        menu.append(new MenuItem({
          label: labels.copyLink,
          click: () => { clipboard.writeText(params.linkURL) },
        }))
      }
    } else {
      // 2. Normal text selection outside editable inputs
      if (params.selectionText.trim().length > 0) {
        menu.append(new MenuItem({ role: 'copy', enabled: params.editFlags.canCopy }))
      }

      // 3. Link (supports both web URLs and in-app file/anchor links)
      if (params.linkURL) {
        menu.append(new MenuItem({
          label: labels.copyLink,
          click: () => { clipboard.writeText(params.linkURL) },
        }))
      }

      // 4. Image copying
      if ((params.mediaType === 'image' || params.hasImageContents) && !params.selectionText.trim()) {
        menu.append(new MenuItem({
          label: labels.copyImage,
          click: () => { webContents.copyImageAt(params.x, params.y) },
        }))
      }
    }

    if (menu.items.length > 0) {
      menu.popup()
    }
  })
}

/**
 * Keep the renderer's view of the maximize state in step with the window's.
 *
 * The header's maximize/restore button must show the right glyph, but the state
 * can change from places the renderer cannot see: our own IPC toggle, an OS
 * snap/tile, or a double-click on the drag region (Electron's built-in
 * behaviour for a draggable region, which does NOT go through our IPC at all).
 * Listening to the window events therefore covers every path, whereas mirroring
 * state in the renderer after each click would miss the others.
 *
 * The initial value is NOT pushed here: a push on load would race the
 * renderer's own subscription (the preload's listener fires before Vue mounts),
 * so the renderer instead queries the current state once when it mounts. That
 * makes the initial read deterministic instead of order-dependent.
 */
function registerWindowStateReporting(win: BrowserWindow): void {
  const send = () => {
    if (win.isDestroyed()) return
    const state: WindowState = { maximized: win.isMaximized() }
    win.webContents.send(WINDOW_STATE_CHANNEL, state)
  }
  win.on('maximize', send)
  win.on('unmaximize', send)
}

/** Minimize the main window (frameless header control). */
export function minimizeMainWindow(): void {
  if (mainWindow && !mainWindow.isDestroyed()) mainWindow.minimize()
}

/**
 * Toggle the main window between maximized and restored (frameless header
 * control). Toggling rather than setting avoids the two sides disagreeing about
 * the current state — the window is the authority.
 */
export function toggleMaximizeMainWindow(): void {
  if (!mainWindow || mainWindow.isDestroyed()) return
  if (mainWindow.isMaximized()) mainWindow.unmaximize()
  else mainWindow.maximize()
}

/**
 * Close the main window (frameless header control).
 *
 * Goes through the window's own `close` event rather than acting directly, so
 * this button, Alt+F4 and the taskbar's "close" all take the same route into
 * `closeFlow.ts`.
 */
export function closeMainWindow(): void {
  if (mainWindow && !mainWindow.isDestroyed()) mainWindow.close()
}

/** Whether the main window is currently maximized (frameless header control). */
export function isMainWindowMaximized(): boolean {
  return !!mainWindow && !mainWindow.isDestroyed() && mainWindow.isMaximized()
}

/**
 * Route every close of the main window through the close-behaviour flow.
 *
 * The header's close button only reaches the main process over IPC, but Alt+F4,
 * the taskbar's "close" and the window manager's X all fire the window's own
 * `close` event — intercepting here is what makes them behave identically
 * instead of some quitting and some asking.
 *
 * The event is prevented up front: the flow decides (quit / hide to tray /
 * ask), and `app.quit()` re-enters this handler with `isAppQuitting()` true, at
 * which point it steps aside. macOS is left alone — it keeps the native frame,
 * its red button already means "close the window, keep the app" and the dock
 * icon is the way back, so there is nothing to ask.
 */
function registerCloseInterception(win: BrowserWindow): void {
  if (process.platform === 'darwin') return
  // Windows shuts down by closing every window. That is not a user close: the
  // dialog would appear on a machine that is already going away, and preventing
  // the close can hold the shutdown up.
  win.on('query-session-end', () => noteOsShutdown())
  win.on('close', (event) => {
    if (isAppQuitting()) return
    event.preventDefault()
    // A flow that throws (a dialog that cannot open, a tray that cannot be
    // created) leaves the window open, which is the safe outcome — it must not
    // become an unhandled rejection either.
    void handleCloseRequest(win).catch(() => { /* window stays open */ })
  })
}

/**
 * The bounds to open the main window with: the saved geometry when it is still
 * reachable, otherwise a centred default on the primary display.
 *
 * `screen` is only usable after the app is ready, and createMainWindow is only
 * called after that — but the try/catch keeps a display-query failure from
 * preventing the window from existing at all (the app would be unusable).
 */
function startupWindowBounds(): { bounds: WindowBounds; maximized: boolean } {
  const stored = getStore()
  const saved = stored.get('windowBounds')
  const maximized = stored.get('windowMaximized') === true
  try {
    const displays = screen.getAllDisplays()
    const primary = screen.getPrimaryDisplay()
    return { bounds: resolveStartupBounds(saved, displays, primary), maximized }
  } catch {
    return { bounds: resolveStartupBounds(saved, [], null), maximized }
  }
}

/** Debounce handle for geometry writes (resize/move fire continuously). */
let geometryTimer: ReturnType<typeof setTimeout> | null = null

/**
 * Persist the window's restored geometry, debounced.
 *
 * Writes go through `boundsToPersist`, which ignores fullscreen and stores the
 * RESTORED rect while maximized — see its doc for why saving the current rect
 * would strand the user with an oversized window after un-maximizing.
 */
function scheduleGeometrySave(win: BrowserWindow): void {
  if (geometryTimer) clearTimeout(geometryTimer)
  geometryTimer = setTimeout(() => {
    geometryTimer = null
    if (win.isDestroyed()) return
    const bounds = boundsToPersist({
      isMaximized: win.isMaximized(),
      isFullScreen: win.isFullScreen(),
      normalBounds: win.getNormalBounds(),
    })
    if (!bounds) return
    const stored = getStore()
    stored.set('windowBounds', bounds)
    stored.set('windowMaximized', win.isMaximized())
  }, 400)
}

/**
 * Remember the window geometry across restarts.
 *
 * `close` rather than `closed`: by the time `closed` fires the window is gone
 * and neither `getNormalBounds()` nor `isMaximized()` is meaningful. The flush
 * is synchronous (no debounce) so the last position is not lost when the timer
 * has not fired yet.
 */
function registerWindowGeometryPersistence(win: BrowserWindow): void {
  win.on('resize', () => scheduleGeometrySave(win))
  win.on('move', () => scheduleGeometrySave(win))
  win.on('maximize', () => scheduleGeometrySave(win))
  win.on('unmaximize', () => scheduleGeometrySave(win))
  win.on('close', () => {
    if (geometryTimer) {
      clearTimeout(geometryTimer)
      geometryTimer = null
    }
    const bounds = boundsToPersist({
      isMaximized: win.isMaximized(),
      isFullScreen: win.isFullScreen(),
      normalBounds: win.getNormalBounds(),
    })
    if (!bounds) return
    const stored = getStore()
    stored.set('windowBounds', bounds)
    stored.set('windowMaximized', win.isMaximized())
  })
}

/**
 * Navigate the main window to the server configured after startup settled.
 *
 * The window may already exist (created early by a second launch, a
 * notification click, or the startup path itself so the loading ring is up
 * immediately). At that point `serverUrl` can still name the PREVIOUS run's
 * address, so the navigation is deferred until `index.ts` has finished starting
 * the embedded server. Reading it here, once, is what keeps the page on the
 * port the new server actually uses — a page pinned to the old address can
 * never reconnect its WebSocket, which is the failure this fixes.
 *
 * No server at all means startup failed (the failure dialog has already
 * explained it) or this is a first run: the local login page is already behind
 * the overlay, so drop the overlay and leave the user there.
 */
function enterConfiguredServer(): void {
  if (!mainWindow || mainWindow.isDestroyed()) return
  // The user cancelled (or the connection deadline fired) while the server was
  // starting: the login page is already on screen and must not be replaced by
  // a late navigation.
  if (!splash?.isVisible()) return
  const url = getStore().get('serverUrl')
  if (!url) {
    dismissSplash()
    return
  }
  // Keep the ring across the navigation and start the connection deadline
  // HERE, not when the window was created: the deadline measures the page
  // load, and the embedded server's own startup has already been waited out.
  showSplashFor(url)
  mainWindow.loadURL(url)
  checkVersionGateFor(url)
}

export function createMainWindow(): BrowserWindow {
  // Idempotent: every caller means "make sure there is a window", and two of
  // them can now race — the startup path and a `second-instance` reveal that
  // arrives while the first copy is still booting. Building twice would leak the
  // first window (nothing holds it) and leave two app windows on screen.
  if (mainWindow && !mainWindow.isDestroyed()) return mainWindow
  const { bounds: startupBounds, maximized: startMaximized } = startupWindowBounds()
  mainWindow = new BrowserWindow({
    ...startupBounds,
    show: false,
    // Windows/Linux get a frameless window and draw their own controls in the
    // header (see windowChrome.ts). macOS keeps the native frame so its
    // top-left traffic lights stay where users expect them. A frameless window
    // is NOT draggable by default — the header supplies the drag region — and
    // is still resizable on Windows/Linux; on Wayland Electron gives frameless
    // windows GTK shadow plus an extended resize border.
    frame: !shouldUseFramelessWindow(process.platform),
    // Enforce the same floor `isValidBounds` applies when restoring. Without
    // it the window can be dragged smaller than the minimum, and then
    // `boundsToPersist` rejects the rect on close (returning null), so the
    // geometry is silently never saved — the next launch falls back to the
    // centred default and the user's size/position is lost with no error.
    minWidth: MIN_WINDOW_WIDTH,
    minHeight: MIN_WINDOW_HEIGHT,
    webPreferences: { preload: path.join(__dirname, '../preload/index.js'), contextIsolation: true, nodeIntegration: false },
  })
  // Maximize before the window is shown, so a maximized restore does not flash
  // the restored rect first.
  if (startMaximized) mainWindow.maximize()
  // A copy runs beside the original; the page title alone would leave two
  // identical entries in the task switcher. Appending the copy's name is the
  // only place the OS can see it — the page knows nothing about instances.
  const copyName = instanceLabel()
  if (copyName) {
    const win = mainWindow
    win.on('page-title-updated', (event, title) => {
      event.preventDefault()
      win.setTitle(title ? `${title} · ${copyName}` : copyName)
    })
  }
  registerWindowGeometryPersistence(mainWindow)
  registerContextMenu(mainWindow.webContents)
  registerKeyboardShortcuts(mainWindow.webContents)
  registerUnloadGuard(mainWindow)
  registerCloseInterception(mainWindow)
  registerWindowStateReporting(mainWindow)
  // After the window's own wiring, before anything can show it: a listener that
  // missed the first `show` would think the window is still hidden.
  notifyMainWindowCreated(mainWindow)
  // A (re)load tears down the renderer's listeners, so anything clicked before
  // it re-registers must be deferred rather than sent into the void.
  mainWindow.webContents.on('did-start-loading', () => markRendererLoading())

  // Native loading overlay, floating above the window's own page. Created before
  // the first navigation so a cold start can cover it too.
  splash?.destroy()
  splash = createSplashController(mainWindow, {
    onAbort: (reason) => {
      // Both paths land on the login page. A timeout also reports why, through
      // the same onConnectError hook the did-fail-load fallback uses — without
      // it the user would be returned to the login page with no explanation.
      void serveLoginOrExplain(reason)
    },
    // While the embedded server is starting, the window shows the local login
    // page behind the overlay. Its load events must not advance the ring or
    // arm the boot fail-safe against a page that is only a placeholder.
    isHeld: () => !isStartupSettled(),
  })

  const serverUrl = getStore().get('serverUrl')
  if (!isStartupSettled()) {
    // Startup is still bringing the embedded server up. Show the ring NOW and
    // navigate only once the server is ready: `serverUrl` may still name the
    // previous run's port, and a page pinned there can never reconnect.
    splash?.showLoading()
    mainWindow.loadFile(loginPagePath())
    void whenStartupSettled().then(() => enterConfiguredServer())
  } else if (serverUrl) {
    // Cold start with a saved server: same gap as connecting from the login
    // page, so the overlay covers it here too.
    // No certificate reset here: an accepted certificate is remembered per
    // origin and bound to its fingerprint (certificate.ts), so recreating the
    // window does not re-ask. A certificate that changed still prompts.
    showSplashFor(serverUrl)
    mainWindow.loadURL(serverUrl)
    // The version gate runs alongside the load; on a mismatch it replaces the
    // loading overlay with the blocking gate.
    checkVersionGateFor(serverUrl)
  } else {
    // First run: no server configured — show a built-in login page to enter the server URL.
    // No overlay: the login page is a local document with nothing to wait for.
    mainWindow.loadFile(loginPagePath())
  }

  // Ensure the window is always shown, even if the page fails to load (e.g. no
  // server at the configured URL), so the user is never left with a hidden window.
  const showWindow = () => { if (mainWindow && !mainWindow.isDestroyed()) mainWindow.show() }
  mainWindow.once('ready-to-show', showWindow)
  mainWindow.webContents.on('did-fail-load', (_e, errorCode, errorDesc, failedUrl, isMainFrame) => {
    showWindow()
    const loginUrl = pathToFileURL(loginPagePath()).toString()
    // Subframe failures, cancelled navigations and a failure of the login page
    // itself must not hijack the window — see shouldFallBackToLogin.
    if (!shouldFallBackToLogin({ errorCode, failedUrl, loginUrl, isMainFrame })) return
    // The server is unreachable, so the overlay's premise (something is
    // loading) no longer holds — drop it before showing the login page.
    dismissSplash()
    // And abandon any version-gate check for this navigation: a mismatch against
    // an unreachable server is not actionable, and its result must not cover the
    // login page the user is about to see.
    abortVersionGate()
    // Server page failed to load (unreachable) — fall back to the server-selection
    // login page so the user can pick another server instead of a blank page,
    // handing it the reason the same way Android does.
    void serveLoginOrExplain(errorDesc || '')
  })
  setTimeout(showWindow, 2000)

  // Open external web links in the user's default browser instead of navigating within the app window.
  mainWindow.webContents.on('will-navigate', (event, url) => {
    const disposition = classifyUrl(url, getStore().get('serverUrl'))
    if (disposition === 'external') {
      event.preventDefault()
      void shell.openExternal(url)
    } else if (disposition === 'block') {
      // Block file:, javascript:, data:, and unknown schemes rather than
      // letting the window navigate to (or the OS handle) arbitrary content.
      event.preventDefault()
    }
  })

  mainWindow.webContents.setWindowOpenHandler(({ url: target }) => {
    // One popup is the app's own: the member's shared screen in a window of its
    // own. It is `about:blank` on purpose (see isAppPopup) — a second document
    // in the same renderer, which is the only way it can receive the live
    // MediaStream. Everything else keeps the "never open a second window" rule.
    if (isAppPopup(target)) {
      return {
        action: 'allow',
        overrideBrowserWindowOptions: {
          width: 1024,
          height: 640,
          minWidth: 480,
          minHeight: 320,
          backgroundColor: '#000000',
          autoHideMenuBar: true,
          // The app window is frameless; a picture window must not inherit
          // that — without a title bar it cannot be moved or resized by hand.
          frame: true,
          titleBarStyle: 'default',
        },
      }
    }
    if (classifyUrl(target, getStore().get('serverUrl')) === 'external') {
      void shell.openExternal(target)
    }
    return { action: 'deny' }
  })
  // The one window that is allowed inherits THIS window's preload, so a page
  // that opens `about:blank` and then assigns `location` would run the whole
  // native bridge (server list, SSH password, file download) on a foreign
  // origin. The popup only ever needs its blank document — the picture is drawn
  // into it through Teleport from the opener — so it is pinned to that and may
  // not open windows of its own.
  mainWindow.webContents.on('did-create-window', (child) => {
    child.webContents.on('will-navigate', (event, url) => {
      if (shouldBlockPopupNavigation(url)) event.preventDefault()
    })
    child.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
  })
  mainWindow.on('closed', () => {
    splash?.destroy()
    splash = null
    mainWindow = null
  })
  return mainWindow
}

/** Navigate the main window back to the server-selection login page. */
export function showLoginPage(): void {
  if (mainWindow && !mainWindow.isDestroyed()) {
    // Reached from the settings "reconfigure server" action and from the
    // notification deep-link fallback. The overlay must come down, or it would
    // sit on top of the login page the user just asked for.
    dismissSplash()
    // A version gate is meaningless on the login page; drop it and invalidate
    // any in-flight check so it cannot reappear here.
    abortVersionGate()
    mainWindow.loadFile(loginPagePath())
  }
}

export function openSandboxWindow(port: number, protocol: string, _host: string, path: string): void {
  const win = new BrowserWindow({
    width: 1000, height: 720,
    webPreferences: { partition: `sandbox-${port}`, contextIsolation: true },
  })
  registerContextMenu(win.webContents)
  win.loadURL(`${protocol}://localhost:${port}${path || '/'}`)
}
