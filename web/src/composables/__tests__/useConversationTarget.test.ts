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
  canSeeChatPanel,
  setActiveTabGetter,
  requestTarget,
  requestAttachmentTarget,
  usePendingTarget,
  cancelTarget,
  dispatchToTarget,
  confirmTarget,
  type PendingRequest,
} from '@/composables/useConversationTarget.ts'

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
    setActiveTabGetter(() => 'chat')
    cancelTarget()
  })

  // ── Visibility gate ──────────────────────────────────────────

  describe('canSeeChatPanel', () => {
    it('narrow layout: visible only on the chat tab', () => {
      mockLayout.isWideScreen = false
      setActiveTabGetter(() => 'chat')
      expect(canSeeChatPanel()).toBe(true)

      setActiveTabGetter(() => 'browse')
      expect(canSeeChatPanel()).toBe(false)
    })

    it('wide layout: visible unless the chat column is collapsed', () => {
      mockLayout.isWideScreen = true
      setActiveTabGetter(() => 'browse')
      mockLayout.chatCollapsed = false
      expect(canSeeChatPanel()).toBe(true)

      // The case the user called out: desktop with the chat panel hidden.
      mockLayout.chatCollapsed = true
      expect(canSeeChatPanel()).toBe(false)
    })

    it('defaults to hidden when no activeTab getter was injected', () => {
      mockLayout.isWideScreen = false
      setActiveTabGetter(undefined as unknown as () => string)
      expect(canSeeChatPanel()).toBe(false)
    })
  })

  // ── requestTarget ────────────────────────────────────────────

  describe('requestTarget', () => {
    it('does NOT open the picker when the chat panel is visible', () => {
      setActiveTabGetter(() => 'chat')
      const opened = requestTarget(request())
      expect(opened).toBe(false)
      expect(usePendingTarget().pickerOpen.value).toBe(false)
    })

    it('opens the picker and stores the request when the panel is hidden', () => {
      setActiveTabGetter(() => 'browse')
      const opened = requestTarget(request({ text: 'hi' }))
      expect(opened).toBe(true)
      expect(usePendingTarget().pickerOpen.value).toBe(true)
      expect(usePendingTarget().pending.value?.text).toBe('hi')
    })

    it('cancelTarget clears the pending request and closes', () => {
      setActiveTabGetter(() => 'browse')
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

    it('sends to the CURRENT session id for kind=current', async () => {
      await dispatchToTarget({ kind: 'current' }, request())
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
      const ok = await dispatchToTarget({ kind: 'current' }, request({
        mode: 'add',
        quotes: [{ id: 'q1', text: 't', note: 'n', filePath: '/x.ts', language: 'ts', startLine: 1, endLine: 1, sourceKind: 'file' }],
      }))

      expect(ok).toBe(true)
      expect(mockChatContext.addStagedQuote).toHaveBeenCalled()
      expect(mockChatContext.stageQuoteIntoDraft).not.toHaveBeenCalled()
    })
  })

  // ── requestAttachmentTarget (the "attach to chat" buttons) ───

  describe('requestAttachmentTarget', () => {
    it('does not open the picker while the chat panel is visible', () => {
      setActiveTabGetter(() => 'chat')
      expect(requestAttachmentTarget(ATTACH)).toBe(false)
      expect(usePendingTarget().pickerOpen.value).toBe(false)
    })

    it('opens the picker with the attachment as an add payload when hidden', () => {
      setActiveTabGetter(() => 'browse')
      expect(requestAttachmentTarget(ATTACH)).toBe(true)

      const { pickerOpen, pending } = usePendingTarget()
      expect(pickerOpen.value).toBe(true)
      expect(pending.value?.mode).toBe('add')
      expect(pending.value?.attachments).toEqual([ATTACH])
      expect(pending.value?.quotes).toHaveLength(0)
    })

    it('preserves a line-range reference', () => {
      setActiveTabGetter(() => 'browse')
      requestAttachmentTarget({ path: '/a.ts', isDir: false, startLine: 5, endLine: 9 })
      expect(usePendingTarget().pending.value?.attachments[0]).toMatchObject({ startLine: 5, endLine: 9 })
    })

    it('ignores an entry with no path', () => {
      setActiveTabGetter(() => 'browse')
      expect(requestAttachmentTarget({ path: '', isDir: false })).toBe(false)
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
  })

  // ── confirmTarget ────────────────────────────────────────────

  describe('confirmTarget', () => {
    it('dispatches the pending request and closes the picker', async () => {
      setActiveTabGetter(() => 'browse')
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
      setActiveTabGetter(() => 'browse')
      requestTarget(request({ text: 'snapshotted' }))
      // The user switches sessions while the picker is open.
      mockIdentity.currentSessionId.value = 's-different'

      await confirmTarget({ kind: 'session', id: 's-other' })

      expect(mockIdentity.enqueueToSession.mock.calls[0][0]).toBe('s-other')
      expect(mockIdentity.enqueueToSession.mock.calls[0][1]).toBe('snapshotted')
    })
  })
})
