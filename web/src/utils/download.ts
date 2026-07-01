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
