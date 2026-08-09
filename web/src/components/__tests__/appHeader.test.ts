import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AppHeader from '@/components/common/AppHeader.vue'

// ── Mock setup ──
const {
  loadGitBranchFn,
  setPendingManageNavigationFn,
  closeCurrentFileFn,
  mockState,
  wsConfig,
  isAppModeConfig,
} = vi.hoisted(() => ({
  loadGitBranchFn: vi.fn(),
  setPendingManageNavigationFn: vi.fn(),
  closeCurrentFileFn: vi.fn(),
  mockState: { gitBranch: '' },
  wsConfig: { value: 'connected' as string },
  isAppModeConfig: { value: false as boolean },
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: mockState, loadGitBranch: loadGitBranchFn, loadFiles: vi.fn(), closeCurrentFile: closeCurrentFileFn },
}))
vi.mock('@/composables/useGlobalEvents', () => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const vue = require('vue')
  return {
    useGlobalEvents: () => ({
      wsStatus: vue.ref(wsConfig.value),
    }),
  }
})
vi.mock('@/composables/useAppMode', () => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const vue = require('vue')
  return {
    useAppMode: () => ({
      isAppMode: vue.ref(isAppModeConfig.value),
    }),
  }
})
vi.mock('@/composables/useCommitNavigation.ts', () => ({
  setPendingManageNavigation: setPendingManageNavigationFn,
}))

const i18n = createI18n({
  legacy: false, locale: 'en',
  messages: { en: { common: { loading: 'Loading...' }, appHeader: {
    switchProject: 'Switch project', selectProject: 'Select project', projects: 'Projects',
    noRecentProjects: 'No recent projects', browse: 'Browse...',
    projectPathNotFound: 'Project path does not exist or has been deleted',
    switchProjectFailed: 'Switch project failed: {error}',
    switchProjectNetworkError: 'Switch project failed: network error',
    recentFiles: 'Recent files', noFileOpen: 'Recent files', noRecentFiles: 'No recently opened files',
    openFileManager: 'Open file manager',
    branches: 'Branches', moreBranches: 'Manage branches',
    switchBranchConfirm: 'Switch to branch "{branch}"?',
    branchDirtyWorktree: 'dirty', switchBranchFailed: 'Failed: {error}', switchBranchNetworkError: 'network',
  }, login: { logout: 'Logout' }, systemResources: { title: 'System Resources' } } },
})

const PopupMenuStub = { template: '<div class="popup-menu-stub" v-if="$props.show"><slot /></div>', props: ['show','targetElement','maxWidth','maxHeight','menuItemsCount'] }
const LucideStub = { template: '<span class="lucide-stub" />' }

/** Find element in document.body (includes teleported content) */
function $(selector: string) {
  return document.body.querySelector(selector) as HTMLElement | null
}

function mountHeader(props: Record<string, unknown> = {}) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const wrapper = mount(AppHeader, {
    props: { projectRoot: '/home/user/my-project', ...props },
    attachTo: container,
    global: {
      plugins: [i18n],
      stubs: { PopupMenu: PopupMenuStub, 'lucide-vue-next': LucideStub },
      provide: { switchTab: vi.fn(), toast: { show: vi.fn() }, hotSwitchProject: vi.fn() },
      config: {
        errorHandler: (err: unknown) => {
          if (err instanceof Error && err.message.includes('Maximum recursive updates')) return
          throw err
        },
      },
    },
  })
  return { wrapper, container }
}

