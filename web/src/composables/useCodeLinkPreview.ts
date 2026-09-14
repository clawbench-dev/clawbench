/**
 * Composable for Markdown repository code link click preview, pin, and drag.
 *
 * Responsibilities:
 * - Lifecycle & state machine: hidden / pending / transient / pinned / sheet
 * - Click-to-open on desktop; touch taps open the bottom sheet
 * - Single active instance across screen
 * - Request generation tracking & AbortController to prevent race conditions
 * - Deduplication and LRU caching (previewCache)
 * - Switch: markdownCodeLinkPreview (default true)
 * - Touch detection ((hover: none), (pointer: coarse))
 */

import { ref, computed, watch, onUnmounted, getCurrentInstance, type Ref, type ComputedRef } from 'vue'
import { store } from '@/stores/app'
import { appLog } from '@/utils/appLog'
import { apiGet } from '@/utils/api'
import { openFilePath } from '@/composables/useFilePathAnnotation'
import { parseLineRanges } from '@/utils/lineRanges'
import { getFileType } from '@/utils/fileType'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import type { NavigationSurface } from '@/composables/useNavigationContext'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import {
  sliceCodeForPreview,
  computeRenderWindow,
  computeFetchWindow,
  windowCovers,
  buildPreviewUrl,
  placeNearAnchor,
  previewCache,
  LARGE_FILE_THRESHOLD_BYTES,
  type CodeSliceResult,
  type FileContentResponse,
  type FetchWindow,
  type CardPlacementResult,
} from '@/utils/codeLinkPreview'

export type PreviewStatus = 'idle' | 'loading' | 'ready' | 'error'
export type PreviewMode = 'transient' | 'pinned' | 'sheet' | 'docked'
export type PreviewErrorCode = 'binary' | 'too-large' | 'not-file' | 'not-found' | 'access-denied' | 'network'
export type PreviewRenderMode = 'rendered' | 'source'

export interface PreviewTarget {
  filePath: string
  lineStart?: number
  lineEnd?: number
  /** Full multi-range target (canonical "90-91,309,938-943"), when annotated. */
  lineRanges?: string
  anchorEl?: HTMLElement
}

/** Whether the preview target is a Markdown file (by extension). */
export function isMarkdownTarget(target: PreviewTarget | null): boolean {
  const filePath = target?.filePath || ''
  return filePath ? Boolean(getFileType(filePath).isMarkdown) : false
}

/** Whether the preview target carries an explicit line annotation. */
export function hasLineRange(target: PreviewTarget | null): boolean {
  return !!(target && target.lineStart && Number.isInteger(target.lineStart) && target.lineStart > 0)
}

export interface UseCodeLinkPreviewOptions {
  containerRef?: Ref<HTMLElement | null>
  /** Surface the preview was opened from — threaded into the full-screen open
   *  so the navigation origin records the right return target. */
  source?: NavigationSurface
  /** Called synchronously before opening the target file in full view. */
  onBeforeOpen?: () => void
  /** Replace the global markdownCodeLinkPreview switch with a caller-owned
   *  gate. Used by the file manager's explicit "preview mode", which must work
   *  independently of the markdown-link-preview preference. */
  enabled?: Ref<boolean> | ComputedRef<boolean>
  /** CSS selector for elements that should NOT count as an "outside" click
   *  when dismissing an unpinned card. The file manager lists pass their row
   *  selector so clicking another row retargets the card in place instead of
   *  closing and reopening it. */
  outsideClickIgnoreSelector?: string
}

// Only one preview surface should be visible across chat/file panes. Keep the
// coordination lightweight: each composable retains its own state, while a
// newly opened instance closes the previously active one.
let activePreviewClose: (() => void) | null = null

