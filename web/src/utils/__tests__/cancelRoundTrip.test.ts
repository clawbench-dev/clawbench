import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { markCancelRequested, reportCancelRoundTrip, resetCancelRoundTrip } from '@/utils/cancelRoundTrip'
import { appLog } from '@/utils/appLog'

describe('cancelRoundTrip', () => {
  let logSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.useFakeTimers()
    resetCancelRoundTrip()
    logSpy = vi.spyOn(appLog, 'i').mockImplementation(() => {})
  })

  afterEach(() => {
    logSpy.mockRestore()
    vi.useRealTimers()
    resetCancelRoundTrip()
  })

  it('reports the elapsed time from click to terminal event', () => {
    markCancelRequested()
    vi.advanceTimersByTime(2500)

    reportCancelRoundTrip('cancelled')

    expect(logSpy).toHaveBeenCalledTimes(1)
    const message = String(logSpy.mock.calls[0][1])
    expect(message).toContain('cancel round-trip: 2500ms')
    expect(message).toContain('terminal=cancelled')
  })

  it('does not report when no cancel was requested', () => {
    // A normal completion with no pending cancel must not emit a bogus 0ms
    // round-trip — the spinner was never waiting on a cancel.
    reportCancelRoundTrip('done')

    expect(logSpy).not.toHaveBeenCalled()
  })

  it('reports only once when both terminal paths fire', () => {
    // chat_stream 'cancelled' and the session_update safety net can both clear
    // the spinner; whichever arrives first must report, the second is a no-op.
    markCancelRequested()
    vi.advanceTimersByTime(1000)

    reportCancelRoundTrip('cancelled')
    vi.advanceTimersByTime(500)
    reportCancelRoundTrip('session_update:cancelled')

    expect(logSpy).toHaveBeenCalledTimes(1)
    expect(String(logSpy.mock.calls[0][1])).toContain('1000ms')
  })

  it('measures from the latest click when the user cancels twice', () => {
    markCancelRequested()
    vi.advanceTimersByTime(3000)
    // Second cancel re-arms the timer rather than reporting the first click.
    markCancelRequested()
    vi.advanceTimersByTime(400)

    reportCancelRoundTrip('cancelled')

    expect(logSpy).toHaveBeenCalledTimes(1)
    expect(String(logSpy.mock.calls[0][1])).toContain('cancel round-trip: 400ms')
  })

  it('drops a stale pending click instead of reporting it', () => {
    // If the terminal event never arrives, a later unrelated cleanup would
    // otherwise report a multi-minute "cancel" that never happened.
    markCancelRequested()
    vi.advanceTimersByTime(121_000)

    reportCancelRoundTrip('session_update:completed')

    expect(logSpy).not.toHaveBeenCalled()
  })

  it('clears the pending measurement after reporting', () => {
    markCancelRequested()
    vi.advanceTimersByTime(100)
    reportCancelRoundTrip('done')

    logSpy.mockClear()
    vi.advanceTimersByTime(100)
    reportCancelRoundTrip('done')

    expect(logSpy).not.toHaveBeenCalled()
  })

  it('reports the boundary duration at exactly the max', () => {
    markCancelRequested()
    vi.advanceTimersByTime(120_000)

    reportCancelRoundTrip('done')

    expect(logSpy).toHaveBeenCalledTimes(1)
    expect(String(logSpy.mock.calls[0][1])).toContain('120000ms')
  })

  it('resetCancelRoundTrip discards a pending measurement', () => {
    markCancelRequested()
    vi.advanceTimersByTime(500)
    resetCancelRoundTrip()

    reportCancelRoundTrip('cancelled')

    expect(logSpy).not.toHaveBeenCalled()
  })
})
