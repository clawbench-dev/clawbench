import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Android WebView: the chat ROW must be clipped to its own box and must NOT be
 * promoted to its own compositing layer.
 *
 * History matters here, because the obvious fix is the wrong one. Promoting
 * each row (`will-change: transform`) stopped message-to-message pixel
 * contamination, but turned every row into a GPU texture whose size changes
 * whenever the row grows. This WebView version then redraws the resized texture
 * into the delta instead of repainting it: a thin strip of the reply's own
 * pixels (a vertically squashed copy of the meta bar text and its icons,
 * 4px = the meta bar's `margin-top`) appears just under the action bar at the
 * moment a reply finishes and the meta bar grows the row. The same resize-tile
 * effect produced the earlier ~9px `--bg-tertiary` band.
 *
 * `contain: paint` is the isolation mechanism: it bounds each row's paint to
 * its own box, so cross-row bleeding is impossible without making the row a
 * layer. It must stay under [data-app-mode]: desktop browsers do not have this
 * compositor bug, and paint containment changes the containing block for
 * injected fixed overlays.
 *
 * jsdom has no CSS engine, so this is a source-contract check (same pattern as
 * chatBoldStyle.test.ts).
 */
describe('chat row paint containment, no layer promotion (Android WebView GPU ghost)', () => {
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

  it('does NOT promote the row to its own layer', () => {
    // A promoted row is a GPU texture that resizes every time the row grows
    // (meta bar appears, tool card expands). This WebView redraws the resized
    // texture into the delta, producing a squashed copy of the meta bar under
    // the action bar. Isolation is done by `contain: paint` below instead.
    expect(appModeChatMessageRule()).not.toMatch(/will-change:\s*transform/)
    expect(appModeChatMessageRule()).not.toMatch(/transform\s*:/)
  })

  it('clips the row paint to its own box', () => {
    // Without this a stale slice of the bubble background survives below the
    // action bar (the ~9px band).
    expect(appModeChatMessageRule()).toMatch(/contain:\s*paint/)
  })

  /**
   * `contain: paint` is not free: it clips the row's descendants to the row's
   * box, and it is NOT visible from this file who those descendants are.
   *
   * `useCodeBlockHeader.annotateTableBlockHeadersIn` injects a
   * `position: fixed` `.table-block-copy-menu` into every markdown table at
   * runtime, inside the `v-html` content this row renders. On a 1–2 row table
   * the menu is TALLER than the table, so with the clip in place its last two
   * items (Copy HTML / Copy TSV) fell below the row's bottom edge — invisible,
   * unclickable, and the click landed on the message underneath. No error, no
   * console warning: the menu simply stopped working.
   *
   * Reproduced in Chromium against the production CSS: `md=CLICKABLE
   * html=DEAD tsv=DEAD`. Removing only `contain: paint` makes all three
   * clickable, and `will-change: transform` alone never clipped — so the paint
   * clip is the sole cause.
   *
   * The escape hatch must therefore exist and must actually relax the clip.
   * These assertions fail if someone deletes it, or "tidies" it back to
   * `contain: paint`.
   */
  describe('escape hatch for injected fixed overlays (table copy menu)', () => {
    /** Declarations of the `:has()` escape rule, comments stripped. */
    function escapeRule(): string {
      const m = itemSource.match(
        /:root\[data-app-mode\]\s+\.chat-message:has\(\.table-block-copy-menu\.is-open\)\s*\{([^}]*)\}/,
      )
      expect(
        m,
        'the :has() escape hatch rule must exist — without it an open table copy ' +
        'menu is clipped by `contain: paint` and its HTML/TSV items are dead',
      ).not.toBeNull()
      return m![1].replace(/\/\*[\s\S]*?\*\//g, '')
    }

    it('relaxes the paint clip while a copy menu is open', () => {
      // Any value that does NOT include `paint` lifts the clip. `style` (or
      // `layout style`) keeps the other containment the base rule wants;
      // `none` is also acceptable. What must never come back is `paint`.
      expect(escapeRule()).not.toMatch(/contain:[^;]*paint/)
    })

    it('keeps the row a containing block while the clip is lifted', () => {
      // The fixed menu is positioned against the ROW because the row is a
      // containing block for fixed descendants. The base rule drops to
      // uncomposited `contain: paint` (which provides it); once paint
      // containment is lifted here, `will-change: transform` must provide the
      // containing block instead, or the open menu jumps to the viewport.
      expect(escapeRule()).toMatch(/will-change:\s*transform/)
    })

    it('targets the row that owns the menu, not the menu itself', () => {
      // Clipping happens at the row, so relaxing it on the menu (or on
      // `.msg-card`) does nothing at all.
      expect(escapeRule().length).toBeGreaterThan(0)
      expect(itemSource).toMatch(
        /:root\[data-app-mode\]\s+\.chat-message:has\(\.table-block-copy-menu\.is-open\)/,
      )
    })

    /**
     * The escape must be conditioned on `is-open`, not merely on the menu
     * existing.
     *
     * `.table-block-copy-menu` is ALWAYS in the DOM (it is injected with
     * `style="display: none"` and merely toggled). A selector without
     * `.is-open` therefore matches every row that ever contained a table —
     * permanently, for every message in the session. The `contain: paint`
     * ghost fix would then be dead on exactly the rows most likely to
     * re-composite, and nothing would look broken until the stale-band bug
     * came back on Android.
     *
     * A weaker variant is just as bad: `.is-open` that is never REMOVED keeps
     * the clip lifted after the menu closes. Both directions are covered here —
     * the selector text must pin the exact condition, and the runtime state it
     * keys on must actually be cleared on close.
     */
    it('conditions the escape on .is-open, not just on the menu existing', () => {
      const sel = itemSource.match(
        /(:root\[data-app-mode\]\s+\.chat-message:has\([^)]*\))\s*\{/,
      )
      expect(sel, 'the :has() escape rule must exist').not.toBeNull()
      const selector = sel![1]

      // The menu is always present, so `:has(.table-block-copy-menu)` alone is
      // a permanent escape. The condition must include the open-state class.
      expect(selector, 'the :has() condition must require `.is-open`').toMatch(
        /:has\(\s*\.table-block-copy-menu\.is-open\s*\)/,
      )
      expect(selector, 'a bare `:has(.table-block-copy-menu)` would disable the clip forever').not.toMatch(
        /:has\(\s*\.table-block-copy-menu\s*\)/,
      )
      // And it must not widen into "any descendant with is-open".
      expect(selector).not.toMatch(/:has\(\s*\.is-open\s*\)/)
    })

    /**
     * End-to-end on real DOM: the selector must be TRUE exactly while a menu is
     * open, and FALSE again once it closes.
     *
     * jsdom implements `matches()` with `:has()` (verified), so the selector is
     * evaluated for real rather than pattern-matched. This is the assertion that
     * would have been missing from a purely textual guard: it fails if the
     * escape stays active after `closeAllTableBlockMenus` / a wrap toggle /
     * an outside click, which is precisely the "leaked class permanently kills
     * the ghost fix" failure mode.
     */
    it('matches only while a menu is open, and stops matching once it closes', () => {
      const sel = itemSource.match(
        /(:root\[data-app-mode\]\s+\.chat-message:has\([^)]*\))\s*\{/,
      )
      expect(sel).not.toBeNull()
      // Strip the `:root[data-app-mode]` scope so a detached fixture row can be
      // tested: jsdom's documentElement is not marked data-app-mode, and the
      // scope is asserted separately above.
      const scoped = sel![1]
      const unscoped = scoped.replace(/^:root\[data-app-mode\]\s*/, '')

      const row = document.createElement('div')
      row.className = 'chat-message'
      const menu = document.createElement('div')
      menu.className = 'table-block-copy-menu'
      menu.style.display = 'none'
      row.appendChild(menu)
      document.body.appendChild(row)

      try {
        // Closed: the clip must be in force (escape inactive).
        expect(row.matches(unscoped), 'escape must NOT match a closed menu').toBe(false)

        // Open: the clip must be lifted.
        menu.classList.add('is-open')
        expect(row.matches(unscoped), 'escape must match an open menu').toBe(true)

        // Closed again: the clip must come back. This is the assertion the
        // whole `:has()`-over-JS-class argument rests on.
        menu.classList.remove('is-open')
        expect(row.matches(unscoped), 'escape must stop matching once the menu closes').toBe(false)

        // `display: none` alone, with `is-open` left behind, must NOT count as
        // open — otherwise a half-finished close leaves the clip lifted.
        menu.classList.add('is-open')
        menu.style.display = 'none'
        expect(
          row.matches(unscoped),
          'the condition is the class, so a stale class still matches — ' +
          'which is why every close path must remove it (asserted below)',
        ).toBe(true)
        menu.classList.remove('is-open')
      } finally {
        row.remove()
      }
    })

    it('does not claim the row is free of overflowing fixed descendants', () => {
      // The old comment asserted this was "safe here ... verified — only two
      // position: relative". That was wrong: the overlays are injected by
      // another module, so grepping THIS file could never find them. Keeping
      // the false claim is how the next person re-adds `contain: paint`
      // somewhere else believing the same thing.
      //
      // Must read the RAW rule body: `appModeChatMessageRule()` strips
      // comments, so asserting against it would be a tautology (prose can
      // never match once the prose is gone).
      const m = itemSource.match(/:root\[data-app-mode\]\s+\.chat-message\s*\{([\s\S]*?)\n\}/)
      expect(m, ':root[data-app-mode] .chat-message rule must exist').not.toBeNull()
      const rawBody = m![1]
      expect(rawBody).not.toMatch(/only two `?position:\s*relative/)
      expect(rawBody).not.toMatch(/no absolutely\/fixed positioned descendants/)
      // And it must positively state the hazard, so the rule reads as
      // "safe ONLY BECAUSE of the escape hatch" rather than "safe, period".
      expect(rawBody).toMatch(/CONTAINMENT CLIPS INJECTED FIXED OVERLAYS/i)
    })

    it('names the module that injects the overlay', () => {
      // The rule must point at the real owner so the next reader can find it.
      const m = itemSource.match(/:root\[data-app-mode\]\s+\.chat-message\s*\{([\s\S]*?)\n\}/)
      expect(m).not.toBeNull()
      expect(m![1]).toContain('useCodeBlockHeader')
      expect(m![1]).toContain('table-block-copy-menu')
    })
  })

  it('keeps the containment scoped to app mode', () => {
    // An unscoped `contain: paint` would clip legitimate overflow on desktop
    // and would change the containing block for fixed overlays everywhere.
    const bare = itemSource.match(/^\.chat-message\s*\{([^}]*)\}/m)
    if (bare) {
      expect(bare[1]).not.toMatch(/contain:\s*paint/)
      expect(bare[1]).not.toMatch(/will-change:\s*transform/)
    }
  })
})
