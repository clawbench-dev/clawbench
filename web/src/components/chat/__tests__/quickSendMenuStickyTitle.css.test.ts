import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Quick-send / quick-command menu: taller scrollport + a pinned title.
 *
 * Two things are easy to regress silently, because jsdom has no CSS engine and
 * nothing else in the suite reads these declarations:
 *
 *  1. HEIGHT — both menus used to be capped at 280px, which fits ~6 rows. With
 *     more saved entries the list scrolled inside a short box. The cap is a
 *     `PopupMenu` prop (`:max-height`), so a refactor that re-adds a small
 *     literal just shrinks the menu again with no visible failure anywhere.
 *  2. PINNED TITLE — `.quick-send-title` is the first child of the popup root,
 *     which is itself the scrollport (PopupMenu sets inline `overflow-y: auto`).
 *     Without `position: sticky` the title scrolls away and the user loses the
 *     "what is this list" label. The background must be OPAQUE and equal to the
 *     popup surface (`--bg-secondary`, see PopupMenu.vue): a transparent one
 *     lets the rows show through as they pass underneath the pinned band.
 *
 * The class is shared by TWO hosts — the chat composer's quick-send menu
 * (ChatInputBar.vue) and the terminal's quick-command menu
 * (TerminalPanelContent.vue) — so the rule must live in css/components.css.
 * It started in ChatInputBar.vue's unscoped block, where it "worked" for the
 * terminal only because the chat panel happened to mount first; that is the
 * same host-order trap the app-menu container rules were moved out for (see
 * the comment above `.app-menu-title` in components.css).
 */

const chatSource = readWebFile('src/components/chat/ChatInputBar.vue')
const terminalSource = readWebFile('src/components/terminal/TerminalPanelContent.vue')

/** Strip comments — the CSS documents the very selectors it defines. */
const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, '')
const componentsCss = stripComments(readWebFile('css/components.css'))

/** Declarations of the first exact `<selector> {` rule. */
function declsOf(css: string, selector: string): string | undefined {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`(?:^|\\n)${escaped}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : undefined
}

/** Every `<style>` block of a .vue file, tagged scoped/unscoped. */
function styleBlocks(src: string): Array<{ scoped: boolean; body: string }> {
  const out: Array<{ scoped: boolean; body: string }> = []
  const re = /<style([^>]*)>([\s\S]*?)<\/style>/g
  let m: RegExpExecArray | null
  while ((m = re.exec(src)) !== null) {
    out.push({ scoped: /\bscoped\b/.test(m[1]), body: stripComments(m[2]) })
  }
  return out
}

describe('quick-send menu: pinned title (global rule)', () => {
  const decls = declsOf(componentsCss, '.quick-send-title')

  it('is declared in css/components.css', () => {
    expect(decls, '.quick-send-title must be declared in css/components.css').toBeDefined()
  })

  it('pins the title to the top of the scrollport', () => {
    // `sticky` + `top: 0` is the whole mechanism: the popup root is the scroll
    // container (PopupMenu's inline overflow-y), so the first child can stick
    // to its top edge.
    expect(decls!).toMatch(/position:\s*sticky/)
    expect(decls!).toMatch(/(?:^|[\s;])top:\s*0/)
  })

  it('uses the popup surface colour so rows do not show through', () => {
    // Must be the same token PopupMenu paints its background with; a
    // transparent (or color-mix) background would bleed the scrolling rows.
    expect(decls!).toMatch(/background:\s*var\(--bg-secondary\b/)
    expect(decls!).not.toMatch(/background:\s*transparent/)
    expect(decls!).not.toMatch(/color-mix/)
  })

  it('stacks above the rows it covers', () => {
    // Rows are `position: relative` (for the trailing inject button), so the
    // pinned band needs a positive z-index to paint over them.
    expect(decls!).toMatch(/z-index:\s*[1-9]/)
  })

  it('is not re-declared by either host component', () => {
    // A per-component copy would win for that host and drift from the other —
    // the exact failure mode the global rule exists to prevent.
    for (const [name, src] of [['ChatInputBar.vue', chatSource], ['TerminalPanelContent.vue', terminalSource]] as const) {
      for (const block of styleBlocks(src)) {
        expect(block.body, `${name} must not declare .quick-send-title`).not.toMatch(/(?:^|\n)\.quick-send-title\s*\{/)
      }
    }
  })
})

describe('quick-send menu: height', () => {
  const heightOf = (src: string, itemsExpr: string) => {
    const m = src.match(new RegExp(`<PopupMenu[^>]*:max-height="(\\d+)"[^>]*:menu-items-count="${itemsExpr}"`))
    expect(m, 'the PopupMenu must keep its max-height prop').not.toBeNull()
    return Number(m![1])
  }

  it('allows a taller chat quick-send menu than the old 280px cap', () => {
    // 280 fitted ~6 rows and scrolled sooner than necessary; 440 matches the
    // theme picker and the usage popover, i.e. the tallest menu tier in the app.
    expect(heightOf(chatSource, 'quickSendItems\\.length \\+ 1')).toBeGreaterThanOrEqual(400)
  })

  it('allows a taller terminal quick-command menu than the old 280px cap', () => {
    expect(heightOf(terminalSource, 'visibleCommands\\.length \\+ 1')).toBeGreaterThanOrEqual(400)
  })

  it('keeps the two menus on the same cap', () => {
    // They are the same surface with the same rows; diverging means one of them
    // silently scrolls earlier than the other for no design reason.
    expect(heightOf(chatSource, 'quickSendItems\\.length \\+ 1'))
      .toBe(heightOf(terminalSource, 'visibleCommands\\.length \\+ 1'))
  })
})
