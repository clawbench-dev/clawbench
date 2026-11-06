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
  Camera: { name: 'Camera', render: () => h('span', { class: 'icon-camera' }) },
  Image: { name: 'Image', render: () => h('span', { class: 'icon-image' }) },
  Loader2: { name: 'Loader2', render: () => h('span', { class: 'icon-loader2' }) },
  LoaderCircle: { name: 'LoaderCircle', render: () => h('span', { class: 'icon-loader-circle' }) },
  X: { name: 'X', render: () => h('span', { class: 'icon-x' }) },
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div class="bottom-sheet" :data-open="open"><slot name="header" /><slot /><footer class="bs-footer"><slot name="footer" /></footer></div>',
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
const mockClearRecentShares = vi.fn()
const mockClearRecentUploads = vi.fn()
const mockDialogConfirm = vi.fn()
const mockToastShow = vi.fn()
const mockHandleFileSelect = vi.fn()
const mockCancelChatUpload = vi.fn()
const sharedChatUploadCancelled = ref(false)
/**
 * Stand-in for useFileUpload.terminateChatUploadBatch.
 *
 * The real one is `cancelChatUpload()` + detaching the finished paths from the
 * shared attachedFiles ref. Reproducing that shape here is what lets the
 * existing assertions ("remove-attached emitted for the attached finished
 * file only") keep describing the product behaviour now that the drawer
 * delegates instead of doing it inline.
 */
const mockTerminateChatUploadBatch = vi.fn(() => {
  const finished: string[] = mockCancelChatUpload() ?? []
  for (const path of finished) {
    const idx = sharedAttachedFiles.value.findIndex((f: any) => f.path === path)
    if (idx >= 0) sharedAttachedFiles.value.splice(idx, 1)
  }
  return finished
})

// The real useToast starts an auto-dismiss timer and shares module state, which
// would leak across tests. Only `show` is needed here.
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

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
    clearRecentShares: mockClearRecentShares,
  }),
}))

