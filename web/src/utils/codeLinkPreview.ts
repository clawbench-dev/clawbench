/**
 * Pure functions and LRU cache for Markdown code link hover preview.
 *
 * Responsibilities:
 * - Line range normalization and code slicing with line/byte limits
 * - URL construction matching store.selectFile
 * - 4-quadrant positioning and zoom-aware viewport clamping
 * - Weighted LRU cache for file content with TTL and memory thresholds
 */

import { isAbsolutePath, normalizeSlashes } from '@/utils/path'
import { toFixedCSS, getZoomedViewport } from '@/composables/useSettingsConfig'

// ── Resource limits & constants ─────────────────────────────────────────────

export const DEFAULT_CONTEXT = 30
export const DEFAULT_NO_RANGE_LINES = 30
export const MAX_RENDER_LINES = 200
export const MAX_RENDER_BYTES = 512 * 1024 // 512 KiB
export const MAX_LINE_BYTES = 128 * 1024 // 128 KiB

export const PREVIEW_LRU_MAX_ITEMS = 20
export const PREVIEW_LRU_MAX_BYTES = 8 * 1024 * 1024 // 8 MiB
export const PREVIEW_LRU_TTL_MS = 30 * 1000 // 30 seconds
export const LARGE_FILE_THRESHOLD_BYTES = 2 * 1024 * 1024 // 2 MiB

export const DEFAULT_EDGE_MARGIN = 8
export const DEFAULT_ANCHOR_GAP = 8

// ── Types ───────────────────────────────────────────────────────────────────

export type TruncateReason = 'lines' | 'bytes' | 'line'

export interface NormalizedRange {
  start?: number
  end?: number
  hasExplicitRange: boolean
}

export interface CodeSliceResult {
  /** Sliced code joined with \n */
  code: string
  /** 1-based start line of the slice in the original file */
  startLine: number
  /** 1-based end line of the slice in the original file */
  endLine: number
  /** Total lines in original file */
  totalLines: number
  /** 1-based start line of target highlight, if any */
  highlightStart?: number
  /** 1-based end line of target highlight, if any */
  highlightEnd?: number
  /** Whether the requested line was beyond total lines in file */
  lineOutOfRange: boolean
  /** Whether rendering was truncated due to limits */
  renderTruncated: boolean
  /** Reason for truncation if renderTruncated is true */
  truncateReason?: TruncateReason
}

export interface FileContentResponse {
  content: string
  name: string
  path: string
  supported: boolean
  isBinary?: boolean
  truncated?: boolean
  size: number
}

export interface CachedFileContent extends FileContentResponse {
  cachedAt: number
  estimatedBytes: number
}

// ── Range Normalization ─────────────────────────────────────────────────────

/**
 * Normalize requested lineStart and lineEnd into clean 1-based line bounds.
 *
 * Rules:
 * - Line numbers must be positive integers (> 0).
 * - If lineStart is missing/invalid, hasExplicitRange is false.
 * - If lineEnd is missing or lineEnd < lineStart, lineEnd defaults to lineStart.
 */
export function normalizePreviewRange(lineStart?: number, lineEnd?: number): NormalizedRange {
  const start = lineStart && Number.isInteger(lineStart) && lineStart > 0 ? lineStart : undefined
  if (!start) {
    return { hasExplicitRange: false }
  }

  let end: number = start
  if (lineEnd && Number.isInteger(lineEnd) && lineEnd >= start) {
    end = lineEnd
  }

  return {
    start,
    end,
    hasExplicitRange: true,
  }
}

// ── String Byte Length ──────────────────────────────────────────────────────

/**
 * Fast estimation / calculation of UTF-8 byte length of a string.
 */
