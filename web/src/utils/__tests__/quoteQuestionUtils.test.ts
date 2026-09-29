import { describe, expect, it } from 'vitest'
import { closestElement, getFileInfo, getLineInfo, relativizeProjectPath, getQuoteSource, messageIdFromKey } from '@/utils/quoteQuestionUtils'

// --- closestElement ---

describe('closestElement', () => {
  it('returns null for null node', () => {
    expect(closestElement(null, '.any')).toBeNull()
  })

  it('returns the element itself when it matches the selector', () => {
    const el = document.createElement('div')
    el.classList.add('target')
    expect(closestElement(el, '.target')).toBe(el)
  })

  it('returns the parent element when a text node is passed and parent matches', () => {
    const parent = document.createElement('span')
    parent.classList.add('target')
    const text = document.createTextNode('hello')
    parent.appendChild(text)
    expect(closestElement(text, '.target')).toBe(parent)
  })

  it('returns null when no ancestor matches the selector', () => {
    const el = document.createElement('div')
    el.classList.add('other')
    expect(closestElement(el, '.target')).toBeNull()
  })

  it('finds closest matching ancestor in a deeply nested DOM', () => {
    const grandparent = document.createElement('div')
    grandparent.classList.add('target')
    const parent = document.createElement('section')
    const child = document.createElement('span')
    grandparent.appendChild(parent)
    parent.appendChild(child)
    // child has no .target, parent has no .target, grandparent has .target
    expect(closestElement(child, '.target')).toBe(grandparent)
  })

  it('picks the closest (nearest) matching ancestor when multiple match', () => {
    const outer = document.createElement('div')
    outer.classList.add('target')
    const inner = document.createElement('div')
    inner.classList.add('target')
    outer.appendChild(inner)
    const leaf = document.createElement('span')
    inner.appendChild(leaf)
    // inner is closer than outer
    expect(closestElement(leaf, '.target')).toBe(inner)
  })

  it('throws on empty selector string (JSDOM throws SyntaxError for invalid selector)', () => {
    const el = document.createElement('div')
    expect(() => closestElement(el, '')).toThrow()
  })

  it('returns null for a detached text node with no parent', () => {
    const text = document.createTextNode('orphan')
    expect(closestElement(text, '.any')).toBeNull()
  })

  it('handles text node whose parentElement does not match', () => {
    const parent = document.createElement('div')
    parent.classList.add('unrelated')
    const text = document.createTextNode('text')
    parent.appendChild(text)
    expect(closestElement(text, '.target')).toBeNull()
  })
})

// --- getLineInfo ---

