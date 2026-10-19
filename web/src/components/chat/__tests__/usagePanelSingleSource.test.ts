import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the context panel body has exactly ONE implementation.
 *
 * It used to be inline in ChatInputBar and was extracted to ContextUsageDetail
 * so the group member panel can render the same thing. The failure mode this
 * pins is the obvious "just copy the rows over" move: the two copies would then
 * drift (one gains a row or changes a threshold, the other does not), and the
 * single-chat popup would silently disagree with the member panel about the
 * same numbers.
 */
describe('context usage panel has a single implementation', () => {
  const BAR = 'src/components/chat/ChatInputBar.vue'
  const DETAIL = 'src/components/chat/ContextUsageDetail.vue'
  const MEMBER = 'src/components/chat/GroupSettingsSheet.vue'
  const UTIL = 'src/utils/usageDisplay.ts'

  it('ChatInputBar renders the shared component instead of its own rows', () => {
    const src = readWebFile(BAR)
    expect(src).toContain('<ContextUsageDetail')
    // Row markup must not come back: these class names now live in the shared
    // component only.
    expect(src).not.toContain('usage-popup-row')
    expect(src).not.toContain('usage-popup-bar-fill')
    expect(src).not.toContain('usage-popup-compact-btn')
  })

  it('the member path renders the same shared component', () => {
    // A member is a session, so clicking one opens the SAME SessionDrawer a
    // single chat opens (addressed at the member row) — its usage tab is the
    // one shared implementation. MemberContextSheet was removed for exactly
    // that reason: a second panel is what would drift.
    expect(readWebFile('src/components/chat/SessionDrawer.vue')).toContain('<ContextUsageDetail')
    expect(readWebFile(MEMBER)).toContain('<SessionDrawer')
  })

  it('both surfaces get their numbers from the shared helpers', () => {
    // Thresholds/formatting live in usageDisplay; a second copy of the colour
    // ramp would be the other way these two could disagree.
    expect(readWebFile(BAR)).toContain('@/utils/usageDisplay')
    // The group roster's chip gets its numbers from the same helpers via
    // GroupSettingsSheet (the row owns the chip).
    expect(readWebFile('src/components/chat/GroupSettingsSheet.vue')).toContain('@/utils/usageDisplay')
    for (const src of [readWebFile(BAR), readWebFile(MEMBER), readWebFile(DETAIL)]) {
      expect(src).not.toContain('#ef4444')
      expect(src).not.toContain('#eab308')
    }
    expect(readWebFile(UTIL)).toContain('#ef4444')
  })

  it('the single-chat popup still asks for the compact action', () => {
    // Compaction is a single-chat action; the shared component must be TOLD to
    // show it, otherwise the button disappears from the only place it belongs.
    expect(readWebFile(BAR)).toMatch(/<ContextUsageDetail[^>]*show-compact/)
  })
})