export function getUtf8ByteLength(str: string): number {
  if (typeof TextEncoder !== 'undefined') {
    return new TextEncoder().encode(str).length
  }
  // Fallback UTF-8 length estimation
  let bytes = 0
  for (let i = 0; i < str.length; i++) {
    const code = str.charCodeAt(i)
    if (code <= 0x7f) bytes += 1
    else if (code <= 0x7ff) bytes += 2
    else if (code >= 0xd800 && code <= 0xdbff) {
      bytes += 4
      i++
    } else bytes += 3
  }
  return bytes
}

// ── Code Slicing ────────────────────────────────────────────────────────────

export interface SliceCodeOptions {
  /** Number of expansion steps (+1 expands 5 lines up/down or 10 lines downward) */
  contextExpansion?: number
  /** Additional lines to expand upward beyond the default context */
  expandAboveLines?: number
  /** Additional lines to expand downward beyond the default context */
  expandBelowLines?: number
}

/**
 * Slice file content for preview with real line numbers, context, and hard resource limits.
 *
 * Slicing constraints:
 * - When target line is given: show [start - 30, end + 30], clamped to file.
 * - When no target line: show first 30 lines.
 * - When lineStart > totalLines: show last up to 30 lines and mark lineOutOfRange = true.
 * - Hard limit MAX_RENDER_LINES (200 lines).
 * - Hard limit MAX_RENDER_BYTES (512 KiB).
 * - Hard limit MAX_LINE_BYTES (128 KiB) per line.
 * - Returns renderTruncated and truncateReason ('lines' | 'bytes' | 'line').
 */
export function sliceCodeForPreview(
  content: string,
  lineStart?: number,
  lineEnd?: number,
  options: SliceCodeOptions = {}
): CodeSliceResult {
  if (content === '') {
    return {
      code: '',
      startLine: 1,
      endLine: 0,
      totalLines: 0,
      lineOutOfRange: false,
      renderTruncated: false,
    }
  }

  // Split preserving exact physical lines (compatible with LF and CRLF)
  const lines = content.split(/\r\n|\r|\n/)
  const totalLines = lines.length

  const { start: reqStart, end: reqEnd, hasExplicitRange } = normalizePreviewRange(lineStart, lineEnd)
  const expansion = Math.max(0, options.contextExpansion ?? 0)
  const extraAbove = Math.max(0, options.expandAboveLines ?? 0)
  const extraBelow = Math.max(0, options.expandBelowLines ?? 0)

  let startLine: number
  let endLine: number
  let highlightStart: number | undefined
  let highlightEnd: number | undefined
  let lineOutOfRange = false
  let renderTruncated = false
  let truncateReason: TruncateReason | undefined

  if (hasExplicitRange && reqStart !== undefined && reqEnd !== undefined) {
    if (reqStart > totalLines) {
      lineOutOfRange = true
      startLine = Math.max(1, totalLines - DEFAULT_NO_RANGE_LINES + 1)
      endLine = totalLines
      highlightStart = undefined
      highlightEnd = undefined
    } else {
      highlightStart = reqStart
      highlightEnd = Math.min(reqEnd, totalLines)

      const contextLines = DEFAULT_CONTEXT + expansion * 5
      const contextAbove = contextLines + extraAbove
      const contextBelow = contextLines + extraBelow
      const targetSpan = highlightEnd - highlightStart + 1

      // If target range itself exceeds MAX_RENDER_LINES, start at highlightStart
      if (targetSpan > MAX_RENDER_LINES) {
        startLine = highlightStart
        endLine = Math.min(totalLines, highlightStart + MAX_RENDER_LINES - 1)
        renderTruncated = true
        truncateReason = 'lines'
      } else {
        let start = Math.max(1, highlightStart - contextAbove)
        let end = Math.min(totalLines, highlightEnd + contextBelow)

        // Only do initial symmetrical redistribution if user hasn't explicitly used directional expansion
        if (extraAbove === 0 && extraBelow === 0) {
          const windowSize = Math.min(MAX_RENDER_LINES, targetSpan + contextLines * 2)
          // If top clamped to 1, expand bottom as much as possible up to windowSize
          if (start === 1) {
            end = Math.min(totalLines, start + windowSize - 1)
          }
          // If bottom clamped to totalLines, expand top as much as possible up to windowSize
          if (end === totalLines) {
            start = Math.max(1, end - windowSize + 1)
          }
        }

        if (end - start + 1 > MAX_RENDER_LINES) {
          end = start + MAX_RENDER_LINES - 1
          renderTruncated = true
          truncateReason = 'lines'
        }

        startLine = start
        endLine = end
      }
    }
  } else {
    // No explicit range: show from line 1
    const count = Math.min(totalLines, DEFAULT_NO_RANGE_LINES + expansion * 10 + extraBelow)
    startLine = 1
    endLine = count
    if (endLine - startLine + 1 > MAX_RENDER_LINES) {
      endLine = startLine + MAX_RENDER_LINES - 1
      renderTruncated = true
      truncateReason = 'lines'
    }
  }

  const renderedLines: string[] = []
  let totalBytes = 0

  for (let i = startLine - 1; i < endLine; i++) {
    if (renderedLines.length >= MAX_RENDER_LINES) {
      renderTruncated = true
      truncateReason = 'lines'
      break
    }

    const lineText = lines[i]
    const lineByteLength = getUtf8ByteLength(lineText)

    if (lineByteLength > MAX_LINE_BYTES) {
      renderTruncated = true
      truncateReason = 'line'
      break
    }

    const nextBytes = totalBytes + lineByteLength + (renderedLines.length > 0 ? 1 : 0)
    if (nextBytes > MAX_RENDER_BYTES) {
      renderTruncated = true
      truncateReason = 'bytes'
      break
    }

    renderedLines.push(lineText)
    totalBytes = nextBytes
  }

  const actualEndLine = renderedLines.length > 0 ? startLine + renderedLines.length - 1 : startLine

  return {
    code: renderedLines.join('\n'),
    startLine,
    endLine: actualEndLine,
    totalLines,
    highlightStart,
    highlightEnd,
    lineOutOfRange,
    renderTruncated,
    truncateReason,
  }
}

