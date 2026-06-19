/**
 * useFileRefresh — shared logic for refreshing the currently viewed file
 * while preserving scroll position and highlighting changes.
 *
 * Used by three independent refresh triggers:
 * 1. Manual refresh (refresh button in FileHeader / FileManager)
 * 2. fsnotify auto-refresh (useFileWatch SSE file_change event)
 * 3. Chat-driven refresh (ChatPanel onFileModified callback)
 *
 * Two highlight mechanisms within a unified flow:
 * - Markdown rendered mode: block-level diff markers (no flash animation)
 * - Code/raw files: line-level diff + two-phase flash (red delete → blue add)
 */
import { ref, watch } from 'vue'
import { store } from '@/stores/app.ts'
import { computeDiff, wholeLineRanges } from '@/utils/diffUtils.ts'
import type { LineDiff } from '@/utils/diffUtils.ts'
import {
  computeMarkdownDiff,
  offscreenExtractBlocks,
  diffMarkers,
  diffOldContent,
  diffOldFilePath,
  clearDiffMarkers,
  extractBlocks,
  computeCodeDiffMarkers,
  type DiffMarker,
  type BlockInfo,
  type DiffResult,
} from '@/composables/useMarkdownDiff.ts'
import { getFileType } from '@/utils/fileType.ts'

// ─── Flash state (consumed by CodePreview for code/raw files) ───

export type FlashType = 'delete' | 'add'

/** A range of characters to highlight within a single line (0-based, inclusive start, exclusive end) */
export interface FlashRange {
    line: number   // 1-based line number
    start: number   // 0-based char offset within the line (string offset, not char index)
    end: number     // 0-based char offset (exclusive; use Infinity for "rest of line")
}

/**
 * Reactive flash ranges — CodePreview reads this to wrap characters
 * in <span class="char-flash-{type}"> during rendering.
 *
 * IMPORTANT: Must always be reassigned (not mutated in-place) for Vue
 * reactivity to trigger the watch in CodePreview.
 */
export const flashRanges = ref<FlashRange[]>([])
export const flashType = ref<FlashType>('add')
let flashTimer: ReturnType<typeof setTimeout> | null = null

// Generation counter to prevent race conditions with concurrent refreshCurrentFile calls
let refreshGeneration = 0
// Whether a refresh is currently in-flight
let refreshing = false
// If a new refresh arrives while one is in-flight, we defer it
let pendingRefreshOptions: { loadDir?: boolean; clearOnError?: boolean } | null = null

function clearFlash() {
    if (flashTimer) { clearTimeout(flashTimer); flashTimer = null }
    flashRanges.value = []
    flashType.value = 'add'
}

function scheduleClearFlash(ms: number) {
    if (flashTimer) clearTimeout(flashTimer)
    flashTimer = setTimeout(() => { flashRanges.value = []; flashType.value = 'add'; flashTimer = null }, ms)
}

// ─── Scroll helpers ───

function getScrollContainer(): HTMLElement | null {
  return (document.querySelector('.markdown-body') || document.querySelector('.raw-content-pre')) as HTMLElement | null
}

/**
 * Save scroll position as { scrollTop, lineHeight } so we can restore it
 * even after content changes alter scrollHeight. Uses pixel scrollTop directly
 * rather than a ratio, which breaks when content is added/removed.
 */
function getScrollPosition(): number {
  const el = getScrollContainer()
  return el ? el.scrollTop : 0
}

/**
 * Restore scroll position after content update.
 * Uses the saved scrollTop pixel value directly — this keeps the viewport
 * anchored to the same absolute position. If content was added above the
 * viewport, the user sees new content scroll in from the top, which is
 * the natural behavior. If content was removed above, the viewport
 * stays at the same absolute offset.
 */
function restoreScrollPosition(scrollTop: number): void {
  if (scrollTop <= 0) return
  const startTime = Date.now()
  const MAX_WAIT = 3000

  function tryRestore() {
    const el = getScrollContainer()
    if (!el) {
      if (Date.now() - startTime < MAX_WAIT) requestAnimationFrame(tryRestore)
      return
    }
    // Only restore if the content is tall enough
    if (el.scrollHeight - el.clientHeight <= 0) {
      if (Date.now() - startTime < MAX_WAIT) requestAnimationFrame(tryRestore)
      return
    }
    el.scrollTop = Math.min(scrollTop, el.scrollHeight - el.clientHeight)
  }
  requestAnimationFrame(() => requestAnimationFrame(tryRestore))
}

