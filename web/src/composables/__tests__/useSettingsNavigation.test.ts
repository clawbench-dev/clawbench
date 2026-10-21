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

    // ── returnToCategory (detail page → its list) ──

    describe('returnToCategory', () => {
        it('pops the detail entry back to its list (the delete-after-navigate bug)', () => {
            const { pushNav, returnToCategory, navStack, currentCategory } = useSettingsNavigation()

            pushNav('project')
            pushNav('project:42')
            returnToCategory('project')

            // Must NOT leave ['project','project:42','project'].
            expect(navStack.value).toEqual(['project'])
            expect(currentCategory.value).toBe('project')
        })

        it('lands on the list when the detail was deep-linked (nothing to pop)', () => {
            const { pushNav, returnToCategory, navStack, currentCategory } = useSettingsNavigation()

            // Deep-link straight into a detail page: no list beneath it. After
            // the detail goes away the trail must be 设置 › 项目, not still show
            // the (now deleted) detail.
            pushNav('project:42')
            returnToCategory('project')

            expect(navStack.value).toEqual(['project'])
            expect(currentCategory.value).toBe('project')
        })

        it('works for agents detail too', () => {
            const { pushNav, returnToCategory, navStack, currentCategory } = useSettingsNavigation()

            pushNav('agents')
            pushNav('agents:abc')
            returnToCategory('agents')

            expect(navStack.value).toEqual(['agents'])
            expect(currentCategory.value).toBe('agents')
        })

        it('does not pop an unrelated detail entry', () => {
            const { pushNav, returnToCategory, navStack, currentCategory } = useSettingsNavigation()

            pushNav('project')
            pushNav('project:42')
            // A stale cross-category call must not pop the project detail.
            returnToCategory('agents')

            expect(navStack.value).toEqual(['project', 'project:42', 'agents'])
            expect(currentCategory.value).toBe('agents')
        })
    })

    // ── replaceTopNav (sibling detail hand-off, e.g. agent copy) ──

    describe('replaceTopNav', () => {
        it('swaps the top entry without growing the stack (copy → new agent)', () => {
            const { pushNav, replaceTopNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('agents')
            pushNav('agents:source')
            replaceTopNav('agents:copy')

            // Depth stays 2 — the breadcrumb must not read
            // 设置 › 源智能体 › 副本.
            expect(navStack.value).toEqual(['agents', 'agents:copy'])
            expect(currentCategory.value).toBe('agents:copy')
        })

        it('replaces a deep-linked detail with no list beneath it', () => {
            const { pushNav, replaceTopNav, navStack, currentCategory } = useSettingsNavigation()

            pushNav('agents:source')
            replaceTopNav('agents:copy')

            expect(navStack.value).toEqual(['agents:copy'])
            expect(currentCategory.value).toBe('agents:copy')
        })

        it('pushes when the stack is empty', () => {
            const { replaceTopNav, navStack, currentCategory } = useSettingsNavigation()

            replaceTopNav('agents:copy')

            expect(navStack.value).toEqual(['agents:copy'])
            expect(currentCategory.value).toBe('agents:copy')
        })

        it('preserves the array identity (mutates in place)', () => {
            const { pushNav, replaceTopNav, navStack } = useSettingsNavigation()

            pushNav('agents')
            pushNav('agents:source')
            const before = navStack.value
            replaceTopNav('agents:copy')

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
        /** Stub fetch: /api/server/instance answers `id`; any other call fails. */
        function stubInstance(id: string) {
            return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
                if (String(input).includes('/api/server/instance')) {
                    return { ok: true, json: async () => ({ instance_id: id }) } as Response
                }
                throw new Error('not up')
            })
        }

        it('hides dialog, sets restarting, calls restartServer', async () => {
            mockRestartServer.mockResolvedValue(undefined)
            stubInstance('inst-a')

            const nav = useSettingsNavigation()
            nav.restartDialogVisible.value = true

            await nav.handleRestart()

            expect(nav.restartDialogVisible.value).toBe(false)
            expect(nav.restarting.value).toBe(true)
            expect(mockRestartServer).toHaveBeenCalled()
        })

        it('reports the server error when the restart is refused', async () => {
            // Regression: a refused restart used to be swallowed, leaving the
            // user believing it was underway.
            mockRestartServer.mockRejectedValue(new Error('running binary is not accessible'))
            stubInstance('inst-a')

            const nav = useSettingsNavigation()
            nav.needsRestart.value = true

            await nav.handleRestart()

            expect(nav.restarting.value).toBe(false)
            expect(nav.needsRestart.value).toBe(true)
            // Shown VERBATIM: apiPost's message is already localized and
            // already says "restart failed", so re-wrapping it produced
            // "重启失败：重启失败：…".
            expect(mockToastShow).toHaveBeenCalledWith(
                'running binary is not accessible',
                expect.objectContaining({ type: 'error' }),
            )
        })

        it('falls back to a generic message when the error carries no text', async () => {
            mockRestartServer.mockRejectedValue(new Error(''))
            stubInstance('inst-a')

            const nav = useSettingsNavigation()
            await nav.handleRestart()

            expect(mockToastShow).toHaveBeenCalledWith(
                'settings.restartFailed',
                expect.objectContaining({ type: 'error' }),
            )
        })

        it('does not start polling when the restart is refused', async () => {
            mockRestartServer.mockRejectedValue(new Error('boom'))
            stubInstance('inst-a')

            const nav = useSettingsNavigation()
            await nav.handleRestart()

            // A poll would keep the overlay up and eventually reload the page
            // onto the unchanged process.
            expect(restartingOverlay.value).toBe(false)
            await vi.advanceTimersByTimeAsync(4000)
            expect(mockToastShow).not.toHaveBeenCalledWith(
                'settings.restartTimeout',
                expect.anything(),
            )
        })
    })

    // ── pollUntilServerUp ──

    describe('pollUntilServerUp', () => {
        /** Stub fetch so /api/server/instance returns `id` once `up` is true. */
        function stubInstanceSequence(ids: Array<string | null>) {
            let i = 0
            return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
                if (!String(input).includes('/api/server/instance')) throw new Error('not up')
                const id = ids[Math.min(i++, ids.length - 1)]
                if (id === null) throw new Error('Connection refused')
                return { ok: true, json: async () => ({ instance_id: id }) } as Response
            })
        }

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

        it('reloads once a DIFFERENT instance answers', async () => {
            mockRestartServer.mockResolvedValue(undefined)

            // Pre-restart probe → inst-a; then the server goes down for two
            // polls and comes back as inst-b.
            stubInstanceSequence(['inst-a', null, null, 'inst-b'])

            const reloadMock = vi.fn()
            Object.defineProperty(window, 'location', {
                value: { reload: reloadMock },
                writable: true,
            })

            const { handleRestart, restartingOverlay } = useSettingsNavigation()

            await handleRestart()

            for (let i = 0; i < 5; i++) {
                await vi.advanceTimersByTimeAsync(2000)
            }

            expect(mockToastShow).not.toHaveBeenCalled()
            expect(reloadMock).toHaveBeenCalled()
            expect(restartingOverlay.value).toBe(false)
        })

        it('does NOT reload while the SAME instance keeps answering', async () => {
            // The reported bug: the outgoing process still answers until
            // srv.Shutdown, so an early poll sees 200 from the old process and
            // the page reloaded onto the same build.
            mockRestartServer.mockResolvedValue(undefined)
            stubInstanceSequence(['inst-a']) // always the same id

            const reloadMock = vi.fn()
            Object.defineProperty(window, 'location', {
                value: { reload: reloadMock },
                writable: true,
            })

            const { handleRestart, restartingOverlay, restarting } = useSettingsNavigation()

            await handleRestart()

            // Long enough to exceed STALE_INSTANCE_LIMIT polls.
            for (let i = 0; i < 17; i++) {
                await vi.advanceTimersByTimeAsync(2000)
            }

            expect(reloadMock).not.toHaveBeenCalled()
            expect(mockToastShow).toHaveBeenCalledWith(
                'settings.restartNotApplied',
                expect.objectContaining({ type: 'error' }),
            )
            expect(restartingOverlay.value).toBe(false)
            expect(restarting.value).toBe(false)
        })

        it('falls back to accepting the first 200 when no baseline was read', async () => {
            // The pre-restart probe failed (server briefly unreachable): we
            // cannot compare ids, so accept the first successful answer rather
            // than hanging until the timeout.
            mockRestartServer.mockResolvedValue(undefined)
            let instanceCalls = 0
            vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
                if (!String(input).includes('/api/server/instance')) throw new Error('not up')
                instanceCalls++
                if (instanceCalls === 1) throw new Error('probe failed') // baseline
                return { ok: true, json: async () => ({ instance_id: 'inst-b' }) } as Response
            })

            const reloadMock = vi.fn()
            Object.defineProperty(window, 'location', {
                value: { reload: reloadMock },
                writable: true,
            })

            const { handleRestart } = useSettingsNavigation()
            await handleRestart()

            await vi.advanceTimersByTimeAsync(2000)
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
            expect(typeof nav.replaceTopNav).toBe('function')
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