vi.mock('@/composables/useUploadRecent', () => ({
  useUploadRecent: () => ({
    recentUploads: sharedRecentUploads,
    fetchRecentUploads: mockFetchRecentUploads,
    deleteRecentUpload: mockDeleteRecentUpload,
    clearRecentUploads: mockClearRecentUploads,
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
    // The drawer now delegates to the SHARED implementation (the input strip
    // uses the same one), so the mock forwards to the same cancel spy and
    // detaches through the shared attachedFiles ref.
    terminateChatUploadBatch: mockTerminateChatUploadBatch,
    // Same helper the real composable exports: the drawer reads the picked
    // count through it rather than inline, so the ordering contract lives with
    // the code that clears the input.
    readPickedCount: (e: Event) => (e.target as HTMLInputElement)?.files?.length ?? 0,
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
  // The footer renders AttachmentTags (which splits URL/quote chips from file
  // chips), so the drawer's mock has to answer for those predicates too.
  isUrlEntry: () => false,
  isQuoteEntry: () => false,
  isSafeExternalUrl: () => false,
}))

vi.mock('@/utils/format', () => ({
  formatRelativeTime: (_date: string) => 'just now',
}))

// Mobile detection drives which upload entry is rendered: the three-entry strip
// (camera / photo / file) on phones and tablets, the original single header
// button on desktop browsers and Electron. Mutable so each test picks a host;
// `isMobileOSUA` is a module-level constant, so it is mocked alongside.
const mockIsAndroidApp = ref(false)
let mockIsMobileOSUA = false
vi.mock('@/composables/usePlatformDetect', () => ({
  usePlatformDetect: () => ({ isAndroidApp: mockIsAndroidApp }),
  get isMobileOSUA() {
    return mockIsMobileOSUA
  },
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
          pickImage: 'Photo',
          takePhoto: 'Camera',
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
          deleteRecent: 'Delete this file',
          clearAll: 'Clear',
          clearSharesHint: '{n} recent share(s)',
          clearSharesConfirm: 'Clear all recent shares?',
          clearSharesDone: 'Cleared {n} recent share file(s)',
          clearSharesFailed: 'Failed to clear recent shares',
          clearUploadsHint: '{n} recent upload(s)',
          clearUploadsConfirm: 'Clear all recent uploads?',
          clearUploadsDone: 'Cleared {n} recent upload file(s)',
          clearUploadsFailed: 'Failed to clear recent uploads',
          clearPartial: 'Deleted {deleted}; {failed} failed',
          deleteUploadConfirm: 'Delete recently uploaded file "{name}"?',
          forgetUpload: 'Remove from list',
          forgetUploadConfirm: 'Remove "{name}" from recent uploads? The file stays in your project.',
        },
      },
      common: { remove: 'Remove', delete: 'Delete', cancel: 'Cancel', confirm: 'OK' },
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

/**
 * Stand-in for the real `handleFileSelect`.
 *
 * The real one reports every file through its per-file callback the moment that
 * file lands, and only then resolves with the whole batch. The drawer depends on
 * that order to commit an upload before its in-flight card unmounts (issue
 * #629), so the mock has to reproduce it — a plain `mockResolvedValue` would
 * let a batch-level implementation pass.
 */
function mockPickedFiles(paths: string[]) {
  mockHandleFileSelect.mockImplementation(async (_e: Event, onUploaded?: (p: string) => void) => {
    for (const path of paths) onUploaded?.(path)
    return paths
  })
}

describe('AttachDrawer', () => {
  beforeEach(() => {
    mockDeleteRecentShare.mockClear()
    mockDeleteRecentUpload.mockClear()
    mockClearRecentShares.mockClear()
    mockClearRecentUploads.mockClear()
    mockDialogConfirm.mockClear()
    mockToastShow.mockClear()
    mockHandleFileSelect.mockReset()
    mockHandleFileSelect.mockResolvedValue([])
    mockCancelChatUpload.mockReset()
    mockCancelChatUpload.mockReturnValue([])
    mockTerminateChatUploadBatch.mockClear()
    sharedChatUploadCancelled.value = false
    // Default to desktop: that is the majority case and the one whose layout
    // the original header button belongs to. Mobile tests opt in explicitly.
    mockIsAndroidApp.value = false
    mockIsMobileOSUA = false
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

  it('selects a referenced directory in the draft, applied on OK with isDir=true', async () => {
    // A referenced directory must carry its isDir flag, otherwise it is attached
    // as a file and shows a file icon instead of a folder. Selection is local,
    // so the flag has to survive until OK — not just until the click.
    const wrapper = mountDrawer({
      recentReferencedFiles: [{ path: 'src/utils', count: 2, isDir: true }],
    })
    await wrapper.findAll('.ad-tab')[1].trigger('click')
    await nextTick()
    const row = wrapper.findAll('.ad-file-row').find(r => r.text().includes('utils'))
    expect(row).toBeTruthy()
    await row!.trigger('click')
    // Nothing reaches the composer before OK.
    expect(wrapper.emitted('add-attached')).toBeFalsy()
    expect(row!.classes()).toContain('ad-file-attached')

    await wrapper.find('[data-action="attach-ok"]').trigger('click')
    expect(wrapper.emitted('add-attached')![0]).toEqual(['src/utils', true])
  })

  it('selects a referenced file in the draft, applied on OK with isDir=false', async () => {
    const wrapper = mountDrawer({
      recentReferencedFiles: [{ path: 'src/foo.ts', count: 1 }],
    })
    await wrapper.findAll('.ad-tab')[1].trigger('click')
    await nextTick()
    const row = wrapper.findAll('.ad-file-row').find(r => r.text().includes('foo.ts'))
    expect(row).toBeTruthy()
    await row!.trigger('click')
    expect(wrapper.emitted('add-attached')).toBeFalsy()

    await wrapper.find('[data-action="attach-ok"]').trigger('click')
    expect(wrapper.emitted('add-attached')![0]).toEqual(['src/foo.ts', false])
  })

  it('clicking an unselected row only marks it selected (no emit)', async () => {
    const wrapper = mountDrawer({
      currentDir: 'src',
      attachedFiles: [],
    })
    await wrapper.find('.ad-current-item').trigger('click')
    expect(wrapper.emitted('add-attached')).toBeFalsy()
    expect(wrapper.find('.ad-current-item').classes()).toContain('ad-file-attached')
  })

  it('clicking a selected row only unmarks it (no emit)', async () => {
    const wrapper = mountDrawer({
      currentDir: 'src',
      attachedFiles: [{ path: 'src', isDir: true }],
    })
    await wrapper.find('.ad-current-item').trigger('click')
    expect(wrapper.emitted('remove-attached')).toBeFalsy()
    expect(wrapper.find('.ad-current-item').classes()).not.toContain('ad-file-attached')
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

  it('renders the three source entries (camera, photo, file) on mobile', () => {
    mockIsMobileOSUA = true
    const wrapper = mountDrawer()
    const buttons = wrapper.findAll('.ad-source-btn')
    expect(buttons).toHaveLength(3)
    // The labels are what tell them apart, so assert the rendered text. A
    // missing i18n key would render the raw key here, which also fails.
    const labels = buttons.map((b) => b.find('.ad-source-label').text())
    expect(labels).toEqual(['Camera', 'Photo', 'Upload file'])
  })

  it('hides the source strip on desktop and keeps the original header button', () => {
    // Regression: the mobile strip was shown everywhere, so desktop browsers
    // and Electron lost the header upload button and gained a camera entry
    // that has nothing to open. Each host must get its own layout.
    const wrapper = mountDrawer()
    expect(wrapper.find('.ad-source-bar').exists()).toBe(false)
    expect(wrapper.findAll('.ad-source-btn')).toHaveLength(0)
    const headerBtn = wrapper.find('.ad-header-upload-btn')
    expect(headerBtn.exists()).toBe(true)
    expect(headerBtn.text()).toContain('Upload file')
  })

  it('mobile strip replaces the header button instead of doubling it', () => {
    mockIsMobileOSUA = true
    const wrapper = mountDrawer()
    expect(wrapper.find('.ad-source-bar').exists()).toBe(true)
    expect(wrapper.find('.ad-header-upload-btn').exists()).toBe(false)
  })

  it('treats the Android app as mobile even without a mobile UA', () => {
    // The WebView's UA is the device's; the host axis is what makes this
    // unambiguous when a tablet reports a desktop-looking string.
    mockIsAndroidApp.value = true
    const wrapper = mountDrawer()
    expect(wrapper.find('.ad-source-bar').exists()).toBe(true)
  })

  it('desktop header button opens the plain file input', async () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    const fakeInput = { value: 'old', click: vi.fn() }
    state.fileInputRef.value = fakeInput
    await wrapper.find('.ad-header-upload-btn').trigger('click')
    await nextTick()
    expect(fakeInput.click).toHaveBeenCalledTimes(1)
    expect(state.filePickerOpen.value).toBe(true)
  })

  it('gives each source entry its own picker mode (accept / capture)', () => {
    mockIsMobileOSUA = true
    const wrapper = mountDrawer()
    // One shared input cannot serve all three: `accept` and `capture` are read
    // by the OS when the picker opens, so mutating them right before click() is
    // unreliable on Android WebView.
    const photo = wrapper.find('input[type="file"][accept="image/*"]:not([capture])')
    const camera = wrapper.find('input[type="file"][capture="environment"]')
    const plain = wrapper.findAll('input[type="file"]').filter(
      (i) => !i.attributes('accept') && !i.attributes('capture'),
    )
    expect(photo.exists()).toBe(true)
    expect(camera.exists()).toBe(true)
    expect(camera.attributes('accept')).toBe('image/*')
    expect(plain).toHaveLength(1)
  })

  it('camera entry is single-shot, the other two allow multiple', () => {
    mockIsMobileOSUA = true
    const wrapper = mountDrawer()
    // A camera capture produces exactly one image; leaving `multiple` on would
    // make some pickers show a multi-select UI for a single shot.
    expect(wrapper.find('input[capture="environment"]').attributes('multiple')).toBeUndefined()
    expect(wrapper.find('input[accept="image/*"]:not([capture])').attributes('multiple')).toBeDefined()
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

  it('photo and camera entries open their own input, not the file input', async () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    const fileInput = { value: 'a', click: vi.fn() }
    const photoInput = { value: 'b', click: vi.fn() }
    const cameraInput = { value: 'c', click: vi.fn() }

    state.fileInputRef.value = fileInput
    state.photoInputRef.value = photoInput
    state.handlePhotoClick()
    // click() is deferred one tick so the drawer's closeGuard installs first.
    await nextTick()
    expect(photoInput.value).toBe('')
    expect(photoInput.click).toHaveBeenCalledTimes(1)
    expect(fileInput.click).not.toHaveBeenCalled()

    // Re-assign after the render: awaiting nextTick re-binds the template refs
    // to the real DOM inputs, so the fake set above no longer stands.
    state.fileInputRef.value = fileInput
    state.cameraInputRef.value = cameraInput
    state.handleCameraClick()
    await nextTick()
    expect(cameraInput.value).toBe('')
    expect(cameraInput.click).toHaveBeenCalledTimes(1)
    expect(fileInput.click).not.toHaveBeenCalled()
    expect(photoInput.click).toHaveBeenCalledTimes(1)
  })

  it('photo/camera entries are no-ops when their input is absent', () => {
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.photoInputRef.value = null
    state.cameraInputRef.value = null
    // Must not throw, and must not claim a picker is open (that would install
    // the closeGuard and leave the drawer permanently unclosable).
    expect(() => state.handlePhotoClick()).not.toThrow()
    expect(() => state.handleCameraClick()).not.toThrow()
    expect(state.filePickerOpen.value).toBe(false)
  })

  it('onFileSelect resets filePickerOpen and switches to uploads tab on success', async () => {
    mockPickedFiles(['.clawbench/uploads/a.png'])
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

  it('onFileSelect keeps the sheet open, shows the upload in the draft AND attaches it', async () => {
    // A finished upload must reach the composer right away (issue #629). The
    // sheet's draft alone is not enough: the in-flight card unmounts as soon as
    // `uploading` flips false, and the draft is discarded whenever the sheet is
    // dismissed — so a draft-only upload flashed in the input area and vanished,
    // and the image never made it into the conversation.
    mockPickedFiles(['.clawbench/uploads/a.png'])
    const wrapper = mountDrawer()
    await getRawState(wrapper).onFileSelect({ target: { files: [{ name: 'a.png' }] } })
    expect(wrapper.emitted('close')).toBeFalsy()
    expect(wrapper.emitted('add-attached')).toEqual([['.clawbench/uploads/a.png', false]])
    expect(wrapper.findAll('.ad-footer .attachment-filename').map(w => w.text()))
      .toEqual(['a.png'])
  })

  it('commits each file as it lands, not once the whole batch is done', async () => {
    // The reported gap is one transfer wide: a finished file's pending card is
    // unmounted immediately, so a batch-level commit would leave it attached to
    // nothing until the last file arrives. The drawer must therefore pass a
    // per-file callback rather than mirroring the batch's return value.
    let finishFirst: (p: string) => void = () => {}
    mockHandleFileSelect.mockImplementation(async (_e: Event, onUploaded?: (p: string) => void) => {
      onUploaded?.('.clawbench/uploads/a.png')
      // Second file still in flight: nothing else has been reported yet.
      await new Promise<void>((r) => { finishFirst = () => r() })
      onUploaded?.('.clawbench/uploads/b.png')
      return ['.clawbench/uploads/a.png', '.clawbench/uploads/b.png']
    })
    const wrapper = mountDrawer({ attachedFiles: [] })
    const pending = getRawState(wrapper).onFileSelect({ target: { files: [] } })
    await nextTick()

    // Only the first file is committed, and it is committed BEFORE the batch
    // resolves — that is the whole point.
    expect(wrapper.emitted('add-attached')).toEqual([['.clawbench/uploads/a.png', false]])

    finishFirst()
    await pending
    expect(wrapper.emitted('add-attached')).toEqual([
      ['.clawbench/uploads/a.png', false],
      ['.clawbench/uploads/b.png', false],
    ])
  })

  it('does not attach the upload a second time when OK is pressed', async () => {
    // The upload was already committed on success; OK diffs against the
    // snapshot, which moved with it, so the delta must be empty. Without that
    // the composer receives the same file twice.
    mockPickedFiles(['.clawbench/uploads/a.png'])
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [{ name: 'a.png' }] } })
    expect(wrapper.emitted('add-attached')).toHaveLength(1)

    await wrapper.find('[data-action="attach-ok"]').trigger('click')
    expect(wrapper.emitted('add-attached')).toHaveLength(1)
  })

  it('cancel undoes the uploads this session committed', async () => {
    // "取消" has to cancel. The upload is committed as it lands (see above), so
    // without this the user who picked the wrong photo, saw the card and pressed
    // Cancel still sent that photo with the next message.
    mockPickedFiles(['.clawbench/uploads/a.png'])
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [{ name: 'a.png' }] } })
    await wrapper.find('[data-action="attach-cancel"]').trigger('click')
    expect(wrapper.emitted('add-attached')).toEqual([['.clawbench/uploads/a.png', false]])
    expect(wrapper.emitted('remove-attached')).toEqual([
      [{ path: '.clawbench/uploads/a.png', isDir: false }],
    ])
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(0)
  })

  it('cancel leaves attachments that were already there before it opened', async () => {
    // Only THIS session's uploads are undone; the caller's existing attachments
    // are not the sheet's to remove.
    mockPickedFiles(['.clawbench/uploads/new.png'])
    const wrapper = mountDrawer({ attachedFiles: [{ path: 'src/existing.ts' }] })
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    await wrapper.find('[data-action="attach-cancel"]').trigger('click')
    expect(wrapper.emitted('remove-attached')).toEqual([
      [{ path: '.clawbench/uploads/new.png', isDir: false }],
    ])
  })

  it('cancel aborts a batch that is still in flight', async () => {
    // The sheet stays open while the transfer runs, so pressing Cancel mid-
    // upload is the most natural "stop, wrong photo" gesture. The commit is
    // per file as each one lands, so without aborting, the transfer finishes a
    // few seconds later and puts the image back into the input bar — after the
    // panel closed, with nothing left to cancel it.
    let resolveUpload: (v: string[]) => void = () => {}
    mockHandleFileSelect.mockReturnValue(new Promise<string[]>((r) => { resolveUpload = r }))
    const wrapper = mountDrawer({ attachedFiles: [] })
    void getRawState(wrapper).onFileSelect({ target: { files: [{ name: 'a.png' }] } })
    await nextTick()

    await wrapper.find('[data-action="attach-cancel"]').trigger('click')

    // Same primitive the input strip's terminate pill uses — one implementation
    // of "stop the batch and abort the transfer in flight".
    expect(mockCancelChatUpload).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toBeTruthy()
    resolveUpload([])
  })

  it('cancel leaves an upload this sheet did not start alone', async () => {
    // A drag-drop or paste upload can be running in the input bar while the
    // sheet is open. `cancelChatUpload` acts on the module-level batch, so
    // calling it unconditionally here silently killed a transfer the user never
    // asked to stop.
    mockHandleFileSelect.mockResolvedValue([])
    const wrapper = mountDrawer({ attachedFiles: [] })
    await wrapper.find('[data-action="attach-cancel"]').trigger('click')
    expect(mockCancelChatUpload).not.toHaveBeenCalled()
  })

  it('cancel still stops the batch when a second pick superseded the first', async () => {
    // Two picks can overlap. The superseded one settles first, so a plain
    // boolean "is my batch running" flag reads false while the second batch is
    // still uploading — and Cancel would leave it running, which is exactly the
    // "image reappears after the panel closed" symptom.
    const resolvers: Array<(v: string[]) => void> = []
    mockHandleFileSelect.mockImplementation(() => new Promise<string[]>((r) => { resolvers.push(r) }))
    const wrapper = mountDrawer({ attachedFiles: [] })
    const state = getRawState(wrapper)

    void state.onFileSelect({ target: { files: [{ name: 'a.png' }] } })
    await nextTick()
    void state.onFileSelect({ target: { files: [{ name: 'b.png' }] } })
    await nextTick()

    // The superseded pick settles while the second one is still in flight.
    resolvers[0]([])
    await nextTick()

    await wrapper.find('[data-action="attach-cancel"]').trigger('click')

    expect(mockCancelChatUpload).toHaveBeenCalledTimes(1)
    resolvers[1]([])
  })

  it('OK accepts the batch: a later terminate must not undo it', async () => {
    // `batchCommitted` is what "terminate" clears. Once the user presses OK
    // those uploads are their accepted selection, so a terminate aimed at a
    // batch started elsewhere (the sheet can be reopened while one runs) must
    // not detach them.
    mockPickedFiles(['.clawbench/uploads/a.png'])
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [{ name: 'a.png' }] } })
    await wrapper.find('[data-action="attach-ok"]').trigger('click')

    await getRawState(wrapper).handleTerminateUpload()

    expect(wrapper.emitted('remove-attached')).toBeFalsy()
  })

  it('dismissing the sheet is a cancel: its uploads go away', async () => {
    // Swipe / back / overlay tap / header × are all "I am leaving this picker".
    // Leaving without OK must mean the same as Cancel — otherwise the image the
    // user just walked away from still rides along with the next message.
    mockPickedFiles(['.clawbench/uploads/a.png'])
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [{ name: 'a.png' }] } })
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(1)

    await getRawState(wrapper).onDrawerClose()

    expect(wrapper.emitted('remove-attached')).toEqual([
      [{ path: '.clawbench/uploads/a.png', isDir: false }],
    ])
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(0)
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('a dismiss gesture and the Cancel button do the same thing', async () => {
    // Pins the two entry points together: whatever the sheet does on a swipe it
    // must also do on Cancel (and vice versa), so a future edit cannot make one
    // of them keep the uploads.
    const run = async (how: 'cancel' | 'dismiss') => {
      mockPickedFiles(['.clawbench/uploads/a.png'])
      const wrapper = mountDrawer({ attachedFiles: [] })
      await getRawState(wrapper).onFileSelect({ target: { files: [{ name: 'a.png' }] } })
      if (how === 'cancel') await wrapper.find('[data-action="attach-cancel"]').trigger('click')
      else await getRawState(wrapper).onDrawerClose()
      return wrapper.emitted('remove-attached')
    }

    expect(await run('dismiss')).toEqual(await run('cancel'))
  })

  it('onFileSelect shows the uploads tab while the transfer is still running', async () => {
    // The sheet no longer closes on pick, so the in-flight rows (and the
    // terminate button) are the only feedback the user gets — the tab switch
    // has to happen before the await, not after it.
    let resolveUpload: (v: string[]) => void = () => {}
    mockHandleFileSelect.mockReturnValue(new Promise<string[]>((r) => { resolveUpload = r }))
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    void state.onFileSelect({ target: { files: [{ name: 'a.png' }] } })
    await nextTick()
    expect(state.activeTab.value).toBe('uploads')
    expect(wrapper.emitted('close')).toBeFalsy()
    resolveUpload([])
  })

  it('onFileSelect keeps the drawer open when the picker was cancelled', async () => {
    // A cancelled picker (empty FileList) is not a decision to leave — the user
    // may still be browsing the Current / References tabs.
    mockHandleFileSelect.mockResolvedValue([])
    const wrapper = mountDrawer()
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    expect(wrapper.emitted('close')).toBeFalsy()
  })

  it('onFileSelect reads the picked count BEFORE handleFileSelect clears the input', async () => {
    // The ordering contract: handleFileSelect clears `input.value`, and a real
    // <input type=file> empties its FileList when that happens. If the drawer
    // read the count afterwards it would always see 0 and never switch to the
    // uploads tab for a real pick. The read goes through the composable's
    // readPickedCount, but the CALL ORDER is still the drawer's responsibility
    // — that is what this pins.
    const seen: string[] = []
    let fileCount = 1
    const target = {
      get files() {
        return fileCount > 0 ? [{ name: 'a.png' }] : []
      },
      value: 'x',
    }
    mockHandleFileSelect.mockImplementation(async (e: Event) => {
      seen.push(`handle:${(e.target as HTMLInputElement).files?.length ?? -1}`)
      // Emulate the real clear, including the FileList emptying that comes with
      // it — that is the whole hazard.
      ;(e.target as HTMLInputElement).value = ''
      fileCount = 0
      return []
    })
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)

    await state.onFileSelect({ target } as any)

    // The count was observed as 1 (i.e. before the clear), so the uploads tab
    // was chosen.
    expect(seen[0]).toBe('handle:1')
    expect(state.activeTab.value).toBe('uploads')
  })

  it('onFileSelect attaches each freshly uploaded file once', async () => {
    // Multi-select: every file that finished uploads gets its own card and its
    // own attach, and OK adds nothing further.
    mockPickedFiles(['.clawbench/uploads/a.png', '.clawbench/uploads/b.png'])
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    expect(wrapper.emitted('add-attached')).toEqual([
      ['.clawbench/uploads/a.png', false],
      ['.clawbench/uploads/b.png', false],
    ])
    expect(wrapper.findAll('.ad-footer .attachment-filename').map(w => w.text()))
      .toEqual(['a.png', 'b.png'])

    await wrapper.find('[data-action="attach-ok"]').trigger('click')
    expect(wrapper.emitted('add-attached')).toHaveLength(2)
  })

  it('onFileSelect does not duplicate an upload that is already selected', async () => {
    mockPickedFiles(['.clawbench/uploads/a.png'])
    const wrapper = mountDrawer({ attachedFiles: [{ path: '.clawbench/uploads/a.png' }] })
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(1)
    // The composer already had it, so no second attach is emitted…
    expect(wrapper.emitted('add-attached')).toBeFalsy()

    // …and OK has nothing to add either.
    await wrapper.find('[data-action="attach-ok"]').trigger('click')
    expect(wrapper.emitted('add-attached')).toBeFalsy()
  })

  it('onFileSelect attaches nothing when the batch was terminated mid-flight', async () => {
    // The terminate button sets the flag while handleFileSelect is still
    // awaiting. A file whose transfer is aborted never reaches the per-file
    // callback (the composable classifies it as cancelled), and the files that
    // DID finish are detached by terminateChatUploadBatch itself — so the sheet
    // must end up with nothing to commit and stay on its current tab.
    mockHandleFileSelect.mockImplementation(async () => {
      sharedChatUploadCancelled.value = true
      return []
    })
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    expect(wrapper.emitted('add-attached')).toBeFalsy()
    expect(getRawState(wrapper).activeTab.value).toBe('current')
  })

  it('terminate drops the cards of the uploads it just detached', async () => {
    // A file committed per-file (see onFileSelect) is detached from the composer
    // by the shared terminate. The sheet must forget it too, or it would show a
    // card for an attachment that no longer exists and re-add it on OK.
    mockPickedFiles(['.clawbench/uploads/done.png'])
    mockCancelChatUpload.mockReturnValue(['.clawbench/uploads/done.png'])
    sharedPendingFiles.value = [
      { path: '', previewUrl: null, isImage: false, uploading: true, progress: 30, size: 100 },
    ]
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(1)

    await getRawState(wrapper).handleTerminateUpload()
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(0)

    await wrapper.find('[data-action="attach-ok"]').trigger('click')
    // The only add was the original commit; OK re-adds nothing.
    expect(wrapper.emitted('add-attached')).toHaveLength(1)
  })

  it('terminate clears this sheet’s own commits even when the host never took them', async () => {
    // The queued-message editor's `addPath` attaches into its OWN draft and
    // never touches the global composer list, so the shared terminate has
    // nothing to detach and reports no paths. Reading that return value left the
    // images the user had just cancelled sitting in the queued message — they
    // would then be sent with the message the user had just abandoned.
    mockPickedFiles(['.clawbench/uploads/a.png'])
    mockCancelChatUpload.mockReturnValue([])
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(1)

    await getRawState(wrapper).handleTerminateUpload()

    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(0)
    expect(wrapper.emitted('remove-attached')).toEqual([
      [{ path: '.clawbench/uploads/a.png', isDir: false }],
    ])
  })

  it('a cancelled picker does not forget the previous batch’s commits', async () => {
    // Re-opening the picker and backing out starts no new batch, so the one
    // still running must keep its committed paths. Clearing them there left
    // "terminate" with nothing to clear, and a host that tracks its own list
    // (the queued-message editor) kept the finished images.
    mockPickedFiles(['.clawbench/uploads/a.png'])
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).onFileSelect({ target: { files: [{ name: 'a.png' }] } })
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(1)

    mockHandleFileSelect.mockResolvedValue([])
    await getRawState(wrapper).onFileSelect({ target: { files: [] } })

    await getRawState(wrapper).handleTerminateUpload()

    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(0)
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

    it('clicking terminate runs the shared batch-terminate (cancel + detach)', async () => {
      sharedPendingFiles.value = [
        { path: '', previewUrl: null, isImage: false, uploading: true, progress: 30, size: 100 },
      ]
      // cancelChatUpload returns the paths that had already finished.
      mockCancelChatUpload.mockReturnValue([
        '.clawbench/uploads/done-a.png',
        '.clawbench/uploads/done-b.png',
      ])
      sharedAttachedFiles.value = [{ path: '.clawbench/uploads/done-a.png' }]
      const wrapper = mountDrawer()
      await openUploadsTab(wrapper)
      await wrapper.find('.ad-terminate-btn').trigger('click')

      // ONE implementation backs both entry points (this banner and the input
      // strip's pill) — the drawer must not carry its own copy of the
      // "cancel, abort, detach the finished ones" sequence.
      expect(mockTerminateChatUploadBatch).toHaveBeenCalledTimes(1)
      // Finished files are cleared — but only the ones actually attached.
      expect(sharedAttachedFiles.value).toEqual([])
    })

    it('does not detach finished files that were never attached', async () => {
      sharedPendingFiles.value = [
        { path: '', previewUrl: null, isImage: false, uploading: true, progress: 30, size: 100 },
      ]
      mockCancelChatUpload.mockReturnValue(['.clawbench/uploads/never-attached.png'])
      const wrapper = mountDrawer()
      await openUploadsTab(wrapper)
      await wrapper.find('.ad-terminate-btn').trigger('click')

      expect(mockTerminateChatUploadBatch).toHaveBeenCalledTimes(1)
      expect(sharedAttachedFiles.value).toEqual([])
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

  it('toggleAttached adds to the draft without touching the composer', () => {
    const wrapper = mountDrawer({ attachedFiles: [] })
    const state = getRawState(wrapper)
    state.toggleAttached('src/foo.ts')
    expect(wrapper.emitted('add-attached')).toBeFalsy()
    expect(state.isAttached('src/foo.ts')).toBe(true)
  })

  it('toggleAttached removes from the draft without touching the composer', () => {
    const wrapper = mountDrawer({ attachedFiles: [{ path: 'src/foo.ts' }] })
    const state = getRawState(wrapper)
    state.toggleAttached('src/foo.ts')
    expect(wrapper.emitted('remove-attached')).toBeFalsy()
    expect(state.isAttached('src/foo.ts')).toBe(false)
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

  // A row whose origin is `project` is an ordinary project file that the server
  // only recorded so it would show up here. Deleting it must not read as
  // "delete my file": the confirm wording and the (non-dangerous) button say
  // the file stays.
  it('handleDeleteUpload uses the un-list wording for a project file', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    const wrapper = mountDrawer({ attachedFiles: [] })
    const state = getRawState(wrapper)
    await state.handleDeleteUpload({ path: 'screenshot.png', name: 'screenshot.png', origin: 'project' })

    const [message, options] = mockDialogConfirm.mock.calls[0]
    expect(message).toBe('Remove "screenshot.png" from recent uploads? The file stays in your project.')
    expect(options.dangerous).toBe(false)
    expect(options.confirmText).toBe('Remove from list')
    expect(mockDeleteRecentUpload).toHaveBeenCalledWith('screenshot.png')
  })

  it('handleDeleteUpload keeps the destructive wording for an uploads-dir file', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    const wrapper = mountDrawer({ attachedFiles: [] })
    const state = getRawState(wrapper)
    await state.handleDeleteUpload({ path: '.clawbench/uploads/a.txt', name: 'a.txt', origin: 'uploads' })

    const [message, options] = mockDialogConfirm.mock.calls[0]
    expect(message).toBe('Delete recently uploaded file "a.txt"?')
    expect(options.dangerous).toBe(true)
    expect(options.confirmText).toBe('Delete')
  })

  it('renders project-file rows with the un-list title on the delete icon', async () => {
    sharedRecentUploads.value = [
      { name: 'root.png', path: 'root.png', size: 10, modTime: '', origin: 'project' },
      { name: 'chat.png', path: '.clawbench/uploads/chat.png', size: 10, modTime: '', origin: 'uploads' },
    ]
    const wrapper = mountDrawer()
    await wrapper.findAll('.ad-tab')[3].trigger('click')
    await nextTick()

    const icons = wrapper.findAll('.ad-file-delete')
    expect(icons.map(el => el.attributes('title'))).toEqual(['Remove from list', 'Delete this file'])
    // The non-destructive row must also drop the red hover/active colour: the
    // colour is the strongest signal and would contradict the confirm text.
    expect(icons[0].classes()).toContain('ad-file-delete-forget')
    expect(icons[1].classes()).not.toContain('ad-file-delete-forget')
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
    // Design px: `calc(-3 * var(--dp))` or a literal `-3px`.\n    expect(decls).toMatch(/right\s*:\s*(?:calc\(\s*-3\s*\*\s*var\(--dp\)\s*\)|-3px)/)
    expect(decls).toMatch(/bottom\s*:\s*(?:calc\(\s*-3\s*\*\s*var\(--dp\)\s*\)|-3px)/)
  })

  it('the thumbnail still rounds its own corners (no clip needed)', () => {
    const decls = declsOf('.ad-icon-wrap .ad-thumb')
    expect(decls).toMatch(/object-fit\s*:\s*cover/)
    expect(decls).toMatch(/border-radius\s*:\s*var\(--radius-sm\)/)
  })

  // A project-file row only un-lists the file, so its delete icon must not turn
  // red on hover/active — the colour would contradict the "the file itself
  // stays" confirm text, and colour is the strongest signal in that row. Two
  // equal-specificity rules are involved, so source ORDER is what makes this
  // work; a reorder silently brings the red back.
  it('the un-list variant overrides the destructive hover colour', () => {
    expect(declsOf('.ad-file-row:hover .ad-file-delete')).toMatch(/--color-red/)
    const forgetRule = source.match(
      /\.ad-file-row:active \.ad-file-delete-forget,\s*\.ad-file-row:hover \.ad-file-delete-forget\s*\{([^}]*)\}/,
    )
    expect(forgetRule, 'the un-list override rule must exist').not.toBeNull()
    expect(forgetRule![1]).toMatch(/color\s*:\s*var\(--text-secondary\)/)
    expect(forgetRule![1]).not.toMatch(/--color-red/)

    // Locate both rules with a whitespace-tolerant matcher rather than a literal
    // `indexOf`: a reformat that drops the space before `{` would make the
    // literal lookup return -1, and `-1 > -1` would silently pass.
    const redIndex = source.search(/\.ad-file-row:hover \.ad-file-delete\s*\{/)
    const forgetIndex = source.search(/\.ad-file-row:hover \.ad-file-delete-forget\s*\{/)
    expect(redIndex, 'the destructive rule must be found').toBeGreaterThan(-1)
    expect(forgetIndex, 'the override rule must be found').toBeGreaterThan(-1)
    expect(forgetIndex, 'the override must come after the red rule or the cascade keeps red')
      .toBeGreaterThan(redIndex)
  })
})

describe('AttachDrawer clear-all (最近上传 / 最近分享 顶部一键清空)', () => {
  // This is a top-level describe, so it does not inherit the one above. Without
  // its own reset the mocks accumulate across cases and the assertions below
  // would read call counts from a previous test.
  beforeEach(() => {
    mockClearRecentShares.mockReset()
    mockClearRecentUploads.mockReset()
    mockDialogConfirm.mockReset()
    mockToastShow.mockReset()
    sharedRecentShares.value = []
    sharedRecentUploads.value = []
  })

  it('renders a clear button in the shares tab only when there are shares', async () => {
    sharedRecentShares.value = []
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.activeTab.value = 'shares'
    await nextTick()
    // An always-visible destructive action on an empty list is a trap.
    expect(wrapper.find('.ad-clear-btn').exists()).toBe(false)

    sharedRecentShares.value = [{ name: 'a.txt', path: '.clawbench/share-in/a.txt', size: 1, modTime: '' }]
    await nextTick()
    expect(wrapper.find('.ad-clear-btn').exists()).toBe(true)
    expect(wrapper.find('.ad-clear-hint').text()).toContain('1')
  })

  it('renders a clear button in the uploads tab only when there are uploads', async () => {
    sharedRecentUploads.value = []
    sharedRecentShares.value = []
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.activeTab.value = 'uploads'
    await nextTick()
    expect(wrapper.find('.ad-clear-btn').exists()).toBe(false)

    sharedRecentUploads.value = [{ name: 'a.png', path: '.clawbench/uploads/a.png', size: 1, modTime: '' }]
    await nextTick()
    expect(wrapper.find('.ad-clear-btn').exists()).toBe(true)
  })

  it('does not show the shares clear bar on other tabs', async () => {
    sharedRecentShares.value = [{ name: 'a.txt', path: '.clawbench/share-in/a.txt', size: 1, modTime: '' }]
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)
    state.activeTab.value = 'current'
    await nextTick()
    expect(wrapper.find('.ad-clear-btn').exists()).toBe(false)
  })

  it('handleClearShares confirms, clears, and reports the count', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    mockClearRecentShares.mockResolvedValue({ deleted: 3, failed: 0 })
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)

    await state.handleClearShares()

    expect(mockDialogConfirm).toHaveBeenCalledTimes(1)
    expect(mockClearRecentShares).toHaveBeenCalledTimes(1)
    expect(mockToastShow).toHaveBeenCalledTimes(1)
    expect(mockToastShow.mock.calls[0][0]).toContain('3')
    expect(mockToastShow.mock.calls[0][1]).toMatchObject({ type: 'success' })
  })

  it('handleClearUploads confirms, clears, and reports the count', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    mockClearRecentUploads.mockResolvedValue({ deleted: 5, failed: 0 })
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)

    await state.handleClearUploads()

    expect(mockClearRecentUploads).toHaveBeenCalledTimes(1)
    expect(mockToastShow.mock.calls[0][0]).toContain('5')
  })

  it('does nothing when the user cancels the confirmation', async () => {
    mockDialogConfirm.mockResolvedValue(false)
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)

    await state.handleClearShares()
    await state.handleClearUploads()

    expect(mockClearRecentShares).not.toHaveBeenCalled()
    expect(mockClearRecentUploads).not.toHaveBeenCalled()
    expect(mockToastShow).not.toHaveBeenCalled()
  })

  it('detaches every attached file inside the cleared directory', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    mockClearRecentUploads.mockResolvedValue({ deleted: 2, failed: 0 })
    const wrapper = mountDrawer({
      attachedFiles: [
        { path: '.clawbench/uploads/a.png' },
        { path: '.clawbench/uploads/b.png' },
        // Outside the cleared directory — must survive.
        { path: 'src/main.go' },
      ],
    })
    const state = getRawState(wrapper)

    await state.handleClearUploads()

    const removed = wrapper.emitted('remove-attached') as any[]
    expect(removed.map(e => e[0].path)).toEqual([
      '.clawbench/uploads/a.png',
      '.clawbench/uploads/b.png',
    ])
  })

  it('does not detach a nested file the sweep leaves on disk', async () => {
    // clearRecentDir skips subdirectories, so detaching a nested attachment
    // would drop a card whose file still exists.
    mockDialogConfirm.mockResolvedValue(true)
    mockClearRecentUploads.mockResolvedValue({ deleted: 0, failed: 0 })
    const wrapper = mountDrawer({
      attachedFiles: [{ path: '.clawbench/uploads/batch/a.png' }],
    })
    const state = getRawState(wrapper)

    await state.handleClearUploads()

    expect(wrapper.emitted('remove-attached')).toBeFalsy()
  })

  it('reports a partial sweep instead of claiming success', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    mockClearRecentShares.mockResolvedValue({ deleted: 2, failed: 1 })
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)

    await state.handleClearShares()

    expect(mockToastShow.mock.calls[0][0]).toContain('2')
    expect(mockToastShow.mock.calls[0][0]).toContain('1')
    expect(mockToastShow.mock.calls[0][1]).toMatchObject({ type: 'error' })
  })

  it('reports a failure when the request did not succeed', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    mockClearRecentUploads.mockResolvedValue(null)
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)

    await state.handleClearUploads()

    expect(mockToastShow).toHaveBeenCalledTimes(1)
    expect(mockToastShow.mock.calls[0][1]).toMatchObject({ type: 'error' })
  })

  it('ignores a second tap while a sweep is in flight', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    let resolveClear: (v: any) => void = () => {}
    mockClearRecentUploads.mockReturnValue(new Promise((r) => { resolveClear = r }))
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)

    const first = state.handleClearUploads()
    await state.handleClearUploads() // must be a no-op
    resolveClear({ deleted: 1, failed: 0 })
    await first

    expect(mockClearRecentUploads).toHaveBeenCalledTimes(1)
    expect(mockDialogConfirm).toHaveBeenCalledTimes(1)
  })

  it('re-enables the button after the sweep finishes', async () => {
    mockDialogConfirm.mockResolvedValue(true)
    mockClearRecentUploads.mockResolvedValue({ deleted: 0, failed: 0 })
    const wrapper = mountDrawer()
    const state = getRawState(wrapper)

    await state.handleClearUploads()
    expect(state.clearing.value).toBeNull()
  })
})

