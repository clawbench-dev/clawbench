/**
 * Server-side feature switches, read as reactive frontend flags.
 *
 * The 功能编排 (dock plugin manager) writes these to the server, not to this
 * device: turning one off stops background work there (polling, sync, the task
 * scheduler) and revokes what it had handed out (share links). So the frontend
 * must not merely hide the dock tab — every *entry point* that performs the
 * action, or jumps to the feature's page, has to be gated too, or the user
 * presses a button that can only fail server-side.
 *
 * This module is the single reader for that question.
 *
 * The six MASTER switches are no longer derived from `/api/config`: the server's
 * plugin registry is the authority (`GET /api/features`), so the frontend keeps
 * no default-value mirror to drift from `model.ApplyDefaults`. `loadConfig()`
 * refreshes that snapshot (and `patchConfig` reloads it), so a switch flipped in
 * settings disables the entries everywhere without a reload.
 *
 * The per-kind SHARE permissions (`share.allow_*`) are sub-options, not features:
 * they are not in the registry, so they still come from the config snapshot and
 * default to ON (a config written before they existed has no key at all).
 */
import { computed, shallowRef, type ComputedRef } from 'vue'
import i18n from '@/i18n'
import { useToast } from '@/composables/useToast'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { dockFeatures, type DockFeature } from '@/composables/dockTabs'

const TAG = 'FeatureFlags'

/**
 * Feature switches that also drive background work on the server.
 *
 * Aliased from `DockFeature` rather than re-listed: the dock tab registry and
 * this reader answer the same question ("which server switch gates this
 * feature"), and two hand-maintained lists drift — adding a switch to one and
 * forgetting the other is exactly how a tab keeps rendering after its server
 * side was switched off. `dockTabs` imports nothing, and this is a type-only
 * import, so neither module gains a runtime dependency.
 *
 * A future switch that is NOT a dock tab (a sub-option such as
 * `share.allow_download`) would have to be added here explicitly — that is
 * intended: those are different questions and should not widen the dock type.
 */
export type FeatureFlagKey =
  | DockFeature
  // Sub-options of a dock feature rather than switches of their own: the share
  // center's per-kind permissions. Turning one off does NOT remove the 分享 tab
  // (the master switch does that) — it only stops that kind of link from being
  // created, so the entries that create one must be gated individually.
  | 'share.allow_files'
  | 'share.allow_sessions'
  | 'share.allow_meetings'
  // Sub-option too, but about an EXISTING link rather than a new one: whether a
  // visitor may download the file behind it. Turning it off leaves the share
  // page readable and only removes the direct-download address.
  | 'share.allow_download'

/**
 * One row of `GET /api/features` — the server's plugin registry.
 *
 * `enabled` is the feature axis (a real switch: the server stops the work);
 * `available` is the capability axis (the host cannot run it here). They are
 * orthogonal — terminal can be enabled and unavailable at once.
 */
export interface ServerFeature {
  id: string
  enabled: boolean
  available: boolean
  reason: '' | 'disabled' | 'capability'
  /** The server's own default, for the first paint before the request lands. */
  defaultOn: boolean
}

/**
 * The feature registry snapshot, pushed in by `loadConfig()`.
 *
 * Empty until the request lands, which reads as "every feature is on" — the same
 * rule the server uses for a config written before a switch existed, so no entry
 * flickers away during first paint.
 */
const serverFeatures = shallowRef<Record<string, ServerFeature>>({})

/** Test/config hook: adopt a freshly loaded `/api/features` payload. */
export function syncServerFeatures(list: readonly ServerFeature[] | null | undefined): void {
  const next: Record<string, ServerFeature> = {}
  for (const entry of list ?? []) {
    if (entry && typeof entry.id === 'string') next[entry.id] = entry
  }
  serverFeatures.value = next
}

/** Refresh the registry snapshot. Failures keep the previous state (never "off"). */
export async function refreshServerFeatures(): Promise<void> {
  try {
    const data = await apiGet<{ features?: ServerFeature[] }>('/api/features')
    syncServerFeatures(data.features)
  } catch (err) {
    appLog.d(TAG, 'failed to load /api/features', err)
  }
}

