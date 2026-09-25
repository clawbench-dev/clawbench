import { describe, expect, it } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { resolve } from 'node:path'
import { createContext, runInContext } from 'node:vm'

const ANDROID_UTILS = 'android/app/src/main/assets/url-utils.js'
const DESKTOP_UTILS = 'desktop/assets/url-utils.js'

/**
 * Locate a repo file from the vitest cwd. Mirrors the idiom in
 * coverageScriptPaths.test.ts: try the cwd, then its parent. No import.meta.url
 * — vitest runs with the repo root as cwd, so relative paths are repo-relative.
 */
function readRepoFile(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    try {
      return readFileSync(resolve(base, rel), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

/**
 * Resolve a repo-relative path to an absolute one, using the same cwd-then-parent
 * candidate search as readRepoFile so both helpers agree on the repo root.
 */
function resolveRepo(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const candidate = resolve(base, rel)
    if (existsSync(candidate)) return candidate
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

interface Parsed {
  protocol: 'http' | 'https' | null
  host: string
  port: string | null
}

/**
 * Evaluate a url-utils.js in a bare vm sandbox and return its global
 * parseServerInput. The bare sandbox is deliberate: load-time references to
 * document/window/ClawBenchNative throw, and any reference on an executed path
 * throws too. (A DOM reference inside an unreached branch is not detected —
 * the function is pure by design, so keep it that way.)
 */
function parserFor(rel: string): (text: string) => Parsed | null {
  const sandbox: Record<string, unknown> = {}
  createContext(sandbox)
  runInContext(readRepoFile(rel), sandbox, { filename: rel })
  const fn = sandbox.parseServerInput
  if (typeof fn !== 'function') throw new Error(`${rel} did not define parseServerInput`)
  return fn as (text: string) => Parsed | null
}

/**
 * One table, asserted against both copies. This is what keeps the two
 * url-utils.js files from drifting: a change to either alone fails here.
 */
const CASES: Array<[string, Parsed | null]> = [
  // Behavior table from the design doc.
  ['https://192.168.1.100:8443', { protocol: 'https', host: '192.168.1.100', port: '8443' }],
  ['http://example.com', { protocol: 'http', host: 'example.com', port: null }],
  ['https://example.com/chat?x=1', { protocol: 'https', host: 'example.com', port: null }],
  ['https://host:8443/api', { protocol: 'https', host: 'host', port: '8443' }],
  ['192.168.1.100:8080', { protocol: null, host: '192.168.1.100', port: '8080' }],
  ['192.168.1.100', { protocol: null, host: '192.168.1.100', port: null }],
  ['not a url', null],
  // Extra edges.
  ['', null],
  ['   ', null],
  // Whitespace around a valid URL — clipboard pastes often carry padding.
  ['  https://host:8443  ', { protocol: 'https', host: 'host', port: '8443' }],
  // An explicit port equal to the scheme default is still explicit.
  ['http://host:80', { protocol: 'http', host: 'host', port: '80' }],
  ['HTTPS://Host:8443', { protocol: 'https', host: 'Host', port: '8443' }],
  ['https://host:8443/', { protocol: 'https', host: 'host', port: '8443' }],
  // Garbage / unsupported forms.
  ['http://', null],
  ['ftp://host', null],
  ['https://user:pass@host:8443', null],
  ['[::1]:8080', null],
]

describe.each([
  ['android', ANDROID_UTILS],
  ['desktop', DESKTOP_UTILS],
])('parseServerInput (%s copy)', (_label, rel) => {
  for (const [input, expected] of CASES) {
    it(`${JSON.stringify(input)} -> ${JSON.stringify(expected)}`, () => {
      expect(parserFor(rel)(input)).toEqual(expected)
    })
  }
})

const ANDROID_LOGIN = 'android/app/src/main/assets/login.html'
const DESKTOP_LOGIN = 'desktop/assets/login.html'

/**
 * Static wiring guard. Robolectric cannot execute JS (ShadowWebView
 * .evaluateJavascript is a no-op), so the paste handler cannot be exercised
 * end-to-end in a unit test. These assertions catch the wiring being forgotten
 * — a missing <script> tag or an unwired listener — which would otherwise ship
 * silently (no CSP, no error, the feature just does nothing).
 */
describe('login.html wiring', () => {
  for (const [label, rel] of [
    ['android', ANDROID_LOGIN],
    ['desktop', DESKTOP_LOGIN],
  ] as const) {
    it(`${label} loads url-utils.js and wires a paste listener on #addHost`, () => {
      const html = readRepoFile(rel)
      expect(html).toContain('<script src="url-utils.js"></script>')
      expect(html).toMatch(/getElementById\('addHost'\)\.addEventListener\('paste'/)
      expect(html).toContain('parseServerInput')
    })
  }
})

/**
 * Byte-equality guard for the duplicated url-utils.js. The plan accepts two
 * copies (no build-time single-source merge), so the only thing keeping them
 * honest is that they are identical. The behavioural table above catches drift
 * that changes output on one of its inputs; this catches the rest (a widened
 * regex on an unexercised shape, a renamed internal, a stray whitespace edit).
 */
describe('url-utils.js copies', () => {
  it('are byte-identical', () => {
    const android = readFileSync(resolveRepo(ANDROID_UTILS))
    const desktop = readFileSync(resolveRepo(DESKTOP_UTILS))
    expect(android.equals(desktop)).toBe(true)
  })
})
