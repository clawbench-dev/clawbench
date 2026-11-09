import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring contract for the group "stop this member" menu.
 *
 * Why a source guard instead of a mount test: `ChatPanelContent` pulls in the
 * whole chat tree, so its test file only covers pure helpers. A wiring bug here
 * is exactly the silent kind that shipped once already — `:stopTargets` was
 * passed to `ChatMessageList` (the FIRST component with the same
 * `:isGroupSession` / `:groupMembers` prop pair) instead of `ChatInputBar`, so
 * the input bar saw an empty list and the stop button quietly fell back to the
 * old two-click whole-turn cancel. Nothing threw; the menu simply never opened.
 */
const PANEL = readWebFile('src/components/chat/ChatPanelContent.vue')
const INPUT_BAR = readWebFile('src/components/chat/ChatInputBar.vue')

/** The opening tag of a child component, from `<Name` to its self-closing `/>`. */
function element(source: string, name: string): string {
  const start = source.indexOf(`<${name}`)
  expect(start, `${name} should be rendered by ChatPanelContent`).toBeGreaterThanOrEqual(0)
  const end = source.indexOf('\n    />', start)
  expect(end, `${name} should close on its own line`).toBeGreaterThan(start)
  return source.slice(start, end)
}

describe('group stop menu wiring', () => {
  it('passes the speaking members to the INPUT BAR (not the message list)', () => {
    const bar = element(PANEL, 'ChatInputBar')
    expect(bar, 'ChatInputBar must receive the speaking members').toContain(':stopTargets="stopTargets"')
    expect(bar, 'ChatInputBar must report the chosen member upward').toContain('@stop-member="handleStopMember"')

    const list = element(PANEL, 'ChatMessageList')
    expect(list, 'ChatMessageList has no stopTargets prop — passing it there silently drops it').not.toContain(':stopTargets')
  })

  it('declares the prop and the event it uses', () => {
    expect(INPUT_BAR, 'ChatInputBar must declare the stopTargets prop').toMatch(/stopTargets:\s*\{\s*type:\s*Array/)
    expect(INPUT_BAR, "ChatInputBar must emit 'stop-member'").toMatch(/'stop-member'/)
  })

  it('opens the menu before falling back to the two-click cancel', () => {
    const start = INPUT_BAR.indexOf('function handleStopClick()')
    expect(start, 'handleStopClick should exist').toBeGreaterThanOrEqual(0)
    const body = INPUT_BAR.slice(start, INPUT_BAR.indexOf('\n}\n', start))
    // The menu branch must come FIRST: otherwise a group chat with speakers
    // still primes the whole-turn cancel instead of offering the member list.
    const menuBranch = body.indexOf('hasStopTargets.value')
    const fallback = body.indexOf('stopMachine.click()')
    expect(menuBranch, 'handleStopClick must branch on hasStopTargets').toBeGreaterThanOrEqual(0)
    expect(fallback, 'the two-click fallback must still exist').toBeGreaterThan(menuBranch)
  })
})
