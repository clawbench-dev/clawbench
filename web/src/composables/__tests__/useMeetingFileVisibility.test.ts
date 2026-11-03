import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, type Ref } from 'vue'
import { useMeetingFileVisibility } from '@/composables/useMeetingFileVisibility'
import {
  meetingFileEntries,
  startMeetingFileTracking,
  _meetingFileSnapshotForTest,
  _resetMeetingFileTrackerForTest,
} from '@/utils/meetingFileTracker'

/**
 * The reported bug: a file opened BEFORE the meeting never fires openFile()
 * during it, so switching to the file tab mid-meeting left the meeting record
 * ("会议期间打开的文件") empty. These cases pin the visibility half of the
 * timeline: on screen starts a visit, off screen ends it.
 */

let sent: Array<{ token: string; files: Array<Record<string, unknown>> }> = []

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-03T10:00:00Z'))
  sent = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (_url: string, init: { body: string }) => {
      sent.push(JSON.parse(init.body))
      return { ok: true, status: 200 }
    }),
  )
  _resetMeetingFileTrackerForTest()
})

afterEach(() => {
  _resetMeetingFileTrackerForTest()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

/**
 * Mount the watcher in its own scope so cases cannot leak into each other.
 * The viewer starts hidden (`''`) — the watcher reacts to transitions, exactly
 * as it does in the app, where the file tab is never the first thing on screen.
 */
function mount(path: Ref<string>) {
  const scope = effectScope()
  scope.run(() => useMeetingFileVisibility(() => path.value))
  return scope
}

describe('useMeetingFileVisibility', () => {
  it('starts an interval for a file that was already open when the meeting began', async () => {
    const path = ref('')
    const scope = mount(path)
    startMeetingFileTracking({ token: 'tok' })
    expect(_meetingFileSnapshotForTest().open, 'nothing on screen yet').toBeNull()

    // The viewer was opened before the meeting; switching to it is the only
    // event the tracker ever sees for this file.
    path.value = 'docs/a.md'
    await nextTick()

    expect(_meetingFileSnapshotForTest().open).toMatchObject({
      path: 'docs/a.md',
      openedAt: Date.now(),
      closedAt: 0,
      source: 'preview',
    })
    scope.stop()
  })

  it('ends the interval when the viewer leaves the screen', async () => {
    const path = ref('')
    const scope = mount(path)
    startMeetingFileTracking({ token: 'tok' })
    path.value = 'docs/a.md'
    await nextTick()
    const openedAt = Date.now()

    vi.advanceTimersByTime(3000)
    path.value = ''
    await nextTick()

    const snap = _meetingFileSnapshotForTest()
    expect(snap.open).toBeNull()
    expect(snap.pending).toHaveLength(1)
    expect(snap.pending[0]).toMatchObject({ path: 'docs/a.md', openedAt, closedAt: openedAt + 3000 })
    scope.stop()
  })

  it('moves the interval when another file comes on screen', async () => {
    const path = ref('')
    const scope = mount(path)
    startMeetingFileTracking({ token: 'tok' })
    path.value = 'docs/a.md'
    await nextTick()
    const openedAt = Date.now()

    vi.advanceTimersByTime(3000)
    path.value = 'docs/b.md'
    await nextTick()

    const snap = _meetingFileSnapshotForTest()
    expect(snap.pending).toHaveLength(1)
    expect(snap.pending[0]).toMatchObject({ path: 'docs/a.md', closedAt: openedAt + 3000 })
    expect(snap.open).toMatchObject({ path: 'docs/b.md', closedAt: 0 })
    scope.stop()
  })

  it('does not split a visit when the host flips to another tab and back', async () => {
    const path = ref('')
    const scope = mount(path)
    startMeetingFileTracking({ token: 'tok' })
    path.value = 'docs/a.md'
    await nextTick()
    const openedAt = Date.now()

    // Below the tracker's coalesce window, so the same file must resume its
    // interval instead of producing an open/close pair per tab flip.
    vi.advanceTimersByTime(2000)
    path.value = ''
    await nextTick()
    vi.advanceTimersByTime(1000)
    path.value = 'docs/a.md'
    await nextTick()

    const snap = _meetingFileSnapshotForTest()
    // One visit, not two: the coalesced event is still the open one.
    expect(meetingFileEntries()).toHaveLength(1)
    expect(snap.pending).toHaveLength(1)
    expect(snap.open).toMatchObject({ path: 'docs/a.md', openedAt, closedAt: 0 })
    scope.stop()
  })

  it('records nothing outside a meeting', async () => {
    const path = ref('')
    const scope = mount(path)
    path.value = 'docs/a.md'
    await nextTick()
    path.value = 'docs/b.md'
    await nextTick()

    expect(_meetingFileSnapshotForTest().open).toBeNull()
    expect(_meetingFileSnapshotForTest().pending).toHaveLength(0)
    expect(sent).toHaveLength(0)
    scope.stop()
  })
})
