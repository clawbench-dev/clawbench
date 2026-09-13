import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Guard tests for the wallpaper blur's compositing cost.
 *
 * Regression: the blur class `.wallpaper-image--blurred` carried
 * `will-change: filter, transform`. Measured with CDP LayerTree snapshots on a
 * 3840x2160 source (6 reps, median), that promotion added 10 composited layers
 * and ~22MB of texture at deviceScaleFactor=1 — and multiplied by DPR^2 on a
 * phone. The blur itself (inline `filter: blur(Npx)` + `transform: scale(1.06)`)
 * costs nothing extra: measured without will-change the layer count and texture
 * are identical to a plain, unblurred wallpaper.
 *
 * The promotion was defending against a re-raster that does not occur: the
 * wallpaper layer is `position:absolute` inside a `position:fixed` container,
 * so it does not scroll with the content. Removing it is a net win.
 *
 * These are source-sniffing tests (jsdom has no CSS engine), the same pattern
 * as wallpaperSurfaceTransparency.css.test.ts. base.css lives outside the
 * vitest source root (web/css), so it is read off disk — the cwd differs
 * between a bare `vitest` run (web/) and the scripts/vitest-run.sh wrapper
 * (repo root), so probe both.
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

describe('wallpaper blur compositing cost', () => {
  it('does not promote the wallpaper image with will-change', () => {
    // Strip comments so an explanatory comment mentioning will-change cannot
    // satisfy or trip this assertion.
    const declarations = css.replace(/\/\*[\s\S]*?\*\//g, '')
    expect(declarations).not.toContain('will-change: filter')
    expect(declarations).not.toMatch(/\.wallpaper-image\b[^{]*\{[^}]*will-change/)
  })

  it('keeps the blur rendering via the inline filter, not a class rule', () => {
    // The blur must still be produced by the inline style App.vue binds
    // (wallpaperImageStyle). Removing the class must not have removed the
    // effect itself — the effect lives in App.vue, the class only ever existed
    // as a promotion hook.
    const app = (() => {
      for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
        try {
          return readFileSync(join(base, 'src/App.vue'), 'utf8')
        } catch {
          // try the next candidate
        }
      }
      throw new Error('src/App.vue not found from cwd: ' + process.cwd())
    })()
    expect(app).toContain('blur(${wallpaperBlurPx.value}px)')
    expect(app).toContain('scale(1.06)')
    // The dead class binding must be gone from the template too.
    expect(app).not.toContain('wallpaper-image--blurred')
  })
})
