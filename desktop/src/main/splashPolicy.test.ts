import { describe, it, expect } from 'vitest'
import {
  CONNECTION_TIMEOUT_MS,
  SPLASH_BG_DARK,
  SPLASH_BG_LIGHT,
  SPLASH_FAILSAFE_MS,
  SPLASH_STAGE_EVENTS,
  SPLASH_STAGE_ORDER,
  isForwardStage,
  shouldShowSplash,
  splashBackgroundColor,
  stageForEvent,
} from './splashPolicy'

describe('stageForEvent', () => {
  it('maps each lifecycle event to its stage', () => {
    // The mapping is the whole stage display: a wrong pairing shows the user a
    // message that contradicts what is actually happening.
    expect(stageForEvent('did-start-loading')).toBe('loading')
    expect(stageForEvent('dom-ready')).toBe('rendering')
    expect(stageForEvent('did-finish-load')).toBe('initializing')
  })

  it('returns null for an unrelated event rather than a default stage', () => {
    // A default would let any stray event silently reset the text to the first
    // stage, which reads as the connection restarting.
    expect(stageForEvent('did-fail-load')).toBeNull()
    expect(stageForEvent('')).toBeNull()
    expect(stageForEvent('did-start-navigation')).toBeNull()
  })

  it('lists events in stage order', () => {
    const order = SPLASH_STAGE_EVENTS.map((e) => SPLASH_STAGE_ORDER.indexOf(e.stage))
    expect(order).toEqual([...order].sort((a, b) => a - b))
  })
})

describe('isForwardStage', () => {
  it('accepts the first stage when nothing is shown yet', () => {
    expect(isForwardStage(null, 'connecting')).toBe(true)
    // Not only the first stage: the overlay page may finish loading after a
    // later event already fired, and that stage must still be applied.
    expect(isForwardStage(null, 'initializing')).toBe(true)
  })

  it('accepts forward moves and rejects backwards ones', () => {
    expect(isForwardStage('connecting', 'loading')).toBe(true)
    expect(isForwardStage('loading', 'initializing')).toBe(true)
    expect(isForwardStage('initializing', 'loading')).toBe(false)
    expect(isForwardStage('rendering', 'connecting')).toBe(false)
  })

  it('rejects a repeat of the current stage', () => {
    // A reload re-fires the same events; re-applying is harmless but the guard
    // keeps the state machine's meaning exact.
    expect(isForwardStage('loading', 'loading')).toBe(false)
    expect(isForwardStage('initializing', 'initializing')).toBe(false)
  })
})

describe('shouldShowSplash', () => {
  it('shows the overlay for remote navigations', () => {
    expect(shouldShowSplash('http://localhost:20000')).toBe(true)
    expect(shouldShowSplash('https://example.com:8443/')).toBe(true)
    expect(shouldShowSplash('HTTPS://EXAMPLE.COM')).toBe(true)
  })

  it('does not show it for the local login page', () => {
    // First run loads the login page from disk; there is no network wait, so an
    // overlay would be a one-frame flash of the logo.
    expect(shouldShowSplash('file:///opt/clawbench/resources/login.html')).toBe(false)
    expect(shouldShowSplash('')).toBe(false)
    expect(shouldShowSplash('about:blank')).toBe(false)
  })

  it('does not treat a non-http scheme as remote', () => {
    // Guards against a future caller passing something like a data: URL and
    // getting an overlay over a document that loads instantly.
    expect(shouldShowSplash('data:text/html,hi')).toBe(false)
    expect(shouldShowSplash('javascript:void(0)')).toBe(false)
  })
})

describe('splashBackgroundColor', () => {
  it('returns the dark backdrop for a dark theme', () => {
    expect(splashBackgroundColor(true)).toBe(SPLASH_BG_DARK)
  })

  it('returns the light backdrop for a light theme', () => {
    expect(splashBackgroundColor(false)).toBe(SPLASH_BG_LIGHT)
  })

  it('uses the default theme backdrops', () => {
    // These must stay the github-dark / github-light --bg-primary values, or the
    // overlay flashes the wrong colour before the page applies its own theme.
    expect(SPLASH_BG_DARK).toBe('#161b22')
    expect(SPLASH_BG_LIGHT).toBe('#f8f9fa')
  })
})

describe('timeouts', () => {
  it('matches the Android splash constants', () => {
    // The two platforms should give up at the same point; a smaller fail-safe
    // would hide the overlay during a legitimately slow boot.
    expect(SPLASH_FAILSAFE_MS).toBe(15_000)
    expect(CONNECTION_TIMEOUT_MS).toBe(90_000)
  })

  it('gives the connection longer than the boot fail-safe', () => {
    // The fail-safe covers "page loaded but JS never reported ready", the
    // connection timeout covers "page never loaded". If the former were longer
    // it would be the one firing for an unreachable server.
    expect(CONNECTION_TIMEOUT_MS).toBeGreaterThan(SPLASH_FAILSAFE_MS)
  })
})
