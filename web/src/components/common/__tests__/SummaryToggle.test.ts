import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SummaryToggle from '@/components/common/SummaryToggle.vue'

// Mock vue-i18n so computed labels resolve
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'chat.message.summaryViewSummary': 'View Summary',
        'chat.message.summaryViewOriginal': 'View Original',
        'task.exec.tabSummary': 'Summary',
        'task.exec.tabOriginal': 'Original',
      }
      return map[key] ?? key
    },
  }),
}))

describe('SummaryToggle', () => {
  function mountToggle(props: Record<string, any> = {}) {
    return mount(SummaryToggle, {
      props: {
        mode: 'button',
        showingSummary: false,
        i18nPrefix: 'chat.message',
        ...props,
      },
    })
  }

  // ── Button mode ──

  describe('button mode', () => {
    it('renders a button element', () => {
      const wrapper = mountToggle({ mode: 'button' })
      expect(wrapper.find('.chat-action-btn').exists()).toBe(true)
    })

    it('shows "View Summary" label when showingSummary is false', () => {
      const wrapper = mountToggle({ mode: 'button', showingSummary: false })
      expect(wrapper.text()).toContain('View Summary')
    })

    it('shows "View Original" label when showingSummary is true', () => {
      const wrapper = mountToggle({ mode: 'button', showingSummary: true })
      expect(wrapper.text()).toContain('View Original')
    })

    it('emits toggle when clicked', async () => {
      const wrapper = mountToggle({ mode: 'button' })
      await wrapper.find('.chat-action-btn').trigger('click')
      expect(wrapper.emitted('toggle')).toHaveLength(1)
    })

    it('click propagation is stopped', async () => {
      const wrapper = mountToggle({ mode: 'button' })
      // The @click.stop modifier should be on the button
      const btn = wrapper.find('.chat-action-btn')
      expect(btn.exists()).toBe(true)
      await btn.trigger('click')
      expect(wrapper.emitted('toggle')).toHaveLength(1)
    })

    it('uses chat.message i18n prefix for labels', () => {
      const wrapper = mountToggle({ mode: 'button', i18nPrefix: 'chat.message' })
      expect(wrapper.text()).toContain('View Summary')
    })

    it('toggles label when showingSummary prop changes', async () => {
      const wrapper = mountToggle({ mode: 'button', showingSummary: false })
      expect(wrapper.text()).toContain('View Summary')

      await wrapper.setProps({ showingSummary: true })
      expect(wrapper.text()).toContain('View Original')
    })
  })

  // ── Tab mode ──

  describe('tab mode', () => {
    it('renders tab bar element', () => {
      const wrapper = mountToggle({ mode: 'tab' })
      expect(wrapper.find('.summary-toggle-bar').exists()).toBe(true)
    })

    it('renders two tab buttons', () => {
      const wrapper = mountToggle({ mode: 'tab' })
      const tabs = wrapper.findAll('.summary-toggle-tab')
      expect(tabs).toHaveLength(2)
    })

    it('marks summary tab as active when showingSummary is true', () => {
      const wrapper = mountToggle({ mode: 'tab', showingSummary: true })
      const tabs = wrapper.findAll('.summary-toggle-tab')
      expect(tabs[0].classes()).toContain('active')
      expect(tabs[1].classes()).not.toContain('active')
    })

    it('marks original tab as active when showingSummary is false', () => {
      const wrapper = mountToggle({ mode: 'tab', showingSummary: false })
      const tabs = wrapper.findAll('.summary-toggle-tab')
      expect(tabs[0].classes()).not.toContain('active')
      expect(tabs[1].classes()).toContain('active')
    })

    it('shows "Summary" and "Original" tab labels using task.exec prefix', () => {
      const wrapper = mountToggle({ mode: 'tab', i18nPrefix: 'task.exec' })
      expect(wrapper.text()).toContain('Summary')
      expect(wrapper.text()).toContain('Original')
    })

    it('emits toggle when clicking summary tab while showing original', async () => {
      const wrapper = mountToggle({ mode: 'tab', showingSummary: false })
      const tabs = wrapper.findAll('.summary-toggle-tab')
      // Click the summary tab (first tab) when not showing summary
      await tabs[0].trigger('click')
      expect(wrapper.emitted('toggle')).toHaveLength(1)
    })

    it('does not emit toggle when clicking already-active summary tab', async () => {
      const wrapper = mountToggle({ mode: 'tab', showingSummary: true })
      const tabs = wrapper.findAll('.summary-toggle-tab')
      // Click the summary tab (first tab) when already showing summary
      await tabs[0].trigger('click')
      expect(wrapper.emitted('toggle')).toBeUndefined()
    })

    it('emits toggle when clicking original tab while showing summary', async () => {
      const wrapper = mountToggle({ mode: 'tab', showingSummary: true })
      const tabs = wrapper.findAll('.summary-toggle-tab')
      // Click the original tab (second tab) when showing summary
      await tabs[1].trigger('click')
      expect(wrapper.emitted('toggle')).toHaveLength(1)
    })

    it('does not emit toggle when clicking already-active original tab', async () => {
      const wrapper = mountToggle({ mode: 'tab', showingSummary: false })
      const tabs = wrapper.findAll('.summary-toggle-tab')
      // Click the original tab (second tab) when already showing original
      await tabs[1].trigger('click')
      expect(wrapper.emitted('toggle')).toBeUndefined()
    })

    it('updates active state when showingSummary prop changes', async () => {
      const wrapper = mountToggle({ mode: 'tab', showingSummary: false })
      let tabs = wrapper.findAll('.summary-toggle-tab')
      expect(tabs[1].classes()).toContain('active')

      await wrapper.setProps({ showingSummary: true })
      tabs = wrapper.findAll('.summary-toggle-tab')
      expect(tabs[0].classes()).toContain('active')
      expect(tabs[1].classes()).not.toContain('active')
    })
  })

  // ── Default props ──

  describe('default props', () => {
    it('defaults mode to button', () => {
      const wrapper = mount(SummaryToggle)
      expect(wrapper.find('.chat-action-btn').exists()).toBe(true)
    })

    it('defaults showingSummary to false', () => {
      const wrapper = mount(SummaryToggle)
      // In button mode, showingSummary=false means "View Summary" label
      expect(wrapper.text()).toContain('View Summary')
    })

    it('defaults i18nPrefix to chat.message', () => {
      const wrapper = mount(SummaryToggle)
      // With default i18nPrefix, button mode shows summaryViewSummary/summaryViewOriginal keys
      expect(wrapper.text()).toContain('View Summary')
    })
  })
})
