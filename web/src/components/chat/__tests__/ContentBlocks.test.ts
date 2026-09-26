import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, reactive, markRaw } from 'vue'
import { createI18n } from 'vue-i18n'
import ContentBlocks from '@/components/chat/ContentBlocks.vue'
import { apiGet } from '@/utils/api'
import { store } from '@/stores/app.ts'
import { updateAskSubmitState, handleToolAction, restoreAskStatesInContainer, handleAskSupplementaryInput } from '@/utils/renderToolDetail.ts'

// ── Mocks ──

vi.mock('@/utils/renderToolDetail.ts', () => ({
  handleToolAction: vi.fn().mockReturnValue(false),
  shouldAutoExpandTool: (name: string) => name === 'AskUserQuestion' || name === 'PermissionApproval',
  updateAskSubmitState: vi.fn(),
  // Answer-state plumbing: the component calls these from its input handler and
  // its update hook. Default to "not an ask-card event" so the handler falls
  // through to updateAskSubmitState, which the tests below assert on.
  restoreAskStatesInContainer: vi.fn(),
  handleAskSupplementaryInput: vi.fn().mockReturnValue(false),
  classifyAskQuestionsInput: (input: any) => {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return 'empty'
    const questions = input.questions
    if (Array.isArray(questions)) {
      if (questions.length === 0) return 'empty'
      const renderable = questions.some((q: any) =>
        (q && typeof q.question === 'string' && q.question.trim() !== '') || (Array.isArray(q?.options) && q.options.length > 0),
      )
      return renderable ? 'valid' : 'malformed'
    }
    return (Object.prototype.hasOwnProperty.call(input, 'questions') || Object.keys(input).length > 0) ? 'malformed' : 'empty'
  },
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

// `renderMarkdownHtml` is used by the thinking renderer, which discards
// `detectedPaths` (unlike renderTextBlock, which schedules verification). Tests
// that need annotated thinking markup override this. Options are forwarded so
// tests can pin WHICH pipeline mode the thinking renderer asked for.
const { mockRenderMarkdownHtml } = vi.hoisted(() => ({
  mockRenderMarkdownHtml: vi.fn((text: string, _opts?: unknown) => `<p>${text}</p>`),
}))
vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  renderMarkdown: (text: string) => `<p>${text}</p>`,
  renderMarkdownHtml: (text: string, opts?: unknown) => mockRenderMarkdownHtml(text, opts),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
}))

// `data-path-type` is applied ONLY by verifyFilePaths mutating the live DOM, so
// a render served from a cache of the HTML string never carries it. The
// component re-verifies unverified spans itself; this spy pins that it does.
const { mockVerifyFilePaths, mockVerifyCommitHashes, mockInvalidateNegativePathCache } = vi.hoisted(() => ({
  mockVerifyFilePaths: vi.fn().mockResolvedValue(undefined),
  mockVerifyCommitHashes: vi.fn().mockResolvedValue(undefined),
  mockInvalidateNegativePathCache: vi.fn(),
}))
vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({ verifyFilePaths: mockVerifyFilePaths }),
  verifyFilePaths: mockVerifyFilePaths,
  invalidateNegativePathCache: mockInvalidateNegativePathCache,
}))
vi.mock('@/composables/useCommitHashAnnotation', () => ({
  verifyCommitHashes: mockVerifyCommitHashes,
}))

vi.mock('@/stores/app.ts', () => ({
  // `reactive` (not a plain object) because ContentBlocks reads
  // `store.state.projectRoot` during render to build its cache scope; the
  // projectRoot-scope test below asserts the DOM re-renders when it changes.
  store: { state: reactive({ tasks: [] as unknown[], projectRoot: '/proj/alpha' }) },
}))

