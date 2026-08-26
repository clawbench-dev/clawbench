import { describe, expect, it } from 'vitest'
import {
  isShowingSummary,
  shouldShowSummary,
  applySummaryUpdate,
  parseMessages,
  buildMessageSnapshot,
  type MessageDisplayMode,
} from '@/utils/chatSessionUtils'

// ── isShowingSummary ──

describe('isShowingSummary', () => {
  it('returns false when message has no summary', () => {
    expect(isShowingSummary({ summary: null })).toBe(false)
    expect(isShowingSummary({ summary: '' })).toBe(false)
    expect(isShowingSummary({})).toBe(false)
  })

  it('returns true by default when summary exists and mode is summary', () => {
    expect(isShowingSummary({ summary: 'A summary' }, 'summary')).toBe(true)
  })

  it('returns false by default when summary exists and mode is original', () => {
    expect(isShowingSummary({ summary: 'A summary' }, 'original')).toBe(false)
  })

  it('respects explicit showingSummary=true with blocks present', () => {
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [{ type: 'text', text: 'Full content' }],
      showingSummary: true,
    }, 'original')).toBe(true)
  })

  it('respects explicit showingSummary=false with blocks present', () => {
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [{ type: 'text', text: 'Full content' }],
      showingSummary: false,
    }, 'summary')).toBe(false)
  })

  it('falls back to summary when blocks empty and showingSummary=false (stripped content)', () => {
    // When content was stripped by the backend, summary is the only renderable
    // content, so we must show it regardless of user preference.
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [],
      showingSummary: false,
    }, 'summary')).toBe(true)
  })

  it('falls back to summary when blocks are undefined and showingSummary=false', () => {
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: undefined,
      showingSummary: false,
    }, 'summary')).toBe(true)
  })

  // ── _loadAttempted latch ──

  it('shows summary as placeholder when _loadAttempted=true and blocks empty', () => {
    // After a failed load attempt, the message has no blocks to render in
    // original view, so the summary stays visible as a placeholder.
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [],
      _loadAttempted: true,
    }, 'original')).toBe(true)
  })

  it('shows summary as placeholder when _loadingOriginal=true and blocks empty', () => {
    // While the full content is being fetched, the summary stays visible so
    // the message bubble is never blank.
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [],
      _loadingOriginal: true,
    }, 'original')).toBe(true)
  })

  it('shows summary as placeholder when both _loadingOriginal and _loadAttempted are true', () => {
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [],
      _loadingOriginal: true,
      _loadAttempted: true,
    }, 'original')).toBe(true)
  })

  it('does not show summary when _loadAttempted=true but blocks are present', () => {
    // After a successful load, blocks are populated and the user's original
    // preference (or default mode) takes over.
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [{ type: 'text', text: 'Full content' }],
      _loadAttempted: true,
    }, 'original')).toBe(false)
  })

  it('does not show summary when _loadingOriginal=true but blocks are present', () => {
    // Edge case: if blocks somehow exist while loading, respect them.
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [{ type: 'text', text: 'Full content' }],
      _loadingOriginal: true,
    }, 'original')).toBe(false)
  })

  it('does not show summary placeholder when _loadAttempted=false and _loadingOriginal=false', () => {
    // Without the latch or loading flag, and in original mode, the summary
    // should not be shown (the component will trigger lazy fetch).
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [],
      _loadAttempted: false,
      _loadingOriginal: false,
    }, 'original')).toBe(false)
  })

  it('shows summary in original mode with _loadAttempted=true even when showingSummary=false', () => {
    // The user toggled to original, but the load failed — keep showing
    // the summary so the bubble isn't blank.
    expect(isShowingSummary({
      summary: 'A summary',
      blocks: [],
      showingSummary: false,
      _loadAttempted: true,
    }, 'original')).toBe(true)
  })

  it('_loadAttempted has no effect when summary is absent', () => {
    expect(isShowingSummary({
      blocks: [],
      _loadAttempted: true,
    }, 'original')).toBe(false)
  })
})

