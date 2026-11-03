import { getNative } from '@/utils/clawbenchNative'
import { appLog, diagLog } from '@/utils/appLog'
import { isAbsolutePath } from '@/utils/path'
import { isShareMode, shareApiUrl } from '@/share/shareMode'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast'
import {
    beginDownload, endDownload, nextDownloadId,
    reportDownloadProgress, setDownloadCancel,
} from '@/composables/useDownloadProgress'

/**
 * Download utilities shared across all components.
 *
 * Three download primitives:
 * - buildLocalFileUrl() — construct /api/fs/raw/ URLs with proper encoding
 * - downloadFileByPath() — download a file by relative or absolute path (web/app dispatch)
 * - downloadBlob()      — download client-side content as a file (blob → <a> or native bridge)
 *
 * `downloadFileByPath` streams the response and reports byte progress to the
 * `useDownloadProgress` singleton, which App.vue renders as a progress bar.
 * Every transport (web fetch/XHR, Electron main process, Android OkHttp)
 * reports into that same state so the bar behaves identically on all hosts.
 */

/** The event both native hosts dispatch with download progress. */
export const DOWNLOAD_PROGRESS_EVENT = 'clawbench-download-progress'

interface DownloadProgressDetail {
    id: number
    received: number
    total: number
    /** Terminal marker: the transfer finished (successfully or not). */
    done?: boolean
    /** Set alongside done when the transfer failed. */
    error?: boolean
    /**
     * Set alongside done when the user cancelled (e.g. dismissed the save
     * dialog, or aborted a native transfer). Distinct from `error` so no
     * failure toast is shown for a deliberate action.
     */
    cancelled?: boolean
}

/**
 * Run a native download and drive the progress bar from the host's events.
 *
 * Both hosts dispatch `clawbench-download-progress` CustomEvents — Android via
 * `evaluateJavascript`, Electron via the preload forwarding IPC. Progress is
 * therefore driven by events, never by polling.
 *
 * Completion is reported differently by each host, so both shapes are handled:
 * Electron's `ipcRenderer.invoke` promise settles when the transfer ends;
 * Android's `@JavascriptInterface` method returns synchronously and only ever
 * signals completion through a `done` event.
 */
function nativeDownloadWithProgress(id: number, path: string, name: string): void {
    const native = getNative()
    // Only hosts exposing the progress method stream and emit events; a legacy
    // shell falls back to its own downloadFile() (no progress).
    if (!native?.downloadFileWithProgress) {
        endDownload(id)
        void native?.downloadFile(path)
        return
    }
    let settled = false
    let cancelled = false
    let sawEvent = false
    let fallback: ReturnType<typeof setTimeout> | null = null

    const handler = (e: Event) => {
        const detail = (e as CustomEvent<DownloadProgressDetail>).detail
        if (!detail || detail.id !== id) return
        sawEvent = true
        if (detail.done) {
            // A host-reported cancellation must not surface as a failure.
            if (detail.cancelled) cancelled = true
            finish(!detail.error)
            return
        }
        reportDownloadProgress(id, detail.received, detail.total)
    }

    const finish = (ok: boolean, notify = true) => {
        if (settled) return
        settled = true
        window.removeEventListener(DOWNLOAD_PROGRESS_EVENT, handler)
        if (fallback !== null) clearTimeout(fallback)
        endDownload(id)
        if (cancelled || !notify) return
        if (ok) useToast().show(gt('upload.downloaded', { count: 1 }), { icon: 'success', type: 'success' })
        else useToast().show(gt('upload.downloadFailed'), { icon: 'error', type: 'error' })
    }

    window.addEventListener(DOWNLOAD_PROGRESS_EVENT, handler)
    // A host that advertises cancelDownload is expected to emit events. If none
    // arrives at all, stop showing a bar that can never advance — silently, so
    // no toast claims an outcome that was never observed (the host's own
    // download still proceeds regardless).
    fallback = setTimeout(() => { if (!sawEvent) finish(true, false) }, 3000)

    // The flag is local so a cancel of a *later* download cannot suppress this
    // one's completion toast (the global cancel state is reset per download).
    setDownloadCancel(id, () => {
        cancelled = true
        try { native.cancelDownload?.(id) } catch { /* older host */ }
    })

    // Completion signal differs by host, and both shapes are needed:
    //  - Electron's ipcRenderer.invoke returns a Promise that settles when the
    //    main-process transfer is done (it never sends a terminal event).
    //  - Android's @JavascriptInterface method is synchronous and returns
    //    undefined immediately, so its Promise would resolve instantly; there
    //    the terminal signal is the `done` event instead. Finishing on the
    //    promise there would hide the bar the moment it appeared.
    // Must be called as a method on the bridge object, not through a detached
    // local: Android's WebView JS bridge rejects a call whose receiver is not
    // the injected object ("can't be invoked on a non-injected object").
    const result = native.downloadFileWithProgress(path, name, id) as unknown
    if (result && typeof (result as Promise<void>).then === 'function') {
        ;(result as Promise<void>).then(() => finish(true)).catch(() => finish(false))
    }
}

