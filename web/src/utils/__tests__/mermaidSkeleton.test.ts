import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The Mermaid loading placeholder is a diagram-shaped skeleton, not a ring.
 *
 * Why this is pinned at the source level
 * --------------------------------------
 * The placeholder markup is injected with `innerHTML` from a util and is
 * replaced the moment `mermaid.render()` resolves, so a component test can only
 * ever observe it for one tick — and jsdom has no layout engine, so the two
 * properties that matter (the shape reads as a fork; the edges actually meet
 * the nodes) cannot be measured at all.
 *
 * What the previous implementation cost: a 20px ring centred in a 660×68 box.
 * It said "waiting" but nothing about what was coming, and the box was sized by
 * its own padding rather than by anything diagram-like — the same defect that
 * `PanelSkeleton` / `ChatSkeleton` fixed for panels and the message area.
 *
 * The geometry is derived from PERCENTAGES shared between the row and its
 * connector, so these assertions read the same numbers. That is deliberate: the
 * first implementation mixed `calc(dp)` widths with a `max-width` clamp, the
 * clamp moved the real child centre to 26.9% while the connector was drawn from
 * 25%, and the fork visibly missed its nodes.
 */

const css = readWebFile('src/assets/mermaid.css').replace(/\/\*[\s\S]*?\*\//g, '')
const mermaidTs = readWebFile('src/utils/mermaid.ts')

/**
 * The declaration block of the first rule whose selector ENDS with `selector`.
 *
 * Anchored on the selector's end so `.mermaid-skeleton-node` cannot match
 * `.mermaid-skeleton-node--root` (whose block differs), and so a `:hover`
 * variant of the same class is not mistaken for the base rule.
 */
function blockOf(source: string, selector: string): string | null {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const re = new RegExp(`(?:^|\\n)[^\\n{}]*${escaped}\\s*\\{([\\s\\S]*?)\\}`)
  const m = re.exec(source)
  return m ? m[1] : null
}

describe('mermaid loading placeholder markup', () => {
  it('injects a skeleton, not a bare spinner', () => {
    // The ring may legitimately still exist elsewhere in the app; what must not
    // come back is a ring as the WHOLE placeholder.
    expect(mermaidTs, 'the loading placeholder must render a skeleton').toContain('mermaid-skeleton')
    expect(
      mermaidTs,
      'the placeholder must not be a lone ring in an empty box',
    ).not.toMatch(/mermaid-loading"><span class="li-spinner/)
  })

  it('renders one node, a fork of two, and a merge', () => {
    // The shape is the point: a stack of bars reads as a paragraph skeleton, and
    // the reader must recognise a diagram before it arrives.
    expect(mermaidTs).toContain('mermaid-skeleton-node--root')
    expect(mermaidTs).toContain('mermaid-skeleton-row')
    expect(mermaidTs).toContain('mermaid-skeleton-node--sink')
    // Two children in the row: the row's markup is built from two `node()`
    // calls between its open and close tag.
    const row = mermaidTs.match(/'<span class="mermaid-skeleton-row">'([\s\S]*?)'<\/span>'/)
    expect(row, 'the row must be a single markup segment').not.toBeNull()
    expect(row![1].match(/node\(\)/g)?.length, 'the row needs exactly two children').toBe(2)
  })

  it('puts a label bar inside every node', () => {
    // An empty box reads as a divider; a box with text in it reads as a node.
    const node = mermaidTs.match(/const node = [\s\S]*?\n\s*return '<div/)
    expect(node, 'the node builder must exist').not.toBeNull()
    expect(node![0]).toContain('mermaid-skeleton-label')
  })

  it('announces the load and hides the decoration', () => {
    // Every visible bar is aria-hidden, so without the one status line a screen
    // reader walking into the diagram position hears nothing for those seconds.
    expect(mermaidTs).toMatch(/role="status"/)
    expect(mermaidTs).toContain('media.diagramLoading')
    expect(mermaidTs).toMatch(/mermaid-skeleton" aria-hidden="true"/)
  })
})

describe('mermaid loading placeholder geometry (mermaid.css contract)', () => {
  it('derives the connector from the same percentages as the children', () => {
    // Children are 38% wide with an 8% gap → centres at 27% and 73%. The
    // connector must be drawn from those exact numbers; mixing in a dp width
    // (with a clamp) is what made the fork miss its nodes.
    const row = blockOf(css, '.mermaid-skeleton-row')
    expect(row, 'the row rule must exist').not.toBeNull()
    expect(row!).toMatch(/gap:\s*8%/)
    expect(row!).toMatch(/width:\s*100%/)

    const child = blockOf(css, '.mermaid-skeleton-row > .mermaid-skeleton-node')
    expect(child, 'the child width rule must exist').not.toBeNull()
    expect(child!, 'children must be a percentage so the centre is stable').toMatch(/width:\s*38%/)
    expect(child!, 'a max-width would move the real centre').not.toMatch(/max-width/)

    // The horizontal spans between the two centres: left 27% / right 27%.
    const horizontal = css.match(/\.mermaid-skeleton-row::before,[\s\S]*?\.mermaid-skeleton-row::after\s*\{([\s\S]*?)\}/)
    expect(horizontal, 'the fork/merge rule must exist').not.toBeNull()
    expect(horizontal![1]).toMatch(/left:\s*27%/)
    expect(horizontal![1]).toMatch(/right:\s*27%/)
  })

  it('anchors each vertical to the node it leaves, at half the gap', () => {
    // The verticals are owned by the nodes (root ↓, sink ↑, children's own
    // stubs), so their length is exactly the gap and the two cannot drift. A
    // full-gap stub on top of a gap-sized margin would overshoot by 2×.
    const root = css.match(/\.mermaid-skeleton-node--root::after,[\s\S]*?\.mermaid-skeleton-node--sink::before\s*\{([\s\S]*?)\}/)
    expect(root, 'the root/sink stub rule must exist').not.toBeNull()
    expect(root![1]).toMatch(/height:\s*calc\(var\(--mm-link\)\s*\/\s*2\)/)
    expect(root![1]).toMatch(/left:\s*50%/)

    // The row's margin is the same --mm-link, so the horizontal lands on the
    // gap's midline where both halves meet.
    const row = blockOf(css, '.mermaid-skeleton-row')
    expect(row!).toMatch(/margin:\s*var\(--mm-link\)\s+0/)
  })

  it('draws the two child stubs from the midline down to their own edge', () => {
    const childStubs = css.match(/\.mermaid-skeleton-row > \.mermaid-skeleton-node::before,[\s\S]*?\.mermaid-skeleton-row > \.mermaid-skeleton-node::after\s*\{([\s\S]*?)\}/)
    expect(childStubs, 'the child stub rule must exist').not.toBeNull()
    expect(childStubs![1]).toMatch(/height:\s*calc\(var\(--mm-link\)\s*\/\s*2\)/)
    expect(childStubs![1]).toMatch(/left:\s*50%/)
  })

  it('consumes the shared skeleton variables instead of declaring them', () => {
    // Same rule as `.skeleton-block`: declaring them here makes every ancestor
    // override a silent no-op.
    const node = blockOf(css, '.mermaid-skeleton-node')
    expect(node, 'the node rule must exist').not.toBeNull()
    expect(node!).toMatch(/var\(--skeleton-fill/)
    expect(node!).toMatch(/skeleton-shimmer/)
    expect(node!, 'must not declare the variables it consumes').not.toMatch(/--skeleton-(fill|sheen)\s*:/)
  })

  it('stops the shimmer but keeps the placeholder under reduced motion', () => {
    const block = /@media \(prefers-reduced-motion: reduce\) \{([\s\S]*?)\n\}/.exec(css)
    expect(block, 'a reduced-motion block must exist').not.toBeNull()
    expect(block![1]).toMatch(/animation:\s*none/)
  })

  it('does not reintroduce a spinner alongside the skeleton', () => {
    // A spinner and a skeleton answer the same question; showing both reads as
    // two things happening.
    expect(css).not.toContain('mermaid-spinner')
  })
})
