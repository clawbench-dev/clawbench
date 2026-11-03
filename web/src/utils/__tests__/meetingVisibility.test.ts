import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import {
  setHostedMeetingToken,
  setVisibleMeetingToken,
  isHostingMeeting,
  isMeetingPageVisible,
  shouldNotifyMeeting,
  _resetMeetingVisibilityForTest,
} from '@/utils/meetingVisibility'

/**
 * The gate that decides whether a meeting event interrupts the host.
 *
 * Two independent facts, and the bug each one prevents:
 *
 *   - "hosting": the server broadcasts a meeting event to EVERY client of the
 *     project, so without this a viewer's own device would be told about the
 *     request they just sent;
 *   - "visible": a host looking at the meeting page already sees the request in
 *     the participants dock, so a toast on top of it is noise.
 */
describe('meetingVisibility', () => {
  let originalVisibility: PropertyDescriptor | undefined

  beforeEach(() => {
    _resetMeetingVisibilityForTest()
    originalVisibility = Object.getOwnPropertyDescriptor(Document.prototype, 'visibilityState')
  })

  afterEach(() => {
    _resetMeetingVisibilityForTest()
    if (originalVisibility) {
      Object.defineProperty(Document.prototype, 'visibilityState', originalVisibility)
    }
  })

  function setDocumentVisibility(state: DocumentVisibilityState) {
    Object.defineProperty(Document.prototype, 'visibilityState', {
      configurable: true,
      get: () => state,
    })
  }

  it('reports hosting only for the meeting this client publishes', () => {
    setHostedMeetingToken('tok-1')
    expect(isHostingMeeting('tok-1')).toBe(true)
    expect(isHostingMeeting('tok-2')).toBe(false)
    expect(isHostingMeeting('')).toBe(false)
  })

  it('reports the page as visible only while the document is visible', () => {
    setVisibleMeetingToken('tok-1')
    setDocumentVisibility('visible')
    expect(isMeetingPageVisible('tok-1')).toBe(true)

    // A backgrounded tab / minimized window keeps the component mounted, but
    // the user is not looking at it — which is exactly when the notification
    // is wanted.
    setDocumentVisibility('hidden')
    expect(isMeetingPageVisible('tok-1')).toBe(false)
  })

  it('notifies only when hosting and not looking at the meeting', () => {
    setDocumentVisibility('visible')
    setHostedMeetingToken('tok-1')

    expect(shouldNotifyMeeting('tok-1')).toBe(true)

    setVisibleMeetingToken('tok-1')
    expect(shouldNotifyMeeting('tok-1')).toBe(false, 'the host is already watching')

    setVisibleMeetingToken('')
    setDocumentVisibility('hidden')
    expect(shouldNotifyMeeting('tok-1')).toBe(true, 'a hidden document is never "watching"')

    expect(shouldNotifyMeeting('tok-2')).toBe(false, 'a viewer is never told')
  })

  it('clears both facts on reset', () => {
    setHostedMeetingToken('tok-1')
    setVisibleMeetingToken('tok-1')
    _resetMeetingVisibilityForTest()

    expect(isHostingMeeting('tok-1')).toBe(false)
    expect(isMeetingPageVisible('tok-1')).toBe(false)
  })

  it('never notifies for an empty token', () => {
    setHostedMeetingToken('')
    expect(shouldNotifyMeeting('')).toBe(false)
  })
})
