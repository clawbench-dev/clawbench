import { computed, nextTick, type Ref, type ComputedRef } from 'vue'
import { store as defaultStore } from '@/stores/app'
import { appLog } from '@/utils/appLog'
import { isAbsolutePath } from '@/utils/path'
import { getFileType } from '@/utils/fileType'
import { openRecentFile } from '@/composables/useRecentFiles'
import { getFileScroll, getFileScrollEntry, setFileScroll, type FileScrollEntry } from '@/utils/fileScrollCache'
import { gt as defaultGt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast'
import { PANE_LEFT, PANE_RIGHT, type ActivePane } from '@/composables/useWideScreenLayout'
import {
  useNavigationContext,
  resolveJumpOriginTab,
  surfaceToTab,
  isSameVisit,
  shouldSettleOrigin,
  type NavigationSurface,
  type NavigationOrigin,
} from '@/composables/useNavigationContext'
import { useFileNavStack } from '@/composables/useFileNavStack'
import { useDirectoryReturn } from '@/composables/useDirectoryReturn'
import { useFileBackTarget } from '@/composables/useFileBackTarget'
import { useNavigationStateMachine } from '@/composables/useNavigationStateMachine'

const TAG = 'Navigation'

export type OpenFileOverlayPayload = {
  path?: string
  lineStart?: number
  lineEnd?: number
  source?: string
}

export interface NavigationCoordinatorOptions {
  store?: typeof defaultStore
  fileNav?: ReturnType<typeof useFileNavStack>
  navigation?: ReturnType<typeof useNavigationContext>
  directoryReturn?: ReturnType<typeof useDirectoryReturn>
  browseFileSession: Ref<boolean>
  markdownViewMode: Ref<string>
  layout: {
    isWideScreen: Ref<boolean>
    activePane: Ref<ActivePane>
    activeTab: Ref<string>
    leftPanelActive: ComputedRef<string> | Ref<string>
    panelIsActive: (tabId: string) => boolean
    switchTab: (tab: string, force?: boolean) => void
    setActivePane: (pane: ActivePane) => void
    setLeftCollapsed: (collapsed: boolean) => void
  }
  fileEditor?: {
    isEditing: () => boolean
    exitEdit: () => void
  }
  viewActions?: {
    scrollToLine?: (start: number, end?: number, path?: string) => void
    closeOverlayAndSync?: () => void
    handleOpenFileManager?: () => void
    isFileManagerMultiSelectActive?: () => boolean
    /**
     * Ask the mounted file viewer for a fresh { scrollTop, anchor } snapshot of
     * the file being left, synchronously.
     *
     * Without it, captures read the module-level scroll cache, which only
     * refreshes on debounced scroll — a jump taken immediately after a fling
     * (or before any scroll at all) would bank a position from an earlier
     * moment and return the user there.
     */
    requestScrollCapture?: () => void
  }
  backHooks?: {
    hasTopmostOverlay?: () => boolean
    closeTopmostOverlay?: () => boolean
    canNavigateBack?: () => boolean
    handleBackNavigation?: () => boolean
  }
  i18n?: {
    t?: (key: string, args?: Record<string, unknown>) => string
    gt?: (key: string, args?: Record<string, unknown>) => string
  }
  toast?: {
    show: (msg: string, opts?: { icon?: string; type?: 'info' | 'error' | 'success'; duration?: number }) => void
  }
}

export function useNavigationCoordinator(options: NavigationCoordinatorOptions) {
  const store = options.store ?? defaultStore
  const fileNav = options.fileNav ?? useFileNavStack()
  const navigation = options.navigation ?? useNavigationContext()
  const browseFileSession = options.browseFileSession
  const directoryReturn = options.directoryReturn ?? useDirectoryReturn(browseFileSession)
  const markdownViewMode = options.markdownViewMode
  const {
    isWideScreen,
    activePane,
    activeTab,
    leftPanelActive,
    panelIsActive,
    switchTab,
    setActivePane,
    setLeftCollapsed,
  } = options.layout

  const gt = options.i18n?.gt ?? defaultGt
  const t = options.i18n?.t ?? ((key: string, args?: Record<string, unknown>) => gt(key, args))
  const toast = options.toast ?? useToast()

  const scrollToLine = options.viewActions?.scrollToLine ?? (() => {})
  const closeOverlayAndSync = options.viewActions?.closeOverlayAndSync ?? (() => {})
  const handleOpenFileManager = options.viewActions?.handleOpenFileManager ?? (() => {})
  const isFileManagerMultiSelectActive = options.viewActions?.isFileManagerMultiSelectActive ?? (() => false)
  const requestScrollCapture = options.viewActions?.requestScrollCapture ?? (() => {})

  const hasTopmostOverlay = options.backHooks?.hasTopmostOverlay ?? (() => false)
  const closeTopmostOverlay = options.backHooks?.closeTopmostOverlay ?? (() => false)
  const canNavigateBack = options.backHooks?.canNavigateBack ?? (() => false)
  const handleBackNavigation = options.backHooks?.handleBackNavigation ?? (() => false)

  const isEditing = options.fileEditor?.isEditing ?? (() => false)
  const exitEdit = options.fileEditor?.exitEdit ?? (() => {})

  let applyingOriginReturn = false
  let directoryRequestId = 0

  function isApplyingOriginReturn(): boolean {
    return applyingOriginReturn
  }

  function surfaceLabel(surface: NavigationSurface | string): string {
    if (surface === 'chat') return t('file.nav.backToChat')
    if (surface === 'task' || surface === 'tasks') return t('file.nav.backToTask')
    if (surface === 'history') return t('git.history.projectHistory')
    if (surface === 'browse') return t('file.nav.back')
    return t('common.back')
  }

  /**
   * Bank where the user is in the current file before leaving it.
   *
   * Reads the live viewer first: the module-level scroll cache only refreshes on
   * debounced scroll, so on its own it can be a fling behind. Falls back to the
   * cache when no viewer is mounted, and leaves the recorded position untouched
   * when neither has anything — never to another file's leftover offset.
   */
  function captureCurrentFileState(pathOverride?: string | null): void {
    const currentPath = store.state.currentFile?.path
    const path = pathOverride ?? currentPath
    if (!path) return
    if (!pathOverride || pathOverride === currentPath) {
      requestScrollCapture()
    }
    // Only borrow the nav stack's own entry when it describes the same file —
    // banking another visit's snapshot here would send the user to the wrong
    // place on return.
    const isCurrentFile = fileNav.currentLocation.value?.path === path
    const navEntry = isCurrentFile
      ? fileNav.currentLocation.value?.scrollEntry
      : undefined
    const cachedEntry = getFileScrollEntry(path) ?? navEntry
    const cachedScroll = getFileScroll(path) ?? (isCurrentFile ? fileNav.currentLocation.value?.scrollTop : undefined)
    // Pass `undefined` (not 0) when the cache holds nothing: updateCurrent
    // skips undefined, keeping a position this visit already recorded from a
    // live capture. Writing 0 here would silently discard it.
    if (isCurrentFile) {
      fileNav.updateCurrent({
        viewMode: markdownViewMode.value,
        scrollTop: cachedScroll,
        scrollEntry: cachedEntry,
      })
    }
  }

  function handleCaptureFileScroll(scroll: number | FileScrollEntry): void {
    const currentPath = store.state.currentFile?.path
    if (!currentPath) return
    if (fileNav.currentLocation.value?.path === currentPath) {
      if (typeof scroll === 'number') {
        fileNav.updateCurrent({ scrollTop: scroll })
      } else if (scroll && typeof scroll === 'object') {
        fileNav.updateCurrent({ scrollTop: scroll.scrollTop, scrollEntry: scroll })
      }
    }
    setFileScroll(currentPath, scroll)
  }

  function settleOriginForTab(tab: string): void {
    if (!shouldSettleOrigin(navigation.origin.value, tab, {
      pending: directoryReturn.pending(),
      currentFilePath: store.state.currentFile?.path,
    })) return
    directoryReturn.clear()
    navigation.consume()
  }

  function beginExternalJump(surface: NavigationSurface | string, label: string, target: Partial<NavigationOrigin> = {}): void {
    browseFileSession.value = false
    const normSurface = (surface === 'tasks' ? 'task' : surface) as NavigationSurface
    const currentTab = resolveJumpOriginTab(normSurface, activeTab.value, {
      isWideScreen: isWideScreen.value,
      chatPaneActive: isWideScreen.value ? activePane.value === PANE_RIGHT : activeTab.value === 'chat',
    })
    fileNav.closeOverlay()
    const origin: NavigationOrigin = {
      surface: normSurface,
      tab: currentTab,
      label,
      ...target,
    }
    // Callers only start jumps from chat/task/history surfaces; a jump from
    // the file view is a file-stack push (see handleOpenFileOverlay), so there
    // is deliberately no 'file' branch here.
    if (navigation.start(origin)) return
    if (!isSameVisit(navigation.origin.value, origin)) {
      directoryReturn.clear()
      navigation.replace(origin)
    }
  }

  function openFileInViewer(path: string, options: { lineStart?: number; lineEnd?: number } = {}): void {
    const { lineStart, lineEnd } = options
    if (lineStart) {
      markdownViewMode.value = 'raw'
    } else if (getFileType(path).isMarkdown) {
      markdownViewMode.value = 'rendered'
    }
    fileNav.openFile(path, { lineStart, lineEnd, viewMode: markdownViewMode.value })
    if (lineStart) scrollToLine(lineStart, lineEnd, path)
  }

  async function goBackFile(): Promise<boolean> {
    captureCurrentFileState()
    const location = fileNav.previousLocation.value
    if (!location?.path) return false
    const previousPath = location.path

    if (location.scrollEntry !== undefined) {
      setFileScroll(previousPath, location.scrollEntry)
    } else if (location.scrollTop !== undefined) {
      setFileScroll(previousPath, location.scrollTop)
    }
    const targetViewMode = location.viewMode ?? (getFileType(previousPath).isMarkdown ? 'rendered' : undefined)
    if (targetViewMode) {
      markdownViewMode.value = targetViewMode
    }
    const ok = await store.selectFile(previousPath)
    if (!ok) {
      toast.show(gt('file.toast.fileNotFound'), { type: 'error', icon: '⚠️', duration: 2000 })
      return false
    }
    fileNav.goBack()

    await nextTick()
    // The recorded reading position wins over lineStart: a visit opened via a
    // line-numbered link keeps its original line forever, but by return time
    // the user has usually scrolled elsewhere — jumping back to the landing
    // line would discard that. scrollToLine stays as the fallback for visits
    // that never captured a position (jumped away before any scroll).
    //
    // A recorded 0 does not count as "captured": it is what a viewer reports
    // before the async scroll-to-line has landed, so treating it as a real
    // position would dump the user at the top of the file instead of the line
    // they were sent to.
    const recordedTop = location.scrollEntry?.scrollTop ?? location.scrollTop
    if (typeof recordedTop === 'number' && recordedTop > 0) {
      // Pixels are only comparable when the pane layout is unchanged. Leaving
      // and returning in the same view mode means content heights match, so
      // pixel offset directly applies.
      const sameView = !location.viewMode || location.viewMode === markdownViewMode.value
      window.dispatchEvent(new CustomEvent('restore-file-scroll', {
        detail: {
          scrollTop: recordedTop,
          scrollEntry: location.scrollEntry,
          preferPx: sameView,
        },
      }))
    } else if (location.lineStart) {
      scrollToLine(location.lineStart, location.lineEnd, previousPath)
    }
    return true
  }

  async function returnToOrigin(): Promise<boolean> {
    const origin = navigation.origin.value
    if (!origin) return false

    ++directoryRequestId
    captureCurrentFileState()

    applyingOriginReturn = true
    try {
      if (origin.tab) {
        if (isWideScreen.value && origin.tab === 'chat') {
          setActivePane(PANE_RIGHT)
        } else {
          switchTab(origin.tab)
        }
      }

      if (origin.filePath) {
        if (origin.scrollEntry !== undefined) {
          setFileScroll(origin.filePath, origin.scrollEntry)
        } else if (origin.scrollTop !== undefined) {
          setFileScroll(origin.filePath, origin.scrollTop)
        }
        const targetViewMode = origin.viewMode ?? (getFileType(origin.filePath).isMarkdown ? 'rendered' : undefined)
        if (targetViewMode) {
          markdownViewMode.value = targetViewMode
        }
        const ok = await store.selectFile(origin.filePath)
        if (!ok) {
          toast.show(t('file.toast.fileNotFoundReturned'), { type: 'error', icon: '⚠️', duration: 2000 })
          directoryReturn.clear()
          navigation.consume()
          return true
        }
        await nextTick()
        // Reading position wins over lineStart — same rationale as goBackFile,
        // including the "0 is not a captured position" rule.
        const recordedTop = origin.scrollEntry?.scrollTop ?? origin.scrollTop
        if (typeof recordedTop === 'number' && recordedTop > 0) {
          const sameView = !origin.viewMode || origin.viewMode === markdownViewMode.value
          window.dispatchEvent(new CustomEvent('restore-file-scroll', {
            detail: {
              scrollTop: recordedTop,
              scrollEntry: origin.scrollEntry,
              preferPx: sameView,
            },
          }))
        } else if (origin.lineStart) {
          scrollToLine(origin.lineStart, origin.lineEnd, origin.filePath)
        }
      } else if (origin.surface !== 'file') {
        closeOverlayAndSync()
      }
    } finally {
      applyingOriginReturn = false
    }

    const restoredVisit = directoryReturn.restore()
    if (!restoredVisit) {
      navigation.consume()
    } else if (typeof restoredVisit.directory === 'string') {
      void store.navigateToDir(restoredVisit.directory)
    }
    return true
  }

  const fileBackTarget = useFileBackTarget(() => ({
    browseSession: browseFileSession.value,
    canGoBackFile: fileNav.canGoBack.value,
    hasOrigin: navigation.hasOrigin.value,
  }))

  const { navigateBack, canHandleBack } = useNavigationStateMachine({
    hasTopmostOverlay,
    closeTopmostOverlay,
    isEditing,
    exitEdit,
    canGoBackFile: () => panelIsActive('view') && fileNav.overlayOpen.value && fileBackTarget.value === 'file',
    goBackFile,
    hasOrigin: () => !(panelIsActive('view') && fileBackTarget.value === 'browse') && navigation.hasOrigin.value,
    returnToOrigin,
    canGoBackDir: () => panelIsActive('browse') && store.state.currentDir !== '',
    goBackDir: async () => {
      await store.navigateToParentDir()
      return true
    },
    closeOverlay: () => {
      if (fileNav.overlayOpen.value && panelIsActive('view')) {
        closeOverlayAndSync()
        browseFileSession.value = false
        switchTab('browse', true)
        return true
      }
      return false
    },
    canCloseOverlay: () => fileNav.overlayOpen.value && panelIsActive('view'),
    canHandleOther: canNavigateBack,
    handleOther: handleBackNavigation,
  })

  const canNavigateBackInView = computed(() => fileBackTarget.value !== null)

  const viewBackLabel = computed(() => {
    if (fileBackTarget.value === 'browse') return t('file.nav.back')
    if (fileNav.canGoBack.value) {
      const prev = fileNav.previousLocation.value
      const prevName = prev?.path ? prev.path.split('/').pop() : ''
      return prevName ? t('file.nav.backToFile', { name: prevName }) : t('common.back')
    }
    if (navigation.hasOrigin.value) {
      return navigation.originLabel.value || t('common.back')
    }
    return t('common.back')
  })

  async function handleNavigateBack(): Promise<void> {
    await navigateBack('header')
  }

  async function handleOpenDirectoryFromContext(path?: string | null, source?: NavigationSurface | string): Promise<void> {
    if (path === undefined || path === null) return
    const reqId = ++directoryRequestId
    const surface: string = source ?? (isWideScreen.value && activePane.value === PANE_RIGHT ? 'chat' : leftPanelActive.value)
    const file = store.state.currentFile
    const fromFileView = (surface === 'view' || surface === 'file') && !!file?.path

    if (fromFileView && file) {
      captureCurrentFileState()
      const location = fileNav.currentLocation.value
      directoryReturn.enter({
        surface: 'file',
        tab: 'view',
        label: t('file.nav.backToFile', { name: file.name || file.path.split('/').pop() }),
        filePath: file.path,
        viewMode: markdownViewMode.value,
        lineStart: location?.lineStart,
        lineEnd: location?.lineEnd,
        scrollTop: location?.scrollTop ?? getFileScroll(file.path) ?? 0,
        scrollEntry: location?.scrollEntry ?? getFileScrollEntry(file.path),
      }, store.state.currentDir)
    } else if (surface === 'chat' || surface === 'task' || surface === 'tasks' || surface === 'history') {
      const normSurface: NavigationSurface = surface === 'tasks' ? 'task' : (surface as NavigationSurface)
      beginExternalJump(normSurface, surfaceLabel(normSurface), normSurface === 'chat' ? { tab: 'chat' } : {})
    }

    const originTab = surfaceToTab(surface)
    const abandonJump = () => {
      if (originTab === null) {
        appLog.w(TAG, `handleOpenDirectoryFromContext: unknown surface "${surface}"`)
      }
      if (fromFileView) {
        directoryReturn.restore()
      } else {
        navigation.consume()
      }
      switchTab(originTab ?? (leftPanelActive.value as string), true)
    }

    handleOpenFileManager()
    try {
      const suppressed = store.state.dirLoading
      const ok = await store.navigateToDir(path)
      if (reqId !== directoryRequestId) return
      if (!ok) {
        if (suppressed) return
        abandonJump()
        return
      }
      if (isWideScreen.value) {
        setLeftCollapsed(false)
        setActivePane(PANE_LEFT)
      }
      switchTab('browse', true)
    } catch (err) {
      if (reqId !== directoryRequestId) return
      abandonJump()
      appLog.e(TAG, 'handleOpenDirectoryFromContext error:', err)
    }
  }

  function handleOpenDirectoryFromEvent(e: Event | CustomEvent<{ path?: string; source?: NavigationSurface }>): void {
    const custom = e as CustomEvent<{ path?: string; source?: NavigationSurface }>
    const path = custom?.detail?.path
    if (path !== undefined && path !== null) {
      void handleOpenDirectoryFromContext(path, custom.detail?.source)
    }
  }

  async function handleSelectFile(path: string): Promise<void> {
    const surface: NavigationSurface = panelIsActive('history') ? 'history' : 'chat'
    const label = surfaceLabel(surface)
    const ok = await store.selectFile(path)
    if (ok) {
      beginExternalJump(surface, label)
      switchTab('view')
      openFileInViewer(path)
    }
  }

  async function handleBrowseSelectFile(path: string): Promise<void> {
    if (isFileManagerMultiSelectActive()) return
    fileNav.closeOverlay()
    browseFileSession.value = true
    const ok = await store.selectFile(path)
    if (ok) {
      openFileInViewer(path)
      switchTab('view')
    } else {
      browseFileSession.value = false
    }
  }

  async function handleTaskOpenFile(filePath: string, lineStart?: number): Promise<void> {
    const ok = await store.selectFile(filePath)
    if (ok) {
      beginExternalJump('task', surfaceLabel('task'))
      switchTab('view')
      openFileInViewer(filePath, { lineStart })
    }
  }

  async function handleOverlayClose(e?: Event | CustomEvent<{ force?: boolean }>): Promise<void> {
    const custom = e as CustomEvent<{ force?: boolean }>
    if (custom?.detail?.force) {
      closeOverlayAndSync()
      browseFileSession.value = false
      return
    }
    await navigateBack('close')
  }

  async function handleOverlayOpenFile(payload: string | { path: string; lineStart?: number; lineEnd?: number }): Promise<void> {
    const { path, lineStart, lineEnd } = typeof payload === 'string' ? { path: payload, lineStart: undefined, lineEnd: undefined } : payload
    const prevPath = fileNav.currentFilePath.value
    // Snapshot the outgoing file from the live DOM before pushing the new one.
    captureCurrentFileState(prevPath)

    if (!path.startsWith('/')) {
      try {
        const resp = await fetch(`/api/dir?path=${encodeURIComponent(path)}`)
        if (resp.ok) {
          await handleOpenDirectoryFromContext(path)
          return
        }
      } catch {
        // Not a directory, fall through
      }
    }

    const isExternal = isAbsolutePath(path)
    const ok = await store.selectFile(path)
    if (ok) {
      openFileInViewer(path, { lineStart, lineEnd })
      if (isExternal) {
        toast.show(gt('file.toast.externalFile'), { icon: 'ℹ️', type: 'info', duration: 2000 })
      }
    }
  }

  async function handleAppHeaderRecentFileSelect(path: string): Promise<void> {
    const isCurrentFileView = panelIsActive('view') && fileNav.overlayOpen.value && store.state.currentFile?.path
    if (isCurrentFileView) {
      captureCurrentFileState()
      const ok = await openRecentFile(path, (p: string) => store.selectFile(p))
      if (ok) {
        switchTab('view')
        openFileInViewer(path)
      }
      return
    }

    const surface: NavigationSurface = (isWideScreen.value ? activePane.value === PANE_RIGHT : activeTab.value === 'chat')
      ? 'chat'
      : panelIsActive('tasks')
      ? 'task'
      : panelIsActive('history')
      ? 'history'
      : panelIsActive('browse')
      ? 'browse'
      : 'file'

    const ok = await openRecentFile(path, (p: string) => store.selectFile(p))
    if (ok) {
      if (surface === 'browse') {
        fileNav.closeOverlay()
        browseFileSession.value = true
      } else if (surface === 'chat' || surface === 'task' || surface === 'history') {
        const label = surfaceLabel(surface)
        beginExternalJump(surface, label, surface === 'chat' ? { tab: 'chat' } : {})
      } else {
        fileNav.closeOverlay()
        browseFileSession.value = false
      }
      switchTab('view')
      openFileInViewer(path)
    }
  }

  function handleOpenFileOverlay(
    e: Event | CustomEvent<OpenFileOverlayPayload> | { detail?: OpenFileOverlayPayload } | OpenFileOverlayPayload
  ): void {
    const detail = (e && typeof e === 'object' && 'detail' in e && (e as { detail?: OpenFileOverlayPayload }).detail)
      ? (e as { detail: OpenFileOverlayPayload }).detail
      : (e as OpenFileOverlayPayload)
    const { path, lineStart, lineEnd, source } = detail || {}
    if (!path) return

    const isCurrentFileView = panelIsActive('view') && fileNav.overlayOpen.value
    const isExplicitFileSource = source === 'file'
    const isFromCurrentFile = isExplicitFileSource || (isCurrentFileView && source !== 'chat' && source !== 'task' && source !== 'history' && (!isWideScreen.value || activePane.value !== PANE_RIGHT))

    if (!isFromCurrentFile) {
      const fromChat = source === 'chat' || (isWideScreen.value ? activePane.value === PANE_RIGHT : activeTab.value === 'chat')
      const fromTask = source === 'task' || panelIsActive('tasks')
      const fromHistory = source === 'history' || panelIsActive('history')
      if (fromChat) {
        beginExternalJump('chat', surfaceLabel('chat'), { tab: 'chat' })
      } else if (fromTask) {
        beginExternalJump('task', surfaceLabel('task'))
      } else if (fromHistory) {
        beginExternalJump('history', surfaceLabel('history'))
      }
    } else {
      const prevPath = fileNav.currentFilePath.value
      captureCurrentFileState(prevPath)
    }

    switchTab('view')
    openFileInViewer(path, { lineStart, lineEnd })
  }

  return {
    isApplyingOriginReturn,
    invalidateDirectoryRequests: () => {
      ++directoryRequestId
    },
    settleOriginForTab,
    handleCaptureFileScroll,
    openFileInViewer,
    goBackFile,
    returnToOrigin,
    fileBackTarget,
    canNavigateBackInView,
    viewBackLabel,
    navigateBack,
    canHandleBack,
    handleNavigateBack,
    handleOpenDirectoryFromContext,
    handleOpenDirectoryFromEvent,
    handleSelectFile,
    handleBrowseSelectFile,
    handleTaskOpenFile,
    handleOverlayClose,
    handleOverlayOpenFile,
    handleAppHeaderRecentFileSelect,
    handleOpenFileOverlay,
  }
}
