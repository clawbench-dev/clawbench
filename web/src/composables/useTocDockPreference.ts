import { ref, computed, watch } from 'vue'
import { appLog } from '@/utils/appLog'
import { getWideScreenState } from './useWideScreenLayout'
import { useFileEditor } from './useFileEditor'
import { readPanelState, writePanelState } from '@/utils/panelState'

export const TOC_DOCK_MIN_WIDTH = 200
export const TOC_DOCK_MAX_WIDTH = 400
export const TOC_DOCK_DEFAULT_WIDTH = 260
export const TOC_DOCK_DEFAULT_SIDE = 'right'
export type TocDockSide = 'left' | 'right'

const TAG = 'TocDockPref'

const { isWideScreen } = getWideScreenState()
const { editing } = useFileEditor()

/**
 * PC-wide-screen TOC dock preference. The dock is the inline right-side TOC
 * panel inside the file overlay (VS Code outline-style), as opposed to the
 * narrow-screen bottom drawer.
 *
 * - Open/close state is remembered globally (localStorage) across reloads.
 * - Width (draggable) is remembered globally, clamped to [200, 400].
 * - Side (left/right) is remembered globally (localStorage).
 * - While the file editor is active the dock is hidden (it is a read-navigation
 *   tool); exiting edit mode restores the previous open state.
 * - effectiveOpen is only ever true on wide screens; on narrow screens the
 *   TOC keeps using the bottom drawer and this module is inert.
 */

const tocDockOpen = ref(false)
const tocDockWidth = ref(TOC_DOCK_DEFAULT_WIDTH)
const tocDockSide = ref<TocDockSide>(TOC_DOCK_DEFAULT_SIDE)

/** Pre-edit open state, restored on exit. In-memory only (not persisted). */
let wasOpenBeforeEdit = false
let initialized = false

// Editing hides the dock; restore the pre-edit state on exit.
// Registered exactly once at module load — re-registering per init() would
// accumulate duplicate watchers that share `wasOpenBeforeEdit` and overwrite
// each other.
watch(editing, (isEditing) => {
    if (isEditing) {
        wasOpenBeforeEdit = tocDockOpen.value
        tocDockOpen.value = false
    } else {
        tocDockOpen.value = wasOpenBeforeEdit
        wasOpenBeforeEdit = false
    }
})

function clampWidth(w: number): number {
    return Math.min(TOC_DOCK_MAX_WIDTH, Math.max(TOC_DOCK_MIN_WIDTH, w))
}

function clampSide(value: string): TocDockSide {
    return value === 'left' ? 'left' : 'right'
}

function init() {
    if (initialized) return
    initialized = true
    // Key names, scope and encoding live in the panel-state registry; the
    // clamping below is this dock's own business rule and stays here.
    tocDockOpen.value = readPanelState<boolean>('tocDockOpen')
    tocDockWidth.value = clampWidth(readPanelState<number>('tocDockWidth'))
    tocDockSide.value = clampSide(readPanelState<string>('tocDockSide'))
}

function persistOpen() {
    if (!writePanelState('tocDockOpen', tocDockOpen.value)) {
        appLog.w(TAG, 'persist open failed')
    }
}

export function useTocDockPreference() {
    init()

    function toggle() {
        // Editing hard-hides the dock (read-navigation tool): accept no toggle
        // while editing, otherwise the open state and its persistence get
        // corrupted by a click that the UI can't even reflect.
        if (editing.value) return
        tocDockOpen.value = !tocDockOpen.value
        persistOpen()
    }

    function close() {
        tocDockOpen.value = false
        persistOpen()
    }

    function setWidth(w: number) {
        tocDockWidth.value = clampWidth(w)
        if (!writePanelState('tocDockWidth', tocDockWidth.value)) {
            appLog.w(TAG, 'persist width failed')
        }
    }

    /** Toggle the dock between the left and right edges. */
    function toggleSide() {
        tocDockSide.value = tocDockSide.value === 'left' ? 'right' : 'left'
        if (!writePanelState('tocDockSide', tocDockSide.value)) {
            appLog.w(TAG, 'persist side failed')
        }
    }

    const effectiveOpen = computed(() => isWideScreen.value && tocDockOpen.value && !editing.value)

    return { tocDockOpen, tocDockWidth, tocDockSide, effectiveOpen, toggle, close, setWidth, toggleSide }
}

/** @internal Reset all state — for tests only */
export function _resetForTest() {
    initialized = false
    tocDockOpen.value = false
    tocDockWidth.value = TOC_DOCK_DEFAULT_WIDTH
    tocDockSide.value = TOC_DOCK_DEFAULT_SIDE
    wasOpenBeforeEdit = false
}