/** Fallback: hand the URL to the browser's native download mechanism. */
function anchorDownload(url: string, fileName: string): void {
    const a = document.createElement('a')
    a.href = url
    a.download = fileName
    document.body.appendChild(a)
    a.click()
    // Delay cleanup to avoid race with download initiation
    setTimeout(() => {
        document.body.removeChild(a)
    }, 1000)
}

/** Trigger a client-side save of an already-received blob. */
export function saveBlob(blob: Blob, fileName: string): void {
    const url = URL.createObjectURL(blob)
    anchorDownload(url, fileName)
    // Give the click a moment to start before revoking the object URL.
    setTimeout(() => URL.revokeObjectURL(url), 1000)
}

/**
 * Build a file-content URL with proper path encoding.
 * - Normal mode: `/api/fs/raw/` (project-relative via URL path, absolute via ?target=).
 * - Share mode: `/api/share/{token}/local/...` so the anonymous share SPA can
 *   fetch the referenced file without auth. Absolute paths resolve through the
 *   token-scoped ?path= endpoint; bare relative paths are served relative to the
 *   shared file's directory by the backend.
 */
export function buildLocalFileUrl(
    path: string,
    options?: { download?: boolean }
): string {
    const params: string[] = []
    if (options?.download) params.push('download=1')

    if (isShareMode()) {
        // Share mode: route through the token-scoped local endpoint.
        if (isAbsolutePath(path)) {
            params.push(`path=${encodeURIComponent(path)}`)
            return shareApiUrl('local') + (params.length ? '?' + params.join('&') : '')
        }
        const encoded = path.split('/').map(s => encodeURIComponent(s)).join('/')
        let url = shareApiUrl('local/' + encoded)
        if (params.length) url += '?' + params.join('&')
        return url
    }

    if (isAbsolutePath(path)) {
        // External file: use ?target= query param
        params.push(`target=${encodeURIComponent(path)}`)
        return '/api/fs/raw/?' + params.join('&')
    }

    // Project-relative: encode segments individually
    const encoded = path.split('/').map(s => encodeURIComponent(s)).join('/')
    let url = `/api/fs/raw/${encoded}`
    if (params.length) url += '?' + params.join('&')
    return url
}

/**
 * Download a file by its relative or absolute path, showing in-product progress.
 *
 * - Web/desktop: streamed XHR, reporting bytes to the progress bar.
 * - APP (Android): native.downloadFileWithProgress() with progress events
 *   dispatched back from the OkHttp streaming implementation.
 *
 * Falls back to the browser's native download when another download is already
 * running (the UI has a single bar) or when streaming is unavailable, so the
 * file always downloads even if progress cannot be shown.
 */
export function downloadFileByPath(path: string, fileName?: string): void {
    if (!path) return
    const name = fileName || path.split('/').pop() || 'download'
    const url = buildLocalFileUrl(path, { download: true })

    const id = nextDownloadId()
    if (!beginDownload(id, name, 0)) {
        // Another download is already showing progress — fall back rather than
        // dropping the request.
        downloadWithoutProgress(path, url, name)
        return
    }

    const native = getNative()
    if (typeof native?.downloadFileWithProgress === 'function') {
        nativeDownloadWithProgress(id, path, name)
        return
    }
    // A native host without the progress method still downloads via its own
    // bridge (no bar); the web/desktop path streams and reports progress.
    if (typeof native?.downloadFile === 'function') {
        endDownload(id)
        void native.downloadFile(path)
        return
    }
    webDownloadWithProgress(id, url, name)
}

const TAG = 'Download'

/**
 * Bytes requested per Range call.
 *
 * The download is split into bounded chunks so a dropped connection costs at
 * most one chunk: everything already received stays buffered and the next
 * attempt resumes from its byte count. A single unbounded request (the previous
 * implementation) had to restart from zero after every hiccup, which on a weak
 * link meant a 200MB transfer could never finish.
 */
const DOWNLOAD_CHUNK_BYTES = 4 * 1024 * 1024
/** Consecutive failures tolerated for one chunk before the download is given up. */
const DOWNLOAD_MAX_RETRIES = 3
/** Backoff between attempts; the last entry repeats for any further retry. */
const DOWNLOAD_RETRY_DELAYS_MS = [500, 1500, 3000]

