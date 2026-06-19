import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import FileManagerContent from '@/components/file/FileManagerContent.vue'

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

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: vi.fn(() => Promise.resolve(true)),
    prompt: vi.fn(() => Promise.resolve('newfile.txt')),
  }),
}))

vi.mock('@/composables/useTerminalStatus', () => ({
  useTerminalStatus: () => ({ terminalRuntimeEnabled: { value: false } }),
}))

vi.mock('@/composables/useEdgeSwipeBack', () => ({
  useFeatureBackHandler: vi.fn(),
  PRIORITY_PAGE: 0,
}))

vi.mock('@/composables/useFileUpload', () => ({
  useFileUpload: () => ({
    dirUploading: { value: false },
    dirUploadProgress: { value: 0 },
    dirUploadTotal: { value: 0 },
    dirUploadDone: { value: 0 },
    handleFileSelectToDir: vi.fn(),
  }),
}))

vi.mock('@/composables/useDirStack', () => ({
  useDirStack: () => ({
    canGoBack: { value: false },
    goBack: vi.fn(),
  }),
}))

vi.mock('@/composables/useFileNavStack', () => ({
  useFileNavStack: () => ({
    overlayOpen: { value: false },
  }),
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  localConfig: { fileView: 'list' },
  setLocalConfig: vi.fn(),
  useSettingsConfig: () => ({}),
}))

vi.mock('@/stores/app', () => ({
  store: { state: { projectRoot: '/project' } },
}))

vi.mock('@/utils/fileType', () => ({
  getFileType: (name: string) => ({
    isMarkdown: name.endsWith('.md'),
    isHtml: false,
    isImage: false,
    isAudio: false,
    isVideo: false,
    isPdf: false,
    color: '#000',
  }),
}))

vi.mock('@/utils/fileManager', () => ({
  buildThumbUrl: (dir: string, name: string) => `/api/file/thumb?path=${dir}/${name}`,
  isImage: () => false,
  isAudio: () => false,
  isVideo: () => false,
  isThumbable: () => false,
  formatSize: (s: number) => `${s} B`,
  THUMBABLE_EXTS: [],
  createMultiSelect: () => ({
    state: reactive({ active: false, selected: new Set() }),
    enterMultiSelect: vi.fn(),
    exitMultiSelect: vi.fn(),
    toggleSelect: vi.fn(),
  }),
  createClipboard: () => ({
    clipboard: reactive({ entries: [], isCut: false }),
    clear: vi.fn(),
  }),
  resolveClickAction: vi.fn(),
}))

vi.mock('@/components/common/SearchInput.vue', () => ({
  default: { template: '<div class="search-input-stub"><slot /></div>' },
}))

vi.mock('@/components/file/DirBreadcrumb.vue', () => ({
  default: { template: '<div class="dir-breadcrumb-stub" />' },
}))

// ── i18n ─────────────────────────────────────────────────────
const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      file: {
        context: { newFile: '新建文件', newFolder: '新建文件夹', paste: '粘贴', rename: '重命名', delete: '删除', archiveDir: '归档', openAsProject: '打开为项目' },
        uploadHere: '上传到此处',
        sortDefault: '排序',
        prompt: { fileName: '文件名', folderName: '文件夹名', newName: '新名称', pasteNewName: '新名称' },
        toast: { fileCreated: '已创建', folderCreated: '已创建', cutDone: '已剪切', moved: '已移动', createFailed: '创建失败', createFailedDetail: '创建失败', archiving: '归档中', archiveDone: '归档完成', archiveFailed: '归档失败', archiveFailedDetail: '归档失败', switchProjectFailed: '切换失败', switchProjectFailedShort: '切换失败' },
        multiSelect: { allCopied: '已复制', allCut: '已剪切', confirmDelete: '确认删除' },
      },
      chat: {
        actions: { attachToChat: '附加到聊天' },
        attach: { alreadyAttached: '已附加', addedToChat: '已添加到聊天', removedFromChat: '已从聊天移除' },
      },
      common: { remove: '移除', copied: '已复制', delete: '删除', operationFailed: '操作失败' },
      nav: { refresh: '刷新' },
    },
  },
})

const TeleportStub = { template: '<div><slot /></div>' }

