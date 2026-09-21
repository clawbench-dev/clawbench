import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Guard tests for the wallpaper-active surface strategy in css/base.css.
 *
 * Regression: full-page tab roots previously stacked a SECOND translucent
 * layer (color-mix over --panel-alpha) on top of the already-translucent
 * .tab-panel. Compounding alphas (panel root → card → panel) dimmed the
 * wallpaper so badly that low opacity settings nearly hid it. The fix makes
 * whole-page roots AND the full-bleed card/list surfaces inside them fully
 * transparent while the wallpaper is active — the .tab-panel becomes the
 * single visible surface. Only narrow page-level bars keep a translucent
 * tint, and interactive states re-add a tint as feedback.
 *
 * These are source-sniffing tests (jsdom has no CSS engine), the same
 * pattern as flashReducedMotion.css.test.ts / modalFooterBtn.theme.css.test.ts.
 * base.css lives outside the vitest source root (web/css), so it is read off
 * disk — the cwd differs between a bare `vitest` run (web/) and the
 * scripts/vitest-run.sh wrapper (repo root), so probe both.
 */

function readBaseCss(): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, 'css/base.css'), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error('css/base.css not found from cwd: ' + process.cwd())
}

const css = readBaseCss()

// Extract the full rule (selector list + declaration block) that contains the
// given selector. The wallpaper scope prefixes every selector with
// html.wallpaper-active, so anchoring on a member selector returns the whole
// grouped rule.
function ruleContaining(anchor: string): string {
  const idx = css.indexOf(anchor)
  expect(idx, `selector "${anchor}" should exist`).toBeGreaterThan(-1)
  const open = css.indexOf('{', idx)
  const close = css.indexOf('}', open)
  return css.slice(idx, close + 1)
}

describe('wallpaper-active single-surface transparency', () => {
  it('keeps whole-page roots fully transparent (no second translucent scrim)', () => {
    // Task / proxy / settings / stats / media-preview page roots must become
    // fully transparent under wallpaper-active so only the .tab-panel surface
    // shows through — NOT a translucent color-mix layer that compounds alphas.
    const rule = ruleContaining('html.wallpaper-active .task-list-page')
    expect(rule).toContain('background: transparent;')
    for (const sel of ['.task-detail-page', '.settings-page', '.proxy-panel-content', '.usage-stats-panel', '.pdf-error', '.forge-panel', '.forge-detail']) {
      expect(rule).toContain(`html.wallpaper-active ${sel}`)
    }
    // The whole-page roots group must not contain a translucent color-mix
    // background — that would reintroduce the compounding-alpha regression.
    expect(rule).not.toContain('color-mix')
  })

  it('applies exactly ONE translucent layer on full-bleed cards/list rows', () => {
    const rule = ruleContaining('html.wallpaper-active .task-item')
    expect(rule).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-secondary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    for (const sel of ['.execution-item', '.overview-card', '.proxy-port-item', '.form-section', '.stats-card-panel', '.forge-comment']) {
      expect(rule).toContain(`html.wallpaper-active ${sel}`)
    }
  })

  it('keeps page-level bars translucent so chrome stays legible over the wallpaper', () => {
    const rule = ruleContaining('html.wallpaper-active .settings-page__header')
    expect(rule).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-primary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    for (const sel of ['.stats-header', '.list-header', '.detail-header', '.proxy-header', '.forge-header', '.forge-detail-header', '.forge-detail-footer']) {
      expect(rule).toContain(`html.wallpaper-active ${sel}`)
    }
  })

  it('lets the wallpaper show through the forge panel and its tab bar', () => {
    // The forge tab is a full-page surface: its root goes fully transparent
    // (only .tab-panel shows through) while the type tab bar keeps the
    // bg-secondary tint shared with the stats tab bar.
    // Anchor on the group's first selector so the slice includes every member.
    const tabs = ruleContaining('html.wallpaper-active .terminal-tab-bar')
    expect(tabs).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-secondary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    expect(tabs).toContain('html.wallpaper-active .stats-tab-bar')
    expect(tabs).toContain('html.wallpaper-active .forge-tabs')

    // The comment body is opaque bg-primary by default; inside a translucent
    // comment card it must clear, or it would cover the card's own alpha.
    const body = ruleContaining('html.wallpaper-active .forge-comment-body')
    expect(body).toContain('background: transparent;')
  })

  it('re-tints interactive states over the translucent card base as feedback', () => {
    const rule = ruleContaining('html.wallpaper-active .task-item:active')
    expect(rule).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-tertiary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    // Running states keep their green tint composited over the translucent base.
    const running = ruleContaining('html.wallpaper-active .task-item.is-running')
    expect(running).toContain('var(--color-green) 5%')
  })

  it('lets the wallpaper show through the CodeMirror code canvas in browse AND edit modes', () => {
    const browse = ruleContaining('html.wallpaper-active .cm-viewer')
    expect(browse).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--code-bg\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    // The editable accent tint must be preserved by mixing over the translucent base.
    const edit = ruleContaining('html.wallpaper-active .cm-viewer.is-editable')
    expect(edit).toContain('var(--accent-color) 6%')
    expect(edit).toContain('var(--code-bg)')
  })

  it('lets the wallpaper show through the file manager search dock', () => {
    // The resident search dock carries the top toolbar's material (--bg-tertiary)
    // and goes translucent at --panel-alpha.
    const dock = ruleContaining('html.wallpaper-active .fs-nav-bottom')
    expect(dock).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-tertiary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    // The pill inside it is a deliberately flat field — no fill and no border in
    // any state, focus included. There is therefore no solid fill to re-tint,
    // and a wallpaper-scoped tint here would put back the box the field sheds
    // (and would win on specificity, since it also matches while unfocused).
    expect(css).not.toContain('html.wallpaper-active .fs-nav-bottom .search-pill')
  })

  it('lets the wallpaper show through the plan panel chip and expanded card', () => {
    // Plan progress UI floats in the chat column between the bubbles and the
    // input bar. Both the collapsed chip (bg-tertiary) and the expanded
    // timeline card (bg-secondary) must go translucent at --panel-alpha so
    // they read consistently with the translucent message bubbles around them.
    const chip = ruleContaining('html.wallpaper-active .plan-chip')
    expect(chip).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-tertiary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    const card = ruleContaining('html.wallpaper-active .plan-expanded')
    expect(card).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-secondary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
  })
})
