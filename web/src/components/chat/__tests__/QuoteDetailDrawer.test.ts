import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import QuoteDetailDrawer from '../QuoteDetailDrawer.vue'
import type { QuoteItem } from '@/utils/quoteItem'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// BottomSheet teleports to <body> and gates on `everOpened`; stub it with a
// transparent wrapper so the drawer's own content is assertable in place.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    props: ['open', 'auto'],
    template: '<div v-if="open" class="bs-stub"><slot name="header" /><slot /><slot name="footer" /></div>',
  },
}))

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<span class="li-stub" />' },
}))

function fileQuote(over: Partial<QuoteItem> = {}): QuoteItem {
  return {
    id: 'q1', text: 'x := 1', note: '', filePath: 'src/a.go', language: 'go',
    startLine: 10, endLine: 20, sourceKind: 'file', ...over,
  }
}

function mountDrawer(props: Record<string, unknown> = {}) {
  return mount(QuoteDetailDrawer, {
    props: { open: true, quote: fileQuote(), saving: false, ...props },
  })
}

describe('QuoteDetailDrawer', () => {
  it('renders the quoted content verbatim', () => {
    const wrapper = mountDrawer({ quote: fileQuote({ text: 'func main() {}' }) })
    expect(wrapper.find('.qd-quoted-text').text()).toBe('func main() {}')
  })

  // A whole-object quote ("quote this file") carries only a label. The empty
  // <pre> read as a rendering bug rather than as "this references the object",
  // so the whole section is omitted.
  it('omits the quoted-content section for a whole-object quote', () => {
    const wrapper = mountDrawer({
      quote: fileQuote({ text: '', startLine: 0, endLine: 0 }),
    })

    expect(wrapper.find('.qd-quoted-text').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('quoteBar.quotedContent')
    // The rest of the drawer still works: the source and the annotation.
    expect(wrapper.find('.qd-source').text()).toContain('a.go')
    expect(wrapper.find('.qd-note-input').exists()).toBe(true)
  })

  it('labels a free-form selection quote generically', () => {
    const wrapper = mountDrawer({
      quote: fileQuote({
        text: 'npm run build', filePath: '', startLine: 0, endLine: 0,
        sourceKind: 'selection',
      }),
    })

    // Without the 'selection' kind it would be inferred as a chat message.
    expect(wrapper.find('.qd-source').text()).toContain('quoteBar.selectionQuote')
    expect(wrapper.find('.qd-source').text()).not.toContain('quoteBar.messageQuote')
    // No source to open, so no jump button.
    expect(wrapper.find('.qd-jump').exists()).toBe(false)
  })

  it('shows the source label with its line range', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.qd-source').text()).toContain('a.go:10-20')
  })

  it('seeds the annotation field from the quote', () => {
    const wrapper = mountDrawer({ quote: fileQuote({ note: 'existing note' }) })
    expect((wrapper.find('.qd-note-input').element as HTMLTextAreaElement).value).toBe('existing note')
  })

  it('offers a jump button for a file quote', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.qd-jump').exists()).toBe(true)
  })

  it('emits jump with the quote when the jump button is clicked', async () => {
    const quote = fileQuote()
    const wrapper = mountDrawer({ quote })

    await wrapper.find('.qd-jump').trigger('click')

    expect(wrapper.emitted('jump')![0]).toEqual([quote])
  })

  it('hides the jump button for a chat-message quote', () => {
    // Nothing to open: the button would be a no-op.
    const wrapper = mountDrawer({
      quote: fileQuote({ sourceKind: 'message', filePath: '', startLine: 0, endLine: 0, text: 'chat text' }),
    })

    expect(wrapper.find('.qd-jump').exists()).toBe(false)
    expect(wrapper.find('.qd-source').text()).toContain('quoteBar.messageQuote')
  })

  it('offers a jump button for a forge quote with an address', () => {
    const wrapper = mountDrawer({
      quote: fileQuote({
        sourceKind: 'url', filePath: 'acme/widgets#7', url: 'https://github.com/acme/widgets/issues/7',
        startLine: 0, endLine: 0,
      }),
    })

    expect(wrapper.find('.qd-jump').exists()).toBe(true)
    expect(wrapper.find('.qd-source').text()).toContain('acme/widgets#7')
  })

  describe('saving', () => {
    it('disables save until the annotation actually changes', async () => {
      const wrapper = mountDrawer({ quote: fileQuote({ note: 'original' }) })
      const saveBtn = wrapper.find('.fbtn-primary')

      expect(saveBtn.attributes('disabled')).toBeDefined()

      await wrapper.find('.qd-note-input').setValue('changed')
      expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeUndefined()
    })

    it('emits save with the edited note', async () => {
      const wrapper = mountDrawer({ quote: fileQuote({ note: 'original' }) })

      await wrapper.find('.qd-note-input').setValue('updated')
      await wrapper.find('.fbtn-primary').trigger('click')

      expect(wrapper.emitted('save')).toEqual([['updated']])
    })

    it('does not emit save when the note is unchanged', async () => {
      const wrapper = mountDrawer({ quote: fileQuote({ note: 'original' }) })

      await wrapper.find('.fbtn-primary').trigger('click')

      expect(wrapper.emitted('save')).toBeFalsy()
    })

    it('disables save while a save is in flight', async () => {
      const wrapper = mountDrawer({ quote: fileQuote({ note: 'original' }), saving: true })

      await wrapper.find('.qd-note-input').setValue('updated')

      expect(wrapper.find('.fbtn-primary').attributes('disabled')).toBeDefined()
    })

    it('emits close from the cancel button', async () => {
      const wrapper = mountDrawer()
      await wrapper.find('.fbtn').trigger('click')
      expect(wrapper.emitted('close')).toBeTruthy()
    })
  })

  it('resets the draft when reopened with a different quote', async () => {
    const wrapper = mountDrawer({ quote: fileQuote({ id: 'q1', note: 'first' }) })

    await wrapper.find('.qd-note-input').setValue('typed but not saved')
    await wrapper.setProps({ quote: fileQuote({ id: 'q2', note: 'second' }) })

    expect((wrapper.find('.qd-note-input').element as HTMLTextAreaElement).value).toBe('second')
  })
})
