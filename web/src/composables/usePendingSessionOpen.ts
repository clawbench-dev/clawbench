// Opening the session a cross-project jump targets (App.vue hotSwitchProject
// Phase 7). Extracted from App.vue so the ready/not-ready branches are testable
// without mounting the whole app.
//
// The subtle part is WHY this is not a `watch(..., { immediate: true })` that
// closes over its own stop handle. An `immediate` callback runs SYNCHRONOUSLY
// inside watch(), before the `const stopWatch = watch(...)` assignment is
// complete — so calling `stopWatch()` from that first invocation throws
// `ReferenceError: Cannot access 'stopWatch' before initialization`.
//
// That error does NOT reach the caller in production: Phase 7 runs after
// several awaits, so no component instance is current and Vue's handleError()
// (which only consults app.config.errorHandler inside `if (instance)`) falls
// through to logError(), which console.errors and does NOT rethrow. watch()
// therefore returns normally and the watcher stays armed. (In a dev build the
// same path rethrows instead — a crash rather than a silent no-op.) The
// observable damage:
//   1. The cross-project session is never opened, so its unread badge is never
//      cleared (mark-as-read runs inside switchSession).
//   2. The stale watcher later fires on the NEXT identity change (e.g. the user
//      clicking another session) and pulls the app back to the pending session —
//      a visible content flicker that looks like the click "didn't take", and
//      which only then clears the badge.
//   3. That second invocation finally calls stopWatch(), so a further click
//      works. Hence the reported "click once to flicker, click again to switch".
import { watch, type Ref } from 'vue'

/**
 * The session a cross-project jump is about to open, published BEFORE the
 * keyed subtree rebuild.
 *
 * Why a module-level value instead of just calling switchSession afterwards:
 * `hotSwitchProject` clears identity, rebuilds the whole `.app-container`
 * subtree, and the remounted chat panel immediately runs its first
 * `loadHistory()`. With no session id that call takes the RECOVERY branch and
 * asks the backend for "this project's most recent session" — so the user sees
 * a DIFFERENT conversation's messages for one frame, before Phase 7 finally
 * switches to the one they clicked. That intermediate render is the reported
 * cross-project flicker (a same-project switch never hits it: identity is
 * already the target, so there is exactly one load).
 *
 * Publishing the target here lets that first load fetch the clicked session
 * directly, so the whole jump costs one request and never renders anything
 * else. `takePendingOpenSession` is a read-and-clear: the value is consumed by
 * the first `loadHistory` that needs a recovery target, and `hotSwitchProject`
 * clears it on every exit path so it cannot leak into a later, unrelated load.
 */
let pendingOpenSessionId: string | null = null

/** Publish the session an upcoming cross-project jump will open. */
export function armPendingOpenSession(sessionId: string | null | undefined): void {
  pendingOpenSessionId = sessionId || null
}

/**
 * Read and clear the armed session. Consume-once on purpose: it is a
 * replacement for the "which session should I recover?" answer, not a standing
 * preference — a later load with an empty identity (a genuinely new project)
 * must fall back to the normal recent/latest lookup.
 */
export function takePendingOpenSession(): string | null {
  const id = pendingOpenSessionId
  pendingOpenSessionId = null
  return id
}

/**
 * Drop the armed session. `sessionId` scopes the clear so a slow, superseded
 * switch cannot disarm the one that superseded it.
 */
export function clearPendingOpenSession(sessionId?: string | null): void {
  if (sessionId && pendingOpenSessionId !== sessionId) return
  pendingOpenSessionId = null
}

/** @internal Inspect the armed value without consuming it — for tests only. */
export function peekPendingOpenSession(): string | null {
  return pendingOpenSessionId
}

