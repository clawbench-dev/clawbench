/**
 * The viewer's nickname, shared by the desktop "join" page and the share page's
 * gate.
 *
 * One key for both: the desktop page writes it before opening the link, so the
 * gate comes up pre-filled and the user is never asked twice. Kept in its own
 * module (rather than inline in either component) because the two surfaces must
 * not be able to drift on the storage key.
 */
export const MEETING_NICK_KEY = 'clawbench_meeting_nick'

/** Longest nickname the server accepts. */
export const MEETING_NICK_MAX = 24

/** Read the remembered nickname, or '' when storage is unavailable. */
export function readMeetingNick(): string {
  try {
    return localStorage.getItem(MEETING_NICK_KEY) ?? ''
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
