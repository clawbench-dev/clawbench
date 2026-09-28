import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { appLog } from '../appLog'
import { collectMotionEnv, sampleDotMovement, installMotionDiag, probeAnimationEngine, _resetMotionDiag } from '../motionDiag'

/**
 * Guards for the temporary "dots visible but not moving" diagnostic.
 *
 * The probe exists to tell apart three causes that look identical on screen:
 * the OS asking for reduced motion (correct), the engine failing to advance an
 * animation (a bug), and the CSS not applying at all (a build problem). The
 * value of the probe is entirely in WHICH fields it reports, so that is what is
 * pinned here — not the formatting.
 *
 * `diagLog` reaches the server through the same fetch relay as `appLog`, so the
 * assertions read the POST body for `diagLog`, and the `appLog` mirror is
 * exercised separately (it goes to the console/logcat, which no log buffer can
 * trim).
 */

/** Collect the `msg` fields of every entry the relay POSTs. */
function captureRelay(): string[] {
  const msgs: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation((_url: unknown, init: unknown) => {
    try {
      const body = JSON.parse((init as { body: string }).body)
      for (const e of body.entries ?? []) msgs.push(String(e.msg))
    } catch {
      /* ignore malformed bodies */
    }
    return Promise.resolve({ ok: true } as Response)
  })
  return msgs
}

/**
 * Minimal stand-in for a running dot with a controllable computed style.
 *
 * The dot must sit inside a `.session-status.is-running` ancestor — that is the
 * selector the probe (and the app) uses, so a bare dot would make the probe
 * report `no-dot` and every assertion would pass vacuously.
 *
 * `getBoundingClientRect` is stubbed because jsdom has NO layout engine: it
 * reports 0×0 for every element, so the probe's "is this dot actually rendered"
 * check would reject the fixture. The stub is not papering over a bug — the
 * probe's check is about `display:none` ancestors, which is exactly what jsdom
 * cannot model.
 */
