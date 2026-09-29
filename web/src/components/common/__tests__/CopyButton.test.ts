import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import CopyButton from '../CopyButton.vue'

const copyTextMock = vi.fn()
// Forward exactly the arguments the component passes (do not pad the optional
// error callback), so assertions see the real call shape.
vi.mock('@/utils/clipboard', () => ({
  copyText: (...args: unknown[]) => copyTextMock(...args),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { common: { copy: 'Copy', copied: 'Copied' } } },
})

function mountBtn(props: Record<string, unknown> = {}) {
  return mount(CopyButton, {
    props: { text: 'hello', ...props },
    global: { plugins: [i18n] },
  })
}

/** Make copyText succeed synchronously, as the real one does on the fast path. */
function succeed() {
  copyTextMock.mockImplementation((_t: string, ok?: () => void) => ok?.())
}

describe('CopyButton', () => {
  beforeEach(() => {
    copyTextMock.mockReset()
    succeed()
  })

  it('renders the copy glyph idle and no text', () => {
    const w = mountBtn()
    expect(w.find('.lucide-copy').exists()).toBe(true)
    expect(w.find('.lucide-check').exists()).toBe(false)
    expect(w.text()).toBe('')
  })

  it('copies the given text and swaps to a check', async () => {
    const w = mountBtn({ text: 'payload' })
    await w.trigger('click')

    // `copyText(text, onSuccess?, onError?)` — the component only passes the
    // success callback (failures simply show no feedback).
    expect(copyTextMock).toHaveBeenCalledWith('payload', expect.any(Function))
    expect(w.find('.lucide-check').exists()).toBe(true)
    expect(w.find('.lucide-copy').exists()).toBe(false)
    expect(w.classes()).toContain('is-copied')
  })

  it('keeps the button free of text in the copied state', async () => {
    // The whole point: feedback is a glyph swap, never a label. A label would
    // change the button's width and shift/cover its neighbours.
    const w = mountBtn()
    await w.trigger('click')
    expect(w.text()).toBe('')
  })

  it('labels the button via i18n in both states', async () => {
    const w = mountBtn({ titleKey: 'common.copy', copiedKey: 'common.copied' })
    expect(w.attributes('title')).toBe('Copy')
    expect(w.attributes('aria-label')).toBe('Copy')

    await w.trigger('click')
    expect(w.attributes('title')).toBe('Copied')
    expect(w.attributes('aria-label')).toBe('Copied')
  })

  it('reverts to the copy glyph after the duration', async () => {
    vi.useFakeTimers()
    try {
      const w = mountBtn({ duration: 1500 })
      await w.trigger('click')
      expect(w.find('.lucide-check').exists()).toBe(true)

      vi.advanceTimersByTime(1500)
      await nextTick()
      expect(w.find('.lucide-check').exists()).toBe(false)
      expect(w.find('.lucide-copy').exists()).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })

  it('ignores a second click while the check is showing', async () => {
    // Without the guard the revert would capture the "copied" label as the
    // idle one and the button would stay stuck on it.
    const w = mountBtn()
    await w.trigger('click')
    const callsAfterFirst = copyTextMock.mock.calls.length
    await w.trigger('click')
    expect(copyTextMock.mock.calls.length).toBe(callsAfterFirst)
  })

  it('does not show the check when the copy fails', async () => {
    // A failed copy never invokes the success callback, so no feedback appears.
    copyTextMock.mockImplementation(() => {})
    const w = mountBtn()
    await w.trigger('click')

    expect(w.find('.lucide-check').exists()).toBe(false)
    expect(w.classes()).not.toContain('is-copied')
  })

  it('does nothing for empty text — empty means "nothing to copy"', async () => {
    // Callers rely on this: a message with no copyable text must not flash a
    // success the user cannot see the result of.
    const w = mountBtn({ text: '' })
    await w.trigger('click')
    expect(copyTextMock).not.toHaveBeenCalled()
    expect(w.classes()).not.toContain('is-copied')
  })

  it('emits click on every press', async () => {
    const w = mountBtn()
    await w.trigger('click')
    await w.trigger('click')
    expect(w.emitted('click')).toHaveLength(2)
  })

  describe('controlled mode', () => {
    it('mirrors the `copied` prop instead of self-managing', async () => {
      const w = mountBtn({ copied: false, text: undefined })
      expect(w.find('.lucide-copy').exists()).toBe(true)

      await w.setProps({ copied: true })
      expect(w.find('.lucide-check').exists()).toBe(true)
      expect(w.classes()).toContain('is-copied')
    })

    it('does not touch the clipboard itself', async () => {
      // The host owns the write in this mode (it may need custom formatting).
      const w = mountBtn({ copied: false, text: 'should-not-be-copied' })
      await w.trigger('click')
      expect(copyTextMock).not.toHaveBeenCalled()
      expect(w.emitted('click')).toHaveLength(1)
    })

    it('treats an explicit `copied: false` as controlled, not absent', async () => {
      const w = mountBtn({ copied: false })
      await w.trigger('click')
      expect(copyTextMock).not.toHaveBeenCalled()
    })
  })

  it('passes host attributes through to the button element', () => {
    const w = mountBtn({ class: 'host-class', 'data-tooltip': 'tip' })
    const btn = w.find('button')
    expect(btn.classes()).toContain('host-class')
    expect(btn.attributes('data-tooltip')).toBe('tip')
  })

  it('clears its timer on unmount so it cannot fire into a dead component', async () => {
    vi.useFakeTimers()
    try {
      const w = mountBtn()
      await w.trigger('click')
      w.unmount()
      // Should not throw / warn about updating an unmounted component.
      expect(() => vi.advanceTimersByTime(5000)).not.toThrow()
    } finally {
      vi.useRealTimers()
    }
  })
})
