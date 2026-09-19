import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

/**
 * Guards the "one parse for the four DOM-annotation steps" optimization.
 *
 * Before: each of annotateWorktreePaths / annotateFilePaths /
 * annotateCommitHashes / annotateLocalhostUrls parsed the full HTML, mutated,
 * and re-serialized (`body.innerHTML`) on its own. Chained, that was four
 * parse+serialize round trips per block — the dominant main-thread cost in the
 * heavy-session freeze (Chrome trace: ~70% of samples across those three
 * annotators, producing multi-second 100%-busy spans).
 *
 * These tests assert the SHARING contract, which a behaviour-only test cannot
 * see: all four `In` steps must receive the SAME Document instance, and
 * DOMParser.parseFromString must be called once (not once per step) for the
 * annotation block.
 *
 * A plain "output looks right" test would pass even if we reverted to four
 * parses, so the spy counts are the actual regression guard.
 */

const parseSpy = vi.fn()
const seenDocs: Document[] = []

vi.mock('@/utils/globals.ts', () => ({
  marked: { parse: (s: string) => `<p>${s}</p>`, use: vi.fn() },
  katex: { renderToString: () => '' },
  DOMPurify: { sanitize: (s: string) => s },
}))

vi.mock('@/utils/markedConfig.ts', () => ({ resetHeadingIds: vi.fn() }))
vi.mock('@/utils/tableRowExpand.ts', () => ({
  injectTableRowAttrsIn: (doc: Document) => { seenDocs.push(doc); return true },
}))
vi.mock('@/composables/useCodeBlockHeader.ts', () => ({
  annotateCodeBlockHeadersIn: (doc: Document) => { seenDocs.push(doc) },
  annotateTableBlockHeadersIn: (doc: Document) => { seenDocs.push(doc) },
}))
vi.mock('@/utils/mediaBlockFactory.ts', () => ({
  annotateMediaBlocksIn: (doc: Document) => { seenDocs.push(doc) },
}))
vi.mock('@/composables/usePlatformDetect.ts', () => ({
  usePlatformDetect: () => ({ isPC: { value: true } }),
}))
vi.mock('@/utils/chatRenderUtils.ts', () => ({
  rewriteImageUrls: (h: string) => h,
  markInlineSvgs: (h: string) => h,
  convertAudioLinks: (h: string) => h,
  convertVideoLinks: (h: string) => h,
  getThumbWidth: () => 400,
}))
vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '/proj', homeDir: '/home/u' } },
}))

// Each `In` step records the Document it was handed.
vi.mock('@/composables/useWorktreeAnnotation.ts', () => ({
  annotateWorktreePathsIn: (doc: Document) => {
    seenDocs.push(doc)
    return { detectedWorktreePaths: [], applied: true }
  },
}))
vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  annotateFilePathsIn: (doc: Document) => {
    seenDocs.push(doc)
    return ['/proj/a.ts']
  },
}))
vi.mock('@/composables/useCommitHashAnnotation.ts', () => ({
  annotateCommitHashesIn: (doc: Document) => {
    seenDocs.push(doc)
    return ['deadbeef']
  },
}))
vi.mock('@/composables/useLocalhostAnnotation.ts', () => ({
  annotateLocalhostUrlsIn: (doc: Document) => {
    seenDocs.push(doc)
    return true
  },
}))

// Capture the REAL DOMParser once, at module scope.
//
// Capturing it inside beforeEach would be self-referential: after the first
// beforeEach stubs the global, the "real" reference on the second run would be
// the previous WRAPPER — nesting the wrappers so that one logical parse counts
// twice. That is a bug in the test, not in the code under test.
const RealDOMParser = DOMParser

beforeEach(() => {
  seenDocs.length = 0
  parseSpy.mockClear()
  vi.stubGlobal('DOMParser', class extends RealDOMParser {
    parseFromString(...args: Parameters<DOMParser['parseFromString']>) {
      parseSpy()
      return super.parseFromString(...args)
    }
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('renderMarkdown annotation steps share one Document per phase', () => {
  it('shares one Document across the table/code/table-header phase', async () => {
    const { renderMarkdown } = await import('@/composables/useMarkdownRenderer.ts')
    renderMarkdown('hello `a.ts` world')

    // seenDocs order: [table, codeHeaders, tableHeaders, worktree, filePaths, commit, localhost, media]
    const tablePhase = seenDocs.slice(0, 3)
    expect(tablePhase).toHaveLength(3)
    expect(tablePhase[0]).toBe(tablePhase[1])
    expect(tablePhase[1]).toBe(tablePhase[2])
  })

  it('shares one Document across the path-annotation phase', async () => {
    const { renderMarkdown } = await import('@/composables/useMarkdownRenderer.ts')
    renderMarkdown('hello `a.ts` world')

    const pathPhase = seenDocs.slice(3, 7)
    expect(pathPhase).toHaveLength(4)
    expect(pathPhase[0]).toBe(pathPhase[1])
    expect(pathPhase[1]).toBe(pathPhase[2])
    expect(pathPhase[2]).toBe(pathPhase[3])
  })

  it('parses three times total, not once per annotator', async () => {
    const { renderMarkdown } = await import('@/composables/useMarkdownRenderer.ts')
    renderMarkdown('hello `a.ts` world')

    // Three phases: (1) table/code headers, (2) path annotators, (3) media.
    // Eight annotators share these three Documents. A regression to per-step
    // parsing makes this 8.
    expect(parseSpy).toHaveBeenCalledTimes(3)
  })

  it('returns detections from the shared pass', async () => {
    const { renderMarkdown } = await import('@/composables/useMarkdownRenderer.ts')
    const out = renderMarkdown('hello `a.ts` world')

    expect(out.detectedPaths).toEqual(['/proj/a.ts'])
    expect(out.detectedSHAs).toEqual(['deadbeef'])
  })

  it('still runs the table/code-header phase when skipEnhancements is set', async () => {
    const { renderMarkdown } = await import('@/composables/useMarkdownRenderer.ts')
    renderMarkdown('hello `a.ts` world', { skipEnhancements: true })

    // Steps 6-8 are not enhancements — they run for every render, including
    // streaming. Only the path/media steps are skipped.
    expect(seenDocs).toHaveLength(3)
    expect(parseSpy).toHaveBeenCalledTimes(1)
  })
})
