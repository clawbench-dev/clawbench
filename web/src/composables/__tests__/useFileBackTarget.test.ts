import { beforeEach, expect, it, vi } from 'vitest'
import { ref, computed } from 'vue'
import { useFileBackTarget } from '../useFileBackTarget'
import { useFileNavStack, _resetForTesting } from '../useFileNavStack'
import { useNavigationContext } from '../useNavigationContext'
import { useNavigationStateMachine } from '../useNavigationStateMachine'
import { useNavigationCoordinator } from '../useNavigationCoordinator'
import type { ActivePane } from '../useWideScreenLayout'

beforeEach(() => {
  _resetForTesting()
  useNavigationContext().resetForTesting()
})

it.each(['header', 'android', 'edge-swipe'] as const)('browse -> Markdown -> code -> %s returns to Markdown before browse', async reason => {
  const files = useFileNavStack()
  const browseSession = ref(true)
  const surface = ref('view')
  const target = useFileBackTarget(() => ({
    browseSession: browseSession.value,
    canGoBackFile: files.canGoBack.value,
    hasOrigin: true, // An older directory origin must not skip the visit's root.
  }))
  const returnToOrigin = vi.fn(async () => true)
  const back = useNavigationStateMachine({
    hasTopmostOverlay: () => false,
    closeTopmostOverlay: () => false,
    isEditing: () => false,
    exitEdit: () => {},
    canGoBackFile: () => surface.value === 'view' && files.overlayOpen.value && target.value === 'file',
    goBackFile: async () => files.goBack() !== null,
    hasOrigin: () => target.value !== 'browse',
    returnToOrigin,
    canGoBackDir: () => false,
    goBackDir: async () => false,
    canCloseOverlay: () => surface.value === 'view' && files.overlayOpen.value,
    closeOverlay: () => {
      files.closeOverlay()
      browseSession.value = false
      surface.value = 'browse'
      return true
    },
  })
  // An earlier unrelated visit is discarded at the browse entry boundary.
  files.openFile('old.md')
  files.closeOverlay()
  files.openFile('README.md', { scrollTop: 350, viewMode: 'rendered' })
  for (let visit = 0; visit < 2; visit++) {
    files.openFile('src/main.ts', { lineStart: 12 })
    expect(target.value).toBe('file')
    expect(await back.navigateBack(reason)).toBe(true)
    expect(files.currentLocation.value).toEqual({ path: 'README.md', scrollTop: 350, viewMode: 'rendered' })
    expect(surface.value).toBe('view')
    expect(files.overlayOpen.value).toBe(true)
    expect(target.value).toBe('browse')
  }
  expect(await back.navigateBack(reason)).toBe(true)
  expect(surface.value).toBe('browse')
  expect(files.overlayOpen.value).toBe(false)
  expect(returnToOrigin).not.toHaveBeenCalled()
})

it('wide screen jump: opening code file with source=file retains Markdown on stack and returns to Markdown before browse', async () => {
  const files = useFileNavStack()
  const navigation = useNavigationContext()
  const browseSession = ref(true)
  const isWideScreen = ref(true)
  const activePane = ref<ActivePane>('right')
  const activeTab = ref('chat')
  const surface = ref('view')
  const markdownViewMode = ref<'rendered' | 'raw'>('rendered')

  const fakeStore = {
    state: {
      currentFile: null as { name: string; path: string } | null,
      currentDir: '',
    },
    selectFile: vi.fn().mockResolvedValue(true),
    navigateToDir: vi.fn().mockResolvedValue(true),
    navigateToParentDir: vi.fn().mockResolvedValue(true),
  }

  const coord = useNavigationCoordinator({
    store: fakeStore as any,
    fileNav: files,
    navigation,
    browseFileSession: browseSession,
    markdownViewMode: markdownViewMode as any,
    layout: {
      isWideScreen,
      activePane,
      activeTab,
      leftPanelActive: computed(() => surface.value),
      panelIsActive: (tab: string) => surface.value === tab,
      switchTab: (tab: string) => {
        surface.value = tab
      },
      setActivePane: (pane: ActivePane) => {
        activePane.value = pane
      },
      setLeftCollapsed: () => {},
    },
    viewActions: {
      closeOverlayAndSync: () => {
        files.closeOverlay()
      },
    },
  })

  // 1. User opens README.md from browse
  files.openFile('README.md', { viewMode: 'rendered' })
  // The real store selects the file too; handleCaptureFileScroll only trusts a
  // capture whose path matches the active file, so the fixture has to mirror it.
  fakeStore.state.currentFile = { name: 'README.md', path: 'README.md' }
  expect(coord.fileBackTarget.value).toBe('browse')
  expect(files.canGoBack.value).toBe(false)

  // 2. In wide screen, activePane is 'right' (chat). User clicks code link in README.md (source: 'file')
  coord.handleCaptureFileScroll(450)
  coord.handleOpenFileOverlay({ path: 'src/app.ts', lineStart: 10, source: 'file' })
  expect(coord.fileBackTarget.value).toBe('file')
  expect(files.canGoBack.value).toBe(true)
  expect(markdownViewMode.value).toBe('raw')

  // 3. First back -> returns to README.md, restores scrollTop and viewMode
  expect(await coord.navigateBack('header')).toBe(true)
  // The visit banks the whole snapshot next to the pixel offset, so a rendered
  // return can also restore its anchor once the layout settles.
  expect(files.currentLocation.value).toEqual({
    path: 'README.md',
    scrollTop: 450,
    viewMode: 'rendered',
    scrollEntry: { scrollTop: 450 },
  })
  expect(markdownViewMode.value).toBe('rendered')
  expect(coord.fileBackTarget.value).toBe('browse')

  // 4. Second back -> returns to file manager (browse)
  expect(await coord.navigateBack('header')).toBe(true)
  expect(surface.value).toBe('browse')
  expect(files.overlayOpen.value).toBe(false)
  expect(navigation.hasOrigin.value).toBe(false)
})

