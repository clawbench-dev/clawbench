import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Mutable shared refs created once (vi.mock is hoisted)
const { wsStatusRef, hasConnectedOnceRef, restartingOverlayRef } = vi.hoisted(() => {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const { ref } = require('vue')
    return {
        wsStatusRef: ref('connected'),
        hasConnectedOnceRef: ref(false),
        restartingOverlayRef: ref(false),
    }
})

vi.mock('@/composables/useGlobalEvents', () => ({
    useGlobalEvents: () => ({
        wsStatus: wsStatusRef,
        hasConnectedOnce: hasConnectedOnceRef,
    }),
}))

vi.mock('@/composables/useSettingsNavigation', () => ({
    useSettingsNavigation: () => ({
        restartingOverlay: restartingOverlayRef,
    }),
}))

// Mock vue's onUnmounted to be a no-op outside a component instance.
// NOTE: must use require('vue') (same instance as the vi.hoisted refs) so the
// composable's watch/computed share the SAME reactivity system as the mocks.
vi.mock('vue', () => {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const vue = require('vue')
    return { ...vue, onUnmounted: vi.fn() }
})

import { nextTick } from 'vue'
import { useConnectionOverlay, RECONNECT_OVERLAY_DELAY_MS } from '@/composables/useConnectionOverlay'

describe('useConnectionOverlay', () => {
    beforeEach(() => {
        wsStatusRef.value = 'connected'
        hasConnectedOnceRef.value = false
        restartingOverlayRef.value = false
        vi.useFakeTimers()
    })

    afterEach(() => {
        vi.useRealTimers()
    })

    function make() {
        return useConnectionOverlay()
    }

    // Vue's default watch flush is async — always await nextTick() after mutating a ref
    // so the watcher has run (and scheduled/cleared the timer) before advancing time.

    it('mode is null when connected', () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        expect(overlay.mode.value).toBeNull()
    })

    it('shows reconnect mode after 1.5s of disconnect once connected before', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        wsStatusRef.value = 'reconnecting'
        await nextTick()
        expect(overlay.mode.value).toBeNull()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBe('reconnect')
    })

    it('does NOT show on cold start (never connected before)', async () => {
        const overlay = make()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBeNull()
    })

    it('does NOT show when reconnected within the 5s window', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS - 100)
        wsStatusRef.value = 'connected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS)
        expect(overlay.mode.value).toBeNull()
    })

    it('clears reconnect mode when back to connected', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBe('reconnect')
        wsStatusRef.value = 'connected'
        await nextTick()
        expect(overlay.mode.value).toBeNull()
    })

    it('shows restart mode immediately (no delay) and takes priority over reconnect', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        wsStatusRef.value = 'reconnecting'
        restartingOverlayRef.value = true
        // mode reads restartingOverlay directly — no flush needed
        expect(overlay.mode.value).toBe('restart')
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBe('restart')
    })

    it('hides restart mode when restartingOverlay goes false', async () => {
        const overlay = make()
        restartingOverlayRef.value = true
        expect(overlay.mode.value).toBe('restart')
        restartingOverlayRef.value = false
        expect(overlay.mode.value).toBeNull()
    })

    it('resets timer on foreground event so overlay does not flash immediately', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        // Disconnect triggers the 5s timer
        wsStatusRef.value = 'disconnected'
        await nextTick()
        // Advance 4s — timer has 1s remaining
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS - 1000)
        expect(overlay.mode.value).toBeNull()
        // Foreground event resets the timer from zero
        window.dispatchEvent(new CustomEvent('clawbench-foreground'))
        await nextTick()
        // The old 1s remainder is gone — overlay still hidden
        await vi.advanceTimersByTimeAsync(1500)
        expect(overlay.mode.value).toBeNull()
        // Only after the full fresh 5s does the overlay appear
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS - 1500 + 100)
        expect(overlay.mode.value).toBe('reconnect')
    })

    it('foreground event clears an already-visible reconnect overlay', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        wsStatusRef.value = 'disconnected'
        await nextTick()
        // Let the timer expire — overlay is showing
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBe('reconnect')
        // Foreground resets: overlay hidden, fresh 5s timer started
        window.dispatchEvent(new CustomEvent('clawbench-foreground'))
        await nextTick()
        expect(overlay.mode.value).toBeNull()
        // Overlay re-appears only after another full 5s
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBe('reconnect')
    })

    it('foreground event does nothing when WS is connected', async () => {
        hasConnectedOnceRef.value = true
        const overlay = make()
        expect(overlay.mode.value).toBeNull()
        window.dispatchEvent(new CustomEvent('clawbench-foreground'))
        await nextTick()
        expect(overlay.mode.value).toBeNull()
        // Advance time — no timer was started, overlay stays hidden
        await vi.advanceTimersByTimeAsync(RECONNECT_OVERLAY_DELAY_MS + 100)
        expect(overlay.mode.value).toBeNull()
    })
})