describe('getLineInfo', () => {
  function makeCodeLine(lineNumber: string): HTMLElement {
    const el = document.createElement('div')
    el.classList.add('code-line')
    el.setAttribute('data-line', lineNumber)
    return el
  }

  function mockSelection(anchorNode: Node | null, focusNode: Node | null) {
    return { anchorNode, focusNode } as Selection
  }

  it('returns correct line numbers when both anchor and focus are in code-line elements', () => {
    const anchor = makeCodeLine('5')
    const focus = makeCodeLine('10')
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 5, endLine: 10 })
  })

  it('swaps when anchor line > focus line', () => {
    const anchor = makeCodeLine('20')
    const focus = makeCodeLine('3')
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 3, endLine: 20 })
  })

  it('returns same start and end when anchor and focus are on the same line', () => {
    const anchor = makeCodeLine('7')
    const focus = makeCodeLine('7')
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 7, endLine: 7 })
  })

  it('returns zeros when anchor is not in a code-line element', () => {
    const anchor = document.createElement('div') // no .code-line
    const focus = makeCodeLine('5')
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 0, endLine: 0 })
  })

  it('returns zeros when focus is not in a code-line element', () => {
    const anchor = makeCodeLine('5')
    const focus = document.createElement('div') // no .code-line
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 0, endLine: 0 })
  })

  it('returns zeros when both anchor and focus are not in code-line elements', () => {
    const anchor = document.createElement('div')
    const focus = document.createElement('div')
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 0, endLine: 0 })
  })

  it('returns zeros when a code-line lacks a data-line attribute', () => {
    const anchor = document.createElement('div')
    anchor.classList.add('code-line')
    // no data-line attribute → anchor edge has no valid line → whole range 0
    const focus = makeCodeLine('3')
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 0, endLine: 0 })
  })

  it('returns zeros when data-line attribute is non-numeric', () => {
    const anchor = document.createElement('div')
    anchor.classList.add('code-line')
    anchor.setAttribute('data-line', 'abc')
    const focus = makeCodeLine('4')
    const sel = mockSelection(anchor, focus)
    // non-numeric data-line → anchor edge has no valid line → whole range 0
    expect(getLineInfo(sel)).toEqual({ startLine: 0, endLine: 0 })
  })

  it('falls back to rendered block [data-source-line] when no code-line exists', () => {
    const anchor = document.createElement('p')
    anchor.setAttribute('data-source-line', '7')
    const focus = document.createElement('p')
    focus.setAttribute('data-source-line', '12')
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 7, endLine: 12 })
  })

  it('mixes code-line and rendered block lines across a selection', () => {
    const anchor = makeCodeLine('3')
    const focus = document.createElement('p')
    focus.setAttribute('data-source-line', '9')
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 3, endLine: 9 })
  })

  it('resolves a block line via a descendant text node', () => {
    const block = document.createElement('p')
    block.setAttribute('data-source-line', '15')
    const textNode = document.createTextNode('inside')
    block.appendChild(textNode)
    const focus = document.createElement('p')
    focus.setAttribute('data-source-line', '18')
    const sel = mockSelection(textNode, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 15, endLine: 18 })
  })

  it('finds code-line via text node parentElement', () => {
    const codeLine = makeCodeLine('12')
    const textNode = document.createTextNode('code here')
    codeLine.appendChild(textNode)
    const focus = makeCodeLine('15')
    const sel = mockSelection(textNode, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 12, endLine: 15 })
  })

  it('resolves a selection inside a table ROW to that row, not the table start', () => {
    // Regression: only <table> carried data-source-line, so selecting any row
    // reported the table's first line. Each <tr> now carries its own line.
    const table = document.createElement('table')
    table.setAttribute('data-source-line', '10')
    table.setAttribute('data-source-end', '13')
    table.innerHTML = [
      '<thead><tr data-source-line="10"><th>列A</th></tr></thead>',
      '<tbody>',
      '<tr data-source-line="12"><td id="row1">1</td></tr>',
      '<tr data-source-line="13"><td id="row2">3</td></tr>',
      '</tbody>',
    ].join('')
    const row1 = table.querySelector('#row1')!
    const row2 = table.querySelector('#row2')!
    const sel = mockSelection(row1.firstChild, row2.firstChild)
    expect(getLineInfo(sel)).toEqual({ startLine: 12, endLine: 13 })
  })
})

// --- getFileInfo ---

