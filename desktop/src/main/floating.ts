import { app, BrowserWindow, Menu, Tray, ipcMain, nativeImage, screen } from 'electron'
import fs from 'node:fs'
import http from 'node:http'
import https from 'node:https'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { getStore } from './store'
import { getCookieHeaderForUrl, record, recordError } from './clientLog'
import { floatingCapsuleLabels, resolveNativeLanguage } from './labels'
import { createMainWindow, getMainWindow } from './window'
import {
  DEFAULT_HEIGHT,
  DEFAULT_WIDTH,
  clampPositionToUnion,
  clampToWorkArea,
  colorsChanged,
  computeStats,
  defaultBounds,
  isClickGesture,
  overviewUrl,
  releaseLooksLost,
  resolveColors,
  sanitizeColors,
  sanitizeStats,
  statsChanged,
  trayTooltip,
} from './floatingPolicy'
import type { FloatingStats, Rect, ResolvedColors } from './floatingPolicy'

/**
 * The always-on-top status capsule — the desktop counterpart of the Android
 * floating window (`FloatingStatusController` / `FloatingStatusView`).
 *
 * Layout of the module follows the shell's existing split: every decision that
 * can be made without Electron lives in `floatingPolicy.ts` and is unit-tested
 * there; what is left here is window plumbing, the overview poll, and IPC.
 *
 * Two things are deliberate and worth keeping if this is edited later:
 *
 * 1. **The numbers are computed natively from `/api/ai/sessions/overview`, not
 *    pushed by the web page.** That is how Android does it, and it means the
 *    desktop capsule cannot drift from the phone's, and the web UI needs no
 *    change to feed it.
 * 2. **Dragging is driven from the main process, not by `-webkit-app-region:
 *    drag`.** A drag region is handed to the OS as a title bar: right-click
 *    opens the Windows system menu instead of ours, and no DOM click ever
 *    reaches the page, which would kill "click to open the app". Measured on
 *    Electron 44 / Windows.
 */

const TAG = 'Floating'

/**
 * Baseline refresh interval.
 *
 * Runs while anything shows the counters — the capsule OR the tray icon — so a
 * hidden capsule still keeps its tooltip current.
 */
const POLL_MS = 15_000
/** How long to wait after an event before re-polling (coalesces a burst). */
const ACTIVITY_DEBOUNCE_MS = 1_200
/** Overview request deadline; a dead server must not leave a request hanging. */
const REQUEST_TIMEOUT_MS = 10_000
/** Cursor poll while dragging (~60Hz). */
const DRAG_TICK_MS = 16
/** A drag this long is treated as abandoned (release never arrived). */
const DRAG_MAX_MS = 15_000
/** A press whose release was lost is resolved after this much cursor stillness. */
const DRAG_STILL_MS = 2_000
/** Persist debounce: a drag fires `moved` continuously. */
const SAVE_DEBOUNCE_MS = 400
/**
 * How long `initFloating` waits for the main window's `show` before building
 * the capsule anyway.
 *
 * Only a fail-safe: the normal path is the `show` event. It must NOT resurrect
 * a capsule the user has since dismissed — see `startupIntentRevoked`.
 */
const STARTUP_SHOW_FALLBACK_MS = 4_000

/** Capsule window, or null when hidden. */
let win: BrowserWindow | null = null
let stats: FloatingStats = { running: 0, pending: 0, unread: 0 }
let colors: ResolvedColors = resolveColors(null)
/** Set once the capsule document has installed its `floating:state` listener. */
let pageReady = false
/**
 * Set when the user has explicitly hidden the capsule (or turned the feature
 * off) since the last time it was shown.
 *
 * Guards the startup fail-safe: `initFloating` schedules a delayed
 * `showCapsule()` in case the main window's `show` never arrives, and that
 * timer used to run unconditionally. A user who dismissed the capsule from the
 * tray within those few seconds got it back, with the tray already destroyed
 * and nothing on screen explaining why.
 */
