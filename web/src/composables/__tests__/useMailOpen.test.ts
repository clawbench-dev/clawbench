import { afterEach, describe, expect, it } from 'vitest'
import {
  _resetMailOpenForTest,
  mailPanelMounted,
  mailPanelUnmounted,
  parkPendingMailOpen,
  takePendingMailOpen,
} from '@/composables/useMailOpen'

// The parked request is module state: a case that leaves one behind would make
// the next mount open a message nobody asked for.
afterEach(() => {
  _resetMailOpenForTest()
})

describe('useMailOpen', () => {
  it('parks a message while no panel is mounted, and hands it out once', () => {
    parkPendingMailOpen(42)
    expect(takePendingMailOpen()).toBe(42)
    expect(takePendingMailOpen()).toBeNull()
  })

  it('does not park while a panel is mounted', () => {
    // A mounted panel hears the event itself; parking as well would leave a
    // stale id that the next mount would open as if it had just been clicked.
    mailPanelMounted()
    try {
      parkPendingMailOpen(42)
      expect(takePendingMailOpen()).toBeNull()
    } finally {
      mailPanelUnmounted()
    }
  })

  it('parks again once the panel is gone', () => {
    mailPanelMounted()
    mailPanelUnmounted()
    parkPendingMailOpen(7)
    expect(takePendingMailOpen()).toBe(7)
  })

  it('ignores an empty id', () => {
    parkPendingMailOpen(0)
    expect(takePendingMailOpen()).toBeNull()
  })
})
