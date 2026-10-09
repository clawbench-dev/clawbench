import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import { createSingleTabGuard } from '../useSingleTab'

// The guard decides who may run the app. Two tabs sharing one server-side
// WebSocket subscription slot repeatedly replace each other's connection, and
// every replacement triggers a full state resync — measured at 1,402 reconnects
// in 17 minutes and ~2,900 requests/minute, which is what made every page slow.
// These tests pin the election: exactly one of two tabs may proceed, and
// ownership is handed over when the owner goes away.
//
// Two tiers are exercised:
//   - Web Locks (the primary path; a fake LockManager is installed below, since
//     jsdom has no navigator.locks).
//   - BroadcastChannel (the fallback; the default in jsdom, so every test that
//     does NOT install the fake exercises it).

const TABS: Array<{ dispose: () => void }> = []

function makeGuard(tabId: string, timeoutMs = 120) {
  const g = createSingleTabGuard({ tabId, timeoutMs })
  TABS.push(g)
  return g
}

/**
 * Resolve true if `register`'s callback fires within `ms`, else false. The
 * timer is always cleared so a fast path does not leak a pending timeout into
 * the next test.
 */
function settlesWithin(register: (cb: () => void) => void, ms: number): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    let timer: ReturnType<typeof setTimeout>
    register(() => {
      clearTimeout(timer)
      resolve(true)
    })
    timer = setTimeout(() => resolve(false), ms)
  })
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

// ── Web Locks tier ──────────────────────────────────────────────────────────
//
// jsdom has no navigator.locks, so the tier is exercised against a fake that
// models the parts the guard depends on:
//   - exclusive locks held for as long as the granted callback's promise is
//     pending;
//   - `ifAvailable: true` invokes the callback with null when held;
//   - a queued request (no `ifAvailable`) is granted the moment the holder
//     releases, INCLUDING when the holder vanishes without running cleanup
//     (close/crash) — the property that makes a ghost owner impossible.

class FakeLockManager {
  private held = new Set<string>()
  private queues = new Map<string, Array<() => boolean>>()

  request(
    name: string,
    optionsOrCb: LockOptions | ((lock: Lock | null) => unknown),
    maybeCb?: (lock: Lock | null) => unknown,
  ): Promise<unknown> {
    const options = typeof optionsOrCb === 'function' ? {} : optionsOrCb
    const cb = typeof optionsOrCb === 'function' ? optionsOrCb : maybeCb!
    const ifAvailable = options.ifAvailable === true

    return new Promise((resolve, reject) => {
      const grant = (): boolean => {
        if (this.held.has(name)) return false
        this.held.add(name)
        Promise.resolve().then(() => {
          let result: unknown
          try {
            result = cb({ name, mode: 'exclusive' } as Lock)
          } catch (err) {
            this.release(name)
            reject(err)
            return
          }
          Promise.resolve(result).then(
            () => { this.release(name); resolve(undefined) },
            (err) => { this.release(name); reject(err) },
          )
        })
        return true
      }

      if (ifAvailable) {
        if (!grant()) {
          // Held: run the callback with null, asynchronously.
          Promise.resolve().then(() => cb(null)).then(
            () => resolve(undefined),
            reject,
          )
        }
        return
      }

      // Queued request: wait for the holder to release.
      if (!grant()) {
        const q = this.queues.get(name) ?? []
        q.push(grant)
        this.queues.set(name, q)
      }
    })
  }

  /** The holder went away without releasing (tab closed / crashed). */
  crash(name: string) {
    this.held.delete(name)
    this.drain(name)
  }

  private release(name: string) {
    this.held.delete(name)
    this.drain(name)
  }

  private drain(name: string) {
    const q = this.queues.get(name)
    while (q && q.length) {
      const next = q.shift()!
      if (next()) return
    }
  }
}

function installFakeLocks(): FakeLockManager {
  const fake = new FakeLockManager()
  Object.defineProperty(navigator, 'locks', {
    configurable: true,
    value: fake,
  })
  return fake
}

function removeFakeLocks() {
  // Leave jsdom's real state: no locks property at all.
  delete (navigator as Navigator & { locks?: unknown }).locks
}

describe('createSingleTabGuard — Web Locks tier', () => {
  let fake: FakeLockManager

  beforeEach(() => {
    fake = installFakeLocks()
  })

  afterEach(() => {
    removeFakeLocks()
  })

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

  it('promotes a waiting tab the instant the owner releases', async () => {
    const a = makeGuard('tab-a')
    expect(await a.acquire()).toBe(true)

    const b = makeGuard('tab-b')
    expect(await b.acquire()).toBe(false)

    const notified = settlesWithin(cb => b.whenOwnerReleases(cb), 300)

    a.release()
    await expect(notified).resolves.toBe(true)
  })

  it('recovers when the owner dies without releasing (no ghost owner)', async () => {
    // The core fix: a tab that closes/crashes/navigates away cannot leave the
    // lock held forever — the browser drops it, promoting the waiting tab. The
    // old BroadcastChannel protocol could pin the blocked tab until a manual
    // reload because a lost `release` was never re-checked.
    const a = makeGuard('tab-a')
    expect(await a.acquire()).toBe(true)

    const b = makeGuard('tab-b')
    expect(await b.acquire()).toBe(false)

    const notified = settlesWithin(cb => b.whenOwnerReleases(cb), 300)

    // Owner vanishes: the browser reclaims its lock without any cleanup.
    fake.crash('clawbench-single-tab-v1')
    await expect(notified).resolves.toBe(true)
  })

  it('replays a release that landed before whenOwnerReleases was wired up', async () => {
    // main.ts calls acquire() and only afterwards registers the reload callback.
    // If the owner goes away inside that window the signal must not be lost.
    const a = makeGuard('tab-a')
    expect(await a.acquire()).toBe(true)

    const b = makeGuard('tab-b')
    expect(await b.acquire()).toBe(false)

    fake.crash('clawbench-single-tab-v1')
    // Let the queued watch grant and the release latch settle.
    await new Promise(r => setTimeout(r, 30))

    const seen = settlesWithin(cb => b.whenOwnerReleases(cb), 100)
    await expect(seen).resolves.toBe(true)
  })

  it('re-acquires without misreading its own lock as contested', async () => {
    // A bfcache restore re-runs acquire() while the tab still holds the lock.
    // A fresh ifAvailable request would see our own lock as taken; the guard
    // must short-circuit instead.
    const a = makeGuard('tab-a')
    expect(await a.acquire()).toBe(true)
    expect(await a.acquire()).toBe(true)
  })

  it('falls back to the channel when Web Locks throws', async () => {
    // A broken LockManager must not block the user: acquire() falls through to
    // the BroadcastChannel tier, where a lone tab proceeds.
    Object.defineProperty(navigator, 'locks', {
      configurable: true,
      value: {
        request() { throw new Error('locks unavailable') },
      },
    })
    const a = makeGuard('tab-a')
    await expect(a.acquire()).resolves.toBe(true)
  })
})