// ── URL Construction ────────────────────────────────────────────────────────

/**
 * Build URL to fetch file content, matching store.selectFile convention.
 * Absolute paths use /api/file?path=..., relative paths use /api/file/...
 */
export function buildPreviewUrl(path: string): string {
  const normalized = normalizeSlashes(path)
  if (isAbsolutePath(normalized)) {
    return `/api/file?path=${encodeURIComponent(normalized)}`
  }
  const cleanPath = normalized.replace(/^\/+/, '')
  return `/api/file/${encodeURIComponent(cleanPath)}`
}

export const DEFAULT_SAFE_AREA_TOP = 36

/**
 * Resolve bottom pixel position of the top fixed ClawBench App Header.
 * Prevents floating preview cards or popups from occluding or hiding under the header.
 */
export function getAppHeaderBottom(): number {
  if (typeof document === 'undefined') return DEFAULT_SAFE_AREA_TOP
  const header = document.querySelector('.header')
  if (header) {
    const rect = header.getBoundingClientRect()
    if (rect.bottom > 0) return Math.round(rect.bottom)
  }
  const safeTop = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--header-safe-area-top')) || 0
  const headerHeight = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--header-height')) || 36
  const total = headerHeight + safeTop
  return total > 0 ? total : DEFAULT_SAFE_AREA_TOP
}

// ── Positioning & Clamp ─────────────────────────────────────────────────────

export interface RectLike {
  left: number
  top: number
  right: number
  bottom: number
  width?: number
  height?: number
}

export interface ViewportSize {
  width: number
  height: number
}

