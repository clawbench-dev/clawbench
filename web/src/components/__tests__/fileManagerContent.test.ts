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
})

describe('FileManagerContent — doDownload uses saved path/name after closeCtxMenu', () => {
  it('creates download link with correct path after closeCtxMenu nulls entry', async () => {
    vi.useFakeTimers()
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
    expect(anchor.href).toContain('docs/readme.md')
    expect(anchor.download).toBe('readme.md')
    expect(clickSpy).toHaveBeenCalled()

    // Flush the setTimeout cleanup in downloadFileByPath
    vi.advanceTimersByTime(1500)

    appendSpy.mockRestore()
    removeSpy.mockRestore()
    vi.useRealTimers()
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

describe('FileManagerContent — onSortSelect', () => {
  it('emits toggleSort and closes sort menu', async () => {
    const wrapper = mountContent()
    wrapper.vm.sortMenuOpen = true
    await nextTick()

    wrapper.vm.onSortSelect('name')

    expect(wrapper.emitted('toggleSort')).toBeTruthy()
    expect(wrapper.emitted('toggleSort')[0]).toEqual(['name'])
    expect(wrapper.vm.sortMenuOpen).toBe(false)
  })
})

describe('FileManagerContent — handleItemClick', () => {
  it('emits navigateDir when clicking a dir item', async () => {
    const wrapper = mountContent()
    const dirItem = document.createElement('div')
    dirItem.classList.add('file-item')
    dirItem.dataset.action = 'dir'
    dirItem.dataset.path = 'src'

    const event = { target: dirItem, ...new MouseEvent('click') }
    Object.defineProperty(event, 'target', { value: dirItem, writable: false })

    wrapper.vm.handleItemClick(event)

    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')[0]).toEqual(['src'])
  })

  it('emits selectFile when clicking a file item', async () => {
    const wrapper = mountContent()
    const fileItem = document.createElement('div')
    fileItem.classList.add('file-item')
    fileItem.dataset.action = 'file'
    fileItem.dataset.path = 'test.ts'

    const event = { target: fileItem, ...new MouseEvent('click') }
    Object.defineProperty(event, 'target', { value: fileItem, writable: false })

    wrapper.vm.handleItemClick(event)

    expect(wrapper.emitted('selectFile')).toBeTruthy()
    expect(wrapper.emitted('selectFile')[0]).toEqual(['test.ts'])
  })

  it('does nothing when loading', async () => {
    const wrapper = mountContent({ dirLoading: true })
    const fileItem = document.createElement('div')
    fileItem.classList.add('file-item')
    fileItem.dataset.action = 'file'
    fileItem.dataset.path = 'test.ts'

    const event = { target: fileItem, ...new MouseEvent('click') }
    Object.defineProperty(event, 'target', { value: fileItem, writable: false })

    wrapper.vm.handleItemClick(event)

    expect(wrapper.emitted('selectFile')).toBeFalsy()
  })
})

describe('FileManagerContent — filteredEntries', () => {
  it('filters by search query', async () => {
    const wrapper = mountContent()
    wrapper.vm._setSearchQuery('test')
    await nextTick()

    const filtered = wrapper.vm._getFilteredEntries()
    expect(filtered.length).toBe(1)
    expect(filtered[0].name).toBe('test.ts')
  })

  it('hides hidden files when showHidden is false', async () => {
    const entries = [
      { name: '.git', type: 'dir', modified: '2025-01-01T00:00:00Z', size: 0 },
      { name: 'src', type: 'dir', modified: '2025-01-01T00:00:00Z', size: 0 },
    ]
    const wrapper = mountContent({ entries, showHidden: false })

    const filtered = wrapper.vm._getFilteredEntries()
    expect(filtered.length).toBe(1)
    expect(filtered[0].name).toBe('src')
  })

  it('shows hidden files when showHidden is true', async () => {
    const entries = [
      { name: '.git', type: 'dir', modified: '2025-01-01T00:00:00Z', size: 0 },
      { name: 'src', type: 'dir', modified: '2025-01-01T00:00:00Z', size: 0 },
    ]
    const wrapper = mountContent({ entries, showHidden: true })

    const filtered = wrapper.vm._getFilteredEntries()
    expect(filtered.length).toBe(2)
  })
})

describe('FileManagerContent — viewMode', () => {
  it('switches to grid view', async () => {
    const wrapper = mountContent()
    wrapper.vm._setViewMode('grid')
    await nextTick()

    expect(wrapper.vm.viewMode).toBe('grid')
  })

  it('defaults to list view', async () => {
    const wrapper = mountContent()
    expect(wrapper.vm.viewMode).toBe('list')
    expect(wrapper.find('.file-list').exists()).toBe(true)
  })
})

describe('FileManagerContent — doRename', () => {
  it('emits rename event with path and new name', async () => {
    const wrapper = mountContent()
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'old.txt', path: 'old.txt' }
    await nextTick()

    await wrapper.vm.doRename()

    expect(wrapper.emitted('rename')).toBeTruthy()
  })
})

describe('FileManagerContent — entryIcon/entryIconColor', () => {
  it('returns Folder for dir entries', () => {
    const wrapper = mountContent()
    const icon = wrapper.vm.entryIcon({ type: 'dir' })
    expect(icon).toBeDefined()
  })

  it('returns FileText for plain file entries', () => {
    const wrapper = mountContent()
    const icon = wrapper.vm.entryIcon({ type: 'file', name: 'test.ts' })
    expect(icon).toBeDefined()
  })
})

describe('FileManagerContent — formatDate', () => {
  it('returns formatted date for today', () => {
    const wrapper = mountContent()
    const today = new Date().toISOString()
    const result = wrapper.vm.formatDate(today)
    expect(result).toBeTruthy()
  })

  it('returns formatted date for past dates', () => {
    const wrapper = mountContent()
    const result = wrapper.vm.formatDate('2024-06-01T00:00:00Z')
    expect(result).toBeTruthy()
  })

  it('returns empty string for null modified', () => {
    const wrapper = mountContent()
    const result = wrapper.vm.formatDate(null)
    expect(result).toBe('')
  })
})

describe('FileManagerContent — onLongPress', () => {
  it('sets context menu from long press event', async () => {
    const wrapper = mountContent()

    const entry = { type: 'file', name: 'test.ts' }
    const touchEvent = { touches: [{ clientX: 100, clientY: 200 }] }

    wrapper.vm.onLongPress(entry, touchEvent)
    await nextTick()

    expect(wrapper.vm.ctxMenu.visible).toBe(true)
    expect(wrapper.vm.ctxMenu.entry.path).toBe('test.ts')
  })
})

describe('FileManagerContent — doBatchCopy/doBatchCut/doBatchDelete', () => {
  it('doBatchCopy copies selected entries to clipboard', async () => {
    const wrapper = mountContent()
    wrapper.vm.multiSelectState.active = true
    wrapper.vm.multiSelectState.selected.add('test.ts')
    await nextTick()

    wrapper.vm.doBatchCopy()

    expect(mockToastShow).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'success' }),
    )
  })

  it('doBatchCut cuts selected entries', async () => {
    const wrapper = mountContent()
    wrapper.vm.multiSelectState.active = true
    wrapper.vm.multiSelectState.selected.add('test.ts')
    await nextTick()

    wrapper.vm.doBatchCut()

    expect(mockToastShow).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'success' }),
    )
  })

  it('doBatchDelete emits batchDelete with paths', async () => {
    const wrapper = mountContent()
    wrapper.vm.multiSelectState.active = true
    wrapper.vm.multiSelectState.selected.add('test.ts')
    await nextTick()

    await wrapper.vm.doBatchDelete()

    expect(wrapper.emitted('batchDelete')).toBeTruthy()
  })
})

