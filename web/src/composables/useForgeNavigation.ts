import { ref } from 'vue'

/**
 * Cross-component deep-link into the forge panel.
 *
 * Module-level (not per-instance) because the forge panel is mounted lazily by
 * TabPanel on its first visit, so a request can be made before any consumer
 * exists; it must survive until the panel mounts AND becomes active. A reactive
 * ref (rather than a plain variable) is required because switchTab() no-ops when
 * the target tab is already showing — without a watcher that second trigger
 * would never fire.
 *
 * Mirrors the established idiom in useSettingsNavigation (pendingSettingsCategory)
 * and useCommitNavigation (pendingCommitNavigation).
 */
export interface ForgeNavigationRequest {
  type: 'issue' | 'pr' | 'pipeline'
  /** Issue/PR number. Ignored for a pipeline. */
  number: number
  /** CI run id. Only meaningful for a pipeline. */
  runId: number
}

const pendingForgeNavigation = ref<ForgeNavigationRequest | null>(null)

/** Request that the forge panel open a specific item. */
export function setPendingForgeNavigation(req: ForgeNavigationRequest) {
  pendingForgeNavigation.value = req
}

/**
 * Consume (and clear) the pending request.
 *
 * Clearing is what makes this one-shot: without it the watcher would re-open the
 * same item on every subsequent activation of the tab.
 */
export function consumePendingForgeNavigation(): ForgeNavigationRequest | null {
  const req = pendingForgeNavigation.value
  pendingForgeNavigation.value = null
  return req
}

/** Reactive ref, watched by the forge panel so a deep-link is honoured. */
export { pendingForgeNavigation }

/** Test-only: clear the pending request between cases. */
export function _resetPendingForgeNavigationForTesting() {
  pendingForgeNavigation.value = null
}
