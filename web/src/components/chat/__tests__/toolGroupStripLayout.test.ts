import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The folded-run row (compact tool calls) has a tail — cancelled/stopped mark,
 * status icon, chevron — pinned to its right edge.
 *
 * It used to be flat children relying on whatever preceded them claiming the
 * free space (`margin-left: auto` on the status icon, `flex: 1` on the running
 * summary). While ONLY a thinking chip ran — no summary, no status icon — the
 * chevron sat next to the text, and pinning the chevron alone instead stranded
 * the green check there. The fix is a tail CONTAINER that owns the free space;
 * jsdom applies no SFC styles, so this is a source guard.
 */
function cssBlock(selector: string): string {
  const src = readWebFile('src/components/chat/ContentBlocks.vue')
  const idx = src.indexOf(selector)
  expect(idx, `${selector} must exist`).toBeGreaterThan(-1)
  const close = src.indexOf('}', src.indexOf('{', idx))
  return src.slice(idx, close + 1)
}

describe('folded-run row layout', () => {
  it('pins the whole tail to the right edge', () => {
    expect(cssBlock('.chat-tool-group-tail')).toMatch(/margin-left:\s*auto/)
  })

  it('keeps the tail children from splitting that space', () => {
    // Same specificity as `.chat-tool-call .tool-check` etc., and later in the
    // file, so the reset actually wins.
    const block = cssBlock('.chat-tool-group-tail .tool-spinner')
    for (const cls of ['.tool-spinner', '.tool-check', '.tool-warn', '.tool-error-icon', '.tool-group-chevron', '.chat-cancelled-mark-inline']) {
      expect(block).toContain(cls)
    }
    expect(block).toMatch(/margin-left:\s*0/)
  })

  it('gives the row the deep-thinking purple while its thinking chip streams', () => {
    // Not a tool category: it only colours the row, and must match the chip.
    expect(cssBlock('.chat-tool-call[data-category="thinking"]')).toMatch(/--tool-accent:\s*#8b5cf6/)
  })
})
