import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import ts from 'typescript'
import { storagesToClear } from './reloadPolicy'

/**
 * Policy table + call-site wiring for the two reload scopes (issue #559).
 *
 * `session.ts` itself (which storages Electron actually receives) is covered
 * behaviourally by `session.test.ts` and `bridge.test.ts`. What is left here is
 * the table and a guard that the two call sites keep asking for *different*
 * scopes.
 *
 * The call-site guard parses the file with the TypeScript compiler instead of
 * matching text. A regex over the source was tried first and was not a guard at
 * all: a string literal or a commented-out old line containing the expected
 * text satisfied it, so "keep the old line as a comment, write the wrong scope
 * on the next line" passed. The AST sees only real call expressions.
 */

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(rel: string): string {
  return readFileSync(resolve(repoRoot, rel), 'utf8')
}

/**
 * Every argument text of every `clearCacheAndReload(...)` call in `rel`.
 *
 * Comments, string literals and dead objects are structurally invisible here,
 * which is the whole point. Non-literal arguments (an identifier, a ternary)
 * come back as their source text, so an indirection fails the equality checks
 * below loudly rather than silently passing.
 *
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

  it('session.ts takes the storage list from the policy, not an inline literal', () => {
    // An inline `storages: [...]` is exactly how the two scopes drifted apart.
    const src = readRepoFile('desktop/src/main/session.ts')
    expect(src).toMatch(/storages:\s*storagesToClear\(scope\)/)
    expect(src).not.toMatch(/storages:\s*\[/)
  })
})
