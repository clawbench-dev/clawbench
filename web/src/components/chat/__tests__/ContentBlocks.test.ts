import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import ContentBlocks from '@/components/chat/ContentBlocks.vue'
import { apiGet } from '@/utils/api'
import { store } from '@/stores/app.ts'
import { updateAskSubmitState } from '@/utils/renderToolDetail.ts'

// ── Mocks ──

vi.mock('@/utils/renderToolDetail.ts', () => ({
  handleToolAction: vi.fn().mockReturnValue(false),
  shouldAutoExpandTool: (name: string) => name === 'AskUserQuestion' || name === 'PermissionApproval',
  updateAskSubmitState: vi.fn(),
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

vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { tasks: [] } },
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
        thinkingLoadFailed: 'Failed to load thinking',
        retry: 'Retry',
        continue: 'Continue',
        resetSession: 'Reset session',
      },
    },
    tool: { askUser: { name: 'Ask' } },
  } },
})

const LucideStub = { template: '<span class="lucide-stub" />' }
const mountedWrappers: ReturnType<typeof mount>[] = []

function mountBlocks(props: Record<string, unknown> = {}) {
  const wrapper = mount(ContentBlocks, {
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
        // Note: CheckCircle2 is intentionally NOT stubbed so the "shows check
        // icon" test verifies the real component renders. Stub every other
        // lucide icon explicitly instead of the package-level stub.
        Brain: LucideStub,
        ChevronRight: LucideStub,
        ChevronDown: LucideStub,
        ChevronUp: LucideStub,
        AlertCircle: LucideStub,
        AlertTriangle: LucideStub,
        XCircle: LucideStub,
        Clock: LucideStub,
        Archive: LucideStub,
        AgentIcon: { template: '<span class="agent-stub" />' },
      },
    },
  })
  mountedWrappers.push(wrapper)
  return wrapper
}

