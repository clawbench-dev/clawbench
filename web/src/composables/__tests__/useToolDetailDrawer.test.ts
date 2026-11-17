import { describe, it, expect, vi, beforeEach } from 'vitest'
import { nextTick, reactive } from 'vue'

// Mock i18n modules before importing the composable
vi.mock('@/i18n', () => ({
  STORAGE_KEY: 'clawbench-locale',
  default: {
    global: {
      locale: { value: 'en' },
      t: (key: string) => key,
    },
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
  createI18n: () => ({}),
}))

import { useToolDetailDrawer } from '../useToolDetailDrawer.ts'

function createMockChatRender() {
  return {
    formatToolInput: (input: Record<string, unknown>, _name: string) => JSON.stringify(input),
    toolCallSummary: () => 'summary',
  }
}

describe('useToolDetailDrawer', () => {
  let drawer: ReturnType<typeof useToolDetailDrawer>

  beforeEach(() => {
    drawer = useToolDetailDrawer({ chatRender: createMockChatRender() })
  })

  describe('fetch-in-flight guard', () => {
    it('prevents concurrent fetchToolCallDetail calls', async () => {
      let resolveFirst!: (v: Response) => void
      const firstPromise = new Promise<Response>((r) => { resolveFirst = r })
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockReturnValueOnce(firstPromise as Promise<Response>)

      // First call starts the fetch
      const fetch1 = drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })
      expect(fetchSpy).toHaveBeenCalledTimes(1)

      // Second call should be blocked by the in-flight guard
      drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })
      expect(fetchSpy).toHaveBeenCalledTimes(1) // still only 1 call

      // Resolve the first fetch
      resolveFirst!(new Response(JSON.stringify({ input: { cmd: 'ls' }, output: 'ok', done: true, status: 'completed' }), { status: 200 }))
      await fetch1

      fetchSpy.mockRestore()
    })
  })

  describe('done/status sync from API', () => {
    it('syncs duration_ms from API response', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'ls' }, output: 'ok', done: true, status: 'completed', duration_ms: 4200 }), { status: 200 })
      )

      await drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })

      expect(drawer.toolDetailData.value.duration).toBe(4200)

      fetchSpy.mockRestore()
    })

    it('syncs done=true from API response', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'ls' }, output: 'ok', done: true, status: 'completed' }), { status: 200 })
      )

      await drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })

      expect(drawer.toolDetailData.value.done).toBe(true)
      expect(drawer.toolDetailData.value.status).toBe('completed')

      fetchSpy.mockRestore()
    })

    it('keeps a running tool running when the API agrees', async () => {
      // Opened on a live, unfinished block: the drawer must stay in its running
      // state (done only ever moves forward, so the API's false is a no-op here
      // rather than the thing that decides it).
      drawer.handleShowToolDetail({ name: 'Bash', input: { cmd: 'sleep 60' }, output: 'ok', tool_id: 'tool1', msgId: 1, blockIdx: 0, done: false })
      expect(drawer.toolDetailData.value.done).toBe(false)

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'ls' }, output: '', done: false, status: 'running' }), { status: 200 })
      )

      await drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })

      expect(drawer.toolDetailData.value.done).toBe(false)
      expect(drawer.toolDetailData.value.status).toBe('running')

      fetchSpy.mockRestore()
    })

    // The API record can be older than the block the drawer opened on (a
    // cancelled turn keeps an empty status in chat_tool_calls for ever). An
    // empty status is "no verdict", not a correction — applying it verbatim
    // erased "interrupted"/"error" and fell back to the green check.
    it('does not let a stale empty status erase the verdict', async () => {
      drawer.handleShowToolDetail({ name: 'Bash', input: { cmd: 'sleep 60' }, output: 'ok', tool_id: 'tool1', msgId: 1, blockIdx: 0, done: true })
      drawer.toolDetailData.value.status = 'interrupted'
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'ls' }, output: 'ok', done: true, status: '' }), { status: 200 })
      )

      await drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })

      expect(drawer.toolDetailData.value.status).toBe('interrupted')

      fetchSpy.mockRestore()
    })

    // A settled row whose tool never reported a result (cancelled turn, crashed
    // agent, steer split) keeps done=false in chat_tool_calls for ever, while the
    // transcript renders that block as done (parseAssistantContent: a turn that
    // is over cannot still be running). The drawer must agree with the
    // transcript — it used to take the record's false verbatim and spin forever,
    // which is the "详情还在执行，收起状态下却是已完成" report.
    it('keeps a finished tool done when a stale API response says done=false', async () => {
      drawer.handleShowToolDetail({ name: 'Bash', input: { cmd: 'sleep 60' }, output: 'ok', tool_id: 'tool1', msgId: 1, blockIdx: 0, done: true })
      expect(drawer.toolDetailData.value.done).toBe(true)

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'sleep 60' }, output: '', done: false }), { status: 200 })
      )
      await drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })

      expect(drawer.toolDetailData.value.done).toBe(true)

      fetchSpy.mockRestore()
    })
  })

  describe('done is monotonic against the live block', () => {
    it('does not let a response produced before the tool finished undo it', async () => {
      // The live block is reactive: the tool finishes while the detail fetch is
      // still in flight (the fetch was started when the row still said
      // done=false). Its response must not walk the drawer back to a spinner.
      const live = reactive({ name: 'Bash', done: false, status: '', duration_ms: 0 })
      const d = useToolDetailDrawer({ chatRender: createMockChatRender(), findLiveBlock: () => live })

      let resolveFetch!: (v: Response) => void
      const pending = new Promise<Response>((r) => { resolveFetch = r })
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockReturnValueOnce(pending as Promise<Response>)

      // output missing → handleShowToolDetail starts the fetch itself
      d.handleShowToolDetail({ name: 'Bash', input: { cmd: 'sleep 60' }, output: '', tool_id: 'tool1', msgId: 1, blockIdx: 0, done: false })
      expect(d.toolDetailData.value.done).toBe(false)

      live.done = true
      await nextTick()
      expect(d.toolDetailData.value.done).toBe(true)

      resolveFetch(new Response(JSON.stringify({ input: { cmd: 'sleep 60' }, output: '', done: false }), { status: 200 }))
      await pending
      await new Promise((r) => setTimeout(r, 0))
      await nextTick()

      expect(d.toolDetailData.value.done).toBe(true)

      fetchSpy.mockRestore()
    })
  })

  describe('closeOverlay resets guards', () => {
    it('resets fetch-in-flight and retry-requested flags', () => {
      drawer.closeOverlay()
      // After close, a new fetch should not be blocked
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'ls' }, output: 'ok', done: true }), { status: 200 })
      )

      // This should not be blocked
      drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })
      expect(fetchSpy).toHaveBeenCalledTimes(1)

      fetchSpy.mockRestore()
    })
  })

  describe('retry-while-in-flight', () => {
    it('retry click during in-flight defers re-fetch until completion', async () => {
      let resolveFirst!: (v: Response) => void
      const firstPromise = new Promise<Response>((r) => { resolveFirst = r })
      const fetchSpy = vi.spyOn(globalThis, 'fetch')
        .mockReturnValueOnce(firstPromise as Promise<Response>)
        .mockResolvedValueOnce(
          new Response(JSON.stringify({ input: { cmd: 'ls' }, output: 'ok2', done: true, status: 'completed' }), { status: 200 })
        )

      // Open overlay with both input AND output so handleShowToolDetail doesn't auto-fetch
      drawer.handleShowToolDetail({ name: 'Bash', input: { cmd: 'ls' }, output: 'initial output', tool_id: 'tool1', msgId: 1, blockIdx: 0 })

      // Start a fetch manually
      const fetch1 = drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })
      expect(fetchSpy).toHaveBeenCalledTimes(1)

      // Set _fetchIds so handleOverlayRetryClick can find them
      drawer.toolDetailData.value._fetchIds = { toolId: 'tool1', msgId: 1 }

      // Simulate retry button click while fetch is in flight
      const emptyDiv = document.createElement('div')
      emptyDiv.className = 'tool-call-empty'
      emptyDiv.dataset.retry = '1'
      const btn = document.createElement('button')
      btn.className = 'tool-call-retry-btn'
      emptyDiv.appendChild(btn)
      const retryClickEvent = { target: btn } as unknown as MouseEvent

      drawer.handleOverlayRetryClick(retryClickEvent)

      // First fetch should still only have 1 call (retry deferred)
      expect(fetchSpy).toHaveBeenCalledTimes(1)

      // Resolve the first fetch
      resolveFirst!(new Response(JSON.stringify({ input: { cmd: 'old' }, output: 'old', done: false, status: 'running' }), { status: 200 }))
      await fetch1
      await nextTick()
      // Wait for retry-initiated fetch to complete
      await nextTick()

      // The retry should have triggered a second fetch
      expect(fetchSpy).toHaveBeenCalledTimes(2)

      fetchSpy.mockRestore()
    })
  })

  describe('handleFileOpenInOverlay', () => {
    it('calls onFileOpen with string payload', () => {
      const mockOnFileOpen = vi.fn()
      const d = useToolDetailDrawer({ chatRender: createMockChatRender(), onFileOpen: mockOnFileOpen })

      d.handleFileOpenInOverlay('/path/to/file.ts')

      expect(mockOnFileOpen).toHaveBeenCalledWith('/path/to/file.ts', undefined, undefined)
      expect(d.isOpen.value).toBe(false)
    })

    it('calls onFileOpen with object payload including line range', () => {
      const mockOnFileOpen = vi.fn()
      const d = useToolDetailDrawer({ chatRender: createMockChatRender(), onFileOpen: mockOnFileOpen })

      d.handleFileOpenInOverlay({ path: '/src/app.ts', lineStart: 10, lineEnd: 25 })

      expect(mockOnFileOpen).toHaveBeenCalledWith('/src/app.ts', 10, 25)
      expect(d.isOpen.value).toBe(false)
    })

    it('closes drawer even when onFileOpen is not provided', () => {
      const d = useToolDetailDrawer({ chatRender: createMockChatRender() })
      d.handleShowToolDetail({ name: 'Bash', input: { cmd: 'ls' }, output: 'ok', msgId: 1, blockIdx: 0 })
      expect(d.isOpen.value).toBe(true)

      d.handleFileOpenInOverlay('/some/file.ts')

      expect(d.isOpen.value).toBe(false)
    })
  })

  describe('handleShowToolDetail with fetch trigger', () => {
    it('fetches tool call detail when input is missing', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'ls' }, output: 'ok', done: true, status: 'completed' }), { status: 200 })
      )

      // Pass block without input but with tool_id and msgId — should trigger fetch
      drawer.handleShowToolDetail({ name: 'Bash', tool_id: 'tool1', msgId: 1, blockIdx: 0 })

      expect(fetchSpy).toHaveBeenCalledTimes(1)
      expect(drawer.toolDetailData.value._fetchIds).toEqual({ toolId: 'tool1', msgId: 1 })

      fetchSpy.mockRestore()
    })

    it('sets activeToolOverlay when blockIdx is provided', () => {
      drawer.handleShowToolDetail({ name: 'Bash', input: { cmd: 'ls' }, output: 'ok', msgId: 42, blockIdx: 3 })

      expect(drawer.activeToolOverlay.value).toEqual({ msgId: '42', blockIdx: 3 })
    })

    // A card without a block index (summary cards pass none — their index is
    // into summaryCards.tools) must not inherit the previous tool's
    // coordinates, or the live-block watcher copies that tool's state here.
    it('clears activeToolOverlay when the block has no index', () => {
      drawer.handleShowToolDetail({ name: 'Bash', input: { cmd: 'ls' }, output: 'ok', msgId: 42, blockIdx: 3 })
      drawer.handleShowToolDetail({ name: 'Read', input: { path: 'a.ts' }, output: 'ok', tool_id: 'tool-sum', msgId: 42 })

      expect(drawer.activeToolOverlay.value).toBeNull()
    })

    it('sets DeepThink display name override when no display_name', () => {
      drawer.handleShowToolDetail({ name: 'DeepThink', input: {}, output: 'result', msgId: 1, blockIdx: 0 })

      expect(drawer.toolDetailData.value.displayNameOverride).toBe('chat.message.deepThinking')
    })

    it('copies duration_ms from the live block', () => {
      drawer.handleShowToolDetail({ name: 'Bash', input: { cmd: 'ls' }, output: 'ok', duration_ms: 9000, msgId: 1, blockIdx: 0 })

      expect(drawer.toolDetailData.value.duration).toBe(9000)
    })

    it('defaults duration to 0 when block has none', () => {
      drawer.handleShowToolDetail({ name: 'Bash', input: { cmd: 'ls' }, output: 'ok', msgId: 1, blockIdx: 0 })

      expect(drawer.toolDetailData.value.duration).toBe(0)
    })

    it('does not override DeepThink display name when display_name is present', () => {
      drawer.handleShowToolDetail({ name: 'DeepThink', display_name: 'explore', input: {}, output: 'result', msgId: 1, blockIdx: 0 })

      expect(drawer.toolDetailData.value.displayNameOverride).toBe('')
    })
  })
})

