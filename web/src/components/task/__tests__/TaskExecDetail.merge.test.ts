import { describe, expect, it } from 'vitest'
import { mergeDbBlocksWithLive } from '@/utils/chatBlocks'

/**
 * Tests for the block-merge logic used in TaskExecDetail.activeMsgData.
 *
 * When a task is running and the user opens its detail view,
 * the streaming message (WS incremental blocks) must be merged with
 * the DB history blocks — NOT replace them. Otherwise the user sees
 * a flash: full history → only latest streaming output.
 */

interface MsgLike {
  blocks: Array<Record<string, unknown>>
  streaming?: boolean
  [key: string]: unknown
}

/**
 * The merge is exercised through the SAME function the component calls.
 * An earlier version of this file kept a hand-copied duplicate of the logic,
 * which meant it could not fail when the component's real implementation
 * regressed — a duplicate that only proves it agrees with itself.
 */
function mergeStreamingWithHistory(
  isStreaming: boolean,
  streamingMsg: MsgLike | null,
  dbMsgData: MsgLike | null,
): MsgLike | null {
  if (isStreaming && streamingMsg) {
    if (streamingMsg.blocks && streamingMsg.blocks.length > 0) {
      const dbBlocks = dbMsgData?.blocks
      if (!dbBlocks || dbBlocks.length === 0) return streamingMsg
      return { ...streamingMsg, blocks: mergeDbBlocksWithLive(dbBlocks, streamingMsg.blocks) }
    }
    if (dbMsgData) return dbMsgData
  }
  if (!isStreaming && streamingMsg) {
    if (streamingMsg.blocks && streamingMsg.blocks.length > 0) {
      const dbBlocks = dbMsgData?.blocks
      if (!dbBlocks || dbBlocks.length === 0) return { ...streamingMsg, streaming: false }
      return { ...streamingMsg, blocks: mergeDbBlocksWithLive(dbBlocks, streamingMsg.blocks), streaming: false }
    }
  }
  return dbMsgData
}

