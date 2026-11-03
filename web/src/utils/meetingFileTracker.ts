/**
 * Records which files the host opened while a meeting was running.
 *
 * The meeting record answers "what did we look at" long after the room is gone,
 * so events go to the server as they happen instead of being collected in
 * memory and written at the end: a crash then loses at most one flush interval.
 *
 * Deliberately a leaf module. `useFileNavStack` calls it from the single file
 * entry point, and importing a composable from there would close a dependency
 * cycle (fileNav → liveShare → chat → fileNav).
 */
import { ref } from 'vue'
import { appLog } from '@/utils/appLog.ts'

const TAG = 'MeetingFiles'

/** How often accumulated events are pushed. Small enough to survive a crash,
 *  large enough that switching files quickly is one request, not ten. */
const FLUSH_INTERVAL_MS = 5000

/**
 * Reopening the same file within this window continues the previous interval.
 *
 * Without it, clicking back and forth between two files produces a row per
 * click, and the transcript reads like someone flipping pages rather than
 * someone reading a document.
 */
const COALESCE_WINDOW_MS = 30_000

/** One interval on the wire. `closedAt: 0` means "still open". */
export interface MeetingFileEvent {
  owner: string
  ownerName: string
  path: string
  openedAt: number
  closedAt: number
  source: string
}

let token = ''
let owner = 'host'
let ownerName = ''
let open: MeetingFileEvent | null = null
let pending: MeetingFileEvent[] = []
/** Everything this meeting has seen, for the live transcript. Bounded: a
 *  meeting opens tens of files, not thousands, and the panel shows the tail. */
let history: MeetingFileEvent[] = []
let timer: ReturnType<typeof setInterval> | null = null
/** Serialises flushes so two batches can never interleave and drop events. */
let chain: Promise<void> = Promise.resolve()
/**
 * Bumped on every start/stop. A stop awaits network I/O, and a new meeting can
 * begin during that await; without this the late stop would clear the NEW
 * meeting's token and silently stop recording it.
 */
let generation = 0

/**
 * Bumped whenever the timeline changes, so a `computed` in the live transcript
 * re-runs. A plain module cannot be reactive on its own, and making the tracker
 * a composable would drag it into the dependency cycle it exists to avoid.
 */
export const meetingFilesVersion = ref(0)

function now(): number {
  return Date.now()
}

/** Whether a meeting is being recorded. */
export function isMeetingFileTracking(): boolean {
  return token !== ''
}

/**
 * Starts recording. Idempotent for the same token so a reconnect does not lose
 * the interval that is already open.
 */
export function startMeetingFileTracking(opts: {
  token: string
  owner?: string
  ownerName?: string
}): void {
  if (!opts.token) return
  if (token === opts.token) return
  // A different meeting while one is still open means the previous one was not
  // stopped cleanly (a crash, a reload). Its open interval cannot be attributed
  // to this meeting, so it is dropped rather than reassigned.
  generation++
  if (timer !== null) clearInterval(timer)
  token = opts.token
  owner = opts.owner ?? 'host'
  ownerName = opts.ownerName ?? ''
  open = null
  pending = []
  history = []
  meetingFilesVersion.value++
  timer = setInterval(() => {
    void flushMeetingFiles()
  }, FLUSH_INTERVAL_MS)
}

/**
 * Stops recording: closes the open interval and pushes what is left.
 *
 * Awaited by the caller that ends the meeting, because the publisher socket
 * closing makes the server stamp every still-open interval with the meeting's
 * end — an event that arrives after that would still be stored, but its close
 * time would be this function's clock rather than the server's.
 */
export async function stopMeetingFileTracking(): Promise<void> {
  if (!token) return
  const gen = ++generation
  closeCurrent()
  if (timer !== null) {
    clearInterval(timer)
    timer = null
  }
  await flushMeetingFiles()
  // A flush that was already running when we arrived has just finished; anything
  // it left behind is sent here, before the token is cleared.
  await flushMeetingFiles()
  if (gen !== generation) return // a new meeting started while we were flushing
  token = ''
  owner = 'host'
  ownerName = ''
}

/**
 * Records that `path` became the file being looked at.
 *
 * A new file always closes the previous one: a person reads one document at a
 * time, and an interval that never ends is indistinguishable from a broken
 * clock.
 */
export function noteMeetingFileOpened(path: string, source = 'preview'): void {
  if (!token || !path) return
  if (open && open.path === path) return

  const at = now()
  // Continue the previous visit when it was this same file and recent enough.
  const resumed = pending.find(
    (e) => e.path === path && e.closedAt > 0 && at - e.closedAt <= COALESCE_WINDOW_MS,
  )
  closeCurrent()
  if (resumed) {
    resumed.closedAt = 0
    open = resumed
    meetingFilesVersion.value++
    return
  }
  open = { owner, ownerName, path, openedAt: at, closedAt: 0, source }
  history.push(open)
  if (history.length > 200) history = history.slice(history.length - 200)
  meetingFilesVersion.value++
}

/** Records that the file preview was closed. */
export function noteMeetingFileClosed(): void {
  if (!token || !open) return
  closeCurrent()
}

function closeCurrent(): void {
  if (!open) return
  const closedAt = now()
  const event = open
  open = null
  // A visit shorter than a second is a mis-click or a fast back/forward; it
  // would add noise to a timeline whose unit is minutes.
  if (closedAt - event.openedAt < 1000) {
    const idx = history.indexOf(event)
    if (idx >= 0) history.splice(idx, 1)
    meetingFilesVersion.value++
    return
  }
  event.closedAt = closedAt
  if (!pending.includes(event)) pending.push(event)
  meetingFilesVersion.value++
}

/** The meeting's file intervals so far, in the order they were opened. */
export function meetingFileEntries(): MeetingFileEvent[] {
  return history.map((e) => ({ ...e }))
}

/** Pushes accumulated events. Never throws: a failed batch stays queued. */
export function flushMeetingFiles(): Promise<void> {
  chain = chain.then(sendPending)
  return chain
}

async function sendPending(): Promise<void> {
  if (!token || pending.length === 0) return
  const batch = pending.slice()
  const batchToken = token
  try {
    const resp = await fetch('/api/live-share/notes/files', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: batchToken, files: batch }),
    })
    if (!resp.ok) throw new Error(`status ${resp.status}`)
    // Remove only what this request carried: an event added while it was in
    // flight must stay queued.
    pending = pending.filter((e) => !batch.includes(e))
  } catch (e) {
    // Keep the batch. Losing an interval is a permanent hole in the record; the
    // next tick retries.
    appLog.w(TAG, 'flush failed', e)
  }
}

/** @internal Reset all state — for tests only. */
export function _resetMeetingFileTrackerForTest(): void {
  if (timer !== null) clearInterval(timer)
  timer = null
  generation++
  token = ''
  owner = 'host'
  ownerName = ''
  open = null
  pending = []
  history = []
  chain = Promise.resolve()
  meetingFilesVersion.value++
}

/** @internal Snapshot of the open interval plus queued events — tests only. */
export function _meetingFileSnapshotForTest(): { open: MeetingFileEvent | null; pending: MeetingFileEvent[] } {
  return { open: open ? { ...open } : null, pending: pending.map((e) => ({ ...e })) }
}
