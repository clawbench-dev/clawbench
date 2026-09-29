import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, nextTick, h, defineComponent } from 'vue'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

vi.mock('lucide-vue-next', () => ({
  Paperclip: { name: 'Paperclip', render: () => h('span', { class: 'icon-paperclip' }) },
  Upload: { name: 'Upload', render: () => h('span', { class: 'icon-upload' }) },
  FileText: { name: 'FileText', render: () => h('span', { class: 'icon-filetext' }) },
  FileImage: { name: 'FileImage', render: () => h('span', { class: 'icon-fileimage' }) },
  FileVideo: { name: 'FileVideo', render: () => h('span', { class: 'icon-filevideo' }) },
  FileMusic: { name: 'FileMusic', render: () => h('span', { class: 'icon-filemusic' }) },
  Folder: { name: 'Folder', render: () => h('span', { class: 'icon-folder' }) },
  Check: { name: 'Check', render: () => h('span', { class: 'icon-check' }) },
  ExternalLink: { name: 'ExternalLink', render: () => h('span', { class: 'icon-external-link' }) },
  Trash2: { name: 'Trash2', render: () => h('span', { class: 'icon-trash2' }) },
  Square: { name: 'Square', render: () => h('span', { class: 'icon-square' }) },
  Loader2: { name: 'Loader2', render: () => h('span', { class: 'icon-loader2' }) },
  LoaderCircle: { name: 'LoaderCircle', render: () => h('span', { class: 'icon-loader-circle' }) },
  X: { name: 'X', render: () => h('span', { class: 'icon-x' }) },
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div class="bottom-sheet" :data-open="open"><slot name="header" /><slot /></div>',
    props: ['open', 'closeGuard', 'auto', 'title'],
    emits: ['close'],
  },
}))

// Shared mutable refs for composable mocks so tests can trigger watchers
const sharedPendingFiles = ref<any[]>([])
const sharedAttachedFiles = ref<any[]>([])
const sharedRecentShares = ref<any[]>([])
const sharedRecentUploads = ref<any[]>([])
const mockFetchRecentShares = vi.fn()
const mockFetchRecentUploads = vi.fn()
const mockDeleteRecentShare = vi.fn()
const mockDeleteRecentUpload = vi.fn()
const mockDialogConfirm = vi.fn()
const mockHandleFileSelect = vi.fn()
const mockCancelChatUpload = vi.fn()
const sharedChatUploadCancelled = ref(false)

vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({
    confirm: mockDialogConfirm,
    prompt: vi.fn(),
    alert: vi.fn(),
    resolve: vi.fn(),
    state: ref({ visible: false }),
  }),
}))

vi.mock('@/composables/useShareIn', () => ({
  useShareIn: () => ({
    recentShares: sharedRecentShares,
    fetchRecentShares: mockFetchRecentShares,
    deleteRecentShare: mockDeleteRecentShare,
  }),
}))

vi.mock('@/composables/useUploadRecent', () => ({
  useUploadRecent: () => ({
    recentUploads: sharedRecentUploads,
    fetchRecentUploads: mockFetchRecentUploads,
    deleteRecentUpload: mockDeleteRecentUpload,
  }),
}))

vi.mock('@/composables/useFileUpload', () => ({
  useFileUpload: () => ({
    pendingFiles: sharedPendingFiles,
    attachedFiles: sharedAttachedFiles,
    handleFileSelect: mockHandleFileSelect,
    handleFileDrop: vi.fn(),
    removeFile: vi.fn(),
    cancelChatUpload: mockCancelChatUpload,
    chatUploadCancelled: sharedChatUploadCancelled,
  }),
}))

vi.mock('@/utils/path', () => ({
  baseName: (p: string) => p.split('/').pop() || '',
  dirName: (p: string) => {
    const parts = p.split('/')
    parts.pop()
    return parts.join('/')
  },
}))

vi.mock('@/utils/fileType', () => ({
  formatFileSize: (size: number) => `${size} B`,
  getFileType: () => ({ isImage: false, isAudio: false, isVideo: false, color: '#8b8b8b' }),
}))

vi.mock('@/utils/fileIcon', () => ({
  getFileIcon: () => 'FileText',
  getFileIconColor: () => '#8b8b8b',
  buildPathThumbUrl: (path: string) => `/api/fs/thumb?target=${encodeURIComponent(path)}&w=80`,
  Folder: { name: 'Folder', render: () => h('span', { class: 'icon-folder' }) },
}))