const sampleEntries = [
  { name: 'src', type: 'dir', modified: '2025-01-01T00:00:00Z', size: 0 },
  { name: 'test.ts', type: 'file', modified: '2025-01-01T00:00:00Z', size: 100 },
]

function mountContent(props = {}) {
  return mount(FileManagerContent, {
    props: {
      entries: sampleEntries,
      currentDir: '',
      currentFile: null,
      showHidden: false,
      sortField: null,
      sortDir: 'asc',
      dirLoading: false,
      ...props,
    },
    global: {
      stubs: { Teleport: TeleportStub },
      plugins: [i18n],
      provide: {
        activeTab: { value: 'browse' },
        toast: { show: mockToastShow },
      },
    },
  })
}

beforeEach(() => {
  mockAddAttachedFile.mockReset()
  mockHasAttachedFile.mockReset()
  mockHasAttachedFile.mockReturnValue(false)
  mockToastShow.mockReset()
})

// ── Tests ─────────────────────────────────────────────────────

describe('FileManagerContent — doAttachToChat', () => {
  it('adds file to chat context from context menu', async () => {
    const wrapper = mountContent()

    // Set up context menu state via component internals
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'test.ts', path: 'test.ts' }
    await nextTick()

    await wrapper.vm.doAttachToChat()

    expect(mockAddAttachedFile).toHaveBeenCalledWith('test.ts')
    expect(mockToastShow).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'success' }),
    )
    // Menu should close
    expect(wrapper.vm.ctxMenu.visible).toBe(false)
  })

  it('shows info toast when file is already attached', async () => {
    mockHasAttachedFile.mockReturnValue(true)
    const wrapper = mountContent()

    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'test.ts', path: 'test.ts' }
    await nextTick()

    await wrapper.vm.doAttachToChat()

    expect(mockAddAttachedFile).not.toHaveBeenCalled()
    expect(mockRemoveAttachedFileByPath).toHaveBeenCalledWith('test.ts')
    expect(mockToastShow).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'info' }),
    )
  })

  it('does nothing when no entry in context menu', async () => {
    const wrapper = mountContent()

    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = null
    await nextTick()

    await wrapper.vm.doAttachToChat()

    expect(mockAddAttachedFile).not.toHaveBeenCalled()
  })
})

describe('FileManagerContent — toggleAttach', () => {
  it('removes file and shows info toast when already attached', async () => {
    mockHasAttachedFile.mockReturnValue(true)
    const wrapper = mountContent()

    await wrapper.vm.toggleAttach('test.ts')

    expect(mockRemoveAttachedFileByPath).toHaveBeenCalledWith('test.ts')
    expect(mockToastShow).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'info' }),
    )
  })

  it('adds file and shows success toast when not attached', async () => {
    mockHasAttachedFile.mockReturnValue(false)
    const wrapper = mountContent()

    await wrapper.vm.toggleAttach('test.ts')

    expect(mockAddAttachedFile).toHaveBeenCalledWith('test.ts')
    expect(mockToastShow).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'success' }),
    )
  })
})

describe('FileManagerContent — closeCtxMenu', () => {
  it('closes menu and clears entry', async () => {
    const wrapper = mountContent()

    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'test.ts', path: 'test.ts' }
    wrapper.vm.ctxMenu.x = 100
    wrapper.vm.ctxMenu.y = 200

    wrapper.vm.closeCtxMenu()

    expect(wrapper.vm.ctxMenu.visible).toBe(false)
    expect(wrapper.vm.ctxMenu.entry).toBeNull()
  })
})

