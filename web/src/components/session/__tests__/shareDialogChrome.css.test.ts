import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Regression guard: the share-dialog chrome must be declared GLOBALLY.
 *
 * Why this test exists: SessionShareDialog reused the class names of
 * ShareLinkDialog's notice banner and link bar, but those rules lived in
 * ShareLinkDialog's `<style scoped>` block. A scoped rule only applies to the
 * component that declares it, so the new dialog rendered both blocks unstyled —
 * measured in a browser, `.share-notice` had `background: rgba(0,0,0,0)` and
 * `padding: 0`, and `.share-dialog-link-input` fell back to the UA's
 * `1.739739px inset` border with `font-family: Arial`.
 *
 * Unit tests passed because they assert text and structure, not computed style.
 * This is the same failure mode the forge chrome hit (see
 * `components/forge/__tests__/forgeDetailChrome.css.test.ts`), so the shape of
 * this guard mirrors that one deliberately.
 *
 * The check is narrow and robust: it asserts each shared class appears as the
 * SUBJECT of a global rule in web/css/components.css, has a BASE rule (not just
 * a modifier), and is not re-declared in either dialog's scoped block.
 */

/**
 * Classes shared by both share dialogs. They live in a family stylesheet
 * (src/assets/share-dialog.css) imported by both dialogs, NOT in a global
 * css/*.css file — so this guard reads that asset directly.
 */
const SHARED_CHROME = [
  // Body / async states
  'share-dialog-body',
  'share-dialog-hint',
  'share-dialog-error',
  // Notice banner
  'share-notice',
  'share-notice-icon',
  'share-notice-info',
  'share-notice-content',
  'share-notice-text',
  'share-notice-divider',
  'share-notice-warning',
  // Link bar
  'share-dialog-link-bar',
  'share-dialog-link-input-wrap',
  'share-dialog-link-input',
  'share-dialog-link-btn',
  'share-dialog-spin',
]

/**
 * Classes a stylesheet DECLARES (gives the element its own styling).
 *
 * Only a selector with NO ancestor part counts. A descendant override like
 * `:root[data-theme-base='dark'] .share-notice-warning` only re-tints under a
 * condition and must NOT satisfy the "declared globally" check — counting it
 * would hide a missing base rule, which is the false negative this guard exists
 * to prevent.
 */
