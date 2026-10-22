import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Port forwards must be re-synced on every return to the foreground.
 *
 * A forward requested while the app is in the background cannot be established:
 * Android 12+ rejects `startForegroundService` from the background, so the
 * native service never starts, nothing binds, and nothing retries — the page's
 * own 5s poll only asks an absent service to reconnect. The observed symptom
 * was a port spinning "connecting" for 20+ minutes until the user manually
 * re-entered the app, and even then it did not recover because the only
 * `syncToNative()` call sites are the two startup paths.
 *
 * App.vue has no component test (its guards read it as source text), and the
 * resume plumbing itself is covered by useAppForeground's own suite. This pins
 * the App-level wiring a refactor can silently drop.
 */
describe('port-forward resync on app resume', () => {
  const app = readWebFile('src/App.vue')

  function listenerRegistrationBody() {
    const start = app.indexOf('function registerAppEventListeners()')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start)
    // The function's own closing brace at column 0 ends the body.
    return body.slice(0, body.indexOf('\n}'))
  }

  it('subscribes to onAppResume inside the global listener set', () => {
    const body = listenerRegistrationBody()
    expect(body).toContain('onAppResume(')
  })

  it('re-runs syncToNative from the resume handler', () => {
    const body = listenerRegistrationBody()
    const start = body.indexOf('onAppResume(')
    expect(start).toBeGreaterThan(-1)
    const handler = body.slice(start)
    // Slice to the handler's closing `})` so a syncToNative call elsewhere in
    // the listener set cannot satisfy this.
    const handlerBody = handler.slice(0, handler.indexOf('})'))
    expect(handlerBody).toContain('syncToNative')
  })

  it('gates the resume sync on app mode', () => {
    // syncToNative is a no-op without the native bridge, but calling it in web
    // mode would still hit /api/proxy/ports on every tab focus — and the web
    // path has no local listener to reconcile.
    const body = listenerRegistrationBody()
    const start = body.indexOf('onAppResume(')
    const handlerBody = body.slice(start).slice(0, body.slice(start).indexOf('})'))
    expect(handlerBody).toContain('isAppMode')
  })

  it('imports onAppResume from useAppForeground', () => {
    expect(app).toContain("import { onAppResume } from './composables/useAppForeground.ts'")
  })
})
