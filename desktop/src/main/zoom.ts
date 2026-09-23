/**
 * Zoom arithmetic for the desktop shell's native (Chromium) page zoom.
 *
 * Kept free of electron imports so the decision table is unit-testable, the
 * same way `shortcuts.ts` is.
 *
 * The shell owns the zoom level itself rather than letting Chromium apply the
 * Ctrl+Wheel gesture. Measured on Electron 44: `webContents` emits
 * `zoom-changed` for that gesture but does NOT change the zoom factor on its
 * own (the built-in behaviour is gone once the app has no menu bar), so
 * without a handler the wheel does nothing at all.
 *
 * `setZoomFactor` — not the CSS `zoom` used by the in-app "界面缩放" setting —
 * is the right primitive here: it is real layout zoom, so it changes
 * `devicePixelRatio` and shrinks `innerWidth`, which keeps `position: fixed`
 * and `getBoundingClientRect()` in one coordinate system. The CSS path has
 * known casualties (xterm mouse selection, mermaid wrapping) that this avoids.
 * The two are independent and compose.
 *
 * Chromium persists the level per origin across restarts (its `Preferences`
 * `per_host_zoom_levels`), so a chosen zoom would survive an app relaunch for
 * free — but only while the zoom mode stays `default`. Switching to `manual`
 * disables that persistence AND stops the factor from being applied at all, so
 * the mode is deliberately left alone.
 *
 * Note the persistence is not observable end to end while the appearance
 * "auto scale" feature is on (its default): that applier runs at startup and
 * recomputes a factor from the screen height, overwriting whatever Chromium
 * restored. Within a session the two compose — this module is the shared
 * primitive both go through, so they share one clamp and cannot disagree about
 * the range.
 */

/** Smallest allowed factor. Matches the in-app `applyUIScale` clamp. */
export const MIN_ZOOM = 0.5

/** Largest allowed factor. Matches the in-app `applyUIScale` clamp. */
export const MAX_ZOOM = 2.0

/**
 * Multiplier per zoom step. A geometric step (rather than a fixed increment)
 * makes zoom-out the exact inverse of zoom-in, so a round trip returns to the
 * starting factor instead of drifting.
 */
export const ZOOM_STEP = 1.1

/** A requested zoom change. `reset` returns to 100%. */
export type ZoomAction = 'in' | 'out' | 'reset'

/**
 * The part of Electron's WebContents this module needs.
 *
 * Declared structurally rather than importing `WebContents` so the module
 * stays free of electron imports and remains unit-testable. A real
 * WebContents satisfies it.
 *
 * Note it is the WebContents, NOT the BrowserWindow: `setZoomFactor` lives on
 * the contents (`BrowserWindow` has no such method), so an IPC handler must
 * pass `win.webContents`.
 */
export interface ZoomTarget {
  getZoomFactor(): number
  setZoomFactor(factor: number): void
}

/**
 * Apply a zoom factor to a window's contents, clamped to the supported range.
 *
 * Shared by both writers of the zoom level — the Ctrl+Wheel / Ctrl+= handler
 * here and the renderer-driven appearance "auto scale" IPC — so neither can
 * push the window outside the range the layout is expected to survive. A null
 * window (no window open yet) is ignored rather than throwing.
 */
export function applyZoomFactor(win: { webContents: ZoomTarget } | null, factor: number): void {
  if (!win) return
  win.webContents.setZoomFactor(clampZoomFactor(factor))
}

/** Clamp a zoom factor into the supported range, rejecting non-finite input. */
export function clampZoomFactor(factor: number): number {
  // A non-finite factor would otherwise propagate into setZoomFactor, which
  // throws on a factor that is not > 0.
  if (!Number.isFinite(factor)) return 1
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, factor))
}

/**
 * Resolve the factor a zoom action should produce.
 *
 * `current` is read back from the webContents rather than tracked here, so the
 * result stays correct even when something else changed the zoom in between
 * (a reload restoring a persisted level, for instance).
 */
export function nextZoomFactor(current: number, action: ZoomAction): number {
  if (action === 'reset') return 1
  const base = Number.isFinite(current) && current > 0 ? current : 1
  return clampZoomFactor(action === 'in' ? base * ZOOM_STEP : base / ZOOM_STEP)
}
