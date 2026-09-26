import { getNative } from '@/utils/clawbenchNative'
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
    const download = native.downloadFileWithProgress

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
        if (ok) useToast().show(gt('upload.downloaded', { count: 1 }), { icon: '✅', type: 'success' })
        else useToast().show(gt('upload.downloadFailed'), { icon: '❌', type: 'error' })
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
    const result = download(path, name, id) as unknown
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

/**
 * Web/desktop download via a streamed XHR.
 *
 * XHR rather than fetch: only XHR exposes `onprogress` on the download
 * direction, so the byte counts needed by the progress bar come for free.
 */
function webDownloadWithProgress(id: number, url: string, name: string): void {
    const xhr = new XMLHttpRequest()
    let cancelled = false
    setDownloadCancel(id, () => { cancelled = true; xhr.abort() })
    xhr.open('GET', url)
    xhr.responseType = 'blob'
    xhr.onprogress = (e) => {
        reportDownloadProgress(id, e.loaded, e.lengthComputable ? e.total : 0)
    }
    xhr.onload = () => {
        endDownload(id)
        if (cancelled) return
        if (xhr.status < 200 || xhr.status >= 300) {
            useToast().show(gt('upload.downloadFailed'), { icon: '❌', type: 'error' })
            return
        }
        saveBlob(xhr.response as Blob, name)
        useToast().show(gt('upload.downloaded', { count: 1 }), { icon: '✅', type: 'success' })
    }
    xhr.onerror = () => {
        endDownload(id)
        if (cancelled) return
        useToast().show(gt('upload.downloadFailed'), { icon: '❌', type: 'error' })
    }
    xhr.onabort = () => endDownload(id)
    xhr.send()
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
