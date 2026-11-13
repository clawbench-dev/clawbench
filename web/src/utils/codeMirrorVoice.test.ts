import { describe, it, expect, vi } from 'vitest'
import type { EditorView } from '@codemirror/view'
import { codeMirrorVoiceHost } from './codeMirrorVoice'

/**
 * The adapter is what makes dictation survive CodeMirror's own rendering pass:
 * text must go in through a transaction, never through the DOM. These tests
 * drive it with a minimal stand-in for `EditorView` so every branch (clamping,
 * selection, caret coordinates) is exercised without a layout engine.
 */
function fakeView(initial = 'hello') {
  let doc = initial
  let selection = { from: 0, to: 0, head: 0 }
  const coords = vi.fn(() => ({ left: 10, right: 12, top: 20, bottom: 36 }) as { left: number; right: number; top: number; bottom: number } | null)
  const dispatch = vi.fn((spec: {
    changes?: { from: number; to: number; insert: string }
    selection?: { anchor: number; head?: number }
  }) => {
    if (spec.changes) {
      const { from, to, insert } = spec.changes
      doc = doc.slice(0, from) + insert + doc.slice(to)
      const at = from + insert.length
      selection = { from: at, to: at, head: at }
    }
    if (spec.selection) {
      const head = spec.selection.head ?? spec.selection.anchor
      selection = {
        from: Math.min(spec.selection.anchor, head),
        to: Math.max(spec.selection.anchor, head),
        head,
      }
    }
  })
  const dom = document.createElement('div')
  document.body.appendChild(dom)
  const view = {
    get state() {
      return {
        doc: { toString: () => doc, length: doc.length },
        selection: { main: selection },
      }
    },
    hasFocus: true,
    dom,
    dispatch,
    coordsAtPos: coords,
  }
  return {
    view: view as unknown as EditorView,
    doc: () => doc,
    selection: () => selection,
    dispatch,
    coords,
  }
}

describe('codeMirrorVoiceHost', () => {
  it('reads the document and the current selection', () => {
    const fake = fakeView('abc')
    const host = codeMirrorVoiceHost(fake.view)
    expect(host.read()).toBe('abc')
    expect(host.range()).toEqual({ from: 0, to: 0 })
  })

  it('inserts through a transaction and moves the caret after it', () => {
    const fake = fakeView('ab')
    const host = codeMirrorVoiceHost(fake.view)
    expect(host.replace(2, 2, 'cd')).toBe(true)
    expect(fake.doc()).toBe('abcd')
    expect(fake.selection()).toMatchObject({ from: 4, head: 4 })
    // Typing semantics keep one dictation in one undo group.
    expect(fake.dispatch.mock.calls[0][0]).toMatchObject({ userEvent: 'input.type' })
  })

  it('clamps offsets from a stale caret instead of throwing', () => {
    const fake = fakeView('ab')
    const host = codeMirrorVoiceHost(fake.view)
    expect(host.replace(-5, 99, 'x')).toBe(true)
    expect(fake.doc()).toBe('x')
  })

  it('restores a selection on cancel', () => {
    const fake = fakeView('keep this')
    const host = codeMirrorVoiceHost(fake.view)
    host.select(5, 9)
    expect(fake.selection()).toEqual({ from: 5, to: 9, head: 9 })
  })

  it('reports the caret box for the status pill, and nothing when unmeasured', () => {
    const fake = fakeView('abc')
    const host = codeMirrorVoiceHost(fake.view)
    const rect = host.rect!()
    expect(rect).toBeInstanceOf(DOMRect)
    expect(rect!.top).toBe(20)
    expect(rect!.height).toBe(16)
    expect(fake.coords).toHaveBeenCalledWith(0)

    fake.coords.mockReturnValueOnce(null)
    expect(host.rect!()).toBeNull()
  })

  it('reports itself dead once the editor leaves the document', () => {
    const fake = fakeView('abc')
    const host = codeMirrorVoiceHost(fake.view)
    expect(host.alive()).toBe(true)
    fake.view.dom.remove()
    expect(host.alive()).toBe(false)
  })
})
