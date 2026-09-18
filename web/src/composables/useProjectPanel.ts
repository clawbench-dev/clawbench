// Per-project memory of which left-column panel the user was last on.
//
// Background (issue #474): switching projects destroyed the left panel's open
// state. The root cause was two-fold — `leftTab` was persisted under a single
// GLOBAL localStorage key (so project B overwrote project A's tab), and during
// the switch `resetProjectState()` nulled `currentFile`, which fired a watcher
// that "helpfully" fell back to the file manager and rewrote that global key.
//
// This module owns the per-project key instead, plus the in-flight flag that
// suppresses those two writes while a switch is running.
//
// Why the flag is needed at all: `hotSwitchProject` resets the module
// singletons (including `currentFile`) BEFORE the new project root is known to
// the watchers, so any watcher that persists on change would write the OLD
// project's panel into the NEW project's key.
import { isDockTabId } from '@/composables/dockTabs'

/** localStorage key prefix; the project root is appended. */
export const PROJECT_PANEL_PREFIX = 'clawbench-project-panel:'

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
  if (!root) return
  try {
    localStorage.setItem(PROJECT_PANEL_PREFIX + root, tab)
  } catch { /* ignore */ }
}

/** The remembered panel for `root`, or null when nothing was stored. */
export function loadProjectPanel(root: string): string | null {
  if (!root) return null
  try {
    return localStorage.getItem(PROJECT_PANEL_PREFIX + root)
  } catch { return null }
}

/**
 * Decide which panel to show for a project.
 *
 * Rules, in order:
 * 1. No memory → the layout's own default: file manager on wide screens (the
 *    left column is always visible there), chat on narrow ones.
 * 2. `chat` on a wide screen → `browse`. Chat is the right-hand pane there and
 *    is never a valid left-column tab, so it cannot be applied.
 * 3. `view` with no file open → `browse`. Landing on an empty viewer is worse
 *    than landing on the file manager; this is the one narrowing kept from the
 *    "cold start must not jump to the file viewer" decision (commit e1bb5fb55),
 *    which this feature otherwise supersedes.
 * 4. Anything not in the dock-tab registry → the same default as rule 1.
 */
export function resolvePanelTab(
  remembered: string | null,
  isWideScreen: boolean,
  hasOpenFile: boolean,
): string {
  const fallback = isWideScreen ? 'browse' : 'chat'
  if (!remembered) return fallback
  if (isWideScreen && remembered === 'chat') return 'browse'
  if (remembered === 'view' && !hasOpenFile) return 'browse'
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
