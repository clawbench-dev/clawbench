/**
 * Viewport coordinates in the same space as `getBoundingClientRect()`.
 *
 * Split out of `useSettingsConfig` so geometry-only call sites (popup menus,
 * context menus) do not pull the whole settings/i18n/API chain — importing that
 * from a component made every test that mounts it mock i18n's `createI18n`.
 *
 * Both functions are identity today: the app scales by resizing the root font,
 * which leaves `getBoundingClientRect()`, `innerWidth` and `position: fixed` in
 * one coordinate space. They are kept as named helpers because the conversion
 * used to be real (under `html { zoom }`) and callers read better with a name.
 */

/** @deprecated scaling no longer changes the coordinate space — use the value directly. */
export function toFixedCSS(viewportCoord: number): number {
  return viewportCoord
}

/** Viewport dimensions in the same coordinate space as `getBoundingClientRect()`. */
export function getZoomedViewport(): { width: number; height: number } {
  return {
    width: window.innerWidth,
    height: window.innerHeight,
  }
}