function declaredSubjects(source: string): Set<string> {
  const out = new Set<string>()
  const css = source.replace(/\/\*[\s\S]*?\*\//g, '')
  for (const m of css.matchAll(/([^{}]+)\{/g)) {
    for (const selector of m[1].split(',')) {
      const trimmed = selector.trim()
      if (!trimmed) continue
      if (/[\s>+~]/.test(trimmed)) continue
      for (const cls of trimmed.matchAll(/\.([a-z][a-z0-9-]*)/g)) out.add(cls[1])
    }
  }
  return out
}

/**
 * Classes with a BASE rule — a selector that is exactly `.class`.
 *
 * Stricter than declaredSubjects: `.share-notice-info > .share-notice-icon` is
 * whitespace-free-ish but a compound/descendant, so it cannot stand in for the
 * base `.share-notice-icon` rule.
 */
function baseRuleClasses(source: string): Set<string> {
  const out = new Set<string>()
  const css = source.replace(/\/\*[\s\S]*?\*\//g, '')
  for (const m of css.matchAll(/([^{}]+)\{/g)) {
    for (const selector of m[1].split(',')) {
      const trimmed = selector.trim()
      if (/^\.[a-z][a-z0-9-]*$/.test(trimmed)) out.add(trimmed.slice(1))
    }
  }
  return out
}

function read(rel: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, rel), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

/** The `<style scoped>` body of a component, or '' when it has none. */
function scopedBlock(rel: string): string {
  const src = read(rel)
  const marker = '<style scoped>'
  const i = src.indexOf(marker)
  if (i === -1) return ''
  const after = src.slice(i + marker.length)
  return after.includes('</style>') ? after.slice(0, after.indexOf('</style>')) : after
}

const SHARED_STYLESHEET = 'src/assets/share-dialog.css'

const DIALOGS = [
  'src/components/file/ShareLinkDialog.vue',
  'src/components/session/SessionShareDialog.vue',
]

describe('share-dialog chrome lives in the shared asset', () => {
  const sharedSource = read(SHARED_STYLESHEET)

  it('declares every shared chrome class in the shared asset', () => {
    const subjects = declaredSubjects(sharedSource)
    const missing = SHARED_CHROME.filter(c => !subjects.has(c))
    expect(
      missing,
      `these must be declared in ${SHARED_STYLESHEET}, not scoped to one dialog: ${missing.join(', ')}`,
    ).toEqual([])
  })

  it('gives every shared chrome class a BASE rule, not just a modifier', () => {
    // A compound rule such as `.share-notice-info > .share-notice-icon` only
    // matches elements inside the notice, so it cannot substitute for the base
    // `.share-notice-icon` rule an element with just that class needs.
    const base = baseRuleClasses(sharedSource)
    const missing = SHARED_CHROME.filter(c => !base.has(c))
    expect(
      missing,
      `these are only styled by a modifier/descendant rule, so an element with just ` +
        `this class gets no styling: ${missing.join(', ')}`,
    ).toEqual([])
  })

  it('keeps the notice box geometry in the shared asset', () => {
    // Anchored to the properties that DEFINE the notice box, so this fails if
    // the rule is ever moved back into a scoped block or deleted.
    const rule = sharedSource.match(/\.share-notice\s*\{([\s\S]*?)\}/)
    expect(rule, '.share-notice base rule must exist').not.toBeNull()
    for (const prop of [
      'display: flex',
      'align-items: flex-start',
      'padding: 9px 11px',
      'border-radius: var(--radius-sm)',
    ]) {
      expect(rule![1], `.share-notice must declare ${prop}`).toContain(prop)
    }
  })

  it('keeps the link-input geometry in the shared asset', () => {
    const rule = sharedSource.match(/\.share-dialog-link-input\s*\{([\s\S]*?)\}/)
    expect(rule, '.share-dialog-link-input base rule must exist').not.toBeNull()
    for (const prop of [
      'padding:7px 84px 7px var(--space-5)',
      'border-radius: var(--radius-sm, 6px)',
      'font-family: var(--font-mono)',
    ]) {
      expect(rule![1], `.share-dialog-link-input must declare ${prop}`).toContain(prop)
    }
  })

  it('keeps the spin animation available to both dialogs', () => {
    // The regenerate button renders RefreshCw with this class; without the
    // keyframes it silently does not spin.
    expect(sharedSource).toContain('@keyframes share-dialog-spin')
    const spin = sharedSource.match(/\.share-dialog-spin\s*\{([\s\S]*?)\}/)
    expect(spin, '.share-dialog-spin rule must exist').not.toBeNull()
    expect(spin![1]).toContain('animation: share-dialog-spin')
  })

  it('does not leave the shared chrome in either dialog scoped block', () => {
    // The defining property per class — a deliberate local override of some
    // OTHER property stays legitimate and must not be flagged.
    const mustBeGlobal: Array<{ cls: string; props: string[] }> = [
      { cls: 'share-notice', props: ['padding: 9px 11px', 'border-radius: var(--radius-sm)'] },
      { cls: 'share-notice-info', props: ['background: color-mix'] },
      { cls: 'share-notice-warning', props: ['font-weight: var(--font-weight-medium)'] },
      { cls: 'share-dialog-link-input', props: ['font-family: var(--font-mono)', 'border-radius: var(--radius-sm, 6px)'] },
      { cls: 'share-dialog-link-btn', props: ['position: absolute'] },
      { cls: 'share-dialog-link-input-wrap', props: ['position: relative'] },
      { cls: 'share-dialog-body', props: ['display: flex'] },
    ]

    for (const rel of DIALOGS) {
      const block = scopedBlock(rel)
      for (const { cls, props } of mustBeGlobal) {
        const rule = block.match(new RegExp(`\\.${cls}\\s*\\{([\\s\\S]*?)\\}`))
        if (!rule) continue
        for (const prop of props) {
          expect(
            rule[1].includes(prop),
            `${rel} re-declares .${cls} (${prop}) — it belongs to the shared chrome in ${SHARED_STYLESHEET}`,
          ).toBe(false)
        }
      }
    }
  })
})

describe('share dialogs use the shared ModalDialog shell', () => {
  it('both pass open/title/z-index and use the default + footer slots', () => {
    for (const rel of DIALOGS) {
      const src = read(rel)
      expect(src, `${rel} must use ModalDialog`).toContain('<ModalDialog')
      expect(src, `${rel} must forward open`).toContain(':open="open"')
      expect(src, `${rel} must forward close`).toContain("@close=")
      expect(src, `${rel} must use the footer slot`).toContain('<template #footer>')
    }
  })

  it('uses the shared .fbtn pill for dialog controls', () => {
    // The footer actions and the select-all control all come from the shared
    // modal-footer-btn stylesheet loaded by ModalDialog; a hand-rolled button
    // would drift from every other dialog.
    const link = read(DIALOGS[0])
    expect(link).toContain('fbtn')
    const session = read(DIALOGS[1])
    expect(session).toContain('fbtn')
    // The invented one-off button class must be gone.
    expect(session).not.toContain('session-share-dialog-link-btn')
  })
})
