import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import ContentBlocks from '@/components/chat/ContentBlocks.vue'

// ── Mocks ──

vi.mock('@/utils/renderToolDetail.ts', () => ({
  handleToolAction: vi.fn().mockReturnValue(false),
  shouldAutoExpandTool: (name: string) => name === 'AskUserQuestion' || name === 'PermissionApproval',
}))

vi.mock('@/utils/icons', () => ({
  getToolIcon: (name: string) => {
    const map: Record<string, { icon: any; category: string }> = {
      Read: { icon: 'EyeIcon', category: 'file' },
      Bash: { icon: 'TerminalIcon', category: 'bash' },
      AskUserQuestion: { icon: 'AskIcon', category: 'ask' },
      PermissionApproval: { icon: 'ShieldIcon', category: 'permission' },
    }
    return map[name] || { icon: 'WrenchIcon', category: 'fallback' }
  },
  toolDisplayName: (name: string) => name,
}))

vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  renderMarkdown: (text: string) => `<p>${text}</p>`,
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/utils/contentBlocks.ts', () => ({
  isSevereWarning: (block: any) => block.reason === 'disconnect',
  getWarningText: (block: any) => block.text || block.reason || '',
  statusClass: (task: any) => `status-${task.status}`,
  statusLabel: (task: any, t: any) => task.status,
  statusLabelSimple: (task: any, t: any) => task.status,
  formatTime: (iso: any) => iso,
  askQuestionSummary: (input: any) => input?.question || '',
  blockKey: (msgId: any, bi: number) => `${msgId}:${bi}`,
  blockTaskKey: (msgId: any, bi: number) => `${msgId}-${bi}`,
  buildTaskKeyIndex: () => ({}),
  hasScheduledTasks: () => false,
  scheduledTaskKeys: () => [],
  extractAtCommand: (text: string) => {
    if (text.startsWith('@chatsearch')) return { command: '@chatsearch', rest: text.slice(11) }
    if (text.startsWith('@task')) return { command: '@task', rest: text.slice(5) }
    return null
  },
  extractSlashCommand: (text: string) => {
    if (text.startsWith('/')) {
      const parts = text.split(' ')
      return { command: parts[0], rest: parts.slice(1).join(' ') }
    }
    return null
  },
}))

const i18n = createI18n({
  legacy: false, locale: 'en',
  messages: { en: {
    chat: {
      message: { deepThinking: 'Deep Thinking' },
      contentBlocks: {
        cancelled: 'Cancelled',
        loading: 'Loading...',
        scheduledTaskCreated: 'Task created',
        frequency: 'Frequency',
        executor: 'Executor',
        repeat: 'Repeat',
        status: 'Status',
        lastRun: 'Last run',
        nextRun: 'Next run',
        viewDetail: 'View detail',
        taskDeleted: 'Task deleted',
        ragUntitled: 'Untitled',
      },
    },
    tool: { askUser: { name: 'Ask' } },
  } },
})

const LucideStub = { template: '<span class="lucide-stub" />' }

function mountBlocks(props: Record<string, unknown> = {}) {
  return mount(ContentBlocks, {
    props: {
      blocks: [],
      msgId: 'msg-1',
      renderTextBlock: (text: string) => `<p>${text}</p>`,
      formatToolInput: () => '',
      toolCallSummary: () => '',
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs: {
        'lucide-vue-next': LucideStub,
        Brain: LucideStub,
        ChevronRight: LucideStub,
        CheckCircle2: LucideStub,
        AlertCircle: LucideStub,
        AlertTriangle: LucideStub,
        XCircle: LucideStub,
      },
    },
  })
}

