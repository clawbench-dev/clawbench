import { appLog, diagLog } from './appLog.ts'

/**
 * TEMPORARY diagnostic for "the running dots are visible but do not move on one
 * device, while the same build animates on another".
 *
 * The leading hypothesis is that the device reports `prefers-reduced-motion:
 * reduce`, which our stylesheet honours by freezing the dots (see the
 * `@media (prefers-reduced-motion: reduce)` block in SessionList.vue). That
 * preference is a per-device OS setting, which matches the report exactly: same
 * build, different machine, different result.
 *
 * The reason this needs its own probe rather than a code read is that the
 * symptom has several indistinguishable causes, and they need different fixes:
 *
 *   - the OS asks for reduced motion      → the freeze is CORRECT behaviour
 *   - the compositor/animation clock is dead (remote desktop, headless, a
 *     software renderer, Android's "remove animations" accessibility flag)
 *                                          → the freeze is a BUG to work around
 *   - the CSS never applied at all        → a build/serving problem
 *
 * So this logs the QUERY RESULT, the COMPUTED STYLE, and — decisively —
 * whether the rendered transform actually changes over time. A frozen dot with
 * `animation-name: none` is the stylesheet honouring a preference; a frozen dot
 * that still reports a running animation is the engine failing to advance it.
 *
 * Emits via `diagLog` so the lines reach {data-dir}/logs/client.log even with
 * the `logCapture` setting off — the whole point is to diagnose a device we do
 * not have in front of us.
 *
 * ── Why each line is emitted in TWO forms ──────────────────────────────────
 * A single `diagLog` call is NOT reliable here, and this was measured, not
 * assumed. `appLog`'s ring buffer caps at 200 entries and, on overflow, drops
 * the OLDEST (`appLog.ts` — `buffer.splice(0, …)`). The relay only flushes on a
 * 2s timer. This probe runs at startup, so its line lands at the FRONT of a
 * buffer that a busy chat view then floods — measured peaks of ~2450 log
 * lines/second while a stream is rendering. The line is therefore trimmed away
 * before the first flush and never reaches the server.
 *
 * Observed before the fix: 14 page loads produced only 2 `env:` lines, while
 * the `movement:` line (emitted ~2.5s later, closer to a flush) always survived.
 * That asymmetry is the signature of front-trimming, and it is exactly how a
 * diagnostic silently reports nothing and looks like "the feature is broken".
 *
 * The fix has two parts, because either alone is insufficient:
 *   1. Re-emit on an interval, so a later copy is always near the next flush.
 *   2. ALSO write to the console, which is the sink that survives regardless —
 *      on Android that is logcat, which is read directly from the device.
 *
 * REMOVE once the report is explained.
 *
 * REMOVE once the report is explained.
 */

const TAG = 'MotionEnv'

/**
 * Every timer this module starts.
 *
 * Tracked so the probe can be stopped as a unit. Without this, a sample chain
 * started by one test keeps emitting into the next one — which showed up as
 * bogus `moved=true` lines failing a frozen-transform assertion. Production
 * only needs it for `pagehide`; tests use `_stopMotionDiag`.
 */
const pendingTimers = new Set<ReturnType<typeof setTimeout>>()
let stopped = false

/** Test-only: cancel every timer and silence further emissions. */
export function _stopMotionDiag(): void {
  stopped = true
  for (const t of pendingTimers) clearTimeout(t)
  pendingTimers.clear()
}

/** Test-only: allow emissions again after `_stopMotionDiag`. */
export function _resetMotionDiag(): void {
  _stopMotionDiag()
  stopped = false
}

function later(fn: () => void, ms: number): void {
  const t = setTimeout(() => {
    pendingTimers.delete(t)
    fn()
  }, ms)
  pendingTimers.add(t)
}

/**
 * Emit one diagnostic line through BOTH sinks.
 *
 * `diagLog` is the load-bearing one: it bypasses the `logCapture` gate, so the
 * line reaches the server even when capture is off.
 *
 * The `appLog.w` copy is the local backup — it writes to the console (and
 * therefore logcat on Android, `desktop.log` on Electron) whenever the relay is
 * not in its Android single-HTTP mode. That gives a copy that no log buffer can
 * trim, which matters because a diagnostic that silently reports nothing looks
 * exactly like a feature that is broken. It goes through `appLog` rather than
 * raw `console.*` because the repo forbids the latter (AGENTS.md).
 *
 * `immediate` is set for the startup lines. They are written before the app is
 * busy, so they sit at the front of a buffer that is about to be flooded past
 * its cap — and overflow drops from the front. Without an immediate flush the
 * line is reliably lost: measured, 14 page loads produced only 2 lines. The
 * movement line is emitted after a delay, so it rides the normal debounce.
 */
