import { describe, expect, it } from 'vitest'
import {
  CHAT_HISTORY_FRAME_CHARS,
  CHAT_HISTORY_MAX,
  CHAT_MAX_CHARS,
} from '../liveControl'
import { chunkChatHistory, mergeChatHistory, toChatWire } from '../chatHistory'
import type { ChatMessage } from '../LiveChatPanel.vue'

function line(n: number, over: Partial<ChatMessage> = {}): ChatMessage {
  return { id: `m${n}`, from: 'v1', name: '小明', text: `第 ${n} 句`, at: n, self: false, ...over }
}

describe('chunkChatHistory', () => {
  it('keeps a short transcript in one frame', () => {
    const frames = chunkChatHistory([line(1), line(2)])
    expect(frames).toHaveLength(1)
    expect(frames[0].map((m) => m.id)).toEqual(['m1', 'm2'])
  })

  it('replays only the tail of a long meeting', () => {
    const all = Array.from({ length: CHAT_HISTORY_MAX + 20 }, (_, i) => line(i))
    const frames = chunkChatHistory(all)
    const ids = frames.flat().map((m) => m.id)
    expect(ids).toHaveLength(CHAT_HISTORY_MAX)
    // The newest lines are the ones a returning viewer needs.
    expect(ids[ids.length - 1]).toBe(`m${CHAT_HISTORY_MAX + 19}`)
    expect(ids).not.toContain('m0')
  })

  it('splits by accumulated length so no frame can be rejected for size', () => {
    // 2000-char lines: a fixed message count would build a frame well past the
    // parser's 64 KiB ceiling.
    const heavy = Array.from({ length: 40 }, (_, i) => line(i, { text: 'x'.repeat(CHAT_MAX_CHARS) }))
    const frames = chunkChatHistory(heavy)
    expect(frames.length).toBeGreaterThan(1)
    for (const f of frames) {
      expect(JSON.stringify({ t: 'chat-history', messages: f }).length).toBeLessThan(64 * 1024)
      expect(JSON.stringify(f).length).toBeLessThanOrEqual(CHAT_HISTORY_FRAME_CHARS)
    }
    // Nothing is lost in the split.
    expect(frames.flat()).toHaveLength(40)
  })

  it('carries the sender so the receiver can decide what is its own', () => {
    expect(toChatWire(line(1))).toEqual({ id: 'm1', from: 'v1', name: '小明', text: '第 1 句', at: 1 })
  })
})

describe('mergeChatHistory', () => {
  const label = (from: string) => `观众 ${from}`

  it('prepends older lines in time order', () => {
    // A rejoin gets a fresh room id, so the lines replayed from the previous
    // session are someone else's as far as this panel is concerned.
    const live = [line(3, { from: 'v1', self: true })]
    const merged = mergeChatHistory(live, [line(1, { from: 'v2' }), line(2, { from: 'host' })], 'v1', label)
    expect(merged.map((m) => m.id)).toEqual(['m1', 'm2', 'm3'])
    expect(merged[0].self).toBe(false)
    expect(merged[2].self).toBe(true)
  })

  it('drops lines it already has instead of duplicating them', () => {
    const existing = [line(1), line(2)]
    const merged = mergeChatHistory(existing, [line(1), line(2), line(3)], 'v1', label)
    expect(merged.map((m) => m.id)).toEqual(['m1', 'm2', 'm3'])
  })

  it('returns the same array when nothing is new', () => {
    const existing = [line(1)]
    expect(mergeChatHistory(existing, [line(1)], 'v1', label)).toBe(existing)
    expect(mergeChatHistory(existing, [], 'v1', label)).toBe(existing)
  })

  it('marks the viewer\'s own earlier lines by sender id', () => {
    const merged = mergeChatHistory([], [line(1, { from: 'v9' })], 'v9', label)
    expect(merged[0].self).toBe(true)
    // A line with no sender (the host's, in the archive shape) is never "self".
    const host = mergeChatHistory([], [line(2, { from: '' })], '', label)
    expect(host[0].self).toBe(false)
  })

  it('falls back to the slot label when a replayed line carries no name', () => {
    const merged = mergeChatHistory([], [line(1, { name: '', from: 'v4' })], 'v1', label)
    expect(merged[0].name).toBe('观众 v4')
  })
})
