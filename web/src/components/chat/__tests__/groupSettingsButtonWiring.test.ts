import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the chat input must expose a group-settings entry in group sessions,
 * and it must open the SAME sheet as clicking the header avatar stack — not a
 * second GroupSettingsSheet instance.
 *
 * The entry used to live in the action bar (`.chat-top-actions`). It moved to
 * the session-info bar under the input because it is a group-wide SETTING, not
 * a session action — that bar already carries the group's other two switches
 * (cruise / auto-approve), so the settings door belongs next to them. The test
 * therefore pins the new location AND the absence from the old one: a leftover
 * action-bar button would give the group two doors that can drift.
 *
 * The header stack is the primary entry; the info bar is the reachable door on
 * narrow panes. A second sheet instance would mean a second roster fetch and
 * two sheets that can drift out of sync, so the chip drives the header stack's
 * `open()`.
 *
 * This asserts the cross-component wiring (bar emits → panel re-emits → App
 * calls the stack ref), which no single mount test covers.
 */
describe('group settings entry in the session info bar', () => {
  const BAR = 'src/components/chat/ChatInputBar.vue'
  const PANEL = 'src/components/chat/ChatPanelContent.vue'
  const APP = 'src/App.vue'
  const STACK = 'src/components/chat/GroupAvatarStack.vue'

  it('renders the entry inside the session-info bar, not the action bar', () => {
    const src = readWebFile(BAR)

    const infoOpen = src.indexOf('<div class="chat-session-info">')
    expect(infoOpen, '.chat-session-info must exist').toBeGreaterThan(-1)
    const infoClose = src.indexOf('</div>', src.indexOf('session-info-settings', infoOpen))
    const infoBlock = src.slice(infoOpen, infoClose)

    // Group-gated and wired to the dedicated emit, with the full label.
    expect(infoBlock).toMatch(/v-else>[\s\S]*?data-action="group-settings"/)
    expect(infoBlock).toContain("$emit('open-group-settings')")
    expect(infoBlock).toContain("t('group.settings')")
    expect(infoBlock).toContain('<Users')

    // And the old action-bar button is gone.
    const actionsOpen = src.indexOf('<div class="chat-top-actions"')
    expect(actionsOpen, '.chat-top-actions must exist').toBeGreaterThan(-1)
    const actionsBlock = src.slice(actionsOpen, src.indexOf('</div>', src.indexOf('auto-speech-btn', actionsOpen)))
    expect(actionsBlock).not.toContain('data-action="group-settings"')
    expect(actionsBlock).not.toContain("$emit('open-group-settings')")
  })

  it('uses the full "group settings" label rather than the abbreviated action-bar one', () => {
    // The action bar had to abbreviate to "Group" to fit a row of icons; the
    // info bar has room for the real name, which is what the user asked for.
    const src = readWebFile(BAR)
    const chip = src.slice(src.indexOf('data-action="group-settings"') - 400, src.indexOf('data-action="group-settings"') + 400)
    expect(chip).toContain("t('group.settings')")
    expect(chip).not.toContain('wideLabels.groupSettings')
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
    // Exactly ONE GroupSettingsSheet is rendered in the whole app (inside the
    // stack) — a second one would fetch the roster twice and desync.
    expect(app).not.toContain('<GroupSettingsSheet')
  })

  it('exposes open() on the header avatar stack', () => {
    const stack = readWebFile(STACK)
    expect(stack).toMatch(/defineExpose\(\{\s*open:\s*openSheet\s*\}\)/)
  })

  it('has the full group.settings label in both locales', () => {
    // The chip shows the FULL name ("群聊设置" / "Group settings"), not the
    // abbreviated wideLabels variant the action bar used.
    for (const locale of ['zh', 'en']) {
      const src = readWebFile(`src/i18n/locales/${locale}.ts`)
      const start = src.indexOf('\n  group: {')
      expect(start, `group namespace must exist in ${locale}`).toBeGreaterThan(-1)
      const block = src.slice(start, src.indexOf('\n  },', start))
      expect(block, `group.settings missing in ${locale}`).toMatch(/^\s{4}settings:/m)
    }
  })
})

/**
 * Guard: the group auto-cruise chip's value and toggle must be bound on the
 * ChatInputBar (where the chip lives), NOT on ChatMessageList. Both components
 * are sibling tags in ChatPanelContent and both take `:isGroupSession` /
 * `:groupMembers`, so a `:groupCruise` binding inserted next to the wrong one
 * silently leaves ChatInputBar with its default (false): the chip never
 * highlights, and clicking always computes next=!false=true, so it can never be
 * turned OFF. This is a cross-tag binding placement bug that no mount test of
 * ChatInputBar alone can catch (the component is correct in isolation).
 */
describe('group cruise chip wiring is bound on ChatInputBar, not ChatMessageList', () => {
  const PANEL = 'src/components/chat/ChatPanelContent.vue'

  it('binds :groupCruise and @toggle-group-cruise inside the ChatInputBar block', () => {
    const src = readWebFile(PANEL)
    // Slice the ChatInputBar element: from its opening tag to the self-close.
    const open = src.indexOf('<ChatInputBar')
    expect(open, '<ChatInputBar must exist').toBeGreaterThan(-1)
    const close = src.indexOf('/>', open)
    const block = src.slice(open, close)

    expect(block, ':groupCruise must be bound on ChatInputBar').toContain(':groupCruise="props.groupCruise"')
    expect(block, '@toggle-group-cruise must be wired on ChatInputBar').toContain("@toggle-group-cruise=\"$emit('toggle-group-cruise', $event)\"")

    // And it must NOT appear on the ChatMessageList tag.
    const listOpen = src.indexOf('<ChatMessageList')
    expect(listOpen, '<ChatMessageList must exist').toBeGreaterThan(-1)
    const listBlock = src.slice(listOpen, src.indexOf('/>', listOpen))
    expect(listBlock, ':groupCruise must not be bound on ChatMessageList').not.toContain('groupCruise')
  })
})