export interface CardPlacementResult {
  /** Left coordinate in viewport pixels */
  viewportX: number
  /** Top coordinate in viewport pixels */
  viewportY: number
  /** CSS left value (scaled for CSS zoom via toFixedCSS) */
  cssLeft: string
  /** CSS top value (scaled for CSS zoom via toFixedCSS) */
  cssTop: string
  /** Maximum allowable height in viewport pixels (optional dynamic constraint) */
  maxHeight?: number
  /** Chosen quadrant name */
  quadrant: 'bottom-right' | 'bottom-left' | 'top-right' | 'top-left' | 'clamped'
}

/**
 * Place floating card near an anchor element using 4-quadrant priority:
 * 1. Bottom-Right: below anchor, left-aligned with anchor
 * 2. Bottom-Left: below anchor, right-aligned with anchor
 * 3. Top-Right: above anchor, left-aligned with anchor
 * 4. Top-Left: above anchor, right-aligned with anchor
 * Fallback: clamp to the vertical side with more available space.
 */
export function placeNearAnchor(
  anchorRect: RectLike,
  cardWidth: number,
  cardHeight: number,
  opts: {
    viewport?: ViewportSize
    edgeMargin?: number
    gap?: number
    safeAreaTop?: number
  } = {}
): CardPlacementResult {
  const vp = opts.viewport ?? (typeof window !== 'undefined' ? getZoomedViewport() : { width: 1024, height: 768 })
  const edgeMargin = opts.edgeMargin ?? DEFAULT_EDGE_MARGIN
  const gap = opts.gap ?? DEFAULT_ANCHOR_GAP
  const resolvedSafeAreaTop = opts.safeAreaTop !== undefined
    ? opts.safeAreaTop
    : (typeof window !== 'undefined' ? getAppHeaderBottom() : DEFAULT_SAFE_AREA_TOP)
  const safeAreaTop = resolvedSafeAreaTop + edgeMargin

  const minX = edgeMargin
  const maxX = Math.max(edgeMargin, vp.width - cardWidth - edgeMargin)
  const minY = safeAreaTop
  const maxY = Math.max(minY, vp.height - cardHeight - edgeMargin)

  // 1. Bottom-Right
  const brX = anchorRect.left
  const brY = anchorRect.bottom + gap
  if (brX >= minX && brX + cardWidth <= vp.width - edgeMargin && brY >= minY && brY + cardHeight <= vp.height - edgeMargin) {
    const availableHeight = vp.height - edgeMargin - brY
    return makePlacement(brX, brY, 'bottom-right', availableHeight)
  }

  // 2. Bottom-Left
  const blX = anchorRect.right - cardWidth
  const blY = anchorRect.bottom + gap
  if (blX >= minX && blX + cardWidth <= vp.width - edgeMargin && blY >= minY && blY + cardHeight <= vp.height - edgeMargin) {
    const availableHeight = vp.height - edgeMargin - blY
    return makePlacement(blX, blY, 'bottom-left', availableHeight)
  }

  // 3. Top-Right
  const trX = anchorRect.left
  const trY = anchorRect.top - gap - cardHeight
  if (trX >= minX && trX + cardWidth <= vp.width - edgeMargin && trY >= minY && trY + cardHeight <= vp.height - edgeMargin) {
    const availableHeight = anchorRect.top - gap - minY
    return makePlacement(trX, trY, 'top-right', availableHeight)
  }

  // 4. Top-Left
  const tlX = anchorRect.right - cardWidth
  const tlY = anchorRect.top - gap - cardHeight
  if (tlX >= minX && tlX + cardWidth <= vp.width - edgeMargin && tlY >= minY && tlY + cardHeight <= vp.height - edgeMargin) {
    const availableHeight = anchorRect.top - gap - minY
    return makePlacement(tlX, tlY, 'top-left', availableHeight)
  }

  // Fallback: prefer side that fits cardHeight, otherwise choose side with more space
  const spaceBelow = Math.max(0, vp.height - (anchorRect.bottom + gap) - edgeMargin)
  const spaceAbove = Math.max(0, anchorRect.top - gap - minY)
  const fitsBelow = spaceBelow >= cardHeight
  const fitsAbove = spaceAbove >= cardHeight
  const goBelow = fitsBelow || (!fitsAbove && spaceBelow >= spaceAbove)

  let rawY: number
  if (goBelow) {
    rawY = anchorRect.bottom + gap
  } else {
    // Going above: limit height to spaceAbove so top does not hide behind header,
    // and bottom stays above anchorRect.top - gap
    const actualHeight = Math.min(cardHeight, spaceAbove)
    rawY = anchorRect.top - gap - actualHeight
  }
  const rawX = anchorRect.left

  const clampedX = Math.min(Math.max(rawX, minX), maxX)
  const clampedY = Math.min(Math.max(rawY, minY), maxY)

  const availableHeight = goBelow
    ? Math.max(0, vp.height - edgeMargin - clampedY)
    : Math.max(0, (anchorRect.top - gap) - clampedY)

  return makePlacement(clampedX, clampedY, 'clamped', availableHeight)
}

