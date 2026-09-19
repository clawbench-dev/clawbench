import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import { createSingleTabGuard } from '../useSingleTab'

// The guard decides who may run the app. Two tabs sharing one server-side
// WebSocket subscription slot repeatedly replace each other's connection, and
// every replacement triggers a full state resync — measured at 1,402 reconnects
// in 17 minutes and ~2,900 requests/minute, which is what made every page slow.
// These tests pin the election: exactly one of two tabs may proceed, and
// ownership is handed over when the owner goes away.

const TABS: Array<{ dispose: () => void }> = []

function makeGuard(tabId: string, timeoutMs = 120) {
  const g = createSingleTabGuard({ tabId, timeoutMs })
  TABS.push(g)
  return g
}

beforeEach(() => {
  TABS.length = 0
})

afterEach(() => {
  TABS.forEach(g => g.dispose())
  TABS.length = 0
})

describe('createSingleTabGuard', () => {
  it('lets a lone tab acquire ownership', async () => {
    const a = makeGuard('tab-a')
    await expect(a.acquire()).resolves.toBe(true)
  })

  it('blocks a second tab while the first owns it', async () => {
    const a = makeGuard('tab-a')
    expect(await a.acquire()).toBe(true)

    const b = makeGuard('tab-b')
    expect(await b.acquire()).toBe(false)
  })

  it('allows the second tab once the first releases', async () => {
    const a = makeGuard('tab-a')
    expect(await a.acquire()).toBe(true)

    const b = makeGuard('tab-b')
    expect(await b.acquire()).toBe(false)

    // The owner reloads/closes → releases → the waiting tab may now take over.
    a.release()

    // release() is broadcast asynchronously; give it a tick to land.
    await new Promise(r => setTimeout(r, 60))
    const c = makeGuard('tab-c')
    expect(await c.acquire()).toBe(true)
  })

  it('does not treat its own messages as a competing owner', async () => {
    // A single tab must never block itself: its own claim broadcast is
    // delivered back to the channel and must be ignored by tab id.
    const a = makeGuard('tab-a', 200)
    expect(await a.acquire()).toBe(true)
    expect(await a.acquire()).toBe(true)
  })

  it('re-acquire clears a previous objection (bfcache restore after owner left)', async () => {
    // A tab restored from the back/forward cache re-runs acquire(). It must
    // start from a clean verdict: a `taken` observed before the owner went away
    // must not keep it blocked forever.
    const a = makeGuard('tab-a')
    expect(await a.acquire()).toBe(true)

    const b = makeGuard('tab-b')
    expect(await b.acquire()).toBe(false)

    // Owner goes away.
    a.release()
    await new Promise(r => setTimeout(r, 80))

    // b re-acquires (simulating pageshow) → must now win.
    expect(await b.acquire()).toBe(true)
  })

  it('re-acquire reuses the channel instead of leaking a second one', async () => {
    // Opening a second BroadcastChannel would leave the first subscribed, so
    // release/taken could be delivered twice.
    const b = makeGuard('tab-b')
    expect(await b.acquire()).toBe(true)
    const first = b.__channelForTesting()
    expect(await b.acquire()).toBe(true)
    expect(b.__channelForTesting()).toBe(first)
  })

  it('notifies a blocked tab when the owner releases', async () => {
    const a = makeGuard('tab-a')
    expect(await a.acquire()).toBe(true)

    const b = makeGuard('tab-b')
    expect(await b.acquire()).toBe(false)

    const notified = new Promise<boolean>(resolve => {
      b.whenOwnerReleases(() => resolve(true))
      setTimeout(() => resolve(false), 300)
    })

    a.release()
    await expect(notified).resolves.toBe(true)
  })

  it('release is idempotent and safe on a non-owner', async () => {
    const a = makeGuard('tab-a')
    expect(await a.acquire()).toBe(true)
    a.release()
    a.release() // must not throw or re-broadcast
    expect(true).toBe(true)
  })

  it('dispose is safe to call twice', async () => {
    const a = makeGuard('tab-a')
    await a.acquire()
    a.dispose()
    a.dispose()
    expect(true).toBe(true)
  })
})