let startupIntentRevoked = false
let saveTimer: NodeJS.Timeout | null = null
let pollTimer: NodeJS.Timeout | null = null
let activityTimer: NodeJS.Timeout | null = null
/** Tray icon; alive for as long as the capsule FEATURE is on. */
let tray: Tray | null = null
/** In-flight drag, or null when the pointer is up. */
let drag: {
  ox: number
  oy: number
  origin: { x: number; y: number }
  cursor: { x: number; y: number }
  stillSince: number
  moved: boolean
  since: number
  timer: NodeJS.Timeout
} | null = null

/**
 * Bumped per request so a slow response cannot overwrite a newer one.
 *
 * The Android controller guards the same race with a state version; here the
 * equivalent is "was anything else in flight while this one was open".
 */
let requestSeq = 0

/** The capsule document, shipped next to login.html (see electron-builder.yml). */
function floatingHtmlPath(): string {
  return path.join(process.resourcesPath, 'floating.html')
}

function preloadPath(): string {
  return path.join(__dirname, '../preload/floating.js')
}

function labels() {
  return floatingCapsuleLabels(resolveNativeLanguage())
}

function isAlive(): boolean {
  return !!win && !win.isDestroyed()
}

/** Whether the capsule is on screen (and therefore keeping the app alive). */
export function isFloatingAlive(): boolean {
  return isAlive() && win!.isVisible()
}

function currentColors(): ResolvedColors {
  return resolveColors(getStoreSafe().floatingColors)
}

/**
 * Read the persisted capsule preferences.
 *
 * Defensive on purpose: `initFloating` can be reached from a startup path where
 * `initStore()` has not run yet, and `getStore()` throws in that case — a throw
 * here would take the whole app down for a cosmetic feature.
 */
function getStoreSafe() {
  try {
    const s = getStore()
    return {
      serverUrl: s.get('serverUrl'),
      floatingEnabled: s.get('floatingEnabled'),
      floatingBounds: s.get('floatingBounds'),
      floatingStats: s.get('floatingStats'),
      floatingColors: s.get('floatingColors'),
    } as const
  } catch {
    return {
      serverUrl: '',
      floatingEnabled: false,
      floatingBounds: null,
      floatingStats: null,
      floatingColors: null,
    } as const
  }
}

function isEnabled(): boolean {
  return getStoreSafe().floatingEnabled === true
}

// ── geometry ────────────────────────────────────────────────────────────────

function workAreas() {
  return screen.getAllDisplays().map((d) => d.workArea)
}

function currentBounds(): Rect {
  const stored = getStoreSafe().floatingBounds
  if (stored && Number.isFinite(stored.x) && Number.isFinite(stored.y)) {
    const rect = {
      x: stored.x,
      y: stored.y,
      width: stored.width || DEFAULT_WIDTH,
      height: stored.height || DEFAULT_HEIGHT,
    }
    return clampToWorkArea(rect, screen.getDisplayMatching(rect).workArea)
  }
  return defaultBounds(screen.getPrimaryDisplay().workArea)
}

function persistBounds(): void {
  if (!isAlive()) return
  try {
    const b = win!.getBounds()
    getStore().set('floatingBounds', { x: b.x, y: b.y, width: b.width, height: b.height })
  } catch {
    /* store unavailable / window gone */
  }
}

function schedulePersistBounds(): void {
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    saveTimer = null
    persistBounds()
  }, SAVE_DEBOUNCE_MS)
}

/** Drop the pending debounce and write the position now (quit path). */
export function flushFloatingBounds(): void {
  if (saveTimer) {
    clearTimeout(saveTimer)
    saveTimer = null
  }
  persistBounds()
}

// ── state pushed into the capsule document ──────────────────────────────────

function statePayload() {
  // `lang` is carried separately: the labels table itself is locale-resolved
  // in the main process, and the document needs it for `dir`/font selection.
  return { stats, colors, labels: labels(), lang: resolveNativeLanguage(), logoUrl: logoUrl() }
}

/**
 * The app icon, shipped as a sibling resource of login.html.
 *
 * Absent (a dev tree that never ran electron-builder) the page falls back to its
 * own glyph rather than showing a broken image.
 */
function logoUrl(): string {
  try {
    const p = path.join(process.resourcesPath, 'logo.png')
    if (fs.existsSync(p)) return pathToFileURL(p).toString()
  } catch {
    /* ignore */
  }
  return ''
}

