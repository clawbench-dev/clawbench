import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, computed } from 'vue'
import { useNavigationContext } from '../useNavigationContext'
import { useFileNavStack, _resetForTesting as resetFileNavStack } from '../useFileNavStack'
import { useDirectoryReturn, _resetForTesting as resetDirectoryReturn } from '../useDirectoryReturn'
import { useNavigationCoordinator } from '../useNavigationCoordinator'
import { PANE_LEFT, PANE_RIGHT, type ActivePane } from '../useWideScreenLayout'

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
})