it('external jump from chat resets file stack and navigates back to chat', async () => {
  const files = useFileNavStack()
  const navigation = useNavigationContext()
  const browseSession = ref(true)
  const isWideScreen = ref(false)
  const activePane = ref<ActivePane>('left')
  const activeTab = ref('chat')
  const surface = ref('chat')
  const markdownViewMode = ref<'rendered' | 'raw'>('rendered')

  const fakeStore = {
    state: {
      currentFile: null as { name: string; path: string } | null,
      currentDir: '',
    },
    selectFile: vi.fn().mockResolvedValue(true),
    navigateToDir: vi.fn().mockResolvedValue(true),
    navigateToParentDir: vi.fn().mockResolvedValue(true),
  }

  const coord = useNavigationCoordinator({
    store: fakeStore as any,
    fileNav: files,
    navigation,
    browseFileSession: browseSession,
    markdownViewMode: markdownViewMode as any,
    layout: {
      isWideScreen,
      activePane,
      activeTab,
      leftPanelActive: computed(() => surface.value),
      panelIsActive: (tab: string) => surface.value === tab,
      switchTab: (tab: string) => {
        surface.value = tab
      },
      setActivePane: (pane: ActivePane) => {
        activePane.value = pane
      },
      setLeftCollapsed: () => {},
    },
    viewActions: {
      closeOverlayAndSync: () => {
        files.closeOverlay()
      },
    },
  })

  // Pre-existing file
  files.openFile('README.md')

  // Chat message clicks a file link
  coord.handleOpenFileOverlay({ path: 'src/main.ts', source: 'chat' })
  expect(files.canGoBack.value).toBe(false)
  expect(coord.fileBackTarget.value).toBe('origin')

  // Back returns to chat
  expect(await coord.navigateBack('header')).toBe(true)
  expect(surface.value).toBe('chat')
  expect(navigation.hasOrigin.value).toBe(false)
})