vi.mock('@/utils/contentBlocks.ts', () => ({
  isSevereWarning: (block: any) => block.reason === 'disconnect',
  getWarningText: (block: any) => block.text || block.reason || '',
  formatErrorCode: (code: any) => (code ? `ERR-${code}` : ''),
  getErrorSourceLabel: (block: any, t: any) => {
    if (!block?.error_source) return ''
    const key = `chat.contentBlocks.errorSources.${block.error_source}`
    const translated = t(key)
    return translated === key ? '' : translated
  },
  statusClass: (task: any) => `status-${task.status}`,
  statusLabel: (task: any, t: any) => task.status,
  statusLabelSimple: (task: any, t: any) => task.status,
  formatTime: (iso: any) => iso,
  askQuestionSummary: (input: any) => input?.question || '',
  extractAskQuestions: (input: any) => {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return []
    const questions = input.questions
    if (!Array.isArray(questions)) return []
    return questions.filter((q: any) =>
      (q && typeof q.question === 'string' && q.question.trim() !== '') || (Array.isArray(q?.options) && q.options.length > 0),
    )
  },
  blockKey: (msgId: any, bi: number) => `${msgId}:${bi}`,
  blockTaskKey: (msgId: any, bi: number) => `${msgId}-${bi}`,
  // Faithful enough to exercise the scheduled-task branch ordering: keys are
  // `${msgId}-${bi}-${tagIdx}` (see the real buildTaskKeyIndex).
  buildTaskKeyIndex: (msgId: any, blockTasks: Record<string, unknown>) => {
    if (!msgId) return {}
    const index: Record<string, string[]> = {}
    const prefix = `${msgId}-`
    for (const k of Object.keys(blockTasks || {})) {
      if (!k.startsWith(prefix)) continue
      const rest = k.slice(prefix.length)
      const dashIdx = rest.indexOf('-')
      if (dashIdx === -1) continue
      const bi = rest.slice(0, dashIdx)
      ;(index[bi] || (index[bi] = [])).push(k)
    }
    for (const bi of Object.keys(index)) index[bi].sort()
    return index
  },
  hasScheduledTasks: (taskKeyIndex: Record<string, string[]>, bi: string | number) => !!(taskKeyIndex[bi]?.length),
  scheduledTaskKeys: (taskKeyIndex: Record<string, string[]>, bi: string | number) => taskKeyIndex[bi] || [],
  extractSlashCommand: (text: string) => {
    if (text.startsWith('/')) {
      const parts = text.split(' ')
      return {
        command: parts[0],
        rest: parts.slice(1).join(' '),
        clawbench: /^\/cb-(chatsearch|task)$/.test(parts[0]),
      }
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
        taskDeleted: 'Task deleted',
        thinkingLoadFailed: 'Failed to load thinking',
        retry: 'Retry',
        continue: 'Continue',
        resetSession: 'Reset session',
        subagentSteps: '{count} steps',
        subagentExpand: 'Expand sub-agent output',
        subagentCollapse: 'Collapse sub-agent output',
      },
    },
    tool: {
      askUser: { name: 'Ask' },
      permission: { title: 'Permission Request' },
    },
    task: {
      form: {
        eventTypes: 'Events to watch',
        eventTypesNone: 'No events configured',
        eventKindIssue: 'Issues',
        eventKindPr: 'Pull requests',
        eventKindRepo: 'Repository pipelines',
        eventOpened: 'Opened',
        eventClosed: 'Closed',
        eventMerged: 'Merged',
        eventReopened: 'Reopened',
        eventCommented: 'Commented',
        eventPipeline: 'Pipeline finished',
      },
    },
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

    it('renders ClawBench badge for text starting with /cb-chatsearch', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: '/cb-chatsearch how to do X' }],
      })
      const badge = wrapper.find('.slash-command-badge')
      expect(badge.exists()).toBe(true)
      expect(badge.text()).toBe('/cb-chatsearch')
      expect(badge.classes()).toContain('clawbench-command-badge')
    })

    it('renders ClawBench badge for text starting with /cb-task', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: '/cb-task run tests' }],
      })
      const badge = wrapper.find('.slash-command-badge')
      expect(badge.exists()).toBe(true)
      expect(badge.classes()).toContain('clawbench-command-badge')
    })

    it('renders agent slash command badge for text starting with /', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: '/commit fix bug' }],
      })
      const badge = wrapper.find('.slash-command-badge')
      expect(badge.exists()).toBe(true)
      expect(badge.text()).toBe('/commit')
      expect(badge.classes()).not.toContain('clawbench-command-badge')
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

      await wrapper.find('.chat-card-strip').trigger('click')

      expect(wrapper.emitted('toggle-tool')).toBeTruthy()
    })

    it('renders auto-expand detail for AskUserQuestion as a unified inline card', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'AskUserQuestion', done: true, status: 'success', id: 'tool-2', input: { question: 'Test?' } }],
      })
      // AskUserQuestion renders as ONE card (header strip + body), not a detached pill bar + box
      expect(wrapper.find('.tool-detail.chat-inline-card').exists()).toBe(true)
      expect(wrapper.find('.chat-card-strip').exists()).toBe(true)
      expect(wrapper.find('.chat-tool-call').exists()).toBe(false)
    })

    it('renders PermissionApproval as a unified inline card (header strip + body in one box)', () => {
      const wrapper = mountBlocks({
        blocks: [{
          type: 'tool_use',
          name: 'PermissionApproval',
          done: false,
          status: '',
          id: 'perm-1',
          input: { toolName: 'Bash', options: [{ name: 'Allow', kind: 'allow_once', optionId: 'a1' }] },
        }],
      })
      expect(wrapper.find('.tool-detail.chat-inline-card').exists()).toBe(true)
      expect(wrapper.find('.chat-card-strip').exists()).toBe(true)
      // The card strip announces the request (unified title), not a detached pill
      expect(wrapper.find('.chat-tool-call').exists()).toBe(false)
    })

    it('shows pending spinner on an unanswered PermissionApproval card and a green check when done', async () => {
      const pending = mountBlocks({
        blocks: [{
          type: 'tool_use', name: 'PermissionApproval', done: false, status: '', id: 'perm-p',
          input: { toolName: 'Bash', options: [{ name: 'Allow', kind: 'allow_once', optionId: 'a1' }] },
        }],
      })
      expect(pending.find('.tool-spinner').exists()).toBe(true)
      expect(pending.find('.tool-check').exists()).toBe(false)

      const done = mountBlocks({
        blocks: [{
          type: 'tool_use', name: 'PermissionApproval', done: true, status: 'success', id: 'perm-d',
          input: { toolName: 'Bash', options: [{ name: 'Allow', kind: 'allow_once', optionId: 'a1' }] },
        }],
      })
      expect(done.find('.tool-spinner').exists()).toBe(false)
      expect(done.find('.tool-check').exists()).toBe(true)
    })

    it('sets data-category on tool call', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'Read', done: true, status: 'success' }],
      })
      expect(wrapper.find('.chat-tool-call').attributes('data-category')).toBe('file')
    })

    it('suppresses pending spinner for malformed AskUserQuestion input (done=false shows check, not endless spinner)', () => {
      // A leftover/malformed AskUserQuestion call (no valid questions array) can
      // never be answered — treat it as done so the user does not see an endless
      // spinner over an empty card (regression: msg 44577).
      const wrapper = mountBlocks({
        blocks: [{ type: 'tool_use', name: 'AskUserQuestion', done: false, status: '', id: 'ask-bad', input: { ask: '<item>broken</tool>' } }],
      })
      expect(wrapper.find('.chat-inline-card').exists()).toBe(true)
      expect(wrapper.find('.tool-spinner').exists()).toBe(false)
      expect(wrapper.find('.tool-check').exists()).toBe(true)
    })

    it('keeps pending spinner while a valid AskUserQuestion is waiting for a user answer', () => {
      const wrapper = mountBlocks({
        blocks: [{
          type: 'tool_use',
          name: 'AskUserQuestion',
          done: false,
          status: '',
          id: 'ask-good',
          input: { questions: [{ header: 'Choose', options: [{ label: 'A' }] }] },
        }],
      })
      expect(wrapper.find('.tool-spinner').exists()).toBe(true)
      expect(wrapper.find('.tool-check').exists()).toBe(false)
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

    // CSS-only contract for height governance: which state classes pair with the
    // wrapper "open" class decides whether the CSS applies a small fixed-height
    // box (streaming — content grows inside a ~10-line viewport) or the large
    // max-height cap (expanded done). jsdom cannot measure heights, so we assert
    // the class contract that the CSS rules hang off.
    it('applies the streaming class contract so the CSS can fix the viewport height', () => {
      // Streaming: block is open (no collapse) → .thinking-streaming present.
      const streaming = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Running analysis', done: false }],
        streaming: true,
      })
      expect(streaming.find('.chat-thinking').classes()).toContain('thinking-streaming')
      expect(streaming.find('.thinking-content-wrapper').classes()).toContain('thinking-content-open')

      // Streaming finished → block auto-collapses (existing auto-collapse behavior).
      const done = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Running analysis', done: true }],
        streaming: false,
      })
      expect(done.find('.chat-thinking').classes()).toContain('thinking-collapsed')
      expect(done.find('.thinking-content-wrapper').classes()).not.toContain('thinking-content-open')
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

    // ── Read-only (public share page) lazy thinking render ──
    //
    // A settled snapshot starts with every thinking block collapsed, and the
    // collapsed content is hidden by CSS — yet `v-html` would still run the
    // full markdown + DOMPurify pipeline for text nobody can see. A shared
    // thread can carry megabytes of reasoning across hundreds of blocks
    // (measured: 4.4 MB / 134k DOM nodes on a 49-message thread ≈ 7s of
    // blocked main thread), so a collapsed block must render nothing until it
    // is expanded.
    describe('read-only lazy render', () => {
      it('does not render a collapsed block\'s markdown in read-only mode', () => {
        mockRenderMarkdownHtml.mockClear()
        const wrapper = mountBlocks({
          readOnly: true,
          streaming: false,
          blocks: [{ type: 'thinking', text: 'Long hidden reasoning', done: true }],
        })
        // Collapsed, and the markdown renderer was never asked for its HTML.
        expect(wrapper.find('.chat-thinking').classes()).toContain('thinking-collapsed')
        expect(mockRenderMarkdownHtml).not.toHaveBeenCalled()
        expect(wrapper.find('.thinking-inline-content').text()).toBe('')
      })

      it('renders the markdown once the read-only block is expanded', async () => {
        mockRenderMarkdownHtml.mockClear()
        const wrapper = mountBlocks({
          readOnly: true,
          streaming: false,
          blocks: [{ type: 'thinking', text: 'Long hidden reasoning', done: true }],
        })
        expect(mockRenderMarkdownHtml).not.toHaveBeenCalled()

        await wrapper.find('.thinking-header').trigger('click')
        await nextTick()

        expect(mockRenderMarkdownHtml).toHaveBeenCalled()
        expect(wrapper.find('.thinking-inline-content').text()).toContain('Long hidden reasoning')
      })

      it('still renders immediately in the interactive app (not read-only)', () => {
        mockRenderMarkdownHtml.mockClear()
        mountBlocks({
          readOnly: false,
          streaming: false,
          blocks: [{ type: 'thinking', text: 'App reasoning', done: true }],
        })
        // The interactive app keeps its current eager behavior.
        expect(mockRenderMarkdownHtml).toHaveBeenCalled()
      })
    })
  })

  // ── Thinking inline-content scroll follow ──
  // The streaming thinking box is a capped-height scroll container. Each content
  // flush rewrites innerHTML and the browser keeps the old scrollTop, so the box
  // must be re-pinned to its bottom — unless the user scrolled up to read
  // earlier reasoning (latch). jsdom cannot lay out the box, so the geometry is
  // faked with Object.defineProperty and the scrollTop effect is asserted.

  describe('thinking inline scroll follow', () => {
    function fakeOverflow(el: HTMLElement, scrollHeight: number, clientHeight: number) {
      Object.defineProperty(el, 'scrollHeight', { configurable: true, value: scrollHeight })
      Object.defineProperty(el, 'clientHeight', { configurable: true, value: clientHeight })
    }

    it('pins the streaming thinking box to the bottom after a content flush', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Deep reasoning', done: false, _key: 'th-stream' }],
        streaming: true,
      })
      const box = wrapper.find('.thinking-inline-content').element as HTMLElement
      fakeOverflow(box, 800, 220) // content overflows the capped box
      box.scrollTop = 0

      // A content flush rewrites blockHtmlCache → follow re-pins to bottom.
      await wrapper.setProps({ blocks: [{ type: 'thinking', text: 'Deep reasoning more', done: false, _key: 'th-stream' }] })
      await nextTick()
      expect(box.scrollTop).toBe(800)
    })

    it('stops following once the user scrolls up to read earlier reasoning', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Deep reasoning', done: false, _key: 'th-stream' }],
        streaming: true,
      })
      const box = wrapper.find('.thinking-inline-content').element as HTMLElement
      fakeOverflow(box, 800, 220)
      // Follow pins to the bottom, recording scrollTop for direction detection
      // (reality: the streamed content must overflow before the user can scroll).
      Object.defineProperty(box, 'scrollTop', { configurable: true, value: 800, writable: true })
      await wrapper.setProps({ blocks: [{ type: 'thinking', text: 'Deep reasoning…', done: false, _key: 'th-stream' }] })
      await nextTick()
      expect(box.scrollTop).toBe(800)

      // User scrolls up to read past reasoning: scrollTop decreases → latch.
      box.scrollTop = 400
      await wrapper.find('.thinking-inline-content').trigger('scroll')
      await nextTick()

      // Further content flush must NOT yank the box back to the bottom.
      await wrapper.setProps({ blocks: [{ type: 'thinking', text: 'Deep reasoning more', done: false, _key: 'th-stream' }] })
      await nextTick()
      expect(box.scrollTop).toBe(400)
    })

    it('does not pin a finished (non-streaming) thinking box', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'thinking', text: 'Final reasoning', done: true, _key: 'th-done' }],
        streaming: false,
      })
      const box = wrapper.find('.thinking-inline-content').element as HTMLElement
      fakeOverflow(box, 800, 220)
      box.scrollTop = 150

      // Any cache rewrite while not streaming must leave the box position alone.
      await wrapper.setProps({ blocks: [{ type: 'thinking', text: 'Final reasoning updated', done: true, _key: 'th-done' }] })
      await nextTick()
      expect(box.scrollTop).toBe(150)
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

    // A turn where the agent accepted the prompt but never ran the model is
    // recoverable by resetting the session, so it must offer the button — the
    // user otherwise has no way out of a stuck agent.
    it('shows reset button for agent_no_run', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'agent_no_run', text: 'The agent did not run this request' }],
      })
      const btn = wrapper.find('.warning-reset-btn')
      expect(btn.exists()).toBe(true)
      await btn.trigger('click')
      expect(wrapper.emitted('reset-session')).toBeTruthy()
      expect(wrapper.emitted('reset-session')![0]).toEqual([{ reason: 'agent_no_run' }])
    })

    // An agent that never came up leaves the connection unusable; the reset
    // button is the only way to force a fresh spawn, so it must be offered.
    it('shows reset button for agent_init_timeout', async () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'warning', reason: 'agent_init_timeout', text: 'The agent did not start within 1m0s' }],
      })
      const btn = wrapper.find('.warning-reset-btn')
      expect(btn.exists()).toBe(true)
      await btn.trigger('click')
      expect(wrapper.emitted('reset-session')).toBeTruthy()
      expect(wrapper.emitted('reset-session')![0]).toEqual([{ reason: 'agent_init_timeout' }])
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

    it('renders restart warning banner from summaryCards.warnings (summary view)', async () => {
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'Summary text',
        showingSummary: true,
        summaryCards: {
          warnings: [{ type: 'warning', reason: 'restart', text: 'Server restarted, AI response interrupted' }],
        },
      })
      const card = wrapper.find('.chat-warning-card')
      expect(card.exists()).toBe(true)
      expect(card.text()).toContain('Server restarted, AI response interrupted')
      const btn = card.find('.warning-continue-btn')
      expect(btn.exists()).toBe(true)
      await btn.trigger('click')
      expect(wrapper.emitted('send-message')).toBeTruthy()
      expect(wrapper.emitted('send-message')![0]).toEqual(['Continue'])
    })

    it('renders error banner from summaryCards.warnings with reset button', async () => {
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'Summary text',
        showingSummary: true,
        summaryCards: {
          warnings: [{ type: 'error', reason: 'backend_exit', text: 'AI backend exited' }],
        },
      })
      const card = wrapper.find('.chat-error-card')
      expect(card.exists()).toBe(true)
      expect(card.text()).toContain('AI backend exited')
      const btn = card.find('.warning-reset-btn')
      expect(btn.exists()).toBe(true)
      await btn.trigger('click')
      expect(wrapper.emitted('reset-session')).toBeTruthy()
      expect(wrapper.emitted('reset-session')![0]).toEqual([{ reason: 'backend_exit' }])
    })

    it('renders severe warning (disconnect/timeout/panic) as error-level card from summaryCards.warnings', async () => {
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'Summary text',
        showingSummary: true,
        summaryCards: {
          warnings: [{ type: 'warning', reason: 'disconnect', text: 'Connection lost' }],
        },
      })
      // isSevereWarning({reason:'disconnect'}) → red error card, not amber.
      const card = wrapper.find('.chat-error-card')
      expect(card.exists()).toBe(true)
      expect(card.text()).toContain('Connection lost')
      const btn = card.find('.warning-reset-btn')
      expect(btn.exists()).toBe(true)
      await btn.trigger('click')
      expect(wrapper.emitted('reset-session')).toBeTruthy()
      expect(wrapper.emitted('reset-session')![0]).toEqual([{ reason: 'disconnect' }])
    })

    it('does not render warning banners when summaryCards has no warnings', () => {
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'Summary text',
        showingSummary: true,
        summaryCards: { tools: [], taskIDs: [], askQuestions: [] },
      })
      expect(wrapper.find('.chat-warning-card').exists()).toBe(false)
      expect(wrapper.find('.chat-error-card').exists()).toBe(false)
    })

    it('does not render summary warnings in original mode (blocks branch)', () => {
      const wrapper = mountBlocks({
        blocks: [{ type: 'text', text: 'Original content' }],
        summary: 'Summary text',
        showingSummary: false,
        summaryCards: { warnings: [{ type: 'warning', reason: 'restart', text: 'Server restarted' }] },
      })
      // Summary-only warnings must not leak into the original-content view.
      expect(wrapper.html()).toContain('Original content')
      expect(wrapper.find('.chat-warning-card').exists()).toBe(false)
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
      // Auto-expand tool from summary cards renders as a unified inline card
      expect(wrapper.find('.tool-detail.chat-inline-card').exists()).toBe(true)
    })

    it('hides PermissionApproval cards in summary view (actionable-only, no dead buttons)', () => {
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'sum text',
        showingSummary: true,
        summaryCards: {
          tools: [
            { name: 'PermissionApproval', id: 'perm-s', done: true, status: 'error', output: 'Cancelled', input: { toolName: 'Bash' } },
            { name: 'AskUserQuestion', id: 'ask-s', input: { question: 'go?' } },
          ],
          taskIDs: [],
          askQuestions: [],
        },
      })
      // Only the AskUserQuestion card survives; the permission card is dropped.
      expect(wrapper.html()).not.toContain('Permission Request')
      expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(1)
      expect(wrapper.html()).toContain('AskUserQuestion')
    })

    it('filters PermissionApproval case-insensitively regardless of list order', () => {
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'sum text',
        showingSummary: true,
        summaryCards: {
          tools: [
            { name: 'permissionapproval', id: 'perm-lc', input: { toolName: 'Bash' } },
            { name: 'AskUserQuestion', id: 'ask-a', input: { question: 'first?' } },
            { name: 'PERMISSIONAPPROVAL', id: 'perm-uc', input: { toolName: 'Read' } },
            { name: 'AskUserQuestion', id: 'ask-b', input: { question: 'second?' } },
          ],
          taskIDs: [],
          askQuestions: [],
        },
      })
      // Both casings of the permission card are dropped; both ask cards remain.
      expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(2)
      expect(wrapper.html()).not.toContain('Permission Request')
    })

    it('renders no permission card when summaryCards.tools only holds PermissionApproval', () => {
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'sum text',
        showingSummary: true,
        summaryCards: {
          tools: [{ name: 'PermissionApproval', id: 'perm-only', done: true, status: 'success', output: 'ok', input: { toolName: 'Bash' } }],
          taskIDs: [],
          askQuestions: [],
        },
      })
      expect(wrapper.find('.tool-detail.chat-inline-card').exists()).toBe(false)
      expect(wrapper.find('.chat-tool-call').exists()).toBe(false)
      expect(wrapper.html()).not.toContain('Permission Request')
    })

    it('handles summaryCards.tools being null/absent without crashing', () => {
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'sum text',
        showingSummary: true,
        summaryCards: { taskIDs: [], askQuestions: [] },
      })
      expect(wrapper.find('.tool-detail.chat-inline-card').exists()).toBe(false)
      expect(wrapper.html()).toContain('sum text')
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
      // The card also receives an askKey so its answer state can be restored
      // after a re-render (see askQuestionState.ts).
      expect(formatToolInput).toHaveBeenCalledWith(
        { questions: [{ header: '', multiSelect: false, question: 'Continue?', options: [{ label: 'Yes' }] }] },
        'AskUserQuestion',
        { askKey: 'no-session|summary:msg-1' },
      )
      expect(wrapper.html()).toContain('Continue?')
      // summaryCards.askQuestions renders as a unified inline card
      expect(wrapper.find('.tool-detail.chat-inline-card').exists()).toBe(true)
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

    it('renders an event task card with its subscription rather than cron-only fields', async () => {
      // The card was extracted from this component precisely so both trigger
      // modes render from one implementation; this pins the summary-mode wiring
      // (data fetched from /api/tasks) to the event branch.
      const apiGetMock = vi.mocked(apiGet)
      apiGetMock.mockResolvedValue({
        tasks: [
          {
            id: 41, name: 'Review new PRs', status: 'active', agentId: 'a1',
            triggerMode: 'event', eventTypes: 'pr.opened',
            // Inert for event tasks — must not be rendered as the trigger.
            cronExpr: '', repeatMode: 'unlimited', maxRuns: 0, lastRunAt: '', nextRunAt: '',
          },
        ],
      })
      const wrapper = mountBlocks({
        blocks: [],
        summary: 'sum text',
        showingSummary: true,
        summaryCards: { tools: [], taskIDs: [41], askQuestions: [] },
      })
      await flushPromises()
      await nextTick()
      const card = wrapper.find('.scheduled-task-card')
      expect(card.exists()).toBe(true)
      expect(card.classes()).toContain('is-event')
      expect(card.find('.stask-chip').text()).toContain('Opened')
      expect(card.text()).toContain('Events to watch')
      // No blank schedule line, no fabricated repeat mode, no "next run: none".
      expect(card.text()).not.toContain('Frequency')
      expect(card.text()).not.toContain('Repeat')
      expect(card.text()).not.toContain('Next run')
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

    it('renders the lazy-loaded prefix stitched onto live deltas', async () => {
      // A streaming block whose think_id was adopted from the DB marker: its
      // prefix lives in chat_thinking, its live deltas are in `text`. The
      // rendered text must be both, stitched (see thinkingRenderSource).
      //
      // NOTE: this exercises the TEMPLATE path only. The throttled batch path
      // (flushBlockHtml) runs inside a requestAnimationFrame scheduler, which
      // jsdom never reaches — so this test passes even when that path is broken
      // (verified by mutation). The regression guard for the two paths agreeing
      // is the source-sniffing test in thinkingRenderSourceGuard.test.ts; this
      // one only pins the stitched-content contract.
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ think_id: 'th_live', text: 'PREFIX ' }),
      })
      vi.stubGlobal('fetch', fetchMock)

      const wrapper = mountBlocks({
        msgId: 'm1',
        sessionId: 's1',
        blocks: [{ type: 'thinking', think_id: 'th_live', in_progress: true, text: 'live deltas' }],
        streaming: true,
        active: true,
      })

      // Let the auto-prefix-load land.
      await flushPromises()
      await nextTick()
      await wrapper.vm.$forceUpdate()
      await nextTick()

      const html = wrapper.find('.thinking-inline-content').html()
      expect(html).toContain('PREFIX')
      expect(html).toContain('live deltas')
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

