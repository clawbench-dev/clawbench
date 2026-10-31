import { afterEach, describe, expect, it, vi } from 'vitest'
import { acquireMic, classifyMicError, describeMicError } from '../micCapture'

describe('classifyMicError', () => {
  it('treats a refused permission as "denied"', () => {
    expect(classifyMicError({ name: 'NotAllowedError' })).toBe('denied')
  })

  it('treats every other failure as "unavailable"', () => {
    // Telling a user with a blocked device to "grant permission" sends them to
    // the wrong settings page, so only NotAllowedError may map to "denied".
    for (const name of ['NotReadableError', 'NotFoundError', 'OverconstrainedError', 'SecurityError']) {
      expect(classifyMicError({ name })).toBe('unavailable')
    }
    expect(classifyMicError(new Error('boom'))).toBe('unavailable')
    expect(classifyMicError(null)).toBe('unavailable')
  })
})

describe('describeMicError', () => {
  it('keeps the name and message that JSON serialisation drops', () => {
    expect(describeMicError({ name: 'NotReadableError', message: 'Could not start audio source' }))
      .toBe('name=NotReadableError message=Could not start audio source')
  })

  it('falls back for values that are not errors', () => {
    expect(describeMicError('nope')).toBe('name=unknown message=nope')
  })
})

describe('acquireMic', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('retries once when the device refuses to start', async () => {
    vi.useFakeTimers()
    const stream = { id: 'stream' }
    const getUserMedia = vi.fn()
      .mockRejectedValueOnce({ name: 'NotReadableError' })
      .mockResolvedValueOnce(stream)
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia } })

    const pending = acquireMic()
    await vi.advanceTimersByTimeAsync(400)

    await expect(pending).resolves.toBe(stream)
    expect(getUserMedia).toHaveBeenCalledTimes(2)
  })

  it('does not retry a refused permission', async () => {
    const getUserMedia = vi.fn().mockRejectedValue({ name: 'NotAllowedError' })
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia } })

    await expect(acquireMic()).rejects.toEqual({ name: 'NotAllowedError' })
    expect(getUserMedia).toHaveBeenCalledTimes(1)
  })

  it('gives up after the retry instead of looping', async () => {
    vi.useFakeTimers()
    const getUserMedia = vi.fn().mockRejectedValue({ name: 'NotReadableError' })
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia } })

    const pending = acquireMic()
    const assertion = expect(pending).rejects.toEqual({ name: 'NotReadableError' })
    await vi.advanceTimersByTimeAsync(400)
    await assertion

    expect(getUserMedia).toHaveBeenCalledTimes(2)
  })

  it('passes the caller constraints through', async () => {
    const getUserMedia = vi.fn().mockResolvedValue({ id: 'stream' })
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia } })

    const constraints = { audio: { echoCancellation: true } }
    await acquireMic(constraints)
    expect(getUserMedia).toHaveBeenCalledWith(constraints)
  })
})
