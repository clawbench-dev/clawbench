import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source guards for "quote this message" routing.
 *
 * `handleQuoteMessage` is a local function inside ChatPanelContent's
 * `<script setup>`, so it cannot be imported and called directly. What matters
 * is a routing DECISION, which is visible in the source.
 *
 * That decision changed: the meta-bar button used to stage the card straight
 * into the active session, which meant "ask something about this message in
 * ANOTHER conversation" required switching sessions first. It now OPENS the
 * floating quote bar in composer mode — the same surface a text selection
 * produces — so the question input and the destination row are both available
 * before anything is committed.
 *
 * Pinned here so a refactor cannot silently go back to the silent staging path.
 */
describe('chat message quote routing', () => {
  const source = readFileSync(
    join(__dirname, '..', 'ChatPanelContent.vue'),
    'utf8',
  )

  /** The body of a top-level `function <name>(...) { ... }` (brace-matched). */
  function functionBody(name: string): string {
    const start = source.indexOf(`function ${name}(`)
    expect(start, `function ${name} must exist in ChatPanelContent.vue`).toBeGreaterThanOrEqual(0)
    const open = source.indexOf('{', start)
    expect(open, `${name} must have a body`).toBeGreaterThan(0)

    let depth = 0
    for (let i = open; i < source.length; i++) {
      const ch = source[i]
      if (ch === '{') depth++
      else if (ch === '}') {
        depth--
        if (depth === 0) return source.slice(open + 1, i)
      }
    }
    throw new Error(`unbalanced braces in ${name}`)
  }

  it('opens the floating quote bar instead of committing immediately', () => {
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/openComposer\s*\(/)
  })

  it('anchors the bar to the pressed button', () => {
    // Without an anchor the bar parks at the pinned top position — for a message
    // near the bottom of a long transcript that is nowhere near the button the
    // user just pressed. The element arrives as the handler's second argument.
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/anchorEl/)
  })

  it('does NOT deliver the card straight into the active session', () => {
    // `requestTarget(...)` here bypassed the bar: the card appeared in the
    // active draft with no chance to type a question or pick another session.
    const body = functionBody('handleQuoteMessage')
    expect(body).not.toMatch(/requestTarget\s*\(/)
    expect(body).not.toMatch(/addStagedQuote\s*\(/)
  })

  it('builds the card with the shared message-quote builder', () => {
    // Keeps the session id / message id locators identical to the selection
    // path; the composer only holds the payload, it does not reshape it.
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/buildMessageQuote\s*\(/)
  })

  it('passes the quoted text as a preview so the bar names what it quotes', () => {
    // A chat message has no path/address the label could point at — without the
    // snippet the bar shows only "引用问答 · <session>" and the user cannot tell
    // WHICH message they are about to ask about.
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/previewText\s*:/)
  })

  it('drops a live text selection before opening', () => {
    // This button means the WHOLE message. A leftover selection would be adopted
    // as the quote instead (openComposer prefers a live selection), so a click
    // right after selecting inside the same message would quote the snippet.
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/removeAllRanges\s*\(/)
  })
})