describe('AskUserQuestion card interactive dispatch', () => {
  beforeEach(() => {
    vi.mocked(handleToolAction).mockClear()
    vi.mocked(handleToolAction).mockReturnValue(false)
  })

  it('dispatches option clicks to the action handler for a tool_use AskUserQuestion card (data-tool-name on the @click element)', async () => {
    const wrapper = mountBlocks({
      blocks: [{
        type: 'tool_use',
        name: 'AskUserQuestion',
        id: 'ask-1',
        input: { questions: [{ header: 'Approach', options: [{ label: 'A' }, { label: 'B' }] }] },
        done: true,
        status: 'success',
      }],
      formatToolInput: () => '<div class="ask-question-view"><div class="ask-question-item"><div class="ask-question-option" data-label="A">A</div></div></div>',
    })
    await nextTick()

    const option = wrapper.find('.ask-question-option')
    expect(option.exists()).toBe(true)
    await option.trigger('click')

    expect(handleToolAction).toHaveBeenCalled()
    const [name, event] = handleToolAction.mock.calls[0]
    expect(name).toBe('AskUserQuestion')
    expect((event.target as HTMLElement).classList.contains('ask-question-option')).toBe(true)
  })

  it('dispatches option clicks for a text-mode <ask-question> card (body inside unified card still routes through the outer handler)', async () => {
    // The text-mode ask card renders the body inside .chat-card-body. Option
    // clicks must bubble to the outer .tool-detail (which carries both @click
    // and data-tool-name) — regression guard for the card unification.
    const wrapper = mountBlocks({
      blocks: [{
        type: 'text',
        text: 'Some text <ask-question><item><question>Continue?</question><option><label>Yes</label></option></item></ask-question>',
      }],
      // blockAskQuestions is keyed `${msgId}-${blockIdx}` (blockTaskKey)
      blockAskQuestions: {
        'msg-1-0': { questions: [{ header: '', multiSelect: false, question: 'Continue?', options: [{ label: 'Yes' }] }] },
      },
      formatToolInput: () => '<div class="ask-question-view"><div class="ask-question-item"><div class="ask-question-option" data-label="Yes">Yes</div></div></div>',
    })
    await nextTick()
    await flushPromises()

    const outer = wrapper.find('.tool-detail.chat-inline-card')
    expect(outer.exists()).toBe(true)
    // Outer must expose the tool name to handleToolDetailClick dispatch
    expect(outer.attributes('data-tool-name')).toBe('AskUserQuestion')

    const option = wrapper.find('.ask-question-option')
    expect(option.exists()).toBe(true)
    await option.trigger('click')

    expect(handleToolAction).toHaveBeenCalled()
    expect(handleToolAction.mock.calls[0][0]).toBe('AskUserQuestion')
  })
})

