import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
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
  type PendingRequest,
} from '@/composables/useConversationTarget.ts'

/** Injected "open a session" handler (App wires the real one). */
const mockOpenSession = vi.fn()

const ATTACH: FileEntry = { path: '/a.txt', isDir: false }

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
    mockIdentity.enqueueToSession.mockResolvedValue(true)
    mockIdentity.createSessionInBackground.mockResolvedValue('s-new')
    mockOpenSession.mockReset()
    setOpenSessionHandler(mockOpenSession)
    cancelTarget()
  })

  // ── requestTarget: the picker is unconditional ───────────────

  describe('requestTarget', () => {
    /**
     * The picker must open EVEN when the chat panel is on screen. The old
     * behavior assumed "panel visible ⇒ current session", which is what made
     * quoting from one conversation into another impossible and made the
     * destination depend on which tab happened to be open.
     */
    it('opens the picker even when the chat panel is visible', () => {
      mockLayout.isWideScreen = false
      requestTarget(request())
      expect(usePendingTarget().pickerOpen.value).toBe(true)
    })

    it('opens the picker on a wide screen too (no visibility shortcut)', () => {
      mockLayout.isWideScreen = true
      mockLayout.chatCollapsed = false
      requestTarget(request())
      expect(usePendingTarget().pickerOpen.value).toBe(true)
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
    it('always opens the picker, even with the chat panel visible', () => {
      mockLayout.isWideScreen = false
      requestAttachmentTarget(ATTACH)

      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value?.mode).toBe('add')
      expect(pending.value?.attachments).toEqual([ATTACH])
      expect(pending.value?.quotes).toHaveLength(0)
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
