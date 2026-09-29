import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import {
  restartingOverlay,
  useSettingsNavigation,
  setPendingSettingsCategory,
  consumePendingSettingsCategory,
  pendingSettingsCategory,
} from '@/composables/useSettingsNavigation'

// Mock dependencies
const mockLoadConfig = vi.fn()
const mockRestartServer = vi.fn()
const mockToastShow = vi.fn()

vi.mock('@/composables/useSettingsConfig', () => ({
    useSettingsConfig: () => ({
        loadConfig: mockLoadConfig,
        restartServer: mockRestartServer,
    }),
}))

vi.mock('@/composables/useToast', () => ({
    useToast: () => ({
        show: mockToastShow,
    }),
}))

vi.mock('vue-i18n', () => ({
    useI18n: () => ({
        t: (key: string) => key,
    }),
}))

// Mock vue's onUnmounted to be a no-op in tests
vi.mock('vue', async () => {
    const actual = await vi.importActual('vue')
    return {
        ...actual,
        onUnmounted: vi.fn(),
    }
})

describe('useSettingsNavigation', () => {
    beforeEach(() => {
        vi.clearAllMocks()
        vi.useFakeTimers()
        restartingOverlay.value = false
    })

    afterEach(() => {
        vi.useRealTimers()
    })

    // ── pushNav / popNav ──

    describe('pushNav / popNav', () => {
        it('pushes category onto nav stack and updates currentCategory', () => {
            const { pushNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('general')

            expect(navStack.value).toEqual(['general'])
            expect(currentCategory.value).toBe('general')
        })

        it('pushes multiple categories', () => {
            const { pushNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('general')
            pushNav('advanced')

            expect(navStack.value).toEqual(['general', 'advanced'])
            expect(currentCategory.value).toBe('advanced')
        })

        it('pops category and updates currentCategory to previous', () => {
            const { pushNav, popNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('general')
            pushNav('advanced')
            popNav()

            expect(navStack.value).toEqual(['general'])
            expect(currentCategory.value).toBe('general')
        })

        it('pops to empty stack sets currentCategory to null', () => {
            const { pushNav, popNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('general')
            popNav()

            expect(navStack.value).toEqual([])
            expect(currentCategory.value).toBeNull()
        })

        it('popNav on empty stack does nothing', () => {
            const { popNav, navStack, currentCategory } = useSettingsNavigation()

            popNav()

            expect(navStack.value).toEqual([])
            expect(currentCategory.value).toBeNull()
        })
    })

    // ── truncateNav (breadcrumb jumps) ──

    describe('truncateNav', () => {
        it('truncates to the given depth and updates currentCategory', () => {
            const { pushNav, truncateNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('tts')
            pushNav('tts:tts_engine')
            truncateNav(1)

            expect(navStack.value).toEqual(['tts'])
            expect(currentCategory.value).toBe('tts')
        })

        it('depth 0 returns to the index', () => {
            const { pushNav, truncateNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('tts')
            pushNav('tts:tts_engine')
            truncateNav(0)

            expect(navStack.value).toEqual([])
            expect(currentCategory.value).toBeNull()
        })

        it('clamps a depth beyond the stack length (never pushes deeper)', () => {
            const { pushNav, truncateNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('general')
            truncateNav(99)

            expect(navStack.value).toEqual(['general'])
            expect(currentCategory.value).toBe('general')
        })

        it('clamps a negative depth to 0', () => {
            const { pushNav, truncateNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('general')
            truncateNav(-5)

            expect(navStack.value).toEqual([])
            expect(currentCategory.value).toBeNull()
        })

        it('is a no-op when already at the requested depth', () => {
            const { pushNav, truncateNav, navStack } = useSettingsNavigation()

            pushNav('general')
            const before = navStack.value
            truncateNav(1)

            expect(navStack.value).toEqual(['general'])
            // Identity preserved — the array was not replaced.
            expect(navStack.value).toBe(before)
        })
    })

    // ── resetState ──

    describe('resetState', () => {
        it('resets all state to defaults', () => {
            const nav = useSettingsNavigation()

            nav.pushNav('general')
            nav.needsRestart.value = true
            nav.restarting.value = true
            nav.restartingOverlay.value = true
            nav.restartDialogVisible.value = true
            nav.changedColdFields.value = ['field1']

            nav.resetState()

            expect(nav.navStack.value).toEqual([])
            expect(nav.currentCategory.value).toBeNull()
            expect(nav.needsRestart.value).toBe(false)
            expect(nav.restarting.value).toBe(false)
            expect(nav.restartingOverlay.value).toBe(false)
            expect(nav.restartDialogVisible.value).toBe(false)
            expect(nav.changedColdFields.value).toEqual([])
        })
    })

    // ── handleRestartNeeded ──

    describe('handleRestartNeeded', () => {
        it('sets changedColdFields, needsRestart, and shows dialog', () => {
            const nav = useSettingsNavigation()

            nav.handleRestartNeeded(['port', 'host'])

            expect(nav.changedColdFields.value).toEqual(['port', 'host'])
            expect(nav.needsRestart.value).toBe(true)
            expect(nav.restartDialogVisible.value).toBe(true)
        })

        it('can be called multiple times, updating fields', () => {
            const nav = useSettingsNavigation()

            nav.handleRestartNeeded(['port'])
            nav.handleRestartNeeded(['host'])

            expect(nav.changedColdFields.value).toEqual(['host'])
            expect(nav.needsRestart.value).toBe(true)
        })
    })

    // ── handleRestart ──

    describe('handleRestart', () => {
        it('hides dialog, sets restarting, calls restartServer', async () => {
            mockRestartServer.mockResolvedValue(undefined)
            // Mock fetch for the polling phase (make it fail immediately for quick test)
            vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('not up'))

            const nav = useSettingsNavigation()
            nav.restartDialogVisible.value = true

            await nav.handleRestart()

            expect(nav.restartDialogVisible.value).toBe(false)
            expect(nav.restarting.value).toBe(true)
            expect(mockRestartServer).toHaveBeenCalled()
        })

        it('sets restarting to false when restartServer throws', async () => {
            mockRestartServer.mockRejectedValue(new Error('restart failed'))

            const nav = useSettingsNavigation()
            nav.needsRestart.value = true

            await nav.handleRestart()

            expect(nav.restarting.value).toBe(false)
        })
    })

    // ── pollUntilServerUp ──

    describe('pollUntilServerUp timeout', () => {
        it('shows error toast when polling times out', async () => {
            mockRestartServer.mockResolvedValue(undefined)

            // Mock fetch to always fail (server never comes back)
            vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('Connection refused'))

            const { handleRestart, restartingOverlay, restarting } = useSettingsNavigation()

            await handleRestart()

            // Fast-forward through all poll attempts (60 attempts × 2s = 120s)
            for (let i = 0; i < 60; i++) {
                await vi.advanceTimersByTimeAsync(2000)
            }

            // Should have shown error toast
            expect(mockToastShow).toHaveBeenCalledWith(
                'settings.restartTimeout',
                expect.objectContaining({ type: 'error' }),
            )

            expect(restartingOverlay.value).toBe(false)
            expect(restarting.value).toBe(false)
        })

        it('clears overlay and reloading when server comes back', async () => {
            mockRestartServer.mockResolvedValue(undefined)

            // Mock fetch to succeed on the 3rd attempt
            let fetchCount = 0
            vi.spyOn(globalThis, 'fetch').mockImplementation(async () => {
                fetchCount++
                if (fetchCount >= 3) {
                    return { ok: true } as Response
                }
                throw new Error('Connection refused')
            })

            // Mock window.location.reload
            const reloadMock = vi.fn()
            Object.defineProperty(window, 'location', {
                value: { reload: reloadMock },
                writable: true,
            })

            const { handleRestart, restartingOverlay } = useSettingsNavigation()

            await handleRestart()

            // Advance through polls
            for (let i = 0; i < 5; i++) {
                await vi.advanceTimersByTimeAsync(2000)
            }

            // Should have reloaded, not shown error toast
            expect(mockToastShow).not.toHaveBeenCalled()
            expect(reloadMock).toHaveBeenCalled()
        })
    })

    // ── returned values ──

    describe('returned values', () => {
        it('exposes all expected properties', () => {
            const nav = useSettingsNavigation()

            expect(nav.navStack).toBeDefined()
            expect(nav.currentCategory).toBeDefined()
            expect(nav.restartDialogVisible).toBeDefined()
            expect(nav.changedColdFields).toBeDefined()
            expect(nav.needsRestart).toBeDefined()
            expect(nav.restarting).toBeDefined()
            expect(nav.restartingOverlay).toBeDefined()
            expect(typeof nav.pushNav).toBe('function')
            expect(typeof nav.popNav).toBe('function')
            expect(typeof nav.resetState).toBe('function')
            expect(typeof nav.handleRestartNeeded).toBe('function')
            expect(typeof nav.handleRestart).toBe('function')
        })
    })

    describe('module-level restartingOverlay', () => {
        it('shares the same ref across multiple useSettingsNavigation() calls', () => {
            const nav1 = useSettingsNavigation()
            const nav2 = useSettingsNavigation()
            expect(nav1.restartingOverlay).toBe(nav2.restartingOverlay)
        })
    })

    // ── pending settings category deep-link ──

    describe('pending settings category deep-link', () => {
        afterEach(() => {
            pendingSettingsCategory.value = null
        })

        it('setPendingSettingsCategory stores the category id', () => {
            setPendingSettingsCategory('appearance')
            expect(pendingSettingsCategory.value).toBe('appearance')
        })

        it('consumePendingSettingsCategory returns and clears the request', () => {
            setPendingSettingsCategory('appearance')
            expect(consumePendingSettingsCategory()).toBe('appearance')
            expect(pendingSettingsCategory.value).toBeNull()
        })

        it('consumePendingSettingsCategory returns null when nothing is pending', () => {
            expect(consumePendingSettingsCategory()).toBeNull()
        })

        it('consume is destructive — second call returns null', () => {
            setPendingSettingsCategory('appearance')
            expect(consumePendingSettingsCategory()).toBe('appearance')
            expect(consumePendingSettingsCategory()).toBeNull()
        })

        it('supports sequential requests (latest wins)', () => {
            setPendingSettingsCategory('appearance')
            setPendingSettingsCategory('terminal')
            expect(consumePendingSettingsCategory()).toBe('terminal')
        })
    })
})
