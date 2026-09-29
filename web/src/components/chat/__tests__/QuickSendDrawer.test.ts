import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, defineComponent } from 'vue'
import QuickSendDrawer from '@/components/chat/QuickSendDrawer.vue'

vi.mock('lucide-vue-next', () => ({
  SendIcon: { name: 'SendIcon', template: '<span />' },
  PencilIcon: { name: 'PencilIcon', template: '<span />' },
  Trash2Icon: { name: 'Trash2Icon', template: '<span />' },
  PlusIcon: { name: 'PlusIcon', template: '<span />' },
  SparklesIcon: { name: 'SparklesIcon', template: '<span />' },
  MoreVerticalIcon: { name: 'MoreVerticalIcon', template: '<span />' },
  DownloadIcon: { name: 'DownloadIcon', template: '<span />' },
  UploadIcon: { name: 'UploadIcon', template: '<span />' },
  Send: { name: 'SendIcon', template: '<span />' },
  Pencil: { name: 'PencilIcon', template: '<span />' },
  Trash2: { name: 'Trash2Icon', template: '<span />' },
  Plus: { name: 'PlusIcon', template: '<span />' },
  Sparkles: { name: 'SparklesIcon', template: '<span />' },
  MoreVertical: { name: 'MoreVerticalIcon', template: '<span />' },
  Download: { name: 'DownloadIcon', template: '<span />' },
  Upload: { name: 'UploadIcon', template: '<span />' },
}))

const { mockDeleteItem, mockReorderItems, mockAddItem, mockToastShow } = vi.hoisted(() => ({
  mockDeleteItem: vi.fn().mockResolvedValue(true),
  mockReorderItems: vi.fn().mockResolvedValue(true),
  mockAddItem: vi.fn().mockResolvedValue(true),
  mockToastShow: vi.fn(),
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    name: 'BottomSheet',
    props: { open: Boolean, auto: Boolean, title: String, instant: Boolean, noHeader: Boolean, handleOnly: Boolean, transparentOverlay: Boolean, fullscreen: Boolean, closeGuard: Boolean },
    emits: ['close'],
    template: `<div v-if="open" class="bottom-sheet-stub"><slot name="header" /><slot /></div>`,
  }),
}))

vi.mock('@/components/chat/QuickSendEditModal.vue', () => ({
  default: defineComponent({
    name: 'QuickSendEditModal',
    props: { open: Boolean, editingItem: Object },
    emits: ['close', 'saved'],
    template: `<div v-if="open" class="qs-edit-modal-stub" />`,
  }),
}))

vi.mock('@/components/chat/MessageClustersDrawer.vue', () => ({
  default: defineComponent({
    name: 'MessageClustersDrawer',
    template: '<div class="mc-stub" />',
  }),
}))

vi.mock('vue-draggable-plus', () => ({
  VueDraggable: defineComponent({
    name: 'VueDraggable',
    props: ['modelValue', 'handle'],
    emits: ['update:modelValue', 'end'],
    template: `<div class="vdp-stub"><slot /></div>`,
  }),
}))

vi.mock('@/composables/useQuickSend', () => ({
  useQuickSend: () => ({
    items: ref([]),
    addItem: mockAddItem,
    reorderItems: mockReorderItems,
    deleteItem: mockDeleteItem,
  }),
}))

