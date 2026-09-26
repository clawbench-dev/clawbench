import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { installInertPathClick } from '@/utils/inertPathClick'
import {
  inertPathPickerState,
  _resetInertPathPickerForTesting,
} from '@/composables/useInertPathPicker'
import { setShareToken } from '@/share/shareMode'

/**
 * The click layer is the single interception point for inert path chips. These
 * tests drive a real document-level listener with real DOM, because the whole
 * point of the design is the ordering/capture semantics — a unit test that calls
 * the handler directly would not prove the listener is wired at all.
 */
let dispose: (() => void) | null = null

beforeEach(() => {
  _resetInertPathPickerForTesting()
  setShareToken(null)
  document.body.innerHTML = ''
})

afterEach(() => {
  dispose?.()
  dispose = null
  setShareToken(null)
  document.body.innerHTML = ''
})

/** Build a verified-missing chip inside a chat bubble and click it. */
function clickChip(html: string, opts: MouseEventInit = {}): HTMLElement {
  const host = document.createElement('div')
  host.innerHTML = html
  document.body.appendChild(host)
  const chip = host.querySelector<HTMLElement>('.chat-file-path-inert')!
  chip.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0, detail: 1, ...opts }))
  return chip
}

describe('installInertPathClick', () => {
  it('opens the picker for a verified-missing chip and carries its line target', () => {
    dispose = installInertPathClick()

    clickChip(
      '<div class="chat-message assistant">'
      + '<span class="chat-file-path chat-file-path-inert" data-file-path="internal/service/chat_history.go"'
      + ' data-path-type="none" data-inert-line-start="42" data-inert-line-end="48">chat_history.go</span>'
      + '</div>',
    )

    expect(inertPathPickerState.open).toBe(true)
    expect(inertPathPickerState.query).toBe('chat_history.go')
    expect(inertPathPickerState.lineStart).toBe(42)
    expect(inertPathPickerState.lineEnd).toBe(48)
    expect(inertPathPickerState.source).toBe('chat')
  })

  it('reads a stashed multi-range target', () => {
    dispose = installInertPathClick()
    clickChip(
      '<div class="chat-message assistant">'
      + '<span class="chat-file-path chat-file-path-inert" data-file-path="src/main.go"'
      + ' data-path-type="none" data-inert-line-ranges="90-91,309">main.go</span>'
      + '</div>',
    )
    expect(inertPathPickerState.lineRanges).toBe('90-91,309')
    expect(inertPathPickerState.lineStart).toBe(90)
    expect(inertPathPickerState.lineEnd).toBe(91)
  })

  it('ignores a glob-pattern chip (no data-file-path — nothing to search for)', () => {
    // markInertLink fires BEFORE the annotation class is added, so a glob chip
    // is a bare <a class="chat-file-path-inert"> with no data-file-path. It has
    // no filename, so it must stay non-interactive.
    //
    // Asserting only `state.open === false` would be a false guard: the click
    // must not even be CONSUMED (no preventDefault), so the chip keeps behaving
    // as a plain text element rather than a silently-swallowed one.
    dispose = installInertPathClick()

    const host = document.createElement('div')
    host.innerHTML = '<div class="chat-message assistant">'
      + '<a class="chat-file-path-inert" data-path-type="none" data-inert-href="src/*.go" title="glob">source files</a>'
      + '</div>'
    document.body.appendChild(host)
    const link = host.querySelector('a')!
    const evt = new MouseEvent('click', { bubbles: true, cancelable: true, detail: 1 })
    link.dispatchEvent(evt)

    expect(inertPathPickerState.open).toBe(false)
    expect(inertPathPickerState.query).toBe('')
    expect(evt.defaultPrevented, 'the glob chip must not be treated as handled').toBe(false)
  })

  it('ignores a live (verified) path chip', () => {
    dispose = installInertPathClick()
    const host = document.createElement('div')
    host.innerHTML = '<div class="chat-message assistant">'
      + '<span class="chat-file-path" data-file-path="src/main.go" data-path-type="file">main.go</span>'
      + '</div>'
    document.body.appendChild(host)
    host.querySelector('span')!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, detail: 1 }))

    expect(inertPathPickerState.open).toBe(false)
  })

  it('honors defaultPrevented so a drag-select does not open the panel', () => {
    // dragClickGuard (installed first, see App.vue) swallows drag clicks with
    // preventDefault. The same node also receives this listener, so the flag is
    // the only signal that the click was a text selection.
    dispose = installInertPathClick()

    const host = document.createElement('div')
    host.innerHTML = '<div class="chat-message assistant">'
      + '<span class="chat-file-path chat-file-path-inert" data-file-path="src/gone.go" data-path-type="none">gone.go</span>'
      + '</div>'
    document.body.appendChild(host)
    const chip = host.querySelector<HTMLElement>('span')!

    const drag = new MouseEvent('click', { bubbles: true, cancelable: true, detail: 1 })
    drag.preventDefault()
    chip.dispatchEvent(drag)

    expect(inertPathPickerState.open).toBe(false)
  })

  it('does nothing in share mode (anonymous viewer must not probe the project)', () => {
    dispose = installInertPathClick()
    setShareToken('tok-123')

    clickChip(
      '<span class="chat-file-path chat-file-path-inert" data-file-path="src/gone.go" data-path-type="none">gone.go</span>',
    )

    expect(inertPathPickerState.open).toBe(false)
  })

  it('ignores modified clicks so they keep their usual meaning', () => {
    dispose = installInertPathClick()
    clickChip(
      '<span class="chat-file-path chat-file-path-inert" data-file-path="src/gone.go" data-path-type="none">gone.go</span>',
      { ctrlKey: true },
    )
    expect(inertPathPickerState.open).toBe(false)
  })

  it('suppresses the click before host handlers run (capture phase + stopPropagation)', () => {
    // The two ungated hosts (ToolDetailDrawer / FileDiffsDrawer) match a bare
    // `.chat-file-path` and would emit a file-open for a missing file. A
    // capture-phase listener that stops propagation prevents that entirely.
    dispose = installInertPathClick()

    const hostReached = vi.fn()
    document.body.addEventListener('click', hostReached)

    clickChip(
      '<span class="chat-file-path chat-file-path-inert" data-file-path="src/gone.go" data-path-type="none">gone.go</span>',
    )

    expect(inertPathPickerState.open).toBe(true)
    expect(hostReached).not.toHaveBeenCalled()

    document.body.removeEventListener('click', hostReached)
  })

  it('preventDefaults the handled click so the browser does not navigate', () => {
    dispose = installInertPathClick()
    const chip = clickChip(
      '<span class="chat-file-path chat-file-path-inert" data-file-path="src/gone.go" data-path-type="none">gone.go</span>',
    )
    // Re-dispatch a fresh event to observe the flag on the event object itself.
    const probe = new MouseEvent('click', { bubbles: true, cancelable: true, detail: 1 })
    chip.dispatchEvent(probe)
    expect(probe.defaultPrevented).toBe(true)
  })

  it('stops responding after disposal', () => {
    const d = installInertPathClick()
    d()

    clickChip(
      '<span class="chat-file-path chat-file-path-inert" data-file-path="src/gone.go" data-path-type="none">gone.go</span>',
    )

    expect(inertPathPickerState.open).toBe(false)
  })
})
