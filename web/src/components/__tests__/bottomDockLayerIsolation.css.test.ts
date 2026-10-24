import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Android WebView: the bottom dock must be its own compositing layer.
 *
 * Sibling of the `.chat-message { will-change: transform }` rule in
 * ChatMessageItem.vue, and it covers the other half of the same compositor bug.
 * That rule promotes each MESSAGE to its own layer, so message-to-message
 * contamination is gone — but the polluted region here is the chat panel's own
 * empty space, and the pixels come from the DOCK, a sibling subtree that has no
 * layer of its own. When a reply finishes the list re-composites and WebView
 * keeps the previous frame's dock pixels at the band's old offset: users see a
 * thin strip of the dock (typically the orange active-tab disc) just below the
 * action bar, which only a reload or a session switch clears.
 *
 * `contain: paint` clips the dock's descendants to the dock's own box so a stale
 * composite cannot bleed above it; `will-change: transform` gives the wrapper
 * its own layer. Both must stay scoped to [data-app-mode] — promoting the dock
 * unconditionally would disable subpixel antialiasing for its labels on
 * non-HiDPI desktop screens.
 *
 * jsdom has no CSS engine, so this is a source-contract check (the same pattern
 * chatBoldStyle.test.ts / chatMetaBarAlignment.test.ts use).
 */
describe('bottom dock layer isolation (Android WebView GPU ghost)', () => {
  const appSource = readFileSync(resolve(__dirname, '../../App.vue'), 'utf8')

  /** Declarations of the first rule whose selector matches `pattern`. */
  function declsIn(pattern: RegExp): string {
    const m = appSource.match(pattern)
    expect(m, `${pattern} rule must exist`).not.toBeNull()
    return m![1]
  }

  it('promotes the dock wrapper to its own layer and clips its descendants', () => {
    const rule = declsIn(/:root\[data-app-mode\]\s+\.bottom-dock-wrapper\s*\{([^}]*)\}/)
    expect(rule).toMatch(/will-change:\s*transform/)
    expect(rule).toMatch(/contain:\s*paint/)
  })

  it('keeps the isolation scoped to app mode', () => {
    // An unscoped rule would also promote the dock in desktop browsers, where
    // the bug does not exist and the layer costs ClearType on its labels.
    const bare = appSource.match(/^\s*\.bottom-dock-wrapper\s*\{([^}]*)\}/m)
    if (bare) {
      expect(bare[1]).not.toMatch(/will-change/)
      expect(bare[1]).not.toMatch(/contain:\s*paint/)
    }
  })
})