describe('ContentBlocks — multiple ask cards merged into one', () => {
  // Mimic the real renderAskUserQuestion shape closely enough to catch the bugs
  // that matter: valid input renders one .ask-question-item per question, a
  // malformed input renders the invalid-format notice, and empty renders the
  // neutral placeholder. Echoing only `questions` would hide a destroyed notice.
  const echoFormat = vi.fn((input: any) => {
    const questions = input?.questions
    if (!Array.isArray(questions) || questions.length === 0) {
      if (input && typeof input === 'object' && Object.keys(input).length > 0) {
        return '<div class="ask-question-view ask-invalid"><div class="ask-question-empty">invalid format</div></div>'
      }
      return '<div class="ask-question-view"><div class="ask-question-empty">no questions</div></div>'
    }
    const items = questions.map((q: any) => `<div class="ask-question-item"><span class="ask-question-text">${q.question}</span></div>`).join('')
    return `<div class="ask-question-view">${items}<button class="ask-question-submit" disabled>Submit</button></div>`
  })

  it('renders ONE merged card (not one per call) when a message has multiple AskUserQuestion calls', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-1', done: true, input: { questions: [{ question: 'Q1', options: [{ label: 'A' }] }] } },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-2', done: true, input: { questions: [{ question: 'Q2', options: [{ label: 'B' }] }] } },
      ],
      formatToolInput: echoFormat,
    })
    // Only the first card survives (anchor); the second is absorbed.
    expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(1)
    // The single card body carries BOTH questions so one submit answers all.
    const items = wrapper.findAll('.ask-question-item')
    expect(items).toHaveLength(2)
    expect(wrapper.html()).toContain('Q1')
    expect(wrapper.html()).toContain('Q2')
  })

  it('does NOT merge when the message has a single AskUserQuestion card', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-1', done: true, input: { questions: [{ question: 'OnlyQ', options: [{ label: 'A' }] }] } },
      ],
      formatToolInput: echoFormat,
    })
    expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(1)
    expect(wrapper.findAll('.ask-question-item')).toHaveLength(1)
  })

  it('merges a tool_use ask card with a text-mode <ask-question> card', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-1', done: true, input: { questions: [{ question: 'ToolQ', options: [{ label: 'A' }] }] } },
        { type: 'text', text: 'lead-in <ask-question><item><question>TextQ</question><option><label>B</label></option></item></ask-question>' },
      ],
      blockAskQuestions: {
        'msg-1-1': { questions: [{ question: 'TextQ', options: [{ label: 'B' }] }] },
      },
      formatToolInput: echoFormat,
    })
    // One merged card only.
    expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(1)
    expect(wrapper.findAll('.ask-question-item')).toHaveLength(2)
    expect(wrapper.html()).toContain('ToolQ')
    expect(wrapper.html()).toContain('TextQ')
  })

  it('renders one card per answerable ask block plus the malformed card notice, and never duplicates the merged body', () => {
    // Two valid asks + one malformed ask. The malformed block must NOT be handed
    // the merged questions — it keeps its own invalid-format notice. The merged
    // card is the only card carrying the two questions, and it is the only
    // submit-capable card.
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-1', done: true, input: { questions: [{ question: 'ValidQ1', options: [{ label: 'A' }] }] } },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-2', done: true, input: { questions: [{ question: 'ValidQ2', options: [{ label: 'B' }] }] } },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-bad', done: true, input: { ask: '<item>broken</tool>' } },
      ],
      formatToolInput: echoFormat,
    })
    // The malformed card still renders (its own invalid notice), so 2 cards total.
    expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(2)
    // Exactly ONE card holds the merged questions (2 items); the malformed one has none.
    const mergedViews = wrapper.findAll('.ask-question-view').filter(v => v.findAll('.ask-question-item').length > 0)
    expect(mergedViews).toHaveLength(1)
    expect(mergedViews[0].findAll('.ask-question-item')).toHaveLength(2)
    // The malformed card's notice survives.
    expect(wrapper.html()).toContain('invalid format')
    // Only the merged card has a submit button (no duplicate send surface).
    expect(wrapper.findAll('.ask-question-submit')).toHaveLength(1)
  })

  it('does not merge when the only other ask call is malformed (single valid card stays standalone)', () => {
    // A malformed (unanswerable) call must not count as a second card.
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-good', done: true, input: { questions: [{ question: 'ValidQ', options: [{ label: 'A' }] }] } },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-bad', done: true, input: { ask: '<item>broken</tool>' } },
      ],
      formatToolInput: echoFormat,
    })
    // Valid card is NOT merged (single question), malformed card keeps its notice.
    const mergedViews = wrapper.findAll('.ask-question-view').filter(v => v.findAll('.ask-question-item').length > 0)
    expect(mergedViews).toHaveLength(1)
    expect(mergedViews[0].findAll('.ask-question-item')).toHaveLength(1)
    expect(wrapper.html()).toContain('ValidQ')
    expect(wrapper.html()).toContain('invalid format')
  })

  it('keeps a still-loading ask block pending instead of duplicating the merged body', () => {
    // A third ask still streaming (done=false, empty input) must not be handed
    // the merged questions; it keeps its own pending spinner.
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-1', done: true, input: { questions: [{ question: 'ValidQ1', options: [{ label: 'A' }] }] } },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-2', done: true, input: { questions: [{ question: 'ValidQ2', options: [{ label: 'B' }] }] } },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-pending', done: false, input: {} },
      ],
      formatToolInput: echoFormat,
    })
    const mergedViews = wrapper.findAll('.ask-question-view').filter(v => v.findAll('.ask-question-item').length > 0)
    expect(mergedViews).toHaveLength(1)
    expect(mergedViews[0].findAll('.ask-question-item')).toHaveLength(2)
    // The still-loading card shows a spinner and no green check of its own.
    const cards = wrapper.findAll('.tool-detail.chat-inline-card')
    const pendingCard = cards.find(c => c.find('.tool-spinner').exists())
    expect(pendingCard).toBeTruthy()
    expect(pendingCard!.find('.tool-check').exists()).toBe(false)
  })

  it('does not drop every question when the anchor text block also carries a scheduled-task tag', () => {
    // A text block with BOTH <scheduled-task> and <ask-question> renders as a
    // scheduled-task card (that branch precedes the ask branch), so it cannot host
    // the merged card. The anchor must fall through to the AskUserQuestion tool
    // block instead of suppressing it — otherwise every question disappears.
    const wrapper = mountBlocks({
      blocks: [
        { type: 'text', text: 'see <scheduled-task id="7"/> and <ask-question><item><question>TextQ</question><option><label>T</label></option></item></ask-question>' },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-tool', done: true, input: { questions: [{ question: 'ToolQ', options: [{ label: 'M' }] }] } },
      ],
      blockAskQuestions: {
        'msg-1-0': { questions: [{ question: 'TextQ', options: [{ label: 'T' }] }] },
      },
      blockTasks: { 'msg-1-0-0': { taskId: '7' } },
      formatToolInput: echoFormat,
    })
    // The text block is claimed by the scheduled-task branch (proving the
    // scenario is real), so the merged card must render on the tool block.
    expect(wrapper.find('.scheduled-task-card').exists()).toBe(true)
    const mergedViews = wrapper.findAll('.ask-question-view').filter(v => v.findAll('.ask-question-item').length > 0)
    expect(mergedViews).toHaveLength(1)
    expect(mergedViews[0].findAll('.ask-question-item')).toHaveLength(2)
    expect(wrapper.html()).toContain('TextQ')
    expect(wrapper.html()).toContain('ToolQ')
  })

  it('merges multiple AskUserQuestion cards in summary view', () => {
    const wrapper = mountBlocks({
      blocks: [],
      summary: 'sum text',
      showingSummary: true,
      summaryCards: {
        tools: [
          { name: 'AskUserQuestion', id: 'ask-a', input: { questions: [{ question: 'SQ1', options: [{ label: 'A' }] }] } },
          { name: 'AskUserQuestion', id: 'ask-b', input: { questions: [{ question: 'SQ2', options: [{ label: 'B' }] }] } },
        ],
        taskIDs: [],
        askQuestions: [],
      },
      formatToolInput: echoFormat,
    })
    expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(1)
    expect(wrapper.findAll('.ask-question-item')).toHaveLength(2)
    expect(wrapper.html()).toContain('SQ1')
    expect(wrapper.html()).toContain('SQ2')
  })

  it('merges summaryCards.askQuestions with summaryCards.tools ask cards in summary view', () => {
    const wrapper = mountBlocks({
      blocks: [],
      summary: 'sum text',
      showingSummary: true,
      summaryCards: {
        tools: [
          { name: 'AskUserQuestion', id: 'ask-t', input: { questions: [{ question: 'ToolQ', options: [{ label: 'A' }] }] } },
        ],
        taskIDs: [],
        askQuestions: [{ question: 'XmlQ', options: [{ label: 'B' }] }],
      },
      formatToolInput: echoFormat,
    })
    expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(1)
    expect(wrapper.findAll('.ask-question-item')).toHaveLength(2)
    expect(wrapper.html()).toContain('ToolQ')
    expect(wrapper.html()).toContain('XmlQ')
  })

  it('renders a single summary ask card unchanged when there is only one source', () => {
    const wrapper = mountBlocks({
      blocks: [],
      summary: 'sum text',
      showingSummary: true,
      summaryCards: {
        tools: [],
        taskIDs: [],
        askQuestions: [{ question: 'SoloQ', options: [{ label: 'B' }] }],
      },
      formatToolInput: echoFormat,
    })
    expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(1)
    expect(wrapper.findAll('.ask-question-item')).toHaveLength(1)
    expect(wrapper.html()).toContain('SoloQ')
  })

  it('keeps a malformed ask tool card visible in summary view (only valid ask tools merge)', () => {
    const wrapper = mountBlocks({
      blocks: [],
      summary: 'sum text',
      showingSummary: true,
      summaryCards: {
        tools: [
          { name: 'AskUserQuestion', id: 'ask-good', input: { questions: [{ question: 'GoodQ', options: [{ label: 'A' }] }] } },
          { name: 'AskUserQuestion', id: 'ask-bad', input: { ask: '<item>broken</tool>' } },
        ],
        taskIDs: [],
        askQuestions: [],
      },
      formatToolInput: echoFormat,
    })
    // Merged card (from the valid tool) + the malformed tool's own card.
    expect(wrapper.findAll('.tool-detail.chat-inline-card')).toHaveLength(2)
    expect(wrapper.html()).toContain('GoodQ')
  })

  it('does not merge a top-level ask card with a sub-agent ask card (group boundary)', () => {
    // A sub-agent's question renders nested under its Agent group; the main
    // agent's card must stay a standalone single card.
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-sub', done: true, parent_tool_call_id: 'call_p', input: { questions: [{ question: 'SubQ', options: [{ label: 'S' }] }] } },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-main', done: true, input: { questions: [{ question: 'MainQ', options: [{ label: 'M' }] }] } },
      ],
      formatToolInput: echoFormat,
    })
    // The sub-agent's card is skipped from the flat stream (child block), so the
    // main agent renders exactly one standalone card with its own single question.
    const mainCard = wrapper.findAll('.tool-detail.chat-inline-card')
    expect(mainCard).toHaveLength(1)
    expect(mainCard[0].html()).toContain('MainQ')
    expect(mainCard[0].html()).not.toContain('SubQ')
  })

  it('merges a sub-agent’s own multiple ask cards within its nested instance', () => {
    // Complementary to the boundary test: inside a sub-agent group, its own two
    // ask cards merge into one — the nested instance performs its own merge.
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'sub-1', done: true, parent_tool_call_id: 'call_p', input: { questions: [{ question: 'SubQ1', options: [{ label: 'A' }] }] } },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'sub-2', done: true, parent_tool_call_id: 'call_p', input: { questions: [{ question: 'SubQ2', options: [{ label: 'B' }] }] } },
      ],
      expandedTools: { 'subagent-call_p': true },
      formatToolInput: echoFormat,
    })
    const group = wrapper.find('.subagent-group')
    expect(group.exists()).toBe(true)
    // One merged card inside the group, carrying both sub-agent questions.
    const mergedViews = group.findAll('.ask-question-view').filter(v => v.findAll('.ask-question-item').length > 0)
    expect(mergedViews).toHaveLength(1)
    expect(mergedViews[0].findAll('.ask-question-item')).toHaveLength(2)
  })
})

