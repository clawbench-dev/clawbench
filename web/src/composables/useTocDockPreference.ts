import { ref, computed, watch } from 'vue'
import { appLog } from '@/utils/appLog'
import { getWideScreenState } from './useWideScreenLayout'
import { useFileEditor } from './useFileEditor'
import { readPanelState, writePanelState, layoutEpoch } from '@/utils/panelState'

export const TOC_DOCK_MIN_WIDTH = 200
export const TOC_DOCK_MAX_WIDTH = 400
export const TOC_DOCK_DEFAULT_WIDTH = 260
export const TOC_DOCK_DEFAULT_SIDE = 'right'
export type TocDockSide = 'left' | 'right'

const TAG = 'TocDockPref'

const { isWideScreen } = getWideScreenState()
const { editing, editingMode } = useFileEditor()

/**
 * Whether the dock must stay hidden because the current edit session cannot
 * feed it a trustworthy outline. The source editor's outline is the saved file
 * (it goes stale as soon as the user types), so the dock stays out of the way
 * there. The visual editor's outline is read live from its DOM, so it keeps the
 * dock available.
 */
const hiddenForEdit = computed(() => editing.value && editingMode.value !== 'wysiwyg')

/**
 * PC-wide-screen TOC dock preference. The dock is the inline right-side TOC
 * panel inside the file overlay (VS Code outline-style), as opposed to the
 * narrow-screen bottom drawer.
 *
 * - Open/close state is remembered globally (localStorage) across reloads.
 * - Width (draggable) is remembered globally, clamped to [200, 400].
 * - Side (left/right) is remembered globally (localStorage).
 * - While the **source** editor is active the dock is hidden (its outline is
 *   the saved file, which goes stale as soon as the user types); exiting that
 *   edit session restores the previous open state. The visual (WYSIWYG) editor
 *   keeps the dock available — its outline is read live from the editor DOM.
 * - effectiveOpen is only ever true on wide screens; on narrow screens the
 *   TOC keeps using the bottom drawer and this module is inert.
 */

const tocDockOpen = ref(false)
const tocDockWidth = ref(TOC_DOCK_DEFAULT_WIDTH)
const tocDockSide = ref<TocDockSide>(TOC_DOCK_DEFAULT_SIDE)

/** Pre-edit open state, restored on exit. In-memory only (not persisted). */
let wasOpenBeforeEdit = false
/** Whether this edit session is the one that force-hid the dock. */
let hiddenForThisEdit = false
let initialized = false
let stopEpochWatch: (() => void) | null = null

// The source editor hides the dock; the visual editor does not. Restore the
// pre-edit state only when this session actually hid it — a dock opened during
// visual editing must not be closed on exit.
// Registered exactly once at module load — re-registering per init() would
// accumulate duplicate watchers that share `wasOpenBeforeEdit` and overwrite
// each other.
watch([editing, editingMode], ([isEditing, mode]) => {
    if (isEditing && mode !== 'wysiwyg') {
        wasOpenBeforeEdit = tocDockOpen.value
        tocDockOpen.value = false
        hiddenForThisEdit = true
    } else if (!isEditing && hiddenForThisEdit) {
        tocDockOpen.value = wasOpenBeforeEdit
        wasOpenBeforeEdit = false
        hiddenForThisEdit = false
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
    load()
    // Re-read when the layout-memory scope or the project root changes; this
    // module loads once per session and would otherwise keep the values it read
    // on first use. Stopped first so a test reset (which clears `initialized`)
    // cannot accumulate one watcher per init.
    stopEpochWatch?.()
    stopEpochWatch = watch(layoutEpoch, load)
}

/** Load the three persisted dock preferences (clamped to this dock's range). */
function load() {
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
        // The source editor has no live outline to offer: accept no toggle
        // there, otherwise the open state and its persistence get corrupted by
        // a click that the UI cannot even reflect.
        if (hiddenForEdit.value) return
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

    const effectiveOpen = computed(() => isWideScreen.value && tocDockOpen.value && !hiddenForEdit.value)

    return { tocDockOpen, tocDockWidth, tocDockSide, effectiveOpen, toggle, close, setWidth, toggleSide }
}

/** @internal Reset all state — for tests only */
export function _resetForTest() {
    initialized = false
    stopEpochWatch?.()
    stopEpochWatch = null
    tocDockOpen.value = false
    tocDockWidth.value = TOC_DOCK_DEFAULT_WIDTH
    tocDockSide.value = TOC_DOCK_DEFAULT_SIDE
    wasOpenBeforeEdit = false
    hiddenForThisEdit = false
}
