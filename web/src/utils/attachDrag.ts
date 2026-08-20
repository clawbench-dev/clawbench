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
// ghost.  Canvas-based ghosts also fail in Chrome (shows a blank/noise square),
// so we use a real DOM element appended off-screen — the browser snapshots it
// reliably, then we remove it immediately after setDragImage.

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

/** Resolve the accent color from CSS variable, with a hard fallback. */
export function resolveAccentColor(): string {
  try {
    const v = getComputedStyle(document.documentElement).getPropertyValue('--accent-color').trim()
    if (v) return v
  } catch { /* ignore */ }
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark'
  return isDark ? '#5b9bd5' : '#4a90d9'
}

/**
 * Build a real DOM element to use as the drag ghost image.
 * Uses an off-screen div with accent background pill, icon badge, and bold
 * white text — then it's passed to setDragImage and immediately removed.
 *
 * Chrome cannot snapshot canvas content for setDragImage (renders as noise),
 * so we must use a real DOM element for reliable rendering.
 */
export function buildAttachDragImage(name: string, isDir: boolean): HTMLElement {
  const accent = resolveAccentColor()
  const el = document.createElement('div')
  el.setAttribute('data-attach-ghost', '')
  el.style.cssText = `
    position: fixed;
    top: -9999px;
    left: -9999px;
    display: inline-flex;
    align-items: center;
    gap: ${ATTACH_DRAG_GHOST_GAP}px;
    padding: 6px 14px;
    border-radius: 10px;
    background: ${accent};
    color: #fff;
    font: ${ATTACH_DRAG_GHOST_FONT};
    white-space: nowrap;
    pointer-events: none;
    box-shadow: 0 2px 8px rgba(0,0,0,0.18);
    z-index: -1;
  `

  // Icon badge
  const badge = document.createElement('span')
  badge.style.cssText = `
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border-radius: 50%;
    background: rgba(255,255,255,0.22);
    flex-shrink: 0;
  `

  // Use Lucide-style SVG icons (simple, no font dependency)
  badge.innerHTML = isDir ? folderSvg : fileSvg

  const label = document.createElement('span')
  label.textContent = name
  label.style.cssText = 'overflow: hidden; text-overflow: ellipsis;'

  el.appendChild(badge)
  el.appendChild(label)

  document.body.appendChild(el)
  return el
}

/** Clean up a drag ghost element previously created by buildAttachDragImage. */
export function removeAttachDragGhost(el: HTMLElement | null) {
  if (el && el.parentNode) el.parentNode.removeChild(el)
}

// ── Minimal inline SVGs for file/folder icons ─────────────────────────────
// Kept as raw strings so they render instantly with no font/icon dependency.

const folderSvg = `<svg width="14" height="12" viewBox="0 0 14 12" fill="none" stroke="rgba(255,255,255,0.9)" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round">
  <path d="M1 3V2a1 1 0 0 1 1-1h3l1.5 2H12a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1V3z"/>
</svg>`

const fileSvg = `<svg width="12" height="14" viewBox="0 0 12 14" fill="none" stroke="rgba(255,255,255,0.9)" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round">
  <path d="M1 1.5a1 1 0 0 1 1-1h5.5L11 4v8.5a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1v-12z"/>
  <path d="M7.5 0.5v3.5H11"/>
  <line x1="3.5" y1="7.5" x2="8.5" y2="7.5"/>
  <line x1="3.5" y1="9.5" x2="6.5" y2="9.5"/>
</svg>`
