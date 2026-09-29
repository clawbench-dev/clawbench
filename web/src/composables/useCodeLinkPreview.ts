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
import { joinPath } from '@/utils/path'
import { buildDirListUrl } from '@/utils/dirList'
import { getFileType } from '@/utils/fileType'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import type { NavigationSurface } from '@/composables/useNavigationContext'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import type { DirPreviewEntry } from '@/composables/useDirPreview'
import {
  sliceCodeForPreview,
  computeRenderWindow,
  computeFetchWindow,
  windowCovers,
  nextLoadWindow,
  mergeLineWindows,
  resolveResponseWindow,
  buildPreviewUrl,
  placeNearAnchor,
  previewCache,
  DEFAULT_NO_RANGE_LINES,
  LARGE_FILE_THRESHOLD_BYTES,
  type CodeSliceResult,
  type FileContentResponse,
  type FetchWindow,
  type LoadedLineWindow,
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
  /**
   * The verified annotation is a directory, not a file. Directories have no
   * content to fetch, so the card renders a listing instead of a code slice.
   */
  isDir?: boolean
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

  // ── Directory targets ───────────────────────────────────────────────────
  // A directory annotation has no file content, so the card lists the
  // directory instead of slicing code. This mirrors the file manager's docked
  // pane (useDirPreview + DirPreviewBody) so both surfaces show the same
  // control; the listing is fetched from the same /api/dir endpoint.
  //
  // Declared BEFORE the file-type computeds below so they can defer to it.
  const isDirTarget = computed(() => target.value?.isDir === true)

  // `getFileType` is purely extension-based, so a DIRECTORY named `assets.png`
  // or `docs.md` looks like a media/markdown file. The verified `isDir` flag is
  // authoritative (it came from the server's stat), so every extension-based
  // classification defers to it. Without this, such a directory would render a
  // media body or a bogus markdown toggle instead of its listing.
  const isMarkdown = computed(() => {
    if (isDirTarget.value) return false
    const filePath = target.value?.filePath || ''
    return filePath ? Boolean(getFileType(filePath).isMarkdown) : false
  })

  const dirEntries = ref<DirPreviewEntry[]>([])
  const dirLoading = ref(false)
  const dirError = ref(false)
  /** Directory the current listing belongs to (guards out-of-order responses). */
  const dirLoadedPath = ref('')
  let dirSeq = 0

  const loadDir = async (path: string) => {
    const mySeq = ++dirSeq
    dirLoading.value = true
    dirError.value = false
    try {
      const url = buildDirListUrl(path)
      const data = await apiGet<{ items: DirPreviewEntry[] }>(url, { timeoutMs: 10_000 })
      if (mySeq !== dirSeq) return
      dirEntries.value = data.items || []
      dirLoadedPath.value = path
    } catch (err) {
      if (mySeq !== dirSeq) return
      appLog.w('CodeLinkPreview', 'Failed to list directory for preview', { path, error: err })
      dirEntries.value = []
      dirError.value = true
      dirLoadedPath.value = path
    } finally {
      if (mySeq === dirSeq) dirLoading.value = false
    }
  }

  /** Hidden entries are filtered at render time so the toolbar toggle applies
   *  without a refetch — same rule as useDirPreview. */
  const dirEntryVisible = (entry: DirPreviewEntry): boolean =>
    localConfig.showHidden === true || !entry.name.startsWith('.')

  /** Fetch the target's directory listing. Called by showPreview and refresh. */
  const fetchDirPreview = (dirPath: string) => {
    dirEntries.value = []
    dirLoadedPath.value = ''
    void loadDir(dirPath)
  }

  /**
   * The card listed a directory and the user picked a child directory: hand off
   * to the file manager, exactly as the docked pane does (its listing becomes
   * the main list, so keeping the card open would just duplicate it).
   *
   * With no `name` this opens the listed directory itself — what the card's
   * "open directory" button does, as opposed to revealing its parent.
   */
  const openDirChild = (name?: string) => {
    const parent = target.value?.filePath || ''
    const full = name ? joinPath(parent, name) : parent
    if (!full) return
    close()
    window.dispatchEvent(new CustomEvent('open-directory-from-context', {
      detail: { path: full, source: options.source },
    }))
  }

  /**
   * The card listed a directory and the user picked a file: open it in the
   * full-screen viewer, matching the docked pane and a double-click in the list.
   */
  const openDirFile = (name: string) => {
    const parent = target.value?.filePath || ''
    close()
    options.onBeforeOpen?.()
    void openFilePath(joinPath(parent, name), undefined, undefined, options.source)
  }

  // ── Media targets (image / SVG / video / audio / PDF) ────────────────────
  // These are served as raw bytes by /api/fs/raw/ (correct MIME, no size
  // cap, inline), NOT by /api/file — which is JSON, 10 MiB-capped and reports
  // every raster image as binary. The preview short-circuits the fetch for
  // them and renders a media body straight from the URL.
  const fileType = computed(() => {
    if (isDirTarget.value) return null
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
  // `isMarkdown` and `isMediaTarget` already return false for a directory target
  // (see their definitions), so this stays a plain alias.
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

  // The contiguous run of file lines currently held client-side. Grown chunk by
  // chunk as the user scrolls (see loadMore), so a long file is walked instead
  // of clipped. `heldWindow` is the same range in the plain FetchWindow shape
  // the cache and the coverage checks use.
  const heldLines = ref<LoadedLineWindow | null>(null)
  const heldWindow = computed<FetchWindow | null>(() => {
    const h = heldLines.value
    if (!h || h.endLine < h.startLine) return null
    return { start: h.startLine, end: h.endLine }
  })
  /** Total lines in the file, known once a windowed response arrives. */
  const fileTotalLines = ref<number | null>(null)
  /**
   * The server hit its own byte cap while collecting a window, so the returned
   * content is short (or empty, when a single line exceeded the cap). Loading
   * further cannot help — the pane surfaces a notice instead.
   */
  const windowTruncated = ref(false)
  /** True while a scroll-driven chunk fetch is in flight. */
  const loadingMore = ref(false)
  /** Guards the load loop so overlapping scroll events cannot stack fetches. */
  let loadLoopRunning = false

  const updateSlice = () => {
    const held = heldLines.value
    if (!held || held.endLine < held.startLine) {
      // Nothing to slice yet (no content held, or the server captured none).
      // Reporting the empty window keeps the pane honest while loadMore
      // recovers it; only the very first load has nothing to report at all.
      if (!held && fileTotalLines.value === null) {
        slicedCode.value = null
        return
      }
      const start = held?.startLine ?? 1
      const total = fileTotalLines.value
      slicedCode.value = {
        code: '',
        startLine: start,
        endLine: start - 1,
        totalLines: total ?? start,
        // An empty file has no lines for an annotation to be out of range OF —
        // the notice means "your line reference is past EOF", not "this file is
        // blank". sliceCodeForPreview makes the same distinction for the
        // non-empty-window path; this branch has to repeat it because it builds
        // the slice itself.
        lineOutOfRange: total !== 0 && !windowTruncated.value,
        renderTruncated: false,
      }
      return
    }

    const lineRanges = target.value?.lineRanges ? parseLineRanges(target.value.lineRanges) : undefined
    const sliceOptions = {
      contextExpansion: contextExpansion.value,
      expandAboveLines: extraAboveLines.value,
      expandBelowLines: extraBelowLines.value,
      lineRanges,
    }

    slicedCode.value = sliceCodeForPreview(
      held.content,
      target.value?.lineStart,
      target.value?.lineEnd,
      {
        ...sliceOptions,
        baseLineOffset: held.startLine,
        // A windowed response reports the true file length; prefer it over the
        // locally-derived count, which only sees the held lines.
        totalLines: fileTotalLines.value ?? undefined,
      }
    )
  }

  /**
   * Loading more lines can never extend the slice once a byte ceiling has cut
   * it short: the slice always walks forward from its own start until the cap
   * trips, so extra content just gets cut at the same place. Stopping here is
   * what keeps scroll events from hammering the network for nothing.
   */
  const loadMoreBlocked = computed(() => slicedCode.value?.renderTruncated === true)

  /**
   * The absolute line range the pane wants rendered right now — the render
   * window, which the scroll handlers grow via expandBelow / expandAbove.
   */
  const wantedRange = (): FetchWindow | null => {
    const total = fileTotalLines.value
    if (total !== null && total <= 0) return null
    // Before the first response the file length is unknown; plan against what is
    // held so the window stays meaningful. It is re-planned against the real
    // count as soon as the server reports it.
    const planTotal = total ?? Math.max(heldLines.value?.endLine ?? 0, DEFAULT_NO_RANGE_LINES)
    const lineRanges = target.value?.lineRanges ? parseLineRanges(target.value.lineRanges) : undefined
    const win = computeRenderWindow(
      target.value?.lineStart,
      target.value?.lineEnd,
      planTotal,
      {
        contextExpansion: contextExpansion.value,
        expandAboveLines: extraAboveLines.value,
        expandBelowLines: extraBelowLines.value,
        lineRanges,
      }
    )
    return { start: win.startLine, end: win.endLine }
  }

  /**
   * Fetch one more chunk of lines to move `heldLines` toward the wanted range.
   * Returns true when a chunk arrived.
   *
   * The loop in loadMore calls this repeatedly so a single gesture that
   * uncovers several screens keeps filling without further input.
   */
  const fetchChunk = async (): Promise<boolean> => {
    const wanted = wantedRange()
    if (!wanted) return false

    const next = nextLoadWindow(heldLines.value, wanted.start, wanted.end)
    if (!next) return false

    const filePath = target.value?.filePath
    if (!filePath) return false

    const projectRoot = store.state.projectRoot || ''
    const cacheKey = previewCache.buildKey(projectRoot, filePath, next)
    const cached = previewCache.get(cacheKey)
    if (cached) {
      applyChunk(toLoadedWindow(cached))
      return true
    }

    const reqId = currentRequestId
    try {
      const url = buildPreviewUrl(filePath, next)
      const resp = await apiGet<FileContentResponse>(url, {
        signal: currentAbortController?.signal,
        timeoutMs: 10_000,
      })
      // A newer target/refresh superseded this chunk: drop it.
      if (reqId !== currentRequestId) return false
      if (resp.isBinary) return false

      previewCache.set(cacheKey, resp)
      applyChunk(toLoadedWindow(resp))
      return true
    } catch (err: unknown) {
      const errObj = err as { name?: string }
      if (errObj?.name === 'AbortError') return false
      appLog.w('CodeLinkPreview', 'Failed to load more lines for preview', {
        path: filePath,
        window: next,
        error: err,
      })
      return false
    }
  }

  /**
   * The absolute line run a chunk response covers, using the response's own
   * window metadata when present. A whole-file response (no window metadata) is
   * held from line 1 — the same rule seedHeldContent applies to the opening
   * fetch — rather than being read as an empty window at the requested start.
   */
  const toLoadedWindow = (resp: FileContentResponse): LoadedLineWindow => {
    const resolved = resolveResponseWindow(resp)
    return { content: resp.content, startLine: resolved.startLine, endLine: resolved.endLine }
  }

  /** Merge a fetched chunk in and re-slice. */
  const applyChunk = (chunk: LoadedLineWindow) => {
    heldLines.value = mergeLineWindows(heldLines.value, chunk)
    if (chunk.endLine >= chunk.startLine) {
      // A real response carries the file's true length; it is authoritative over
      // the locally-derived count, which only ever sees what is held.
      fileTotalLines.value = fileTotalLines.value ?? heldLines.value.endLine
    }
    updateSlice()
  }

  /**
   * Load chunks until the wanted range is covered, no further progress is
   * possible, or the byte ceiling has capped the slice. Safe to call from every
   * scroll event: the in-flight guard collapses overlapping calls into one loop.
   */
  const loadMore = async (): Promise<void> => {
    if (loadLoopRunning || loadMoreBlocked.value || windowTruncated.value) return
    loadLoopRunning = true
    loadingMore.value = true
    try {
      // Bounded so a pathological file cannot spin here: each pass fetches at
      // most one chunk and the wanted range never moves inside this loop.
      for (let guard = 0; guard < 64; guard++) {
        if (loadMoreBlocked.value || windowTruncated.value) break
        const grew = await fetchChunk()
        if (!grew) break
        const wanted = wantedRange()
        const held = heldWindow.value
        if (!wanted || !held || windowCovers(held, wanted.start, wanted.end)) break
      }
    } finally {
      loadLoopRunning = false
      loadingMore.value = false
    }
  }

  /** Drop everything held for the previous target (new file, media, directory). */
  const resetHeldContent = () => {
    heldLines.value = null
    fileContent.value = null
    slicedCode.value = null
    fileTotalLines.value = null
    windowTruncated.value = false
  }

  /**
   * Adopt a freshly fetched (or cached) response as the run of lines held.
   *
   * The response's own windowStart / windowEnd are authoritative, because the
   * server may clamp the requested range or capture no lines at all (the
   * empty-window encoding). A response with NO window metadata is the whole
   * file — the server ignores the line window on non-text / sanitized paths —
   * so it is held in file coordinates and its length becomes the known total
   * (see resolveResponseWindow).
   */
  const seedHeldContent = (resp: FileContentResponse) => {
    const resolved = resolveResponseWindow(resp)
    heldLines.value = {
      content: resp.content,
      startLine: resolved.startLine,
      endLine: resolved.endLine,
    }
    fileTotalLines.value = resolved.totalLines
    windowTruncated.value = resp.windowTruncated === true
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

    // A silent fetch keeps the current slice on screen instead of flipping back
    // to the loading spinner.
    if (!opts.silent) {
      status.value = 'loading'
      errorCode.value = null
      errorMessage.value = null
      isLargeFile.value = false
    }

    // Directories have no file content: the card lists them instead. The listing
    // lives in dirEntries, so there is nothing to slice.
    //
    // This MUST be checked before isMediaTarget: getFileType() is purely
    // extension-based, so a DIRECTORY named `assets.png` or `docs.md` looks like
    // a media/markdown file. The verified `isDir` flag is authoritative — it came
    // from the server's stat — so it wins over the extension guess. Checking
    // media first would swallow the directory and never fetch its listing.
    if (isDirTarget.value) {
      resetHeldContent()
      fetchDirPreview(newTarget.filePath)
      status.value = 'ready'
      return
    }

    // Media files are served as raw bytes by /api/fs/raw/ and rendered
    // straight from that URL — there is no JSON content to fetch, and /api/file
    // would reject every raster image as binary (10 MiB cap + null-byte sniff).
    // Go straight to 'ready' so the media body can mount.
    if (isMediaTarget.value) {
      resetHeldContent()
      status.value = 'ready'
      return
    }

    // Ask for just the lines this preview can render (plus margin), so a large
    // file is never transferred in full. Later growth is incremental
    // (fetchChunk / loadMore) instead of one ever-wider request.
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
        seedHeldContent(cached)
        updateSlice()
        status.value = 'ready'
        return
      }
    }

    try {
      const url = buildPreviewUrl(newTarget.filePath, win)
      const resp = await apiGet<FileContentResponse>(url, { signal, timeoutMs: 10_000 })
      if (reqId !== currentRequestId) return

      // Record the response before the binary early-return: the unsupported
      // placeholder shows the file's size, and a binary response is the only
      // source of it (there is no content to slice).
      fileContent.value = resp

      if (resp.isBinary) {
        status.value = 'error'
        errorCode.value = 'binary'
        return
      }

      isLargeFile.value = (resp.size ?? 0) > LARGE_FILE_THRESHOLD_BYTES
      seedHeldContent(resp)
      // Large files ARE cached now: only the window is held, not the whole file,
      // so the 2 MiB guard (which existed to keep whole-file content out of the
      // LRU) no longer applies.
      previewCache.set(cacheKey, resp)

      updateSlice()
      status.value = 'ready'
      // The opening window does not always cover what the slice wants to show:
      // an annotation past EOF gets an empty window, and a server-clamped window
      // can fall short. A blank pane never scrolls, so the body cannot recover
      // either case — pull the missing lines here instead. In the ordinary case
      // the window already covers the render window and this is a no-op.
      void loadMore()
    } catch (err: unknown) {
      if (reqId !== currentRequestId) return
      const errObj = err as { name?: string; msgKey?: string; message?: string; status?: number }
      if (errObj?.name === 'AbortError' || signal.aborted) {
        // Aborted silently
        return
      }
      appLog.w('CodeLinkPreview', 'Failed to fetch file content for preview', { path: newTarget.filePath, error: err })
      // A silent fetch must not destroy the slice the user is reading: keep the
      // current content and status.
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
    resetHeldContent()
    loadingMore.value = false
    // Retargeting away from a directory (or onto another one) must not leave the
    // previous listing on screen while the new one loads.
    dirSeq += 1
    dirEntries.value = []
    dirLoadedPath.value = ''
    dirLoading.value = false
    dirError.value = false
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
    resetHeldContent()
    // Drop the listing and invalidate any in-flight fetch so a stale response
    // can't repopulate the card after it was closed.
    dirSeq += 1
    dirEntries.value = []
    dirLoadedPath.value = ''
    dirLoading.value = false
    dirError.value = false
    errorCode.value = null
    errorMessage.value = null
    isLargeFile.value = false
    contextExpansion.value = 0
    extraAboveLines.value = 0
    extraBelowLines.value = 0
    loadingMore.value = false
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
    // A directory listing is its own fetch; re-run it rather than re-slicing.
    // Checked before media for the same reason as fetchPreview: a directory
    // named `assets.png` must not bump the media nonce instead of re-listing.
    if (isDirTarget.value) {
      fetchDirPreview(target.value.filePath)
      return
    }
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

  /**
   * Grow the requested window and pull in whatever lines that needs.
   *
   * These are async because growing past the held run requires a fetch: the
   * scroll-anchoring caller has to await the content landing before it can
   * measure the inserted height.
   */
  const expandAbove = async (count = 10) => {
    extraAboveLines.value += Math.max(1, count)
    updateSlice()
    await loadMore()
  }

  const expandBelow = async (count = 10) => {
    extraBelowLines.value += Math.max(1, count)
    updateSlice()
    await loadMore()
  }

  const expandToTop = async () => {
    if (!slicedCode.value) return
    const remaining = Math.max(0, slicedCode.value.startLine - 1)
    if (remaining > 0) {
      extraAboveLines.value += remaining
      updateSlice()
      await loadMore()
    }
  }

  const expandToBottom = async () => {
    if (!slicedCode.value) return
    const remaining = Math.max(0, slicedCode.value.totalLines - slicedCode.value.endLine)
    if (remaining > 0) {
      extraBelowLines.value += remaining
      updateSlice()
      await loadMore()
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

    // Only verified paths qualify: "file" previews its content, "dir" previews
    // its listing. Unverified paths (no data-path-type yet) return null and fall
    // through to the container's original handler.
    const pathType = targetEl.getAttribute('data-path-type')
    if (pathType !== 'file' && pathType !== 'dir') return null

    const filePath = targetEl.getAttribute('data-file-path')
    if (!filePath) return null

    // Line annotations are meaningless for a directory; ignore any suffix.
    if (pathType === 'dir') {
      return { filePath, isDir: true, anchorEl: targetEl }
    }

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
    // Scroll-driven growth: the pane asks for more lines as the user reaches
    // the end of what is loaded.
    loadingMore,
    loadMore,
    loadMoreBlocked,
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
    // Directory targets (the card lists the directory instead of slicing code).
    isDirTarget,
    dirEntries,
    dirLoading,
    dirError,
    dirLoadedPath,
    dirEntryVisible,
    openDirChild,
    openDirFile,
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
 * Shared interceptor for clicks on verified path annotations
 * (`.chat-file-path[data-file-path]` with `data-path-type` of `file` or `dir`).
 *
 * The composable binds a capture-phase click listener once its container ref is
 * mounted; until that binding is in place this helper is the fallback used by
 * container-level click handlers (chat / markdown preview / task views), so
 * every surface shares one decision instead of five copies.
 *
 * Both verified types are intercepted: a file previews its content, a directory
 * previews its listing. Only unverified paths (no `data-path-type` yet) return
 * false and fall through to the container's original handlers.
 * Returns true when the event was handled by the preview (open it).
 */
export function handleVerifiedFilePathClick(event: MouseEvent, preview: CodeLinkPreviewController): boolean {
  if (!preview.enabled.value) return false
  const isTouch = preview.isTouchDevice()
  const isModifier = !isTouch && (event.ctrlKey || event.metaKey)
  const target = event.target as HTMLElement | null
  const linkOrBtn = target?.closest<HTMLElement>('.chat-file-path[data-file-path], .chat-file-open-btn[data-file-path]') ?? null
  const pathEl = target?.closest<HTMLElement>('.chat-file-path[data-file-path]') ?? null
  const pathType = linkOrBtn?.getAttribute('data-path-type')
  const isVerifiedPath = pathType === 'file' || pathType === 'dir'
  // Desktop: modifier-click on either the path text or the open button pins the
  // preview; plain click on the path text opens a transient preview.
  if (isVerifiedPath && ((isModifier && linkOrBtn) || (!isTouch && pathEl))) {
    preview.handleClick(event)
    return true
  }
  // Touch: tapping the path text opens the bottom-sheet preview.
  if (isVerifiedPath && isTouch && pathEl) {
    preview.handleClick(event)
    return true
  }
  return false
}
