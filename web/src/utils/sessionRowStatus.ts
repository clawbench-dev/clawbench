/**
 * Status language shared by the project rows and the other-project cards.
 *
 * Leaf module (no imports): both `SessionList.vue` and `OtherProjectCards.vue`
 * render the same three states, and duplicating the priority rules is how the
 * two panes end up disagreeing about what "unread" means.
 *
 * Priority is pending > unread. They are mutually exclusive by construction
 * (one slot, one state) so the two can never be confused for one another.
 *
 * `pending` outranks `unread` because a session blocked on an approval is the
 * one case where the user is the blocker: it must not be hidden behind "there
 * is a reply you have not looked at".
 *
 * A RUNNING row returns null — the slot stays empty. "Work is progressing" is
 * already carried by the row's comet / ring, and a second marker in the slot
 * would say the same thing twice. Because the slot is `v-if`'d on this value,
 * an empty result renders no element at all, so it costs the row no width.
 * A running row that also has unread replies still shows the unread dot.
 */
export type SessionRowStatus = 'pending' | 'unread' | null

export interface SessionStatusSource {
  pendingApproval?: boolean
  unreadCount?: number
}

export function rowStatus(session: SessionStatusSource | null | undefined): SessionRowStatus {
  if (!session) return null
  if (session.pendingApproval) return 'pending'
  if ((session.unreadCount ?? 0) > 0) return 'unread'
  return null
}

/** i18n key for a status slot's tooltip / aria label ('' when none). */
export function statusLabelKey(status: SessionRowStatus): string {
  if (status === 'pending') return 'session.statusPending'
  if (status === 'unread') return 'session.statusUnread'
  return ''
}