/**
 * Push the current state into the capsule document, and keep the tray in step.
 *
 * When the document has not announced readiness yet the send is simply skipped:
 * the `floating:ready` handler calls this again, and `did-finish-load` covers
 * the cached-document case. No queue is needed, because every field here is a
 * full snapshot rather than a delta.
 *
 * The tray is refreshed even with no window: it is the surface that outlives
 * the capsule, so its tooltip has to track the counters on its own.
 */
function pushState(): void {
  if (isAlive() && pageReady) {
    try {
      win!.webContents.send('floating:state', statePayload())
    } catch {
      /* destroyed mid-send */
    }
  }
  syncTray()
}

// ── overview poll ───────────────────────────────────────────────────────────

/**
 * GET the overview JSON with the browser session's cookies.
 *
 * The main process has no `document.cookie`; the auth cookie lives in Electron's
 * jar (see `clientLog.getCookieHeaderForUrl`), and the server answers 401
 * without it. `rejectUnauthorized: false` matches every other main-process
 * request to the user's own server (download.ts, clientLog.ts): a self-hosted
 * install commonly has a self-signed certificate, and the session cookie — not
 * the cert chain — is the credential.
 */
function fetchOverview(url: string, cookie: string | undefined): Promise<unknown | null> {
  return new Promise((resolve) => {
    const lib = url.startsWith('https:') ? https : http
    let settled = false
    const finish = (value: unknown | null): void => {
      if (settled) return
      settled = true
      resolve(value)
    }
    const req = lib.get(url, { headers: cookie ? { Cookie: cookie } : undefined, rejectUnauthorized: false }, (res) => {
      const status = res.statusCode ?? 0
      if (status < 200 || status >= 400) {
        res.resume()
        record('W', TAG, `overview responded ${status}`)
        finish(null)
        return
      }
      const chunks: Buffer[] = []
      let size = 0
      res.on('data', (c: Buffer) => {
        // A misbehaving endpoint must not be able to exhaust the shell's memory.
        size += c.length
        if (size > 2_000_000) {
          req.destroy()
          finish(null)
          return
        }
        chunks.push(c)
      })
      res.on('end', () => {
        try {
          finish(JSON.parse(Buffer.concat(chunks).toString('utf8')))
        } catch (err) {
          recordError(TAG, err)
          finish(null)
        }
      })
      res.on('error', () => finish(null))
    })
    req.setTimeout(REQUEST_TIMEOUT_MS, () => {
      req.destroy()
      finish(null)
    })
    req.on('error', () => finish(null))
  })
}

/**
 * Pull the overview and repaint if the counters moved.
 *
 * Never throws and keeps the previous numbers on any failure: a capsule that
 * blanks itself because the laptop just lost its VPN would be worse than one
 * showing slightly stale counts, which is also how the Android controller
 * behaves between refreshes.
 *
 * Runs whether or not the capsule window is up: the tray icon outlives it, and
 * its tooltip shows the same three counters, so a hidden capsule must not stop
 * the numbers moving (see `wantsPolling`).
 */
export async function refreshFloatingStats(reason = 'poll'): Promise<void> {
  const url = overviewUrl(getStoreSafe().serverUrl)
  if (!url) return
  const seq = ++requestSeq
  try {
    const cookie = await getCookieHeaderForUrl(url)
    const overview = await fetchOverview(url, cookie)
    // A newer request superseded this one.
    if (seq !== requestSeq || overview === null) return
    const next = computeStats(overview)
    if (!statsChanged(next, stats)) return
    stats = next
    record('I', TAG, `${reason}: running=${stats.running} pending=${stats.pending} unread=${stats.unread}`)
    try {
      getStore().set('floatingStats', stats)
    } catch {
      /* store unavailable */
    }
    pushState()
  } catch (err) {
    recordError(TAG, err)
  }
}

/**
 * Something happened that is likely to change the counters.
 *
 * Android refreshes on its own WebSocket events; the desktop shell has no native
 * event channel (the page owns the socket), so the web app reports its own
 * terminal events here. Debounced, because a finishing batch fires one event per
 * session.
 *
 * Deliberately NOT hung off `native:notify`: that call is gated by the
 * "desktop notifications" setting and by page focus (see
 * `useNotification.showBrowserNotification`), so a user who turned notifications
 * off — or who was looking at the window — would leave the capsule on the 15s
 * poll alone. The counters are a different concern from the popup.
 */
