import { describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { createRenderCrashRecovery } from '@/utils/renderCrashRecovery'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
  flushLogNow: vi.fn(),
}))

function fakeStorage(initial: Record<string, string> = {}) {
  const map = new Map(Object.entries(initial))
  return {
    getItem: (k: string) => (map.has(k) ? (map.get(k) as string) : null),
    setItem: (k: string, v: string) => { map.set(k, v) },
    dump: () => Object.fromEntries(map),
  }
}

function setup(initial?: Record<string, string>) {
  let clock = 1_000_000
  const reload = vi.fn()
  const storage = fakeStorage(initial)
  const recovery = createRenderCrashRecovery({
    reload,
    storage,
    now: () => clock,
    reloadDelayMs: 0,
  })
  return {
    recovery,
    reload,
    storage,
    advance: (ms: number) => { clock += ms },
  }
}

const MSG = 'render threw a generic boom'

describe('renderCrashRecovery', () => {
  it('does not reload on a one-off error', () => {
    const { recovery, reload } = setup()
    expect(recovery.note(MSG, 'MeetingPanel')).toBe(false)
    expect(reload).not.toHaveBeenCalled()
  })

  it('reloads when the same error repeats inside the window (the UI is stuck)', () => {
    const { recovery, reload, advance } = setup()
    recovery.note(MSG, 'MeetingPanel')
    advance(1_000)
    expect(recovery.note(MSG, 'MeetingPanel')).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does not treat a different component or a different error as a repeat', () => {
    const { recovery, reload, advance } = setup()
    recovery.note(MSG, 'MeetingPanel')
    advance(500)
    expect(recovery.note(MSG, 'ChatPanelContent')).toBe(false)
    advance(500)
    expect(recovery.note('other failure', 'ChatPanelContent')).toBe(false)
    expect(reload).not.toHaveBeenCalled()
  })

  it('forgets the repeat once the window has passed', () => {
    const { recovery, reload, advance } = setup()
    recovery.note(MSG, 'MeetingPanel')
    advance(11_000)
    expect(recovery.note(MSG, 'MeetingPanel')).toBe(false)
    expect(reload).not.toHaveBeenCalled()
  })

  it('honours the cool-down: no second reload right after the first', () => {
    const { recovery, reload, advance } = setup()
    recovery.note(MSG, 'X')
    advance(100)
    recovery.note(MSG, 'X')
    expect(reload).toHaveBeenCalledTimes(1)

    // Same error again immediately: the counter reset, so this is a fresh
    // "first" observation — and even the repeat must not reload again.
    advance(100)
    expect(recovery.note(MSG, 'X')).toBe(false)
    advance(100)
    expect(recovery.note(MSG, 'X')).toBe(false)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('allows one more reload after the cool-down, then stops for good', () => {
    const { recovery, reload, advance } = setup()
    recovery.note(MSG, 'X')
    advance(100)
    recovery.note(MSG, 'X')
    expect(reload).toHaveBeenCalledTimes(1)

    advance(61_000)
    recovery.note(MSG, 'X')
    advance(100)
    recovery.note(MSG, 'X')
    expect(reload).toHaveBeenCalledTimes(2)

    advance(61_000)
    recovery.note(MSG, 'X')
    advance(100)
    recovery.note(MSG, 'X')
    expect(reload).toHaveBeenCalledTimes(2)
  })

  it('carries the reload budget across a reload (sessionStorage), so a crash loop cannot spin', () => {
    const first = setup()
    first.recovery.note(MSG, 'X')
    first.advance(100)
    first.recovery.note(MSG, 'X')
    expect(first.reload).toHaveBeenCalledTimes(1)

    // Same tab session after the reload: budget already spent.
    const second = setup(first.storage.dump())
    second.recovery.note(MSG, 'X')
    second.advance(100)
    expect(second.recovery.note(MSG, 'X')).toBe(false)
    expect(second.reload).not.toHaveBeenCalled()
  })

  it('still works when storage is unavailable', () => {
    let clock = 0
    const reload = vi.fn()
    const recovery = createRenderCrashRecovery({
      reload,
      storage: undefined,
      now: () => clock,
    })
    // `undefined` falls back to sessionStorage, which jsdom provides; the point
    // here is only that a throwing storage must not break the decision.
    const throwing = createRenderCrashRecovery({
      reload,
      storage: {
        getItem: () => { throw new Error('denied') },
        setItem: () => { throw new Error('denied') },
      },
      now: () => clock,
      reloadDelayMs: 0,
    })
    throwing.note(MSG, 'X')
    clock += 100
    expect(throwing.note(MSG, 'X')).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)
    void recovery
  })
})

/**
 * main.ts mounts the app, so its error handler has no importable surface. Read
 * the source instead (same approach as mainSingleTabGate.test.ts): the two
 * things that must not silently disappear are the component name in the log
 * (without it a minified stack is unactionable) and the "only a component
 * update counts" gate (a one-off render error must not trigger a reload).
 */
function readMain(): string {
  for (const p of [resolve(process.cwd(), 'src/main.ts'), resolve(process.cwd(), 'web/src/main.ts')]) {
    try {
      return readFileSync(p, 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`could not locate main.ts from cwd=${process.cwd()}`)
}

describe('main.ts error handler wiring', () => {
  const src = readMain()

  it('names the component that threw', () => {
    expect(src).toContain('componentName(')
    expect(src).toContain('${name ? ` <${name}>` : \'\'}')
  })

  it('feeds only component-update failures to the recovery', () => {
    const gate = src.indexOf("if (info === 'component update')")
    expect(gate).toBeGreaterThan(-1)
    expect(src.indexOf('renderCrashRecovery.note(')).toBeGreaterThan(gate)
  })

  it('logs the ancestor chain, so a report says which subtree got stuck', () => {
    expect(src).toContain('componentChain(')
    expect(src).toContain('update failed in ${chain')
  })

  it('installs the handler before mounting', () => {
    expect(src.indexOf('app.config.errorHandler')).toBeLessThan(src.indexOf("app.mount('#app')"))
  })
})

describe('renderCrashRecovery — known corruption signature', () => {
  it('reloads on the FIRST emitsOptions error (the tree is already corrupt)', () => {
    const { recovery, reload } = setup()
    expect(recovery.note("Cannot read properties of null (reading 'emitsOptions')", 'App')).toBe(true)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('still respects the reload budget for the immediate path', () => {
    const { recovery, reload, advance, storage } = setup()
    recovery.note("Cannot read properties of null (reading 'emitsOptions')", 'App')
    expect(reload).toHaveBeenCalledTimes(1)
    advance(61_000)
    recovery.note("Cannot read properties of null (reading 'emitsOptions')", 'App')
    expect(reload).toHaveBeenCalledTimes(2)
    advance(61_000)
    expect(recovery.note("Cannot read properties of null (reading 'emitsOptions')", 'App')).toBe(false)
    expect(reload).toHaveBeenCalledTimes(2)
    void storage
  })
})
