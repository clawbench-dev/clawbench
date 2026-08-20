import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'

// ── Mock dependencies ──

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
  useFilePathAnnotation: () => ({
    verifyFilePaths: vi.fn(),
  }),
}))

vi.mock('@/composables/useCommitHashAnnotation', () => ({
  useCommitHashAnnotation: () => ({
    verifyCommitHashes: vi.fn(),
  }),
}))

vi.mock('@/composables/useThinkingContent', () => ({
  clearThinkingCache: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  store: {
    state: { tasks: [] },
  },
}))

vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
}))

vi.mock('@/utils/renderToolDetail', () => ({
  formatToolInput: vi.fn(),
}))

vi.mock('@/utils/streamPerf', () => ({
  extractScheduledTaskIds: vi.fn(() => []),
  stripScheduledTaskTags: vi.fn((t: string) => t),
  detectAskQuestion: vi.fn(() => ({ found: false })),
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

vi.mock('@/utils/chatRenderUtils', () => ({
  parseAskQuestionContent: vi.fn(),
}))

vi.mock('@/utils/chatBlocks', () => ({
  parseAssistantContent: vi.fn(),
  toolCallSummary: vi.fn(),
  hasImagesInContent: vi.fn(),
  formatMessageTime: vi.fn(),
  formatDetailTime: vi.fn(),
  truncate: vi.fn(),
}))

vi.mock('@/utils/format', () => ({
  humanizeCron: vi.fn(),
  repeatLabel: vi.fn(),
}))

vi.mock('@/utils/taskBlockStore', () => ({
  createTaskBlockStore: () => ({
    blocks: {},
    fetchBatchData: vi.fn(),
  }),
}))

import { useChatRender } from '@/composables/useChatRender'
import { renderMermaidInElement } from '@/composables/useMarkdownRenderer'

describe('useChatRender — updateRenderedContents', () => {
  let chatContainer: HTMLDivElement | null = null

  beforeEach(() => {
    vi.clearAllMocks()
    // Create the DOM element that updateRenderedContents looks up via getElementById
    chatContainer = document.createElement('div')
    chatContainer.id = 'aiChatMessages'
    document.body.appendChild(chatContainer)
  })

  afterEach(() => {
    if (chatContainer && chatContainer.parentNode) {
      chatContainer.parentNode.removeChild(chatContainer)
    }
    chatContainer = null
  })

  function createRender(messages: any[] = []) {
    return useChatRender({
      messages: { value: messages },
      theme: ref('dark'),
      currentSessionId: ref('test-session'),
    })
  }

  describe('forceFullRender=true', () => {
    it('should call renderMermaidInElement via nextTick', async () => {
      const render = createRender([{ role: 'user', id: 1, content: 'hi' }])

      render.updateRenderedContents(true)

      // Mermaid rendering is deferred to nextTick
      expect(renderMermaidInElement).not.toHaveBeenCalled()
      await nextTick()
      expect(renderMermaidInElement).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        'chat-mermaid',
      )
    })

    it('should reset lastRenderedCount to messages.length', () => {
      const msgs = [{ role: 'user', id: 1 }, { role: 'assistant', id: 2 }]
      const render = createRender(msgs)

      render.updateRenderedContents(true)

      // After forceFullRender, a subsequent non-force call with same count
      // should see newMsgCount=0 and early-return (no mermaid render).
      // This verifies lastRenderedCount was updated.
      ;(renderMermaidInElement as ReturnType<typeof vi.fn>).mockClear()
      render.updateRenderedContents(false)

      // newMsgCount = 2 - 2 = 0 → early return, no Mermaid render
      expect(renderMermaidInElement).not.toHaveBeenCalled()
    })
  })

  describe('forceFullRender=false with new messages', () => {
    it('should not render Mermaid (deferred to post-streaming forceFullRender)', () => {
      const msgs = [{ role: 'user', id: 1 }]
      const render = createRender(msgs)

      // Simulate lastRenderedCount=0 by not having called updateRenderedContents yet
      render.updateRenderedContents(false)

      // During streaming, Mermaid is not rendered
      expect(renderMermaidInElement).not.toHaveBeenCalled()
    })
  })

  describe('forceFullRender=false with no new messages (newMsgCount <= 0)', () => {
    it('should early-return and not render Mermaid — the #387 bug path', async () => {
      const msgs = [{ role: 'user', id: 1 }, { role: 'assistant', id: 2 }]
      const render = createRender(msgs)

      // First call sets lastRenderedCount = messages.length
      render.updateRenderedContents(true)
      await nextTick()
      ;(renderMermaidInElement as ReturnType<typeof vi.fn>).mockClear()

      // Second call with forceFull=false and same message count → early return
      render.updateRenderedContents(false)

      expect(renderMermaidInElement).not.toHaveBeenCalled()
    })
  })

  describe('defensive: lastRenderedCount > messages.length', () => {
    it('should auto-force full render when messages shrink (loadHistory replaced messages)', async () => {
      // Start with 3 messages
      const msgs = ref([{ role: 'user', id: 1 }, { role: 'assistant', id: 2 }, { role: 'user', id: 3 }])
      const render = useChatRender({
        messages: msgs,
        theme: { value: 'dark' },
        currentSessionId: { value: 'test-session' },
      })

      // Force render sets lastRenderedCount = 3
      render.updateRenderedContents(true)
      await nextTick()
      ;(renderMermaidInElement as ReturnType<typeof vi.fn>).mockClear()

      // loadHistory replaces messages with only 2 (DB dedup, etc.)
      msgs.value = [{ role: 'user', id: 1 }, { role: 'assistant', id: 2 }]

      // Even with forceFull=false, the defensive check should auto-force
      render.updateRenderedContents(false)

      await nextTick()
      expect(renderMermaidInElement).toHaveBeenCalled()
    })
  })

  describe('Mermaid re-render after DOM replacement (#387)', () => {
    it('should re-render Mermaid when updateRenderedContents(true) is called after messages are replaced', async () => {
      const msgs = ref([{ role: 'user', id: 1 }, { role: 'assistant', id: 2 }])
      const render = useChatRender({
        messages: msgs,
        theme: { value: 'dark' },
        currentSessionId: { value: 'test-session' },
      })

      // Step 1: stream done → forceFullRender on old DOM
      render.updateRenderedContents(true)
      await nextTick()
      ;(renderMermaidInElement as ReturnType<typeof vi.fn>).mockClear()

      // Step 2: loadHistory replaces messages (same count but new objects)
      msgs.value = [{ role: 'user', id: 1, content: 'hello' }, { role: 'assistant', id: 2, content: 'world' }]

      // Step 3: Without the fix, updateRenderedContents(false) would early-return
      // With the fix, the caller passes forceFull=true after loadHistory
      render.updateRenderedContents(true)

      await nextTick()
      expect(renderMermaidInElement).toHaveBeenCalledWith(
        expect.any(HTMLElement),
        'chat-mermaid',
      )
    })
  })
})