export function noteFloatingActivity(): void {
  if (!wantsPolling()) return
  if (activityTimer) clearTimeout(activityTimer)
  activityTimer = setTimeout(() => {
    activityTimer = null
    void refreshFloatingStats('activity')
  }, ACTIVITY_DEBOUNCE_MS)
}

/**
 * Whether anything on screen still shows the counters.
 *
 * True while the capsule is visible OR a tray icon exists. Only when both are
 * gone does polling stop — that is the state where nothing would repaint.
 */
function wantsPolling(): boolean {
  return isFloatingAlive() || !!tray
}

function startPolling(): void {
  if (pollTimer) return
  pollTimer = setInterval(() => void refreshFloatingStats('poll'), POLL_MS)
  void refreshFloatingStats('open')
}

/**
 * Stop polling only if nothing is left to update.
 *
 * Called on capsule teardown, which is NOT the same as "the feature is off":
 * the tray usually survives, and its tooltip is the only remaining surface.
 */
function stopPollingIfIdle(): void {
  if (wantsPolling()) return
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = null
  if (activityTimer) clearTimeout(activityTimer)
  activityTimer = null
}

// ── window lifecycle ────────────────────────────────────────────────────────

function createWindow(): BrowserWindow | null {
  if (isAlive()) return win
  const b = currentBounds()
  win = new BrowserWindow({
    ...b,
    useContentSize: true,
    show: false,
    // A capsule, not a window: no frame, no taskbar entry, no resize grip.
    frame: false,
    transparent: true,
    resizable: false,
    movable: true,
    minimizable: false,
    maximizable: false,
    fullscreenable: false,
    skipTaskbar: true,
    hasShadow: false,
    alwaysOnTop: true,
    webPreferences: {
      preload: preloadPath(),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      partition: 'clawbench-floating',
    },
  })
  win.setMenuBarVisibility(false)
  // The level is re-applied explicitly: 'screen-saver' outranks other topmost
  // windows, which is what a status capsule is for.
  win.setAlwaysOnTop(true, 'screen-saver', 1)
  win.setResizable(false)
  if (process.platform === 'darwin') {
    win.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true })
  }
  win.on('moved', schedulePersistBounds)
  win.on('resize', schedulePersistBounds)
  // Losing the window mid-drag (an alt-tab, a modal stealing focus) must not
  // leave the capsule glued to the cursor.
  win.on('blur', () => clearDrag())
  win.webContents.on('did-finish-load', () => {
    // The document exists, but its listener may not have run yet; the capsule
    // announces readiness over `floating:ready`. Pushing here covers the case
    // where that handshake lands first (a cached document).
    pushState()
  })
  win.webContents.on('destroyed', () => {
    pageReady = false
    win = null
  })
  void win.loadFile(floatingHtmlPath()).catch((err) => record('E', TAG, `capsule failed to load: ${String(err)}`))
  startPolling()
  return win
}

function destroyWindow(): void {
  clearDrag()
  if (!isAlive()) {
    pageReady = false
    stopPollingIfIdle()
    return
  }
  persistBounds()
  pageReady = false
  const w = win
  win = null
  try {
    w!.destroy()
  } catch {
    /* already gone */
  }
  // After the teardown, so the tray (if it survives) is what decides whether
  // the counters still need a source.
  stopPollingIfIdle()
}

/** The tray icon for the capsule's feature. */
function trayImage(): Electron.NativeImage {
  try {
    const img = nativeImage.createFromPath(path.join(process.resourcesPath, 'logo.png'))
    if (!img.isEmpty()) return img.resize({ width: 16, height: 16 })
  } catch {
    /* icon missing */
  }
  return nativeImage.createEmpty()
}

