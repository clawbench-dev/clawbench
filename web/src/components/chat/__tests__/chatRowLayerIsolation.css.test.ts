import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Android WebView: the chat ROW must be both its own layer and clipped to its
 * own box.
 *
 * `will-change: transform` alone promotes `.chat-message` to a compositing layer
 * — that stopped message-to-message pixel contamination (phantom metadata text).
 * It does NOT bound what the layer paints, so a stale slice of the bubble's own
 * background can still survive a re-composite. Users report exactly that: a
 * ~9px band of `--bg-tertiary` sitting just under the action bar of the reply
 * that just finished streaming (older replies are clean — their layer was
 * composited long ago), measured as a full-width strip in the panel background
 * colour between the meta bar and the composer.
 *
 * `contain: paint` bounds the row's paint to its own box. Both declarations must
 * stay under [data-app-mode]: desktop browsers do not have this compositor bug
 * and would pay a layer for every message for nothing.
 *
 * jsdom has no CSS engine, so this is a source-contract check (same pattern as
 * chatBoldStyle.test.ts).
 */
describe('chat row layer + paint containment (Android WebView GPU ghost)', () => {
  const itemSource = readFileSync(
    resolve(__dirname, '../ChatMessageItem.vue'),
    'utf8',
  )

  function appModeChatMessageRule(): string {
    const m = itemSource.match(/:root\[data-app-mode\]\s+\.chat-message\s*\{([^}]*)\}/)
    expect(m, ':root[data-app-mode] .chat-message rule must exist').not.toBeNull()
    // Strip comments BEFORE matching. The rule's own doc block names
    // `contain: paint` in prose, so a naive /contain:\s*paint/ over the raw
    // body passes even when the declaration has been changed to `contain:
    // none` — verified by mutation.
    return m![1].replace(/\/\*[\s\S]*?\*\//g, '')
  }

  it('promotes the row to its own layer', () => {
    // Removing this reintroduces message-to-message contamination.
    expect(appModeChatMessageRule()).toMatch(/will-change:\s*transform/)
  })

  it('clips the row paint to its own box', () => {
    // Without this a stale slice of the bubble background survives below the
    // action bar (the ~9px band).
    expect(appModeChatMessageRule()).toMatch(/contain:\s*paint/)
  })

  it('keeps both declarations scoped to app mode', () => {
    // An unscoped `contain: paint` would clip legitimate overflow on desktop
    // (and `will-change` would cost a layer per message).
    const bare = itemSource.match(/^\.chat-message\s*\{([^}]*)\}/m)
    if (bare) {
      expect(bare[1]).not.toMatch(/contain:\s*paint/)
      expect(bare[1]).not.toMatch(/will-change:\s*transform/)
    }
  })
})
