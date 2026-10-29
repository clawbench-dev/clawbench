/**
 * Shared window-geometry primitives.
 *
 * Pure functions over plain rects and work areas (DIPs), no Electron imports, so
 * both the main window's restore logic (`windowBoundsPolicy.ts`) and the floating
 * capsule (`floatingPolicy.ts`) can clamp against the same rules without either
 * one owning the other's vocabulary.
 */

export interface Size { width: number; height: number }

export interface Point { x: number; y: number }

export interface Rect { x: number; y: number; width: number; height: number }

/** A display work area, in DIPs. */
export interface WorkArea { x: number; y: number; width: number; height: number }

/** Below this a restored window is nonsense (a corrupt or hand-edited config). */
export const MIN_USABLE_WIDTH = 200
export const MIN_USABLE_HEIGHT = 150

/** The bounding box of every work area — the region windows may live in. */
export function unionOf(areas: WorkArea[]): WorkArea | null {
  if (areas.length === 0) return null
  const left = Math.min(...areas.map((a) => a.x))
  const top = Math.min(...areas.map((a) => a.y))
  const right = Math.max(...areas.map((a) => a.x + a.width))
  const bottom = Math.max(...areas.map((a) => a.y + a.height))
  return { x: left, y: top, width: right - left, height: bottom - top }
}

/**
 * Pull a rect back into one work area.
 *
 * Used when a stored geometry refers to a monitor that is no longer there: the
 * size is kept where it fits, the origin is clamped so at least some of the
 * window is on screen and grabbable.
 */
export function clampToWorkArea(rect: Rect, area: WorkArea): Rect {
  const width = Math.min(rect.width || area.width, area.width)
  const height = Math.min(rect.height || area.height, area.height)
  return {
    width: Math.round(width),
    height: Math.round(height),
    x: Math.round(Math.min(Math.max(rect.x, area.x), area.x + area.width - width)),
    y: Math.round(Math.min(Math.max(rect.y, area.y), area.y + area.height - height)),
  }
}

/**
 * Clamp a drag position against the UNION of every work area.
 *
 * Dragging must be able to cross monitors, so a movable surface is bounded by the
 * union rather than by the display it happens to sit on — the same rule that lets
 * a window be parked on a second screen without being trapped there.
 */
export function clampPositionToUnion(x: number, y: number, size: Size, areas: WorkArea[]): Point {
  const u = unionOf(areas)
  if (!u) return { x, y }
  return {
    x: Math.round(Math.min(Math.max(x, u.x), Math.max(u.x, u.x + u.width - size.width))),
    y: Math.round(Math.min(Math.max(y, u.y), Math.max(u.y, u.y + u.height - size.height))),
  }
}

/** Position a rect at the top-right corner of a work area, inset by `margin`. */
export function cornerBounds(area: WorkArea, size: Size, margin: number): Rect {
  return {
    x: Math.round(area.x + area.width - size.width - margin),
    y: Math.round(area.y + margin),
    width: size.width,
    height: size.height,
  }
}

/** Whether a stored rect is large enough to be a real window rather than garbage. */
export function isUsableSize(size: Size): boolean {
  return Number.isFinite(size.width) && Number.isFinite(size.height)
    && size.width >= MIN_USABLE_WIDTH && size.height >= MIN_USABLE_HEIGHT
}
