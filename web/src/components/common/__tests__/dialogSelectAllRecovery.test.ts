import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k }) }))

// Toggleable platform flag, so both the Android and the non-Android path can be
// exercised. `vi.hoisted` lets the mock factory below close over it.
const platform = vi.hoisted(() => ({ isAndroid: true }))

vi.mock('@/composables/usePlatformDetect', () => ({
  // A getter, not a constant: the component reads this on every event, so the
  // value must reflect the current test's platform.
  get isAndroidUA() { return platform.isAndroid },
  usePlatformDetect: () => ({ isPC: { value: false } }),
}))

import { useDialog } from '@/composables/useDialog'
import DialogOverlay from '@/components/common/DialogOverlay.vue'

describe('DialogOverlay Android select-all delete recovery', () => {
  let wrapper: VueWrapper<any> | null = null

  beforeEach(() => { platform.isAndroid = true; vi.useFakeTimers() })
  afterEach(() => {
    wrapper?.unmount(); wrapper = null
    const { state, resolve } = useDialog()
    if (state.value.visible) resolve(null)
    document.body.querySelectorAll('.dlg-overlay').forEach(el => el.remove())
    vi.useRealTimers()
  })

  function open(value: string) {
    const { state } = useDialog()
    state.value = {
      visible: true, type: 'prompt', title: 'T', message: 'M', value,
      placeholder: 'p', confirmText: '', cancelText: '', dangerous: false,
      extraText: '', extraPrimedText: '', onExtraAction: null, resolve: vi.fn(),
    } as any
    wrapper = mount(DialogOverlay, { attachTo: document.body })
  }

  function textarea(): HTMLTextAreaElement {
    return document.body.querySelector('.dlg-textarea') as HTMLTextAreaElement
  }

  function fireBeforeInput(ta: HTMLTextAreaElement, inputType: string, data: string | null) {
    const ev = new InputEvent('beforeinput', { bubbles: true, cancelable: true, inputType, data })
    Object.defineProperty(ev, 'isComposing', { value: false })
    ta.dispatchEvent(ev)
  }

  function applyEdit(ta: HTMLTextAreaElement, newValue: string) {
    ta.value = newValue
    ta.dispatchEvent(new Event('input', { bubbles: true }))
  }

  it('rebuilds the textarea element on the Android signature', async () => {
    open('OLDNAME')
    await nextTick(); await nextTick()

    const before = textarea()
    before.focus()
    before.select()

    fireBeforeInput(before, 'insertText', '')
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick(); await nextTick()

    // A brand-new element means a brand-new InputConnection — the whole point,
    // since the old connection is dead and cannot be revived in place.
    const after = textarea()
    expect(after).toBeTruthy()
    expect(after).not.toBe(before)
    expect(after.value).toBe('')
  })

  it('refocuses the rebuilt textarea so the user can keep typing', async () => {
    open('OLDNAME')
    await nextTick(); await nextTick()

    const before = textarea()
    before.focus()
    before.select()
    fireBeforeInput(before, 'insertText', '')
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick(); await nextTick()

    expect(document.activeElement).toBe(textarea())
  })

  it('keeps the typed value after the rebuild', async () => {
    open('OLDNAME')
    await nextTick(); await nextTick()

    const before = textarea()
    before.focus()
    before.select()
    fireBeforeInput(before, 'insertText', '')
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick(); await nextTick()

    // The user's next keystrokes land on the rebuilt element.
    const rebuilt = textarea()
    rebuilt.value = 'ceshi'
    rebuilt.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick(); await nextTick()

    expect(textarea().value).toBe('ceshi')
  })

  it('places the caret at the end of the rebuilt field', async () => {
    open('OLDNAME')
    await nextTick(); await nextTick()

    const before = textarea()
    before.focus()
    before.select()
    fireBeforeInput(before, 'insertText', '')
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick(); await nextTick()

    const rebuilt = textarea()
    // Without this the assertion below could pass on the untouched element.
    expect(rebuilt).not.toBe(before)
    // Recovery already collapsed the caret on the rebuilt element; the spy is
    // installed afterwards, so only the *state* is asserted here.
    expect(rebuilt.selectionStart).toBe(rebuilt.selectionEnd)
    expect(rebuilt.selectionStart).toBe(0)

    // Typing appends rather than replacing: the old whole-value selection must
    // not come back.
    rebuilt.value = 'ab'
    rebuilt.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick(); await nextTick()

    expect(rebuilt.selectionStart).toBe(rebuilt.selectionEnd)
  })

  it('end-to-end: deleting all then typing yields the new name on confirm', async () => {
    // The user-visible outcome the whole fix exists for: previously the typed
    // text never reached the dialog, so confirming returned null and the rename
    // silently did nothing.
    const resolveFn = vi.fn()
    const { state } = useDialog()
    state.value = {
      visible: true, type: 'prompt', title: 'T', message: 'M', value: 'OLDNAME',
      placeholder: 'p', confirmText: '', cancelText: '', dangerous: false,
      extraText: '', extraPrimedText: '', onExtraAction: null, resolve: resolveFn,
    } as any
    wrapper = mount(DialogOverlay, { attachTo: document.body })
    await nextTick(); await nextTick()

    const before = textarea()
    before.focus()
    before.select()

    // 1) Select-all delete, in the Android IME's shape.
    fireBeforeInput(before, 'insertText', '')
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick(); await nextTick()

    // 2) The user types the new name on the rebuilt field.
    const rebuilt = textarea()
    // Guard against a vacuous pass: jsdom has no dead-InputConnection bug, so
    // without this the flow would "succeed" even if recovery never ran.
    expect(rebuilt).not.toBe(before)
    applyEdit(rebuilt, 'ceshi')
    await nextTick(); await nextTick()

    // 3) Confirm.
    ;(document.body.querySelector('.dlg-ok') as HTMLElement).click()
    await nextTick()

    expect(resolveFn).toHaveBeenCalledWith('ceshi')
  })

  it('does not rebuild on a normal desktop delete', async () => {
    open('OLDNAME')
    await nextTick(); await nextTick()

    const before = textarea()
    before.focus()
    before.setSelectionRange(7, 7)

    fireBeforeInput(before, 'deleteContentBackward', null)
    applyEdit(before, 'OLDNAM')
    await nextTick(); await nextTick(); await nextTick()

    // Same element — desktop behaviour must stay untouched.
    expect(textarea()).toBe(before)
  })

  it('does not rebuild when typing a character over a selection', async () => {
    open('OLDNAME')
    await nextTick(); await nextTick()

    const before = textarea()
    before.focus()
    before.select()

    fireBeforeInput(before, 'insertText', 'x')
    applyEdit(before, 'x')
    await nextTick(); await nextTick(); await nextTick()

    expect(textarea()).toBe(before)
  })

  it('does not carry a detection across a dialog close/reopen', async () => {
    open('OLDNAME')
    await nextTick(); await nextTick()

    // A signature whose matching input event never arrives (the dialog closed
    // mid-edit) must not stay armed for the next dialog.
    const before = textarea()
    before.focus()
    before.select()
    fireBeforeInput(before, 'insertText', '')
    await nextTick()

    const { state } = useDialog()
    state.value.visible = false
    await nextTick(); await nextTick()
    state.value = { ...state.value, visible: true, value: 'OLD2', resolve: vi.fn() } as any
    await nextTick(); await nextTick()

    const reopened = textarea()
    applyEdit(reopened, 'OLD2x')
    await nextTick(); await nextTick(); await nextTick()

    expect(textarea()).toBe(reopened)
  })

  it('does NOT rebuild on non-Android even when the signature matches', async () => {
    // The rebuild costs a focus round-trip and the bug does not exist off
    // Android, so PC/desktop behaviour must stay exactly as it is today.
    platform.isAndroid = false
    open('OLDNAME')
    await nextTick(); await nextTick()

    const before = textarea()
    before.focus()
    before.select()

    fireBeforeInput(before, 'insertText', '')
    applyEdit(before, '')
    await nextTick(); await nextTick(); await nextTick(); await nextTick()

    expect(textarea()).toBe(before)
  })
})