interface ContentRange {
    start: number
    end: number
    /** Full file size; 0 when the server sent `/*`. */
    total: number
}

/** Parse `Content-Range: bytes 0-1023/2048`. Returns null when malformed. */
function parseContentRange(header: string | null): ContentRange | null {
    if (!header) return null
    const match = /^bytes\s+(\d+)-(\d+)\/(\d+|\*)\s*$/i.exec(header.trim())
    if (!match) return null
    return {
        start: Number(match[1]),
        end: Number(match[2]),
        total: match[3] === '*' ? 0 : Number(match[3]),
    }
}

/**
 * Web/desktop download via a streamed XHR, resumed across connection drops.
 *
 * XHR rather than fetch: only XHR exposes `onprogress` on the download
 * direction, so the byte counts needed by the progress bar come for free.
 *
 * The file is fetched as a sequence of `Range` chunks (the backend serves file
 * content through `http.ServeContent`, which answers 206). A failed chunk is
 * retried with backoff, and the bar keeps its position instead of resetting —
 * the "progress bar silently disappears at 10%" report was the failure path
 * tearing the bar down on the first hiccup.
 *
 * A server that ignores `Range` answers 200 with the whole body; that is
 * handled by replacing the buffer, so arbitrary URLs passed in by callers
 * (`downloadUrlWithProgress`) keep working.
 */