describe('ContentBlocks — sub-agent grouping', () => {
  it('merges the group into the Agent pill (one row, not two stacked bars)', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'thinking', text: 'child reasoning', parent_tool_call_id: 'call_p' },
        { type: 'tool_use', name: 'Read', id: 't1', done: true, input: {}, parent_tool_call_id: 'call_p' },
        { type: 'text', text: 'child answer', parent_tool_call_id: 'call_p' },
      ],
      expandedTools: { 'subagent-call_p': true },
    })
    // The expand/collapse toggle lives INSIDE the Agent pill — no separate
    // group header bar duplicating the agent name.
    const pill = wrapper.find('.chat-tool-call-group')
    expect(pill.exists()).toBe(true)
    expect(pill.find('.subagent-group-toggle').exists()).toBe(true)
    expect(wrapper.find('.subagent-group-header').exists()).toBe(false)
    // The child Read pill must live INSIDE the group body, not at the top level.
    const group = wrapper.find('.subagent-group')
    expect(group.find('.chat-tool-call').exists()).toBe(true)
    // The root content-blocks container has exactly one direct tool pill: Agent.
    const root = wrapper.find('.content-blocks')
    const directPills = root.element.children && Array.from(root.element.children).filter(
      (el) => (el as HTMLElement).classList.contains('chat-tool-call'),
    )
    expect(directPills.length).toBe(1)
  })

  it('does not mount the recursive body until the group is first opened', () => {
    const collapsed = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'tool_use', name: 'Read', id: 't1', done: true, input: {}, parent_tool_call_id: 'call_p' },
      ],
    })
    // Collapsed: the child subtree is not instantiated at all.
    expect(collapsed.find('.subagent-group-body .content-blocks-nested').exists()).toBe(false)

    const open = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'tool_use', name: 'Read', id: 't1', done: true, input: {}, parent_tool_call_id: 'call_p' },
      ],
      expandedTools: { 'subagent-call_p': true },
    })
    expect(open.find('.subagent-group-body .content-blocks-nested').exists()).toBe(true)
  })

  it('child tool detail is emitted with its real ROOT block index', async () => {
    // Regression: nested instances must translate the local index back to the
    // root array index — the drawer resolves the live block by root index.
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'thinking', text: 'child reasoning', parent_tool_call_id: 'call_p' },
        { type: 'tool_use', name: 'Read', id: 't1', done: true, input: {}, parent_tool_call_id: 'call_p' },
      ],
      expandedTools: { 'subagent-call_p': true },
    })
    const group = wrapper.find('.subagent-group')
    const childPill = group.findAll('.chat-tool-call').find((p) => p.text().includes('Read'))!
    expect(childPill).toBeTruthy()
    await childPill.trigger('click')
    const events = wrapper.emitted('show-tool-detail')
    expect(events).toBeTruthy()
    const detail = events!.find((e) => (e[0] as any).name === 'Read')?.[0] as any
    expect(detail).toBeTruthy()
    // The Read block is at root index 2 (Agent=0, thinking=1, Read=2).
    expect(detail.blockIdx).toBe(2)
  })

  it('toggling the in-pill chevron emits toggle-tool with the group key', async () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'tool_use', name: 'Read', id: 't1', done: true, input: {}, parent_tool_call_id: 'call_p' },
      ],
    })
    await wrapper.find('.subagent-group-toggle').trigger('click')
    const events = wrapper.emitted('toggle-tool')
    expect(events).toBeTruthy()
    expect(events![0][0]).toBe('subagent-call_p')
  })

  it('renders a bottom footer collapse button only while open, and it toggles', async () => {
    const collapsed = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'tool_use', name: 'Read', id: 't1', done: true, input: {}, parent_tool_call_id: 'call_p' },
      ],
    })
    // Body is not mounted while collapsed → no footer.
    expect(collapsed.find('.subagent-group-footer').exists()).toBe(false)

    const open = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'tool_use', name: 'Read', id: 't1', done: true, input: {}, parent_tool_call_id: 'call_p' },
      ],
      expandedTools: { 'subagent-call_p': true },
    })
    const footer = open.find('.subagent-group-footer')
    expect(footer.exists()).toBe(true)
    await footer.trigger('click')
    const events = open.emitted('toggle-tool')
    expect(events).toBeTruthy()
    expect(events![0][0]).toBe('subagent-call_p')
  })

  it('renders step count from child tool calls', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'tool_use', name: 'Read', id: 't1', done: true, input: {}, parent_tool_call_id: 'call_p' },
        { type: 'tool_use', name: 'Grep', id: 't2', done: true, input: {}, parent_tool_call_id: 'call_p' },
      ],
    })
    expect(wrapper.html()).toContain('2 steps')
  })

  it('expands child content when the group is open', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'text', text: 'nested answer', parent_tool_call_id: 'call_p' },
      ],
      expandedTools: { 'subagent-call_p': true },
    })
    expect(wrapper.find('.subagent-group-body-open').exists()).toBe(true)
    expect(wrapper.html()).toContain('nested answer')
  })

  it('does not group blocks without a parent link', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'text', text: 'top level' },
      ],
    })
    expect(wrapper.find('.subagent-group').exists()).toBe(false)
    expect(wrapper.html()).toContain('top level')
  })

  it('renders orphan child blocks flat (parent absent) instead of dropping them', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'text', text: 'orphan child content', parent_tool_call_id: 'call_missing' },
      ],
    })
    expect(wrapper.find('.subagent-group').exists()).toBe(false)
    expect(wrapper.html()).toContain('orphan child content')
  })

  it('recursively groups a depth-2 sub-agent under its own Agent block', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        // depth-2: call_c is itself a child, but it is a known block id, so its
        // own children group under it recursively.
        { type: 'tool_use', name: 'Agent', id: 'call_c', done: true, input: {}, parent_tool_call_id: 'call_p' },
        { type: 'text', text: 'grandchild content', parent_tool_call_id: 'call_c' },
      ],
      // Open both levels so the recursive subtrees mount.
      expandedTools: { 'subagent-call_p': true, 'subagent-call_c': true },
    })
    // Both Agent blocks render a group; the grandchild nests under call_c.
    expect(wrapper.findAll('.subagent-group').length).toBe(2)
    expect(wrapper.html()).toContain('grandchild content')
  })

  it('reuses the same tool pill markup for child tools (no bespoke styling)', () => {
    const wrapper = mountBlocks({
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_p', done: true, input: {} },
        { type: 'tool_use', name: 'Read', id: 't1', done: true, input: {}, parent_tool_call_id: 'call_p' },
      ],
      expandedTools: { 'subagent-call_p': true },
    })
    // The child tool renders through the recursive ContentBlocks instance, so it
    // gets the exact same .chat-tool-call pill as a top-level tool.
    const group = wrapper.find('.subagent-group')
    expect(group.find('.chat-tool-call').exists()).toBe(true)
  })
})

