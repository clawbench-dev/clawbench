/**
 * Shared setup for the four FileManagerContent suites.
 *
 * The spec outgrew one file (6.2k lines, 379 cases, ~4.5 minutes on its own,
 * and it was the file whose worker timed out during full-suite teardown).
 *
 * IMPORTANT — how the mocks work here:
 * `vi.mock()` is hoisted to the top of the module that calls it. A helper that
 * calls `vi.mock` and is imported by a suite does take effect for that suite,
 * but the hoisting means the FACTORY cannot close over anything the suite
 * defines afterwards. The factories here only use `vi.hoisted` state and
 * module-level `ref`s exported from this file, so re-importing this module
 * from each suite installs the identical mock set.
 *
 * Suites must therefore import this file for its side effects (the mocks) AND
 * for `mountContent` / `sampleEntries` / `readSource`.
 */
import { vi, beforeEach, afterEach } from 'vitest'

// Full-suite scheduling: these suites drive many async paths (DOM events,
// timers, uploads). They finish well under 1s in isolation, but under the
// coverage gate's full-suite run the worker pool is busy and the defaults
// occasionally flake.
//
// `hookTimeout` matters as much as `testTimeout`: the failure surfaced as
// "Hook timed out in 10000ms" from the shared `beforeEach` (which clears ~40
// mocks), not from the test body. Raising only testTimeout left that 10s
// default in place.
//
// This lives here rather than in each suite because `vi.setConfig` is per
// module — a suite that forgot it fell back to the 5s default and timed out on
// a case that takes ~5.4s under load.
vi.setConfig({ testTimeout: 60_000, hookTimeout: 60_000 })
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { reactive, ref, computed, defineComponent } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { createI18n } from 'vue-i18n'
import FileManagerContent from '@/components/file/FileManagerContent.vue'
import { cleanupDragGhost as cleanupDragGhostImpl } from '@/utils/attachDrag'
import { PANEL_KEY_PREFIX, DEFAULT_FM_PREVIEW_RATIO } from '@/utils/panelState'
export { PANEL_KEY_PREFIX, DEFAULT_FM_PREVIEW_RATIO }

import { cancelTarget, usePendingTarget } from '@/composables/useConversationTarget.ts'
export { cancelTarget, usePendingTarget }

// jsdom does not implement CSS.escape (used by scrollToEntryAndSelect).
const cssGlobal = globalThis as unknown as { CSS?: { escape?: (v: string) => string } }
if (typeof cssGlobal.CSS === 'undefined') {
  cssGlobal.CSS = {}
}
if (typeof cssGlobal.CSS.escape !== 'function') {
  cssGlobal.CSS.escape = (v: string) => String(v).replace(/[^a-zA-Z0-9_-]/g, (c) => `\\${c}`)
}

// Plugin to register the long-press directive globally
export const LongPressPlugin = {
  install(app: { directive: (n: string, d: unknown) => void }) {
    app.directive('long-press', { mounted: () => {}, unmounted: () => {} })
  },
}

// ── Mocks ──
// Hoisted: `useChatContext` is now evaluated while the component modules are
// imported (the preview pulls in the quote bar), which happens before ordinary
// top-level consts exist.
const {
  mockAddAttachedFile,
  mockHasAttachedFile,
  mockRemoveAttachedFileByPath,
  mockToggleAttachedFile,
} = vi.hoisted(() => ({
  mockAddAttachedFile: vi.fn(),
  mockHasAttachedFile: vi.fn(() => false),
  mockRemoveAttachedFileByPath: vi.fn(),
  mockToggleAttachedFile: vi.fn(),
}))

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

const mockIsAppMode = ref(false)
// Mutable so a test can flip the desktop-shell flag: the shortcut gate must
// distinguish "Android WebView" from "Electron desktop shell", and BOTH report
// isAppMode === true.
const mockIsDesktopApp = ref(false)
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: mockIsAppMode, isDesktopApp: mockIsDesktopApp }),
}))

const mockDialogConfirm = vi.hoisted(() => vi.fn(() => Promise.resolve(true)))
const mockDialogPrompt = vi.hoisted(() => vi.fn(() => Promise.resolve('newfile.txt')))
const mockDialogAlert = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: mockDialogConfirm,
    prompt: mockDialogPrompt,
    alert: mockDialogAlert,
  }),
}))

