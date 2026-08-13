/** Best-effort read of the current clipboard text. Resolves with the text, or rejects if unavailable. */
export function readClipboardText(): Promise<string> {
    // Prefer the native Android bridge (reliable in WebViews, e.g. ClawBench APK).
    const native = (window as unknown as { ClawBenchNative?: { readClipboardText?: () => string } }).ClawBenchNative
    if (native?.readClipboardText) {
        try {
            return Promise.resolve(native.readClipboardText() ?? '')
        } catch {
            // fall through to standard APIs
        }
    }
    if (navigator.clipboard?.readText) {
        return navigator.clipboard.readText().then((text) => text ?? '')
    }
    // Fallback for environments without the async clipboard API (e.g. older WebViews).
    return new Promise((resolve, reject) => {
        try {
            const ta = document.createElement('textarea')
            ta.style.cssText = 'position:fixed;opacity:0;top:0'
            document.body.appendChild(ta)
            ta.focus()
            ta.select()
            if (document.execCommand('paste')) resolve(ta.value || '')
            else reject(new Error('clipboard read unsupported'))
            document.body.removeChild(ta)
        } catch {
            reject(new Error('clipboard read unsupported'))
        }
    })
}

export function copyText(text: string, onSuccess?: () => void, onError?: () => void): void {
    const fallbackCopy = (text: string): boolean => {
        const ta = document.createElement('textarea')
        ta.value = text
        ta.style.cssText = 'position:fixed;opacity:0;top:0'
        document.body.appendChild(ta)
        ta.focus()
        ta.select()
        try { return document.execCommand('copy') } catch { return false }
        finally { document.body.removeChild(ta) }
    }

    if (navigator.clipboard?.writeText) {
        navigator.clipboard.writeText(text).then(() => {
            onSuccess?.()
        }).catch(() => {
            if (fallbackCopy(text)) onSuccess?.()
            else onError?.()
        })
    } else {
        if (fallbackCopy(text)) onSuccess?.()
        else onError?.()
    }
}
