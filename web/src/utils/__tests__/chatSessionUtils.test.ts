import { describe, expect, it, vi } from 'vitest'
import {
  buildMessageSnapshot,
  parseMessages,
  applySummaryUpdate,
  shouldShowSummary,
  isShowingSummary,
} from '@/utils/chatSessionUtils.ts'

// ── buildMessageSnapshot ──

describe('buildMessageSnapshot', () => {
  it('creates fingerprint from message properties', () => {
    const msgs = [
      { id: '1', role: 'user', content: 'hello', createdAt: '2026-01-01T00:00:00Z', streaming: false },
    ]
    expect(buildMessageSnapshot(msgs)).toBe('1:user:5:2026-01-01T00:00:00Z:0')
  })

  it('handles missing id', () => {
    const msgs = [
      { role: 'user', content: 'hi', createdAt: '2026-01-01', streaming: false },
    ]
    expect(buildMessageSnapshot(msgs)).toBe(':user:2:2026-01-01:0')
  })

  it('handles empty content', () => {
    const msgs = [
      { id: '2', role: 'assistant', content: '', createdAt: '', streaming: true },
    ]
    expect(buildMessageSnapshot(msgs)).toBe('2:assistant:0::1')
  })

  it('handles multiple messages', () => {
    const msgs = [
      { id: '1', role: 'user', content: 'hello', createdAt: '2026-01-01', streaming: false },
      { id: '2', role: 'assistant', content: 'world', createdAt: '2026-01-01', streaming: false },
    ]
    expect(buildMessageSnapshot(msgs)).toBe('1:user:5:2026-01-01:0|2:assistant:5:2026-01-01:0')
  })

  it('returns empty for empty array', () => {
    expect(buildMessageSnapshot([])).toBe('')
  })

  it('detects content length changes', () => {
    const msgs1 = [{ id: '1', role: 'user', content: 'hi', createdAt: '2026-01-01', streaming: false }]
    const msgs2 = [{ id: '1', role: 'user', content: 'hello', createdAt: '2026-01-01', streaming: false }]
    expect(buildMessageSnapshot(msgs1)).not.toBe(buildMessageSnapshot(msgs2))
  })

  it('detects streaming flag change', () => {
    const msgs1 = [{ id: '1', role: 'assistant', content: '', createdAt: '', streaming: false }]
    const msgs2 = [{ id: '1', role: 'assistant', content: '', createdAt: '', streaming: true }]
    expect(buildMessageSnapshot(msgs1)).not.toBe(buildMessageSnapshot(msgs2))
  })

  it('detects role change', () => {
    const msgs1 = [{ id: '1', role: 'user', content: 'hi', createdAt: '2026-01-01', streaming: false }]
    const msgs2 = [{ id: '1', role: 'assistant', content: 'hi', createdAt: '2026-01-01', streaming: false }]
    expect(buildMessageSnapshot(msgs1)).not.toBe(buildMessageSnapshot(msgs2))
  })

  it('detects id change', () => {
    const msgs1 = [{ id: '1', role: 'user', content: 'hi', createdAt: '2026-01-01', streaming: false }]
    const msgs2 = [{ id: '2', role: 'user', content: 'hi', createdAt: '2026-01-01', streaming: false }]
    expect(buildMessageSnapshot(msgs1)).not.toBe(buildMessageSnapshot(msgs2))
  })

  it('detects createdAt change', () => {
    const msgs1 = [{ id: '1', role: 'user', content: 'hi', createdAt: '2026-01-01', streaming: false }]
    const msgs2 = [{ id: '1', role: 'user', content: 'hi', createdAt: '2026-01-02', streaming: false }]
    expect(buildMessageSnapshot(msgs1)).not.toBe(buildMessageSnapshot(msgs2))
  })

  it('produces stable output for identical input', () => {
    const msgs = [{ id: '1', role: 'user', content: 'hello', createdAt: '2026-01-01', streaming: false }]
    expect(buildMessageSnapshot(msgs)).toBe(buildMessageSnapshot(msgs))
  })

  it('handles null content', () => {
    const msgs = [
      { id: '1', role: 'user', content: null, createdAt: '2026-01-01', streaming: false },
    ]
    // (null || '') = '', length is 0
    expect(buildMessageSnapshot(msgs)).toContain(':0:')
  })

  it('handles undefined content', () => {
    const msgs = [
      { id: '1', role: 'user', content: undefined, createdAt: '2026-01-01', streaming: false },
    ]
    expect(buildMessageSnapshot(msgs)).toContain(':0:')
  })

  it('handles very long content (only checks length)', () => {
    const longContent = 'x'.repeat(10000)
    const msgs = [
      { id: '1', role: 'user', content: longContent, createdAt: '2026-01-01', streaming: false },
    ]
    expect(buildMessageSnapshot(msgs)).toContain(':10000:')
  })
})

