import { ref } from 'vue'

/**
 * One forge item a notification (or completion card) wants the panel to open.
 *
 * `itemKey` is the opaque server-side read key and is the ONLY field that can
 * name a pipeline: a pipeline's `number` is always 0, so its identity lives in
 * the key ("pipeline/run:<id>"). It is passed back verbatim to the read
 * endpoint — rebuilding it from (type, number) would produce "pipeline/0" and
 * silently match nothing.
 */
export interface ForgeTarget {
  /** The project whose bound repository holds this item. Empty means "the active project". */
  projectPath?: string
  /** "issue" | "pr" | "pipeline" */
  type: 'issue' | 'pr' | 'pipeline'
  /** Issue/PR number. Always 0 for a pipeline. */
  number: number
  /** CI run id; 0 for anything that is not a pipeline. */
  runId: number
  /** Opaque read key (`issue/12`, `pr/7`, `pipeline/run:555`). */
  itemKey: string
}

/**
 * Module-level pending deep-link target.
 *
 * Set by producers that live OUTSIDE the forge panel (the system-notification
 * click in App.vue, the in-app completion card) and consumed by
 * ForgePanelContent. It must be module-level rather than a prop for the same
 * reason the settings deep-link is (useSettingsNavigation): the panel is
 * mounted lazily by its TabPanel, so a target that arrives before the user has
 * ever opened the tab has to survive until the panel mounts.
 *
 * Two further facts force a *reactive* ref rather than a plain consume-on-mount
 * function:
 *
 *   - `switchTab('forge')` is a no-op when the forge tab is ALREADY active, so
 *     there is no prop change for the panel to react to. The ref itself is the
 *     only signal in that case.
 *   - The panel stays mounted (TabPanel uses v-show), so a cross-project target
 *     is seen by the OLD project's panel first. That instance must not consume
 *     it — the consumer gates on `projectPath`.
 */
const pendingForgeTarget = ref<ForgeTarget | null>(null)

/** Request that the forge panel open a specific item. */
export function setPendingForgeTarget(target: ForgeTarget) {
  pendingForgeTarget.value = target
}

/**
 * Consume (and clear) the pending target.
 *
 * Clearing up front is deliberate: a target that cannot be opened (unbound
 * repository, vanished item) must not linger and fire again on the panel's next
 * activation.
 */
export function consumePendingForgeTarget(): ForgeTarget | null {
  const target = pendingForgeTarget.value
  pendingForgeTarget.value = null
  return target
}

/**
 * Drop the pending target without opening anything.
 *
 * Used when the navigation that set it was abandoned — e.g. a cross-project
 * click whose project switch was rejected. Leaving it set would make the panel
 * open a stale item the next time its project happened to match.
 */
export function clearPendingForgeTarget() {
  pendingForgeTarget.value = null
}

/**
 * Reactive ref for the panel's watcher, so a target is honored even when the
 * forge tab is already active and switchTab() does not change `active`.
 */
export { pendingForgeTarget }
