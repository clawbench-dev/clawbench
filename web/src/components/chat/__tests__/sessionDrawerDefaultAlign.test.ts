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
 * The model tab happened to be correct by accident: its label lives inside a
 * `flex: 1` wrapper (now the shared ModelRow component), which absorbs the free
 * space and shoves the trailing slot to the right. The thinking / mode /
 * transport tabs render a bare `.model-item-name` with no growth, so the
 * badge/star just trails the text wherever it ends.
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

  it('keeps the trailing slot a direct child of the flex row, outside the model row', () => {
    // `margin-left: auto` only reaches the right edge if the trailing slot is a
    // direct child of the flex row (`.model-item` / `.thinking-item`). The model
    // tab now delegates its icon + name/id to the shared ModelRow component
    // (which is `flex: 1`), so the badge/star must remain SIBLINGS of that
    // component inside `.model-item` — not passed into its slot, and not part of
    // the model row. A refactor that moved them inside would push the auto
    // margin away from the row's end and drift the badge back next to the text.
    const modelItem = source.match(/<button\s+class="model-item"[\s\S]*?<\/button>/)?.[0] ?? ''
    expect(modelItem, '.model-item markup should be found').not.toBe('')
    expect(modelItem, 'model tab must use the shared ModelRow').toContain('<ModelRow')
    // The default badge / star are direct children of `.model-item`, after the
    // ModelRow element (which is self-closing, so they are not its slot content).
    expect(modelItem, 'default badge must be a direct child of the row').toMatch(
      /<ModelRow[^>]*\/>\s*<span v-if="m\.id === defaultModelId" class="default-label">/,
    )
    expect(modelItem, 'set-default star must be a direct child of the row').toMatch(
      /<button v-if="m\.id !== defaultModelId" class="set-default-btn"/,
    )
  })
})
