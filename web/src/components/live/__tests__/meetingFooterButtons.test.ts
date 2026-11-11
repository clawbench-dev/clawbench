import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The meeting tab's footer actions must use the same button language as the
 * task tab's action bar (`.fbtn` pills in a flat bar), not a local
 * `.meeting-btn` look-alike.
 *
 * jsdom applies no stylesheets, so a component test cannot see this drift —
 * and the drift is invisible to type checking too (both are just class names).
 * This reads the sources instead: the five meeting screens each declare their
 * own scoped `.meeting-footer`, so the bar is compared against the task bar
 * declaration by declaration, and the markup is checked for stray classes.
 */

const ROOT = join(__dirname, '..', '..', '..', '..', '..')

const MEETING_SCREENS = [
  'MeetingList.vue',
  'MeetingCreate.vue',
  'MeetingJoin.vue',
  'MeetingStage.vue',
  'MeetingDetail.vue',
]

function source(relative: string): string {
  return readFileSync(join(ROOT, relative), 'utf8')
}

function meetingSource(file: string): string {
  return source(join('web/src/components/live', file))
}

/** `var(--x, 6px)` and `var(--x)` are the same decision — only the token counts. */
function stripFallbacks(value: string): string {
  return value.replace(/var\((--[\w-]+)(?:,[^()]*)?\)/g, 'var($1)')
}

/** Declarations of one rule, as `prop: value`. */
function ruleDecls(css: string, selector: string): Record<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))
  if (!m) throw new Error(`rule ${selector} not found`)
  const out: Record<string, string> = {}
  for (const decl of m[1].split(';')) {
    const i = decl.indexOf(':')
    if (i < 0) continue
    const prop = decl.slice(0, i).trim()
    const value = stripFallbacks(decl.slice(i + 1).trim()).replace(/\s+/g, ' ')
    if (prop) out[prop] = value
  }
  return out
}

function styleOf(relative: string): string {
  const src = source(relative)
  const start = src.indexOf('<style')
  if (start < 0) throw new Error(`${relative} has no <style> block`)
  return src.slice(start)
}

function meetingStyleOf(file: string): string {
  return styleOf(join('web/src/components/live', file))
}

/** The `<div class="meeting-footer">…</div>` block. The footer holds buttons only. */
function footerBlock(file: string): string {
  // `v-if` may precede the class (会议中 only draws its bar while a meeting runs),
  // so the opening tag is matched loosely; the bar itself is still the same one.
  const m = meetingSource(file).match(/<div\b[^>]*class="meeting-footer"[^>]*>([\s\S]*?)\n\s*<\/div>/)
  if (!m) throw new Error(`${file} has no .meeting-footer block`)
  return m[1]
}

function footerButtonClasses(file: string): string[][] {
  // `\sclass=` (not `\bclass=`) so a `:class="…"` binding is not mistaken for a
  // static class list — the point is to see the literal classes.
  return [...footerBlock(file).matchAll(/<button\b[^>]*?\sclass="([^"]*)"/g)].map((m) =>
    m[1].split(/\s+/).filter(Boolean),
  )
}

describe('meeting footer actions use the shared .fbtn language', () => {
  // The task tab's fixed action bar is the reference the meeting footers follow.
  const taskBar = ruleDecls(styleOf('web/src/components/task/TaskDetailPage.vue'), '.detail-actions')

  it.each(MEETING_SCREENS)('%s draws its footer bar like the task action bar', (file) => {
    const footer = ruleDecls(meetingStyleOf(file), '.meeting-footer')
    // Only the shape/colour declarations: alignment and overflow are per-screen
    // decisions (the detail footer pins 删除 to the left with a spacer).
    expect(footer).toMatchObject({
      display: taskBar.display,
      padding: taskBar.padding,
      background: taskBar.background,
      'border-top': taskBar['border-top'],
      'flex-shrink': taskBar['flex-shrink'],
    })
  })

  it.each(MEETING_SCREENS)('%s scrolls instead of wrapping a narrow panel', (file) => {
    // The left dock column can be dragged down to 320px. Three 30px pills plus
    // a 复制全部 label do not fit there, so the row scrolls horizontally. It
    // must not wrap (the footer would grow into the content) and it must not
    // clip: `safe flex-end` falls back to the start when the row overflows, so
    // the leftmost action (删除) stays reachable.
    const footer = ruleDecls(meetingStyleOf(file), '.meeting-footer')
    expect(footer['flex-wrap']).toBe('nowrap')
    expect(footer['overflow-x']).toBe('auto')
    expect(footer['justify-content']).toBe('safe flex-end')
  })

  it.each(MEETING_SCREENS)('%s puts only .fbtn pills in the footer', (file) => {
    const buttons = footerButtonClasses(file)
    expect(buttons.length).toBeGreaterThanOrEqual(2)
    for (const classes of buttons) {
      expect(classes).toContain('fbtn')
      // The local look-alike must not creep back in: one pill language only.
      expect(classes.some((c) => c.startsWith('meeting-btn'))).toBe(false)
    }
  })

  it.each(MEETING_SCREENS)('%s imports the shared footer-button stylesheet itself', (file) => {
    // A component that uses .fbtn without going through ModalDialog has to load
    // the sheet — an ancestor happening to import it is not a contract.
    expect(meetingSource(file)).toContain("import '@/assets/modal-footer-btn.css'")
  })

  it('loads the sheet in the tab that draws a pill outside the footer too', () => {
    // 资源's publish action is a `.fbtn` inside the tab body, not in the footer,
    // so the per-screen list above never covers it.
    expect(meetingSource('MeetingResources.vue')).toContain("import '@/assets/modal-footer-btn.css'")
  })

  it('keeps the primary/danger variants the actions rely on', () => {
    expect(footerButtonClasses('MeetingList.vue').some((c) => c.includes('fbtn-primary'))).toBe(true)
    expect(footerButtonClasses('MeetingCreate.vue').some((c) => c.includes('fbtn-primary'))).toBe(true)
    expect(footerButtonClasses('MeetingJoin.vue').some((c) => c.includes('fbtn-primary'))).toBe(true)
    // Delete is destructive: the shared red pill, not a hand-mixed tint.
    expect(footerButtonClasses('MeetingDetail.vue').some((c) => c.includes('fbtn-danger'))).toBe(true)
    // 结束会议 is the same kind of action as 删除: it ends the meeting for
    // everyone, so it is the red pill too.
    expect(footerButtonClasses('MeetingStage.vue').some((c) => c.includes('fbtn-danger'))).toBe(true)
  })
})
