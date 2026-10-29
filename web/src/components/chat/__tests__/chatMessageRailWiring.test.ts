import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring contract for the rail's in-flight ("replying") node.
 *
 * Why a source guard: the rail's click path is three hops across three files
 * (rail → `select-pending` → panel handler → message list's exposed method), and
 * `ChatPanelContent.test.ts` never mounts the panel (it pulls in the whole chat
 * tree). A break in the middle throws nothing and renders nothing wrong — the
 * node simply does nothing when clicked, which is exactly how it shipped once:
 * the rail emitted `select-pending`, but the panel never listened, so the event
 * fell on the floor and `scrollToGenerating` had no consumer at all.
 */
const PANEL = readWebFile('src/components/chat/ChatPanelContent.vue')
const LIST = readWebFile('src/components/chat/ChatMessageList.vue')

/** Source of a function, from its declaration to the next top-level `\n}`. */
function fnBody(src: string, name: string): string {
  const start = src.indexOf(name)
  expect(start, `${name} should exist`).toBeGreaterThanOrEqual(0)
  const end = src.indexOf('\n}\n', start)
  expect(end, `${name} should have a top-level closing brace`).toBeGreaterThan(start)
  return src.slice(start, end)
}

describe('chat message rail — in-flight node wiring', () => {
  it('binds select-pending on the rail, not just select', () => {
    // The pending slot has no index row, so `select` cannot carry it.
    expect(PANEL).toMatch(/<ChatMessageRail[\s\S]*?@select-pending="handleRailSelectPending"/)
  })

  it('forwards the pending event to the message list', () => {
    const fn = fnBody(PANEL, 'function handleRailSelectPending')
    expect(fn, 'the handler must actually call through to the list').toMatch(
      /messageListRef\.value\?\.scrollToGenerating\(\)/,
    )
  })

  it('exposes scrollToGenerating and routes it at the streaming bubble', () => {
    // Scrolling to the BOTTOM is not the same thing: while a reply grows, the
    // bottom keeps moving, so a smooth bottom-scroll gets overtaken by the next
    // chunk and the reader never lands on the reply.
    expect(LIST).toMatch(/scrollToGenerating: \(\) => scrollToStreamingMessage\(\)/)

    const fn = fnBody(LIST, 'function scrollToStreamingMessage')
    expect(fn).toMatch(/data-streaming="true"/)
    // No streaming bubble mounted (the turn ended between click and call) must
    // still do something, or the control looks broken.
    expect(fn, 'must fall back instead of doing nothing').toMatch(/scrollToBottomSmooth\(\)/)
  })
})
