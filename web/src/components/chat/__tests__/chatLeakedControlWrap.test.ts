import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Leaked form/XML control wrapping guard.
 *
 * A malformed <ask-question> payload — e.g. an <option> with no <question> —
 * fails isValidAskContent(), so detectAskQuestion() reports not-found and the
 * raw XML is NOT stripped. It falls through to markdown, marked passes the tags
 * through, and DOMPurify keeps them (measured: option, optgroup, select,
 * textarea, label, fieldset, legend, header). Those become REAL form elements
 * inside the chat bubble.
 *
 * The UA stylesheet gives <option> `white-space: nowrap` (<select> gets `pre`),
 * so a long option label lays out as one unbreakable line. Measured in headless
 * Chrome at 390px with the real compiled bundle:
 *
 *   <option> prose label (157 chars)  -> 485px  past the bubble
 *   <option> 600-char URL             -> 5394px past the bubble
 *
 * .chat-message.assistant is `overflow: hidden`, so the excess is clipped
 * silently and cannot be scrolled to — the reported "one very long line running
 * off the screen" symptom.
 *
 * jsdom has no CSS engine, so this is a source-contract check (the same pattern
 * chatBoldStyle.test.ts and dockedPaneStacking.css.test.ts use).
 */
describe('chat bubble: leaked form/XML controls must still wrap', () => {
  const source = readFileSync(
    resolve(__dirname, '../ChatMessageItem.vue'),
    'utf8',
  )
  // The non-scoped <style> block — it is the one that penetrates v-html output.
  const globalStyle = source.slice(source.lastIndexOf('<style>'))

  /** Declarations of the first rule matching `selector {`. */
  function declsOf(selector: string): string {
    const m = globalStyle.match(
      new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}'),
    )
    expect(m, `${selector} rule must exist`).not.toBeNull()
    return m![1]
  }

  it('resets white-space on every tag DOMPurify keeps from a leaked payload', () => {
    // The UA default is the root cause: <option> is nowrap, <select> is pre.
    // All eight preserved tags are covered so a differently-shaped malformed
    // payload cannot slip through.
    const decls = declsOf('.chat-message :is(option, optgroup, select, textarea, label, fieldset, legend, header)')
    expect(decls, 'must override the UA nowrap/pre default').toMatch(
      /white-space:\s*normal/,
    )
    // Guarantees even an unbreakable token (long URL / base64 blob) breaks.
    expect(decls, 'must break unbreakable tokens too').toMatch(
      /overflow-wrap:\s*anywhere/,
    )
  })

  it('scopes the guard to .chat-message so real markdown output is untouched', () => {
    // A bare `option { … }` rule would leak into the whole app (and into
    // markdown file previews, which legitimately render these tags nowhere).
    // The selector must stay anchored to the message bubble.
    expect(globalStyle).not.toMatch(/^\s*(?:option|label|header|select)\s*\{/m)
    const rule = globalStyle.match(/\.chat-message :is\([^)]*\)\s*\{[^}]*\}/)
    expect(rule, 'guard must be anchored to .chat-message').not.toBeNull()
  })

  it('does not disable wrapping of real code blocks', () => {
    // Regression guard: the code-block rules deliberately use pre/pre-wrap, and
    // the new guard must not be broad enough to catch <pre>/<code> (neither is
    // in the DOMPurify-preserved leak set). Assert both stay as designed.
    const pre = declsOf('.chat-message.assistant pre')
    expect(pre).toMatch(/white-space:\s*pre/)
    const wrapped = declsOf('.chat-message.assistant .code-block-wrapper.word-wrap pre code')
    expect(wrapped).toMatch(/white-space:\s*pre-wrap/)
  })
})
