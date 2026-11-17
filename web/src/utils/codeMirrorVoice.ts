/**
 * CodeMirror as a dictation target.
 *
 * `voiceTarget` deliberately writes only through a `VoiceTargetHost`, because
 * reaching into a CodeMirror DOM node by hand loses to the editor's own state:
 * the next measurement pass puts the document back and the transcript is gone.
 * This adapter dispatches real transactions instead, so the text lands in the
 * document, the undo history and the dirty flag exactly as if it had been
 * typed.
 *
 * Structural, not a component: both editable CodeMirror surfaces in the app
 * (the file editor and the task script field) share it.
 */
import type { EditorView } from '@codemirror/view'
import type { VoiceTargetHost } from './voiceTarget'

function clamp(value: number, max: number): number {
  return Math.max(0, Math.min(value, max))
}

/**
 * Whether the view would silently drop a write right now.
 *
 * `EditorState.readOnly` is checked on every write, not only when the host is
 * created: `CodeMirrorViewer` flips it through a compartment when `editable`
 * goes false (a file gained a diff, the preview switched to read-only), and the
 * registered host keeps a reference to the live view — a host resolved while
 * the editor was writable outlives that. CodeMirror ignores a transaction from
 * a read-only state without throwing, so an unchecked `replace()` reported
 * success while nothing landed: the mic stayed on, no text appeared, and Esc
 * "undid" text that was never written.
 */
function viewRejectsWrites(view: EditorView): boolean {
  return !view.dom.isConnected || view.state.readOnly
}

/**
 * Wraps a live `EditorView` as a host.
 *
 * Only call this while the view accepts input — read-only views must not
 * resolve to a host at all (see `registerVoiceHost`'s factory). The host still
 * re-checks `state.readOnly` on every write: the view can become read-only
 * after the host was resolved.
 */
export function codeMirrorVoiceHost(view: EditorView): VoiceTargetHost {
  return {
    read: () => view.state.doc.toString(),
    range: () => {
      const sel = view.state.selection.main
      return { from: sel.from, to: sel.to }
    },
    focused: () => view.hasFocus,
    replace(from, to, text) {
      // A read-only view ignores transactions silently — answering `true` would
      // make voiceTarget believe the transcript landed (see viewRejectsWrites).
      if (viewRejectsWrites(view)) return false
      const length = view.state.doc.length
      const start = clamp(from, length)
      const end = Math.max(start, clamp(to, length))
      view.dispatch({
        changes: { from: start, to: end, insert: text },
        selection: { anchor: start + text.length },
        // Typing semantics, so history groups the streaming partials of one
        // dictation into a single undo step instead of dozens.
        userEvent: 'input.type',
      })
      return true
    },
    select(from, to) {
      if (viewRejectsWrites(view)) return
      const length = view.state.doc.length
      view.dispatch({ selection: { anchor: clamp(from, length), head: clamp(to, length) } })
    },
    alive: () => view.dom.isConnected,
    rect() {
      // Anchor the status pill to the caret, not to the editor box: a file
      // editor can fill the screen, and its top edge is nowhere near the line
      // being written.
      const coords = view.coordsAtPos(view.state.selection.main.head)
      if (!coords) return null
      return new DOMRect(coords.left, coords.top, Math.max(1, coords.right - coords.left), Math.max(1, coords.bottom - coords.top))
    },
  }
}
