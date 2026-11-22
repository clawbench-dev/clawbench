import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring contract for "retry" (the reset-session button on the AI error card).
 *
 * Why a source guard instead of a mount test: `ChatPanelContent.test.ts` covers
 * pure helpers only — it never mounts the panel, which pulls in the whole chat
 * tree. The behaviour itself (what gets replayed) is tested for real in
 * `fileAttachmentUtils.test.ts` via `buildRetryPayload`; what is left to pin
 * here is that the panel actually uses it.
 *
 * The bug this locks down: the retry replayed only `lastUserMsg.content`, so an
 * uploaded image was dropped and the model got bare text. A pure-image message
 * was skipped entirely by the truthiness guard, so the button did nothing.
 */
const PANEL = readWebFile('src/components/chat/ChatPanelContent.vue')

/** Source of a function, from its declaration to the next top-level `\n}`. */
function fnBody(name: string): string {
  const start = PANEL.indexOf(name)
  expect(start, `${name} should exist in ChatPanelContent.vue`).toBeGreaterThanOrEqual(0)
  const end = PANEL.indexOf('\n}\n', start)
  expect(end, `${name} should have a top-level closing brace`).toBeGreaterThan(start)
  return PANEL.slice(start, end)
}

describe('chat panel retry wiring', () => {
  const resetFn = fnBody('async function handleResetSession')

  it('replays the attachments of the retried message, not just its text', () => {
    expect(resetFn, 'the retry must build its payload from the message files').toMatch(
      /buildRetryPayload\(lastUserMsg\.content, lastUserMsg\.files\)/,
    )
    // Passing only the text is exactly the bug: the send loses every file.
    expect(resetFn, 'the retry must NOT send the bare text alone').not.toMatch(
      /sendMessage\(lastUserMsg\.content\)/,
    )
  })

  it('does not gate the resend on the text being non-empty', () => {
    // An image sent on its own has no text; the old guard skipped the whole
    // resend, so clicking retry appeared to do nothing.
    expect(resetFn, 'an empty text with attachments must still resend').not.toMatch(
      /if \(lastUserMsg\?\.content\)/,
    )
    expect(resetFn, 'buildRetryPayload already reports "nothing to send"').toMatch(/if \(!retry\) return/)
  })

  it('sends through the same direct-send entry as a first send', () => {
    // Going through sendMessage (the composer path) would re-read the input box
    // and could clear a draft the user is typing on an automatic recovery.
    expect(resetFn).toMatch(/await sendMessageNow\(retry\.text, retry\.filePaths, retry\.allFiles\)/)
  })
})