// ── shouldShowSummary ──

describe('shouldShowSummary', () => {
  it('returns false when no summary exists', () => {
    expect(shouldShowSummary({})).toBe(false)
    expect(shouldShowSummary({ summary: null })).toBe(false)
    expect(shouldShowSummary({ summary: '' })).toBe(false)
  })

  it('defaults to summary mode when no explicit preference', () => {
    expect(shouldShowSummary({ summary: 'S' }, 'summary')).toBe(true)
    expect(shouldShowSummary({ summary: 'S' }, 'original')).toBe(false)
  })

  it('defaults to summary mode when no mode argument given', () => {
    expect(shouldShowSummary({ summary: 'S' })).toBe(true)
  })

  it('respects showingSummary=true with blocks', () => {
    expect(shouldShowSummary({
      summary: 'S',
      blocks: [{ type: 'text', text: 'Full' }],
      showingSummary: true,
    }, 'original')).toBe(true)
  })

  it('respects showingSummary=false with blocks', () => {
    expect(shouldShowSummary({
      summary: 'S',
      blocks: [{ type: 'text', text: 'Full' }],
      showingSummary: false,
    }, 'summary')).toBe(false)
  })

  it('overrides showingSummary=false with summary when blocks empty', () => {
    // Stripped content: must show summary regardless of preference
    expect(shouldShowSummary({
      summary: 'S',
      blocks: [],
      showingSummary: false,
    }, 'summary')).toBe(true)
  })

  it('returns false in original mode with empty blocks and no preference', () => {
    // Triggers lazy fetch — the component will call ensureMessageContent
    expect(shouldShowSummary({
      summary: 'S',
      blocks: [],
    }, 'original')).toBe(false)
  })
})

// ── applySummaryUpdate ──

describe('applySummaryUpdate', () => {
  it('sets summary and summaryCards on the message', () => {
    const msg: Record<string, unknown> = {}
    applySummaryUpdate(msg, 'New summary', { createdFiles: ['/a.ts'] }, true)
    expect(msg.summary).toBe('New summary')
    expect(msg.summaryCards).toEqual({ createdFiles: ['/a.ts'] })
  })

  it('does not set summaryCards when null', () => {
    const msg: Record<string, unknown> = {}
    applySummaryUpdate(msg, 'S', null, true)
    expect(msg.summary).toBe('S')
    expect(msg.summaryCards).toBeUndefined()
  })

  it('does not set summaryCards when undefined', () => {
    const msg: Record<string, unknown> = {}
    applySummaryUpdate(msg, 'S', undefined, true)
    expect(msg.summary).toBe('S')
    expect(msg.summaryCards).toBeUndefined()
  })

  it('does not overwrite showingSummary', () => {
    // applySummaryUpdate must NOT touch showingSummary — it only stores
    // the user's explicit preference, not the auto-switch decision.
    const msg: Record<string, unknown> = { showingSummary: false }
    applySummaryUpdate(msg, 'S', null, true)
    expect(msg.showingSummary).toBe(false)
  })

  it('sets summary even when atBottom is false', () => {
    const msg: Record<string, unknown> = {}
    applySummaryUpdate(msg, 'S', null, false)
    expect(msg.summary).toBe('S')
  })

  it('allows setting summary to null', () => {
    const msg: Record<string, unknown> = { summary: 'old' }
    applySummaryUpdate(msg, null, null, true)
    expect(msg.summary).toBeNull()
  })
})

// ── parseMessages ──