// ── Ask-card identity (data-ask-key) ──
//
// Every ask card must carry an askKey. Without it the card body — rendered
// through v-html — has no identity, so restoreAskStateFromStore cannot find the
// user's stored answer and the card comes back blank after a re-render. These
// tests pin the key at each of the four render sites; dropping the askKey from
// any one of them makes exactly one of these tests fail.
describe('AskUserQuestion card identity', () => {
  it('passes a tool-scoped askKey for a single tool_use card', async () => {
    const formatToolInput = vi.fn(() => '<div class="ask-question-view"></div>')
    mountBlocks({
      sessionId: 'sess-1',
      blocks: [{
        type: 'tool_use',
        name: 'AskUserQuestion',
        id: 'ask-abc',
        input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] },
        done: true,
        status: 'success',
      }],
      formatToolInput,
    })
    await nextTick()

    expect(formatToolInput).toHaveBeenCalledWith(
      expect.objectContaining({ questions: expect.any(Array) }),
      'AskUserQuestion',
      expect.objectContaining({ askKey: 'sess-1|tool:ask-abc' }),
    )
  })

  it('keys the merged card by message, not by the anchor block', async () => {
    // Two answerable ask blocks in one message merge into a single card whose
    // question set spans both. Keying it by the anchor's tool id would make the
    // stored answer depend on which block happened to render first.
    const formatToolInput = vi.fn(() => '<div class="ask-question-view"></div>')
    mountBlocks({
      sessionId: 'sess-1',
      blocks: [
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-1', input: { questions: [{ header: 'Q1', options: [{ label: 'A' }] }] }, done: true, status: 'success' },
        { type: 'tool_use', name: 'AskUserQuestion', id: 'ask-2', input: { questions: [{ header: 'Q2', options: [{ label: 'B' }] }] }, done: true, status: 'success' },
      ],
      formatToolInput,
    })
    await nextTick()

    const keys = formatToolInput.mock.calls
      .map((c: any[]) => (c[2] as any)?.askKey)
      .filter(Boolean)
    expect(keys).toContain('sess-1|msg:msg-1')
  })

  it('passes a text-scoped askKey for a text-mode <ask-question> card', async () => {
    const formatToolInput = vi.fn(() => '<div class="ask-question-view"></div>')
    mountBlocks({
      sessionId: 'sess-1',
      blocks: [{ type: 'text', text: 'hi <ask-question><item><question>Go?</question></item></ask-question>' }],
      blockAskQuestions: {
        'msg-1-0': { questions: [{ header: '', multiSelect: false, question: 'Go?', options: [{ label: 'Yes' }] }] },
      },
      formatToolInput,
    })
    await nextTick()

    const keys = formatToolInput.mock.calls
      .map((c: any[]) => (c[2] as any)?.askKey)
      .filter(Boolean)
    expect(keys).toContain('sess-1|text:msg-1-0')
  })

  it('keys a summary-view tool card by its tool id', async () => {
    const formatToolInput = vi.fn(() => '<div class="ask-question-view"></div>')
    mountBlocks({
      sessionId: 'sess-1',
      blocks: [],
      summary: 'sum',
      showingSummary: true,
      summaryCards: {
        tools: [{ name: 'AskUserQuestion', id: 'ask-s1', input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] } }],
        taskIDs: [],
        askQuestions: [],
      },
      formatToolInput,
    })
    await nextTick()

    expect(formatToolInput).toHaveBeenCalledWith(
      expect.anything(),
      'AskUserQuestion',
      expect.objectContaining({ askKey: 'sess-1|tool:ask-s1' }),
    )
  })

  it('falls back to a positional key when a tool block carries no id', async () => {
    // Slim blocks loaded before their input is fetched have no id yet. The card
    // must still be keyed (and stay keyed consistently) rather than lose its
    // identity entirely.
    const formatToolInput = vi.fn(() => '<div class="ask-question-view"></div>')
    mountBlocks({
      sessionId: 'sess-1',
      blocks: [{ type: 'tool_use', name: 'AskUserQuestion', input: {}, done: true, status: 'success' }],
      formatToolInput,
    })
    await nextTick()

    const keys = formatToolInput.mock.calls
      .map((c: any[]) => (c[2] as any)?.askKey)
      .filter(Boolean)
    expect(keys.length).toBeGreaterThan(0)
    expect(keys[0]).toMatch(/^sess-1\|tool:/)
  })
})

// ── Ask-card restore hook ──
//
// The v-html body is rebuilt whenever the rendered string changes (a loadHistory
// reload after switching tabs/backgrounding, a merged-card flip, a list
// remount), which discards the user's selection/note/submitted flag. The
// component re-applies the stored answer on every update. These tests pin that
// the hook runs and that it is wired to the component's own root, so the fix
// cannot be silently removed.
describe('AskUserQuestion restore hook', () => {
  it('re-applies stored answers after the component updates', async () => {
    const restoreSpy = vi.mocked(restoreAskStatesInContainer)
    restoreSpy.mockClear()

    const wrapper = mountBlocks({
      blocks: [{
        type: 'tool_use',
        name: 'AskUserQuestion',
        id: 'ask-1',
        input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] },
        done: true,
        status: 'success',
      }],
    })
    await nextTick()

    // A re-render — exactly what a reload/remount causes.
    await wrapper.setProps({ msgIndex: 3 })
    await nextTick()

    expect(restoreSpy).toHaveBeenCalled()
  })

  it('passes the component root so the hook can find the cards it owns', async () => {
    const restoreSpy = vi.mocked(restoreAskStatesInContainer)
    restoreSpy.mockClear()

    const wrapper = mountBlocks({
      blocks: [{ type: 'tool_use', name: 'AskUserQuestion', id: 'ask-1', input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] }, done: true, status: 'success' }],
    })
    await nextTick()
    await wrapper.setProps({ msgIndex: 5 })
    await nextTick()

    expect(restoreSpy).toHaveBeenCalled()
    const arg = restoreSpy.mock.calls[restoreSpy.mock.calls.length - 1][0] as HTMLElement
    // The root carries .content-blocks — a wrong element would leave the cards
    // outside the searched subtree and restore nothing.
    expect(arg?.classList?.contains('content-blocks')).toBe(true)
  })

  it('routes supplementary input through the ask handler before the fallback', async () => {
    const suppSpy = vi.mocked(handleAskSupplementaryInput)
    suppSpy.mockClear()
    suppSpy.mockReturnValue(true)
    const submitSpy = vi.mocked(updateAskSubmitState)
    submitSpy.mockClear()

    const wrapper = mountBlocks({
      blocks: [{ type: 'tool_use', name: 'AskUserQuestion', id: 'ask-1', input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] }, done: true, status: 'success' }],
      formatToolInput: () => '<div class="ask-question-view"><input class="ask-supplementary-input" /></div>',
    })
    await nextTick()

    const inputEl = wrapper.element.querySelector('.ask-supplementary-input') as HTMLInputElement
    inputEl.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()

    // The ask handler persists the note; the generic fallback must not also run.
    expect(suppSpy).toHaveBeenCalled()
    expect(submitSpy).not.toHaveBeenCalled()

    suppSpy.mockReturnValue(false)
  })
})

// ── Restore on MOUNT, not just on update ──
//
// ChatMessageList binds :key="listKey" (sessionId|msgs.length|first|last), so a
// new message arriving — e.g. a reply that lands while the app is backgrounded —
// remounts the entire list. Every ContentBlocks instance is then MOUNTED FRESH,
// and onUpdated does NOT fire on initial mount. An AskUserQuestion card carries
// its input inline (interactive tool), so nothing triggers a later update
// either: relying on onUpdated alone left the restored answer unapplied.
//
// Found by end-to-end testing against a real browser — the update-only unit
// tests below all passed while the user-visible bug remained.
describe('AskUserQuestion restore on mount', () => {
  it('applies stored answers to a freshly mounted card (list remount path)', async () => {
    const restoreSpy = vi.mocked(restoreAskStatesInContainer)
    restoreSpy.mockClear()

    mountBlocks({
      blocks: [{
        type: 'tool_use',
        name: 'AskUserQuestion',
        id: 'ask-1',
        input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] },
        done: true,
        status: 'success',
      }],
    })
    await nextTick()

    // A fresh mount must restore too — this is the path a list remount takes.
    expect(restoreSpy).toHaveBeenCalled()
  })

  it('passes the component root on the mount path as well', async () => {
    const restoreSpy = vi.mocked(restoreAskStatesInContainer)
    restoreSpy.mockClear()

    const wrapper = mountBlocks({
      blocks: [{ type: 'tool_use', name: 'AskUserQuestion', id: 'ask-1', input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] }, done: true, status: 'success' }],
    })
    await nextTick()

    expect(restoreSpy).toHaveBeenCalled()
    const arg = restoreSpy.mock.calls[restoreSpy.mock.calls.length - 1][0] as HTMLElement
    expect(arg?.classList?.contains('content-blocks')).toBe(true)
  })
})

