import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Footer action bars scroll horizontally when the buttons do not fit; they must
 * not wrap (the bar would grow into the content) and must not clip the leftmost
 * action. jsdom applies no stylesheets, so this reads the sources.
 *
 * The four meeting footers have their own guard (meetingFooterButtons.test.ts);
 * these are the shared bars every other surface uses.
 */

const ROOT = join(__dirname, '..', '..', '..', '..', '..')

/** Declarations of the rule whose selector starts at `marker`. */
function declsAt(file: string, marker: string): Record<string, string> {
  // Strip comments first: a `;` inside one would split a declaration in half.
  const css = readFileSync(join(ROOT, file), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '')
  const at = css.indexOf(marker)
  if (at < 0) throw new Error(`${file}: selector ${marker} not found`)
  const open = css.indexOf('{', at)
  const close = css.indexOf('}', open)
  const out: Record<string, string> = {}
  for (const decl of css.slice(open + 1, close).split(';')) {
    const i = decl.indexOf(':')
    if (i < 0) continue
    out[decl.slice(0, i).trim()] = decl.slice(i + 1).trim().replace(/\s+/g, ' ')
  }
  return out
}

const BARS: Array<[string, string]> = [
  ['web/src/assets/modal-card.css', '.modal-footer,'],
  ['web/src/components/common/BottomSheet.vue', '.bs-panel > .bs-footer'],
  ['web/src/components/task/TaskDetailPage.vue', '.detail-actions'],
  ['web/src/components/task/TaskExecDetail.vue', '.exec-detail-actions'],
  ['web/src/components/task/TaskFormPage.vue', '.form-footer'],
  ['web/src/components/settings/SettingsPage.vue', '.settings-page__footer'],
  ['web/src/components/common/DialogOverlay.vue', '.dlg-actions'],
]

describe('footer action bars scroll instead of wrapping', () => {
  it.each(BARS)('%s (%s) is a nowrap scroller', (file, marker) => {
    const d = declsAt(file, marker)
    expect(d['flex-wrap']).toBe('nowrap')
    expect(d['overflow-x']).toBe('auto')
  })

  it('keeps pills at their intrinsic width so the row overflows and scrolls', () => {
    expect(declsAt('web/src/assets/modal-footer-btn.css', '.fbtn {')['flex-shrink']).toBe('0')
  })

  it('hides the scrollbar itself on the footer bars', () => {
    // A footer is one row of pills; a painted scrollbar steals height and reads
    // as an unrelated control. The standard property covers Chromium/Firefox,
    // the ::-webkit- rule covers WebKit (which ignores scrollbar-width).
    const css = 'web/src/assets/modal-footer-btn.css'
    expect(declsAt(css, '.modal-footer,')['scrollbar-width']).toBe('none')
    expect(declsAt(css, '.meeting-footer::-webkit-scrollbar')['display']).toBe('none')
  })
})
