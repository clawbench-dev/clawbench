import type { MessageBoxOptions } from 'electron'

/**
 * What the window's close button does — the pure half of the close flow.
 *
 * `closeFlow.ts` owns the Electron plumbing (the dialog, the tray, quitting);
 * everything that can be decided without Electron lives here so it can be
 * unit-tested. Same split as `windowChrome.ts` / `splashPolicy.ts`.
 */

/**
 * - `ask`  — every close asks (the fresh-install behaviour).
 * - `tray` — hide the window and keep running behind the tray icon.
 * - `exit` — quit the app.
 */
export type CloseAction = 'ask' | 'tray' | 'exit'

/** The action the user picked in the dialog; `null` means they cancelled. */
export type CloseChoice = 'tray' | 'exit' | null

export const DEFAULT_CLOSE_ACTION: CloseAction = 'ask'

const CLOSE_ACTIONS: readonly CloseAction[] = ['ask', 'tray', 'exit']

/**
 * Validate a stored/received value.
 *
 * The store is a dumb persistence layer, so a hand-edited or downgraded config
 * can hold anything; an unrecognized value must fall back to asking rather than
 * reach the flow, where it would have to be special-cased at every branch.
 */
export function normalizeCloseAction(raw: unknown): CloseAction {
  return typeof raw === 'string' && (CLOSE_ACTIONS as readonly string[]).includes(raw)
    ? (raw as CloseAction)
    : DEFAULT_CLOSE_ACTION
}

/** Strings for the dialog, resolved per language by `labels.ts`. */
export interface CloseDialogLabels {
  title: string
  message: string
  detail: string
  /** Button: hide the window and keep the app in the tray. */
  tray: string
  /** Button: quit the app. */
  exit: string
  /** Button: do nothing (Esc / closing the dialog lands here). */
  cancel: string
  /** Checkbox: persist the picked action. */
  remember: string
  /** Shown instead of hiding when no tray icon could be created. */
  trayUnavailable: string
  ok: string
}

/**
 * The dialog.
 *
 * Button order is `[tray, exit, cancel]` and the default is `tray`: the
 * non-destructive choice must be the one a stray Enter lands on, and `cancel`
 * is what Esc and the dialog's own close box return. The checkbox starts
 * unchecked — "remember" is an opt-in, not something a hurried click can
 * silently set.
 */
export function buildCloseDialogOptions(labels: CloseDialogLabels): MessageBoxOptions {
  return {
    type: 'question',
    buttons: [labels.tray, labels.exit, labels.cancel],
    defaultId: 0,
    cancelId: 2,
    noLink: true,
    title: labels.title,
    message: labels.message,
    detail: labels.detail,
    checkboxLabel: labels.remember,
    checkboxChecked: false,
  }
}

export interface CloseDialogResult {
  response: number
  checkboxChecked: boolean
}

/**
 * Turn the dialog's raw result into "what to do now" and "what to persist".
 *
 * `next` equals `current` unless the box was ticked, so the caller can persist
 * unconditionally or compare — either way an unticked dialog leaves the stored
 * choice alone.
 */
export function interpretCloseDialog(
  result: CloseDialogResult,
  current: CloseAction,
): { choice: CloseChoice; next: CloseAction } {
  const choice: CloseChoice =
    result.response === 0 ? 'tray' : result.response === 1 ? 'exit' : null
  const next: CloseAction = choice && result.checkboxChecked ? choice : current
  return { choice, next }
}
