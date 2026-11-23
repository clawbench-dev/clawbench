/**
 * Choosing which session takes over when the open one disappears remotely.
 *
 * Lives here, outside App.vue, because App.vue has no component test (mounting
 * it needs the whole app graph) and this is the one decision in the
 * "session was archived/destroyed on another device" flow that can be wrong in
 * a way nobody notices until the user is staring at the wrong session.
 *
 * The rule is deliberately identical to what a LOCAL archive does (see
 * useChatSession.archiveSession): take the first remaining session, and create a
 * fresh one when the project has none left. Sharing the rule is the point —
 * "I archived it here" and "another device archived it" must be indistinguishable
 * to the user, or the same action would leave them on different screens
 * depending on which device they happened to press the button on.
 */

export interface RemovableSessionRow {
  id: string
}

/** What the caller should do after a remote removal of the open session. */
export type RemoteRemovalOutcome =
  /** Nothing to do: the session was not the one open, or it is still open. */
  | { action: 'none' }
  /** Switch to another session that still exists. */
  | { action: 'switch'; sessionId: string }
  /** No sessions remain — create one, exactly as a local archive of the last
   *  session does. */
  | { action: 'create' }

/**
 * Decide how to leave a session that was removed on another device.
 *
 * @param removedId      the session the server reported as removed
 * @param currentId      the session this client currently has open
 * @param remaining      the current project's sessions AFTER the removal (as
 *                       returned by the server; the removed one is normally
 *                       already absent, but it is filtered out defensively so a
 *                       stale read can never send us back into it)
 */
export function planRemoteRemoval(
  removedId: string,
  currentId: string,
  remaining: ReadonlyArray<RemovableSessionRow>,
): RemoteRemovalOutcome {
  if (!removedId) return { action: 'none' }
  // The event is broadcast to every client, including those watching another
  // project. Only the session that is actually open can leave the user looking
  // at a ghost, so anything else is just a list refresh.
  if (removedId !== currentId) return { action: 'none' }

  // A stale/partial response that still contains the removed session must not
  // be treated as "it is fine, stay" — filter it out and pick another.
  const next = remaining.find(s => s?.id && s.id !== removedId)
  if (next) return { action: 'switch', sessionId: next.id }
  return { action: 'create' }
}
