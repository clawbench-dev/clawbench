import { describe, expect, it, vi } from 'vitest'
import {
  buildMessageSnapshot,
  parseMessages,
  applySummaryUpdate,
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

  // ── showingSummary auto-set for assistant messages with summary ──

  it('sets showingSummary=true for assistant messages with non-empty summary', () => {
    const msgs = [
      { role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }), summary: 'A brief summary' },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].showingSummary).toBe(true)
  })

  it('sets showingSummary=false for assistant messages with empty summary', () => {
    const msgs = [
      { role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }), summary: '' },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].showingSummary).toBe(false)
  })

  it('sets showingSummary=false for assistant messages with null summary', () => {
    const msgs = [
      { role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }), summary: null },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].showingSummary).toBe(false)
  })

  it('sets showingSummary=false for assistant messages without summary field', () => {
    const msgs = [
      { role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }) },
    ]
    const result = parseMessages(msgs, mockParser)
    expect(result[0].showingSummary).toBe(false)
  })

  // ── parseMessages preserves existing showingSummary state ──

  it('preserves showingSummary=false from existingMessages when summary exists', () => {
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

  it('preserves showingSummary=true from existingMessages when summary exists', () => {
    const rawMsgs = [
      { id: 'm1', role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }), summary: 'A summary' },
    ]
    const existing = [
      { id: 'm1', showingSummary: true },
    ]
    const result = parseMessages(rawMsgs, mockParser, existing)
    expect(result[0].showingSummary).toBe(true)
  })

  it('sets default showingSummary=true when no existingMessages and summary exists', () => {
    const rawMsgs = [
      { id: 'm1', role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }), summary: 'A summary' },
    ]
    const result = parseMessages(rawMsgs, mockParser)
    expect(result[0].showingSummary).toBe(true)
  })

  it('sets default showingSummary when existingMessages has undefined showingSummary', () => {
    const rawMsgs = [
      { id: 'm1', role: 'assistant', content: JSON.stringify({ blocks: [{ type: 'text', text: 'Hello' }] }), summary: 'A summary' },
    ]
    // existingMessages exists but this message has no showingSummary set yet
    const existing = [
      { id: 'm1' },
    ]
    const result = parseMessages(rawMsgs, mockParser, existing)
    // showingSummary is undefined in existing, so use default (true for messages with summary)
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
      // m3 not in existing (new message)
    ]
    const result = parseMessages(rawMsgs, mockParser, existing)
    expect(result[0].showingSummary).toBe(false)  // Preserved user toggle
    expect(result[1].showingSummary).toBe(true)   // Preserved
    expect(result[2].showingSummary).toBe(true)   // Default for new message with summary
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
    applySummaryUpdate(msg, 'A summary', true)
    expect(msg.summary).toBe('A summary')
  })

  it('stores null summary on the message', () => {
    const msg: any = { id: '1', showingSummary: true }
    applySummaryUpdate(msg, null, true)
    expect(msg.summary).toBeNull()
  })

  it('sets showingSummary=true when non-empty summary arrives and showingSummary is undefined', () => {
    const msg = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, 'Summary text', true)
    expect(msg.showingSummary).toBe(true)
  })

  it('sets showingSummary=false when summary is empty and showingSummary is undefined', () => {
    const msg = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, '', true)
    expect(msg.showingSummary).toBe(false)
  })

  it('sets showingSummary=false when summary is null and showingSummary is undefined', () => {
    const msg = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, null, true)
    expect(msg.showingSummary).toBe(false)
  })

  it('does not depend on atBottom', () => {
    const msg = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, 'Summary text', false)
    expect(msg.showingSummary).toBe(true)
  })

  it('handles undefined summary', () => {
    const msg: any = { id: '1', showingSummary: undefined }
    applySummaryUpdate(msg, undefined, true)
    expect(msg.showingSummary).toBe(false)
    expect(msg.summary).toBeUndefined()
  })

  it('does not override showingSummary when already set to true', () => {
    const msg = { id: '1', showingSummary: true, summary: 'Old' }
    applySummaryUpdate(msg, 'Updated summary', true)
    expect(msg.showingSummary).toBe(true)
    expect(msg.summary).toBe('Updated summary')
  })

  it('does not override showingSummary when already set to false', () => {
    const msg = { id: '1', showingSummary: false, summary: 'Old' }
    applySummaryUpdate(msg, 'New summary', true)
    expect(msg.showingSummary).toBe(false)
    expect(msg.summary).toBe('New summary')
  })
})
