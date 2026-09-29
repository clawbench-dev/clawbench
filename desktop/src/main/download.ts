import { dialog, shell } from 'electron'
import fs from 'node:fs'
import path from 'node:path'
import http from 'node:http'
import https from 'node:https'
import { getStore } from './store'
import { getMainWindow } from './window'

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
  await new Promise<void>((resolve, reject) => {
    const lib = url.startsWith('https:') ? https : http
    const req = lib.get(url, (res: import('node:http').IncomingMessage) => {
      if (res.statusCode && res.statusCode >= 400) {
        res.resume()
        if (downloadId !== undefined) emitProgress(downloadId, 0, 0, true, true)
        reject(new Error(`HTTP ${res.statusCode}`))
        return
      }
      const declared = Number(res.headers['content-length'])
      const total = Number.isFinite(declared) && declared > 0 ? declared : 0
      if (downloadId !== undefined) emitProgress(downloadId, 0, total)

      let received = 0
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

      const f = fs.createWriteStream(dest)
      res.pipe(f)
        .on('finish', () => {
          f.close()
          if (downloadId !== undefined) emitProgress(downloadId, received, total || received)
          resolve()
        })
        .on('error', (err) => {
          if (downloadId !== undefined) {
            emitProgress(downloadId, received, total, true, !cancelledDownloads.has(downloadId))
          }
          reject(err)
        })
    })
    req.on('error', (err) => {
      if (downloadId !== undefined) {
        // A destroy() from cancelDownload lands here; report it as a
        // cancellation so the renderer shows no failure toast.
        emitProgress(downloadId, 0, 0, true, !cancelledDownloads.has(downloadId),
          cancelledDownloads.has(downloadId))
      }
      reject(err)
    })
    if (downloadId !== undefined) {
      activeDownloads.set(downloadId, { destroy: () => req.destroy() })
    }
  })
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
  const dest = await pickSavePath(name)
  if (!dest) {
    // Dismissed at the save dialog: settle the bar, but as a cancellation
    // rather than a failure so the renderer shows no error toast.
    if (downloadId !== undefined) emitProgress(downloadId, 0, 0, true, false, true)
    return
  }
  try {
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
