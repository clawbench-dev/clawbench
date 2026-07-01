/**
 * Download utilities shared across all components.
 *
 * Three download primitives:
 * - buildLocalFileUrl() — construct /api/local-file/ URLs with proper encoding
 * - downloadFileByPath() — download a project file by relative path (web/app dispatch)
 * - downloadBlob()      — download client-side content as a file (blob → <a> or Android bridge)
 */

/**
 * Build a `/api/local-file/` URL with proper path encoding.
 * Each path segment is individually encodeURIComponent'd to preserve `/` separators.
 */
export function buildLocalFileUrl(
    path: string,
    options?: { download?: boolean; timestamp?: boolean }
): string {
    const encoded = path.split('/').map(s => encodeURIComponent(s)).join('/')
    let url = `/api/local-file/${encoded}`
    const params: string[] = []
    if (options?.download) params.push('download=1')
    if (options?.timestamp) params.push(`t=${Date.now()}`)
    if (params.length) url += '?' + params.join('&')
    return url
}

/**
 * Download a project file by its relative path.
 * - Web: <a> tag click with ?download=1
 * - APP (Android): native.downloadFile() → DownloadManager
 */
export function downloadFileByPath(path: string, fileName?: string): void {
    if (!path) return
    const native = (window as any).AndroidNative
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
 * Download a string as a file via Blob.
 * - Web: URL.createObjectURL + <a> tag click
 * - APP (Android): FileReader → base64 → AndroidNative.downloadBlob
 */
export function downloadBlob(content: string, filename: string, mimeType: string) {
    const blob = new Blob([content], { type: mimeType })
    const native = (window as any).AndroidNative
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
