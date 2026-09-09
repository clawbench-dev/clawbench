/**
 * Touch "attach markdown source range to chat" for rendered Mermaid diagrams.
 *
 * A rendered diagram (`.mermaid` div produced by web/src/utils/mermaid.ts)
 * keeps `data-source-line` (1-based line of the opening ```mermaid fence in
 * the markdown file) and `data-mermaid` (the raw fence body). The markdown
 * file path sits on the ancestor `.markdown-body[data-file-path]`. Tapping the
 * `.mermaid-attach-badge` (armed by mermaid.ts in file-preview contexts) adds a
 * REFERENCE to the markdown file scoped to the diagram's fence line range —
 * the same composite-key model the whole-file / image attachments use, so one
 * file can carry several independent diagram references.
 *
 * Mirrors the image badge flow (web/src/utils/mdImageAttach.ts); differs only
 * in that the "path" is the markdown file and the identity includes the range.
 */

/** Selectors of the attach affordances injected around rendered diagrams:
 *  the header attach button (file-preview block header) and the legacy corner
 *  badge (kept for hand-built fixtures). */
export const MERMAID_ATTACH_BADGE = '.mermaid-block-attach-btn, .mermaid-attach-badge'

export interface MermaidRangeHit {
  /** Markdown file path (project-relative) from `.markdown-body[data-file-path]`. */
  path: string
  /** 1-based source line of the opening ```mermaid fence. */
  startLine: number
  /** 1-based source line of the closing fence (inclusive). */
  endLine: number
  /** The diagram container (`.mermaid`); its center is the fly origin. */
  container: HTMLElement
}

export interface MermaidAttachActions {
  add: (path: string, startLine: number, endLine: number) => void
  remove: (path: string, startLine: number, endLine: number) => void
  has: (path: string, startLine: number, endLine: number) => boolean
  toast: (msg: string, opts?: { icon?: string; type?: 'success' | 'error' | 'info'; duration?: number }) => void
  /** i18n message keys resolved by the caller. */
  messages: { added: string; removed: string }
}

/**
 * End line of the mermaid fence from its start line and body.
 *
 * Layout: line S = ```mermaid, lines S+1.. hold the body (`data-mermaid`, the
 * trimmed fence content, no markers), and the closing fence is the line after
 * the last body line. So end = S + bodyLineCount + 1.
 */
export function mermaidFenceEndLine(startLine: number, bodySource: string): number {
  const bodyLines = bodySource === '' ? 0 : bodySource.split('\n').length
  return startLine + 1 + bodyLines
}

/**
 * Resolve a click/tap target to a mermaid range-reference.
 * Returns null when the target is not the badge, the container or its line
 * metadata is missing, or there is no markdown file path ancestor.
 */
export function resolveMermaidBadgeClick(e: Event): MermaidRangeHit | null {
  const target = e.target as HTMLElement | null
  if (!target || !target.closest(MERMAID_ATTACH_BADGE)) return null
  // The container is either an ANCESTOR (legacy badge sits inside div.mermaid)
  // or a SIBLING child of the block wrapper (header attach button sits before
  // div.mermaid inside .mermaid-block-wrapper).
  let container = target.closest<HTMLElement>('div.mermaid[data-mermaid]')
  if (!container) {
    const wrapper = target.closest<HTMLElement>('.mermaid-block-wrapper')
    container = wrapper?.querySelector<HTMLElement>('div.mermaid[data-mermaid]') || null
  }
  const mdBody = target.closest<HTMLElement>('.markdown-body[data-file-path]')
  const path = mdBody?.getAttribute('data-file-path') || ''
  const startLine = parseInt(container?.getAttribute('data-source-line') || '', 10)
  const bodySource = container?.dataset.mermaid || ''
  if (!container || !path || !Number.isFinite(startLine)) return null
  // Prefer the authoritative closing-fence line stamped by the renderer —
  // it survives body whitespace that textContent.trim() drops when the
  // diagram body is stored. Only fall back to recomputing from the body when
  // no data-source-end is present (hand-built container or pre-fix render).
  const endRaw = container.getAttribute('data-source-end')
  const endLine = endRaw && Number.isFinite(parseInt(endRaw, 10))
    ? parseInt(endRaw, 10)
    : mermaidFenceEndLine(startLine, bodySource)
  return { path, startLine, endLine, container }
}

/**
 * Toggle the markdown range reference and fire the fly-to-chat particle.
 * Returns true when the badge handled the event (caller stops propagation).
 */
export function handleMermaidAttachClick(e: Event, actions: MermaidAttachActions): boolean {
  const hit = resolveMermaidBadgeClick(e)
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

  const rect = hit.container.getBoundingClientRect()
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