function trayMenu(): Menu {
  const t = labels()
  const capsuleOn = isFloatingAlive()
  const main = getMainWindow()
  const mainOnScreen = !!main && !main.isDestroyed() && main.isVisible()
  return Menu.buildFromTemplate([
    { label: capsuleOn ? t.hideFloating : t.showFloating, click: () => toggleCapsule() },
    { label: t.openMain, enabled: !mainOnScreen, click: () => openMainWindow() },
    // `enabled` is evaluated when the menu is BUILT, but the click can land
    // seconds later, after the window is gone. `hideMainWindow` re-checks.
    { label: t.hideMain, enabled: mainOnScreen, click: () => hideMainWindow() },
    { type: 'separator' },
    { label: t.resetPosition, click: () => resetCapsulePosition() },
    { type: 'separator' },
    { label: t.disableFloating, click: () => applyFloatingEnabled(false) },
    { label: t.quit, click: () => app.quit() },
  ])
}

/**
 * Create the tray icon, or refresh it if it already exists.
 *
 * The tray lives as long as the FEATURE does, not as long as the capsule window
 * is visible. Without that, hiding the capsule would leave no way back: no
 * window, no tray, and the preference only reachable by editing the config.
 */
function ensureTray(): void {
  if (tray) {
    syncTray()
    return
  }
  try {
    tray = new Tray(trayImage())
    // Left click toggles the capsule — the same gesture the capsule's own body
    // uses, so "hide" and "bring back" read as one control in two places.
    tray.on('click', () => toggleCapsule())
    tray.on('double-click', () => openMainWindow())
    syncTray()
    record('I', TAG, 'tray icon created')
  } catch (err) {
    tray = null
    record('E', TAG, `tray unavailable: ${String(err)}`)
  }
}

function destroyTray(): void {
  if (!tray) return
  try {
    tray.destroy()
  } catch {
    /* already gone */
  }
  tray = null
}

/** Keep the tooltip and the menu in step with the counters and window state. */
function syncTray(): void {
  if (!tray) return
  try {
    tray.setToolTip(trayTooltip(stats, labels()))
    tray.setContextMenu(trayMenu())
  } catch {
    /* tray gone */
  }
}

/** Whether a tray icon exists — which is what keeps the app alive window-less. */
export function hasFloatingTray(): boolean {
  return !!tray
}

/** Show the capsule window without touching the feature flag. */
function showCapsule(): void {
  // An explicit show clears the "the user dismissed this" marker, so a later
  // startup fail-safe cannot mistake a deliberate show for a stale one.
  startupIntentRevoked = false
  const main = getMainWindow()
  const mainWasVisible = !!main && !main.isDestroyed() && main.isVisible()
  const w = createWindow()
  if (!w || w.isDestroyed()) return
  pushState()
  w.showInactive()
  // Measured on 0.127.0: creating and showing the capsule immediately after the
  // main window has been shown can leave that window hidden (801x488,
  // IsWindowVisible=false) even though nothing asked it to hide — a race between
  // the two windows' show sequences, and the capsule's z-order/focus juggling
  // makes it intermittent. Re-assert the main window only when it WAS visible a
  // moment ago, so a window the user deliberately hid stays hidden.
  if (mainWasVisible && main && !main.isDestroyed() && !main.isVisible()) {
    record('W', TAG, 'showing the capsule hid the main window — restoring it')
    main.show()
  }
  syncTray()
}

/** Hide the capsule window; the tray remains and is the way back. */
function hideCapsule(): void {
  // Remember that this was the user's call, not a transient state. The startup
  // fail-safe in `initFloating` consults this before resurrecting the capsule.
  startupIntentRevoked = true
  destroyWindow()
  syncTray()
}

function toggleCapsule(): void {
  if (isFloatingAlive()) hideCapsule()
  else showCapsule()
}

export function applyFloatingEnabled(enabled: boolean): void {
  const on = !!enabled
  try {
    getStore().set('floatingEnabled', on)
  } catch {
    /* store unavailable */
  }
  if (on) {
    ensureTray()
    showCapsule()
  } else {
    // Turning the FEATURE off removes the tray too, so nothing would be left to
    // click: make sure the main window is on screen first.
    //
    // `startupIntentRevoked` is set before the teardown so the startup
    // fail-safe cannot re-show a capsule the user just turned off.
    startupIntentRevoked = true
    destroyTray()
    const w = getMainWindow()
    if (!w || w.isDestroyed()) {
      createMainWindow()
    } else if (!w.isVisible()) {
      if (w.isMinimized()) w.restore()
      w.show()
    }
    hideCapsule()
  }
  record('I', TAG, `capsule feature ${on ? 'on' : 'off'}`)
  // The web app owns the settings row; tell it what actually happened so a
  // switch flipped from the tray menu does not leave the UI out of date.
  notifyEnabledChanged(on)
}

