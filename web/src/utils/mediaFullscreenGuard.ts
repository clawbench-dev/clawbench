/**
 * Exit HTML5 fullscreen when the media element holding it fails to load.
 *
 * Why this exists
 * ---------------
 * The video "maximize" is the browser's native fullscreen on the `<video>`
 * element — the app has no fullscreen code of its own. When the file is deleted
 * in the background the watcher refreshes the viewer, `store.selectFile`
 * short-circuits media by extension (no existence check), the `<video>` gets a
 * fresh prop, re-requests the now-404 URL, and its `@error` handler sets
 * `loadFailed` → the element is HIDDEN with `.local-media-hidden`
 * (`display: none !important`, media-block.css).
 *
 * Hiding a fullscreen element does NOT exit fullscreen. The black backdrop
 * disappears (so it looks like the maximize auto-closed) but the browser keeps
 * the top-layer fullscreen `<html>` element, which swallows EVERY pointer event
 * — the UI stays visible and completely unclickable until a refresh.
 *
 * The failure is specific to the hidden-but-still-in-DOM path: REMOVING the
 * element lets the browser auto-exit, and `localMediaFallback.ts` only handles
 * `<img>`, so nothing covered this.
 *
 * What this does
 * --------------
 * One document-level CAPTURE-phase `error` listener (resource errors do not
 * bubble — same reason as localMediaFallback). When the errored element is a
 * media element AND it is (or wraps / is wrapped by) the fullscreen element,
 * exit fullscreen. The capture phase runs before the component's own `@error`
 * hides the element, and exiting afterwards works too, so either ordering is
 * safe.
 *
 * One listener covers every surface: the file viewer (VideoPreview), the
 * preview card (MediaPreviewBody) and the v-html chat video player
 * (chatRenderUtils) — none of which route through a shared component.
 *
 * Kept in its own module (not folded into localMediaFallback) on purpose: many
 * tests partially mock that module with a hand-written whitelist, and a new
 * export would arrive as `undefined` there.
 */

import { appLog } from '@/utils/appLog'

const TAG = 'MediaFullscreen'

/** Marker so a nested error cannot install a second listener. */
let installed = false
let handler: ((e: Event) => void) | null = null

interface FullscreenDocument extends Document {
    webkitFullscreenElement?: Element | null
    msFullscreenElement?: Element | null
    webkitExitFullscreen?: () => Promise<void> | void
    msExitFullscreen?: () => Promise<void> | void
}

/** The element currently holding fullscreen, across vendor prefixes. */
function currentFullscreenElement(): Element | null {
    if (typeof document === 'undefined') return null
    const d = document as FullscreenDocument
    return d.fullscreenElement ?? d.webkitFullscreenElement ?? d.msFullscreenElement ?? null
}

/**
 * Exit fullscreen when `el` is the fullscreen element, or is related to it as
 * an ancestor/descendant. The relation check keeps an unrelated failing video
 * from tearing down a fullscreen the user opened elsewhere.
 *
 * Returns true when an exit was issued. Never throws: an exit refusal (e.g. a
 * browser that needs a user gesture) must not break the error path.
 */
export function exitFullscreenIfContains(el: Element | null | undefined): boolean {
    if (!el || typeof document === 'undefined') return false
    const fs = currentFullscreenElement()
    if (!fs) return false
    if (fs !== el && !el.contains(fs) && !fs.contains(el)) return false

    const d = document as FullscreenDocument
    try {
        if (typeof d.exitFullscreen === 'function') {
            void d.exitFullscreen().catch(() => { /* refusal is not actionable */ })
        } else if (typeof d.webkitExitFullscreen === 'function') {
            d.webkitExitFullscreen()
        } else if (typeof d.msExitFullscreen === 'function') {
            d.msExitFullscreen()
        } else {
            return false
        }
    } catch {
        return false
    }
    appLog.d(TAG, `media element failed while fullscreen — exiting to avoid a stuck top layer (${el.tagName})`)
    return true
}

/** True for the media elements whose failure can leave a stuck fullscreen. */
function isMediaElement(el: Element): boolean {
    return el.tagName === 'VIDEO' || el.tagName === 'AUDIO'
}

/**
 * Install the document-level capture-phase error listener. Idempotent; safe to
 * call from every app bootstrap. Returns a teardown function (tests / HMR).
 */
export function installMediaFullscreenGuard(): () => void {
    if (installed || typeof document === 'undefined') return () => {}
    installed = true

    handler = (e: Event) => {
        const target = e.target as Element | null
        if (!target || !isMediaElement(target)) return
        exitFullscreenIfContains(target)
    }

    document.addEventListener('error', handler, true)
    return uninstallMediaFullscreenGuard
}

/** Tear down the listener (tests / HMR). */
export function uninstallMediaFullscreenGuard(): void {
    if (handler) document.removeEventListener('error', handler, true)
    handler = null
    installed = false
}
