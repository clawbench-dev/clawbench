import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { defineComponent } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
  createI18n: (opts: any) => ({
    global: {
      t: (k: string) => k,
      locale: { value: opts?.locale ?? 'en' },
    },
    install() {},
  }),
}))

const {
  mockCopyText, mockOpenFilePath, mockHandleCodeBlockClick, mockHandleTableBlockClick,
  mockHandleLocalhostUrlClick, mockDialogConfirm, mockDialogAlert,
} = vi.hoisted(() => ({
  mockCopyText: vi.fn((text: string, cb?: () => void) => { if (cb) cb() }),
  mockOpenFilePath: vi.fn(),
  mockHandleCodeBlockClick: vi.fn(() => false),
  mockHandleTableBlockClick: vi.fn(() => false),
  mockHandleLocalhostUrlClick: vi.fn(() => false),
  mockDialogConfirm: vi.fn(() => Promise.resolve(true)),
  mockDialogAlert: vi.fn(() => Promise.resolve()),
}))

vi.mock('@/utils/clipboard.ts', () => ({
  copyText: (text: string, cb: () => void) => mockCopyText(text, cb),
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (k: string) => k,
}))

vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  openFilePath: mockOpenFilePath,
}))

vi.mock('@/composables/useCodeBlockHeader.ts', () => ({
  handleCodeBlockClick: (e: Event) => mockHandleCodeBlockClick(e),
  handleTableBlockClick: (e: Event) => mockHandleTableBlockClick(e),
}))

vi.mock('@/composables/useLocalhostAnnotation.ts', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: mockHandleLocalhostUrlClick }),
}))

vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({ confirm: mockDialogConfirm, alert: mockDialogAlert }),
}))

const { mockStore } = vi.hoisted(() => ({
  mockStore: {
    setProject: vi.fn(() => Promise.resolve()),
  },
}))

vi.mock('@/stores/app.ts', () => ({
  store: mockStore,
}))

vi.mock('@/utils/lightbox.ts', () => ({
  extractImageName: (src: string) => src.split('/').pop() || '',
}))

vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: defineComponent({
    name: 'ModalDialog',
    props: ['open', 'title', 'maxWidth', 'fullHeight', 'zIndex'],
    emits: ['close'],
    template: '<div class="modal-stub" :data-open="String(open)"><slot name="header" /><slot /><slot name="footer" /></div>',
  }),
}))

import { createI18n } from 'vue-i18n'
import TableRowModal from '@/components/common/TableRowModal.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        table: { row: 'Row', prevRow: 'Prev', nextRow: 'Next' },
        attach: {
          switchWorktree: 'Switch worktree',
          openDirectory: 'Open directory',
          openWorktree: 'Open worktree',
        },
      },
      common: { cancel: 'Cancel', copied: 'Copied' },
    },
  },
})

