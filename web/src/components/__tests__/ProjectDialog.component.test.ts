import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
  createI18n: (opts: any) => ({
    global: { t: (k: string) => k, locale: { value: opts?.locale ?? 'en' } },
    install() {},
  }),
}))

const { mockFetch, mockStore } = vi.hoisted(() => {
  return {
    mockFetch: vi.fn(),
    mockStore: { state: { rootPaths: ['/'], homeDir: '/home/user' } },
  }
})

vi.stubGlobal('fetch', mockFetch)

vi.mock('@/stores/app', () => ({ store: mockStore }))

const { mockDialogPrompt, mockDialogAlert, mockDialogConfirm } = vi.hoisted(() => ({
  mockDialogPrompt: vi.fn(),
  mockDialogAlert: vi.fn(),
  mockDialogConfirm: vi.fn(),
}))

vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({
    prompt: mockDialogPrompt,
    alert: mockDialogAlert,
    confirm: mockDialogConfirm,
  }),
}))

import ProjectDialog from '@/components/ProjectDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      projectDialog: {
        title: 'Select Project Directory',
        newFolder: 'New folder',
        search: 'Search',
        showHiddenFiles: 'Show hidden',
        hideHiddenFiles: 'Hide hidden',
        promptFolderName: 'Folder name',
        promptNewName: 'New name',
        noMatchDirs: 'No matches',
        emptyDir: 'Empty',
        confirmDelete: 'Delete? {name}',
        loadFailed: 'Load failed',
        createFailed: 'Create failed',
        renameFailed: 'Rename failed',
        deleteFailed: 'Delete failed',
        setProjectFailed: 'Set failed',
        setProjectFailedDetail: 'Set failed: {error}',
        renameFailedDetail: 'Rename failed: {error}',
        deleteFailedDetail: 'Delete failed: {error}',
      },
      jump: { title: 'Jump', placeholder: 'Path', confirm: 'Go', cancel: 'Cancel', button: 'Jump' },
      nav: { refresh: 'Refresh' },
      common: { loading: 'Loading...', rename: 'Rename', delete: 'Delete', cancel: 'Cancel', confirm: 'Confirm' },
    },
  },
})

const TeleportStub = { template: '<div><slot /></div>' }

const JumpStub = defineComponent({
  props: ['open'],
  emits: ['close', 'confirm'],
  setup() { return {} },
  template: '<div v-if="open" class="jump-dialog-stub" @click="$emit(\'confirm\', \'src/utils\')" />',
})

const FileIconStub = defineComponent({
  name: 'FileIcon',
  props: ['path', 'isDir', 'size'],
  setup() { return {} },
  template: '<span class="file-icon-stub" />',
})

const SearchInputStub = defineComponent({
  name: 'SearchInput',
  props: ['modelValue', 'placeholder'],
  emits: ['update:modelValue'],
  setup(props) { return {} },
  template: '<input class="search-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
})

const RefreshButtonStub = defineComponent({
  name: 'RefreshButton',
  props: ['loading', 'disabled', 'title', 'icon', 'size'],
  emits: ['click'],
  setup() { return {} },
  template: '<button class="refresh-stub" :disabled="disabled" @click="$emit(\'click\')" />',
})

const DirBreadcrumbStub = defineComponent({
  name: 'DirBreadcrumb',
  props: ['path'],
  emits: ['navigate'],
  setup() { return {} },
  template: '<div class="breadcrumb-stub" @click="$emit(\'navigate\', \'\')" />',
})

const LoadingIndicatorStub = defineComponent({
  name: 'LoadingIndicator',
  props: ['size', 'label'],
  setup() { return {} },
  template: '<div class="loading-stub" />',
})

const ModalDialogStub = defineComponent({
  name: 'ModalDialog',
  props: ['open', 'title', 'maxWidth', 'fullHeight', 'zIndex'],
  emits: ['close'],
  setup() { return {} },
  template: '<div class="modal-stub" :data-open="String(open)"><slot name="header" /><slot /><slot name="footer" /></div>',
})

