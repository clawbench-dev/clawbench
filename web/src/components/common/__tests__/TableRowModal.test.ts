import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { defineComponent, nextTick, h } from 'vue'
import TableRowModal from '@/components/common/TableRowModal.vue'

// Hoisted mocks
const { mockHandleLocalhostUrlClick, mockConfirm, mockOpenFilePath, mockCopyText } = vi.hoisted(() => ({
  mockHandleLocalhostUrlClick: vi.fn(() => false),
  mockConfirm: vi.fn().mockResolvedValue(false),
  mockOpenFilePath: vi.fn().mockResolvedValue(true),
  mockCopyText: vi.fn(),
}))

// Mock ModalDialog — replaces the real component (which uses Teleport) with a simple mock
// Must use h() render function inside vi.hoisted because defineComponent isn't available there
const MockModalDialog = defineComponent({
  name: 'MockModalDialog',
  props: { open: Boolean, title: String },
  emits: ['close'],
  template: `
    <div v-if="open" class="modal-dialog-mock">
      <div class="modal-title">{{ title }}</div>
      <slot />
      <div class="modal-footer"><slot name="footer" /></div>
    </div>
  `,
})

vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: defineComponent({
    name: 'MockModalDialog',
    props: { open: Boolean, title: String },
    emits: ['close'],
    template: '<div v-if="open" class="modal-dialog-mock"><div class="modal-title">{{ title }}</div><slot /><div class="modal-footer"><slot name="footer" /></div></div>',
  }),
}))

// Mock vue-i18n
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: any) => {
      const map: Record<string, string> = {
        'chat.table.row': 'Row',
        'chat.table.prevRow': 'Prev',
        'chat.table.nextRow': 'Next',
        'chat.attach.switchWorktree': 'Switch Worktree',
        'chat.attach.openDirectory': 'Open Directory',
        'chat.attach.openWorktree': 'Open Worktree',
        'common.cancel': 'Cancel',
        'common.copied': 'Copied',
      }
      return map[key] ?? key
    },
  }),
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

vi.mock('@/composables/useLocalhostAnnotation.ts', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: mockHandleLocalhostUrlClick }),
}))

vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({
    confirm: mockConfirm,
  }),
}))

vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  openFilePath: mockOpenFilePath,
}))

vi.mock('@/utils/clipboard.ts', () => ({
  copyText: mockCopyText,
}))

vi.mock('@/composables/useCodeBlockHeader.ts', () => ({
  handleCodeBlockClick: vi.fn(() => false),
  handleTableBlockClick: vi.fn(() => false),
}))

vi.mock('@/utils/lightbox.ts', () => ({
  extractImageName: (src: string) => {
    try {
      const path = new URL(src, 'http://localhost').pathname
      const prefix = '/api/local-file/'
      return path.startsWith(prefix) ? path.slice(prefix.length).split('/').pop() || '' : path.split('/').pop() || ''
    } catch { return '' }
  },
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '/tmp' }, setProject: vi.fn().mockResolvedValue(undefined) },
}))

