import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

/**
 * markSessionRead is the ONE place that issues `POST /api/ai/chat/read`.
 *
 * It has two callers with very different shapes — `useChatSession.switchSession`
 * (same project, cookie project is the owner) and App.vue's cross-project jump
 * (which does NOT go through switchSession, so it must mark read itself) — and
 * the request has to be identical for both. These tests pin the request shape
 * and the two failure modes, which the composable-level tests cannot see: they
 * mock fetch at a higher level and would keep passing if this helper stopped
 * sending project_path or started throwing on a dropped connection.
 */

const warn = vi.fn()
vi.mock('@/utils/appLog', () => ({
  appLog: { w: (...args: any[]) => warn(...args), d: vi.fn(), i: vi.fn(), e: vi.fn() },
}))

import { markSessionRead } from '@/utils/sessionRead'

describe('markSessionRead', () => {
  beforeEach(() => {
    warn.mockClear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('POSTs the session id and no project_path for a same-project switch', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, body: null })
    vi.stubGlobal('fetch', fetchMock)

    await markSessionRead('sess-1')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/ai/chat/read?session_id=sess-1')
    // Same-project switches must NOT send project_path: the backend's fallback
    // to the cookie project is the correct (and only needed) behaviour there.
    expect(url).not.toContain('project_path')
    expect(init).toMatchObject({ method: 'POST' })
  })

  it('forwards project_path so a cross-project open can prove ownership', async () => {
    // Without this the backend falls back to the cookie project and 403s the
    // session belonging to another project — leaving the unread badge stuck,
    // which is the whole point of opening a row from the "other projects" tab.
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, body: null })
    vi.stubGlobal('fetch', fetchMock)

    await markSessionRead('sess-2', '/project/B')

    const [url] = fetchMock.mock.calls[0]
    const params = new URLSearchParams(String(url).split('?')[1])
    expect(params.get('session_id')).toBe('sess-2')
    expect(params.get('project_path')).toBe('/project/B')
  })

  it('does nothing without a session id', async () => {
    // Guard rather than a request for `session_id=`: an empty id would hit the
    // backend's "no session" branch and log a spurious failure.
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, body: null })
    vi.stubGlobal('fetch', fetchMock)

    await markSessionRead('')

    expect(fetchMock).not.toHaveBeenCalled()
    expect(warn).not.toHaveBeenCalled()
  })

  it('warns but does not throw on a non-OK response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 403, body: null })
    vi.stubGlobal('fetch', fetchMock)

    await expect(markSessionRead('sess-3')).resolves.toBeUndefined()

    expect(warn).toHaveBeenCalledTimes(1)
    expect(String(warn.mock.calls[0][1])).toContain('403')
  })

  it('swallows a transport failure instead of failing the caller', async () => {
    // Read state is not a precondition for anything the caller does next:
    // opening a session must not surface a dropped connection as an error.
    const fetchMock = vi.fn().mockRejectedValue(new Error('Failed to fetch'))
    vi.stubGlobal('fetch', fetchMock)

    await expect(markSessionRead('sess-4')).resolves.toBeUndefined()

    expect(warn).toHaveBeenCalledTimes(1)
  })
  it('cancels the response body so the request actually finishes', async () => {
    // WHY THIS IS NOT A DETAIL: a fetch whose body is never consumed keeps the
    // response stream open, so the browser still counts the request as in
    // flight — `requestfinished` never fires and `networkidle` never settles.
    // The cold-start read (sessionReadGate) made EVERY page load hold one such
    // request, which timed out five Playwright specs at
    // `waitForLoadState('networkidle')` after a reload. A mock that returns a
    // body-less object (as the other cases here do) cannot see the difference,
    // so this case asserts the call itself.
    const cancel = vi.fn().mockResolvedValue(undefined)
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, body: { cancel } })
    vi.stubGlobal('fetch', fetchMock)

    await markSessionRead('sess-5')

    expect(cancel, 'the response body must be cancelled or the request never finishes').toHaveBeenCalledTimes(1)
  })

  it('tolerates a body that cannot be cancelled', async () => {
    // Response.body is null for a body-less response, and a real cancel() can
    // reject if the stream is already closed. Neither may turn a read into an
    // error — the badge work already succeeded by this point.
    const cancel = vi.fn().mockRejectedValue(new Error('stream already closed'))
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500, body: { cancel } })
    vi.stubGlobal('fetch', fetchMock)

    await expect(markSessionRead('sess-6')).resolves.toBeUndefined()
    expect(cancel).toHaveBeenCalledTimes(1)
    // The non-OK warning still fires; a cancel failure must not suppress it.
    expect(warn).toHaveBeenCalledTimes(1)
  })
})
