import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import type { FileEntry } from '@/utils/fileAttachmentUtils'

// ── Mocks ──────────────────────────────────────────────────────

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const { mockLayout } = vi.hoisted(() => ({
  mockLayout: { isWideScreen: false, chatCollapsed: false },
}))
vi.mock('@/composables/useWideScreenLayout.ts', () => ({
  useWideScreenLayout: () => ({
    isWideScreen: { value: mockLayout.isWideScreen },
    chatCollapsed: { value: mockLayout.chatCollapsed },
  }),
  // Faithful copy of the real predicate (useWideScreenLayout.ts:204).
  isChatPanelVisible: (state: { isWideScreen: boolean; chatCollapsed: boolean; activeTab: string }) => {
    if (!state.isWideScreen) return state.activeTab === 'chat'
    return !state.chatCollapsed
  },
}))

const { mockToastShow } = vi.hoisted(() => ({ mockToastShow: vi.fn() }))
vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

const { mockState } = vi.hoisted(() => ({
  mockState: { sessionMaxCount: 0, sessionCount: 0, sessionListVersion: 0 },
}))
vi.mock('@/stores/app.ts', () => ({ store: { state: mockState } }))

const { mockIdentity } = vi.hoisted(() => ({
  mockIdentity: {
    currentSessionId: { value: '' },
    // Read by confirmTarget's `choose` path to describe the CURRENT session
    // without going through the picker's (possibly stale) list.
    currentSessionTitle: { value: '' },
    currentAgentId: { value: '' },
    enqueueToSession: vi.fn(),
    createSessionInBackground: vi.fn(),
    switchSession: vi.fn(),
  },
}))
vi.mock('@/composables/useSessionIdentity.ts', () => ({
  useSessionIdentity: () => mockIdentity,
}))

const { mockChatContext } = vi.hoisted(() => ({
  mockChatContext: {
    addStagedQuote: vi.fn(),
    addAttachedFile: vi.fn(),
    stageQuoteIntoDraft: vi.fn(),
    stageAttachmentIntoDraft: vi.fn(),
  },
}))
vi.mock('@/composables/useChatContext.ts', () => ({
  useChatContext: () => mockChatContext,
}))

vi.mock('@/utils/quoteItem.ts', () => ({
  // The real materializeQuotes maps StagedQuote → FileEntry; the dispatcher only
  // needs it to produce entries, so echo the shape the real one would.
  materializeQuotes: (quotes: Array<{ id: string; text: string; filePath: string }>) =>
    quotes.map(q => ({ path: q.filePath, kind: 'quote' as const, id: q.id, text: q.text })),
}))

import {
  setOpenSessionHandler,
  requestTarget,
  requestAttachmentTarget,
  usePendingTarget,
  cancelTarget,
  dispatchToTarget,
  confirmTarget,
  chooseTarget,
  usePickerIntent,
  useChosenTarget,
  clearChosenTarget,
  setChosenTarget,
  setTargetMetaLookup,
  type PendingRequest,
} from '@/composables/useConversationTarget.ts'

/** Injected "open a session" handler (App wires the real one). */
const mockOpenSession = vi.fn()

const ATTACH: FileEntry = { path: '/a.txt', isDir: false }

/** A minimal pending quote, for the cases that only care about delivery. */
const QUOTE = {
  id: 'q1', text: 't', note: 'n', filePath: '/x.ts',
  language: 'ts', startLine: 1, endLine: 1, sourceKind: 'file' as const,
}

function request(overrides: Partial<PendingRequest> = {}): PendingRequest {
  return {
    mode: 'send',
    quotes: [],
    attachments: [],
    text: 'hello',
    ...overrides,
  }
}

