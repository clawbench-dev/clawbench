import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Contract for the shared settings-panel controls.
 *
 * Why this exists: the switch was declared verbatim in BOTH SettingsItem.vue
 * and WallpaperSetting.vue (same class name, byte-identical rules) and a third
 * time in SettingsGroupPanel.vue under `.group-panel__switch`; the slider trio
 * (slider / slider-value / slider-reset) was likewise copied into two files.
 * A size change had to be made in two or three places and silently diverged
 * when one was missed — which is exactly what happened when the controls were
 * shrunk to match the `.fbtn` button height.
 *
 * They now live once in the global sheet (css/components.css), and the
 * components keep only layout. The trap this guards is the same one
 * countBadge.css.test.ts documents: a scoped rule compiles to `.foo[data-v-x]`
 * (0,2,0) and OUTRANKS the global single class (0,1,0), so re-adding geometry
 * in a component's scoped block would silently win and undo the shared size,
 * with nothing failing. That is asserted here.
 *
 * jsdom has no CSS engine, so these are source-sniffing checks. components.css
 * lives outside the vitest source root (web/css), so it is read off disk; the
 * cwd differs between a bare `vitest` run (web/) and scripts/vitest-run.sh
 * (repo root), so probe both.
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

const SETTINGS_DIR = 'src/components/settings'
const SHARED_CSS = 'css/components.css'

const componentsCss = readWebFile(SHARED_CSS)

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Every `<style>` block in a .vue source, concatenated. */
function styleBlocks(sfc: string): string {
  return [...sfc.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(m => m[1]).join('\n')
}

/**
 * Declarations of the first rule in `css` whose selector is exactly `.cls`.
 * Handles comma-separated selector groups (e.g. the number/text input pair).
 */
function baseRule(css: string, cls: string): string | null {
  const target = `.${cls}`
  for (const m of stripComments(css).matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    const parts = m[1].split(',').map(s => s.trim())
    if (parts.includes(target)) return m[2]
  }
  return null
}

/** Numeric px value of a `prop: Npx` declaration, or NaN. */
function px(decls: string, prop: string): number {
  const m = decls.match(new RegExp(`(?:^|;)\\s*${prop}:\\s*(-?[\\d.]+)px`))
  return m ? Number(m[1]) : NaN
}

/** Controls whose SHAPE must be owned solely by the shared sheet. */
const SHARED_CONTROLS = [
  'settings-item__switch',
  'settings-item__switch-track',
  'settings-item__slider',
  'settings-item__slider-value',
  'settings-item__slider-reset',
]

/** Shape properties a scoped rule must never re-declare for a shared control. */
const SHAPE_PROPS = ['width', 'height', 'padding', 'border-radius', 'font-size']

describe('settings controls are defined once, globally', () => {
  it.each(SHARED_CONTROLS)('.%s is declared in the shared sheet', (cls) => {
    expect(baseRule(componentsCss, cls), `.${cls} must exist in ${SHARED_CSS}`).not.toBeNull()
  })

  it('no settings component re-declares a shared control\'s shape', () => {
    // A scoped `.settings-item__switch { width: ... }` would compile to
    // (0,2,0) and silently beat the global rule (0,1,0).
    const offenders: string[] = []
    for (const file of readWebDir(SETTINGS_DIR)) {
      if (!file.endsWith('.vue')) continue
      const css = stripComments(styleBlocks(readWebFile(`${SETTINGS_DIR}/${file}`)))
      for (const m of css.matchAll(/([^{}]+)\{([^}]*)\}/g)) {
        const selectors = m[1].split(',').map(s => s.trim())
        for (const cls of SHARED_CONTROLS) {
          // Only the plain single-class selector is "the shared rule"; a
          // layout-only companion (e.g. `.settings-item__slider-row`) is fine.
          if (!selectors.includes(`.${cls}`)) continue
          for (const prop of SHAPE_PROPS) {
            if (new RegExp(`(?:^|;)\\s*${prop}\\s*:`).test(m[2])) {
              offenders.push(`${file}: .${cls} re-declares ${prop}`)
            }
          }
        }
      }
    }
    expect(
      offenders,
      `these belong to the shared sheet (${SHARED_CSS}), not a scoped block:\n${offenders.join('\n')}`,
    ).toEqual([])
  })

  it('the third switch copy under .group-panel__switch is gone', () => {
    expect(componentsCss).not.toMatch(/\.group-panel__switch\s*\{/)
    for (const file of readWebDir(SETTINGS_DIR)) {
      if (!file.endsWith('.vue')) continue
      expect(
        readWebFile(`${SETTINGS_DIR}/${file}`),
        `${file} must reuse .settings-item__switch, not its own copy`,
      ).not.toContain('group-panel__switch')
    }
  })
})

describe('settings controls are sized to the .fbtn control height', () => {
  it('the switch is 44x26 with a 22px thumb travelling 18px', () => {
    const sw = baseRule(componentsCss, 'settings-item__switch')!
    expect(px(sw, 'width')).toBe(44)
    expect(px(sw, 'height')).toBe(26)

    const thumb = componentsCss.match(
      /\.settings-item__switch-track::after\s*\{([^}]*)\}/,
    )![1]
    expect(px(thumb, 'width')).toBe(22)
    expect(px(thumb, 'height')).toBe(22)
    // Travel must match width - thumb - 2*gap, or the knob over/undershoots.
    const travel = Number(
      componentsCss.match(
        /\.settings-item__switch-input:checked \+ \.settings-item__switch-track::after\s*\{[^}]*translateX\((-?[\d.]+)px\)/,
      )?.[1],
    )
    expect(travel).toBe(44 - 22 - 2 * 2)
  })

  it('the slider is 100px wide', () => {
    expect(px(baseRule(componentsCss, 'settings-item__slider')!, 'width')).toBe(100)
  })

  it('inputs are 30px tall and stay RECTANGULAR, not pill-shaped', () => {
    // 30px matches the .fbtn button height; --radius-sm keeps a field visually
    // distinct from a clickable pill button. Inputs are single-definition (they
    // were never duplicated), so they live in the component, not the shared
    // sheet — this asserts the size wherever they are declared.
    const input =
      baseRule(componentsCss, 'settings-item__number-input') ??
      baseRule(styleBlocks(readWebFile(`${SETTINGS_DIR}/SettingsItem.vue`)), 'settings-item__number-input')
    expect(input, 'the number-input rule must exist').not.toBeNull()
    expect(input!).toContain('height: 30px;')
    expect(input!).toContain('border-radius: var(--radius-sm);')
    expect(input!, 'an input must not use the pill radius').not.toContain('--radius-full')
  })
})