function mountModal(props: Record<string, unknown> = {}, provideData: Record<string, any> = {}) {
  return mount(TableRowModal, {
    props,
    global: {
      stubs: { Teleport: true },
      plugins: [i18n],
      provide: {
        toast: provideData.toast ?? null,
        hotSwitchProject: provideData.hotSwitchProject ?? null,
        openLightbox: provideData.openLightbox ?? null,
        openMdImages: provideData.openMdImages ?? null,
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('TableRowModal — mount', () => {
  it('mounts with null data', () => {
    const wrapper = mountModal({ data: null })
    expect(wrapper.exists()).toBe(true)
  })

  it('mounts with valid data', () => {
    const wrapper = mountModal({
      data: { headers: ['A', 'B'], rows: [['1', '2'], ['3', '4']], currentIndex: 0 },
    })
    expect(wrapper.exists()).toBe(true)
    expect(wrapper.find('.table-row-form').exists()).toBe(true)
  })

  it('renders header with row label and index', () => {
    const wrapper = mountModal({
      data: { headers: ['A', 'B'], rows: [['1', '2'], ['3', '4']], currentIndex: 0 },
    })
    expect(wrapper.text()).toContain('chat.table.row 1 / 2')
  })

  it('renders a field for each header', () => {
    const wrapper = mountModal({
      data: { headers: ['A', 'B', 'C'], rows: [['1', '2', '3']], currentIndex: 0 },
    })
    expect(wrapper.findAll('.table-row-field')).toHaveLength(3)
  })
})

describe('TableRowModal — navigation', () => {
  it('emits prev when prev button clicked', async () => {
    const wrapper = mountModal({
      data: { headers: ['A'], rows: [['1'], ['2']], currentIndex: 1 },
    })
    const btns = wrapper.findAll('.table-row-nav-btn')
    await btns[0].trigger('click')
    expect(wrapper.emitted('prev')).toBeTruthy()
  })

  it('emits next when next button clicked', async () => {
    const wrapper = mountModal({
      data: { headers: ['A'], rows: [['1'], ['2']], currentIndex: 0 },
    })
    const btns = wrapper.findAll('.table-row-nav-btn')
    await btns[1].trigger('click')
    expect(wrapper.emitted('next')).toBeTruthy()
  })

  it('disables prev when at first row', () => {
    const wrapper = mountModal({
      data: { headers: ['A'], rows: [['1'], ['2']], currentIndex: 0 },
    })
    const btns = wrapper.findAll('.table-row-nav-btn')
    expect(btns[0].attributes('disabled')).toBeDefined()
    expect(btns[1].attributes('disabled')).toBeUndefined()
  })

  it('disables next when at last row', () => {
    const wrapper = mountModal({
      data: { headers: ['A'], rows: [['1'], ['2']], currentIndex: 1 },
    })
    const btns = wrapper.findAll('.table-row-nav-btn')
    expect(btns[0].attributes('disabled')).toBeUndefined()
    expect(btns[1].attributes('disabled')).toBeDefined()
  })

  it('emits close when modal closes', async () => {
    const wrapper = mountModal({
      data: { headers: ['A'], rows: [['1']], currentIndex: 0 },
    })
    const modal = wrapper.findComponent({ name: 'ModalDialog' })
    await modal.vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})

describe('TableRowModal — handleValueDblClick', () => {
  it('copies text and shows toast', async () => {
    const toastShow = vi.fn()
    const wrapper = mountModal(
      { data: { headers: ['A'], rows: [['1']], currentIndex: 0 } },
      { toast: { show: toastShow } }
    )
    const vm = wrapper.vm as any
    const target = document.createElement('div')
    target.className = 'table-row-value'
    const valueEl = document.createElement('span')
    valueEl.className = 'table-row-value'
    valueEl.textContent = 'hello'
    Object.defineProperty(target, 'closest', {
      value: (sel: string) => sel === '.table-row-value' ? valueEl : null,
    })
    const ev = { target } as any
    vm.handleValueDblClick(ev)
    expect(mockCopyText).toHaveBeenCalledWith('hello', expect.any(Function))
    expect(toastShow).toHaveBeenCalled()
  })

  it('does nothing when closest returns null', async () => {
    const wrapper = mountModal(
      { data: { headers: ['A'], rows: [['1']], currentIndex: 0 } },
      {}
    )
    const vm = wrapper.vm as any
    const target = document.createElement('div')
    Object.defineProperty(target, 'closest', { value: () => null })
    vm.handleValueDblClick({ target } as any)
    expect(mockCopyText).not.toHaveBeenCalled()
  })

  it('does nothing when text is empty', async () => {
    const wrapper = mountModal(
      { data: { headers: ['A'], rows: [['1']], currentIndex: 0 } },
      {}
    )
    const vm = wrapper.vm as any
    const valueEl = document.createElement('span')
    valueEl.textContent = '   '
    const target = document.createElement('div')
    Object.defineProperty(target, 'closest', { value: () => valueEl })
    vm.handleValueDblClick({ target } as any)
    expect(mockCopyText).not.toHaveBeenCalled()
  })
})

describe('TableRowModal — handleValueClick handlers', () => {
  it('handleCodeBlockClick is called', async () => {
    const wrapper = mountModal({ data: { headers: ['A'], rows: [['1']], currentIndex: 0 } })
    const vm = wrapper.vm as any
    const ev = { target: document.createElement('div') } as any
    vm.handleValueClick(ev)
    expect(mockHandleCodeBlockClick).toHaveBeenCalled()
  })

  it('handleTableBlockClick is called', async () => {
    mockHandleCodeBlockClick.mockReturnValueOnce(false)
    const wrapper = mountModal({ data: { headers: ['A'], rows: [['1']], currentIndex: 0 } })
    const vm = wrapper.vm as any
    const ev = { target: document.createElement('div') } as any
    vm.handleValueClick(ev)
    expect(mockHandleTableBlockClick).toHaveBeenCalled()
  })

  it('handleLocalhostUrlClick is called', async () => {
    mockHandleCodeBlockClick.mockReturnValue(false)
    mockHandleTableBlockClick.mockReturnValue(false)
    const wrapper = mountModal({ data: { headers: ['A'], rows: [['1']], currentIndex: 0 } })
    const vm = wrapper.vm as any
    const ev = { target: document.createElement('div') } as any
    vm.handleValueClick(ev)
    expect(mockHandleLocalhostUrlClick).toHaveBeenCalled()
  })

  it('worktree button click triggers hotSwitchProject', async () => {
    mockHandleCodeBlockClick.mockReturnValue(false)
    mockHandleTableBlockClick.mockReturnValue(false)
    mockHandleLocalhostUrlClick.mockReturnValue(false)
    const hotSwitchProject = vi.fn(() => Promise.resolve())
    const wrapper = mountModal(
      { data: { headers: ['A'], rows: [['1']], currentIndex: 0 } },
      { hotSwitchProject }
    )
    const vm = wrapper.vm as any
    const wtBtn = document.createElement('button')
    wtBtn.className = 'chat-worktree-btn'
    wtBtn.setAttribute('data-worktree-path', '/path/to/wt')
    const target = document.createElement('div')
    Object.defineProperty(target, 'closest', {
      value: (sel: string) => sel === '.chat-worktree-btn' ? wtBtn : null,
    })
    const ev = { target, preventDefault: vi.fn(), stopPropagation: vi.fn() } as any
    await vm.handleValueClick(ev)
    expect(mockDialogConfirm).toHaveBeenCalled()
    expect(hotSwitchProject).toHaveBeenCalledWith('/path/to/wt')
  })

  it('commit hash click dispatches navigate-to-commit event', async () => {
    mockHandleCodeBlockClick.mockReturnValue(false)
    mockHandleTableBlockClick.mockReturnValue(false)
    mockHandleLocalhostUrlClick.mockReturnValue(false)
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
    const wrapper = mountModal({ data: { headers: ['A'], rows: [['1']], currentIndex: 0 } })
    const vm = wrapper.vm as any
    const commitEl = document.createElement('span')
    commitEl.setAttribute('data-commit-sha', 'abc1234')
    const target = document.createElement('div')
    Object.defineProperty(target, 'closest', {
      value: (sel: string) => sel === '.chat-commit-hash, .chat-commit-open-btn' ? commitEl : null,
    })
    const ev = { target, preventDefault: vi.fn(), stopPropagation: vi.fn() } as any
    await vm.handleValueClick(ev)
    expect(dispatchSpy).toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeTruthy()
    dispatchSpy.mockRestore()
  })

  it('file-open button click calls openFilePath', async () => {
    mockHandleCodeBlockClick.mockReturnValue(false)
    mockHandleTableBlockClick.mockReturnValue(false)
    mockHandleLocalhostUrlClick.mockReturnValue(false)
    const wrapper = mountModal({ data: { headers: ['A'], rows: [['1']], currentIndex: 0 } })
    const vm = wrapper.vm as any
    const fileBtn = document.createElement('button')
    fileBtn.setAttribute('data-file-path', '/src/main.ts')
    fileBtn.setAttribute('data-line-start', '10')
    fileBtn.setAttribute('data-line-end', '20')
    const target = document.createElement('div')
    Object.defineProperty(target, 'closest', {
      value: (sel: string) => sel === '.chat-file-open-btn' ? fileBtn : null,
    })
    const ev = { target, preventDefault: vi.fn(), stopPropagation: vi.fn() } as any
    await vm.handleValueClick(ev)
    expect(mockOpenFilePath).toHaveBeenCalledWith('/src/main.ts', 10, 20)
  })
})