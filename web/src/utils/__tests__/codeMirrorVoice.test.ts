import { describe, expect, it } from 'vitest'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { codeMirrorVoiceHost } from '@/utils/codeMirrorVoice'
import { writeVoiceText, type VoiceTarget } from '@/utils/voiceTarget'

/**
 * The dictation adapter must not claim a write it did not make.
 *
 * `CodeMirrorViewer` resolves a host once (on mount) and re-checks `editable`
 * only inside that factory. Flipping the editor to read-only afterwards (the
 * file gained a diff, the preview switched mode) leaves the old host holding a
 * live `EditorView` whose state is read-only. CodeMirror then IGNORES the
 * transaction without throwing, so an unchecked `replace()` returned true while
 * nothing landed: the mic stayed on, no text appeared, and Esc "removed" text
 * that was never there.
 */
function makeView(doc: string, readOnly = false) {
  const parent = document.createElement('div')
  document.body.appendChild(parent)
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc,
      extensions: readOnly ? [EditorState.readOnly.of(true)] : [],
    }),
  })
  return view
}

function makeTarget(view: EditorView, start = 0): VoiceTarget {
  return {
    el: view.dom,
    host: codeMirrorVoiceHost(view),
    start,
    inserted: 0,
    applied: '',
    replaced: '',
    hasWritten: false,
  }
}

describe('codeMirrorVoiceHost', () => {
  it('writes into a writable view', () => {
    const view = makeView('hello')
    const target = makeTarget(view, 5)
    writeVoiceText(target, ' world')
    expect(view.state.doc.toString()).toBe('hello world')
    expect(target.hasWritten).toBe(true)
    view.destroy()
  })

  it('refuses to write once the view has been switched to read-only', () => {
    const view = makeView('hello')
    const host = codeMirrorVoiceHost(view)
    const start = view.state.doc.length
    // Same host object, editor reconfigured after the host was resolved.
    view.setState(EditorState.create({ doc: 'hello', extensions: [EditorState.readOnly.of(true)] }))
    expect(host.replace(start, start, ' world')).toBe(false)
    expect(view.state.doc.toString()).toBe('hello')
    view.destroy()
  })

  it('leaves the target untouched when the write is refused', () => {
    // voiceTarget's contract: a false from `replace` means "nothing happened",
    // so the engine keeps the text as a capsule instead of believing it landed.
    const view = makeView('hello')
    view.setState(EditorState.create({ doc: 'hello', extensions: [EditorState.readOnly.of(true)] }))
    const target = makeTarget(view, 5)
    writeVoiceText(target, ' world')
    expect(target.hasWritten).toBe(false)
    expect(target.inserted).toBe(0)
    expect(view.state.doc.toString()).toBe('hello')
    view.destroy()
  })

  it('does not move the selection in a read-only view', () => {
    // `select()` used to dispatch unconditionally: growing the transaction
    // count on a view the user cannot edit served no purpose.
    const view = makeView('hello', true)
    const host = codeMirrorVoiceHost(view)
    const before = view.state.selection.main.anchor
    host.select(0, 5)
    expect(view.state.selection.main.anchor).toBe(before)
    view.destroy()
  })

  it('refuses writes to a detached view', () => {
    const view = makeView('hello')
    const host = codeMirrorVoiceHost(view)
    view.destroy()
    expect(host.replace(0, 0, 'x')).toBe(false)
  })
})