describe('mergeStreamingWithHistory', () => {
  it('returns DB history when streaming has no blocks yet', () => {
    const dbMsg: MsgLike = {
      blocks: [{ type: 'text', text: 'Previous output' }],
      streaming: false,
    }
    const streamingMsg: MsgLike = {
      blocks: [],
      streaming: true,
    }

    const result = mergeStreamingWithHistory(true, streamingMsg, dbMsg)
    expect(result).toBe(dbMsg) // Same reference — no flash
  })

  it('merges DB history + streaming blocks when both have content', () => {
    const dbMsg: MsgLike = {
      blocks: [{ type: 'text', text: 'History text' }],
      streaming: false,
    }
    const streamingMsg: MsgLike = {
      blocks: [{ type: 'text', text: 'New streaming text' }],
      streaming: true,
    }

    const result = mergeStreamingWithHistory(true, streamingMsg, dbMsg)
    expect(result!.blocks).toEqual([
      { type: 'text', text: 'History text' },
      { type: 'text', text: 'New streaming text' },
    ])
    expect(result!.streaming).toBe(true)
  })

  it('returns streaming-only blocks when no DB history exists', () => {
    const streamingMsg: MsgLike = {
      blocks: [{ type: 'text', text: 'Streaming only' }],
      streaming: true,
    }

    const result = mergeStreamingWithHistory(true, streamingMsg, null)
    expect(result).toBe(streamingMsg)
  })

  it('returns null when no streaming and no DB history', () => {
    const result = mergeStreamingWithHistory(true, null, null)
    expect(result).toBeNull()
  })

  it('merges after streaming stops (fallback before refresh)', () => {
    const dbMsg: MsgLike = {
      blocks: [{ type: 'text', text: 'History' }],
      streaming: false,
    }
    const streamingMsg: MsgLike = {
      blocks: [{ type: 'text', text: 'Last chunk' }],
      // streaming flag already removed by stopPreview
    }

    const result = mergeStreamingWithHistory(false, streamingMsg, dbMsg)
    expect(result!.blocks).toEqual([
      { type: 'text', text: 'History' },
      { type: 'text', text: 'Last chunk' },
    ])
    expect(result!.streaming).toBe(false)
  })

  it('returns DB-only content when not streaming and no streamingMsg', () => {
    const dbMsg: MsgLike = {
      blocks: [{ type: 'text', text: 'DB content' }],
      streaming: false,
    }

    const result = mergeStreamingWithHistory(false, null, dbMsg)
    expect(result).toBe(dbMsg)
  })

  it('preserves tool_use blocks from both history and streaming', () => {
    const dbMsg: MsgLike = {
      blocks: [
        { type: 'tool_use', name: 'ReadFile', id: 't1', done: true },
        { type: 'text', text: 'File contents...' },
      ],
      streaming: false,
    }
    const streamingMsg: MsgLike = {
      blocks: [
        { type: 'tool_use', name: 'WriteFile', id: 't2', done: false },
      ],
      streaming: true,
    }

    const result = mergeStreamingWithHistory(true, streamingMsg, dbMsg)
    expect(result!.blocks).toHaveLength(3)
    expect(result!.blocks[0]).toEqual({ type: 'tool_use', name: 'ReadFile', id: 't1', done: true })
    expect(result!.blocks[1]).toEqual({ type: 'text', text: 'File contents...' })
    expect(result!.blocks[2]).toEqual({ type: 'tool_use', name: 'WriteFile', id: 't2', done: false })
  })

  it('drops a DB thinking marker whose think_id the live stream already carries', () => {
    // Regression: the DB row is flushed every 500ms while the turn runs, so its
    // content carries a slim {think_id} marker for the SAME block the live
    // stream is appending to. Concatenating both emitted one think_id twice,
    // and the renderer keys thinking blocks by think_id — a duplicate v-for key
    // corrupts Vue's keyed diff.
    const dbMsg: MsgLike = {
      blocks: [
        { type: 'text', text: 'earlier' },
        { type: 'thinking', think_id: 'th_same', in_progress: true },
      ],
      streaming: false,
    }
    const streamingMsg: MsgLike = {
      blocks: [{ type: 'thinking', text: 'live deltas', think_id: 'th_same' }],
      streaming: true,
    }

    const result = mergeStreamingWithHistory(true, streamingMsg, dbMsg)!
    const keys = result.blocks
      .filter((b: any) => b.type === 'thinking' && b.think_id)
      .map((b: any) => b.think_id)
    expect(new Set(keys).size, 'think_id must not repeat').toBe(keys.length)

    // The DB marker is dropped, the live block (with the real text) survives,
    // and the earlier non-thinking history is kept.
    expect(result.blocks).toHaveLength(2)
    expect(result.blocks[0]).toEqual({ type: 'text', text: 'earlier' })
    expect(result.blocks[1]).toMatchObject({ type: 'thinking', think_id: 'th_same', text: 'live deltas' })
  })

  it('keeps a DB thinking marker the live stream does NOT have', () => {
    // A block that finished before the placeholder was recreated must survive —
    // only duplicates are dropped, not history.
    const dbMsg: MsgLike = {
      blocks: [{ type: 'thinking', think_id: 'th_old', done: true }],
      streaming: false,
    }
    const streamingMsg: MsgLike = {
      blocks: [{ type: 'thinking', text: 'new block', think_id: 'th_new' }],
      streaming: true,
    }
    const result = mergeStreamingWithHistory(true, streamingMsg, dbMsg)!
    expect(result.blocks).toHaveLength(2)
    expect(result.blocks[0]).toMatchObject({ think_id: 'th_old' })
    expect(result.blocks[1]).toMatchObject({ think_id: 'th_new' })
  })

  it('passes plain text history through untouched', () => {
    // No thinking ids involved: the merge must behave exactly as before.
    const dbMsg: MsgLike = {
      blocks: [{ type: 'text', text: 'a' }, { type: 'text', text: 'b' }],
      streaming: false,
    }
    const streamingMsg: MsgLike = {
      blocks: [{ type: 'text', text: 'c' }],
      streaming: true,
    }
    const result = mergeStreamingWithHistory(true, streamingMsg, dbMsg)!
    expect(result.blocks).toEqual([
      { type: 'text', text: 'a' },
      { type: 'text', text: 'b' },
      { type: 'text', text: 'c' },
    ])
  })
})