function makePlacement(
  x: number,
  y: number,
  quadrant: CardPlacementResult['quadrant'],
  maxHeight?: number
): CardPlacementResult {
  return {
    viewportX: x,
    viewportY: y,
    cssLeft: `${toFixedCSS(x)}px`,
    cssTop: `${toFixedCSS(y)}px`,
    maxHeight: maxHeight !== undefined ? Math.round(maxHeight) : undefined,
    quadrant,
  }
}

/**
 * Clamp card position within viewport bounds during drag or resize.
 * Ensures the titlebar (at top of card) always stays visible and below the app header.
 */
export function clampCardPosition(
  x: number,
  y: number,
  cardWidth: number,
  cardHeight: number,
  viewport?: ViewportSize,
  safeAreaTop?: number,
  edgeMargin = DEFAULT_EDGE_MARGIN
): { viewportX: number; viewportY: number; cssLeft: string; cssTop: string } {
  const vp = viewport ?? (typeof window !== 'undefined' ? getZoomedViewport() : { width: 1024, height: 768 })
  const resolvedSafeAreaTop = safeAreaTop !== undefined
    ? safeAreaTop
    : (typeof window !== 'undefined' ? getAppHeaderBottom() : DEFAULT_SAFE_AREA_TOP)
  const topSafe = resolvedSafeAreaTop + edgeMargin

  const minX = edgeMargin
  const maxX = Math.max(edgeMargin, vp.width - cardWidth - edgeMargin)
  const minY = topSafe
  const maxY = Math.max(minY, vp.height - cardHeight - edgeMargin)

  const clampedX = Math.min(Math.max(x, minX), maxX)
  const clampedY = Math.min(Math.max(y, minY), maxY)

  return {
    viewportX: clampedX,
    viewportY: clampedY,
    cssLeft: `${toFixedCSS(clampedX)}px`,
    cssTop: `${toFixedCSS(clampedY)}px`,
  }
}

// ── Weighted LRU Cache ──────────────────────────────────────────────────────

/**
 * Estimate memory consumption of cached file content in bytes.
 * Characters in JS are 2 bytes in memory UTF-16, plus object overhead.
 */
export function estimateFileMemoryBytes(item: { content?: string }): number {
  return (item.content?.length || 0) * 2 + 512
}

export class CodeLinkPreviewCache {
  private cache = new Map<string, CachedFileContent>()
  private currentBytes = 0

  constructor(
    public readonly maxItems = PREVIEW_LRU_MAX_ITEMS,
    public readonly maxBytes = PREVIEW_LRU_MAX_BYTES,
    public readonly ttlMs = PREVIEW_LRU_TTL_MS,
    public readonly largeFileThreshold = LARGE_FILE_THRESHOLD_BYTES
  ) {}

  public buildKey(projectRoot: string, normalizedPath: string): string {
    return `${normalizeSlashes(projectRoot)}::${normalizeSlashes(normalizedPath)}`
  }

