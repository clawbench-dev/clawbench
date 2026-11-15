import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, defineComponent } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import QuickCommandDrawer from '@/components/terminal/QuickCommandDrawer.vue'
import { _resetCommandHistoryForTesting } from '@/composables/useCommandHistory'
import { localConfig } from '@/composables/useSettingsConfig'

// The history tab runs the real useCommandHistory against mocked HTTP helpers,
// so the drawer's wiring (refresh on tab switch, tap-to-insert) is exercised
// rather than stubbed away.
const mockApiGet = vi.fn()
const mockApiPost = vi.fn()
const mockApiDelete = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
  apiPost: (...args: any[]) => mockApiPost(...args),
  apiDelete: (...args: any[]) => mockApiDelete(...args),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { w: vi.fn(), d: vi.fn(), i: vi.fn(), e: vi.fn() },
}))

vi.mock('lucide-vue-next', () => ({
  ZapIcon: { name: 'ZapIcon', template: '<span />' },
  PencilIcon: { name: 'PencilIcon', template: '<span />' },
  Trash2Icon: { name: 'Trash2Icon', template: '<span />' },
  PlusIcon: { name: 'PlusIcon', template: '<span />' },
  EyeOffIcon: { name: 'EyeOffIcon', template: '<span />' },
  PinIcon: { name: 'PinIcon', template: '<span />' },
  HistoryIcon: { name: 'HistoryIcon', template: '<span />' },
  MoreVerticalIcon: { name: 'MoreVerticalIcon', template: '<span />' },
  DownloadIcon: { name: 'DownloadIcon', template: '<span />' },
  UploadIcon: { name: 'UploadIcon', template: '<span />' },
  Zap: { name: 'ZapIcon', template: '<span />' },
  Pencil: { name: 'PencilIcon', template: '<span />' },
  Trash2: { name: 'Trash2Icon', template: '<span />' },
  Plus: { name: 'PlusIcon', template: '<span />' },
  EyeOff: { name: 'EyeOffIcon', template: '<span />' },
  Pin: { name: 'PinIcon', template: '<span />' },
  History: { name: 'HistoryIcon', template: '<span />' },
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

  beforeEach(() => {
    mockApiGet.mockReset().mockResolvedValue([])
    mockApiPost.mockReset().mockResolvedValue({ added: 0 })
    mockApiDelete.mockReset().mockResolvedValue({ success: true })
    _resetCommandHistoryForTesting()
    localConfig.terminalHistoryEnabled = true
  })

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

  // The add (+) button must sit at the far right of the header. It and the
  // "more" (⋮) button are the only two actions, and both used to carry
  // `margin-left: auto` on `.create-btn` — with TWO auto margins flex splits
  // the free space equally between them, so + landed in the middle. The fix
  // mirrors QuickSendDrawer: wrap the buttons in `.bs-header-actions` and keep
  // the single auto margin on the wrapper, never on the button.
  describe('header action alignment', () => {
    const vue = readFileSync(resolve(__dirname, '../QuickCommandDrawer.vue'), 'utf8')

    it('groups the header buttons in a right-aligned actions wrapper', () => {
      expect(vue).toMatch(
        /<span class="bs-header-actions">[\s\S]*?addNewCommand[\s\S]*?showMoreMenu[\s\S]*?<\/span>/,
      )
    })

    it('puts the single auto margin on the wrapper, not on each button', () => {
      // `[^}]*` keeps each match inside its own rule block — a `[\s\S]*?`
      // would span past the closing brace and match a later rule.
      expect(vue).toMatch(/\.bs-header-actions\s*\{[^}]*margin-left:\s*auto;/)
      // A second auto margin on `.create-btn` would re-split the space.
      expect(vue).not.toMatch(/\.create-btn\s*\{[^}]*margin-left:\s*auto;/)
    })
  })

  describe('recent commands tab', () => {
    it('loads history when the tab is opened', async () => {
      mockApiGet.mockResolvedValue([
        { id: 1, command: 'git status', use_count: 2, last_used_at: '', project_path: '', project_only: false },
      ])
      const wrapper = mountDrawer()

      ;(wrapper.vm as any).activeTab = 'recent'
      await flushPromises()

      expect(mockApiGet).toHaveBeenCalledWith(expect.stringContaining('/api/terminal/history'))
      expect(wrapper.findAll('.qc-history-row')).toHaveLength(1)
    })

    it('emits insert (not run) when a remembered command is tapped', async () => {
      mockApiGet.mockResolvedValue([
        { id: 7, command: 'git log --oneline', use_count: 1, last_used_at: '', project_path: '', project_only: false },
      ])
      const wrapper = mountDrawer()
      ;(wrapper.vm as any).activeTab = 'recent'
      await flushPromises()

      await wrapper.find('.qc-history-row').trigger('click')

      expect(wrapper.emitted('insert')).toEqual([['git log --oneline']])
      expect(wrapper.emitted('close')).toBeTruthy()
    })

    it('shows the empty state when there is nothing recorded', async () => {
      const wrapper = mountDrawer()
      ;(wrapper.vm as any).activeTab = 'recent'
      await flushPromises()

      expect(wrapper.find('.qc-empty').exists()).toBe(true)
      expect(wrapper.findAll('.qc-history-row')).toHaveLength(0)
    })

    it('explains that history is off instead of importing it', async () => {
      localConfig.terminalHistoryEnabled = false
      const wrapper = mountDrawer()
      ;(wrapper.vm as any).activeTab = 'recent'
      await flushPromises()

      expect(wrapper.find('.qc-empty').exists()).toBe(true)
      // Recording is off, so the panel must not read the shell history files.
      expect(mockApiPost).not.toHaveBeenCalled()
      expect(mockApiGet).not.toHaveBeenCalled()
    })

    it('needs two taps to clear history', async () => {
      mockApiGet.mockResolvedValue([
        { id: 1, command: 'ls', use_count: 1, last_used_at: '', project_path: '', project_only: false },
      ])
      const wrapper = mountDrawer()
      ;(wrapper.vm as any).activeTab = 'recent'
      await flushPromises()

      // First tap only arms the button.
      await wrapper.find('.bs-header-actions .create-btn:last-child').trigger('click')
      await wrapper.vm.$nextTick()
      expect(mockApiDelete).not.toHaveBeenCalled()
      expect(wrapper.find('.create-btn.danger').exists()).toBe(true)

      await wrapper.find('.bs-header-actions .create-btn:last-child').trigger('click')
      await flushPromises()
      expect(mockApiDelete).toHaveBeenCalledWith('/api/terminal/history')
    })
  })
})