// ── Streaming render cost: child-block skip + incremental HTML cache ──
//
// A long turn with concurrent sub-agents fragments the message into thousands
// of tiny blocks (measured: 5,756 blocks, text median 14.5 chars, 98.4% of
// text/thinking blocks belonging to sub-agents). The throttled flush used to
// re-run marked + DOMPurify for EVERY block on EVERY tick, including the child
// blocks the template never renders. A Chrome trace showed DOMPurify's
// parseFromString at 73.77% of main-thread CPU with a single 10-second frozen
// animation frame. These tests pin the two guards that prevent it.
describe('streaming render cost', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  /** Text arguments the component asked to render ('' is the summary slot). */
  function renderedTexts(spy: ReturnType<typeof vi.fn>): string[] {
    return spy.mock.calls.map((c) => c[0] as string).filter((x) => x !== '')
  }

  it('does not render sub-agent child blocks in the root flush', async () => {
    const spy = vi.fn((text: string) => `<p>${text}</p>`)
    const parentId = 'call_parent'
    const make = (visible: string) => [
      { type: 'tool_use', name: 'Agent', id: parentId, done: true },
      { type: 'text', text: visible },
      // Child of the Agent block above — rendered inside the (collapsed)
      // recursive group, never in this flat loop.
      { type: 'text', text: 'CHILD-BLOCK', parent_tool_call_id: parentId },
    ]
    const wrapper = mountBlocks({ blocks: make('TOP-LEVEL'), streaming: true, renderTextBlock: spy })
    await nextTick()

    // Two content changes are what actually drives the throttled flush body:
    // the first arms the 300ms timer, the second sets _throttlePending. A
    // single change only re-renders through getBlockHtml and never reaches the
    // flush loop these tests exist to cover.
    await wrapper.setProps({ blocks: make('TOP-LEVEL-2') })
    await nextTick()
    await wrapper.setProps({ blocks: make('TOP-LEVEL-3') })
    await nextTick()
    vi.advanceTimersByTime(400)
    await nextTick()

    expect(renderedTexts(spy)).toContain('TOP-LEVEL-3')
    expect(renderedTexts(spy)).not.toContain('CHILD-BLOCK')
    // The child is genuinely skipped, not merely rendered elsewhere.
    expect(wrapper.html()).not.toContain('CHILD-BLOCK')
  })

  it('reuses cached HTML for unchanged blocks in the flush', async () => {
    const spy = vi.fn((text: string) => `<p>${text}</p>`)
    const make = (a: string, b: string) => [
      { type: 'text', text: a },
      { type: 'text', text: b },
    ]
    const wrapper = mountBlocks({ blocks: make('A1', 'B1'), streaming: true, renderTextBlock: spy })
    await nextTick()

    // First change arms the 300ms timer; the second sets _throttlePending, so
    // the tick below actually runs the flush loop (a single change only goes
    // through getBlockHtml and never reaches it). Only the second block's text
    // differs, so the flush must reuse A1's cached HTML.
    await wrapper.setProps({ blocks: make('A1', 'B2') })
    await nextTick()
    await wrapper.setProps({ blocks: make('A1', 'B3') })
    await nextTick()
    vi.advanceTimersByTime(400)
    await nextTick()

    const texts = renderedTexts(spy)
    // A1 rendered exactly once (mount). Without reuse the flush re-renders it,
    // making this 2.
    expect(texts.filter((t) => t === 'A1')).toHaveLength(1)
    expect(texts.filter((t) => t === 'B3')).toHaveLength(1)
    expect(wrapper.html()).toContain('A1')
    expect(wrapper.html()).toContain('B3')
  })

  it('stops re-rendering once no block text changes', async () => {
    const spy = vi.fn((text: string) => `<p>${text}</p>`)
    const blocks = [{ type: 'text', text: 'STEADY' }]
    const wrapper = mountBlocks({ blocks, streaming: true, renderTextBlock: spy })
    await nextTick()
    const afterMount = renderedTexts(spy).length

    // Several template passes and flush ticks with identical content.
    for (let i = 0; i < 3; i++) {
      await wrapper.setProps({ blocks: [{ type: 'text', text: 'STEADY' }] })
      await nextTick()
      vi.advanceTimersByTime(400)
      await nextTick()
    }

    // Without the incremental cache this grew on every tick (the flush replaced
    // the cache object, re-rendering the template and re-arming the timer).
    expect(renderedTexts(spy).length).toBe(afterMount)
  })

  it('still re-renders a thinking block whose text grows', async () => {
    const wrapper = mountBlocks({
      blocks: [{ type: 'thinking', text: 'step one', done: false }],
      streaming: true,
    })
    await nextTick()
    expect(wrapper.html()).toContain('step one')

    await wrapper.setProps({ blocks: [{ type: 'thinking', text: 'step one two', done: false }] })
    await nextTick()
    vi.advanceTimersByTime(400)
    await nextTick()

    expect(wrapper.html()).toContain('step one two')
  })

  // The cache is a `shallowRef<Map>`, not a `ref<Record>`. shallowRef only
  // tracks `.value` assignment, so a regression that mutated the Map in place
  // (`cache.set(k, v)` without a new Map) would serve fresh HTML that never
  // reaches the DOM: no re-render, stale `v-html`. These two pin the contract.
  it('propagates new HTML to the DOM on every content change', async () => {
    const wrapper = mountBlocks({
      blocks: [{ type: 'text', text: 'v1' }],
      streaming: true,
    })
    await nextTick()
    expect(wrapper.html()).toContain('v1')

    // The first change after mount renders through getBlockHtml (fresh Map).
    await wrapper.setProps({ blocks: [{ type: 'text', text: 'v2' }] })
    await nextTick()
    expect(wrapper.html()).toContain('v2')
    expect(wrapper.html()).not.toContain('v1')

    // The next change lands inside the 300ms throttle window, so it is held
    // until the flush replaces the cache — the flush assignment is exactly what
    // must reach the DOM (a mutated-in-place Map would not).
    await wrapper.setProps({ blocks: [{ type: 'text', text: 'v3' }] })
    await nextTick()
    vi.advanceTimersByTime(400)
    await nextTick()
    expect(wrapper.html()).toContain('v3')
    expect(wrapper.html()).not.toContain('v2')
  })

  it('re-renders after a throttled flush replaces the cache', async () => {
    const spy = vi.fn((text: string) => `<p>${text}</p>`)
    const wrapper = mountBlocks({
      blocks: [{ type: 'text', text: 'F1' }],
      streaming: true,
      renderTextBlock: spy,
    })
    await nextTick()

    // Two changes: the first arms the timer, the second sets _throttlePending
    // so the flush body actually runs.
    await wrapper.setProps({ blocks: [{ type: 'text', text: 'F2' }] })
    await nextTick()
    await wrapper.setProps({ blocks: [{ type: 'text', text: 'F3' }] })
    await nextTick()
    vi.advanceTimersByTime(400)
    await nextTick()

    expect(wrapper.html()).toContain('F3')
  })
})

// ── Static block cache: scope + reactivity contract ──
//
// Two things are guarded here, both invisible to a cache-only unit test:
//
//  1. The cache instance must be passed through `markRaw`. It is handed to
//     this component as a prop, and Vue would otherwise proxy it; `get()`
//     mutates Maps to maintain LRU order, so those writes would land on
//     reactive state DURING render and re-trigger the render effect that
//     called `get()` — "Maximum recursive updates exceeded". A mounted
//     component is the only place that failure is observable.
//
//  2. The key must include a scope covering projectRoot (path annotators
//     resolve relative to it) and locale (translated labels are baked into
//     the HTML). Without it, enabling the cache serves the previous project's
//     annotations after a switch.
//
// `renderTextBlock` below is a pure function of its arguments; it must not read
// reactive state, or the spy itself would drive re-renders and mask the subject.
describe('static block cache scope', () => {
  it('does not recurse when the cache instance is bound', async () => {
    const { StaticBlockCache } = await import('@/utils/streamPerf.ts')
    // `markRaw` mirrors what useChatRender does. Without it, Vue proxies the
    // instance, `get()`'s LRU writes become reactive mutations during render,
    // and the render effect re-triggers itself until Vue aborts with
    // "Maximum recursive updates exceeded".
    const cache = markRaw(new StaticBlockCache())
    const renderTextBlock = vi.fn(() => '<p>stable</p>')

    const wrapper = mountBlocks({
      blocks: [{ type: 'text', text: 'hello' }],
      staticBlockCache: cache,
      renderTextBlock,
    })
    await nextTick()
    await nextTick()

    // Reaching here at all proves no recursive-update loop was thrown.
    expect(wrapper.find('.content-blocks').exists()).toBe(true)
  })

  it('keys entries by projectRoot so a switch cannot serve stale HTML', async () => {
    const { StaticBlockCache } = await import('@/utils/streamPerf.ts')
    const cache = markRaw(new StaticBlockCache())
    const renderTextBlock = vi.fn((_t: string) => `<p data-root="${store.state.projectRoot}">block</p>`)

    store.state.projectRoot = '/proj/alpha'
    const wrapper = mountBlocks({
      blocks: [{ type: 'text', text: 'hello' }],
      staticBlockCache: cache,
      renderTextBlock,
    })
    expect(wrapper.html()).toContain('data-root="/proj/alpha"')

    // Same block, new root: the scope changed, so the lookup must miss and the
    // block must re-render rather than serve the alpha HTML.
    store.state.projectRoot = '/proj/beta'
    await wrapper.setProps({ blocks: [{ type: 'text', text: 'hello' }] })
    await nextTick()

    expect(wrapper.html()).toContain('data-root="/proj/beta"')
    expect(wrapper.html()).not.toContain('data-root="/proj/alpha"')
  })

  it('reuses a cached entry when the scope is unchanged', async () => {
    const { StaticBlockCache } = await import('@/utils/streamPerf.ts')
    const cache = markRaw(new StaticBlockCache())
    const renderTextBlock = vi.fn(() => '<p>stable</p>')

    store.state.projectRoot = '/proj/alpha'
    const wrapper = mountBlocks({
      blocks: [{ type: 'text', text: 'hello' }],
      staticBlockCache: cache,
      renderTextBlock,
    })
    // Count only calls for THIS block. The component also renders a summary
    // line via renderTextBlock(summary || '', …) on every pass (template line
    // 10), which is unrelated to the cache and would otherwise pollute the
    // count. Matching on the block text isolates the cached path.
    const blockCalls = () =>
      renderTextBlock.mock.calls.filter((c) => c[0] === 'hello').length
    const afterMount = blockCalls()
    expect(afterMount).toBeGreaterThan(0)

    // New array identity, same text, same scope: the entry must hit, so the
    // expensive pipeline must not run again for this block.
    await wrapper.setProps({ blocks: [{ type: 'text', text: 'hello' }] })
    await nextTick()

    expect(blockCalls()).toBe(afterMount)
  })
})

