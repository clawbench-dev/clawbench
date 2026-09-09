import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  resolveMermaidDragTarget,
  onMermaidDragStart,
  onMermaidDragEnd,
  type MermaidDragHit,
} from '@/utils/mdMermaidDrag'
import { readAttachDragData, ATTACH_DRAG_MIME } from '@/utils/attachDrag'

/** File-preview diagram: svg inside div.mermaid inside .markdown-body[data-file-path]. */
function makePreviewMermaid(opts: { mdPath?: string; start?: string; end?: string } = {}): {
  md: HTMLElement
  container: HTMLElement
  svg: SVGSVGElement
} {
  const md = document.createElement('div')
  md.className = 'markdown-body'
  md.setAttribute('data-file-path', opts.mdPath ?? 'docs/guide.md')
  const container = document.createElement('div')
  container.className = 'mermaid'
  container.dataset.mermaid = 'graph TD; A-->B'
  if (opts.start) container.setAttribute('data-source-line', opts.start)
  if (opts.end) container.setAttribute('data-source-end', opts.end)
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg') as unknown as SVGSVGElement
  container.appendChild(svg)
  md.appendChild(container)
  return { md, container, svg }
}

/** A chat-like bare diagram (no data-file-path ancestor). */
function makeBareMermaid(): { container: HTMLElement; svg: SVGSVGElement } {
  const container = document.createElement('div')
  container.className = 'mermaid'
  container.dataset.mermaid = 'graph TD; A-->B'
  container.setAttribute('data-source-line', '5')
  container.setAttribute('data-source-end', '8')
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg') as unknown as SVGSVGElement
  container.appendChild(svg)
  return { container, svg }
}

function dragFrom(svg: Element): DragEvent {
  return { target: svg, dataTransfer: null, stopPropagation: () => {} } as unknown as DragEvent
}

function makeDataTransfer() {
  const store = new Map<string, string>()
  const types: string[] = []
  return {
    effectAllowed: '',
    setData(type: string, value: string) {
      if (!store.has(type)) types.push(type)
      store.set(type, value)
    },
    getData(type: string) {
      return store.get(type) ?? ''
    },
    setDragImage: vi.fn(),
    types: types as unknown as readonly string[],
  } as unknown as DataTransfer
}

describe('resolveMermaidDragTarget', () => {
  it('resolves a svg drag in a file-preview mermaid to its md line range', () => {
    const { container, svg } = makePreviewMermaid({ start: '5', end: '8' })
    const hit = resolveMermaidDragTarget(dragFrom(svg))
    expect(hit).toEqual({ container, path: 'docs/guide.md', startLine: 5, endLine: 8 } satisfies MermaidDragHit)
  })

  it('falls back to the closing-fence line when data-source-end is absent', () => {
    // start 4 + 1 fence + 1 body line = end 6
    const { container, svg } = makePreviewMermaid({ start: '4' })
    const hit = resolveMermaidDragTarget(dragFrom(svg))
    expect(hit?.startLine).toBe(4)
    expect(hit?.endLine).toBe(6)
  })

  it('returns null for mermaid without a .markdown-body[data-file-path] ancestor (chat/share)', () => {
    const { svg } = makeBareMermaid()
    expect(resolveMermaidDragTarget(dragFrom(svg))).toBeNull()
  })

  it('returns null in share mode even inside a file preview', async () => {
    const { setShareToken } = await import('@/share/shareMode')
    const { svg } = makePreviewMermaid({ start: '5', end: '8' })
    setShareToken('tok-share')
    try {
      expect(resolveMermaidDragTarget(dragFrom(svg))).toBeNull()
    } finally {
      setShareToken(null)
    }
  })

  it('returns null for non-mermaid drag targets', () => {
    const el = document.createElement('div')
    expect(resolveMermaidDragTarget(dragFrom(el))).toBeNull()
  })

  it('ignores drags starting on the header buttons / expand icon', () => {
    const { md, svg } = makePreviewMermaid({ start: '5', end: '8' })
    // Header button lives next to the container inside a wrapper.
    const wrapper = document.createElement('div')
    wrapper.className = 'mermaid-block-wrapper'
    const btn = document.createElement('button')
    btn.className = 'mermaid-block-attach-btn'
    wrapper.appendChild(btn)
    md.insertBefore(wrapper, md.querySelector('.mermaid'))
    expect(resolveMermaidDragTarget(dragFrom(btn))).toBeNull()
    // svg body still resolves
    expect(resolveMermaidDragTarget(dragFrom(svg))).not.toBeNull()
  })
})

describe('onMermaidDragStart / End', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('writes a ranged attach payload and sets a drag ghost', () => {
    const { svg } = makePreviewMermaid({ start: '5', end: '8' })
    const stopPropagation = vi.fn()
    const dt = makeDataTransfer()
    onMermaidDragStart({
      target: svg,
      dataTransfer: dt,
      stopPropagation,
    } as unknown as DragEvent)
    expect(stopPropagation).toHaveBeenCalled()
    expect(dt.effectAllowed).toBe('copy')
    expect(dt.setDragImage).toHaveBeenCalledTimes(1)
    const data = readAttachDragData(dt)
    expect(data).toEqual({ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 8 })
    expect(dt.getData(ATTACH_DRAG_MIME)).toContain('"startLine":5')
    expect(dt.getData('text/plain')).toBe('docs/guide.md')
    onMermaidDragEnd({} as DragEvent)
  })

  it('does nothing for non-resolvable targets', () => {
    const { svg } = makeBareMermaid()
    const dt = makeDataTransfer()
    onMermaidDragStart({ target: svg, dataTransfer: dt, stopPropagation: () => {} } as unknown as DragEvent)
    expect(dt.setDragImage).not.toHaveBeenCalled()
  })
})
