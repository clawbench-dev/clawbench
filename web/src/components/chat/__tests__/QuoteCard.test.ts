import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import QuoteCard from '../QuoteCard.vue'
import type { QuoteItem } from '@/utils/quoteItem'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

function quote(over: Partial<QuoteItem> = {}): QuoteItem {
  return {
    id: 'q1', text: 'x := 1', note: '', filePath: 'src/a.go', language: 'go',
    startLine: 10, endLine: 20, sourceKind: 'file', ...over,
  }
}

function mountCard(props: Record<string, unknown> = {}) {
  return mount(QuoteCard, { props: { quote: quote(), ...props } })
}

describe('QuoteCard', () => {
  // The card deliberately does NOT render the quoted text. It is a compact chip
  // in a single-line, nowrap row shared by the input and the sent bubble, where
  // the content does not fit — the full text lives in the detail drawer, and the
  // tooltip carries it for an unlabelled quote.
  it('does not render the quoted text on the card', () => {
    const wrapper = mountCard({ quote: quote({ text: 'func main() {}' }) })

    expect(wrapper.text()).not.toContain('func main() {}')
  })

  it('carries the shared classes both surfaces style against', () => {
    // chat-file-attachment / attachment-quote are what make the global rules in
    // ChatMessageItem.vue (sent bubble) and the scoped rules in ChatInputBar.vue
    // (input) both apply. Renaming them silently un-styles a surface.
    const wrapper = mountCard()
    expect(wrapper.classes()).toContain('chat-file-attachment')
    expect(wrapper.classes()).toContain('attachment-quote')
  })

  it('shows the filename with its line range', () => {
    const wrapper = mountCard()
    expect(wrapper.find('.attachment-filename').text()).toBe('a.go:10-20')
  })

  it('shows a single line number for a one-line quote', () => {
    const wrapper = mountCard({ quote: quote({ startLine: 7, endLine: 7 }) })
    expect(wrapper.find('.attachment-filename').text()).toBe('a.go:7')
  })

  it('shows no line range when there is none', () => {
    const wrapper = mountCard({
      quote: quote({ sourceKind: 'url', filePath: 'acme/widgets#7', startLine: 0, endLine: 0 }),
    })
    expect(wrapper.find('.attachment-filename').text()).toBe('acme/widgets#7')
  })

  it('shows the annotation indicator only when a note exists', () => {
    expect(mountCard({ quote: quote({ note: 'why?' }) }).find('.attachment-quote-note-icon').exists()).toBe(true)
    expect(mountCard({ quote: quote({ note: '' }) }).find('.attachment-quote-note-icon').exists()).toBe(false)
  })

  it('uses the note as the tooltip when present', () => {
    const wrapper = mountCard({ quote: quote({ note: 'why?' }) })
    expect(wrapper.attributes('title')).toBe('why?')
  })

  it('falls back to the path, then the text, for the tooltip', () => {
    expect(mountCard({ quote: quote({ filePath: 'src/a.go' }) }).attributes('title')).toBe('src/a.go')
    // A chat quote has no path — the text is the only identifying thing.
    expect(mountCard({
      quote: quote({ sourceKind: 'message', filePath: '', text: 'chat text' }),
    }).attributes('title')).toBe('chat text')
  })

  it('emits click with the quote', async () => {
    const q = quote()
    const wrapper = mountCard({ quote: q })

    await wrapper.trigger('click')

    expect(wrapper.emitted('click')![0]).toEqual([q])
  })

  it('renders no remove button unless removable', () => {
    expect(mountCard().find('.attachment-close-btn').exists()).toBe(false)
    expect(mountCard({ removable: true }).find('.attachment-close-btn').exists()).toBe(true)
  })

  it('emits remove without also emitting click', async () => {
    // The close button sits inside the clickable card, so it must stop the
    // click — otherwise removing a chip would also open the drawer.
    const q = quote()
    const wrapper = mountCard({ quote: q, removable: true })

    await wrapper.find('.attachment-close-btn').trigger('click')

    expect(wrapper.emitted('remove')![0]).toEqual([q])
    expect(wrapper.emitted('click')).toBeFalsy()
  })
})

describe('QuoteCard — whole-object quote (no content)', () => {
  it('renders a file reference with no line range', () => {
    // What the entry-point buttons produce: a card naming the file, with no
    // content and no line info.
    const wrapper = mountCard({
      quote: quote({ text: '', filePath: 'src/main.ts', startLine: 0, endLine: 0, note: 'explain this' }),
    })

    expect(wrapper.find('.attachment-filename').text()).toBe('main.ts')
    expect(wrapper.attributes('title')).toBe('explain this')
  })

  it('renders a forge reference with its label', () => {
    const wrapper = mountCard({
      quote: quote({
        text: '', filePath: 'acme/widgets#7', startLine: 0, endLine: 0,
        url: 'https://github.com/acme/widgets/issues/7', sourceKind: 'url',
      }),
    })

    expect(wrapper.find('.attachment-filename').text()).toBe('acme/widgets#7')
  })
})
