import { describe, expect, it } from 'vitest'

/**
 * Tests for the cancelled-marker visibility logic in ContentBlocks.vue.
 *
 * Bug #208: When the last content block is a thinking block and the message was
 * cancelled, the "已中断" (cancelled) mark was visually hidden/trapped under
 * the collapsed thinking chip (28px max-height). The fix renders the cancelled
 * mark inline inside the thinking header when the last block is thinking,
 * instead of placing it below all blocks.
 *
 * The core logic is:
 * - isLastBlock(bi): bi === blocks.length - 1
 * - isLastBlockThinking: blocks[blocks.length - 1]?.type === 'thinking'
 * - Inline cancelled mark visible: !streaming && isLastBlock(bi) && cancelled
 * - Outer cancelled mark visible: cancelled && !isLastBlockThinking
 */

describe('isLastBlock logic', () => {
  /** Replicates the isLastBlock function from ContentBlocks.vue */
  function isLastBlock(bi: number, blocks: any[]): boolean {
    return bi === (blocks?.length || 0) - 1
  }

  it('returns true for the last block index', () => {
    const blocks = [{ type: 'text' }, { type: 'thinking' }]
    expect(isLastBlock(1, blocks)).toBe(true)
  })

  it('returns false for non-last block indices', () => {
    const blocks = [{ type: 'text' }, { type: 'thinking' }]
    expect(isLastBlock(0, blocks)).toBe(false)
  })

  it('returns false for empty blocks array', () => {
    expect(isLastBlock(0, [])).toBe(false)
  })

  it('returns true for single-element array at index 0', () => {
    const blocks = [{ type: 'thinking' }]
    expect(isLastBlock(0, blocks)).toBe(true)
  })
})

describe('isLastBlockThinking logic', () => {
  /** Replicates the isLastBlockThinking computed from ContentBlocks.vue */
  function isLastBlockThinking(blocks: any[]): boolean {
    if (!blocks || blocks.length === 0) return false
    return blocks[blocks.length - 1].type === 'thinking'
  }

  it('returns true when the last block is thinking', () => {
    const blocks = [{ type: 'text' }, { type: 'thinking' }]
    expect(isLastBlockThinking(blocks)).toBe(true)
  })

  it('returns false when the last block is not thinking', () => {
    const blocks = [{ type: 'text' }, { type: 'tool_use' }]
    expect(isLastBlockThinking(blocks)).toBe(false)
  })

  it('returns false for empty array', () => {
    expect(isLastBlockThinking([])).toBe(false)
  })

  it('returns false for null/undefined', () => {
    expect(isLastBlockThinking(null as any)).toBe(false)
    expect(isLastBlockThinking(undefined as any)).toBe(false)
  })

  it('returns true for single thinking block', () => {
    const blocks = [{ type: 'thinking' }]
    expect(isLastBlockThinking(blocks)).toBe(true)
  })
})

describe('cancelled marker visibility (bug #208)', () => {
  /**
   * Simulates the template logic:
   * - Inline cancelled mark (in thinking header):
   *     !streaming && isLastBlock(bi) && cancelled && block.type === 'thinking'
   * - Outer cancelled mark (below all blocks):
   *     cancelled && !isLastBlockThinking
   */
  function getCancelledMarkerState(
    blocks: any[],
    cancelled: boolean,
    streaming: boolean,
  ) {
    const lastIdx = blocks.length - 1
    const lastIsThinking =
      blocks.length > 0 && blocks[lastIdx]?.type === 'thinking'

    // Inline: shown in thinking header when last block is thinking and cancelled
    const showInline =
      cancelled && !streaming && lastIsThinking && lastIdx >= 0

    // Outer: shown below all blocks, but NOT when last is thinking (to avoid duplicate)
    const showOuter = cancelled && !lastIsThinking

    return { showInline, showOuter }
  }

  it('shows inline mark when last block is thinking and message is cancelled', () => {
    const blocks = [{ type: 'text' }, { type: 'thinking' }]
    const { showInline, showOuter } = getCancelledMarkerState(
      blocks,
      true, /* cancelled */
      false, /* streaming */
    )
    // Bug #208 fix: inline mark visible in thinking header
    expect(showInline).toBe(true)
    // Outer mark hidden to avoid duplication
    expect(showOuter).toBe(false)
  })

  it('shows outer mark when last block is not thinking and message is cancelled', () => {
    const blocks = [{ type: 'text' }, { type: 'tool_use' }]
    const { showInline, showOuter } = getCancelledMarkerState(
      blocks,
      true, /* cancelled */
      false, /* streaming */
    )
    expect(showInline).toBe(false)
    expect(showOuter).toBe(true)
  })

  it('shows no mark when message is not cancelled', () => {
    const blocks = [{ type: 'thinking' }]
    const { showInline, showOuter } = getCancelledMarkerState(
      blocks,
      false, /* cancelled */
      false, /* streaming */
    )
    expect(showInline).toBe(false)
    expect(showOuter).toBe(false)
  })

  it('does not show inline mark while streaming (spinner visible instead)', () => {
    const blocks = [{ type: 'thinking' }]
    const { showInline, showOuter } = getCancelledMarkerState(
      blocks,
      true, /* cancelled */
      true, /* streaming — shouldn't happen normally but test the guard */
    )
    // Inline mark guarded by !streaming
    expect(showInline).toBe(false)
  })

  it('shows outer mark for text-only message that was cancelled', () => {
    const blocks = [{ type: 'text', text: 'Hello' }]
    const { showInline, showOuter } = getCancelledMarkerState(
      blocks,
      true, /* cancelled */
      false, /* streaming */
    )
    expect(showInline).toBe(false)
    expect(showOuter).toBe(true)
  })
})