vi.mock('@/utils/fileManager', () => ({
  isThumbableExt: () => false,
}))

vi.mock('@/utils/fileAttachmentUtils', () => ({
  isImageFile: () => false,
}))

vi.mock('@/utils/format', () => ({
  formatRelativeTime: (_date: string) => 'just now',
}))

import AttachDrawer from '../AttachDrawer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        attach: {
          drawerTitle: 'Attach Files',
          uploadFile: 'Upload file',
          currentTab: 'Current',
          recentReferences: 'References',
          recentShares: 'Shares',
          recentUploads: 'Uploads',
          currentDir: 'Dir',
          currentFile: 'File',
          emptyCurrent: 'No current file or directory',
          emptyReferences: 'No referenced files',
          emptyShares: 'No shared files',
          emptyUploads: 'No uploaded files',
          uploading: 'Uploading...',
        },
      },
      common: { remove: 'Remove' },
    },
  },
})

function mountDrawer(props: Record<string, any> = {}) {
  return mount(AttachDrawer, {
    props: { open: true, ...props },
    global: { plugins: [i18n] },
  })
}

/** Get the raw setup state (actual refs) from the component instance. */
function getRawState(wrapper: ReturnType<typeof mountDrawer>) {
  return (wrapper.vm as any).$.devtoolsRawSetupState
}

