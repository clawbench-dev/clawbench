import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Settings row typography must stay aligned with the other panels.
 *
 * The drift this pins: every settings row label (category rows, item labels,
 * option labels, group-panel labels, agent names) was sized `--font-size-xl`
 * (15px). That token is also the document base (`base.css` sizes html/body from
 * it), so those labels rendered at the raw default — visibly larger than the
 * 13–14px primary text every other panel uses for the same role:
 *
 *   task  .task-item-name    14px  / .task-item-meta  12px
 *   forge .forge-row-text    14px  / .forge-row-meta  12px
 *   git   .git-file-name     13px  / .git-file-dir    11px
 *   settings (was)           15px  / .settings-item__desc 12px
 *
 * The token file itself states the intent: `--font-size-md: 13px; /* default UI
 * text: chat messages, settings *\/`. Row labels now use `--font-size-lg`
 * (14px), matching the task/forge row title, keeping the 14px→12px primary/
 * secondary step intact.
 *
 * Not swept up, and deliberately so:
 *   - `.settings-item__option-check` / `.group-panel__option-check` are a "✓"
 *     glyph, not text.
 *   - `.settings-item__editor-toggle` sizes a 16px Eye icon.
 *   - `.about-brand__name` and the dialog headers are 2xl headings.
 *   - inputs keep their own size; the sweep below only flags row labels, and a
 *     separate check pins that an input is never smaller than its label.
 *
 * jsdom has no CSS engine, so this reads the raw SFC styles, following
 * designTokens.css.test.ts. cwd differs between a bare `vitest` run (web/) and
 * scripts/vitest-run.sh (repo root), so probe both.
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

/**
 * List a directory under web/, probing both candidate roots for the same
 * reason readWebFile does — a bare `vitest` from web/ vs `npm test` from the
 * repo root disagree on cwd.
 */
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

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/**
 * Every `<style>` block in a .vue source, concatenated.
 *
 * Some settings components have TWO blocks (a scoped one and a global one for
 * teleported content), so reading only the first would silently miss rules.
 */
function styleBlocks(sfc: string): string {
  return [...sfc.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(m => m[1]).join('\n')
}

/**
 * The declaration block of the first rule that targets `.cls`.
 *
 * The selector may be a comma-separated group (e.g.
 * `.settings-item__number-input,\n.settings-item__text-input { … }`), so each
 * comma-separated part is compared individually rather than the whole selector
 * string — exact matching would miss a class that only appears in a group.
 */
function baseRule(sfc: string, cls: string): string | null {
  const target = `.${cls}`
  for (const m of stripComments(styleBlocks(sfc)).matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    const parts = m[1].split(',').map(s => s.trim())
    if (parts.includes(target)) return m[2]
  }
  return null
}

const SETTINGS_DIR = 'src/components/settings'

/** Row labels that must render at the shared body size, with their file. */
const LABEL_RULES: Array<[file: string, cls: string]> = [
  ['SettingsIndex.vue', 'settings-index__label'],
  ['SettingsItem.vue', 'settings-item__label'],
  ['SettingsItem.vue', 'settings-item__option-label'],
  ['SettingsGroupPanel.vue', 'group-panel__enable-label'],
  ['SettingsGroupPanel.vue', 'group-panel__entry-label'],
  ['SettingsGroupPanel.vue', 'group-panel__option-label'],
  ['SettingsAgentsIndex.vue', 'settings-agents-index__name'],
  ['SettingsAgentsIndex.vue', 'settings-agents-index__rescan-label'],
  ['WallpaperSetting.vue', 'settings-item__label'],
]

describe('settings row labels use the shared body size', () => {
  it.each(LABEL_RULES)('%s .%s is --font-size-lg, not --font-size-xl', (file, cls) => {
    const decls = baseRule(readWebFile(`${SETTINGS_DIR}/${file}`), cls)
    expect(decls, `${file} .${cls} rule must exist`).not.toBeNull()
    expect(decls, `${file} .${cls} must use --font-size-lg`).toMatch(
      /font-size:\s*var\(--font-size-lg\)/,
    )
    expect(decls, `${file} .${cls} must not use --font-size-xl`).not.toMatch(
      /font-size:\s*var\(--font-size-xl\)/,
    )
  })

  it('matches the task and forge row-title size', () => {
    // The reference the settings labels are aligned to. If those are retuned,
    // this guard should be revisited rather than silently diverging.
    const task = baseRule(readWebFile('src/components/task/TaskListPage.vue'), 'task-item-name')
    expect(task, '.task-item-name must exist').not.toBeNull()
    expect(task).toMatch(/font-size:\s*var\(--font-size-lg\)/)

    const forgeText = stripComments(readWebFile('css/components.css')).match(
      /\.forge-row-text\s*\{([^}]*)\}/,
    )
    expect(forgeText, '.forge-row-text must exist').not.toBeNull()
    expect(forgeText![1]).toMatch(/font-size:\s*var\(--font-size-lg\)/)
  })

  it('keeps a clear primary/secondary step (14px label over 12px desc)', () => {
    const label = baseRule(readWebFile(`${SETTINGS_DIR}/SettingsItem.vue`), 'settings-item__label')
    const desc = baseRule(readWebFile(`${SETTINGS_DIR}/SettingsItem.vue`), 'settings-item__desc')
    expect(label).toMatch(/font-size:\s*var\(--font-size-lg\)/)
    expect(desc).toMatch(/font-size:\s*var\(--font-size-sm\)/)
  })
})

describe('inputs are never smaller than their label', () => {
  /** Real text-entry rules: the value the user types must not shrink below the
   *  label that names the field. */
  const INPUTS: Array<[file: string, cls: string]> = [
    ['SettingsItem.vue', 'settings-item__text-input'],
    ['CopyAgentDialog.vue', 'copy-agent-dialog__input'],
    ['PasswordChangeDialog.vue', 'password-dialog__input'],
  ]

  it.each(INPUTS)('%s .%s is at least --font-size-lg', (file, cls) => {
    const decls = baseRule(readWebFile(`${SETTINGS_DIR}/${file}`), cls)
    expect(decls, `${file} .${cls} rule must exist`).not.toBeNull()
    // xl(15px) or 2xl(16px) are both fine; anything below lg(14px) is not.
    expect(
      decls,
      `${file} .${cls} must not render smaller than the row label (lg = 14px)`,
    ).not.toMatch(/font-size:\s*var\(--font-size-(2xs|xs|sm|md)\)/)
  })
})

describe('no settings row label was left behind', () => {
  it('has no label-ish rule still sized --font-size-xl', () => {
    // A sweep, so a new settings row added later cannot reintroduce the drift.
    // Selectors for non-text elements (✓ glyph, icon-sized button) and headings
    // (2xl) are handled elsewhere and are not flagged here.
    const offenders: string[] = []
    for (const file of readWebDir(SETTINGS_DIR)) {
      if (!file.endsWith('.vue')) continue
      const css = stripComments(styleBlocks(readWebFile(`${SETTINGS_DIR}/${file}`)))
      for (const m of css.matchAll(/([^{}]+)\{([^}]*)\}/g)) {
        const selector = m[1].trim()
        if (!/font-size:\s*var\(--font-size-xl\)/.test(m[2])) continue
        if (/input|toggle|icon|check|slider/i.test(selector)) continue
        offenders.push(`${file}: ${selector}`)
      }
    }
    expect(
      offenders,
      `these still use --font-size-xl (15px) for row text:\n${offenders.join('\n')}`,
    ).toEqual([])
  })
})
