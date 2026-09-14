// Opening the session a cross-project jump targets (App.vue hotSwitchProject
// Phase 7). Extracted from App.vue so the ready/not-ready branches are testable
// without mounting the whole app.
//
// The subtle part is WHY this is not a `watch(..., { immediate: true })` that
// closes over its own stop handle. An `immediate` callback runs SYNCHRONOUSLY
// inside watch(), before the `const stopWatch = watch(...)` assignment is
// complete — so calling `stopWatch()` from that first invocation throws
// `ReferenceError: Cannot access 'stopWatch' before initialization`. Vue routes
// watcher-callback errors through callWithErrorHandling, which in production
// only console.errors them, so watch() still returns normally and the watcher
// stays armed. The observable damage:
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
}

/**
 * Open `sessionId` as soon as session identity is available.
 *
 * Returns a stop handle for the (only) case where a watcher was needed; it is a
 * no-op when identity was already resolved.
 */
export function openPendingSessionWhenReady(opts: OpenPendingSessionOptions): () => void {
  const { currentSessionId, sessionId, projectPath, switchTab, switchSession } = opts

  const open = () => {
    switchTab('chat')
    switchSession(sessionId, projectPath)
  }

  // Identity is normally already resolved: Phase 6 awaits initSessionFromAPI()
  // before reaching Phase 7. Open directly — no watcher, so no way for a later
  // identity change to re-open this session or to fight the user's next click.
  if (currentSessionId.value) {
    open()
    return () => {}
  }

  // Identity not ready (initSessionFromAPI found no session). Deliberately NO
  // `immediate` here: the callback must not run before `stop` is initialized.
  const stop = watch(currentSessionId, (id) => {
    if (!id) return
    stop()
    open()
  })
  return stop
}
