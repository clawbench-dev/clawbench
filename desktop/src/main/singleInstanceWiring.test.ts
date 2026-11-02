import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guards for the single-instance lock.
 *
 * `index.ts` imports `electron` at module scope, so no unit test can load it.
 * The failure mode is silent and looks like a UI bug: a second copy starts
 * normally and brings its own tray icon and its own floating capsule, both
 * placed from the same stored bounds — drag one and the other is left behind,
 * so the user sees "two capsules out of nowhere". It is easy to trigger by
 * accident, because with the capsule feature on, closing the main window leaves
 * only a tray icon and the app looks closed.
 *
 * Same convention as `floatingWiring.test.ts` / `splashWiring.test.ts`.
 */

const repoRoot = resolve(__dirname, '../../..')
const readRepoFile = (rel: string): string => readFileSync(resolve(repoRoot, rel), 'utf8')

const INDEX = 'desktop/src/main/index.ts'
const FLOATING = 'desktop/src/main/floating.ts'

describe('the shell runs as one instance', () => {
  it('claims the lock before anything can touch shared state', () => {
    const src = readRepoFile(INDEX)
    const lockAt = src.indexOf('requestSingleInstanceLock')
    expect(lockAt, 'the lock must be requested').toBeGreaterThan(-1)
    // Ordering is the whole point: a doomed copy that reaches `initStore()` has
    // already opened (and can write) the store the running copy owns, and one
    // that reaches `whenReady` has already built a window.
    expect(lockAt, 'the lock must be claimed before the store opens').toBeLessThan(src.indexOf('initStore()'))
    expect(lockAt, 'the lock must be claimed before whenReady').toBeLessThan(src.indexOf('app.whenReady()'))
  })

  it('the copy that loses the lock quits', () => {
    const lines = readRepoFile(INDEX).split('\n')
    const start = lines.findIndex((l) => l.includes('if (!app.requestSingleInstanceLock())'))
    expect(start, 'the lock check must be an early bail').toBeGreaterThan(-1)
    // The bail is the next few lines; matching a lazy `[\s\S]*?` would run on
    // into the else-branch and find `app.quit()` from somewhere else.
    const bail = lines.slice(start, start + 6).join('\n')
    expect(bail, 'a second copy must exit instead of starting').toContain('app.quit()')
  })

  it('a second launch reveals the running app instead of adding a capsule', () => {
    const src = readRepoFile(INDEX)
    const handler = src.match(/app\.on\('second-instance', \(\) => ([^\n]+)\)/)
    expect(handler, 'second-instance handler not found').not.toBeNull()
    // The shared "bring the app forward" path (tray menu, capsule click) — not a
    // second window and not a second capsule.
    expect(handler![1], 'the handler must reuse openMainWindow').toContain('openMainWindow()')
    expect(readRepoFile(FLOATING), 'openMainWindow must be exported for it').toMatch(/export function openMainWindow\(\): void/)
  })

  it('creating the main window twice cannot leak a second one', () => {
    // `second-instance` can arrive while the first copy is still booting, and
    // both paths call `createMainWindow`. Without the guard the first window is
    // dropped on the floor while still on screen.
    const src = readRepoFile('desktop/src/main/window.ts')
    const head = src.match(/export function createMainWindow\(\): BrowserWindow \{([\s\S]*?)\n  mainWindow = new BrowserWindow/)
    expect(head, 'createMainWindow not found').not.toBeNull()
    expect(head![1], 'an existing window must be returned instead of replaced').toContain(
      'if (mainWindow && !mainWindow.isDestroyed()) return mainWindow',
    )
  })
})

describe('a destroyed capsule cannot orphan a live one', () => {
  it('the destroyed listener only clears its own window', () => {
    // `webContents.destroyed` is asynchronous (measured on Electron 44: it fires
    // after `destroy()` returns). If the module variable already points at a
    // re-created capsule, nulling it blindly leaves that window alive but
    // untracked — no menu or click can reach it, and the next show puts a second
    // capsule on screen.
    const src = readRepoFile(FLOATING)
    expect(src, 'the window identity must be captured').toContain('const self = win')
    const handler = src.match(/win\.webContents\.on\('destroyed', \(\) => \{([\s\S]*?)\n  \}\)/)
    expect(handler, 'the destroyed listener not found').not.toBeNull()
    expect(handler![1], 'the listener must compare before clearing').toContain('if (win === self) win = null')
    expect(handler![1], 'the listener must not clear unconditionally').not.toMatch(/\n\s{4}win = null\n/)
  })
})
