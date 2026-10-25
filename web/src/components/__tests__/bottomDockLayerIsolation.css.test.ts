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

  /**
   * `contain: paint` clips the dock's descendants to the dock's box. That is
   * the point — but it also means any OVERLAY the dock opens must not be a
   * descendant, or it gets clipped exactly like the chat row's table copy menu
   * did (see chatRowLayerIsolation.css.test.ts).
   *
   * The dock's overflow menu escapes because it is teleported to <body>. That
   * escape is currently accidental: nothing about `.dock-overflow-popup` says
   * "must be teleported", so moving the menu back inside the wrapper to fix
   * some unrelated positioning bug would silently clip it in app mode only.
   * This guard pins the teleport.
   */
  describe('the dock overflow menu must escape the paint clip', () => {
    it('renders .dock-overflow-popup inside a <Teleport to="body">', () => {
      // Find every Teleport block and require one that both targets body and
      // contains the popup. A plain `indexOf` would pass on a teleport that
      // wraps something else entirely.
      const teleports = [...appSource.matchAll(/<Teleport\b([^>]*)>([\s\S]*?)<\/Teleport>/g)]
      expect(teleports.length, 'App.vue must contain at least one <Teleport>').toBeGreaterThan(0)

      const hostsPopup = teleports.filter(([, attrs, body]) =>
        /to\s*=\s*["']body["']/.test(attrs) && body.includes('dock-overflow-popup'),
      )
      expect(
        hostsPopup.length,
        '.dock-overflow-popup must be teleported to <body>: .bottom-dock-wrapper has ' +
        '`contain: paint` in app mode, which clips any descendant overlay',
      ).toBe(1)
    })

    it('does not also render the popup inside the dock wrapper', () => {
      // A second, non-teleported copy would be the clipped one — and it would
      // shadow the teleported menu in DOM order.
      const wrapper = appSource.match(/<div class="bottom-dock-wrapper"[\s\S]*?<\/div>\s*<\/div>\s*<\/div>/)
      if (wrapper) {
        expect(wrapper[0]).not.toContain('dock-overflow-popup')
      }
    })
  })
})
