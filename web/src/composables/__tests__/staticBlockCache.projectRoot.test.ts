import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { ref, reactive, nextTick } from 'vue'

/**
 * Guards the rendered-block cache against being reused across a projectRoot
 * change.
 *
 * The hazard: `annotateFilePaths` resolves paths RELATIVE TO projectRoot, so
 * the rendered HTML of a block is a function of (text, projectRoot) — not of
 * text alone. An absolute path inside the project is rewritten project-relative
 * (`/proj/.worktrees/feat/src/a.ts` -> `src/a.ts`); the same text under the
 * main repo root becomes `.worktrees/feat/src/a.ts`. Two different strings.
 *
 * `StaticBlockCache`'s key is `${msgId}-${blockIdx}-${len}-${prefix}${suffix}`
 * — message id, block index and text, but NOT projectRoot. So the cache must be
 * invalidated when the project changes, or a stale entry (with the previous
 * project's path annotations) is served.
 *
 * The store is a REAL `reactive` object here, because the fix under test is a
 * `watch(() => store.state.projectRoot, …)` — a plain-object mock would make
 * that watcher silently never fire and the test would pass for the wrong
 * reason.
 */

const appState = reactive({
  projectRoot: '/home/x/proj',
  homeDir: '/home/x',
  tasks: [] as unknown[],
})

vi.mock('@/stores/app', () => ({ store: { state: appState } }))

vi.mock('@/composables/useLocale', () => ({ gt: (k: string) => k }))

vi.mock('@/utils/globals.ts', () => ({
  marked: { parse: (s: string) => `<p>${s}</p>`, use: vi.fn() },
  katex: { renderToString: () => '' },
  DOMPurify: { sanitize: (s: string) => s },
  highlightCode: (c: string) => c,
}))
vi.mock('@/utils/markedConfig.ts', () => ({ resetHeadingIds: vi.fn() }))
vi.mock('@/utils/tableRowExpand.ts', () => ({ injectTableRowAttrsIn: () => false }))
vi.mock('@/composables/useCodeBlockHeader.ts', () => ({
  annotateCodeBlockHeadersIn: () => {},
  annotateTableBlockHeadersIn: () => {},
}))
vi.mock('@/utils/mediaBlockFactory.ts', () => ({ annotateMediaBlocksIn: () => {} }))
vi.mock('@/utils/chatRenderUtils.ts', () => ({
  rewriteImageUrls: (h: string) => h,
  markInlineSvgs: (h: string) => h,
  convertAudioLinks: (h: string) => h,
  convertVideoLinks: (h: string) => h,
  getThumbWidth: () => 800,
}))
vi.mock('@/composables/usePlatformDetect.ts', () => ({
  usePlatformDetect: () => ({ isPC: { value: true } }),
}))
vi.mock('@/composables/useWorktreeAnnotation.ts', () => ({
  annotateWorktreePathsIn: () => ({ detectedWorktreePaths: [], applied: false }),
  useWorktreeAnnotation: () => ({}),
}))
vi.mock('@/composables/useLocalhostAnnotation.ts', () => ({
  annotateLocalhostUrlsIn: () => false,
  useLocalhostAnnotation: () => ({}),
}))
vi.mock('@/composables/useCommitHashAnnotation.ts', () => ({
  annotateCommitHashesIn: () => [],
  useCommitHashAnnotation: () => ({ verifyCommitHashes: vi.fn() }),
  clearCommitHashCache: vi.fn(),
}))
vi.mock('@/composables/useThinkingContent.ts', () => ({ clearThinkingCache: vi.fn() }))
vi.mock('@/utils/api', () => ({ apiGet: vi.fn() }))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
vi.mock('@/utils/renderToolDetail.ts', () => ({ formatToolInput: vi.fn() }))
vi.mock('@/utils/taskBlockStore.ts', () => ({
  createTaskBlockStore: () => ({ blocks: {}, fetchBatchData: vi.fn() }),
}))
vi.mock('@/utils/chatBlocks.ts', () => ({
  parseAssistantContent: vi.fn(),
  toolCallSummary: vi.fn(),
  hasImagesInContent: vi.fn(() => false),
  formatMessageTime: vi.fn(),
  formatDetailTime: vi.fn(),
  truncate: (s: string) => s,
}))
vi.mock('@/utils/streamPerf.ts', async (importOriginal) => {
  // Keep the REAL StaticBlockCache — it is under test.
  const actual = await importOriginal<typeof import('@/utils/streamPerf.ts')>()
  return {
    ...actual,
    extractScheduledTaskIds: () => [],
    stripScheduledTaskTags: (s: string) => s,
    detectAskQuestion: () => ({ found: false, items: [], matches: [], reasons: [] }),
    stripAskQuestionTag: (s: string) => s,
    taskChanged: () => false,
  }
})

