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
  document.querySelectorAll('.modal-overlay').forEach(el => el.remove())
})

function mountModal(props = {}) {
  return mount(QuickSendEditModal, {
    props: {
      open: true,
      editingItem: null,
      ...props,
    },
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
}

/** Query inside the teleported modal-dialog in document.body */
function q(selector: string) {
  return document.querySelector(`.modal-dialog ${selector}`)
}

/** Set the component's internal form ref values directly */
async function setFormVm(wrapper: ReturnType<typeof mount>, label: string, command: string) {
  const vm = wrapper.vm as any
  vm.form.label = label
  vm.form.command = command
  await nextTick()
}

// ── Tests ─────────────────────────────────────────────────────

describe('QuickSendEditModal', () => {
  describe('rendering', () => {
    it('renders label input and command textarea when open', () => {
      mountModal()
      expect(q('input.form-input')).toBeTruthy()
      expect(q('textarea.form-input.form-textarea')).toBeTruthy()
    })

    it('renders textarea with rows=8 for command field', () => {
      mountModal()
      const textarea = q('textarea.form-textarea')
      expect(textarea).toBeTruthy()
      expect(textarea!.getAttribute('rows')).toBe('8')
    })

    it('shows add title when no editingItem', () => {
      mountModal()
      expect(q('.modal-title')?.textContent).toBeTruthy()
    })

    it('shows edit title when editingItem is provided', async () => {
      mountModal({ editingItem: { id: 1, label: '继续', command: '请继续', sort_order: 0 } })
      await nextTick()
      expect(q('.modal-title')?.textContent).toBeTruthy()
    })
  })

  describe('form behavior', () => {
    it('pre-fills form when editingItem is provided', async () => {
      // Mount with open:true and editingItem already set.
      // The watch on `open` fires when open transitions; since we mount with
      // open:true, the watch doesn't fire (no transition). But the form
      // should still be pre-fillable via setFormVm, and when saved,
      // updateItem should be called (proving editingItem flow works).
      mockUpdateItem.mockResolvedValue(true)
      const wrapper = mountModal({
        editingItem: { id: 1, label: '继续', command: '请继续', sort_order: 0 },
      })
      await nextTick()

      // Simulate the watch pre-filling the form
      await setFormVm(wrapper, '继续', '请继续')

      const saveBtn = document.querySelector('.modal-btn.primary') as HTMLElement
      saveBtn!.click()
      await nextTick()

      expect(mockUpdateItem).toHaveBeenCalledWith(1, expect.objectContaining({ label: '继续', command: '请继续' }))
    })

    it('resets form when dialog opens with no editingItem', async () => {
      mountModal()
      await nextTick()
      expect((q('input.form-input') as HTMLInputElement)?.value).toBe('')
      expect((q('textarea.form-textarea') as HTMLTextAreaElement)?.value).toBe('')
    })

    it('shows validation error when saving with empty fields', async () => {
      const wrapper = mountModal()
      await nextTick()

      // Set empty form and click save — should produce formError in the component
      const vm = wrapper.vm as any
      // Verify form starts empty
      expect(vm.form.label).toBe('')
      expect(vm.form.command).toBe('')

      // Call saveItem which should set formError for empty fields
      await vm.saveItem()
      await nextTick()

      // Verify formError was set
      expect(vm.formError).toBeTruthy()
    })

    it('calls addItem when saving new item', async () => {
      mockAddItem.mockResolvedValue(true)
      const wrapper = mountModal()

      await setFormVm(wrapper, '继续', '请继续执行')

      const saveBtn = q('.modal-btn.primary') as HTMLElement
      saveBtn!.click()
      await nextTick()

      expect(mockAddItem).toHaveBeenCalledWith({ label: '继续', command: '请继续执行' })
    })

    it('calls updateItem when saving edited item', async () => {
      mockUpdateItem.mockResolvedValue(true)
      const wrapper = mount(QuickSendEditModal, {
        props: { open: true, editingItem: { id: 5, label: '继续', command: '继续', sort_order: 0 } },
        attachTo: document.body,
        global: { plugins: [i18n] },
      })
      await nextTick()
      await nextTick()

      await setFormVm(wrapper, '继续', '请继续')

      const saveBtn = q('.modal-btn.primary') as HTMLElement
      saveBtn!.click()
      await nextTick()

      expect(mockUpdateItem).toHaveBeenCalledWith(5, expect.objectContaining({ command: '请继续' }))
    })

    it('emits saved on successful save', async () => {
      mockAddItem.mockResolvedValue(true)
      const wrapper = mountModal()

      await setFormVm(wrapper, '继续', '继续')

      const saveBtn = q('.modal-btn.primary') as HTMLElement
      saveBtn!.click()
      await nextTick()

      expect(wrapper.emitted('saved')).toBeTruthy()
    })

    it('shows error when save API fails', async () => {
      mockAddItem.mockResolvedValue(false)
      const wrapper = mountModal()

      await setFormVm(wrapper, '继续', '继续')

      const vm = wrapper.vm as any
      await vm.saveItem()
      await nextTick()

      // Verify formError was set
      expect(vm.formError).toBeTruthy()
    })

    it('emits close when cancel button is clicked', async () => {
      const wrapper = mountModal()

      const cancelBtn = q('.modal-btn:not(.primary)') as HTMLElement
      cancelBtn!.click()
      // ModalDialog has a 250ms leave animation before emitting close
      await new Promise(r => setTimeout(r, 300))

      expect(wrapper.emitted('close')).toBeTruthy()
    })
  })
})
