/**
 * Drag-out of embedded images in rendered markdown views.
 *
 * Rendered markdown (MarkdownPreview.vue / MarkdownPreviewBody.vue) shows local
 * images as plain <img class="lightbox-img">. The shared file-preview pipeline
 * stamps each local image with `data-attach-src` = its DECODED project-relative
 * path (resolved against the markdown file's directory), so a user can drag the
 * image onto the chat column and attach it — reusing the same internal
 * `application/x-clawbench-attach` payload that file-manager entries use.
 *
 * Both markdown containers attach one delegated @dragstart/@dragend handler
 * pair from here; nothing per-image is needed.
 */

import { setAttachDragData, buildAttachDragImage, cleanupDragGhost } from '@/utils/attachDrag'

/** Whether the image src is served by one of our local file endpoints. */
export function isApiServedSrc(img: HTMLImageElement): boolean {
  const src = img.getAttribute('src') || ''
  return /^\/api\/local-file\//.test(src) || /^\/api\/file\//.test(src)
}

/**
 * Resolve the drag target to a local image and its attachable file path.
 *
 * Priority: the pipeline's decoded `data-attach-src`. Fallback: reverse-derive
 * the path from a `/api/local-file/<rel>` src (covers HTML produced before the
 * attribute existed, e.g. cached renders / static exports; thumbnail srcs
 * `/api/file/…` are query-param based and cannot be reverse-derived). Returns
 * null for anything that has no local-file semantics (external URLs, data:
 * URIs, plain DOM nodes).
 */
export function resolveMdImageDragTarget(e: DragEvent): { img: HTMLImageElement; path: string } | null {
  const target = e.target
  if (!(target instanceof HTMLImageElement)) return null

  const attachSrc = target.getAttribute('data-attach-src')
  if (attachSrc) return { img: target, path: attachSrc }

  // Fallback: only served local-file srcs are safe to reverse-derive.
  if (!isApiServedSrc(target)) return null
  const src = target.getAttribute('src') || ''
  const m = src.match(/^\/api\/local-file\/(.+?)(?:\?.*)?$/)
  if (!m) return null
  let path = m[1]
  try {
    path = decodeURIComponent(path)
  } catch {
    // malformed percent sequence — keep the raw captured path
  }
  return path ? { img: target, path } : null
}

/** Delegated dragstart: write the attach payload and a custom drag ghost. */
export function onMdImageDragStart(e: DragEvent): void {
  const hit = resolveMdImageDragTarget(e)
  if (!hit) return
  const dt = e.dataTransfer
  if (!dt) return
  // Stop bubbling so enclosing OS-file drop surfaces / other drag logic never
  // see this internal drag as a foreign payload.
  e.stopPropagation()
  dt.effectAllowed = 'copy'
  const fileName = hit.path.split('/').pop() || hit.path
  setAttachDragData(dt, hit.path, false)
  const ghost = buildAttachDragImage(fileName, false)
  dt.setDragImage(ghost, 14, 16)
}

/** Delegated dragend: remove the custom ghost (must outlive dragstart). */
export function onMdImageDragEnd(_e: DragEvent): void {
  cleanupDragGhost()
}