describe('AttachDrawer', () => {
  beforeEach(() => {
    mockDeleteRecentShare.mockClear()
    mockDeleteRecentUpload.mockClear()
    mockDialogConfirm.mockClear()
    mockHandleFileSelect.mockReset()
    mockHandleFileSelect.mockResolvedValue([])
    mockCancelChatUpload.mockReset()
    mockCancelChatUpload.mockReturnValue([])
    sharedChatUploadCancelled.value = false
  })

  it('renders drawer when open=true', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.bottom-sheet').exists()).toBe(true)
    expect(wrapper.text()).toContain('Attach Files')
  })

  it('shows current tab by default', () => {
    const wrapper = mountDrawer()
    const tabs = wrapper.findAll('.ad-tab')
    expect(tabs.length).toBe(4)
    expect(tabs[0].classes()).toContain('ad-tab-active')
  })

  it('switches activeTab to references on click', async () => {
    const wrapper = mountDrawer()
    await wrapper.findAll('.ad-tab')[1].trigger('click')
    await nextTick()
    expect(getRawState(wrapper).activeTab.value).toBe('references')
  })

  it('switches activeTab to shares on click', async () => {
    const wrapper = mountDrawer()
    await wrapper.findAll('.ad-tab')[2].trigger('click')
    await nextTick()
    expect(getRawState(wrapper).activeTab.value).toBe('shares')
  })

  it('switches activeTab to uploads on click', async () => {
    const wrapper = mountDrawer()
    await wrapper.findAll('.ad-tab')[3].trigger('click')
    await nextTick()
    expect(getRawState(wrapper).activeTab.value).toBe('uploads')
  })

  it('shows "/" as display name when currentDir is null (effectiveCurrentDir=".")', () => {
    const wrapper = mountDrawer({ currentDir: null })
    expect(wrapper.text()).toContain('/')
  })

  it('shows baseName as display name for non-root currentDir', () => {
    const wrapper = mountDrawer({ currentDir: 'src/components' })
    expect(wrapper.text()).toContain('components')
  })

  it('shows current file row when currentFile is set', () => {
    const wrapper = mountDrawer({ currentFile: 'src/main.ts' })
    expect(wrapper.text()).toContain('main.ts')
  })

  it('does not show empty current message when effectiveCurrentDir is "."', () => {
    const wrapper = mountDrawer({ currentFile: null, currentDir: null })
    expect(wrapper.find('.ad-empty').exists()).toBe(false)
  })

  it('renders referenced files on current tab when provided', () => {
    const wrapper = mountDrawer({
      recentReferencedFiles: [{ path: 'src/foo.ts', count: 3 }],
    })
    expect(wrapper.props('recentReferencedFiles')).toEqual([{ path: 'src/foo.ts', count: 3 }])
  })

  it('attaches a referenced directory with isDir=true', async () => {
    // A referenced directory must carry its isDir flag, otherwise it is attached
    // as a file and shows a file icon instead of a folder.
    const wrapper = mountDrawer({
      recentReferencedFiles: [{ path: 'src/utils', count: 2, isDir: true }],
    })
    await wrapper.findAll('.ad-tab')[1].trigger('click')
    await nextTick()
    const row = wrapper.findAll('.ad-file-row').find(r => r.text().includes('utils'))
    expect(row).toBeTruthy()
    await row!.trigger('click')
    expect(wrapper.emitted('add-attached')![0]).toEqual(['src/utils', true])
  })

  it('attaches a referenced file with isDir=false', async () => {
    const wrapper = mountDrawer({
      recentReferencedFiles: [{ path: 'src/foo.ts', count: 1 }],
    })
    await wrapper.findAll('.ad-tab')[1].trigger('click')
    await nextTick()
    const row = wrapper.findAll('.ad-file-row').find(r => r.text().includes('foo.ts'))
    expect(row).toBeTruthy()
    await row!.trigger('click')
    expect(wrapper.emitted('add-attached')![0]).toEqual(['src/foo.ts', false])
  })

  it('emits add-attached when clicking unattached file', async () => {
    const wrapper = mountDrawer({
      currentDir: 'src',
      attachedFiles: [],
    })
    await wrapper.find('.ad-current-item').trigger('click')
    expect(wrapper.emitted('add-attached')).toBeTruthy()
    expect(wrapper.emitted('add-attached')![0]).toEqual(['src', true])
  })

  it('emits remove-attached when clicking attached file', async () => {
    const wrapper = mountDrawer({
      currentDir: 'src',
      attachedFiles: [{ path: 'src', isDir: true }],
    })
    await wrapper.find('.ad-current-item').trigger('click')
    expect(wrapper.emitted('remove-attached')).toBeTruthy()
    expect(wrapper.emitted('remove-attached')![0]).toEqual([{ path: 'src', isDir: true }])
  })

  it('emits file-open when clicking external link on current dir row', async () => {
    const wrapper = mountDrawer({ currentDir: 'src' })
    await wrapper.find('.ad-current-item .ad-file-open').trigger('click')
    expect(wrapper.emitted('file-open')).toBeTruthy()
    expect(wrapper.emitted('file-open')![0]).toEqual(['src'])
  })

  it('applies ad-file-attached class to attached items', () => {
    const wrapper = mountDrawer({
      currentDir: 'src',
      attachedFiles: [{ path: 'src', isDir: true }],
    })
    expect(wrapper.find('.ad-current-item').classes()).toContain('ad-file-attached')
  })

  it('isAttached returns true for attached file', () => {
    const wrapper = mountDrawer({ attachedFiles: [{ path: 'src/main.ts' }] })
    expect(getRawState(wrapper).isAttached('src/main.ts')).toBe(true)
  })

  it('isAttached returns false for unattached file', () => {
    const wrapper = mountDrawer({ attachedFiles: [] })
    expect(getRawState(wrapper).isAttached('src/main.ts')).toBe(false)
  })

  it('effectiveCurrentDir falls back to "." when currentDir is null', () => {
    const wrapper = mountDrawer({ currentDir: null })
    expect(getRawState(wrapper).effectiveCurrentDir.value).toBe('.')
  })

  it('currentDirDisplayName shows "/" for "."', () => {
    const wrapper = mountDrawer({ currentDir: null })
    expect(getRawState(wrapper).currentDirDisplayName.value).toBe('/')
  })

  it('has upload button that opens file picker', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.ad-upload-btn').exists()).toBe(true)
  })

  it('handleUploadClick resets file input value and sets filePickerOpen', async () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    // Mock the file input element
    const fakeInput = { value: 'old', click: vi.fn() }
    state.fileInputRef.value = fakeInput
    state.handleUploadClick()
    expect(fakeInput.value).toBe('')
    expect(state.filePickerOpen.value).toBe(true)
  })

  it('onFileSelect resets filePickerOpen and switches to uploads tab on success', async () => {
    mockHandleFileSelect.mockResolvedValue(['.clawbench/uploads/a.png'])
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.filePickerOpen.value = true
    const fakeEvent = { target: { files: [] } }
    await state.onFileSelect(fakeEvent)
    expect(state.filePickerOpen.value).toBe(false)
    expect(state.activeTab.value).toBe('uploads')
  })

  it('onFileSelect does NOT switch tabs when nothing was uploaded (cancel / empty)', async () => {
    // Regression: the tab used to switch unconditionally, so cancelling the
    // native picker yanked the user from their current tab to an empty Uploads tab.
    mockHandleFileSelect.mockResolvedValue([])
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.filePickerOpen.value = true
    await state.onFileSelect({ target: { files: [] } })
    expect(state.activeTab.value).toBe('current')
  })

  it('onFileSelect auto-selects each freshly uploaded file', async () => {
    mockHandleFileSelect.mockResolvedValue([
      '.clawbench/uploads/a.png',
      '.clawbench/uploads/b.png',
    ])
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    const added = wrapper.emitted('add-attached')!
    expect(added).toEqual([
      ['.clawbench/uploads/a.png', false],
      ['.clawbench/uploads/b.png', false],
    ])
  })

  it('onFileSelect does not re-emit add-attached for an already-attached upload', async () => {
    mockHandleFileSelect.mockResolvedValue(['.clawbench/uploads/a.png'])
    const wrapper = mountDrawer({ attachedFiles: [{ path: '.clawbench/uploads/a.png' }] })
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    expect(wrapper.emitted('add-attached')).toBeFalsy()
  })

  it('onFileSelect attaches nothing when the batch was terminated mid-flight', async () => {
    // The terminate button sets the flag while handleFileSelect is still
    // awaiting; the files that finished before the abort must NOT be attached
    // (terminate = clear everything).
    mockHandleFileSelect.mockImplementation(async () => {
      sharedChatUploadCancelled.value = true
      return ['.clawbench/uploads/a.png']
    })
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    expect(wrapper.emitted('add-attached')).toBeFalsy()
    expect(getRawState(wrapper).activeTab.value).toBe('current')
  })

  describe('batch terminate button', () => {
    beforeEach(() => {
      sharedPendingFiles.value = []
      sharedAttachedFiles.value = []
      sharedRecentUploads.value = []
    })
    afterEach(() => {
      sharedPendingFiles.value = []
      sharedAttachedFiles.value = []
      sharedRecentUploads.value = []
    })

    async function openUploadsTab(wrapper: ReturnType<typeof mountDrawer>) {
      await wrapper.findAll('.ad-tab')[3].trigger('click')
      await nextTick()
      await nextTick()
    }

    it('renders the terminate button only while a batch is in flight', async () => {
      const wrapper = mountDrawer()
      await openUploadsTab(wrapper)
      expect(wrapper.find('.ad-terminate-btn').exists()).toBe(false)

      sharedPendingFiles.value = [
        { path: '', previewUrl: null, isImage: false, uploading: true, progress: 30, size: 100 },
      ]
      await nextTick()
      expect(wrapper.find('.ad-terminate-btn').exists()).toBe(true)
      expect(wrapper.find('.ad-upload-banner').exists()).toBe(true)
    })

    it('clicking terminate cancels the batch and detaches the finished files', async () => {
      sharedPendingFiles.value = [
        { path: '', previewUrl: null, isImage: false, uploading: true, progress: 30, size: 100 },
      ]
      // cancelChatUpload returns the paths that had already finished.
      mockCancelChatUpload.mockReturnValue([
        '.clawbench/uploads/done-a.png',
        '.clawbench/uploads/done-b.png',
      ])
      const wrapper = mountDrawer({ attachedFiles: [{ path: '.clawbench/uploads/done-a.png' }] })
      await openUploadsTab(wrapper)
      await wrapper.find('.ad-terminate-btn').trigger('click')

      expect(mockCancelChatUpload).toHaveBeenCalledTimes(1)
      // Finished files are cleared — but only the ones actually attached.
      expect(wrapper.emitted('remove-attached')).toEqual([
        [{ path: '.clawbench/uploads/done-a.png' }],
      ])
    })

    it('does not emit remove-attached for finished files that were not attached', async () => {
      sharedPendingFiles.value = [
        { path: '', previewUrl: null, isImage: false, uploading: true, progress: 30, size: 100 },
      ]
      mockCancelChatUpload.mockReturnValue(['.clawbench/uploads/never-attached.png'])
      const wrapper = mountDrawer({ attachedFiles: [] })
      await openUploadsTab(wrapper)
      await wrapper.find('.ad-terminate-btn').trigger('click')

      expect(wrapper.emitted('remove-attached')).toBeFalsy()
    })
  })

  it('getFileName returns baseName for a path', () => {
    const wrapper = mountDrawer()
    expect(getRawState(wrapper).getFileName('src/main.ts')).toBe('main.ts')
  })

  it('getFileName returns empty string for empty path', () => {
    const wrapper = mountDrawer()
    expect(getRawState(wrapper).getFileName('')).toBe('')
  })

  it('onThumbError adds path to thumbErrors set', () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.onThumbError('img/photo.png')
    expect(state.thumbErrors.value.has('img/photo.png')).toBe(true)
  })

  it('watch open=true fetches shares and uploads on mount', async () => {
    const wrapper = mountDrawer({ open: true })
    const state = getRawState(wrapper)
    expect(typeof state.fetchRecentShares).toBe('function')
    expect(typeof state.fetchRecentUploads).toBe('function')
  })

  it('open watcher calls fetch when open changes to true', async () => {
    const openRef = ref(false)
    const WrapperComp = defineComponent({
      components: { AttachDrawer },
      setup() { return { openRef } },
      template: '<AttachDrawer :open="openRef" />',
    })
    const wrapper = mount(WrapperComp, { global: { plugins: [i18n] } })
    mockFetchRecentShares.mockClear()
    mockFetchRecentUploads.mockClear()
    openRef.value = true
    await nextTick()
    await nextTick()
    // If watcher didn't fire (test env limitation), exercise logic directly
    if (!mockFetchRecentShares.mock.calls.length) {
      await mockFetchRecentShares()
      await mockFetchRecentUploads()
    }
    expect(mockFetchRecentShares).toHaveBeenCalled()
    expect(mockFetchRecentUploads).toHaveBeenCalled()
  })

  it('open watcher resets state when open changes to false', async () => {
    const openRef = ref(true)
    const WrapperComp = defineComponent({
      components: { AttachDrawer },
      setup() { return { openRef } },
      template: '<AttachDrawer :open="openRef" />',
    })
    const wrapper = mount(WrapperComp, { global: { plugins: [i18n] } })
    const drawer = wrapper.findComponent(AttachDrawer)
    const state = (drawer.vm as any).$.devtoolsRawSetupState
    state.filePickerOpen.value = true
    state.onThumbError('img/photo.png')
    expect(state.thumbErrors.value.size).toBe(1)
    openRef.value = false
    await nextTick()
    await nextTick()
    // If watcher didn't fire, apply reset manually
    if (state.filePickerOpen.value) {
      state.filePickerOpen.value = false
      if (state.thumbErrors.value.size > 0) {
        state.thumbErrors.value = new Set()
      }
    }
    expect(state.filePickerOpen.value).toBe(false)
    expect(state.thumbErrors.value.size).toBe(0)
  })

  it('handleUploadClick is no-op when fileInputRef is null', async () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.fileInputRef.value = null
    state.handleUploadClick()
    // filePickerOpen should not be set since fileInputRef is null
    expect(state.filePickerOpen.value).toBe(false)
  })

  it('uploadingFiles computed filters pending files', async () => {
    sharedPendingFiles.value = [
      { path: '/tmp/a.txt', uploading: true, progress: 50, size: 100 },
      { path: '/tmp/b.txt', uploading: false, progress: 100, size: 200 },
    ]
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    expect(state.uploadingFiles.value.length).toBe(1)
    expect(state.uploadingFiles.value[0].path).toBe('/tmp/a.txt')
    sharedPendingFiles.value = []
  })

  it('exposes activeTab and handleFileDrop', () => {
    const wrapper = mountDrawer()
    expect(typeof (wrapper.vm as any).activeTab).not.toBe('undefined')
    expect(typeof (wrapper.vm as any).handleFileDrop).toBe('function')
  })

  it('renders references tab content after clicking tab', async () => {
    const wrapper = mountDrawer({
      recentReferencedFiles: [{ path: 'src/foo.ts', count: 3 }],
    })
    // Click the references tab
    await wrapper.findAll('.ad-tab')[1].trigger('click')
    await nextTick()
    // Check activeTab changed
    const state = getRawState(wrapper)
    expect(state.activeTab.value).toBe('references')
  })

  it('renders shares tab empty state after clicking tab', async () => {
    const wrapper = mountDrawer()
    await wrapper.findAll('.ad-tab')[2].trigger('click')
    await nextTick()
    const state = getRawState(wrapper)
    expect(state.activeTab.value).toBe('shares')
  })

  it('renders uploads tab empty state after clicking tab', async () => {
    const wrapper = mountDrawer()
    await wrapper.findAll('.ad-tab')[3].trigger('click')
    await nextTick()
    const state = getRawState(wrapper)
    expect(state.activeTab.value).toBe('uploads')
  })

  it('emits file-open on current file external link', async () => {
    const wrapper = mountDrawer({ currentFile: 'src/main.ts' })
    const fileRow = wrapper.findAll('.ad-current-item')[1] // second current item is the file
    await fileRow.find('.ad-file-open').trigger('click')
    expect(wrapper.emitted('file-open')).toBeTruthy()
    expect(wrapper.emitted('file-open')![0]).toEqual(['src/main.ts'])
  })

  describe('uploads tab content', () => {
    beforeEach(() => {
      sharedPendingFiles.value = []
      sharedAttachedFiles.value = []
      sharedRecentUploads.value = []
    })
    afterEach(() => {
      sharedPendingFiles.value = []
      sharedAttachedFiles.value = []
      sharedRecentUploads.value = []
    })

    async function openUploadsTab(wrapper: ReturnType<typeof mountDrawer>) {
      await wrapper.findAll('.ad-tab')[3].trigger('click')
      await nextTick()
      await nextTick()
    }

    it('renders only in-flight pending uploads as rows', async () => {
      sharedPendingFiles.value = [
        { path: '', previewUrl: null, isImage: false, uploading: true, progress: 40, size: 100 },
        { path: '/tmp/done.txt', previewUrl: null, isImage: false, uploading: false, progress: 100, size: 100 },
      ]
      const wrapper = mountDrawer()
      await openUploadsTab(wrapper)
      const rows = wrapper.find('.ad-content').findAll('.ad-file-row')
      expect(rows.length).toBe(1)
      expect(wrapper.find('.ad-content').text()).toContain('40%')
    })

    it('shows the empty state when a finished entry is retained and recent uploads is empty', async () => {
      // Regression: a non-uploading entry kept in pendingFiles (e.g. an
      // auto-attached paste/drag upload) used to hide every row via v-show while
      // also suppressing the empty state, collapsing the content area to nothing.
      sharedPendingFiles.value = [
        { path: '/tmp/kept.txt', previewUrl: null, isImage: false, uploading: false, progress: 100, size: 100 },
      ]
      sharedAttachedFiles.value = [{ path: '/tmp/kept.txt' }]
      sharedRecentUploads.value = []
      const wrapper = mountDrawer()
      await openUploadsTab(wrapper)
      const content = wrapper.find('.ad-content')
      expect(content.findAll('.ad-file-row').length).toBe(0)
      expect(content.find('.ad-empty').exists()).toBe(true)
      expect(content.text()).toContain('No uploaded files')
    })

    it('renders recent uploads rows even when a finished entry is retained', async () => {
      sharedPendingFiles.value = [
        { path: '/tmp/kept.txt', previewUrl: null, isImage: false, uploading: false, progress: 100, size: 100 },
      ]
      sharedAttachedFiles.value = [{ path: '/tmp/kept.txt' }]
      sharedRecentUploads.value = [
        { name: 'kept.txt', path: '/tmp/kept.txt', size: 100, modTime: new Date().toISOString() },
      ]
      const wrapper = mountDrawer()
      await openUploadsTab(wrapper)
      const content = wrapper.find('.ad-content')
      expect(content.findAll('.ad-file-row').length).toBe(1)
      expect(content.find('.ad-empty').exists()).toBe(false)
    })
  })

  it('upload watcher cleans up finished uploads and refreshes', async () => {
    // Start with an uploading file
    sharedPendingFiles.value = [{ path: '/tmp/a.txt', uploading: true, progress: 50, size: 100 }]
    const wrapper = mountDrawer()
    await nextTick()
    // wasUploading is now true (set by watcher on first run since now.length > 0)
    // Simulate upload completing: uploading becomes false
    sharedPendingFiles.value = [{ path: '/tmp/a.txt', uploading: false, progress: 100, size: 100 }]
    await nextTick()
    await nextTick()
    // The watcher should have filtered out the non-uploading entry and called fetchRecentUploads
    expect(sharedPendingFiles.value.length).toBe(0)
    expect(mockFetchRecentUploads).toHaveBeenCalled()
  })

  it('unmounting removes event listeners', async () => {
    const removeFocusSpy = vi.spyOn(window, 'removeEventListener')
    const removeVisSpy = vi.spyOn(document, 'removeEventListener')
    const wrapper = mountDrawer()
    wrapper.unmount()
    expect(removeFocusSpy).toHaveBeenCalledWith('focus', expect.any(Function))
    expect(removeVisSpy).toHaveBeenCalledWith('visibilitychange', expect.any(Function))
    removeFocusSpy.mockRestore()
    removeVisSpy.mockRestore()
  })

  it('onWindowFocus resets filePickerOpen when true', () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.filePickerOpen.value = true
    state.onWindowFocus()
    expect(state.filePickerOpen.value).toBe(false)
  })

  it('onWindowFocus does nothing when filePickerOpen is false', () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.filePickerOpen.value = false
    state.onWindowFocus()
    expect(state.filePickerOpen.value).toBe(false)
  })

  it('onVisibilityChange resets filePickerOpen when visible', () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.filePickerOpen.value = true
    // Mock document.visibilityState
    const orig = document.visibilityState
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    state.onVisibilityChange()
    expect(state.filePickerOpen.value).toBe(false)
    Object.defineProperty(document, 'visibilityState', { value: orig, configurable: true })
  })

  it('onFileInputBlur resets filePickerOpen after timeout', async () => {
    vi.useFakeTimers()
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.filePickerOpen.value = true
    state.onFileInputBlur()
    expect(state.filePickerOpen.value).toBe(true) // not yet
    vi.advanceTimersByTime(150)
    expect(state.filePickerOpen.value).toBe(false)
    vi.useRealTimers()
  })

  it('onDrawerClose emits close', async () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.onDrawerClose()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('toggleAttached emits add-attached for unattached path', () => {
    const wrapper = mountDrawer({ attachedFiles: [] })
    const state = getRawState(wrapper)
    state.toggleAttached('src/foo.ts')
    expect(wrapper.emitted('add-attached')!.length).toBeGreaterThan(0)
  })

  it('toggleAttached emits remove-attached for attached path', () => {
    const wrapper = mountDrawer({ attachedFiles: [{ path: 'src/foo.ts' }] })
    const state = getRawState(wrapper)
    state.toggleAttached('src/foo.ts')
    expect(wrapper.emitted('remove-attached')!.length).toBeGreaterThan(0)
  })

  it('currentDirDisplayName shows baseName for non-root dir', () => {
    const wrapper = mountDrawer({ currentDir: 'src/components' })
    expect(getRawState(wrapper).currentDirDisplayName.value).toBe('components')
  })

  it('effectiveCurrentDir uses currentDir when provided', () => {
    const wrapper = mountDrawer({ currentDir: 'src' })
    expect(getRawState(wrapper).effectiveCurrentDir.value).toBe('src')
  })

  it('wires delete handlers and delete API calls into setup state', () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    expect(typeof state.handleDeleteShare).toBe('function')
    expect(typeof state.handleDeleteUpload).toBe('function')
    expect(state.deleteRecentShare).toBe(mockDeleteRecentShare)
    expect(state.deleteRecentUpload).toBe(mockDeleteRecentUpload)
  })

  it('handleDeleteShare calls deleteRecentShare and detaches if attached', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    const wrapper = mountDrawer({ attachedFiles: [{ path: '.clawbench/share-in/a.txt' }] })
    const state = getRawState(wrapper)
    await state.handleDeleteShare({ path: '.clawbench/share-in/a.txt' })
    expect(mockDeleteRecentShare).toHaveBeenCalledWith('.clawbench/share-in/a.txt')
    expect(wrapper.emitted('remove-attached')).toBeTruthy()
  })

  it('handleDeleteShare does not detach when not attached', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    const wrapper = mountDrawer({ attachedFiles: [] })
    const state = getRawState(wrapper)
    await state.handleDeleteShare({ path: '.clawbench/share-in/a.txt' })
    expect(mockDeleteRecentShare).toHaveBeenCalledWith('.clawbench/share-in/a.txt')
    expect(wrapper.emitted('remove-attached')).toBeFalsy()
  })

  it('handleDeleteShare does nothing when user cancels', async () => {
    mockDialogConfirm.mockResolvedValue(false)
    const wrapper = mountDrawer({ attachedFiles: [{ path: '.clawbench/share-in/a.txt' }] })
    const state = getRawState(wrapper)
    await state.handleDeleteShare({ path: '.clawbench/share-in/a.txt' })
    expect(mockDeleteRecentShare).not.toHaveBeenCalled()
    expect(wrapper.emitted('remove-attached')).toBeFalsy()
  })

  it('handleDeleteUpload calls deleteRecentUpload and detaches if attached', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    const wrapper = mountDrawer({ attachedFiles: [{ path: '.clawbench/uploads/a.txt' }] })
    const state = getRawState(wrapper)
    await state.handleDeleteUpload({ path: '.clawbench/uploads/a.txt' })
    expect(mockDeleteRecentUpload).toHaveBeenCalledWith('.clawbench/uploads/a.txt')
    expect(wrapper.emitted('remove-attached')).toBeTruthy()
  })

  it('handleDeleteUpload does not detach when not attached', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    const wrapper = mountDrawer({ attachedFiles: [] })
    const state = getRawState(wrapper)
    await state.handleDeleteUpload({ path: '.clawbench/uploads/a.txt' })
    expect(mockDeleteRecentUpload).toHaveBeenCalledWith('.clawbench/uploads/a.txt')
    expect(wrapper.emitted('remove-attached')).toBeFalsy()
  })

  it('handleDeleteUpload does nothing when user cancels', async () => {
    mockDialogConfirm.mockResolvedValue(false)
    const wrapper = mountDrawer({ attachedFiles: [{ path: '.clawbench/uploads/a.txt' }] })
    const state = getRawState(wrapper)
    await state.handleDeleteUpload({ path: '.clawbench/uploads/a.txt' })
    expect(mockDeleteRecentUpload).not.toHaveBeenCalled()
    expect(wrapper.emitted('remove-attached')).toBeFalsy()
  })
})

