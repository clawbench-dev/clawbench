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
 * This module is the single reader for that question. It holds only the config
 * snapshot `loadConfig()` pushes in (see `syncFeatureConfig`), so a switch
 * flipped in settings disables the entries everywhere without a reload.
 *
 * Defaults are ON and mirror the server's `ApplyDefaults`: a config file written
 * before a switch existed has no key at all, and reading that as "off" would
 * hide features from every existing install on upgrade.
 */
import { computed, shallowRef, type ComputedRef } from 'vue'
import i18n from '@/i18n'
import { useToast } from '@/composables/useToast'
import type { DockFeature } from '@/composables/dockTabs'

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
 * Built-in defaults, mirroring `model.ApplyDefaults`. All six are ON.
 *
 * `terminal.enabled` is listed for completeness: the terminal's real runtime
 * gate is `/api/terminal/status` (see useTerminalStatus), which also covers the
 * "config says on but the manager is nil" case.
 */
export const FEATURE_FLAG_DEFAULTS: Readonly<Record<FeatureFlagKey, boolean>> = {
  'forge.enabled': true,
  'mail.enabled': true,
  'terminal.enabled': true,
  'kb.enabled': true,
  'tasks.enabled': true,
  'share.enabled': true,
  // Per-kind share permissions (设置 → 分享中心). ON by default like the master
  // switch: a config written before they existed must not silently stop every
  // kind of link from being created.
  'share.allow_files': true,
  'share.allow_sessions': true,
  'share.allow_meetings': true,
  'share.allow_download': true,
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
 * Whether a feature is on right now.
 *
 * Only an explicit `false` disables it: a missing key (old config, or a request
 * that has not landed yet) falls back to the built-in default, and a malformed
 * non-boolean value is treated as the default rather than silently disabling a
 * feature.
 */
export function isFeatureEnabled(key: FeatureFlagKey): boolean {
  const value = readPath(key)
  return typeof value === 'boolean' ? value : FEATURE_FLAG_DEFAULTS[key]
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