describe('getFileInfo', () => {
  it('returns filePath and language from .raw-content-pre', () => {
    const wrapper = document.createElement('pre')
    wrapper.classList.add('raw-content-pre')
    wrapper.setAttribute('data-file-path', '/src/main.go')
    wrapper.setAttribute('data-language', 'go')
    const container = document.createElement('code')
    wrapper.appendChild(container)
    expect(getFileInfo(container)).toEqual({ filePath: '/src/main.go', language: 'go' })
  })

  it('returns filePath and empty language from .markdown-body', () => {
    const wrapper = document.createElement('div')
    wrapper.classList.add('markdown-body')
    wrapper.setAttribute('data-file-path', '/docs/README.md')
    const container = document.createElement('p')
    wrapper.appendChild(container)
    expect(getFileInfo(container)).toEqual({ filePath: '/docs/README.md', language: '' })
  })

  it('prioritizes .raw-content-pre when both .raw-content-pre and .markdown-body are ancestors', () => {
    const markdown = document.createElement('div')
    markdown.classList.add('markdown-body')
    markdown.setAttribute('data-file-path', '/from-markdown.md')
    markdown.setAttribute('data-language', 'md')
    const raw = document.createElement('pre')
    raw.classList.add('raw-content-pre')
    raw.setAttribute('data-file-path', '/from-raw.go')
    raw.setAttribute('data-language', 'go')
    const container = document.createElement('code')
    raw.appendChild(container)
    markdown.appendChild(raw)
    // .raw-content-pre is closer, so it takes priority
    expect(getFileInfo(container)).toEqual({ filePath: '/from-raw.go', language: 'go' })
  })

  it('returns empty strings when container is not in .raw-content-pre, .markdown-body, or .office-preview-body', () => {
    const container = document.createElement('div')
    expect(getFileInfo(container)).toEqual({ filePath: '', language: '' })
  })

  it('defaults to empty string when data-file-path is missing on .raw-content-pre', () => {
    const wrapper = document.createElement('pre')
    wrapper.classList.add('raw-content-pre')
    wrapper.setAttribute('data-language', 'js')
    const container = document.createElement('code')
    wrapper.appendChild(container)
    expect(getFileInfo(container)).toEqual({ filePath: '', language: 'js' })
  })

  it('defaults to empty string when data-language is missing on .raw-content-pre', () => {
    const wrapper = document.createElement('pre')
    wrapper.classList.add('raw-content-pre')
    wrapper.setAttribute('data-file-path', '/src/app.ts')
    const container = document.createElement('code')
    wrapper.appendChild(container)
    expect(getFileInfo(container)).toEqual({ filePath: '/src/app.ts', language: '' })
  })

  it('defaults to empty string when data-file-path is missing on .markdown-body', () => {
    const wrapper = document.createElement('div')
    wrapper.classList.add('markdown-body')
    const container = document.createElement('p')
    wrapper.appendChild(container)
    expect(getFileInfo(container)).toEqual({ filePath: '', language: '' })
  })

  it('finds .raw-content-pre through intermediate elements', () => {
    const wrapper = document.createElement('pre')
    wrapper.classList.add('raw-content-pre')
    wrapper.setAttribute('data-file-path', '/deep/file.py')
    wrapper.setAttribute('data-language', 'python')
    const mid = document.createElement('div')
    const container = document.createElement('span')
    mid.appendChild(container)
    wrapper.appendChild(mid)
    expect(getFileInfo(container)).toEqual({ filePath: '/deep/file.py', language: 'python' })
  })

  it('container itself is .raw-content-pre returns its own attributes', () => {
    const el = document.createElement('pre')
    el.classList.add('raw-content-pre')
    el.setAttribute('data-file-path', '/self.rs')
    el.setAttribute('data-language', 'rust')
    // closest('.raw-content-pre') on the element itself returns itself
    expect(getFileInfo(el)).toEqual({ filePath: '/self.rs', language: 'rust' })
  })

  it('returns filePath and empty language from .office-preview-body', () => {
    const wrapper = document.createElement('div')
    wrapper.classList.add('office-preview-body')
    wrapper.setAttribute('data-file-path', '/docs/report.docx')
    const container = document.createElement('div')
    wrapper.appendChild(container)
    expect(getFileInfo(container)).toEqual({ filePath: '/docs/report.docx', language: '' })
  })

  it('defaults to empty string when data-file-path is missing on .office-preview-body', () => {
    const wrapper = document.createElement('div')
    wrapper.classList.add('office-preview-body')
    const container = document.createElement('div')
    wrapper.appendChild(container)
    expect(getFileInfo(container)).toEqual({ filePath: '', language: '' })
  })

  it('prioritizes .raw-content-pre over .office-preview-body', () => {
    const office = document.createElement('div')
    office.classList.add('office-preview-body')
    office.setAttribute('data-file-path', '/from-office.docx')
    const raw = document.createElement('pre')
    raw.classList.add('raw-content-pre')
    raw.setAttribute('data-file-path', '/from-raw.go')
    raw.setAttribute('data-language', 'go')
    const container = document.createElement('code')
    raw.appendChild(container)
    office.appendChild(raw)
    // .raw-content-pre is checked first and is closer
    expect(getFileInfo(container)).toEqual({ filePath: '/from-raw.go', language: 'go' })
  })

  it('prioritizes .markdown-body over .office-preview-body', () => {
    const office = document.createElement('div')
    office.classList.add('office-preview-body')
    office.setAttribute('data-file-path', '/from-office.xlsx')
    const md = document.createElement('div')
    md.classList.add('markdown-body')
    md.setAttribute('data-file-path', '/from-markdown.md')
    const container = document.createElement('p')
    md.appendChild(container)
    office.appendChild(md)
    // .markdown-body is checked before .office-preview-body
    expect(getFileInfo(container)).toEqual({ filePath: '/from-markdown.md', language: '' })
  })
})

// --- getQuoteSource ---

