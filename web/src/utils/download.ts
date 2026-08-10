import { getNative } from '@/utils/clawbenchNative'

/**
 * Download utilities shared across all components.
 *
 * Three download primitives:
 * - buildLocalFileUrl() — construct /api/local-file/ URLs with proper encoding
 * - downloadFileByPath() — download a file by relative or absolute path (web/app dispatch)
 * - downloadBlob()      — download client-side content as a file (blob → <a> or native bridge)
 */

/**
 * Check if a path is an absolute path (external to the project).
 * On Unix: starts with /
 * On Windows: starts with a drive letter (C:\) or UNC (\\)
 */
function isAbsolutePath(p: string): boolean {
    return p.startsWith('/') || /^[A-Za-z]:/.test(p) || p.startsWith('\\\\')
}

/**
 * Build a `/api/local-file/` URL with proper path encoding.
 * - For project-relative paths: encodes each segment individually to preserve `/` separators.
 * - For absolute paths (external files): uses `?path=` query param to pass the absolute path.
 */
export function buildLocalFileUrl(
    path: string,
    options?: { download?: boolean }
): string {
    const params: string[] = []
    if (options?.download) params.push('download=1')

    if (isAbsolutePath(path)) {
        // External file: use ?path= query param
        params.push(`path=${encodeURIComponent(path)}`)
        return '/api/local-file/?' + params.join('&')
    }

    // Project-relative: encode segments individually
    const encoded = path.split('/').map(s => encodeURIComponent(s)).join('/')
    let url = `/api/local-file/${encoded}`
    if (params.length) url += '?' + params.join('&')
    return url
}

/**
 * Download a file by its relative or absolute path.
 * - Web: <a> tag click with ?download=1
 * - APP (Android): native.downloadFile() → DownloadManager
 */
export function downloadFileByPath(path: string, fileName?: string): void {
    if (!path) return
    const native = getNative()
    if (typeof native !== 'undefined' && native?.downloadFile) {
        native.downloadFile(path)
        return
    }
    const a = document.createElement('a')
    a.href = buildLocalFileUrl(path, { download: true })
    a.download = fileName || path.split('/').pop() || ''
    document.body.appendChild(a)
    a.click()
    // Delay cleanup to avoid race with download initiation
    setTimeout(() => {
        document.body.removeChild(a)
    }, 1000)
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
