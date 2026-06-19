/**
 * useFileRefresh — shared logic for refreshing the currently viewed file
 * while preserving scroll position and highlighting changes.
 *
 * Used by three independent refresh triggers:
 * 1. Manual refresh (refresh button in FileHeader / FileManager)
 * 2. fsnotify auto-refresh (useFileWatch SSE file_change event)
 * 3. Chat-driven refresh (ChatPanel onFileModified callback)
 *
 * Two highlight mechanisms:
 * - CodePreview path: flashRanges (line+char offset) for code/raw files
 *   — two-phase flash (red delete → blue add) still applies
 * - MarkdownPreview path: block-level diff markers via useMarkdownDiff
 *   — single-phase: load new content + show persistent markers + drawer
 */
import { ref, watch } from 'vue'
import { store } from '@/stores/app.ts'
import { computeDiff, wholeLineRanges, charMapToRanges } from '@/utils/diffUtils.ts'
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

/**
 * Text snippets that changed — kept for backward compatibility.
 * No longer used by MarkdownPreview (which now uses block-level diff markers).
 * Still exported to avoid breaking imports.
 */
export const flashTextSnippets = ref<string[]>([])
export const flashType = ref<FlashType>('add')
let flashTimer: ReturnType<typeof setTimeout> | null = null

// Generation counter to prevent race conditions with concurrent refreshCurrentFile calls
let refreshGeneration = 0

function clearFlash() {
    if (flashTimer) { clearTimeout(flashTimer); flashTimer = null }
    flashRanges.value = []
    flashTextSnippets.value = []
    flashType.value = 'add'
}

function scheduleClearFlash(ms: number) {
    if (flashTimer) clearTimeout(flashTimer)
    flashTimer = setTimeout(() => { flashRanges.value = []; flashTextSnippets.value = []; flashType.value = 'add'; flashTimer = null }, ms)
}

// ─── Scroll helpers ───

function getScrollContainer(): HTMLElement | null {
  return (document.querySelector('.markdown-body') || document.querySelector('.raw-content-pre')) as HTMLElement | null
}

function getScrollRatio(el: HTMLElement | null): number {
  if (!el) return 0
  const maxScroll = el.scrollHeight - el.clientHeight
  if (maxScroll <= 0) return 0
  return el.scrollTop / maxScroll
}

