import { describe, expect, it, vi, beforeEach } from 'vitest'
import { nextTick } from 'vue'

// ── createSelectState ───────────────────────────────────────────────────────

// We import after the mock setup
import { createSelectState } from '@/composables/useSelectState'

describe('createSelectState', () => {
    const mockLoadPref = vi.fn().mockReturnValue(null)

    beforeEach(() => {
        vi.clearAllMocks()
        mockLoadPref.mockReturnValue(null)
    })

    it('initializes with empty state', () => {
        const state = createSelectState('mode', mockLoadPref)
        expect(state.currentId.value).toBe('')
        expect(state.currentName.value).toBe('')
        expect(state.available.value).toEqual([])
    })

    // ── update ──

    describe('update', () => {
        it('sets currentId and resolves currentName from available items', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.update('code', [{ id: 'ask', name: 'Ask' }, { id: 'code', name: 'Code' }])

            expect(state.currentId.value).toBe('code')
            expect(state.currentName.value).toBe('Code')
            expect(state.available.value).toEqual([
                { id: 'ask', name: 'Ask' },
                { id: 'code', name: 'Code' },
            ])
        })

        it('uses id as fallback name when item not in list', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.update('architect', [{ id: 'ask', name: 'Ask' }])

            expect(state.currentId.value).toBe('architect')
            expect(state.currentName.value).toBe('architect')
        })

        it('does not update currentId when id is empty', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.update('code', [{ id: 'code', name: 'Code' }])
            state.update('', [{ id: 'ask', name: 'Ask' }])

            expect(state.currentId.value).toBe('code')
            expect(state.available.value).toEqual([{ id: 'ask', name: 'Ask' }])
        })

        it('does not update available when array is empty', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.update('code', [{ id: 'code', name: 'Code' }])
            state.update('ask', [])

            expect(state.available.value).toEqual([{ id: 'code', name: 'Code' }])
        })
    })

    // ── updateAvailable ──

    describe('updateAvailable', () => {
        it('updates available without changing currentId', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.update('code', [{ id: 'code', name: 'Code' }])
            state.updateAvailable([
                { id: 'ask', name: 'Ask' },
                { id: 'code', name: 'Code' },
                { id: 'architect', name: 'Architect' },
            ])

            expect(state.currentId.value).toBe('code')
            expect(state.currentName.value).toBe('Code')
            expect(state.available.value).toHaveLength(3)
        })

        it('resolves currentName when currentId was set before available items arrived', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.currentId.value = 'architect'
            state.currentName.value = ''
            state.updateAvailable([
                { id: 'ask', name: 'Ask' },
                { id: 'architect', name: 'Architect' },
            ])

            expect(state.currentName.value).toBe('Architect')
        })

        it('does not update when items array is empty', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.update('code', [{ id: 'code', name: 'Code' }])
            state.updateAvailable([])

            expect(state.available.value).toEqual([{ id: 'code', name: 'Code' }])
        })
    })

    // ── clear ──

    describe('clear', () => {
        it('resets all state to empty', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.update('code', [{ id: 'code', name: 'Code' }])

            state.clear()

            expect(state.currentId.value).toBe('')
            expect(state.currentName.value).toBe('')
            expect(state.available.value).toEqual([])
        })

        it('clears currentId too (fixes clearThinkingEffortState bug)', () => {
            const state = createSelectState('thought_level', mockLoadPref)
            state.update('high', [{ id: 'high', name: 'High' }])

            state.clear()

            // Bug fix: clear must also reset currentId, not just available and currentName
            expect(state.currentId.value).toBe('')
            expect(state.currentName.value).toBe('')
            expect(state.available.value).toEqual([])
        })
    })

    // ── syncFromData ──

    describe('syncFromData', () => {
        it('updates currentId and available from server data', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.syncFromData('code', [{ id: 'ask', name: 'Ask' }, { id: 'code', name: 'Code' }])

            expect(state.currentId.value).toBe('code')
            expect(state.currentName.value).toBe('Code')
            expect(state.available.value).toHaveLength(2)
        })

        it('resolves name from available items', () => {
            const state = createSelectState('thought_level', mockLoadPref)
            state.syncFromData('high', [
                { id: 'low', name: 'Low' },
                { id: 'high', name: 'High' },
            ])

            expect(state.currentName.value).toBe('High')
        })

        it('does not overwrite currentId when server value is empty but local value exists', () => {
            const state = createSelectState('thought_level', mockLoadPref)
            state.update('high', [{ id: 'high', name: 'High' }])
            state.syncFromData('', [{ id: 'low', name: 'Low' }, { id: 'high', name: 'High' }])

            // Guard: if server returns empty currentId, keep the user's selection
            expect(state.currentId.value).toBe('high')
        })

        it('updates available even when server currentId is empty', () => {
            const state = createSelectState('thought_level', mockLoadPref)
            state.syncFromData('', [{ id: 'low', name: 'Low' }])

            expect(state.available.value).toEqual([{ id: 'low', name: 'Low' }])
        })
    })

    // ── loadPref ──

    describe('loadPref', () => {
        it('loads preference from loadPrefFn', () => {
            mockLoadPref.mockReturnValue('saved-mode')
            const state = createSelectState('mode', mockLoadPref)

            state.loadPref('agent-1')

            expect(mockLoadPref).toHaveBeenCalledWith('agent-1')
            expect(state.currentId.value).toBe('saved-mode')
        })

        it('does not overwrite existing currentId when pref returns null', () => {
            mockLoadPref.mockReturnValue(null)
            const state = createSelectState('mode', mockLoadPref)
            state.update('code', [{ id: 'code', name: 'Code' }])

            state.loadPref('agent-1')

            expect(state.currentId.value).toBe('code')
        })

        it('does nothing when agentId is empty', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.loadPref('')

            expect(mockLoadPref).not.toHaveBeenCalled()
        })
    })

    // ── category ──

    describe('category', () => {
        it('stores the category name', () => {
            const modeState = createSelectState('mode', mockLoadPref)
            const effortState = createSelectState('thought_level', mockLoadPref)

            expect(modeState.category).toBe('mode')
            expect(effortState.category).toBe('thought_level')
        })
    })

    // ── syncAndFallback ──

    describe('syncAndFallback', () => {
        it('uses server value when present', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.syncAndFallback('code', [{ id: 'code', name: 'Code' }], 'agent-1')

            expect(state.currentId.value).toBe('code')
            expect(state.currentName.value).toBe('Code')
        })

        it('falls back to loadPref when server value is empty and currentId is empty', () => {
            mockLoadPref.mockReturnValue('saved-mode')
            const state = createSelectState('mode', mockLoadPref)

            state.syncAndFallback('', [], 'agent-1')

            expect(state.currentId.value).toBe('saved-mode')
            expect(mockLoadPref).toHaveBeenCalledWith('agent-1')
        })

        it('resolves name from available after fallback', () => {
            mockLoadPref.mockReturnValue('code')
            const state = createSelectState('mode', mockLoadPref)

            state.syncAndFallback('', [{ id: 'code', name: 'Code' }], 'agent-1')

            expect(state.currentId.value).toBe('code')
            expect(state.currentName.value).toBe('Code')
        })

        it('uses id as name when not in available after fallback', () => {
            mockLoadPref.mockReturnValue('architect')
            const state = createSelectState('mode', mockLoadPref)

            state.syncAndFallback('', [], 'agent-1')

            expect(state.currentId.value).toBe('architect')
            expect(state.currentName.value).toBe('architect')
        })

        it('clears name when both server and pref are empty', () => {
            mockLoadPref.mockReturnValue(null)
            const state = createSelectState('mode', mockLoadPref)
            state.update('code', [{ id: 'code', name: 'Code' }])
            state.clear()

            state.syncAndFallback('', [], 'agent-1')

            expect(state.currentId.value).toBe('')
            expect(state.currentName.value).toBe('')
        })

        it('does not call loadPref when server value is present', () => {
            const state = createSelectState('mode', mockLoadPref)
            state.syncAndFallback('code', [{ id: 'code', name: 'Code' }], 'agent-1')

            expect(mockLoadPref).not.toHaveBeenCalled()
        })
    })
})
