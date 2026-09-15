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
  const { currentSessionId, sessionId, projectPath, switchTab, switchSession, isStillRelevant } = opts

  const relevant = () => (isStillRelevant ? isStillRelevant() : true)

  const open = () => {
    switchTab('chat')
    switchSession(sessionId, projectPath)
  }

  // Identity is normally already resolved: Phase 6 awaits initSessionFromAPI()
  // before reaching Phase 7. Open directly — no watcher, so no way for a later
  // identity change to re-open this session or to fight the user's next click.
  if (currentSessionId.value) {
    if (relevant()) open()
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
    open()
  })
  return () => {
    done = true
    stop()
  }
}
