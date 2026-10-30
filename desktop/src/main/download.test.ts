import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { EventEmitter } from 'node:events'
import { Readable } from 'node:stream'
import { mkdtempSync, readFileSync, rmSync, existsSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'

/**
 * The desktop download must stream progress to the renderer, because the
 * renderer's progress bar is the only in-product feedback (the OS gives none
 * beyond the save dialog). These tests drive the real `fetchToFile` plumbing
 * with a fake `http` client and assert the IPC contract the preload forwards
 * as `clawbench-download-progress`.
 */

const { sentEvents, httpGetImpl, cookieHeader } = vi.hoisted(() => ({
  sentEvents: [] as Array<{ channel: string; payload: unknown }>,
  httpGetImpl: vi.fn(),
  cookieHeader: { value: undefined as string | undefined },
}))

vi.mock('electron', () => ({
  dialog: { showSaveDialog: vi.fn() },
  shell: { showItemInFolder: vi.fn() },
}))

vi.mock('./store', () => ({
  getStore: () => ({ get: () => 'http://localhost:20000' }),
}))

vi.mock('./window', () => ({
  getMainWindow: () => ({
    isDestroyed: () => false,
    webContents: {
      send: (channel: string, payload: unknown) => { sentEvents.push({ channel, payload }) },
    },
  }),
}))

// The download reads the auth cookies from Electron's jar (the main process has
// no document.cookie). Mock the reader so the tests can assert the header is
// actually attached, and so importing ./clientLog does not pull in electron's
// `app`/`session` mocks this file does not provide.
vi.mock('./clientLog', () => ({
  getCookieHeaderForUrl: async () => cookieHeader.value,
}))

vi.mock('node:http', () => ({ default: { get: httpGetImpl } }))
vi.mock('node:https', () => ({ default: { get: httpGetImpl } }))

import { downloadFileByPath, cancelDownload } from './download'
import { dialog, shell } from 'electron'

/**
 * Build a fake ClientRequest/IncomingMessage pair for one download. The
 * response is a real Readable so `res.pipe(fileStream)` behaves exactly as it
 * does against a live socket.
 */
function fakeResponse(opts: {
  contentLength?: number
  chunks?: Buffer[]
  statusCode?: number
  /** Gate the body on this promise so a test can cancel mid-stream. */
  release?: Promise<void>
  /** Error the body after the chunks, mimicking a dropped connection. */
  failAfter?: Error
}) {
  const chunks = opts.chunks ?? [Buffer.alloc(0)]
  async function* body() {
    if (opts.release) await opts.release
    for (const c of chunks) yield c
    if (opts.failAfter) throw opts.failAfter
  }
  const res = Readable.from(body()) as Readable & {
    statusCode: number
    headers: Record<string, string>
  }
  res.statusCode = opts.statusCode ?? 200
  res.headers = opts.contentLength === undefined ? {} : { 'content-length': String(opts.contentLength) }

  const req = new EventEmitter() as EventEmitter & { destroy: () => void }
  // Real ClientRequest.destroy() surfaces as an 'error' event on the request
  // (ECONNRESET / aborted). Without mimicking that, the cancellation path
  // would never settle and the test would pass for the wrong reason.
  req.destroy = vi.fn(() => {
    queueMicrotask(() => req.emit('error', Object.assign(new Error('aborted'), { code: 'ECONNRESET' })))
  })
  httpGetImpl.mockImplementation((_url: string, _opts: unknown, cb: (r: unknown) => void) => {
    queueMicrotask(() => cb(res))
    return req
  })
  return { res, req }
}

describe('desktop download progress', () => {
  let dir: string
  let dest: string

  beforeEach(() => {
    sentEvents.length = 0
    httpGetImpl.mockReset()
    cookieHeader.value = undefined
    dir = mkdtempSync(path.join(tmpdir(), 'clawbench-dl-'))
    dest = path.join(dir, 'out.bin')
    vi.mocked(dialog.showSaveDialog).mockResolvedValue({ canceled: false, filePath: dest })
    vi.mocked(shell.showItemInFolder).mockImplementation(() => {})
  })

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true })
  })

  it('emits progress events tagged with the renderer download id', async () => {
    fakeResponse({ contentLength: 8, chunks: [Buffer.from('abcd'), Buffer.from('efgh')] })

    await downloadFileByPath('data/out.bin', 'out.bin', 42)

    const progress = sentEvents.filter(e => e.channel === 'clawbench-download-progress')
    expect(progress.length).toBeGreaterThan(0)
    for (const e of progress) {
      expect((e.payload as { id: number }).id).toBe(42)
    }
    // The final event carries the full byte count and the declared total.
    const last = progress.at(-1)!.payload as { received: number; total: number }
    expect(last.received).toBe(8)
    expect(last.total).toBe(8)
  })

  it('writes the received bytes to the chosen destination', async () => {
    fakeResponse({ contentLength: 5, chunks: [Buffer.from('hello')] })

    await downloadFileByPath('data/out.bin', 'out.bin', 7)

    expect(readFileSync(dest, 'utf8')).toBe('hello')
    expect(shell.showItemInFolder).toHaveBeenCalledWith(dest)
  })

  it('attaches the session cookies so the authenticated endpoint accepts it', async () => {
    // Without a Cookie header the server answers 401/403 and the download
    // silently writes nothing (the save dialog still appeared first). This is
    // the regression: the main process has no document.cookie, so it must read
    // the jar itself.
    cookieHeader.value = 'clawbench_session=tok; clawbench_project=%2Fproj'
    fakeResponse({ contentLength: 5, chunks: [Buffer.from('hello')] })

    await downloadFileByPath('data/out.bin', 'out.bin', 21)

    expect(httpGetImpl).toHaveBeenCalled()
    const opts = (httpGetImpl.mock.calls[0] as unknown[])[1] as { headers?: Record<string, string> }
    expect(opts.headers?.Cookie).toBe('clawbench_session=tok; clawbench_project=%2Fproj')
  })

  it('omits the Cookie header when not logged in (no cookies in the jar)', async () => {
    cookieHeader.value = undefined
    fakeResponse({ contentLength: 5, chunks: [Buffer.from('hello')] })

    await downloadFileByPath('data/out.bin', 'out.bin', 22)

    const opts = (httpGetImpl.mock.calls[0] as unknown[])[1] as { headers?: Record<string, string> }
    expect(opts.headers).toBeUndefined()
  })

  it('accepts a self-signed certificate (rejectUnauthorized: false)', async () => {
    // A self-hosted server commonly uses a self-signed cert; refusing it would
    // break downloads on exactly those installs. The cookie is the credential,
    // matching tunnel.ts / clientLog.ts / h2Transport.ts.
    fakeResponse({ contentLength: 5, chunks: [Buffer.from('hello')] })

    await downloadFileByPath('data/out.bin', 'out.bin', 23)

    const opts = (httpGetImpl.mock.calls[0] as unknown[])[1] as { rejectUnauthorized?: boolean }
    expect(opts.rejectUnauthorized).toBe(false)
  })

  it('reports total 0 (indeterminate) when the server sends no Content-Length', async () => {
    fakeResponse({ chunks: [Buffer.from('chunked')] })

    await downloadFileByPath('data/out.bin', 'out.bin', 3)

    const progress = sentEvents.filter(e => e.channel === 'clawbench-download-progress')
    expect(progress.length).toBeGreaterThan(0)
    // A streamed response (the archive endpoint) has no length; the renderer
    // renders an indeterminate bar rather than a misleading 0%.
    expect((progress[0].payload as { total: number }).total).toBe(0)
  })

  it('emits a terminal error event and settles the bar on HTTP failure', async () => {
    fakeResponse({ statusCode: 404 })

    await expect(downloadFileByPath('missing.bin', 'missing.bin', 9)).rejects.toThrow('HTTP 404')

    const last = sentEvents.at(-1)!.payload as { id: number; done: boolean; error: boolean }
    expect(last.id).toBe(9)
    expect(last.done).toBe(true)
    expect(last.error).toBe(true)
  })

  it('settles the bar when the user cancels the save dialog', async () => {
    vi.mocked(dialog.showSaveDialog).mockResolvedValue({ canceled: true, filePath: '' })

    await downloadFileByPath('data/out.bin', 'out.bin', 11)

    // Nothing was transferred, but the renderer must not be left showing a bar.
    // Dismissing the dialog is a cancellation, not a failure.
    const last = sentEvents.at(-1)!.payload as { id: number; done: boolean; error: boolean; cancelled: boolean }
    expect(last.id).toBe(11)
    expect(last.done).toBe(true)
    expect(last.cancelled).toBe(true)
    expect(last.error).toBe(false)
    expect(httpGetImpl).not.toHaveBeenCalled()
  })

  it('destroys the in-flight request when cancelled', async () => {
    // The body is gated so the transfer is still in flight when cancel lands;
    // an immediate body would finish (and deregister) first, making this vacuous.
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    const { req } = fakeResponse({ contentLength: 4, chunks: [Buffer.from('data')], release: gate })

    const p = downloadFileByPath('data/out.bin', 'out.bin', 5)
    // The request is only registered after the save dialog resolves, so wait
    // for http.get to have been called before cancelling.
    await vi.waitFor(() => expect(httpGetImpl).toHaveBeenCalled())
    cancelDownload(5)

    expect(req.destroy).toHaveBeenCalled()

    // A cancellation is not a failure: the terminal event must say so, or the
    // renderer would show an error toast for a deliberate action. destroy()
    // surfaces as an async 'error' event, so wait for it to arrive.
    await vi.waitFor(() => {
      const last = sentEvents.at(-1)!.payload as { done: boolean }
      expect(last.done).toBe(true)
    })
    const last = sentEvents.at(-1)!.payload as { done: boolean; error: boolean; cancelled: boolean }
    expect(last.cancelled).toBe(true)
    expect(last.error).toBe(false)

    release()
    await p.catch(() => {})
  })

  it('announces the download before the save dialog resolves', async () => {
    // The renderer hides a bar that saw no event for 3s. Choosing a destination
    // folder routinely takes longer than that, so an event has to exist while
    // the dialog is still open — otherwise the bar dies mid-dialog and every
    // later byte count is dropped as stale ("no progress at all").
    let resolveDialog!: (r: { canceled: boolean; filePath: string }) => void
    vi.mocked(dialog.showSaveDialog).mockReturnValue(
      new Promise((r) => { resolveDialog = r }) as ReturnType<typeof dialog.showSaveDialog>
    )
    fakeResponse({ contentLength: 5, chunks: [Buffer.from('hello')] })

    const p = downloadFileByPath('data/out.bin', 'out.bin', 31)

    await vi.waitFor(() => {
      const announced = sentEvents.some(e => (e.payload as { id: number }).id === 31)
      expect(announced).toBe(true)
    })

    resolveDialog({ canceled: false, filePath: dest })
    await p
    expect(readFileSync(dest, 'utf8')).toBe('hello')
  })

  it('supersedes an in-flight transfer to the same destination', async () => {
    // Retrying a download that looked stuck used to start a second transfer to
    // the same path: two write streams truncate the file and then write at
    // independent offsets, so the result was interleaved garbage.
    let releaseFirst!: () => void
    const firstGate = new Promise<void>((r) => { releaseFirst = r })
    const { req: firstReq } = fakeResponse({
      contentLength: 4, chunks: [Buffer.from('AAAA')], release: firstGate,
    })

    const first = downloadFileByPath('data/a.bin', 'a.bin', 41)
    await vi.waitFor(() => expect(httpGetImpl).toHaveBeenCalled())

    fakeResponse({ contentLength: 4, chunks: [Buffer.from('BBBB')] })
    await downloadFileByPath('data/a.bin', 'a.bin', 42)

    expect(firstReq.destroy).toHaveBeenCalled()
    // Only the second attempt's bytes are on disk, intact.
    expect(readFileSync(dest, 'utf8')).toBe('BBBB')

    releaseFirst()
    await first.catch(() => {})
  })

  it('removes the partial file when the connection drops mid-transfer', async () => {
    // `pipe` forwards data but not errors. Without handling them the promise
    // never settled and a truncated file stayed on disk looking complete.
    fakeResponse({
      contentLength: 10,
      chunks: [Buffer.from('abc')],
      failAfter: new Error('ECONNRESET'),
    })

    await expect(downloadFileByPath('data/out.bin', 'out.bin', 51)).rejects.toThrow('ECONNRESET')

    expect(existsSync(dest)).toBe(false)
    const last = sentEvents.at(-1)!.payload as { done: boolean; error: boolean; cancelled: boolean }
    expect(last.done).toBe(true)
    expect(last.error).toBe(true)
    expect(last.cancelled).toBe(false)
  })

  it('leaves an existing destination file alone when the request fails', async () => {
    // Nothing was written, so cleanup must not unlink what the user already had
    // there (the save dialog offered to overwrite it, and that never happened).
    writeFileSync(dest, 'keep')
    fakeResponse({ statusCode: 404 })

    await expect(downloadFileByPath('missing.bin', 'missing.bin', 61)).rejects.toThrow('HTTP 404')

    expect(readFileSync(dest, 'utf8')).toBe('keep')
  })
})
