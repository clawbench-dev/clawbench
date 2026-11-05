/**
 * The meeting detail's current view, for navigation-origin bookkeeping.
 *
 * Opening a file from the meeting panel switches the left column to the file
 * view. The navigation coordinator records an origin so Back can return — but
 * the origin needs to know WHICH meeting and WHICH tab, otherwise returning
 * lands on the meeting list and the user loses the record they were reading.
 *
 * This module is the hand-off between the panel (which knows the token/tab) and
 * the coordinator (which builds the origin). It is a leaf: no imports from
 * navigation or the panel, so neither side creates a cycle.
 *
 * `pendingReturn` exists because the panel's `active` watcher runs a tick after
 * `switchTab()` while the coordinator consumes the origin synchronously. A
 * consumed origin cannot be read back, so the coordinator also parks the
 * return here; the panel takes it exactly once.
 */
import { ref } from 'vue'

export type MeetingDetailTab = 'minutes' | 'transcript' | 'revised' | 'chat' | 'resources'

export interface MeetingView {
  token: string
  tab: MeetingDetailTab
}

/**
 * The tab a record opens on: 资源.
 *
 * The first thing a reader wants from a finished meeting is what it LEFT
 * BEHIND — the folder, the minutes file, the recording — and the meeting link,
 * which the resource tab now carries. The reading tabs are one click away.
 */
const DEFAULT_TAB: MeetingDetailTab = 'resources'

const _view = ref<MeetingView>({ token: '', tab: DEFAULT_TAB })
let _pendingReturn: MeetingView | null = null
/** One-shot "come back to the create form" request (see setMeetingCreateReturn). */
let _pendingCreateReturn = false

/** Record what the meeting panel is showing right now. */
export function setMeetingView(token: string, tab: MeetingDetailTab): void {
  _view.value = { token, tab }
}

/** The current view; the coordinator copies this into the navigation origin. */
export function getMeetingView(): MeetingView {
  return { ..._view.value }
}

/** Clear the view when the panel leaves the detail (list/stage/unmount). */
export function clearMeetingView(): void {
  _view.value = { token: '', tab: DEFAULT_TAB }
}

/**
 * Park the view a Back press should restore.
 *
 * Set by `beginExternalJump` for the `live` surface, taken by MeetingPanel when
 * it becomes active again. One-shot: taking clears it.
 */
export function setMeetingReturn(token: string, tab: MeetingDetailTab): void {
  _pendingReturn = { token, tab }
}

/** Take (and clear) the parked return target, if any. */
export function takeMeetingReturn(): MeetingView | null {
  const pending = _pendingReturn
  _pendingReturn = null
  return pending
}

/**
 * Park "the user left the CREATE form, bring them back to it".
 *
 * The create form is deliberately abandoned on a normal tab switch (a
 * half-filled form is not a place to come back to), but the relay
 * configuration is the one jump taken FROM that form to finish a field it
 * cannot edit itself — coming back must land on the form again, not on the
 * record list. One-shot: taking clears it.
 */
export function setMeetingCreateReturn(): void {
  _pendingCreateReturn = true
}

/** Take (and clear) the create-form return request, if any. */
export function takeMeetingCreateReturn(): boolean {
  const pending = _pendingCreateReturn
  _pendingCreateReturn = false
  return pending
}

/** @internal Reset module state — for tests only. */
export function _resetMeetingViewForTesting(): void {
  _view.value = { token: '', tab: DEFAULT_TAB }
  _pendingReturn = null
  _pendingCreateReturn = false
}
