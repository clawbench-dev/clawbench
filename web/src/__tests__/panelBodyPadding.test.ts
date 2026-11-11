import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * One inset for every card panel's scroll body.
 *
 * The meeting tab was the visible symptom: the record list inset its cards by
 * 8/10/12 and a record opened from that list by 12/16, so the SAME tab changed
 * density when you opened a row. The other card panels each invented their own
 * value too (tasks 8, share 8/12, notifications 0/12/24).
 *
 * jsdom applies no stylesheets, so nothing here is observable in a component
 * test — the source is the only place this can be pinned. Every entry names the
 * rule that must spend the token; a hardcoded `--space-N` there fails.
 */

const ROOT = join(__dirname, '..', '..')

function source(rel: string): string {
  return readFileSync(join(ROOT, rel), 'utf8')
}

/** The declaration block for `selector`, comments stripped. */
function rule(rel: string, selector: string): string {
  const src = source(rel).replace(/\/\*[\s\S]*?\*\//g, '')
  const at = src.indexOf(`${selector} {`)
  if (at === -1) throw new Error(`${rel}: no rule for "${selector}"`)
  return src.slice(at, src.indexOf('}', at))
}

const PANELS: Array<[file: string, selector: string]> = [
  ['src/components/live/MeetingList.vue', '.meeting-body'],
  ['src/components/live/MeetingDetail.vue', '.meeting-body'],
  ['src/components/task/TaskListPage.vue', '.task-list-body'],
  ['src/components/share/ShareCenterPanel.vue', '.share-body'],
  ['src/components/notifications/NotificationsPanelContent.vue', '.notif-body'],
  ['src/components/stats/ClocStatsPanel.vue', '.cloc-body'],
  ['src/components/stats/GitCodeStatsPanel.vue', '.git-body'],
  ['src/components/stats/UsageStatsPanel.vue', '.stats-body'],
]

describe('panel body padding is one token', () => {
  it('declares the token once, in the shared variables', () => {
    const css = readFileSync(join(ROOT, 'css', 'variables.css'), 'utf8')
    expect(css).toMatch(/--panel-body-pad:\s*var\(--space-5\);/)
  })

  it.each(PANELS)('%s %s spends it', (file, selector) => {
    const decl = rule(file, selector)
    // The horizontal inset is the one that must be shared; a rule may still set
    // its own bottom padding (the notifications body ends on a taller step).
    const padding = decl.match(/padding:\s*([^;]+);/)?.[1] ?? ''
    expect(padding, `${file} ${selector} must spend --panel-body-pad`).toContain('var(--panel-body-pad')
  })
})