// M5: Markdown -> 目录 -> 文件 C. Back from C must land on the directory it was
// opened from; only a further Back unwinds the directory excursion and returns
// to the Markdown that started it. Jumping straight to the Markdown would skip
// the directory and leave the user unable to walk back up to their own folder.
it('M5: file opened during a directory excursion returns to the directory first, then to the suspended file', async () => {
  const files = useFileNavStack()
  const navigation = useNavigationContext()
  const browseSession = ref(false)
  const surface = ref('view')
  const suspendedDirectory = ref(false)
  // The suspended visit before the excursion: Markdown with a chat origin.
  navigation.start({ surface: 'chat', tab: 'chat', label: 'Back to Chat' })
  files.openFile('doc.md', { viewMode: 'rendered', scrollTop: 120 })
  const suspended = { files: files.snapshot(), navigation: navigation.snapshot(), browseSession: false }
  // The excursion starts: the stack is reset and the origin becomes the file.
  files.closeOverlay()
  navigation.consume()
  navigation.start({ surface: 'file', tab: 'view', label: 'Back to doc.md', filePath: 'doc.md' })
  suspendedDirectory.value = true

  // User opens a file from the directory.
  files.openFile('src/c.ts')
  browseSession.value = true
  surface.value = 'view'

  const target = useFileBackTarget(() => ({
    browseSession: browseSession.value,
    canGoBackFile: files.canGoBack.value,
    hasOrigin: navigation.hasOrigin.value,
  }))

  const returnToOrigin = vi.fn(async () => {
    // Returning to the suspended Markdown also resumes the suspended visit.
    const origin = navigation.origin.value
    if (origin?.filePath) {
      files.restore(suspended.files)
      navigation.restore(suspended.navigation)
      browseSession.value = false
      suspendedDirectory.value = false
    }
    return true
  })

  const back = useNavigationStateMachine({
    hasTopmostOverlay: () => false,
    closeTopmostOverlay: () => false,
    isEditing: () => false,
    exitEdit: () => {},
    canGoBackFile: () => surface.value === 'view' && files.overlayOpen.value && target.value === 'file',
    goBackFile: async () => files.goBack() !== null,
    hasOrigin: () => surface.value !== 'view' || target.value !== 'browse' ? navigation.hasOrigin.value : false,
    returnToOrigin,
    canGoBackDir: () => false,
    goBackDir: async () => false,
    canCloseOverlay: () => files.overlayOpen.value && surface.value === 'view',
    closeOverlay: () => {
      files.closeOverlay()
      browseSession.value = false
      surface.value = 'browse'
      return true
    },
  })

  // The single file C has no history. It was opened from the directory, so its
  // return target is that directory — the excursion is only unwound afterwards.
  expect(files.canGoBack.value).toBe(false)
  expect(target.value).toBe('browse')

  // First Back: close the file, land on the directory.
  expect(await back.navigateBack('android')).toBe(true)
  expect(returnToOrigin).not.toHaveBeenCalled()
  expect(surface.value).toBe('browse')
  expect(navigation.origin.value?.filePath).toBe('doc.md')
  expect(suspendedDirectory.value).toBe(true)

  // Second Back (now on the browse panel, no browse session): the excursion
  // unwinds to the Markdown that started it.
  expect(target.value).toBe('origin')
  expect(await back.navigateBack('android')).toBe(true)
  expect(returnToOrigin).toHaveBeenCalledTimes(1)
  // The suspended visit is back: the Markdown is current and its chat origin restored.
  expect(files.currentLocation.value?.path).toBe('doc.md')
  expect(navigation.origin.value?.surface).toBe('chat')
  expect(suspendedDirectory.value).toBe(false)
})

it('recent file navigation: opening recent file while viewing a file preserves history stack and returns to previous file', async () => {
  const files = useFileNavStack()
  const navigation = useNavigationContext()
  const browseSession = ref(false)
  const surface = ref('view')

  // Jump from chat to file A
  navigation.start({ surface: 'chat', tab: 'chat', label: 'Back to Chat' })
  files.openFile('FileA.ts', { scrollTop: 50 })

  const target = useFileBackTarget(() => ({
    browseSession: browseSession.value,
    canGoBackFile: files.canGoBack.value,
    hasOrigin: navigation.hasOrigin.value,
  }))

  const returnToOrigin = vi.fn(async () => {
    surface.value = 'chat'
    return true
  })

  const back = useNavigationStateMachine({
    hasTopmostOverlay: () => false,
    closeTopmostOverlay: () => false,
    isEditing: () => false,
    exitEdit: () => {},
    canGoBackFile: () => surface.value === 'view' && files.overlayOpen.value && target.value === 'file',
    goBackFile: async () => files.goBack() !== null,
    hasOrigin: () => target.value !== 'browse' && navigation.hasOrigin.value,
    returnToOrigin,
    canGoBackDir: () => false,
    goBackDir: async () => false,
  })

  // User opens recent File B while already in file view
  // Per handleAppHeaderRecentFileSelect fix: pushes to files without clearing or creating degenerate origin
  files.openFile('FileB.ts', { scrollTop: 0 })

  // Stack has [FileA, FileB]
  expect(files.canGoBack.value).toBe(true)
  expect(target.value).toBe('file')

  // Back 1: returns to FileA
  expect(await back.navigateBack('header')).toBe(true)
  expect(files.currentLocation.value?.path).toBe('FileA.ts')
  expect(returnToOrigin).not.toHaveBeenCalled()
  expect(target.value).toBe('origin')

  // Back 2: returns to chat origin
  expect(await back.navigateBack('header')).toBe(true)
  expect(returnToOrigin).toHaveBeenCalledTimes(1)
  expect(surface.value).toBe('chat')
})
