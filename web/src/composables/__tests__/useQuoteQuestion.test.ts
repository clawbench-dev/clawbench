import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

// Mock useSessionIdentity before importing the module under test.
//
// The module under test imports both the composable and the module-level
// accessors (getSessionId/getSessionTitle), so all three must be provided —
// a whitelist that omits one makes the whole file fail to load.
const mockSendMessage = vi.fn()
const mockSessionState = vi.hoisted(() => ({ sessionId: '', sessionTitle: '' }))
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: () => ({
    sendMessage: mockSendMessage,
  }),
  getSessionId: () => mockSessionState.sessionId,
  getSessionTitle: () => mockSessionState.sessionTitle,
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

// Mock useWideScreenLayout: force narrow-screen so selection-collapse tests
// exercise the narrow-mode quote-bar behavior. jsdom defaults to a 1024px-wide
// viewport, which the real singleton treats as wide-screen (isWideScreen=true),
// and the wide-screen branch hides the bar on selection collapse — the opposite
// of what these tests assert.
vi.mock('@/composables/useWideScreenLayout', () => ({
  useWideScreenLayout: () => ({ isWideScreen: { value: false } }),
}))

// Keep real quoteQuestionUtils for selectionchange tests (closestElement, getLineInfo, getFileInfo)

// Import the real useChatContext (not mocked) — it's a singleton
import { useChatContext } from '../useChatContext.ts'
import { consumePendingChatInput, _resetChatInputInjectionForTesting } from '@/utils/chatInputInjection.ts'
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
    _resetChatInputInjectionForTesting()
    vi.useFakeTimers()
  })

  afterEach(() => {
    // Clean up DOM first, then flush pending timers while fake timers are still
    // active. jsdom's removeAllRanges() schedules an async selectionchange via
    // setTimeout; draining it here keeps that event from leaking into the next
    // test as a real timer (which races the listener and flakes assertions).
    document.body.innerHTML = ''
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
    vi.runAllTimers()
    vi.useRealTimers()
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

  describe('hideBar', () => {
    it('hides the bar and clears quoteData immediately', () => {
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'hello', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      qq.pinBar()
      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value).not.toBeNull()

      qq.hideBar()
      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()
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

    it('sends the typed message and stages the quote as a card', async () => {
      const qq = useQuoteQuestion()
      // ChatPanelContent reads stagedQuotes synchronously before its first
      // await, so capture them at send time — clearAll() wipes them afterwards.
      let atSendTime: unknown[] = []
      mockSendMessage.mockImplementation(async () => {
        atSendTime = [...ctx.stagedQuotes.value]
      })

      qq.showBar({ text: 'some code', filePath: '/src/foo.ts', language: 'typescript', startLine: 10, endLine: 20 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('explain this')

      // The quote no longer rides in the message text — it becomes a structured
      // card (see ChatPanelContent.sendMessage, which materialises stagedQuotes
      // into files entries). Only the user's own words are the message.
      expect(mockSendMessage).toHaveBeenCalledWith('explain this')
      expect(atSendTime).toHaveLength(1)
      expect(atSendTime[0]).toMatchObject({
        text: 'some code', filePath: '/src/foo.ts', startLine: 10, endLine: 20,
      })
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
      expect(ctx.stagedQuotes.value).toHaveLength(0)
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

    it('stages the active selection alongside existing staged quotes', async () => {
      const qq = useQuoteQuestion()
      let atSendTime: Array<{ text: string; note: string }> = []
      mockSendMessage.mockImplementation(async () => {
        atSendTime = ctx.stagedQuotes.value.map(q => ({ text: q.text, note: q.note }))
      })
      ctx.addStagedQuote(
        { text: 'first()', filePath: '/first.ts', language: 'ts', startLine: 1, endLine: 2 },
        'Review this first',
      )
      ctx.setQuoteData({ text: 'second()', filePath: '/second.ts', language: 'ts', startLine: 8, endLine: 8 })

      await qq.sendMessage('Compare them')

      expect(mockSendMessage).toHaveBeenCalledWith('Compare them')
      // Both quotes are now cards (materialised by ChatPanelContent), not text.
      expect(atSendTime).toHaveLength(2)
      expect(atSendTime.map(q => q.text)).toEqual(['first()', 'second()'])
      expect(atSendTime[0].note).toBe('Review this first')
    })

    it('deduplicates the active selection against staged quotes', async () => {
      const qq = useQuoteQuestion()
      let atSendTime: Array<{ text: string; note: string }> = []
      mockSendMessage.mockImplementation(async () => {
        atSendTime = ctx.stagedQuotes.value.map(q => ({ text: q.text, note: q.note }))
      })
      const quote = { text: 'same()', filePath: '/same.ts', language: 'ts', startLine: 4, endLine: 4 }
      ctx.addStagedQuote(quote, 'Keep this note')
      ctx.setQuoteData({ ...quote })

      await qq.sendMessage('Explain')

      expect(mockSendMessage).toHaveBeenCalledWith('Explain')
      expect(atSendTime).toHaveLength(1)
      expect(atSendTime[0].note).toBe('Keep this note')
    })
  })

  describe('addToConversation', () => {
    it('stages an active quote without requiring text', () => {
      const qq = useQuoteQuestion()
      ctx.setQuoteData({ text: 'selected', filePath: '/a.ts', language: 'ts', startLine: 3, endLine: 4 })

      qq.addToConversation('')

      expect(ctx.stagedQuotes.value).toHaveLength(1)
      expect(ctx.stagedQuotes.value[0].note).toBe('')
      expect(ctx.quoteData.value).toBeNull()
      expect(qq.visible.value).toBe(false)
    })

    it('stores entered text as the staged quote note', () => {
      const qq = useQuoteQuestion()
      ctx.setQuoteData({ text: 'selected', filePath: '/a.ts', language: 'ts', startLine: 3, endLine: 4 })

      qq.addToConversation('  Why is this needed?  ')

      expect(ctx.stagedQuotes.value[0].note).toBe('Why is this needed?')
    })
  })

  describe('composer mode (opened from an entry point, no quote)', () => {
    it('opens with no quote and does NOT touch the chat attachments', () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      expect(qq.composerMode.value).toBe(true)
      expect(qq.visible.value).toBe(true)
      // Crucially NOT the full body: nothing is quoted until the user selects.
      expect(ctx.quoteData.value).toBeNull()
      // And crucially not in the chat input either: the attachment is only
      // previewed in the bar until the user commits. Attaching on open put the
      // chip in the main chat input the moment the button was clicked, and
      // dismissing the bar left it behind.
      expect(ctx.attachedFiles.value).toHaveLength(0)
    })

    it('previews the issue/PR URL as a chip in the bar', () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      expect(qq.composerAttachment.value).toEqual({
        kind: 'url', label: 'acme/widgets#7', url: 'https://github.com/acme/widgets/issues/7',
      })
    })

    it('previews a local file when the composer is opened with filePath', () => {
      // The file browser header opens the same composer, but its attachment is a
      // local file rather than an external URL.
      const qq = useQuoteQuestion()
      qq.openComposer({ filePath: '/proj/src/main.ts', label: 'main.ts' })

      expect(qq.composerMode.value).toBe(true)
      expect(qq.visible.value).toBe(true)
      expect(qq.composerAttachment.value).toEqual({
        kind: 'file', label: 'main.ts', path: '/proj/src/main.ts',
      })
      expect(ctx.attachedFiles.value).toHaveLength(0)
    })

    it('drops the previewed attachment when the composer is dismissed', () => {
      // Closing the bar must not leave anything in the chat input — that is the
      // regression this whole pending-attachment indirection exists to prevent.
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      qq.hideComposer()

      expect(qq.composerAttachment.value).toBeNull()
      expect(ctx.attachedFiles.value).toHaveLength(0)
    })

    it('ignores an open request with neither url nor filePath', () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ label: 'nothing' })

      expect(qq.composerMode.value).toBe(false)
      expect(qq.visible.value).toBe(false)
      expect(qq.composerAttachment.value).toBeNull()
    })

    it('stages the quoted issue as a card before sending', async () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      // The card must already be staged when the send runs — ChatPanelContent
      // reads stagedQuotes synchronously before its first await, so staging any
      // later would drop it. Afterwards the batch is cleared by the send
      // itself, so it is only observable here.
      let atSendTime: unknown[] = []
      mockSendMessage.mockImplementation(async () => {
        atSendTime = [...ctx.stagedQuotes.value]
      })

      await qq.sendMessage('why is this broken?')

      expect(mockSendMessage).toHaveBeenCalledWith('why is this broken?')
      expect(atSendTime).toHaveLength(1)
      expect(atSendTime[0]).toMatchObject({
        sourceKind: 'url', url: 'https://github.com/acme/widgets/issues/7', filePath: 'acme/widgets#7',
        // Whole-object quote: no content, the AI reads the referenced issue.
        text: '',
      })
      // A card, not a separate attachment chip.
      expect(ctx.attachedFiles.value).toHaveLength(0)
      // Consumed by the message, not left behind for the next one.
      expect(ctx.stagedQuotes.value).toHaveLength(0)
    })

    it('stages the quoted file as a card before sending', async () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ filePath: '/proj/src/main.ts', label: 'main.ts' })

      let atSendTime: unknown[] = []
      mockSendMessage.mockImplementation(async () => {
        atSendTime = [...ctx.stagedQuotes.value]
      })

      await qq.sendMessage('explain this')

      expect(atSendTime).toHaveLength(1)
      expect(atSendTime[0]).toMatchObject({
        filePath: '/proj/src/main.ts', sourceKind: 'file', text: '',
      })
      // Not a separate file attachment.
      expect(ctx.attachedFiles.value).toHaveLength(0)
    })

    it('does not attach anything just by opening twice', () => {
      const qq = useQuoteQuestion()
      const ctxArg = { url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' }
      qq.openComposer(ctxArg)
      qq.openComposer(ctxArg)

      expect(ctx.attachedFiles.value).toHaveLength(0)
      expect(qq.composerAttachment.value?.label).toBe('acme/widgets#7')
    })

    it('sends the user input with no quote at all', async () => {
      // Regression: sendMessage used to early-return when quoteData was null,
      // which made the no-selection path impossible.
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      await qq.sendMessage('why is this broken?')

      expect(mockSendMessage).toHaveBeenCalledWith('why is this broken?')
    })

    it('sends the user input and stages the quote as a card', async () => {
      const qq = useQuoteQuestion()
      let atSendTime: unknown[] = []
      mockSendMessage.mockImplementation(async () => {
        atSendTime = [...ctx.stagedQuotes.value]
      })
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })
      ctx.setQuoteData({ text: 'build failed', filePath: 'acme/widgets#7', language: 'issue', startLine: 0, endLine: 0 })

      await qq.sendMessage('why?')

      // The typed text is the message; the quote is a card.
      expect(mockSendMessage).toHaveBeenCalledWith('why?')
      expect(atSendTime).toHaveLength(1)
      expect((atSendTime[0] as { text: string }).text).toBe('build failed')
    })

    it('does nothing when there is neither a quote nor input', async () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      await qq.sendMessage('   ')

      expect(mockSendMessage).not.toHaveBeenCalled()
    })

    it('leaves composer mode after sending', async () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      await qq.sendMessage('hi')

      expect(qq.composerMode.value).toBe(false)
      expect(qq.visible.value).toBe(false)
    })

    it('keeps the bar open when the selection is cleared', () => {
      // The bar was opened deliberately, so losing the selection must not close
      // it — otherwise deselecting discards the snippet the user just picked.
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })
      ctx.setQuoteData({ text: 'picked', filePath: 'acme/widgets#7', language: 'issue', startLine: 0, endLine: 0 })

      document.dispatchEvent(new Event('selectionchange'))
      vi.runAllTimers()

      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value).not.toBeNull()
    })

    describe('add to conversation', () => {
      it('stages the quote with the typed note instead of injecting text', () => {
        const onAdd = vi.fn()
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7', onAdd })
        ctx.setQuoteData({ text: 'build failed', filePath: 'acme/widgets#7', language: 'issue', startLine: 0, endLine: 0 })

        qq.addToConversation('please fix')

        // The typed text becomes the quote's ANNOTATION — one card, exactly
        // like the file browser / forge selection flow. Nothing is injected
        // into the chat input.
        expect(consumePendingChatInput()).toBeNull()
        expect(ctx.stagedQuotes.value).toHaveLength(1)
        expect(ctx.stagedQuotes.value[0].note).toBe('please fix')
        expect(onAdd).toHaveBeenCalledTimes(1)
      })

      it('stages the quote with an empty note when nothing was typed', () => {
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })
        ctx.setQuoteData({ text: 'build failed', filePath: 'acme/widgets#7', language: 'issue', startLine: 0, endLine: 0 })

        qq.addToConversation('')

        expect(ctx.stagedQuotes.value).toHaveLength(1)
        expect(ctx.stagedQuotes.value[0].note).toBe('')
        expect(consumePendingChatInput()).toBeNull()
      })

      it('uses the typed text as the annotation even with no selection', () => {
        // The card always exists (it references the file/issue itself), so the
        // typed text always has somewhere to go — it is the annotation. Nothing
        // is injected into the chat input.
        const onAdd = vi.fn()
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7', onAdd })

        qq.addToConversation('please fix')

        expect(consumePendingChatInput()).toBeNull()
        expect(ctx.stagedQuotes.value).toHaveLength(1)
        expect(ctx.stagedQuotes.value[0].note).toBe('please fix')
        expect(ctx.stagedQuotes.value[0].text).toBe('')
        expect(onAdd).toHaveBeenCalledTimes(1)
      })

      it('injects nothing when there is no quote and no note, but still navigates', () => {
        const onAdd = vi.fn()
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7', onAdd })

        qq.addToConversation('')

        expect(consumePendingChatInput()).toBeNull()
        expect(onAdd).toHaveBeenCalledTimes(1)
      })

      it('stages the quoted URL as a card on add', () => {
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

        qq.addToConversation('')

        expect(ctx.stagedQuotes.value).toHaveLength(1)
        expect(ctx.stagedQuotes.value[0]).toMatchObject({
          sourceKind: 'url', url: 'https://github.com/acme/widgets/issues/7', filePath: 'acme/widgets#7', text: '',
        })
        // A card, not a separate attachment chip.
        expect(ctx.attachedFiles.value).toHaveLength(0)
        expect(qq.composerAttachment.value).toBeNull()
      })

      it('stages the quoted file as a card on add', () => {
        const qq = useQuoteQuestion()
        qq.openComposer({ filePath: '/proj/src/main.ts', label: 'main.ts' })

        qq.addToConversation('')

        expect(ctx.stagedQuotes.value).toHaveLength(1)
        expect(ctx.stagedQuotes.value[0]).toMatchObject({
          filePath: '/proj/src/main.ts', sourceKind: 'file', text: '',
        })
        expect(ctx.attachedFiles.value).toHaveLength(0)
      })
    })

    it('hideComposer closes without leaving composer mode latched', () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      qq.hideComposer()

      expect(qq.composerMode.value).toBe(false)
      expect(qq.visible.value).toBe(false)
    })

    it('hideBar does NOT tear down an entry-point composer', () => {
      // CodeMirror viewers call hideBar() whenever their internal selection
      // becomes empty — which happens the moment the user clicks into the bar or
      // types. Clearing composerContext there made the commit a no-op, so the
      // quote button silently did nothing in the file browser.
      const qq = useQuoteQuestion()
      qq.openComposer({ filePath: '/proj/src/main.ts', label: 'main.ts' })

      qq.hideBar()

      expect(qq.composerMode.value).toBe(true)
      expect(qq.visible.value).toBe(true)

      // And the commit still works afterwards.
      qq.addToConversation('explain')
      expect(ctx.stagedQuotes.value).toHaveLength(1)
      expect(ctx.stagedQuotes.value[0]).toMatchObject({
        filePath: '/proj/src/main.ts', note: 'explain',
      })
    })

    it('hideBar still hides a plain selection bar', () => {
      // The selection flow must keep its existing behaviour.
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'code', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 2 }, { delay: 0 })
      vi.advanceTimersByTime(10)
      expect(qq.visible.value).toBe(true)

      qq.hideBar()

      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()
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

    // Git history diffs became quotable so a selected hunk can be sent to the
    // AI. The container carries `data-quote-source` (the file name), so the
    // quote is labelled with the file rather than falling back to a generic
    // "selected text" label.
    it('shows bar for a selection inside a git diff, labelled with the file', () => {
      const wrapper = mountWithComposable()
      createSelectionInContainer('git-diff-scroll', {
        'data-quote-source': 'src/main.go',
        'data-quote-language': 'diff',
      })

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value?.filePath).toBe('src/main.go')
      expect(ctx.quoteData.value?.sourceKind).toBe('file')

      wrapper.unmount()
    })

    // Without a file name the diff is still quotable, but there is nothing to
    // open — so the kind must be 'selection', not the 'message' that inference
    // would produce for an entry with no path and no url.
    it('tags a diff selection with no file name as a free-form selection', () => {
      const wrapper = mountWithComposable()
      createSelectionInContainer('git-diff-scroll', { 'data-quote-source': '' })

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value?.sourceKind).toBe('selection')

      wrapper.unmount()
    })

    // The terminal cannot rely on the DOM selection (xterm sets
    // `user-select: none`, so the browser reports a COLLAPSED selection there).
    // It pushes its selection in and pins it; the collapsed shadow must not
    // tear the bar down, which is what the pin protects against.
    it('keeps a pinned terminal quote alive through a collapsed xterm selectionchange', () => {
      const wrapper = mountWithComposable()
      const term = document.createElement('div')
      term.className = 'xterm'
      const inner = document.createElement('span')
      inner.textContent = 'terminal output'
      term.appendChild(inner)
      document.body.appendChild(term)

      // What updateSelectionFromTerm does: pin, then show.
      const qq = useQuoteQuestion()
      qq.pinBar()
      qq.showBar({ text: 'terminal output', filePath: '', language: '', startLine: 0, endLine: 0, sourceKind: 'selection' }, { delay: 0 })
      vi.advanceTimersByTime(10)
      expect(qq.visible.value).toBe(true)

      // The browser's collapsed shadow selection inside the terminal.
      const sel = window.getSelection()
      sel?.removeAllRanges()
      vi.advanceTimersByTime(0)
      const range = document.createRange()
      range.selectNodeContents(inner)
      range.collapse(true)
      sel?.addRange(range)
      vi.advanceTimersByTime(0)

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      expect(qq.visible.value).toBe(true)
      expect(ctx.quoteData.value?.text).toBe('terminal output')

      wrapper.unmount()
    })

    // The pin is what protects the bar — NOT a container check. Without it a
    // collapsed selection would be read as "the user deselected" and hide the
    // bar. This guards the terminal integration's reliance on pinBar().
    it('hides an UNPINNED bar when a collapsed selection appears', () => {
      const wrapper = mountWithComposable()
      const qq = useQuoteQuestion()

      // A plain file-preview quote is never pinned.
      qq.showBar({ text: 'file code', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 2 }, { delay: 0 })
      vi.advanceTimersByTime(10)
      expect(qq.visible.value).toBe(true)

      const sel = window.getSelection()
      sel?.removeAllRanges()
      vi.advanceTimersByTime(0)

      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      expect(qq.visible.value).toBe(false)

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

    describe('chat messages', () => {      /**
       * Build a chat message row: `.chat-message[data-msg-key]` >
       * `.msg-card` > `.msg-content-wrapper` > text. Mirrors ChatMessageItem's
       * real structure, including the meta bar sibling that must NOT be
       * quotable.
       */
      function createChatMessage(msgKey: string | null) {
        const row = document.createElement('div')
        row.className = 'chat-message assistant'
        if (msgKey) row.setAttribute('data-msg-key', msgKey)

        const card = document.createElement('div')
        card.className = 'msg-card'
        const wrapper = document.createElement('div')
        wrapper.className = 'msg-content-wrapper'
        const textNode = document.createElement('span')
        textNode.textContent = 'the assistant reply'
        wrapper.appendChild(textNode)
        card.appendChild(wrapper)

        // Meta bar lives OUTSIDE the content wrapper — a selection here must not
        // be treated as message content.
        const metaBar = document.createElement('div')
        metaBar.className = 'chat-meta-bar'
        const metaText = document.createElement('span')
        metaText.textContent = '2m ago'
        metaBar.appendChild(metaText)

        row.appendChild(card)
        row.appendChild(metaBar)
        document.body.appendChild(row)
        return { row, textNode, metaText }
      }

      function selectNode(node: Node) {
        const range = document.createRange()
        range.selectNodeContents(node)
        const sel = window.getSelection()
        sel?.removeAllRanges()
        vi.advanceTimersByTime(0)
        sel?.addRange(range)
        vi.advanceTimersByTime(0)
      }

      it('shows the bar for a selection inside a chat message', () => {
        const wrapper = mountWithComposable()
        const { textNode } = createChatMessage('db-42')
        selectNode(textNode)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        const qq = useQuoteQuestion()
        expect(qq.visible.value).toBe(true)
        expect(ctx.quoteData.value?.text).toBe('the assistant reply')

        wrapper.unmount()
      })

      it('attributes the quote to the message via data-msg-key', () => {
        const wrapper = mountWithComposable()
        const { textNode } = createChatMessage('db-42')
        selectNode(textNode)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        expect(ctx.quoteData.value?.messageId).toBe(42)
        expect(ctx.quoteData.value?.sourceKind).toBe('message')
        // A chat quote has no file and no line info.
        expect(ctx.quoteData.value?.filePath).toBe('')
        expect(ctx.quoteData.value?.startLine).toBe(0)

        wrapper.unmount()
      })

      it('quotes an optimistic message without a message id', () => {
        // Local messages have no data-msg-key. The quote is still valid; it just
        // cannot be addressed later.
        const wrapper = mountWithComposable()
        const { textNode } = createChatMessage(null)
        selectNode(textNode)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        expect(useQuoteQuestion().visible.value).toBe(true)
        expect(ctx.quoteData.value?.messageId).toBeUndefined()

        wrapper.unmount()
      })

      it('does not quote the meta bar (timestamps, buttons)', () => {
        // The meta bar sits inside .chat-message but is chrome, not content.
        const wrapper = mountWithComposable()
        const { metaText } = createChatMessage('db-7')
        selectNode(metaText)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        expect(useQuoteQuestion().visible.value).toBe(false)
        expect(ctx.quoteData.value).toBeNull()

        wrapper.unmount()
      })

      it('does not quote a button inside a chat message', () => {
        const wrapper = mountWithComposable()
        const { row } = createChatMessage('db-7')
        const btn = document.createElement('button')
        btn.textContent = 'Copy'
        row.querySelector('.msg-content-wrapper')!.appendChild(btn)
        selectNode(btn)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        expect(useQuoteQuestion().visible.value).toBe(false)

        wrapper.unmount()
      })

      it('does not quote an attachment chip', () => {
        const wrapper = mountWithComposable()
        const { row } = createChatMessage('db-7')
        const chip = document.createElement('span')
        chip.className = 'chat-file-attachment'
        const chipText = document.createElement('span')
        chipText.textContent = 'a.go'
        chip.appendChild(chipText)
        row.querySelector('.msg-content-wrapper')!.appendChild(chip)
        selectNode(chipText)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        expect(useQuoteQuestion().visible.value).toBe(false)

        wrapper.unmount()
      })

      it('carries the forge url through so the quote can be opened', () => {
        const wrapper = mountWithComposable()
        const container = document.createElement('div')
        container.className = 'markdown-body'
        container.setAttribute('data-quote-source', 'acme/widgets#7')
        container.setAttribute('data-quote-language', 'issue')
        container.setAttribute('data-quote-url', 'https://github.com/acme/widgets/issues/7')
        const textNode = document.createElement('span')
        textNode.textContent = 'issue body'
        container.appendChild(textNode)
        document.body.appendChild(container)
        selectNode(textNode)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        expect(ctx.quoteData.value?.sourceKind).toBe('url')
        expect(ctx.quoteData.value?.url).toBe('https://github.com/acme/widgets/issues/7')

        wrapper.unmount()
      })

      it('still quotes a link selection in a FILE preview (chrome guard is chat-scoped)', () => {
        // The chrome exclusion matches `a`, which in a file preview is real
        // content. Scoping it to .chat-message keeps ordinary prose quotable.
        const wrapper = mountWithComposable()
        const container = document.createElement('div')
        container.className = 'markdown-body'
        container.setAttribute('data-file-path', '/docs/readme.md')
        const link = document.createElement('a')
        link.textContent = 'see the guide'
        container.appendChild(link)
        document.body.appendChild(container)
        selectNode(link)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        expect(useQuoteQuestion().visible.value).toBe(true)
        expect(ctx.quoteData.value?.text).toBe('see the guide')
        expect(ctx.quoteData.value?.filePath).toBe('/docs/readme.md')

        wrapper.unmount()
      })
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

    describe('pointer-drag guard', () => {
      // Track the mounted wrapper so it is always unmounted even when an
      // assertion fails mid-test (a leaked listener otherwise bleeds into the
      // next test and breaks listenerCount bookkeeping).
      let guardWrapper: ReturnType<typeof mountWithComposable> | null = null
      afterEach(() => {
        guardWrapper?.unmount()
        guardWrapper = null
      })

      it('does not show the bar while the pointer is still pressed (mid-drag)', () => {
        guardWrapper = mountWithComposable()
        createSelectionInContainer('markdown-body', { 'data-file-path': '/drag.md' })

        // User is still dragging: pointer down + selection built up so far.
        document.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, bubbles: true }))
        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        const qq = useQuoteQuestion()
        expect(qq.visible.value).toBe(false)
        expect(ctx.quoteData.value).toBeNull()

        // Release so the pointer count does not leak into other tests.
        document.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
      })

      it('suppresses a debounced evaluation that fires while the user pauses mid-drag', () => {
        guardWrapper = mountWithComposable()
        createSelectionInContainer('markdown-body', { 'data-file-path': '/drag.md' })

        document.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, bubbles: true }))
        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150) // debounce fires while dragging -> must be suppressed
        expect(useQuoteQuestion().visible.value).toBe(false)

        // Finishing the drag re-evaluates the final selection (deferred 120ms).
        document.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
        vi.advanceTimersByTime(120)
        expect(useQuoteQuestion().visible.value).toBe(true)
        expect(ctx.quoteData.value?.text).toBe('selected code text')
      })

      it('shows the bar after pointerup even without a trailing selectionchange', () => {
        guardWrapper = mountWithComposable()
        createSelectionInContainer('markdown-body', { 'data-file-path': '/drag.md' })

        document.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, bubbles: true }))
        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)
        expect(useQuoteQuestion().visible.value).toBe(false)

        document.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
        vi.advanceTimersByTime(120)

        expect(useQuoteQuestion().visible.value).toBe(true)
        expect(ctx.quoteData.value?.text).toBe('selected code text')
      })

      it('shows the bar on mobile when the selection finalizes just after pointerup', () => {
        // Touch: at pointerup the browser has not registered the selection yet,
        // so an immediate evaluate would hide the bar. The deferred evaluate must
        // wait and pick up the selection once it settles.
        guardWrapper = mountWithComposable()
        // Selection is empty when the pointer is released (still settling).
        document.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, bubbles: true }))
        document.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))

        // The final selection registers right after pointerup.
        createSelectionInContainer('markdown-body', { 'data-file-path': '/settle.md' })
        // The pointerup deferred evaluate may be rescheduled by the late
        // selectionchange; advance past the debounce so it runs.
        vi.advanceTimersByTime(200)

        expect(useQuoteQuestion().visible.value).toBe(true)
        expect(ctx.quoteData.value?.text).toBe('selected code text')
      })

      it('releases the guard on touchend when pointerup is swallowed (mobile)', () => {
        // Mobile native selection UI can swallow pointerup, leaving pointerCount
        // held. touchend still fires and must release the guard so the bar shows.
        guardWrapper = mountWithComposable()
        document.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, bubbles: true }))
        createSelectionInContainer('markdown-body', { 'data-file-path': '/touch.md' })
        // No pointerup — it is swallowed by the native selection UI.
        document.dispatchEvent(new Event('touchend'))
        vi.advanceTimersByTime(120)

        expect(useQuoteQuestion().visible.value).toBe(true)
        expect(ctx.quoteData.value?.text).toBe('selected code text')
      })

      it('self-heals the guard when neither pointerup nor touchend fires', () => {
        // Worst case: both release events are swallowed. The 700ms safety timer
        // must drop the guard so a settling selection still surfaces the bar.
        guardWrapper = mountWithComposable()
        document.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, bubbles: true }))
        createSelectionInContainer('markdown-body', { 'data-file-path': '/stale.md' })
        // Advance past the safety timer (no release event fired).
        vi.advanceTimersByTime(800)

        expect(useQuoteQuestion().visible.value).toBe(true)
        expect(ctx.quoteData.value?.text).toBe('selected code text')
      })
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
