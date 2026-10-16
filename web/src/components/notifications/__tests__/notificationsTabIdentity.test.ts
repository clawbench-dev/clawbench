import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * The notifications tab is a LOG, and its icon must say so.
 *
 * The tab is a browsable, append-only log of what happened. A bell reads as an
 * alert channel ("something needs your attention") and implies the unread state
 * the log deliberately does not have, so the icon is an Inbox. The label is
 * "通知中心" / "Notification Center" — the product's name for the feature — and
 * must stay in sync between the dock tab and the panel header. These are easy
 * to regress to the generic bell/Notifications pair, so pin both.
 *
 * cwd differs between a bare `vitest` run (web/) and scripts/vitest-run.sh
 * (repo root), so probe both roots for the source files.
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

const panel = readWebFile('src/components/notifications/NotificationsPanelContent.vue')

describe('notifications tab reads as a log, not an alert channel', () => {
  it('names the tab and the panel "通知中心" / "Notification Center" in both locales', () => {
    // The dock tooltip and the panel header must carry the SAME name — a
    // divergence means the panel you land on is not the tab you clicked.
    expect(zh.nav.notifications).toBe('通知中心')
    expect(zh.notifications.title).toBe(zh.nav.notifications)
    expect(en.nav.notifications).toBe('Notification Center')
    expect(en.notifications.title).toBe(en.nav.notifications)
  })

  it('does not call it a history — the log has no "past events" framing', () => {
    // The old label ("历史通知" / "Notification History") implied a browsable
    // archive; the product name is now simply "Notification Center".
    expect(zh.nav.notifications).not.toContain('历史')
    expect(en.nav.notifications.toLowerCase()).not.toContain('history')
  })

  it('uses an Inbox icon for the dock tab', () => {
    // The lucide component carries no stable `name` (it is a functional
    // wrapper), so pin the import binding in the icon module instead — that is
    // the actual source of truth for which glyph a tab gets.
    const meta = readWebFile('src/composables/dockTabMeta.ts')
    const icons = meta.match(/import\s*\{([\s\S]*?)\}\s*from\s*'lucide-vue-next'/)
    expect(icons, 'dockTabMeta must import its icons').not.toBeNull()
    expect(icons![1]).toMatch(/\bInbox\b/)
    expect(icons![1], 'the bell import must be gone').not.toMatch(/\bBell\b/)
    expect(meta).toMatch(/notifications:\s*Inbox\b/)
    expect(meta, 'the notifications tab must not map to Bell').not.toMatch(/notifications:\s*Bell\b/)
  })

  it('uses the same Inbox glyph in the panel header and empty state', () => {
    // A bell in the header would re-introduce the alert reading the tab just
    // dropped; the empty state should be an empty inbox, not a muted bell.
    expect(panel).toMatch(/<Inbox class="crumb-icon"/)
    expect(panel).not.toMatch(/<Bell\b/)
    expect(panel).not.toMatch(/<BellOff\b/)
    // And the imports must not carry the unused bell glyphs either.
    expect(panel).not.toMatch(/\bBellOff\b/)
    expect(panel).not.toMatch(/import\s*\{[^}]*\bBell\b[^}]*\}\s*from\s*'lucide-vue-next'/)
  })
})
