/**
 * Internal drag-and-drop payload for attaching files/directories to chat.
 *
 * HTML5 DnD only carries a restricted set of types across the app, so we use
 * a custom MIME type holding a JSON payload. The file manager writes it on
 * dragstart; the chat column reads it on drop and attaches via useChatContext.
 * This deliberately does NOT put the item in dataTransfer.files, so the
 * file manager's own OS-file upload drop handler ignores internal drags.
 */

export const ATTACH_DRAG_MIME = 'application/x-clawbench-attach'

export interface AttachDragData {
  path: string
  isDir: boolean
}

/** Write the internal attach payload into a drag event's dataTransfer. */
export function setAttachDragData(dt: DataTransfer, path: string, isDir: boolean) {
  try {
    dt.setData(ATTACH_DRAG_MIME, JSON.stringify({ path, isDir } satisfies AttachDragData))
    dt.setData('text/plain', path)
  } catch {
    // dataTransfer may be unavailable in some synthetic events — ignore
  }
}

/** Read the internal attach payload, or null if this is not an internal drag. */
export function readAttachDragData(dt: DataTransfer | null | undefined): AttachDragData | null {
  if (!dt) return null
  try {
    const raw = dt.getData(ATTACH_DRAG_MIME)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (parsed && typeof parsed === 'object' && typeof (parsed as AttachDragData).path === 'string') {
      const data = parsed as AttachDragData
      return { path: data.path, isDir: data.isDir === true }
    }
  } catch {
    // malformed payload — treat as non-internal
  }
  return null
}

/** Whether a drag event carries our internal attach payload (used to gate dragover/drop). */
export function hasAttachDragData(dt: DataTransfer | null | undefined): boolean {
  if (!dt) return false
  try {
    return !!dt.types?.includes?.(ATTACH_DRAG_MIME)
  } catch {
    return false
  }
}

// ── Custom drag ghost ──────────────────────────────────────────────────────
// The OS-native drag image snapshots the source element, so a selected/accent
// item renders with a jarring gradient + tint over the browser's translucent
// ghost. We instead draw a flat, semi-transparent chip ourselves and hand it
// to setDragImage — a plain opacity look with no gradient.

export const ATTACH_DRAG_GHOST_FONT = '13px system-ui, sans-serif'
export const ATTACH_DRAG_GHOST_PAD_X = 12
export const ATTACH_DRAG_GHOST_ICON_W = 22
export const ATTACH_DRAG_GHOST_GAP = 7

export interface AttachDragImageSize {
  w: number
  h: number
}

/** Rough CJK-aware text width estimate so ghost sizing is stable and testable. */
export function estimateTextWidth(text: string): number {
  let width = 0
  for (const ch of text) {
    width += /[\u1100-\uFFFF]/.test(ch) ? 13 : 6.5
  }
  return width
}

/** Ghost chip dimensions for a given file name. */
export function computeAttachDragImageSize(name: string): AttachDragImageSize {
  const textW = estimateTextWidth(name)
  const w = Math.max(64, Math.ceil(textW + ATTACH_DRAG_GHOST_ICON_W + ATTACH_DRAG_GHOST_GAP + ATTACH_DRAG_GHOST_PAD_X * 2))
  return { w, h: 38 }
}

/** Convert a hex (#rgb/#rrggbb) or rgb()/rgba() color to an rgba() string with the given alpha. */
export function toRgba(color: string, alpha: number): string {
  const c = (color || '').trim()
  const hex = c.match(/^#?([0-9a-f]{6}|[0-9a-f]{3})$/i)
  if (hex) {
    let s = hex[1]
    if (s.length === 3) s = s.split('').map((ch) => ch + ch).join('')
    const n = parseInt(s, 16)
    return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`
  }
  const rgb = c.match(/rgba?\(([^)]+)\)/)
  if (rgb) {
    const parts = rgb[1].split(',').map((x) => parseFloat(x.trim()))
    return `rgba(${parts[0]}, ${parts[1]}, ${parts[2]}, ${alpha})`
  }
  return `rgba(74, 144, 217, ${alpha})`
}

/**
 * Draw a flat, semi-transparent drag ghost for an attach drag. Falls back to a
 * blank canvas (and thus the OS ghost) if 2D canvas is unavailable.
 */
export function buildAttachDragImage(name: string, isDir: boolean): HTMLCanvasElement {
  const { w, h } = computeAttachDragImageSize(name)
  const pad = 4
  const scale = 2
  const canvas = document.createElement('canvas')
  canvas.width = (w + pad * 2) * scale
  canvas.height = (h + pad * 2) * scale
  const ctx = canvas.getContext('2d')
  if (!ctx) return canvas

  ctx.scale(scale, scale)

  const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent-color').trim() || '#4a90d9'
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark'

  ctx.fillStyle = toRgba(accent, 0.28)
  roundRectPath(ctx, pad, pad, w, h, 8)
  ctx.fill()
  ctx.lineWidth = 1.5
  ctx.strokeStyle = toRgba(accent, 0.6)
  ctx.stroke()

  ctx.font = ATTACH_DRAG_GHOST_FONT
  ctx.textBaseline = 'middle'
  ctx.textAlign = 'left'
  ctx.fillStyle = isDark ? 'rgba(255, 255, 255, 0.92)' : 'rgba(0, 0, 0, 0.9)'
  ctx.fillText(isDir ? '📁' : '📄', pad + 8, pad + h / 2 + 1)
  ctx.fillText(name, pad + 8 + ATTACH_DRAG_GHOST_ICON_W + ATTACH_DRAG_GHOST_GAP, pad + h / 2 + 1)

  return canvas
}

function roundRectPath(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
  ctx.beginPath()
  ctx.moveTo(x + r, y)
  ctx.arcTo(x + w, y, x + w, y + h, r)
  ctx.arcTo(x + w, y + h, x, y + h, r)
  ctx.arcTo(x, y + h, x, y, r)
  ctx.arcTo(x, y, x + w, y, r)
  ctx.closePath()
}
