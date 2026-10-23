/**
 * Main-window geometry memory — the pure half.
 *
 * Kept free of any `electron` import so the decision logic (is this saved rect
 * still usable? what do we persist while maximized?) can be unit-tested. The
 * window module supplies the real display list and does the actual
 * get/setBounds calls.
 */

export interface WindowBounds {
  x: number
  y: number
  width: number
  height: number
}

export interface DisplayLike {
  bounds: WindowBounds
  workArea: WindowBounds
}

/** Used when there is nothing usable stored — matches the historical default. */
export const DEFAULT_WINDOW_BOUNDS: WindowBounds = { x: 0, y: 0, width: 1280, height: 800 }

export const MIN_WINDOW_WIDTH = 640
export const MIN_WINDOW_HEIGHT = 480

/**
 * How much of the saved rect must be inside a display's work area for it to be
 * considered reachable. A window saved on a monitor that has since been
 * unplugged would otherwise restore off-screen — invisible, and with no
 * affordance to bring it back.
 *
 * 64px is the smallest drag handle we could expect a user to grab: enough that
 * a sliver of the title bar remains on screen even if the rest hangs off.
 */
export const MIN_VISIBLE_PX = 64

function isFiniteNumber(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v)
}

/** Whether `b` looks like a rect we could hand to setBounds. */
export function isValidBounds(b: unknown): b is WindowBounds {
  if (!b || typeof b !== 'object') return false
  const r = b as Partial<WindowBounds>
  return (
    isFiniteNumber(r.x) &&
    isFiniteNumber(r.y) &&
    isFiniteNumber(r.width) &&
    isFiniteNumber(r.height) &&
    r.width >= MIN_WINDOW_WIDTH &&
    r.height >= MIN_WINDOW_HEIGHT
  )
}

/** Whether any part of `bounds` (beyond MIN_VISIBLE_PX) overlaps `area`. */
function overlapsEnough(bounds: WindowBounds, area: WindowBounds): boolean {
  const overlapX = Math.min(bounds.x + bounds.width, area.x + area.width) - Math.max(bounds.x, area.x)
  const overlapY = Math.min(bounds.y + bounds.height, area.y + area.height) - Math.max(bounds.y, area.y)
  return overlapX >= MIN_VISIBLE_PX && overlapY >= MIN_VISIBLE_PX
}

/**
 * Pick the bounds to open with.
 *
 * Order: a saved rect that is still reachable on some connected display →
 * centred default on the primary display → the bare default. Centring rather
 * than reusing {0,0} matters because {0,0} is the top-left corner of the
 * primary display, which on a multi-monitor setup can be the far edge of a
 * screen the user is not looking at.
 */
export function resolveStartupBounds(
  saved: unknown,
  displays: readonly DisplayLike[],
  primary: DisplayLike | null,
): WindowBounds {
  if (isValidBounds(saved) && displays.some((d) => overlapsEnough(saved, d.workArea))) {
    // Copy: the caller may hand us the store's own object, and setBounds
    // mutating it in place would write through to persisted state.
    return { x: saved.x, y: saved.y, width: saved.width, height: saved.height }
  }
  if (!primary) return { ...DEFAULT_WINDOW_BOUNDS }
  const area = primary.workArea
  const width = Math.min(DEFAULT_WINDOW_BOUNDS.width, area.width)
  const height = Math.min(DEFAULT_WINDOW_BOUNDS.height, area.height)
  return {
    x: Math.round(area.x + (area.width - width) / 2),
    y: Math.round(area.y + (area.height - height) / 2),
    width,
    height,
  }
}

/**
 * The rect to persist for a window in its current state.
 *
 * When maximized/fullscreen we must store the RESTORED geometry, not the
 * current one — saving the maximized rect would make "un-maximize" after a
 * restart restore to a full-screen-sized window (and then `maximize()` again on
 * top, so the user could never get their old size back).
 */
export function boundsToPersist(input: {
  isMaximized: boolean
  isFullScreen: boolean
  normalBounds: WindowBounds
}): WindowBounds | null {
  if (input.isFullScreen) return null
  if (!isValidBounds(input.normalBounds)) return null
  const b = input.normalBounds
  return { x: b.x, y: b.y, width: b.width, height: b.height }
}
