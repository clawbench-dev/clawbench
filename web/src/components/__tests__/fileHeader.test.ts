import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import FileHeader from '@/components/file/FileHeader.vue'

// ── Mocks ────────────────────────────────────────────────────
const mockAddAttachedFile = vi.fn()
const mockHasAttachedFile = vi.fn(() => false)
const mockRemoveAttachedFileByPath = vi.fn()
const mockToggleAttachedFile = vi.fn()

vi.mock('@/composables/useChatContext', () => ({
  useChatContext: () => ({
    addAttachedFile: mockAddAttachedFile,
    hasAttachedFile: mockHasAttachedFile,
    removeAttachedFileByPath: mockRemoveAttachedFileByPath,
    toggleAttachedFile: mockToggleAttachedFile,
    attachedFiles: { value: [] },
    quoteData: { value: null },
    setQuoteData: vi.fn(),
    removeAttachedFile: vi.fn(),
    clearAll: vi.fn(),
  }),
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

vi.mock('@/utils/fileType', () => ({
  getFileType: (name: string) => {
    const ext = name.split('.').pop()?.toLowerCase()
    const isMarkdown = ['md', 'markdown'].includes(ext || '')
    const isHtml = ext === 'html'
    return {
      isMarkdown,
      isHtml,
      isImage: ['png', 'jpg', 'gif'].includes(ext || ''),
      isAudio: false,
      isVideo: false,
      isPdf: ext === 'pdf',
      color: '#000',
    }
  },
}))

// ── i18n ─────────────────────────────────────────────────────
const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      file: {
        header: { openAsText: '以文本打开', toc: '目录', search: '搜索', more: '更多' },
      },
      chat: {
        actions: { attachToChat: '附加到聊天' },
        attach: { alreadyAttached: '已附加', addedToChat: '已添加到聊天', removedFromChat: '已从聊天移除' },
      },
      nav: { refresh: '刷新' },
    },
  },
})

const TeleportStub = { template: '<div><slot /></div>' }

function mountHeader(props = {}) {
  return mount(FileHeader, {
    props: {
      file: { name: 'test.ts', path: '/test.ts', content: 'hello', isBinary: false },
      viewMode: 'code',
      tocOpen: false,
      searchOpen: false,
      wordWrap: false,
      showLineNumbers: false,
      stickyScroll: false,
      overlayOpen: false,
      overlayCanGoBack: false,
      ...props,
    },
    global: {
      stubs: { Teleport: TeleportStub },
      plugins: [i18n],
    },
  })
}

beforeEach(() => {
  mockAddAttachedFile.mockReset()
  mockHasAttachedFile.mockReset()
  mockHasAttachedFile.mockReturnValue(false)
  mockToastShow.mockReset()
})

describe('FileHeader — handleAttachToChat', () => {
  it('adds file to chat context and shows success toast', async () => {
    const wrapper = mountHeader()

    await wrapper.vm.handleAttachToChat()
    await nextTick()

    expect(mockAddAttachedFile).toHaveBeenCalledWith('/test.ts')
    expect(mockToastShow).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'success' }),
    )
  })

  it('shows info toast when file is already attached', async () => {
    mockHasAttachedFile.mockReturnValue(true)
    const wrapper = mountHeader()

    await wrapper.vm.handleAttachToChat()
    await nextTick()

    expect(mockAddAttachedFile).not.toHaveBeenCalled()
    expect(mockRemoveAttachedFileByPath).toHaveBeenCalledWith('/test.ts')
    expect(mockToastShow).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'info' }),
    )
  })

  it('does nothing when file path is missing', async () => {
    const wrapper = mountHeader({ file: { name: 'no-path', content: '' } })

    await wrapper.vm.handleAttachToChat()
    await nextTick()

    expect(mockAddAttachedFile).not.toHaveBeenCalled()
  })
})

describe('FileHeader — handleRefresh', () => {
  it('closes menu and emits refresh', async () => {
    const wrapper = mountHeader()

    wrapper.vm.menuOpen = true
    await nextTick()

    await wrapper.vm.handleRefresh()
    await nextTick()

    expect(wrapper.emitted('refresh')).toBeTruthy()
    expect(wrapper.vm.menuOpen).toBe(false)
  })
})

describe('FileHeader — menu toggling', () => {
  it('toggleMenu opens and closes the menu', async () => {
    const wrapper = mountHeader()

    wrapper.vm.toggleMenu()
    expect(wrapper.vm.menuOpen).toBe(true)

    wrapper.vm.toggleMenu()
    expect(wrapper.vm.menuOpen).toBe(false)
  })
})
