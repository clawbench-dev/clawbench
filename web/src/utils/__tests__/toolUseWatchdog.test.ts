import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ToolUseWatchdog } from '@/utils/toolUseWatchdog'

describe('ToolUseWatchdog', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('fires onTimeout when the tool goes silent for timeoutMs', () => {
    const watchdog = new ToolUseWatchdog()
    const onTimeout = vi.fn()
    watchdog.start('tool-1', 30000, onTimeout)

    vi.advanceTimersByTime(29999)
    expect(onTimeout).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1)
    expect(onTimeout).toHaveBeenCalledTimes(1)
  })

  it('resets the timer on a subsequent start (progress event keeps the tool alive)', () => {
    const watchdog = new ToolUseWatchdog()
    const onTimeout = vi.fn()

    // First sight of the tool call
    watchdog.start('tool-1', 30000, onTimeout)

    // Progress at 20s — must reset the 30s countdown, not keep the original one
    vi.advanceTimersByTime(20000)
    watchdog.start('tool-1', 30000, onTimeout)

    // Only 10s after the progress event → original timer would have fired,
    // the reset timer must NOT have fired yet
    vi.advanceTimersByTime(10000)
    expect(onTimeout).not.toHaveBeenCalled()

    // Past the reset deadline → fires now
    vi.advanceTimersByTime(20001)
    expect(onTimeout).toHaveBeenCalledTimes(1)
  })

  it('fires only once even after timeout (timer is cleaned up)', () => {
    const watchdog = new ToolUseWatchdog()
    const onTimeout = vi.fn()
    watchdog.start('tool-1', 30000, onTimeout)

    vi.advanceTimersByTime(60000)
    expect(onTimeout).toHaveBeenCalledTimes(1)
  })

  it('does not fire after clear()', () => {
    const watchdog = new ToolUseWatchdog()
    const onTimeout = vi.fn()
    watchdog.start('tool-1', 30000, onTimeout)

    watchdog.clear('tool-1')
    vi.advanceTimersByTime(60000)
    expect(onTimeout).not.toHaveBeenCalled()
  })

  it('tracks multiple tool calls independently', () => {
    const watchdog = new ToolUseWatchdog()
    const onTimeout1 = vi.fn()
    const onTimeout2 = vi.fn()

    watchdog.start('tool-1', 10000, onTimeout1)
    watchdog.start('tool-2', 10000, onTimeout2)

    vi.advanceTimersByTime(10001)
    expect(onTimeout1).toHaveBeenCalledTimes(1)
    expect(onTimeout2).toHaveBeenCalledTimes(1)
  })

  it('clear() only cancels the given tool, not others', () => {
    const watchdog = new ToolUseWatchdog()
    const onTimeout1 = vi.fn()
    const onTimeout2 = vi.fn()

    watchdog.start('tool-1', 10000, onTimeout1)
    watchdog.start('tool-2', 10000, onTimeout2)

    watchdog.clear('tool-1')
    vi.advanceTimersByTime(10001)
    expect(onTimeout1).not.toHaveBeenCalled()
    expect(onTimeout2).toHaveBeenCalledTimes(1)
  })

  it('clearAll() cancels every pending timer', () => {
    const watchdog = new ToolUseWatchdog()
    const onTimeout1 = vi.fn()
    const onTimeout2 = vi.fn()

    watchdog.start('tool-1', 10000, onTimeout1)
    watchdog.start('tool-2', 10000, onTimeout2)

    watchdog.clearAll()
    vi.advanceTimersByTime(60000)
    expect(onTimeout1).not.toHaveBeenCalled()
    expect(onTimeout2).not.toHaveBeenCalled()
  })
})
