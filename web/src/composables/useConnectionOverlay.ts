import { ref, computed, watch, onUnmounted } from 'vue'
import { useGlobalEvents } from './useGlobalEvents'
import { useSettingsNavigation } from './useSettingsNavigation'
import { appLog } from '@/utils/appLog'

// Delay before showing the reconnect mask, so transient blips that recover
// within this window never flash a fullscreen overlay.
export const RECONNECT_OVERLAY_DELAY_MS = 5000

export type ConnectionOverlayMode = 'restart' | 'reconnect' | null

/**
 * Drives the unified fullscreen status overlay.
 *
 * - 'restart'   → shown immediately while the server is restarting (user-initiated).
 * - 'reconnect' → shown only after the WS stays disconnected for
 *   RECONNECT_OVERLAY_DELAY_MS AND the connection was established at least once
 *   (prevents a mask flash on first page load).
 * - null        → overlay hidden.
 *
 * Restart takes priority over reconnect.
 *
 * When the app returns to foreground, the reconnect timer is reset so that
 * the overlay won't flash immediately — the 5s grace period restarts from
 * zero, giving the WS a fresh window to reconnect.
 */
export function useConnectionOverlay() {
    const { wsStatus, hasConnectedOnce } = useGlobalEvents()
    const { restartingOverlay } = useSettingsNavigation()

    const showReconnect = ref(false)
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    function clearTimer() {
        if (reconnectTimer) {
            clearTimeout(reconnectTimer)
            reconnectTimer = null
        }
    }

    /** Reset the reconnect timer so the 5s grace period restarts from zero. */
    function resetTimer() {
        clearTimer()
        showReconnect.value = false
        // If WS is not connected and we've connected before, restart the timer
        if (wsStatus.value !== 'connected' && hasConnectedOnce.value) {
            appLog.d('ConnectionOverlay', 'foreground reset: restarting reconnect timer')
            reconnectTimer = setTimeout(() => {
                showReconnect.value = true
                reconnectTimer = null
            }, RECONNECT_OVERLAY_DELAY_MS)
        }
    }

    watch(
        wsStatus,
        (status) => {
            if (status === 'connected') {
                clearTimer()
                showReconnect.value = false
                return
            }
            // disconnected or reconnecting
            if (!hasConnectedOnce.value || showReconnect.value || reconnectTimer) return
            reconnectTimer = setTimeout(() => {
                showReconnect.value = true
                reconnectTimer = null
            }, RECONNECT_OVERLAY_DELAY_MS)
        },
        { immediate: true },
    )

    // On foreground: reset the timer so the overlay doesn't flash immediately
    // after resuming from background (the old timer may have nearly expired
    // during pauseTimers).
    function onForeground() {
        resetTimer()
    }
    window.addEventListener('clawbench-foreground', onForeground)

    onUnmounted(() => {
        clearTimer()
        window.removeEventListener('clawbench-foreground', onForeground)
    })

    const mode = computed<ConnectionOverlayMode>(() => {
        if (restartingOverlay.value) return 'restart'
        if (showReconnect.value) return 'reconnect'
        return null
    })

    return { mode }
}
