import { describe, expect, it } from 'vitest'

/**
 * BottomSheet positions itself with two variables that describe the HOST's
 * chrome, not the sheet's own:
 *
 *   .bs-overlay { top: calc(var(--header-height) + var(--header-safe-area-top));
 *                 bottom: var(--dock-height, 0); }
 *
 * The app keeps them truthful — App.vue measures the real dock and writes
 * `--dock-height`, and variables.css carries the app's 36px header — so the
 * sheet lands between the app header and the dock.
 *
 * The share SPA mounts neither. Without a local restatement it inherits the
 * app's geometry, which produced two defects on the real page:
 *
 *   - sheets stopped 47px short of the bottom (the `--dock-height` default)
 *     with nothing underneath them;
 *   - the scrim started 8px INSIDE the 44px share topbar, covering the bottom
 *     of the title.
 *
 * jsdom has no CSS engine, so this reads the stylesheet. It also asserts the
 * declarations are on `:root`: BottomSheet is `Teleport to="body"`, so its
 * overlay is a sibling of the share root — a variable set on `.share-view`
 * would never reach it.
 */

async function rawCss(): Promise<string> {
  const mod = await import('../../../css/share-chrome.css?raw')
  return String(mod.default).replace(/\/\*[\s\S]*?\*\//g, '')
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** Declarations of the first rule whose selector list is exactly `selector`. */
function declsOf(css: string, selector: string): string | null {
  const pattern = selector.split(',').map((s) => escapeRe(s.trim())).join('\\s*,\\s*')
  const m = css.match(new RegExp(`(?:^|[},])\\s*${pattern}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

/** Value of a custom property inside a declaration block, or null. */
function customProp(decls: string, name: string): string | null {
  const m = decls.match(new RegExp(`(?:^|;)\\s*${escapeRe(name)}\\s*:\\s*([^;]+)`, 'm'))
  return m ? m[1].trim() : null
}

describe('share chrome restates the host layout variables', () => {
  it('zeroes --dock-height (the share page has no dock)', async () => {
    const root = declsOf(await rawCss(), ':root')
    expect(root, 'share-chrome.css must declare a :root block').not.toBeNull()
    // Any non-zero value leaves the same gap the bug report described; assert the
    // resolved length rather than the exact spelling so `0` and `0px` both pass.
    expect(customProp(root!, '--dock-height')).toMatch(/^0(px)?$/)
  })

  it('points --header-height at the topbar height, not the app 36px', async () => {
    const root = declsOf(await rawCss(), ':root')
    const value = customProp(root!, '--header-height')
    expect(value, '--header-height must be restated for the share host').not.toBeNull()
    // Indirect through the topbar token: hard-coding 44px here as well would let
    // the bar and the sheet drift apart on the next change.
    expect(value).toBe('var(--share-topbar-height)')
    expect(customProp(root!, '--share-topbar-height')).toBe('44px')
  })

  it('renders the topbar at that same token', async () => {
    const topbar = declsOf(await rawCss(), '.share-topbar')
    expect(topbar, '.share-topbar must exist').not.toBeNull()
    // The whole point of the indirection: one value drives both.
    expect(customProp(topbar!, 'height')).toBe('var(--share-topbar-height)')
  })

  it('does not restate --header-safe-area-top', async () => {
    // The share SPA pads for the notch NOWHERE (share.html has no viewport-fit
    // padding and .share-topbar has no inset). Forcing a non-zero inset here
    // would push every sheet down by an amount the page's own chrome does not
    // account for.
    const root = declsOf(await rawCss(), ':root')
    expect(customProp(root!, '--header-safe-area-top')).toBeNull()
  })
})
