import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The settings panel header must stay aligned with the task panel header.
 *
 * Both are 36px bars over a bg-primary page, with the same padding, gap and
 * bottom border, and the same level structure:
 *
 *   root level  → section glyph + title
 *   drilled down → breadcrumb whose ROOT crumb carries the glyph
 *
 * The settings header used to differ in three ways: a 16px display title
 * (`--font-size-2xl`), a 36px back button that the task header does not have,
 * and a breadcrumb with its own gap/hover treatment. The back button is gone —
 * the root crumb is the way back, running through the same unsaved-changes
 * guard — so the two headers are now the same shape.
 *
 * jsdom has no CSS engine, so these are source-sniffing checks (the pattern
 * used by forgeDetailChrome.css.test.ts / countBadge.css.test.ts). cwd differs
 * between a bare `vitest` run (web/) and scripts/vitest-run.sh (repo root), so
 * probe both.
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

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/**
 * The `<style>` block of a .vue source.
 *
 * A whole SFC cannot be scanned for CSS: the template contains `{`/`}` in
 * interpolations, which the rule regex would mis-parse as declaration blocks.
 */
function styleBlock(sfc: string): string {
  const m = sfc.match(/<style[^>]*>([\s\S]*?)<\/style>/)
  return m ? m[1] : ''
}

/**
 * The declaration block of the first rule whose selector is exactly `.cls`.
 *
 * Selectors are compared after trimming rather than anchored with a lookbehind,
 * so a rule that follows a comment (stripped to nothing, leaving only a newline
 * before the selector) is still found.
 */
function baseRule(sfc: string, cls: string): string | null {
  const target = `.${cls}`
  for (const m of stripComments(styleBlock(sfc)).matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    if (m[1].trim() === target) return m[2]
  }
  return null
}

const settingsPage = readWebFile('src/components/settings/SettingsPage.vue')
const taskList = readWebFile('src/components/task/TaskListPage.vue')
const settingsCrumb = readWebFile('src/components/settings/SettingsBreadcrumb.vue')
const taskCrumb = readWebFile('src/components/task/TaskBreadcrumb.vue')

