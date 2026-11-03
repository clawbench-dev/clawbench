import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  clipQuoteText,
  isStaleGenerating,
  meetingQuoteLabel,
  useMeetingHistory,
  MEETING_QUOTE_MAX_CHARS,
} from '../useMeetingHistory'

/**
 * Pure helpers plus the quote builder. The builder is the one place that knows
 * a meeting quote needs its text fetched — the drag only carries the token.
 */

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('clipQuoteText', () => {
  it('leaves short text alone', () => {
    expect(clipQuoteText('  hello  ')).toBe('hello')
  })

  it('says how much it cut', () => {
    const text = 'a'.repeat(MEETING_QUOTE_MAX_CHARS + 10)
    const out = clipQuoteText(text)
    expect(out.length).toBeGreaterThan(MEETING_QUOTE_MAX_CHARS)
    expect(out).toContain('truncated')
    expect(out).toContain(String(text.length))
  })
})

describe('isStaleGenerating', () => {
  it('is false while the job is fresh', () => {
    expect(isStaleGenerating({ status: 'generating', updatedAt: Date.now() })).toBe(false)
  })

  it('flags a job that has not moved for ten minutes', () => {
    expect(isStaleGenerating({ status: 'generating', updatedAt: Date.now() - 11 * 60 * 1000 })).toBe(true)
  })

  it('ignores a finished record however old', () => {
    expect(isStaleGenerating({ status: 'ready', updatedAt: 0 })).toBe(false)
  })
})

describe('meetingQuoteLabel', () => {
  it('includes the title and the start time', () => {
    const label = meetingQuoteLabel({ title: '周会', startedAt: new Date(2026, 10, 1, 9, 5).getTime() })
    expect(label).toContain('周会')
    expect(label).toContain('11-01 09:05')
  })

  it('falls back to an untitled label without a time', () => {
    expect(meetingQuoteLabel({ title: '', startedAt: 0 })).toContain('Untitled meeting')
  })
})

describe('useMeetingHistory.buildQuote', () => {
  function stubArtifacts(map: Record<string, string | null>) {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      const kind = new URL(url, 'http://x').searchParams.get('kind') ?? ''
      const body = map[kind]
      if (body == null) return { ok: false, status: 404, json: async () => ({}) } as Response
      return { ok: true, status: 200, json: async () => ({ content: body, file: `${kind}.md` }) } as Response
    }))
  }

  it('prefers the minutes, then the revised transcript, then the raw one', async () => {
    stubArtifacts({ minutes: null, revised: 'REVISED', transcript: 'RAW' })
    const quote = await useMeetingHistory().buildQuote({ token: 't1', title: '周会', startedAt: 0 })

    expect(quote?.text).toBe('REVISED')
    expect(quote?.meetingToken).toBe('t1')
    expect(quote?.language).toBe('meeting')
  })

  it('returns null when the meeting has nothing to quote', async () => {
    stubArtifacts({})
    const quote = await useMeetingHistory().buildQuote({ token: 't1', title: '周会', startedAt: 0 })
    expect(quote).toBeNull()
  })
})