function emitDiag(msg: string, immediate = false): void {
  if (stopped) return
  diagLog(TAG, msg, immediate ? { immediate: true } : undefined)
  try {
    appLog.w(TAG, msg)
  } catch {
    /* logging must never break the probe */
  }
}

/**
 * Self-contained animation-engine test.
 *
 * The dot-based probe needs a running session with the list open, which is
 * exactly what the first phone report did NOT have (`dotAnim=no-dot`) — so it
 * produced no answer. This test creates its own element and its own animation,
 * so it works on any device the moment the app loads, with no app state at all.
 *
 * It answers the question that actually matters here: "can this browser advance
 * a CSS animation at all?" If a freshly created element with a freshly created
 * animation does not advance, the problem is the engine/compositor on that
 * device — nothing to do with our session row. If it DOES advance, then the
 * engine is fine and the fault is specific to the row's own styles, which
 * narrows the search to the component.
 *
 * The element is detached (never inserted into the document) but `getComputedStyle`
 * on a detached element does not resolve animations, so it is attached inside a
 * zero-size, aria-hidden, pointer-events-none wrapper — invisible, but a real
 * rendered box so the engine animates it. The wrapper is removed at the end.
 */
export function probeAnimationEngine(): void {
  let host: HTMLDivElement | null = null
  let el: HTMLDivElement | null = null
  try {
    host = document.createElement('div')
    host.setAttribute('aria-hidden', 'true')
    host.style.cssText =
      'position:fixed;left:-9999px;top:0;width:4px;height:4px;overflow:hidden;pointer-events:none;opacity:0;'
    el = document.createElement('div')
    el.style.cssText =
      'width:4px;height:4px;background:transparent;animation:clawbench-engine-probe 1s linear infinite;'
    // The keyframes must exist somewhere; inject them inline so this test does
    // not depend on any stylesheet having loaded.
    const style = document.createElement('style')
    style.textContent = '@keyframes clawbench-engine-probe{from{transform:translateX(0)}to{transform:translateX(8px)}}'
    host.appendChild(style)
    host.appendChild(el)
    document.body.appendChild(host)
  } catch {
    emitDiag('engine: could not create the probe element')
    return
  }

  const read = (): string | null => {
    if (!el) return null
    try {
      return getComputedStyle(el).transform
    } catch {
      return null
    }
  }
  const readClock = (): number | null => {
    if (!el) return null
    try {
      const a = el.getAnimations()
      if (!a.length) return null
      const t = a[0].currentTime
      return typeof t === 'number' ? Math.round(t) : null
    } catch {
      return null
    }
  }

  const values: string[] = []
  const clocks: (number | null)[] = []
  const SAMPLES = 6
  const GAP_MS = 200

  const cleanup = () => {
    try {
      host?.remove()
    } catch {
      /* ignore */
    }
    host = null
    el = null
  }

  const take = (i: number) => {
    if (stopped) {
      cleanup()
      return
    }
    const v = read()
    if (v !== null) values.push(v)
    clocks.push(readClock())
    if (i + 1 < SAMPLES) {
      later(() => take(i + 1), GAP_MS)
      return
    }
    const unique = [...new Set(values)]
    const clockNums = clocks.filter((c): c is number => c !== null)
    const clockMoved = clockNums.length > 1 && clockNums[0] !== clockNums[clockNums.length - 1]
    emitDiag(
      `engine: canAnimate=${unique.length > 1} spread=${unique.length}/${values.length} ` +
        `clockMoved=${clockMoved} clock=${clockNums.join(',') || 'none'} ` +
        `t0="${values[0] ?? '-'}" t1="${values[values.length - 1] ?? '-'}"`,
    )
    cleanup()
  }
  take(0)
}

/** The dot selector, kept in one place so the probe and the app cannot drift. */
const RUNNING_DOT = '.session-status.is-running .session-status-dot'

/**
 * The first running dot that is ACTUALLY RENDERED.
 *
 * This distinction is load-bearing. The session list is kept alive with
 * `v-show`, so the dots stay in the DOM while the drawer is closed — an
 * element inside `display:none` still matches `querySelector` but reports a
 * zero-size box, and engines return an empty `getAnimations()` for it. Sampling
 * one of those would report "animation missing, no movement", which is exactly
 * the symptom being investigated and would be a false positive.
 */
