import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the chat action bar must expose a group-settings button in group
 * sessions, and it must open the SAME sheet as clicking the header avatar
 * stack — not a second GroupMemberSheet instance.
 *
 * The header stack is the primary entry, but the action bar is where every
 * other session action lives and it is the reachable door on narrow panes. A
 * second sheet instance would mean a second roster fetch and two sheets that
 * can drift out of sync, so the button drives the header stack's `open()`.
 *
 * This asserts the cross-component wiring (bar emits → panel re-emits → App
 * calls the stack ref), which no single mount test covers.
 */
describe('group settings button in the input action bar', () => {
  const BAR = 'src/components/chat/ChatInputBar.vue'
  const PANEL = 'src/components/chat/ChatPanelContent.vue'
  const APP = 'src/App.vue'
  const STACK = 'src/components/chat/GroupAvatarStack.vue'

  it('renders a group-only button in the action bar', () => {
    const src = readWebFile(BAR)
    const open = src.indexOf('<div class="chat-top-actions"')
    expect(open, '.chat-top-actions must exist').toBeGreaterThan(-1)
    const close = src.indexOf('recommendation-chip', open)
    const block = src.slice(open, close)

    // Group-gated and wired to a dedicated emit.
    expect(block).toMatch(/v-if="isGroupSession"[\s\S]*?data-action="group-settings"/)
    expect(block).toContain("$emit('open-group-settings')")
    expect(block).toContain("t('group.settings')")
    expect(block).toContain('<Users')
  })

  it('declares the emit in defineEmits', () => {
    const src = readWebFile(BAR)
    expect(src).toMatch(/defineEmits\(\[[\s\S]*?'open-group-settings'/)
  })

  it('is re-emitted across the bar → panel boundary', () => {
    const panel = readWebFile(PANEL)
    expect(panel).toContain('@open-group-settings="$emit(\'open-group-settings\')"')
    expect(panel).toMatch(/defineEmits\(\[[\s\S]*?'open-group-settings'/)
  })

  it('drives the header avatar stack ref rather than mounting a second sheet', () => {
    const app = readWebFile(APP)

    // The header stack carries a ref so the action bar can reach it.
    expect(app).toMatch(/<GroupAvatarStack[\s\S]*?ref="groupAvatarStackRef"/)
    // App listens for the panel's emit and forwards to the ref's open().
    expect(app).toContain('@open-group-settings="openGroupSettings"')
    expect(app).toMatch(/function openGroupSettings\(\)\s*\{[\s\S]*?groupAvatarStackRef\.value\?\.open\(\)/)
    // Exactly ONE GroupMemberSheet is rendered in the whole app (inside the
    // stack) — a second one would fetch the roster twice and desync.
    expect(app).not.toContain('<GroupMemberSheet')
  })

  it('exposes open() on the header avatar stack', () => {
    const stack = readWebFile(STACK)
    expect(stack).toMatch(/defineExpose\(\{\s*open:\s*openSheet\s*\}\)/)
  })

  it('has the settings keys in both locales', () => {
    for (const locale of ['zh', 'en']) {
      const src = readWebFile(`src/i18n/locales/${locale}.ts`)
      expect(src, `group.settings missing in ${locale}`).toMatch(/^\s*settings:/m)
      const start = src.indexOf('wideLabels: {')
      expect(start, `wideLabels must exist in ${locale}`).toBeGreaterThan(-1)
      const block = src.slice(start, src.indexOf('},', start))
      expect(block, `wideLabels.groupSettings missing in ${locale}`).toMatch(/^\s*groupSettings:/m)
    }
  })
})