/** The declarations every header bar must share to read as the same bar. */
const BAR_GEOMETRY = [
  /height:\s*var\(--header-height\)/,
  /padding:\s*0 var\(--space-2\) 0 var\(--space-6\)/,
  /gap:\s*var\(--space-3\)/,
  /background:\s*var\(--bg-primary\)/,
  /border-bottom:\s*1px solid var\(--border-color/,
  /flex-shrink:\s*0/,
]

describe('settings header geometry matches the task header', () => {
  it('declares the same bar geometry as .list-header', () => {
    const decls = baseRule(settingsPage, 'settings-page__header')
    expect(decls, '.settings-page__header rule must exist').not.toBeNull()
    for (const prop of BAR_GEOMETRY) {
      expect(decls, `settings header must declare ${prop}`).toMatch(prop)
    }
  })

  it('keeps the task header as the reference shape', () => {
    // If the task header is retuned, this guard should be revisited — pin it so
    // the comparison cannot silently become "both wrong".
    const decls = baseRule(taskList, 'list-header')
    expect(decls, '.list-header rule must exist').not.toBeNull()
    for (const prop of BAR_GEOMETRY) {
      expect(decls, `task header must declare ${prop}`).toMatch(prop)
    }
  })

  it('uses the body-sized title, not a display heading', () => {
    // A 16px (--font-size-2xl) title in a 36px bar read as a competing page
    // title next to the task header's --font-size-md.
    const decls = baseRule(settingsPage, 'settings-page__title')
    expect(decls, '.settings-page__title rule must exist').not.toBeNull()
    expect(decls).toMatch(/font-size:\s*var\(--font-size-md\)/)
    expect(decls).not.toMatch(/font-size:\s*var\(--font-size-2xl\)/)
  })

  it('uses the same root glyph size as the task header', () => {
    // Task header: <Clock :size="14">. Settings must not go back to :size="20".
    const settingsIcon = settingsPage.match(/class="settings-page__header-icon"[^>]*/)
    expect(settingsIcon, 'the settings header icon must exist').not.toBeNull()
    const settingsSize = settingsPage.match(/<Settings :size="(\d+)"/)
    expect(settingsSize, 'the Settings icon must be rendered with an explicit size').not.toBeNull()
    expect(settingsSize![1]).toBe('14')

    const taskSize = taskCrumb.match(/<Clock :size="(\d+)"/)
    expect(taskSize, 'the task root glyph must be rendered with an explicit size').not.toBeNull()
    expect(settingsSize![1]).toBe(taskSize![1])
  })
})

describe('settings header has no back button', () => {
  it('does not render a back button in the header', () => {
    // The root crumb is the pointer route back; a back button beside a
    // breadcrumb whose first crumb already does the same thing is redundant.
    expect(settingsPage).not.toContain('settings-page__back')
  })

  it('does not leave the back-button rule behind in the stylesheet', () => {
    expect(baseRule(settingsPage, 'settings-page__back')).toBeNull()
  })

  it('still wires the back gesture through the unsaved-changes guard', () => {
    // Removing the button must not remove the guard: the edge-swipe / system
    // back path still calls handleBack.
    expect(settingsPage).toMatch(/useFeatureBackHandler\(/)
    expect(settingsPage).toMatch(/handleBack/)
    expect(settingsPage).toMatch(/confirmDiscardIfDirty/)
  })

  it('keeps the root crumb as a working exit', () => {
    // The root crumb must still emit navigate for depth 0 (the settings index).
    expect(settingsCrumb).toMatch(/i\s*<\s*crumbs\.length\s*-\s*1\s*&&\s*\$emit\('navigate'/)
  })
})

describe('breadcrumb sub-levels match between settings and task', () => {
  it('puts the section glyph on the settings breadcrumb root crumb', () => {
    expect(settingsCrumb).toMatch(/i === 0/)
    expect(settingsCrumb).toMatch(/class="crumb-icon"/)
    // A missing import renders no svg at all.
    const imports = settingsCrumb.match(/import\s*\{([^}]*)\}\s*from\s*'lucide-vue-next'/)
    expect(imports, 'the breadcrumb must import its glyph').not.toBeNull()
    expect(imports![1]).toContain('Settings')
  })

  it('gives the crumb the same flex-row treatment as the task crumb', () => {
    for (const [label, src] of [['settings', settingsCrumb], ['task', taskCrumb]] as const) {
      const decls = baseRule(src, 'crumb')
      expect(decls, `${label} .crumb rule must exist`).not.toBeNull()
      expect(decls, `${label} .crumb must be a flex row`).toMatch(/display:\s*inline-flex/)
      expect(decls, `${label} .crumb must centre its glyph`).toMatch(/align-items:\s*center/)
      expect(decls, `${label} .crumb must gap glyph from label`).toMatch(/gap:\s*var\(--space-3\)/)
    }
  })

  it('keeps the breadcrumb glyph from being squeezed', () => {
    expect(baseRule(settingsCrumb, 'crumb-icon')).toMatch(/flex-shrink:\s*0/)
    expect(baseRule(taskCrumb, 'crumb-icon')).toMatch(/flex-shrink:\s*0/)
  })

  it('drops the extra container gap so both trails share one rhythm', () => {
    // The task breadcrumb has no container gap (the crumb padding + separator
    // margin provide the spacing); the settings one had `gap: var(--space-2)`.
    const settingsDecls = baseRule(settingsCrumb, 'settings-breadcrumb')
    expect(settingsDecls, '.settings-breadcrumb rule must exist').not.toBeNull()
    expect(settingsDecls).not.toMatch(/gap:/)

    const taskDecls = baseRule(taskCrumb, 'task-breadcrumb')
    expect(taskDecls, '.task-breadcrumb rule must exist').not.toBeNull()
    expect(taskDecls).not.toMatch(/gap:/)
  })

  it('hovers crumbs with the same token as the task breadcrumb', () => {
    const settingsHover = stripComments(settingsCrumb).match(/\.crumb:hover\s*\{([^}]*)\}/)
    expect(settingsHover, 'a settings crumb hover rule must exist').not.toBeNull()
    expect(settingsHover![1]).toMatch(/background:\s*var\(--bg-secondary/)

    const taskHover = stripComments(taskCrumb).match(/\.crumb\.clickable:hover\s*\{([^}]*)\}/)
    expect(taskHover, 'a task crumb hover rule must exist').not.toBeNull()
    expect(taskHover![1]).toMatch(/background:\s*var\(--bg-secondary/)
  })
})