const mockDownloadFileByPath = vi.hoisted(() => vi.fn())
const mockPostForBlob = vi.hoisted(() => vi.fn(async () => ({ blob: null })))
vi.mock('@/utils/download', () => ({
  downloadFileByPath: mockDownloadFileByPath,
  postForBlobWithProgress: mockPostForBlob,
}))

// "Send by email" stages the path and opens the mail panel's composer; the
// manager only has to hand over the right path, so the helper is stubbed.
const mockSendPathsByMail = vi.hoisted(() => vi.fn(async () => ({ outcome: 'opened' })))
vi.mock('@/utils/mailAttach', () => ({
  sendPathsByMail: mockSendPathsByMail,
}))

const mockCopyText = vi.hoisted(() => vi.fn((_text: string, onSuccess?: () => void) => onSuccess?.()))
vi.mock('@/utils/clipboard', () => ({
  copyText: mockCopyText,
}))

vi.mock('@/composables/useTerminalStatus', () => ({
  useTerminalStatus: () => ({ terminalRuntimeEnabled: { value: true } }),
}))
const mockIsTouchPrimary = ref(true)
// HOST axis must be derived from the app-mode mocks, not hardcoded: the
// Android-gated branches (keyboard shortcuts, folder upload) read isAndroidApp,
// and the real composable computes it as `isAppMode && !isDesktopApp`. A frozen
// `ref(false)` would make every Android test take the desktop path.
vi.mock('@/composables/usePlatformDetect', () => ({
  usePlatformDetect: () => ({
    isAndroidApp: computed(() => mockIsAppMode.value && !mockIsDesktopApp.value),
    isNativeApp: computed(() => mockIsAppMode.value),
    isWebApp: computed(() => !mockIsAppMode.value),
    isElectron: computed(() => mockIsDesktopApp.value),
    isTouchPrimary: mockIsTouchPrimary,
  }),
}))

const mockHandleFileSelectToDir = vi.fn()
const mockHandleFileDropToDir = vi.fn()
const mockHandleFileDropToDirStructured = vi.fn()
const mockHandleFolderSelect = vi.fn()
const mockHandleFolderDropExpanded = vi.fn()
const mockDownloadDirAsTree = vi.fn()
const mockCancelDirUpload = vi.fn()
const mockDirUploading = ref(false)
const mockDirUploadProgress = ref(0)
const mockDirUploadTotal = ref(0)
const mockDirUploadDone = ref(0)

vi.mock('@/composables/useFileUpload', () => ({
  useFileUpload: () => ({
    dirUploading: mockDirUploading,
    dirUploadProgress: mockDirUploadProgress,
    dirUploadTotal: mockDirUploadTotal,
    dirUploadDone: mockDirUploadDone,
    handleFileSelectToDir: mockHandleFileSelectToDir,
    handleFileDropToDir: mockHandleFileDropToDir,
    handleFileDropToDirStructured: mockHandleFileDropToDirStructured,
    handleFolderSelect: mockHandleFolderSelect,
    handleFolderDropExpanded: mockHandleFolderDropExpanded,
    downloadDirAsTree: mockDownloadDirAsTree,
    cancelDirUpload: mockCancelDirUpload,
  }),
}))

vi.mock('@/composables/useFileNavStack', () => ({
  useFileNavStack: () => ({
    overlayOpen: { value: false },
  }),
}))

const mockToolbarCollapsedIds = vi.hoisted(() => ([]))

vi.mock('@/composables/useToolbarOverflow', () => ({
  useToolbarOverflow: () => ({
    inlineIds: computed(() => ['refresh', 'newFile', 'newFolder', 'upload', 'viewToggle', 'previewMode', 'multiselect', 'hidden', 'jump']),
    collapsedIds: computed(() => mockToolbarCollapsedIds),
    contentWidth: ref(800),
    startObserving: vi.fn(),
    stopObserving: vi.fn(),
  }),
}))

