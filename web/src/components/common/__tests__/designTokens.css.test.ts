import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, join } from 'node:path'

/**
 * Drift guard for the design tokens in `css/variables.css`.
 *
 * Why this exists: component tests that assert a tokenised declaration run
 * under jsdom, which does NOT resolve `var()`. `getComputedStyle(el).fontSize`
 * hands back the literal string `var(--font-size-2xl)`, so those assertions
 * only prove *which token name* was written — not that the token means 16px.
 * The invariant they are really protecting ("the textarea stays 16px so mobile
 * Safari does not zoom on focus") would silently break if someone retuned the
 * token, and every component test would still pass.
 *
 * This spec closes that gap by pinning the token values themselves. It reads
 * the raw stylesheet, following the pattern already used for the font stacks
 * in `utils/__tests__/fontConfig.test.ts`.
 *
 * If a value here needs to change, change it deliberately — and expect to
 * revisit the components whose layout depends on it.
 */

function readCss(relPath: string): string {
  // cwd differs between a bare `vitest` run (web/) and scripts/vitest-run.sh
  // (repo root), so probe both, as other source-sniffing specs here do.
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${relPath} not found from cwd: ${process.cwd()}`)
}

const css = readCss('css/variables.css')

/** The value of a custom property as declared in the `:root` block. */
function token(name: string): string {
  const m = css.match(new RegExp(`${name}:\\s*([^;]+);`))
  expect(m, `token ${name} should be defined`).not.toBeNull()
  return m![1].trim()
}

describe('typography tokens (variables.css)', () => {
  it('defines the seven font-size steps at their documented px values', () => {
    expect(token('--font-size-2xs')).toBe('10px')
    expect(token('--font-size-xs')).toBe('11px')
    expect(token('--font-size-sm')).toBe('12px')
    expect(token('--font-size-md')).toBe('13px')
    expect(token('--font-size-lg')).toBe('14px')
    expect(token('--font-size-xl')).toBe('15px')
    expect(token('--font-size-2xl')).toBe('16px')
  })

  it('keeps --font-size-xl equal to the html/body base size', () => {
    // base.css sizes the document from this token; a mismatch would make every
    // rem-free px value in the app render at an unexpected scale.
    const base = readCss('css/base.css')
    expect(base).toMatch(/font-size:\s*var\(--font-size-xl\)/)
    expect(token('--font-size-xl')).toBe('15px')
  })

  it('defines the three font-weight steps', () => {
    expect(token('--font-weight-medium')).toBe('500')
    expect(token('--font-weight-semibold')).toBe('600')
    expect(token('--font-weight-bold')).toBe('700')
  })

  it('defines the four line-height steps as unitless values', () => {
    expect(token('--line-height-tight')).toBe('1.2')
    expect(token('--line-height-snug')).toBe('1.4')
    expect(token('--line-height-normal')).toBe('1.5')
    expect(token('--line-height-relaxed')).toBe('1.6')
  })
})

describe('spacing tokens (variables.css)', () => {
  it('defines the eight steps at their documented px values', () => {
    expect(token('--space-1')).toBe('2px')
    expect(token('--space-2')).toBe('4px')
    expect(token('--space-3')).toBe('6px')
    expect(token('--space-4')).toBe('8px')
    expect(token('--space-5')).toBe('10px')
    expect(token('--space-6')).toBe('12px')
    expect(token('--space-7')).toBe('16px')
    expect(token('--space-8')).toBe('20px')
  })

  it('keeps the steps strictly increasing', () => {
    // A spacing scale that goes backwards (or repeats) makes "one step up"
    // meaningless and invites arbitrary values back in.
    const px = [1, 2, 3, 4, 5, 6, 7, 8].map((n) =>
      parseFloat(token(`--space-${n}`)),
    )
    for (let i = 1; i < px.length; i++) {
      expect(px[i], `--space-${i + 1} should be larger than --space-${i}`).toBeGreaterThan(px[i - 1])
    }
  })
})

describe('radius tokens (variables.css)', () => {
  it('defines the five steps at their documented values', () => {
    expect(token('--radius-xs')).toBe('3px')
    expect(token('--radius-sm')).toBe('6px')
    expect(token('--radius-md')).toBe('10px')
    expect(token('--radius-lg')).toBe('14px')
    expect(token('--radius-full')).toBe('999px')
  })

  it('keeps --radius-full large enough to always render as a pill', () => {
    // The pill form relies on the radius exceeding half the element height;
    // a smaller value would silently turn pills into rounded rectangles.
    expect(parseFloat(token('--radius-full'))).toBeGreaterThan(100)
  })
})

describe('duration tokens (variables.css)', () => {
  it('defines the three steps at their documented values', () => {
    expect(token('--duration-fast')).toBe('0.1s')
    expect(token('--duration-base')).toBe('0.15s')
    expect(token('--duration-slow')).toBe('0.2s')
  })
})

describe('stacking-order tokens (variables.css)', () => {
  it('defines the named levels at their original literal values', () => {
    // These replaced magic numbers; the values must not drift, or a surface
    // that used to sit above another would silently swap order.
    expect(token('--z-overlay')).toBe('1000')
    expect(token('--z-overlay-raised')).toBe('1001')
    expect(token('--z-header')).toBe('1100')
    expect(token('--z-sheet')).toBe('1200')
    expect(token('--z-preview-tooltip')).toBe('1300')
    expect(token('--z-header-overlay')).toBe('2000')
    expect(token('--z-quote-bar')).toBe('2400')
    expect(token('--z-context-menu-backdrop')).toBe('2499')
    expect(token('--z-context-menu')).toBe('2500')
    expect(token('--z-modal')).toBe('3000')
    expect(token('--z-popover-backdrop')).toBe('9998')
    expect(token('--z-popover')).toBe('9999')
    expect(token('--z-lightbox')).toBe('10000')
  })

  it('keeps each backdrop one below the surface it scrims', () => {
    // A backdrop must sort under its own surface but over everything else;
    // equal values would let the scrim cover the menu it belongs to.
    expect(parseInt(token('--z-context-menu'))).toBe(
      parseInt(token('--z-context-menu-backdrop')) + 1,
    )
    expect(parseInt(token('--z-popover'))).toBe(
      parseInt(token('--z-popover-backdrop')) + 1,
    )
  })

  it('keeps the topmost surfaces above the modal tier', () => {
    // Popovers and the lightbox escape modals on purpose (a menu opened from
    // a dialog must not be clipped by it).
    expect(parseInt(token('--z-popover'))).toBeGreaterThan(parseInt(token('--z-modal')))
    expect(parseInt(token('--z-lightbox'))).toBeGreaterThan(parseInt(token('--z-popover')))
  })
})

describe('shadow tokens (variables.css)', () => {
  it('defines all three elevation steps in every theme', () => {
    // --shadow-lg was referenced (with a hard-coded fallback) long before it
    // was ever defined, so those surfaces ignored the theme and always drew
    // the same shadow. Assert every theme block carries the full set.
    const themeBlocks = css.split(/^\[data-theme="/m).slice(1)
    expect(themeBlocks.length, 'expected theme blocks').toBeGreaterThan(30)
    for (const block of themeBlocks) {
      const name = block.slice(0, block.indexOf('"'))
      for (const step of ['sm', 'md', 'lg']) {
        expect(
          block.includes(`--shadow-${step}:`),
          `theme "${name}" is missing --shadow-${step}`,
        ).toBe(true)
      }
    }
  })

  it('keeps the geometry ordered from sm to lg', () => {
    // Elevation reads through blur radius; equal geometry would make the
    // three steps indistinguishable.
    const blur = (step: string) => {
      const m = token(`--shadow-${step}`).match(/0 \d+px (\d+)px/)
      expect(m, `--shadow-${step} should match "0 <y>px <blur>px"`).not.toBeNull()
      return parseInt(m![1])
    }
    expect(blur('md')).toBeGreaterThan(blur('sm'))
    expect(blur('lg')).toBeGreaterThan(blur('md'))
  })

  it('does not hard-code shadow fallbacks at the call sites', () => {
    // `var(--shadow-lg, 0 8px 32px …)` looked defensive but pinned the shadow
    // to a light-theme value; the token now exists, so call sites must use it
    // bare. Checked across the whole stylesheet set, not just variables.css.
    const sources = ['css/components.css', 'css/layout.css']
    for (const rel of sources) {
      const src = readCss(rel)
      expect(src, `${rel} should not inline a shadow fallback`).not.toMatch(
        /var\(--shadow-[a-z]+,\s*0 /,
      )
    }
  })
})

describe('opacity tokens (variables.css)', () => {
  it('defines the four de-emphasis levels, darkest first', () => {
    // Ordered so a reader can tell "more disabled" from "less": disabled <
    // muted < soft < hover, all below 1.
    expect(token('--opacity-disabled')).toBe('0.4')
    expect(token('--opacity-muted')).toBe('0.5')
    expect(token('--opacity-soft')).toBe('0.7')
    expect(token('--opacity-hover')).toBe('0.9')
  })

  it('keeps every level strictly between hidden and shown', () => {
    // 0 and 1 stay literal at the call sites; a token that reached either end
    // would mean the element is not de-emphasised at all.
    for (const name of ['disabled', 'muted', 'soft', 'hover']) {
      const v = parseFloat(token(`--opacity-${name}`))
      expect(v, `--opacity-${name} should be > 0`).toBeGreaterThan(0)
      expect(v, `--opacity-${name} should be < 1`).toBeLessThan(1)
    }
  })

  it('increases monotonically with emphasis', () => {
    const levels = ['disabled', 'muted', 'soft', 'hover'].map((n) =>
      parseFloat(token(`--opacity-${n}`)),
    )
    for (let i = 1; i < levels.length; i++) {
      expect(levels[i], `${levels[i]} should exceed ${levels[i - 1]}`).toBeGreaterThan(
        levels[i - 1],
      )
    }
  })
})

describe('token hygiene (variables.css)', () => {
  it('does not redeclare static tokens per theme', () => {
    // These are layout/typography values, not colours: they live once in
    // :root. A theme block redefining them would be a copy-paste slip that
    // makes one theme render at a different scale.
    for (const name of [
      '--font-size-md',
      '--font-weight-semibold',
      '--line-height-normal',
      '--space-4',
      '--radius-sm',
      '--duration-base',
    ]) {
      const occurrences = css.split('\n').filter((l) => l.trim().startsWith(`${name}:`))
      expect(occurrences, `${name} should be declared exactly once`).toHaveLength(1)
    }
  })

  it('has no token referencing an undefined token', () => {
    // A `var(--typo)` with no fallback resolves to nothing, so the declaration
    // is dropped silently. Catch the typo at test time instead of in the UI.
    const defined = new Set(
      [...css.matchAll(/^\s*(--[a-z0-9-]+)\s*:/gm)].map((m) => m[1]),
    )
    const referenced = [...css.matchAll(/var\(\s*(--[a-z0-9-]+)/g)].map((m) => m[1])
    const missing = [...new Set(referenced)].filter((n) => !defined.has(n))
    expect(missing, `undefined tokens referenced: ${missing.join(', ')}`).toEqual([])
  })
})

describe('token references across the app', () => {
  // The checks above only read variables.css, so a component could reference a
  // token that does not exist and nothing would notice — which is exactly how
  // `var(--color-text-primary)` (a typo for --text-primary) and a fallback-less
  // `var(--text-tertiary)` survived in the codebase.
  const webRoot = (() => {
    for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
      try {
        readFileSync(join(base, 'css/variables.css'))
        return base
      } catch { /* next */ }
    }
    throw new Error('web root not found from ' + process.cwd())
  })()

  /** Every .css/.vue under css/ and src/, excluding build output and tests. */
  function collectSources(): { path: string; text: string }[] {
    const out: { path: string; text: string }[] = []
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        const full = join(dir, e.name)
        if (e.isDirectory()) {
          if (['node_modules', 'dist', '__tests__', 'vendor-build'].includes(e.name)) continue
          walk(full)
        } else if (/\.(css|vue)$/.test(e.name)) {
          out.push({ path: full.slice(webRoot.length + 1), text: readFileSync(full, 'utf8') })
        }
      }
    }
    walk(join(webRoot, 'css'))
    walk(join(webRoot, 'src'))
    return out
  }

  /** Every .ts/.vue — where a component may set a token at runtime. */
  function collectScripts(): string[] {
    const out: string[] = []
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        const full = join(dir, e.name)
        if (e.isDirectory()) {
          if (['node_modules', 'dist', '__tests__', 'vendor-build'].includes(e.name)) continue
          walk(full)
        } else if (/\.(ts|vue)$/.test(e.name)) {
          out.push(readFileSync(full, 'utf8'))
        }
      }
    }
    walk(join(webRoot, 'src'))
    return out
  }

  const sources = collectSources()

  /** Tokens declared anywhere (variables.css plus any local declaration). */
  const declared = new Set<string>()
  for (const { text } of sources) {
    for (const m of text.matchAll(/(--[a-z0-9-]+)\s*:/g)) declared.add(m[1])
  }

  /**
   * Tokens a component writes at runtime (`style.setProperty('--x')` or a
   * `:style="{ '--x': … }"` binding). They are legitimately absent from
   * variables.css — the component supplies the value — so they are excluded
   * from the "undefined" check. Collected from scripts too, since the setter
   * often lives in a composable rather than the component that reads it.
   */
  const runtimeSet = new Set<string>()
  for (const text of [...sources.map((s) => s.text), ...collectScripts()]) {
    for (const m of text.matchAll(/['"](--[a-z0-9-]+)['"]\s*:/g)) runtimeSet.add(m[1])
    for (const m of text.matchAll(/setProperty\(\s*['"](--[a-z0-9-]+)['"]/g)) runtimeSet.add(m[1])
  }

  it('never references a token that nothing declares', () => {
    const offenders: string[] = []
    for (const { path, text } of sources) {
      // Drop comments first: prose may mention a token family (`var(--bg-*)`)
      // which is not a real reference.
      const code = text
        .replace(/\/\*[\s\S]*?\*\//g, '')
        .replace(/\/\/[^\n]*/g, '')
      for (const m of code.matchAll(/var\(\s*(--[a-z0-9-]+)\s*(,)?/g)) {
        const name = m[1]
        if (declared.has(name) || runtimeSet.has(name)) continue
        offenders.push(`${path}: ${name}${m[2] ? ' (has fallback)' : ' (NO fallback)'}`)
      }
    }
    // A missing token with a fallback is a latent bug (it will not follow the
    // theme); without one it is already broken. Both should be fixed, so both
    // fail — the message marks which is which.
    expect(offenders, `undefined token references:\n${offenders.join('\n')}`).toEqual([])
  })

  it('finds the sources it is meant to guard', () => {
    // Guard against the walk silently matching nothing (e.g. a cwd change),
    // which would make the check above vacuously pass.
    expect(sources.length).toBeGreaterThan(100)
    expect(declared.size).toBeGreaterThan(50)
  })
})