function visibleRunningDot(): HTMLElement | null {
  try {
    for (const el of document.querySelectorAll<HTMLElement>(RUNNING_DOT)) {
      if (el.getBoundingClientRect().width > 0) return el
    }
    return null
  } catch {
    return null
  }
}

function mq(query: string): boolean | 'unsupported' {
  try {
    if (typeof window.matchMedia !== 'function') return 'unsupported'
    return window.matchMedia(query).matches
  } catch {
    return 'unsupported'
  }
}

/** `getComputedStyle` on the first rendered running dot, or null when none. */
function dotStyle(): CSSStyleDeclaration | null {
  const el = visibleRunningDot()
  if (!el) return null
  try {
    return getComputedStyle(el)
  } catch {
    return null
  }
}

/**
 * Snapshot of everything that decides whether the dots can move.
 *
 * Exported for the unit test; the call site is `installMotionDiag()`.
 */
export function collectMotionEnv(): string {
  const reduce = mq('(prefers-reduced-motion: reduce)')
  const noPref = mq('(prefers-reduced-motion: no-preference)')
  const cs = dotStyle()

  // A dot that exists but has no animation at all is the stylesheet's
  // reduced-motion branch. Reporting the pair makes that unambiguous.
  const animName = cs ? cs.animationName : 'no-dot'
  const animDur = cs ? cs.animationDuration : '-'
  const opacity = cs ? cs.opacity : '-'

  // How many animations the engine actually holds for the dot. A CSS animation
  // that the stylesheet disabled is absent here too, but this also catches the
  // case where the declaration exists and the engine dropped it. -1 means no
  // rendered dot; -2 means the API itself threw.
  let animCount: number
  try {
    const el = visibleRunningDot()
    animCount = el ? el.getAnimations().length : -1
  } catch {
    animCount = -2
  }

  return [
    `reduce=${reduce}`,
    `no-preference=${noPref}`,
    `reduceSupported=${reduce !== 'unsupported'}`,
    `dotAnim=${animName}`,
    `dotDur=${animDur}`,
    `dotOpacity=${opacity}`,
    `dotAnims=${animCount}`,
  ].join(' ')
}

/** Environment facts that are useful context but do not change per sample. */
function collectEnvFacts(): string {
  const ua = typeof navigator !== 'undefined' ? navigator.userAgent : '?'
  const native = (() => {
    try {
      return typeof (window as unknown as { ClawBenchNative?: unknown }).ClawBenchNative !== 'undefined'
    } catch {
      return false
    }
  })()
  return [
    `native=${native}`,
    `ua="${ua}"`,
    `visibility=${typeof document !== 'undefined' ? document.visibilityState : '?'}`,
    `dpr=${typeof window !== 'undefined' ? window.devicePixelRatio : '?'}`,
    `hwConc=${typeof navigator !== 'undefined' ? navigator.hardwareConcurrency : '?'}`,
    `reducedMotionCSS=${mq('(prefers-reduced-motion)')}`,
  ].join(' ')
}

/**
 * Sample the running dot's rendered transform over a window and report whether
 * it changed.
 *
 * This is the load-bearing check. `getComputedStyle` reports the CURRENT
 * animated value, so samples taken apart differ whenever the animation is
 * actually advancing — regardless of what the declaration says.
 *
 * It takes SEVERAL samples rather than two, and reports the observed spread.
 * Two samples can land at the same point of a slow curve and read as "frozen"
 * when nothing is wrong; a spread over ~1.2s (longer than the 1.05s cycle)
 * cannot. `spread` is the honest summary: 0 means the value genuinely never
 * changed across a full cycle.
 *
 * `animTime` is the decisive extra field for the Android/WebView case. It is the
 * animation's own clock, independent of the rendered value:
 *
 *   - clock ADVANCES, transform does NOT  → the engine is running the animation
 *     but the compositor is not producing new frames (GPU/compositing problem,
 *     which is the remote-desktop / software-renderer family)
 *   - clock does NOT advance               → the animation clock itself is
 *     stalled or the animation was never started
 *
 * Without this split, "the dot does not move" is ambiguous between the two, and
 * they need completely different fixes.
 */
