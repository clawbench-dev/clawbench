import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import QuoteQuestionBar from '../QuoteQuestionBar.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/utils/clipboard.ts', () => ({
  copyText: vi.fn().mockResolvedValue(true),
}))

const QUOTE = { text: 'build failed on main', filePath: 'acme/widgets#7', language: 'issue', startLine: 0, endLine: 0 }

function mountBar(props: Record<string, unknown> = {}) {
  return mount(QuoteQuestionBar, {
    props: { visible: true, quoteData: null, composerMode: false, ...props },
  })
}

describe('QuoteQuestionBar', () => {
  describe('composer mode (no quote yet)', () => {
    it('renders the input even though there is no quote', () => {
      // Regression: the render gate was `visible && quoteData`, so a composer
      // opened with no selection rendered nothing at all.
      const wrapper = mountBar({ composerMode: true, quoteData: null })

      expect(wrapper.find('.quote-question-bar').exists()).toBe(true)
      expect(wrapper.find('.qq-textarea').exists()).toBe(true)
    })

    it('shows no quoted snippet when there is nothing selected', () => {
      const wrapper = mountBar({ composerMode: true, quoteData: null })

      expect(wrapper.find('.qq-quoted-snippet').exists()).toBe(false)
    })

    it('skips the collapsed preview row and goes straight to the input', () => {
      const wrapper = mountBar({ composerMode: true, quoteData: null })

      expect(wrapper.find('.quote-bar-row').exists()).toBe(false)
      expect(wrapper.find('.quote-bar-expanded').exists()).toBe(true)
    })

    it('renders the snippet once a selection is captured', () => {
      const wrapper = mountBar({ composerMode: true, quoteData: QUOTE })

      expect(wrapper.find('.qq-quoted-snippet').exists()).toBe(true)
      expect(wrapper.text()).toContain('build failed on main')
    })

    it('pins the bar on open so it survives selection loss', () => {
      const wrapper = mountBar({ composerMode: true })
      expect(wrapper.emitted('pin')).toBeTruthy()
    })

    it('emits send with the typed text', async () => {
      const wrapper = mountBar({ composerMode: true })
      await wrapper.find('.qq-textarea').setValue('why is this broken?')
      await wrapper.find('.qq-send-btn').trigger('click')

      expect(wrapper.emitted('send')).toEqual([['why is this broken?']])
    })

    it('does not render at all when not visible', () => {
      const wrapper = mountBar({ composerMode: true, visible: false })
      expect(wrapper.find('.quote-question-bar').exists()).toBe(false)
    })

    describe('pending attachment chip', () => {
      it('renders the URL the composer will attach', () => {
        const wrapper = mountBar({
          composerMode: true,
          composerAttachment: { kind: 'url', label: 'acme/widgets#7', url: 'https://github.com/acme/widgets/issues/7' },
        })

        const chip = wrapper.find('.qq-pending-attachment')
        expect(chip.exists()).toBe(true)
        expect(chip.text()).toContain('acme/widgets#7')
      })

      it('renders a local file attachment with the file icon', () => {
        const wrapper = mountBar({
          composerMode: true,
          composerAttachment: { kind: 'file', label: 'main.ts', path: '/proj/src/main.ts' },
        })

        const chip = wrapper.find('.qq-pending-attachment')
        expect(chip.exists()).toBe(true)
        expect(chip.text()).toContain('main.ts')
        expect(chip.find('.lucide-link').exists()).toBe(false)
      })

      it('offers no remove button — the chip is not in the chat context yet', () => {
        // Dismissing the bar is the way to discard it; a close button here would
        // imply the entry already exists somewhere it could be removed from.
        const wrapper = mountBar({
          composerMode: true,
          composerAttachment: { kind: 'url', label: 'acme/widgets#7', url: 'https://github.com/acme/widgets/issues/7' },
        })

        expect(wrapper.find('.qq-pending-attachment .attachment-close-btn').exists()).toBe(false)
        expect(wrapper.find('.qq-pending-attachment button').exists()).toBe(false)
      })

      it('is absent in the normal selection flow', () => {
        const wrapper = mountBar({ composerMode: false, quoteData: QUOTE })

        expect(wrapper.find('.qq-pending-attachment').exists()).toBe(false)
      })

      it('is absent in composer mode with no attachment', () => {
        const wrapper = mountBar({ composerMode: true, composerAttachment: null })

        expect(wrapper.find('.qq-pending-attachment').exists()).toBe(false)
      })
    })
  })

  describe('normal selection flow (unchanged)', () => {
    it('still renders the collapsed preview row for a quote', () => {
      const wrapper = mountBar({ quoteData: QUOTE })

      expect(wrapper.find('.quote-bar-row').exists()).toBe(true)
      expect(wrapper.find('.qq-textarea').exists()).toBe(false)
    })

    it('renders nothing without a quote when not in composer mode', () => {
      const wrapper = mountBar({ composerMode: false, quoteData: null })
      expect(wrapper.find('.quote-question-bar').exists()).toBe(false)
    })

    it('expands to the input when the preview row is clicked', async () => {
      const wrapper = mountBar({ quoteData: QUOTE })
      await wrapper.find('.quote-bar-row').trigger('click')
      await wrapper.vm.$nextTick()

      expect(wrapper.find('.qq-textarea').exists()).toBe(true)
    })

    it('emits add with the typed note', async () => {
      const wrapper = mountBar({ quoteData: QUOTE })
      await wrapper.find('.quote-bar-row').trigger('click')
      await wrapper.vm.$nextTick()
      await wrapper.find('.qq-textarea').setValue('please fix')
      await wrapper.find('.qq-add-btn').trigger('click')

      expect(wrapper.emitted('add')).toEqual([['please fix']])
    })
  })
})