vi.mock('@/utils/appLog.ts', () => ({
  default: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

describe('TableRowModal', () => {
  let wrapper: VueWrapper<any> | null = null
  let container: HTMLDivElement

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    vi.clearAllMocks()
    mockHandleLocalhostUrlClick.mockReturnValue(false)
    mockConfirm.mockResolvedValue(false)
    mockOpenFilePath.mockResolvedValue(true)
  })

  afterEach(() => {
    if (wrapper) {
      wrapper.unmount()
      wrapper = null
    }
    if (container.parentNode) {
      document.body.removeChild(container)
    }
  })

  const sampleData = {
    headers: ['Name', 'Age', 'City'],
    rows: [['Alice', '30', 'NYC'], ['Bob', '25', 'LA'], ['Carol', '35', 'SF']],
    currentIndex: 0,
  }

  function mountModal(data: any = null) {
    wrapper = mount(TableRowModal, {
      props: { data },
      global: {
        provide: {
          toast: { show: vi.fn() },
          switchTab: vi.fn(),
          hotSwitchProject: vi.fn(),
        },
      },
    })
    return wrapper
  }

  it('renders modal when data is provided', () => {
    const w = mountModal(sampleData)
    expect(w.find('.modal-dialog-mock').exists()).toBe(true)
  })

  it('does not render table content when data is null', () => {
    const w = mountModal(null)
    expect(w.find('.table-row-form').exists()).toBe(false)
  })

  it('renders header labels', () => {
    const w = mountModal(sampleData)
    const labels = w.findAll('.table-row-label')
    expect(labels.length).toBe(3)
    expect(labels[0].text()).toBe('Name')
    expect(labels[1].text()).toBe('Age')
    expect(labels[2].text()).toBe('City')
  })

  it('renders row values for current index', () => {
    const w = mountModal(sampleData)
    const values = w.findAll('.table-row-value')
    expect(values.length).toBe(3)
    expect(values[0].element.innerHTML).toContain('Alice')
    expect(values[1].element.innerHTML).toContain('30')
  })

  it('renders navigation buttons', () => {
    const w = mountModal(sampleData)
    const btns = w.findAll('.table-row-nav-btn')
    expect(btns.length).toBe(2) // prev + next
  })

  it('disables prev button on first row', () => {
    const w = mountModal({ ...sampleData, currentIndex: 0 })
    const btns = w.findAll('.table-row-nav-btn')
    expect(btns[0].element.disabled).toBe(true)
  })

  it('disables next button on last row', () => {
    const w = mountModal({ ...sampleData, currentIndex: 2 })
    const btns = w.findAll('.table-row-nav-btn')
    expect(btns[1].element.disabled).toBe(true)
  })

  it('enables prev button when not on first row', () => {
    const w = mountModal({ ...sampleData, currentIndex: 1 })
    const btns = w.findAll('.table-row-nav-btn')
    expect(btns[0].element.disabled).toBe(false)
  })

  it('enables next button when not on last row', () => {
    const w = mountModal({ ...sampleData, currentIndex: 0 })
    const btns = w.findAll('.table-row-nav-btn')
    expect(btns[1].element.disabled).toBe(false)
  })

  it('emits close when prev button clicked', async () => {
    const w = mountModal({ ...sampleData, currentIndex: 1 })
    const btns = w.findAll('.table-row-nav-btn')
    await btns[0].trigger('click')
    expect(w.emitted('prev')).toBeTruthy()
  })

  it('emits next when next button clicked', async () => {
    const w = mountModal({ ...sampleData, currentIndex: 0 })
    const btns = w.findAll('.table-row-nav-btn')
    await btns[1].trigger('click')
    expect(w.emitted('next')).toBeTruthy()
  })

  it('emits close when ModalDialog emits close', async () => {
    const w = mountModal(sampleData)
    await w.findComponent(MockModalDialog).vm.$emit('close')
    expect(w.emitted('close')).toBeTruthy()
  })

  it('shows row index in title', () => {
    const w = mountModal({ ...sampleData, currentIndex: 1 })
    const title = w.find('.modal-title')
    expect(title.text()).toContain('2 / 3')
  })

  it('handles double-click to copy value text', async () => {
    const w = mountModal(sampleData)
    const valueEl = w.findAll('.table-row-value')[0]
    await valueEl.trigger('dblclick')
    // copyText should be called with the text content
    expect(mockCopyText).toHaveBeenCalled()
  })

  it('skips copy on double-click when text is empty', async () => {
    const data = { headers: ['Name'], rows: [['']], currentIndex: 0 }
    const w = mountModal(data)
    const valueEl = w.findAll('.table-row-value')[0]
    await valueEl.trigger('dblclick')
    // copyText should NOT be called for empty text
    expect(mockCopyText).not.toHaveBeenCalled()
  })

  it('handles localhost URL click', async () => {
    mockHandleLocalhostUrlClick.mockReturnValue(true)
    const w = mountModal(sampleData)
    const valueEl = w.findAll('.table-row-value')[0]
    await valueEl.trigger('click')
    expect(mockHandleLocalhostUrlClick).toHaveBeenCalled()
  })

  it('handles worktree button click with confirm cancel', async () => {
    mockConfirm.mockResolvedValue(false)
    const data = {
      headers: ['Path'],
      rows: [['<span class="chat-worktree-btn" data-worktree-path="/wt" data-file-path="/wt/file.go">link</span>']],
      currentIndex: 0,
    }
    const w = mountModal(data)
    const valueEl = w.findAll('.table-row-value')[0]
    const wtBtn = valueEl.element.querySelector('.chat-worktree-btn') as HTMLElement
    if (wtBtn) {
      wtBtn.click()
      await nextTick()
      await vi.waitFor(() => expect(mockConfirm).toHaveBeenCalled())
      expect(mockOpenFilePath).toHaveBeenCalled()
    }
  })

  it('handles worktree button click with confirm accept and hotSwitchProject', async () => {
    mockConfirm.mockResolvedValue(true)
    const mockHotSwitch = vi.fn().mockResolvedValue(undefined)
    wrapper = mount(TableRowModal, {
      props: { data: { headers: ['P'], rows: [['<span class="chat-worktree-btn" data-worktree-path="/wt">x</span>']], currentIndex: 0 } },
      global: {
        provide: { toast: { show: vi.fn() }, switchTab: vi.fn(), hotSwitchProject: mockHotSwitch },
      },
    })
    const valueEl = wrapper.findAll('.table-row-value')[0]
    const wtBtn = valueEl.element.querySelector('.chat-worktree-btn') as HTMLElement
    if (wtBtn) {
      wtBtn.click()
      await nextTick()
      await vi.waitFor(() => expect(mockHotSwitch).toHaveBeenCalledWith('/wt'))
    }
  })

  it('handles commit hash click', async () => {
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
    const data = {
      headers: ['Commit'],
      rows: [['<span class="chat-commit-hash" data-commit-sha="abc123">abc</span>']],
      currentIndex: 0,
    }
    const w = mountModal(data)
    const valueEl = w.findAll('.table-row-value')[0]
    const commitEl = valueEl.element.querySelector('.chat-commit-hash') as HTMLElement
    if (commitEl) {
      commitEl.click()
      await nextTick()
      expect(dispatchSpy).toHaveBeenCalled()
      expect(w.emitted('close')).toBeTruthy()
    }
    dispatchSpy.mockRestore()
  })

  it('handles file-open button click', async () => {
    const data = {
      headers: ['File'],
      rows: [['<span class="chat-file-open-btn" data-file-path="/foo.go" data-line-start="10" data-line-end="20">open</span>']],
      currentIndex: 0,
    }
    const w = mountModal(data)
    const valueEl = w.findAll('.table-row-value')[0]
    const fileBtn = valueEl.element.querySelector('.chat-file-open-btn') as HTMLElement
    if (fileBtn) {
      fileBtn.click()
      await nextTick()
      await vi.waitFor(() => expect(mockOpenFilePath).toHaveBeenCalledWith('/foo.go', 10, 20))
      expect(w.emitted('close')).toBeTruthy()
    }
  })

  it('handles file-open button click without line numbers', async () => {
    const data = {
      headers: ['File'],
      rows: [['<span class="chat-file-open-btn" data-file-path="/bar.go">open</span>']],
      currentIndex: 0,
    }
    const w = mountModal(data)
    const valueEl = w.findAll('.table-row-value')[0]
    const fileBtn = valueEl.element.querySelector('.chat-file-open-btn') as HTMLElement
    if (fileBtn) {
      fileBtn.click()
      await nextTick()
      await vi.waitFor(() => expect(mockOpenFilePath).toHaveBeenCalledWith('/bar.go', undefined, undefined))
    }
  })

  it('opens lightbox on image click', async () => {
    const mockOpenLightbox = vi.fn()
    const mockOpenMdImages = vi.fn()
    const data = {
      headers: ['Preview'],
      rows: [['<img class="chat-img lightbox-img" src="/api/local-file/img.png" alt="test">']],
      currentIndex: 0,
    }
    wrapper = mount(TableRowModal, {
      props: { data },
      global: {
        provide: {
          toast: { show: vi.fn() },
          switchTab: vi.fn(),
          hotSwitchProject: vi.fn(),
          openLightbox: mockOpenLightbox,
          openMdImages: mockOpenMdImages,
        },
      },
    })
    const img = wrapper.element.querySelector('.lightbox-img') as HTMLElement
    expect(img).toBeTruthy()
    img.click()
    await nextTick()
    expect(mockOpenLightbox).toHaveBeenCalled()
    expect(mockOpenMdImages).not.toHaveBeenCalled()
  })

  it('opens lightbox with md images navigation when multiple images exist', async () => {
    const mockOpenLightbox = vi.fn()
    const mockOpenMdImages = vi.fn()
    const data = {
      headers: ['Preview'],
      rows: [['<img class="chat-img lightbox-img" src="/api/local-file/a.png" alt="A"><img class="chat-img lightbox-img" src="/api/local-file/b.png" alt="B">']],
      currentIndex: 0,
    }
    wrapper = mount(TableRowModal, {
      props: { data },
      global: {
        provide: {
          toast: { show: vi.fn() },
          switchTab: vi.fn(),
          hotSwitchProject: vi.fn(),
          openLightbox: mockOpenLightbox,
          openMdImages: mockOpenMdImages,
        },
      },
    })
    const imgs = wrapper.element.querySelectorAll('.lightbox-img')
    expect(imgs.length).toBe(2)
    // Click the second image
    imgs[1].click()
    await nextTick()
    expect(mockOpenMdImages).toHaveBeenCalled()
    const [list, startIdx] = mockOpenMdImages.mock.calls[0]
    expect(list.length).toBe(2)
    expect(startIdx).toBe(1)
    expect(list[1].name).toBe('B')
  })
})
