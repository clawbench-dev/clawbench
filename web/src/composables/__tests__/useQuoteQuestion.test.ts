import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'

// Mock useSessionIdentity before importing the module under test.
//
// The module under test imports both the composable and the module-level
// accessors (getSessionId/getSessionTitle), so all three must be provided —
// a whitelist that omits one makes the whole file fail to load.
const mockSendMessage = vi.fn()
const mockSessionState = vi.hoisted(() => ({ sessionId: '', sessionTitle: '' }))
// A real ref, not a bare `{ value }`: the composable watches it to drop the
// native-menu snapshot on session change, and a plain object would not be
// reactive (the watcher would never fire).
const mockRefs = vi.hoisted(() => ({ currentSessionId: null as unknown as { value: string } }))
vi.mock('@/composables/useSessionIdentity', async () => {
  const { ref } = await import('vue')
  const currentSessionId = ref('')
  mockRefs.currentSessionId = currentSessionId
  return {
    useSessionIdentity: () => ({
      sendMessage: mockSendMessage,
      currentSessionId,
    }),
    getSessionId: () => mockSessionState.sessionId,
    getSessionTitle: () => mockSessionState.sessionTitle,
  }
})

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
const mockLayout = vi.hoisted(() => ({ isWideScreen: false, chatCollapsed: false }))
vi.mock('@/composables/useWideScreenLayout', () => ({
  useWideScreenLayout: () => ({
    isWideScreen: { value: mockLayout.isWideScreen },
    chatCollapsed: { value: mockLayout.chatCollapsed },
  }),
  isChatPanelVisible: () => true,
}))

// The "搜索文件" action opens the SAME drawer a dashed (verified-missing) path
// opens. Mock it so the assertion is on the PATH HANDED OVER — the contract with
// the dashed-path flow — rather than on that drawer's own rendering.
const mockOpenInertPathPicker = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useInertPathPicker.ts', () => ({
  openInertPathPicker: mockOpenInertPathPicker,
  deriveSearchQuery: (p: string | null | undefined) => (p ?? '').trim(),
}))

// Keep real quoteQuestionUtils for selectionchange tests (closestElement, getLineInfo, getFileInfo)

// Import the real useChatContext (not mocked) — it's a singleton
import { useChatContext } from '../useChatContext.ts'
import { consumePendingChatInput, _resetChatInputInjectionForTesting } from '@/utils/chatInputInjection.ts'
import { useQuoteQuestion } from '../useQuoteQuestion.ts'
import type { QuoteData } from '../useChatContext.ts'
import { cancelTarget, usePendingTarget } from '../useConversationTarget.ts'
import { _setPlatformForTest, _resetPlatformForTest } from '@/composables/usePlatformDetect'