// ─── Pre-fetch helper (does NOT update store) ───

async function prefetchFileContent(path: string): Promise<string | null> {
    try {
        const resp = await fetch(`/api/file/${encodeURIComponent(path)}`)
        if (!resp.ok) return null
        const data = await resp.json()
        // Don't try to diff binary or too-large files
        if (data.isBinary || data.tooLarge || data.error) return null
        return data.content ?? null
    } catch {
        return null
    }
}

// ─── Is current file a markdown file? ───

function isCurrentFileMarkdown(): boolean {
    const name = store.state.currentFile?.name
    if (!name) return false
    return getFileType(name)?.isMarkdown || false
}

/**
 * Check if a markdown file is currently displayed in rendered mode
 * (vs raw/code mode). Returns true when `.markdown-body .markdown-content`
 * is present in the DOM.
 */
function isMarkdownRenderedMode(): boolean {
    return !!document.querySelector('.markdown-body .markdown-content')
}

/**
 * Get old block list from the live DOM for markdown block-level diff.
 */
function getOldBlockList() {
    const content = document.querySelector('.markdown-body .markdown-content') || document.querySelector('.markdown-body')
    if (!content) return []
    return extractBlocks(content)
}

// ─── Clear flash on file navigation ───

watch(() => store.state.currentFile?.path, (newPath, oldPath) => {
    if (newPath !== oldPath) {
        clearFlash()
        clearDiffMarkers()
    }
})

// ─── Main refresh function ───

const DELETE_FLASH_MS = 1200
const ADD_FLASH_CLEAR_MS = 2000

/**
 * Refresh the currently viewed file content while preserving scroll position.
 *
 * Unified flow:
 * - Markdown rendered mode: block-level diff + persistent markers (no flash)
 * - Code/raw files: line-level diff + two-phase flash (red delete → blue add) + persistent markers
 *
 * Deduplication: if a refresh is already in-flight, the new call is deferred
 * and merged — when the current refresh completes, one more refresh runs with
 * the latest options.  This prevents concurrent refreshes from fighting over
 * flashRanges / diffMarkers (which caused the "first edit misses deletion
 * flash and markers" bug).
 *
 * @param options.loadDir - Also refresh the directory listing (default: false)
 * @param options.clearOnError - If the file fails to load, clear currentFile (default: false)
 */
export async function refreshCurrentFile(options: {
  loadDir?: boolean
  clearOnError?: boolean
} = {}): Promise<void> {
  // Dedup: if a refresh is already running, just remember to do another one after
  if (refreshing) {
    // Merge: if either wants loadDir or clearOnError, keep that
    if (pendingRefreshOptions) {
      if (options.loadDir) pendingRefreshOptions.loadDir = true
      if (options.clearOnError) pendingRefreshOptions.clearOnError = true
    } else {
      pendingRefreshOptions = { ...options }
    }
    return
  }
  refreshing = true

  try {
    await doRefreshCurrentFile(options)
  } finally {
    refreshing = false
    // If a refresh was deferred while we were running, execute it now
    if (pendingRefreshOptions) {
      const opts = pendingRefreshOptions
      pendingRefreshOptions = null
      await refreshCurrentFile(opts)
    }
  }
}

/**
 * Internal implementation — always runs serially (caller ensures dedup).
 *
 * Unified flow for both markdown and code files:
 *   1. Common preamble (save old content, scroll ratio, prefetch)
 *   2. Diff computation (branch: block-level for markdown, line-level for code)
 *   3. Phase 1: Red-flash deletions (code only)
 *   4. Phase 2: Store update (common)
 *   5. Apply markers (common)
 *   6. Phase 3: Blue-flash additions (code only)
 *   7. Restore scroll (common)
 */
