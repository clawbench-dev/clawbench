import { GROUP_USER_TARGET_NAME } from '@/utils/groupRouting'

/**
 * The viewer's nickname on the public share page.
 *
 * The share page is unauthenticated: the link is the only credential, and the
 * person opening it usually has no account on this server. So this name is a
 * GUEST display name stored per device/origin — it cannot be read from the
 * account's user.nickname. The desktop join page writes the account nickname
 * into this same key before opening the link, so the common same-origin case
 * comes up pre-filled.
 *
 * Kept in its own module (rather than inline in either component) because the
 * two surfaces must not be able to drift on the storage key or the limit.
 */
export const MEETING_NICK_KEY = 'clawbench_meeting_nick'

/**
 * Longest nickname the server accepts. Kept equal to model.MaxUserNicknameRunes
 * (32) so a nickname valid in Settings is also valid when joining a meeting.
 */
export const MEETING_NICK_MAX = 32

/**
 * Read the remembered nickname, or '' when storage is unavailable.
 *
 * A remembered value equal to the fallback literal is treated as unset: that
 * literal is not an identity, and pre-filling it would make every viewer who
 * never configured a name show up as "User" — indistinguishable in the
 * publisher's audience list. A guest may still TYPE it deliberately; only the
 * automatic pre-fill refuses to invent a shared name.
 */
export function readMeetingNick(): string {
  try {
    const v = localStorage.getItem(MEETING_NICK_KEY) ?? ''
    return v.trim() === GROUP_USER_TARGET_NAME ? '' : v
  } catch {
    return ''
  }
}

/** Persist the nickname. Failures are ignored: it is a convenience. */
export function saveMeetingNick(name: string): void {
  try {
    localStorage.setItem(MEETING_NICK_KEY, name)
  } catch {
    /* private mode */
  }
}

/** A nickname is valid when it is non-blank and within the server's limit. */
export function isValidMeetingNick(name: string): boolean {
  const trimmed = name.trim()
  return trimmed.length > 0 && [...trimmed].length <= MEETING_NICK_MAX
}
