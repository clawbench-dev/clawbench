import { describe, it, expect, vi, beforeEach } from 'vitest'
import { nextTick } from 'vue'

// Mock i18n modules before importing the composable
vi.mock('@/i18n', () => ({
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
    it('syncs done=true from API response', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'ls' }, output: 'ok', done: true, status: 'completed' }), { status: 200 })
      )

      await drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })

      expect(drawer.toolDetailData.value.done).toBe(true)
      expect(drawer.toolDetailData.value.status).toBe('completed')

      fetchSpy.mockRestore()
    })

    it('syncs done=false from API response during streaming', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'ls' }, output: '', done: false, status: 'running' }), { status: 200 })
      )

      await drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })

      expect(drawer.toolDetailData.value.done).toBe(false)
      expect(drawer.toolDetailData.value.status).toBe('running')

      fetchSpy.mockRestore()
    })

    it('syncs empty string status from API response', async () => {
      drawer.toolDetailData.value.status = 'running'
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ input: { cmd: 'ls' }, output: 'ok', done: true, status: '' }), { status: 200 })
      )

      await drawer.fetchToolCallDetail('tool1', 1, { name: 'Bash' })

      expect(drawer.toolDetailData.value.status).toBe('')

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
      expect(d.drawer.isOpen.value).toBe(false)
    })

    it('calls onFileOpen with object payload including line range', () => {
      const mockOnFileOpen = vi.fn()
      const d = useToolDetailDrawer({ chatRender: createMockChatRender(), onFileOpen: mockOnFileOpen })

      d.handleFileOpenInOverlay({ path: '/src/app.ts', lineStart: 10, lineEnd: 25 })

      expect(mockOnFileOpen).toHaveBeenCalledWith('/src/app.ts', 10, 25)
      expect(d.drawer.isOpen.value).toBe(false)
    })

    it('closes drawer even when onFileOpen is not provided', () => {
      const d = useToolDetailDrawer({ chatRender: createMockChatRender() })
      d.drawer.open()
      expect(d.drawer.isOpen.value).toBe(true)

      d.handleFileOpenInOverlay('/some/file.ts')

      expect(d.drawer.isOpen.value).toBe(false)
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

    it('sets DeepThink display name override when no display_name', () => {
      drawer.handleShowToolDetail({ name: 'DeepThink', input: {}, output: 'result', msgId: 1, blockIdx: 0 })

      expect(drawer.toolDetailData.value.displayNameOverride).toBe('chat.message.deepThinking')
    })

    it('does not override DeepThink display name when display_name is present', () => {
      drawer.handleShowToolDetail({ name: 'DeepThink', display_name: 'explore', input: {}, output: 'result', msgId: 1, blockIdx: 0 })

      expect(drawer.toolDetailData.value.displayNameOverride).toBe('')
    })
  })
})