/**
 * Tell the renderer that the feature flag changed outside its own settings row.
 *
 * The tray and the capsule's own context menu can both turn the feature off,
 * which the web app has no way of observing (it only writes). Pushing the
 * authoritative value back keeps the settings switch from claiming the capsule
 * is on when the tray just removed it.
 */
function notifyEnabledChanged(enabled: boolean): void {
  const w = getMainWindow()
  if (!w || w.isDestroyed()) return
  try {
    w.webContents.send('clawbench-floating-enabled-changed', enabled)
  } catch {
    /* renderer gone */
  }
}

/**
 * Palette pushed by the web UI through the existing `setTheme` bridge.
 *
 * No new web-side call is needed for theming: the four colour arguments Android
 * persists for its capsule are the same ones, and the status dots stay fixed
 * semantic colors on both platforms.
 */
export function updateFloatingColors(raw: unknown): void {
  const next = sanitizeColors(raw)
  if (!next) return
  const stored = getStoreSafe().floatingColors
  const merged = resolveColors({ ...(stored ?? {}), ...next })
  if (!colorsChanged(merged, colors)) return
  colors = merged
  try {
    getStore().set('floatingColors', merged)
  } catch {
    /* store unavailable */
  }
  pushState()
}

/**
 * Counters supplied by the page. Only used by tests and the dev hook.
 *
 * Routed through `sanitizeStats` so the dev hook cannot display a value the
 * polled path would have rejected (this used to clamp on its own, without the
 * `MAX_COUNT` ceiling).
 */
export function setFloatingStatsOverride(raw: unknown): void {
  const next = sanitizeStats(raw)
  if (!statsChanged(next, stats)) return
  stats = next
  pushState()
}

// ── click vs drag ───────────────────────────────────────────────────────────

/**
 * Begin following the OS cursor with the capsule under the grab point.
 *
 * The cursor is authoritative rather than the renderer's mousemove stream: the
 * capsule is small, so a fast flick would otherwise outrun its own viewport and
 * the pointer would leave the window mid-drag.
 */
function startDrag(offsetX: number, offsetY: number): void {
  if (!isAlive()) return
  clearDrag()
  const ox = Number.isFinite(offsetX) ? offsetX : 0
  const oy = Number.isFinite(offsetY) ? offsetY : 0
  const cursor = screen.getCursorScreenPoint()
  drag = {
    ox,
    oy,
    origin: { x: cursor.x, y: cursor.y },
    cursor: { x: cursor.x, y: cursor.y },
    stillSince: Date.now(),
    moved: false,
    since: Date.now(),
    timer: setInterval(dragTick, DRAG_TICK_MS),
  }
  dragTick()
}

function dragTick(): void {
  if (!drag || !isAlive()) {
    clearDrag()
    return
  }
  const now = Date.now()
  if (now - drag.since > DRAG_MAX_MS) {
    record('W', TAG, 'drag timed out (no drag-end) — releasing the capsule')
    clearDrag()
    return
  }
  const p = screen.getCursorScreenPoint()
  if (p.x !== drag.cursor.x || p.y !== drag.cursor.y) {
    drag.cursor = { x: p.x, y: p.y }
    drag.stillSince = now
  } else if (releaseLooksLost(now, drag.stillSince, DRAG_STILL_MS)) {
    // The release never arrived — the pointer left the capsule and the page lost
    // its mouseup. Resolve the gesture rather than dragging the capsule around
    // the desktop for the rest of the session.
    record('W', TAG, 'drag release lost — resolving after the cursor went still')
    endDrag()
    return
  }
  if (!drag.moved && !isClickGesture(drag.origin, p)) drag.moved = true
  if (!drag.moved) return
  const b = win!.getBounds()
  const t = clampPositionToUnion(p.x - drag.ox, p.y - drag.oy, { width: b.width, height: b.height }, workAreas())
  win!.setPosition(t.x, t.y, false)
}

