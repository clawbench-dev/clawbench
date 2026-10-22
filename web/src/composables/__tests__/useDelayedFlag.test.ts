import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { useDelayedFlag } from '@/composables/useDelayedFlag'

describe('useDelayedFlag', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  /** Run the composable inside a real effect scope so onScopeDispose works. */
  function setup(initial: boolean, delayMs?: number) {
    const scope = effectScope()
    const source = ref(initial)
    const flag = scope.run(() => useDelayedFlag(source, delayMs))!
    return { source, flag, stop: () => scope.stop() }
  }

  it('stays false while the source flips on and off faster than the delay', async () => {
    const { source, flag, stop } = setup(false, 120)
    source.value = true
    await nextTick()
    // A local aggregate often answers this fast; showing the skeleton for it
    // would flash grey blocks on every chip click.
    vi.advanceTimersByTime(60)
    source.value = false
    await nextTick()
    vi.advanceTimersByTime(500)
    expect(flag.value).toBe(false)
    stop()
  })

  it('turns true once the source has been on for the full delay', async () => {
    const { source, flag, stop } = setup(false, 120)
    source.value = true
    await nextTick()
    vi.advanceTimersByTime(119)
    expect(flag.value).toBe(false)
    vi.advanceTimersByTime(1)
    expect(flag.value).toBe(true)
    stop()
  })

  it('clears immediately when the source goes false (no trailing skeleton)', async () => {
    const { source, flag, stop } = setup(false, 120)
    source.value = true
    await nextTick()
    vi.advanceTimersByTime(200)
    expect(flag.value).toBe(true)

    source.value = false
    await nextTick()
    // Must not wait for the timer: the data has landed, so the placeholder is
    // already wrong.
    expect(flag.value).toBe(false)
    stop()
  })

  it('honours an already-true source on setup', async () => {
    const { flag, stop } = setup(true, 50)
    vi.advanceTimersByTime(50)
    await nextTick()
    expect(flag.value).toBe(true)
    stop()
  })

  it('does not fire after the scope is disposed', async () => {
    const { source, flag, stop } = setup(false, 120)
    source.value = true
    await nextTick()
    stop()
    vi.advanceTimersByTime(500)
    await nextTick()
    expect(flag.value).toBe(false)
  })
})
