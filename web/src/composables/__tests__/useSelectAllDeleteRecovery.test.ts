import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick, defineComponent, h, ref } from 'vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Toggleable platform flag so both the Android and the desktop path can be
// exercised. `vi.hoisted` lets the mock factory below close over it.
const platform = vi.hoisted(() => ({ isAndroid: true }))

vi.mock('@/composables/usePlatformDetect', () => ({
  // A getter, not a constant: the composable reads this on every event, so the
  // value must reflect the current test's platform.
  get isAndroidUA() { return platform.isAndroid },
  usePlatformDetect: () => ({ isPC: { value: false } }),
}))

import { useSelectAllDeleteRecovery } from '@/composables/useSelectAllDeleteRecovery'

describe('useSelectAllDeleteRecovery', () => {
  let wrapper: VueWrapper<any> | null = null

  beforeEach(() => { platform.isAndroid = true })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
  })

  /** Harness: a real textarea bound to the composable, plus a rebuild counter. */
  function mountHarness(initial = 'OLDNAME') {
    const rebuilt: HTMLTextAreaElement[] = []
    const Harness = defineComponent({
      setup() {
        const el = ref<HTMLTextAreaElement | null>(null)
        const recovery = useSelectAllDeleteRecovery({
          getElement: () => el.value,
          onRebuilt: (node) => rebuilt.push(node),
        })
        // `epoch` must be a TOP-LEVEL key of the setup return: Vue only unwraps
        // refs there, so nesting it inside `recovery` would leave the render
        // function reading the Ref object itself. Its identity never changes,
        // so the :key would never differ and no rebuild would happen.
        return { el, recovery, epoch: recovery.inputEpoch }
      },
      render() {
        return h('div', [
          h('textarea', {
            key: this.epoch,
            ref: (v: any) => { this.el = v },
            value: initial,
            onBeforeinput: (e: InputEvent) => this.recovery.onBeforeInput(e),
            onInput: () => this.recovery.onInput(),
          }),
        ])
      },
    })
    wrapper = mount(Harness, { attachTo: document.body })
    return { rebuilt }
  }

  function textarea(): HTMLTextAreaElement {
    return document.body.querySelector('textarea') as HTMLTextAreaElement
  }

  /** Dispatch the Android signature: empty insert over a live selection. */
  function fireSignature(ta: HTMLTextAreaElement, opts: { isComposing?: boolean } = {}) {
    const ev = new InputEvent('beforeinput', { bubbles: true, cancelable: true, inputType: 'insertText', data: '' })
    if (opts.isComposing) Object.defineProperty(ev, 'isComposing', { value: true })
    ta.dispatchEvent(ev)
  }

  /** Dispatch the `input` that follows, after applying the edit to the DOM. */
  function applyEdit(ta: HTMLTextAreaElement, newValue: string) {
    ta.value = newValue
    ta.dispatchEvent(new Event('input', { bubbles: true }))
  }

  it('rebuilds the textarea when the Android signature fires over a selection', async () => {
    mountHarness()
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    before.select()

    fireSignature(before)
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick()

    // A new element is the whole point: it is the only way to get a fresh
    // InputConnection, since the old one is dead and cannot be revived.
    expect(textarea()).not.toBe(before)
  })

  it('does not rebuild on desktop even when the signature matches', async () => {
    platform.isAndroid = false
    mountHarness()
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    before.select()

    fireSignature(before)
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick()

    expect(textarea()).toBe(before)
  })

  it('does not rebuild for a desktop delete', async () => {
    mountHarness()
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    before.setSelectionRange(7, 7)

    const ev = new InputEvent('beforeinput', { bubbles: true, cancelable: true, inputType: 'deleteContentBackward' })
    before.dispatchEvent(ev)
    applyEdit(before, 'OLDNAM')
    await nextTick(); await nextTick(); await nextTick()

    expect(textarea()).toBe(before)
  })

  it('does not rebuild on a collapsed caret (nothing was selected)', async () => {
    mountHarness()
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    before.setSelectionRange(7, 7)

    fireSignature(before)
    // Fire the matching input event too: the rebuild only happens from
    // onInput, so without it this case would pass even if the collapsed-caret
    // guard were removed.
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick()

    expect(textarea()).toBe(before)
  })

  it('does not rebuild while composing', async () => {
    // Defensive: composition arrives as insertCompositionText, which the
    // inputType check already rejects. This guards an IME emitting a bare
    // insertText at a composition boundary — rebuilding would tear the element
    // out from under the composition.
    mountHarness()
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    before.select()

    fireSignature(before, { isComposing: true })
    // Same reasoning as the collapsed-caret case: drive onInput as well.
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick()

    expect(textarea()).toBe(before)
  })

  it('defers the rebuild until the input event, not the beforeinput', async () => {
    // Load-bearing: touching the selection or replacing the element inside
    // `beforeinput` cancels the pending edit in a real browser, so the old text
    // is never deleted (verified with Chromium via CDP). jsdom does not
    // implement that cancellation, so this can only assert the ordering we
    // control — the element must survive its own beforeinput.
    mountHarness()
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    before.select()

    fireSignature(before)
    await nextTick(); await nextTick()

    // No `input` yet: the element must still be the original one.
    expect(textarea()).toBe(before)
  })

  it('rebuilds on the input event that follows the signature', async () => {
    mountHarness()
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    before.select()

    fireSignature(before)
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick()

    expect(textarea()).not.toBe(before)
  })

  it('restores focus and the caret where the replaced selection began', async () => {
    mountHarness('ABCDEFGH')
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    // A PARTIAL selection, not a select-all: the signature covers any
    // non-collapsed selection, and the caret must land at its start.
    before.setSelectionRange(2, 5)

    fireSignature(before)
    applyEdit(before, 'ABFGH')
    await nextTick(); await nextTick(); await nextTick()

    const rebuilt = textarea()
    expect(rebuilt).not.toBe(before)
    expect(document.activeElement).toBe(rebuilt)
    expect(rebuilt.selectionStart).toBe(2)
    expect(rebuilt.selectionEnd).toBe(2)
  })

  it('clamps the caret when the new value is shorter than the old caret position', async () => {
    mountHarness('ABCDEFGH')
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    // Selection at the very end: after deleting it the caret (8) is past the
    // end of the new value, so it must be clamped rather than throw.
    before.setSelectionRange(5, 8)

    fireSignature(before)
    applyEdit(before, 'ABCDE')
    await nextTick(); await nextTick(); await nextTick()

    const rebuilt = textarea()
    expect(rebuilt.selectionStart).toBe(5)
    expect(rebuilt.selectionEnd).toBe(5)
  })

  it('calls onRebuilt with the new element', async () => {
    const { rebuilt } = mountHarness()
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    before.select()

    fireSignature(before)
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick()

    expect(rebuilt).toHaveLength(1)
    expect(rebuilt[0]).toBe(textarea())
    expect(rebuilt[0]).not.toBe(before)
  })

  it('reset() drops a detection whose input event never arrived', async () => {
    mountHarness()
    await nextTick(); await nextTick()
    const before = textarea()
    before.focus()
    before.select()

    // Signature seen, but the dialog/session went away before the matching
    // input event — a stale flag must not rebuild on a later keystroke.
    fireSignature(before)
    ;(wrapper!.vm as any).recovery.reset()
    await nextTick()

    const ev = new InputEvent('beforeinput', { bubbles: true, cancelable: true, inputType: 'insertText', data: 'x' })
    before.dispatchEvent(ev)
    applyEdit(before, 'x')
    await nextTick(); await nextTick(); await nextTick()

    expect(textarea()).toBe(before)
  })
})