export function sampleDotMovement(): string {
  const el = visibleRunningDot()
  if (!el) {
    // No running row on screen — nothing to sample. Say so rather than
    // reporting a false "frozen".
    emitDiag(`movement: no running dot rendered; ${collectMotionEnv()}`)
    return 'no-dot'
  }

  const readValue = (): string | null => {
    try {
      const c = getComputedStyle(el)
      return `${c.transform}|${c.opacity}`
    } catch {
      return null
    }
  }

  /** The animation's own clock, or null when there is no animation object. */
  const readClock = (): number | null => {
    try {
      const anims = el.getAnimations()
      if (!anims.length) return null
      const t = anims[0].currentTime
      return typeof t === 'number' ? Math.round(t) : null
    } catch {
      return null
    }
  }

  const values: string[] = []
  const clocks: (number | null)[] = []
  const SAMPLES = 6
  const GAP_MS = 200

  const take = (i: number) => {
    if (stopped) return
    const v = readValue()
    if (v !== null) values.push(v)
    clocks.push(readClock())
    if (i + 1 < SAMPLES) {
      later(() => take(i + 1), GAP_MS)
      return
    }
    const unique = [...new Set(values)]
    const moved = unique.length > 1
    const clockNums = clocks.filter((c): c is number => c !== null)
    const clockMoved = clockNums.length > 1 && clockNums[0] !== clockNums[clockNums.length - 1]
    emitDiag(
      `movement: moved=${moved} spread=${unique.length}/${values.length} ` +
        `clockMoved=${clockMoved} clock=${clockNums.join(',') || 'none'} ` +
        `t0="${values[0] ?? '-'}" t1="${values[values.length - 1] ?? '-'}" ${collectMotionEnv()}`,
    )
  }
  take(0)
  return 'sampled'
}

/**
 * Install the motion-environment diagnostic.
 *
 * Called once from `main.ts` after the app mounts. It keeps watching until it
 * can actually measure something, then stops.
 *
 * ── Why it POLLS instead of retrying a fixed number of times ───────────────
 * The dot is only measurable once the session list is actually on screen: the
 * list is kept alive with `v-show`, so a closed drawer leaves the dots in the
 * DOM at zero size, and `visibleRunningDot()` correctly rejects them (sampling
 * one would report a false "frozen").
 *
 * The first version retried 12 times at 2.5s and then gave up — a 30s budget.
 * The first real report from a phone arrived inside that window with
 * `dotAnim=no-dot` and no movement sample at all: the app was up, but the user
 * had not opened the list yet. The user opens the drawer whenever they like,
 * which can be minutes later, so the probe must watch indefinitely (bounded by
 * a long budget purely to avoid a permanently idle timer) rather than assume a
 * dot is on screen at startup.
 *
 * The `env:` line is re-emitted on a slow interval, and the wait is also
 * re-checked on every visibility change (opening a drawer after backgrounding
 * the app is the common mobile path).
 */
export function installMotionDiag(): void {
  /** How often to look for a rendered running dot. */
  const POLL_MS = 2000
  /** How long to keep looking before reporting that we never saw one. */
  const BUDGET_MS = 15 * 60 * 1000
  /** Re-emit cadence for the environment line. See the header for why. */
  const ENV_REPEAT_MS = 300_000

  const startedAt = Date.now()
  const envLine = () => `env: ${collectEnvFacts()} | ${collectMotionEnv()}`

  // Immediate: written before the app is busy, so it must not wait out a
  // debounce that a render flood will overrun. See emitDiag.
  emitDiag(envLine(), true)

  // Answer "can this browser animate at all?" without depending on the user
  // having a running session open. The first phone report had `dotAnim=no-dot`
  // and therefore told us nothing; this always produces an answer.
  probeAnimationEngine()

  const envTimer = window.setInterval(() => emitDiag(envLine()), ENV_REPEAT_MS)
  let done = false

  const stopWatching = () => {
    if (done) return
    done = true
    window.clearInterval(envTimer)
    window.clearInterval(pollTimer)
  }

  /** Sample once a rendered dot exists; otherwise keep waiting. */
  const check = () => {
    if (done || stopped) return
    if (visibleRunningDot()) {
      sampleDotMovement() // emits the movement line once its samples complete
      stopWatching()
      return
    }
    if (Date.now() - startedAt > BUDGET_MS) {
      emitDiag(
        `gave up after ${Math.round(BUDGET_MS / 60000)}min: no rendered running dot ever appeared; ${collectMotionEnv()}`,
      )
      stopWatching()
    }
  }

  const pollTimer = window.setInterval(check, POLL_MS)

  // Opening the drawer right after backgrounding the app is the common mobile
  // path; check on the flip so it is sampled immediately, not up to 2s later.
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') check()
  })

  // Never keep the tab alive for a diagnostic.
  window.addEventListener('pagehide', stopWatching, { once: true })
}
