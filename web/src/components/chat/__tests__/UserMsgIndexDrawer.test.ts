import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

vi.mock('lucide-vue-next', () => ({
  MessagesSquare: { name: 'MessagesSquareIcon', render: () => null },
  Split: { name: 'SplitIcon', render: () => null },
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div><slot name="header" /><slot /></div>',
    props: ['open', 'auto', 'title'],
    emits: ['close'],
  },
}))

vi.mock('@/utils/format.ts', () => ({
  formatRelativeTime: vi.fn(() => '2m ago'),
}))

import UserMsgIndexDrawer from '@/components/chat/UserMsgIndexDrawer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: {} },
})

function mountSheet(props = {}) {
  return mount(UserMsgIndexDrawer, {
    props: { open: true, messages: [], ...props },
    global: { plugins: [i18n] },
  })
}

describe('UserMsgIndexDrawer', () => {
  describe('truncateText', () => {
    it('renders truncated message text via truncateUserMsg', () => {
      const messages = [
        { id: 1, content: 'Hello world', role: 'user' },
      ]
      const wrapper = mountSheet({ messages })
      const text = wrapper.find('.msg-text')
      expect(text.exists()).toBe(true)
      expect(text.text()).toContain('Hello world')
    })

    it('renders multiple messages with indices', () => {
      const messages = [
        { id: 1, content: 'First', role: 'user' },
        { id: 2, content: 'Second', role: 'user' },
      ]
      const wrapper = mountSheet({ messages })
      const items = wrapper.findAll('.msg-item')
      expect(items).toHaveLength(2)
      expect(items[0].find('.msg-index').text()).toBe('1')
      expect(items[1].find('.msg-index').text()).toBe('2')
    })

    it('marks active message', () => {
      const messages = [
        { id: 1, content: 'A', role: 'user' },
        { id: 2, content: 'B', role: 'user' },
      ]
      const wrapper = mountSheet({ messages, activeId: 2 })
      const items = wrapper.findAll('.msg-item')
      expect(items[0].classes()).not.toContain('active')
      expect(items[1].classes()).toContain('active')
    })

    it('emits select on message click', async () => {
      const messages = [
        { id: 1, content: 'Click me', role: 'user' },
      ]
      const wrapper = mountSheet({ messages })
      await wrapper.find('.msg-item').trigger('click')
      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual([messages[0]])
    })

    it('emits fork on fork button click', async () => {
      const messages = [
        { id: 1, content: 'Hello', role: 'user' },
        { id: 2, content: 'World', role: 'user' },
      ]
      const wrapper = mountSheet({ messages })
      const forkBtns = wrapper.findAll('.msg-fork-btn')
      expect(forkBtns).toHaveLength(2)
      await forkBtns[0].trigger('click')
      expect(wrapper.emitted('fork')).toBeTruthy()
      expect(wrapper.emitted('fork')![0]).toEqual([messages[0]])
    })

    it('shows loading state', () => {
      const wrapper = mountSheet({ loading: true })
      expect(wrapper.find('.panel-loading').exists()).toBe(true)
    })

    it('shows jumping state', () => {
      const wrapper = mountSheet({ jumping: true })
      expect(wrapper.find('.panel-loading').exists()).toBe(true)
    })
  })
})
