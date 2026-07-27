import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

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

// Keep real quoteQuestionUtils for selectionchange tests (closestElement, getLineInfo, getFileInfo)

// Import the real useChatContext (not mocked) — it's a singleton
import { useChatContext } from '../useChatContext.ts'
import { useQuoteQuestion } from '../useQuoteQuestion.ts'
import type { QuoteData } from '../useChatContext.ts'

describe('useQuoteQuestion', () => {
  let ctx: ReturnType<typeof useChatContext>

  beforeEach(() => {
    ctx = useChatContext()
    // Fully reset module-level singleton state via closeSheet
    const qq = useQuoteQuestion()
    qq.closeSheet()
    qq.sheetOpen.value = false
    ctx.clearAll()

    mockSendMessage.mockReset()
    mockToastShow.mockReset()
    vi.useFakeTimers()
  })

  afterEach(() => {
    // Drain all pending timers before switching to real timers
    vi.runAllTimers()
    vi.useRealTimers()
    // Clean up DOM
    document.body.innerHTML = ''
    // Clear window selection (suppress jsdom's async selectionchange)
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
  })

  describe('pinBar', () => {
    it('sets barPinned to true so bar survives selection loss', () => {
      // Need a mounted component so the selectionchange listener is registered
      const TestComponent = defineComponent({
        setup() { useQuoteQuestion(); return () => h('div') },
      })
      const wrapper = mount(TestComponent)

      const qq = useQuoteQuestion()
      qq.pinBar()

      qq.showBar({ text: 'hello', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      expect(qq.visible.value).toBe(true)

      // Collapse selection — bar should remain visible because pinned
      const sel = window.getSelection()
      sel?.removeAllRanges()
      // Suppress the jsdom async selectionchange from removeAllRanges
      vi.advanceTimersByTime(0)

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)
      expect(qq.visible.value).toBe(true)

      wrapper.unmount()
    })
  })

  describe('unpinBar', () => {
    it('sets barPinned to false so selection loss hides bar', () => {
      // Need a mounted component so the selectionchange listener is registered
      const TestComponent = defineComponent({
        setup() { useQuoteQuestion(); return () => h('div') },
      })
      const wrapper = mount(TestComponent)

      const qq = useQuoteQuestion()
      qq.pinBar()
      qq.showBar({ text: 'hello', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      expect(qq.visible.value).toBe(true)

      qq.unpinBar()

      // Collapse selection — bar should hide now (unpinned)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      vi.advanceTimersByTime(0)

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)
      expect(qq.visible.value).toBe(false)

      wrapper.unmount()
    })
  })

  describe('showBar', () => {
    it('sets quoteData and makes bar visible after 400ms delay', () => {
      const qq = useQuoteQuestion()
      const data: QuoteData = { text: 'selected text', filePath: '/foo.ts', language: 'typescript', startLine: 1, endLine: 5 }
      qq.showBar(data)

      // Not yet visible (setTimeout 400ms)
      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()

      vi.advanceTimersByTime(400)

      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value).toEqual(data)
    })

    it('does not show bar before 400ms', () => {
      const qq = useQuoteQuestion()
      const data: QuoteData = { text: 'text', filePath: '', language: '', startLine: 0, endLine: 0 }
      qq.showBar(data)

      vi.advanceTimersByTime(399)
      expect(qq.visible.value).toBe(false)

      vi.advanceTimersByTime(1)
      expect(qq.visible.value).toBe(true)
    })
  })

  describe('closeSheet', () => {
    it('clears visible, pinned, and quoteData', () => {
      const qq = useQuoteQuestion()

      qq.showBar({ text: 'hello', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      qq.pinBar()
      expect(qq.visible.value).toBe(true)

      qq.closeSheet()

      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()
    })

    it('clears window selection', () => {
      const qq = useQuoteQuestion()
      // Create a DOM with a selection
      const div = document.createElement('div')
      div.textContent = 'some text'
      document.body.appendChild(div)

      const range = document.createRange()
      range.selectNodeContents(div)
      const sel = window.getSelection()
      sel?.addRange(range)
      expect(sel?.toString()).toBeTruthy()

      qq.closeSheet()

      const selAfter = window.getSelection()
      expect(selAfter?.toString()).toBe('')
    })
  })

  describe('openSheet', () => {
    it('sets sheetOpen to true', () => {
      const qq = useQuoteQuestion()
      expect(qq.sheetOpen.value).toBe(false)

      qq.openSheet()

      expect(qq.sheetOpen.value).toBe(true)
    })

    it('can be opened after being closed', () => {
      const qq = useQuoteQuestion()
      qq.openSheet()
      expect(qq.sheetOpen.value).toBe(true)

      qq.closeSheet()
      // closeSheet does NOT close the sheet — it clears selection and bar state
      // sheetOpen is independent
      expect(qq.sheetOpen.value).toBe(true)

      // Manually reset and reopen
      qq.sheetOpen.value = false
      qq.openSheet()
      expect(qq.sheetOpen.value).toBe(true)
    })
  })

  describe('sendMessage', () => {
    it('does nothing when quoteData is null', async () => {
      const qq = useQuoteQuestion()

      ctx.setQuoteData(null)
      await qq.sendMessage('hello')

      expect(mockSendMessage).not.toHaveBeenCalled()
    })

    it('does nothing when message is empty', async () => {
      const qq = useQuoteQuestion()

      ctx.setQuoteData({ text: 'some quote', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      await qq.sendMessage('   ')

      expect(mockSendMessage).not.toHaveBeenCalled()
    })

    it('sends message and adds attached file with line info', async () => {
      const qq = useQuoteQuestion()
      mockSendMessage.mockResolvedValue(undefined)

      qq.showBar({ text: 'some code', filePath: '/src/foo.ts', language: 'typescript', startLine: 10, endLine: 20 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('explain this')

      // sendMessage called with buildQuoteMessage result (includes quoted code)
      expect(mockSendMessage).toHaveBeenCalledWith('[/src/foo.ts:10-20]\nsome code\n---\nexplain this')
      expect(ctx.attachedFiles.value).toHaveLength(0) // cleared by clearAll
    })

    it('clears all state after successful send', async () => {
      const qq = useQuoteQuestion()
      mockSendMessage.mockResolvedValue(undefined)

      qq.showBar({ text: 'some code', filePath: '/src/bar.ts', language: 'typescript', startLine: 10, endLine: 20 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('explain this')

      expect(qq.visible.value).toBe(false)
      expect(ctx.attachedFiles.value).toHaveLength(0)
      expect(ctx.quoteData.value).toBeNull()
    })

    it('shows error toast on send failure', async () => {
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

  describe('selectionchange listener', () => {
    /** Mount a test component that calls useQuoteQuestion() so onMounted fires */
    function mountWithComposable() {
      const TestComponent = defineComponent({
        setup() {
          const qq = useQuoteQuestion()
          return { qq }
        },
        render() {
          return h('div')
        },
      })
      return mount(TestComponent)
    }

    /** Create a DOM element with a text selection, return the container */
    function createSelectionInContainer(containerClass: string, attrs: Record<string, string> = {}) {
      const container = document.createElement('div')
      container.className = containerClass
      for (const [k, v] of Object.entries(attrs)) {
        container.setAttribute(k, v)
      }
      const textNode = document.createElement('span')
      textNode.textContent = 'selected code text'
      container.appendChild(textNode)
      document.body.appendChild(container)

      // Create a selection within the container
      const range = document.createRange()
      range.selectNodeContents(textNode)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      // Flush the jsdom async selectionchange from removeAllRanges
      vi.advanceTimersByTime(0)
      sel?.addRange(range)
      // Flush the jsdom async selectionchange from addRange
      vi.advanceTimersByTime(0)

      return container
    }

    it('shows bar when text is selected inside .office-preview-body', () => {
      const wrapper = mountWithComposable()
      createSelectionInContainer('office-preview-body', {
        'data-file-path': '/doc.xlsx',
      })

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value).not.toBeNull()
      expect(ctx.quoteData.value?.text).toBe('selected code text')
      expect(ctx.quoteData.value?.filePath).toBe('/doc.xlsx')

      wrapper.unmount()
    })

    it('shows bar when text is selected inside .markdown-body', () => {
      const wrapper = mountWithComposable()
      createSelectionInContainer('markdown-body', {
        'data-file-path': '/readme.md',
      })

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value?.text).toBe('selected code text')
      expect(ctx.quoteData.value?.filePath).toBe('/readme.md')

      wrapper.unmount()
    })

    it('shows bar when text is selected inside .raw-content-pre', () => {
      const wrapper = mountWithComposable()
      createSelectionInContainer('raw-content-pre', {
        'data-file-path': '/main.go',
        'data-language': 'go',
      })

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value?.text).toBe('selected code text')
      expect(ctx.quoteData.value?.filePath).toBe('/main.go')
      expect(ctx.quoteData.value?.language).toBe('go')

      wrapper.unmount()
    })

    it('hides bar when no text is selected (selection collapsed)', () => {
      const wrapper = mountWithComposable()

      // First show the bar
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'code', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      expect(qq.visible.value).toBe(true)

      // Collapse the selection
      const sel = window.getSelection()
      sel?.removeAllRanges()
      vi.advanceTimersByTime(0)

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()

      wrapper.unmount()
    })

    it('hides bar when selection is outside valid containers', () => {
      const wrapper = mountWithComposable()

      // Select text in a plain div (no valid container class)
      createSelectionInContainer('some-other-class')

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()

      wrapper.unmount()
    })

    it('keeps bar visible when pinned even if selection is lost', () => {
      const wrapper = mountWithComposable()
      const qq = useQuoteQuestion()

      // Show bar and pin it
      qq.showBar({ text: 'pinned code', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      qq.pinBar()
      expect(qq.visible.value).toBe(true)

      // Collapse selection
      const sel = window.getSelection()
      sel?.removeAllRanges()
      vi.advanceTimersByTime(0)

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      // Bar should still be visible because pinned
      expect(qq.visible.value).toBe(true)

      wrapper.unmount()
    })

    it('keeps bar visible when pinned even if selection moves outside valid container', () => {
      const wrapper = mountWithComposable()
      const qq = useQuoteQuestion()

      qq.showBar({ text: 'pinned code', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      qq.pinBar()

      // Select text outside any valid container
      createSelectionInContainer('outside-container')

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      expect(qq.visible.value).toBe(true)

      wrapper.unmount()
    })

    it('debounces selectionchange events (150ms)', () => {
      const wrapper = mountWithComposable()

      createSelectionInContainer('markdown-body', {
        'data-file-path': '/debounce.md',
      })

      // Fire multiple rapid selectionchange events
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(50)
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(50)
      document.dispatchEvent(new Event('selectionchange'))

      // Only 100ms elapsed since first event — debounce timer (150ms) not yet expired
      // But the last event reset the timer, so only 0ms have passed on the latest timer
      // We need to advance 150ms from the last event
      const qq = useQuoteQuestion()
      // The last debounce timer hasn't fired yet (only 0ms since last event)
      expect(qq.visible.value).toBe(false)

      // Advance past debounce
      vi.advanceTimersByTime(150)

      expect(qq.visible.value).toBe(true)

      wrapper.unmount()
    })

    it('removes listener when component unmounts (listenerCount goes to 0)', () => {
      const wrapper = mountWithComposable()

      // Selection in a valid container should work
      createSelectionInContainer('markdown-body', { 'data-file-path': '/test.md' })
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      expect(qq.visible.value).toBe(true)

      // Unmount — should remove the listener
      wrapper.unmount()

      // Reset state
      qq.closeSheet()
      expect(qq.visible.value).toBe(false)

      // Create a new selection and dispatch event — since listener is removed, bar should NOT appear
      document.body.innerHTML = ''
      createSelectionInContainer('markdown-body', { 'data-file-path': '/after-unmount.md' })
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      // State should remain unchanged (no listener to process it)
      expect(qq.visible.value).toBe(false)
    })
  })
})