function webDownloadWithProgress(id: number, url: string, name: string): void {
    /** Chunks received so far, in order. */
    const chunks: Blob[] = []
    /** Bytes committed to `chunks` — also the offset of the next Range request. */
    let banked = 0
    /** Full file size; 0 while unknown (no Content-Range / no Content-Length). */
    let total = 0
    /** Consecutive failures for the current chunk. */
    let failures = 0
    let cancelled = false
    /**
     * Set once a Range request was refused outright (416, which is what
     * `http.ServeContent` answers for a zero-byte file). The next attempt then
     * asks for the whole body instead.
     */
    let ignoreRange = false
    let xhr: XMLHttpRequest | null = null
    let retryTimer: ReturnType<typeof setTimeout> | null = null

    const clearRetryTimer = () => {
        if (retryTimer !== null) { clearTimeout(retryTimer); retryTimer = null }
    }

    setDownloadCancel(id, () => {
        cancelled = true
        clearRetryTimer()
        const inFlight = xhr
        xhr = null
        inFlight?.abort()
    })

    const succeed = () => {
        // Preserve the response's MIME type: a single chunk keeps it verbatim,
        // several chunks carry the type of the first one.
        const blob = chunks.length === 1 ? chunks[0] : new Blob(chunks, { type: chunks[0]?.type || '' })
        chunks.length = 0
        endDownload(id)
        if (cancelled) return
        saveBlob(blob, name)
        useToast().show(gt('upload.downloaded', { count: 1 }), { icon: 'success', type: 'success' })
    }

    const fail = (reason: string, detail?: Record<string, unknown>) => {
        endDownload(id)
        if (cancelled) return
        // The failing stage is usually the response one, where an intermediary
        // returns something the browser cannot describe. Record what is
        // observable so a "download failed" toast can be diagnosed later.
        const info = { name, reason, received: banked, total, ...detail }
        appLog.w(TAG, 'download failed', info)
        // The terminal line must reach the server even with 调试日志捕获 off
        // (its default) — otherwise the report this fix is answering stays
        // undiagnosable. It fires at most once per failed transfer.
        diagLog(TAG, `download failed ${JSON.stringify(info)}`)
        useToast().show(gt('upload.downloadFailed'), { icon: 'error', type: 'error' })
    }

    const retry = (reason: string, detail?: Record<string, unknown>) => {
        failures += 1
        appLog.w(TAG, 'download attempt failed', {
            name, reason, attempt: failures, received: banked, total, ...detail,
        })
        if (failures > DOWNLOAD_MAX_RETRIES) { fail(reason, detail); return }
        // Hold the bar at the banked offset: the transfer continues from there,
        // so resetting to 0 would read as a restart (and jump backwards).
        reportDownloadProgress(id, banked, total)
        const delay = DOWNLOAD_RETRY_DELAYS_MS[Math.min(failures, DOWNLOAD_RETRY_DELAYS_MS.length) - 1]
        retryTimer = setTimeout(() => {
            retryTimer = null
            if (!cancelled) requestChunk()
        }, delay)
    }

    const requestChunk = () => {
        if (cancelled) return
        if (total > 0 && banked >= total) { succeed(); return }
        const start = banked
        const current = new XMLHttpRequest()
        xhr = current
        current.open('GET', url)
        current.responseType = 'blob'
        // Bounded even for the first request: a 206 also carries the total size
        // (Content-Range), which the plain response's Content-Length would not
        // reveal for a resumed transfer.
        if (!ignoreRange) {
            current.setRequestHeader('Range', `bytes=${start}-${start + DOWNLOAD_CHUNK_BYTES - 1}`)
        }
        current.onprogress = (e) => {
            // A 206 slice reports only its own bytes, so the bar is offset by
            // what earlier chunks already delivered. A 200 means Range was
            // ignored and the body is the whole file from byte 0.
            const base = current.status === 206 ? start : 0
            if (total === 0) {
                const range = parseContentRange(current.getResponseHeader('Content-Range'))
                if (range) total = range.total
            }
            const sliceTotal = e.lengthComputable ? base + e.total : 0
            reportDownloadProgress(id, base + e.loaded, total || sliceTotal)
        }
        current.onload = () => {
            if (cancelled) return
            const status = current.status
            const blob = current.response as Blob | null
            if (status === 200) {
                // Range ignored (or a server without Range support): the body is
                // the whole file, so anything banked by earlier attempts is
                // discarded rather than concatenated.
                if (!blob) { fail('empty-response', { status }); return }
                chunks.length = 0
                chunks.push(blob)
                banked = blob.size
                total = blob.size
                succeed()
                return
            }
            if (status === 206) {
                if (!blob || blob.size === 0) { retry('empty-partial-response', { status }); return }
                const range = parseContentRange(current.getResponseHeader('Content-Range'))
                if (!range || range.total === 0) {
                    // A 206 must carry Content-Range. Without it the remaining
                    // bytes cannot be located, so ask for the whole body in one
                    // plain request — and give up if that is refused too,
                    // rather than guessing at chunk boundaries forever.
                    if (ignoreRange) { fail('missing-content-range', { status }); return }
                    ignoreRange = true
                    chunks.length = 0
                    banked = 0
                    failures = 0
                    requestChunk()
                    return
                }
                // A proxy or cache that mishandles Range (a classic: Range is
                // not part of its cache key) can answer with a different slice
                // than the one requested. Appending that would produce a file
                // of exactly the right size with corrupted content and no error
                // at all, so verify the slice instead of trusting it.
                if (range.start !== start) {
                    retry('range-start-mismatch', { status, expected: start, got: range.start })
                    return
                }
                if (range.end - range.start + 1 !== blob.size) {
                    retry('range-length-mismatch', { status, expected: range.end - range.start + 1, got: blob.size })
                    return
                }
                if (total > 0 && total !== range.total) {
                    // The file changed underneath the transfer: concatenating
                    // the two versions would corrupt it.
                    fail('range-total-changed', { status, expected: total, got: range.total })
                    return
                }
                total = range.total
                chunks.push(blob)
                banked += blob.size
                failures = 0
                if (banked >= total) { succeed(); return }
                requestChunk()
                return
            }
            if (status >= 500) { retry(`http-${status}`, { status }); return }
            if (status === 416 && !ignoreRange && banked === 0) {
                // `http.ServeContent` answers 416 to a Range request on a
                // zero-byte file. Ask for the whole body instead of failing a
                // download that used to work.
                ignoreRange = true
                failures = 0
                requestChunk()
                return
            }
            fail(`http-${status}`, { status })
        }
        current.onerror = () => {
            if (!cancelled) retry('network-error', { status: current.status, readyState: current.readyState })
        }
        current.ontimeout = () => {
            if (!cancelled) retry('timeout', { status: current.status, readyState: current.readyState })
        }
        current.onabort = () => { endDownload(id) }
        current.send()
    }

    requestChunk()
}

/** Progress-less fallback used when the single progress bar is occupied. */
function downloadWithoutProgress(path: string, url: string, name: string): void {
    const native = getNative()
    if (typeof native?.downloadFile === 'function') {
        void native.downloadFile(path)
        return
    }
    anchorDownload(url, name)
}

/**
 * Download an arbitrary URL with in-product progress (web/desktop only).
 *
 * Used by surfaces that build their own download URL — the public share SPA
 * (`/api/share/{token}/download`) and the archive endpoint — so they get the
 * same progress bar as `downloadFileByPath` without going through its
 * path→URL construction.
 *
 * Falls back to an anchor click when another download already occupies the
 * single bar, so the file is never dropped.
 */
