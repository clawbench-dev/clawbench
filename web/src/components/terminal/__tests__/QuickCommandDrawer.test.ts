import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, defineComponent } from 'vue'
import QuickCommandDrawer from '@/components/terminal/QuickCommandDrawer.vue'

vi.mock('lucide-vue-next', () => ({
  ZapIcon: { name: 'ZapIcon', template: '<span />' },
  PencilIcon: { name: 'PencilIcon', template: '<span />' },
  Trash2Icon: { name: 'Trash2Icon', template: '<span />' },
  PlusIcon: { name: 'PlusIcon', template: '<span />' },
  EyeOffIcon: { name: 'EyeOffIcon', template: '<span />' },
  MoreVerticalIcon: { name: 'MoreVerticalIcon', template: '<span />' },
  DownloadIcon: { name: 'DownloadIcon', template: '<span />' },
  UploadIcon: { name: 'UploadIcon', template: '<span />' },
  Zap: { name: 'ZapIcon', template: '<span />' },
  Pencil: { name: 'PencilIcon', template: '<span />' },
  Trash2: { name: 'Trash2Icon', template: '<span />' },
  Plus: { name: 'PlusIcon', template: '<span />' },
  EyeOff: { name: 'EyeOffIcon', template: '<span />' },
  MoreVertical: { name: 'MoreVerticalIcon', template: '<span />' },
  Download: { name: 'DownloadIcon', template: '<span />' },
  Upload: { name: 'UploadIcon', template: '<span />' },
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    name: 'BottomSheet',
    props: { open: Boolean, auto: Boolean, title: String, instant: Boolean, noHeader: Boolean, handleOnly: Boolean, transparentOverlay: Boolean, fullscreen: Boolean, closeGuard: Boolean },
    emits: ['close'],
    template: `<div v-if="open" class="bottom-sheet-stub"><slot name="header" /><slot /></div>`,
  }),
}))

vi.mock('@/components/terminal/QuickCommandEditModal.vue', () => ({
  default: defineComponent({
    name: 'QuickCommandEditModal',
    props: { open: Boolean, editingCommand: Object },
    emits: ['close', 'saved'],
    template: `<div v-if="open" class="qc-edit-modal-stub" />`,
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

vi.mock('@/composables/useQuickCommands', () => ({
  useQuickCommands: () => ({
    commands: ref([]),
    addCommand: vi.fn().mockResolvedValue(true),
    reorderCommands: vi.fn().mockResolvedValue(true),
    deleteCommand: vi.fn().mockResolvedValue(true),
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
  useToast: () => ({ show: vi.fn() }),
}))

describe('QuickCommandDrawer', () => {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })

  function mountDrawer() {
    return mount(QuickCommandDrawer, {
      props: { open: true },
      global: { plugins: [i18n] },
    })
  }

  it('renders the bottom sheet when open', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.bottom-sheet-stub').exists()).toBe(true)
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
})
