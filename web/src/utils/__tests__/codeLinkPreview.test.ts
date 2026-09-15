import { describe, expect, it, beforeEach, vi } from 'vitest'
import {
  normalizePreviewRange,
  sliceCodeForPreview,
  computeRenderWindow,
  computeFetchWindow,
  windowCovers,
  buildPreviewUrl,
  getAppHeaderBottom,
  placeNearAnchor,
  clampCardPosition,
  CodeLinkPreviewCache,
  previewCache,
  MAX_RENDER_LINES,
  MAX_RENDER_BYTES,
  MAX_LINE_BYTES,
  FETCH_WINDOW_MARGIN,
  MAX_FETCH_WINDOW_LINES,
  splitHighlightedHtml,
} from '@/utils/codeLinkPreview'

// Mock zoom helpers
vi.mock('@/composables/useSettingsConfig', () => ({
  toFixedCSS: (val: number) => val,
  getZoomedViewport: () => ({ width: 1024, height: 768 }),
}))

describe('codeLinkPreview utils', () => {
  describe('normalizePreviewRange', () => {
    it('normalizes single line and ranged lines', () => {
      expect(normalizePreviewRange(10)).toEqual({ start: 10, end: 10, hasExplicitRange: true })
      expect(normalizePreviewRange(10, 20)).toEqual({ start: 10, end: 20, hasExplicitRange: true })
    })

    it('handles inverted ranges by setting end = start', () => {
      expect(normalizePreviewRange(20, 10)).toEqual({ start: 20, end: 20, hasExplicitRange: true })
    })

    it('handles non-positive and missing line numbers', () => {
      expect(normalizePreviewRange()).toEqual({ hasExplicitRange: false })
      expect(normalizePreviewRange(0)).toEqual({ hasExplicitRange: false })
      expect(normalizePreviewRange(-5)).toEqual({ hasExplicitRange: false })
    })
  })

  describe('sliceCodeForPreview', () => {
    const sampleCode = [
      'line 1',
      'line 2',
      'line 3',
      'line 4',
      'line 5',
      'line 6',
      'line 7',
      'line 8',
      'line 9',
      'line 10',
    ].join('\n')

    it('returns empty slice for empty content', () => {
      const res = sliceCodeForPreview('')
      expect(res.code).toBe('')
      expect(res.totalLines).toBe(0)
      expect(res.renderTruncated).toBe(false)
    })

    it('slices with default 30 lines when no line range is given', () => {
      const res = sliceCodeForPreview(sampleCode)
      expect(res.startLine).toBe(1)
      expect(res.endLine).toBe(10)
      expect(res.totalLines).toBe(10)
      expect(res.highlightStart).toBeUndefined()
      expect(res.highlightEnd).toBeUndefined()
      expect(res.lineOutOfRange).toBe(false)
      expect(res.renderTruncated).toBe(false)
    })

    it('slices target line with generous default 30 context lines', () => {
      // 200 lines sample: target line 100 -> [100-30, 100+30] = [70, 130]
      const twoHundredLines = Array.from({ length: 200 }, (_, i) => `line ${i + 1}`).join('\n')
      const res = sliceCodeForPreview(twoHundredLines, 100)
      expect(res.startLine).toBe(70)
      expect(res.endLine).toBe(130)
      expect(res.highlightStart).toBe(100)
      expect(res.highlightEnd).toBe(100)
      expect(res.renderTruncated).toBe(false)
    })

    it('clamps context lines at file beginning and end and redistributes context budget', () => {
      const twoHundredLines = Array.from({ length: 200 }, (_, i) => `line ${i + 1}`).join('\n')
      // target line 5 -> windowSize = 61 -> [1, 61]
      const topRes = sliceCodeForPreview(twoHundredLines, 5)
      expect(topRes.startLine).toBe(1)
      expect(topRes.endLine).toBe(61)

      // target line 195 -> windowSize = 61 -> [140, 200]
      const bottomRes = sliceCodeForPreview(twoHundredLines, 195)
      expect(bottomRes.startLine).toBe(140)
      expect(bottomRes.endLine).toBe(200)

      // Small 10 lines file -> includes all 10 lines
      const smallRes = sliceCodeForPreview(sampleCode, 5)
      expect(smallRes.startLine).toBe(1)
      expect(smallRes.endLine).toBe(10)
    })

    it('supports CRLF and trailing empty line', () => {
      const crlfCode = 'line 1\r\nline 2\r\nline 3\r\n'
      const res = sliceCodeForPreview(crlfCode, 2)
      expect(res.totalLines).toBe(4) // last empty line
      expect(res.code).toContain('line 2')
    })

    it('handles lineStart exceeding total lines', () => {
      const res = sliceCodeForPreview(sampleCode, 999)
      expect(res.lineOutOfRange).toBe(true)
      expect(res.startLine).toBe(1)
      expect(res.endLine).toBe(10)
      expect(res.highlightStart).toBeUndefined()
    })

    it('truncates when line count exceeds MAX_RENDER_LINES (200)', () => {
      const manyLines = Array.from({ length: 300 }, (_, i) => `line ${i + 1}`).join('\n')
      const res = sliceCodeForPreview(manyLines, 1, 280)
      expect(res.renderTruncated).toBe(true)
      expect(res.truncateReason).toBe('lines')
      expect(res.endLine - res.startLine + 1).toBe(MAX_RENDER_LINES)
    })

    describe('multi-range annotations', () => {
      const lines300 = Array.from({ length: 300 }, (_, i) => `line ${i + 1}`).join('\n')

      it('anchors the window at the earliest range and reports all in-window ranges', () => {
        const res = sliceCodeForPreview(lines300, 90, 91, {
          lineRanges: [
            { start: 90, end: 91 },
            { start: 95, end: 95 },
            { start: 110, end: 110 },
          ],
        })
        expect(res.startLine).toBe(60) // 90 - 30 context
        expect(res.highlightRanges).toEqual([
          { start: 90, end: 91 },
          { start: 95, end: 95 },
          { start: 110, end: 110 },
        ])
        // Overall min/max remain the legacy span.
        expect(res.highlightStart).toBe(90)
        expect(res.highlightEnd).toBe(110)
      })

      it('clamps out-of-window ranges away when they exceed the 200-line cap', () => {
        // Window = [90-30, 300+30] capped to 200 lines → [60, 259]; the 300
        // range falls outside and must not be highlighted in the slice.
        const res = sliceCodeForPreview(lines300, 90, 91, {
          lineRanges: [
            { start: 90, end: 91 },
            { start: 300, end: 300 },
          ],
        })
        expect(res.endLine - res.startLine + 1).toBeLessThanOrEqual(MAX_RENDER_LINES)
        expect(res.highlightRanges).toEqual([{ start: 90, end: 91 }])
        expect(res.highlightEnd).toBe(300)
      })

      it('keeps a single-range list equivalent to the legacy pair', () => {
        const res = sliceCodeForPreview(lines300, 100, 100, { lineRanges: [{ start: 100, end: 100 }] })
        expect(res.highlightRanges).toEqual([{ start: 100, end: 100 }])
        expect(res.highlightStart).toBe(100)
      })
    })

    it('truncates when total bytes exceed MAX_RENDER_BYTES (512 KiB)', () => {
      // Create 100 lines each ~10 KiB -> total ~1 MiB
      const bigLine = 'a'.repeat(10 * 1024)
      const bigCode = Array.from({ length: 100 }, () => bigLine).join('\n')
      const res = sliceCodeForPreview(bigCode, 1, 100)
      expect(res.renderTruncated).toBe(true)
      expect(res.truncateReason).toBe('bytes')
    })

    it('truncates when a single line exceeds MAX_LINE_BYTES (128 KiB)', () => {
      const giantLine = 'x'.repeat(MAX_LINE_BYTES + 1000)
      const code = ['short line', giantLine, 'after'].join('\n')
      const res = sliceCodeForPreview(code, 1, 3)
      expect(res.renderTruncated).toBe(true)
      expect(res.truncateReason).toBe('line')
      expect(res.code).toBe('short line')
    })

    it('supports contextExpansion (+5 lines for range, +10 for no range)', () => {
      const twoHundredLines = Array.from({ length: 200 }, (_, i) => `line ${i + 1}`).join('\n')
      // Target line 100 -> context normally 30 -> [70, 130]
      // Expansion 1 -> context 30 + 5 = 35 -> [65, 135]
      const expRes = sliceCodeForPreview(twoHundredLines, 100, 100, { contextExpansion: 1 })
      expect(expRes.startLine).toBe(65)
      expect(expRes.endLine).toBe(135)

      // No range -> normally 30 -> expansion 1 -> 40 lines
      const noRangeExp = sliceCodeForPreview(twoHundredLines, undefined, undefined, { contextExpansion: 1 })
      expect(noRangeExp.startLine).toBe(1)
      expect(noRangeExp.endLine).toBe(40)
    })

    it('supports directional expansion (expandAboveLines and expandBelowLines)', () => {
      const twoHundredLines = Array.from({ length: 200 }, (_, i) => `line ${i + 1}`).join('\n')
      // Target line 100 -> normally context 30 -> [70, 130]
      // expandAboveLines: 15 -> startLine 70 - 15 = 55, endLine 130
      const aboveRes = sliceCodeForPreview(twoHundredLines, 100, 100, { expandAboveLines: 15 })
      expect(aboveRes.startLine).toBe(55)
      expect(aboveRes.endLine).toBe(130)

      // expandBelowLines: 20 -> startLine 70, endLine 130 + 20 = 150
      const belowRes = sliceCodeForPreview(twoHundredLines, 100, 100, { expandBelowLines: 20 })
      expect(belowRes.startLine).toBe(70)
      expect(belowRes.endLine).toBe(150)
    })
  })

  describe('computeRenderWindow', () => {
    it('shows the head of the file with no annotation', () => {
      const win = computeRenderWindow(undefined, undefined, 1000)
      expect(win.startLine).toBe(1)
      expect(win.endLine).toBe(30)
    })

    it('centers context on the annotated line and clamps to the file', () => {
      const win = computeRenderWindow(500, 500, 1000)
      expect(win.startLine).toBe(470)
      expect(win.endLine).toBe(530)
      expect(win.highlightStart).toBe(500)
      expect(win.highlightEnd).toBe(500)
    })

    it('flags an annotation past EOF and falls back to the tail', () => {
      const win = computeRenderWindow(9999, 9999, 100)
      expect(win.lineOutOfRange).toBe(true)
      expect(win.startLine).toBe(71)
      expect(win.endLine).toBe(100)
      expect(win.highlightStart).toBeUndefined()
    })

    it('caps the window at MAX_RENDER_LINES', () => {
      const win = computeRenderWindow(1, 400, 1000)
      expect(win.endLine - win.startLine + 1).toBe(MAX_RENDER_LINES)
      expect(win.renderTruncated).toBe(true)
      expect(win.truncateReason).toBe('lines')
    })
  })

  describe('sliceCodeForPreview with a line window', () => {
    // Server returns only lines 101..160 of a 1000-line file.
    const windowContent = Array.from({ length: 60 }, (_, i) => `line ${101 + i}`).join('\n')
    const windowOpts = { baseLineOffset: 101, totalLines: 1000 }

    it('reports absolute line numbers for the window', () => {
      const res = sliceCodeForPreview(windowContent, 130, 130, windowOpts)
      expect(res.totalLines).toBe(1000)
      expect(res.startLine).toBe(101)
      expect(res.endLine).toBe(160)
      expect(res.code.split('\n')[0]).toBe('line 101')
      expect(res.code.split('\n')[29]).toBe('line 130')
    })

    it('keeps "lines remaining" in file coordinates, not window coordinates', () => {
      const res = sliceCodeForPreview(windowContent, 130, 130, windowOpts)
      // 100 lines exist above the window, 840 below — derived from totalLines.
      expect(res.startLine - 1).toBe(100)
      expect(res.totalLines - res.endLine).toBe(840)
    })

    it('falls back to the nearest held edge when the window misses entirely', () => {
      // Window holds 101..160; a render window of 170..230 has no overlap. Rather
      // than blanking out mid-read, fall back to the nearest real line (160) and
      // let the caller widen the fetch.
      const res = sliceCodeForPreview(windowContent, 200, 200, windowOpts)
      expect(res.startLine).toBe(160)
      expect(res.code).toBe('line 160')
    })

    it('clamps an expansion past the fetched edge to what is held', () => {
      // Expanding to the top of a 1000-line file while holding only 101..160:
      // the slice keeps the window's top edge, all of which are real lines.
      const res = sliceCodeForPreview(windowContent, 130, 130, {
        ...windowOpts,
        expandAboveLines: 500,
      })
      expect(res.startLine).toBe(101)
      expect(res.endLine).toBe(160)
      expect(res.code.split('\n')[0]).toBe('line 101')
    })

    it('still flags an annotation past the real EOF', () => {
      const res = sliceCodeForPreview(windowContent, 5000, 5000, windowOpts)
      expect(res.lineOutOfRange).toBe(true)
    })

    it('matches whole-file slicing when the window covers the whole file', () => {
      const whole = Array.from({ length: 300 }, (_, i) => `line ${i + 1}`).join('\n')
      const viaFile = sliceCodeForPreview(whole, 150, 150)
      const viaWindow = sliceCodeForPreview(whole, 150, 150, { baseLineOffset: 1, totalLines: 300 })
      expect(viaWindow).toEqual(viaFile)
    })
  })

  describe('computeFetchWindow', () => {
    it('opens on the file head when there is no annotation', () => {
      const win = computeFetchWindow({ filePath: 'a.ts' }, null)
      expect(win.start).toBe(1)
      expect(win.end).toBe(30 + FETCH_WINDOW_MARGIN)
    })

    it('surrounds the annotated line with margin', () => {
      const win = computeFetchWindow({ filePath: 'a.ts', lineStart: 500, lineEnd: 510 }, 1000)
      // Render window is 470..540; margin extends both sides.
      expect(win.start).toBe(470 - FETCH_WINDOW_MARGIN)
      expect(win.end).toBe(540 + FETCH_WINDOW_MARGIN)
    })

    it('clamps to the file length once known', () => {
      const win = computeFetchWindow({ filePath: 'a.ts', lineStart: 500, lineEnd: 510 }, 520)
      expect(win.end).toBe(520)
    })

    it('spans a multi-range annotation without exceeding the render cap', () => {
      const win = computeFetchWindow({ filePath: 'a.ts', lineRanges: '90-91,309,938-943' }, 2000)
      expect(win.start).toBe(1)
      // The render window is capped at 200 lines from the first range, so the
      // request stays bounded rather than spanning to line 943.
      expect(win.end - win.start + 1).toBeLessThanOrEqual(MAX_FETCH_WINDOW_LINES)
      expect(win.end).toBeLessThan(943 + FETCH_WINDOW_MARGIN)
    })

    it('stays bounded for a huge annotation span', () => {
      const win = computeFetchWindow({ filePath: 'a.ts', lineStart: 100, lineEnd: 5000 }, null)
      // The render window is capped at 200 lines from the annotation start, plus
      // margin — so a 4901-line span never becomes a 4901-line request.
      expect(win.start).toBe(1)
      expect(win.end).toBe(299 + FETCH_WINDOW_MARGIN)
      expect(win.end - win.start + 1).toBeLessThanOrEqual(MAX_FETCH_WINDOW_LINES)
    })
  })

  describe('windowCovers', () => {
    it('is false without a fetched window', () => {
      expect(windowCovers(null, 1, 10)).toBe(false)
    })

    it('requires full containment', () => {
      expect(windowCovers({ start: 100, end: 200 }, 120, 180)).toBe(true)
      expect(windowCovers({ start: 100, end: 200 }, 90, 180)).toBe(false)
      expect(windowCovers({ start: 100, end: 200 }, 120, 210)).toBe(false)
    })
  })

  describe('splitHighlightedHtml', () => {
    it('returns empty array for empty input', () => {
      expect(splitHighlightedHtml('')).toEqual([])
    })

    it('splits plain text into lines', () => {
      expect(splitHighlightedHtml('line 1\nline 2\nline 3')).toEqual(['line 1', 'line 2', 'line 3'])
    })

    it('balances multi-line span tokens across lines', () => {
      const html = '<span class="hljs-comment">/* line 1\n * line 2\n */</span>'
      const lines = splitHighlightedHtml(html)
      expect(lines).toEqual([
        '<span class="hljs-comment">/* line 1</span>',
        '<span class="hljs-comment"> * line 2</span>',
        '<span class="hljs-comment"> */</span>',
      ])
    })

    it('handles nested spans spanning across newlines', () => {
      const html = '<span class="outer"><span class="inner">a\nb</span></span>'
      const lines = splitHighlightedHtml(html)
      expect(lines).toEqual([
        '<span class="outer"><span class="inner">a</span></span>',
        '<span class="outer"><span class="inner">b</span></span>',
      ])
    })

    it('handles CRLF line endings', () => {
      const html = '<span class="str">foo\r\nbar</span>'
      const lines = splitHighlightedHtml(html)
      expect(lines).toEqual(['<span class="str">foo</span>', '<span class="str">bar</span>'])
    })
  })

  describe('buildPreviewUrl', () => {
    it('encodes relative paths', () => {
      expect(buildPreviewUrl('src/utils/math.ts')).toBe('/api/file/src%2Futils%2Fmath.ts')
      expect(buildPreviewUrl('utils/math.ts')).toBe('/api/file/utils%2Fmath.ts')
    })

    it('encodes Unix absolute paths with query param', () => {
      expect(buildPreviewUrl('/etc/hosts')).toBe('/api/file?path=%2Fetc%2Fhosts')
    })

    it('encodes Windows absolute paths with query param', () => {
      expect(buildPreviewUrl('C:\\repo\\file.ts')).toBe('/api/file?path=C%3A%2Frepo%2Ffile.ts')
      expect(buildPreviewUrl('D:/repo/file.ts')).toBe('/api/file?path=D%3A%2Frepo%2Ffile.ts')
    })

    it('appends the line window to a relative-path URL', () => {
      expect(buildPreviewUrl('src/a.ts', { start: 101, end: 200 }))
        .toBe('/api/file/src%2Fa.ts?lineStart=101&lineEnd=200')
    })

    it('appends the line window to an absolute-path URL with &', () => {
      expect(buildPreviewUrl('/etc/hosts', { start: 5, end: 9 }))
        .toBe('/api/file?path=%2Fetc%2Fhosts&lineStart=5&lineEnd=9')
    })

    it('omits the window when none is given', () => {
      expect(buildPreviewUrl('src/a.ts', null)).toBe('/api/file/src%2Fa.ts')
    })
  })

  describe('placeNearAnchor & clampCardPosition', () => {
    const viewport = { width: 1000, height: 800 }
    const cardWidth = 400
    const cardHeight = 300

    it('prefers bottom-right quadrant when space is available', () => {
      const anchorRect = { left: 100, top: 100, right: 200, bottom: 120 }
      const res = placeNearAnchor(anchorRect, cardWidth, cardHeight, { viewport, edgeMargin: 8, gap: 8 })
      expect(res.quadrant).toBe('bottom-right')
      expect(res.viewportX).toBe(100)
      expect(res.viewportY).toBe(128)
    })

    it('uses bottom-left quadrant when right side is constrained', () => {
      const anchorRect = { left: 700, top: 100, right: 950, bottom: 120 }
      const res = placeNearAnchor(anchorRect, cardWidth, cardHeight, { viewport, edgeMargin: 8, gap: 8 })
      expect(res.quadrant).toBe('bottom-left')
      expect(res.viewportX).toBe(950 - cardWidth)
      expect(res.viewportY).toBe(128)
    })

    it('uses top-right quadrant when bottom side is constrained', () => {
      const anchorRect = { left: 100, top: 600, right: 200, bottom: 620 }
      const res = placeNearAnchor(anchorRect, cardWidth, cardHeight, { viewport, edgeMargin: 8, gap: 8 })
      expect(res.quadrant).toBe('top-right')
      expect(res.viewportX).toBe(100)
      expect(res.viewportY).toBe(600 - 8 - cardHeight)
    })

    it('clamps card position during drag within viewport bounds', () => {
      const resLeft = clampCardPosition(-50, 100, cardWidth, cardHeight, viewport)
      expect(resLeft.viewportX).toBe(8)

      const resRight = clampCardPosition(900, 100, cardWidth, cardHeight, viewport)
      expect(resRight.viewportX).toBe(viewport.width - cardWidth - 8)

      const resTop = clampCardPosition(100, -20, cardWidth, cardHeight, viewport, 20)
      expect(resTop.viewportY).toBe(28)

      // Clamps to safe area top when not passed
      const resDefaultTop = clampCardPosition(100, -20, cardWidth, cardHeight, viewport)
      expect(resDefaultTop.viewportY).toBe(getAppHeaderBottom() + 8)
    })

    it('clamps card above bottom anchor and constrains maxHeight without occluding header', () => {
      // Anchor near the bottom of viewport
      const anchorRect = { left: 100, top: 720, right: 200, bottom: 740 }
      const largeCardHeight = 450
      const res = placeNearAnchor(anchorRect, cardWidth, largeCardHeight, {
        viewport,
        edgeMargin: 8,
        gap: 8,
        safeAreaTop: 44,
      })

      // Top margin should never be less than safeAreaTop + edgeMargin
      expect(res.viewportY).toBeGreaterThanOrEqual(44 + 8)
      // Bottom of card must not overlap anchorRect.top
      const actualHeight = res.maxHeight ? Math.min(largeCardHeight, res.maxHeight) : largeCardHeight
      expect(res.viewportY + actualHeight).toBeLessThanOrEqual(anchorRect.top)
      expect(res.maxHeight).toBeDefined()
    })

    it('clamps card in fallback below anchor and ensures viewportY + maxHeight does not exceed viewport bottom', () => {
      // Anchor Rect where spaceBelow is less than cardHeight but larger than spaceAbove
      const anchorRect = { left: 100, top: 350, right: 200, bottom: 370 }
      const largeCardHeight = 450
      const res = placeNearAnchor(anchorRect, cardWidth, largeCardHeight, {
        viewport: { width: 1000, height: 600 },
        edgeMargin: 12,
        gap: 8,
        safeAreaTop: 40,
      })

      // When falling back, viewportY + maxHeight should not exceed viewport.height - edgeMargin
      expect(res.maxHeight).toBeDefined()
      if (res.maxHeight) {
        expect(res.viewportY + res.maxHeight).toBeLessThanOrEqual(600 - 12)
      }
    })

    it('measures getAppHeaderBottom from DOM element when present', () => {
      expect(getAppHeaderBottom()).toBe(36)

      const headerEl = document.createElement('div')
      headerEl.className = 'header'
      Object.defineProperty(headerEl, 'getBoundingClientRect', {
        value: () => ({ bottom: 37, top: 0, left: 0, right: 1000, height: 37, width: 1000 }),
      })
      document.body.appendChild(headerEl)

      expect(getAppHeaderBottom()).toBe(37)

      headerEl.remove()
    })
  })

  describe('CodeLinkPreviewCache (LRU)', () => {
    let cache: CodeLinkPreviewCache

    beforeEach(() => {
      cache = new CodeLinkPreviewCache(3, 10000, 1000, 50000)
    })

    it('builds canonical keys', () => {
      expect(cache.buildKey('/root/dir', 'src/file.ts')).toBe('/root/dir::src/file.ts')
      expect(cache.buildKey('C:\\root', 'src\\file.ts')).toBe('C:/root::src/file.ts')
    })

    it('sets and gets cached items within TTL', () => {
      const key = 'test::file.ts'
      const item = { content: 'hello', name: 'file.ts', path: 'file.ts', supported: true, size: 5 }
      const success = cache.set(key, item, 1000)
      expect(success).toBe(true)
      expect(cache.get(key, 1500)).toMatchObject(item)
    })

    it('expires items past TTL', () => {
      const key = 'test::file.ts'
      const item = { content: 'hello', name: 'file.ts', path: 'file.ts', supported: true, size: 5 }
      cache.set(key, item, 1000)
      expect(cache.get(key, 2500)).toBeUndefined()
    })

    it('does not cache content exceeding largeFileThreshold', () => {
      const key = 'test::large.ts'
      const largeItem = {
        content: 'x'.repeat(60000),
        name: 'large.ts',
        path: 'large.ts',
        supported: true,
        size: 60000,
      }
      const success = cache.set(key, largeItem)
      expect(success).toBe(false)
      expect(cache.get(key)).toBeUndefined()
    })

    it('caches a small window of a large file (budget tracks held content)', () => {
      // A windowed response for a 50 MB file holds only a few lines. The old
      // whole-file size must not disqualify it, or large files would never cache.
      const key = 'test::big.ts::1-200'
      const windowed = {
        content: 'line\n'.repeat(50),
        name: 'big.ts',
        path: 'big.ts',
        supported: true,
        size: 50 * 1024 * 1024,
        totalLines: 1_000_000,
        windowStart: 1,
        windowEnd: 50,
      }
      expect(cache.set(key, windowed)).toBe(true)
      expect(cache.get(key)?.windowStart).toBe(1)
    })

    it('evicts least recently used item when maxItems is reached', () => {
      cache.set('k1', { content: '1', name: '1', path: '1', supported: true, size: 1 }, 1000)
      cache.set('k2', { content: '2', name: '2', path: '2', supported: true, size: 1 }, 1000)
      cache.set('k3', { content: '3', name: '3', path: '3', supported: true, size: 1 }, 1000)

      // Access k1 to make it most recently used (order now k2, k3, k1)
      cache.get('k1', 1100)

      // Insert k4 -> should evict k2
      cache.set('k4', { content: '4', name: '4', path: '4', supported: true, size: 1 }, 1200)

      expect(cache.get('k2', 1300)).toBeUndefined()
      expect(cache.get('k1', 1300)).toBeDefined()
      expect(cache.get('k3', 1300)).toBeDefined()
      expect(cache.get('k4', 1300)).toBeDefined()
    })

    it('evicts items when maxBytes is exceeded', () => {
      const byteCache = new CodeLinkPreviewCache(10, 2000, 10000, 50000)
      // Each item content of 400 chars has estimatedBytes = 400*2 + 512 = 1312 bytes
      byteCache.set('b1', { content: 'x'.repeat(400), name: '1', path: '1', supported: true, size: 400 }, 1000)
      expect(byteCache.size).toBe(1)

      // Second item will exceed 2000 bytes -> evicts b1
      byteCache.set('b2', { content: 'y'.repeat(400), name: '2', path: '2', supported: true, size: 400 }, 1000)
      expect(byteCache.size).toBe(1)
      expect(byteCache.get('b1', 1000)).toBeUndefined()
      expect(byteCache.get('b2', 1000)).toBeDefined()
    })

    it('clears all items', () => {
      cache.set('k1', { content: '1', name: '1', path: '1', supported: true, size: 1 })
      expect(cache.size).toBe(1)
      cache.clear()
      expect(cache.size).toBe(0)
      expect(cache.totalEstimatedBytes).toBe(0)
    })
  })
})
