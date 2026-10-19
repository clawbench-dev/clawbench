import { describe, it, expect, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import ts from 'typescript'
import { performReload, storagesToClear } from './reloadPolicy'
import type { ReloadDeps } from './reloadPolicy'

/**
 * Behaviour of the reload primitive (issue #559).
 *
 * `storagesToClear()` alone would not have caught the bug: the defect was in
 * *what the shared function passed it*. So the interesting assertions here run
 * `performReload` against a recording fake and check the calls Electron would
 * receive.
 *
 * `session.ts` itself is not imported: the desktop package's dependencies are
 * not installed in CI (the root vitest job installs only the root
 * `node_modules`), so importing `electron` there fails to resolve and the
 * whole suite would be reported as a load failure rather than a test failure.
 * That is why the orchestration lives in this dependency-free module; the
 * remaining risk — a call site wiring the wrong scope — is covered below by
 * parsing the two files.
 */

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(rel: string): string {
  return readFileSync(resolve(repoRoot, rel), 'utf8')
}

/** A `ReloadDeps` that records the order and arguments of every call. */
function recordingDeps(): { deps: ReloadDeps; order: string[]; storages: () => ReloadStorageList } {
  const order: string[] = []
  let cleared: ReloadStorageList = null
  return {
    order,
    storages: () => cleared,
    deps: {
      clearCache: async () => { order.push('clearCache') },
      clearStorageData: async (options) => {
        order.push('clearStorageData')
        cleared = options.storages
      },
      reload: () => { order.push('reload') },
    },
  }
}

type ReloadStorageList = readonly string[] | null

describe('storagesToClear', () => {
  it('keeps localStorage, indexdb and cookies for a post-upgrade reload', () => {
    const storages = storagesToClear('cache')
    expect(storages).not.toContain('localstorage')
    expect(storages).not.toContain('indexdb')
    expect(storages).not.toContain('cookies')
  })

  it('still drops cached app-shell copies so stale chunks cannot survive', () => {
    // The exact set matters: a future "just add one more storage" edit must be
    // a deliberate change here, not a silent widening.
    expect(new Set(storagesToClear('cache'))).toEqual(new Set(['cachestorage', 'serviceworkers']))
  })

  it('clears everything for an explicit hard reload', () => {
    expect(new Set(storagesToClear('everything'))).toEqual(
      new Set(['localstorage', 'indexdb', 'cookies', 'cachestorage', 'serviceworkers']),
    )
  })
})

describe('performReload', () => {
  it('keeps this device\'s preferences when the server upgrade reloads', async () => {
    // The whole point of #559: theme / wallpaper / fonts / panel layout live
    // only in localStorage, and a server upgrade invalidates none of them.
    const rec = recordingDeps()
    await performReload(rec.deps, 'cache')
    expect(rec.storages()).not.toBeNull()
    expect(rec.storages()).not.toContain('localstorage')
    expect(rec.storages()).not.toContain('indexdb')
    expect(rec.storages()).not.toContain('cookies')
  })

  it('clears everything for the explicit hard reload', async () => {
    // The documented Ctrl+Shift+R contract: signs out, resets preferences.
    const rec = recordingDeps()
    await performReload(rec.deps, 'everything')
    expect(new Set(rec.storages())).toEqual(
      new Set(['localstorage', 'indexdb', 'cookies', 'cachestorage', 'serviceworkers']),
    )
  })

  it('drops the HTTP cache too, then reloads', async () => {
    const rec = recordingDeps()
    await performReload(rec.deps, 'cache')
    // Order is load-bearing: reloading first would let the outgoing page write
    // its state back into the storage this call is trying to clear.
    expect(rec.order).toEqual(['clearCache', 'clearStorageData', 'reload'])
  })

  it('does not reload when clearing fails', async () => {
    // A reload after a failed clear would land on a page whose storage may be
    // half-wiped, with no indication that anything went wrong.
    const reload = vi.fn()
    await expect(performReload(
      {
        clearCache: async () => undefined,
        clearStorageData: async () => { throw new Error('storage busy') },
        reload,
      },
      'cache',
    )).rejects.toThrow('storage busy')
    expect(reload).not.toHaveBeenCalled()
  })
})

/**
 * Every argument text of every `clearCacheAndReload(...)` call in `rel`.
 *
 * Parsed rather than regex-matched. A text guard is not a guard here: a
 * commented-out old line, a string literal, or a dead object carrying the
 * expected text all satisfy it, so "keep the old line as a comment and write
 * the wrong scope on the next line" passes (verified by mutation).
 * Property accesses are included because `window.ts` calls the lazily imported
 * module as `m.clearCacheAndReload(...)` (a top-level import would be a cycle).
 */
function reloadCallArgs(rel: string): string[] {
  const source = ts.createSourceFile(rel, readRepoFile(rel), ts.ScriptTarget.ES2022, true)
  const args: string[] = []
  const visit = (node: ts.Node): void => {
    if (ts.isCallExpression(node)) {
      const callee = node.expression
      const name = ts.isIdentifier(callee)
        ? callee.text
        : ts.isPropertyAccessExpression(callee) ? callee.name.text : ''
      if (name === 'clearCacheAndReload') {
        args.push(node.arguments.map((a) => a.getText(source)).join(', '))
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  return args
}

describe('reload call sites', () => {
  it('the upgrade bridge asks for the cache-only scope, exactly once', () => {
    // `useUpgrade` calls `reloadApp` automatically after every server upgrade,
    // so this is the call that must never clear local storage (issue #559).
    expect(reloadCallArgs('desktop/src/main/bridge.ts')).toEqual(["'cache'"])
  })

  it('the Ctrl+Shift+R shortcut asks for the full scope, exactly once', () => {
    // Documented in docs/user-guide/shortcuts.md as clearing cookies + storage.
    expect(reloadCallArgs('desktop/src/main/window.ts')).toEqual(["'everything'"])
  })

  it('session.ts delegates to the testable orchestrator', () => {
    // Without this the two guards above would still pass while session.ts went
    // back to clearing a hard-coded list.
    const src = readRepoFile('desktop/src/main/session.ts')
    expect(src).toMatch(/performReload\(/)
    expect(src).not.toMatch(/storages:\s*\[/)
    expect(src).not.toMatch(/clearStorageData\(\{\s*storages:\s*storagesToClear/)
  })
})
