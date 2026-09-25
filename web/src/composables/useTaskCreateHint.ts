/**
 * "Create a task with AI instead" hint, shown when the user clicks the "+"
 * button on the task list.
 *
 * The flag lives in localStorage rather than in the server config: it is a
 * per-browser UI nudge, not an account setting, and there is no settings row
 * to re-enable it. Mirrors the WelcomeOverlay dismissal pattern.
 */

export const TASK_CREATE_HINT_KEY = 'clawbench_task_create_hint_dismissed'

/**
 * Whether the user has permanently dismissed the hint.
 *
 * localStorage access is wrapped because it throws in some privacy modes
 * (Safari private browsing) — a storage failure must not break task creation,
 * so it degrades to "not dismissed" (the hint simply shows again).
 */
export function isTaskCreateHintDismissed(): boolean {
  try {
    return localStorage.getItem(TASK_CREATE_HINT_KEY) === 'true'
  } catch {
    return false
  }
}

/** Remember that the user never wants to see the hint again. */
export function dismissTaskCreateHint(): void {
  try {
    localStorage.setItem(TASK_CREATE_HINT_KEY, 'true')
  } catch {
    // Storage unavailable — the hint will reappear next time, which is the
    // harmless failure mode. Never let this break the caller.
  }
}