// ── Quick-preview composable mock ──
// The file manager owns an instance of useCodeLinkPreview gated on preview mode.
// Mock it so tests can assert open/close decisions without the real fetch/cache.
const mockShowPreview = vi.hoisted(() => vi.fn())
const mockClosePreview = vi.hoisted(() => vi.fn())
// One shared ref set across every useCodeLinkPreview() call, so a test can flip
// visibility and have the mounted component react (a per-call ref would not be
// reachable from the test).
const mockPreviewRefs = vi.hoisted(() => ({
  visible: null as { value: boolean } | null,
  mode: null as { value: string } | null,
  // The component reads `target.value.filePath` to skip re-showing the file the
  // pane already displays (keyboard highlight clamped at either end).
  target: null as { value: { filePath: string } | null } | null,
}))
vi.mock('@/composables/useCodeLinkPreview', () => {
  const visible = ref(false)
  const mode = ref('transient')
  const target = ref<{ filePath: string } | null>(null)
  mockPreviewRefs.visible = visible
  mockPreviewRefs.mode = mode
  mockPreviewRefs.target = target
  return {
    useCodeLinkPreview: (opts: { enabled?: { value: boolean } } = {}) => ({
      enabled: opts.enabled ?? { value: true },
      outsideClickIgnoreSelector: '.file-item, .grid-item',
      visible,
      mode,
      target,
      showPreview: mockShowPreview,
      close: mockClosePreview,
    }),
  }
})

// The directory-listing pane owns its own fetch (useDirPreview). Mock it so
// tests never hit /api/dir; the returned refs are shared so a test can drive
// the pane's loading/entries state.
const mockDirPreviewState = vi.hoisted(() => ({
  entries: [] as Array<{ name: string; type: string; target?: string }>,
  loading: false,
  error: false,
}))
vi.mock('@/composables/useDirPreview', async () => {
  const { ref } = await import('vue')
  const entries = ref(mockDirPreviewState.entries)
  const loading = ref(mockDirPreviewState.loading)
  const error = ref(mockDirPreviewState.error)
  return {
    useDirPreview: () => ({
      entries,
      loading,
      error,
      loadedPath: ref(''),
      visible: (e: { name: string }) => !e.name.startsWith('.'),
      refresh: vi.fn(),
    }),
  }
})

const { mockLocalConfig } = vi.hoisted(() => ({
  mockLocalConfig: { fileView: 'list', filePreviewMode: false } as Record<string, unknown>,
}))
// The component reads/watches the *reactive proxy*, so tests that need to
// trigger its watchers must mutate the proxy — not the raw target object.
const { mockLocalConfigProxy } = vi.hoisted(() => ({
  mockLocalConfigProxy: { current: null as Record<string, unknown> | null },
}))
vi.mock('@/composables/useSettingsConfig', () => {
  const proxy = reactive(mockLocalConfig)
  mockLocalConfigProxy.current = proxy as unknown as Record<string, unknown>
  return {
    localConfig: proxy,
    setLocalConfig: (key: string, value: unknown) => { mockLocalConfig[key] = value },
    useSettingsConfig: () => ({}),
    getZoomedViewport: () => ({ width: 1024, height: 768 }),
    toFixedCSS: (v: number) => Math.round(v * 100) / 100,
  }
})

const mockNavigateToDir = vi.hoisted(() => vi.fn())
vi.mock('@/stores/app', () => ({
  store: {
    state: { projectRoot: '/project', currentDir: '', currentFile: null, dirEntries: [] },
    loadGitBranch: vi.fn(),
    loadFiles: vi.fn(),
    selectFile: vi.fn(),
    setProject: vi.fn(),
    navigateToDir: mockNavigateToDir,
  },
}))

vi.mock('@/utils/fileType', () => ({
  getFileType: (name: string) => ({
    isMarkdown: name.endsWith('.md'),
    isHtml: false,
    isImage: /\.(png|jpg|jpeg|gif|svg|webp)$/i.test(name),
    isAudio: /\.(mp3|wav|ogg)$/i.test(name),
    isVideo: /\.(mp4|mov)$/i.test(name),
    isPdf: false,
    color: '#000',
  }),
}))

// No-op logger: keeps the 9999-retry loop test from flooding /api/client-log.
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Mock useFileRefresh: the refresh button spin is driven by the shared
// isRefreshing ref (which tracks the real refresh duration in the app).
const { mockIsRefreshing } = vi.hoisted(() => ({ mockIsRefreshing: { value: false } }))
vi.mock('@/composables/useFileRefresh', () => ({
  isRefreshing: mockIsRefreshing,
}))

// Hoisted so the vi.mock factory below can close over it.
const mockThumbable = vi.hoisted(() => ({ value: false }))

