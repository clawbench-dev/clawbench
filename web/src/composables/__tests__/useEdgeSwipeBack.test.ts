import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { handleBackNavigation, registerBackHandler, canNavigateBack, PRIORITY_PAGE, PRIORITY_OVERLAY } from '../useBackHandler'

// Mock useAppMode — default to web mode (isAppMode = false)
const isAppModeRef = { value: false }
vi.mock('../useAppMode', () => ({
    useAppMode: () => ({ isAppMode: isAppModeRef }),
}))

// Mock vue lifecycle hooks
const mountedCallbacks: (() => void)[] = []
const unmountedCallbacks: (() => void)[] = []

vi.mock('vue', async () => {
    const actual = await vi.importActual('vue')
    return {
        ...actual,
        onMounted: (cb: () => void) => mountedCallbacks.push(cb),
        onBeforeUnmount: (cb: () => void) => unmountedCallbacks.push(cb),
    }
})

// Polyfill Touch for jsdom (not available by default)
class MockTouch {
    identifier: number
    target: EventTarget
    clientX: number
    clientY: number
    pageX: number
    pageY: number
    screenX: number
    screenY: number
    radiusX: number
    radiusY: number
    rotationAngle: number
    force: number

    constructor(init: { identifier: number; target: EventTarget; clientX: number; clientY: number }) {
        this.identifier = init.identifier
        this.target = init.target
        this.clientX = init.clientX
        this.clientY = init.clientY
        this.pageX = init.clientX
        this.pageY = init.clientY
        this.screenX = init.clientX
        this.screenY = init.clientY
        this.radiusX = 0
        this.radiusY = 0
        this.rotationAngle = 0
        this.force = 1
    }
}

// @ts-expect-error polyfill
globalThis.Touch = MockTouch

// Helper to dispatch touch events
function dispatchTouch(type: string, touches: { clientX: number; clientY: number }[]) {
    const mockTouches = touches.map((t, i) =>
        new MockTouch({ identifier: i, target: document, clientX: t.clientX, clientY: t.clientY })
    )
    const event = new TouchEvent(type, {
        touches: mockTouches,
        changedTouches: mockTouches,
    })
    document.dispatchEvent(event)
}

// Track registered handlers to clean up between tests
const cleanupFns: (() => void)[] = []