function mountDialog(props: Record<string, unknown> = {}, provides: Record<string, any> = {}) {
  const toastValue = provides.toast === null ? null : (provides.toast ?? { show: vi.fn() })
  const hspValue = provides.hotSwitchProject === null ? null : (provides.hotSwitchProject ?? vi.fn())
  return mount(ProjectDialog, {
    props: { open: true, ...props },
    global: {
      stubs: {
        Teleport: TeleportStub,
        JumpDirDialog: JumpStub,
        FileIcon: FileIconStub,
        SearchInput: SearchInputStub,
        RefreshButton: RefreshButtonStub,
        DirBreadcrumb: DirBreadcrumbStub,
        LoadingIndicator: LoadingIndicatorStub,
        ModalDialog: ModalDialogStub,
      },
      plugins: [i18n],
      provide: {
        toast: toastValue,
        hotSwitchProject: hspValue,
      },
    },
  })
}

/** Mount then trigger prop change to fire the open watcher (which lacks immediate: true). */
async function mountOpen(props: Record<string, unknown> = {}, provides: Record<string, any> = {}) {
  const wrapper = mountDialog({ ...props, open: false }, provides)
  await wrapper.setProps({ open: true })
  return wrapper
}

function okJson(data: any) { return Promise.resolve({ ok: true, json: () => Promise.resolve(data) }) }
function errJson(data: any) { return Promise.resolve({ ok: false, json: () => Promise.resolve(data) }) }

beforeEach(() => {
  mockFetch.mockReset()
  mockStore.state.rootPaths = ['/']
  mockStore.state.homeDir = '/home/user'
  mockFetch.mockResolvedValue(okJson({ path: '/home/user', items: [] }))
  mockDialogPrompt.mockReset()
  mockDialogConfirm.mockReset()
  mockDialogAlert.mockReset()
})

