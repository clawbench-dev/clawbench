import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: "is the WS alive?" must stay answered in ONE place.
 *
 * History this pins down. Deciding whether the event socket was usable used to
 * be spread over four spots, each asking a slightly different question:
 *
 *   1. the close handler       — "did it close?"
 *   2. a periodic tick (60s)   — "OPEN but no message?"
 *   3. a periodic tick (90s)   — "OPEN but no ping?"      (never fired)
 *   4. the foreground resume   — its own copy of all of the above
 *
 * Each was written to plug a hole the others left, and every new hole got a new
 * detector instead of a better shared question. That is exactly how the
 * reported "back from background, never reconnects, only a force-quit fixes
 * it" bug appeared. On 2026-10-15 the resume path was taught to leave a
 * CONNECTING socket alone — correct, since tearing down a healthy in-flight
 * handshake is churn — but no existing detector covered a handshake stuck in
 * CONNECTING forever. The one state that needed killing became the one state
 * nothing looked at, and the client sat on "连接断开，正在重连…" indefinitely.
 *
 * The fix was to collapse the four into `socketFailure()`. This guard exists so
 * the collapse is not silently undone: a fifth detector added later would look
 * perfectly reasonable in review while restoring the original shape of the bug.
 *
 * The invariant is deliberately narrow — `readyState` may only be read by the
 * classifier and by `send()` (which needs OPEN to decide whether a write can go
 * out at all). Anything else inspecting socket state is a second decision point
 * by definition.
 */
describe('WS liveness is decided in exactly one place', () => {
  const SOURCE = 'src/composables/useGlobalEvents.ts'

  /** Strip comments so prose describing the states cannot satisfy the guard. */
  function stripComments(src: string): string {
    return src
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .replace(/(^|[^:])\/\/.*$/gm, '$1')
  }

  /** Names of the functions whose bodies read `readyState`. */
  function functionsReadingReadyState(src: string): string[] {
    const code = stripComments(src)
    const owners: string[] = []
    const re = /function\s+(\w+)\s*\(/g
    const starts: Array<{ name: string; index: number }> = []
    let m: RegExpExecArray | null
    while ((m = re.exec(code)) !== null) starts.push({ name: m[1], index: m.index })

    for (let i = 0; i < starts.length; i++) {
      const from = starts[i].index
      const to = i + 1 < starts.length ? starts[i + 1].index : code.length
      if (code.slice(from, to).includes('readyState')) owners.push(starts[i].name)
    }
    return owners
  }

  it('parses the source it is supposed to guard (guard is actually armed)', () => {
    // Without this, a rename or a moved file makes every assertion below pass
    // on an empty list — the classic "filtered to zero matches" false green.
    const owners = functionsReadingReadyState(readWebFile(SOURCE))
    expect(owners).toContain('socketFailure')
  })

  it('only the classifier and send() read socket state', () => {
    expect(functionsReadingReadyState(readWebFile(SOURCE)).sort()).toEqual(['send', 'socketFailure'])
  })

  it('the classifier is the single definition of "usable"', () => {
    const code = stripComments(readWebFile(SOURCE))
    // One *implementation*. Overload signatures (`function socketFailure(...):
    // ...` with no body) are type declarations, not a second policy — only a
    // line that opens a body counts.
    expect(code.match(/function\s+socketFailure\s*\([^)]*\)[^;{]*\{/g)).toHaveLength(1)
    // The two failure modes that need a clock, each with its own constant.
    expect(code).toContain("'handshake-timeout'")
    expect(code).toContain("'silent'")
  })

  it('has no ping-only liveness threshold', () => {
    // A ping-only threshold could never fire: every ping also refreshes the
    // timestamp the silence check reads, so "90s without a ping" is always
    // preceded by "60s without any message". It was removed; reintroducing one
    // adds a detector that cannot work and that future readers will trust.
    const code = stripComments(readWebFile(SOURCE))
    expect(code).not.toMatch(/HEARTBEAT_STALE_MS/)
    expect(code).not.toMatch(/lastPingAt/)
  })

  it('the liveness tick starts at dial time, not at open', () => {
    // The tick has to be running while the handshake is still in flight —
    // otherwise a socket that never opens is never inspected, which is the
    // original bug. Starting it inside `onopen` would silently restore that.
    const code = stripComments(readWebFile(SOURCE))
    // Matches `function connect(` regardless of the parameter list: the resume
    // path passes an option, and the guard is about WHERE the tick starts.
    const connectStart = code.indexOf('function connect(')
    expect(connectStart).toBeGreaterThan(-1)
    const onopen = code.indexOf('ws.onopen', connectStart)
    expect(onopen).toBeGreaterThan(connectStart)
    const body = code.slice(connectStart, onopen)

    expect(body, 'startHeartbeat() must run before the socket can open').toContain('startHeartbeat(')
  })
})
