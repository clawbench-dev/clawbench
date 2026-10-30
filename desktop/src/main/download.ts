import { dialog, shell } from 'electron'
import fs from 'node:fs'
import path from 'node:path'
import http from 'node:http'
import type { ClientRequest, IncomingMessage } from 'node:http'
import https from 'node:https'
import { getStore } from './store'
import { getMainWindow } from './window'
import { getCookieHeaderForUrl } from './clientLog'

function pickSavePath(defaultName: string): Promise<string | null> {
  return dialog.showSaveDialog({ defaultPath: defaultName }).then(r => r.canceled || !r.filePath ? null : r.filePath)
}

function resolveLocalFileUrl(filePath: string): string {
  const base = getStore().get('serverUrl') || ''
  if (filePath.startsWith('/')) {
    return `${base}/api/fs/raw/?download=1&target=${encodeURIComponent(filePath)}`
  }
  return `${base}/api/fs/raw/${filePath.split('/').map(encodeURIComponent).join('/')}?download=1`
}

/**
 * In-flight streamed downloads, keyed by the renderer's download id.
 * Lets `native:cancel-download` abort the matching request.
 */
const activeDownloads = new Map<number, { destroy: () => void }>()

/**
 * Ids the user explicitly cancelled. Destroying a request surfaces as an
 * `error` event on the request, which would otherwise be reported as a failed
 * download; this set lets the error path report a cancellation instead.
 */
const cancelledDownloads = new Set<number>()

/**
 * In-flight transfers keyed by destination path.
 *
 * A retry aimed at the same file must not run alongside the transfer it
 * replaces: both would `createWriteStream(dest)` — which truncates the path —
 * and then write at independent offsets, so the file that lands on disk is
 * interleaved garbage that no archive tool can open. The newer transfer
 * therefore aborts the older one and waits for it to release the path (the
 * older one removes its own partial file first).
 */
interface ActiveTransfer {
  downloadId?: number
  abort: () => void
  /** Settles once the transfer released the destination. */
  done: Promise<void>
}

const activeByDest = new Map<string, ActiveTransfer>()

/**
 * Report download progress to the renderer. The preload forwards this IPC
 * message into a `clawbench-download-progress` CustomEvent, which is the same
 * event the Android shell dispatches via `evaluateJavascript` — so the
 * renderer's progress handling is identical on both hosts.
 */
function emitProgress(
  id: number, received: number, total: number,
  done = false, error = false, cancelled = false
): void {
  const w = getMainWindow()
  if (!w || w.isDestroyed()) return
  w.webContents.send('clawbench-download-progress', { id, received, total, done, error, cancelled })
}

interface StreamOptions {
  /** Renderer download id; when omitted no progress events are emitted. */
  downloadId?: number
}

/**
 * Stream a URL to a file, reporting Content-Length-based progress.
 *
 * `total` is 0 when the server sends no Content-Length (the streamed archive
 * endpoint), which the renderer renders as an indeterminate bar.
 */
async function fetchToFile(url: string, dest: string, opts: StreamOptions = {}): Promise<void> {
  const { downloadId } = opts
  // The main process has no `document.cookie`; the auth cookies live in
  // Electron's jar (see clientLog.getCookieHeaderForUrl). Without them the
  // authenticated /api/fs/raw/ endpoint answers 401 (or 403 with no project
  // cookie), so every desktop download would fail silently at the transfer —
  // the save dialog still appears, making the failure look like "nothing
  // happened". Read them before opening the request.
  const cookie = await getCookieHeaderForUrl(url)

  // Supersede a transfer already writing this file (see activeByDest).
  const previous = activeByDest.get(dest)
  if (previous) {
    if (previous.downloadId !== undefined) cancelledDownloads.add(previous.downloadId)
    previous.abort()
    await previous.done
  }

  let req: ClientRequest | null = null
  let fileStream: fs.WriteStream | null = null
  let received = 0
  let total = 0
  let settled = false
  let resolveDone!: () => void
  const done = new Promise<void>((resolve) => { resolveDone = resolve })

  const transfer: ActiveTransfer = { downloadId, abort: () => { req?.destroy() }, done }
  activeByDest.set(dest, transfer)

  /** Close the write stream and drop a half-written file (never a finished one). */
  const discardPartialFile = async (): Promise<void> => {
    const stream = fileStream
    fileStream = null
    if (!stream) return
    if (!stream.closed) {
      await new Promise<void>((resolve) => {
        stream.once('close', resolve)
        stream.destroy()
      })
    }
    try {
      await fs.promises.unlink(dest)
    } catch {
      // Never created, or already gone.
    }
  }

  try {
    await new Promise<void>((resolve, reject) => {
      /**
       * Settle once, and report the outcome to the renderer. A dropped
       * connection is classified as a cancellation when the user asked for it,
       * so no failure toast appears for a deliberate action.
       */
      const finish = (err?: Error): void => {
        if (settled) return
        settled = true
        if (err) {
          const cancelled = downloadId !== undefined && cancelledDownloads.has(downloadId)
          // Stop a transfer that is still running (e.g. the write failed).
          req?.destroy()
          if (downloadId !== undefined) {
            emitProgress(downloadId, received, total, true, !cancelled, cancelled)
          }
          reject(err)
          return
        }
        if (downloadId !== undefined) emitProgress(downloadId, received, total || received)
        resolve()
      }

      const lib = url.startsWith('https:') ? https : http
      req = lib.get(url, {
        headers: cookie ? { Cookie: cookie } : undefined,
        // A self-hosted server commonly uses a self-signed certificate. The
        // shell already talks to it this way elsewhere (tunnel.ts, clientLog.ts,
        // h2Transport.ts); refusing it here would break downloads on exactly
        // those installs. The session cookie, not the cert, is the credential.
        rejectUnauthorized: false,
      }, (res: IncomingMessage) => {
        if (res.statusCode && res.statusCode >= 400) {
          res.resume()
          finish(new Error(`HTTP ${res.statusCode}`))
          return
        }
        const declared = Number(res.headers['content-length'])
        total = Number.isFinite(declared) && declared > 0 ? declared : 0
        if (downloadId !== undefined) emitProgress(downloadId, 0, total)

        let lastReported = 0
        // Throttle to ~1% granularity so a fast local transfer does not flood IPC.
        const step = total > 0 ? Math.max(1, Math.floor(total / 100)) : 1 << 20

        res.on('data', (chunk: Buffer) => {
          received += chunk.length
          if (downloadId !== undefined && received - lastReported >= step) {
            lastReported = received
            emitProgress(downloadId, received, total)
          }
        })

        // `pipe` forwards data but not errors: without these a connection that
        // drops mid-transfer leaves the promise pending forever and a truncated
        // file on disk that looks like a finished download.
        res.on('error', (err) => finish(err))
        res.on('aborted', () => finish(new Error('response aborted')))

        const stream = fs.createWriteStream(dest)
        fileStream = stream
        res.pipe(stream)
          .on('finish', () => {
            // Complete: the file must survive the cleanup below.
            fileStream = null
            finish()
          })
          .on('error', (err) => finish(err))
      })
      req.on('error', (err) => finish(err))
      if (downloadId !== undefined) {
        activeDownloads.set(downloadId, { destroy: () => req?.destroy() })
      }
    })
  } finally {
    await discardPartialFile()
    if (activeByDest.get(dest) === transfer) activeByDest.delete(dest)
    // Resolved last: a superseding transfer may only start once this one has
    // released the path.
    resolveDone()
  }
}