/** Drop drag state without acting on it (window gone, focus lost, stale press). */
function clearDrag(): void {
  if (drag?.timer) clearInterval(drag.timer)
  drag = null
}

/** Release: a press that never moved is a click, and a click toggles the app window. */
function endDrag(): void {
  if (!drag) return
  const d = drag
  drag = null
  if (d.timer) clearInterval(d.timer)
  if (isClickGesture(d.origin, d.cursor)) toggleMainWindow()
  else persistBounds()
}

/**
 * Show the app, or get it out of the way.
 *
 * Android's bubble only ever opens the app on tap. On the desktop the same click
 * is asked for in the other direction too — one control for "hide this window",
 * with the capsule left behind as the way back — so it toggles. A missing window
 * is created rather than being a dead click.
 */
function toggleMainWindow(): void {
  const w = getMainWindow()
  if (!w || w.isDestroyed()) {
    createMainWindow()
    return
  }
  if (!w.isMinimized() && w.isVisible()) {
    w.hide()
    return
  }
  if (w.isMinimized()) w.restore()
  if (!w.isVisible()) w.show()
  w.focus()
}

/**
 * Bring the app forward, creating the main window if the user had closed it.
 *
 * Mirrors `notification.revealWindow()`: `focus()` alone is a no-op on a
 * minimized window, and the whole point of the click is "take me to the thing
 * that needs me".
 */
function openMainWindow(): void {
  const w = getMainWindow()
  if (!w || w.isDestroyed()) {
    createMainWindow()
    return
  }
  if (w.isMinimized()) w.restore()
  if (!w.isVisible()) w.show()
  w.focus()
}

function resetCapsulePosition(): void {
  if (!isAlive()) return
  const b = defaultBounds(screen.getPrimaryDisplay().workArea)
  win!.setBounds(b)
  try {
    getStore().set('floatingBounds', b)
  } catch {
    /* store unavailable */
  }
}

/**
 * Hide the main window, if it is still there.
 *
 * The menus compute `enabled` when they are BUILT, but a click can land seconds
 * later — after the user closed the window some other way. `main!.hide()` would
 * then throw on a destroyed window.
 */
function hideMainWindow(): void {
  const w = getMainWindow()
  if (!w || w.isDestroyed()) return
  w.hide()
}

function popupMenu(): void {
  if (!isAlive()) return
  const t = labels()
  const main = getMainWindow()
  const mainOnScreen = !!main && !main.isDestroyed() && main.isVisible()
  const menu = Menu.buildFromTemplate([
    { label: t.openMain, enabled: !mainOnScreen, click: () => openMainWindow() },
    // Hiding (not minimizing) is what the request asks for: the window leaves
    // the screen AND Alt-Tab, and the capsule is the way back — which is exactly
    // why hiding the capsule below refuses to strand a hidden window.
    { label: t.hideMain, enabled: mainOnScreen, click: () => hideMainWindow() },
    { label: t.resetPosition, click: () => resetCapsulePosition() },
    { type: 'separator' },
    { label: t.hide, click: () => hideCapsule() },
    { label: t.quit, click: () => app.quit() },
  ])
  menu.popup({ window: win! })
}

// ── IPC ─────────────────────────────────────────────────────────────────────

