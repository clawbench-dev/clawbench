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