/** Stream a project file to an explicit destination (used by share-out). */
export async function downloadFileByPathTo(filePath: string, dest: string): Promise<void> {
  await fetchToFile(resolveLocalFileUrl(filePath), dest)
}

/**
 * Download a project file with a save dialog, streaming progress back to the
 * renderer. `downloadId` is echoed in every progress event so the renderer can
 * ignore a superseded download.
 */
export async function downloadFileByPath(
  filePath: string, fileName?: string, downloadId?: number
): Promise<void> {
  const name = fileName || path.basename(filePath)
  // Announce before the save dialog opens. The renderer hides a bar that saw no
  // event for 3s (its guard against a host that never reports), and picking a
  // destination folder routinely takes longer than that — without this the bar
  // vanished while the dialog was still open, every later byte count was
  // ignored as stale, and the download looked like it never started.
  if (downloadId !== undefined) emitProgress(downloadId, 0, 0)
  const dest = await pickSavePath(name)
  if (!dest) {
    // Dismissed at the save dialog: settle the bar, but as a cancellation
    // rather than a failure so the renderer shows no error toast.
    if (downloadId !== undefined) emitProgress(downloadId, 0, 0, true, false, true)
    return
  }
  try {
    // The bar's cancel button cannot close a native dialog, so a cancel that
    // landed while the dialog was open only shows up here — honour it instead
    // of downloading a file the user already gave up on.
    if (downloadId !== undefined && cancelledDownloads.has(downloadId)) return
    await fetchToFile(resolveLocalFileUrl(filePath), dest, { downloadId })
    shell.showItemInFolder(dest)
  } catch (err) {
    if (downloadId !== undefined) {
      // fetchToFile already emitted a terminal event; only classify here so a
      // cancellation is not re-reported as a failure.
      if (!cancelledDownloads.has(downloadId)) throw err
    } else {
      throw err
    }
  } finally {
    if (downloadId !== undefined) {
      activeDownloads.delete(downloadId)
      cancelledDownloads.delete(downloadId)
    }
  }
}

/** Abort an in-flight download started by downloadFileByPath. */
export function cancelDownload(downloadId: number): void {
  cancelledDownloads.add(downloadId)
  activeDownloads.get(downloadId)?.destroy()
  activeDownloads.delete(downloadId)
}

export async function downloadByUrl(url: string, fileName: string): Promise<void> {
  const dest = await pickSavePath(fileName || path.basename(url))
  if (!dest) return
  await new Promise<void>((resolve, reject) => {
    https.get(url, (res) => {
      const f = fs.createWriteStream(dest)
      res.pipe(f).on('finish', () => { f.close(); resolve() }).on('error', reject)
    }).on('error', reject)
  })
  shell.showItemInFolder(dest)
}

export async function downloadBlob(base64: string, fileName: string): Promise<void> {
  const dest = await pickSavePath(fileName)
  if (!dest) return
  fs.writeFileSync(dest, Buffer.from(base64, 'base64'))
  shell.showItemInFolder(dest)
}