describe('useEdgeSwipeBack', () => {
    let useEdgeSwipeBack: typeof import('../useEdgeSwipeBack')['useEdgeSwipeBack']

    beforeEach(async () => {
        mountedCallbacks.length = 0
        unmountedCallbacks.length = 0
        const mod = await import('../useEdgeSwipeBack')
        useEdgeSwipeBack = mod.useEdgeSwipeBack
    })

    afterEach(() => {
        unmountedCallbacks.forEach(cb => cb())
        unmountedCallbacks.length = 0
        mountedCallbacks.length = 0
        cleanupFns.forEach(fn => fn())
        cleanupFns.length = 0
    })

    it('dispatches clawbench-back-press event on right-edge left swipe', () => {
        const listener = vi.fn()
        window.addEventListener('clawbench-back-press', listener)

        useEdgeSwipeBack()
        mountedCallbacks.forEach(cb => cb())

        const innerWidth = window.innerWidth
        const startX = innerWidth - 10
        const endX = startX - 80

        dispatchTouch('touchstart', [{ clientX: startX, clientY: 200 }])
        dispatchTouch('touchend', [{ clientX: endX, clientY: 210 }])

        expect(listener).toHaveBeenCalledTimes(1)

        window.removeEventListener('clawbench-back-press', listener)
    })

    it('does not dispatch back-press event on center swipe', () => {
        const listener = vi.fn()
        window.addEventListener('clawbench-back-press', listener)

        useEdgeSwipeBack()
        mountedCallbacks.forEach(cb => cb())

        dispatchTouch('touchstart', [{ clientX: 200, clientY: 200 }])
        dispatchTouch('touchend', [{ clientX: 120, clientY: 210 }])

        expect(listener).not.toHaveBeenCalled()

        window.removeEventListener('clawbench-back-press', listener)
    })

    it('does not dispatch back-press event on left-edge right swipe', () => {
        const listener = vi.fn()
        window.addEventListener('clawbench-back-press', listener)

        useEdgeSwipeBack()
        mountedCallbacks.forEach(cb => cb())

        dispatchTouch('touchstart', [{ clientX: 10, clientY: 200 }])
        dispatchTouch('touchend', [{ clientX: 90, clientY: 210 }])

        expect(listener).not.toHaveBeenCalled()

        window.removeEventListener('clawbench-back-press', listener)
    })

    it('does not dispatch back-press event on short right-edge swipe', () => {
        const listener = vi.fn()
        window.addEventListener('clawbench-back-press', listener)

        useEdgeSwipeBack()
        mountedCallbacks.forEach(cb => cb())

        const innerWidth = window.innerWidth
        const startX = innerWidth - 10
        const endX = startX - 20 // too short

        dispatchTouch('touchstart', [{ clientX: startX, clientY: 200 }])
        dispatchTouch('touchend', [{ clientX: endX, clientY: 200 }])

        expect(listener).not.toHaveBeenCalled()

        window.removeEventListener('clawbench-back-press', listener)
    })

    it('dispatches back-press event even when no handler can go back', () => {
        const listener = vi.fn()
        window.addEventListener('clawbench-back-press', listener)

        useEdgeSwipeBack()
        mountedCallbacks.forEach(cb => cb())

        const innerWidth = window.innerWidth
        const startX = innerWidth - 10
        const endX = startX - 80

        dispatchTouch('touchstart', [{ clientX: startX, clientY: 200 }])
        dispatchTouch('touchend', [{ clientX: endX, clientY: 210 }])

        // In web mode (default), the event is dispatched
        expect(listener).toHaveBeenCalledTimes(1)

        window.removeEventListener('clawbench-back-press', listener)
    })

    it('does not dispatch back-press event in app mode (native handles it)', () => {
        // Switch to app mode
        isAppModeRef.value = true

        const listener = vi.fn()
        window.addEventListener('clawbench-back-press', listener)

        useEdgeSwipeBack()
        mountedCallbacks.forEach(cb => cb())

        const innerWidth = window.innerWidth
        const startX = innerWidth - 10
        const endX = startX - 80

        dispatchTouch('touchstart', [{ clientX: startX, clientY: 200 }])
        dispatchTouch('touchend', [{ clientX: endX, clientY: 210 }])

        // In app mode, JS should NOT dispatch — native handles it
        expect(listener).not.toHaveBeenCalled()

        window.removeEventListener('clawbench-back-press', listener)
        isAppModeRef.value = false // reset
    })

    it('removes event listeners on unmount', () => {
        const removeSpy = vi.spyOn(document, 'removeEventListener')

        useEdgeSwipeBack()
        mountedCallbacks.forEach(cb => cb())
        unmountedCallbacks.forEach(cb => cb())

        expect(removeSpy).toHaveBeenCalledWith('touchstart', expect.any(Function))
        expect(removeSpy).toHaveBeenCalledWith('touchend', expect.any(Function))

        removeSpy.mockRestore()
    })
})

describe('useFeatureBackHandler', () => {
    let useFeatureBackHandler: typeof import('../useEdgeSwipeBack')['useFeatureBackHandler']

    beforeEach(async () => {
        mountedCallbacks.length = 0
        unmountedCallbacks.length = 0
        const mod = await import('../useEdgeSwipeBack')
        useFeatureBackHandler = mod.useFeatureBackHandler
    })

    afterEach(() => {
        unmountedCallbacks.forEach(cb => cb())
        unmountedCallbacks.length = 0
        mountedCallbacks.length = 0
        cleanupFns.forEach(fn => fn())
        cleanupFns.length = 0
    })

    it('registers a back handler on mount', () => {
        const goBack = vi.fn()

        useFeatureBackHandler('test-feature', () => true, goBack, PRIORITY_PAGE)

        // After mount
        mountedCallbacks.forEach(cb => cb())

        expect(handleBackNavigation()).toBe(true)
        expect(goBack).toHaveBeenCalledTimes(1)
    })

    it('unregisters the handler on unmount', () => {
        const goBack = vi.fn()

        useFeatureBackHandler('test-feature', () => true, goBack, PRIORITY_PAGE)
        mountedCallbacks.forEach(cb => cb())
        handleBackNavigation() // consume the first call
        expect(goBack).toHaveBeenCalledTimes(1)

        // After unmount
        unmountedCallbacks.forEach(cb => cb())

        // The handler should no longer be active
        expect(canNavigateBack()).toBe(false)
    })

    it('handler respects canGoBack return value', () => {
        let canReturn = false
        const goBack = vi.fn()

        useFeatureBackHandler('test-feature', () => canReturn, goBack, PRIORITY_PAGE)
        mountedCallbacks.forEach(cb => cb())

        expect(handleBackNavigation()).toBe(false)
        expect(goBack).not.toHaveBeenCalled()

        canReturn = true
        expect(handleBackNavigation()).toBe(true)
        expect(goBack).toHaveBeenCalledTimes(1)
    })
})