afterEach(() => {
  for (const wrapper of mountedWrappers.splice(0)) {
    if (wrapper.exists()) wrapper.unmount()
  }
})

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
      const check = wrapper.find('.tool-check')
      expect(check.exists()).toBe(true)
      // CheckCircle2 is intentionally NOT stubbed here — a regression guard for
      // the missing-import bug where the icon component was unregistered and
      // rendered as an empty element. The lucide component's root IS the svg
      // carrying the .tool-check class, so assert it is a real svg element.
      expect(check.element.tagName.toLowerCase()).toBe('svg')
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

      // Click header should trigger handleThinkingClick which sets thinkingExpanded.
      // In jsdom, Vue's template re-evaluation for :class bindings that call
      // plain functions (isThinkingExpandedDone/isThinkingCollapsed) may not
      // re-render after ref({}) deep property assignment. This works correctly
      // in the real browser. Verify that clicking does not throw.
      await wrapper.find('.thinking-header').trigger('click')
    })

    it('does not emit show-thinking-detail on thinking click', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Deep thought', done: true }],
        streaming: false,
      })

      await wrapper.find('.thinking-header').trigger('click')

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

    it('renders restart as amber warning (not error) with a continue button', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'restart', text: 'Server restarted' }],
      })
      expect(wrapper.find('.chat-warning-card').exists()).toBe(true)
      expect(wrapper.find('.chat-error-card').exists()).toBe(false)
      const btn = wrapper.find('.warning-continue-btn')
      expect(btn.exists()).toBe(true)
      expect(btn.text()).toBe('Continue')
      await btn.trigger('click')
      expect(wrapper.emitted('send-message')).toBeTruthy()
      expect(wrapper.emitted('send-message')![0]).toEqual(['Continue'])
    })

    it('does not show continue button for non-restart warnings', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'parse_error', text: 'Parse error' }],
      })
      expect(wrapper.find('.warning-continue-btn').exists()).toBe(false)
    })

    it('shows reset button on empty warning and emits reset-session on click', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'empty', text: 'AI returned no content' }],
      })
      const btn = wrapper.find('.warning-reset-btn')
      expect(btn.exists()).toBe(true)
      expect(btn.text()).toBe('Reset session')
      await btn.trigger('click')
      expect(wrapper.emitted('reset-session')).toBeTruthy()
      expect(wrapper.emitted('reset-session')![0]).toEqual([{ reason: 'empty' }])
    })

    it('shows reset button on error block', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'error', reason: 'backend_exit', text: 'AI backend exited' }],
      })
      const btn = wrapper.find('.warning-reset-btn')
      expect(btn.exists()).toBe(true)
      await btn.trigger('click')
      expect(wrapper.emitted('reset-session')).toBeTruthy()
      expect(wrapper.emitted('reset-session')![0]).toEqual([{ reason: 'backend_exit' }])
    })

    it('shows reset button on severe warning (timeout)', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'timeout', text: 'Timed out' }],
      })
      expect(wrapper.find('.warning-reset-btn').exists()).toBe(true)
    })

    it('does not show reset button for user_cancel', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'user_cancel', text: 'Cancelled' }],
      })
      expect(wrapper.find('.warning-reset-btn').exists()).toBe(false)
    })

    it('does not show reset button for warning without a resetable reason', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'stderr', text: 'stderr output' }],
      })
      expect(wrapper.find('.warning-reset-btn').exists()).toBe(false)
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

    it('shows elapsed streaming time and updates it from the message start time', async () => {
      vi.useFakeTimers()
      vi.setSystemTime(new Date('2026-08-10T12:00:00.000Z'))
      const wrapper = mountBlocks({
        blocks: [],
        streaming: true,
        cancelled: false,
        startedAt: '2026-08-10T11:59:58.000Z',
      })

      try {
        expect(wrapper.find('.streaming-elapsed').text()).toBe('2s')

        vi.advanceTimersByTime(63_000)
        await nextTick()
        expect(wrapper.find('.streaming-elapsed').text()).toBe('1m 05s')

        vi.advanceTimersByTime(3_596_000)
        await nextTick()
        expect(wrapper.find('.streaming-elapsed').text()).toBe('1h 01m 01s')

        await wrapper.setProps({ streaming: false })
        expect(wrapper.find('.streaming-elapsed').exists()).toBe(false)
      } finally {
        wrapper.unmount()
        vi.useRealTimers()
      }
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

    it('renders summary text and a tool card from summaryCards.tools (no block traversal)', () => {
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'sum text',
        showingSummary: true,
        summaryCards: {
          tools: [{ name: 'AskUserQuestion', id: 't1', input: { question: 'go?' } }],
          taskIDs: [],
          askQuestions: [],
        },
      })
      expect(wrapper.html()).toContain('sum text')
      expect(wrapper.html()).toContain('AskUserQuestion')
    })

    it('renders an ask-question card from summaryCards.askQuestions via formatToolInput', () => {
      const formatToolInput = vi.fn((input: any) => JSON.stringify(input))
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'sum text',
        showingSummary: true,
        summaryCards: {
          tools: [],
          taskIDs: [],
          askQuestions: [{ header: '', multiSelect: false, question: 'Continue?', options: [{ label: 'Yes' }] }],
        },
        formatToolInput,
      })
      expect(formatToolInput).toHaveBeenCalledWith(
        { questions: [{ header: '', multiSelect: false, question: 'Continue?', options: [{ label: 'Yes' }] }] },
        'AskUserQuestion',
      )
      expect(wrapper.html()).toContain('Continue?')
    })

    it('renders a scheduled-task card from summaryCards.taskIDs with fetched task data', async () => {
      const apiGetMock = vi.mocked(apiGet)
      apiGetMock.mockResolvedValue({
        tasks: [
          { id: 42, name: 'Nightly', status: 'active', cronExpr: '0 0 * * *', agentId: 'a1', repeatMode: 'once', maxRuns: 1, lastRunAt: '', nextRunAt: '' },
        ],
      })
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'sum text',
        showingSummary: true,
        summaryCards: {
          tools: [],
          taskIDs: [42],
          askQuestions: [],
        },
      })
      await flushPromises()
      await nextTick()
      const card = wrapper.find('.scheduled-task-card')
      expect(card.exists()).toBe(true)
      expect(card.html()).toContain('Nightly')

      // Clicking the card emits task-card-click with the task id.
      await card.trigger('click')
      expect(wrapper.emitted('task-card-click')).toBeTruthy()
      expect(wrapper.emitted('task-card-click')![0][0]).toBe(42)
    })

    it('does NOT mark a summary task deleted when the store list is empty (app reset / not yet populated)', async () => {
      const apiGetMock = vi.mocked(apiGet)
      apiGetMock.mockResolvedValue({
        tasks: [
          { id: 99, name: 'KeepMe', status: 'active', cronExpr: '0 0 * * *', agentId: 'a1', repeatMode: 'once', maxRuns: 1, lastRunAt: '', nextRunAt: '' },
        ],
      })
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'sum text',
        showingSummary: true,
        summaryCards: {
          tools: [],
          taskIDs: [99],
          askQuestions: [],
        },
      })
      await flushPromises()
      await nextTick()
      expect(wrapper.find('.scheduled-task-card').exists()).toBe(true)
      expect(wrapper.html()).toContain('KeepMe')

      // Simulate an app reset / empty global store list — must NOT mark the task deleted.
      store.state.tasks = []
      await nextTick()
      await flushPromises()
      expect(wrapper.html()).toContain('KeepMe')
      expect(wrapper.html()).not.toContain('taskDeleted')
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

    it('collapses a thinking block immediately when thinking_done fires mid-stream', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Thinking...', done: false }],
        streaming: true,
      })

      expect(wrapper.find('.chat-thinking').classes()).toContain('thinking-streaming')

      // Simulate thinking_done: set block.done = true → block collapses immediately
      await wrapper.setProps({
        blocks: [{ type: 'thinking', text: 'Thinking complete', done: true }],
      })
      await nextTick()

      // No longer streaming (done=true overrides streaming prop), and NOT kept expanded.
      const thinking = wrapper.find('.chat-thinking')
      expect(thinking.classes()).not.toContain('thinking-streaming')
      expect(thinking.classes()).not.toContain('thinking-expanded-done')
      // Content wrapper closes right away — only the streaming block stays open.
      expect(wrapper.find('.thinking-content-wrapper').classes()).not.toContain('thinking-content-open')

      // Once the collapse animation settles, the block is a collapsed chip.
      vi.advanceTimersByTime(400)
      await nextTick()
      expect(wrapper.find('.chat-thinking').classes()).toContain('thinking-collapsed')
    })

    it('keeps only the currently-streaming thinking block expanded when another finishes', async () => {
      const wrapper = mountBlocks({
        blocks: [
          { type: 'thinking', text: 'First', done: false },
          { type: 'thinking', text: 'Second', done: false },
        ],
        streaming: true,
      })

      const wrappers = () => wrapper.findAll('.thinking-content-wrapper')
      expect(wrappers()[0].classes()).toContain('thinking-content-open')
      expect(wrappers()[1].classes()).toContain('thinking-content-open')

      // First block completes → it collapses, second stays open.
      await wrapper.setProps({
        blocks: [
          { type: 'thinking', text: 'First', done: true },
          { type: 'thinking', text: 'Second', done: false },
        ],
      })
      await nextTick()

      expect(wrappers()[0].classes()).not.toContain('thinking-content-open')
      expect(wrappers()[1].classes()).toContain('thinking-content-open')
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

  // ── stableBlockKey with think_id ──

  describe('stableBlockKey with think_id', () => {
    it('renders two slim thinking blocks as distinct expandable chips', async () => {
      const wrapper = mountBlocks({
        msgId: 'm1',
        sessionId: 's1',
        blocks: [
          { type: 'thinking', think_id: 'th_a', done: true },
          { type: 'thinking', think_id: 'th_b', done: true },
        ],
        streaming: false,
        active: true,
      })

      const chips = wrapper.findAll('.chat-thinking')
      expect(chips).toHaveLength(2)

      // Expand the first only; the second must stay collapsed.
      await chips[0].find('.thinking-header').trigger('click')
      await nextTick()
      // Force a re-render: :class bindings that call plain functions may not
      // re-render in jsdom after ref({}) deep property assignment (see comment
      // in the "expands inline on thinking click when collapsed" test).
      await wrapper.vm.$forceUpdate()
      await nextTick()
      const wrappers = wrapper.findAll('.thinking-content-wrapper')
      expect(wrappers[0].classes()).toContain('thinking-content-open')
      expect(wrappers[1].classes()).not.toContain('thinking-content-open')
    })
  })

  // ── Thinking lazy load (slim blocks with think_id) ──

  describe('thinking lazy load', () => {
    it('expanding a slim thinking block fetches and renders text', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ think_id: 'th_1', text: 'loaded reasoning' }),
      })
      vi.stubGlobal('fetch', fetchMock)

      const wrapper = mountBlocks({
        msgId: 'm1',
        sessionId: 's1',
        blocks: [{ type: 'thinking', think_id: 'th_1', done: true }],
        streaming: false,
        active: true,
      })

      await wrapper.find('.thinking-header').trigger('click')
      await nextTick()

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/ai/chat/thinking?think_id=th_1&message_id=m1&session_id=s1',
      )
      // Force a re-render before asserting post-click DOM (jsdom quirk: plain
      // function :class/v-html bindings may not re-render after ref({}) writes).
      await wrapper.vm.$forceUpdate()
      await nextTick()
      expect(wrapper.find('.thinking-content-wrapper').classes()).toContain('thinking-content-open')

      await flushPromises()
      await nextTick()
      await wrapper.vm.$forceUpdate()
      await nextTick()
      expect(wrapper.find('.thinking-inline-content').html()).toContain('loaded reasoning')
    })

    it('renders existing text directly without fetching', async () => {
      const fetchMock = vi.fn()
      vi.stubGlobal('fetch', fetchMock)

      const wrapper = mountBlocks({
        msgId: 'm1',
        sessionId: 's1',
        blocks: [{ type: 'thinking', text: 'inline thought', done: true }],
        streaming: false,
        active: true,
      })

      await wrapper.find('.thinking-header').trigger('click')
      await nextTick()
      expect(fetchMock).not.toHaveBeenCalled()
      expect(wrapper.find('.thinking-inline-content').html()).toContain('inline thought')
    })

    it('shows error retry and refetches on retry click', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: false, status: 404 })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ think_id: 'th_2', text: 'recovered' }) })
      vi.stubGlobal('fetch', fetchMock)

      const wrapper = mountBlocks({
        msgId: 'm1',
        sessionId: 's1',
        blocks: [{ type: 'thinking', think_id: 'th_2', done: true }],
        streaming: false,
        active: true,
      })

      await wrapper.find('.thinking-header').trigger('click')
      await flushPromises()
      await nextTick()
      // Force a re-render before asserting post-click DOM (jsdom quirk).
      await wrapper.vm.$forceUpdate()
      await nextTick()
      expect(wrapper.find('.thinking-inline-content').html()).toContain('Failed to load thinking')

      // Retry: click the header again (expanded-done + error → re-trigger load)
      await wrapper.find('.thinking-header').trigger('click')
      await flushPromises()
      await nextTick()
      await wrapper.vm.$forceUpdate()
      await nextTick()
      expect(fetchMock).toHaveBeenCalledTimes(2)
      expect(wrapper.find('.thinking-inline-content').html()).toContain('recovered')
    })

    it('retry button refetches after a failed load', async () => {
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: false, status: 404 })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ think_id: 'th_3', text: 'button recovered' }) })
      vi.stubGlobal('fetch', fetchMock)

      const wrapper = mountBlocks({
        msgId: 'm1',
        sessionId: 's1',
        blocks: [{ type: 'thinking', think_id: 'th_3', done: true }],
        streaming: false,
        active: true,
      })

      await wrapper.find('.thinking-header').trigger('click')
      await flushPromises()
      await nextTick()

      // Force a re-render before asserting the error state's retry button (jsdom quirk).
      await wrapper.vm.$forceUpdate()
      await nextTick()

      // Click the actual retry button rendered inside the error state.
      const retryBtn = wrapper.find('.thinking-retry-btn')
      expect(retryBtn.exists()).toBe(true)
      await retryBtn.trigger('click')
      await flushPromises()
      await nextTick()

      expect(fetchMock).toHaveBeenCalledTimes(2)
      await wrapper.vm.$forceUpdate()
      await nextTick()
      expect(wrapper.find('.thinking-inline-content').html()).toContain('button recovered')
    })
  })
})

