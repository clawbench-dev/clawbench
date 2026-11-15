import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * One glyph for 视频会议, everywhere.
 *
 * `utils/meetingIcon.ts` exists because each surface used to hard-code its own
 * icon and they drifted: the dock tab, the meeting headers and the quote chip
 * used `MonitorUp` (a monitor with an outgoing arrow) while the share centre
 * and the notification centre painted a `Video` camera. A camera reads as "a
 * recording", not as the meeting feature, so the same feature looked like two.
 *
 * This is a source guard on purpose: the defect is "a component picked a
 * different icon", which a rendered-DOM test cannot see once both icons are
 * stubbed to the same shape.
 */

const ROOT = resolve(__dirname, '../../../../..')

/**
 * Every surface that wears the meeting brand, dock tab included.
 *
 * The settings index is deliberately absent: its 会议 row is DERIVED from the
 * live plugin's `settingsPage.icon` (the same `MEETING_ICON` constant), so it
 * can no longer pick a second glyph of its own. SettingsIndex.test.ts asserts
 * the rendered row still draws that constant.
 */
const SURFACES = [
  'web/src/plugins/live/plugin.ts',
  'web/src/utils/quoteSourceMeta.ts',
  'web/src/components/live/MeetingList.vue',
  'web/src/components/live/MeetingCreate.vue',
  'web/src/components/live/MeetingJoin.vue',
  'web/src/components/live/MeetingStage.vue',
  'web/src/components/live/MeetingDetail.vue',
  'web/src/components/live/TranscriptList.vue',
  'web/src/components/share/ShareCenterPanel.vue',
  'web/src/components/notifications/NotificationsPanelContent.vue',
]

function read(path: string): string {
  return readFileSync(resolve(ROOT, path), 'utf8')
}

describe('meeting icon consistency', () => {
  it.each(SURFACES)('%s draws the shared MEETING_ICON', (path) => {
    expect(read(path)).toContain('MEETING_ICON')
  })

  it.each(SURFACES)('%s does not hard-code the glyph again', (path) => {
    const src = read(path)
    expect(src).not.toMatch(/\bMonitorUp\b/)
    // `Video` is legitimate for a camera device picker, never for the meeting
    // brand. None of the surfaces above is a device picker.
    expect(src).not.toMatch(/\bVideo\b/)
  })

  it('keeps the constant itself a single definition', () => {
    const src = read('web/src/utils/meetingIcon.ts')
    expect(src.match(/export const MEETING_ICON/g)).toHaveLength(1)
  })
})
