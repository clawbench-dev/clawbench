import { describe, expect, it } from 'vitest'

/**
 * Tests for the block-merge logic used in TaskExecDetail.activeMsgData.
 *
 * When a scheduled task is running and the user opens its detail view,
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
 * Pure-function equivalent of the activeMsgData merge logic.
 * Extracted for testability — the actual implementation is in
 * TaskExecDetail.vue's computed property.
 */
function mergeStreamingWithHistory(
  isStreaming: boolean,
  streamingMsg: MsgLike | null,
  dbMsgData: MsgLike | null,
): MsgLike | null {
  if (isStreaming && streamingMsg) {
    if (streamingMsg.blocks && streamingMsg.blocks.length > 0) {
      const dbBlocks = dbMsgData?.blocks
      if (dbBlocks && dbBlocks.length > 0) {
        return { ...streamingMsg, blocks: [...dbBlocks, ...streamingMsg.blocks] }
      }
      return streamingMsg
    }
    // Streaming started but no WS content yet — keep DB history visible
    if (dbMsgData) return dbMsgData
  }
  // After streaming stops, merge streamingMsg blocks with DB history
  if (!isStreaming && streamingMsg) {
    if (streamingMsg.blocks && streamingMsg.blocks.length > 0) {
      const dbBlocks = dbMsgData?.blocks
      if (dbBlocks && dbBlocks.length > 0) {
        return { ...streamingMsg, blocks: [...dbBlocks, ...streamingMsg.blocks], streaming: false }
      }
      return { ...streamingMsg, streaming: false }
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
})
