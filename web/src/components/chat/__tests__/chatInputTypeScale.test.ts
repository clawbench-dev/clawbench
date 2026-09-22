import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The chat input must read as part of the conversation, not as a separate and
 * larger surface. Concretely: every textarea that mirrors the chat input bar
 * (.chat-textarea, .qq-textarea) uses the SAME type scale as the message body
 * (.chat-message: --font-size-md + --line-height-snug).
 *
 * (The in-app completion card used to be a third member of this family. It is
 * now a plain notification with no textarea at all, so it is no longer listed —
 * see CompletionPopover.vue.)
 *
 * Why a source-sniffing spec instead of computed styles: jsdom does not resolve
 * var(), so a component-level assertion can only prove *which token name* was
 * written. It cannot catch the regression that actually happened — the input
 * being pinned to the heading/input token (--font-size-2xl) while the body
 * stayed at --font-size-md. Pinning the declarations closes that gap.
 *
 * Height caps are intentionally derived from the same line box
 * (calc(1em * var(--line-height-snug) * N + padding)) rather than a hard-coded
 * px value, so retuning the token cannot silently desync the cap from the text.
 */
describe('chat input type scale matches the message body', () => {
  const inputBar = readWebFile('src/components/chat/ChatInputBar.vue')
  const messageItem = readWebFile('src/components/chat/ChatMessageItem.vue')
  const quoteBar = readWebFile('src/components/common/QuoteQuestionBar.vue')

  /** Extract the declaration block of a single class rule. */
  function ruleBlock(source: string, selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const m = source.match(new RegExp(`(?:^|\\n)\\s*${escaped}\\s*\\{([\\s\\S]*?)\\}`))
    expect(m, `rule ${selector} should exist`).not.toBeNull()
    return m![1]
  }

  it('sizes the message body from the default-UI-text token', () => {
    const body = ruleBlock(messageItem, '.chat-message')
    expect(body).toMatch(/font-size:\s*var\(--font-size-md\)/)
    expect(body).toMatch(/line-height:\s*var\(--line-height-snug\)/)
  })

  it.each([
    ['chat input bar', () => inputBar, '.chat-textarea'],
    ['quote bar', () => quoteBar, '.qq-textarea'],
  ])('%s textarea uses the same type scale as the body', (_name, source, selector) => {
    const block = ruleBlock(source(), selector)
    expect(block).toMatch(/font-size:\s*var\(--font-size-md\)/)
    // The pre-fix scale: 16px text in a fixed 20px line box.
    expect(block).not.toMatch(/font-size:\s*var\(--font-size-2xl\)/)
    expect(block).not.toMatch(/line-height:\s*20px/)
  })

  it.each([
    ['chat input bar', () => inputBar, '.chat-textarea'],
    ['quote bar', () => quoteBar, '.qq-textarea'],
  ])('%s textarea uses the INTEGER line box, not the unitless ratio', (_name, source, selector) => {
    const block = ruleBlock(source(), selector)
    // A unitless --line-height-snug resolves to 13px × 1.4 = 18.2px. Android
    // WebView rounds a form control's line box and content-box height
    // independently, so at a fractional line box the two roundings stop
    // cancelling and a single line of text sits visibly high. Only the integer
    // token keeps the caret centred; desktop Chrome hides the bug by rounding
    // both the same way.
    expect(block).toMatch(/line-height:\s*var\(--input-line-height\)/)
    expect(block).not.toMatch(/line-height:\s*var\(--line-height-snug\)/)
    expect(block).not.toMatch(/line-height:\s*[\d.]+em/)
  })

  it('keeps --input-line-height a whole number of px', () => {
    // The integer constraint is the entire point of the token, and nothing else
    // in the design system enforces it — a retune to e.g. 1.4em or 18.2px would
    // silently reintroduce the WebView drift.
    const vars = readWebFile('css/variables.css')
    const m = vars.match(/--input-line-height:\s*([^;]+);/)
    expect(m, 'token --input-line-height should be defined').not.toBeNull()
    const raw = m![1].trim()
    expect(raw).toMatch(/^\d+px$/)
  })

  it.each([
    ['chat input bar', () => inputBar, '.chat-textarea', 10],
    ['quote bar', () => quoteBar, '.qq-textarea', 3],
  ])('%s derives its height caps from the line box, not fixed px', (_name, source, selector, lines) => {
    const block = ruleBlock(source(), selector)
    // A hard-coded min-height would no longer track the token if it is retuned.
    expect(block).not.toMatch(/min-height:\s*\d+px/)
    expect(block).not.toMatch(/max-height:\s*calc\(20px/)
    expect(block).toContain(`calc(var(--input-line-height) * ${lines}`)
  })

  it.each([
    ['chat input bar', () => inputBar, '.chat-input-row'],
    ['quote bar', () => quoteBar, '.qq-input-row'],
  ])('%s row has symmetric vertical padding', (_name, source, selector) => {
    // These rows are `align-items: flex-end`, so a top/bottom padding difference
    // shows up directly as the icon buttons sitting off-centre against the
    // textarea — the controls share a baseline, not a centre. The original
    // 4px/6px (and 2px/4px) asymmetry was invisible while the buttons were
    // larger; it became obvious once they shrank to 26px squares.
    const block = ruleBlock(source(), selector)
    const padding = block.match(/padding:\s*([^;]+);/)?.[1].trim()
    expect(padding, `${selector} should declare padding`).toBeTruthy()
    const parts = padding!.split(/\s+/)
    const resolve = (v: string) => {
      const token: Record<string, number> = {
        'var(--space-1)': 2, 'var(--space-2)': 4, 'var(--space-3)': 6, 'var(--space-4)': 8,
      }
      if (v in token) return token[v]
      const px = v.match(/^(\d+)px$/)
      return px ? Number(px[1]) : NaN
    }
    // 2-value shorthand (top/bottom + left/right) is inherently symmetric.
    if (parts.length === 2) {
      expect(resolve(parts[0])).not.toBeNaN()
      return
    }
    expect(parts, `${selector} padding should be 2 or 3 values`).toHaveLength(3)
    expect(resolve(parts[0]), `${selector} top vs bottom padding`).toBe(resolve(parts[2]))
  })
})