describe('ProjectDialog — mount', () => {
  it('mounts without errors', () => {
    const wrapper = mountDialog()
    expect(wrapper.exists()).toBe(true)
  })

  it('emits close when modal closes', async () => {
    const wrapper = mountDialog()
    const modal = wrapper.findComponent({ name: 'ModalDialog' })
    await modal.vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('emits close on cancel button click', async () => {
    const wrapper = mountDialog()
    await wrapper.find('.cancel-btn').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})

describe('ProjectDialog — loadBrowse', () => {
  it('loads items when opened', async () => {
    mockFetch.mockResolvedValueOnce(okJson({ path: '/home/user', items: [] }))
    const wrapper = await mountOpen()
    await flushPromises()
    expect(mockFetch).toHaveBeenCalled()
  })

  it('handles fetch failure with toast', async () => {
    mockFetch.mockRejectedValueOnce(new Error('boom'))
    const toastShow = vi.fn()
    const wrapper = await mountOpen({}, { toast: { show: toastShow } })
    await flushPromises()
    expect(toastShow).toHaveBeenCalled()
  })

  it('handles root-level response (path="")', async () => {
    mockFetch.mockResolvedValueOnce(okJson({ path: '', items: [{ name: 'root', type: 'dir' }] }))
    const wrapper = await mountOpen()
    await flushPromises()
    expect(wrapper.vm).toBeTruthy()
  })
})

function mountNonRoot(props: Record<string, unknown> = {}, provides: Record<string, any> = {}) {
  // Helper: mount with currentBrowseAbs set so isRootLevel is false
  return mountOpen(props, provides).then(async (wrapper) => {
    await flushPromises()
    const vm = wrapper.vm as any
    vm.currentBrowseAbs = '/home/user'
    vm.browsePath = '/home/user'
    await flushPromises()
    return wrapper
  })
}

describe('ProjectDialog — toolbar buttons', () => {
  it('toggles show hidden files', async () => {
    const wrapper = await mountNonRoot()
    const showBtn = wrapper.find('button[title*="projectDialog.showHiddenFiles"]')
    expect(showBtn.exists()).toBe(true)
    await showBtn.trigger('click')
    expect((wrapper.vm as any).showHidden).toBe(true)
  })

  it('refresh button calls loadBrowse', async () => {
    mockFetch.mockClear()
    mockFetch.mockResolvedValue(okJson({ path: '/home/user', items: [] }))
    const wrapper = await mountOpen()
    await flushPromises()
    await wrapper.find('.refresh-stub').trigger('click')
    await flushPromises()
    expect(mockFetch).toHaveBeenCalled()
  })

  it('new folder button calls dialog.prompt', async () => {
    mockDialogPrompt.mockResolvedValueOnce(null)
    const wrapper = await mountNonRoot()
    const btn = wrapper.find('button[title*="projectDialog.newFolder"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(mockDialogPrompt).toHaveBeenCalled()
  })

  it('new folder with valid name creates folder', async () => {
    mockDialogPrompt.mockResolvedValueOnce('newfolder')
    mockFetch.mockResolvedValueOnce(okJson({ path: '/home/user/newfolder', items: [] }))
    const wrapper = await mountNonRoot()
    const btn = wrapper.find('button[title*="projectDialog.newFolder"]')
    await btn.trigger('click')
    await flushPromises()
    expect(mockFetch).toHaveBeenCalledWith('/api/projects', expect.objectContaining({ method: 'POST' }))
  })

  it('new folder with empty name does nothing', async () => {
    mockDialogPrompt.mockResolvedValueOnce('   ')
    const wrapper = await mountNonRoot()
    const btn = wrapper.find('button[title*="projectDialog.newFolder"]')
    await btn.trigger('click')
    await flushPromises()
    const postCalls = mockFetch.mock.calls.filter(c => c[1]?.method === 'POST')
    expect(postCalls).toHaveLength(0)
  })

  it('new folder with server error shows alert', async () => {
    mockDialogPrompt.mockResolvedValueOnce('newfolder')
    mockFetch.mockResolvedValue(okJson({ path: '/home/user', items: [] }))
    const wrapper = await mountNonRoot()
    // Queue error response AFTER mount so it's consumed by POST (next fetch)
    mockFetch.mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'exists' }) })
    const btn = wrapper.find('button[title*="projectDialog.newFolder"]')
    await btn.trigger('click')
    await flushPromises()
    expect(mockDialogAlert).toHaveBeenCalled()
  })
})

describe('ProjectDialog — search filtering', () => {
  it('filters items by search query', async () => {
    mockFetch.mockResolvedValueOnce(okJson({ path: '/home/user', items: [
      { name: 'src', type: 'dir' }, { name: 'docs', type: 'dir' }, { name: 'README', type: 'file' },
    ] }))
    const wrapper = await mountOpen()
    await flushPromises()
    const search = wrapper.find('.search-stub')
    await search.setValue('src')
    await flushPromises()
    expect(wrapper.vm).toBeTruthy()
  })

  it('shows empty message when search has no matches', async () => {
    mockFetch.mockResolvedValueOnce(okJson({ path: '/home/user', items: [
      { name: 'src', type: 'dir' },
    ] }))
    const wrapper = await mountOpen()
    await flushPromises()
    const search = wrapper.find('.search-stub')
    await search.setValue('xyz')
    await flushPromises()
    expect(wrapper.find('.dialog-empty').exists()).toBe(true)
  })

  it('shows empty when directory is empty', async () => {
    mockFetch.mockResolvedValueOnce(okJson({ path: '/home/user', items: [] }))
    const wrapper = await mountOpen()
    await flushPromises()
    expect(wrapper.find('.dialog-empty').exists()).toBe(true)
  })
})

describe('ProjectDialog — hidden files', () => {
  it('filters out hidden files when showHidden=false', async () => {
    mockFetch.mockResolvedValueOnce(okJson({ path: '/home/user', items: [
      { name: '.git', type: 'dir' }, { name: 'src', type: 'dir' },
    ] }))
    const wrapper = await mountOpen()
    await flushPromises()
    expect(wrapper.findAll('.dialog-item')).toHaveLength(1)
  })

  it('shows hidden files when showHidden=true', async () => {
    mockFetch.mockResolvedValueOnce(okJson({ path: '/home/user', items: [
      { name: '.git', type: 'dir' }, { name: 'src', type: 'dir' },
    ] }))
    const wrapper = await mountOpen()
    await flushPromises()
    const btn = wrapper.find('button[title*="HiddenFiles"]')
    await btn.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.dialog-item').length).toBeGreaterThanOrEqual(2)
  })
})

describe('ProjectDialog — confirm action', () => {
  it('calls hotSwitchProject with selectedPath', async () => {
    const hotSwitchProject = vi.fn()
    const wrapper = await mountOpen({}, { hotSwitchProject })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedPath = '/home/user/src'
    vm.confirm()
    await flushPromises()
    expect(hotSwitchProject).toHaveBeenCalledWith('/home/user/src')
  })

  it('falls back to currentBrowseAbs when selectedPath is empty', async () => {
    const hotSwitchProject = vi.fn()
    mockFetch.mockResolvedValueOnce(okJson({ path: '/home/user', items: [] }))
    const wrapper = await mountOpen({}, { hotSwitchProject })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.currentBrowseAbs = '/home/user'
    vm.confirm()
    await flushPromises()
    expect(hotSwitchProject).toHaveBeenCalledWith('/home/user')
  })

  it('does nothing when no path selected', async () => {
    const hotSwitchProject = vi.fn()
    const wrapper = await mountOpen({}, { hotSwitchProject })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedPath = ''
    vm.currentBrowseAbs = ''
    vm.confirm()
    await flushPromises()
    expect(hotSwitchProject).not.toHaveBeenCalled()
  })

  it('uses legacy /api/project endpoint when no hotSwitchProject', async () => {
    mockFetch.mockResolvedValue(okJson({ path: '/home/user', items: [] }))
    const reloadSpy = vi.fn()
    ;(window as any).location = { ...window.location, reload: reloadSpy }
    const wrapper = await mountOpen({}, { hotSwitchProject: null })
    await flushPromises()
    mockFetch.mockResolvedValueOnce(okJson({ path: '/home/user/src' }))
    const vm = wrapper.vm as any
    vm.selectedPath = '/home/user/src'
    await vm.confirm()
    await flushPromises()
    expect(mockFetch).toHaveBeenCalledWith('/api/project', expect.objectContaining({ method: 'POST' }))
  })

  it('shows alert on legacy endpoint failure', async () => {
    mockFetch.mockResolvedValue(okJson({ path: '/home/user', items: [] }))
    const wrapper = await mountOpen({}, { hotSwitchProject: null })
    await flushPromises()
    mockFetch.mockResolvedValueOnce({ ok: false, text: () => Promise.resolve('failed') })
    const vm = wrapper.vm as any
    vm.selectedPath = '/home/user/src'
    await vm.confirm()
    await flushPromises()
    expect(mockDialogAlert).toHaveBeenCalled()
  })
})

describe('ProjectDialog — onBreadcrumbNavigate', () => {
  it('navigates to root on empty path', async () => {
    mockFetch.mockResolvedValue(okJson({ path: '', items: [] }))
    const wrapper = await mountOpen()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.onBreadcrumbNavigate('')
    expect(vm.browsePath).toBe('')
  })

  it('normalizes Windows path with forward slashes', async () => {
    mockFetch.mockResolvedValue(okJson({ path: 'C:\\foo', items: [] }))
    const wrapper = await mountOpen()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.onBreadcrumbNavigate('C:/foo/bar')
    expect(vm.browsePath).toBe('C:\\foo\\bar')
  })

  it('prepends slash for Unix relative path', async () => {
    mockFetch.mockResolvedValue(okJson({ path: 'src/utils', items: [] }))
    const wrapper = await mountOpen()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.onBreadcrumbNavigate('src/utils')
    expect(vm.browsePath).toBe('/src/utils')
  })
})

describe('ProjectDialog — rename and delete', () => {
  it('rename shows alert on error', async () => {
    mockDialogPrompt.mockResolvedValueOnce('newname')
    mockFetch.mockResolvedValue(okJson({ path: '/home/user', items: [] }))
    const wrapper = await mountOpen()
    await flushPromises()
    mockFetch.mockResolvedValueOnce(errJson({ error: 'exists' }))
    const vm = wrapper.vm as any
    await vm.doRename({ name: 'old', path: '/home/user/old' })
    expect(mockDialogAlert).toHaveBeenCalled()
  })

  it('rename skips when name is unchanged', async () => {
    mockDialogPrompt.mockResolvedValueOnce('same')
    const wrapper = await mountOpen()
    await flushPromises()
    const vm = wrapper.vm as any
    mockFetch.mockClear()
    await vm.doRename({ name: 'same', path: '/home/user/same' })
    expect(mockFetch).not.toHaveBeenCalledWith(expect.stringContaining('/api/file/rename'), expect.anything())
  })

  it('rename success refreshes browse', async () => {
    mockDialogPrompt.mockResolvedValueOnce('newname')
    mockFetch.mockResolvedValue(okJson({ path: '/home/user', items: [] }))
    mockFetch.mockResolvedValueOnce(okJson({}))
    const wrapper = await mountOpen()
    await flushPromises()
    const vm = wrapper.vm as any
    mockFetch.mockClear()
    await vm.doRename({ name: 'old', path: '/home/user/old' })
    await flushPromises()
    expect(mockFetch).toHaveBeenCalledWith('/api/file/rename', expect.objectContaining({ method: 'POST' }))
  })

  it('delete cancels if confirm returns false', async () => {
    mockDialogConfirm.mockResolvedValueOnce(false)
    const wrapper = await mountOpen()
    await flushPromises()
    const vm = wrapper.vm as any
    mockFetch.mockClear()
    await vm.doDelete({ name: 'foo', path: '/home/user/foo' })
    expect(mockFetch).not.toHaveBeenCalledWith(expect.stringContaining('/api/file/delete'), expect.anything())
  })

  it('delete success refreshes browse', async () => {
    mockDialogConfirm.mockResolvedValueOnce(true)
    mockFetch.mockResolvedValue(okJson({ path: '/home/user', items: [] }))
    mockFetch.mockResolvedValueOnce(okJson({}))
    const wrapper = await mountOpen()
    await flushPromises()
    const vm = wrapper.vm as any
    mockFetch.mockClear()
    await vm.doDelete({ name: 'foo', path: '/home/user/foo' })
    await flushPromises()
    expect(mockFetch).toHaveBeenCalledWith('/api/file/delete', expect.objectContaining({ method: 'POST' }))
  })

  it('delete error shows alert', async () => {
    mockDialogConfirm.mockResolvedValueOnce(true)
    mockFetch.mockResolvedValue(okJson({ path: '/home/user', items: [] }))
    const wrapper = await mountOpen()
    await flushPromises()
    mockFetch.mockResolvedValueOnce(errJson({ error: 'permission denied' }))
    const vm = wrapper.vm as any
    await vm.doDelete({ name: 'foo', path: '/home/user/foo' })
    expect(mockDialogAlert).toHaveBeenCalled()
  })
})

describe('ProjectDialog — isWindows', () => {
  it('isWindows true when multiple rootPaths', async () => {
    mockStore.state.rootPaths = ['C:\\', 'D:\\']
    const wrapper = await mountOpen()
    await flushPromises()
    expect((wrapper.vm as any).isWindows).toBe(true)
  })

  it('isWindows false when single rootPath', async () => {
    mockStore.state.rootPaths = ['/']
    const wrapper = await mountOpen()
    await flushPromises()
    expect((wrapper.vm as any).isWindows).toBe(false)
  })
})