import { computed, ref } from 'vue'
import {
  DOCK_FEATURES,
  DOCK_TAB_IDS,
  isPermanentDockTab,
  mergeDockOrder,
  isDockTabId,
  type DockFeature,
  type DockTabId,
} from './dockTabs'
import { DEFAULT_DOCK_PRESET, readPanelState, writePanelState } from '@/utils/panelState'

/**
 * Dock orchestration: visibility, order, the shared profile preset, and the
 * server switches a preset turns on or off.
 *
 * This is the single writer for the three persisted keys (`dockHidden`,
 * `dockOrder`, `dockPreset`). The dock itself only reads the derived lists; the
 * settings manager is the only UI that mutates them.
 *
 * Presets are the coarse choice, the manager list is the fine one:
 *   - developer: every tab shown and every server feature on, default order
 *   - standard:  everyday set — Forge / terminal / port mapping hidden and Forge
 *                + terminal switched off; everything else on
 *   - minimal:   only the permanent tabs (notifications hidden too); every
 *                optional feature switched off
 *   - custom:    the user changed something by hand; plugins should treat it as
 *                the neutral default
 *
 * `custom` is not a button — it is what a manual change turns the profile into.
 * A plugin that adapts its own UI to the profile should therefore check
 * `isDeveloper` / `isStandard` explicitly and fall back to neutral otherwise.
 */
export type DockPreset = 'developer' | 'standard' | 'minimal' | 'custom'

/** Presets the user can apply from the manager. `custom` is derived. */
export const APPLICABLE_DOCK_PRESETS: readonly Exclude<DockPreset, 'custom'>[] = [
  'developer',
  'standard',
  'minimal',
]

const PRESET_VALUES: readonly DockPreset[] = [
  'developer',
  'standard',
  'minimal',
  'custom',
]

/** One server switch a preset sets. */
export interface DockFeatureChange {
  key: DockFeature
  value: boolean
}

/**
 * One preset, as RULES rather than a spelled-out plan.
 *
 * Everything a preset does derives from these declarations plus the registry
 * constants (`DOCK_TAB_ORDER`, `DOCK_FEATURES`, the permanent-tab set), so a
 * newly added plugin is handled correctly by all three presets with no
 * registration anywhere.
 *
 *   - `hideTabs` / `keepOnly` decide which entries disappear;
 *   - `parkHiddenBefore` decides where the hidden ones sit in the order (they
 *     must stay IN the order, so re-enabling one lands it where the preset put
 *     it rather than at the end);
 *   - `disableFeatures` / `disableAllFeatures` decide the server switches —
 *     every declared feature is written, so an unlisted one reads as ON.
 */
export interface DockPresetRules {
  /** Explicit exceptions: these closable tabs are hidden. */
  hideTabs?: readonly DockTabId[]
  /** `"permanent"` = hide everything the user can hide; only the core tabs stay. */
  keepOnly?: 'permanent'
  /** Where the hidden tabs sit in the derived order. */
  parkHiddenBefore?: DockTabId
  /** These server switches go OFF; every other declared switch goes ON. */
  disableFeatures?: readonly DockFeature[]
  /** Every declared server switch goes OFF. */
  disableAllFeatures?: boolean
}

/**
 * The three presets as declarations.
 *
 * Default order is a product decision, so it is never spelled out here: a new
 * plugin takes its slot from `DOCK_TAB_ORDER` and the presets follow.
 */
export const DOCK_PRESETS: Readonly<Record<Exclude<DockPreset, 'custom'>, DockPresetRules>> = {
  developer: {
    // All tabs, default order, every server switch on.
  },
  standard: {
    // The everyday set: the developer-only tabs (Forge, terminal, port
    // mapping) disappear and their server work stops; everything else runs.
    hideTabs: ['forge', 'terminal', 'proxy'],
    parkHiddenBefore: 'settings',
    disableFeatures: ['forge.enabled', 'terminal.enabled'],
  },
  minimal: {
    // Only the permanent tabs; every optional feature switched off.
    keepOnly: 'permanent',
    parkHiddenBefore: 'settings',
    disableAllFeatures: true,
  },
}

/**
 * Everything applying a preset does. Kept as data (not hidden inside the
 * apply function) so the manager can preview the consequences in its
 * confirmation dialog from the same source that performs them.
 */