function installDot(opts: {
  animationName?: string
  transform?: () => string
  /** Simulate the drawer being closed: the dot is in the DOM but not rendered. */
  hidden?: boolean
}) {
  const slot = document.createElement('span')
  slot.className = 'session-status is-running'
  const el = document.createElement('i')
  el.className = 'session-status-dot'
  el.getAnimations = () => []
  const w = opts.hidden ? 0 : 3
  el.getBoundingClientRect = () =>
    ({ width: w, height: w, top: 0, left: 0, right: w, bottom: w, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect
  slot.appendChild(el)
  document.body.appendChild(slot)
  const transform = opts.transform ?? (() => 'matrix(1, 0, 0, 1, 0, 0)')
  vi.spyOn(window, 'getComputedStyle').mockImplementation(
    () =>
      ({
        animationName: opts.animationName ?? 'session-status-bounce',
        animationDuration: '1.05s',
        opacity: '1',
        transform: transform(),
      }) as unknown as CSSStyleDeclaration,
  )
  return el
}

/**
 * jsdom ships no `matchMedia` at all, which is why every probe reading would
 * otherwise be `unsupported` and the assertions would pass vacuously. Assign it
 * directly rather than via `spyOn` — there is no existing function to spy on.
 */
function stubMatchMedia(reduceMatches: boolean) {
  const impl = (query: string) =>
    ({
      matches: query.includes('prefers-reduced-motion: reduce') ? reduceMatches : !reduceMatches,
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      onchange: null,
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: impl,
  })
}

beforeEach(() => {
  // Stop any sample chain the previous test started: it would otherwise keep
  // emitting into this test and produce a bogus `moved=true` line, which is
  // exactly how a frozen-transform assertion failed for the wrong reason.
  _resetMotionDiag()
  document.body.innerHTML = ''
  stubMatchMedia(false)
})

afterEach(() => {
  _resetMotionDiag()
  vi.restoreAllMocks()
})

describe('motion environment diagnostic', () => {
  it('reports the reduced-motion query and the dot animation as separate fields', () => {
    // These two must be reported TOGETHER and separately. `reduce=true` with
    // `dotAnim=none` is the stylesheet correctly honouring the preference;
    // `reduce=false` with `dotAnim=none` means the animation is missing for
    // some other reason. Collapsing them into one field would lose that.
    installDot({ animationName: 'none' })
    const line = collectMotionEnv()

    expect(line, 'the query result must be reported').toMatch(/reduce=(true|false)/)
    expect(line, 'the computed animation must be reported').toMatch(/dotAnim=/)
    expect(line).toContain('dotAnim=none')
    expect(line, 'the animation count distinguishes "disabled" from "dropped"').toMatch(/dotAnims=/)
  })

  it('reports reduce=true when the device asks for reduced motion', () => {
    // This is the hypothesis under test: a device that reports the preference
    // makes our stylesheet freeze the dots, which is correct behaviour and must
    // be distinguishable from a stalled animation engine.
    stubMatchMedia(true)
    installDot({ animationName: 'none' })
    const line = collectMotionEnv()
    expect(line, 'the preference must be visible in the log').toContain('reduce=true')
    expect(line).toContain('dotAnim=none')
  })

  it('reports no-dot rather than a false "frozen" when nothing is running', () => {
    // A false "frozen" would send the investigation in the wrong direction, so
    // the absence of a dot must be stated explicitly.
    vi.spyOn(window, 'getComputedStyle').mockImplementation(
      () => ({ animationName: 'x', animationDuration: '1s', opacity: '1', transform: 'none' }) as unknown as CSSStyleDeclaration,
    )
    const line = collectMotionEnv()
    expect(line, 'a missing dot must be reported as such').toContain('dotAnim=no-dot')
  })

  it('reports moved=true when the rendered transform advances between samples', async () => {
    // The load-bearing check: an animation can be declared and still not
    // advance (dead compositor / animation clock). Only comparing two samples
    // of the RENDERED value catches that, so the assertion is on the probe's
    // own emitted line rather than on a re-implementation of its comparison.
    let n = 0
    installDot({ transform: () => `matrix(1, 0, 0, 1, 0, ${n})` })
    const msgs = captureRelay()

    sampleDotMovement()
    n = 5 // the engine advanced between the first and later reads
    // The probe takes 6 samples 200ms apart, so the wait must exceed 1.2s.
    await new Promise((r) => setTimeout(r, 1800))

    const movement = msgs.find((m) => m.includes('movement:'))
    expect(movement, 'the probe must emit a movement line').toBeTruthy()
    expect(movement, 'an advancing transform must be reported as moved').toContain('moved=true')
  })

  it('reports moved=false when the transform is frozen, which is the reported bug', async () => {
    // This is the symptom to diagnose: the dot is rendered (so the CSS applied)
    // but its transform never changes. The probe must call that out explicitly,
    // because "no movement" is what distinguishes a stalled engine from a
    // correctly-honoured reduced-motion preference.
    installDot({ transform: () => 'matrix(1, 0, 0, 1, 0, 0)' })
    const msgs = captureRelay()

    sampleDotMovement()
    await new Promise((r) => setTimeout(r, 1800))

    const movement = msgs.find((m) => m.includes('movement:'))
    expect(movement, 'the probe must emit a movement line').toBeTruthy()
    expect(movement, 'a frozen transform must be reported as moved=false').toContain('moved=false')
  })

  it('ignores a dot that is in the DOM but not rendered', async () => {
    // The session list is kept alive with `v-show`, so dots stay in the DOM
    // while the drawer is closed. An element inside `display:none` still matches
    // querySelector but reports a zero-size box and an empty getAnimations().
    // Sampling one would report "no animation, no movement" — the exact symptom
    // under investigation — so it would be a FALSE POSITIVE and send the whole
    // investigation the wrong way.
    installDot({ hidden: true, animationName: 'none' })
    const msgs = captureRelay()

    sampleDotMovement()
    await new Promise((r) => setTimeout(r, 1800))

    const movement = msgs.find((m) => m.includes('movement:'))
    expect(movement, 'the probe must emit a line').toBeTruthy()
    expect(movement, 'a hidden dot must be reported as no-dot, not as frozen').toContain('no running dot')
    expect(movement, 'a hidden dot must never be reported as moved=false').not.toContain('moved=false')
  })

  it('mirrors every line to appLog, the sink the log buffer cannot drop', async () => {
    // The HTTP relay drops the OLDEST entry when its 200-entry buffer overflows,
    // and this probe writes at startup — so a single emission reliably loses the
    // race against a busy chat view (measured: only 2 of 14 page loads got
    // through). The appLog copy is the sink that survives regardless: it goes to
    // the console (logcat on Android), which nothing trims. Without this mirror
    // a diagnostic can silently report nothing and look like a broken feature.
    //
    // Asserted on the appLog WARN path rather than console.warn directly, because
    // appLog routes to the console only in non-single-HTTP mode. Spying on the
    // exported function pins the mirror without depending on that routing.
    const warned: string[] = []
    const spy = vi.spyOn(appLog, 'w').mockImplementation((...a: unknown[]) => {
      warned.push(a.map(String).join(' '))
    })
    installDot({ animationName: 'none' })

    sampleDotMovement()
    await new Promise((r) => setTimeout(r, 1800))

    expect(spy, 'every emitted line must also go through appLog').toHaveBeenCalled()
    expect(
      warned.some((w) => w.includes('MotionEnv') && w.includes('movement:')),
      'the movement line must reach appLog',
    ).toBe(true)
  })

  it('keeps watching for a rendered dot instead of giving up at startup', async () => {
    // The first version retried 12 times and gave up after ~30s. The first real
    // report from a phone landed inside that window with `dotAnim=no-dot` and
    // no movement sample: the app was up, but the user had not opened the
    // session list yet. Users open it whenever they like, so a probe that only
    // looks at startup reports nothing useful.
    //
    // This pins the fix: installMotionDiag must keep polling until a rendered
    // dot appears, then sample it.
    //
    // The explicit timeout is needed because the test spans two poll intervals
    // plus a full sample window (~6s), which is longer than vitest's 5s default.
    const msgs: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation((_u: unknown, init: unknown) => {
      try {
        for (const e of JSON.parse((init as { body: string }).body).entries ?? []) msgs.push(String(e.msg))
      } catch { /* ignore */ }
      return Promise.resolve({ ok: true } as Response)
    })
    vi.spyOn(console, 'warn').mockImplementation(() => {})

    installMotionDiag()
    // The first poll fires at POLL_MS (2s), so the assertion must happen AFTER
    // that tick — asserting at ~100ms would pass even if the probe gave up on
    // its very first check, because no check had run yet. (That weaker version
    // of this test survived a mutation that gave up immediately.)
    await new Promise((r) => setTimeout(r, 2300))
    expect(
      msgs.some((m) => m.includes('gave up')),
      'the probe must not give up while a dot may still appear',
    ).toBe(false)

    // Now the user opens the drawer: a rendered dot appears. It must still be
    // picked up, i.e. the probe did not stop watching after the first miss.
    installDot({ transform: () => 'matrix(1, 0, 0, 1, 0, 0)' })
    await new Promise((r) => setTimeout(r, 3600))

    expect(
      msgs.some((m) => m.includes('movement:')),
      'once a rendered dot appears, the probe must sample it',
    ).toBe(true)
  }, 15_000)

  it('does not throw when matchMedia is unavailable', () => {
    // Older WebViews and some test environments have no matchMedia. The probe
    // is diagnostic, so it must degrade rather than break the app it measures.
    const orig = window.matchMedia
    // @ts-expect-error deliberately removing the API
    delete window.matchMedia
    installDot({})
    let line = ''
    expect(() => { line = collectMotionEnv() }).not.toThrow()
    expect(line, 'a missing API must be reported, not silently treated as false').toContain(
      'reduce=unsupported',
    )
    window.matchMedia = orig
  })
})

describe('animation engine probe', () => {
  it('answers without needing a running session open', async () => {
    // The first phone report came back `dotAnim=no-dot` because the user had no
    // running session on screen — so the dot-based probe produced NO answer.
    // This self-contained test creates its own element and animation, so it
    // reports on every device the moment the app loads.
    const msgs = captureRelay()
    probeAnimationEngine()
    await new Promise((r) => setTimeout(r, 1800))

    const line = msgs.find((m) => m.includes('engine:'))
    expect(line, 'the engine probe must always emit a line').toBeTruthy()
    expect(line, 'it must report whether animation is possible').toMatch(/canAnimate=(true|false)/)
    expect(line, 'the clock split distinguishes stalled engine from stalled compositor').toMatch(
      /clockMoved=(true|false)/,
    )
  })

  it('leaves no element behind', async () => {
    // The probe element must not accumulate in the DOM or become visible.
    probeAnimationEngine()
    await new Promise((r) => setTimeout(r, 1800))
    expect(
      document.querySelector('[aria-hidden="true"][style*="-9999px"]'),
      'the probe must clean up after itself',
    ).toBeNull()
  })
})
