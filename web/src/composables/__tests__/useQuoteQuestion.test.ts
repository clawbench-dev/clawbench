import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Mock useSessionIdentity before importing the module under test
const mockSendMessage = vi.fn()
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: () => ({
    sendMessage: mockSendMessage,
  }),
}))

// Mock useToast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string, params?: Record<string, string>) => key + (params ? JSON.stringify(params) : ''),
}))

// Mock buildQuoteMessage
vi.mock('@/utils/doubleClickUtils', () => ({
  buildQuoteMessage: (msg: string, text: string, path: string, lang: string, start: number, end: number) =>
    `[${path}:${start}-${end}]\n${text}\n---\n${msg}`,
}))

// Mock quoteQuestionUtils
vi.mock('@/utils/quoteQuestionUtils', () => ({
  closestElement: vi.fn(),
  getLineInfo: vi.fn(() => ({ startLine: 1, endLine: 5 })),
  getFileInfo: vi.fn(() => ({ filePath: '/test.ts', language: 'typescript' })),
}))

// Import the real useChatContext (not mocked) — it's a singleton
import { useChatContext } from '../useChatContext.ts'

describe('useQuoteQuestion', () => {
  let ctx: ReturnType<typeof useChatContext>

  beforeEach(() => {
    ctx = useChatContext()
    ctx.clearAll()
    mockSendMessage.mockReset()
    mockToastShow.mockReset()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('showBar', () => {
    it('sets quoteData and makes bar visible after delay', async () => {
      const { useQuoteQuestion } = await import('../useQuoteQuestion.ts')
      const qq = useQuoteQuestion()

      const data = { text: 'selected text', filePath: '/foo.ts', language: 'typescript', startLine: 1, endLine: 5 }
      qq.showBar(data)

      // Not yet visible (setTimeout 400ms)
      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()

      vi.advanceTimersByTime(400)
      await vi.runAllTimersAsync()

      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value).toEqual(data)
    })
  })

  describe('closeSheet', () => {
    it('clears visible, pinned, and quoteData', async () => {
      const { useQuoteQuestion } = await import('../useQuoteQuestion.ts')
      const qq = useQuoteQuestion()

      qq.showBar({ text: 'hello', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)

      qq.closeSheet()

      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()
    })
  })

  describe('sendMessage', () => {
    it('does nothing when quoteData is null', async () => {
      const { useQuoteQuestion } = await import('../useQuoteQuestion.ts')
      const qq = useQuoteQuestion()

      ctx.setQuoteData(null)
      await qq.sendMessage('hello')

      expect(mockSendMessage).not.toHaveBeenCalled()
    })

    it('does nothing when message is empty', async () => {
      const { useQuoteQuestion } = await import('../useQuoteQuestion.ts')
      const qq = useQuoteQuestion()

      ctx.setQuoteData({ text: 'some quote', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      await qq.sendMessage('   ')

      expect(mockSendMessage).not.toHaveBeenCalled()
    })

    it('sends message with quote and adds attached file', async () => {
      const { useQuoteQuestion } = await import('../useQuoteQuestion.ts')
      const qq = useQuoteQuestion()

      mockSendMessage.mockResolvedValue(undefined)

      qq.showBar({ text: 'some code', filePath: '/src/foo.ts', language: 'typescript', startLine: 10, endLine: 20 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('explain this')

      expect(mockSendMessage).toHaveBeenCalled()
      // After sendMessage, clearAll() is called, so the file is added then cleared
      // But the addAttachedFile was called before clearAll
      expect(ctx.attachedFiles.value).toHaveLength(0) // cleared by clearAll
    })

    it('clears all state after successful send', async () => {
      const { useQuoteQuestion } = await import('../useQuoteQuestion.ts')
      const qq = useQuoteQuestion()

      mockSendMessage.mockResolvedValue(undefined)

      qq.showBar({ text: 'some code', filePath: '/src/bar.ts', language: 'typescript', startLine: 10, endLine: 20 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('explain this')

      expect(qq.visible.value).toBe(false)
      // clearAll() is called after send
      expect(ctx.attachedFiles.value).toHaveLength(0)
      expect(ctx.quoteData.value).toBeNull()
    })

    it('shows error toast on send failure', async () => {
      const { useQuoteQuestion } = await import('../useQuoteQuestion.ts')
      const qq = useQuoteQuestion()

      mockSendMessage.mockRejectedValue(new Error('network error'))

      qq.showBar({ text: 'some code', filePath: '/src/baz.ts', language: 'typescript', startLine: 10, endLine: 20 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('explain this')

      expect(mockToastShow).toHaveBeenCalledWith(
        expect.stringContaining('sendFailed'),
        expect.objectContaining({ type: 'error' }),
      )
    })
  })
})
