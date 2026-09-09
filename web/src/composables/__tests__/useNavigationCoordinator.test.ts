import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, computed } from 'vue'
import { useNavigationContext } from '../useNavigationContext'
import { useFileNavStack, _resetForTesting as resetFileNavStack } from '../useFileNavStack'
import { useDirectoryReturn, _resetForTesting as resetDirectoryReturn } from '../useDirectoryReturn'
import { useNavigationCoordinator } from '../useNavigationCoordinator'
import { PANE_LEFT, PANE_RIGHT, type ActivePane } from '../useWideScreenLayout'
import { setFileScroll } from '@/utils/fileScrollCache'

describe('useNavigationCoordinator', () => {
  const navigation = useNavigationContext()
  const fileNav = useFileNavStack()
  const browseFileSession = ref(false)
  const directoryReturn = useDirectoryReturn(browseFileSession)

  const isWideScreen = ref(false)
  const activePane = ref<ActivePane>(PANE_LEFT)
  const activeTab = ref('chat')
  const leftPanelActive = computed(() => (isWideScreen.value ? 'view' : activeTab.value))
  const markdownViewMode = ref('rendered')

  const fakeStore = {
    state: {
      currentFile: null as { name: string; path: string } | null,
      currentDir: '',
      dirLoading: false,
    },
    selectFile: vi.fn().mockResolvedValue(true),
    navigateToDir: vi.fn().mockResolvedValue(true),
    navigateToParentDir: vi.fn().mockResolvedValue(true),
  }

  const mockViewActions = {
    scrollToLine: vi.fn(),
    closeOverlayAndSync: vi.fn(),
    handleOpenFileManager: vi.fn(),
    isFileManagerMultiSelectActive: vi.fn().mockReturnValue(false),
    requestScrollCapture: vi.fn(),
  }

  const mockBackHooks = {
    hasTopmostOverlay: vi.fn().mockReturnValue(false),
    closeTopmostOverlay: vi.fn().mockReturnValue(false),
    canNavigateBack: vi.fn().mockReturnValue(false),
    handleBackNavigation: vi.fn().mockReturnValue(false),
  }

  const mockToast = {
    show: vi.fn(),
  }

  function createCoordinator() {
    return useNavigationCoordinator({
      store: fakeStore as any,
      fileNav,
      navigation,
      directoryReturn,
      browseFileSession,
      markdownViewMode,
      layout: {
        isWideScreen,
        activePane,
        activeTab,
        leftPanelActive,
        panelIsActive: (tabId: string) => (isWideScreen.value ? tabId === 'view' : activeTab.value === tabId),
        switchTab: (tab: string) => {
          activeTab.value = tab
        },
        setActivePane: (pane: ActivePane) => {
          activePane.value = pane
        },
        setLeftCollapsed: vi.fn(),
      },
      fileEditor: {
        isEditing: vi.fn().mockReturnValue(false),
        exitEdit: vi.fn(),
      },
      viewActions: mockViewActions,
      backHooks: mockBackHooks,
      toast: mockToast,
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockBackHooks.hasTopmostOverlay.mockReturnValue(false)
    mockBackHooks.closeTopmostOverlay.mockReturnValue(false)
    mockBackHooks.canNavigateBack.mockReturnValue(false)
    mockBackHooks.handleBackNavigation.mockReturnValue(false)
    mockViewActions.isFileManagerMultiSelectActive.mockReturnValue(false)
    fakeStore.selectFile.mockResolvedValue(true)
    fakeStore.navigateToDir.mockResolvedValue(true)
    fakeStore.navigateToParentDir.mockResolvedValue(true)
    navigation.resetForTesting()
    resetFileNavStack()
    resetDirectoryReturn()
    browseFileSession.value = false
    isWideScreen.value = false
    activePane.value = PANE_LEFT
    activeTab.value = 'chat'
    markdownViewMode.value = 'rendered'
    fakeStore.state.currentFile = null
    fakeStore.state.currentDir = ''
    fakeStore.state.dirLoading = false
  })

  describe('handleOpenDirectoryFromContext', () => {
    it('switches to browse tab and loads directory on successful jump', async () => {
      const coord = createCoordinator()

      await coord.handleOpenDirectoryFromContext('src/components', 'chat')

      expect(mockViewActions.handleOpenFileManager).toHaveBeenCalled()
      expect(fakeStore.navigateToDir).toHaveBeenCalledWith('src/components')
      expect(activeTab.value).toBe('browse')
      expect(navigation.hasOrigin.value).toBe(true)
      expect(navigation.origin.value?.surface).toBe('chat')
    })

    it('suspends current file into directoryReturn when jumped from file view', async () => {
      fakeStore.state.currentFile = { name: 'App.vue', path: 'src/App.vue' }
      fakeStore.state.currentDir = 'src'
      activeTab.value = 'view'
      fileNav.openFile('src/App.vue', { viewMode: 'rendered' })

      const coord = createCoordinator()
      await coord.handleOpenDirectoryFromContext('src/components', 'file')

      expect(directoryReturn.pending()).toBe(true)
      expect(mockViewActions.handleOpenFileManager).toHaveBeenCalled()
      expect(fakeStore.navigateToDir).toHaveBeenCalledWith('src/components')
      expect(activeTab.value).toBe('browse')
    })

    it('triggers abandonJump and rolls back when directory load fails', async () => {
      fakeStore.navigateToDir.mockResolvedValueOnce(false)
      const coord = createCoordinator()

      await coord.handleOpenDirectoryFromContext('invalid/dir', 'chat')

      expect(navigation.hasOrigin.value).toBe(false)
      expect(activeTab.value).toBe('chat')
    })

    it('suppresses failure when dirLoading was true concurrently', async () => {
      fakeStore.state.dirLoading = true
      fakeStore.navigateToDir.mockResolvedValueOnce(false)
      const coord = createCoordinator()

      await coord.handleOpenDirectoryFromContext('concurrent/dir', 'chat')

      // Since dirLoading was true, losing the race is suppressed, so abandonJump is not called
      expect(navigation.hasOrigin.value).toBe(true)
    })
  })

  describe('handleOpenFileOverlay', () => {
    it('starts origin and opens file from chat on wide screen', () => {
      isWideScreen.value = true
      activePane.value = PANE_RIGHT
      const coord = createCoordinator()

      coord.handleOpenFileOverlay({
        detail: { path: 'src/main.ts', lineStart: 42, source: 'chat' },
      } as any)

      expect(navigation.hasOrigin.value).toBe(true)
      expect(navigation.origin.value?.surface).toBe('chat')
      expect(navigation.origin.value?.tab).toBe('chat')
      expect(fileNav.overlayOpen.value).toBe(true)
      expect(fileNav.currentFilePath.value).toBe('src/main.ts')
      expect(markdownViewMode.value).toBe('raw')
      expect(mockViewActions.scrollToLine).toHaveBeenCalledWith(42, undefined, 'src/main.ts')
    })

    it('preserves history stack when clicking code link within file view', () => {
      activeTab.value = 'view'
      fileNav.openFile('README.md', { viewMode: 'rendered' })
      const coord = createCoordinator()

      coord.handleOpenFileOverlay({
        detail: { path: 'src/index.ts', lineStart: 10, source: 'file' },
      } as any)

      expect(fileNav.canGoBack.value).toBe(true)
      expect(fileNav.currentFilePath.value).toBe('src/index.ts')
      expect(markdownViewMode.value).toBe('raw')
    })
  })

  describe('returnToOrigin', () => {
    it('returns to chat tab and closes overlay when origin is chat', async () => {
      navigation.start({ surface: 'chat', tab: 'chat', label: 'Back to Chat' })
      const coord = createCoordinator()

      const handled = await coord.returnToOrigin()

      expect(handled).toBe(true)
      expect(activeTab.value).toBe('chat')
      expect(mockViewActions.closeOverlayAndSync).toHaveBeenCalled()
      expect(navigation.hasOrigin.value).toBe(false)
    })

    it('restores suspended file visit and unwinds directory', async () => {
      // Simulate file visit suspended in directoryReturn
      fileNav.openFile('README.md', { viewMode: 'rendered' })
      directoryReturn.enter(
        { surface: 'file', tab: 'view', label: 'Back to README.md', filePath: 'README.md', viewMode: 'rendered' },
        'docs',
      )
      const coord = createCoordinator()

      const handled = await coord.returnToOrigin()

      expect(handled).toBe(true)
      expect(fakeStore.selectFile).toHaveBeenCalledWith('README.md')
      expect(fakeStore.navigateToDir).toHaveBeenCalledWith('docs')
    })

    it('restores suspended directory even when root directory is empty string', async () => {
      fileNav.openFile('README.md', { viewMode: 'rendered' })
      directoryReturn.enter(
        { surface: 'file', tab: 'view', label: 'Back to README.md', filePath: 'README.md', viewMode: 'rendered' },
        '',
      )
      const coord = createCoordinator()

      await coord.returnToOrigin()

      expect(fakeStore.navigateToDir).toHaveBeenCalledWith('')
    })

    it('handles file not found gracefully when returning to origin', async () => {
      fakeStore.selectFile.mockResolvedValueOnce(false)
      navigation.start({ surface: 'file', tab: 'view', label: 'Back to Deleted', filePath: 'deleted.md' })
      const coord = createCoordinator()

      const handled = await coord.returnToOrigin()

      expect(handled).toBe(true)
      expect(mockToast.show).toHaveBeenCalled()
      expect(navigation.hasOrigin.value).toBe(false)
    })

    it('keeps the origin line when the banked position is 0', async () => {
      // Opened from a line link, jumped away before scroll-to-line landed.
      navigation.start({
        surface: 'file',
        tab: 'view',
        label: 'Back to A.md',
        filePath: 'src/A.md',
        lineStart: 42,
        viewMode: 'raw',
        scrollTop: 0,
      })
      const coord = createCoordinator()
      const spy = vi.spyOn(window, 'dispatchEvent')
      await coord.returnToOrigin()
      const dispatched = spy.mock.calls.some(([e]) => (e as CustomEvent).type === 'restore-file-scroll')
      spy.mockRestore()

      expect(dispatched).toBe(false)
      expect(mockViewActions.scrollToLine).toHaveBeenCalledWith(42, undefined, 'src/A.md')
    })
  })

  describe('openFileInViewer', () => {
    it('sets markdownViewMode to raw when lineStart is given', () => {
      const coord = createCoordinator()
      coord.openFileInViewer('doc.md', { lineStart: 15, lineEnd: 20 })

      expect(markdownViewMode.value).toBe('raw')
      expect(mockViewActions.scrollToLine).toHaveBeenCalledWith(15, 20, 'doc.md')
    })

    it('sets markdownViewMode to rendered for markdown without lineStart', () => {
      markdownViewMode.value = 'raw'
      const coord = createCoordinator()
      coord.openFileInViewer('doc.md')

      expect(markdownViewMode.value).toBe('rendered')
    })
  })

  describe('handleSelectFile', () => {
    it('opens file from chat by default', async () => {
      const coord = createCoordinator()
      await coord.handleSelectFile('src/main.ts')

      expect(fakeStore.selectFile).toHaveBeenCalledWith('src/main.ts')
      expect(activeTab.value).toBe('view')
      expect(navigation.hasOrigin.value).toBe(true)
      expect(navigation.origin.value?.surface).toBe('chat')
    })

    it('opens file from history when history panel is active', async () => {
      activeTab.value = 'history'
      const coord = createCoordinator()
      await coord.handleSelectFile('src/main.ts')

      expect(navigation.origin.value?.surface).toBe('history')
    })

    it('does nothing when file selection fails', async () => {
      fakeStore.selectFile.mockResolvedValueOnce(false)
      const coord = createCoordinator()
      await coord.handleSelectFile('invalid.ts')

      expect(activeTab.value).toBe('chat')
      expect(navigation.hasOrigin.value).toBe(false)
    })
  })

  describe('handleBrowseSelectFile', () => {
    it('sets browse session and opens file in viewer', async () => {
      const coord = createCoordinator()
      await coord.handleBrowseSelectFile('src/index.ts')

      expect(browseFileSession.value).toBe(true)
      expect(fakeStore.selectFile).toHaveBeenCalledWith('src/index.ts')
      expect(activeTab.value).toBe('view')
    })

    it('bails when file manager multi-select is active', async () => {
      mockViewActions.isFileManagerMultiSelectActive.mockReturnValueOnce(true)
      const coord = createCoordinator()
      await coord.handleBrowseSelectFile('src/index.ts')

      expect(fakeStore.selectFile).not.toHaveBeenCalled()
    })

    it('resets browseFileSession when selectFile fails', async () => {
      fakeStore.selectFile.mockResolvedValueOnce(false)
      const coord = createCoordinator()
      await coord.handleBrowseSelectFile('nonexistent.ts')

      expect(browseFileSession.value).toBe(false)
      expect(activeTab.value).toBe('chat')
    })
  })

  describe('handleTaskOpenFile', () => {
    it('opens file with task origin and line number', async () => {
      const coord = createCoordinator()
      await coord.handleTaskOpenFile('task.log', 10)

      expect(fakeStore.selectFile).toHaveBeenCalledWith('task.log')
      expect(navigation.hasOrigin.value).toBe(true)
      expect(navigation.origin.value?.surface).toBe('task')
      expect(activeTab.value).toBe('view')
      expect(mockViewActions.scrollToLine).toHaveBeenCalledWith(10, undefined, 'task.log')
    })

    it('does nothing when selectFile fails', async () => {
      fakeStore.selectFile.mockResolvedValueOnce(false)
      const coord = createCoordinator()
      await coord.handleTaskOpenFile('task.log')

      expect(navigation.hasOrigin.value).toBe(false)
      expect(activeTab.value).toBe('chat')
    })
  })

  describe('handleOverlayClose', () => {
    it('forces close when custom event has force: true', async () => {
      browseFileSession.value = true
      const coord = createCoordinator()
      await coord.handleOverlayClose({ detail: { force: true } } as any)

      expect(mockViewActions.closeOverlayAndSync).toHaveBeenCalled()
      expect(browseFileSession.value).toBe(false)
    })

    it('delegates to navigateBack when force is not specified', async () => {
      activeTab.value = 'view'
      fileNav.openFile('test.ts')
      const coord = createCoordinator()
      await coord.handleOverlayClose()

      expect(mockViewActions.closeOverlayAndSync).toHaveBeenCalled()
      expect(activeTab.value).toBe('browse')
    })
  })

  describe('handleOverlayOpenFile', () => {
    it('fetches /api/dir and navigates to dir if path is a directory', async () => {
      const originalFetch = globalThis.fetch
      globalThis.fetch = vi.fn().mockResolvedValue({ ok: true })

      try {
        const coord = createCoordinator()
        await coord.handleOverlayOpenFile('src/components')

        expect(globalThis.fetch).toHaveBeenCalledWith('/api/dir?path=src%2Fcomponents')
        expect(fakeStore.navigateToDir).toHaveBeenCalledWith('src/components')
      } finally {
        globalThis.fetch = originalFetch
      }
    })

    it('opens file and handles external absolute path with toast', async () => {
      const coord = createCoordinator()
      await coord.handleOverlayOpenFile({ path: '/var/log/app.log', lineStart: 5, lineEnd: 10 })

      expect(fakeStore.selectFile).toHaveBeenCalledWith('/var/log/app.log')
      expect(mockViewActions.scrollToLine).toHaveBeenCalledWith(5, 10, '/var/log/app.log')
      expect(mockToast.show).toHaveBeenCalled()
    })

    it('handles plain string file path payload when fetch fails', async () => {
      const originalFetch = globalThis.fetch
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Network error'))

      try {
        const coord = createCoordinator()
        await coord.handleOverlayOpenFile('src/utils.ts')

        expect(fakeStore.selectFile).toHaveBeenCalledWith('src/utils.ts')
      } finally {
        globalThis.fetch = originalFetch
      }
    })
  })

  describe('handleAppHeaderRecentFileSelect', () => {
    it('switches file within current file view', async () => {
      activeTab.value = 'view'
      fileNav.overlayOpen.value = true
      fakeStore.state.currentFile = { path: 'old.ts', name: 'old.ts' }

      const coord = createCoordinator()
      await coord.handleAppHeaderRecentFileSelect('new.ts')

      expect(fakeStore.selectFile).toHaveBeenCalledWith('new.ts')
      expect(activeTab.value).toBe('view')
    })

    it('sets browseFileSession when opened from browse panel', async () => {
      activeTab.value = 'browse'
      const coord = createCoordinator()
      await coord.handleAppHeaderRecentFileSelect('recent.ts')

      expect(browseFileSession.value).toBe(true)
      expect(navigation.hasOrigin.value).toBe(false)
      expect(activeTab.value).toBe('view')
    })

    it('sets task origin when opened from tasks panel', async () => {
      activeTab.value = 'tasks'
      const coord = createCoordinator()
      await coord.handleAppHeaderRecentFileSelect('recent.ts')

      expect(navigation.hasOrigin.value).toBe(true)
      expect(navigation.origin.value?.surface).toBe('task')
      expect(activeTab.value).toBe('view')
    })

    it('sets history origin when opened from history panel', async () => {
      activeTab.value = 'history'
      const coord = createCoordinator()
      await coord.handleAppHeaderRecentFileSelect('recent.ts')

      expect(navigation.hasOrigin.value).toBe(true)
      expect(navigation.origin.value?.surface).toBe('history')
    })

    it('sets chat origin when opened from chat', async () => {
      activeTab.value = 'chat'
      const coord = createCoordinator()
      await coord.handleAppHeaderRecentFileSelect('recent.ts')

      expect(navigation.hasOrigin.value).toBe(true)
      expect(navigation.origin.value?.surface).toBe('chat')
    })
  })

  describe('handleOpenDirectoryFromEvent and handleOpenFileOverlay extra paths', () => {
    it('delegates to handleOpenDirectoryFromContext when event has valid path', async () => {
      const coord = createCoordinator()
      coord.handleOpenDirectoryFromEvent({ detail: { path: 'src/docs', source: 'chat' } } as any)

      expect(mockViewActions.handleOpenFileManager).toHaveBeenCalled()
      expect(fakeStore.navigateToDir).toHaveBeenCalledWith('src/docs')
    })

    it('ignores handleOpenDirectoryFromEvent when path is undefined', () => {
      const coord = createCoordinator()
      coord.handleOpenDirectoryFromEvent({ detail: {} } as any)

      expect(fakeStore.navigateToDir).not.toHaveBeenCalled()
    })

    it('handles open file overlay with task and history sources', () => {
      activeTab.value = 'tasks'
      const coord = createCoordinator()
      coord.handleOpenFileOverlay({ detail: { path: 'task.md', source: 'task' } } as any)

      expect(navigation.origin.value?.surface).toBe('task')

      activeTab.value = 'history'
      coord.handleOpenFileOverlay({ detail: { path: 'commit.diff', source: 'history' } } as any)
      expect(navigation.origin.value?.surface).toBe('history')
    })

    it('handles open file overlay when called with direct payload object', () => {
      const coord = createCoordinator()
      coord.handleOpenFileOverlay({ path: 'direct.ts' })

      expect(activeTab.value).toBe('view')
      expect(fileNav.currentFilePath.value).toBe('direct.ts')
    })

    it('ignores handleOpenFileOverlay without path', () => {
      const coord = createCoordinator()
      coord.handleOpenFileOverlay({} as any)

      expect(activeTab.value).toBe('chat')
    })
  })

  describe('back navigation state machine & viewBackLabel', () => {
    it('handles exitEdit when fileEditor isEditing is true', async () => {
      const coord = createCoordinator()
      const mockFileEditor = {
        isEditing: vi.fn().mockReturnValue(true),
        exitEdit: vi.fn(),
      }
      const coordWithEditor = useNavigationCoordinator({
        store: fakeStore as any,
        fileNav,
        navigation,
        browseFileSession,
        markdownViewMode,
        layout: {
          isWideScreen,
          activePane,
          activeTab,
          leftPanelActive,
          panelIsActive: () => true,
          switchTab: vi.fn(),
          setActivePane: vi.fn(),
          setLeftCollapsed: vi.fn(),
        },
        fileEditor: mockFileEditor,
        viewActions: mockViewActions,
        backHooks: mockBackHooks,
        toast: mockToast,
      })

      expect(coordWithEditor.canHandleBack('header')).toBe(true)
      await coordWithEditor.handleNavigateBack()
      expect(mockFileEditor.exitEdit).toHaveBeenCalled()
    })

    it('handles topmost overlay before other back actions', async () => {
      mockBackHooks.hasTopmostOverlay.mockReturnValue(true)
      mockBackHooks.closeTopmostOverlay.mockReturnValue(true)
      const coord = createCoordinator()

      expect(coord.canHandleBack('header')).toBe(true)
      await coord.handleNavigateBack()
      expect(mockBackHooks.closeTopmostOverlay).toHaveBeenCalled()
    })

    it('navigates back through file navigation stack', async () => {
      activeTab.value = 'view'
      fileNav.openFile('first.ts')
      fileNav.openFile('second.ts', { lineStart: 10, lineEnd: 20, viewMode: 'raw' })

      const coord = createCoordinator()
      expect(coord.canNavigateBackInView.value).toBe(true)
      expect(coord.viewBackLabel.value).toContain('first.ts')

      await coord.handleNavigateBack()
      expect(fakeStore.selectFile).toHaveBeenCalledWith('first.ts')
    })

    it('shows toast when goBackFile fails to select previous file', async () => {
      activeTab.value = 'view'
      fileNav.openFile('first.ts')
      fileNav.openFile('second.ts')
      fakeStore.selectFile.mockResolvedValueOnce(false)

      const coord = createCoordinator()
      await coord.handleNavigateBack()
      expect(mockToast.show).toHaveBeenCalled()
    })

    it('navigates to parent directory when back in browse panel', async () => {
      activeTab.value = 'browse'
      fakeStore.state.currentDir = 'sub/dir'

      const coord = createCoordinator()
      expect(coord.canHandleBack('header')).toBe(true)
      await coord.handleNavigateBack()
      expect(fakeStore.navigateToParentDir).toHaveBeenCalled()
    })

    it('closes overlay and returns to browse when browse session was active', async () => {
      activeTab.value = 'view'
      browseFileSession.value = true
      fileNav.openFile('viewed.ts')

      const coord = createCoordinator()
      expect(coord.viewBackLabel.value).toBe('Back')
      await coord.handleNavigateBack()

      expect(mockViewActions.closeOverlayAndSync).toHaveBeenCalled()
      expect(browseFileSession.value).toBe(false)
      expect(activeTab.value).toBe('browse')
    })

    it('delegates to backHooks when no internal handlers match', async () => {
      mockBackHooks.canNavigateBack.mockReturnValue(true)
      mockBackHooks.handleBackNavigation.mockReturnValue(true)

      const coord = createCoordinator()
      expect(coord.canHandleBack('header')).toBe(true)
      await coord.handleNavigateBack()
      expect(mockBackHooks.handleBackNavigation).toHaveBeenCalled()
    })
  })

  describe('scroll capture on jump', () => {
    it('snapshots the live viewer before pushing a linked file', async () => {
      fakeStore.state.currentFile = { name: 'A.md', path: 'src/A.md' }
      activeTab.value = 'view'
      fileNav.openFile('src/A.md', { viewMode: 'rendered' })

      const coord = createCoordinator()
      await coord.handleOpenFileOverlay({ path: 'src/B.md', source: 'file' })

      expect(mockViewActions.requestScrollCapture).toHaveBeenCalled()
      expect(fileNav.previousLocation.value?.path).toBe('src/A.md')
    })

    it('banks the position the live capture reported, not a stale cache read', async () => {
      fakeStore.state.currentFile = { name: 'A.md', path: 'src/A.md' }
      activeTab.value = 'view'
      // Cache holds an old offset from an earlier moment in the file.
      fileNav.openFile('src/A.md', { viewMode: 'rendered', scrollTop: 40 })
      setFileScroll('src/A.md', 40)

      const coord = createCoordinator()
      // Mirrors the real wiring: FileViewer captures the live pane, FileOverlay
      // re-emits it, App forwards it back into the coordinator.
      mockViewActions.requestScrollCapture.mockImplementation(() => {
        coord.handleCaptureFileScroll({
          scrollTop: 2400,
          anchor: { id: 'sec-two', line: 12, relTop: -30 },
          blockAnchor: null,
          ratio: null,
        })
      })

      await coord.handleOpenFileOverlay({ path: 'src/B.md', source: 'file' })

      expect(fileNav.previousLocation.value?.scrollTop).toBe(2400)
      expect(fileNav.previousLocation.value?.scrollEntry?.scrollTop).toBe(2400)
      expect(fileNav.previousLocation.value?.scrollEntry?.anchor).toEqual({ id: 'sec-two', line: 12, relTop: -30 })
    })
  })

  describe('return restore strategy', () => {
    function restoreDetailFrom(spy: ReturnType<typeof vi.spyOn>) {
      const call = spy.mock.calls.find(([e]) => (e as CustomEvent).type === 'restore-file-scroll')
      return (call?.[0] as CustomEvent | undefined)?.detail
    }

    it('leads with the pixel offset when the view mode is unchanged', async () => {
      fakeStore.state.currentFile = { name: 'B.md', path: 'src/B.md' }
      activeTab.value = 'view'
      fileNav.openFile('src/A.md', { viewMode: 'rendered', scrollTop: 1200 })
      fileNav.openFile('src/B.md', { viewMode: 'rendered' })
      markdownViewMode.value = 'rendered'

      const coord = createCoordinator()
      const spy = vi.spyOn(window, 'dispatchEvent')
      const ok = await coord.goBackFile()
      const detail = restoreDetailFrom(spy)
      spy.mockRestore()

      expect(ok).toBe(true)
      expect(detail).toEqual(expect.objectContaining({ scrollTop: 1200, preferPx: true }))
    })

    it('leads with the pixel offset when returning from a raw file back to a rendered file', async () => {
      fakeStore.state.currentFile = { name: 'B.md', path: 'src/B.md' }
      activeTab.value = 'view'
      fileNav.openFile('src/A.md', { viewMode: 'rendered', scrollTop: 1200 })
      fileNav.openFile('src/B.md', { viewMode: 'raw' })
      markdownViewMode.value = 'raw'

      const coord = createCoordinator()
      const spy = vi.spyOn(window, 'dispatchEvent')
      await coord.goBackFile()
      const detail = restoreDetailFrom(spy)
      spy.mockRestore()

      // A is restored in rendered mode, so pixels match and preferPx is true.
      expect(markdownViewMode.value).toBe('rendered')
      expect(detail).toEqual(expect.objectContaining({ scrollTop: 1200, preferPx: true }))
    })

    it('sets markdownViewMode before store.selectFile on return so no spurious mode switch occurs', async () => {
      fakeStore.state.currentFile = { name: 'target.go', path: 'src/target.go' }
      activeTab.value = 'view'
      fileNav.openFile('README.md', { viewMode: 'rendered', scrollTop: 1800 })
      fileNav.openFile('src/target.go', { lineStart: 50, lineEnd: 60, viewMode: 'raw' })
      markdownViewMode.value = 'raw'

      let viewModeDuringSelectFile: string | undefined
      fakeStore.selectFile = vi.fn(async () => {
        viewModeDuringSelectFile = markdownViewMode.value
        return true
      })

      const coord = createCoordinator()
      const spy = vi.spyOn(window, 'dispatchEvent')
      const ok = await coord.goBackFile()
      const detail = restoreDetailFrom(spy)
      spy.mockRestore()

      expect(ok).toBe(true)
      expect(viewModeDuringSelectFile).toBe('rendered')
      expect(markdownViewMode.value).toBe('rendered')
      expect(detail).toEqual(expect.objectContaining({ scrollTop: 1800, preferPx: true }))
    })

    it('does not request live scroll capture or overwrite currentLocation when pathOverride is not currentFile', async () => {
      fakeStore.state.currentFile = { name: 'B.go', path: 'src/B.go' }
      activeTab.value = 'view'
      fileNav.openFile('src/A.md', { viewMode: 'rendered', scrollTop: 1500 })
      setFileScroll('src/A.md', 1500)

      mockViewActions.requestScrollCapture.mockClear()
      const coord = createCoordinator()

      // In openFilePath, store.selectFile('src/B.go') runs before open-file-overlay.
      // So currentFile is already B.go, but handleOpenFileOverlay is called with prevPath = A.md.
      await coord.handleOpenFileOverlay({ path: 'src/B.go', source: 'file' })

      // Live capture on B.go DOM must NOT be requested for A.md
      expect(mockViewActions.requestScrollCapture).not.toHaveBeenCalled()
      // A.md's saved position must NOT have been corrupted
      expect(fileNav.previousLocation.value?.scrollTop).toBe(1500)
    })

    it('restores the reading position instead of the original line for a line-linked visit', async () => {
      fakeStore.state.currentFile = { name: 'B.md', path: 'src/B.md' }
      activeTab.value = 'view'
      // A was opened via a line-numbered link (line 42), then the user read on.
      fileNav.openFile('src/A.md', { lineStart: 42, viewMode: 'raw', scrollTop: 2400 })
      fileNav.openFile('src/B.md', { viewMode: 'rendered' })
      markdownViewMode.value = 'rendered'

      const coord = createCoordinator()
      const spy = vi.spyOn(window, 'dispatchEvent')
      await coord.goBackFile()
      const detail = restoreDetailFrom(spy)
      spy.mockRestore()

      expect(detail).toEqual(expect.objectContaining({ scrollTop: 2400 }))
      expect(mockViewActions.scrollToLine).not.toHaveBeenCalled()
    })

    it('falls back to scrollToLine when no position was ever recorded', async () => {
      fakeStore.state.currentFile = { name: 'B.md', path: 'src/B.md' }
      activeTab.value = 'view'
      fileNav.openFile('src/A.md', { lineStart: 42, viewMode: 'raw' })
      fileNav.openFile('src/B.md', { viewMode: 'rendered' })
      markdownViewMode.value = 'rendered'

      const coord = createCoordinator()
      await coord.goBackFile()

      expect(mockViewActions.scrollToLine).toHaveBeenCalledWith(42, undefined, 'src/A.md')
    })

    it('treats a recorded 0 as "not captured" and keeps the line', async () => {
      fakeStore.state.currentFile = { name: 'B.md', path: 'src/B.md' }
      activeTab.value = 'view'
      // A was opened via a line link; the async scroll-to-line had not landed
      // when the user jumped away, so the capture banked 0.
      fileNav.openFile('src/A.md', { lineStart: 42, viewMode: 'raw', scrollTop: 0 })
      fileNav.openFile('src/B.md', { viewMode: 'rendered' })
      markdownViewMode.value = 'rendered'

      const coord = createCoordinator()
      const spy = vi.spyOn(window, 'dispatchEvent')
      await coord.goBackFile()
      const detail = restoreDetailFrom(spy)
      spy.mockRestore()

      expect(detail).toBeUndefined()
      expect(mockViewActions.scrollToLine).toHaveBeenCalledWith(42, undefined, 'src/A.md')
    })
  })

  describe('helper methods', () => {
    it('manages file scroll and origin settlement', () => {
      const coord = createCoordinator()
      coord.handleCaptureFileScroll(150)

      navigation.start({ surface: 'chat', tab: 'chat', label: 'Chat' })
      expect(navigation.hasOrigin.value).toBe(true)
      coord.settleOriginForTab('chat')
      expect(navigation.hasOrigin.value).toBe(false)

      expect(coord.isApplyingOriginReturn()).toBe(false)
      coord.invalidateDirectoryRequests()
    })
  })
})