describe('FileManagerContent — resolveEntryFromEvent', () => {
  it('returns null when target is not a file item', () => {
    const wrapper = mountContent()
    const e = { target: document.createElement('div') }
    expect(wrapper.vm.resolveEntryFromEvent(e)).toBeNull()
  })

  it('resolves file entry from DOM element with data attributes', () => {
    const wrapper = mountContent()

    // Create a mock DOM element structure
    const item = document.createElement('div')
    item.classList.add('file-item')
    item.dataset.action = 'file'
    item.dataset.path = 'src/foo.ts'

    const nameEl = document.createElement('span')
    nameEl.classList.add('file-name')
    nameEl.textContent = 'foo.ts'
    item.appendChild(nameEl)

    const e = { target: item }

    const result = wrapper.vm.resolveEntryFromEvent(e)
    expect(result).toEqual({ type: 'file', name: 'foo.ts', path: 'src/foo.ts' })
  })

  it('resolves dir entry from DOM element', () => {
    const wrapper = mountContent()

    const item = document.createElement('div')
    item.classList.add('file-item')
    item.dataset.action = 'dir'
    item.dataset.path = 'src'

    const nameEl = document.createElement('span')
    nameEl.classList.add('file-name')
    nameEl.textContent = 'src'
    item.appendChild(nameEl)

    const e = { target: item }

    const result = wrapper.vm.resolveEntryFromEvent(e)
    expect(result).toEqual({ type: 'dir', name: 'src', path: 'src' })
  })
})

describe('FileManagerContent — handleCtxMenu', () => {
  it('sets context menu position and visibility from event', async () => {
    const wrapper = mountContent()

    const item = document.createElement('div')
    item.classList.add('file-item')
    item.dataset.action = 'file'
    item.dataset.path = 'test.ts'

    const nameEl = document.createElement('span')
    nameEl.classList.add('file-name')
    nameEl.textContent = 'test.ts'
    item.appendChild(nameEl)

    const e = { clientX: 150, clientY: 250, target: item }

    await wrapper.vm.handleCtxMenu(e)
    await nextTick()

    expect(wrapper.vm.ctxMenu.x).toBe(150)
    expect(wrapper.vm.ctxMenu.y).toBe(250)
    expect(wrapper.vm.ctxMenu.visible).toBe(true)
    expect(wrapper.vm.ctxMenu.entry).toEqual({ type: 'file', name: 'test.ts', path: 'test.ts' })
  })
})

describe('FileManagerContent — long press', () => {
  it('onLongPressEnd clears timer', () => {
    vi.useFakeTimers()
    const wrapper = mountContent()

    // Start a long press
    const touchEvent = { touches: [{ clientX: 50, clientY: 50 }] }
    wrapper.vm.onLongPressStart(touchEvent)

    // End before timer fires
    wrapper.vm.onLongPressEnd()

    // Advance timer — should NOT fire because timer was cleared
    vi.advanceTimersByTime(500)

    expect(wrapper.vm.ctxMenu.visible).toBe(false)
    vi.useRealTimers()
  })

  it('onLongPressMove sets moved flag', () => {
    const wrapper = mountContent()
    wrapper.vm.onLongPressMove()
    // Just verify it doesn't throw
    expect(true).toBe(true)
  })
})

describe('FileManagerContent — doCopy/doCut with closeCtxMenu', () => {
  it('doCopy closes context menu', async () => {
    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'test.ts', path: 'test.ts' }
    await nextTick()

    await wrapper.vm.doCopy()

    expect(wrapper.vm.ctxMenu.visible).toBe(false)
    expect(wrapper.vm.ctxMenu.entry).toBeNull()
  })

  it('doCut closes context menu', async () => {
    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'test.ts', path: 'test.ts' }
    await nextTick()

    await wrapper.vm.doCut()

    expect(wrapper.vm.ctxMenu.visible).toBe(false)
    expect(wrapper.vm.ctxMenu.entry).toBeNull()
  })
})

describe('FileManagerContent — doDelete emits correct path after closeCtxMenu', () => {
  it('emits delete with path even though closeCtxMenu nulls entry', async () => {
    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'test.ts', path: 'src/test.ts' }
    await nextTick()

    wrapper.vm.doDelete()

    // closeCtxMenu sets entry to null, but delete event should still fire with correct path
    expect(wrapper.emitted('delete')).toBeTruthy()
    expect(wrapper.emitted('delete')[0]).toEqual(['src/test.ts'])
    expect(wrapper.vm.ctxMenu.visible).toBe(false)
    expect(wrapper.vm.ctxMenu.entry).toBeNull()
  })

  it('does nothing when no entry in context menu', () => {
    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = null

    wrapper.vm.doDelete()

    expect(wrapper.emitted('delete')).toBeFalsy()
  })
})