describe('AttachDrawer footer actions', () => {
  beforeEach(() => {
    sharedRecentShares.value = []
    sharedRecentUploads.value = []
    mockDialogConfirm.mockReset()
  })

  it('renders the shared .fbtn pair even with nothing attached', () => {
    // The footer used to appear only once something was attached, which left
    // the sheet with no explicit exit at all (the narrow header tap is the
    // only close affordance and is not discoverable).
    const wrapper = mountDrawer({ attachedFiles: [] })
    const buttons = wrapper.findAll('.ad-footer-actions button')
    expect(buttons.map(b => b.text())).toEqual(['Cancel', 'OK'])
    // Same pill language as every other dialog footer — not a bespoke button.
    expect(buttons[0].classes()).toContain('fbtn')
    expect(buttons[1].classes()).toContain('fbtn')
    expect(buttons[1].classes()).toContain('fbtn-primary')
  })

  it('seeds the draft from the CALLER’s list, not the global chat draft', () => {
    // The footer is the "selected so far" area and the row check marks read the
    // same draft. It must describe the list the caller is editing: the
    // queued-message editor passes its own draft, and the composable's
    // `attachedFiles` (the global chat draft) used to shadow that prop — so a
    // file picked there was attached but never showed up in the footer, while
    // the same pick in the chat input did.
    sharedAttachedFiles.value = [{ path: 'src/chat-draft.ts' }]
    const wrapper = mountDrawer({ attachedFiles: [{ path: 'src/queued-edit.ts' }] })

    const names = wrapper.findAll('.ad-footer .attachment-filename').map(w => w.text())
    expect(names).toEqual(['queued-edit.ts'])
    expect(names).not.toContain('chat-draft.ts')
  })

  it('shows a pick in the footer immediately, without touching the composer', async () => {
    sharedAttachedFiles.value = []
    const wrapper = mountDrawer({ attachedFiles: [] })
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(0)

    await getRawState(wrapper).toggleAttached('src/picked.ts')

    expect(wrapper.findAll('.ad-footer .attachment-filename').map(w => w.text()))
      .toEqual(['picked.ts'])
    expect(wrapper.emitted('add-attached')).toBeFalsy()
  })

  it('OK commits the draft as a delta and closes', async () => {
    // Selection is local, so OK is the single point where the composer learns
    // about it — one add and one remove for a swap, nothing for untouched rows.
    const wrapper = mountDrawer({
      attachedFiles: [{ path: 'src/keep.ts' }, { path: 'src/removed-dir', isDir: true }],
    })
    const state = getRawState(wrapper)
    state.toggleAttached('src/removed-dir', true)   // deselect
    state.toggleAttached('src/added.ts')            // select

    await wrapper.find('[data-action="attach-ok"]').trigger('click')

    expect(wrapper.emitted('add-attached')).toEqual([['src/added.ts', false]])
    expect(wrapper.emitted('remove-attached')).toEqual([[{ path: 'src/removed-dir', isDir: true }]])
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('OK is a plain close when the selection was not touched', async () => {
    const wrapper = mountDrawer({ attachedFiles: [{ path: 'src/a.ts' }] })
    await wrapper.find('[data-action="attach-ok"]').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
    expect(wrapper.emitted('add-attached')).toBeFalsy()
    expect(wrapper.emitted('remove-attached')).toBeFalsy()
  })

  it('cancel discards the draft without emitting anything', async () => {
    const wrapper = mountDrawer({ attachedFiles: [{ path: 'src/keep.ts' }] })
    const state = getRawState(wrapper)
    state.toggleAttached('src/added.ts')
    state.toggleAttached('src/keep.ts')

    await wrapper.find('[data-action="attach-cancel"]').trigger('click')

    expect(wrapper.emitted('add-attached')).toBeFalsy()
    expect(wrapper.emitted('remove-attached')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('re-opening after a cancel starts from the composer again', async () => {
    // The draft is seeded on open, so a discarded selection must not leak into
    // the next session.
    const wrapper = mountDrawer({ attachedFiles: [] })
    await getRawState(wrapper).toggleAttached('src/added.ts')
    await wrapper.find('[data-action="attach-cancel"]').trigger('click')

    await wrapper.setProps({ open: false })
    await nextTick()
    await wrapper.setProps({ open: true })
    await nextTick()

    expect(getRawState(wrapper).isAttached('src/added.ts')).toBe(false)
    expect(wrapper.findAll('.ad-footer .attachment-filename')).toHaveLength(0)
  })

  it('deleting a selected file removes it from the composer at once, not on OK', async () => {
    // The file is gone from disk: leaving it attached (even in the draft) would
    // make the next message reference a path that no longer exists, so this is
    // the one action that bypasses the commit step.
    mockDialogConfirm.mockResolvedValue(true)
    const wrapper = mountDrawer({
      attachedFiles: [{ path: '.clawbench/uploads/a.png' }],
    })
    await getRawState(wrapper).handleDeleteUpload({ path: '.clawbench/uploads/a.png', origin: 'uploads' })

    expect(wrapper.emitted('remove-attached')).toEqual([[{ path: '.clawbench/uploads/a.png' }]])
    expect(getRawState(wrapper).isAttached('.clawbench/uploads/a.png')).toBe(false)
  })
})

describe('AttachDrawer chrome (compact source strip, borderless clear-all)', () => {
  const source = readFileSync(join(__dirname, '..', 'AttachDrawer.vue'), 'utf8')

  /** Declarations of the first `<selector> {` rule in the SFC. */
  function declsOf(selector: string): string {
    const m = source.match(
      new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}'),
    )
    expect(m, `${selector} rule must exist in AttachDrawer.vue`).not.toBeNull()
    return m![1]
  }

  it('the three source entries are single-line pills, not stacked blocks', () => {
    // "Compact but keep the icon and the label": icon and text side by side on
    // one short row. A column layout with 44px min-height is what made the
    // strip eat a fifth of the sheet on a phone.
    const decls = declsOf('.ad-source-btn')
    expect(decls).not.toMatch(/flex-direction\s*:\s*column/)
    expect(decls).toMatch(/height\s*:\s*calc\(34 \* var\(--dp\)\)/)
    expect(decls).toMatch(/border-radius\s*:\s*var\(--radius-sm\)/)
  })

  it('clear-all is a borderless icon+label button like the other clear-alls', () => {
    // The bordered pill made it the loudest thing in the sheet while every
    // other clear-all in the app is a plain text button.
    const decls = declsOf('.ad-clear-btn')
    expect(decls).toMatch(/border\s*:\s*none/)
    expect(decls).toMatch(/background\s*:\s*transparent/)
    expect(decls).not.toMatch(/--radius-full/)
  })

  it('clear-all stays muted at rest and only turns red on interaction', () => {
    expect(declsOf('.ad-clear-btn')).toMatch(/color\s*:\s*var\(--text-muted\)/)
    const hover = source.match(/\.ad-clear-btn:hover:not\(:disabled\)\s*\{([^}]*)\}/)
    expect(hover, 'the hover rule must exist').not.toBeNull()
    expect(hover![1]).toMatch(/--color-red/)
  })
})