export function useCodeLinkPreview(options: UseCodeLinkPreviewOptions = {}) {
  const { containerRef } = options
  const { localConfig } = useSettingsConfig()
  const { isPC } = usePlatformDetect()

  const enabled = computed(() => options.enabled
    ? options.enabled.value
    : localConfig.markdownCodeLinkPreview === true)

  // Selector for clicks that should not dismiss an unpinned card (see
  // outsideClickIgnoreSelector). Exposed so CodeLinkPreview's document
  // pointerdown handler can honor it.
  const outsideClickIgnoreSelector = options.outsideClickIgnoreSelector ?? ''

  // Reactive preview state
  const visible = ref(false)
  const status = ref<PreviewStatus>('idle')
  const mode = ref<PreviewMode>('transient')
  const target = ref<PreviewTarget | null>(null)
  const fileContent = ref<FileContentResponse | null>(null)
  const slicedCode = ref<CodeSliceResult | null>(null)
  const errorCode = ref<PreviewErrorCode | null>(null)
  const errorMessage = ref<string | null>(null)
  const isLargeFile = ref(false)
  const contextExpansion = ref(0)
  const extraAboveLines = ref(0)
  const extraBelowLines = ref(0)
  const placement = ref<CardPlacementResult | null>(null)
  const isPinned = computed(() => mode.value === 'pinned')
  /** Docked = rendered inline in a caller-owned pane (file manager bottom pane)
   *  rather than as a floating card. It has no placement/drag and is always
   *  "open until closed". */
  const isDocked = computed(() => mode.value === 'docked')

  // ── Rendered-vs-source view ─────────────────────────────────────────────
  // The preview has two body renderers: the line-based code slice (source)
  // and a rendered .markdown-body read-only document view (Markdown only).
  // A Markdown file WITHOUT a line range defaults to the rendered view; with a
  // line range it opens as the code slice so the user can pinpoint the
  // referenced lines — but the rendered view is still reachable via the eye
  // toggle (it renders the same line-slice window the code view shows).
  const renderMode = ref<PreviewRenderMode>('source')

  const isMarkdown = computed(() => {
    const filePath = target.value?.filePath || ''
    return filePath ? Boolean(getFileType(filePath).isMarkdown) : false
  })

  // ── Media targets (image / SVG / video / audio / PDF) ────────────────────
  // These are served as raw bytes by /api/local-file/ (correct MIME, no size
  // cap, inline), NOT by /api/file — which is JSON, 10 MiB-capped and reports
  // every raster image as binary. The preview short-circuits the fetch for
  // them and renders a media body straight from the URL.
  const fileType = computed(() => {
    const filePath = target.value?.filePath || ''
    return filePath ? getFileType(filePath) : null
  })
  const isImageTarget = computed(() => Boolean(fileType.value?.isImage))
  const isVideoTarget = computed(() => Boolean(fileType.value?.isVideo))
  const isAudioTarget = computed(() => Boolean(fileType.value?.isAudio))
  const isPdfTarget = computed(() => Boolean(fileType.value?.isPdf))
  const isMediaTarget = computed(() =>
    isImageTarget.value || isVideoTarget.value || isAudioTarget.value || isPdfTarget.value
  )

  // Bumped by refresh() for media targets so the media element re-requests its
  // URL (media bypasses the JSON fetch/cache entirely).
  const mediaRefreshNonce = ref(0)

  const hasExplicitLineRange = computed(() => {
    const t = target.value
    return !!(t && t.lineStart && Number.isInteger(t.lineStart) && t.lineStart > 0)
  })

  /** Whether the current target is a Markdown file (renderable in the doc view). */
  const canRenderMarkdown = computed(() => isMarkdown.value)

  // A Markdown file default-renders unless the annotation pinned a line range
  // (source slice is the useful view then). Re-evaluate on each target change.
  const effectiveRenderMode = computed<PreviewRenderMode>(() =>
    canRenderMarkdown.value ? renderMode.value : 'source'
  )

  // Timers & concurrency
  let currentRequestId = 0
  let currentAbortController: AbortController | null = null

  const isTouchDevice = (): boolean => {
    if (typeof window === 'undefined') return false
    if (!isPC.value) return true
    if (typeof window.innerWidth === 'number' && window.innerWidth < 768) return true
    if (typeof window.matchMedia !== 'undefined') {
      if (window.matchMedia('(hover: none), (pointer: coarse)').matches) return true
    }
    return false
  }

  // The line window the currently-held fileContent covers (null when it is the
  // whole file). Expanding context past it triggers a wider re-fetch.
  const fetchedWindow = ref<FetchWindow | null>(null)
  /** Total lines in the file, known once a windowed response arrives. */
  const fileTotalLines = ref<number | null>(null)
  /**
   * The server hit its own byte cap while collecting the window, so the returned
   * content is short (or empty, when a single line exceeded the cap). Widening
   * cannot help — the pane surfaces a notice instead.
   */
  const windowTruncated = ref(false)
  /** Guards the "widen the window" re-fetch so it converges in one extra round. */
  let pendingWiden = false

  const updateSlice = () => {
    if (!fileContent.value) return
    const lineRanges = target.value?.lineRanges ? parseLineRanges(target.value.lineRanges) : undefined
    const sliceOptions = {
      contextExpansion: contextExpansion.value,
      expandAboveLines: extraAboveLines.value,
      expandBelowLines: extraBelowLines.value,
      lineRanges,
    }
    const win = fetchedWindow.value
    if (win && win.end < win.start) {
      // The server captured no lines at all (its byte cap tripped, or the range
      // starts past EOF). `content` is empty, so slicing it would yield a bogus
      // blank line — report the empty window instead, and the pane shows the
      // truncation / out-of-range notice.
      slicedCode.value = {
        code: '',
        startLine: win.start,
        endLine: win.start - 1,
        totalLines: fileTotalLines.value ?? win.start,
        lineOutOfRange: !windowTruncated.value,
        renderTruncated: false,
      }
      return
    }
    if (win) {
      slicedCode.value = sliceCodeForPreview(
        fileContent.value.content,
        target.value?.lineStart,
        target.value?.lineEnd,
        {
          ...sliceOptions,
          baseLineOffset: win.start,
          // A windowed response reports the true file length; prefer it over the
          // locally-derived count, which only sees the window.
          totalLines: fileTotalLines.value ?? undefined,
        }
      )
    } else {
      slicedCode.value = sliceCodeForPreview(
        fileContent.value.content,
        target.value?.lineStart,
        target.value?.lineEnd,
        sliceOptions
      )
    }

    // The slice wanted lines outside the fetched window (it stops at the window
    // edge). Widen once to cover them; pendingWiden makes this converge even if
    // the server clamps the request. A server-truncated window is not widened —
    // the cap would just trip again.
    if (!pendingWiden && win && !windowTruncated.value && slicedCode.value) {
      const wanted = computeRenderWindow(
        target.value?.lineStart,
        target.value?.lineEnd,
        slicedCode.value.totalLines,
        sliceOptions
      )
      if (!windowCovers(win, wanted.startLine, wanted.endLine)) {
        pendingWiden = true
        // Request exactly the wanted render window. It is bounded by
        // MAX_RENDER_LINES (200), well under the server's window cap, so no
        // further clamping is needed.
        const wider: FetchWindow = { start: wanted.startLine, end: wanted.endLine }
        fetchPreview(target.value as PreviewTarget, true, wider, { silent: true }).finally(() => {
          pendingWiden = false
        })
      }
    }
  }

  const updatePlacement = (anchorEl?: HTMLElement, customWidth?: number, customHeight?: number) => {
    const el = anchorEl || target.value?.anchorEl
    if (!el || typeof el.getBoundingClientRect !== 'function') return
    const rect = el.getBoundingClientRect()
    // Realistic card dimensions (compact initial estimate before DOM measurement).
    // Media cards are wider/taller (the file IS the content), so estimate them
    // accordingly — otherwise the placement clamp would size the card as if it
    // were a narrow code slice and leave viewport space unused.
    const media = isMediaTarget.value
    const defaultWidth = media ? 960 : 720
    const defaultHeight = media ? 420 : 260
    const cardWidth = customWidth ?? Math.min(defaultWidth, typeof window !== 'undefined' ? window.innerWidth - 24 : 700)
    const cardHeight = customHeight ?? Math.min(defaultHeight, typeof window !== 'undefined' ? Math.min(defaultHeight, window.innerHeight * 0.6) : 240)
    placement.value = placeNearAnchor(rect, cardWidth, cardHeight)
  }

  const fetchPreview = async (
    newTarget: PreviewTarget,
    forceRefresh = false,
    windowOverride: FetchWindow | null = null,
    opts: { silent?: boolean } = {}
  ) => {
    const reqId = ++currentRequestId
    if (currentAbortController) {
      currentAbortController.abort()
    }
    currentAbortController = new AbortController()
    const signal = currentAbortController.signal

    // A silent fetch (widening the window after an expand) keeps the current
    // slice on screen instead of flipping back to the loading spinner — the
    // expand handler is anchoring scroll around the existing content.
    if (!opts.silent) {
      status.value = 'loading'
      errorCode.value = null
      errorMessage.value = null
      isLargeFile.value = false
    }

    // Media files are served as raw bytes by /api/local-file/ and rendered
    // straight from that URL — there is no JSON content to fetch, and /api/file
    // would reject every raster image as binary (10 MiB cap + null-byte sniff).
    // Go straight to 'ready' so the media body can mount.
    if (isMediaTarget.value) {
      fileContent.value = null
      slicedCode.value = null
      fetchedWindow.value = null
      fileTotalLines.value = null
      windowTruncated.value = false
      status.value = 'ready'
      return
    }

    // Ask for just the lines this preview can render (plus margin), so a large
    // file is never transferred in full. A widen re-fetch passes the exact
    // window it needs; otherwise the window is planned from the target.
    const win: FetchWindow = windowOverride
      ?? computeFetchWindow(newTarget, fileTotalLines.value)

    const projectRoot = store.state.projectRoot || ''
    const cacheKey = previewCache.buildKey(projectRoot, newTarget.filePath, win)

    if (forceRefresh) {
      previewCache.delete(cacheKey)
    } else {
      const cached = previewCache.get(cacheKey)
      if (cached) {
        if (reqId !== currentRequestId) return
        fileContent.value = cached
        isLargeFile.value = (cached.size ?? 0) > LARGE_FILE_THRESHOLD_BYTES
        fetchedWindow.value = cached.windowStart ? { start: cached.windowStart, end: cached.windowEnd ?? cached.windowStart } : null
        fileTotalLines.value = cached.totalLines ?? null
        windowTruncated.value = cached.windowTruncated === true
        updateSlice()
        status.value = 'ready'
        return
      }
    }

    try {
      const url = buildPreviewUrl(newTarget.filePath, win)
      const resp = await apiGet<FileContentResponse>(url, { signal, timeoutMs: 10_000 })
      if (reqId !== currentRequestId) return

      if (resp.isBinary) {
        status.value = 'error'
        errorCode.value = 'binary'
        return
      }

      fileContent.value = resp
      isLargeFile.value = (resp.size ?? 0) > LARGE_FILE_THRESHOLD_BYTES
      fetchedWindow.value = resp.windowStart ? { start: resp.windowStart, end: resp.windowEnd ?? resp.windowStart } : null
      fileTotalLines.value = resp.totalLines ?? null
      windowTruncated.value = resp.windowTruncated === true
      // Large files ARE cached now: only the window is held, not the whole file,
      // so the 2 MiB guard (which existed to keep whole-file content out of the
      // LRU) no longer applies.
      previewCache.set(cacheKey, resp)

      updateSlice()
      status.value = 'ready'
    } catch (err: unknown) {
      if (reqId !== currentRequestId) return
      const errObj = err as { name?: string; msgKey?: string; message?: string; status?: number }
      if (errObj?.name === 'AbortError' || signal.aborted) {
        // Aborted silently
        return
      }
      appLog.w('CodeLinkPreview', 'Failed to fetch file content for preview', { path: newTarget.filePath, error: err })
      // A silent widen must not destroy the slice the user is reading: keep the
      // current content and status, and let pendingWiden reset so a later expand
      // can retry.
      if (opts.silent) return
      status.value = 'error'
      const msgKey = errObj?.msgKey || ''
      const msg = errObj?.message || ''
      if (msgKey === 'FileTooLarge' || errObj?.status === 413) {
        errorCode.value = 'too-large'
      } else if (msgKey === 'NotAFile') {
        errorCode.value = 'not-file'
      } else if (msgKey === 'FileNotFoundShort' || msgKey === 'FileNotFound' || errObj?.status === 404) {
        errorCode.value = 'not-found'
        previewCache.delete(cacheKey)
      } else if (msgKey === 'AccessDenied' || errObj?.status === 403) {
        errorCode.value = 'access-denied'
      } else {
        errorCode.value = 'network'
        errorMessage.value = msg || 'Network error'
      }
    }
  }

  const showPreview = (newTarget: PreviewTarget, previewMode: PreviewMode = 'transient') => {
    if (!enabled.value) return

    if (activePreviewClose && activePreviewClose !== close) {
      activePreviewClose()
    }
    activePreviewClose = close

    const wasPinned = mode.value === 'pinned'
    target.value = newTarget
    // A new target is a different file: drop the previous window/length so the
    // first request is planned from the annotation alone.
    fetchedWindow.value = null
    fileTotalLines.value = null
    windowTruncated.value = false
    mode.value = wasPinned && previewMode !== 'sheet' ? 'pinned' : previewMode
    contextExpansion.value = 0
    extraAboveLines.value = 0
    extraBelowLines.value = 0
    mediaRefreshNonce.value = 0
    visible.value = true

    // Markdown files without a pinned line range default to the rendered
    // document view on every open; anything else (line-annotated paths, code)
    // stays in the source slice view.
    renderMode.value = isMarkdownTarget(newTarget) && !hasLineRange(newTarget) ? 'rendered' : 'source'

    // Once pinned (including after dragging), retain the current placement
    // while switching to another link. The card is reused in-place.
    // Docked panes have no placement to compute.
    if (mode.value !== 'sheet' && mode.value !== 'docked' && !wasPinned) {
      updatePlacement(newTarget.anchorEl)
    }

    fetchPreview(newTarget)
  }

  const close = (opts: { clearCache?: boolean } = {}) => {
    if (activePreviewClose === close) {
      activePreviewClose = null
    }

    if (currentAbortController) {
      currentAbortController.abort()
      currentAbortController = null
    }

    visible.value = false
    status.value = 'idle'
    target.value = null
    fileContent.value = null
    slicedCode.value = null
    fetchedWindow.value = null
    fileTotalLines.value = null
    windowTruncated.value = false
    errorCode.value = null
    errorMessage.value = null
    isLargeFile.value = false
    contextExpansion.value = 0
    extraAboveLines.value = 0
    extraBelowLines.value = 0
    mediaRefreshNonce.value = 0
    placement.value = null
    mode.value = 'transient'
    renderMode.value = 'source'

    if (opts.clearCache) {
      previewCache.clear()
    }
  }

  const pin = () => {
    if (!visible.value || mode.value === 'sheet' || mode.value === 'docked') return
    mode.value = 'pinned'
  }

  const unpin = () => {
    if (!visible.value || mode.value === 'sheet' || mode.value === 'docked') return
    mode.value = 'transient'
  }

  const togglePin = () => {
    if (mode.value === 'pinned') unpin()
    else pin()
  }

  /** Toggle between the rendered document and the source slice (Markdown only). */
  const toggleRenderMode = () => {
    if (!canRenderMarkdown.value) return
    renderMode.value = renderMode.value === 'rendered' ? 'source' : 'rendered'
  }

  const refresh = () => {
    if (!target.value) return
    // Media is not fetched through fetchPreview (no JSON body), so a refresh
    // must instead force the media element to re-request its URL. Bumping this
    // nonce feeds MediaPreviewBody's cache-busting param.
    if (isMediaTarget.value) {
      mediaRefreshNonce.value += 1
      return
    }
    fetchPreview(target.value, true)
  }

  const expandContext = () => {
    contextExpansion.value += 1
    updateSlice()
  }

  const shrinkContext = () => {
    if (contextExpansion.value <= 0 && extraAboveLines.value <= 0 && extraBelowLines.value <= 0) return
    if (contextExpansion.value > 0) contextExpansion.value -= 1
    extraAboveLines.value = Math.max(0, extraAboveLines.value - 5)
    extraBelowLines.value = Math.max(0, extraBelowLines.value - 5)
    updateSlice()
  }

  const expandAbove = (count = 10) => {
    extraAboveLines.value += Math.max(1, count)
    updateSlice()
  }

  const expandBelow = (count = 10) => {
    extraBelowLines.value += Math.max(1, count)
    updateSlice()
  }

  const expandToTop = () => {
    if (!slicedCode.value) return
    const remaining = Math.max(0, slicedCode.value.startLine - 1)
    if (remaining > 0) {
      extraAboveLines.value += remaining
      updateSlice()
    }
  }

  const expandToBottom = () => {
    if (!slicedCode.value) return
    const remaining = Math.max(0, slicedCode.value.totalLines - slicedCode.value.endLine)
    if (remaining > 0) {
      extraBelowLines.value += remaining
      updateSlice()
    }
  }

  const openFull = () => {
    if (!target.value) return
    const { filePath, lineStart, lineEnd, lineRanges } = target.value
    options.onBeforeOpen?.()
    if (lineRanges) {
      openFilePath(filePath, lineStart, lineEnd, options.source, lineRanges)
    } else {
      openFilePath(filePath, lineStart, lineEnd, options.source)
    }
    close()
  }

  // Card pointer/focus events. These used to feed a transient auto-close
  // timer; since click-opened cards now persist until an explicit dismiss
  // (Esc / close button / open full / tab or file switch / replace), they are
  // kept as no-ops so template bindings and the component surface stay stable.
  const onCardPointerEnter = () => {}
  const onCardPointerLeave = () => {}
  const onCardFocusIn = () => {}
  const onCardFocusOut = (_e: FocusEvent) => {}

  // Target extraction helper
  const extractTargetFromElement = (el: HTMLElement): PreviewTarget | null => {
    const targetEl = el.closest<HTMLElement>('.chat-file-path[data-file-path], .chat-file-open-btn[data-file-path]')
    if (!targetEl) return null

    // Check verification status: must be file, not dir or unverified
    const pathType = targetEl.getAttribute('data-path-type')
    if (pathType !== 'file') return null

    const filePath = targetEl.getAttribute('data-file-path')
    if (!filePath) return null

    const startAttr = targetEl.getAttribute('data-line-start')
    const endAttr = targetEl.getAttribute('data-line-end')
    const lineStart = startAttr ? parseInt(startAttr, 10) : undefined
    const lineEnd = endAttr ? parseInt(endAttr, 10) : undefined
    const lineRanges = targetEl.getAttribute('data-line-ranges') || undefined

    return {
      filePath,
      lineStart,
      lineEnd,
      lineRanges,
      anchorEl: targetEl,
    }
  }

  const handleClick = (e: MouseEvent) => {
    if (!enabled.value) return

    const targetEl = (e.target as HTMLElement)?.closest<HTMLElement>('.chat-file-path[data-file-path], .chat-file-open-btn[data-file-path]')
    if (!targetEl) return

    const extracted = extractTargetFromElement(targetEl)
    if (!extracted) return

    const isTouch = isTouchDevice()
    const isModifier = !isTouch && (e.ctrlKey || e.metaKey)

    // 1. Ctrl / Cmd + Click: toggle or replace with pinned preview (desktop only)
    if (isModifier) {
      e.preventDefault()
      e.stopPropagation()
      showPreview(extracted, 'pinned')
      return
    }

    // Desktop path clicks open the preview instead of navigating/opening the
    // file. The open-button keeps its existing "open file" behavior unless
    // Ctrl/Cmd is held.
    if (!isTouch && targetEl.classList.contains('chat-file-path')) {
      e.preventDefault()
      e.stopPropagation()
      showPreview(extracted, 'transient')
      return
    }

    // 2. Touch tap on path text -> open BottomSheet
    if (isTouch) {
      const isPathText = targetEl.classList.contains('chat-file-path')
      if (isPathText) {
        e.preventDefault()
        e.stopPropagation()
        showPreview(extracted, 'sheet')
        return
      }
      // Tap on open button -> let ordinary handleClick in MarkdownPreview handle openFilePath
    }
  }

  // Bind delegation to container. Only the click listener does real work today
  // (mouse/focus listeners were removed with the transient hover-close logic).
  const bindEvents = (el: HTMLElement | null) => {
    if (!el) return
    el.addEventListener('click', handleClick, true)
  }

  const unbindEvents = (el: HTMLElement | null) => {
    if (!el) return
    el.removeEventListener('click', handleClick, true)
  }

  if (containerRef) {
    watch(
      () => containerRef.value,
      (newEl, oldEl) => {
        if (oldEl) unbindEvents(oldEl)
        if (newEl && enabled.value) bindEvents(newEl)
      },
      { immediate: true }
    )
  }

  // Watch switch state: when turned off, close immediately and clear cache
  watch(enabled, (on) => {
    if (!on) {
      close({ clearCache: true })
      if (containerRef?.value) unbindEvents(containerRef.value)
    } else {
      if (containerRef?.value) bindEvents(containerRef.value)
    }
  })

  // Watch file changes in store: close preview when user switches Markdown file
  watch(
    () => store.state.currentFile?.path,
    () => {
      close({ clearCache: false })
    }
  )

  if (getCurrentInstance()) {
    onUnmounted(() => {
      close()
      if (containerRef?.value) unbindEvents(containerRef.value)
    })
  }

  return {
    enabled,
    outsideClickIgnoreSelector,
    visible,
    status,
    mode,
    isPinned,
    isDocked,
    target,
    fileContent,
    slicedCode,
    errorCode,
    errorMessage,
    isLargeFile,
    windowTruncated,
    contextExpansion,
    extraAboveLines,
    extraBelowLines,
    placement,
    renderMode,
    isMarkdown,
    hasExplicitLineRange,
    canRenderMarkdown,
    effectiveRenderMode,
    isImageTarget,
    isVideoTarget,
    isAudioTarget,
    isPdfTarget,
    isMediaTarget,
    mediaRefreshNonce,
    showPreview,
    close,
    pin,
    unpin,
    togglePin,
    toggleRenderMode,
    refresh,
    expandContext,
    shrinkContext,
    expandAbove,
    expandBelow,
    expandToTop,
    expandToBottom,
    openFull,
    onCardPointerEnter,
    onCardPointerLeave,
    onCardFocusIn,
    onCardFocusOut,
    handleClick,
    isTouchDevice,
    updatePlacement,
    bindEvents,
    unbindEvents,
  }
}

