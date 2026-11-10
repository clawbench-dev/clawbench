/**
 * Per-project record of which terminal tabs are open.
 *
 * The shell itself lives on the SERVER: the PTY keeps running after its
 * WebSocket client goes away (idle timeout defaults to "never") and keeps
 * filling a replay ring buffer. What does not survive locally is everything
 * describing that shell — tab title, working directory and above all the
 * backend session id — because it lives in the terminal panel component, and
 * the whole app tree is keyed by project path (App.vue `:key="projectKey"`).
 * So both a project switch AND a page reload lose the ids, and the shells
 * become unreachable orphans.
 *
 * This module is what crosses both boundaries. The panel reattaches by id on
 * mount (the server then replays the screen the shell kept producing) and
 * writes the record back whenever the tabs change.
 *
 * It is deliberately BROWSER storage, not server memory. The tab list belongs
 * to one device: the server would hand the same shells to every device signed
 * in to it, and a terminal session accepts a single client (a second connection
 * kicks the first), so a phone and a desktop would keep knocking each other
 * off. Browser storage also survives a page reload, a browser restart and an
 * app restart — everything a single device needs.
 */

export interface SavedTerminalTab {
  /**
   * Backend PTY session id. Empty when the tab never connected, or when its
   * shell already exited (the server clears it on exit). An empty id is not a
   * reason to drop the tab: restoring it opens a fresh shell in `cwd`, which is
   * what the user expects from a tab they left behind.
   */
  sessionId: string
  cwd: string
  title: string
}

export interface SavedTerminalTabs {
  /**
   * The tabs that were open, in order. An EMPTY array is meaningful: the user
   * had closed every tab, so coming back must show the empty state rather than
   * silently spawning a new shell.
   */
  tabs: SavedTerminalTab[]
  /** Index into `tabs` of the tab that was active, so the user lands back on it. */
  activeIndex: number
}

/** Prefix + project path. The path is unique per project, so no escaping is needed. */
const KEY_PREFIX = 'clawbench-terminal-tabs:'

function storageKey(projectPath: string): string {
  return KEY_PREFIX + projectPath
}

function isSavedTab(value: unknown): value is SavedTerminalTab {
  if (!value || typeof value !== 'object') return false
  const tab = value as Record<string, unknown>
  return typeof tab.sessionId === 'string'
    && typeof tab.cwd === 'string'
    && typeof tab.title === 'string'
}

/**
 * Anything read back from storage crosses a version boundary (an older build
 * may have written a different shape, and storage is user-editable), so it is
 * validated instead of trusted — a malformed `tabs` would break the panel's
 * first render, not just lose a tab.
 */
function isSavedTerminalTabs(value: unknown): value is SavedTerminalTabs {
  if (!value || typeof value !== 'object') return false
  const snap = value as Record<string, unknown>
  if (!Array.isArray(snap.tabs) || !snap.tabs.every(isSavedTab)) return false
  return typeof snap.activeIndex === 'number' && Number.isFinite(snap.activeIndex)
}

/**
 * Remember a project's tabs. A falsy project path is ignored: before the first
 * project is resolved there is nothing to key the record by, and saving under
 * an empty key would leak one project's shells into another.
 *
 * Storage failures (quota, disabled storage) are swallowed on purpose: losing
 * the record costs the user their tab list on the next load, while throwing
 * here would take down the terminal panel mid-session.
 */
export function saveTerminalTabs(projectPath: string, snapshot: SavedTerminalTabs): void {
  if (!projectPath) return
  try {
    localStorage.setItem(storageKey(projectPath), JSON.stringify(snapshot))
  } catch { /* storage unavailable — the tabs still work for this session */ }
}

/** The tabs recorded for a project, or null when there is no usable record. */
export function readTerminalTabs(projectPath: string): SavedTerminalTabs | null {
  if (!projectPath) return null
  try {
    const raw = localStorage.getItem(storageKey(projectPath))
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    return isSavedTerminalTabs(parsed) ? parsed : null
  } catch {
    // Unparsable record (older build, manual edit) — fall back to a fresh tab
    // instead of failing to open the panel.
    return null
  }
}
