import { describe, expect, it, vi } from 'vitest'
import {
  RECORDING_MIME_CANDIDATES,
  RecordingChunkQueue,
  pickRecordingMime,
} from '../meetingRecording'

function chunk(seq: number, size = 4): { seq: number; blob: Blob } {
  return { seq, blob: new Blob([new Uint8Array(size)]) }
}

describe('pickRecordingMime', () => {
  it('takes the first codec the engine reports', () => {
    const supported = (t: string) => t === 'video/webm;codecs=vp8,opus'
    expect(pickRecordingMime(supported)).toBe('video/webm;codecs=vp8,opus')
  })

  it('prefers the candidates in order', () => {
    const supported = (t: string) => RECORDING_MIME_CANDIDATES.includes(t as never)
    expect(pickRecordingMime(supported)).toBe(RECORDING_MIME_CANDIDATES[0])
  })

  it('returns an empty string when nothing is reported, letting the engine choose', () => {
    expect(pickRecordingMime(() => false)).toBe('')
    // No isTypeSupported at all (some embedded engines).
    expect(pickRecordingMime(undefined)).toBe('')
  })
})

describe('RecordingChunkQueue', () => {
  it('uploads chunks one at a time, in order', async () => {
    const sent: number[] = []
    let inFlight = 0
    let maxInFlight = 0
    const queue = new RecordingChunkQueue({
      send: async (c) => {
        inFlight++
        maxInFlight = Math.max(maxInFlight, inFlight)
        await Promise.resolve()
        sent.push(c.seq)
        inFlight--
        return true
      },
      onFatal: vi.fn(),
    })

    queue.enqueue(chunk(0))
    queue.enqueue(chunk(1))
    queue.enqueue(chunk(2))
    await queue.finish()

    expect(sent).toEqual([0, 1, 2])
    // Two concurrent appends would interleave the bytes of the same file.
    expect(maxInFlight).toBe(1)
    expect(queue.depth).toBe(0)
  })

  it('retries a retryable failure instead of skipping the chunk', async () => {
    let attempts = 0
    const sent: number[] = []
    const queue = new RecordingChunkQueue({
      retryDelays: [0, 0, 0],
      send: async (c) => {
        attempts++
        if (attempts < 3) return false
        sent.push(c.seq)
        return true
      },
      onFatal: vi.fn(),
    })

    queue.enqueue(chunk(0))
    await queue.finish()

    expect(attempts).toBe(3)
    expect(sent).toEqual([0])
  })

  it('stops the recording when a chunk cannot be retried', async () => {
    const onFatal = vi.fn()
    const queue = new RecordingChunkQueue({
      send: async () => {
        throw new Error('409 gap')
      },
      onFatal,
    })

    queue.enqueue(chunk(0))
    queue.enqueue(chunk(1))
    await queue.finish()

    expect(onFatal).toHaveBeenCalledWith('failed')
    // The remaining chunk is dropped: without the missing one the file cannot
    // be decoded anyway.
    expect(queue.depth).toBe(0)
  })

  it('fails loudly when the buffer fills instead of growing without bound', async () => {
    const onFatal = vi.fn()
    let release: (ok: boolean) => void = () => undefined
    const queue = new RecordingChunkQueue({
      // Held open: the queue is "offline" for the rest of the test.
      send: () => new Promise<boolean>((resolve) => { release = resolve }),
      onFatal,
      maxPendingBytes: 10,
    })

    queue.enqueue(chunk(0, 6))
    queue.enqueue(chunk(1, 6))

    expect(onFatal).toHaveBeenCalledWith('overflow')
    expect(queue.depth).toBe(0)
    // Let the in-flight upload settle so the test leaves no pending promise.
    release(false)
    for (let i = 0; i < 4; i++) await Promise.resolve()
  })
})