  public get(key: string, now = Date.now()): CachedFileContent | undefined {
    const item = this.cache.get(key)
    if (!item) return undefined

    // Check TTL expiration
    if (now - item.cachedAt > this.ttlMs) {
      this.delete(key)
      return undefined
    }

    // Refresh LRU order: delete and re-insert at end
    this.cache.delete(key)
    this.cache.set(key, item)
    return item
  }

  public set(key: string, response: FileContentResponse, now = Date.now()): boolean {
    const contentBytes = response.size ?? getUtf8ByteLength(response.content || '')

    // Files exceeding 2 MiB threshold are NOT cached
    if (contentBytes > this.largeFileThreshold) {
      return false
    }

    // Evict expired entries first
    this.purgeExpired(now)

    // If key already exists, remove it first to update byte accounting
    if (this.cache.has(key)) {
      this.delete(key)
    }

    const estimatedBytes = estimateFileMemoryBytes(response)

    // Evict oldest items if exceeding capacity
    while (
      (this.cache.size >= this.maxItems || this.currentBytes + estimatedBytes > this.maxBytes) &&
      this.cache.size > 0
    ) {
      const oldestKey = this.cache.keys().next().value
      if (oldestKey !== undefined) {
        this.delete(oldestKey)
      } else {
        break
      }
    }

    const cachedItem: CachedFileContent = {
      ...response,
      cachedAt: now,
      estimatedBytes,
    }

    this.cache.set(key, cachedItem)
    this.currentBytes += estimatedBytes
    return true
  }

  public delete(key: string): boolean {
    const item = this.cache.get(key)
    if (!item) return false
    this.cache.delete(key)
    this.currentBytes = Math.max(0, this.currentBytes - item.estimatedBytes)
    return true
  }

  public clear(): void {
    this.cache.clear()
    this.currentBytes = 0
  }

  public purgeExpired(now = Date.now()): void {
    for (const [key, item] of this.cache.entries()) {
      if (now - item.cachedAt > this.ttlMs) {
        this.delete(key)
      }
    }
  }

  public get size(): number {
    return this.cache.size
  }

  public get totalEstimatedBytes(): number {
    return this.currentBytes
  }
}

/** Global shared LRU cache instance for code link previews */
export const previewCache = new CodeLinkPreviewCache()

// ── Syntax Highlight Line Splitter ──────────────────────────────────────────

/**
 * Splits syntax-highlighted HTML string into per-line HTML strings,
 * properly balancing and restoring any opened <span> tags across line breaks.
 */
export function splitHighlightedHtml(html: string): string[] {
  if (!html) return []
  const lines: string[] = []
  const openTags: string[] = []
  let currentLine = ''

  // Matches open span, close span, newline, or chunk of text / other tag
  const tokenRegex = /(<span\b[^>]*>)|(<\/span>)|(\r\n|\n|\r)|([^<\r\n]+)|(<[^>]+>)/g
  let match: RegExpExecArray | null

  while ((match = tokenRegex.exec(html)) !== null) {
    const [, openSpan, closeSpan, newline, text, otherTag] = match

    if (newline) {
      let lineWithClosed = currentLine
      for (let i = openTags.length - 1; i >= 0; i--) {
        lineWithClosed += '</span>'
      }
      lines.push(lineWithClosed)
      currentLine = openTags.join('')
    } else if (openSpan) {
      openTags.push(openSpan)
      currentLine += openSpan
    } else if (closeSpan) {
      openTags.pop()
      currentLine += closeSpan
    } else if (text) {
      currentLine += text
    } else if (otherTag) {
      currentLine += otherTag
    }
  }

  let lineWithClosed = currentLine
  for (let i = openTags.length - 1; i >= 0; i--) {
    lineWithClosed += '</span>'
  }
  lines.push(lineWithClosed)

  return lines
}

