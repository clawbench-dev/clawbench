import { describe, expect, it, vi } from 'vitest'
import {
  SHOT_MAX_PER_MEETING,
  SHOT_MIN_INTERVAL_MS,
  ShotQueue,
  ShotScheduler,
  dhash,
  hashDistance,
  newClientShotId,
  shotRetryable,
  type ShotJob,
  type ShotSendResult,
} from '../shotCapture'

function job(id: string, reason: ShotJob['reason'] = 'file'): ShotJob {
  return {
    clientShotId: id,
    at: 1_700_000_000_000,
    reason,
    ref: '',
    width: 1280,
    height: 720,
    blob: new Blob(['x']),
  }
}

function flatGray(value: number): number[] {
  return new Array(9 * 8).fill(value)
}

/** A real hash is 64 bits; short fake strings make every pair look identical. */
function h64(bit: '0' | '1'): string {
  return bit.repeat(64)
}

describe('dhash', () => {
  it('is stable for a flat picture and sensitive to a layout change', () => {
    const flat = dhash(flatGray(120))
    expect(flat).toHaveLength(64)
    expect(hashDistance(flat, dhash(flatGray(120)))).toBe(0)

    // A checkerboard alternates above/below its neighbour everywhere, which is
    // the shape a layout change takes in this hash.
    const checker = flatGray(120).map((_, i) => ((i + Math.floor(i / 9)) % 2 ? 250 : 10))
    expect(hashDistance(flat, dhash(checker))).toBeGreaterThan(8)
  })

  it('does not depend on overall brightness', () => {
    const dark = flatGray(10)
    for (let i = 0; i < 30; i++) dark[i] = 40
    const bright = dark.map((v) => Math.min(255, v * 4))
    expect(hashDistance(dhash(dark), dhash(bright))).toBe(0)
  })
})

describe('ShotScheduler', () => {
  const base = 1_700_000_000_000

  it('keeps the first frame and rejects an identical one at the same instant', () => {
    const s = new ShotScheduler()
    expect(s.shouldKeep('file', h64('0'), base).keep).toBe(true)
    s.noteKept('file', h64('0'), base)

    expect(s.shouldKeep('change', h64('1'), base + 500)).toEqual({ keep: false, reject: 'interval' })
  })

  it('rejects the same picture after the interval but accepts a different one', () => {
    const s = new ShotScheduler()
    s.noteKept('file', h64('0'), base)

    expect(s.shouldKeep('change', h64('0'), base + SHOT_MIN_INTERVAL_MS + 1)).toEqual({
      keep: false,
      reject: 'duplicate',
    })
    expect(s.shouldKeep('change', h64('1'), base + SHOT_MIN_INTERVAL_MS + 1).keep).toBe(true)
  })

  it('lets a manual capture through every gate', () => {
    const s = new ShotScheduler(0, 0)
    s.noteKept('file', h64('0'), base)
    expect(s.shouldKeep('manual', h64('0'), base + 1).keep).toBe(true)
  })

  it('stops at the per-meeting cap', () => {
    const s = new ShotScheduler(1000, 2)
    s.noteKept('file', h64('0'), base)
    s.noteKept('file', h64('1'), base + 60_000)
    expect(s.shouldKeep('file', h64('0'), base + 120_000)).toEqual({ keep: false, reject: 'quota' })
  })

  it('counts the hourly window, not the whole meeting', () => {
    const s = new ShotScheduler(2, SHOT_MAX_PER_MEETING)
    s.noteKept('file', h64('0'), base)
    s.noteKept('file', h64('1'), base + 10_000)
    // Low-value trigger over the hourly quota.
    expect(s.shouldKeep('change', h64('0'), base + 20_000)).toEqual({ keep: false, reject: 'quota' })
    // A file open is still worth keeping.
    expect(s.shouldKeep('file', h64('0'), base + 20_000).keep).toBe(true)
    // An hour later the window has moved.
    expect(s.shouldKeep('change', h64('0'), base + 3_600_001).keep).toBe(true)
  })
})

