import { ref, computed, watch, onUnmounted, getCurrentInstance, type Ref, type ComputedRef, readonly } from 'vue'
import { appLog } from '@/utils/appLog'
import { getBigScreenState } from './useBigScreenLayout'

const { isBigScreen, leftTab } = getBigScreenState()

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

/** Options for the encapsulated (new) useTabDrawer API. */
export interface TabDrawerOptions {
  /**
   * If false, the drawer does NOT auto-restore when switching back to its tab.
   * Use for small pickers/popups that shouldn't surprise-reopen on tab return.
   * Default: true
   */
  autoRestore?: boolean
}

/** Return type of useTabDrawer — explicit type prevents accidental misuse. */
export interface TabDrawer {
  /**
   * Computed ref for the drawer's effective open state.
   * In templates, bind as `:open="drawer.effectiveOpen.value"` (NOT `.effectiveOpen`).
   */
  effectiveOpen: ComputedRef<boolean>
  /** Read-only access to the drawer's open state. Use open()/close() to mutate. */
  isOpen: Readonly<Ref<boolean>>
  /** Open the drawer (sets internal ref = true, does NOT switch tab) */
  open: () => void
  /** Close the drawer (sets internal ref = false) */
  close: () => void
  /** Toggle the drawer open state */
  toggle: () => void
}

// Dev-mode warning dedup — tracks which call sites have already warned
const _legacyWarned = new Set<string>()

/**
 * Register a tab-scoped drawer.
 *
 * New API (preferred):
 *   const drawer = useTabDrawer('chat')
 *   drawer.open()   // open
 *   drawer.close()  // close
 *   drawer.toggle() // toggle
 *   <SomeDrawer :open="drawer.effectiveOpen.value" @close="drawer.close()" />
 *
 * Legacy API (backward-compat, will warn in dev):
 *   const fooOpen = ref(false)
 *   const drawer = useTabDrawer('chat', fooOpen)
 *
 * @param tabId  The tab this drawer belongs to (e.g. 'browse', 'chat', 'terminal')
 * @param openRefOrOptions  Either a Ref<boolean> (legacy) or TabDrawerOptions (new)
 */
export function useTabDrawer(tabId: string, openRefOrOptions?: Ref<boolean> | TabDrawerOptions): TabDrawer {
  const isLegacy = openRefOrOptions != null && typeof openRefOrOptions === 'object' && 'value' in openRefOrOptions

  let openRef: Ref<boolean>
  let autoRestore: boolean

  if (isLegacy) {
    // Legacy path: external ref
    openRef = openRefOrOptions as Ref<boolean>
    autoRestore = true
    if (import.meta.env.DEV) {
      const key = `${tabId}:${_getCallerSite()}`
      if (!_legacyWarned.has(key)) {
        _legacyWarned.add(key)
        appLog.w('useTabDrawer', `Legacy call useTabDrawer('${tabId}', openRef) — migrate to useTabDrawer('${tabId}') and use drawer.open()/close() instead. This warning fires once per call site.`)
      }
    }
  } else {
    // New path: internal ref
    openRef = ref(false)
    const options = (openRefOrOptions as TabDrawerOptions) || {}
    autoRestore = options.autoRestore !== false
  }

  let set = registry.get(tabId)
  if (!set) {
    set = new Set()
    registry.set(tabId, set)
  }
  set.add(openRef)

  // Only register cleanup for component-scoped drawers.
  // Module-level singletons (useSessionIdentity, useMarkdownDiff) live for
  // the app's lifetime and are cleaned up by resetTabDrawerState() instead.
  if (getCurrentInstance()) {
    onUnmounted(() => {
      set?.delete(openRef)
    })
  }

  const effectiveOpen = computed(() => {
    const tabActive =
      currentTab.value === tabId ||
      (isBigScreen.value && (tabId === 'chat' || tabId === leftTab.value))
    return tabActive && openRef.value
  })

  // For autoRestore: false, close the drawer when its tab is no longer active
  // (narrow: currentTab changed; big-screen: leftTab changed away)
  if (!autoRestore) {
    const closeIfInactive = () => {
      if (!openRef.value) return
      const active =
        currentTab.value === tabId ||
        (isBigScreen.value && (tabId === 'chat' || tabId === leftTab.value))
      if (!active) openRef.value = false
    }
    watch(() => [currentTab.value, isBigScreen.value, leftTab.value], closeIfInactive)
  }

  return {
    effectiveOpen,
    isOpen: readonly(openRef),
    open: () => { openRef.value = true },
    close: () => { openRef.value = false },
    toggle: () => { openRef.value = !openRef.value },
  }
}

/** Best-effort caller site identification for dev-mode warnings. */
function _getCallerSite(): string {
  try {
    const stack = new Error().stack
    if (!stack) return 'unknown'
    const lines = stack.split('\n')
    // Skip Error ctor + useTabDrawer + _getCallerSite — find the actual caller
    for (let i = 3; i < lines.length; i++) {
      const m = lines[i].match(/(?:at\s+)?(?:.*?\s+\()?(.+?):(\d+):(\d+)/)
      if (m) return `${m[1]}:${m[2]}`
    }
    return 'unknown'
  } catch {
    return 'unknown'
  }
}

/**
 * Call from switchTab() to update the current tab.
 * Drawers are visually hidden via effectiveOpen (computed) when their tab
 * is inactive; the openRef itself is preserved so the drawer re-opens
 * when the user switches back (unless autoRestore: false).
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
