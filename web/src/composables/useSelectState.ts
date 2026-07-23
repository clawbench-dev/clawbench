import { ref } from 'vue'

/**
 * SelectState represents a unified select-one-from-many state.
 * Used to generalize mode and thinking effort handling.
 *
 * @param category - The category name (e.g., "mode", "thought_level")
 * @param loadPrefFn - Function to load persisted preference for this category
 */
export function createSelectState(
    category: string,
    loadPrefFn: (agentId: string) => string | null,
) {
    const currentId = ref('')
    const currentName = ref('')
    const available = ref<Array<{ id: string; name: string }>>([])

    /** Resolve the display name for a given id from available items. */
    function resolveName(id: string): string {
        const item = available.value.find(item => item.id === id)
        return item?.name || id
    }

    /** Update current id and available items (full state update). */
    function update(id: string, items: Array<{ id: string; name: string }>) {
        if (id) {
            currentId.value = id
            currentName.value = items.find(m => m.id === id)?.name || id
        }
        if (items.length > 0) {
            available.value = items
        }
    }

    /** Update available items without changing the current selection. */
    function updateAvailable(items: Array<{ id: string; name: string }>) {
        if (items.length > 0) {
            available.value = items
            // Resolve name if id was set before items arrived
            if (currentId.value) {
                currentName.value = resolveName(currentId.value)
            }
        }
    }

    /** Clear all state. Fixes the bug where clearThinkingEffortState
     *  did not clear currentId (only cleared available and currentName). */
    function clear() {
        currentId.value = ''
        currentName.value = ''
        available.value = []
    }

    /** Sync state from server data (REST API or SSE event).
     *  Preserves user's selection when server returns empty currentId. */
    function syncFromData(valueFromServer: string, availableItems: Array<{ id: string; name: string }>) {
        if (valueFromServer) {
            currentId.value = valueFromServer
            const item = availableItems.find(l => l.id === valueFromServer)
            currentName.value = item?.name || valueFromServer
        }
        if (availableItems.length > 0) {
            available.value = availableItems
            // Resolve name if currentId was set before items arrived
            if (currentId.value && !currentName.value) {
                currentName.value = resolveName(currentId.value)
            }
        }
    }

    /** Load persisted preference for the given agent. */
    function loadPref(agentId: string) {
        if (!agentId) return
        const pref = loadPrefFn(agentId)
        if (pref) {
            currentId.value = pref
        }
    }

    return {
        category,
        currentId,
        currentName,
        available,
        update,
        updateAvailable,
        clear,
        syncFromData,
        loadPref,
    }
}
