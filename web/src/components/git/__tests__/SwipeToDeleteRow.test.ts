import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import SwipeToDeleteRow from '@/components/git/SwipeToDeleteRow.vue'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const mockOnContentClick = vi.fn()
const mockUseSwipeDelete = {
  offset: { value: 0 },
  onTouchStart: vi.fn(),
  onTouchMove: vi.fn(),
  onTouchEnd: vi.fn(),
  onContentClick: mockOnContentClick,
}

vi.mock('@/composables/useSwipeDelete', () => ({
  useSwipeDelete: () => mockUseSwipeDelete,
}))

function mountRow(props: Record<string, unknown> = {}) {
  return mount(SwipeToDeleteRow, {
    props: {
      deletable: true,
      ...props,
    },
    slots: {
      default: '<div class="slot-content">Item</div>',
    },
    global: {
      stubs: {
        Trash2: true,
      },
    },
  })
}

describe('SwipeToDeleteRow', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseSwipeDelete.offset.value = 0
  })

  describe('when deletable=true', () => {
    it('renders the swipe wrapper with delete background', () => {
      const wrapper = mountRow()
      expect(wrapper.find('.swipe-to-delete').exists()).toBe(true)
      expect(wrapper.find('.swipe-delete-bg').exists()).toBe(true)
      expect(wrapper.find('.swipe-delete-content').exists()).toBe(true)
    })

    it('renders slot content inside the content layer', () => {
      const wrapper = mountRow()
      expect(wrapper.find('.swipe-delete-content .slot-content').exists()).toBe(true)
    })

    it('emits delete when delete background is clicked', async () => {
      const wrapper = mountRow()
      await wrapper.find('.swipe-delete-bg').trigger('click')
      expect(wrapper.emitted('delete')).toBeTruthy()
    })

    it('calls onContentClick when content is clicked', async () => {
      mockOnContentClick.mockReturnValue(false)
      const wrapper = mountRow()
      await wrapper.find('.swipe-delete-content').trigger('click')
      expect(mockOnContentClick).toHaveBeenCalled()
    })
  })

  describe('when deletable=false', () => {
    it('does not render the swipe wrapper', () => {
      const wrapper = mountRow({ deletable: false })
      expect(wrapper.find('.swipe-to-delete').exists()).toBe(false)
      expect(wrapper.find('.swipe-delete-bg').exists()).toBe(false)
    })

    it('renders slot content directly without wrapper', () => {
      const wrapper = mountRow({ deletable: false })
      expect(wrapper.find('.slot-content').exists()).toBe(true)
    })
  })
})
