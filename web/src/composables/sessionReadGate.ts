// The ONE rule for "the user is looking at this reply, so clear its badge".
//
// History: `GET /api/ai/chat` used to call UpdateLastRead itself, so merely
// fetching messages marked them read. `360241f44` removed that — background
// reloads were silently clearing badges nobody had looked at — and every
// user-intent path grew its own `POST /api/ai/chat/read`. Six copies later the
// rule had drifted: some checked `appInForeground`, one also checked
// `isReplayingEvents`, NONE checked whether the chat panel was actually on
// screen. So a resume landing on the file manager, or a wide-screen layout with
// the chat column collapsed, cleared a badge the user never saw.
//
// The rule is stated once, here:
//
//     read ⇔ the chat panel showing the current session is on screen
//            AND the app is in the foreground
//            AND the turn was not replayed after a disconnect
//
// "On screen" cannot be computed here: it depends on the narrow-layout active
// tab (a ref owned by App.vue) and on the wide-screen collapse state. App.vue
// therefore REGISTERS a predicate and this module only consumes it. That keeps
// the gate a leaf — it imports no component, store or layout module — and keeps
// the layout knowledge in the single place that owns it.
//
// Visibility is deliberately FAIL-OPEN when nothing is registered. This gate can
// only ever restrict behaviour that used to be unconditional, so an
// unregistered host (tests driving the composable directly, or a future
// embedder) must keep the read calls it made before rather than silently losing
// them. App.vue registers unconditionally during setup, so production is always
// gated.
import { markSessionRead } from '@/utils/sessionRead'
import { useAppForeground } from '@/composables/useAppForeground'

/** Predicate answering "is the chat panel showing the current session?". */
let visibilitySource: (() => boolean) | null = null

/**
 * Install the chat-panel visibility predicate.
 *
 * Call unconditionally during setup — a conditional registration silently
 * disables the gate (fail-open), which looks exactly like the bug this module
 * exists to prevent. `null` unregisters, which only tests need.
 */
export function registerChatPanelVisibility(fn: (() => boolean) | null): void {
  visibilitySource = fn
}

/** @internal Whether the chat panel is on screen. Fail-open when unregistered. */
export function isChatPanelOnScreen(): boolean {
  return visibilitySource ? visibilitySource() : true
}

export interface MarkVisibleSessionReadOptions {
  /**
   * The session's owning project. Forwarded for CROSS-PROJECT opens, where the
   * cookie project is not the owner and the backend would 403 without it. Omit
   * for same-project reads: there the cookie fallback IS the proof of ownership,
   * and sending the current projectRoot would 403 a session that belongs
   * elsewhere (e.g. a notification deep link into another project).
   */
  projectPath?: string
  /**
   * True while the global WS is replaying events missed during a disconnect. A
   * completion replayed after the fact must NOT clear the badge: the user was
   * not watching when it finished, which is exactly why it is unread.
   */
  replaying?: boolean
  /**
   * Runs after the read SETTLES, so a list refresh sees the cleared badge.
   *
   * Ordering is part of the contract, not a caller preference: the session list
   * carries the per-session unread count, so refreshing before UpdateLastRead
   * lands reads the pre-read state and re-lights the badge this call exists to
   * clear. Centralising it here is what stops the race recurring per caller.
   */
  onMarked?: () => void
}

/**
 * Mark `sessionId` read if the user is actually looking at it.
 *
 * Returns whether a read was issued. Never throws — markSessionRead already
 * swallows transport and HTTP failures, so this only adds the decision in front
 * of it and must not turn a failed read into a caller-visible error.
 */
export async function markVisibleSessionRead(
  sessionId: string,
  opts: MarkVisibleSessionReadOptions = {},
): Promise<boolean> {
  if (!sessionId) return false
  if (opts.replaying) return false
  const { appInForeground } = useAppForeground()
  if (!appInForeground.value) return false
  if (!isChatPanelOnScreen()) return false

  await markSessionRead(sessionId, opts.projectPath)
  opts.onMarked?.()
  return true
}

/** @internal Reset the registered predicate — for tests only. */
export function _resetChatPanelVisibilityForTest(): void {
  visibilitySource = null
}