describe('getQuoteSource', () => {
  it('reads the label and language from an ancestor', () => {
    const wrap = document.createElement('div')
    wrap.setAttribute('data-quote-source', 'acme/widgets#123')
    wrap.setAttribute('data-quote-language', 'issue')
    const inner = document.createElement('div')
    wrap.appendChild(inner)

    expect(getQuoteSource(inner)).toEqual({ label: 'acme/widgets#123', language: 'issue', url: '' })
  })

  it('returns null outside a labelled region so callers can fall back', () => {
    const plain = document.createElement('div')
    plain.className = 'markdown-body'
    expect(getQuoteSource(plain)).toBeNull()
  })

  it('treats an empty label as absent', () => {
    const wrap = document.createElement('div')
    wrap.setAttribute('data-quote-source', '')
    const inner = document.createElement('div')
    wrap.appendChild(inner)
    expect(getQuoteSource(inner)).toBeNull()
  })

  it('defaults the language to an empty string', () => {
    const wrap = document.createElement('div')
    wrap.setAttribute('data-quote-source', 'a/b#1')
    const inner = document.createElement('div')
    wrap.appendChild(inner)
    expect(getQuoteSource(inner)).toEqual({ label: 'a/b#1', language: '', url: '' })
  })

  // The locators are the machine keys behind the human-readable label. Without
  // them a quote can name its source but never reach it.
  describe('source locators', () => {
    function sourceWith(attrs: Record<string, string>) {
      const wrap = document.createElement('div')
      wrap.setAttribute('data-quote-source', '每日构建 (#12)')
      for (const [k, v] of Object.entries(attrs)) wrap.setAttribute(k, v)
      const inner = document.createElement('div')
      wrap.appendChild(inner)
      return getQuoteSource(inner)
    }

    it('reads the commit', () => {
      expect(sourceWith({ 'data-quote-commit': 'a1b2c3d' })).toMatchObject({ commitSha: 'a1b2c3d' })
    })

    it('reads the task id as a number', () => {
      expect(sourceWith({ 'data-quote-task-id': '12' })).toMatchObject({ taskId: 12 })
    })

    it('reads the session and message ids', () => {
      expect(sourceWith({ 'data-quote-session-id': 'sess-abc', 'data-quote-message-id': '42' }))
        .toMatchObject({ sessionId: 'sess-abc', messageId: 42 })
    })

    it('reads the execution id', () => {
      expect(sourceWith({ 'data-quote-execution-id': 'exec-7' })).toMatchObject({ executionId: 'exec-7' })
    })

    it('omits locators that are absent rather than setting them to undefined', () => {
      const got = sourceWith({})
      expect('commitSha' in got!).toBe(false)
      expect('taskId' in got!).toBe(false)
      expect('sessionId' in got!).toBe(false)
      expect('messageId' in got!).toBe(false)
      expect('executionId' in got!).toBe(false)
    })

    // A non-numeric or non-positive id is not an id. Emitting 0/NaN would make
    // the drawer offer a jump that goes nowhere.
    it('ignores a non-numeric or non-positive id', () => {
      expect(sourceWith({ 'data-quote-task-id': 'abc' })).not.toHaveProperty('taskId')
      expect(sourceWith({ 'data-quote-task-id': '0' })).not.toHaveProperty('taskId')
      expect(sourceWith({ 'data-quote-message-id': '-3' })).not.toHaveProperty('messageId')
    })
  })

  it('reads the source address so a forge quote can offer a jump action', () => {
    // Without the address the label alone is not openable, and the quote detail
    // drawer would have no jump target.
    const wrap = document.createElement('div')
    wrap.setAttribute('data-quote-source', 'acme/widgets#123')
    wrap.setAttribute('data-quote-url', 'https://github.com/acme/widgets/issues/123')
    const inner = document.createElement('div')
    wrap.appendChild(inner)

    expect(getQuoteSource(inner)?.url).toBe('https://github.com/acme/widgets/issues/123')
  })
})

// --- messageIdFromKey ---

describe('messageIdFromKey', () => {
  it('parses a db-prefixed key', () => {
    expect(messageIdFromKey('db-42')).toBe(42)
  })

  it('returns undefined for an absent key (optimistic message)', () => {
    expect(messageIdFromKey(null)).toBeUndefined()
    expect(messageIdFromKey(undefined)).toBeUndefined()
    expect(messageIdFromKey('')).toBeUndefined()
  })

  it('rejects a key that is not db-prefixed', () => {
    expect(messageIdFromKey('local-3')).toBeUndefined()
    expect(messageIdFromKey('42')).toBeUndefined()
  })

  it('rejects a non-numeric or non-positive id', () => {
    expect(messageIdFromKey('db-abc')).toBeUndefined()
    expect(messageIdFromKey('db-0')).toBeUndefined()
    expect(messageIdFromKey('db--5')).toBeUndefined()
  })
})
