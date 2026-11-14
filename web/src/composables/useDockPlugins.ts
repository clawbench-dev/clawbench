import { computed, ref } from 'vue'
import {
  DOCK_TAB_BY_ID,
  DOCK_TABS,
  REORDERABLE_DOCK_TABS,
  isDockTabId,
  mergeDockOrder,
  type DockTabId,
} from './dockTabs'
import { DEFAULT_DOCK_PRESET, readPanelState, writePanelState } from '@/utils/panelState'

/**
 * Dock plugin orchestration: visibility, order and the shared profile preset.
 *
 * This is the single writer for the three persisted keys (`dockHidden`,
 * `dockOrder`, `dockPreset`). The dock itself only reads the derived lists; the
 * settings manager is the only UI that mutates them.
 *
 * Presets are deliberately coarse:
 *   - developer: nothing hidden
 *   - standard:  hide dev-related tabs (terminal / port mapping / Forge)
 *   - minimal:   hide every closable tab (only chat + browse + view + settings)
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

/** What each preset hides. Data-driven so a new devRelated tab is covered. */
const PRESET_HIDDEN: Readonly<Record<Exclude<DockPreset, 'custom'>, readonly DockTabId[]>> = {
  developer: [],
  standard: DOCK_TABS.filter((t) => t.devRelated).map((t) => t.id),
  minimal: DOCK_TABS.filter((t) => t.closable).map((t) => t.id),
}

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

/** The user order merged with the registry default (unknown ids dropped). */
const orderedIds = computed<DockTabId[]>(() => mergeDockOrder(order.value))

/** Reorderable tabs in their current order (hidden ones included). */
const reorderableIds = computed<DockTabId[]>(() =>
  orderedIds.value.filter((id) => REORDERABLE_DOCK_TABS.includes(id)),
)

/** Tabs the manager shows as enabled (hidden set inverted). */
const visibleIds = computed<DockTabId[]>(() =>
  DOCK_TABS.filter((t) => !hidden.value.has(t.id)).map((t) => t.id),
)

function persist(): void {
  writePanelState('dockHidden', [...hidden.value])
  writePanelState('dockOrder', order.value)
  writePanelState('dockPreset', preset.value)
}

/** Apply a preset: reset visibility and order to that preset's defaults. */
export function applyDockPreset(next: Exclude<DockPreset, 'custom'>): void {
  preset.value = next
  hidden.value = new Set(PRESET_HIDDEN[next])
  order.value = []
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
 * Move a reorderable tab by one position. No-op at the ends and for tabs that
 * are not reorderable (primary workspace tabs, settings).
 */
export function moveDockTab(id: DockTabId, delta: -1 | 1): void {
  if (!REORDERABLE_DOCK_TABS.includes(id)) return
  const list = [...orderedIds.value]
  const i = list.indexOf(id)
  const j = i + delta
  if (i < 0 || j < 0 || j >= list.length) return
  ;[list[i], list[j]] = [list[j], list[i]]
  order.value = list
  preset.value = 'custom'
  persist()
}

/** Restore the developer preset (all tabs visible, default order). */
export function resetDockPlugins(): void {
  applyDockPreset('developer')
}

/** True when the tab is hidden by the user (unknown ids are never hidden). */
export function isDockTabHidden(id: string): boolean {
  return isDockTabId(id) && hidden.value.has(id)
}

/** True when the tab can be reordered (and therefore gets up/down buttons). */
export function isDockTabReorderable(id: DockTabId): boolean {
  return REORDERABLE_DOCK_TABS.includes(id)
}

/** Shared state + actions for the dock and the settings manager. */
export function useDockPlugins() {
  return {
    hidden: hiddenIds,
    order: orderedIds,
    reorderableIds,
    visibleIds,
    preset,
    applyPreset: applyDockPreset,
    setVisible: setDockTabVisible,
    toggle: toggleDockTab,
    move: moveDockTab,
    reset: resetDockPlugins,
    isHidden: isDockTabHidden,
    isReorderable: isDockTabReorderable,
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