function restoreScrollRatio(ratio: number): void {
  if (ratio <= 0) return
  const startTime = Date.now()
  const MAX_WAIT = 3000

  function tryRestore() {
    const el = getScrollContainer()
    if (!el) {
      if (Date.now() - startTime < MAX_WAIT) requestAnimationFrame(tryRestore)
      return
    }
    const maxScroll = el.scrollHeight - el.clientHeight
    if (maxScroll <= 0) {
      if (Date.now() - startTime < MAX_WAIT) requestAnimationFrame(tryRestore)
      return
    }
    el.scrollTop = ratio * maxScroll
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

// ─── Markdown diff path ───

/**
 * Get old block list from the MarkdownPreview component's cache.
 * Falls back to extracting from the live DOM.
 */
function getOldBlockList() {
    // Fallback: extract from current DOM
    // Content is inside .markdown-content (child of .markdown-body)
    const content = document.querySelector('.markdown-body .markdown-content') || document.querySelector('.markdown-body')
    if (!content) return []
    return extractBlocks(content)
}

/**
 * Refresh a markdown file using the new block-level diff + marker system.
 * Single-phase: load new content → compute diff → show markers.
 */
async function refreshMarkdownFile(
    currentFilePath: string,
    currentFile: any,
    gen: number,
    scrollRatio: number,
    options: { loadDir?: boolean, clearOnError?: boolean },
): Promise<void> {
    const oldContent = currentFile?.content ?? null

    // Pre-fetch new content
    const newContent = await prefetchFileContent(currentFilePath)
    if (gen !== refreshGeneration) return

    // Get old block list (from MarkdownPreview cache)
    const oldBlocks = getOldBlockList()

    // Compute new block list from off-screen render
    let newBlocks: BlockInfo[] = []
    if (newContent !== null) {
        newBlocks = offscreenExtractBlocks(newContent)
    }

    // Compute diff
    const diffResult = (oldBlocks.length > 0 && newBlocks.length > 0 && newContent !== oldContent)
        ? computeMarkdownDiff(oldBlocks, newBlocks)
        : null

    // Load new content into store (triggers MarkdownPreview re-render)
    await store.selectFile(
        currentFilePath,
        currentFile?.isImage,
        currentFile?.isAudio,
        false,
    )

    if (gen !== refreshGeneration) return

    // Clear file on error if requested
    if (options.clearOnError && store.state.currentFile?.error) {
        store.state.currentFile = null
        clearDiffMarkers()
        return
    }

    // Apply diff markers
    if (diffResult && diffResult.hasChanges) {
        diffMarkers.value = diffResult.markers
        diffOldContent.value = oldContent
        diffOldFilePath.value = currentFilePath
    } else {
        clearDiffMarkers()
    }

    // Restore scroll position
    restoreScrollRatio(scrollRatio)
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
 * For code/raw files: Two-phase flash (red delete → blue add).
 * For markdown files: Block-level diff markers + bottom drawer.
 *
 * @param options.loadDir - Also refresh the directory listing (default: false)
 * @param options.clearOnError - If the file fails to load, clear currentFile (default: false)
 */
export async function refreshCurrentFile(options: {
  loadDir?: boolean
  clearOnError?: boolean
} = {}): Promise<void> {
  const { loadDir = false, clearOnError = false } = options
  const gen = ++refreshGeneration

  const currentFilePath = store.state.currentFile?.path
  const currentFile = store.state.currentFile

  // Save old content for change detection
  const oldContent = currentFile?.content ?? null
  const oldPath = currentFilePath

  // Save scroll position as ratio before refresh
  const scrollEl = getScrollContainer()
  const scrollRatio = getScrollRatio(scrollEl)

  // Refresh directory listing if requested
  if (loadDir && store.state.currentDir !== undefined) {
    store.loadFiles(store.state.currentDir)
  }

  if (!currentFilePath) return

  // ─── Markdown path: block-level diff + markers ───
  // When a .md file is in raw mode, use the code diff path instead
  if (isCurrentFileMarkdown() && isMarkdownRenderedMode()) {
      await refreshMarkdownFile(currentFilePath, currentFile, gen, scrollRatio, options)
      return
  }

  // ─── Code/raw file path: two-phase flash ───

  // Phase 0: Pre-fetch new content for diff
  let newContent: string | null = null
  let hasDeletions = false
  let diffResult: LineDiff | null = null
  let codeMarkers: DiffMarker[] | null = null

  if (oldContent) {
      newContent = await prefetchFileContent(currentFilePath)
      if (gen !== refreshGeneration) return
      if (newContent !== null && newContent !== oldContent) {
          diffResult = computeDiff(oldContent, newContent)
          hasDeletions = diffResult.deletedInOld.length > 0 || diffResult.deletedChars.size > 0
          // Compute persistent diff markers for code files
          codeMarkers = computeCodeDiffMarkers(diffResult, oldContent, newContent)
      }
  }

  // Phase 1: Red-flash deletions (if any)
  if (hasDeletions && diffResult) {
      const delRanges: FlashRange[] = [
          ...wholeLineRanges(diffResult.deletedInOld),
          ...charMapToRanges(diffResult.deletedChars),
      ]

      flashRanges.value = delRanges
      flashTextSnippets.value = []
      flashType.value = 'delete'

      await new Promise<void>(resolve => setTimeout(resolve, DELETE_FLASH_MS))

      if (gen !== refreshGeneration || store.state.currentFile?.path !== oldPath) {
          clearFlash()
          return
      }
  }

  // Phase 2: Update store with new content
  await store.selectFile(
    currentFilePath,
    currentFile?.isImage,
    currentFile?.isAudio,
    false,
  )

  if (gen !== refreshGeneration) return

  if (clearOnError && store.state.currentFile?.error) {
    store.state.currentFile = null
    clearFlash()
    return
  }

  // Set persistent diff markers (after content update, since markers reference new line numbers)
  if (codeMarkers && codeMarkers.length > 0) {
      diffMarkers.value = codeMarkers
      diffOldContent.value = oldContent
      diffOldFilePath.value = currentFilePath
  } else {
      clearDiffMarkers()
  }

  // Phase 3: Blue-flash additions
  if (diffResult) {
      const addRanges: FlashRange[] = [
          ...wholeLineRanges(diffResult.addedInNew),
          ...charMapToRanges(diffResult.addedChars),
      ]

      if (addRanges.length > 0) {
          flashRanges.value = addRanges
          flashTextSnippets.value = []
          flashType.value = 'add'
          scheduleClearFlash(ADD_FLASH_CLEAR_MS)
      } else {
          clearFlash()
      }
  } else {
      clearFlash()
  }

  // Restore scroll position
  restoreScrollRatio(scrollRatio)
}

export { getScrollContainer, getScrollRatio, restoreScrollRatio }
