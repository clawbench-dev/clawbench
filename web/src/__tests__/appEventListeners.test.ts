import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * App.vue's global listeners must be torn down as completely as they are
 * installed.
 *
 * `registerAppEventListeners` and `unregisterAppEventListeners` are two
 * hand-written lists in a 4,000-line file, and they drifted: four listeners
 * (theme / font / show-hidden / sort change) were added to the first and never
 * to the second, so every mount-after-unmount left a set of closures writing to
 * a disposed component. The guard flag was module-level and never cleared, so
 * the remounted instance did not register its own handlers at all and ran on
 * the old ones.
 *
 * App.vue has no component test (mounting it needs the whole app graph), so the
 * symmetry is pinned by comparing the two call-site sets in the source — the
 * same technique as `portForwardResumeResync.test.ts`.
 */
describe('App.vue global event listener symmetry', () => {
  const app = readWebFile('src/App.vue')

  function functionBody(name: string): string {
    const start = app.indexOf(`function ${name}(`)
    expect(start, `${name} not found`).toBeGreaterThan(-1)
    const rest = app.slice(start + 1)
    // The function's own closing brace at column 0 ends the body.
    return rest.slice(0, rest.indexOf('\n}'))
  }

  /** `target.addEventListener('x', handler)` → `"target:event:handler"`. */
  function callSites(body: string, method: 'addEventListener' | 'removeEventListener'): string[] {
    const re = new RegExp(`\\b(window|document)\\.${method}\\(\\s*'([^']+)'\\s*,\\s*([A-Za-z0-9_$]+)`, 'g')
    const out: string[] = []
    for (const m of body.matchAll(re)) out.push(`${m[1]}:${m[2]}:${m[3]}`)
    return out.sort()
  }

  const added = callSites(functionBody('registerAppEventListeners'), 'addEventListener')
  const removed = callSites(functionBody('unregisterAppEventListeners'), 'removeEventListener')

  it('finds both lists (a rename must not turn this guard into a no-op)', () => {
    expect(added.length).toBeGreaterThanOrEqual(15)
    expect(removed.length).toBe(added.length)
  })

  it('removes exactly the listeners it adds, on the same target with the same handler', () => {
    expect(removed).toEqual(added)
  })

  it('tears the four settings-driven listeners down too', () => {
    // The specific regression: these four were added, never removed.
    for (const event of [
      'clawbench-theme-change',
      'clawbench-font-change',
      'clawbench-showhidden-change',
      'clawbench-sort-change',
    ]) {
      expect(added.some((s) => s.startsWith(`window:${event}:`)), `${event} not registered`).toBe(true)
      expect(removed.some((s) => s.startsWith(`window:${event}:`)), `${event} not removed`).toBe(true)
    }
  })

  it('resets the registration guard on teardown so a remount registers again', () => {
    // A module-level `let appEventListenersRegistered = false` that is only ever
    // set to true makes the second instance silently reuse the first one's
    // (unmounted) closures.
    const body = functionBody('unregisterAppEventListeners')
    expect(body).toMatch(/appEventListenersRegistered\s*=\s*false/)
  })

  it('is actually called from onUnmounted', () => {
    const start = app.indexOf('onUnmounted(() => {\n    stopLocalLinkGuard')
    expect(start, 'the app teardown hook moved — update this guard').toBeGreaterThan(-1)
    const rest = app.slice(start)
    const unmount = rest.slice(0, rest.indexOf('\n})'))
    expect(unmount).toContain('unregisterAppEventListeners()')
  })
})