// A worktree path: rendered under the MAIN repo root it is project-relative;
// rendered under the WORKTREE root it is the same file but a different string.
const WT_FILE = '/home/x/proj/.worktrees/feat/src/a.ts'
const TEXT = `edit ${WT_FILE} now`
const MAIN_ROOT = '/home/x/proj'
const WORKTREE_ROOT = '/home/x/proj/.worktrees/feat'

async function setup() {
  const { useChatRender } = await import('@/composables/useChatRender.ts')
  return useChatRender({
    messages: ref([]),
    theme: ref('light'),
    currentSessionId: ref(''),
  })
}

beforeEach(() => {
  appState.projectRoot = MAIN_ROOT
  appState.homeDir = '/home/x'
})

afterEach(() => {
  vi.resetModules()
})

describe('premise: rendered HTML depends on projectRoot', () => {
  it('same text renders differently under the repo root vs its worktree root', async () => {
    const { renderTextBlock } = await setup()

    const underMain = renderTextBlock(TEXT, '4242', 0, false, false)
    appState.projectRoot = WORKTREE_ROOT
    const underWorktree = renderTextBlock(TEXT, '4242', 0, false, false)

    expect(underMain).not.toBe(underWorktree)
    expect(underMain).toContain('data-file-path=".worktrees/feat/src/a.ts"')
    expect(underWorktree).toContain('data-file-path="src/a.ts"')
  })
})

describe('StaticBlockCache key ignores projectRoot (the hazard)', () => {
  it('would return the entry stored under a different projectRoot if not cleared', async () => {
    const { StaticBlockCache } = await import('@/utils/streamPerf.ts')
    const cache = new StaticBlockCache()

    cache.set('4242', 0, TEXT, '<p>MAIN_ROOT_HTML</p>')
    appState.projectRoot = WORKTREE_ROOT

    // The raw cache has no notion of projectRoot — this documents why an
    // explicit invalidation is required.
    expect(cache.get('4242', 0, TEXT)).toBe('<p>MAIN_ROOT_HTML</p>')
  })
})

