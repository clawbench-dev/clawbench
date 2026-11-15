import { computed, ref } from 'vue'
import {
  DOCK_TAB_BY_ID,
  DOCK_TABS,
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
 * Everything applying a preset does. Kept as data (not hidden inside the
 * apply function) so the manager can preview the consequences in its
 * confirmation dialog from the same source that performs them.
 */
export interface DockPresetPlan {
  /** Tabs hidden in the dock. */
  hidden: readonly DockTabId[]
  /** Full left-dock order (all 14 ids). */
  order: readonly DockTabId[]
  /** Server master switches to write, in one request. */
  features: readonly DockFeatureChange[]
}

/**
 * The three presets, spelled out.
 *
 * Orders are literal arrays rather than derived filters: the three presets
 * differ in ways a filter cannot express (standard parks the developer tabs at
 * the end instead of removing them, minimal keeps the historical slot order
 * so turning a feature back on lands it where it used to be).
 */
export const DOCK_PRESET_PLANS: Readonly<Record<Exclude<DockPreset, 'custom'>, DockPresetPlan>> = {
  developer: {
    hidden: [],
    order: [
      'browse', 'view', 'history', 'forge', 'tasks', 'terminal', 'stats',
      'mail', 'kb', 'live', 'share', 'proxy', 'notifications', 'settings',
    ],
    features: [
      { key: 'forge.enabled', value: true },
      { key: 'mail.enabled', value: true },
      { key: 'terminal.enabled', value: true },
      { key: 'kb.enabled', value: true },
      { key: 'tasks.enabled', value: true },
      { key: 'share.enabled', value: true },
    ],
  },
  standard: {
    hidden: ['forge', 'terminal', 'proxy'],
    order: [
      'browse', 'view', 'history', 'tasks', 'stats', 'mail', 'kb', 'live',
      'share', 'notifications', 'forge', 'terminal', 'proxy', 'settings',
    ],
    features: [
      { key: 'forge.enabled', value: false },
      { key: 'mail.enabled', value: true },
      { key: 'terminal.enabled', value: false },
      { key: 'kb.enabled', value: true },
      { key: 'tasks.enabled', value: true },
      { key: 'share.enabled', value: true },
    ],
  },
  minimal: {
    hidden: [
      'notifications', 'history', 'forge', 'tasks', 'terminal', 'stats',
      'mail', 'kb', 'live', 'share', 'proxy',
    ],
    order: [
      'browse', 'view', 'notifications', 'history', 'forge', 'tasks',
      'terminal', 'stats', 'mail', 'kb', 'live', 'share', 'proxy', 'settings',
    ],
    features: [
      { key: 'forge.enabled', value: false },
      { key: 'mail.enabled', value: false },
      { key: 'terminal.enabled', value: false },
      { key: 'kb.enabled', value: false },
      { key: 'tasks.enabled', value: false },
      { key: 'share.enabled', value: false },
    ],
  },
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

/** Only closable tabs can be hidden; a stale value must not hide a core tab. */
function isClosable(id: DockTabId): boolean {
  return !!DOCK_TAB_BY_ID.get(id)?.closable
}

/** Tabs the manager shows as enabled (hidden set inverted). */
const visibleIds = computed<DockTabId[]>(() =>
  DOCK_TABS.filter((t) => !t.closable || !hidden.value.has(t.id)).map((t) => t.id),
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
  const meta = DOCK_TAB_BY_ID.get(id)
  if (!meta || !meta.closable) return
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