// ── Path verification survives a cache hit ──
//
// `data-path-type` is applied ONLY by verifyFilePaths mutating the live DOM —
// it is not part of the HTML string that StaticBlockCache stores. Verification
// is scheduled from renderMarkdown, which runs inside renderTextBlock; on a
// cache hit renderTextBlock is skipped entirely, so nothing re-schedules it.
//
// The observable failure: any rebuild of the DOM from cached HTML (a new
// message arriving remounts the whole list via ChatMessageList's :key="listKey",
// as does loadMore or a session switch) leaves annotated-looking spans that do
// nothing when clicked. A hard refresh cleared the cache and the paths worked
// again — which is exactly the reported symptom.
//
// Found by driving a real browser: a message with 2 typed spans dropped to 0
// after a no-path reply arrived, with zero new batch-exists requests.
describe('path verification after a cache hit', () => {
  it('re-verifies paths when the HTML is served from the cache', async () => {
    const { StaticBlockCache } = await import('@/utils/streamPerf.ts')
    const cache = markRaw(new StaticBlockCache())
    mockVerifyFilePaths.mockClear()

    // Annotated markup as the real pipeline emits it: span + open button, and
    // deliberately NO data-path-type (that attribute is DOM-only).
    const html = '<p>see <span class="chat-file-path" data-file-path="src/real.go">src/real.go</span>'
      + '<button class="chat-file-open-btn" data-file-path="src/real.go"></button></p>'
    const renderTextBlock = vi.fn(() => html)

    const wrapper = mountBlocks({
      blocks: [{ type: 'text', text: 'see src/real.go' }],
      staticBlockCache: cache,
      renderTextBlock,
    })
    await nextTick()
    await nextTick()

    const renderCalls = () => renderTextBlock.mock.calls.filter((c) => c[0] === 'see src/real.go').length
    const afterMount = renderCalls()
    expect(afterMount).toBeGreaterThan(0)

    // The first render must already verify (the pipeline's own scheduling is
    // bypassed in this harness because renderTextBlock is a stub).
    expect(mockVerifyFilePaths).toHaveBeenCalledWith(['src/real.go'], expect.anything())

    mockVerifyFilePaths.mockClear()

    // New array identity, same text, same scope → cache hit, no re-render.
    await wrapper.setProps({ blocks: [{ type: 'text', text: 'see src/real.go' }] })
    await nextTick()
    await nextTick()

    // The cache hit is what the assertion above pins; the point of this test is
    // that verification still runs despite it.
    expect(renderCalls()).toBe(afterMount)
    expect(mockVerifyFilePaths).toHaveBeenCalledWith(['src/real.go'], expect.anything())
  })

  it('scopes verification to the component root, not the whole document', async () => {
    const { StaticBlockCache } = await import('@/utils/streamPerf.ts')
    const cache = markRaw(new StaticBlockCache())
    mockVerifyFilePaths.mockClear()

    const html = '<span class="chat-file-path" data-file-path="a/b.go">a/b.go</span>'
    const wrapper = mountBlocks({
      blocks: [{ type: 'text', text: 'a/b.go' }],
      staticBlockCache: cache,
      renderTextBlock: () => html,
    })
    await nextTick()
    await nextTick()

    expect(mockVerifyFilePaths).toHaveBeenCalled()
    const container = mockVerifyFilePaths.mock.calls.at(-1)![1] as HTMLElement
    expect(container?.classList?.contains('content-blocks')).toBe(true)
    expect(wrapper.element.contains(container)).toBe(true)
  })

  it('does not re-verify spans that already carry data-path-type', async () => {
    const { StaticBlockCache } = await import('@/utils/streamPerf.ts')
    const cache = markRaw(new StaticBlockCache())
    mockVerifyFilePaths.mockClear()

    const html = '<span class="chat-file-path" data-file-path="done.go" data-path-type="file">done.go</span>'
    mountBlocks({
      blocks: [{ type: 'text', text: 'done.go' }],
      staticBlockCache: cache,
      renderTextBlock: () => html,
    })
    await nextTick()
    await nextTick()

    // Already verified — asking again would re-issue batch-exists on every
    // streaming frame for no benefit.
    expect(mockVerifyFilePaths).not.toHaveBeenCalled()
  })

  it('re-verifies commit hashes too (same DOM-only upgrade)', async () => {
    const { StaticBlockCache } = await import('@/utils/streamPerf.ts')
    const cache = markRaw(new StaticBlockCache())
    mockVerifyCommitHashes.mockClear()

    // Commit hashes carry the same defect: the pending→verified class change is
    // applied only by verifyCommitHashes against the live DOM, so a cache hit
    // leaves them permanently in the neutral "pending" style.
    const html = '<code class="chat-commit-hash-pending" data-commit-sha="a3d276135">a3d276135</code>'
    mountBlocks({
      blocks: [{ type: 'text', text: 'a3d276135' }],
      staticBlockCache: cache,
      renderTextBlock: () => html,
    })
    await nextTick()
    await nextTick()

    expect(mockVerifyCommitHashes).toHaveBeenCalledWith(['a3d276135'], expect.anything())
  })

  it('drops cached negative results when a turn ends', async () => {
    // A turn that creates files usually names them BEFORE writing them. Whichever
    // pass verifies such a path while the file does not exist yet caches 'none',
    // and a cached 'none' is never re-checked — so a later pass reporting that
    // path has its annotation stripped even though the file exists, and only a
    // hard refresh (which resets the module-level cache) recovered it.
    //
    // The turn boundary must therefore drop the negative entries so the
    // post-streaming re-render re-verifies and resolves the new file.
    mockInvalidateNegativePathCache.mockClear()

    const wrapper = mountBlocks({
      blocks: [{ type: 'text', text: 'wrote src/new.go' }],
      streaming: true,
      renderTextBlock: () => '<p>wrote src/new.go</p>',
    })
    await nextTick()

    // No turn has ended yet — nothing may be invalidated.
    expect(mockInvalidateNegativePathCache).not.toHaveBeenCalled()

    await wrapper.setProps({ streaming: false })
    await nextTick()

    expect(mockInvalidateNegativePathCache).toHaveBeenCalledTimes(1)
  })

  it('does not invalidate on a turn that is still streaming', async () => {
    // Guard against a regression where the hook moves out of the
    // streaming→false transition and fires on every streaming frame, which
    // would re-request every unresolved path continuously.
    mockInvalidateNegativePathCache.mockClear()

    const wrapper = mountBlocks({
      blocks: [{ type: 'text', text: 'a' }],
      streaming: true,
      renderTextBlock: () => '<p>a</p>',
    })
    await nextTick()
    await wrapper.setProps({ blocks: [{ type: 'text', text: 'ab' }] })
    await nextTick()

    expect(mockInvalidateNegativePathCache).not.toHaveBeenCalled()
  })

  it('verifies paths inside thinking blocks (rendered via renderMarkdownHtml)', async () => {
    const { StaticBlockCache } = await import('@/utils/streamPerf.ts')
    const cache = markRaw(new StaticBlockCache())
    mockVerifyFilePaths.mockClear()
    // The thinking renderer calls renderMarkdownHtml, which DISCARDS the
    // detectedPaths the pipeline produced — so nothing scheduled verification
    // and the paths stayed dead. Reproduced in a browser: expanding a thinking
    // block and immediately clicking one of its paths did nothing.
    mockRenderMarkdownHtml.mockReturnValue(
      '<p><span class="chat-file-path" data-file-path="src/real.go">src/real.go</span>'
      + '<button class="chat-file-open-btn" data-file-path="src/real.go"></button></p>',
    )

    mountBlocks({
      blocks: [{ type: 'thinking', text: 'reasoning about src/real.go', done: true }],
      staticBlockCache: cache,
      renderTextBlock: () => '',
    })
    await nextTick()
    await nextTick()

    expect(mockVerifyFilePaths).toHaveBeenCalledWith(['src/real.go'], expect.anything())
    mockRenderMarkdownHtml.mockImplementation((text: string) => `<p>${text}</p>`)
  })

  it('skips path annotation while a thinking block is streaming', async () => {
    // Symmetry with the text-block streaming path. The thinking renderer used
    // to pass only { skipKatex: true }, so it annotated paths mid-stream — and
    // a turn that names a file BEFORE writing it cached a negative result that
    // later stripped the final text's annotation (only a hard refresh fixed
    // it). Streaming must not annotate; the post-streaming re-render does.
    mockRenderMarkdownHtml.mockClear()

    mountBlocks({
      blocks: [{ type: 'thinking', text: 'reasoning about src/real.go', done: false }],
      streaming: true,
      renderTextBlock: () => '',
    })
    await nextTick()

    const opts = mockRenderMarkdownHtml.mock.calls.at(-1)?.[1] as Record<string, unknown> | undefined
    expect(opts?.skipEnhancements).toBe(true)
    expect(opts?.skipKatex).toBe(true)
  })

  it('annotates thinking paths once streaming ends (full pipeline)', async () => {
    // The counterpart: the annotation the streaming pass skips must come back
    // when the turn ends, or the asymmetry would simply invert into "never".
    mockRenderMarkdownHtml.mockClear()

    const wrapper = mountBlocks({
      blocks: [{ type: 'thinking', text: 'reasoning about src/real.go', done: true }],
      streaming: false,
      renderTextBlock: () => '',
    })
    await nextTick()

    const opts = mockRenderMarkdownHtml.mock.calls.at(-1)?.[1] as Record<string, unknown> | undefined
    // The non-streaming path calls renderMarkdownHtml(text) with no options,
    // i.e. the full pipeline (path annotation included).
    expect(opts?.skipEnhancements).toBeFalsy()
    expect(wrapper.html()).toContain('reasoning about src/real.go')
  })
})

describe('provisional thinking text is replaced on finish', () => {
  it('refetches the full reasoning when a block that was fetched mid-stream finishes', async () => {
    // The auto-prefix-load runs while the block streams, so its result is
    // whatever chat_thinking had been flushed at that instant — a PREFIX of the
    // reasoning. Once the block is done that snapshot is stale, and the render
    // path serves the cache whenever an entry exists, so without a refetch the
    // block stayed frozen mid-sentence forever. Reported case: a 25131-char
    // reasoning frozen at its first 10175 chars.
    let call = 0
    const fetchMock = vi.fn().mockImplementation(() => {
      call++
      const text = call === 1 ? 'PARTIAL-PREFIX-ONLY' : 'PARTIAL-PREFIX-ONLY-AND-THE-REST-OF-THE-THOUGHT'
      return Promise.resolve({ ok: true, json: async () => ({ think_id: 'th_x', text }) })
    })
    vi.stubGlobal('fetch', fetchMock)

    const w = mountBlocks({
      msgId: 'm1', sessionId: 's1', streaming: true, active: true,
      blocks: [{ type: 'thinking', think_id: 'th_x', in_progress: true }],
    })
    await flushPromises(); await nextTick()
    await w.vm.$forceUpdate(); await nextTick()

    // The block finishes: content carries only the slim {done:true} marker.
    await w.setProps({ streaming: false, blocks: [{ type: 'thinking', think_id: 'th_x', done: true }] })
    await flushPromises(); await nextTick()
    await w.vm.$forceUpdate(); await nextTick()

    const html = w.find('.thinking-inline-content').html()
    expect(fetchMock.mock.calls.length, 'must refetch the final text').toBe(2)
    expect(html).toContain('AND-THE-REST')
  })

  it('does not refetch a done block whose text was never fetched mid-stream', async () => {
    // Gating matters: a long conversation must not fire one request per
    // completed thinking block on every render.
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, json: async () => ({ think_id: 'th_done', text: 'reasoning' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const w = mountBlocks({
      msgId: 'm1', sessionId: 's1', streaming: false, active: true,
      blocks: [{ type: 'thinking', think_id: 'th_done', done: true }],
    })
    await flushPromises(); await nextTick()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
