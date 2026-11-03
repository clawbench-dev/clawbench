/**
 * Which meeting is on screen, and which one this client is hosting.
 *
 * Two plain module strings, not a reactive store: the only reader is the global
 * event handler (which runs outside any component's reactive scope) and the
 * only writers are MeetingPanel's watchers. Keeping this a LEAF — no imports at
 * all — is deliberate: `useGlobalEvents` is imported by nearly everything in
 * the app, so reaching into `useLiveShare` from there would close a module
 * cycle of exactly the kind that already broke other composables.
 *
 * Why two facts instead of one:
 *
 *   - "hosting" decides WHO should be told. The server broadcasts a meeting
 *     event to every client of the project, including the viewers' devices;
 *     only the publisher's client is the host.
 *   - "visible" decides WHETHER. A host looking at the meeting page already
 *     sees the request in the participants dock, so a toast and an OS
 *     notification on top of it would be pure noise.
 */

let hostedToken = ''
let visibleToken = ''

/** Record the meeting this client publishes ('' when not hosting). */
export function setHostedMeetingToken(token: string): void {
  hostedToken = token
}

/** Record the meeting whose stage is currently on screen ('' when none). */
export function setVisibleMeetingToken(token: string): void {
  visibleToken = token
}

/** True when this client is the publisher of `token`. */
export function isHostingMeeting(token: string): boolean {
  return !!token && hostedToken === token
}

/**
 * True when the user is looking at this meeting right now.
 *
 * A hidden document does not count. A backgrounded tab (or a minimized window)
 * keeps the component mounted, so `visibleToken` still matches — but the user
 * is not looking at it, and reaching them is the entire point of the
 * notification.
 */
export function isMeetingPageVisible(token: string): boolean {
  if (!token || visibleToken !== token) return false
  if (typeof document === 'undefined') return false
  return document.visibilityState === 'visible'
}

/**
 * Whether this client should raise a notification for an event in `token`:
 * it hosts the meeting, and the meeting page is not what the user is watching.
 */
export function shouldNotifyMeeting(token: string): boolean {
  return isHostingMeeting(token) && !isMeetingPageVisible(token)
}

/** Test-only: module state outlives a single case. */
export function _resetMeetingVisibilityForTest(): void {
  hostedToken = ''
  visibleToken = ''
}
