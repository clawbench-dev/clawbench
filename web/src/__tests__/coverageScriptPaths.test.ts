import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'

/**
 * Regression guard for the path helpers inside check-frontend-coverage.sh.
 *
 * The gate's Python lives in a heredoc, so there is nothing importable. These
 * tests therefore extract the REAL function source out of the shell script and
 * run it with python3 — deliberately not a hand-copied reimplementation, which
 * would pass even if the shipped script were broken (the classic tautology:
 * delete the production code and the test still passes).
 *
 * The bug being guarded: `extract_src_path` used to match the first `/src/`
 * anywhere in a path. That also matches `desktop/src/…`, `android/app/src/…`
 * and `web/vendor-build/excalidraw/src/…`, so a desktop file reduced to
 * `src/main/install.ts` and its first two segments became the Tier 1 bucket
 * `src/main` — a directory the frontend does not have. Desktop files were
 * averaged into that phantom bucket, failing Tier 1 locally while CI passed
 * (CI never installs desktop/node_modules, so it collects only the
 * dependency-free desktop tests).
 */

function readScript(): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    try {
      return readFileSync(resolve(base, 'scripts/check-frontend-coverage.sh'), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`check-frontend-coverage.sh not found from cwd: ${process.cwd()}`)
}

/** Pull `def <name>(...)` plus its body out of the heredoc, by indentation. */
function extractPythonFn(script: string, name: string): string {
  const lines = script.split('\n')
  const start = lines.findIndex((l) => l.startsWith(`def ${name}(`))
  if (start < 0) throw new Error(`def ${name} not found in the coverage script`)
  const body = [lines[start]]
  for (let i = start + 1; i < lines.length; i++) {
    const line = lines[i]
    // A top-level statement ends the function body.
    if (line.trim() !== '' && !/^\s/.test(line)) break
    body.push(line)
  }
  return body.join('\n')
}

/** Run the extracted helpers over the given paths, inside the real python3. */
function classify(paths: string[]): Record<string, [string | null, string | null]> {
  const script = readScript()
  const program = [
    extractPythonFn(script, 'extract_src_path'),
    extractPythonFn(script, 'extract_web_src_path'),
    'import json, sys',
    'paths = json.loads(sys.argv[1])',
    'print(json.dumps({p: [extract_src_path(p), extract_web_src_path(p)] for p in paths}))',
  ].join('\n\n')
  const out = execFileSync('python3', ['-c', program, JSON.stringify(paths)], { encoding: 'utf8' })
  return JSON.parse(out)
}

const REPO = '/home/runner/work/clawbench/clawbench'

describe('check-frontend-coverage.sh path helpers', () => {
  it('resolves frontend sources to src/...', () => {
    const r = classify([
      `${REPO}/web/src/components/file/FileManagerContent.vue`,
      `${REPO}/web/src/utils/path.ts`,
      'web/src/composables/useLocale.ts',
    ])
    expect(r[`${REPO}/web/src/components/file/FileManagerContent.vue`][0]).toBe('src/components/file/FileManagerContent.vue')
    expect(r[`${REPO}/web/src/utils/path.ts`][0]).toBe('src/utils/path.ts')
    expect(r['web/src/composables/useLocale.ts'][0]).toBe('src/composables/useLocale.ts')
  })

  it('skips non-frontend trees that also contain /src/', () => {
    // This is the regression. Before the fix each of these yielded a bogus
    // `src/...` path and landed in a phantom Tier 1 bucket.
    const nonFrontend = [
      `${REPO}/desktop/src/main/install.ts`,
      `${REPO}/desktop/src/main/clientLog.ts`,
      `${REPO}/desktop/src/shared/types.ts`,
      `${REPO}/android/app/src/main/java/com/clawbench/app/AppLog.java`,
      `${REPO}/web/vendor-build/excalidraw/src/index.tsx`,
    ]
    const r = classify(nonFrontend)
    for (const p of nonFrontend) {
      expect(r[p][0], `${p} must not be treated as frontend coverage`).toBeNull()
      expect(r[p][1], `${p} must not be treated as frontend coverage`).toBeNull()
    }
  })

  it('skips repo paths with no src/ at all', () => {
    const others = [`${REPO}/npm/main/bin/platform.js`, `${REPO}/web/stylelint.config.js`, `${REPO}/web/css/share-chrome.css`]
    const r = classify(others)
    for (const p of others) expect(r[p][0]).toBeNull()
  })

  it('keeps extract_web_src_path aligned with extract_src_path', () => {
    // Tier 2 normalizes with the web/ prefixed helper; Tier 1 with the bare
    // one. Both must agree on what counts as a frontend path, or a file could
    // be gated by one tier and invisible to the other.
    const paths = [
      `${REPO}/web/src/utils/path.ts`,
      `${REPO}/desktop/src/main/install.ts`,
      `${REPO}/android/app/src/main/java/X.java`,
    ]
    const r = classify(paths)
    for (const p of paths) {
      const [bare, webPrefixed] = r[p]
      // `bare === null` ⟺ `webPrefixed === null`
      expect(bare === null).toBe(webPrefixed === null)
      if (bare !== null) expect(webPrefixed).toBe(`web/${bare}`)
    }
  })

  it('does not fabricate a bucket for desktop files', () => {
    // End-to-end statement of the bug: the Tier 1 bucket is `parts[:2]` of the
    // extracted path. No frontend path may ever produce the bucket `src/main`.
    const buckets = Object.values(classify([`${REPO}/desktop/src/main/install.ts`, `${REPO}/desktop/src/main/tunnel.ts`]))
      .map(([bare]) => (bare ? bare.split('/').slice(0, 2).join('/') : null))
      .filter(Boolean)
    expect(buckets).not.toContain('src/main')
    expect(buckets).toEqual([])
  })
})