export interface OpenPendingSessionOptions {
  /** Live session identity — resolved by initSessionFromAPI() before this runs. */
  currentSessionId: Ref<string>
  /** Session the user picked in the cross-project tab. */
  sessionId: string
  /**
   * Owning project of that session. Forwarded to switchSession so the backend
   * can verify ownership when marking read; without it the backend falls back
   * to the cookie project, 403s, and the unread badge stays stuck.
   */
  projectPath?: string
  /** Activates the chat tab. Injected so this stays free of App.vue internals. */
  switchTab: (tab: string) => void
  /** Opens the session (App.vue passes sessionIdentity.switchSession). */
  switchSession: (sessionId: string, projectPath?: string) => void | Promise<void>
  /**
   * What to do when identity ALREADY equals the target — i.e. the jump's first
   * history load fetched it (the armed target) and the conversation is on
   * screen.
   *
   * Calling `switchSession` in that case is pure damage: it starts with
   * `dispatch({type:'clear'})` and refetches the same window, so the user sees
   * the messages they just got wiped and a skeleton put back — the second half
   * of the cross-project flicker. Skipping it must still reproduce the two
   * side effects `switchSession` owns: marking the session read (the whole
   * point of opening a row from another project) and refreshing the list so the
   * badge clears in the right order.
   */
  markReadAndRefresh?: () => void | Promise<void>
  /**
   * Relevance guard, consulted before opening and again on every identity
   * change while waiting. Callers pass "am I still on the project this session
   * belongs to?".
   *
   * This is what stops a fallback watcher from outliving its navigation. When
   * identity is unresolved at Phase 7 a watcher must be armed (ChatPanel's
   * loadHistory recovery resolves identity slightly later), but that watcher
   * would otherwise still be armed when the user switches to an UNRELATED
   * project — and the new project's identity resolution would then open this
   * stale session there. Bounding it by project closes that hole.
   *
   * Defaults to always-relevant when omitted.
   */
  isStillRelevant?: () => boolean
}

/**
 * Open `sessionId` as soon as session identity is available.
 *
 * Returns a stop handle for the (only) case where a watcher was needed; it is a
 * no-op when identity was already resolved.
 */
export function openPendingSessionWhenReady(opts: OpenPendingSessionOptions): () => void {
  const { currentSessionId, sessionId, projectPath, switchTab, switchSession, isStillRelevant, markReadAndRefresh } = opts

  const relevant = () => (isStillRelevant ? isStillRelevant() : true)

  const open = () => {
    switchTab('chat')
    switchSession(sessionId, projectPath)
  }

  /**
   * The target is already loaded. Do the read-marking work switchSession would
   * have done, but do NOT clear/reload — that would throw away the conversation
   * the user is now looking at.
   */
  const adopt = () => {
    switchTab('chat')
    if (markReadAndRefresh) void markReadAndRefresh()
    else open()
  }

  // Identity is normally already resolved: Phase 6 awaits initSessionFromAPI()
  // before reaching Phase 7. If it resolved to the TARGET (the armed cross-
  // project session), adopt it; if it resolved to some other session, open the
  // target. Either way no watcher is left behind, so a later identity change
  // can neither re-open this session nor fight the user's next click.
  if (currentSessionId.value) {
    if (!relevant()) return () => {}
    if (currentSessionId.value === sessionId) adopt()
    else open()
    return () => {}
  }

  // Identity not ready (initSessionFromAPI found no session — a failed/aborted
  // fetch). Deliberately NO `immediate` here: the callback must not run before
  // `stop` is initialized. The watcher self-stops on its first firing, so it can
  // neither open twice nor stay armed indefinitely.
  let done = false
  const stop = watch(currentSessionId, (id) => {
    if (done) return
    // Navigation was superseded (different project now) — disarm without opening.
    if (!relevant()) {
      done = true
      stop()
      return
    }
    if (!id) return
    done = true
    stop()
    // The load that finally resolved identity may itself have loaded the target
    // (the armed cross-project session) — same rule as the resolved branch.
    if (id === sessionId) adopt()
    else open()
  })
  return () => {
    done = true
    stop()
  }
}
