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
export type CloseChoice = 'tray' | 'exit' | 'exit-stop' | null

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
  /** Button: quit the app AND stop the bundled server (integrated builds only). */
  exitStop: string
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
 * Button order is `[tray, exit, (exit-stop,) cancel]` and the default is
 * `tray`: the non-destructive choice must be the one a stray Enter lands on,
 * and `cancel` is what Esc and the dialog's own close box return. The checkbox
 * starts unchecked — "remember" is an opt-in, not something a hurried click can
 * silently set.
 *
 * `canStopServer` is true only for an integrated install whose bundled server is
 * running. Its extra button exists because "quit" no longer stops that server:
 * without it, closing the window would silently leave a backend running with no
 * visible way to end it.
 */
export function buildCloseDialogOptions(
  labels: CloseDialogLabels,
  options: { canStopServer?: boolean } = {},
): MessageBoxOptions {
  const buttons = [labels.tray, labels.exit]
  if (options.canStopServer) buttons.push(labels.exitStop)
  buttons.push(labels.cancel)
  return {
    type: 'question',
    buttons,
    defaultId: 0,
    cancelId: buttons.length - 1,
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
 * choice alone. "Exit and stop the server" persists as plain `exit`: stopping
 * the server is a one-off action, not a standing close behaviour.
 */
export function interpretCloseDialog(
  result: CloseDialogResult,
  current: CloseAction,
  options: { canStopServer?: boolean } = {},
): { choice: CloseChoice; next: CloseAction } {
  let choice: CloseChoice = null
  if (result.response === 0) choice = 'tray'
  else if (result.response === 1) choice = 'exit'
  else if (options.canStopServer && result.response === 2) choice = 'exit-stop'
  const persisted: CloseAction | null = choice === 'exit-stop' ? 'exit' : choice
  const next: CloseAction = persisted && result.checkboxChecked ? persisted : current
  return { choice, next }
}
