import { describe, it, expect, beforeEach } from 'vitest'
import { nextTick } from 'vue'

import { useTocDockPreference, _resetForTest, TOC_DOCK_MIN_WIDTH, TOC_DOCK_MAX_WIDTH, TOC_DOCK_DEFAULT_WIDTH, TOC_DOCK_DEFAULT_SIDE } from '@/composables/useTocDockPreference'
import { PANEL_KEY_PREFIX } from '@/utils/panelState'

// Key names are owned by the panel-state registry now, so the tests read them
// from there rather than from the composable.
const TOC_DOCK_OPEN_KEY = PANEL_KEY_PREFIX + 'toc-dock-open'
const TOC_DOCK_WIDTH_KEY = PANEL_KEY_PREFIX + 'toc-dock-width'
const TOC_DOCK_SIDE_KEY = PANEL_KEY_PREFIX + 'toc-dock-side'
import { _setWideScreenForTest, _resetForTest as _resetWideForTest } from '@/composables/useWideScreenLayout'
import { useFileEditor, _resetForTesting as _resetEditorForTesting } from '@/composables/useFileEditor'

describe('useTocDockPreference', () => {
    beforeEach(() => {
        _resetForTest()
        _resetWideForTest()
        _resetEditorForTesting()
        _setWideScreenForTest(true)
        localStorage.clear()
    })

    it('starts closed with default width when nothing is persisted', () => {
        const pref = useTocDockPreference()
        expect(pref.tocDockOpen.value).toBe(false)
        expect(pref.tocDockWidth.value).toBe(TOC_DOCK_DEFAULT_WIDTH)
        expect(pref.tocDockSide.value).toBe(TOC_DOCK_DEFAULT_SIDE)
        expect(pref.effectiveOpen.value).toBe(false)
    })

    it('toggleSide flips the side and persists it to localStorage', () => {
        const pref = useTocDockPreference()
        expect(pref.tocDockSide.value).toBe('right')

        pref.toggleSide()
        expect(pref.tocDockSide.value).toBe('left')
        expect(localStorage.getItem(TOC_DOCK_SIDE_KEY)).toBe('left')

        pref.toggleSide()
        expect(pref.tocDockSide.value).toBe('right')
        expect(localStorage.getItem(TOC_DOCK_SIDE_KEY)).toBe('right')
    })

    it('restores a persisted side on init', () => {
        localStorage.setItem(TOC_DOCK_SIDE_KEY, 'left')
        const pref = useTocDockPreference()
        expect(pref.tocDockSide.value).toBe('left')
    })

    it('falls back to right for an invalid persisted side', () => {
        localStorage.setItem(TOC_DOCK_SIDE_KEY, 'top')
        const pref = useTocDockPreference()
        expect(pref.tocDockSide.value).toBe('right')
    })

    it('toggle opens the dock and persists the open state to localStorage', () => {
        const pref = useTocDockPreference()
        pref.toggle()
        expect(pref.tocDockOpen.value).toBe(true)
        expect(pref.effectiveOpen.value).toBe(true)
        expect(localStorage.getItem(TOC_DOCK_OPEN_KEY)).toBe('1')

        pref.toggle()
        expect(pref.tocDockOpen.value).toBe(false)
        expect(pref.effectiveOpen.value).toBe(false)
        expect(localStorage.getItem(TOC_DOCK_OPEN_KEY)).toBe('0')
    })

    it('restores persisted open state on init', () => {
        localStorage.setItem(TOC_DOCK_OPEN_KEY, '1')
        const pref = useTocDockPreference()
        expect(pref.tocDockOpen.value).toBe(true)
        expect(pref.effectiveOpen.value).toBe(true)
    })

    it('clamps width to [TOC_DOCK_MIN_WIDTH, TOC_DOCK_MAX_WIDTH] and persists', () => {
        const pref = useTocDockPreference()
        pref.setWidth(50)
        expect(pref.tocDockWidth.value).toBe(TOC_DOCK_MIN_WIDTH)
        expect(localStorage.getItem(TOC_DOCK_WIDTH_KEY)).toBe(String(TOC_DOCK_MIN_WIDTH))

        pref.setWidth(999)
        expect(pref.tocDockWidth.value).toBe(TOC_DOCK_MAX_WIDTH)
        expect(localStorage.getItem(TOC_DOCK_WIDTH_KEY)).toBe(String(TOC_DOCK_MAX_WIDTH))

        pref.setWidth(320)
        expect(pref.tocDockWidth.value).toBe(320)
        expect(localStorage.getItem(TOC_DOCK_WIDTH_KEY)).toBe('320')
    })

    it('restores persisted width on init', () => {
        localStorage.setItem(TOC_DOCK_WIDTH_KEY, '340')
        const pref = useTocDockPreference()
        expect(pref.tocDockWidth.value).toBe(340)
    })

    it('closes the dock on demand', () => {
        const pref = useTocDockPreference()
        pref.toggle()
        expect(pref.tocDockOpen.value).toBe(true)
        pref.close()
        expect(pref.tocDockOpen.value).toBe(false)
    })

    it('hides the dock while editing and restores the previous open state after', async () => {
        const pref = useTocDockPreference()
        const { setEditing } = useFileEditor()
        pref.toggle() // open
        expect(pref.effectiveOpen.value).toBe(true)

        setEditing(true)
        await nextTick()
        // Effective open must be false while editing (state preserved internally)
        expect(pref.effectiveOpen.value).toBe(false)

        setEditing(false)
        await nextTick()
        // Restores to the pre-edit open state
        expect(pref.effectiveOpen.value).toBe(true)
    })

    it('stays closed after editing if it was closed before editing', async () => {
        const pref = useTocDockPreference()
        const { setEditing } = useFileEditor()
        expect(pref.effectiveOpen.value).toBe(false)

        setEditing(true)
        await nextTick()
        setEditing(false)
        await nextTick()
        expect(pref.effectiveOpen.value).toBe(false)
    })

    it('toggling while editing does not open the dock and does not corrupt restore state', async () => {
        const pref = useTocDockPreference()
        const { setEditing } = useFileEditor()
        pref.toggle() // open, wasOpenBeforeEdit not set yet
        setEditing(true)
        await nextTick()
        expect(pref.effectiveOpen.value).toBe(false)

        // User clicks TOC button while editing — must stay hidden
        pref.toggle()
        expect(pref.effectiveOpen.value).toBe(false)

        setEditing(false)
        await nextTick()
        // Still restores to pre-edit open state (was open)
        expect(pref.effectiveOpen.value).toBe(true)
    })

    it('does not flip or persist open state when toggled while editing', async () => {
        const pref = useTocDockPreference()
        const { setEditing } = useFileEditor()
        // Start closed.
        expect(pref.tocDockOpen.value).toBe(false)

        setEditing(true)
        await nextTick()

        // Toggling while editing must be a no-op: internal state and the
        // persisted value both stay unchanged (the dock is hard-hidden).
        pref.toggle()
        expect(pref.tocDockOpen.value).toBe(false)
        expect(localStorage.getItem(TOC_DOCK_OPEN_KEY)).toBeNull()

        setEditing(false)
        await nextTick()
        expect(pref.effectiveOpen.value).toBe(false)
    })

    it('effectiveOpen is always false on narrow screens regardless of toggle', () => {
        _setWideScreenForTest(false)
        const pref = useTocDockPreference()
        pref.toggle()
        expect(pref.tocDockOpen.value).toBe(true) // internal state persists
        expect(pref.effectiveOpen.value).toBe(false) // but not effective
    })

    it('width can still be set on narrow screens (persisted for later wide use)', () => {
        _setWideScreenForTest(false)
        const pref = useTocDockPreference()
        pref.setWidth(280)
        expect(pref.tocDockWidth.value).toBe(280)
        expect(localStorage.getItem(TOC_DOCK_WIDTH_KEY)).toBe('280')
    })
})
