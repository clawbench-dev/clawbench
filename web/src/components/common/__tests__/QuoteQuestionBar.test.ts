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
