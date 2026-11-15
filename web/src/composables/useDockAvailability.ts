import { computed, type ComputedRef } from 'vue'
import { DOCK_TABS, isDockPluginAvailable } from './dockPluginRuntime'
import { isFeatureEnabled, serverFeature } from './useFeatureFlags'
import { useDockPlugins } from './useDockPlugins'
import { isDockTabId, type DockTabId } from './dockTabs'

/**
 * The three independent reasons a dock entry can be missing — in one object.
 *
 * They are deliberately NOT merged into a single boolean (see the design's
 * "three kinds of off"): each has a different owner and a different remedy.
 *
 *  - `featureDisabled` — the SERVER switched the feature off (`/api/features`,
 *    the plugin registry is the authority). Turning it back on is a real
 *    settings change; the server has already stopped the work.
 *  - `unavailable` — the HOST cannot run it here (no PTY, no tunnel transport,
 *    indexer failed to start). A capability problem, not a setting; the panel
 *    stays reachable where it can explain itself.
 *  - `hidden` — the USER hid the entry on this device. The server keeps working.
 *
 * Callers that need "can I navigate here" combine all three; the manager shows
 * them apart.
 */
export interface DockAvailability {
  /** Server-side feature switched off. */
  featureDisabled: ReadonlySet<DockTabId>
  /** Host/environment cannot run it right now. */
  unavailable: ReadonlySet<DockTabId>
  /** Hidden by the user on this device. */
  hidden: ReadonlySet<DockTabId>
}

/** True when any axis blocks the tab (used by navigation guards). */
export function isDockTabBlocked(id: string, availability: DockAvailability): boolean {
  if (!isDockTabId(id)) return false
  return (
    availability.hidden.has(id) ||
    availability.featureDisabled.has(id) ||
    availability.unavailable.has(id)
  )
}

/**
 * Build the availability object for the whole dock.
 *
 * Both remote axes come from the plugin registry (`GET /api/features`), which
 * `loadConfig()` refreshes — so a switch flipped in settings updates the dock
 * without a reload. `unavailable` is the union of the server's `available` flag
 * and the plugin's own local capability probe: the server knows whether its
 * runtime came up, the client knows whether the host has the native capability
 * (tunnel bridge), and either saying "no" is enough.
 */
export function useDockAvailability(): { availability: ComputedRef<DockAvailability> } {
  const { hidden } = useDockPlugins()

  const availability = computed<DockAvailability>(() => {
    const featureDisabled = new Set<DockTabId>()
    const unavailable = new Set<DockTabId>()

    for (const tab of DOCK_TABS) {
      if (tab.feature && !isFeatureEnabled(tab.feature)) featureDisabled.add(tab.id)
      const server = serverFeature(tab.id)
      if ((server && !server.available) || !isDockPluginAvailable(tab.id)) unavailable.add(tab.id)
    }

    return { featureDisabled, unavailable, hidden: hidden.value }
  })

  return { availability }
}