/**
 * Minimal surface of useCodeLinkPreview() consumed by the shared click
 * interceptor. Kept as a structural interface so containers (chat, markdown
 * preview, task prompt / execution detail) do not need to name the full
 * composable return type.
 */
export interface CodeLinkPreviewController {
  enabled: { value: boolean }
  isTouchDevice: () => boolean
  handleClick: (event: MouseEvent) => void
}

/**
 * Shared interceptor for clicks on verified file-path annotations
 * (`.chat-file-path[data-file-path]` with `data-path-type="file"`).
 *
 * The composable binds a capture-phase click listener once its container ref is
 * mounted; until that binding is in place this helper is the fallback used by
 * container-level click handlers (chat / markdown preview / task views), so
 * every surface shares one decision instead of four copies.
 *
 * Only verified *file* paths are intercepted — directories and not-yet-verified
 * paths return false and fall through to the container's original handlers.
 * Returns true when the event was handled by the preview (open it).
 */
export function handleVerifiedFilePathClick(event: MouseEvent, preview: CodeLinkPreviewController): boolean {
  if (!preview.enabled.value) return false
  const isTouch = preview.isTouchDevice()
  const isModifier = !isTouch && (event.ctrlKey || event.metaKey)
  const target = event.target as HTMLElement | null
  const linkOrBtn = target?.closest<HTMLElement>('.chat-file-path[data-file-path], .chat-file-open-btn[data-file-path]') ?? null
  const pathEl = target?.closest<HTMLElement>('.chat-file-path[data-file-path]') ?? null
  const isVerifiedFile = linkOrBtn?.getAttribute('data-path-type') === 'file'
  // Desktop: modifier-click on either the path text or the open button pins the
  // preview; plain click on the path text opens a transient preview.
  if (isVerifiedFile && ((isModifier && linkOrBtn) || (!isTouch && pathEl))) {
    preview.handleClick(event)
    return true
  }
  // Touch: tapping the path text opens the bottom-sheet preview.
  if (isVerifiedFile && isTouch && pathEl) {
    preview.handleClick(event)
    return true
  }
  return false
}