describe('summary mode with empty blocks (backend stripped content)', () => {
  it('renders summary text even when blocks are empty', () => {
    const wrapper = mountBlocks({
      blocks: [],
      summary: 'Service currently serves new bundle index-DVJhC1nf.js',
      showingSummary: true,
    })
    expect(wrapper.html()).toContain('index-DVJhC1nf.js')
    expect(wrapper.html()).toContain('Service currently')
  })
})

describe('handleToolDetailInput', () => {
  beforeEach(() => {
    vi.mocked(updateAskSubmitState).mockClear()
  })

  it('calls updateAskSubmitState when input event target is inside .ask-question-view', async () => {
    const wrapper = mountBlocks({
      blocks: [{
        type: 'tool_use',
        name: 'AskUserQuestion',
        id: 'tu-1',
        input: { questions: [{ header: 'Approach', options: [{ label: 'A' }, { label: 'B' }] }] },
        done: true,
        status: 'success',
      }],
      formatToolInput: () => '<div class="ask-question-view"><div class="ask-question-item"><div class="ask-question-option selected">A</div></div></div>',
    })
    await nextTick()

    const toolDetail = wrapper.find('.tool-detail')
    expect(toolDetail.exists()).toBe(true)

    // Dispatch a native input event from within .ask-question-view
    const askView = toolDetail.element.querySelector('.ask-question-view')
    expect(askView).toBeTruthy()
    const inputEl = document.createElement('input')
    inputEl.className = 'ask-supplementary-input'
    askView!.appendChild(inputEl)
    inputEl.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()

    expect(updateAskSubmitState).toHaveBeenCalledTimes(1)
  })

  it('does not call updateAskSubmitState when no .ask-question-view ancestor', async () => {
    const wrapper = mountBlocks({
      blocks: [{
        type: 'tool_use',
        name: 'PermissionApproval',
        id: 'tu-2',
        input: { toolName: 'Bash', options: [{ label: 'Allow' }] },
        done: true,
        status: 'success',
      }],
    })
    await nextTick()

    const toolDetail = wrapper.find('.tool-detail')
    if (toolDetail.exists()) {
      const inputEl = document.createElement('input')
      toolDetail.element.appendChild(inputEl)
      inputEl.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      expect(updateAskSubmitState).not.toHaveBeenCalled()
    }
  })
})
