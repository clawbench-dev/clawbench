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
    for (const sel of ['.task-detail-page', '.settings-page', '.proxy-panel-content', '.usage-stats-panel', '.pdf-error']) {
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
    for (const sel of ['.execution-item', '.overview-card', '.proxy-port-item', '.form-section', '.stats-card-panel']) {
      expect(rule).toContain(`html.wallpaper-active ${sel}`)
    }
  })

  it('keeps page-level bars translucent so chrome stays legible over the wallpaper', () => {
    const rule = ruleContaining('html.wallpaper-active .settings-page__header')
    expect(rule).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-primary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    for (const sel of ['.stats-header', '.list-header', '.detail-header', '.proxy-header']) {
      expect(rule).toContain(`html.wallpaper-active ${sel}`)
    }
  })

  it('re-tints interactive states over the translucent card base as feedback', () => {
    const rule = ruleContaining('html.wallpaper-active .task-item:active')
    expect(rule).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-tertiary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    // Running states keep their green tint composited over the translucent base.
    const running = ruleContaining('html.wallpaper-active .task-item.is-running')
    expect(running).toContain('var(--success-color, #16a34a) 5%')
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
})

describe('wallpaper-active share-SPA chrome (public /share/{token})', () => {
  it('keeps .share-view fully transparent so the wallpaper layer is the single surface', () => {
    const rule = ruleContaining('html.wallpaper-active .share-view')
    expect(rule).toContain('background: transparent;')
    // Must not stack a second translucent color-mix layer over the wallpaper.
    expect(rule).not.toContain('color-mix')
  })

  it('applies one translucent layer to the share top bar, content column and TOC rail', () => {
    const rule = ruleContaining('html.wallpaper-active .share-topbar')
    expect(rule).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--bg-secondary\)\s*var\(--panel-alpha\),\s*transparent\);/,
    )
    // The content column mirrors the in-app .tab-panel translucent surface,
    // so the reading column and the TOC rail share the same single layer.
    for (const sel of ['.share-content', '.share-toc']) {
      expect(rule).toContain(`html.wallpaper-active ${sel}`)
    }
  })
})