// ── parseMessages ──

describe('parseMessages', () => {
  const mockParser = (content: string) => {
    if (!content) return { blocks: [], metadata: null, cancelled: false }
    try {
      const parsed = JSON.parse(content)
      if (parsed.blocks) return { blocks: parsed.blocks, metadata: parsed.metadata || null, cancelled: parsed.cancelled || false }
    } catch {}
    return { blocks: [{ type: 'text', text: content }], metadata: null, cancelled: false }
  }

  it('parses assistant messages with blocks', () => {
    const msgs = [
      { role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }) },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].blocks).toEqual([{ type: 'text', text: 'Hello' }])
  })

  it('parses user messages into text blocks', () => {
    const msgs = [
      { role: 'user', content: 'Hello AI' },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].blocks).toEqual([{ type: 'text', text: 'Hello AI' }])
  })

  it('creates empty blocks for user messages with no content', () => {
    const msgs = [
      { role: 'user', content: '' },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].blocks).toEqual([])
  })

  it('preserves user blocks if already present', () => {
    const msgs = [
      { role: 'user', content: 'Hello', blocks: [{ type: 'text', text: 'Hello' }] },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].blocks).toEqual([{ type: 'text', text: 'Hello' }])
  })

  it('marks streaming assistant messages as fromDB', () => {
    const msgs = [
      { role: 'assistant', content: '', streaming: true },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].fromDB).toBe(true)
    expect(result[0].streaming).toBe(true)
  })

  it('does not mark non-streaming messages as fromDB', () => {
    const msgs = [
      { role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Done' }] }) },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].fromDB).toBeUndefined()
  })

  it('handles mixed user and assistant messages', () => {
    const msgs = [
      { role: 'user', content: 'Question' },
      { role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Answer' }] }) },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result).toHaveLength(2)
    expect(result[0].blocks[0].text).toBe('Question')
    expect(result[1].blocks[0].text).toBe('Answer')
  })

  it('extracts metadata from assistant content', () => {
    const msgs = [
      { role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hi' }], metadata: { tokens: 50 } }) },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].metadata).toEqual({ tokens: 50 })
  })

  it('extracts cancelled flag from assistant content', () => {
    const msgs = [
      { role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'partial' }], cancelled: true }) },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].cancelled).toBe(true)
  })

  it('handles empty array', () => {
    expect(parseMessages([], mockParser)).toEqual([])
  })

  it('preserves other message properties', () => {
    const msgs = [
      { role: 'user', content: 'Hello', id: 'msg-1', createdAt: '2026-01-01' },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].id).toBe('msg-1')
    expect(result[0].createdAt).toBe('2026-01-01')
  })

  it('delegates to the parser function', () => {
    const customParser = vi.fn().mockReturnValue({ blocks: [{ type: 'text', text: 'custom' }], metadata: null, cancelled: false })
    const msgs = [
      { role: 'assistant', content: 'test content' },
    ]
    parseMessages(msgs, customParser)
    expect(customParser).toHaveBeenCalledWith('test content')
  })

  it('handles user message with null content', () => {
    const msgs = [
      { role: 'user', content: null },
    ]
    const result = parseMessages(msgs, customParser)
    expect(result[0].blocks).toEqual([])
  })

  it('handles user message with non-string content (no blocks field)', () => {
    const msgs = [
      { role: 'user', content: 42 },
    ]
    const result = parseMessages(msgs, mockParser)
    // content is 42 (number), (42 || '') = 42 (truthy), so blocks = [{ type: 'text', text: 42 }]
    // But actually msg.content ? [{ type: 'text', text: msg.content }] : []
    // 42 is truthy, so blocks = [{ type: 'text', text: 42 }]
    expect(result[0].blocks).toEqual([{ type: 'text', text: 42 }])
  })

  it('unwraps bare content-array JSON user messages', () => {
    const msgs = [
      { role: 'user', content: JSON.stringify([{ type: 'text', text: '数组用户消息' }]) },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].blocks).toEqual([{ type: 'text', text: '数组用户消息' }])
  })

  it('unwraps ACP notification wrapper JSON user messages', () => {
    const msgs = [
      { role: 'user', content: JSON.stringify({ content: { text: '通知用户消息', type: 'text' }, messageId: 'm1', sessionUpdate: 'user_message_chunk' }) },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].blocks).toEqual([{ type: 'text', text: '通知用户消息' }])
  })

  it('unwraps nested ACP notification in a text block of user messages', () => {
    const msgs = [
      {
        role: 'user',
        content: JSON.stringify({
          blocks: [
            {
              text: JSON.stringify({ content: { text: '嵌套消息', type: 'text' }, messageId: 'm1', sessionUpdate: 'user_message_chunk' }),
              type: 'text',
            },
          ],
        }),
      },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].blocks).toEqual([{ type: 'text', text: '嵌套消息' }])
  })

  it('keeps user message with blocks already parsed intact', () => {
    const msgs = [
      { role: 'user', content: '原内容', blocks: [{ type: 'text', text: '已解析' }] },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].blocks).toEqual([{ type: 'text', text: '已解析' }])
  })

  // ── parseMessages: showingSummary only stores the user's explicit preference ──
  // The field stays undefined until the user toggles; parseMessages preserves an
  // existing preference but never derives a default boolean. The render decision
  // is made by shouldShowSummary().

  it('leaves showingSummary undefined when no existing preference and summary exists', () => {
    const msgs = [
      { id: 'm1', role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }), summary: 'A brief summary' },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].showingSummary).toBeUndefined()
  })

  it('preserves showingSummary=false from existingMessages', () => {
    const rawMsgs = [
      { id: 'm1', role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }), summary: 'A summary' },
    ]
    // User toggled to view original (showingSummary=false)
    const existing = [
      { id: 'm1', showingSummary: false },
    ]
    const result = parseMessages(rawMsgs, mockParser, existing)
    expect(result[0].showingSummary).toBe(false)
  })

  it('preserves showingSummary=true from existingMessages', () => {
    const rawMsgs = [
      { id: 'm1', role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }), summary: 'A summary' },
    ]
    const existing = [
      { id: 'm1', showingSummary: true },
    ]
    const result = parseMessages(rawMsgs, mockParser, existing)
    expect(result[0].showingSummary).toBe(true)
  })

  it('preserves showingSummary for multiple messages independently', () => {
    const rawMsgs = [
      { id: 'm1', role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'A' }] }), summary: 'Summary A' },
      { id: 'm2', role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'B' }] }), summary: 'Summary B' },
      { id: 'm3', role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'C' }] }), summary: 'Summary C' },
    ]
    const existing = [
      { id: 'm1', showingSummary: false },  // User toggled to original
      { id: 'm2', showingSummary: true },   // Still showing summary
      // m3 not in existing (new message) → stays undefined
    ]
    const result = parseMessages(rawMsgs, mockParser, existing)
    expect(result[0].showingSummary).toBe(false)  // Preserved user toggle
    expect(result[1].showingSummary).toBe(true)   // Preserved
    expect(result[2].showingSummary).toBeUndefined() // No explicit preference
  })

  // ── shouldShowSummary: the render decision ──

  it('shouldShowSummary returns false when there is no summary', () => {
    expect(shouldShowSummary({ summary: '', blocks: [{ type: 'text', text: 'x' }] })).toBe(false)
    expect(shouldShowSummary({ summary: null, blocks: [{ type: 'text', text: 'x' }] })).toBe(false)
    expect(shouldShowSummary({ blocks: [{ type: 'text', text: 'x' }] })).toBe(false)
  })

  it('shouldShowSummary respects global default mode when user has no explicit preference', () => {
    // default 'summary' preserves current behavior
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }] })).toBe(true)
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }] }, 'summary')).toBe(true)
    // global original mode → show full text
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }] }, 'original')).toBe(false)
  })

  it('shouldShowSummary with original default and stripped content returns false to trigger lazy load', () => {
    expect(shouldShowSummary({ summary: 'sum', blocks: [] }, 'original')).toBe(false)
    expect(shouldShowSummary({ summary: 'sum', blocks: [], showingSummary: undefined }, 'original')).toBe(false)
  })

  it('shouldShowSummary with summary default and stripped content returns true', () => {
    expect(shouldShowSummary({ summary: 'sum', blocks: [] })).toBe(true)
    expect(shouldShowSummary({ summary: 'sum', blocks: [] }, 'summary')).toBe(true)
  })

  it('shouldShowSummary keeps explicit preference overriding global original mode', () => {
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }], showingSummary: true }, 'original')).toBe(true)
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }], showingSummary: false }, 'summary')).toBe(false)
  })

  it('shouldShowSummary keeps forcing summary for stripped content with explicit original preference', () => {
    // Regression preserved: stream interrupted → summary generated async after
    // showingSummary=false; stripped content (blocks empty) must still show summary.
    expect(shouldShowSummary({ summary: 'late sum', blocks: [], showingSummary: false }, 'original')).toBe(true)
  })

  // ── isShowingSummary: the latch-aware UI decision ──

  it('isShowingSummary keeps summary visible while content is being lazily fetched', () => {
    expect(isShowingSummary({ summary: 'sum', blocks: [], _loadingOriginal: true }, 'original')).toBe(true)
  })

  it('isShowingSummary keeps summary visible after a load attempt ended without content', () => {
    expect(isShowingSummary({ summary: 'sum', blocks: [], _loadAttempted: true }, 'original')).toBe(true)
  })

  it('isShowingSummary releases the placeholder once content is available', () => {
    expect(isShowingSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'Full' }], _loadAttempted: true }, 'original')).toBe(false)
  })

  it('isShowingSummary falls back to shouldShowSummary when no load is pending or attempted', () => {
    expect(isShowingSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }] }, 'original')).toBe(false)
    expect(isShowingSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }] }, 'summary')).toBe(true)
    expect(isShowingSummary({ summary: 'sum', blocks: [], showingSummary: true }, 'original')).toBe(true)
    expect(isShowingSummary({ blocks: [{ type: 'text', text: 'x' }] }, 'summary')).toBe(false)
  })

  it('shouldShowSummary returns true when summary exists and user has no explicit preference (content present)', () => {
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }] })).toBe(true)
  })

  it('shouldShowSummary respects explicit preference to view original when content present', () => {
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }], showingSummary: false })).toBe(false)
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }], showingSummary: true })).toBe(true)
  })

  it('shouldShowSummary forces true when content stripped (blocks empty) even if user preferred original', () => {
    // Regression: after a stream is interrupted, the summary is generated
    // asynchronously AFTER the message was marked showingSummary=false. On reload
    // with view=summary the content is stripped (blocks empty), so forcing original
    // view would render an empty bubble.
    expect(shouldShowSummary({ summary: 'late sum', blocks: [], showingSummary: false })).toBe(true)
  })

  it('shouldShowSummary respects global mode after showingSummary is cleared', () => {
    // When the global display mode changes (e.g. from original to summary),
    // the ChatPanelContent watch clears all per-message showingSummary
    // preferences. After clearing, the global defaultMode must take effect.
    const msg: Record<string, unknown> = { summary: 'sum', blocks: [{ type: 'text', text: 'x' }], showingSummary: false }
    // Before clearing: explicit preference overrides global
    expect(shouldShowSummary(msg, 'summary')).toBe(false)
    // Simulate the watch clearing the preference
    delete msg.showingSummary
    // After clearing: global default takes effect
    expect(shouldShowSummary(msg, 'summary')).toBe(true)
    expect(shouldShowSummary(msg, 'original')).toBe(false)
  })

  // ── sessionRunning parameter: strip stale streaming for completed sessions ──

  it('strips streaming from assistant message when sessionRunning=false', () => {
    const msgs = [
      { role: 'assistant', content: '', streaming: true },
    ]
    const result = parseMessages(msgs, mockParser, undefined, false)
    expect(result[0].streaming).toBeUndefined()
    expect(result[0].fromDB).toBeUndefined()
  })

  it('preserves streaming on assistant message when sessionRunning=true', () => {
    const msgs = [
      { role: 'assistant', content: '', streaming: true },
    ]
    const result = parseMessages(msgs, mockParser, undefined, true)
    expect(result[0].streaming).toBe(true)
    expect(result[0].fromDB).toBe(true)
  })

  it('preserves streaming on assistant message when sessionRunning is undefined (backward compat)', () => {
    const msgs = [
      { role: 'assistant', content: '', streaming: true },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].streaming).toBe(true)
    expect(result[0].fromDB).toBe(true)
  })

  it('always strips streaming from user messages', () => {
    const msgs = [
      { role: 'user', content: 'Hello', streaming: true },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].streaming).toBeUndefined()
  })

  it('strips streaming from user messages even when sessionRunning=true', () => {
    const msgs = [
      { role: 'user', content: 'Hello', streaming: true },
    ]
    const result = parseMessages(msgs, mockParser, undefined, true)
    expect(result[0].streaming).toBeUndefined()
  })

  it('handles mixed messages with sessionRunning=false', () => {
    const msgs = [
      { role: 'user', content: 'Question' },
      { role: 'assistant', content: '', streaming: true },  // stale streaming from crash
      { role: 'user', content: 'Follow up', streaming: true },  // user msg with stale streaming
    ]
    const result = parseMessages(msgs, mockParser, undefined, false)
    expect(result[0].blocks).toEqual([{ type: 'text', text: 'Question' }])
    expect(result[1].streaming).toBeUndefined()  // stripped for completed session
    expect(result[2].streaming).toBeUndefined()  // always stripped for user messages
  })
})