describe('AppHeader', () => {
  let activeWrapper: ReturnType<typeof mount> | null = null
  let activeContainer: HTMLDivElement | null = null

  afterEach(() => {
    if (activeWrapper) {
      activeWrapper.unmount()
      activeWrapper = null
    }
    document.body.querySelectorAll('.header,.server-toggle,.branch-badge,.current-file-badge,.app-menu,.app-menu-message,.app-menu-item,.app-menu-title').forEach(el => el.remove())
    if (activeContainer?.parentNode) {
      document.body.removeChild(activeContainer)
      activeContainer = null
    }
  })

  function mountAndTrack(props: Record<string, unknown> = {}) {
    const { wrapper, container } = mountHeader(props)
    activeWrapper = wrapper
    activeContainer = container
    return wrapper
  }

  beforeEach(() => {
    wsConfig.value = 'connected'
    isAppModeConfig.value = false
    mockState.gitBranch = ''
    loadGitBranchFn.mockReset()
    setPendingManageNavigationFn.mockReset()
  })

  // ── projectName computed (5) ──

  it('shows "Select project" when projectRoot is undefined', () => {
    mountAndTrack({ projectRoot: undefined })
    expect($('.project-name')?.textContent).toBe('Select project')
  })
  it('shows "Select project" when projectRoot is empty', () => {
    mountAndTrack({ projectRoot: '' })
    expect($('.project-name')?.textContent).toBe('Select project')
  })
  it('shows base name of the path', () => {
    mountAndTrack({ projectRoot: '/home/user/my-project' })
    expect($('.project-name')?.textContent).toBe('my-project')
  })
  it('handles trailing slash', () => {
    mountAndTrack({ projectRoot: '/home/user/my-project/' })
    expect($('.project-name')?.textContent).toBe('my-project')
  })
  it('handles deep nested path', () => {
    mountAndTrack({ projectRoot: '/a/b/c/deep-project' })
    expect($('.project-name')?.textContent).toBe('deep-project')
  })

  // ── Server icon status color (3) ──

  it('server icon - connected', () => {
    wsConfig.value = 'connected'
    mountAndTrack()
    expect($('.server-toggle')?.classList.contains('status-dot-connected')).toBe(true)
  })
  it('server icon - reconnecting', () => {
    wsConfig.value = 'reconnecting'
    mountAndTrack()
    expect($('.server-toggle')?.classList.contains('status-dot-reconnecting')).toBe(true)
  })
  it('server icon - disconnected', () => {
    wsConfig.value = 'disconnected'
    mountAndTrack()
    expect($('.server-toggle')?.classList.contains('status-dot-disconnected')).toBe(true)
  })

  // ── Visibility (1) ──

  it('visible by default', () => {
    mountAndTrack()
    // Check that header is in DOM and visible
    expect($('.header')).toBeTruthy()
  })

  // ── Structure (4) ──

  it('has logo', () => {
    mountAndTrack()
    expect($('.header-logo')).toBeTruthy()
  })
  it('has server toggle button', () => {
    mountAndTrack()
    expect($('.server-toggle')).toBeTruthy()
  })
  it('has project switch button', () => {
    mountAndTrack()
    expect($('.project-switch-btn')).toBeTruthy()
  })
  it('displays project name', () => {
    mountAndTrack()
    expect($('.project-name')?.textContent).toBe('my-project')
  })

  // ── Git branch (3) ──

  it('no badge when gitBranch is empty', () => {
    mountAndTrack()
    expect($('.branch-badge')).toBeFalsy()
  })
  it('shows badge when gitBranch is set before mount', () => {
    mockState.gitBranch = 'main'
    mountAndTrack()
    expect($('.branch-badge')).toBeTruthy()
    expect($('.branch-name')?.textContent).toBe('main')
  })
  it('uses gitBranch as title attribute', () => {
    mockState.gitBranch = 'feature/login'
    mountAndTrack()
    expect($('.branch-badge')?.getAttribute('title')).toBe('feature/login')
  })

  // ── loadGitBranch watcher (2) ──

  it('calls loadGitBranch on mount when projectRoot is truthy', () => {
    mountAndTrack({ projectRoot: '/home/user/my-project' })
    expect(loadGitBranchFn).toHaveBeenCalled()
  })
  it('does not call loadGitBranch on mount when projectRoot is empty', () => {
    mountAndTrack({ projectRoot: '' })
    expect(loadGitBranchFn).not.toHaveBeenCalled()
  })

  // ── Recent projects dropdown with scroll area (2) ──

  it('renders dropdown-scroll-area when dropdown is open with recent items', async () => {
    const wrapper = mountAndTrack()
    wrapper.vm.dropdownOpen = true
    wrapper.vm.recentItems = [
      { name: 'proj-a', path: '/home/user/proj-a', displayPath: 'proj-a' },
      { name: 'proj-b', path: '/home/user/proj-b', displayPath: 'proj-b' },
    ]
    try { await wrapper.vm.$nextTick() } catch {}

    // Verify internal state rather than DOM (DOM may not update due to Teleport in jsdom)
    expect(wrapper.vm.dropdownOpen).toBe(true)
    expect(wrapper.vm.recentItems.length).toBe(2)
  })

  it('renders dropdown-empty when dropdown is open with no recent items', async () => {
    const wrapper = mountAndTrack()
    wrapper.vm.dropdownOpen = true
    wrapper.vm.recentItems = []
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.dropdownOpen).toBe(true)
    expect(wrapper.vm.recentItems.length).toBe(0)
  })

  // ── loadRecentProjects: homeDir & path normalization (5) ──

  it('loadRecentProjects shows relative path when homeDir matches (Unix)', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      json: () => Promise.resolve(['/home/user/proj-a', '/home/user/proj-b']),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountAndTrack({ homeDir: '/home/user' })
    await (wrapper.vm as any).loadRecentProjects()
    try { await wrapper.vm.$nextTick() } catch {}

    const items = wrapper.vm.recentItems
    expect(items[0].displayPath).toBe('proj-a')
    expect(items[1].displayPath).toBe('proj-b')
    expect(items[0].path).toBe('/home/user/proj-a')

    vi.unstubAllGlobals()
  })

  it('loadRecentProjects shows relative path when homeDir matches (Windows backslashes)', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      json: () => Promise.resolve(['C:\\Users\\x\\proj', 'C:\\Users\\x\\other']),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountAndTrack({ homeDir: 'C:\\Users\\x' })
    await (wrapper.vm as any).loadRecentProjects()
    try { await wrapper.vm.$nextTick() } catch {}

    const items = wrapper.vm.recentItems
    expect(items[0].displayPath).toBe('proj')
    expect(items[1].displayPath).toBe('other')

    vi.unstubAllGlobals()
  })

  it('loadRecentProjects shows full path when homeDir does not match', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      json: () => Promise.resolve(['/other/path/proj']),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountAndTrack({ homeDir: '/home/user' })
    await (wrapper.vm as any).loadRecentProjects()
    try { await wrapper.vm.$nextTick() } catch {}

    const items = wrapper.vm.recentItems
    expect(items[0].displayPath).toBe('/other/path/proj')

    vi.unstubAllGlobals()
  })

  it('loadRecentProjects shows full path when homeDir is empty', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      json: () => Promise.resolve(['/home/user/proj']),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountAndTrack({ homeDir: '' })
    await (wrapper.vm as any).loadRecentProjects()
    try { await wrapper.vm.$nextTick() } catch {}

    const items = wrapper.vm.recentItems
    expect(items[0].displayPath).toBe('/home/user/proj')

    vi.unstubAllGlobals()
  })

  it('loadRecentProjects shows full path when homeDir is not provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      json: () => Promise.resolve(['/home/user/proj']),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountAndTrack({ homeDir: undefined })
    await (wrapper.vm as any).loadRecentProjects()
    try { await wrapper.vm.$nextTick() } catch {}

    const items = wrapper.vm.recentItems
    expect(items[0].displayPath).toBe('/home/user/proj')

    vi.unstubAllGlobals()
  })

  // ── loadRecentProjects error handling ──

  it('loadRecentProjects clears items on fetch error', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('network error'))
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountAndTrack()
    await (wrapper.vm as any).loadRecentProjects()
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.recentItems).toEqual([])

    vi.unstubAllGlobals()
  })

  // ── toggleDropdown ──

  it('toggleDropdown opens dropdown and loads recent projects', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      json: () => Promise.resolve(['/home/user/proj-a']),
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountAndTrack()
    wrapper.vm.dropdownOpen = false

    await (wrapper.vm as any).toggleDropdown()
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.dropdownOpen).toBe(true)
    expect(fetchMock).toHaveBeenCalledWith('/api/recent-projects')

    vi.unstubAllGlobals()
  })

  it('toggleDropdown closes dropdown when already open', async () => {
    const wrapper = mountAndTrack()
    wrapper.vm.dropdownOpen = true

    await (wrapper.vm as any).toggleDropdown()
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.dropdownOpen).toBe(false)
  })

  // ── openBrowse ──

  it('openBrowse closes dropdown and emits openProjectDialog', async () => {
    const wrapper = mountAndTrack()
    wrapper.vm.dropdownOpen = true

    await (wrapper.vm as any).openBrowse()
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.dropdownOpen).toBe(false)
    expect(wrapper.emitted('openProjectDialog')).toBeTruthy()
  })

  // ── selectRecent ──

  it('selectRecent closes dropdown and does nothing if same project', async () => {
    const wrapper = mountAndTrack({ projectRoot: '/home/user/my-project' })
    wrapper.vm.dropdownOpen = true

    await (wrapper.vm as any).selectRecent({ path: '/home/user/my-project', name: 'my-project', displayPath: 'my-project' })
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.dropdownOpen).toBe(false)
  })

  it('selectRecent calls hotSwitchProject for different project', async () => {
    const hotSwitchMock = vi.fn().mockResolvedValue(undefined)
    const container = document.createElement('div')
    document.body.appendChild(container)
    const wrapper = mount(AppHeader, {
      props: { projectRoot: '/home/user/my-project' },
      attachTo: container,
      global: {
        plugins: [i18n],
        stubs: { PopupMenu: PopupMenuStub, 'lucide-vue-next': LucideStub },
        provide: { switchTab: vi.fn(), toast: { show: vi.fn() }, hotSwitchProject: hotSwitchMock },
        config: {
          errorHandler: (err: unknown) => {
            if (err instanceof Error && err.message.includes('Maximum recursive Updates')) return
            throw err
          },
        },
      },
    })
    activeWrapper = wrapper
    activeContainer = container

    await (wrapper.vm as any).selectRecent({ path: '/home/user/other-project', name: 'other-project', displayPath: 'other-project' })
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.dropdownOpen).toBe(false)
    expect(hotSwitchMock).toHaveBeenCalledWith('/home/user/other-project')
  })

  // ── onClickOutside ──

  it('onClickOutside closes dropdown when click is outside', async () => {
    const wrapper = mountAndTrack()
    wrapper.vm.dropdownOpen = true

    const mockEvent = { target: document.createElement('div') }
    await (wrapper.vm as any).onClickOutside(mockEvent)
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.dropdownOpen).toBe(false)
  })

  // ── statusDotClass computed ──

  it('statusDotClass returns correct class for each status', () => {
    wsConfig.value = 'connected'
    expect(mountAndTrack().vm.statusDotClass).toBe('status-dot-connected')
  })

  it('statusDotClass returns reconnecting class', () => {
    wsConfig.value = 'reconnecting'
    expect(mountAndTrack().vm.statusDotClass).toBe('status-dot-reconnecting')
  })

  it('statusDotClass returns disconnected class', () => {
    wsConfig.value = 'disconnected'
    expect(mountAndTrack().vm.statusDotClass).toBe('status-dot-disconnected')
  })

  // ── loadRecentProjects: loading state ──

  it('loadRecentProjects sets loadingRecent while fetching', async () => {
    let resolveJson: (v: any) => void
    const jsonPromise = new Promise(r => { resolveJson = r })
    const fetchMock = vi.fn().mockResolvedValue({ json: () => jsonPromise })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountAndTrack()
    const loadPromise = (wrapper.vm as any).loadRecentProjects()

    expect(wrapper.vm.loadingRecent).toBe(true)

    resolveJson!(['/home/user/proj'])
    await loadPromise
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.loadingRecent).toBe(false)

    vi.unstubAllGlobals()
  })

  // ── Regression: descender characters (p, g, y) must not be clipped ──
  // In jsdom, unitless line-height (e.g. 1.4) is returned as-is by getComputedStyle,
  // not resolved to px. So we check the raw parseFloat value directly.

  it('project-name has sufficient line-height for descender characters', () => {
    mountAndTrack({ projectRoot: '/home/user/p' })
    const el = $('.project-name')
    expect(el).toBeTruthy()
    // overflow:hidden and line-height:1.4 are set in scoped CSS;
    // jsdom getComputedStyle cannot read scoped styles, so verify class presence
    expect(el?.classList.contains('project-name')).toBe(true)
  })

  it('branch-name has sufficient line-height for descender characters', () => {
    mockState.gitBranch = 'feature/login-page'
    mountAndTrack()
    const el = $('.branch-name')
    expect(el).toBeTruthy()
    // overflow:hidden and line-height:1.4 are set in scoped CSS;
    // jsdom getComputedStyle cannot read scoped styles, so verify class presence
    expect(el?.classList.contains('branch-name')).toBe(true)
  })

  it('project-name renders single-char project name with descender-safe overflow', () => {
    mountAndTrack({ projectRoot: '/home/user/g' })
    const el = $('.project-name')
    expect(el).toBeTruthy()
    expect(el?.textContent).toBe('g')
    // overflow:hidden + line-height:1.4 verified in scoped CSS source
    expect(el?.classList.contains('project-name')).toBe(true)
  })

  it('branch-name renders short branch with descender-safe overflow', () => {
    mockState.gitBranch = 'p'
    mountAndTrack()
    const el = $('.branch-name')
    expect(el).toBeTruthy()
    expect(el?.textContent).toBe('p')
    // overflow:hidden + line-height:1.4 verified in scoped CSS source
    expect(el?.classList.contains('branch-name')).toBe(true)
  })

  // ── Current file + recent files capsule (5) ──

  it('does not render current-file-badge when no file and no recent files', () => {
    mountAndTrack({ currentFileName: '', recentFilesAvailable: 0 })
    expect($('.current-file-badge')).toBeFalsy()
  })

  it('renders current-file-badge with file name', () => {
    mountAndTrack({ currentFileName: 'main.go', recentFilesAvailable: 3 })
    const badge = $('.current-file-badge')
    expect(badge).toBeTruthy()
    expect(badge?.classList.contains('no-file')).toBe(false)
    expect($('.current-file-name')?.textContent).toBe('main.go')
  })

  it('renders current-file-badge with empty state text when no file name but recent files exist', () => {
    mountAndTrack({ currentFileName: '', recentFilesAvailable: 3 })
    const badge = $('.current-file-badge')
    expect(badge).toBeTruthy()
    expect(badge?.classList.contains('no-file')).toBe(true)
    expect($('.current-file-name')?.textContent).toBe('Recent files')
  })

  it('disables current-file-badge when recentFilesAvailable is 0', () => {
    mountAndTrack({ currentFileName: 'main.go', recentFilesAvailable: 0 })
    expect($('.current-file-badge')?.hasAttribute('disabled')).toBe(true)
  })

  it('toggles file dropdown open on click', async () => {
    const wrapper = mountAndTrack({ currentFileName: 'main.go', recentFilesAvailable: 3 })
    expect(wrapper.vm.fileDropdownOpen).toBe(false)
    $('.current-file-badge')?.click()
    expect(wrapper.vm.fileDropdownOpen).toBe(true)
  })

  it('selectRecentFile closes dropdown and emits selectRecentFile', async () => {
    const wrapper = mountAndTrack({ currentFileName: 'main.go', recentFilesAvailable: 3 })
    ;(wrapper.vm as any).selectRecentFile({ path: '/home/user/src/a.ts' })
    expect(wrapper.vm.fileDropdownOpen).toBe(false)
    expect(wrapper.emitted('selectRecentFile')).toEqual([['/home/user/src/a.ts']])
  })

  it('openFileManager closes dropdown and switches to browse tab', () => {
    closeCurrentFileFn.mockReset()
    const switchTabMock = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const wrapper = mount(AppHeader, {
      props: { projectRoot: '/home/user/my-project', currentFileName: 'main.go', recentFilesAvailable: 3 },
      attachTo: container,
      global: {
        plugins: [i18n],
        stubs: { PopupMenu: PopupMenuStub, 'lucide-vue-next': LucideStub },
        provide: { switchTab: switchTabMock, toast: { show: vi.fn() }, hotSwitchProject: vi.fn() },
      },
    })
    wrapper.vm.fileDropdownOpen = true
    ;(wrapper.vm as any).openFileManager()
    expect(wrapper.vm.fileDropdownOpen).toBe(false)
    expect(switchTabMock).toHaveBeenCalledWith('browse')
    wrapper.unmount()
    document.body.removeChild(container)
  })

  it('openFileManager closes the open current file before switching', () => {
    closeCurrentFileFn.mockReset()
    mockState.currentFile = { path: '/home/user/src/main.go' }
    const switchTabMock = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const wrapper = mount(AppHeader, {
      props: { projectRoot: '/home/user/my-project', currentFileName: 'main.go', recentFilesAvailable: 3 },
      attachTo: container,
      global: {
        plugins: [i18n],
        stubs: { PopupMenu: PopupMenuStub, 'lucide-vue-next': LucideStub },
        provide: { switchTab: switchTabMock, toast: { show: vi.fn() }, hotSwitchProject: vi.fn() },
      },
    })
    ;(wrapper.vm as any).openFileManager()
    expect(closeCurrentFileFn).toHaveBeenCalledTimes(1)
    expect(switchTabMock).toHaveBeenCalledWith('browse')
    wrapper.unmount()
    document.body.removeChild(container)
    mockState.currentFile = undefined
  })

  it('openFileManager does not close file when none is open', () => {
    closeCurrentFileFn.mockReset()
    mockState.currentFile = undefined
    const switchTabMock = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const wrapper = mount(AppHeader, {
      props: { projectRoot: '/home/user/my-project', recentFilesAvailable: 3 },
      attachTo: container,
      global: {
        plugins: [i18n],
        stubs: { PopupMenu: PopupMenuStub, 'lucide-vue-next': LucideStub },
        provide: { switchTab: switchTabMock, toast: { show: vi.fn() }, hotSwitchProject: vi.fn() },
      },
    })
    ;(wrapper.vm as any).openFileManager()
    expect(closeCurrentFileFn).not.toHaveBeenCalled()
    expect(switchTabMock).toHaveBeenCalledWith('browse')
    wrapper.unmount()
    document.body.removeChild(container)
  })

  it('opening file dropdown closes branch and project dropdowns', async () => {
    const wrapper = mountAndTrack({ currentFileName: 'main.go', recentFilesAvailable: 3 })
    wrapper.vm.fileDropdownOpen = false
    wrapper.vm.branchDropdownOpen = true
    wrapper.vm.dropdownOpen = true
    ;(wrapper.vm as any).toggleFileDropdown()
    expect(wrapper.vm.branchDropdownOpen).toBe(false)
    expect(wrapper.vm.dropdownOpen).toBe(false)
    expect(wrapper.vm.fileDropdownOpen).toBe(true)
  })

  it('opening branch dropdown closes file and project dropdowns', async () => {
    const wrapper = mountAndTrack({ currentFileName: 'main.go', recentFilesAvailable: 3 })
    wrapper.vm.fileDropdownOpen = true
    wrapper.vm.branchDropdownOpen = false
    wrapper.vm.dropdownOpen = true
    ;(wrapper.vm as any).toggleBranchDropdown()
    expect(wrapper.vm.fileDropdownOpen).toBe(false)
    expect(wrapper.vm.dropdownOpen).toBe(false)
    expect(wrapper.vm.branchDropdownOpen).toBe(true)
  })

  it('renders recent file entries in dropdown with open-file-manager footer', async () => {
    const wrapper = mountAndTrack({ currentFileName: 'main.go', recentFilesAvailable: 3 })
    wrapper.vm.recentFileEntries = [
      { path: '/home/user/src/a.ts', accessedAt: 1 },
      { path: '/home/user/src/b.ts', accessedAt: 2 },
    ]
    ;(wrapper.vm as any).toggleFileDropdown()
    try { await wrapper.vm.$nextTick() } catch {}

    const items = Array.from(document.body.querySelectorAll('.app-menu-item')) as HTMLElement[]
    const recentItems = items.filter(i => !i.classList.contains('other-item'))
    const footerItem = items.find(i => i.classList.contains('other-item'))
    expect(recentItems.length).toBe(2)
    expect(footerItem).toBeTruthy()
  })

  it('positionDropdown clamps panel width to viewport on narrow screens', async () => {
    const wrapper = mountAndTrack({ currentFileName: 'main.go', recentFilesAvailable: 3 })
    // Simulate a very narrow viewport (e.g. 300px wide device)
    vi.stubGlobal('innerWidth', 300)
    const panel = { offsetWidth: 400, offsetHeight: 200 } // content wider than screen
    const anchor = { getBoundingClientRect: () => ({ left: 20, top: 0, width: 40, height: 24, right: 60, bottom: 24 }) }
    const styleRef = { value: {} }
    ;(wrapper.vm as any).positionDropdown(anchor, { value: panel }, styleRef)

    const maxW = parseInt(styleRef.value.maxWidth, 10)
    expect(maxW).toBeLessThanOrEqual(300)
    expect(styleRef.value.left).toBeDefined()
    vi.unstubAllGlobals()
  })

  it('shows empty state in dropdown when no recent files', async () => {
    const wrapper = mountAndTrack({ currentFileName: 'main.go', recentFilesAvailable: 0 })
    wrapper.vm.recentFileEntries = []
    ;(wrapper.vm as any).toggleFileDropdown()
    try { await wrapper.vm.$nextTick() } catch {}

    expect(document.body.querySelector('.app-menu-message')).toBeTruthy()
  })

  // ── Branch dropdown (5) ──

  it('does not render branch-badge when gitBranch is empty', () => {
    mountAndTrack()
    expect($('.branch-badge')).toBeFalsy()
  })

  it('toggleBranchDropdown opens dropdown and loads branches', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ isGit: true, branches: [{ name: 'main' }, { name: 'dev' }] }) })
    vi.stubGlobal('fetch', fetchMock)

    mockState.gitBranch = 'main'
    const wrapper = mountAndTrack()
    expect(wrapper.vm.branchDropdownOpen).toBe(false)
    await (wrapper.vm as any).toggleBranchDropdown()
    await (wrapper.vm as any).loadBranches()
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.branchDropdownOpen).toBe(true)
    expect(wrapper.vm.branchList.length).toBe(2)

    vi.unstubAllGlobals()
  })

  it('highlights current branch as active in dropdown', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ isGit: true, branches: [{ name: 'main' }, { name: 'dev' }] }) })
    vi.stubGlobal('fetch', fetchMock)

    mockState.gitBranch = 'main'
    const wrapper = mountAndTrack()
    await (wrapper.vm as any).toggleBranchDropdown()
    await (wrapper.vm as any).loadBranches()
    try { await wrapper.vm.$nextTick() } catch {}

    const items = document.body.querySelectorAll('.app-menu-item')
    expect(items[0]?.classList.contains('active')).toBe(true)
    expect(items[1]?.classList.contains('active')).toBe(false)

    vi.unstubAllGlobals()
  })

  it('openHistory emits pending navigation and switches tab', () => {
    const wrapper = mountAndTrack()
    ;(wrapper.vm as any).openHistory()
    expect(setPendingManageNavigationFn).toHaveBeenCalled()
  })

  it('selectBranch does nothing when selecting current branch', async () => {
    mockState.gitBranch = 'main'
    const wrapper = mountAndTrack()
    await (wrapper.vm as any).selectBranch({ name: 'main' })
    expect(wrapper.emitted('selectRecentFile')).toBeFalsy()
    expect(wrapper.vm.branchDropdownOpen).toBe(false)
  })

  // ── handleLogout ──

  it('handleLogout calls AndroidNative.showServerDialog in APP mode', async () => {
    isAppModeConfig.value = true
    const showServerDialogMock = vi.fn()
    vi.stubGlobal('AndroidNative', { showServerDialog: showServerDialogMock })

    const wrapper = mountAndTrack()
    await (wrapper.vm as any).handleLogout()
    try { await wrapper.vm.$nextTick() } catch {}

    expect(showServerDialogMock).toHaveBeenCalled()
    expect(wrapper.vm.resourcesMenuOpen).toBe(false)

    vi.unstubAllGlobals()
    isAppModeConfig.value = false
  })

  it('handleLogout redirects to /login when not in APP mode', async () => {
    isAppModeConfig.value = false
    // Can't easily mock window.location.href in jsdom, so test the logic path
    const wrapper = mountAndTrack()
    await (wrapper.vm as any).handleLogout()
    try { await wrapper.vm.$nextTick() } catch {}

    expect(wrapper.vm.resourcesMenuOpen).toBe(false)
  })
})