describe('projectRoot watcher invalidates the cache', () => {
  it('drops cached entries when projectRoot changes', async () => {
    const { renderTextBlock, staticBlockCache } = await setup()

    const htmlMain = renderTextBlock(TEXT, '4242', 0, false, false)
    staticBlockCache.set('4242', 0, TEXT, htmlMain, false)
    expect(staticBlockCache.get('4242', 0, TEXT)).toBe(htmlMain)

    appState.projectRoot = WORKTREE_ROOT
    await nextTick() // watchers flush on the microtask queue by default

    expect(staticBlockCache.get('4242', 0, TEXT)).toBeUndefined()
  })

  it('re-renders with the NEW project root after the switch', async () => {
    const { renderTextBlock, staticBlockCache } = await setup()

    // Render + cache under the main root (ContentBlocks.getBlockHtml order:
    // get() -> miss -> renderTextBlock() -> set()).
    const htmlMain = renderTextBlock(TEXT, '4242', 0, false, false)
    staticBlockCache.set('4242', 0, TEXT, htmlMain, false)
    expect(htmlMain).toContain('data-file-path=".worktrees/feat/src/a.ts"')

    appState.projectRoot = WORKTREE_ROOT
    await nextTick()

    // Same read sequence ContentBlocks performs after the switch.
    const hit = staticBlockCache.get('4242', 0, TEXT)
    const served = hit !== undefined ? hit : renderTextBlock(TEXT, '4242', 0, false, false)

    // The user must see the worktree-relative annotation, not the stale one.
    expect(served).toContain('data-file-path="src/a.ts"')
    expect(served).not.toContain('data-file-path=".worktrees/feat/src/a.ts"')
  })

  it('keeps entries when an unrelated state field changes', async () => {
    const { renderTextBlock, staticBlockCache } = await setup()

    const html = renderTextBlock(TEXT, '4242', 0, false, false)
    staticBlockCache.set('4242', 0, TEXT, html, false)

    // homeDir is not part of the invalidation contract for this fix; a bare
    // state mutation that is not projectRoot must not nuke the cache (that
    // would defeat the retention the session-switch watcher is there for).
    appState.homeDir = '/home/other'
    await nextTick()

    expect(staticBlockCache.get('4242', 0, TEXT)).toBe(html)
  })

  it('does not clear on session switch (the retention this fix must preserve)', async () => {
    const { useChatRender } = await import('@/composables/useChatRender.ts')
    const sessionId = ref('sess-1')
    const { renderTextBlock, staticBlockCache } = useChatRender({
      messages: ref([]),
      theme: ref('light'),
      currentSessionId: sessionId,
    })

    const html = renderTextBlock(TEXT, '4242', 0, false, false)
    staticBlockCache.set('4242', 0, TEXT, html, false)

    sessionId.value = 'sess-2'
    await nextTick()

    // Session switches must NOT drop the cache — that retention is the whole
    // point of removing the old `clear()` here.
    expect(staticBlockCache.get('4242', 0, TEXT)).toBe(html)
  })
})

/**
 * The invalidation above only matters if the cache is actually reachable from
 * the render path. It previously was not: `ChatPanelContent` never bound
 * `staticBlockCache` on `ChatMessageList`, so `ContentBlocks` received its
 * `default: null` and the whole cache was dead code. These source-contract
 * checks pin the wiring, since a unit test on the cache alone cannot see a
 * missing prop binding.
 */
describe('staticBlockCache wiring (source contract)', () => {
  // Paths are resolved from this file's own directory (__dirname), not the
  // process cwd — the suite runs from the repo root in CI and from web/ locally.
  const read = (rel: string) => readFileSync(resolve(__dirname, rel), 'utf8')

  it('ChatPanelContent binds the cache from useChatRender onto ChatMessageList', () => {
    const src = read('../../components/chat/ChatPanelContent.vue')
    expect(src).toMatch(/:staticBlockCache="render\.staticBlockCache"/)
  })

  it('ChatMessageList forwards the cache to ChatMessageItem', () => {
    const src = read('../../components/chat/ChatMessageList.vue')
    expect(src).toMatch(/:staticBlockCache="staticBlockCache"/)
    expect(src).toMatch(/staticBlockCache: Object/)
  })

  it('ChatMessageItem forwards the cache to ContentBlocks', () => {
    const src = read('../../components/chat/ChatMessageItem.vue')
    expect(src).toMatch(/:staticBlockCache="staticBlockCache"/)
    expect(src).toMatch(/staticBlockCache: Object/)
  })

  it('ContentBlocks consumes the cache in getBlockHtml', () => {
    const src = read('../../components/chat/ContentBlocks.vue')
    expect(src).toMatch(/props\.staticBlockCache\.get\(/)
    expect(src).toMatch(/props\.staticBlockCache\.set\(/)
  })
})