describe('parseMessages', () => {
  const mockParser = (content: string) => {
    const blocks = content ? [{ type: 'text', text: content }] : []
    return { blocks, metadata: null, cancelled: false }
  }

  it('parses assistant messages with blocks and metadata', () => {
    const raw = [{ id: '1', role: 'assistant', content: 'Hello' }]
    const result = parseMessages(raw, mockParser)
    expect(result[0].blocks).toEqual([{ type: 'text', text: 'Hello' }])
  })

  it('preserves existing showingSummary=true', () => {
    const raw = [{ id: '1', role: 'assistant', content: 'Hello', summary: 'S' }]
    const existing = [{ id: '1', showingSummary: true }]
    const result = parseMessages(raw, mockParser, existing)
    expect(result[0].showingSummary).toBe(true)
  })

  it('preserves existing showingSummary=false', () => {
    const raw = [{ id: '1', role: 'assistant', content: 'Hello', summary: 'S' }]
    const existing = [{ id: '1', showingSummary: false }]
    const result = parseMessages(raw, mockParser, existing)
    expect(result[0].showingSummary).toBe(false)
  })

  it('does not set showingSummary when no existing preference', () => {
    const raw = [{ id: '1', role: 'assistant', content: 'Hello', summary: 'S' }]
    const result = parseMessages(raw, mockParser)
    expect(result[0].showingSummary).toBeUndefined()
  })

  it('strips streaming flag when session is not running', () => {
    const raw = [{ id: '1', role: 'assistant', content: 'Hello', streaming: true }]
    const result = parseMessages(raw, mockParser, undefined, false)
    expect(result[0].streaming).toBeUndefined()
    expect(result[0].fromDB).toBeUndefined()
  })

  it('keeps streaming flag and sets fromDB when session is running', () => {
    const raw = [{ id: '1', role: 'assistant', content: 'Hello', streaming: true }]
    const result = parseMessages(raw, mockParser, undefined, true)
    expect(result[0].streaming).toBe(true)
    expect(result[0].fromDB).toBe(true)
  })

  it('strips streaming flag from user messages', () => {
    const raw = [{ id: '1', role: 'user', content: 'Hi', streaming: true }]
    const result = parseMessages(raw, mockParser)
    expect(result[0].streaming).toBeUndefined()
  })

  it('parses user message with block-format content', () => {
    // parseMessages calls onParseAssistantContent for {"blocks":...} content,
    // which returns the parsed blocks. The mock parser wraps the raw string
    // as a text block, so the result reflects the mock's behavior.
    const raw = [{ id: '1', role: 'user', content: '{"blocks":[{"type":"text","text":"Hi"}]}' }]
    const result = parseMessages(raw, mockParser)
    // mockParser treats the whole JSON string as content text
    expect(result[0].blocks).toEqual([{ type: 'text', text: '{"blocks":[{"type":"text","text":"Hi"}]}' }])
  })

  it('creates text block for plain user message', () => {
    const raw = [{ id: '1', role: 'user', content: 'Hello' }]
    const result = parseMessages(raw, mockParser)
    expect(result[0].blocks).toEqual([{ type: 'text', text: 'Hello' }])
  })

  it('creates empty blocks for user message with no content', () => {
    const raw = [{ id: '1', role: 'user', content: '' }]
    const result = parseMessages(raw, mockParser)
    expect(result[0].blocks).toEqual([])
  })

  it('preserves existing blocks on user message if already present', () => {
    const existingBlocks = [{ type: 'text', text: 'existing' }]
    const raw = [{ id: '1', role: 'user', content: 'Hello', blocks: existingBlocks }]
    const result = parseMessages(raw, mockParser)
    expect(result[0].blocks).toBe(existingBlocks)
  })
})

// ── buildMessageSnapshot ──

describe('buildMessageSnapshot', () => {
  it('builds a fingerprint from message fields', () => {
    const msgs = [
      { id: '1', role: 'user', content: 'Hi', createdAt: '2024-01-01' },
      { id: '2', role: 'assistant', content: 'Hello', createdAt: '2024-01-02', streaming: true },
    ]
    const snapshot = buildMessageSnapshot(msgs)
    expect(snapshot).toBe('1:user:2:2024-01-01:0|2:assistant:5:2024-01-02:1')
  })

  it('handles missing fields gracefully', () => {
    const msgs = [{}]
    const snapshot = buildMessageSnapshot(msgs)
    // role is undefined, so the template literal produces "undefined"
    expect(snapshot).toBe(':undefined:0::0')
  })

  it('returns empty string for empty array', () => {
    expect(buildMessageSnapshot([])).toBe('')
  })
})

