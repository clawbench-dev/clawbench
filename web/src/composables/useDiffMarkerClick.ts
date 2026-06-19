/**
 * useDiffMarkerClick — shared click handler for diff markers.
 *
 * Both CodePreview (inline markers) and MarkdownPreview (overlay markers)
 * use identical click logic — only the CSS selector differs.
 */

import { diffMarkers, openDiffDrawer } from '@/composables/useMarkdownDiff.ts'

/**
 * Handle a click event, checking if a diff marker was clicked.
 * If so, opens the diff drawer and stops propagation.
 *
 * @param event    The click event
 * @param selector CSS selector for the marker element (e.g. '.diff-marker-inline' or '.diff-marker-overlay')
 * @returns true if a marker was clicked (caller should skip further handling), false otherwise
 */
export function handleDiffMarkerClick(event: Event, selector: string): boolean {
    const markerEl = (event.target as Element).closest(selector)
    if (!markerEl) return false
    event.preventDefault()
    event.stopPropagation()
    const markerId = markerEl.getAttribute('data-marker-id')
    if (markerId) {
        const marker = diffMarkers.value.find(m => m.id === markerId)
        if (marker) openDiffDrawer(marker)
    }
    return true
}
