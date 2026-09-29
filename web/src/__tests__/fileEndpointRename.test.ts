import { describe, expect, it } from 'vitest'
import { readdirSync, statSync } from 'node:fs'
import { resolve, join } from 'node:path'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the three file-read endpoints keep their renamed shape.
 *
 * A customer's internal IDS flagged normal ClawBench traffic as a "Crawlab
 * arbitrary file read" attack because ClawBench's file-read endpoints were
 * shape-identical to the vulnerable Crawlab endpoint:
 *
 *     GET /api/file?path=../../etc/passwd      (Nuclei: crawlab-lfi.yaml)
 *
 * The endpoints were renamed to remove that fingerprint:
 *
 *     /api/file?path=<abs>        -> /api/fs/file?target=<abs>
 *     /api/file/<rel>             -> /api/fs/file/<rel>
 *     /api/file/thumb?path=…&w=…  -> /api/fs/thumb?target=…&w=…
 *     /api/local-file/?path=<abs> -> /api/fs/raw/?target=<abs>
 *     /api/local-file/<rel>       -> /api/fs/raw/<rel>
 *
 * Why a source scan rather than only unit tests: the URL strings are built in
 * ~40 scattered call sites (the thumb URL alone has four independent builders),
 * and several consumers PARSE these URLs back out. A single stale producer or
 * parser reintroduces the fingerprint (or silently breaks live-refresh / drag /
 * export) without failing any existing behavioural test. This scans every
 * non-test source file so a missed site is caught at build time.
 *
 * Deliberately NOT flagged (out of scope — these are different endpoints that
 * never matched the Crawlab fingerprint, and several legitimately take `path=`):
 *   /api/file/list-tree, /symbols, /batch-exists, /batch-base64, /write,
 *   /rename, /delete, /create, /copy, /move, /archive, /theme-wallpaper,
 *   /content-search, /watch/ws, and the token-scoped /api/share/{token}/… .
 */

/** Every non-test source file under a web-relative directory. */
function listSourceFiles(relDir: string): string[] {
  const root = process.cwd().endsWith('web')
    ? resolve(process.cwd(), relDir)
    : resolve(process.cwd(), 'web', relDir)
  const out: string[] = []
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      if (entry === 'node_modules' || entry === '__tests__') continue
      const full = join(dir, entry)
      if (statSync(full).isDirectory()) walk(full)
      else if (/\.(ts|vue)$/.test(entry)) out.push(full)
    }
  }
  walk(root)
  return out
}

/** Path relative to web/, for readWebFile (cwd-agnostic). */
function webRel(full: string): string {
  return full.replace(/^.*?web\//, '')
}

const SOURCES = listSourceFiles('src')

describe('file-read endpoint rename', () => {
  it('scans a non-trivial number of source files', () => {
    // Guards against the walk silently returning nothing (e.g. a cwd change),
    // which would make every assertion below vacuously pass.
    expect(SOURCES.length).toBeGreaterThan(100)
  })

  it('no source file builds or parses the old /api/local-file/ endpoint', () => {
    // The backend serves /api/local-file/ again as a DEPRECATED alias, but only
    // for pre-rename native clients that cannot update themselves. The frontend
    // must never (re)introduce it: the web build always ships with the server,
    // so it can use the current endpoint and must keep the rename intact.
    const offenders: string[] = []
    for (const full of SOURCES) {
      const src = readWebFile(webRel(full))
      if (/api\/local-file/.test(src)) offenders.push(webRel(full))
    }
    expect(offenders, `old /api/local-file/ endpoint still referenced in:\n${offenders.join('\n')}`).toEqual([])
  })

  it('no source file builds or parses the old /api/file/thumb endpoint', () => {
    const offenders: string[] = []
    for (const full of SOURCES) {
      const src = readWebFile(webRel(full))
      if (/api\/file\/thumb/.test(src)) offenders.push(webRel(full))
    }
    expect(offenders, `old /api/file/thumb endpoint still referenced in:\n${offenders.join('\n')}`).toEqual([])
  })

  it('no source file builds the old /api/file?path= or /api/file/?path= shape', () => {
    const offenders: string[] = []
    for (const full of SOURCES) {
      const src = readWebFile(webRel(full))
      // The bare `/api/file` endpoint (content read) with a path query param.
      // Escaped-slash regex forms are included.
      if (/api\/file\/?\?path=|api\\\/file\\\/\?path=/.test(src)) offenders.push(webRel(full))
    }
    expect(offenders, `old /api/file?path= shape still referenced in:\n${offenders.join('\n')}`).toEqual([])
  })

  it('the renamed endpoints are present (rename actually happened)', () => {
    // The inverse guard: if someone reverts the rename, the "no old shape"
    // assertions above pass trivially only if the new shape exists somewhere.
    const all = SOURCES.map(f => readWebFile(webRel(f))).join('\n')
    expect(all).toContain('/api/fs/file')
    expect(all).toContain('/api/fs/raw/')
    expect(all).toContain('/api/fs/thumb')
  })

  it('sibling /api/file/* endpoints are left alone', () => {
    // A naive global replace of `/api/file/` would have taken these out; they
    // are different endpoints and must survive the rename. (Only those actually
    // referenced from web/src are listed — `/api/file/create` is backend/test
    // only and has no frontend producer to protect.)
    const all = SOURCES.map(f => readWebFile(webRel(f))).join('\n')
    for (const keep of [
      '/api/file/list-tree',
      '/api/file/symbols',
      '/api/file/batch-exists',
      '/api/file/batch-base64',
      '/api/file/write',
      '/api/file/rename',
      '/api/file/delete',
      '/api/file/copy',
      '/api/file/move',
      '/api/file/archive',
      '/api/file/content-search',
      '/api/file/theme-wallpaper',
    ]) {
      expect(all, `${keep} was removed by the rename but is out of scope`).toContain(keep)
    }
  })
})
