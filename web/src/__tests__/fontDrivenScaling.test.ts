import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Guards the font-driven scaling contract.
 *
 * The UI scale is applied by resizing the ROOT FONT SIZE, and every size is
 * expressed relative to it through `--dp` (= `1rem / 15`). That replaced
 * `html { zoom }`, which looked equivalent but was not: zoom does not scale the
 * layout viewport, so at zoom 1.5 on a 1000px window `100vw` drew 1500 visual px,
 * `60dvh` grew from 60% to 90% of the screen, media queries still saw 1000px,
 * and canvases stretched their backing store.
 *
 * These assertions are the fence around that mechanism. jsdom has no CSS engine,
 * so this reads the raw stylesheets (same approach as designTokens.css.test.ts);
 * cwd differs between a bare `vitest` run (web/) and scripts/vitest-run.sh (repo
 * root), so both are probed.
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

function readWebDir(relPath: string): string[] {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readdirSync(join(base, relPath))
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

const variablesCss = readWebFile('css/variables.css')
const baseCss = readWebFile('css/base.css')

/** Every `.css` and `.vue` `<style>` block under src/, plus the global sheets. */
function allStyleSources(): Array<{ file: string; css: string }> {
  const out: Array<{ file: string; css: string }> = []
  for (const f of readWebDir('css')) {
    if (f.endsWith('.css')) out.push({ file: `css/${f}`, css: readWebFile(`css/${f}`) })
  }
  const walk = (dir: string) => {
    for (const entry of readWebDir(dir)) {
      const rel = `${dir}/${entry}`
      if (entry.endsWith('.vue')) {
        const sfc = readWebFile(rel)
        const css = [...sfc.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(m => m[1]).join('\n')
        if (css) out.push({ file: rel, css })
      } else if (!entry.includes('.') && entry !== '__tests__') {
        walk(rel)
      }
    }
  }
  walk('src')
  return out
}

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

describe('the root font size is the only scaling knob', () => {
  it('derives the root font size from --ui-scale', () => {
    // Not a plain px value: everything hangs off the root size, so anything that
    // changes it (this declaration, the browser's own default-font-size
    // preference, a user stylesheet) rescales the UI consistently.
    expect(stripComments(baseCss)).toMatch(
      /html\s*\{\s*font-size:\s*calc\(15px \* var\(--ui-scale/,
    )
  })

  it('does NOT size html from a --font-size-* token', () => {
    // `--dp` is `1rem / 15`, so `--font-size-*` resolves against the root size.
    // If html took one of those tokens the cycle
    //   1rem ← root size ← token ← 1rem
    // would close and the browser would fall back to the 16px initial value —
    // which is exactly what happened on the first attempt at this migration and
    // made the whole UI ignore the setting.
    const html = stripComments(baseCss).match(/\bhtml\s*\{([^}]*)\}/g) ?? []
    for (const rule of html) {
      expect(rule, 'html must not take --font-size-* as its own size').not.toMatch(
        /font-size:\s*var\(--font-size-/,
      )
    }
    // `body` may, because by then the root size is already resolved.
    expect(stripComments(baseCss)).toMatch(/body\s*\{[^}]*font-size:\s*var\(--font-size-xl\)/)
  })

  it('defines --dp as one fifteenth of a rem and nothing else', () => {
    expect(variablesCss).toMatch(/--dp:\s*calc\(1rem \/ 15\)/)
    // A second definition anywhere would shadow the first depending on order.
    const occurrences = variablesCss.split('\n').filter(l => /^\s*--dp\s*:/.test(l))
    expect(occurrences, '--dp must be declared exactly once').toHaveLength(1)
  })

  it('declares --ui-scale with a scale-1 default', () => {
    // The default matters: the first paint (and every standalone document) reads
    // the variable before any JS has run.
    expect(variablesCss).toMatch(/--ui-scale:\s*1;/)
  })

  it('applies the stored scale before first paint', () => {
    // Without this the first frame renders at 100% and every element jumps to
    // its real size once the app mounts — a visible reflow of the whole UI on
    // every reload. The theme and font blocks above set the same precedent.
    const html = readWebFile('index.html')
    expect(html, 'index.html must pre-set --ui-scale').toMatch(/setProperty\(\s*['"]--ui-scale['"]/)
    // It must read the SAME key useSettingsConfig writes, or the bootstrap and
    // the applier would disagree and the flash would come back.
    expect(html).toContain('clawbench-settings-uiScale')
    // And clamp, so a corrupt/hand-edited value cannot blank the UI.
    expect(html).toMatch(/Math\.min\(\s*2\s*,\s*Math\.max\(\s*0?\.5/)
    // A pre-rem build left `zoom` on <html>; clearing it before paint keeps the
    // two mechanisms from compounding for one frame.
    expect(html).toMatch(/style\.zoom\s*=\s*''/)
  })
})

describe('the CSS zoom mechanism is gone', () => {
  it('no stylesheet sets `zoom` on the root', () => {
    // A leftover `html { zoom: … }` would compound with the rem scaling and
    // shrink the UI to a fraction of its intended size.
    for (const { file, css } of allStyleSources()) {
      const rules = stripComments(css).match(/\{[^}]*\}/g) ?? []
      for (const rule of rules) {
        expect(rule, `${file} must not set zoom`).not.toMatch(/(?:^|[;{\s])zoom\s*:/)
      }
    }
  })

  it('applyUIScale writes --ui-scale and clears any legacy zoom', () => {
    const src = readWebFile('src/utils/uiScale.ts')
    expect(src).toMatch(/setProperty\('--ui-scale'/)
    // Clearing is not cosmetic: a build from before this change (or a user
    // stylesheet) can leave `zoom` on <html>, and the two mechanisms multiply.
    expect(src).toMatch(/style\.zoom\s*=\s*''/)
  })

  it('no longer routes Electron through native setZoomFactor', () => {
    // The native path had the same coordinate-space problem AND suppressed every
    // ResizeObserver in the page. One mechanism in every host means the layout
    // cannot behave differently per platform.
    const src = readWebFile('src/composables/useSettingsConfig.ts')
    const code = stripComments(src)
    expect(code).not.toMatch(/setZoomFactor/)
  })
})

describe('geometry follows the scale', () => {
  it('every design token that is a length is expressed through --dp', () => {
    for (const name of ['space-1', 'space-4', 'space-10', 'radius-xs', 'radius-sm', 'radius-md', 'radius-lg',
      'font-size-2xs', 'font-size-md', 'font-size-xl', 'font-size-2xl',
      'icon-size-sm', 'icon-size-xl', 'header-height']) {
      const m = variablesCss.match(new RegExp(`--${name}:\\s*([^;]+);`))
      expect(m, `--${name} should be defined`).not.toBeNull()
      expect(m![1], `--${name} must scale`).toContain('var(--dp)')
    }
  })

  it('leaves the deliberately-unscaled values literal', () => {
    // Each of these is intentionally NOT a design length:
    //   --radius-full  a pill only has to exceed half the height
    //   --input-line-height  written by JS as a whole px (WebView rounding)
    //   --header-safe-area-top  a physical notch, not a design length
    expect(variablesCss).toMatch(/--radius-full:\s*999px;/)
    expect(variablesCss).toMatch(/--input-line-height:\s*\d+px;/)
    expect(variablesCss).toMatch(/--header-safe-area-top:\s*env\(safe-area-inset-top/)
  })

  it('keeps hairline borders literal', () => {
    // A border is meant to be one device pixel. Scaling 1px to 1.5px at 150%
    // makes every divider and focus ring blurry, so `calc(1 * var(--dp))` must
    // not appear as a border width.
    for (const { file, css } of allStyleSources()) {
      for (const m of stripComments(css).matchAll(/border[a-z-]*:\s*calc\(\s*1\s*\*\s*var\(--dp\)\s*\)/g)) {
        throw new Error(`${file} scales a 1px border: ${m[0]}`)
      }
    }
  })

  it('sizes lucide icons from their width attribute', () => {
    // 885 call sites pass a numeric `:size`, which lucide writes to the SVG's
    // width/height ATTRIBUTES. Attributes do not follow the root font size, so
    // without these rules every icon would stay physically small while its
    // surroundings grew. The rules select on the attribute, so no template has
    // to change.
    const components = readWebFile('css/components.css')
    expect(components).toMatch(/:where\(svg\[width="14"\]\)/)
    expect(components).toMatch(/:where\(svg\[width="16"\]\)/)
    // `:where()` keeps specificity at 0,1,0 so a component that sizes its own
    // SVG (.agent-icon-svg, .li-spinner) still wins.
    const rules = components.match(/:where\(svg\[width="\d+"\]\)\s*\{[^}]*\}/g) ?? []
    expect(rules.length).toBeGreaterThan(15)
    for (const rule of rules) expect(rule).toContain('var(--dp)')
  })
})

describe('the wide-screen breakpoint uses the effective width', () => {
  it('divides the viewport by the scale before comparing', () => {
    // Panels get physically bigger with the scale, so the same window holds
    // fewer design px. Skipping the division would claim there is room for two
    // columns while the columns themselves are 1.5x wider.
    const src = readWebFile('src/composables/useWideScreenLayout.ts')
    expect(stripComments(src)).toMatch(/cssWidth\s*\/\s*scale\s*>=\s*WIDE_SCREEN_MIN_WIDTH/)
  })

  it('re-reads the decision when the scale changes', () => {
    // Rem scaling resizes elements but never the viewport, so `resize` does not
    // fire; without an explicit signal the layout would keep the two-column
    // decision from the previous scale.
    const src = readWebFile('src/composables/useWideScreenLayout.ts')
    expect(src).toContain('UI_SCALE_EVENT')
    const applier = readWebFile('src/utils/uiScale.ts')
    expect(applier).toContain('dispatchEvent')
  })
})

describe('canvas-rendered surfaces scale explicitly', () => {
  // CSS variables cannot reach a canvas, so any surface that draws its own text
  // (xterm, charts) has to multiply the scale into its font size by hand. These
  // guards pin the two that do.

  it('the terminal multiplies the UI scale into xterm fontSize', () => {
    // xterm renders glyphs into a canvas: unlike every other element it does
    // NOT inherit the root font size. Without this the terminal stayed tiny
    // while the panel around it grew.
    const src = readWebFile('src/components/terminal/TerminalPanelContent.vue')
    const code = stripComments(src)
    // The value handed to xterm must be the scaled one...
    expect(code).toMatch(/fontSize\.value\s*=\s*Math\.max\(1,\s*fontSizeDesign\.value\s*\*\s*uiScale\.value\)|const fontSize = computed\(\(\) => Math\.max\(1,\s*fontSizeDesign\.value\s*\*\s*uiScale\.value\)\)/)
    // ...while what gets PERSISTED stays a design value, so the Settings slider
    // keeps meaning "12 at 100%".
    expect(code).toMatch(/setLocalConfig\('terminalFontSize',\s*clamped\)/)
    expect(code).not.toMatch(/setLocalConfig\('terminalFontSize',\s*fontSize\.value\)/)
  })

  it('the terminal steps its DESIGN size on pinch and ctrl+wheel', () => {
    // `applyFontSize` takes design px. Feeding it the already-scaled value would
    // multiply the scale into the stored setting on every gesture (12 → 18 at
    // 150%, then 27, …) — the terminal would run away on the first pinch.
    const src = stripComments(readWebFile('src/components/terminal/TerminalPanelContent.vue'))
    const steps = src.match(/applyFontSize\([^)]*\+ delta\)/g) ?? []
    expect(steps.length).toBeGreaterThanOrEqual(2)
    for (const s of steps) {
      expect(s, `gesture step must use the design size: ${s}`).toContain('fontSizeDesign.value')
    }
  })

  it('the terminal refits when the scale changes', () => {
    // Cell metrics change with the font size, so the cols/rows the PTY was told
    // about are stale — and rem scaling fires no `resize` event.
    const src = readWebFile('src/components/terminal/TerminalPanelContent.vue')
    expect(src).toContain('UI_SCALE_EVENT')
    expect(src).toMatch(/addEventListener\(UI_SCALE_EVENT/)
    expect(src).toMatch(/removeEventListener\(UI_SCALE_EVENT/)
    // The stored setting must ALSO be watched: `fontSizeDesign` seeds once at
    // setup and the Settings row writes through `setLocalConfig` (which has no
    // side effect for this key), so without a watch a change in Settings only
    // reached terminals opened afterwards — an open one kept its old size until
    // a reload. That is the actual "nothing changed" symptom.
    expect(src).toMatch(/watch\(\(\) => localConfig\.terminalFontSize/)
    // The listener must actually re-fit, not just re-read the scale. Scoped to
    // the function body rather than a fixed character window, so a comment
    // tweak cannot silently turn this into a no-op assertion.
    const fn = src.match(/function onUiScaleChange\(\)[\s\S]*?\n\}/)
    expect(fn, 'onUiScaleChange must exist').not.toBeNull()
    expect(fn![0]).toMatch(/updateFontSize\(/)
    expect(fn![0]).toMatch(/fit\(\)/)
  })
})

describe('max()/min()/clamp() arguments are all lengths', () => {
  // `max(0, <length>)` is INVALID CSS: a bare `0` is a <number> and the
  // comparison needs both arguments in the same type. The browser drops the
  // whole declaration at computed-value time — it does not clamp to 0, it makes
  // the property vanish, so the element silently loses its padding.
  //
  // This bit the Markdown preview during the rem migration: a correct
  // `max(0px, (100% - 900px)/2)` was rewritten to `max(0, (100% - 900px)/2)`
  // while the numbers were being converted, and the reading column lost all of
  // its side padding (text flush against the screen edge).
  //
  // The check is deliberately textual: jsdom computes no layout, and the
  // failure is a parse-time validity issue rather than a value issue.
  it('no declaration mixes a bare 0 with a length inside max/min/clamp', () => {
    const offenders: string[] = []
    for (const { file, css } of allStyleSources()) {
      const code = stripComments(css)
      for (const m of code.matchAll(/\b(max|min|clamp)\(/g)) {
        // Take the balanced argument list starting at this `(`.
        let depth = 0
        let i = m.index! + m[0].length - 1
        let args = ''
        for (; i < code.length; i++) {
          const ch = code[i]
          if (ch === '(') depth++
          else if (ch === ')') { depth--; if (depth === 0) break }
          if (depth >= 1 && !(depth === 1 && ch === '(')) args += ch
        }
        // Split on top-level commas only.
        const parts: string[] = []
        let d = 0
        let cur = ''
        for (const ch of args) {
          if (ch === '(') d++
          if (ch === ')') d--
          if (ch === ',' && d === 0) { parts.push(cur.trim()); cur = '' } else cur += ch
        }
        parts.push(cur.trim())
        if (parts.length < 2) continue
        const hasBareZero = parts.some(p => /^0(\.0+)?$/.test(p))
        const hasLength = parts.some(p => /var\(|\d(px|rem|em|%|vh|vw|vmin|vmax|ch|ex)\b|calc\(/.test(p))
        if (hasBareZero && hasLength) {
          const line = code.slice(0, m.index!).split('\n').length
          offenders.push(`${file}:${line}  ${m[1]}(${parts.join(', ')})`)
        }
      }
    }
    expect(offenders, `bare 0 mixed with a length — the declaration is dropped:\n${offenders.join('\n')}`).toEqual([])
  })

  it('the Markdown reading column keeps its side padding', () => {
    // Pin the two places that carry the "center the 900px column" trick. A
    // regression here is invisible to a class-name test but immediately
    // visible as text against the screen edge.
    const content = stripComments(readWebFile('css/content.css'))
    expect(content).toMatch(/\.markdown-preview \.markdown-body\s*\{[^}]*max\(0px,\s*\(100% - calc\(900 \* var\(--dp\)\)\) \/ 2\)/)
    const preview = stripComments(readWebFile('src/components/file/MarkdownPreview.vue'))
    expect(preview).toMatch(/right:\s*max\(0px,\s*\(100% - calc\(900 \* var\(--dp\)\)\) \/ 2\)/)
  })
})
