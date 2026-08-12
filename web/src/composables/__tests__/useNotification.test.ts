import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// ── Setup: Mock the Notification API and browser globals ──

const mockNotificationInstances: any[] = []
let mockNotificationPermission: NotificationPermission = 'default'
let mockRequestPermissionResult: NotificationPermission = 'granted'

class MockNotification {
    title: string
    options: any
    onclick: (() => void) | null = null
    onclose: (() => void) | null = null
    static permission: NotificationPermission = mockNotificationPermission
    static requestPermission = vi.fn(async () => {
        mockNotificationPermission = mockRequestPermissionResult
        MockNotification.permission = mockRequestPermissionResult
        return mockRequestPermissionResult
    })

    constructor(title: string, options?: any) {
        this.title = title
        this.options = options
        mockNotificationInstances.push(this)
    }
    close() {
        if (this.onclose) this.onclose()
    }
}

// Mock useToast
vi.mock('@/composables/useToast', () => ({
    useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useLocale', () => ({
    gt: (key: string) => key,
}))

describe('useNotification', () => {
    beforeEach(() => {
        mockNotificationInstances.length = 0
        mockNotificationPermission = 'default'
        mockRequestPermissionResult = 'granted'
        MockNotification.permission = 'default'
        MockNotification.requestPermission.mockClear()
    })

    afterEach(() => {
        vi.restoreAllMocks()
        delete (window as any).ClawBenchNative
    })

    // ── requestNotificationPermission ──

    describe('requestNotificationPermission', () => {
        it('returns "denied" when Notification API is not available', async () => {
            // Temporarily remove Notification from window
            const origNotification = (globalThis as any).Notification
            delete (globalThis as any).Notification

            const { requestNotificationPermission } = await import('@/composables/useNotification')
            const result = await requestNotificationPermission()
            expect(result).toBe('denied')

            // Restore
            ;(globalThis as any).Notification = origNotification
        })

        it('returns "granted" immediately if already granted', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            const { requestNotificationPermission } = await import('@/composables/useNotification')
            const result = await requestNotificationPermission()

            expect(result).toBe('granted')
            expect(MockNotification.requestPermission).not.toHaveBeenCalled()
        })

        it('requests permission when current state is "default"', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'default'
            mockRequestPermissionResult = 'granted'

            const { requestNotificationPermission } = await import('@/composables/useNotification')
            const result = await requestNotificationPermission()

            expect(MockNotification.requestPermission).toHaveBeenCalled()
            expect(result).toBe('granted')
        })

        it('returns "denied" when permission is already denied', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'denied'

            const { requestNotificationPermission } = await import('@/composables/useNotification')
            const result = await requestNotificationPermission()

            expect(result).toBe('denied')
            expect(MockNotification.requestPermission).not.toHaveBeenCalled()
        })

        it('passes through the result when user grants', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'default'
            mockRequestPermissionResult = 'granted'

            const { requestNotificationPermission } = await import('@/composables/useNotification')
            const result = await requestNotificationPermission()

            expect(result).toBe('granted')
        })

        it('passes through the result when user denies', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'default'
            mockRequestPermissionResult = 'denied'

            const { requestNotificationPermission } = await import('@/composables/useNotification')
            const result = await requestNotificationPermission()

            expect(result).toBe('denied')
        })
    })

    // ── showBrowserNotification ──

    describe('showBrowserNotification', () => {
        it('does not create notification when page is visible and focused', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            // Mock document to be visible and focused
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            vi.spyOn(document, 'hasFocus').mockReturnValue(true)

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test')

            expect(mockNotificationInstances).toHaveLength(0)
        })

        it('creates notification when page is not visible', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test Title', { body: 'Test body' })

            expect(mockNotificationInstances).toHaveLength(1)
            expect(mockNotificationInstances[0].title).toBe('Test Title')
            expect(mockNotificationInstances[0].options.body).toBe('Test body')
        })

        it('creates notification when page is visible but not focused', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test')

            expect(mockNotificationInstances).toHaveLength(1)
        })

        it('does not create notification when Notification API is not available', async () => {
            delete (globalThis as any).Notification

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test')

            expect(mockNotificationInstances).toHaveLength(0)

            // Restore
            ;(globalThis as any).Notification = MockNotification
        })

        it('does not create notification when permission is not granted', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'denied'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test')

            expect(mockNotificationInstances).toHaveLength(0)
        })

        it('sets default icon and badge when not provided', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test')

            expect(mockNotificationInstances[0].options.icon).toBe('/assets/favicon.png')
            expect(mockNotificationInstances[0].options.badge).toBe('/assets/favicon.png')
        })

        it('uses custom icon and badge when provided', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test', { icon: '/custom.png', badge: '/badge.png' })

            expect(mockNotificationInstances[0].options.icon).toBe('/custom.png')
            expect(mockNotificationInstances[0].options.badge).toBe('/badge.png')
        })

        it('generates unique tag when not provided', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            // Mock Date.now to return different values
            let timeVal = 1000
            vi.spyOn(Date, 'now').mockImplementation(() => timeVal++)

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test1')
            showBrowserNotification('Test2')

            const tags = mockNotificationInstances.map(n => n.options.tag)
            expect(tags[0]).not.toBe(tags[1])
            expect(tags[0]).toContain('clawbench-')
        })

        it('uses custom tag when provided', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test', { tag: 'my-tag' })

            expect(mockNotificationInstances[0].options.tag).toBe('my-tag')
        })

        it('handles onClick callback', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const onClick = vi.fn()
            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Test', { onClick })

            // Simulate click
            const notification = mockNotificationInstances[0]
            expect(notification.onclick).toBeDefined()
            notification.onclick!()

            expect(onClick).toHaveBeenCalled()
        })

        it('tracks notification in active set and removes on close', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const { showBrowserNotification, closeAllNotifications } = await import('@/composables/useNotification')
            showBrowserNotification('Test')

            expect(mockNotificationInstances).toHaveLength(1)

            // The notification should have an onclose handler
            const notification = mockNotificationInstances[0]
            expect(notification.onclose).toBeDefined()

            // closeAll should call close on the notification
            const closeSpy = vi.spyOn(notification, 'close')
            closeAllNotifications()
            expect(closeSpy).toHaveBeenCalled()
        })
    })

    // ── native host path ──

    describe('showBrowserNotification native host', () => {
        it('routes to nativeNotify when a native host with nativeNotify is present', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const nativeNotify = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { nativeNotify }

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Native Title', { body: 'Native body', nav: 'settings' })

            expect(nativeNotify).toHaveBeenCalledWith('Native Title', 'Native body', 'settings')
            // Native path should not create a browser Notification instance
            expect(mockNotificationInstances).toHaveLength(0)
        })

        it('routes to nativeNotify with empty body and no nav when omitted', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'denied'
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const nativeNotify = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { nativeNotify }

            const { showBrowserNotification } = await import('@/composables/useNotification')
            showBrowserNotification('Native Title')

            expect(nativeNotify).toHaveBeenCalledWith('Native Title', '', undefined)
        })

        it('ignores nativeNotify rejection', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const nativeNotify = vi.fn().mockRejectedValue(new Error('native failed'))
            ;(window as any).ClawBenchNative = { nativeNotify }

            const { showBrowserNotification } = await import('@/composables/useNotification')
            expect(() => showBrowserNotification('Native Title')).not.toThrow()
            await Promise.resolve()
            expect(nativeNotify).toHaveBeenCalled()
        })
    })

    // ── closeAllNotifications ──

    describe('closeAllNotifications', () => {
        it('does not throw when no active notifications', async () => {
            const { closeAllNotifications } = await import('@/composables/useNotification')
            expect(() => closeAllNotifications()).not.toThrow()
        })

        it('closes all active notifications', async () => {
            ;(globalThis as any).Notification = MockNotification
            MockNotification.permission = 'granted'

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
            vi.spyOn(document, 'hasFocus').mockReturnValue(false)

            const { showBrowserNotification, closeAllNotifications } = await import('@/composables/useNotification')
            showBrowserNotification('Test1')
            showBrowserNotification('Test2')

            expect(mockNotificationInstances).toHaveLength(2)

            const closeSpies = mockNotificationInstances.map(n => vi.spyOn(n, 'close'))
            closeAllNotifications()

            for (const spy of closeSpies) {
                expect(spy).toHaveBeenCalled()
            }
        })
    })

    // ── useNotification composable ──

    describe('useNotification composable', () => {
        it('exposes all functions and reactive permission', async () => {
            ;(globalThis as any).Notification = MockNotification

            const { useNotification } = await import('@/composables/useNotification')
            const composable = useNotification()

            expect(composable.permission).toBeDefined()
            expect(typeof composable.requestPermission).toBe('function')
            expect(typeof composable.show).toBe('function')
            expect(typeof composable.closeAll).toBe('function')
        })
    })
})