vi.mock('@/utils/fileManager', () => ({
  buildThumbUrl: (dir: string, name: string) => `/api/fs/thumb?target=${dir}/${name}`,
  isImage: (e: { name?: string }) => /\.(png|jpg|jpeg|gif|svg|webp)$/i.test(e.name || ''),
  isAudio: (e: { name?: string }) => /\.(mp3|wav|ogg)$/i.test(e.name || ''),
  isVideo: (e: { name?: string }) => /\.(mp4|mov)$/i.test(e.name || ''),
  // Controllable so the thumbnail lazy-mount tests can exercise the real
  // render path. Defaults to false, which is what the pre-existing tests
  // assume (no <img> is expected anywhere in this suite).
  isThumbable: (e?: { name?: string }) => mockThumbable.value && /\.(png|jpg|jpeg|gif)$/i.test(e?.name || ''),
  isThumbableExt: (path: string) => mockThumbable.value && /\.(png|jpg|jpeg|gif)$/i.test(path || ''),
  formatSize: (s: number) => {
    if (s >= 1024) return `${(s / 1024).toFixed(1)} KB`
    return `${s} B`
  },
  THUMBABLE_EXTS: [],
  numberedName: (baseName: string, index: number) => {
    const lastDot = baseName.lastIndexOf('.')
    if (lastDot <= 0) return `${baseName}_${index}`
    return `${baseName.slice(0, lastDot)}_${index}${baseName.slice(lastDot)}`
  },
  createMultiSelect: () => {
    const state = reactive({ active: false, selected: new Set() })
    return {
      state,
      enterMultiSelect: () => { state.active = true; state.selected.clear() },
      enterMultiSelectKeepSelection: () => { state.active = true },
      exitMultiSelect: () => { state.active = false; state.selected.clear() },
      toggleSelect: (path: string) => { if (state.selected.has(path)) state.selected.delete(path); else state.selected.add(path) },
    }
  },
  createClipboard: () => ({
    clipboard: reactive({ entries: [], isCut: false }),
    clear: vi.fn(),
  }),
  resolveClickAction: vi.fn(),
}))

vi.mock('@/components/file/DirBreadcrumb.vue', () => ({
  default: { template: '<div class="dir-breadcrumb-stub" />' },
}))

vi.mock('@/components/file/JumpDirDialog.vue', () => ({
  default: defineComponent({
    props: ['open'],
    emits: ['close', 'confirm'],
    template: '<div v-if="open" class="jump-dialog-stub" />',
  }),
}))

// Stub the share dialog: its own create/regenerate/revoke behaviour is covered
// by ShareLinkDialog.test.ts. Here we only assert the menu hands it the right
// target path, so the stub exposes that as an attribute.
vi.mock('@/components/file/ShareLinkDialog.vue', () => ({
  default: defineComponent({
    props: ['open', 'file', 'isDir'],
    emits: ['close'],
    template: '<div v-if="open" class="share-link-dialog-stub" :data-path="file && file.path" :data-isdir="String(!!isDir)" />',
  }),
}))

// Mock the shared path-annotation opener so the content-search delegation can be
// asserted without a real file fetch. The component must route a chosen hit
// through openFilePath — a bare `open-file-overlay` dispatch would skip
// store.selectFile and land on an empty viewer.
const mockOpenFilePath = vi.hoisted(() => vi.fn(async () => true))
vi.mock('@/composables/useFilePathAnnotation', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useFilePathAnnotation')>()
  return { ...actual, openFilePath: mockOpenFilePath }
})

// Stub the content-search dialog: its own behaviour is covered in
// ContentSearchDialog.test.ts. Here we only need to trigger its openFile emit.
vi.mock('@/components/file/ContentSearchDialog.vue', () => ({
  default: defineComponent({
    name: 'ContentSearchDialog',
    props: ['currentDir'],
    emits: ['openFile', 'close'],
    setup(_props, { expose }) {
      expose({ open: () => {}, close: () => {}, focusInput: () => {} })
      return () => null
    },
  }),
}))