// ── ensureMessageContent logic simulation ──
// The ensureMessageContent function is defined inline in ChatPanelContent.vue
// and cannot be imported directly. We test the behavioral contract that
// isShowingSummary and the _loadAttempted / _loadingOriginal flags enforce.

describe('ensureMessageContent behavioral contract', () => {
  it('keeps summary visible while _loadingOriginal is true (content being fetched)', () => {
    // When the user toggles to original view but blocks are empty, the
    // component calls ensureMessageContent which sets _loadingOriginal=true.
    // During this time, isShowingSummary must return true so the bubble
    // isn't blank.
    const msg = {
      summary: 'Summary text',
      blocks: [],
      showingSummary: false,
      _loadingOriginal: true,
    }
    expect(isShowingSummary(msg, 'original')).toBe(true)
  })

  it('switches to original view once blocks are loaded and _loadingOriginal is cleared', () => {
    // After the fetch completes, blocks are populated and _loadingOriginal
    // is cleared. The user's preference (showingSummary=false) now takes
    // effect, and the original content is displayed.
    const msg = {
      summary: 'Summary text',
      blocks: [{ type: 'text', text: 'Full content' }],
      showingSummary: false,
      _loadingOriginal: false,
    }
    expect(isShowingSummary(msg, 'original')).toBe(false)
  })

  it('keeps summary as placeholder after failed load (_loadAttempted=true)', () => {
    // ensureMessageContent sets _loadAttempted=true in its finally block,
    // even if the fetch failed. isShowingSummary uses this to keep the
    // summary visible so the message is never blank.
    const msg = {
      summary: 'Summary text',
      blocks: [],
      showingSummary: false,
      _loadingOriginal: false,
      _loadAttempted: true,
    }
    expect(isShowingSummary(msg, 'original')).toBe(true)
  })

  it('does not re-trigger lazy fetch when _loadAttempted=true', () => {
    // The component should not emit ensure-content when _loadAttempted=true.
    // This is verified by ChatMessageItem's test, but the logic is:
    // isShowingSummary returns true for _loadAttempted with empty blocks,
    // so the component renders the summary view and does not emit
    // ensure-content (which is only emitted when isShowingSummary would
    // return false in original mode).
    const msg = {
      summary: 'Summary text',
      blocks: [],
      _loadAttempted: true,
    }
    // In original mode without _loadAttempted, this would return false
    // (triggering lazy fetch). With _loadAttempted=true, it returns true
    // (no lazy fetch, summary stays as placeholder).
    expect(isShowingSummary(msg, 'original')).toBe(true)
  })

  it('simulates the full ensureMessageContent lifecycle: load -> success', () => {
    // Step 1: Initial state — summary view, blocks empty
    let msg = { summary: 'Summary', blocks: [], showingSummary: false }
    expect(isShowingSummary(msg, 'original')).toBe(true) // blocks empty fallback

    // Step 2: User toggles to original, component sets _loadingOriginal=true
    msg = { ...msg, _loadingOriginal: true }
    expect(isShowingSummary(msg, 'original')).toBe(true) // loading placeholder

    // Step 3: Fetch succeeds, blocks populated, _loadingOriginal cleared
    msg = { ...msg, blocks: [{ type: 'text', text: 'Full' }], _loadingOriginal: false, _loadAttempted: true }
    expect(isShowingSummary(msg, 'original')).toBe(false) // showing original content
  })

  it('simulates the full ensureMessageContent lifecycle: load -> failure', () => {
    // Step 1: Initial state — summary view, blocks empty
    let msg = { summary: 'Summary', blocks: [], showingSummary: false }
    expect(isShowingSummary(msg, 'original')).toBe(true)

    // Step 2: Component sets _loadingOriginal=true
    msg = { ...msg, _loadingOriginal: true }
    expect(isShowingSummary(msg, 'original')).toBe(true)

    // Step 3: Fetch fails, blocks still empty, _loadingOriginal cleared,
    //         _loadAttempted=true
    msg = { ...msg, _loadingOriginal: false, _loadAttempted: true }
    expect(isShowingSummary(msg, 'original')).toBe(true) // summary stays as placeholder
  })
})

