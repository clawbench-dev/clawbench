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

export const ATTACH_DRAG_GHOST_FONT = 'bold 13px system-ui, sans-serif'
export const ATTACH_DRAG_GHOST_PAD_X = 14
export const ATTACH_DRAG_GHOST_ICON_W = 28
export const ATTACH_DRAG_GHOST_GAP = 8

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
  const w = Math.max(80, Math.ceil(textW + ATTACH_DRAG_GHOST_ICON_W + ATTACH_DRAG_GHOST_GAP + ATTACH_DRAG_GHOST_PAD_X * 2))
  return { w, h: 44 }
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
 * Draw a bold, vivid drag ghost for an attach drag. Uses a solid accent-filled
 * pill with a canvas-drawn folder/file icon and bold white text — stands out
 * clearly over both light and dark content.  Avoids emoji on canvas because
 * Chrome renders them as empty or □ glyphs in a 2D canvas context.
 * Falls back to a blank canvas (and thus the OS ghost) if 2D canvas is unavailable.
 */
export function buildAttachDragImage(name: string, isDir: boolean): HTMLCanvasElement {
  const { w, h } = computeAttachDragImageSize(name)
  const pad = 6
  const scale = 2
  const canvas = document.createElement('canvas')
  canvas.width = (w + pad * 2) * scale
  canvas.height = (h + pad * 2) * scale
  const ctx = canvas.getContext('2d')
  if (!ctx) return canvas

  ctx.scale(scale, scale)

  const accent = resolveAccentColor()
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark'

  // ── Drop shadow ──
  ctx.shadowColor = isDark ? 'rgba(0,0,0,0.45)' : 'rgba(0,0,0,0.18)'
  ctx.shadowBlur = 8
  ctx.shadowOffsetY = 2

  // ── Solid accent background pill ──
  ctx.fillStyle = accent
  roundRectPath(ctx, pad, pad, w, h, 10)
  ctx.fill()

  // ── Clear shadow for subsequent draws ──
  ctx.shadowColor = 'transparent'
  ctx.shadowBlur = 0
  ctx.shadowOffsetY = 0

  // ── Left icon badge (white circle with drawn icon) ──
  const badgeR = 12
  const badgeCx = pad + 16
  const badgeCy = pad + h / 2
  ctx.fillStyle = 'rgba(255, 255, 255, 0.25)'
  ctx.beginPath()
  ctx.arc(badgeCx, badgeCy, badgeR, 0, Math.PI * 2)
  ctx.fill()

  // Draw a simple folder or file glyph in white inside the badge
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.9)'
  ctx.fillStyle = 'rgba(255, 255, 255, 0.9)'
  ctx.lineWidth = 1.4
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'
  if (isDir) {
    drawFolderGlyph(ctx, badgeCx - 6, badgeCy - 5, 12, 10)
  } else {
    drawFileGlyph(ctx, badgeCx - 5, badgeCy - 6, 10, 12)
  }

  // ── File/dir name (white bold) ──
  ctx.font = ATTACH_DRAG_GHOST_FONT
  ctx.textBaseline = 'middle'
  ctx.textAlign = 'left'
  ctx.fillStyle = 'rgba(255, 255, 255, 0.95)'
  ctx.fillText(name, pad + 8 + ATTACH_DRAG_GHOST_ICON_W + ATTACH_DRAG_GHOST_GAP, pad + h / 2 + 1)

  return canvas
}

/** Resolve the accent color from CSS variable, with a hard fallback. */
function resolveAccentColor(): string {
  try {
    const v = getComputedStyle(document.documentElement).getPropertyValue('--accent-color').trim()
    if (v) return v
  } catch { /* ignore */ }
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark'
  return isDark ? '#5b9bd5' : '#4a90d9'
}

/**
 * Draw a simple folder glyph (tab + body) centred in the bounding box.
 * Uses only stroke/fill — no emoji, so it works in all browsers on canvas.
 */
function drawFolderGlyph(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number) {
  const tabW = w * 0.45
  const tabH = h * 0.25
  // Tab
  ctx.beginPath()
  ctx.moveTo(x, y + tabH)
  ctx.lineTo(x, y)
  ctx.lineTo(x + tabW, y)
  ctx.lineTo(x + tabW + 2, y + tabH)
  ctx.lineTo(x + w, y + tabH)
  ctx.stroke()
  // Body
  ctx.beginPath()
  ctx.moveTo(x, y + tabH)
  ctx.lineTo(x, y + h)
  ctx.lineTo(x + w, y + h)
  ctx.lineTo(x + w, y + tabH)
  ctx.closePath()
  ctx.fill()
}

/**
 * Draw a simple file glyph (page with folded corner) centred in the bounding box.
 * Uses only stroke/fill — no emoji, so it works in all browsers on canvas.
 */
function drawFileGlyph(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number) {
  const fold = Math.min(w, h) * 0.3
  // Page outline with dog-ear
  ctx.beginPath()
  ctx.moveTo(x, y)
  ctx.lineTo(x + w - fold, y)
  ctx.lineTo(x + w, y + fold)
  ctx.lineTo(x + w, y + h)
  ctx.lineTo(x, y + h)
  ctx.closePath()
  ctx.stroke()
  // Dog-ear fold
  ctx.beginPath()
  ctx.moveTo(x + w - fold, y)
  ctx.lineTo(x + w - fold, y + fold)
  ctx.lineTo(x + w, y + fold)
  ctx.stroke()
  // Two text lines
  const lineY1 = y + h * 0.5
  const lineY2 = y + h * 0.7
  const lineX1 = x + 2
  const lineX2 = x + w - 3
  ctx.lineWidth = 1
  ctx.beginPath()
  ctx.moveTo(lineX1, lineY1)
  ctx.lineTo(lineX2, lineY1)
  ctx.moveTo(lineX1, lineY2)
  ctx.lineTo(lineX2 * 0.6, lineY2)
  ctx.stroke()
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
