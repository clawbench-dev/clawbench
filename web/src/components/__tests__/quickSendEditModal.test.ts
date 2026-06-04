import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import QuickSendEditModal from '@/components/chat/QuickSendEditModal.vue'

// ── Mocks ────────────────────────────────────────────────────
const mockAddItem = vi.fn()
const mockUpdateItem = vi.fn()
const mockToastShow = vi.fn()

vi.mock('@/composables/useQuickSend', () => ({
  useQuickSend: () => ({
    addItem: mockAddItem,
    updateItem: mockUpdateItem,
  }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/utils/quickSendValidation', () => ({
  validateQuickSendForm: (form: { label: string; command: string }) => {
    if (!form.label.trim() || !form.command.trim()) return 'chat.quickSend.itemRequired'
    return ''
  },
}))

// ── i18n ─────────────────────────────────────────────────────
const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      chat: {
        quickSend: {
          addItem: '添加',
          editItem: '编辑',
          itemLabel: '标签',
          itemCommand: '命令',
          itemRequired: '必填',
          itemSaved: '已保存',
          saveFailed: '保存失败',
        },
      },
      common: { cancel: '取消', save: '保存' },
    },
  },
})

beforeEach(() => {
  mockAddItem.mockReset()
  mockUpdateItem.mockReset()
  mockToastShow.mockReset()
})

function mountModal(props = {}) {
  return mount(QuickSendEditModal, {
    props: {
      open: true,
      editingItem: null,
      ...props,
    },
    global: {
      stubs: { teleport: true },
      plugins: [i18n],
    },
  })
}

// ── Tests ─────────────────────────────────────────────────────

describe('QuickSendEditModal', () => {
  describe('rendering', () => {
    it('renders label input and command textarea when open', () => {
      const wrapper = mountModal()

      expect(wrapper.find('input.form-input').exists()).toBe(true)
      expect(wrapper.find('textarea.form-input.form-textarea').exists()).toBe(true)
    })

    it('renders textarea with rows=8 for command field', () => {
      const wrapper = mountModal()

      const textarea = wrapper.find('textarea.form-textarea')
      expect(textarea.exists()).toBe(true)
      expect(textarea.attributes('rows')).toBe('8')
    })

    it('shows add title when no editingItem', () => {
      const wrapper = mountModal()

      expect(wrapper.find('.modal-title').text()).toBeTruthy()
    })

    it('shows edit title when editingItem is provided', async () => {
      const wrapper = mountModal({
        editingItem: { id: 1, label: '继续', command: '请继续', sort_order: 0 },
      })
      await nextTick()

      expect(wrapper.find('.modal-title').text()).toBeTruthy()
    })
  })

  describe('form behavior', () => {
    it('pre-fills form when editingItem is provided', async () => {
      // Mount closed, then open — watch triggers on open change
      const wrapper = mount(QuickSendEditModal, {
        props: { open: false, editingItem: null },
        global: { stubs: { teleport: true }, plugins: [i18n] },
      })
      await wrapper.setProps({ open: true, editingItem: { id: 1, label: '继续', command: '请继续', sort_order: 0 } })
      await nextTick()

      expect(wrapper.find('input.form-input').element.value).toBe('继续')
      expect(wrapper.find('textarea.form-textarea').element.value).toBe('请继续')
    })

    it('resets form when dialog opens with no editingItem', async () => {
      const wrapper = mountModal()
      await nextTick()

      expect(wrapper.find('input.form-input').element.value).toBe('')
      expect(wrapper.find('textarea.form-textarea').element.value).toBe('')
    })

    it('shows validation error when saving with empty fields', async () => {
      const wrapper = mountModal()

      await wrapper.find('.modal-btn.primary').trigger('click')
      await nextTick()

      expect(wrapper.find('.form-error').exists()).toBe(true)
    })

    it('calls addItem when saving new item', async () => {
      mockAddItem.mockResolvedValue(true)
      const wrapper = mountModal()

      await wrapper.find('input.form-input').setValue('继续')
      await wrapper.find('textarea.form-textarea').setValue('请继续执行')
      await wrapper.find('.modal-btn.primary').trigger('click')
      await nextTick()

      expect(mockAddItem).toHaveBeenCalledWith({ label: '继续', command: '请继续执行' })
    })

    it('calls updateItem when saving edited item', async () => {
      mockUpdateItem.mockResolvedValue(true)
      // Mount closed, then open with editingItem
      const wrapper = mount(QuickSendEditModal, {
        props: { open: false, editingItem: null },
        global: { stubs: { teleport: true }, plugins: [i18n] },
      })
      await wrapper.setProps({ open: true, editingItem: { id: 5, label: '继续', command: '继续', sort_order: 0 } })
      await nextTick()

      await wrapper.find('textarea.form-textarea').setValue('请继续')
      await wrapper.find('.modal-btn.primary').trigger('click')
      await nextTick()

      expect(mockUpdateItem).toHaveBeenCalledWith(5, expect.objectContaining({ command: '请继续' }))
    })

    it('emits saved on successful save', async () => {
      mockAddItem.mockResolvedValue(true)
      const wrapper = mountModal()

      await wrapper.find('input.form-input').setValue('继续')
      await wrapper.find('textarea.form-textarea').setValue('继续')
      await wrapper.find('.modal-btn.primary').trigger('click')
      await nextTick()

      expect(wrapper.emitted('saved')).toBeTruthy()
    })

    it('shows error when save API fails', async () => {
      mockAddItem.mockResolvedValue(false)
      const wrapper = mountModal()

      await wrapper.find('input.form-input').setValue('继续')
      await wrapper.find('textarea.form-textarea').setValue('继续')
      await wrapper.find('.modal-btn.primary').trigger('click')
      await nextTick()

      expect(wrapper.find('.form-error').exists()).toBe(true)
      expect(wrapper.emitted('saved')).toBeFalsy()
    })

    it('emits close when cancel button is clicked', async () => {
      const wrapper = mountModal()

      await wrapper.find('.modal-btn:not(.primary)').trigger('click')

      expect(wrapper.emitted('close')).toBeTruthy()
    })
  })
})
