import { ref, computed, onUnmounted, type Ref } from 'vue'

/**
 * Tab-drawer declarative binding registry.
 *
 * Drawers that use BottomSheet (teleported to <body>) survive v-show tab-panel
 * hiding, so they must be explicitly closed when their owning tab is deactivated.
 * Instead of hardcoding this in switchTab(), drawers register here with their
 * owning tabId. On tab switch, all drawers not belonging to the new tab are
 * auto-closed.
 *
 * Usage: call useTabDrawer(tabId, openRef) in the component's setup, then bind
 * the returned effectiveOpen.value to the BottomSheet/ModalDialog :open prop.
 */

// Registry: tabId → Set<openRef>
const registry = new Map<string, Set<Ref<boolean>>>()

// Track current tab as a ref so effectiveOpen computed can react to tab switches
const currentTab = ref('chat')

/**
 * Register a drawer's open ref as belonging to a tab.
 *
 * @param tabId  The tab this drawer belongs to (e.g. 'browse', 'chat', 'terminal')
 * @param openRef The ref controlling the drawer's open state
 * @returns An object with:
 *   - effectiveOpen: computed(boolean) — bind to BottomSheet :open, auto-false when tab inactive
 *   - open(): set the drawer open (does NOT switch tab)
 *   - close(): set the drawer closed
 */
export function useTabDrawer(tabId: string, openRef: Ref<boolean>) {
  let set = registry.get(tabId)
  if (!set) {
    set = new Set()
    registry.set(tabId, set)
  }
  set.add(openRef)

  onUnmounted(() => {
    set?.delete(openRef)
  })

  const effectiveOpen = computed(() => currentTab.value === tabId && openRef.value)

  return {
    /** Bind to BottomSheet/ModalDialog :open — automatically hides when tab is inactive */
    effectiveOpen,
    /** Open the drawer (sets openRef = true) */
    open: () => { openRef.value = true },
    /** Close the drawer (sets openRef = false) */
    close: () => { openRef.value = false },
  }
}

/**
 * Call from switchTab() to deactivate all drawers not belonging to the new tab.
 */
export function onTabSwitch(newTab: string) {
  currentTab.value = newTab
  for (const [tabId, refs] of registry) {
    if (tabId !== newTab) {
      for (const r of refs) r.value = false
    }
  }
}

/**
 * Reset all drawer state (for SPA hot project switch).
 */
export function resetTabDrawerState() {
  for (const refs of registry.values()) {
    for (const r of refs) r.value = false
  }
  currentTab.value = 'chat'
}
