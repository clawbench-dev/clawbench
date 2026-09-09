/**
 * Drag-out of rendered Mermaid diagrams in file-preview markdown views.
 *
 * A rendered diagram is a `div.mermaid[data-mermaid]` holding the generated
 * `<svg>`, with `data-source-line` / `data-source-end` (the md code fence
 * range) on the container. Dragging the svg onto the chat column attaches the
 * diagram's markdown file as a LINE-RANGE reference (not a file path) — the
 * same composite-key attachment the header attach button performs.
 *
 * The internal `application/x-clawbench-attach` payload is extended with an
 * optional startLine/endLine; the chat drop handler (App.vue) attaches a range
 * when those are present. Only meaningful in file-preview contexts: the
 * container must sit under a `.markdown-body[data-file-path]` ancestor on a
 * non-share page. Chat-message mermaid diagrams and shares stay non-draggable.
 */

import { isShareMode } from '@/share/shareMode'
import { setAttachDragData, buildAttachDragImage, cleanupDragGhost } from '@/utils/attachDrag'
import { mermaidFenceEndLine } from '@/utils/mdMermaidAttach'

export interface MermaidDragHit {
  /** The rendered diagram container (div.mermaid). */
  container: HTMLElement
  /** Markdown file path from `.markdown-body[data-file-path]`. */
  path: string
  /** 1-based source line of the opening ```mermaid fence. */
  startLine: number
  /** 1-based source line of the closing fence (inclusive). */
  endLine: number
}

/**
 * Resolve a drag target to a file-preview mermaid diagram reference.
 * The drag starts on the diagram's inline `<svg>` (or any child element inside
 * div.mermaid). Returns null outside file previews (chat / share / export) or
 * when the diagram carries no source metadata.
 */
export function resolveMermaidDragTarget(e: DragEvent): MermaidDragHit | null {
  const target = e.target as HTMLElement | null
  if (!target) return null
  if (isShareMode()) return null
  const container = target.closest<HTMLElement>('div.mermaid[data-mermaid]')
  if (!container) return null
  // Only the diagram body itself (svg subtree) starts the drag — the header
  // buttons and expand icon should not.
  if (target.closest('.lightbox-expand-icon, .mermaid-block-header')) return null
  const mdBody = target.closest<HTMLElement>('.markdown-body[data-file-path]')
  const path = mdBody?.getAttribute('data-file-path') || ''
  if (!path) return null

  const startRaw = container.getAttribute('data-source-line')
  const startLine = parseInt(startRaw || '', 10)
  if (!Number.isFinite(startLine)) return null
  const endRaw = container.getAttribute('data-source-end')
  const endLine = endRaw
    ? parseInt(endRaw, 10)
    : mermaidFenceEndLine(startLine, container.dataset.mermaid || '')
  return { container, path, startLine, endLine }
}

/** Delegated dragstart: write the ranged attach payload + custom drag ghost. */
export function onMermaidDragStart(e: DragEvent): void {
  const hit = resolveMermaidDragTarget(e)
  if (!hit) return
  const dt = e.dataTransfer
  if (!dt) return
  e.stopPropagation()
  dt.effectAllowed = 'copy'
  const fileName = hit.path.split('/').pop() || hit.path
  setAttachDragData(dt, hit.path, false, hit.startLine, hit.endLine)
  const ghost = buildAttachDragImage(`${fileName}:${hit.startLine}-${hit.endLine}`, false)
  dt.setDragImage(ghost, 14, 16)
}

/** Delegated dragend: remove the custom ghost (must outlive dragstart). */
export function onMermaidDragEnd(_e: DragEvent): void {
  cleanupDragGhost()
}
