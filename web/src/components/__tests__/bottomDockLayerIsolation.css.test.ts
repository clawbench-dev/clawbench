import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Android WebView: the bottom dock is CLIPPED but never promoted to a layer.
 *
 * Symptom this covers: a thin strip of the dock (typically the orange
 * active-tab disc) appears in the gap above the composer after a reply
 * finishes. The pixels come from the dock, a sibling subtree of the chat panel.
 * `contain: paint` clips the dock's descendants to the dock's own box so they
 * cannot be painted above it.
 *
 * ⚠️ `will-change: transform` must NOT come back here. It was the previous fix,
 * and it is the bug being removed tree-wide: an element that changes size while
 * composited is exactly what this WebView draws as a stretched stale texture.
 * The dock's height does change (badges/labels), so promoting it reintroduces
 * the ghost class. The only layer boundary lives on the stable-size chat panel
 * (ChatPanelContent.vue); see chatCompositingBoundary.css.test.ts.
 *
 * jsdom has no CSS engine, so this is a source-contract check (the same pattern
 * chatBoldStyle.test.ts / chatMetaBarAlignment.test.ts use).
 */
describe('bottom dock paint clip (Android WebView GPU ghost)', () => {
  const appSource = readFileSync(resolve(__dirname, '../../App.vue'), 'utf8')

  /** Declarations of the first rule whose selector matches `pattern`. */
  function declsIn(pattern: RegExp): string {
    const m = appSource.match(pattern)
    expect(m, `${pattern} rule must exist`).not.toBeNull()
    return m![1]
  }

  it('clips the dock descendants without promoting a layer', () => {
    const rule = declsIn(/:root\[data-app-mode\]\s+\.bottom-dock-wrapper\s*\{([^}]*)\}/)
    expect(rule).toMatch(/contain:\s*paint/)
    // Promotion is the ghost source, not the fix — it must stay out.
    expect(rule).not.toMatch(/will-change:\s*transform/)
    expect(rule).not.toMatch(/transform\s*:/)
  })

  it('keeps the clip scoped to app mode', () => {
    // An unscoped rule would clip legitimate overflow on desktop, where the
    // compositor bug does not exist.
    const bare = appSource.match(/^\s*\.bottom-dock-wrapper\s*\{([^}]*)\}/m)
    if (bare) {
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
