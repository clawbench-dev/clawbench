/**
 * Attach a rendered markdown CODE BLOCK or TABLE to the chat as a reference to
 * the markdown file's source line range.
 *
 * The shared markdown pipeline stamps every fenced-code `<pre>` and `<table>`
 * with `data-source-line` / `data-source-end` (1-based inclusive source range,
 * see web/src/utils/markedConfig.ts). The header annotator (useCodeBlockHeader)
 * adds a paperclip `.code-block-attach-btn` / `.table-block-attach-btn` to each
 * block's header actions. Tapping it in a FILE-PREVIEW context (where an
 * ancestor `.markdown-body[data-file-path]` carries the md path) attaches that
 * line range as a reference — same composite-key model as the Mermaid badge.
 *
 * Chat / export / share render the same header markup, but the resolver requires
 * a `.markdown-body[data-file-path]` ancestor and a non-share page, so the
 * button is inert there (CSS also hides it outside file previews).
 */

import { isShareMode } from '@/share/shareMode'

/** Selectors of the header attach buttons injected by useCodeBlockHeader. */
export const BLOCK_ATTACH_BTN = '.code-block-attach-btn, .table-block-attach-btn'

export interface BlockRangeHit {
  /** 'code' when from a code block, 'table' when from a table block. */
  kind: 'code' | 'table'
  /** Markdown file path from `.markdown-body[data-file-path]`. */
  path: string
  /** 1-based first source line of the block. */
  startLine: number
  /** 1-based last source line of the block (inclusive). */
  endLine: number
  /** The block element (`pre` / `table`) whose center is the fly origin. */
  el: HTMLElement
}

/**
 * Resolve a click/tap target to a code/table range reference.
 * Returns null when not on an attach header button, the underlying block has no
 * source metadata, or there is no markdown file path ancestor (chat/export/share).
 */
export function resolveBlockAttachClick(e: Event): BlockRangeHit | null {
  const target = e.target as HTMLElement | null
  if (!target || !target.closest(BLOCK_ATTACH_BTN)) return null
  if (isShareMode()) return null

  const mdBody = target.closest<HTMLElement>('.markdown-body[data-file-path]')
  const path = mdBody?.getAttribute('data-file-path') || ''
  if (!path) return null

  const wrapper = target.closest<HTMLElement>('.code-block-wrapper, .table-block-wrapper')
  const el = wrapper?.querySelector<HTMLElement>('pre[data-source-line], table[data-source-line]')
  if (!el) return null

  const startLine = parseInt(el.getAttribute('data-source-line') || '', 10)
  const endRaw = el.getAttribute('data-source-end')
  const endLine = endRaw ? parseInt(endRaw, 10) : startLine
  if (!Number.isFinite(startLine) || !Number.isFinite(endLine)) return null

  const kind: 'code' | 'table' = el.tagName === 'TABLE' ? 'table' : 'code'
  return { kind, path, startLine, endLine, el }
}

/** Ranged attach actions — same shape as the mermaid badge toggle. */
import type { MermaidAttachActions as RangedAttachActions } from '@/utils/mdMermaidAttach'
export type { RangedAttachActions }
/**
 * Toggle the markdown range reference and fire the fly-to-chat particle.
 * Returns true when the button handled the event (caller stops propagation).
 */
export function handleBlockAttachClick(e: Event, actions: RangedAttachActions): boolean {
  const hit = resolveBlockAttachClick(e)
  if (!hit) return false

  e.preventDefault()
  e.stopPropagation()

  if (actions.has(hit.path, hit.startLine, hit.endLine)) {
    actions.remove(hit.path, hit.startLine, hit.endLine)
    actions.toast(actions.messages.removed, { icon: '📎', type: 'info', duration: 1500 })
  } else {
    actions.add(hit.path, hit.startLine, hit.endLine)
    actions.toast(actions.messages.added, { icon: '📎', type: 'success', duration: 1500 })
  }

  const rect = hit.el.getBoundingClientRect()
  const from = rect.width > 0 && rect.height > 0
    ? { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 }
    : { x: rect.left, y: rect.top }
  const dockChatBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
  const to = dockChatBtn?.getBoundingClientRect()
  if (to) {
    window.dispatchEvent(new CustomEvent('attach-to-chat', {
      detail: {
        from,
        to: { x: to.left + to.width / 2, y: to.top + to.height / 2 },
      },
    }))
  }

  return true
}
