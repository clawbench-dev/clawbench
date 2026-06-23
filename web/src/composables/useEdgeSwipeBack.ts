/**
 * Edge swipe back gesture detection for drill-down pages.
 *
 * Detects swipe from the right edge of the screen going left,
 * and triggers back navigation via the global back handler.
 *
 * Also prevents Android's edge-swipe-to-exit by consuming touch
 * events that start within the edge zones (left 20px and right 20px).
 */
import { onMounted, onBeforeUnmount } from 'vue'
import { registerBackHandler, type BackHandler } from './useBackHandler'
import { useAppMode } from './useAppMode'

const EDGE_ZONE = 20 // px from screen edge to detect edge swipes
const SWIPE_THRESHOLD = 50 // minimum px to trigger back navigation
const SWIPE_MAX_DURATION = 400 // ms — must be a quick swipe gesture
const MAX_VERTICAL_RATIO = 0.75 // horizontal must dominate over vertical

/**
 * Set up global edge swipe detection on the document body.
 * This should be called once from App.vue.
 *
 * - Detects right-edge-left-swipe and triggers back navigation
 * - Prevents Android edge-swipe-to-exit by consuming touches
 *   that start within the edge zones and would otherwise cause the
 *   system to handle the gesture (exit app or go back in WebView history)
 */
export function useEdgeSwipeBack() {
    let touchStartX = 0
    let touchStartY = 0
    let touchStartTime = 0
    let touchStartEdge: 'left' | 'right' | null = null

    // In Android app mode, the native onBackPressed handles back gestures via
    // evaluateJavascript — the JS edge-swipe detection would cause double-dispatch.
    // Only dispatch from JS when running in a browser (web mode).
    const { isAppMode } = useAppMode()

    function onTouchStart(e: TouchEvent) {
        if (e.touches.length !== 1) return
        const touch = e.touches[0]
        touchStartX = touch.clientX
        touchStartY = touch.clientY
        touchStartTime = Date.now()
        touchStartEdge = null

        // Detect if touch starts in an edge zone
        if (touch.clientX <= EDGE_ZONE) {
            touchStartEdge = 'left'
        } else if (touch.clientX >= window.innerWidth - EDGE_ZONE) {
            touchStartEdge = 'right'
        }
    }

    function onTouchEnd(_e: TouchEvent) {
        if (!touchStartEdge) {
            touchStartEdge = null
            return
        }

        const touch = _e.changedTouches[0]
        const deltaX = touch.clientX - touchStartX
        const deltaY = touch.clientY - touchStartY
        const duration = Date.now() - touchStartTime

        // Right edge swipe left → back navigation
        if (touchStartEdge === 'right' && deltaX < -SWIPE_THRESHOLD) {
            // Must be more horizontal than vertical
            if (Math.abs(deltaY) <= Math.abs(deltaX) * MAX_VERTICAL_RATIO) {
                // Must be a quick swipe or significant distance
                if (duration < SWIPE_MAX_DURATION || Math.abs(deltaX) > SWIPE_THRESHOLD * 2) {
                    // In web mode, dispatch the clawbench-back-press event so the
                    // double-back-to-exit logic in App.vue applies uniformly.
                    // In Android app mode, skip — the native onBackPressed already
                    // dispatches this event via evaluateJavascript, and dispatching
                    // from JS too would cause double-dispatch on a single swipe.
                    if (!isAppMode.value) {
                        window.dispatchEvent(new CustomEvent('clawbench-back-press'))
                    }
                }
            }
        }

        touchStartEdge = null
    }

    onMounted(() => {
        document.addEventListener('touchstart', onTouchStart, { passive: true })
        document.addEventListener('touchend', onTouchEnd, { passive: true })
    })

    onBeforeUnmount(() => {
        document.removeEventListener('touchstart', onTouchStart)
        document.removeEventListener('touchend', onTouchEnd)
    })
}

/**
 * Register a feature's ability to navigate back.
 * Call this from a drill-down page component.
 *
 * The canGoBack function is evaluated lazily on each back press / swipe,
 * so it should reflect the current state (e.g., currentView !== 'list').
 *
 * @param id Unique feature ID (e.g., 'file-overlay', 'browse', 'settings')
 * @param canGoBack Function returning true if the feature can navigate back
 * @param goBack Function to perform the back navigation
 * @param priority Dispatch priority — higher = checked first.
 *                  Use PRIORITY_OVERLAY (1000) for overlays/modals,
 *                  PRIORITY_PAGE (100) for normal drill-down pages.
 */
export function useFeatureBackHandler(
    id: string,
    canGoBack: () => boolean,
    goBack: () => void,
    priority: number,
) {
    let unregister: (() => void) | null = null

    onMounted(() => {
        const handler: BackHandler = { id, canGoBack, goBack, priority }
        unregister = registerBackHandler(handler)
    })

    onBeforeUnmount(() => {
        if (unregister) {
            unregister()
            unregister = null
        }
    })
}

// Re-export priority constants for convenience
export { PRIORITY_OVERLAY, PRIORITY_PAGE } from './useBackHandler'
