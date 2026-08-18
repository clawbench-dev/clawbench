import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { StreamFrameScheduler } from './streamFrameScheduler'

describe('StreamFrameScheduler', () => {
  let rafSpy: ReturnType<typeof vi.spyOn>
  let cancelRafSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    rafSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb: FrameRequestCallback) => {
      // Execute synchronously for test determinism
      cb(0)
      return 1
    })
    cancelRafSpy = vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(() => {})
  })

  afterEach(() => {
    rafSpy.mockRestore()
    cancelRafSpy.mockRestore()
  })

  it('executes scheduled callback via rAF', () => {
    const scheduler = new StreamFrameScheduler()
    const fn = vi.fn()
    scheduler.schedule('a', fn)
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('replaces callback when same name is rescheduled before flush', () => {
    const scheduler = new StreamFrameScheduler()
    const fn1 = vi.fn()
    const fn2 = vi.fn()

    // Use deferred mock so callbacks don't fire immediately
    rafSpy.mockRestore()
    let capturedCb: FrameRequestCallback | null = null
    rafSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb: FrameRequestCallback) => {
      capturedCb = cb
      return 1
    })

    scheduler.schedule('a', fn1)
    expect(fn1).not.toHaveBeenCalled()

    // Replace 'a' before flush
    scheduler.schedule('a', fn2)
    expect(fn1).not.toHaveBeenCalled()
    expect(fn2).not.toHaveBeenCalled()

    // Flush — only fn2 should run (fn1 was replaced)
    if (capturedCb) (capturedCb as FrameRequestCallback)(0)
    expect(fn1).not.toHaveBeenCalled()
    expect(fn2).toHaveBeenCalledTimes(1)
  })

  it('runs multiple named callbacks in the same rAF frame', () => {
    const order: string[] = []
    const scheduler = new StreamFrameScheduler()
    const fn1 = vi.fn(() => order.push('a'))
    const fn2 = vi.fn(() => order.push('b'))

    // Mock rAF to defer execution so both can be queued
    rafSpy.mockRestore()
    let capturedCb: FrameRequestCallback | null = null
    rafSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb: FrameRequestCallback) => {
      capturedCb = cb
      return 1
    })

    scheduler.schedule('a', fn1)
    scheduler.schedule('b', fn2)

    // Both should be pending but not yet executed
    expect(fn1).not.toHaveBeenCalled()
    expect(fn2).not.toHaveBeenCalled()

    // Flush the rAF
    if (capturedCb) (capturedCb as FrameRequestCallback)(0)
    expect(fn1).toHaveBeenCalledTimes(1)
    expect(fn2).toHaveBeenCalledTimes(1)
    expect(order).toEqual(['a', 'b'])
  })

  it('cancel() removes a named callback and cancels rAF when queue is empty', () => {
    const scheduler = new StreamFrameScheduler()
    const fn = vi.fn()

    // Mock rAF to NOT execute immediately so we can test cancel
    rafSpy.mockRestore()
    let rafCallback: FrameRequestCallback | null = null
    rafSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb: FrameRequestCallback) => {
      rafCallback = cb
      return 42
    })

    scheduler.schedule('a', fn)
    expect(scheduler.has('a')).toBe(true)
    expect(scheduler.pending).toBe(true)

    scheduler.cancel('a')
    expect(scheduler.has('a')).toBe(false)
    expect(scheduler.pending).toBe(false)
    expect(cancelRafSpy).toHaveBeenCalledWith(42)

    // Simulate the rAF firing (shouldn't happen, but verify no-op)
    if (rafCallback) (rafCallback as FrameRequestCallback)(0)
    expect(fn).not.toHaveBeenCalled()
  })

  it('cancel() does not cancel rAF if other callbacks remain', () => {
    const scheduler = new StreamFrameScheduler()
    const fnA = vi.fn()
    const fnB = vi.fn()

    rafSpy.mockRestore()
    rafSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation(() => 42)

    scheduler.schedule('a', fnA)
    scheduler.schedule('b', fnB)
    scheduler.cancel('a')

    expect(cancelRafSpy).not.toHaveBeenCalled()
    expect(scheduler.has('b')).toBe(true)
  })

  it('cancelAll() clears everything and cancels rAF', () => {
    const scheduler = new StreamFrameScheduler()
    const fn = vi.fn()

    rafSpy.mockRestore()
    rafSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation(() => 99)

    scheduler.schedule('x', fn)
    scheduler.schedule('y', fn)
    expect(scheduler.pending).toBe(true)

    scheduler.cancelAll()
    expect(scheduler.pending).toBe(false)
    expect(scheduler.has('x')).toBe(false)
    expect(scheduler.has('y')).toBe(false)
    expect(cancelRafSpy).toHaveBeenCalledWith(99)
  })

  it('has() and pending reflect current state', () => {
    const scheduler = new StreamFrameScheduler()
    expect(scheduler.pending).toBe(false)
    expect(scheduler.has('a')).toBe(false)

    // rAF executes synchronously in default mock, so schedule + immediate flush
    scheduler.schedule('a', () => {})
    // After synchronous flush, queue is cleared
    expect(scheduler.pending).toBe(false)
  })
})