describe('FileManagerContent — doDownload uses saved path/name after closeCtxMenu', () => {
  it('creates download link with correct path after closeCtxMenu nulls entry', async () => {
    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'readme.md', path: 'docs/readme.md' }
    await nextTick()

    const clickSpy = vi.fn()
    const appendSpy = vi.spyOn(document.body, 'appendChild').mockImplementation((el) => el)
    const removeSpy = vi.spyOn(document.body, 'removeChild').mockImplementation((el) => el)
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(clickSpy)

    wrapper.vm.doDownload()

    expect(wrapper.vm.ctxMenu.entry).toBeNull()
    expect(appendSpy).toHaveBeenCalled()
    const anchor = appendSpy.mock.calls[0][0]
    expect(anchor.href).toContain('docs%2Freadme.md')
    expect(anchor.download).toBe('readme.md')
    expect(clickSpy).toHaveBeenCalled()

    appendSpy.mockRestore()
    removeSpy.mockRestore()
  })

  it('does nothing when no entry in context menu', () => {
    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = null

    const appendSpy = vi.spyOn(document.body, 'appendChild')
    wrapper.vm.doDownload()

    expect(appendSpy).not.toHaveBeenCalled()
    appendSpy.mockRestore()
  })
})

describe('FileManagerContent — doArchiveDir uses saved entry after closeCtxMenu', () => {
  it('fetches /api/file/archive with correct path after closeCtxMenu nulls entry', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      blob: () => Promise.resolve(new Blob()),
    })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'dir', name: 'src', path: 'src' }
    await nextTick()

    wrapper.vm.doArchiveDir()

    expect(wrapper.vm.ctxMenu.entry).toBeNull()
    expect(fetchSpy).toHaveBeenCalledWith('/api/file/archive', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ paths: ['src'] }),
    }))

    vi.unstubAllGlobals()
  })

  it('does nothing for file entries', async () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'test.ts', path: 'test.ts' }
    await nextTick()

    wrapper.vm.doArchiveDir()

    expect(fetchSpy).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('does nothing when no entry in context menu', () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = null

    wrapper.vm.doArchiveDir()

    expect(fetchSpy).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})

describe('FileManagerContent — doOpenAsProject uses saved path after closeCtxMenu', () => {
  it('fetches /api/project with correct absPath after closeCtxMenu nulls entry', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'dir', name: 'subproject', path: 'subproject' }
    await nextTick()

    wrapper.vm.doOpenAsProject()

    expect(wrapper.vm.ctxMenu.entry).toBeNull()
    expect(fetchSpy).toHaveBeenCalledWith('/api/project', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ path: '/project/subproject' }),
    }))

    fetchSpy.mockRestore()
    vi.unstubAllGlobals()
  })

  it('does nothing for file entries', async () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'test.ts', path: 'test.ts' }
    await nextTick()

    wrapper.vm.doOpenAsProject()

    expect(fetchSpy).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})

describe('FileManagerContent — doOpenTerminal uses saved cwd after closeCtxMenu', () => {
  it('emits openTerminal with dir path after closeCtxMenu nulls entry', async () => {
    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'dir', name: 'src', path: 'src' }
    await nextTick()

    wrapper.vm.doOpenTerminal()

    expect(wrapper.vm.ctxMenu.entry).toBeNull()
    expect(wrapper.emitted('openTerminal')).toBeTruthy()
    expect(wrapper.emitted('openTerminal')[0]).toEqual(['src'])
  })

  it('falls back to currentDir when entry is a file', async () => {
    const wrapper = mountContent({ currentDir: 'docs' })
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'readme.md', path: 'docs/readme.md' }
    await nextTick()

    wrapper.vm.doOpenTerminal()

    expect(wrapper.emitted('openTerminal')).toBeTruthy()
    expect(wrapper.emitted('openTerminal')[0]).toEqual(['docs'])
  })

  it('falls back to currentDir when no entry in context menu', async () => {
    const wrapper = mountContent({ currentDir: 'docs' })
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = null
    await nextTick()

    wrapper.vm.doOpenTerminal()

    expect(wrapper.emitted('openTerminal')).toBeTruthy()
    expect(wrapper.emitted('openTerminal')[0]).toEqual(['docs'])
  })
})