describe('useConversationTarget', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockLayout.isWideScreen = false
    mockLayout.chatCollapsed = false
    mockState.sessionMaxCount = 0
    mockState.sessionCount = 0
    mockState.sessionListVersion = 0
    mockIdentity.currentSessionId.value = 's-current'
    mockIdentity.currentSessionTitle.value = ''
    mockIdentity.currentAgentId.value = ''
    mockIdentity.enqueueToSession.mockResolvedValue(true)
    mockIdentity.createSessionInBackground.mockResolvedValue('s-new')
    mockOpenSession.mockReset()
    setOpenSessionHandler(mockOpenSession)
    clearChosenTarget()
    cancelTarget()
  })

  // ── Choosing a destination ahead of time (the bar's "To:" row) ──

  describe('chooseTarget / chosenTarget', () => {
    it('opens the picker in `choose` mode with no pending payload', () => {
      chooseTarget()

      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value).toBeNull()
      expect(usePickerIntent().value).toBe('choose')
    })

    it('records the pick WITHOUT delivering anything', async () => {
      // The whole point: the user is still composing, so confirming a
      // destination must not send an empty payload anywhere.
      setTargetMetaLookup(() => ({ title: 'Other chat', agentId: 'a2' }))
      chooseTarget()

      await confirmTarget({ kind: 'session', id: 's-other' })

      expect(useChosenTarget().value).toEqual({ id: 's-other', title: 'Other chat', agentId: 'a2' })
      expect(mockIdentity.enqueueToSession).not.toHaveBeenCalled()
      expect(usePendingTarget().pickerOpen.value).toBe(false)
      setTargetMetaLookup(null)
    })

    it('uses the live identity title for the CURRENT session', async () => {
      // The picker's list may be stale for the session on screen; identity is
      // authoritative there.
      mockIdentity.currentSessionId.value = 's-current'
      mockIdentity.currentSessionTitle.value = 'Live title'
      mockIdentity.currentAgentId.value = 'a-live'
      chooseTarget()

      await confirmTarget({ kind: 'session', id: 's-current' })

      expect(useChosenTarget().value).toEqual({
        id: 's-current', title: 'Live title', agentId: 'a-live',
      })
    })

    it('leaves the choice untouched for `create` (nothing to preview yet)', async () => {
      chooseTarget()
      await confirmTarget({ kind: 'create' })
      expect(useChosenTarget().value).toBeNull()
    })

    it('a chosen target wins over the active session on delivery', async () => {
      // The row still shows the picked conversation, so delivering to the
      // active session would contradict what is on screen.
      mockIdentity.currentSessionId.value = 's-active'
      setChosenTarget({ id: 's-picked', title: 'Picked', agentId: 'a1' })

      requestTarget(request({ mode: 'send', text: 'hi' }), { deliverTo: 'active' })
      await flushPromises()

      expect(mockIdentity.enqueueToSession).toHaveBeenCalledTimes(1)
      expect(mockIdentity.enqueueToSession.mock.calls[0][0]).toBe('s-picked')
    })

    it('falls back to the active session when nothing was chosen', async () => {
      mockIdentity.currentSessionId.value = 's-active'
      requestTarget(request({ mode: 'send', text: 'hi' }), { deliverTo: 'active' })
      await flushPromises()
      expect(mockIdentity.enqueueToSession.mock.calls[0][0]).toBe('s-active')
    })

    it('cancelTarget does not leave the picker stuck in `choose` mode', () => {
      // A cancelled choose would otherwise make the NEXT deliver-confirm only
      // record a destination and silently drop the card.
      chooseTarget()
      cancelTarget()
      expect(usePickerIntent().value).toBe('deliver')
    })

    it('confirmTarget in `deliver` mode still delivers after a choose was cancelled', async () => {
      chooseTarget()
      cancelTarget()
      requestTarget(request({ mode: 'send', text: 'hi' }))
      await confirmTarget({ kind: 'session', id: 's-other' })
      expect(mockIdentity.enqueueToSession).toHaveBeenCalledTimes(1)
    })
  })

  // ── requestTarget: picker by default, active session on request ──

  describe('requestTarget', () => {
    /**
     * The DEFAULT is still the picker. Entry points that are ABOUT choosing a
     * destination (quoting a whole chat message, attaching a file from the
     * browser) must keep asking — that is the only way "quote into another
     * conversation" stays reachable.
     */
    it('opens the picker when the caller does not ask for the active session', () => {
      mockLayout.isWideScreen = false
      requestTarget(request())
      expect(usePendingTarget().pickerOpen.value).toBe(true)
    })

    it('opens the picker on a wide screen too', () => {
      mockLayout.isWideScreen = true
      mockLayout.chatCollapsed = false
      requestTarget(request())
      expect(usePendingTarget().pickerOpen.value).toBe(true)
    })

    /**
     * Comments pass `deliverTo: 'active'`: the user is annotating what they are
     * reading, so the conversation they are in is the destination. No dialog.
     */
    it('delivers straight to the active session with deliverTo: active', async () => {
      mockIdentity.currentSessionId.value = 's-current'

      requestTarget(request({ mode: 'add', text: '', quotes: [QUOTE] }), { deliverTo: 'active' })
      // Dispatch is kicked off synchronously (not awaited by the caller).
      await Promise.resolve()

      expect(usePendingTarget().pickerOpen.value).toBe(false)
      expect(mockIdentity.enqueueToSession).not.toHaveBeenCalled()
      expect(mockChatContext.addStagedQuote).toHaveBeenCalledWith(expect.objectContaining({ id: 'q1' }), 'n')
    })

    it('falls back to the picker when there is no active session', () => {
      mockIdentity.currentSessionId.value = ''
      requestTarget(request(), { deliverTo: 'active' })
      expect(usePendingTarget().pickerOpen.value).toBe(true)
    })

    it('falls back to the picker when the active-session delivery fails', async () => {
      // The chat panel may not be mounted yet (enqueue bridge missing) or the
      // request may throw. Losing the card silently is the one outcome that
      // must not happen, so the user is asked where it should go instead.
      mockIdentity.currentSessionId.value = 's-current'
      mockIdentity.enqueueToSession.mockResolvedValue(false)

      requestTarget(request({ mode: 'send', text: 'hi' }), { deliverTo: 'active' })
      await flushPromises()

      expect(usePendingTarget().pickerOpen.value).toBe(true)
      expect(usePendingTarget().pending.value?.text).toBe('hi')
    })

    it('does not open a picker over an empty payload', async () => {
      // Nothing to deliver: `dispatchToTarget` rejects it, and a dialog over an
      // empty request would be pure noise.
      mockIdentity.currentSessionId.value = 's-current'
      requestTarget(request({ mode: 'send', text: '   ' }), { deliverTo: 'active' })
      await flushPromises()
      expect(usePendingTarget().pickerOpen.value).toBe(false)
    })

    it('stores the request payload', () => {
      requestTarget(request({ text: 'hi' }))
      expect(usePendingTarget().pending.value?.text).toBe('hi')
    })

    it('cancelTarget clears the pending request and closes', () => {
      requestTarget(request())
      cancelTarget()
      expect(usePendingTarget().pickerOpen.value).toBe(false)
      expect(usePendingTarget().pending.value).toBeNull()
    })
  })

  // ── dispatch: send ───────────────────────────────────────────

  describe('dispatchToTarget — send', () => {
    it('enqueues into the picked session without switching', async () => {
      const ok = await dispatchToTarget({ kind: 'session', id: 's-other' }, request({ text: 'q' }))

      expect(ok).toBe(true)
      expect(mockIdentity.enqueueToSession).toHaveBeenCalledTimes(1)
      const [sid, text] = mockIdentity.enqueueToSession.mock.calls[0]
      expect(sid).toBe('s-other')
      expect(text).toBe('q')
      // It must not touch the live input — the card belongs to another session.
      expect(mockChatContext.addStagedQuote).not.toHaveBeenCalled()
      expect(mockChatContext.stageQuoteIntoDraft).not.toHaveBeenCalled()
    })

    it('carries the materialised quotes and attachments in the payload', async () => {
      await dispatchToTarget({ kind: 'session', id: 's-other' }, request({
        quotes: [{ id: 'q1', text: 'const x = 1', note: '', filePath: '/x.ts', language: 'ts', startLine: 1, endLine: 1, sourceKind: 'file' }],
        attachments: [ATTACH],
      }))

      const entries: FileEntry[] = mockIdentity.enqueueToSession.mock.calls[0][2]
      expect(entries.map(e => e.path)).toEqual(['/a.txt', '/x.ts'])
    })

    it('sends to the CURRENT session when the user picks the current row', async () => {
      // Picking the row for the session already on screen is just kind=session
      // with that id — the dispatcher routes it to the same session either way.
      await dispatchToTarget({ kind: 'session', id: 's-current' }, request())
      expect(mockIdentity.enqueueToSession.mock.calls[0][0]).toBe('s-current')
    })

    it('refuses an empty payload', async () => {
      const ok = await dispatchToTarget({ kind: 'session', id: 's-other' }, request({ text: '   ' }))
      expect(ok).toBe(false)
      expect(mockIdentity.enqueueToSession).not.toHaveBeenCalled()
    })

    it('allows an attachment-only send with no text', async () => {
      const ok = await dispatchToTarget({ kind: 'session', id: 's-other' }, request({ text: '', attachments: [ATTACH] }))
      expect(ok).toBe(true)
      expect(mockIdentity.enqueueToSession).toHaveBeenCalled()
    })

    it('reports failure and does not claim success when the send fails', async () => {
      mockIdentity.enqueueToSession.mockResolvedValue(false)
      const ok = await dispatchToTarget({ kind: 'session', id: 's-other' }, request())
      expect(ok).toBe(false)
      expect(mockToastShow).not.toHaveBeenCalledWith('quoteBar.sentToSession', expect.anything())
    })
  })

  // ── dispatch: add ────────────────────────────────────────────

  describe('dispatchToTarget — add', () => {
    it('stages into the picked session draft (not the live input)', async () => {
      const ok = await dispatchToTarget({ kind: 'session', id: 's-other' }, request({
        mode: 'add',
        quotes: [{ id: 'q1', text: 't', note: 'n', filePath: '/x.ts', language: 'ts', startLine: 1, endLine: 1, sourceKind: 'file' }],
        attachments: [ATTACH],
      }))

      expect(ok).toBe(true)
      expect(mockChatContext.stageQuoteIntoDraft).toHaveBeenCalledWith('s-other', expect.objectContaining({ filePath: '/x.ts' }), 'n')
      expect(mockChatContext.stageAttachmentIntoDraft).toHaveBeenCalledWith('s-other', ATTACH)
      expect(mockChatContext.addStagedQuote).not.toHaveBeenCalled()
      expect(mockIdentity.enqueueToSession).not.toHaveBeenCalled()
      expect(mockToastShow).toHaveBeenCalledWith('quoteBar.addedToSessionDraft', expect.anything())
    })

    it('stages into the LIVE input when the target is the current session', async () => {
      // The picker's top row is the current session, so this is the common
      // "just the one I'm in" path. It must land in the live input, not the
      // stored draft (there is nothing to restore for a session already open).
      const ok = await dispatchToTarget({ kind: 'session', id: 's-current' }, request({
        mode: 'add',
        quotes: [{ id: 'q1', text: 't', note: 'n', filePath: '/x.ts', language: 'ts', startLine: 1, endLine: 1, sourceKind: 'file' }],
      }))

      expect(ok).toBe(true)
      expect(mockChatContext.addStagedQuote).toHaveBeenCalled()
      expect(mockChatContext.stageQuoteIntoDraft).not.toHaveBeenCalled()
    })

    // ── "add and open" ──

    it('does NOT open the session on a plain add', async () => {
      await dispatchToTarget({ kind: 'session', id: 's-other' }, request({ mode: 'add', text: '' }))
      expect(mockOpenSession).not.toHaveBeenCalled()
      expect(mockIdentity.switchSession).not.toHaveBeenCalled()
    })

    it('stages first, then opens the session when openAfter is set', async () => {
      const order: string[] = []
      mockChatContext.stageQuoteIntoDraft.mockImplementation(() => { order.push('stage') })
      mockOpenSession.mockImplementation(() => { order.push('open') })

      await dispatchToTarget({ kind: 'session', id: 's-other' }, request({
        mode: 'add',
        text: '',
        openAfter: true,
        quotes: [{ id: 'q1', text: 't', note: '', filePath: '/x.ts', language: 'ts', startLine: 1, endLine: 1, sourceKind: 'file' }],
      }))

      expect(order).toEqual(['stage', 'open'])
      expect(mockOpenSession).toHaveBeenCalledWith('s-other')
    })

    it('opens the freshly created session when openAfter is set', async () => {
      await dispatchToTarget({ kind: 'create' }, request({ mode: 'add', text: '', openAfter: true }))

      expect(mockIdentity.createSessionInBackground).toHaveBeenCalled()
      expect(mockOpenSession).toHaveBeenCalledWith('s-new')
      expect(mockToastShow).toHaveBeenCalledWith('quoteBar.openAndCreated', expect.anything())
    })

    it('does not open when the target is already the current session', async () => {
      await dispatchToTarget({ kind: 'session', id: 's-current' }, request({ mode: 'add', text: '', openAfter: true }))
      expect(mockOpenSession).not.toHaveBeenCalled()
    })
  })

  // ── requestAttachmentTarget (the "attach to chat" buttons) ───

  describe('requestAttachmentTarget', () => {
    it('opens the picker (the file browser is where the destination IS the question)', () => {
      mockLayout.isWideScreen = false
      requestAttachmentTarget(ATTACH)

      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value?.mode).toBe('add')
      expect(pending.value?.attachments).toEqual([ATTACH])
      expect(pending.value?.quotes).toHaveLength(0)
    })

    it('delivers to the active session when the caller says so (markdown badges)', async () => {
      // A comment on a document is about the conversation being worked in, so
      // the badge path does not ask. Same fallback rule as requestTarget.
      mockIdentity.currentSessionId.value = 's-current'
      requestAttachmentTarget(ATTACH, { deliverTo: 'active' })
      await Promise.resolve()

      expect(usePendingTarget().pickerOpen.value).toBe(false)
      expect(mockChatContext.addAttachedFile).toHaveBeenCalledWith('/a.txt', false, undefined, undefined)
    })

    it('preserves a line-range reference', () => {
      requestAttachmentTarget({ path: '/a.ts', isDir: false, startLine: 5, endLine: 9 })
      expect(usePendingTarget().pending.value?.attachments[0]).toMatchObject({ startLine: 5, endLine: 9 })
    })

    it('ignores an entry with no path (nothing to attach, nothing opens)', () => {
      requestAttachmentTarget({ path: '', isDir: false })
      expect(usePendingTarget().pickerOpen.value).toBe(false)
    })
  })

  // ── dispatch: create ─────────────────────────────────────────
  describe('dispatchToTarget — create', () => {
    it('creates a session and sends to it', async () => {
      const ok = await dispatchToTarget({ kind: 'create' }, request())

      expect(ok).toBe(true)
      expect(mockIdentity.createSessionInBackground).toHaveBeenCalled()
      expect(mockIdentity.enqueueToSession.mock.calls[0][0]).toBe('s-new')
      // The toast names the create, not a plain "sent".
      expect(mockToastShow).toHaveBeenCalledWith('quoteBar.createdSession', expect.anything())
    })

    it('refuses to create past the session limit', async () => {
      mockState.sessionMaxCount = 5
      mockState.sessionCount = 5

      const ok = await dispatchToTarget({ kind: 'create' }, request())

      expect(ok).toBe(false)
      expect(mockIdentity.createSessionInBackground).not.toHaveBeenCalled()
      expect(mockToastShow).toHaveBeenCalledWith('chat.session.sessionLimitReached', expect.anything())
    })

    it('bails out when creation fails', async () => {
      mockIdentity.createSessionInBackground.mockResolvedValue('')
      const ok = await dispatchToTarget({ kind: 'create' }, request())

      expect(ok).toBe(false)
      expect(mockIdentity.enqueueToSession).not.toHaveBeenCalled()
      expect(mockToastShow).toHaveBeenCalledWith('chat.session.createFailed', expect.anything())
    })

    it('treats the new session as non-current (stages into its draft on add)', async () => {
      await dispatchToTarget({ kind: 'create' }, request({
        mode: 'add',
        quotes: [{ id: 'q1', text: 't', note: '', filePath: '/x.ts', language: 'ts', startLine: 1, endLine: 1, sourceKind: 'file' }],
      }))

      expect(mockChatContext.stageQuoteIntoDraft).toHaveBeenCalledWith('s-new', expect.anything(), '')
      expect(mockChatContext.addStagedQuote).not.toHaveBeenCalled()
    })

    it('add mode does NOT open the new session (plain create = stay put)', async () => {
      await dispatchToTarget({ kind: 'create' }, request({ mode: 'add', text: '' }))

      expect(mockIdentity.createSessionInBackground).toHaveBeenCalled()
      expect(mockOpenSession).not.toHaveBeenCalled()
      // The toast must not claim it was opened.
      expect(mockToastShow).toHaveBeenCalledWith('quoteBar.createdSession', expect.anything())
      expect(mockToastShow).not.toHaveBeenCalledWith('quoteBar.openAndCreated', expect.anything())
    })

    it('add mode opens the new session when openAfter is set (create and open)', async () => {
      await dispatchToTarget({ kind: 'create' }, request({ mode: 'add', text: '', openAfter: true }))

      expect(mockOpenSession).toHaveBeenCalledWith('s-new')
      expect(mockToastShow).toHaveBeenCalledWith('quoteBar.openAndCreated', expect.anything())
    })

    it('a created session never takes the live-input path, even if ids collide', async () => {
      // Defensive: if the new id happened to equal currentSessionId, the card
      // must still land in that session's draft (which is then opened), not in
      // the live input of a different session.
      mockIdentity.currentSessionId.value = 's-new'
      await dispatchToTarget({ kind: 'create' }, request({
        mode: 'add',
        text: '',
        quotes: [{ id: 'q1', text: 't', note: '', filePath: '/x.ts', language: 'ts', startLine: 1, endLine: 1, sourceKind: 'file' }],
      }))

      expect(mockChatContext.stageQuoteIntoDraft).toHaveBeenCalledWith('s-new', expect.anything(), '')
      expect(mockChatContext.addStagedQuote).not.toHaveBeenCalled()
    })
  })

  // ── confirmTarget ────────────────────────────────────────────

  describe('confirmTarget', () => {
    it('dispatches the pending request and closes the picker', async () => {
      requestTarget(request({ text: 'pending text' }))

      await confirmTarget({ kind: 'session', id: 's-other' })

      expect(mockIdentity.enqueueToSession.mock.calls[0][1]).toBe('pending text')
      expect(usePendingTarget().pickerOpen.value).toBe(false)
      expect(usePendingTarget().pending.value).toBeNull()
    })

    it('is a no-op when nothing is pending', async () => {
      cancelTarget()
      await confirmTarget({ kind: 'session', id: 's-other' })
      expect(mockIdentity.enqueueToSession).not.toHaveBeenCalled()
    })

    it('uses the snapshot, so a session switch during the dialog cannot retarget it', async () => {
      requestTarget(request({ text: 'snapshotted' }))
      // The user switches sessions while the picker is open.
      mockIdentity.currentSessionId.value = 's-different'

      await confirmTarget({ kind: 'session', id: 's-other' })

      expect(mockIdentity.enqueueToSession.mock.calls[0][0]).toBe('s-other')
      expect(mockIdentity.enqueueToSession.mock.calls[0][1]).toBe('snapshotted')
    })

    it('passes openAfter through to the dispatch when the caller asks for it', async () => {
      requestTarget(request({ mode: 'add', text: '' }))

      await confirmTarget({ kind: 'session', id: 's-other' }, { openAfter: true })

      expect(mockOpenSession).toHaveBeenCalledWith('s-other')
    })

    it('a send is never turned into an open, even if openAfter slips through', async () => {
      requestTarget(request({ mode: 'send', text: 'hi' }))

      await confirmTarget({ kind: 'session', id: 's-other' }, { openAfter: true })

      // send means "run it over there, leave me here".
      expect(mockOpenSession).not.toHaveBeenCalled()
      expect(mockIdentity.enqueueToSession).toHaveBeenCalled()
    })
  })
})
