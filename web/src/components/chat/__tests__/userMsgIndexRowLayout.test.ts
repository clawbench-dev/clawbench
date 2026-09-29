import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Conversation-index rows are a fixed-height list: every row shows exactly one
 * line (role chip · preview · time) and anything longer is clipped with an
 * ellipsis. jsdom has no CSS engine, so this is a source-contract check — the
 * same pattern chatMetaBarAlignment.test.ts / chatBoldStyle.test.ts use.
 *
 * The rows live in MessageIndexRow.vue (shared by UserMsgIndexDrawer and the
 * public conversation-share TOC), so the contract is asserted against that
 * component rather than either host.
 */
const source = readFileSync(resolve(__dirname, '../MessageIndexRow.vue'), 'utf8')
const scopedStart = source.lastIndexOf('<style scoped>')
const style = source.slice(scopedStart, source.indexOf('<style>', scopedStart))

/**
 * Declarations of the top-level rule whose selector is exactly `selector`
 * (a literal CSS selector, e.g. ".msg-text"). Anchored to a line start so a
 * descendant rule (".msg-item.active .msg-text") is never mistaken for the
 * standalone one.
 */
function decls(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = style.match(new RegExp(`\\n\\s*${escaped}\\s*\\{([^}]*)\\}`))
  expect(m, `${selector} rule must exist`).not.toBeNull()
  return m![1]
}

describe('UserMsgIndexDrawer: one-line rows', () => {
  it('keeps the row body a single horizontal flex line', () => {
    const body = decls('.msg-body')
    expect(body, 'the body must lay out on one axis').toMatch(/display:\s*flex/)
    // A column would stack the preview under the chip — the old two-line row.
    expect(body, 'the body must not stack into rows').not.toMatch(/flex-direction:\s*column/)
  })

  it('clips the preview to one line with an ellipsis', () => {
    const text = decls('.msg-text')
    expect(text).toMatch(/white-space:\s*nowrap/)
    expect(text).toMatch(/overflow:\s*hidden/)
    expect(text).toMatch(/text-overflow:\s*ellipsis/)
    // Wrapping would defeat nowrap; break-word belongs to the old multi-line row.
    expect(text).not.toMatch(/white-space:\s*pre-wrap/)
  })

  it('makes the preview the only element that gives up width', () => {
    // The chip and timestamp must stay whole; the preview absorbs the squeeze.
    expect(decls('.msg-role-tag')).toMatch(/flex-shrink:\s*0/)
    expect(decls('.msg-time')).toMatch(/flex-shrink:\s*0/)
    expect(decls('.msg-text')).toMatch(/min-width:\s*0/)
  })

  it('sizes the role chip as a square icon badge, not a text pill', () => {
    // The chip holds only an icon now; a padding-based pill would collapse to a
    // sliver and the row's leading column would no longer line up.
    const tag = decls('.msg-role-tag')
    expect(tag).toMatch(/width:\s*20px/)
    expect(tag).toMatch(/height:\s*20px/)
    expect(tag, 'a text pill would set horizontal padding').not.toMatch(/padding:\s*1px\s+6px/)
  })
})

describe('UserMsgIndexDrawer: icon-only role chips', () => {
  it('renders an icon per role and no role text', () => {
    // The chip must not fall back to a text label — that is what this replaced.
    expect(source).toMatch(/<Bot v-if="msg\.role === 'assistant'"/)
    expect(source).toMatch(/<User v-else/)
    expect(source, 'the old text label must be gone').not.toMatch(
      /msg-role-tag[\s\S]{0,200}conversationIndexRoleUser'\s*\}\}\s*<\/span>/,
    )
  })

  it('exposes the role as an accessible name for the icon', () => {
    // Icon-only controls are invisible to screen readers without this.
    // roleLabel is a computed in the shared row component (the message is a prop).
    expect(source).toMatch(/:aria-label="roleLabel"/)
    expect(source).toMatch(/const roleLabel = computed/)
  })
})

describe('UserMsgIndexDrawer: timeline rail', () => {
  it('draws a solid segment per row so consecutive rows join seamlessly', () => {
    const rail = decls('.msg-item::before')
    expect(rail).toMatch(/width:\s*2px/)
    expect(rail).toMatch(/top:\s*0/)
    expect(rail).toMatch(/bottom:\s*0/)
    // A per-row gradient fades the line at every row boundary — the rail then
    // reads as a row of disconnected dashes instead of one continuous line.
    expect(rail, 'the rail must not fade per row').not.toMatch(/linear-gradient/)
  })

  it('trims the rail to start and end on a node', () => {
    expect(decls('.msg-item:first-child::before')).toMatch(/top:\s*50%/)
    expect(decls('.msg-item:last-child::before')).toMatch(/bottom:\s*50%/)
    // The old rule hid the last row's segment outright, leaving the rail
    // dangling below the second-to-last node.
    expect(style, 'the last row must keep its rail').not.toMatch(
      /\.msg-item:last-child::before\s*\{[^}]*display:\s*none/,
    )
  })

  it('hides the rail when a single row has no neighbour to connect', () => {
    expect(style).toMatch(/\.msg-item:first-child:last-child::before\s*\{[^}]*display:\s*none/)
  })

  it('masks the rail behind the node with the row background', () => {
    // The node paints a ring in the row's own background colour; if that colour
    // does not track the row (hover/active) the node shows a mismatched halo.
    expect(decls('.msg-item')).toMatch(/--rail-punch:/)
    expect(decls('.msg-node')).toMatch(/box-shadow:\s*0 0 0 3px var\(--rail-punch\)/)
    expect(decls('.msg-item.active')).toMatch(/--rail-punch:/)
    expect(style).toMatch(/\.msg-item:hover\s*\{[^}]*--rail-punch:/)
  })
})