/**
 * Tests for the lazy-original scroll re-sync fix.
 *
 * Root cause: in original mode, messages with a summary arrive with blocks
 * stripped by the backend. ensureMessageContent fetches the full content
 * asynchronously. When the blocks arrive, the container height grows but the
 * browser keeps the old scrollTop — a force-scrolled view (session switch)
 * ends up visually stuck mid-list. The fix re-syncs scroll once after blocks
 * are filled.
 */
describe('ChatPanelContent — ensureMessageContent scroll re-sync', () => {
  it('source calls scrollBottom() after populating msg.blocks', async () => {
    const mod = await import('@/components/chat/ChatPanelContent.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    // The lazy fetch must re-sync scroll right after blocks are assigned,
    // so a force-scrolled view is pinned back to the bottom (isAtBottom=true)
    // while a manual toggle keeps the current reading position.
    const region = source.slice(source.indexOf('async function ensureMessageContent'), source.indexOf('async function handleRefreshSession'))
    expect(region).toContain('msg.blocks = blocks')
    expect(region).toMatch(/msg\.blocks = blocks[\s\S]*?scrollBottom\(\)/)
  })

  it('source calls scrollBottom() without force to respect user scroll position', async () => {
    const mod = await import('@/components/chat/ChatPanelContent.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    const region = source.slice(source.indexOf('async function ensureMessageContent'), source.indexOf('async function handleRefreshSession'))
    // Non-force scrollBottom keeps isAtBottom guard in ChatMessageList:
    // user at bottom (switch-back) → pinned; user reading earlier → position kept.
    expect(region).toMatch(/scrollBottom\(\)/)
    expect(region).not.toMatch(/scrollBottom\(true\)/)
  })
})

// ── Failed-send input restore ──
// When a message send fails (network disconnected / HTTP 5xx), the input box
// must NOT stay cleared — the text is restored so the user can retry.

describe('ChatPanelContent — failed send keeps input text', () => {
  async function sourceRegion(start: string, end: string) {
    const mod = await import('@/components/chat/ChatPanelContent.vue?raw')
    const source = typeof mod.default === 'string' ? mod.default : ''
    return source.slice(source.indexOf(start), source.indexOf(end))
  }

  it('captures inputText before clearing and restores it in the send catch block', async () => {
    const region = await sourceRegion('async function sendMessage(text)', 'async function sendMessageNow(text, filePaths, files)')
    // The current input text must be remembered so the catch path can restore it.
    expect(region).toMatch(/(?:let|const)\s+inputText\s*=/)
    // The direct-send failure path must restore the captured text instead of
    // leaving the box empty.
    expect(region).toMatch(/catch\s*(?:\([^)]*\))?\s*\{[\s\S]*?restoreInput\(inputText\)/)
  })

  it('restores input text when the enqueue request fails', async () => {
    const region = await sourceRegion('async function sendMessage(text)', 'async function sendMessageNow(text, filePaths, files)')
    // In the queue path, enqueueMessage returns false on failure — the input
    // must then be restored with the captured text.
    expect(region).toMatch(/enqueueAndMaybeStart\(/)
    expect(region).toMatch(/restoreInput\(inputText\)/)
    expect(region).toMatch(/enqueueMessage/)
  })
})
