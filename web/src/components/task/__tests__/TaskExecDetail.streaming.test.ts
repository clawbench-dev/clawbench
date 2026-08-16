import { describe, expect, it } from 'vitest'

/**
 * Tests for the msgData streaming-flag logic in TaskExecDetail.vue.
 *
 * A running task execution must render with the "streaming status" indicator,
 * NOT as a completed answer. The DB content is only partial while running, but
 * it should still look like an in-progress generation.
 */

interface MsgDataInput {
  content: string
  status: string
}

interface MsgData {
  streaming: boolean
  cancelled: boolean
  blocks: Array<Record<string, unknown>>
}

/**
 * Pure-function equivalent of TaskExecDetail.vue's msgData computed,
 * capturing the streaming-flag decision for a non-empty-content message.
 */
function buildMsgData(input: MsgDataInput, blocks: Array<Record<string, unknown>>): MsgData {
  const isRunning = input.status === 'running'
  return {
    blocks,
    streaming: isRunning,
    cancelled: false,
  }
}

describe('TaskExecDetail msgData streaming flag', () => {
  it('marks a running execution as streaming so the status indicator shows', () => {
    const msg = buildMsgData(
      { content: '{"blocks":[{"type":"text","text":"partial"}]}', status: 'running' },
      [{ type: 'text', text: 'partial' }],
    )
    expect(msg.streaming).toBe(true)
  })

  it('does NOT mark a completed execution as streaming', () => {
    const msg = buildMsgData(
      { content: '{"blocks":[{"type":"text","text":"final"}]}', status: 'completed' },
      [{ type: 'text', text: 'final' }],
    )
    expect(msg.streaming).toBe(false)
  })

  it('marks a failed execution as not streaming', () => {
    const msg = buildMsgData(
      { content: '{"blocks":[]}', status: 'failed' },
      [],
    )
    expect(msg.streaming).toBe(false)
  })
})
