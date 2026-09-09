import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  mermaidFenceEndLine,
  resolveMermaidBadgeClick,
  handleMermaidAttachClick,
  type MermaidAttachActions,
} from '@/utils/mdMermaidAttach'

/** Build .markdown-body[data-file-path] > .mermaid[data-source-line][data-mermaid] > svg + badge. */
function makeMdBody(opts: {
  mdPath?: string
  srcLine?: string
  srcEnd?: string
  body?: string
} = {}): { md: HTMLElement; container: HTMLElement; badge: HTMLElement } {
  const md = document.createElement('div')
  md.className = 'markdown-body'
  md.setAttribute('data-file-path', opts.mdPath ?? 'docs/guide.md')
  const container = document.createElement('div')
  container.className = 'mermaid'
  if (opts.srcLine) container.setAttribute('data-source-line', opts.srcLine)
  if (opts.srcEnd) container.setAttribute('data-source-end', opts.srcEnd)
  if (opts.body !== undefined) container.dataset.mermaid = opts.body
  const badge = document.createElement('span')
  badge.className = 'mermaid-attach-badge'
  container.appendChild(document.createElementNS('http://www.w3.org/2000/svg', 'svg'))
  container.appendChild(badge)
  md.appendChild(container)
  return { md, container, badge }
}

function clickOn(badge: HTMLElement): Event {
  return { target: badge, preventDefault: () => {}, stopPropagation: () => {} } as unknown as Event
}

function makeActions(overrides: Partial<MermaidAttachActions> = {}): MermaidAttachActions {
  return {
    add: vi.fn(),
    remove: vi.fn(),
    has: vi.fn(() => false),
    toast: vi.fn(),
    messages: { added: 'Added to chat', removed: 'Removed from chat attachments' },
    ...overrides,
  }
}

describe('mermaidFenceEndLine', () => {
  it('computes the closing fence line from start + body lines', () => {
    // ```mermaid on line 5, two body lines, closing ``` on line 8.
    expect(mermaidFenceEndLine(5, 'graph TD\nA-->B')).toBe(8)
  })

  it('handles a single-line body', () => {
    // ``` at 10, body on 11, closing ``` at 12.
    expect(mermaidFenceEndLine(10, 'A-->B')).toBe(12)
  })

  it('empty body: opening fence immediately followed by the closing fence', () => {
    // ``` at 3, closing ``` at 4.
    expect(mermaidFenceEndLine(3, '')).toBe(4)
  })
})

describe('resolveMermaidBadgeClick', () => {
  it('resolves a badge tap to the markdown range', () => {
    const { container, badge } = makeMdBody({ srcLine: '5', body: 'graph TD\nA-->B' })
    const hit = resolveMermaidBadgeClick(clickOn(badge))
    expect(hit).toEqual({ path: 'docs/guide.md', startLine: 5, endLine: 8, container })
  })

  it('returns null for clicks not on the badge', () => {
    const { container } = makeMdBody({ srcLine: '5', body: 'A-->B' })
    expect(resolveMermaidBadgeClick({ target: container } as unknown as Event)).toBeNull()
    expect(resolveMermaidBadgeClick({ target: document.createElement('p') } as unknown as Event)).toBeNull()
  })

  it('returns null when the md body lacks data-file-path', () => {
    const { badge } = makeMdBody({ mdPath: '', srcLine: '5', body: 'A-->B' })
    expect(resolveMermaidBadgeClick(clickOn(badge))).toBeNull()
  })

  it('returns null when the container has no data-source-line', () => {
    const { badge } = makeMdBody({ body: 'A-->B' })
    expect(resolveMermaidBadgeClick(clickOn(badge))).toBeNull()
  })

  it('prefers the authoritative data-source-end over a body-derived fence line', () => {
    // Body ends with a blank line that textContent.trim() would drop, so the
    // recomputed end (5+1+1=7) is one short of the true closing fence (8).
    // The renderer-stamped data-source-end="8" must win.
    const { container, badge } = makeMdBody({ srcLine: '5', srcEnd: '8', body: 'graph TD\n  A-->B\n' })
    const hit = resolveMermaidBadgeClick(clickOn(badge))
    expect(hit).toEqual({ path: 'docs/guide.md', startLine: 5, endLine: 8, container })
  })

  it('falls back to a body-derived fence line when data-source-end is absent', () => {
    // Hand-built container / pre-fix render without the attr → recompute.
    const { container, badge } = makeMdBody({ srcLine: '5', body: 'graph TD\nA-->B' })
    const hit = resolveMermaidBadgeClick(clickOn(badge))
    expect(hit).toEqual({ path: 'docs/guide.md', startLine: 5, endLine: 8, container })
  })

  it('resolves a header attach button whose container is a wrapper sibling', () => {
    // File-preview diagrams sit in .mermaid-block-wrapper where the header
    // (attach button) precedes the div.mermaid container — not an ancestor.
    const md = document.createElement('div')
    md.className = 'markdown-body'
    md.setAttribute('data-file-path', 'docs/guide.md')
    const wrapper = document.createElement('div')
    wrapper.className = 'mermaid-block-wrapper'
    const header = document.createElement('div')
    header.className = 'mermaid-block-header'
    const btn = document.createElement('button')
    btn.className = 'mermaid-block-attach-btn'
    header.appendChild(btn)
    wrapper.appendChild(header)
    const container = document.createElement('div')
    container.className = 'mermaid'
    container.setAttribute('data-source-line', '5')
    container.setAttribute('data-source-end', '9')
    container.dataset.mermaid = 'graph TD; A-->B'
    wrapper.appendChild(container)
    md.appendChild(wrapper)
    const hit = resolveMermaidBadgeClick(clickOn(btn))
    expect(hit).toEqual({ path: 'docs/guide.md', startLine: 5, endLine: 9, container })
  })
})

describe('handleMermaidAttachClick', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('adds the markdown range reference on first tap', () => {
    const { badge } = makeMdBody({ srcLine: '5', body: 'graph TD\nA-->B' })
    const actions = makeActions()
    const ret = handleMermaidAttachClick(clickOn(badge), actions)
    expect(ret).toBe(true)
    expect(actions.add).toHaveBeenCalledWith('docs/guide.md', 5, 8)
    expect(actions.remove).not.toHaveBeenCalled()
    expect(actions.toast).toHaveBeenCalledWith('Added to chat', { icon: '📎', type: 'success', duration: 1500 })
  })

  it('removes the exact range when already attached', () => {
    const { badge } = makeMdBody({ srcLine: '5', body: 'A-->B' })
    const actions = makeActions({ has: vi.fn(() => true) })
    handleMermaidAttachClick(clickOn(badge), actions)
    expect(actions.remove).toHaveBeenCalledWith('docs/guide.md', 5, 7)
    expect(actions.add).not.toHaveBeenCalled()
  })

  it('does nothing and returns false for non-badge clicks', () => {
    const actions = makeActions()
    const ret = handleMermaidAttachClick({ target: document.createElement('p') } as unknown as Event, actions)
    expect(ret).toBe(false)
    expect(actions.add).not.toHaveBeenCalled()
  })

  it('stops propagation and prevents default (blocks the lightbox expand handler)', () => {
    const { badge } = makeMdBody({ srcLine: '5', body: 'A-->B' })
    const stopPropagation = vi.fn()
    const preventDefault = vi.fn()
    handleMermaidAttachClick({
      target: badge,
      stopPropagation,
      preventDefault,
    } as unknown as Event, makeActions())
    expect(stopPropagation).toHaveBeenCalled()
    expect(preventDefault).toHaveBeenCalled()
  })
})
