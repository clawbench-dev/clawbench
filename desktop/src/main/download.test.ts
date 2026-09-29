import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { EventEmitter } from 'node:events'
import { Readable } from 'node:stream'
import { mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'

/**
 * The desktop download must stream progress to the renderer, because the
 * renderer's progress bar is the only in-product feedback (the OS gives none
 * beyond the save dialog). These tests drive the real `fetchToFile` plumbing
 * with a fake `http` client and assert the IPC contract the preload forwards
 * as `clawbench-download-progress`.
 */

const { sentEvents, httpGetImpl } = vi.hoisted(() => ({
  sentEvents: [] as Array<{ channel: string; payload: unknown }>,
  httpGetImpl: vi.fn(),
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
}) {
  const chunks = opts.chunks ?? [Buffer.alloc(0)]
  async function* body() {
    if (opts.release) await opts.release
    for (const c of chunks) yield c
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
  httpGetImpl.mockImplementation((_url: string, cb: (r: unknown) => void) => {
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
})