function customParser(content: string) {
  if (!content) return { blocks: [], metadata: null, cancelled: false }
  return { blocks: [{ type: 'text', text: content }], metadata: null, cancelled: false }
}

// ── applySummaryUpdate ──

describe('applySummaryUpdate', () => {
  it('stores summary on the message', () => {
    const msg: any = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, 'A summary', null, true)
    expect(msg.summary).toBe('A summary')
  })

  it('stores null summary on the message', () => {
    const msg: any = { id: '1', showingSummary: true }
    applySummaryUpdate(msg, null, null, true)
    expect(msg.summary).toBeNull()
  })

  it('does not set showingSummary when summary arrives and it is undefined', () => {
    // applySummaryUpdate stores the summary but never touches showingSummary,
    // which records only the user's explicit preference. The render decision is
    // made by shouldShowSummary().
    const msg: any = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, 'Summary text', null, true)
    expect(msg.summary).toBe('Summary text')
    expect(msg.showingSummary).toBeUndefined()
  })

  it('keeps showingSummary undefined when summary is empty', () => {
    const msg: any = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, '', null, true)
    expect(msg.summary).toBe('')
    expect(msg.showingSummary).toBeUndefined()
  })

  it('keeps showingSummary undefined when summary is null', () => {
    const msg: any = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, null, null, true)
    expect(msg.summary).toBeNull()
    expect(msg.showingSummary).toBeUndefined()
  })

  it('handles undefined summary', () => {
    const msg: any = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, undefined, null, true)
    expect(msg.summary).toBeUndefined()
    expect(msg.showingSummary).toBeUndefined()
  })

  it('does not override showingSummary when already set to true', () => {
    const msg = { id: '1', showingSummary: true, summary: 'Old' }
    applySummaryUpdate(msg, 'Updated summary', null, true)
    expect(msg.showingSummary).toBe(true)
    expect(msg.summary).toBe('Updated summary')
  })

  it('does not override showingSummary when already set to false', () => {
    const msg = { id: '1', showingSummary: false, summary: 'Old' }
    applySummaryUpdate(msg, 'New summary', null, true)
    expect(msg.showingSummary).toBe(false)
    expect(msg.summary).toBe('New summary')
  })
})

// ── summaryCards parsing ──

describe('summaryCards parsing', () => {
  it('attachs summaryCards from raw message', () => {
    const raw = [{
      id: 1, role: 'assistant', content: '{"blocks":[]}',
      summary: 'sum', summaryCards: { tools: [{ name: 'Bash', id: 't1' }], taskIDs: [1], askQuestions: [] },
    }]
    const msgs = parseMessages(raw, () => ({ blocks: [] }), [], true)
    expect((msgs[0] as any).summaryCards).toBeTruthy()
    expect((msgs[0] as any).summaryCards.tools[0].name).toBe('Bash')
  })

  it('applySummaryUpdate stores cards', () => {
    const msg: any = { id: 1, role: 'assistant', blocks: [] }
    applySummaryUpdate(msg, 'sum', { tools: [{ name: 'AskUserQuestion' }] }, true)
    expect(msg.summary).toBe('sum')
    expect(msg.summaryCards.tools[0].name).toBe('AskUserQuestion')
  })
})