// Mock useFileSearch so the inline search mode can be driven from tests
// without opening a real SSE connection.
const searchState = reactive({
  query: '',
  recursive: false,
  scope: 'current' as 'current' | 'global',
  exact: false,
  results: [] as Array<{ name: string; path: string; type: string; matchedIndices: number[] }>,
  searching: false,
  total: 0,
  truncated: false,
  searchBasePath: '',
})
const mockSearchStart = vi.hoisted(() => vi.fn())
const mockSearchCancel = vi.hoisted(() => vi.fn())
const mockSearchReset = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useFileSearch', () => ({
  useFileSearch: () => ({
    state: searchState,
    effectiveDir: { value: '' },
    // Mirror the real composable: global scope always recurses.
    effectiveRecursive: computed(() => searchState.scope === 'global' || searchState.recursive),
    startSearch: mockSearchStart,
    cancelSearch: mockSearchCancel,
    // Mirror the real reset(): clears the query so the results layer collapses.
    reset: () => {
      mockSearchReset()
      searchState.query = ''
      searchState.results = []
      searchState.total = 0
      searchState.truncated = false
      searchState.searchBasePath = ''
    },
    getDisplayLimit: () => 100,
  }),
}))

// ── i18n ──
export const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  // The `messages` below are a hand-picked SUBSET of the real locales, so every
  // key a component asks for that is not in it logs a warning: 20 keys x 384
  // mounts = ~77k lines, about 30s of this file's runtime.
  //
  // Silencing the warning here is safe because it does not hide a defect: a
  // genuinely missing key is caught by `src/i18n/__tests__/literalKeys.test.ts`,
  // which scans every literal `t('a.b.c')` in the source against the REAL
  // locale objects. What is missing here is only this file's convenience
  // dictionary — copying the real locales in would just be a second copy to
  // maintain, and would still need re-copying on every new string.
  //
  // These cases assert behaviour (did the click do the right thing, which
  // element rendered), not rendered copy, so nothing here depends on the text.
  missingWarn: false,
  fallbackWarn: false,
  messages: {
    zh: {
      file: {
        context: { newFile: '新建文件', newFolder: '新建文件夹', paste: '粘贴', rename: '重命名', delete: '删除', archiveDir: '归档', openAsProject: '打开为项目', copy: '复制', cut: '剪切', copyPath: '拷贝路径', pathCopied: '路径已拷贝', shareLink: '分享链接', shareExternal: '分享', sendByMail: '用邮件发送' },
        uploadHere: '上传到此处',
        dropToUpload: '松开上传到当前目录',
        pasteToUpload: '粘贴上传文件...',
        sortDefault: '排序',
        sortByName: '按名称',
        sortByTime: '按时间',
        sortByType: '按类型',
        sortBySize: '按大小',
        showHiddenFiles: '显示隐藏文件',
        hideHiddenFiles: '隐藏隐藏文件',
        viewList: '列表',
        viewGrid: '网格',
        previewModeOn: '开启预览模式',
        previewModeOff: '关闭预览模式',
        emptyDir: '空目录',
        noFiles: '无文件',
        truncateHint: '截断提示',
        gitIgnored: '已被 .gitignore 忽略',
        multiSelect: { allCopied: '已复制', allCut: '已剪切', confirmDelete: '确认删除', enter: '多选', exit: '退出', tapToSelect: '点击选择', selectedCount: '已选 {n} 项', selectAll: '全选', deselectAll: '取消全选', archive: '归档', share: '分享' },
        prompt: { fileName: '文件名', folderName: '文件夹名', newName: '新名称' },
        toast: { fileCreated: '已创建', folderCreated: '已创建', cutDone: '已剪切', moved: '已移动', createFailed: '创建失败', createFailedDetail: '创建失败', archiving: '归档中', archiveDone: '归档完成', archiveFailed: '归档失败', archiveFailedDetail: '归档失败', switchProjectFailed: '切换失败', switchProjectFailedShort: '切换失败', operationFailedDetail: '操作失败: {error}', mailNotConfigured: '还没有配置邮箱账号', mailSendFailed: '准备邮件附件失败' },
        search: { placeholder: '搜索文件名...', recursive: '递归搜索', exact: '精确匹配', scopeGlobal: '全局搜索', scopeCurrent: '当前目录', wordExact: '精确', wordRecursive: '递归', wordCurrent: '在当前目录', wordGlobal: '在当前项目下', wordVerb: '搜索', reset: '重置', noResults: '未找到文件', searching: '搜索中...', resultCount: '找到 {count} 个文件', resultCountPlus: '找到 {limit}+ 个文件', truncated: '如需找到更多文件，请输入更精确的关键词', searchFrom: '搜索范围: {path}' },
        nav: { parentDir: '返回上一级', fsRoot: '文件系统根目录', projectRoot: '项目根目录' },
      },
      chat: {
        actions: { attachToChat: '附加到聊天' },
        attach: { alreadyAttached: '已附加', addedToChat: '已添加到聊天', removedFromChat: '已从聊天移除', removeFromChat: '从聊天移除' },
      },
      common: { remove: '移除', copied: '已复制', delete: '删除', operationFailed: '操作失败', rename: '重命名', download: '下载', cancel: '取消' },
      nav: { refresh: '刷新', more: '更多' },
      search: { defaultPlaceholder: '搜索' },
      jump: {
        title: '跳转到目录',
        placeholder: '输入目录路径',
        confirm: '跳转',
        cancel: '取消',
        button: '跳转',
        copyPath: '复制路径',
      },
    },
  },
})

