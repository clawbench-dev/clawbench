import { ref, computed, onUnmounted, type Ref, type ComputedRef } from 'vue'

/**
 * Tab-drawer declarative binding registry.
 *
 * Drawers that use BottomSheet (teleported to <body>) survive v-show tab-panel
 * hiding, so effectiveOpen must return false when the owning tab is deactivated.
 * The openRef itself is preserved across tab switches so the drawer re-opens
 * when the user switches back.
 *
 * IMPORTANT: In templates, always use `drawer.effectiveOpen.value` (with .value),
 * NOT just `drawer.effectiveOpen`. Vue only auto-unwraps top-level refs from
 * <script setup>, not nested computed refs on objects. Omitting .value passes
 * the ComputedRef object (truthy) instead of the boolean, causing BottomSheet
 * to always receive open=true.
 */

// Registry: tabId → Set<openRef>
const registry = new Map<string, Set<Ref<boolean>>>()

// Track current tab as a ref so effectiveOpen computed can react to tab switches
const currentTab = ref('chat')

/** Return type of useTabDrawer — explicit type prevents accidental misuse. */
export interface TabDrawer {
  /**
   * Computed ref for the drawer's effective open state.
   * In templates, bind as `:open="drawer.effectiveOpen.value"` (NOT `.effectiveOpen`).
   */
  effectiveOpen: ComputedRef<boolean>
  /** Open the drawer (sets openRef = true, does NOT switch tab) */
  open: () => void
  /** Close the drawer (sets openRef = false) */
  close: () => void
}

/**
 * Register a drawer's open ref as belonging to a tab.
 *
 * @param tabId  The tab this drawer belongs to (e.g. 'browse', 'chat', 'terminal')
 * @param openRef The ref controlling the drawer's open state
 * @returns TabDrawer with effectiveOpen computed, open(), and close()
 */
export function useTabDrawer(tabId: string, openRef: Ref<boolean>): TabDrawer {
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
    effectiveOpen,
    open: () => { openRef.value = true },
    close: () => { openRef.value = false },
  }
}

/**
 * Call from switchTab() to update the current tab.
 * Drawers are visually hidden via effectiveOpen (computed) when their tab
 * is inactive; the openRef itself is preserved so the drawer re-opens
 * when the user switches back.
 */
export function onTabSwitch(newTab: string) {
  currentTab.value = newTab
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