describe('FileManagerContent — doBatchArchive', () => {
  it('calls doArchive with selected paths', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      blob: () => Promise.resolve(new Blob()),
    })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    wrapper.vm.multiSelectState.active = true
    wrapper.vm.multiSelectState.selected.add('test.ts')
    await nextTick()

    wrapper.vm.doBatchArchive()

    expect(fetchSpy).toHaveBeenCalledWith('/api/file/archive', expect.objectContaining({
      method: 'POST',
    }))

    vi.unstubAllGlobals()
  })
})

describe('FileManagerContent — doPaste', () => {
  it('calls copy API for non-cut clipboard', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    // Set clipboard entries
    wrapper.vm.clipboard.entries = [{ type: 'file', name: 'file.txt', path: 'file.txt' }]
    wrapper.vm.clipboard.isCut = false
    await nextTick()

    await wrapper.vm.doPaste()

    expect(fetchSpy).toHaveBeenCalledWith('/api/file/copy', expect.objectContaining({
      method: 'POST',
    }))

    vi.unstubAllGlobals()
  })

  it('calls move API for cut clipboard', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    wrapper.vm.clipboard.entries = [{ type: 'file', name: 'file.txt', path: 'src/file.txt' }]
    wrapper.vm.clipboard.isCut = true
    await nextTick()

    await wrapper.vm.doPaste()

    expect(fetchSpy).toHaveBeenCalledWith('/api/file/move', expect.objectContaining({
      method: 'POST',
    }))

    vi.unstubAllGlobals()
  })

  it('skips API call for same-path cut (no-op)', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent({ currentDir: 'src' })
    wrapper.vm.clipboard.entries = [{ type: 'file', name: 'file.txt', path: 'src/file.txt' }]
    wrapper.vm.clipboard.isCut = true
    wrapper.vm.ctxMenu.entry = null
    await nextTick()

    await wrapper.vm.doPaste()

    expect(fetchSpy).not.toHaveBeenCalled()

    vi.unstubAllGlobals()
  })

  it('preserves clipboard on cut-paste failure', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: false, status: 500, text: () => Promise.resolve('error') })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    wrapper.vm.clipboard.entries = [{ type: 'file', name: 'file.txt', path: 'src/file.txt' }]
    wrapper.vm.clipboard.isCut = true
    await nextTick()

    await wrapper.vm.doPaste()

    // Clipboard should be preserved on failure so user can retry
    expect(wrapper.vm.clipboard.entries.length).toBe(1)

    vi.unstubAllGlobals()
  })

  it('clears clipboard on successful cut-paste', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    wrapper.vm.clipboard.entries = [{ type: 'file', name: 'file.txt', path: 'src/file.txt' }]
    wrapper.vm.clipboard.isCut = true
    await nextTick()

    await wrapper.vm.doPaste()

    expect(wrapper.vm.clipboard.entries.length).toBe(0)

    vi.unstubAllGlobals()
  })
})

describe('FileManagerContent — doNewFile / doNewFolder', () => {
  it('calls create file API', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    await wrapper.vm.doNewFile()

    expect(fetchSpy).toHaveBeenCalledWith('/api/file/create', expect.objectContaining({
      method: 'POST',
    }))

    vi.unstubAllGlobals()
  })

  it('calls create dir API', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchSpy)

    const wrapper = mountContent()
    await wrapper.vm.doNewFolder()

    expect(fetchSpy).toHaveBeenCalledWith('/api/dir/create', expect.objectContaining({
      method: 'POST',
    }))

    vi.unstubAllGlobals()
  })
})

describe('FileManagerContent — isTerminalDisabled', () => {
  it('is true when terminal is not enabled', async () => {
    const wrapper = mountContent()
    expect(wrapper.vm.isTerminalDisabled).toBe(true)
  })
})
