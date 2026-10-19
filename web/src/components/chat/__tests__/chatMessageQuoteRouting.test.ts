import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source guards for "quote this message" routing.
 *
 * `handleQuoteMessage` is a local function inside ChatPanelContent's
 * `<script setup>`, so it cannot be imported and called directly. What matters
 * is a routing DECISION, which is visible in the source: the action must go
 * through the shared quote dispatcher (`requestTarget`) — never by staging the
 * card locally, which is what hard-wired the destination and made
 * "quote from this conversation into another" impossible.
 *
 * The message quote is a comment on the conversation being read, so it also
 * declares `deliverTo: 'active'` (issue #560): the active conversation is the
 * destination, no dialog. Pinning that here keeps a future refactor from
 * silently reinstating the mandatory picker.
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

  it('routes the message quote through the shared dispatcher', () => {
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/requestTarget\s*\(/)
  })

  it('delivers to the active conversation instead of always asking', () => {
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/deliverTo:\s*'active'/)
  })

  it('does NOT stage the message quote straight into the current draft', () => {
    // Staging here would bypass the dispatcher entirely — the destination would
    // no longer be decided in one place (and cross-session delivery would break).
    const body = functionBody('handleQuoteMessage')
    expect(body).not.toMatch(/addStagedQuote\s*\(/)
  })

  it('carries the message id so the quote stays addressable', () => {
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/buildMessageQuote\s*\(/)
  })
})