describe('useQuoteQuestion', () => {
  let ctx: ReturnType<typeof useChatContext>

  beforeEach(() => {
    ctx = useChatContext()
    cancelTarget()
    // Fully reset module-level singleton state via closeSheet
    const qq = useQuoteQuestion()
    qq.closeSheet()
    qq.sheetOpen.value = false
    ctx.clearAll()

    mockSendMessage.mockReset()
    mockToastShow.mockReset()
    // Reset explicitly: it is a hoisted mock, so `vi.clearAllMocks` in another
    // suite does not necessarily cover it, and a stale call from a previous
    // test would satisfy a `toHaveBeenCalledWith` for the wrong argument.
    mockOpenInertPathPicker.mockClear()
    _resetChatInputInjectionForTesting()
    // Reset the platform axes so an Android test cannot leak into the next one.
    _resetPlatformForTest()
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

  describe('scroll dismissal (issue #544)', () => {
    function mountWithComposable() {
      const TestComponent = defineComponent({
        setup() { useQuoteQuestion(); return () => h('div') },
      })
      return mount(TestComponent)
    }

    it('hides the floating bar when the page scrolls', () => {
      const wrapper = mountWithComposable()
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'hello', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      expect(qq.visible.value).toBe(true)

      // Capture-phase document listener catches scrolls on any inner container.
      document.dispatchEvent(new Event('scroll'))
      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()

      wrapper.unmount()
    })

    it('keeps the bar while it is pinned (user is typing a comment)', () => {
      const wrapper = mountWithComposable()
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'hello', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      qq.pinBar()

      document.dispatchEvent(new Event('scroll'))
      expect(qq.visible.value).toBe(true)

      wrapper.unmount()
    })

    it('ignores a scroll that originates inside the bar itself', () => {
      const wrapper = mountWithComposable()
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'hello', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 })
      vi.advanceTimersByTime(400)
      expect(qq.visible.value).toBe(true)

      // The textarea's own scrollbar must not dismiss the bar.
      const bar = document.createElement('div')
      bar.className = 'quote-question-bar'
      const inner = document.createElement('textarea')
      bar.appendChild(inner)
      document.body.appendChild(bar)
      inner.dispatchEvent(new Event('scroll', { bubbles: true }))
      expect(qq.visible.value).toBe(true)

      wrapper.unmount()
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

    // ── The picker owns the destination ──
    // A send never goes out from here: it is snapshotted for the picker, and
    // confirmTarget performs it for whichever session the user picks.

    it('holds the send for the picker instead of sending', async () => {
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'some code', filePath: '/src/foo.ts', language: 'typescript', startLine: 10, endLine: 20 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('explain this')

      // Nothing was sent — the user has not picked a destination yet.
      expect(mockSendMessage).not.toHaveBeenCalled()
      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value?.mode).toBe('send')
      expect(pending.value?.text).toBe('explain this')
      expect(pending.value?.quotes[0].filePath).toBe('/src/foo.ts')
      // Nothing leaks into the current session's input.
      expect(ctx.stagedQuotes.value).toHaveLength(0)
      expect(qq.visible.value).toBe(false)
    })

    it('does not send even when the chat panel is on screen', async () => {
      // The picker is unconditional — there is no "panel visible ⇒ current
      // session" shortcut any more, so the send is still held.
      mockLayout.isWideScreen = false
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'code', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 1 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('hi')

      expect(mockSendMessage).not.toHaveBeenCalled()
      expect(usePendingTarget().pickerOpen.value).toBe(true)
    })

    it('carries the message text and the quote together in the pending request', async () => {
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'some code', filePath: '/src/foo.ts', language: 'typescript', startLine: 10, endLine: 20 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('explain this')

      const { pending } = usePendingTarget()
      expect(pending.value?.text).toBe('explain this')
      expect(pending.value?.quotes).toHaveLength(1)
      expect(pending.value?.quotes[0]).toMatchObject({
        text: 'some code', filePath: '/src/foo.ts', startLine: 10, endLine: 20,
      })
    })

    it('dismisses the bar and clears the active quote so it cannot float above the picker', async () => {
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'some code', filePath: '/src/bar.ts', language: 'typescript', startLine: 10, endLine: 20 })
      vi.advanceTimersByTime(400)

      await qq.sendMessage('explain this')

      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()
      // The payload lives in the pending request, not the live draft.
      expect(ctx.stagedQuotes.value).toHaveLength(0)
    })

    it('snapshots only the active selection, not previously staged quotes', async () => {
      // A staged quote belongs to the CURRENT session's draft. The send's
      // destination is unknown, so only the active selection rides along.
      const qq = useQuoteQuestion()
      ctx.addStagedQuote(
        { text: 'first()', filePath: '/first.ts', language: 'ts', startLine: 1, endLine: 2 },
        'Review this first',
      )
      ctx.setQuoteData({ text: 'second()', filePath: '/second.ts', language: 'ts', startLine: 8, endLine: 8 })

      await qq.sendMessage('Compare them')

      const { pending } = usePendingTarget()
      expect(pending.value?.quotes.map(q => q.text)).toEqual(['second()'])
    })
  })

  describe('addToConversation', () => {
    it('opens the picker and stages nothing locally', () => {
      const qq = useQuoteQuestion()
      ctx.setQuoteData({ text: 'selected', filePath: '/a.ts', language: 'ts', startLine: 3, endLine: 4 })

      qq.addToConversation('')

      // Nothing staged into the live input: the card belongs to a session the
      // user has not chosen yet.
      expect(ctx.stagedQuotes.value).toHaveLength(0)
      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value?.mode).toBe('add')
      expect(pending.value?.quotes).toHaveLength(1)
      expect(pending.value?.quotes[0].filePath).toBe('/a.ts')
      expect(pending.value?.quotes[0].note).toBe('')
      // The bar is dismissed so it cannot float above the dialog.
      expect(qq.visible.value).toBe(false)
      expect(ctx.quoteData.value).toBeNull()
    })

    it('stores entered text as the pending quote note', () => {
      const qq = useQuoteQuestion()
      ctx.setQuoteData({ text: 'selected', filePath: '/a.ts', language: 'ts', startLine: 3, endLine: 4 })

      qq.addToConversation('  Why is this needed?  ')

      expect(usePendingTarget().pending.value?.quotes[0].note).toBe('Why is this needed?')
    })

    it('opens the picker even with the chat panel visible', () => {
      mockLayout.isWideScreen = false
      const qq = useQuoteQuestion()
      ctx.setQuoteData({ text: 'selected', filePath: '/a.ts', language: 'ts', startLine: 3, endLine: 4 })

      qq.addToConversation('')

      expect(usePendingTarget().pickerOpen.value).toBe(true)
      expect(ctx.stagedQuotes.value).toHaveLength(0)
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

    it('carries the whole-object issue card in the pending send', async () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      await qq.sendMessage('why is this broken?')

      const { pending } = usePendingTarget()
      expect(pending.value?.text).toBe('why is this broken?')
      expect(pending.value?.quotes[0]).toMatchObject({
        sourceKind: 'url', url: 'https://github.com/acme/widgets/issues/7', filePath: 'acme/widgets#7',
        // Whole-object quote: no content, the AI reads the referenced issue.
        text: '',
      })
      // A card, not a separate attachment chip.
      expect(ctx.attachedFiles.value).toHaveLength(0)
      // Nothing staged locally — the picker owns delivery.
      expect(ctx.stagedQuotes.value).toHaveLength(0)
    })

    it('carries the quoted file as a card in the pending send', async () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ filePath: '/proj/src/main.ts', label: 'main.ts' })

      await qq.sendMessage('explain this')

      const { pending } = usePendingTarget()
      expect(pending.value?.quotes[0]).toMatchObject({
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
      // which made the no-selection path impossible. The send is now held for
      // the picker, but the payload must still carry the typed text.
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      await qq.sendMessage('why is this broken?')

      expect(mockSendMessage).not.toHaveBeenCalled()
      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value?.mode).toBe('send')
      expect(pending.value?.text).toBe('why is this broken?')
      // The composer card references the URL even with no selection.
      expect(pending.value?.quotes).toHaveLength(1)
      expect(pending.value?.quotes[0]).toMatchObject({
        sourceKind: 'url', url: 'https://github.com/acme/widgets/issues/7',
      })
    })

    it('carries a selected snippet as the pending quote', async () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })
      ctx.setQuoteData({ text: 'build failed', filePath: 'acme/widgets#7', language: 'issue', startLine: 0, endLine: 0 })

      await qq.sendMessage('why?')

      const { pending } = usePendingTarget()
      expect(pending.value?.text).toBe('why?')
      expect(pending.value?.quotes[0].text).toBe('build failed')
    })

    it('does nothing when there is neither a quote nor input', async () => {
      const qq = useQuoteQuestion()
      qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

      await qq.sendMessage('   ')

      expect(mockSendMessage).not.toHaveBeenCalled()
      expect(usePendingTarget().pickerOpen.value).toBe(false)
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
      it('opens the picker with the typed note as the quote annotation', () => {
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })
        ctx.setQuoteData({ text: 'build failed', filePath: 'acme/widgets#7', language: 'issue', startLine: 0, endLine: 0 })

        qq.addToConversation('please fix')

        // The typed text becomes the quote's ANNOTATION — one card, exactly
        // like the file browser / forge selection flow. Nothing is injected
        // into the chat input, and nothing is staged locally (the picker owns
        // the destination).
        expect(consumePendingChatInput()).toBeNull()
        expect(ctx.stagedQuotes.value).toHaveLength(0)
        const { pickerOpen, pending } = usePendingTarget()
        expect(pickerOpen.value).toBe(true)
        expect(pending.value?.quotes).toHaveLength(1)
        expect(pending.value?.quotes[0].note).toBe('please fix')
      })

      it('opens the picker with an empty note when nothing was typed', () => {
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })
        ctx.setQuoteData({ text: 'build failed', filePath: 'acme/widgets#7', language: 'issue', startLine: 0, endLine: 0 })

        qq.addToConversation('')

        expect(usePendingTarget().pending.value?.quotes[0].note).toBe('')
        expect(consumePendingChatInput()).toBeNull()
      })

      it('builds the card even with no selection (the object itself is quoted)', () => {
        // The card always exists (it references the file/issue itself), so the
        // typed text always has somewhere to go — it is the annotation.
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

        qq.addToConversation('please fix')

        expect(consumePendingChatInput()).toBeNull()
        const { pending } = usePendingTarget()
        expect(pending.value?.quotes).toHaveLength(1)
        expect(pending.value?.quotes[0].note).toBe('please fix')
        expect(pending.value?.quotes[0].text).toBe('')
      })

      it('opens the picker with no quotes when there is no selection and no note', () => {
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

        qq.addToConversation('')

        expect(consumePendingChatInput()).toBeNull()
        // No selection → buildComposerQuote still returns the whole-object card.
        expect(usePendingTarget().pickerOpen.value).toBe(true)
        expect(usePendingTarget().pending.value?.quotes[0].text).toBe('')
      })

      it('builds the quoted URL as a card in the pending request', () => {
        const qq = useQuoteQuestion()
        qq.openComposer({ url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7' })

        qq.addToConversation('')

        const { pending } = usePendingTarget()
        expect(pending.value?.quotes[0]).toMatchObject({
          sourceKind: 'url', url: 'https://github.com/acme/widgets/issues/7', filePath: 'acme/widgets#7', text: '',
        })
        // A card, not a separate attachment chip.
        expect(ctx.attachedFiles.value).toHaveLength(0)
        expect(qq.composerAttachment.value).toBeNull()
      })

      it('builds the quoted file as a card in the pending request', () => {
        const qq = useQuoteQuestion()
        qq.openComposer({ filePath: '/proj/src/main.ts', label: 'main.ts' })

        qq.addToConversation('')

        const { pending } = usePendingTarget()
        expect(pending.value?.quotes[0]).toMatchObject({
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
      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value?.quotes[0]).toMatchObject({
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
        // A chat quote carries no line info.
        expect(ctx.quoteData.value?.startLine).toBe(0)

        wrapper.unmount()
      })

      // The label is the session title — that is what the user recognises — and
      // the session id travels SEPARATELY. Without the id the quote cannot be
      // reopened (a message id is only unique within its session).
      it('labels the quote with the session title and carries the session id', () => {
        mockSessionState.sessionId = 'f033de62-5b46-4f57-ab7b-99c527d37ab2'
        mockSessionState.sessionTitle = '在界面点创建定时任务按钮的时候'
        const wrapper = mountWithComposable()
        const { textNode } = createChatMessage('db-42')
        selectNode(textNode)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        expect(ctx.quoteData.value?.filePath).toBe('在界面点创建定时任务按钮的时候')
        expect(ctx.quoteData.value?.sessionId).toBe('f033de62-5b46-4f57-ab7b-99c527d37ab2')
        // The id must NOT be folded into the label: the card shows the title,
        // and the id is what makes it addressable.
        expect(ctx.quoteData.value?.filePath).not.toContain('f033de62')

        wrapper.unmount()
      })

      // A session with no title yet must not render a blank card.
      it('falls back to the session id when the session has no title', () => {
        mockSessionState.sessionId = 'sess-abc'
        mockSessionState.sessionTitle = ''
        const wrapper = mountWithComposable()
        const { textNode } = createChatMessage('db-42')
        selectNode(textNode)

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        expect(ctx.quoteData.value?.filePath).toBe('sess-abc')
        expect(ctx.quoteData.value?.sessionId).toBe('sess-abc')

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

    /**
     * anchorRect drives the floating toolbar's position (issue #544). jsdom has
     * no layout, so getClientRects() returns an empty list for a real range —
     * we stub it to prove the composable captures the LAST line box (the point
     * the pointer left) and clears it again on every hide path.
     */
    describe('anchorRect (floating toolbar position)', () => {
      function stubRects(rects: DOMRect[]) {
        const proto = Range.prototype as unknown as { getClientRects: () => DOMRectList }
        // Indexable like a real DOMRectList (the composable reads
        // `rects[rects.length - 1]`), not just array-like via `item()`.
        const list = {
          length: rects.length,
          item: (i: number) => rects[i] ?? null,
          ...Object.fromEntries(rects.map((r, i) => [i, r])),
        }
        vi.spyOn(proto, 'getClientRects').mockReturnValue(list as unknown as DOMRectList)
      }

      function fakeRect(top: number): DOMRect {
        return { top, bottom: top + 20, left: 100, right: 200, width: 100, height: 20, x: 100, y: top, toJSON: () => ({}) } as DOMRect
      }

      afterEach(() => {
        vi.restoreAllMocks()
      })

      it('captures the last line box of the selection', () => {
        const wrapper = mountWithComposable()
        // Two line boxes; the toolbar anchors to the LAST (bottom) one.
        stubRects([fakeRect(100), fakeRect(140)])
        createSelectionInContainer('markdown-body', { 'data-file-path': '/a.md' })

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        const qq = useQuoteQuestion()
        expect(qq.visible.value).toBe(true)
        expect(qq.anchorRect.value?.top).toBe(140)

        wrapper.unmount()
      })

      it('is cleared when the bar is hidden', () => {
        const wrapper = mountWithComposable()
        stubRects([fakeRect(100)])
        createSelectionInContainer('markdown-body', { 'data-file-path': '/a.md' })
        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        const qq = useQuoteQuestion()
        expect(qq.anchorRect.value).not.toBeNull()

        qq.hideBar()
        expect(qq.anchorRect.value).toBeNull()

        wrapper.unmount()
      })

      it('is cleared by closeSheet', () => {
        const wrapper = mountWithComposable()
        stubRects([fakeRect(100)])
        createSelectionInContainer('markdown-body', { 'data-file-path': '/a.md' })
        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        const qq = useQuoteQuestion()
        expect(qq.anchorRect.value).not.toBeNull()

        qq.closeSheet()
        expect(qq.anchorRect.value).toBeNull()

        wrapper.unmount()
      })

      it('stays null when the selection has no laid-out boxes', () => {
        // getClientRects() empty (e.g. a CodeMirror/terminal shadow selection
        // that the DOM cannot measure) → no anchor, so the bar keeps its pinned
        // top position instead of floating to a bogus spot.
        const wrapper = mountWithComposable()
        stubRects([])
        createSelectionInContainer('markdown-body', { 'data-file-path': '/a.md' })

        document.dispatchEvent(new Event('selectionchange'))
        vi.advanceTimersByTime(150)

        const qq = useQuoteQuestion()
        expect(qq.visible.value).toBe(true)
        expect(qq.anchorRect.value).toBeNull()

        wrapper.unmount()
      })
    })
  })

  /**
   * The Android native text-selection menu (ClawBenchWebView.java) calls
   * window.__clawbenchSelectionAction('quote'|'comment'), which App.vue routes
   * to handleNativeSelectionAction. The menu's action mode has already
   * dismissed by then, so the DOM selection is normally GONE — the snapshot
   * taken while it was live is the reliable source.
   */
  describe('handleNativeSelectionAction (Android native menu)', () => {
    function mountWithComposable() {
      const TestComponent = defineComponent({
        setup() { useQuoteQuestion(); return () => h('div') },
      })
      return mount(TestComponent)
    }

    /** Create a quotable DOM selection in a markdown body. */
    function selectInMarkdown(text = 'selected code text') {
      const container = document.createElement('div')
      container.className = 'markdown-body'
      container.setAttribute('data-file-path', '/a.md')
      const span = document.createElement('span')
      span.textContent = text
      container.appendChild(span)
      document.body.appendChild(container)

      const range = document.createRange()
      range.selectNodeContents(span)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      vi.advanceTimersByTime(0)
      sel?.addRange(range)
      vi.advanceTimersByTime(0)
      return container
    }

    it('quote hands the selection to the session picker', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown()
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      qq.handleNativeSelectionAction('quote')

      // addToConversation('') → the picker holds the card; nothing is staged
      // locally (the destination is the user's choice).
      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value?.quotes).toHaveLength(1)
      expect(pending.value?.quotes[0].text).toBe('selected code text')
      expect(ctx.stagedQuotes.value).toHaveLength(0)

      wrapper.unmount()
    })

    it('comment surfaces the bar and asks it to open the input', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown()
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      const before = qq.expandRequest.value
      qq.handleNativeSelectionAction('comment')

      expect(qq.visible.value).toBe(true)
      expect(qq.expandRequest.value).toBe(before + 1)

      wrapper.unmount()
    })

    it('falls back to the snapshot when the selection is already gone', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown()
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      // The native action mode finished → the WebView cleared the selection
      // before calling into JS. This is the normal Android sequence.
      window.getSelection()?.removeAllRanges()
      vi.advanceTimersByTime(0)

      const qq = useQuoteQuestion()
      qq.handleNativeSelectionAction('quote')

      const { pending } = usePendingTarget()
      expect(pending.value?.quotes).toHaveLength(1)
      expect(pending.value?.quotes[0].text).toBe('selected code text')

      wrapper.unmount()
    })

    it('does nothing when there is neither a selection nor a snapshot', () => {
      const wrapper = mountWithComposable()
      const qq = useQuoteQuestion()

      qq.handleNativeSelectionAction('quote')

      expect(ctx.stagedQuotes.value).toHaveLength(0)
      expect(usePendingTarget().pickerOpen.value).toBe(false)
      expect(qq.visible.value).toBe(false)

      wrapper.unmount()
    })

    // ── search-file: reveal the selected text as a path ──

    it('search-file opens the same drawer a dashed path opens', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown('src/utils/foo.ts')
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      qq.handleNativeSelectionAction('search-file')
      expect(mockOpenInertPathPicker).toHaveBeenCalledWith({ path: 'src/utils/foo.ts' })

      wrapper.unmount()
    })

    /**
     * The point of the entry: it works on an ARBITRARY selected string, not
     * only on text the markdown renderer happened to annotate. Nothing in this
     * fixture carries a path annotation.
     */
    it('search-file works on unannotated text', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown('not/a/real/path/but/still/selectable.txt')
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      qq.handleNativeSelectionAction('search-file')
      expect(mockOpenInertPathPicker)
        .toHaveBeenCalledWith({ path: 'not/a/real/path/but/still/selectable.txt' })

      wrapper.unmount()
    })

    /**
     * search-file navigates; it must NOT fall through into the QUOTE flow.
     *
     * A missing `return` is only observable through the comment branch: the
     * code after our block adopts the selection as the active quote and then
     * bumps `expandRequest` to open the bar's comment input. (It does not reach
     * the picker — `action === 'quote'` is false, so the quote commit is
     * skipped.) Asserting the picker alone would therefore be a tautology that
     * passes with or without the `return`.
     */
    it('search-file does not open the comment composer', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown('src/a.ts')
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      const before = qq.expandRequest.value

      qq.handleNativeSelectionAction('search-file')
      expect(mockOpenInertPathPicker).toHaveBeenCalled()

      expect(qq.expandRequest.value,
        'falling through would open the comment input').toBe(before)
      expect(usePendingTarget().pickerOpen.value).toBe(false)
      expect(ctx.stagedQuotes.value).toHaveLength(0)

      wrapper.unmount()
    })

    /**
     * A selection with prose around the path must search for the PATH, not for
     * the first word: "see" would match hundreds of files.
     */
    it('search-file picks the path-like token, not the first word', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown('see src/a.ts for details')
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      qq.handleNativeSelectionAction('search-file')
      expect(mockOpenInertPathPicker).toHaveBeenCalledWith({ path: 'src/a.ts' })

      wrapper.unmount()
    })

    /** A bare filename is still a legitimate query when nothing looks like a path. */
    it('search-file falls back to the only token when nothing looks like a path', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown('config.yaml')
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      qq.handleNativeSelectionAction('search-file')
      expect(mockOpenInertPathPicker).toHaveBeenCalledWith({ path: 'config.yaml' })

      wrapper.unmount()
    })

    /** Backticks/quotes a selection carries are stripped before searching. */
    it('search-file strips wrapping quotes and backticks', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown('`lib/x.ts`')
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      qq.handleNativeSelectionAction('search-file')
      expect(mockOpenInertPathPicker).toHaveBeenCalledWith({ path: 'lib/x.ts' })

      wrapper.unmount()
    })

    it('search-file falls back to the snapshot when the selection is gone', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown('lib/snapshot.ts')
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      // The native action mode cleared the DOM selection before calling in.
      window.getSelection()?.removeAllRanges()
      vi.advanceTimersByTime(0)

      const qq = useQuoteQuestion()
      qq.handleNativeSelectionAction('search-file')
      expect(mockOpenInertPathPicker).toHaveBeenCalledWith({ path: 'lib/snapshot.ts' })

      wrapper.unmount()
    })

    it('ignores an unknown action', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown()
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      const before = qq.expandRequest.value
      qq.handleNativeSelectionAction('delete-everything')

      expect(ctx.stagedQuotes.value).toHaveLength(0)
      expect(qq.expandRequest.value).toBe(before)

      wrapper.unmount()
    })

    it('does not quote a stale selection from a non-quotable region', () => {
      const wrapper = mountWithComposable()
      // A selection in chrome (not a whitelisted container) is never captured,
      // so the snapshot stays empty and the action is a no-op.
      const container = document.createElement('div')
      container.className = 'some-chrome'
      const span = document.createElement('span')
      span.textContent = 'not content'
      container.appendChild(span)
      document.body.appendChild(container)
      const range = document.createRange()
      range.selectNodeContents(span)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      vi.advanceTimersByTime(0)
      sel?.addRange(range)
      vi.advanceTimersByTime(0)
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      qq.handleNativeSelectionAction('quote')

      expect(ctx.stagedQuotes.value).toHaveLength(0)

      wrapper.unmount()
    })

    /**
     * The snapshot must be dropped on an explicit dismissal/commit — otherwise
     * a later native action would quote text the user already dealt with. (It
     * must NOT be dropped on plain selection-collapse, which is what the
     * "falls back to the snapshot" test above covers.)
     */
    it('is cleared by closeSheet so a later action cannot re-quote it', () => {
      const wrapper = mountWithComposable()
      selectInMarkdown()
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      qq.closeSheet()

      window.getSelection()?.removeAllRanges()
      vi.advanceTimersByTime(0)
      qq.handleNativeSelectionAction('quote')

      expect(ctx.stagedQuotes.value).toHaveLength(0)

      wrapper.unmount()
    })

    /**
     * On Android the bar never becomes visible, so none of the dismissal paths
     * run when the user simply switches sessions — the snapshot has to be
     * dropped by the session watcher or the next 引用 would quote the previous
     * session's text.
     */
    it('is cleared when the session changes', async () => {
      const wrapper = mountWithComposable()
      selectInMarkdown()
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      // Switch session: the watcher on currentSessionId fires.
      mockRefs.currentSessionId.value = 'other-session'
      await nextTick()

      const qq = useQuoteQuestion()
      window.getSelection()?.removeAllRanges()
      vi.advanceTimersByTime(0)
      qq.handleNativeSelectionAction('quote')

      expect(ctx.stagedQuotes.value).toHaveLength(0)

      mockRefs.currentSessionId.value = ''
      wrapper.unmount()
    })
  })

  /**
   * On Android the system draws its OWN selection menu, so the in-page bar
   * would be a duplicate — and it renders UNDER that system window anyway.
   * Only the DOM-selection path is suppressed; terminal / CodeMirror /
   * OpenAPI-iframe selections have no native menu and must keep the bar.
   */
  describe('Android suppresses the in-page bar for DOM selections', () => {
    function mountWithComposable() {
      const TestComponent = defineComponent({
        setup() { useQuoteQuestion(); return () => h('div') },
      })
      return mount(TestComponent)
    }

    function selectInMarkdown() {
      const container = document.createElement('div')
      container.className = 'markdown-body'
      container.setAttribute('data-file-path', '/a.md')
      const span = document.createElement('span')
      span.textContent = 'selected code text'
      container.appendChild(span)
      document.body.appendChild(container)
      const range = document.createRange()
      range.selectNodeContents(span)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      vi.advanceTimersByTime(0)
      sel?.addRange(range)
      vi.advanceTimersByTime(0)
    }

    it('hides the bar but still records the quote for the native menu', () => {
      _setPlatformForTest({ isAndroidApp: true })
      const wrapper = mountWithComposable()
      selectInMarkdown()
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      const qq = useQuoteQuestion()
      expect(qq.visible.value).toBe(false)
      // The native menu needs the data even though the bar is hidden.
      expect(ctx.quoteData.value?.text).toBe('selected code text')

      // And the native action works off it.
      qq.handleNativeSelectionAction('quote')
      expect(usePendingTarget().pending.value?.quotes[0].text).toBe('selected code text')

      wrapper.unmount()
    })

    it('shows the bar on web (regression guard for the Android branch)', () => {
      _setPlatformForTest({ isAndroidApp: false })
      const wrapper = mountWithComposable()
      selectInMarkdown()
      document.dispatchEvent(new Event('selectionchange'))
      vi.advanceTimersByTime(150)

      expect(useQuoteQuestion().visible.value).toBe(true)

      wrapper.unmount()
    })

    it('still shows the bar on Android for a programmatic showBar (terminal/CodeMirror)', () => {
      // Terminal xterm / CodeMirror / OpenAPI iframe selections reach the bar
      // via showBar(), not evaluateSelection — there is no native menu for
      // them, so suppressing would remove their only entry point.
      _setPlatformForTest({ isAndroidApp: true })
      const wrapper = mountWithComposable()
      const qq = useQuoteQuestion()
      qq.showBar({ text: 'term text', filePath: '/proj', language: '', startLine: 0, endLine: 0 }, { delay: 0 })
      vi.advanceTimersByTime(0)

      expect(qq.visible.value).toBe(true)

      wrapper.unmount()
    })
  })
})
