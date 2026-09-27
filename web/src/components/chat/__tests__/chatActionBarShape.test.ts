import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The chat Action Bar's running-session signal is a sweep travelling over a
 * faint accent TINT — and the tint is what makes the rounded box work.
 *
 * History, because the pieces only make sense together: the button paints no
 * fill at rest, so its radius used to be invisible except on hover/active. The
 * sweep, however, is a straight-edged band clipped by `overflow: hidden`, so in
 * a rounded box its leading and trailing corners got cut and it read as a
 * truncated stripe. Squaring the button fixed that but broke the shape language
 * shared by every other button. The shipped resolution keeps the radius AND
 * gives the running state a 10% accent fill, so the band now travels across a
 * surface and its clipped corners read as the light entering/leaving the chip.
 *
 * Three things have to stay true together, which is why they are pinned here:
 *   1. the base rule is rounded (a token, not a literal 0);
 *   2. the running state carries the tint (without it, back to a cut stripe);
 *   3. the running state still answers hover (its tint outranks the generic
 *      `.chat-action-btn:hover`, so the hover rule would silently stop working).
 *
 * This is the SCOPED rule in ChatInputBar.vue, deliberately separate from the
 * global `.chat-action-btn` in `assets/chat-actions.css` (which styles the
 * message meta-bar buttons — those have no sweep). Editing the global rule
 * would restyle every copy/info button on every message.
 *
 * jsdom does not resolve var()/color-mix() and has no layout engine, so this is
 * a source-contract check — the same pattern chatInputTypeScale.test.ts uses.
 */
describe('chat Action Bar running signal: rounded box + accent tint + sweep', () => {
  const source = readWebFile('src/components/chat/ChatInputBar.vue')
  // The scoped block. The second (non-scoped) <style> block holds only the
  // quick-send menu styles.
  const scoped = source.slice(0, source.lastIndexOf('<style>'))

  /**
   * Declarations of a rule whose selector starts a line.
   *
   * Leading whitespace is allowed so rules nested in an at-rule (the hover
   * rules live inside `@media (hover: hover)`) are still found; requiring `{`
   * right after the selector keeps `.chat-action-btn` from matching
   * `.chat-action-btn:hover`.
   */
  function rule(selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const m = scoped.match(new RegExp(`\\n\\s*${escaped}\\s*\\{([\\s\\S]*?)\\n\\s*\\}`))
    expect(m, `rule ${selector} should exist in the scoped block`).not.toBeNull()
    return m![1]
  }

  it('rounds the base button from a radius token', () => {
    const base = rule('.chat-action-btn')
    expect(base).toMatch(/border-radius:\s*var\(--radius-/)
    // The literal 0 was the previous (reverted) decision — a square box.
    expect(base, 'a literal 0 would square the button again').not.toMatch(
      /border-radius:\s*0(?:px)?\s*;/,
    )
  })

  it('gives the running state an accent tint', () => {
    const running = rule('.chat-action-btn.has-running')
    // The tint is the whole reason the rounded box reads correctly; without it
    // the clipped sweep corners come back as a truncated stripe.
    expect(running, 'the running state must paint a surface for the sweep').toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--accent-color[^)]*\)\s*10%,\s*transparent\)/,
    )
  })

  it('keeps the sweep clipped to the button box', () => {
    const running = rule('.chat-action-btn.has-running')
    expect(running).toMatch(/overflow:\s*hidden/)
  })

  it('does not round the sweep back through a descendant rule', () => {
    const sweep = rule('.chat-action-btn.has-running::before')
    expect(sweep, 'the sweep band must not round its own corners').not.toMatch(
      /border-radius/,
    )
  })

  it('keeps the running state hoverable', () => {
    // `.chat-action-btn.has-running` (0,2,0) outranks the plain
    // `.chat-action-btn:hover` (0,1,1) in the same sheet, so the generic hover
    // fill can no longer reach a running button. Without an explicit hover rule
    // the button goes inert to the pointer exactly while a session is running.
    const hover = rule('.chat-action-btn.has-running:hover')
    expect(hover, 'the running state needs its own hover fill').toMatch(
      /background:\s*color-mix\(/,
    )
  })
})
