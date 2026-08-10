import { describe, it, expect, vi, beforeEach } from 'vitest'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { selectionToQuote, type SelectionLike } from '@/utils/codeSelectionQuote.ts'

// Build a real EditorView whose contentDOM is attached to the document so
// posAtDOM() can resolve DOM nodes to document offsets.
function makeView(doc: string): EditorView {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const view = new EditorView({ parent: host, state: EditorState.create({ doc }) })
  return view
}

function fakeSel(overrides: Partial<SelectionLike>): SelectionLike {
  return {
    anchorNode: null,
    anchorOffset: 0,
    focusNode: null,
    focusOffset: 0,
    isCollapsed: false,
    toString: () => '',
    ...overrides,
  }
}

const source = { filePath: '/p/main.go', language: 'go' }

describe('selectionToQuote', () => {
  let view: EditorView

  beforeEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
    view = makeView('line one\nline two\nline three')
  })

  it('returns null for a null selection', () => {
    expect(selectionToQuote(null, view, source)).toBeNull()
  })

  it('returns null for a collapsed selection', () => {
    expect(selectionToQuote(fakeSel({ isCollapsed: true }), view, source)).toBeNull()
  })

  it('returns null when the selection is outside the editor content', () => {
    const outside = document.createElement('div')
    document.body.appendChild(outside)
    const sel = fakeSel({
      anchorNode: outside,
      focusNode: outside,
      toString: () => 'text',
    })
    expect(selectionToQuote(sel, view, source)).toBeNull()
  })

  it('returns null for an empty selection', () => {
    const sel = fakeSel({ anchorNode: view.contentDOM, focusNode: view.contentDOM, toString: () => '   ' })
    expect(selectionToQuote(sel, view, source)).toBeNull()
  })

  it('maps a selection spanning the whole doc to lines 1..3 with text', () => {
    const sel = fakeSel({
      anchorNode: view.contentDOM,
      anchorOffset: 0,
      focusNode: view.contentDOM,
      focusOffset: view.contentDOM.childNodes.length,
      toString: () => 'line one\nline two\nline three',
    })
    expect(selectionToQuote(sel, view, source)).toEqual({
      text: 'line one\nline two\nline three',
      filePath: '/p/main.go',
      language: 'go',
      startLine: 1,
      endLine: 3,
    })
  })

  it('resolves a real DOM selection to accurate line numbers', () => {
    // Select the second line's text node only.
    const lines = Array.from(view.contentDOM.querySelectorAll('.cm-line'))
    const secondLine = lines[1] as HTMLElement
    const textNode = Array.from(secondLine.childNodes).find(n => n.nodeType === Node.TEXT_NODE) as Text
    const sel = fakeSel({
      anchorNode: textNode,
      anchorOffset: 0,
      focusNode: textNode,
      focusOffset: textNode.length,
      toString: () => textNode.textContent ?? '',
    })
    expect(selectionToQuote(sel, view, source)).toMatchObject({ startLine: 2, endLine: 2 })
  })
})