describe('settings buttons use the shared .fbtn pill', () => {
  it('.settings-item__action no longer owns any geometry', () => {
    // The wallpaper row buttons are `.fbtn` now; a leftover scoped rule here
    // would re-square them without failing anything else.
    const src = readWebFile(`${SETTINGS_DIR}/WallpaperSetting.vue`)
    const rule = baseRule(styleBlocks(src), 'settings-item__action')
    if (rule === null) return // no rule at all is the expected end state
    for (const prop of SHAPE_PROPS) {
      expect(rule, `.settings-item__action must not re-declare ${prop}`).not.toMatch(
        new RegExp(`(?:^|;)\\s*${prop}\\s*:`),
      )
    }
  })

  it('the wallpaper and editor buttons carry the .fbtn class', () => {
    const wallpaper = readWebFile(`${SETTINGS_DIR}/WallpaperSetting.vue`)
    expect(wallpaper).toMatch(/class="fbtn fbtn-primary settings-item__action"/)
    expect(wallpaper).toMatch(/class="fbtn settings-item__action"/)

    const item = readWebFile(`${SETTINGS_DIR}/SettingsItem.vue`)
    expect(item).toMatch(/class="fbtn fbtn-primary settings-item__editor-confirm"/)
    // The component must load the pill stylesheet itself rather than depend on
    // an ancestor happening to have imported it.
    expect(item).toMatch(/import '@\/assets\/modal-footer-btn\.css'/)
  })
})
