import { describe, expect, it } from 'vitest'
import { closestElement, getFileInfo, getLineInfo, buildQuoteMessage, relativizeProjectPath, buildMultiQuoteMessage } from '@/utils/quoteQuestionUtils'

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

  it('defaults to 0 when data-line attribute is missing', () => {
    const anchor = document.createElement('div')
    anchor.classList.add('code-line')
    // no data-line attribute
    const focus = makeCodeLine('3')
    const sel = mockSelection(anchor, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 0, endLine: 3 })
  })

  it('produces NaN when data-line attribute is non-numeric', () => {
    const anchor = document.createElement('div')
    anchor.classList.add('code-line')
    anchor.setAttribute('data-line', 'abc')
    const focus = makeCodeLine('4')
    const sel = mockSelection(anchor, focus)
    // 'abc' || '0' → 'abc' (truthy), parseInt('abc') → NaN
    // Math.min(NaN, 4) → NaN
    expect(getLineInfo(sel)).toEqual({ startLine: NaN, endLine: NaN })
  })

  it('finds code-line via text node parentElement', () => {
    const codeLine = makeCodeLine('12')
    const textNode = document.createTextNode('code here')
    codeLine.appendChild(textNode)
    const focus = makeCodeLine('15')
    const sel = mockSelection(textNode, focus)
    expect(getLineInfo(sel)).toEqual({ startLine: 12, endLine: 15 })
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

// --- buildQuoteMessage ---
describe('buildQuoteMessage', () => {
  it('embeds quoted code with language and line range', () => {
    const result = buildQuoteMessage('explain this', 'func main()', '/cmd/main.go', 'go', 10, 25)
    expect(result).toBe('explain this\n\n```go:/cmd/main.go:10-25\nfunc main()\n```')
  })

  it('embeds quoted code with single line number', () => {
    const result = buildQuoteMessage('what is this?', 'return nil', '/internal/handler.go', 'go', 42, 42)
    expect(result).toBe('what is this?\n\n```go:/internal/handler.go:42\nreturn nil\n```')
  })

  it('embeds quoted code without line numbers', () => {
    const result = buildQuoteMessage('explain', 'some text', '/README.md', '', 0, 0)
    expect(result).toBe('explain\n\n' + '```' + ':/README.md\nsome text\n' + '```')
  })

  it('uses empty language prefix when language is empty', () => {
    const result = buildQuoteMessage('question', 'plain text', '/notes.txt', '', 5, 5)
    expect(result).toBe('question\n\n' + '```' + ':/notes.txt:5\nplain text\n' + '```')
  })

  it('trims user message whitespace', () => {
    const result = buildQuoteMessage('  explain this  ', 'code', '/f.go', 'go', 1, 2)
    expect(result).toBe('explain this\n\n```go:/f.go:1-2\ncode\n```')
  })

  it('embeds quoted code with language but no line numbers', () => {
    const result = buildQuoteMessage('explain', 'some code', '/main.go', 'go', 0, 0)
    expect(result).toBe('explain\n\n```go:/main.go\nsome code\n```')
  })
})

describe('relativizeProjectPath', () => {
  it('strips the project root prefix from an in-project absolute path', () => {
    expect(relativizeProjectPath('/home/user/proj/src/a.ts', '/home/user/proj')).toBe('src/a.ts')
  })

  it('returns the path unchanged when not under the project root', () => {
    expect(relativizeProjectPath('/other/dir/b.ts', '/home/user/proj')).toBe('/other/dir/b.ts')
  })

  it('returns a project-relative path unchanged', () => {
    expect(relativizeProjectPath('src/a.ts', '/home/user/proj')).toBe('src/a.ts')
  })

  it('handles empty path and empty root', () => {
    expect(relativizeProjectPath('', '/home/user/proj')).toBe('')
    expect(relativizeProjectPath('src/a.ts', '')).toBe('src/a.ts')
  })

  it('does not strip a root-prefixed sibling path (e.g. proj-other)', () => {
    expect(relativizeProjectPath('/home/user/proj-other/a.ts', '/home/user/proj')).toBe('/home/user/proj-other/a.ts')
  })
})

describe('buildMultiQuoteMessage', () => {
  const quotes = [
    { text: 'const a = 1', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 1, note: 'Check initialization' },
    { text: 'return a', filePath: '/b.ts', language: 'ts', startLine: 8, endLine: 9, note: '' },
  ]

  it('places the overall question first and each note before its quote', () => {
    expect(buildMultiQuoteMessage('Compare these', quotes)).toBe(
      'Compare these\n\nCheck initialization\n\n```ts:/a.ts:1\nconst a = 1\n```\n\n```ts:/b.ts:8-9\nreturn a\n```',
    )
  })

  it('allows quotes to be sent without an overall question', () => {
    expect(buildMultiQuoteMessage('', [quotes[1]])).toBe('```ts:/b.ts:8-9\nreturn a\n```')
  })

  it('preserves quote order across selections from the same file', () => {
    const sameFile = [
      { ...quotes[0], note: '', startLine: 10, endLine: 12, text: 'first' },
      { ...quotes[0], note: '', startLine: 30, endLine: 31, text: 'second' },
    ]
    const result = buildMultiQuoteMessage('', sameFile)
    expect(result.indexOf('first')).toBeLessThan(result.indexOf('second'))
    expect(result).toContain('```ts:/a.ts:10-12')
    expect(result).toContain('```ts:/a.ts:30-31')
  })
})