// ── Ask-card identity in the drawer ──
//
// The drawer renders the same AskUserQuestion body as the chat list, so it must
// pass the SAME askKey — otherwise an answer typed in the drawer and one typed
// in the list become two divergent copies of the same question, and only one of
// them survives a reload.
describe('useToolDetailDrawer — ask-card identity', () => {
  it('passes a session-scoped tool askKey when formatting a tool body', () => {
    const formatToolInput = vi.fn((_input: unknown, _name: string) => '<div class="ask-question-view"></div>')
    const d = useToolDetailDrawer({
      chatRender: { formatToolInput, toolCallSummary: () => 'summary' },
      sessionId: () => 'sess-1',
    })

    d.handleShowToolDetail({
      name: 'AskUserQuestion',
      id: 'ask-xyz',
      input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] },
      output: 'ok',
      msgId: 1,
      blockIdx: 0,
    })

    expect(formatToolInput).toHaveBeenCalledWith(
      expect.anything(),
      'AskUserQuestion',
      expect.objectContaining({ askKey: 'sess-1|tool:ask-xyz' }),
    )
  })

  it('falls back to tool_id when the block carries no id', () => {
    const formatToolInput = vi.fn((_input: unknown, _name: string) => '<div class="ask-question-view"></div>')
    const d = useToolDetailDrawer({
      chatRender: { formatToolInput, toolCallSummary: () => 'summary' },
      sessionId: () => 'sess-1',
    })

    d.handleShowToolDetail({
      name: 'AskUserQuestion',
      tool_id: 'ask-legacy',
      input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] },
      output: 'ok',
      msgId: 1,
      blockIdx: 0,
    })

    expect(formatToolInput).toHaveBeenCalledWith(
      expect.anything(),
      'AskUserQuestion',
      expect.objectContaining({ askKey: 'sess-1|tool:ask-legacy' }),
    )
  })

  it('uses the same no-session fallback the chat list uses', () => {
    // A card with no session id must still key consistently across both views,
    // or the drawer and the list would disagree about which card this is.
    const formatToolInput = vi.fn((_input: unknown, _name: string) => '<div class="ask-question-view"></div>')
    const d = useToolDetailDrawer({
      chatRender: { formatToolInput, toolCallSummary: () => 'summary' },
    })

    d.handleShowToolDetail({
      name: 'AskUserQuestion',
      id: 'ask-1',
      input: { questions: [{ header: 'H', options: [{ label: 'A' }] }] },
      output: 'ok',
      msgId: 1,
      blockIdx: 0,
    })

    expect(formatToolInput).toHaveBeenCalledWith(
      expect.anything(),
      'AskUserQuestion',
      expect.objectContaining({ askKey: 'no-session|tool:ask-1' }),
    )
  })
})
