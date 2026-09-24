import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useAppForeground } from '../useAppForeground'

describe('useAppForeground', () => {
  const originalSetAppForeground = (window as unknown as { __setAppForeground?: unknown }).__setAppForeground
  const originalAppResume = (window as unknown as { __clawbenchAppResume?: unknown }).__clawbenchAppResume
  const originalAddEventListener = document.addEventListener
  // Deterministic clock for the resume dedupe window. A real spy (not fake
  // timers) so the dynamically imported module sees it. Recreated per test
  // because afterEach restores all mocks.
  let nowSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    // Fresh module state for each test by resetting the global signal and
    // listeners.
    vi.resetModules()
    nowSpy = vi.spyOn(Date, 'now').mockReturnValue(1_000_000)
  })

  afterEach(() => {
    const w = window as unknown as { __setAppForeground?: unknown; __clawbenchAppResume?: unknown }
    if (originalSetAppForeground === undefined) {
      delete w.__setAppForeground
    } else {
      w.__setAppForeground = originalSetAppForeground
    }
    if (originalAppResume === undefined) {
      delete w.__clawbenchAppResume
    } else {
      w.__clawbenchAppResume = originalAppResume
    }
    document.addEventListener = originalAddEventListener
    vi.restoreAllMocks()
  })

  it('starts with app in the foreground', async () => {
    const { useAppForeground: useFG } = await import('../useAppForeground')
    const { appInForeground } = useFG()
    expect(appInForeground.value).toBe(true)
  })

  it('installs the native bridges UNCONDITIONALLY (no pre-existing function required)', async () => {
    // Regression: the bridges used to be installed behind
    // `if (typeof window.__setAppForeground === 'function')`. Since the native
    // host only ever CALLS the global (guarded by the same typeof check) and
    // never defines it, neither side ever created it — the bridge silently
    // never worked, so the Android foreground re-sync never ran. Nothing is
    // pre-seeded here on purpose: the composable must be the one to define them.
    const w = window as unknown as { __setAppForeground?: unknown; __clawbenchAppResume?: unknown }
    delete w.__setAppForeground
    delete w.__clawbenchAppResume

    const { useAppForeground: useFG } = await import('../useAppForeground')
    useFG()

    expect(typeof w.__setAppForeground).toBe('function')
    expect(typeof w.__clawbenchAppResume).toBe('function')
  })

  it('picks up the native host foreground signal (Android onPause/onResume bridge)', async () => {
    const { useAppForeground: useFG } = await import('../useAppForeground')
    const { appInForeground } = useFG()

    // The composable installs itself as the global handler, so the injected
    // function IS the bridge that Android MainActivity.onPause/onResume calls.
    const bridge = (window as unknown as { __setAppForeground: (fg: boolean) => void }).__setAppForeground
    expect(bridge).toBeTypeOf('function')

    // Simulate Android MainActivity.onPause() pushing background state.
    bridge(false)
    expect(appInForeground.value).toBe(false)

    // onResume pushes foreground state.
    bridge(true)
    expect(appInForeground.value).toBe(true)
  })

  it('falls back to document.visibilityState when no native bridge exists', async () => {
    // No native host: the composable installs its own bridges, but nothing
    // external calls them, so the Page Visibility API must drive the state.
    let visibilityHandler: (() => void) | null = null
    document.addEventListener = vi.fn((type: string, handler: EventListenerOrEventListenerObject) => {
      if (type === 'visibilitychange') visibilityHandler = handler as () => void
    }) as typeof document.addEventListener

    const { useAppForeground: useFG } = await import('../useAppForeground')
    const { appInForeground } = useFG()

    expect(appInForeground.value).toBe(true)

    // Simulate the page going hidden (e.g. desktop browser tab backgrounded).
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    visibilityHandler?.()
    expect(appInForeground.value).toBe(false)

    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    visibilityHandler?.()
    expect(appInForeground.value).toBe(true)
  })

  it('notifies onAppForeground listeners only on actual transitions', async () => {
    const { useAppForeground: useFG, onAppForeground: onFG } = await import('../useAppForeground')
    const { appInForeground } = useFG()

    const calls: boolean[] = []
    const unsubscribe = onFG((fg) => calls.push(fg))

    const bridge = (window as unknown as { __setAppForeground: (fg: boolean) => void }).__setAppForeground

    // Background → listener fires with false.
    bridge(false)
    expect(calls).toEqual([false])
    expect(appInForeground.value).toBe(false)

    // Foreground → listener fires with true.
    bridge(true)
    expect(calls).toEqual([false, true])

    // Same-state no-op (e.g. repeated onResume with no pause in between)
    // must not fire again.
    bridge(true)
    expect(calls).toEqual([false, true])

    // Unsubscribed listener stops receiving notifications.
    unsubscribe()
    bridge(false)
    expect(calls).toEqual([false, true])
    expect(appInForeground.value).toBe(false)
  })

  it('does NOT fire onAppResume when the app goes to the background', async () => {
    // Backgrounding must never run the resume work (re-sync / mark-read) — the
    // floating status window needs the unread badge to survive while the app is
    // away. Only the state signal fires.
    const { useAppForeground: useFG, onAppResume, onAppForeground } = await import('../useAppForeground')
    const { appInForeground } = useFG()

    let resumes = 0
    const states: boolean[] = []
    onAppResume(() => { resumes++ })
    onAppForeground((fg) => states.push(fg))

    const bridge = (window as unknown as { __setAppForeground: (fg: boolean) => void }).__setAppForeground
    bridge(false)

    expect(states).toEqual([false])
    expect(resumes).toBe(0)
    expect(appInForeground.value).toBe(false)
  })

  it('fires onAppResume on every resume, even when the state never left the foreground', async () => {
    // Regression for the reported symptom: Android freezes the WebView while
    // paused, so the onPause JS call can be dropped entirely. The state is then
    // still `true` on return — a state-transition listener never fires and the
    // session is never re-synced. The resume signal must not be gated on a
    // transition.
    const { useAppForeground: useFG, onAppResume } = await import('../useAppForeground')
    const { appInForeground } = useFG()

    let resumes = 0
    onAppResume(() => { resumes++ })

    const resumeBridge = (window as unknown as { __clawbenchAppResume: () => void }).__clawbenchAppResume
    expect(resumeBridge).toBeTypeOf('function')

    // No preceding background notification at all — state is still true.
    expect(appInForeground.value).toBe(true)
    resumeBridge()
    expect(resumes).toBe(1)

    // A second genuine resume still fires (past the dedupe window).
    nowSpy.mockReturnValue(Date.now() + 5000)
    resumeBridge()
    expect(resumes).toBe(2)
  })

  it('collapses a native resume immediately followed by a visibilitychange resume', async () => {
    // Android can produce both signals for one resume (the native hook, plus a
    // visibilitychange when the WebView did manage to flip to hidden). Firing
    // the re-sync twice would double the refresh work for no benefit.
    let visibilityHandler: (() => void) | null = null
    document.addEventListener = vi.fn((type: string, handler: EventListenerOrEventListenerObject) => {
      if (type === 'visibilitychange') visibilityHandler = handler as () => void
    }) as typeof document.addEventListener

    const { useAppForeground: useFG, onAppResume } = await import('../useAppForeground')
    useFG()

    let resumes = 0
    onAppResume(() => { resumes++ })

    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    const resumeBridge = (window as unknown as { __clawbenchAppResume: () => void }).__clawbenchAppResume
    resumeBridge()
    visibilityHandler?.()

    expect(resumes).toBe(1)
  })

  it('unsubscribed resume listeners stop receiving notifications', async () => {
    const { useAppForeground: useFG, onAppResume } = await import('../useAppForeground')
    useFG()

    let resumes = 0
    const unsubscribe = onAppResume(() => { resumes++ })
    const resumeBridge = (window as unknown as { __clawbenchAppResume: () => void }).__clawbenchAppResume

    resumeBridge()
    expect(resumes).toBe(1)

    unsubscribe()
    nowSpy.mockReturnValue(Date.now() + 5000)
    resumeBridge()
    expect(resumes).toBe(1)
  })
})
