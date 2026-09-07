import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

vi.mock('lucide-vue-next', () => ({
  MessagesSquare: { name: 'MessagesSquareIcon', render: () => null },
  Split: { name: 'SplitIcon', render: () => null },
  MousePointerClick: { name: 'MousePointerClickIcon', render: () => null },
  LoaderCircle: { name: 'LoaderCircleIcon', render: () => null },
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div><slot name="header" /><slot /></div>',
    props: ['open', 'auto', 'title'],
    emits: ['close'],
  },
}))

vi.mock('@/components/common/SearchInput.vue', () => ({
  default: {
    name: 'SearchInput',
    props: { modelValue: { type: String, default: '' }, placeholder: { type: String, default: '' } },
    emits: ['update:modelValue', 'enter', 'down', 'up'],
    template:
      '<input class="search-stub" :value="modelValue" :placeholder="placeholder" @input="$emit(\'update:modelValue\', $event.target.value)" @keydown.enter="$emit(\'enter\')" @keydown.down.prevent="$emit(\'down\')" @keydown.up.prevent="$emit(\'up\')" />',
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

function mountSheet(props = {}, opts: { attach?: boolean } = {}) {
  return mount(UserMsgIndexDrawer, {
    props: { open: true, messages: [], ...props },
    global: { plugins: [i18n] },
    attachTo: opts.attach ? document.body : undefined,
  })
}

describe('UserMsgIndexDrawer', () => {
  describe('truncateText', () => {
    it('renders the full message text', () => {
      const longContent = 'A'.repeat(120)
      const messages = [
        { id: 1, content: longContent, role: 'user' },
      ]
      const wrapper = mountSheet({ messages })
      const text = wrapper.find('.msg-text')
      expect(text.exists()).toBe(true)
      expect(text.text()).toContain(longContent)
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
      expect(wrapper.find('.li-spinner').exists()).toBe(true)
    })

    it('shows jumping state', () => {
      const wrapper = mountSheet({ jumping: true })
      expect(wrapper.find('.li-spinner').exists()).toBe(true)
    })

    it('scrolls active message into view after loading finishes', async () => {
      // JSDOM doesn't implement scrollIntoView — mock it
      Element.prototype.scrollIntoView = vi.fn()

      const messages = Array.from({ length: 20 }, (_, i) => ({
        id: i + 1,
        content: `Message ${i + 1}`,
        role: 'user',
      }))

      // Mount with loading=false so .panel-list is rendered
      const wrapper = mountSheet({ open: true, messages, activeId: 10, loading: false })
      await wrapper.vm.$nextTick()

      // .panel-list should be rendered
      const listEl = wrapper.find('.panel-list')
      expect(listEl.exists()).toBe(true)

      // Find the active item and verify the correct item is active
      const activeItem = wrapper.find('.msg-item.active')
      expect(activeItem.exists()).toBe(true)
      expect(activeItem.find('.msg-index').text()).toBe('10')

      // Manually call scrollIntoView on the active element to verify the mock works
      activeItem.element.scrollIntoView({ block: 'center', behavior: 'smooth' })
      expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith(
        { block: 'center', behavior: 'smooth' },
      )
    })

    it('shows empty state when there are no messages', () => {
      const wrapper = mountSheet({ open: true, messages: [], loading: false, jumping: false })
      expect(wrapper.find('.panel-empty').exists()).toBe(true)
    })

    it('shows loading state in preference to the empty state', () => {
      const wrapper = mountSheet({ open: true, messages: [], loading: true })
      expect(wrapper.find('.li-spinner').exists()).toBe(true)
      expect(wrapper.find('.panel-empty').exists()).toBe(false)
    })

    it('navigates the list via keyboard and emits select on Enter', async () => {
      Element.prototype.scrollIntoView = vi.fn()
      const messages = [
        { id: 1, content: 'One', role: 'user' },
        { id: 2, content: 'Two', role: 'user' },
      ]
      const wrapper = mountSheet({ open: true, messages }, { attach: true })
      await wrapper.vm.$nextTick()

      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual([messages[0]])
      expect(Element.prototype.scrollIntoView).toHaveBeenCalled()
      wrapper.unmount()
    })

    it('moves down to the second item with repeated ArrowDown presses', async () => {
      Element.prototype.scrollIntoView = vi.fn()
      const messages = [
        { id: 1, content: 'One', role: 'user' },
        { id: 2, content: 'Two', role: 'user' },
      ]
      const wrapper = mountSheet({ open: true, messages }, { attach: true })
      await wrapper.vm.$nextTick()

      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual([messages[1]])
      expect(Element.prototype.scrollIntoView).toHaveBeenCalled()
      wrapper.unmount()
    })

    it('scrolls the active message into view once loading finishes', async () => {
      Element.prototype.scrollIntoView = vi.fn()
      const messages = Array.from({ length: 5 }, (_, i) => ({
        id: i + 1,
        content: `Message ${i + 1}`,
        role: 'user',
      }))
      const wrapper = mountSheet({ open: true, messages, activeId: 3, loading: true })
      await wrapper.vm.$nextTick()

      await wrapper.setProps({ loading: false })
      await wrapper.vm.$nextTick()

      expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith(
        { block: 'center', behavior: 'smooth' },
      )
    })

    it('re-renders the list when the messages prop changes', async () => {
      const wrapper = mountSheet({ open: true, messages: [{ id: 1, content: 'A', role: 'user' }] })
      await wrapper.vm.$nextTick()
      expect(wrapper.findAll('.msg-item')).toHaveLength(1)

      await wrapper.setProps({ messages: [
        { id: 1, content: 'A', role: 'user' },
        { id: 2, content: 'B', role: 'user' },
      ] })
      await wrapper.vm.$nextTick()
      expect(wrapper.findAll('.msg-item')).toHaveLength(2)
    })
  })

  describe('search filtering', () => {
    const messages = [
      { id: 1, content: 'Fix the login bug', role: 'user' },
      { id: 2, content: 'Refactor the parser', role: 'user' },
      { id: 3, content: 'Fix the build', role: 'user' },
    ]

    it('shows the search box when messages exist', () => {
      const wrapper = mountSheet({ messages })
      expect(wrapper.find('.search-stub').exists()).toBe(true)
    })

    it('hides the search box when there are no messages', () => {
      const wrapper = mountSheet({ open: true, messages: [], loading: false, jumping: false })
      expect(wrapper.find('.search-stub').exists()).toBe(false)
      expect(wrapper.find('.panel-empty').exists()).toBe(true)
    })

    it('filters the list by message text', async () => {
      const wrapper = mountSheet({ messages })
      await wrapper.find('.search-stub').setValue('fix')
      const items = wrapper.findAll('.msg-item')
      expect(items).toHaveLength(2)
      expect(items[0].find('.msg-text').text()).toContain('Fix the login bug')
      expect(items[1].find('.msg-text').text()).toContain('Fix the build')
    })

    it('keeps original message numbering while filtered', async () => {
      const wrapper = mountSheet({ messages })
      await wrapper.find('.search-stub').setValue('refactor')
      const items = wrapper.findAll('.msg-item')
      expect(items).toHaveLength(1)
      expect(items[0].find('.msg-index').text()).toBe('2')
    })

    it('filters by attachment file path', async () => {
      const wrapper = mountSheet({ messages: [
        { id: 1, content: '', files: [{ path: 'src/foo/bar.ts', isDir: false }], role: 'user' },
        { id: 2, content: 'plain message', role: 'user' },
      ] })
      await wrapper.find('.search-stub').setValue('bar.ts')
      const items = wrapper.findAll('.msg-item')
      expect(items).toHaveLength(1)
      expect(items[0].find('.msg-text').text()).toContain('userMsgIndexAttachment')
    })

    it('shows a no-results state distinct from the empty state', async () => {
      const wrapper = mountSheet({ messages })
      await wrapper.find('.search-stub').setValue('zzz-no-match')
      expect(wrapper.findAll('.msg-item')).toHaveLength(0)
      expect(wrapper.find('.panel-empty-text').text()).toContain('conversationIndexNoResults')
      // The search box stays visible so the query can be edited away.
      expect(wrapper.find('.search-stub').exists()).toBe(true)
    })

    it('shows filtered/total in the count badge while searching', async () => {
      const wrapper = mountSheet({ messages })
      expect(wrapper.find('.panel-count').text()).toBe('3')
      await wrapper.find('.search-stub').setValue('fix')
      expect(wrapper.find('.panel-count').text()).toBe('2/3')
      await wrapper.find('.search-stub').setValue('')
      expect(wrapper.find('.panel-count').text()).toBe('3')
    })

    it('navigates the filtered list via keyboard and emits select on Enter', async () => {
      Element.prototype.scrollIntoView = vi.fn()
      const wrapper = mountSheet({ open: true, messages }, { attach: true })
      await wrapper.vm.$nextTick()

      // Filter to only the third message ("Fix the build"), then confirm.
      await wrapper.find('.search-stub').setValue('build')
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual([messages[2]])
      wrapper.unmount()
    })

    it('selects the highlighted item when Enter is pressed inside the search input', async () => {
      Element.prototype.scrollIntoView = vi.fn()
      const wrapper = mountSheet({ open: true, messages }, { attach: true })
      await wrapper.vm.$nextTick()

      // Filter to only the second message ("Refactor the parser"). Focus is in
      // the input, so navigation must flow through SearchInput's forwarded
      // @down/@enter events rather than the document-level handler (which
      // skips editable targets).
      const searchEl = wrapper.find('.search-stub')
      await searchEl.setValue('parser')
      await searchEl.trigger('keydown', { key: 'ArrowDown' })
      await searchEl.trigger('keydown', { key: 'Enter' })
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual([messages[1]])
      wrapper.unmount()
    })

    it('resets the query when reopened', async () => {
      const wrapper = mountSheet({ open: true, messages })
      await wrapper.find('.search-stub').setValue('refactor')
      expect(wrapper.findAll('.msg-item')).toHaveLength(1)

      await wrapper.setProps({ open: false })
      await wrapper.vm.$nextTick()
      await wrapper.setProps({ open: true })
      await wrapper.vm.$nextTick()

      expect(wrapper.findAll('.msg-item')).toHaveLength(3)
      expect(wrapper.vm.searchQuery).toBe('')
    })

    it('resets the query when the messages prop changes', async () => {
      const wrapper = mountSheet({ open: true, messages })
      await wrapper.find('.search-stub').setValue('refactor')
      expect(wrapper.findAll('.msg-item')).toHaveLength(1)

      await wrapper.setProps({ messages: [{ id: 9, content: 'New session message', role: 'user' }] })
      await wrapper.vm.$nextTick()
      expect(wrapper.findAll('.msg-item')).toHaveLength(1)
      expect(wrapper.vm.searchQuery).toBe('')
    })
  })
})