export interface DockPresetPlan {
  /** Tabs hidden in the dock. */
  hidden: readonly DockTabId[]
  /** Full left-dock order (all ids, derived from DOCK_TAB_ORDER). */
  order: readonly DockTabId[]
  /** Server master switches to write, in one request. */
  features: readonly DockFeatureChange[]
}

/**
 * Expand one preset's rules into the concrete plan.
 *
 * Hidden ids only ever come from the closable set: a preset cannot hide a
 * permanent tab (that would lock the user out of the manager that fixes it).
 * The order keeps the DEFAULT slots — visible tabs first, then the hidden block
 * parked before `parkHiddenBefore` — so a feature re-enabled later lands where
 * the preset put it, not at the end.
 */
function expandPreset(rules: DockPresetRules): DockPresetPlan {
  const hidden = new Set<DockTabId>()
  if (rules.keepOnly === 'permanent') {
    for (const id of DOCK_TAB_IDS) if (!isPermanentDockTab(id)) hidden.add(id)
  }
  for (const id of rules.hideTabs ?? []) {
    if (isDockTabId(id) && !isPermanentDockTab(id)) hidden.add(id)
  }

  const visible: DockTabId[] = DOCK_TAB_IDS.filter((id) => !hidden.has(id))
  const parked: DockTabId[] = DOCK_TAB_IDS.filter((id) => hidden.has(id))
  let order = visible
  const before = rules.parkHiddenBefore
  if (before && parked.length > 0) {
    const idx = visible.indexOf(before)
    order =
      idx >= 0
        ? [...visible.slice(0, idx), ...parked, ...visible.slice(idx)]
        : [...visible, ...parked]
  }

  const off = new Set<DockFeature>(rules.disableAllFeatures ? DOCK_FEATURES : (rules.disableFeatures ?? []))
  const features: DockFeatureChange[] = DOCK_FEATURES.map((key) => ({ key, value: !off.has(key) }))

  return { hidden: [...hidden], order, features }
}

/**
 * The three plans, derived from the declarations above.
 *
 * Kept as the public export because the manager previews the consequences from
 * the same data it applies, and the guard test asserts each plan still covers
 * the full tab set.
 */
export const DOCK_PRESET_PLANS: Readonly<Record<Exclude<DockPreset, 'custom'>, DockPresetPlan>> = {
  developer: expandPreset(DOCK_PRESETS.developer),
  standard: expandPreset(DOCK_PRESETS.standard),
  minimal: expandPreset(DOCK_PRESETS.minimal),
}

/** Presets that stop server-side work, so applying them needs a confirmation. */
export const PRESETS_NEEDING_CONFIRM: readonly Exclude<DockPreset, 'custom'>[] = [
  'standard',
  'minimal',
]

function readPreset(): DockPreset {
  const raw = readPanelState<string>('dockPreset')
  return (PRESET_VALUES as readonly string[]).includes(raw)
    ? (raw as DockPreset)
    : (DEFAULT_DOCK_PRESET as DockPreset)
}

function readHidden(): Set<DockTabId> {
  const raw = readPanelState<string[]>('dockHidden') ?? []
  return new Set(raw.filter(isDockTabId))
}

function readOrder(): DockTabId[] {
  const raw = readPanelState<string[]>('dockOrder') ?? []
  return raw.filter(isDockTabId)
}

// Module-level singleton: every consumer (dock, settings manager, plugins)
// must see the same hidden set and profile. Mirrors useMailUnread / useAgents.
const hidden = ref<Set<DockTabId>>(readHidden())
const order = ref<DockTabId[]>(readOrder())
const preset = ref<DockPreset>(readPreset())

/** The current hidden set. Replaced (not mutated) so watchers fire reliably. */
const hiddenIds = computed<ReadonlySet<DockTabId>>(() => hidden.value)

/** The full left-dock order (all ids, default slots preserved for new ones). */
const orderedIds = computed<DockTabId[]>(() => mergeDockOrder(order.value))

/**
 * Only closable tabs can be hidden; a stale value must not hide a core tab.
 *
 * The permanent ids live in `dockTabs.ts` (not only in the manifests) so this
 * module stays a leaf: it is reached from FileHeader, which must not drag the
 * whole plugin graph (lucide + every panel's composables) into its module load.
 * `dockPluginManifest.test.ts` asserts the constant matches the manifests.
 */
function isClosable(id: DockTabId): boolean {
  return !isPermanentDockTab(id)
}

