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
    // ...and each must clear the sticky spinner on the way out, or it hangs on
    // screen for a switch the user already navigated away from (duration: 0 =
    // manual dismiss only).
    const dismissals = body.match(/return dismissSwitchingToast\(\)/g) ?? []
    expect(dismissals.length, 'every stale-session return must dismiss the sticky toast').toBe(guards.length)
  })

  it('gives the request a timeout that outlasts the server budget', () => {
    // The invariant is the NESTING, not the literal number: the client must not
    // be the one that gives up, or a slow-but-successful switch is reported as a
    // failure. Server: ai 30s < handler 35s. So assert the relation, and pin the
    // number only as a floor.
    const client = Number(PANEL.match(/MODEL_SWITCH_TIMEOUT_MS\s*=\s*([\d_]+)/)?.[1]?.replace(/_/g, ''))
    expect(Number.isFinite(client), 'MODEL_SWITCH_TIMEOUT_MS must be defined').toBe(true)
    expect(client, 'client timeout must exceed the 35s handler budget').toBeGreaterThan(35_000)
  })

  it('mirrors the pick locally when the user selects a level', () => {
    // Issue #552: the server remembers the pick per model, but the cached agent
    // list is what a NEW chat reads. Without this local mirror the new chat
    // shows the agent's default and then sends it as an explicit override,
    // defeating the remembered pick.
    const body = fnBody('function handleSwitchThinkingEffort')
    expect(body, 'the pick must be mirrored into the cached agent list').toMatch(
      /setThinkingEffortForModelLocal\(/,
    )
    expect(body, 'it must be keyed to the current agent AND model').toMatch(
      /identity\.currentAgentId\.value,\s*identity\.currentModelId\.value/,
    )
  })

  it('applies the resolved effort after a successful switch', () => {
    // Issue #552: the effort is model-scoped, so a switch can silently move it
    // (OpenCode reports "low" for most models). The server resolves the new
    // level; the panel must adopt it AND tell the user, or the change is only
    // discovered mid-task. Deleting this call leaves every other test green.
    const body = fnBody('async function handleSwitchModel')
    expect(body, 'the resolved effort must be applied after the switch').toMatch(
      /applyEffortAfterModelSwitch\(res, effortBefore\)/,
    )
    // ...and the previous level must be captured BEFORE the request, or the
    // comparison inside it is always "unchanged".
    const captureIdx = body.indexOf('const effortBefore = identity.currentThinkingEffort.value')
    const awaitIdx = body.indexOf('await apiPatch(')
    expect(captureIdx, 'the pre-switch level must be captured').toBeGreaterThanOrEqual(0)
    expect(captureIdx, 'it must be captured before the request resolves').toBeLessThan(awaitIdx)
  })

  it('clears the chip when the new model has no resolved level', () => {
    // The server omits `thinkingEffort` when the model has no preference and the
    // agent decides. Keeping the previous model's level would show a level the
    // agent is not running — the same lie, mirrored.
    const body = fnBody('function applyEffortAfterModelSwitch')
    expect(body, 'an absent level must clear the chip').toMatch(
      /currentId\.value = ''/,
    )
  })

  // `persistSessionUpdate` is chained with .then() by its callers, so it must
  // ALWAYS return a promise. Returning undefined when there is no session threw
  // a TypeError instead of being a no-op — the pick's optimistic state was left
  // un-reconciled and the error surfaced in the console only.
  it('persistSessionUpdate is total: no session still yields a promise', () => {
    const body = fnBody('function persistSessionUpdate')
    expect(body, 'the no-session path must return a resolved promise').toMatch(
      /if \(!sid\) return Promise\.resolve\(null\)/,
    )
    expect(body, 'the no-session path must not return bare').not.toMatch(/if \(!sid\) return\s*\n/)
  })

  // A durable-write failure must be visible: the level applies now but dies on
  // restart. Without the flag the user only notices later, with no explanation.
  it('surfaces a pick that could not be remembered', () => {
    const body = fnBody('function handleSwitchThinkingEffort')
    expect(body, 'the not-remembered flag must be handled').toMatch(
      /thinkingEffortNotRemembered/,
    )
    expect(body, 'and explained to the user').toMatch(
      /thinkingEffortSwitcher\.notRemembered/,
    )
  })

  it('reverts to the agent default when the session has no persisted model', () => {
    // A session created via POST /api/ai/sessions deliberately has model='', so
    // the chip's pre-click value came from the agent. Without this fallback the
    // guard `if (data.modelId)` skips and the rejected model stays on the chip.
    const body = fnBody('function restoreModelSelection')
    expect(body, 'must fall back to the agent default for an empty model').toMatch(
      /syncModelFromAgent\(/,
    )
  })
})