export const TeleportStub = { template: '<div><slot /></div>' }

// Stub the preview card: the file-manager tests assert that the preview was
// asked to open (via the exposed composable state), not the card internals.
export const CodeLinkPreviewStub = defineComponent({
  name: 'CodeLinkPreview',
  props: ['preview', 'docked'],
  emits: ['closed'],
  template: '<div class="code-link-preview-stub" :data-docked="String(!!docked)" />',
})

// Stub the directory-listing body: the tests assert which pane body was chosen
// and how its events are handled, not the listing's own rendering.
export const DirPreviewBodyStub = defineComponent({
  name: 'DirPreviewBody',
  props: ['entries', 'loading', 'error', 'visible', 'dirName', 'dirPath'],
  emits: ['open-file', 'open-dir', 'open-self', 'closed'],
  template: '<div class="dir-preview-stub" :data-count="entries.length" :data-dir-path="dirPath" />',
})

export const sampleEntries = [
  { name: 'src', type: 'dir', modified: '2025-01-01T00:00:00Z', size: 0 },
  { name: 'test.ts', type: 'file', modified: '2025-01-01T00:00:00Z', size: 100 },
  { name: 'readme.md', type: 'file', modified: '2025-01-02T00:00:00Z', size: 500 },
]

export function mountContent(props = {}) {
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
      stubs: { Teleport: TeleportStub, CodeLinkPreview: CodeLinkPreviewStub, DirPreviewBody: DirPreviewBodyStub },
      plugins: [i18n, LongPressPlugin],
      provide: {
        activeTab: { value: 'browse' },
        toast: { show: mockToastShow },
      },
    },
  })
}

beforeEach(() => {
  // Panel state lives in localStorage now (`fm-preview-open`, `fm-preview-ratio`),
  // so a test that opens the preview pane persists that choice and the NEXT
  // test's mount reads it back — the pane would already be open, `previewPaneEnabled`
  // would be true, and a test asserting "no split" would fail for a reason that
  // has nothing to do with what it is checking. Clearing here makes every case
  // start from the registry defaults instead of the previous case's leftovers.
  localStorage.clear()
  mockAddAttachedFile.mockReset()
  mockHasAttachedFile.mockReset()
  mockHasAttachedFile.mockReturnValue(false)
  mockToastShow.mockReset()
  mockSearchStart.mockReset()
  mockSearchCancel.mockReset()
  mockSearchReset.mockReset()
  searchState.query = ''
  searchState.recursive = false
  searchState.scope = 'current'
  searchState.exact = false
  searchState.results = []
  searchState.searching = false
  searchState.total = 0
  searchState.truncated = false
  searchState.searchBasePath = ''
  mockHandleFileSelectToDir.mockReset()
  mockHandleFileDropToDir.mockReset()
  mockHandleFileDropToDir.mockResolvedValue(undefined)
  mockHandleFileDropToDirStructured.mockReset()
  mockHandleFileDropToDirStructured.mockResolvedValue(undefined)
  mockHandleFolderSelect.mockReset()
  mockHandleFolderSelect.mockResolvedValue(undefined)
  mockIsTouchPrimary.value = true
  mockIsAppMode.value = false
  mockIsDesktopApp.value = false
  mockIsRefreshing.value = false
  mockToolbarCollapsedIds.length = 0
  mockDirUploading.value = false
  mockDirUploadProgress.value = 0
  mockDirUploadTotal.value = 0
  mockDirUploadDone.value = 0
  mockDialogConfirm.mockReset()
  mockDialogConfirm.mockResolvedValue(true)
  mockDialogPrompt.mockReset()
  mockDialogPrompt.mockResolvedValue('newfile.txt')
  mockDialogAlert.mockReset()
  mockDownloadFileByPath.mockReset()
  mockPostForBlob.mockReset()
  mockPostForBlob.mockResolvedValue({ blob: null })
  mockCopyText.mockReset()
  mockCopyText.mockImplementation((_text: string, onSuccess?: () => void) => onSuccess?.())
  mockSendPathsByMail.mockReset()
  mockSendPathsByMail.mockResolvedValue({ outcome: 'opened' })
  mockNavigateToDir.mockReset()
  mockShowPreview.mockReset()
  mockClosePreview.mockReset()
  // Shared preview refs: reset so a previous test's open pane doesn't leak in.
  mockPreviewRefs.visible!.value = false
  mockPreviewRefs.mode!.value = 'transient'
  mockPreviewRefs.target!.value = null
  // Clear IN PLACE: the useDirPreview mock captured this array in a ref at
  // import time, so reassigning the field would leave the ref pointing at the
  // old array and a test's push would never reach the component.
  mockDirPreviewState.entries.length = 0
  mockDirPreviewState.loading = false
  mockDirPreviewState.error = false
  // The settings mock is a single shared reactive object: a test that switches
  // the view mode would otherwise leak 'grid' into every later mount.
  mockLocalConfig.fileView = 'list'
  mockLocalConfig.filePreviewMode = false
})

