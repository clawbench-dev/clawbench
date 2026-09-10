import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  resolveBlockAttachClick,
  handleBlockAttachClick,
  type RangedAttachActions,
} from '@/utils/mdBlockAttach'

function makeCodeBlock(opts: {
  mdPath?: string
  start?: string
  end?: string
} = {}): { md: HTMLElement; pre: HTMLElement; btn: HTMLElement } {
  const md = document.createElement('div')
  md.className = 'markdown-body'
  md.setAttribute('data-file-path', opts.mdPath ?? 'docs/guide.md')
  const wrapper = document.createElement('div')
  wrapper.className = 'code-block-wrapper'
  const btn = document.createElement('button')
  btn.className = 'code-block-attach-btn'
  const pre = document.createElement('pre')
  if (opts.start) pre.setAttribute('data-source-line', opts.start)
  if (opts.end) pre.setAttribute('data-source-end', opts.end)
  const code = document.createElement('code')
  pre.appendChild(code)
  wrapper.appendChild(btn)
  wrapper.appendChild(pre)
  md.appendChild(wrapper)
  return { md, pre, btn }
}

function makeTableBlock(opts: {
  mdPath?: string
  start?: string
  end?: string
} = {}): { md: HTMLElement; table: HTMLElement; btn: HTMLElement } {
  const md = document.createElement('div')
  md.className = 'markdown-body'
  md.setAttribute('data-file-path', opts.mdPath ?? 'docs/guide.md')
  const wrapper = document.createElement('div')
  wrapper.className = 'table-block-wrapper'
  const btn = document.createElement('button')
  btn.className = 'table-block-attach-btn'
  const tableWrap = document.createElement('div')
  tableWrap.className = 'table-wrap'
  const table = document.createElement('table')
  if (opts.start) table.setAttribute('data-source-line', opts.start)
  if (opts.end) table.setAttribute('data-source-end', opts.end)
  tableWrap.appendChild(table)
  wrapper.appendChild(btn)
  wrapper.appendChild(tableWrap)
  md.appendChild(wrapper)
  return { md, table, btn }
}

function clickOn(btn: HTMLElement): Event {
  return { target: btn, preventDefault: () => {}, stopPropagation: () => {} } as unknown as Event
}

function makeActions(overrides: Partial<RangedAttachActions> = {}): RangedAttachActions {
  return {
    add: vi.fn(),
    remove: vi.fn(),
    has: vi.fn(() => false),
    toast: vi.fn(),
    messages: { added: 'Added to chat', removed: 'Removed from chat attachments' },
    ...overrides,
  }
}

describe('resolveBlockAttachClick', () => {
  it('resolves a code block attach button to its md range', () => {
    const { pre, btn } = makeCodeBlock({ start: '10', end: '15' })
    const hit = resolveBlockAttachClick(clickOn(btn))
    expect(hit).toEqual({ kind: 'code', path: 'docs/guide.md', startLine: 10, endLine: 15, el: pre })
  })

  it('resolves a table attach button to its md range', () => {
    const { table, btn } = makeTableBlock({ start: '3', end: '6' })
    const hit = resolveBlockAttachClick(clickOn(btn))
    expect(hit).toEqual({ kind: 'table', path: 'docs/guide.md', startLine: 3, endLine: 6, el: table })
  })

  it('falls back endLine to startLine when data-source-end is absent', () => {
    const { btn } = makeCodeBlock({ start: '4' })
    const hit = resolveBlockAttachClick(clickOn(btn))
    expect(hit?.startLine).toBe(4)
    expect(hit?.endLine).toBe(4)
  })

  it('returns null for clicks not on an attach button', () => {
    expect(resolveBlockAttachClick({ target: document.createElement('p') } as unknown as Event)).toBeNull()
  })

  it('returns null when the md body has no data-file-path (chat/export/share)', () => {
    const { btn } = makeCodeBlock({ mdPath: '', start: '1', end: '2' })
    expect(resolveBlockAttachClick(clickOn(btn))).toBeNull()
  })

  it('returns null when the block carries no data-source-line', () => {
    const { btn } = makeCodeBlock({})
    expect(resolveBlockAttachClick(clickOn(btn))).toBeNull()
  })

  it('returns null in share mode even inside a file preview', async () => {
    const { setShareToken } = await import('@/share/shareMode')
    const { btn } = makeCodeBlock({ start: '1', end: '2' })
    setShareToken('tok-share')
    try {
      expect(resolveBlockAttachClick(clickOn(btn))).toBeNull()
    } finally {
      setShareToken(null)
    }
  })
})

describe('handleBlockAttachClick', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('adds the md range reference on first tap', () => {
    const { btn } = makeCodeBlock({ start: '10', end: '15' })
    const actions = makeActions()
    const ret = handleBlockAttachClick(clickOn(btn), actions)
    expect(ret).toBe(true)
    expect(actions.add).toHaveBeenCalledWith('docs/guide.md', 10, 15)
    expect(actions.remove).not.toHaveBeenCalled()
    expect(actions.toast).toHaveBeenCalledWith('Added to chat', { icon: '📎', type: 'success', duration: 1500 })
  })

  it('removes the exact range when already attached', () => {
    const { btn } = makeTableBlock({ start: '3', end: '6' })
    const actions = makeActions({ has: vi.fn(() => true) })
    handleBlockAttachClick(clickOn(btn), actions)
    expect(actions.remove).toHaveBeenCalledWith('docs/guide.md', 3, 6)
    expect(actions.add).not.toHaveBeenCalled()
  })

  it('does nothing and returns false for non-button clicks', () => {
    const actions = makeActions()
    const ret = handleBlockAttachClick({ target: document.createElement('p') } as unknown as Event, actions)
    expect(ret).toBe(false)
    expect(actions.add).not.toHaveBeenCalled()
  })

  it('stops propagation and prevents default (keeps the header copy/wrap handlers inert)', () => {
    const { btn } = makeCodeBlock({ start: '1', end: '2' })
    const stopPropagation = vi.fn()
    const preventDefault = vi.fn()
    handleBlockAttachClick({
      target: btn,
      stopPropagation,
      preventDefault,
    } as unknown as Event, makeActions())
    expect(stopPropagation).toHaveBeenCalled()
    expect(preventDefault).toHaveBeenCalled()
  })
})