describe('shotRetryable', () => {
  it('retries transport, server and rate-limit failures only', () => {
    expect(shotRetryable(0)).toBe(true)
    expect(shotRetryable(429)).toBe(true)
    expect(shotRetryable(502)).toBe(true)
    expect(shotRetryable(400)).toBe(false)
    expect(shotRetryable(413)).toBe(false)
    expect(shotRetryable(404)).toBe(false)
  })
})

describe('ShotQueue', () => {
  it('uploads one frame at a time, in order', async () => {
    const sent: string[] = []
    const q = new ShotQueue({
      send: async (j) => {
        sent.push(j.clientShotId)
        return { ok: true }
      },
      onLost: () => undefined,
      sleep: async () => undefined,
    })
    q.enqueue(job('a'))
    q.enqueue(job('b'))
    q.enqueue(job('c'))
    await q.flush(1000)
    expect(sent).toEqual(['a', 'b', 'c'])
  })

  it('retries a 5xx and reports success once', async () => {
    let calls = 0
    const lost = vi.fn()
    const q = new ShotQueue({
      send: async (): Promise<ShotSendResult> => {
        calls++
        return calls === 1 ? { ok: false, status: 503 } : { ok: true }
      },
      onLost: lost,
      sleep: async () => undefined,
    })
    q.enqueue(job('a'))
    await q.flush(1000)
    expect(calls).toBe(2)
    expect(lost).not.toHaveBeenCalled()
  })

  it('does not retry a 4xx and reports the frame lost', async () => {
    let calls = 0
    const lost = vi.fn()
    const q = new ShotQueue({
      send: async (): Promise<ShotSendResult> => {
        calls++
        return { ok: false, status: 400 }
      },
      onLost: lost,
      sleep: async () => undefined,
    })
    q.enqueue(job('a'))
    await q.flush(1000)
    expect(calls).toBe(1)
    expect(lost).toHaveBeenCalledTimes(1)
  })

  it('drops the oldest low-value frame when the queue overflows', async () => {
    let release: (() => void) | null = null
    const sent: string[] = []
    const lost = vi.fn()
    const q = new ShotQueue({
      send: async (j) => {
        sent.push(j.clientShotId)
        if (j.clientShotId === 'in-flight') await new Promise<void>((resolve) => (release = resolve))
        return { ok: true }
      },
      onLost: lost,
      sleep: async () => undefined,
      maxPending: 2,
    })
    q.enqueue(job('in-flight'))
    q.enqueue(job('low-1', 'change'))
    q.enqueue(job('low-2', 'change'))
    // Overflow: the oldest non-manual frame goes, the manual one survives.
    q.enqueue(job('manual', 'manual'))
    expect(lost).toHaveBeenCalledTimes(1)

    release?.()
    await q.flush(1000)
    expect(sent).toContain('manual')
    expect(sent).not.toContain('low-1')
  })

  it('gives up at the timeout instead of hanging the caller', async () => {
    const q = new ShotQueue({
      // Resolves late, so the race is exercised without leaving a promise that
      // never settles (which the runner reports as a leak).
      send: () => new Promise<ShotSendResult>((resolve) => setTimeout(() => resolve({ ok: true }), 30)),
      onLost: () => undefined,
      sleep: async () => undefined,
    })
    q.enqueue(job('a'))
    await expect(q.flush(5)).resolves.toBeUndefined()
    await q.flush(100)
  })
})

describe('newClientShotId', () => {
  it('falls back when randomUUID is unavailable (plain http on a LAN)', () => {
    const original = globalThis.crypto
    vi.stubGlobal('crypto', {})
    try {
      const id = newClientShotId()
      expect(id).toMatch(/^[a-z0-9]+-[a-z0-9]+$/)
      expect(newClientShotId()).not.toBe(id)
    } finally {
      vi.stubGlobal('crypto', original)
    }
  })
})