// A dragstart without a matching dragend leaves the drag ghost (and its 5s
// safety timer) alive in `document.body`. The timer then fires while a LATER
// test — or no test at all — owns the DOM, which surfaces as an unhandled error
// attributed to whatever ran last. Clearing after every case keeps that timer
// inside the case that created it.
//
// Wrapped rather than re-exported: a bare `export { cleanupDragGhost }` from a
// module that also installs mocks resolved to undefined for the suites (the
// re-export sits after the mock factories, which vitest hoists above it).
export function cleanupDragGhost(): void {
  cleanupDragGhostImpl()
}

// Unmount every wrapper a case created, after that case ends.
//
// The component arms a highlight-retry timer when asked to scroll to an entry
// that is not in the listing yet, and the timer keeps firing while the listing
// is pending. Without unmounting, that loop outlives its own case: the next
// case's `Element.prototype.scrollIntoView` spy then records a scroll that
// belongs to the previous test, and `scrollSpy` assertions see phantom calls.
// `onUnmounted` clears the timer, so unmounting is the actual fix — the drag
// ghost cleanup below only covers a different timer.
enableAutoUnmount(afterEach)


/** Read the SFC source. jsdom does not load `<style>`, so style assertions must
 *  inspect the file — and cwd differs between a bare `vitest` run (web/) and
 *  scripts/vitest-run.sh (repo root), so probe both. */
export function readSource(): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, 'src/components/file/FileManagerContent.vue'), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error('FileManagerContent.vue not found from cwd: ' + process.cwd())
}

/**
 * Every mock handle the suites need, behind one object.
 *
 * `vi.hoisted()` results cannot be `export`ed — vitest rejects the module with
 * "Cannot export hoisted variable". They stay module-private and are published
 * here instead, so a suite writes `mocks.mockToastShow` and still gets the same
 * function instance the mock factory closed over.
 */
export const mocks = {
  mockCancelDirUpload,
  mockClosePreview,
  mockCopyText,
  mockDialogAlert,
  mockDialogConfirm,
  mockDialogPrompt,
  mockDirPreviewState,
  mockDirUploadDone,
  mockDirUploadProgress,
  mockDirUploadTotal,
  mockDirUploading,
  mockDownloadDirAsTree,
  mockDownloadFileByPath,
  mockHandleFileDropToDir,
  mockHandleFileDropToDirStructured,
  mockHandleFileSelectToDir,
  mockHandleFolderDropExpanded,
  mockHandleFolderSelect,
  mockIsAppMode,
  mockIsDesktopApp,
  mockIsTouchPrimary,
  mockNavigateToDir,
  mockOpenFilePath,
  mockPostForBlob,
  mockPreviewRefs,
  mockSearchCancel,
  mockSearchReset,
  mockSearchStart,
  mockShowPreview,
  mockThumbable,
  mockToastShow,
  mockToolbarCollapsedIds,
  searchState,
  mockLocalConfig,
  mockLocalConfigProxy,
  mockIsRefreshing,
  mockAddAttachedFile,
  mockHasAttachedFile,
  mockRemoveAttachedFileByPath,
  mockToggleAttachedFile,
  mockSendPathsByMail,
}