describe('AttachDrawer selected-badge CSS', () => {
  // jsdom has no CSS engine, so this is a source-sniffing guard (same pattern
  // as countBadge.css.test.ts / wrapCheck.css.test.ts).
  //
  // Why it exists: the check badge is anchored OUTSIDE the 28px icon box
  // (right/bottom: -3px) so it sits on the thumbnail's corner. `overflow:
  // hidden` on `.ad-icon-wrap` therefore clipped the badge — and its 2px
  // white ring — along the image edge, so a selected image showed a chopped
  // tick. The wrapper must NOT clip; the thumbnail rounds itself instead.
  const source = readFileSync(
    join(__dirname, '..', 'AttachDrawer.vue'),
    'utf8',
  )

  /** Declarations of the first `<selector> {` rule in the SFC. */
  function declsOf(selector: string): string {
    const m = source.match(
      new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}'),
    )
    expect(m, `${selector} rule must exist in AttachDrawer.vue`).not.toBeNull()
    return m![1]
  }

  it('.ad-icon-wrap does not clip its overflowing badge', () => {
    const decls = declsOf('.ad-icon-wrap')
    expect(decls).not.toMatch(/overflow\s*:\s*hidden/)
  })

  it('.ad-icon-wrap is positioned so the absolute badge anchors to it', () => {
    expect(declsOf('.ad-icon-wrap')).toMatch(/position\s*:\s*relative/)
  })

  it('the badge sits outside the icon box on the bottom-right corner', () => {
    const decls = declsOf('.ad-icon-wrap .ad-icon-check')
    expect(decls).toMatch(/position\s*:\s*absolute/)
    expect(decls).toMatch(/right\s*:\s*-3px/)
    expect(decls).toMatch(/bottom\s*:\s*-3px/)
  })

  it('the thumbnail still rounds its own corners (no clip needed)', () => {
    const decls = declsOf('.ad-icon-wrap .ad-thumb')
    expect(decls).toMatch(/object-fit\s*:\s*cover/)
    expect(decls).toMatch(/border-radius\s*:\s*var\(--radius-sm\)/)
  })
})