/** Tabs the manager shows as enabled (hidden set inverted). */
const visibleIds = computed<DockTabId[]>(() =>
  DOCK_TAB_IDS.filter((id) => !isClosable(id) || !hidden.value.has(id)),
)

function persist(): void {
  writePanelState('dockHidden', [...hidden.value])
  writePanelState('dockOrder', order.value)
  writePanelState('dockPreset', preset.value)
}

/**
 * Apply a preset: one commit for every server switch, then visibility + order.
 *
 * `commit` performs the server write (the manager passes a single batched
 * PATCH). It runs FIRST and its failure propagates: nothing local has been
 * touched yet, so a failed preset leaves the dock exactly as it was — no
 * half-applied state to unwind. `commit` is optional only so tests and
 * non-server callers can exercise the local half; every UI caller passes one.
 */
export async function applyDockPreset(
  next: Exclude<DockPreset, 'custom'>,
  commit?: (changes: readonly DockFeatureChange[]) => Promise<void>,
): Promise<void> {
  const plan = DOCK_PRESET_PLANS[next]
  if (commit && plan.features.length > 0) await commit(plan.features)
  preset.value = next
  hidden.value = new Set(plan.hidden)
  order.value = [...plan.order]
  persist()
}

/** Show/hide one tab. Core tabs ignore this; any manual change becomes custom. */
export function setDockTabVisible(id: DockTabId, visible: boolean): void {
  if (!isDockTabId(id) || !isClosable(id)) return
  const next = new Set(hidden.value)
  if (visible) next.delete(id)
  else next.add(id)
  hidden.value = next
  preset.value = 'custom'
  persist()
}

export function toggleDockTab(id: DockTabId): void {
  setDockTabVisible(id, hidden.value.has(id))
}

/**
 * Replace the order wholesale (drag and drop). Every row in the manager is
 * draggable, so this is normally the complete list; a partial list keeps the
 * unmentioned tabs in their default slots (see mergeDockOrder) rather than
 * dropping them.
 */
export function setDockTabOrder(ids: readonly DockTabId[]): void {
  const next = mergeDockOrder(ids)
  const same =
    next.length === orderedIds.value.length && next.every((id, i) => id === orderedIds.value[i])
  if (same) return
  order.value = next
  preset.value = 'custom'
  persist()
}

/** Restore the developer preset (all tabs visible, default order, all on). */
export async function resetDockPlugins(
  commit?: (changes: readonly DockFeatureChange[]) => Promise<void>,
): Promise<void> {
  await applyDockPreset('developer', commit)
}

/** True when the tab is hidden by the user (unknown/core ids are never hidden). */
export function isDockTabHidden(id: string): boolean {
  return isDockTabId(id) && isClosable(id) && hidden.value.has(id)
}

/**
 * Mark the profile `custom` without touching visibility or order.
 *
 * The manager's feature switches live on the SERVER, so flipping one changes
 * neither the hidden set nor the order — but it does mean the dock no longer
 * matches any preset, and plugins reading the profile must stop treating it as
 * "developer" or "standard".
 */
export function markDockPresetCustom(): void {
  if (preset.value === 'custom') return
  preset.value = 'custom'
  persist()
}

/** Shared state + actions for the dock and the settings manager. */
export function useDockPlugins() {
  return {
    hidden: hiddenIds,
    order: orderedIds,
    visibleIds,
    preset,
    applyPreset: applyDockPreset,
    setVisible: setDockTabVisible,
    setOrder: setDockTabOrder,
    toggle: toggleDockTab,
    reset: resetDockPlugins,
    isHidden: isDockTabHidden,
    markCustom: markDockPresetCustom,
  }
}

/**
 * Read-only profile for PLUGINS (not the dock shell).
 *
 * A plugin may use this to adapt its own internal UI — for example, a panel
 * that has advanced/debug controls can show them only in `developer`. Treat
 * `custom` as the neutral default: the user has manually changed the dock, so
 * there is no longer a clear "developer vs standard" intent to follow.
 */
export function usePluginProfile() {
  return {
    profile: preset,
    isDeveloper: computed(() => preset.value === 'developer'),
    isStandard: computed(() => preset.value === 'standard'),
    isMinimal: computed(() => preset.value === 'minimal'),
    isCustom: computed(() => preset.value === 'custom'),
  }
}

/** Test hook: re-read the persisted state after clearing storage. */
export function _resetDockPluginsForTest(): void {
  hidden.value = readHidden()
  order.value = readOrder()
  preset.value = readPreset()
}
