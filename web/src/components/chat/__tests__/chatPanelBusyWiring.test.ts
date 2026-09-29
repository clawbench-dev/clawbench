import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring contract for the long-action busy feedback (fork / ACP sync).
 *
 * Why a source guard instead of a mount test: `ChatPanelContent.test.ts` covers
 * pure helpers only (it never mounts the component — the panel pulls in the
 * whole chat tree). A wiring bug here is exactly the silent kind: the indicator
 * simply never appears, nothing throws, and no behavioural test notices.
 *
 * The two things that must stay true:
 *   1. `startBusy` is CLAIM-based (returns false when the indicator is taken).
 *      It used to call `stopBusy()` first, which let a second action steal the
 *      indicator — and then the first action's `finally` cleared the second's.
 *      The sticky toast is a singleton, so the two tickers also fought over it.
 *   2. Each caller only releases what it claimed (`stopBusy('fork')` /
 *      guarded by `claimed`), so a late `finally` cannot clear someone else.
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

describe('chat panel busy wiring', () => {
  it('startBusy claims instead of preempting', () => {
    const startFn = fnBody('function startBusy')
    expect(startFn, 'startBusy must refuse to preempt an in-flight action').toMatch(
      /if \(busy\.value !== null\) return false/,
    )
    expect(startFn, 'startBusy must report whether it won the claim').toMatch(/return true/)
    // The old (buggy) shape released first, so a concurrent action won the
    // indicator and the first one's finally then wiped the second's state.
    expect(startFn, 'startBusy must NOT release an action it does not own').not.toMatch(/stopBusy\(/)
  })

  it('each caller releases only the kind it claimed', () => {
    const forkFn = fnBody('async function runFork')
    expect(forkFn, 'the fork path must bail out if it lost the claim').toMatch(/if \(!startBusy\('fork'\)\) return/)
    expect(forkFn, 'the fork path must release its own kind').toMatch(/stopBusy\('fork'\)/)

    const syncFn = fnBody('async function handleSyncAcpSession')
    expect(syncFn, 'the sync path must honour the claim result').toMatch(/const claimed = startBusy\('sync'\)/)
    expect(syncFn, 'the sync path must release its own kind').toMatch(/stopBusy\('sync'\)/)
    expect(syncFn, 'the sync path must not release a claim it never won').toMatch(/if \(claimed\) stopBusy\('sync'\)/)

    // stopBusy(kind) itself must refuse to clear a different action's state.
    const stopFn = fnBody('function stopBusy')
    expect(stopFn, 'stopBusy must ignore a kind it does not own').toMatch(/if \(kind && busy\.value !== kind\) return/)
  })

  it('renders the BusyBar and passes the busy kind to the input bar', () => {
    // The bar is the only indicator visible regardless of scroll/viewport; the
    // busyKind prop is what blocks a concurrent sync.
    expect(PANEL).toMatch(/<BusyBar[^>]*:visible="busy !== null"/)
    expect(PANEL).toMatch(/:busyKind="busy"/)
  })

  it('clears the ticker on unmount so it cannot outlive the panel', () => {
    // A fork/sync request keeps running after the panel unmounts; without this
    // the interval would keep writing to a toast no panel owns.
    const unmountFn = PANEL.slice(PANEL.indexOf('onUnmounted(() =>'), PANEL.indexOf('</script>'))
    expect(unmountFn).toMatch(/stopBusy\(\)/)
  })

  it('feeds the forked message id down to the message list', () => {
    // Drives the in-place spinner on the clicked message's fork button.
    expect(PANEL).toMatch(/:forkingMessageId="forkingMessageId"/)
  })
})
