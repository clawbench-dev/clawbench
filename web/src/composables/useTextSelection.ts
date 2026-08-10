import { ref, onMounted, onUnmounted } from 'vue'

/**
 * Reactive flag that is true while the user is actively selecting text
 * (a non-empty selection). Floating UI (e.g. back/forward nav, chat scroll
 * buttons) hides during selection so it does not interfere with drag-select /
 * long-press selection.
 */
export function useTextSelectionActive() {
    const active = ref(false)

    function update() {
        const sel = window.getSelection?.()
        active.value = !!(sel && sel.toString().length > 0)
    }

    onMounted(() => {
        document.addEventListener('selectionchange', update)
        document.addEventListener('mouseup', update)
    })

    onUnmounted(() => {
        document.removeEventListener('selectionchange', update)
        document.removeEventListener('mouseup', update)
    })

    return { active }
}
