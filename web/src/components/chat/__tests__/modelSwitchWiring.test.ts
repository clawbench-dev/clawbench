import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring contract for the click-time model switch.
 *
 * Why a source guard: ChatPanelContent.test.ts covers pure helpers only (it
 * never mounts the component — the panel pulls in the whole chat tree). The
 * bugs this guards against are silent: no throw, no behavioural test notices.
 *
 *   1. The switch must be AWAITED against the server. The old code fired a
 *      fire-and-forget `persistSessionUpdate(...)` whose `.catch(() => {})`
 *      swallowed every failure, so a rejected switch looked like a success.
 *   2. A toast must appear IMMEDIATELY on click. The agent-side switch can take
 *      ~10s cold; without it the click reads as a no-op.
 *   3. A rejected switch must restore the chip, or it lies about the model.
 *   4. A busy agent is NOT a failure: the server keeps the choice and defers it,
 *      so the client must not revert or error-toast that case.
 */
const PANEL = readWebFile('src/components/chat/ChatPanelContent.vue')

function fnBody(name: string): string {
  const start = PANEL.indexOf(name)
  expect(start, `${name} should exist in ChatPanelContent.vue`).toBeGreaterThanOrEqual(0)
  const end = PANEL.indexOf('\n}\n', start)
  expect(end, `${name} should have a top-level closing brace`).toBeGreaterThan(start)
  return PANEL.slice(start, end)
}

describe('click-time model switch wiring', () => {
  it('awaits the server instead of firing and forgetting', () => {
    const body = fnBody('async function handleSwitchModel')
    expect(body, 'handleSwitchModel must await the PATCH').toMatch(/await apiPatch\(/)
    expect(body, 'the failure path must be handled').toMatch(/catch\s*\(err\)/)
    // The old swallow-everything call must not come back.
    expect(body, 'must not fall back to the error-swallowing persist helper').not.toMatch(
      /persistSessionUpdate\(\{\s*modelId/,
    )
  })

  it('toasts immediately on click, before the request resolves', () => {
    const body = fnBody('async function handleSwitchModel')
    const toastIdx = body.indexOf("t('chat.modelSwitcher.switching')")
    const awaitIdx = body.indexOf('await apiPatch(')
    expect(toastIdx, 'a loading toast must be shown').toBeGreaterThanOrEqual(0)
    expect(awaitIdx, 'the request must be awaited').toBeGreaterThanOrEqual(0)
    expect(toastIdx, 'the toast must precede the await (immediate feedback)').toBeLessThan(awaitIdx)
  })

  it('restores the chip when the switch is rejected outright', () => {
    const body = fnBody('async function handleSwitchModel')
    expect(body, 'a hard rejection must restore the previous model').toMatch(/restoreModelSelection\(/)
  })

  it('treats a deferred (busy) switch as success, not failure', () => {
    const body = fnBody('async function handleSwitchModel')
    // Deferred is checked in the try-block (success path) and says so.
    const tryEnd = body.indexOf('} catch')
    const tryBlock = body.slice(0, tryEnd)
    expect(tryBlock, 'the deferred flag must be read in the success path').toMatch(/modelSwitchDeferred/)
    expect(tryBlock, 'deferred must be reported to the user').toMatch(/chat\.modelSwitcher\.deferred/)
  })

  it('ignores a result that arrives after the user switched sessions', () => {
    const body = fnBody('async function handleSwitchModel')
    // Both the success and failure paths must check the session is still current.
    const guards = body.match(/identity\.currentSessionId\.value !== sid/g) ?? []
    expect(guards.length, 'both paths must guard against a stale session').toBeGreaterThanOrEqual(2)
  })

  it('gives the request a timeout that outlasts the server budget', () => {
    expect(PANEL, 'the client timeout must exceed the handler timeout').toMatch(
      /MODEL_SWITCH_TIMEOUT_MS\s*=\s*45_000/,
    )
  })
})
