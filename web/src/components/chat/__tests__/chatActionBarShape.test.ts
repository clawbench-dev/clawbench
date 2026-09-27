import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The chat Action Bar buttons are SQUARE, and that is load-bearing for the
 * running-session sweep.
 *
 * The buttons paint no fill at rest (`background: none`), so their radius only
 * ever becomes visible on hover/active — which is why a rounded box survived
 * for so long. The running-session button is what exposes it: it draws a
 * travelling band across its own box (`.chat-action-btn.has-running::before`,
 * a `linear-gradient` clipped by `overflow: hidden`). A rounded box clips that
 * band's leading and trailing corners, so the "sweep of light" reads as a
 * truncated stripe with two cut corners instead of a band running edge to edge.
 *
 * Note this is the SCOPED rule in ChatInputBar.vue, deliberately separate from
 * the global `.chat-action-btn` in `assets/chat-actions.css` (which still rounds
 * the message meta-bar buttons — those have no sweep). Changing the global rule
 * would silently square every copy button on every message.
 *
 * jsdom does not resolve var() and has no layout engine, so this is a
 * source-contract check — the same pattern chatInputTypeScale.test.ts uses.
 */
describe('chat Action Bar buttons are square', () => {
  const source = readWebFile('src/components/chat/ChatInputBar.vue')
  // The scoped block — the Action Bar's own geometry. The second (non-scoped)
  // <style> block holds only the quick-send menu styles.
  const scoped = source.slice(0, source.lastIndexOf('<style>'))

  /** Declarations of a rule whose selector starts a line. */
  function rule(selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const m = scoped.match(new RegExp(`\\n${escaped}\\s*\\{([\\s\\S]*?)\\n\\}`))
    expect(m, `rule ${selector} should exist in the scoped block`).not.toBeNull()
    return m![1]
  }

  it('gives the base action button zero radius instead of a token', () => {
    const base = rule('.chat-action-btn')
    // A literal 0 is the design system's way of stating "this one is square on
    // purpose" — the radius tokens are all non-zero.
    expect(base).toMatch(/border-radius:\s*0(?:px)?\s*;/)
    expect(base, 'a radius token would round the box again').not.toMatch(
      /border-radius:\s*var\(--radius-/,
    )
  })

  it('keeps the sweep clipped to the button box', () => {
    // The band is a pseudo-element wider than the travel path; without the clip
    // it would spill into the neighbouring buttons. This is the other half of
    // the square-corner contract, so it is pinned alongside it.
    const running = rule('.chat-action-btn.has-running')
    expect(running).toMatch(/overflow:\s*hidden/)
  })

  it('does not round the sweep back through a descendant rule', () => {
    // A descendant selector (e.g. `.chat-action-btn.has-running::before`)
    // carrying its own radius would reintroduce the cut corners without
    // touching the base rule.
    const sweep = rule('.chat-action-btn.has-running::before')
    expect(sweep, 'the sweep band must not round its own corners').not.toMatch(
      /border-radius/,
    )
  })
})
