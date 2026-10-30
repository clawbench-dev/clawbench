/**
 * Ink board state: strokes + laser pointer, shared by the publisher's drawing
 * surface and every viewer's read-only overlay.
 *
 * Kept free of DOM so the state machine (start/extend/end/undo/clear, laser
 * auto-hide) is testable without a canvas.
 */
import { ref, shallowRef } from 'vue'
import type { ControlMessage, InkStroke } from './liveControl'
import { undoStroke } from './liveControl'

/** Laser hides itself after this long without an update. */
export const LASER_IDLE_MS = 2500

export interface LaserState {
  x: number
  y: number
  on: boolean
}

export function useInkBoard() {
  const strokes = shallowRef<InkStroke[]>([])
  const laser = ref<LaserState | null>(null)

  /** The stroke being drawn right now (null when the pen is up). */
  let active: InkStroke | null = null
  let laserTimer: ReturnType<typeof setTimeout> | null = null

  function hideLaser(): void {
    if (laser.value) laser.value = { ...laser.value, on: false }
    if (laserTimer) {
      clearTimeout(laserTimer)
      laserTimer = null
    }
  }

  function applyLaser(msg: Extract<ControlMessage, { t: 'laser' }>): void {
    if (!msg.on) {
      hideLaser()
      return
    }
    laser.value = { x: msg.x, y: msg.y, on: true }
    if (laserTimer) clearTimeout(laserTimer)
    // A pointer that stays on screen after the presenter stopped moving looks
    // like a frozen picture, so it fades on its own.
    laserTimer = setTimeout(() => {
      laserTimer = null
      if (laser.value) laser.value = { ...laser.value, on: false }
    }, LASER_IDLE_MS)
  }

  function beginStroke(point: { x: number; y: number }, color: string, width: number): void {
    active = { color, width, points: [point] }
    strokes.value = [...strokes.value, active]
  }

  function extendStroke(point: { x: number; y: number }): void {
    if (!active) return
    active.points = [...active.points, point]
    // Replace the array so the renderer re-runs; the stroke object itself is
    // mutated in place because it is already in the list.
    strokes.value = [...strokes.value]
  }

  function endStroke(): void {
    active = null
  }

  function undo(): void {
    active = null
    strokes.value = undoStroke(strokes.value)
  }

  function clear(): void {
    active = null
    strokes.value = []
  }

  /**
   * Applies a frame that arrived over the wire.
   *
   * `from` is ignored here: a viewer's stroke is drawn exactly like the
   * publisher's, because both are normalised to the shared picture.
   */
  function apply(msg: ControlMessage): boolean {
    switch (msg.t) {
      case 'laser':
        applyLaser(msg)
        return true
      case 'ink': {
        if (msg.phase === 'start') {
          beginStroke({ x: msg.x, y: msg.y }, msg.color, msg.width)
          return true
        }
        if (msg.phase === 'move') {
          // A move without a start (dropped frame, late join) still has to
          // appear — dropping it would leave a hole in the middle of a line.
          if (!active) beginStroke({ x: msg.x, y: msg.y }, msg.color, msg.width)
          else extendStroke({ x: msg.x, y: msg.y })
          return true
        }
        endStroke()
        return true
      }
      case 'ink-undo':
        undo()
        return true
      case 'ink-clear':
        clear()
        return true
      default:
        return false
    }
  }

  function dispose(): void {
    if (laserTimer) clearTimeout(laserTimer)
    laserTimer = null
  }

  return {
    strokes,
    laser,
    beginStroke,
    extendStroke,
    endStroke,
    undo,
    clear,
    apply,
    hideLaser,
    dispose,
  }
}