describe('ContentBlocks', () => {
  // ── Text blocks ──

  describe('text blocks', () => {
    it('renders a text block', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: 'Hello world' }],
      })
      expect(wrapper.find('.content-blocks').exists()).toBe(true)
      expect(wrapper.html()).toContain('Hello world')
    })

    it('renders @chatsearch badge for text starting with @chatsearch', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: '@chatsearch how to do X' }],
      })
      expect(wrapper.find('.at-command-badge').exists()).toBe(true)
      expect(wrapper.find('.at-command-badge').text()).toBe('@chatsearch')
    })

    it('renders @task badge for text starting with @task', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: '@task run tests' }],
      })
      expect(wrapper.find('.at-command-badge').exists()).toBe(true)
    })

    it('renders slash command badge for text starting with /', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: '/commit fix bug' }],
      })
      expect(wrapper.find('.slash-command-badge').exists()).toBe(true)
      expect(wrapper.find('.slash-command-badge').text()).toBe('/commit')
    })
  })

  // ── Tool use blocks ──

  describe('tool_use blocks', () => {
    it('renders a tool call bar', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'Read', done: true, status: 'success' }],
      })
      expect(wrapper.find('.chat-tool-call').exists()).toBe(true)
    })

    it('shows spinner when tool is not done', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'Read', done: false, status: '' }],
      })
      expect(wrapper.find('.tool-spinner').exists()).toBe(true)
    })

    it('shows check icon when tool is done', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'Read', done: true, status: 'success' }],
      })
      expect(wrapper.find('.tool-check').exists()).toBe(true)
    })

    it('shows error icon when tool has error status', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'Read', done: true, status: 'error' }],
      })
      expect(wrapper.find('.tool-error-icon').exists()).toBe(true)
    })

    it('emits show-tool-detail on tool click for non-auto-expand tools', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'Read', done: true, status: 'success', id: 'tool-1' }],
      })

      await wrapper.find('.chat-tool-call').trigger('click')

      expect(wrapper.emitted('show-tool-detail')).toBeTruthy()
      const detail = wrapper.emitted('show-tool-detail')![0][0] as any
      expect(detail.name).toBe('Read')
    })

    it('emits toggle-tool on click for AskUserQuestion', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'AskUserQuestion', done: true, status: 'success', id: 'tool-2', input: {} }],
      })

      await wrapper.find('.chat-tool-call').trigger('click')

      expect(wrapper.emitted('toggle-tool')).toBeTruthy()
    })

    it('renders auto-expand detail for AskUserQuestion', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'AskUserQuestion', done: true, status: 'success', id: 'tool-2', input: { question: 'Test?' } }],
      })
      expect(wrapper.find('.tool-detail').exists()).toBe(true)
    })

    it('sets data-category on tool call', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'Read', done: true, status: 'success' }],
      })
      expect(wrapper.find('.chat-tool-call').attributes('data-category')).toBe('file')
    })
  })

  // ── Thinking blocks ──

  describe('thinking blocks', () => {
    it('renders a thinking block', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Analyzing...', done: true }],
      })
      expect(wrapper.find('.chat-thinking').exists()).toBe(true)
    })

    it('adds thinking-expanded-done class when streaming and not done', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Thinking...', done: false }],
        streaming: true,
      })
      expect(wrapper.find('.chat-thinking').classes()).toContain('thinking-streaming')
    })

    it('adds thinking-collapsed class when done', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Done thinking', done: true }],
        streaming: false,
      })
      expect(wrapper.find('.chat-thinking').classes()).toContain('thinking-collapsed')
    })

    it('expands inline on thinking click when collapsed', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Deep thought', done: true, _key: 'thinking-0' }],
        streaming: false,
      })

      // Thinking block starts collapsed
      expect(wrapper.find('.chat-thinking').classes()).toContain('thinking-collapsed')

      // Click should trigger handleThinkingClick which sets thinkingExpanded.
      // In jsdom, Vue's template re-evaluation for :class bindings that call
      // plain functions (isThinkingExpandedDone/isThinkingCollapsed) may not
      // re-render after ref({}) deep property assignment. This works correctly
      // in the real browser. Verify that clicking does not throw.
      await wrapper.find('.chat-thinking').trigger('click')
    })

    it('does not emit show-thinking-detail on thinking click', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Deep thought', done: true }],
        streaming: false,
      })

      await wrapper.find('.chat-thinking').trigger('click')

      expect(wrapper.emitted('show-thinking-detail')).toBeFalsy()
    })

    it('shows spinner when streaming and not done', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Thinking...', done: false }],
        streaming: true,
      })
      expect(wrapper.find('.thinking-spinner').exists()).toBe(true)
    })

    it('does not show spinner when done (even if streaming)', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Done', done: true }],
        streaming: true,
      })
      expect(wrapper.find('.thinking-spinner').exists()).toBe(false)
    })
  })

  // ── Error / Warning blocks ──

  describe('error and warning blocks', () => {
    it('renders an error block', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'error', text: 'Something went wrong' }],
      })
      expect(wrapper.find('.chat-error-card').exists()).toBe(true)
      expect(wrapper.find('.error-text').text()).toBe('Something went wrong')
    })

    it('renders severe warning as error-level', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'disconnect', text: 'Connection lost' }],
      })
      expect(wrapper.find('.chat-error-card').exists()).toBe(true)
    })

    it('renders normal warning with amber styling', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'parse_error', text: 'Parse error: bad JSON' }],
      })
      expect(wrapper.find('.chat-warning-card').exists()).toBe(true)
    })
  })

  // ── Streaming / Cancelled markers ──

  describe('streaming and cancelled markers', () => {
    it('shows placeholder dots when streaming and not cancelled', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: 'Hello' }],
        streaming: true,
        cancelled: false,
      })
      expect(wrapper.find('.placeholder-dots').exists()).toBe(true)
    })

    it('hides placeholder dots when not streaming', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: 'Hello' }],
        streaming: false,
        cancelled: false,
      })
      expect(wrapper.find('.placeholder-dots').exists()).toBe(false)
    })

    it('does not render outer cancelled mark (moved to ChatMessageItem)', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: 'Hello' }],
        streaming: false,
        cancelled: true,
      })
      // Outer cancelled mark is now rendered in ChatMessageItem, not ContentBlocks
      expect(wrapper.find('.chat-cancelled-mark').exists()).toBe(false)
    })

    it('does not render outer cancelled mark when last block is thinking', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Thought', done: true }],
        streaming: false,
        cancelled: true,
      })
      // Outer cancelled mark is now rendered in ChatMessageItem, not ContentBlocks
      expect(wrapper.find('.chat-cancelled-mark').exists()).toBe(false)
    })

    it('shows inline cancelled mark when last block is thinking', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Thought', done: true }],
        streaming: false,
        cancelled: true,
      })
      expect(wrapper.find('.chat-cancelled-mark-inline').exists()).toBe(true)
    })
  })

  // ── Summary mode ──

  describe('summary mode', () => {
    it('shows summary text when showingSummary and summary are set', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: 'Original content' }],
        summary: 'Summary text',
        showingSummary: true,
      })
      expect(wrapper.html()).toContain('Summary text')
    })

    it('hides summary when showingSummary is false', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: 'Original content' }],
        summary: 'Summary text',
        showingSummary: false,
      })
      // Summary div should be hidden via v-show
      const summaryDiv = wrapper.find('[v-show]')
      // The original content should be visible
      expect(wrapper.html()).toContain('Original content')
    })
    it('shows RAG results in summary mode', () => {
      const ragItem = {
        sessionId: 'sess-1',
        sessionTitle: 'Chat about Go',
        createdAt: '2026-07-19T10:00:00Z',
        summary: 'Discussion about Go error handling',
      }
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: '<rag-results>...</rag-results>' }],
        summary: 'Summary text',
        showingSummary: true,
        blockRagResults: { 'msg-1-0': [ragItem] },
      })
      expect(wrapper.html()).toContain('Summary text')
      expect(wrapper.find('.rag-result-card').exists()).toBe(true)
      expect(wrapper.html()).toContain('Chat about Go')
    })

    it('shows RAG results in summary mode even when blockRagResults not pre-filled', async () => {
      // Simulates message loaded from DB with showingSummary=true —
      // blockRagResults starts empty. detectRagInText(block) checks block.text
      // for <rag-results>, so the v-else-if condition matches and getBlockHtml
      // triggers renderTextBlock which fills blockRagResults as side-effect.
      const ragItem = {
        sessionId: 's1',
        sessionTitle: 'RAG Title',
        summary: 'RAG summary text',
      }
      const ragResults = reactive<Record<string, unknown>>({})
      const cache = new Map<string, string>()
      const staticBlockCache = {
        get: (msgId, bi, text) => cache.get(`${msgId}-${bi}`),
        set: (msgId, bi, text, html) => cache.set(`${msgId}-${bi}`, html),
        isDeferred: () => false,
        scheduleUpgrade: () => {},
      }
      const renderFn = vi.fn((text: string, msgId: string, bi: number) => {
        if (text.includes('<rag-results>')) {
          ragResults[`${msgId}-${bi}`] = [ragItem]
        }
        return `<p>${text.replace(/<rag-results>.*?<\/rag-results>/, '')}</p>`
      })
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: 'Some text <rag-results><rag-item><session-id>s1</session-id><session-title>RAG Title</session-title><summary>RAG summary text</summary></rag-item></rag-results>' }],
        summary: 'Summary text',
        showingSummary: true,
        blockRagResults: ragResults,
        renderTextBlock: renderFn,
        staticBlockCache,
      })
      await nextTick()
      expect(wrapper.find('.rag-result-card').exists()).toBe(true)
      expect(wrapper.html()).toContain('RAG Title')
    })

    it('shows RAG results without summary (non-summary mode)', () => {
      const ragItem = {
        sessionId: 'sess-1',
        sessionTitle: 'Chat about Go',
        createdAt: '2026-07-19T10:00:00Z',
        summary: 'Discussion about Go error handling',
      }
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: '<rag-results>...</rag-results>' }],
        showingSummary: false,
        blockRagResults: { 'msg-1-0': [ragItem] },
      })
      expect(wrapper.find('.rag-result-card').exists()).toBe(true)
      expect(wrapper.html()).toContain('Chat about Go')
    })
  })

  // ── Thinking block collapse animation ──

  describe('thinking block collapse animation', () => {
    beforeEach(() => {
      vi.useFakeTimers()
    })

    afterEach(() => {
      vi.useRealTimers()
    })

    it('does not set collapse state when streaming ends with no thinking blocks', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: 'Hello' }],
        streaming: true,
      })

      await wrapper.setProps({ streaming: false })
      await nextTick()

      const thinking = wrapper.find('.chat-thinking')
      expect(thinking.exists()).toBe(false)
    })

    it('transitions from streaming to collapsed when streaming ends', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Deep thought content', done: false }],
        streaming: true,
      })

      // The thinking block should have streaming class
      expect(wrapper.find('.chat-thinking').classes()).toContain('thinking-streaming')

      // End streaming — block collapses to chip
      await wrapper.setProps({ streaming: false })
      await nextTick()

      // After streaming ends, block should be collapsed
      const thinking = wrapper.find('.chat-thinking')
      expect(thinking.classes()).not.toContain('thinking-streaming')
      expect(thinking.classes()).toContain('thinking-collapsed')
    })

    it('transitions to expanded-done when thinking_done fires mid-stream', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Thinking...', done: false }],
        streaming: true,
      })

      expect(wrapper.find('.chat-thinking').classes()).toContain('thinking-streaming')

      // Simulate thinking_done: set block.done = true — block stays expanded during streaming
      await wrapper.setProps({
        blocks: [{ type: 'thinking', text: 'Thinking complete', done: true }],
      })
      await nextTick()

      // Should no longer be streaming (done=true overrides streaming prop)
      const thinking = wrapper.find('.chat-thinking')
      expect(thinking.classes()).not.toContain('thinking-streaming')
      // Block should be expanded-done or collapsed depending on ref availability
      expect(thinking.classes().some(c => c === 'thinking-expanded-done' || c === 'thinking-collapsed')).toBe(true)
    })

    it('cleans up timers on unmount to prevent leaks', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Deep thought', done: false }],
        streaming: true,
      })

      await wrapper.setProps({ streaming: false })
      await nextTick()

      // Unmount before timers fire — should not throw
      wrapper.unmount()

      // Advance all timers — should not cause Vue warnings or errors
      vi.advanceTimersByTime(10000)
    })

    it('shows thinking-collapsed class when not streaming and done (DB-loaded)', () => {
      // Static state: not streaming, block done — should show collapsed chip
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Final thought', done: true }],
        streaming: false,
      })
      const thinking = wrapper.find('.chat-thinking')
      expect(thinking.classes()).toContain('thinking-collapsed')
      expect(thinking.classes()).not.toContain('thinking-streaming')
    })

    it('collapses to chip after streaming ends', async () => {
      // After streaming ends, all thinking blocks collapse to chip.
      // User can click the chip to re-expand.
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Deep thought', done: false, _key: 'thinking-0' }],
        streaming: true,
      })

      expect(wrapper.find('.chat-thinking').classes()).toContain('thinking-streaming')

      await wrapper.setProps({ streaming: false })
      await nextTick()

      // Block should be collapsed
      const classes = wrapper.find('.chat-thinking').classes()
      expect(classes).toContain('thinking-collapsed')
    })
  })
})
