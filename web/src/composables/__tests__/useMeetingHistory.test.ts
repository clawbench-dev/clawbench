import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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

describe('useMeetingHistory regeneration', () => {
  it('asks the plan endpoint for the exact target and downstream flag', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({ target: 'minutes', steps: [], warnings: [], blocked: null }),
    }))
    vi.stubGlobal('fetch', fetchMock)

    const plan = await useMeetingHistory().regenPlan('tok', 'minutes', true)
    expect(plan?.target).toBe('minutes')
    const url = String(fetchMock.mock.calls[0][0])
    expect(url).toContain('/api/live-share/notes/regenerate/plan?')
    expect(url).toContain('target=minutes')
    expect(url).toContain('downstream=1')
  })

  it('returns the updated session on success', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({ session: { token: 'tok' } }),
    })))

    const result = await useMeetingHistory().regenerate('tok', 'transcript', false)
    expect(result.ok).toBe(true)
    expect(result.session?.token).toBe('tok')
  })

  it('returns the blocked plan on 409 instead of pretending it started', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false,
      status: 409,
      json: async () => ({ error: 'no source', plan: { target: 'minutes', steps: [], warnings: [], blocked: { code: 'no_source', message: 'no source' } } }),
    })))

    const result = await useMeetingHistory().regenerate('tok', 'minutes', false)
    expect(result.ok).toBe(false)
    expect(result.plan?.blocked?.code).toBe('no_source')
  })

  it('reports cancellation success from the response status', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ ok: true }) })))
    await expect(useMeetingHistory().cancelRegeneration('tok')).resolves.toBe(true)

    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 409, json: async () => ({}) })))
    await expect(useMeetingHistory().cancelRegeneration('tok')).resolves.toBe(false)
  })
})

/**
 * Import uploads through XHR (fetch cannot report upload progress). The fake
 * below drives the three outcomes the panel distinguishes: success, a refusal
 * that carries a message key, and a dropped connection.
 */
describe('useMeetingHistory.importFile', () => {
  class FakeXHR {
    static instances: FakeXHR[] = []
    upload: { onprogress?: (e: ProgressEvent) => void } = {}
    status = 0
    responseText = ''
    onload: (() => void) | null = null
    onerror: (() => void) | null = null
    onabort: (() => void) | null = null
    method = ''
    url = ''
    body: FormData | null = null

    open(method: string, url: string): void {
      this.method = method
      this.url = url
    }

    send(body: FormData): void {
      this.body = body
      FakeXHR.instances.push(this)
    }
  }

  beforeEach(() => {
    FakeXHR.instances = []
    vi.stubGlobal('XMLHttpRequest', FakeXHR)
  })

  function file(name = 'weekly.m4a'): File {
    return new File(['x'], name, { type: 'audio/mp4', lastModified: 1_700_000_000_000 })
  }

  it('posts the file and its own timestamp to the import endpoint', async () => {
    const pending = useMeetingHistory().importFile(file())
    const xhr = FakeXHR.instances[0]
    expect(xhr.method).toBe('POST')
    expect(xhr.url).toBe('/api/live-share/notes/import')
    expect(xhr.body?.get('startedAt')).toBe('1700000000000')
    expect((xhr.body?.get('file') as File).name).toBe('weekly.m4a')

    xhr.status = 200
    xhr.responseText = JSON.stringify({ token: 't' })
    xhr.onload?.()
    await pending
  })

  it('reports upload progress and resolves with the new token', async () => {
    const seen: number[] = []
    const pending = useMeetingHistory().importFile(file(), (pct) => seen.push(pct))
    const xhr = FakeXHR.instances[0]

    xhr.upload.onprogress?.({ lengthComputable: true, loaded: 5, total: 20 } as ProgressEvent)
    expect(seen).toEqual([25])

    xhr.status = 200
    xhr.responseText = JSON.stringify({ token: 'abc' })
    xhr.onload?.()
    await expect(pending).resolves.toEqual({ ok: true, token: 'abc' })
    expect(seen.at(-1)).toBe(100)
  })

  it('surfaces the server message key when the import is refused', async () => {
    const pending = useMeetingHistory().importFile(file())
    const xhr = FakeXHR.instances[0]
    xhr.status = 503
    xhr.responseText = JSON.stringify({ error: 'no ffmpeg', msgKey: 'MeetingImportUnavailable' })
    xhr.onload?.()

    await expect(pending).resolves.toEqual({ ok: false, reason: 'MeetingImportUnavailable' })
  })

  it('treats a transport failure as a network error', async () => {
    const pending = useMeetingHistory().importFile(file())
    FakeXHR.instances[0].onerror?.()
    await expect(pending).resolves.toEqual({ ok: false, reason: 'network' })
  })
})

describe('useMeetingHistory.load', () => {
  it('re-runs a load requested while one is still in flight', async () => {
    let release: (value: unknown) => void = () => {}
    const first = new Promise((resolve) => { release = resolve })
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => first)
      .mockImplementationOnce(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ sessions: [{ token: 'imported' }] }),
      }))
    vi.stubGlobal('fetch', fetchMock)

    const history = useMeetingHistory()
    const running = history.load()
    // A refresh clicked during a poll — or an import finishing mid-poll — used
    // to be dropped entirely, leaving the new record off screen.
    const queued = history.load()
    release({ ok: true, status: 200, json: async () => ({ sessions: [] }) })
    await Promise.all([running, queued])

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(history.sessions.value[0]?.token).toBe('imported')
  })
})