async function doRefreshCurrentFile(options: {
  loadDir?: boolean
  clearOnError?: boolean
}): Promise<void> {
  const { loadDir = false, clearOnError = false } = options
  const gen = ++refreshGeneration

  const currentFilePath = store.state.currentFile?.path
  const currentFile = store.state.currentFile

  // Save old content for change detection
  const oldContent = currentFile?.content ?? null
  const oldPath = currentFilePath

  // Save scroll position before refresh
  const savedScrollTop = getScrollPosition()

  // Refresh directory listing if requested
  if (loadDir && store.state.currentDir !== undefined) {
    store.loadFiles(store.state.currentDir)
  }

  if (!currentFilePath) return

  // Determine diff mode
  const isMarkdown = isCurrentFileMarkdown() && isMarkdownRenderedMode()

  // ─── Phase 0: Pre-fetch + diff computation ───

  const newContent = await prefetchFileContent(currentFilePath)
  if (gen !== refreshGeneration) return

  let diffResult: LineDiff | null = null
  let codeMarkers: DiffMarker[] | null = null
  let markdownResult: DiffResult | null = null
  let hasDeletions = false

  if (isMarkdown) {
      // Block-level diff for rendered markdown
      const oldBlocks = getOldBlockList()
      const newBlocks: BlockInfo[] = newContent !== null ? offscreenExtractBlocks(newContent) : []
      markdownResult = (oldBlocks.length > 0 && newBlocks.length > 0 && newContent !== oldContent)
          ? computeMarkdownDiff(oldBlocks, newBlocks)
          : null
  } else if (oldContent) {
      // Line-level diff for code/raw files
      if (newContent !== null && newContent !== oldContent) {
          diffResult = computeDiff(oldContent, newContent)
          hasDeletions = diffResult.deletedInOld.length > 0 || diffResult.deletedChars.size > 0
          codeMarkers = computeCodeDiffMarkers(diffResult, oldContent, newContent)
      }
  }

  // ─── Phase 1: Red-flash deletions (code only) ───

  if (!isMarkdown && hasDeletions && diffResult) {
      const delRanges: FlashRange[] = [
          ...wholeLineRanges(diffResult.deletedInOld),
          ...wholeLineRanges([...diffResult.deletedChars.keys()]),
      ]

      flashRanges.value = delRanges
      flashType.value = 'delete'

      await new Promise<void>(resolve => setTimeout(resolve, DELETE_FLASH_MS))

      // Stale generation: just exit — don't clearFlash because the current
      // generation may have already set its own flash state.
      if (gen !== refreshGeneration || store.state.currentFile?.path !== oldPath) {
          return
      }
  }

  // ─── Phase 2: Update store with new content (common) ───

  await store.selectFile(
    currentFilePath,
    currentFile?.isImage,
    currentFile?.isAudio,
    false,
  )

  if (gen !== refreshGeneration) return

  if (clearOnError && store.state.currentFile?.error) {
    store.state.currentFile = null
    if (isMarkdown) clearDiffMarkers()
    else clearFlash()
    return
  }

  // ─── Apply markers (common) ───

  if (isMarkdown) {
      if (markdownResult && markdownResult.hasChanges) {
          diffMarkers.value = markdownResult.markers
          diffOldContent.value = oldContent
          diffOldFilePath.value = currentFilePath
      } else {
          clearDiffMarkers()
      }
  } else {
      if (codeMarkers && codeMarkers.length > 0) {
          diffMarkers.value = codeMarkers
          diffOldContent.value = oldContent
          diffOldFilePath.value = currentFilePath
      } else {
          clearDiffMarkers()
      }
  }

  // ─── Phase 3: Blue-flash additions (code only) ───

  if (isMarkdown) {
      // Markdown path: no flash animation, clear any stale flash state
      clearFlash()
  } else if (diffResult) {
      const addRanges: FlashRange[] = [
          ...wholeLineRanges(diffResult.addedInNew),
          ...wholeLineRanges([...diffResult.addedChars.keys()]),
      ]

      if (addRanges.length > 0) {
          flashRanges.value = addRanges
          flashType.value = 'add'
          scheduleClearFlash(ADD_FLASH_CLEAR_MS)
      } else {
          clearFlash()
      }
  } else {
      clearFlash()
  }

  // ─── Restore scroll position (common) ───

  restoreScrollPosition(savedScrollTop)
}

export { getScrollContainer, getScrollPosition, restoreScrollPosition }
