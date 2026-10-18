import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source guards for "quote this message" routing.
 *
 * `handleQuoteMessage` is a local function inside ChatPanelContent's
 * `<script setup>`, so it cannot be imported and called directly. What matters
 * is a routing DECISION, which is visible in the source: the action must go
 * through the shared session picker (`requestTarget`), exactly like every other
 * quote entry point. Staging it locally (`addStagedQuote`) is the old behavior
 * and is what made "quote from this conversation into another" impossible.
 *
 * These guards are deliberately narrow — they pin the routing call, not the
 * whole function — so unrelated edits to the component do not trip them.
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

  it('routes the message quote through the session picker', () => {
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/requestTarget\s*\(/)
  })

  it('does NOT stage the message quote straight into the current draft', () => {
    // addStagedQuote here is the regression: it hard-wired the destination to
    // the session on screen.
    const body = functionBody('handleQuoteMessage')
    expect(body).not.toMatch(/addStagedQuote\s*\(/)
  })

  it('carries the message id so the quote stays addressable', () => {
    const body = functionBody('handleQuoteMessage')
    expect(body).toMatch(/buildMessageQuote\s*\(/)
  })
})
