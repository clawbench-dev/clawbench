import { getNative } from '@/utils/clawbenchNative'

/**
 * Give keyboard focus back to whatever had it before a temporary textarea had
 * to take it for `execCommand`.
 *
 * This is not cosmetic. Both fallbacks below are reachable from BACKGROUND
 * work — copy-on-select fires after a terminal drag, with no click of its own —
 * and `execCommand` only works on a focused, selected element. So the helper
 * textarea takes focus, and unless it is handed back the user is left with
 * focus on `<body>`: the terminal stops receiving keystrokes entirely, and
 * every shortcut aimed at it (Shift+Insert included) silently does nothing.
 * Measured on Chromium over plain http, where `navigator.clipboard` is
 * unavailable and the fallback therefore runs on every selection.
 */
function restoreFocus(previous: Element | null): void {
    const el = previous as HTMLElement | null
    if (!el || el === document.body) return
    try {
        el.focus?.({ preventScroll: true })
    } catch {
        // A detached or non-focusable element is not worth failing a copy over.
    }
}

/** Best-effort read of the current clipboard text. Resolves with the text, or rejects if unavailable. */
export function readClipboardText(): Promise<string> {
    // Prefer the native Android bridge (reliable in WebViews, e.g. ClawBench APK).
    const native = getNative()
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
        const previous = document.activeElement
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
        } finally {
            restoreFocus(previous)
        }
    })
}

export function copyText(text: string, onSuccess?: () => void, onError?: () => void): void {
    const native = getNative()
    if (native?.writeClipboardText) {
        try {
            Promise.resolve(native.writeClipboardText(text)).then(() => onSuccess?.()).catch(() => onError?.())
        } catch {
            onError?.()
        }
        return
    }
    const fallbackCopy = (text: string): boolean => {
        const previous = document.activeElement
        const ta = document.createElement('textarea')
        ta.value = text
        ta.style.cssText = 'position:fixed;opacity:0;top:0'
        document.body.appendChild(ta)
        ta.focus()
        ta.select()
        try { return document.execCommand('copy') } catch { return false }
        finally {
            document.body.removeChild(ta)
            // See restoreFocus: taking focus here is unavoidable, but leaving it
            // there would silently break typing into the terminal (or any other
            // surface) after an automatic copy.
            restoreFocus(previous)
        }
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
