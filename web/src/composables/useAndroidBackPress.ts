import { appLog } from '@/utils/appLog'
import { BACK_PRESS_EVENT } from './useBackHandler'
import type { BackReason } from './useNavigationStateMachine'

export { BACK_PRESS_EVENT }

/** Used when the event carries no detail — the Android native back button. */
const DEFAULT_REASON: BackReason = 'android'

function isBackReason(value: unknown): value is BackReason {
    return value === 'header' || value === 'android' || value === 'edge-swipe'
        || value === 'close'
}

declare global {
    interface Window {
        __clawbenchBackHandled?: boolean
    }
}

export interface AndroidBackPressOptions {
    /**
     * Pure, synchronous predicate: will this press be consumed?
     * Must not mutate state — it is evaluated before any navigation runs.
     */
    canHandle: (reason: BackReason) => boolean
    /** Perform the back navigation. May be async; the flag is decided before it runs. */
    navigate: (reason: BackReason) => Promise<boolean> | boolean
    /** Returns true when the user confirmed exit (second press inside the timeout). */
    requestExitConfirm: () => boolean
    /** Show the "press again to exit" hint. */
    showExitHint: () => void
}

/**
 * Bridge between a platform back request and the app.
 *
 * Two sources dispatch `BACK_PRESS_EVENT`: MainActivity's `onBackPressed`
 * (hardware / predictive back) and the web edge-swipe gesture. They carry
 * different `reason` values so the state machine sees the real trigger.
 *
 * MainActivity delegates the back press by evaluating a JS snippet that
 * dispatches the event and then **synchronously** returns
 * `window.__clawbenchBackHandled`. Therefore the flag must be written in the
 * same tick as the event — an `async` listener that awaits the navigation
 * writes it in a microtask, long after the native side has already read
 * `false` and exited the app.
 *
 * Everything that decides the flag is synchronous; the navigation itself runs
 * detached afterwards.
 *
 * @returns dispose function that removes the listener.
 */
export function useAndroidBackPress(options: AndroidBackPressOptions): () => void {
    function onBackPress(event: Event) {
        const detail = (event as CustomEvent<{ reason?: unknown }>).detail
        const reason: BackReason = isBackReason(detail?.reason) ? detail.reason : DEFAULT_REASON

        if (options.canHandle(reason)) {
            window.__clawbenchBackHandled = true
            void Promise.resolve(options.navigate(reason)).catch((err) => {
                appLog.e('Navigation', 'back press navigation failed:', err)
            })
            return
        }

        // No back stack — double-back-to-exit pattern.
        if (options.requestExitConfirm()) {
            // Second press within the timeout → allow the native exit.
            window.__clawbenchBackHandled = false
            return
        }
        window.__clawbenchBackHandled = true
        options.showExitHint()
    }

    window.addEventListener(BACK_PRESS_EVENT, onBackPress)
    return () => window.removeEventListener(BACK_PRESS_EVENT, onBackPress)
}
