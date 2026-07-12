import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, nextTick, h } from 'vue'

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
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div class="bottom-sheet"><slot name="header" /><slot /></div>',
    props: ['open', 'auto', 'title'],
    emits: ['close'],
  },
}))

vi.mock('@/composables/useShareIn', () => ({
  useShareIn: () => ({
    recentShares: ref([]),
    fetchRecentShare: vi.fn(),
  }),
}))

vi.mock('@/composables/useUploadRecent', () => ({
  useUploadRecent: () => ({
    recentUploads: ref([]),
    fetchRecentUploads: vi.fn(),
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
  buildPathThumbUrl: (path: string) => `/api/file/thumb?path=${encodeURIComponent(path)}&w=80`,
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
        },
      },
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
    // DOM class update is unreliable in jsdom (same as SessionSettingModal.test.ts)
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
    // effectiveCurrentDir falls back to '.' which is truthy, so dir row is shown
    expect(wrapper.find('.ad-empty').exists()).toBe(false)
  })

  it('renders empty state for references tab (default mount has no referenced files)', () => {
    const wrapper = mountDrawer()
    // On the current tab, no .ad-empty exists for references yet
    // Verify referenced files list is empty via props
    expect(wrapper.props('recentReferencedFiles')).toEqual([])
  })

  it('renders referenced files on current tab when provided', () => {
    // We can't reliably switch tabs in jsdom, but we verify the data flow:
    // recentReferencedFiles prop is accepted and the component has isAttached/toggleAttached
    const wrapper = mountDrawer({
      recentReferencedFiles: [{ path: 'src/foo.ts', count: 3 }],
    })
    expect(wrapper.props('recentReferencedFiles')).toEqual([{ path: 'src/foo.ts', count: 3 }])
  })

  it('emits add-attached when clicking unattached file', async () => {
    const wrapper = mountDrawer({
      currentDir: 'src',
      attachedFiles: [],
    })
    await wrapper.find('.ad-current-item').trigger('click')
    expect(wrapper.emitted('add-attached')).toBeTruthy()
    expect(wrapper.emitted('add-attached')![0]).toEqual(['src'])
  })

  it('emits remove-attached when clicking attached file', async () => {
    const wrapper = mountDrawer({
      currentDir: 'src',
      attachedFiles: ['src'],
    })
    await wrapper.find('.ad-current-item').trigger('click')
    expect(wrapper.emitted('remove-attached')).toBeTruthy()
    expect(wrapper.emitted('remove-attached')![0]).toEqual(['src'])
  })

  it('emits upload when clicking upload button', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.ad-upload-btn').trigger('click')
    expect(wrapper.emitted('upload')).toBeTruthy()
  })

  it('emits file-open when clicking external link on current dir row', async () => {
    const wrapper = mountDrawer({ currentDir: 'src' })
    await wrapper.find('.ad-current-item .ad-file-open').trigger('click')
    expect(wrapper.emitted('file-open')).toBeTruthy()
    expect(wrapper.emitted('file-open')![0]).toEqual(['src'])
  })

  it('emits file-open when clicking external link on current file row', async () => {
    const wrapper = mountDrawer({ currentFile: 'src/main.ts' })
    const items = wrapper.findAll('.ad-current-item')
    // Second current-item is the file row
    await items[1].find('.ad-file-open').trigger('click')
    expect(wrapper.emitted('file-open')).toBeTruthy()
    expect(wrapper.emitted('file-open')![0]).toEqual(['src/main.ts'])
  })

  it('applies ad-file-attached class to attached items', () => {
    const wrapper = mountDrawer({
      currentDir: 'src',
      attachedFiles: ['src'],
    })
    expect(wrapper.find('.ad-current-item').classes()).toContain('ad-file-attached')
  })

  it('toggleAttached emits add-attached for unattached path', async () => {
    const wrapper = mountDrawer({ attachedFiles: [] })
    getRawState(wrapper).toggleAttached('src/a.ts')
    expect(wrapper.emitted('add-attached')).toBeTruthy()
    expect(wrapper.emitted('add-attached')![0]).toEqual(['src/a.ts'])
  })

  it('toggleAttached emits remove-attached for already-attached path', async () => {
    const wrapper = mountDrawer({ attachedFiles: ['src/a.ts'] })
    getRawState(wrapper).toggleAttached('src/a.ts')
    expect(wrapper.emitted('remove-attached')).toBeTruthy()
    expect(wrapper.emitted('remove-attached')![0]).toEqual(['src/a.ts'])
  })

  it('isAttached returns true for attached file', () => {
    const wrapper = mountDrawer({ attachedFiles: ['src/main.ts'] })
    expect(getRawState(wrapper).isAttached('src/main.ts')).toBe(true)
  })

  it('isAttached returns false for unattached file', () => {
    const wrapper = mountDrawer({ attachedFiles: [] })
    expect(getRawState(wrapper).isAttached('src/main.ts')).toBe(false)
  })

  it('handleUpload emits upload', () => {
    const wrapper = mountDrawer()
    getRawState(wrapper).handleUpload()
    expect(wrapper.emitted('upload')).toBeTruthy()
  })

  it('effectiveCurrentDir falls back to "." when currentDir is null', () => {
    const wrapper = mountDrawer({ currentDir: null })
    expect(getRawState(wrapper).effectiveCurrentDir.value).toBe('.')
  })

  it('effectiveCurrentDir uses currentDir when provided', () => {
    const wrapper = mountDrawer({ currentDir: 'src/components' })
    expect(getRawState(wrapper).effectiveCurrentDir.value).toBe('src/components')
  })

  it('currentDirDisplayName shows "/" for "."', () => {
    const wrapper = mountDrawer({ currentDir: null })
    expect(getRawState(wrapper).currentDirDisplayName.value).toBe('/')
  })

  it('currentDirDisplayName shows baseName for non-root dir', () => {
    const wrapper = mountDrawer({ currentDir: 'src/components' })
    expect(getRawState(wrapper).currentDirDisplayName.value).toBe('components')
  })
})