vi.mock('@/composables/useQuickSendIO', () => ({
  createJsonImporter: () => ({
    trigger: vi.fn(),
    importFromText: vi.fn(),
  }),
  buildExportPayload: (kind: string, items: unknown[]) => ({ version: 1, kind, items }),
  downloadJson: vi.fn(),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

describe('QuickSendDrawer', () => {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })

  function mountDrawer() {
    return mount(QuickSendDrawer, {
      props: { open: true },
      global: { plugins: [i18n] },
    })
  }

  it('renders the bottom sheet when open', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.bottom-sheet-stub').exists()).toBe(true)
  })

  it('renders empty state when no items', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.qs-empty').exists()).toBe(true)
  })

  it('opens edit modal when add button is triggered', async () => {
    const wrapper = mountDrawer()
    const addBtn = wrapper.find('.create-btn[title="chat.quickSend.addItem"]')
    expect(addBtn.exists()).toBe(true)
    await addBtn.trigger('click')
    expect(wrapper.find('.qs-edit-modal-stub').exists()).toBe(true)
  })

  it('renders add, more and clusters buttons in header', () => {
    const wrapper = mountDrawer()
    const buttons = wrapper.findAll('.create-btn')
    expect(buttons.length).toBe(3)
  })

  it('declares close emit', () => {
    expect(QuickSendDrawer.emits).toContain('close')
  })

  it('emits close when bottom sheet fires close', async () => {
    const wrapper = mountDrawer()
    await wrapper.findComponent({ name: 'BottomSheet' }).vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('addNewItem sets editingItem to null and opens edit modal', async () => {
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).addNewItem()
    expect((wrapper.vm as any).editingItem).toBeNull()
    expect((wrapper.vm as any).editOpen).toBe(true)
  })

  it('editItem sets editingItem and opens edit modal', async () => {
    const wrapper = mountDrawer()
    const item = { id: 1, label: 'Continue', command: 'continue', sort_order: 0 }
    ;(wrapper.vm as any).editItem(item as any)
    expect((wrapper.vm as any).editingItem).toEqual(item)
    expect((wrapper.vm as any).editOpen).toBe(true)
  })

  it('onItemSaved closes edit modal and clears editingItem', () => {
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).editingItem = { id: 1, label: 'x', command: 'x', sort_order: 0 }
    ;(wrapper.vm as any).editOpen = true
    ;(wrapper.vm as any).onItemSaved()
    expect((wrapper.vm as any).editOpen).toBe(false)
    expect((wrapper.vm as any).editingItem).toBeNull()
  })

  // The "more actions" menu is a PopupMenu teleported to <body>, so the
  // BottomSheet's v-show does not hide it. Without an explicit close it keeps
  // hovering over whatever replaced the drawer — e.g. after a tab switch, which
  // closes the drawer through useTabDrawer.
  it('closes the more-actions menu when the drawer closes', async () => {
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).showMoreMenu = true
    await wrapper.vm.$nextTick()
    expect((wrapper.vm as any).showMoreMenu).toBe(true)

    await wrapper.setProps({ open: false })
    await wrapper.vm.$nextTick()
    expect((wrapper.vm as any).showMoreMenu).toBe(false)
  })

  it('leaves the more-actions menu alone when the drawer re-opens', async () => {
    // Re-opening must not be treated as a close (an inverted guard would clear
    // the menu on the wrong transition).
    const wrapper = mountDrawer()
    await wrapper.setProps({ open: false })
    await wrapper.vm.$nextTick()
    ;(wrapper.vm as any).showMoreMenu = true
    await wrapper.vm.$nextTick()

    await wrapper.setProps({ open: true })
    await wrapper.vm.$nextTick()
    expect((wrapper.vm as any).showMoreMenu).toBe(true)
  })

  it('toggleDeleteConfirm toggles deleteConfirmId', () => {
    const wrapper = mountDrawer()
    expect((wrapper.vm as any).deleteConfirmId).toBeNull()
    ;(wrapper.vm as any).toggleDeleteConfirm(42)
    expect((wrapper.vm as any).deleteConfirmId).toBe(42)
    ;(wrapper.vm as any).toggleDeleteConfirm(42)
    expect((wrapper.vm as any).deleteConfirmId).toBeNull()
  })

  it('doDelete calls deleteItem and shows toast on success', async () => {
    mockDeleteItem.mockClear()
    mockToastShow.mockClear()
    const wrapper = mountDrawer()
    await (wrapper.vm as any).doDelete(7)
    expect(mockDeleteItem).toHaveBeenCalledWith(7)
    expect(mockToastShow).toHaveBeenCalled()
  })

  it('doDelete does not show toast when delete fails', async () => {
    mockDeleteItem.mockReset()
    mockDeleteItem.mockResolvedValueOnce(false)
    mockToastShow.mockClear()
    const wrapper = mountDrawer()
    await (wrapper.vm as any).doDelete(8)
    expect(mockToastShow).not.toHaveBeenCalled()
  })

  it('onDragEnd calls reorderItems with local order', async () => {
    mockReorderItems.mockReset()
    mockReorderItems.mockResolvedValueOnce(true)
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).localItems = [
      { id: 1, label: 'a', command: 'x', sort_order: 0 },
      { id: 2, label: 'b', command: 'y', sort_order: 1 },
    ]
    await (wrapper.vm as any).onDragEnd()
    expect(mockReorderItems).toHaveBeenCalledWith([1, 2])
  })
})