export function downloadUrlWithProgress(url: string, fileName: string): void {
    if (!url) return
    const id = nextDownloadId()
    if (!beginDownload(id, fileName, 0)) {
        anchorDownload(url, fileName)
        return
    }
    webDownloadWithProgress(id, url, fileName)
}

export interface PostBlobResult {
    blob: Blob | null
    /** Server-provided error message when the request failed, if any. */
    errorDetail?: string
    /** True when the user cancelled, so the caller can skip the error toast. */
    cancelled?: boolean
}

/**
 * POST a JSON body and stream the response to a file, with progress.
 *
 * The archive endpoint streams a zip it builds on the fly and therefore sends
 * no Content-Length; the bar then runs indeterminately while the transfer is in
 * flight. Returns the received Blob so the caller can hand it to the platform's
 * save path (native bridge on Android, object URL on web).
 *
 * When another download already owns the single progress bar, this falls back
 * to a plain fetch so the request still happens — otherwise the archive would
 * silently fail to download.
 */
export async function postForBlobWithProgress(
    url: string, body: unknown, fileName: string
): Promise<PostBlobResult> {
    const id = nextDownloadId()
    if (!beginDownload(id, fileName, 0)) return postForBlobWithoutProgress(url, body)

    return new Promise<PostBlobResult>((resolve) => {
        const xhr = new XMLHttpRequest()
        let cancelled = false
        setDownloadCancel(id, () => { cancelled = true; xhr.abort() })
        xhr.open('POST', url)
        xhr.setRequestHeader('Content-Type', 'application/json')
        xhr.responseType = 'blob'
        xhr.onprogress = (e) => {
            reportDownloadProgress(id, e.loaded, e.lengthComputable ? e.total : 0)
        }
        xhr.onload = () => {
            endDownload(id)
            if (cancelled) return resolve({ blob: null, cancelled: true })
            if (xhr.status < 200 || xhr.status >= 300) {
                // The error body arrives as a Blob because responseType is
                // 'blob'; read it back so the caller can show the reason.
                const errBlob = xhr.response as Blob | null
                if (!errBlob) return resolve({ blob: null })
                errBlob.text()
                    .then((text) => {
                        let detail = ''
                        try { detail = JSON.parse(text).error || '' } catch { /* not JSON */ }
                        resolve({ blob: null, errorDetail: detail })
                    })
                    .catch(() => resolve({ blob: null }))
                return
            }
            resolve({ blob: xhr.response as Blob })
        }
        xhr.onerror = () => { endDownload(id); resolve({ blob: null }) }
        xhr.onabort = () => { endDownload(id); resolve({ blob: null, cancelled: true }) }
        xhr.send(JSON.stringify(body))
    })
}

/** Progress-less archive fetch used when the single bar is already taken. */
async function postForBlobWithoutProgress(url: string, body: unknown): Promise<PostBlobResult> {
    try {
        const resp = await fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        })
        if (!resp.ok) {
            const err = await resp.json().catch(() => ({ error: '' }))
            return { blob: null, errorDetail: err.error || '' }
        }
        return { blob: await resp.blob() }
    } catch {
        return { blob: null }
    }
}

/**
 * Download a file by its full URL (e.g. /api/apk).
 * - Web: <a> tag click
 * - APP (Android): native.downloadUrl() → DownloadManager
 */
export function downloadByUrl(url: string, fileName?: string): void {
    if (!url) return
    const native = getNative()
    if (typeof native !== 'undefined' && native?.downloadUrl) {
        native.downloadUrl(url, fileName || '')
        return
    }
    const a = document.createElement('a')
    a.href = url
    a.download = fileName || url.split('/').pop() || ''
    document.body.appendChild(a)
    a.click()
    setTimeout(() => {
        document.body.removeChild(a)
    }, 1000)
}

/**
 * Download a string as a file via Blob.
 * - Web: URL.createObjectURL + <a> tag click
 * - APP (Android): FileReader → base64 → ClawBenchNative.downloadBlob
 */
export function downloadBlob(content: string, filename: string, mimeType: string) {
    const blob = new Blob([content], { type: mimeType })
    const native = getNative()
    const isApp = typeof native !== 'undefined' && native?.downloadBlob

    if (isApp) {
        const reader = new FileReader()
        reader.onload = () => {
            const base64 = (reader.result as string).split(',')[1]
            native.downloadBlob(base64, filename)
        }
        reader.readAsDataURL(blob)
    } else {
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = filename
        document.body.appendChild(a)
        a.click()
        // Delay cleanup to avoid race with download initiation
        setTimeout(() => {
            document.body.removeChild(a)
            URL.revokeObjectURL(url)
        }, 1000)
    }
}
