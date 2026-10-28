// Per-project memory of which left-column panel the user was last on, plus the
// in-flight flag that suppresses those writes while a project switch is running.
//
// Background (issue #474): switching projects destroyed the left panel's open
// state. The root cause was two-fold — `leftTab` was persisted under a single
// GLOBAL localStorage key (so project B overwrote project A's tab), and during
// the switch `resetProjectState()` nulled `currentFile`, which fired a watcher
// that "helpfully" fell back to the file manager and rewrote that global key.
//
// This module owns the switch-in-flight flag and the panel-tab resolution rules.
// The storage itself now goes through the panel-state registry
// (utils/panelState.ts), which is the single place that decides a state's key
// name, scope and encoding — the left tab is registered there as the one
// per-project entry. The column's collapsed flag used to live here too; it is a
// global state now and belongs to that registry instead.
//
// Why the flag is needed at all: `hotSwitchProject` resets the module
// singletons (including `currentFile`) BEFORE the new project root is known to
// the watchers, so any watcher that persists on change would write the OLD
// project's panel into the NEW project's key.
import { isDockTabId, type TabId } from '@/composables/dockTabs'
import { readPanelState, writePanelState } from '@/utils/panelState'

/**
 * Depth of in-flight project switches (normally 0 or 1).
 *
 * A counter rather than a boolean because switches CAN overlap: `hotSwitchProject`
 * is invoked from the project picker, the session-completion popup, the git
 * panel and the Android push handlers, so a second switch can start while the
 * first is still awaiting its backend round-trip. With a boolean, whichever
 * finished first would clear the flag and let the still-running switch's watchers
 * persist the wrong project's panel — the very bug this module fixes.
 *
 * Module-level (not a ref) on purpose: it is read from watcher callbacks and
 * never rendered, so making it reactive would only invite it into a computed.
 */
let panelSwitchDepth = 0

/** Mark the start of a project switch. Must run BEFORE the store is reset. */
export function beginPanelSwitch(): void {
  panelSwitchDepth++
}

/**
 * Mark the end of a project switch. Only the last outstanding switch re-enables
 * persistence; redundant calls on an already-zero depth are ignored.
 */
export function endPanelSwitch(): void {
  if (panelSwitchDepth > 0) panelSwitchDepth--
}

/**
 * Whether a project switch is in progress. Persisting watchers must bail out
 * while this is true — see the module doc.
 */
export function isPanelSwitchInFlight(): boolean {
  return panelSwitchDepth > 0
}

/** Remember `tab` as the panel `root` was last on. No-op without a root. */
export function saveProjectPanel(root: string, tab: string): void {
  writePanelState('leftTab', tab, root)
}

/** The remembered panel for `root`, or null when nothing was stored. */
export function loadProjectPanel(root: string): string | null {
  return readPanelState<string | null>('leftTab', root)
}

/**
 * Decide which panel to show for a project.
 *
 * Rules, in order:
 * 1. No memory → the layout's own default: file manager on wide screens (the
 *    left column is always visible there), chat on narrow ones.
 * 2. `chat` on a wide screen → `browse`. Chat is the right-hand pane there and
 *    is never a valid left-column tab, so it cannot be applied.
 * 3. `view` with no file open → `browse`, on BOTH layouts. This is deliberately
 *    layout-independent, unlike rules 1/2/4: the user was working with files, so
 *    the file manager is the useful landing panel even on a narrow screen (where
 *    the layout default is chat). The alternative — rule 1's default — would
 *    drop them on chat and discard the only context the memory still carries.
 *    This is also the one narrowing kept from the "cold start must not jump to
 *    the file viewer" decision (commit e1bb5fb55), which this feature otherwise
 *    supersedes.
 * 4. Anything not in the dock-tab registry → the same default as rule 1.
 * 5. `forge` while the user has hidden the integration entry → the same default
 *    as rule 1. Registry membership alone is not enough here: the forge tab is
 *    still switchable (a notification deep link opens it), but the user has
 *    asked for it not to be part of the dock, so restoring a project must not
 *    land them on a panel with no entry pointing at it (issue #590).
 */
export function resolvePanelTab(
  remembered: string | null,
  isWideScreen: boolean,
  hasOpenFile: boolean,
  forgeHidden = false,
): TabId {
  const fallback: TabId = isWideScreen ? 'browse' : 'chat'
  if (!remembered) return fallback
  if (isWideScreen && remembered === 'chat') return 'browse'
  if (remembered === 'view' && !hasOpenFile) return 'browse'
  if (remembered === 'forge' && forgeHidden) return fallback
  return isDockTabId(remembered) ? remembered : fallback
}

/**
 * @internal Clear all state — for tests only.
 *
 * Resets the in-flight flag as well as storage. A leaked `true` flag makes the
 * save watcher silently stop persisting, which is exactly the failure mode this
 * module exists to fix — so it must be resettable.
 */
export function _clearProjectPanelForTesting(): void {
  panelSwitchDepth = 0
}