/** The registry row for a plugin id, or undefined before the request lands. */
export function serverFeature(id: string): ServerFeature | undefined {
  return serverFeatures.value[id]
}

/** True for the six master switches the server registry owns. */
function isMasterFeature(key: FeatureFlagKey): key is DockFeature {
  return dockFeatures().includes(key)
}

/**
 * The server config snapshot these flags read, pushed in by
 * `useSettingsConfig.loadConfig()`.
 *
 * Deliberately a copy rather than a read of `serverConfig` itself: this module
 * has to stay a leaf. Importing the settings module would drag it into every
 * component that gates an entry, and dozens of component tests mock
 * `useSettingsConfig` with only the handful of exports that component uses — a
 * missing export must read as "config unknown" (features ON), never crash a
 * render. `loadConfig` is the single place the config lands, so it is also the
 * single place that pushes it here.
 */
const featureConfig = shallowRef<Record<string, unknown>>({})

/** Test/config hook: adopt a freshly loaded server config. */
export function syncFeatureConfig(config: Record<string, unknown> | null | undefined): void {
  featureConfig.value = config && typeof config === 'object' ? config : {}
}

function readPath(path: string): unknown {
  const parts = path.split('.')
  let current: unknown = featureConfig.value
  for (const part of parts) {
    if (current == null || typeof current !== 'object') return undefined
    current = (current as Record<string, unknown>)[part]
  }
  return current
}

/**
 * Whether a feature is on right now, in three steps:
 *
 * 1. a MASTER switch the registry knows about → the registry's value (it is the
 *    authority: it reflects the server's live state, not a snapshot);
 * 2. otherwise the config snapshot (`/api/config`) — this covers the per-kind
 *    share permissions AND the window before `/api/features` lands, when the
 *    config is the only thing we have;
 * 3. neither → ON, the same "missing means on" rule the server uses for a
 *    config written before a switch existed.
 */
export function isFeatureEnabled(key: FeatureFlagKey): boolean {
  if (isMasterFeature(key)) {
    const entry = serverFeatures.value[key.split('.')[0]]
    if (entry) return entry.enabled
  }
  const value = readPath(key)
  return typeof value === 'boolean' ? value : true
}

/** Reactive form of `isFeatureEnabled` for templates and computeds. */
export function useFeatureFlag(key: FeatureFlagKey): ComputedRef<boolean> {
  return computed(() => isFeatureEnabled(key))
}

/** Every switch at once, for callers that gate several entries in one file. */
export function useFeatureFlags() {
  return {
    forgeEnabled: useFeatureFlag('forge.enabled'),
    mailEnabled: useFeatureFlag('mail.enabled'),
    terminalEnabled: useFeatureFlag('terminal.enabled'),
    kbEnabled: useFeatureFlag('kb.enabled'),
    tasksEnabled: useFeatureFlag('tasks.enabled'),
    shareEnabled: useFeatureFlag('share.enabled'),
    shareAllowFiles: useFeatureFlag('share.allow_files'),
    shareAllowSessions: useFeatureFlag('share.allow_sessions'),
    shareAllowMeetings: useFeatureFlag('share.allow_meetings'),
    shareAllowDownload: useFeatureFlag('share.allow_download'),
  }
}

/**
 * Whether a given KIND of share may be created right now.
 *
 * Always AND-ed with the master switch: the per-kind switches are permissions,
 * not enables — turning every kind on while 分享 itself is off must still block
 * (the server revokes links and refuses new ones in that state).
 */
export function useShareKindAllowed(kind: 'files' | 'sessions' | 'meetings'): ComputedRef<boolean> {
  return computed(() => {
    if (!isFeatureEnabled('share.enabled')) return false
    return isFeatureEnabled(`share.allow_${kind}`)
  })
}

/**
 * Tell the user why the entry they pressed did nothing, and where to fix it.
 *
 * One message for every gated entry: the reason is always the same ("the
 * feature is off") and so is the remedy ("功能编排"). A per-feature wording
 * would only multiply strings without adding information.
 */
export function notifyFeatureDisabled(): void {
  useToast().show(i18n.global.t('feature.disabled'), {
    icon: 'blocked',
    type: 'info',
    duration: 3000,
  })
}