/** Registered from `bridge.ts` so every native channel is installed in one place. */
export function registerFloatingIpc(): void {
  // ── from the main window's renderer (the web app) ──
  ipcMain.on('native:floating-enabled', (_e, enabled) => applyFloatingEnabled(enabled === true))
  ipcMain.on('native:floating-theme', (_e, colorsRaw) => updateFloatingColors(colorsRaw))
  // Dev/test hook: lets a page (or the console) drive the capsule without a
  // server. The poll is the production source of these numbers.
  ipcMain.on('native:floating-stats', (_e, raw) => setFloatingStatsOverride(raw))
  // "Session state probably changed" — the web app reports its own terminal WS
  // events here.
  //
  // A dedicated channel rather than a side effect of `native:notify`: that call
  // is gated by the desktopNotification setting and by page focus, so a user
  // with notifications off (or who was looking at the window) never produced
  // one, leaving the capsule on the 15s poll alone. The counters and the popup
  // are separate concerns.
  ipcMain.on('native:floating-activity', () => noteFloatingActivity())
  // Answers "is the FEATURE on", which is what the settings row renders. The
  // capsule's visibility is a separate question: a user who hid the capsule
  // from the tray still has the feature on, and the tray is their way back.
  ipcMain.on('native:floating-is-enabled', (e) => {
    e.returnValue = isEnabled()
  })

  // ── from the capsule itself (whitelisted in preload/floating.ts) ──
  //
  // Every one of these is sender-checked. The capsule shares an IPC namespace
  // with the main window, so without the check any renderer that can reach
  // `ipcRenderer` could quit the app (`floating:quit`) or hide the capsule.
  // Today the main window's preload exposes no raw send, but that is a property
  // of the preload, not of these handlers.
  const fromCapsule = (e: Electron.IpcMainEvent | Electron.IpcMainInvokeEvent): boolean =>
    isAlive() && e.sender === win!.webContents

  ipcMain.on('floating:ready', (e) => {
    if (!fromCapsule(e)) return
    pageReady = true
    pushState()
  })
  ipcMain.on('floating:activate', (e) => { if (fromCapsule(e)) openMainWindow() })
  ipcMain.on('floating:context', (e) => { if (fromCapsule(e)) popupMenu() })
  ipcMain.on('floating:hide', (e) => { if (fromCapsule(e)) hideCapsule() })
  ipcMain.on('floating:quit', (e) => { if (fromCapsule(e)) app.quit() })
  ipcMain.on('floating:drag-start', (e, x, y) => {
    if (fromCapsule(e)) startDrag(Number(x), Number(y))
  })
  ipcMain.on('floating:drag-end', (e) => {
    if (fromCapsule(e)) endDrag()
  })
  // The capsule knows its natural size (font, language, counter widths); the
  // main process only clamps it back onto a display.
  ipcMain.handle('floating:resize', (e, width, height) => {
    if (!fromCapsule(e)) return false
    const w = Math.max(120, Math.min(520, Math.round(Number(width) || DEFAULT_WIDTH)))
    const h = Math.max(40, Math.min(200, Math.round(Number(height) || DEFAULT_HEIGHT)))
    const b = win!.getBounds()
    win!.setBounds(clampToWorkArea({ ...b, width: w, height: h }, screen.getDisplayMatching(b).workArea))
    schedulePersistBounds()
    return true
  })
}

/** Arm the capsule at startup when the preference says it should already be up. */
export function initFloating(): void {
  colors = currentColors()
  const last = getStoreSafe().floatingStats
  if (last) {
    // Show the last known counts immediately rather than 0/0/0 while the first
    // request is in flight; the poll corrects them a moment later.
    stats = { ...stats, ...last }
  }
  if (isEnabled()) {
    ensureTray()
    // Do not build the capsule before the main window has been shown: creating it
    // from initFloating (which runs right after createMainWindow, while that
    // window is still `show: false`) was observed on 0.127.0 to leave the main
    // window hidden for the session. Waiting for 'show' keeps that sequence
    // intact; the timeout is a fail-safe so a page that never loads cannot leave
    // the feature silently off.
    const main = getMainWindow()
    if (!main || main.isDestroyed() || main.isVisible()) {
      showCapsule()
    } else {
      main.once('show', () => {
        if (startupIntentRevoked) return
        showCapsule()
      })
      setTimeout(() => {
        // The fail-safe must not resurrect a capsule the user dismissed while
        // this timer was pending (tray "hide" / "turn off" both land here), nor
        // one whose feature flag has since been turned off.
        if (startupIntentRevoked || !isEnabled()) return
        if (!isFloatingAlive()) showCapsule()
      }, STARTUP_SHOW_FALLBACK_MS)
    }
  }
  // A monitor unplugged or a resolution change can strand the capsule off-screen.
  screen.on('display-metrics-changed', () => {
    if (!isAlive()) return
    const b = win!.getBounds()
    win!.setBounds(clampToWorkArea(b, screen.getDisplayMatching(b).workArea))
  })
}
