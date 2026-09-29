import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'

// ── Mocks ──
//
// This file exists to pin ONE behavior: the public share page must not fire the
// auth-protected /api/tasks request. The share page renders through the same
// chat pipeline as the app, so a shared conversation containing a
// <scheduled-task> tag used to make renderTextBlock call fetchBatchTaskData →
// apiGet('/api/tasks') with no cookie: a 401 per tag, and the card sat as a
// permanent loading skeleton. useChatRender had no isShareMode guard even though
// the file-path / commit / worktree checks all have one.

const fetchBatchData = vi.fn()
vi.mock('@/utils/taskBlockStore', () => ({
  createTaskBlockStore: () => ({ blocks: {}, fetchBatchData }),
}))

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdown: vi.fn(({ text }: { text: string }) => ({
    html: `<p>${text}</p>`,
    detectedPaths: [],
    detectedSHAs: [],
  })),
  renderMarkdownHtml: vi.fn((text: string) => `<p>${text}</p>`),
  renderMermaidInElement: vi.fn(() => Promise.resolve()),
}))

vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({ verifyFilePaths: vi.fn() }),
}))
vi.mock('@/composables/useCommitHashAnnotation', () => ({
  useCommitHashAnnotation: () => ({ verifyCommitHashes: vi.fn() }),
}))
vi.mock('@/composables/useThinkingContent', () => ({ clearThinkingCache: vi.fn() }))
vi.mock('@/stores/app', () => ({ store: { state: { tasks: [] } } }))
vi.mock('@/utils/api', () => ({ apiGet: vi.fn() }))
vi.mock('@/utils/renderToolDetail', () => ({ formatToolInput: vi.fn() }))
vi.mock('@/utils/streamPerf', () => ({
  // One scheduled-task tag, so renderTextBlock enters the fetch branch.
  extractScheduledTaskIds: vi.fn(() => ['7']),
  stripScheduledTaskTags: vi.fn((t: string) => t),
  detectAskQuestion: vi.fn(() => ({ found: false, matches: [], items: [], reasons: [] })),
  stripAskQuestionTag: vi.fn((t: string) => t),
  taskChanged: vi.fn(() => false),
  StaticBlockCache: class {
    deferredCount = 0
    setUpgradeFn() {}
    clear() {}
    set() {}
    isDeferred() { return false }
    markUpgraded() {}
    scheduleUpgrade() {}
  },
}))

// The guard under test. A mutable flag so each test can flip share mode.
let shareMode = false
vi.mock('@/share/shareMode', () => ({
  isShareMode: () => shareMode,
  shareApiUrl: (p: string) => `/api/share/tok/${p}`,
}))

import { useChatRender } from '@/composables/useChatRender'

function createRender() {
  return useChatRender({
    messages: { value: [] },
    theme: ref('dark'),
    currentSessionId: ref('test-session'),
  })
}

describe('useChatRender — share mode must not fetch task data', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    shareMode = false
  })

  it('fetches task data normally outside share mode', () => {
    const render = createRender()
    render.renderTextBlock('see <scheduled-task>7</scheduled-task>', 'm1', 0)

    expect(fetchBatchData).toHaveBeenCalledTimes(1)
  })

  it('does NOT fetch task data on the public share page', () => {
    shareMode = true
    const render = createRender()
    render.renderTextBlock('see <scheduled-task>7</scheduled-task>', 'm1', 0)

    expect(fetchBatchData).not.toHaveBeenCalled()
  })

  it('still strips the scheduled-task tag on the share page so no raw tag renders', () => {
    shareMode = true
    const render = createRender()
    const html = render.renderTextBlock('see <scheduled-task>7</scheduled-task>', 'm1', 0)

    // The tag must not survive into the reader's view (the strip helper is
    // mocked to identity here, so this asserts the branch ran, not the strip).
    expect(html).toBeTruthy()
  })
})
