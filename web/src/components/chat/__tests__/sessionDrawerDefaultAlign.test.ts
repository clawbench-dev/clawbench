import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The "默认" badge / set-default star must sit flush against the row's right
 * edge in EVERY tab of the session settings drawer — not directly after the
 * option label.
 *
 * Measured in a real browser (Chrome, 800px panel, row padding 14px):
 *
 *   tab    non-default star right   default badge right   row right
 *   模型   786                      786                   786   ✓
 *   思考    84                      132                   786   ✗
 *   模式   126                      211                   786   ✗
 *   协议   110                      141                   786   ✗
 *
 * The model tab happened to be correct by accident: its label lives inside
 * `.model-item-labels { flex: 1 }`, which absorbs the free space and shoves the
 * trailing slot to the right. The thinking / mode / transport tabs render a
 * bare `.model-item-name` with no growth, so the badge/star just trails the
 * text wherever it ends.
 *
 * The fix right-aligns the trailing slot itself (`margin-left: auto`), which is
 * independent of what precedes it — so it holds for all four tabs.
 *
 * jsdom has no CSS engine, so this is a source-contract check (the same pattern
 * chatMetaBarAlignment.test.ts / chatInputTypeScale.test.ts use).
 */
describe('session settings drawer: default badge is right-aligned in every tab', () => {
  const source = readWebFile('src/components/chat/SessionDrawer.vue')

  /** Extract the declaration block of a single class rule. */
  function ruleBlock(selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const m = source.match(new RegExp(`(?:^|\\n)\\s*${escaped}\\s*\\{([\\s\\S]*?)\\}`))
    expect(m, `rule ${selector} should exist`).not.toBeNull()
    return m![1]
  }

  it.each([
    ['default badge', '.default-label'],
    ['set-default star', '.set-default-btn'],
  ])('%s right-aligns itself in the row', (_name, selector) => {
    const block = ruleBlock(selector)
    // `margin-left: auto` is what pushes the trailing slot to the right edge
    // regardless of the label width. Without it the badge trails the text.
    expect(block, `${selector} must right-align via auto margin`).toMatch(
      /margin-left:\s*auto/,
    )
    // It must also never shrink, or a long label would squeeze it and break the
    // shared right edge.
    expect(block, `${selector} must not shrink`).toMatch(/flex-shrink:\s*0/)
  })

  it('keeps the trailing slot a direct child of the flex row, outside the label wrapper', () => {
    // `margin-left: auto` only reaches the right edge if the trailing slot is a
    // direct child of the flex row (`.model-item` / `.thinking-item`). If a
    // refactor moves it inside `.model-item-labels`, the auto margin would push
    // it away from the *wrapper's* end instead, and the badge would drift back
    // next to the text. Assert the labels wrapper holds only the name/id.
    const labelsBlocks = source.match(
      /<span class="model-item-labels">([\s\S]*?)<\/span>\s*(?=<span v-if=|<button v-if=)/g,
    ) || []
    expect(labelsBlocks.length, 'label wrapper markup should be found').toBeGreaterThan(0)
    for (const block of labelsBlocks) {
      expect(block, 'labels wrapper must not contain the default badge').not.toContain('default-label')
      expect(block, 'labels wrapper must not contain the set-default star').not.toContain('set-default-btn')
    }
  })
})
