import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
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

interface Parsed {
  protocol: 'http' | 'https' | null
  host: string
  port: string | null
}

/**
 * Evaluate a url-utils.js in a bare vm sandbox and return its global
 * parseServerInput. A bare sandbox (no DOM, no Node globals) is deliberate:
 * the file must not touch document/window/ClawBenchNative, and this fails
 * loudly if it does.
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
  ['192.168.1.100:8080', { protocol: null, host: '192.168.1.100', port: '8080' }],
  ['192.168.1.100', { protocol: null, host: '192.168.1.100', port: null }],
  ['not a url', null],
  // Extra edges.
  ['', null],
  ['   ', null],
  // An explicit port equal to the scheme default is still explicit.
  ['http://host:80', { protocol: 'http', host: 'host', port: '80' }],
  ['HTTPS://Host:8443', { protocol: 'https', host: 'Host', port: '8443' }],
  ['https://host:8443/', { protocol: 'https', host: 'host', port: '8443' }],
  // Garbage / unsupported forms.
  ['http://', null],
  ['ftp://host', null],
  ['https://user:pass@host:8443', null],
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
