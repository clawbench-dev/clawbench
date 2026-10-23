import { describe, expect, it } from 'vitest'
import { execFileSync } from 'node:child_process'
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

/**
 * Behavioural tests for scripts/npm-publish-idempotent.sh.
 *
 * These run the real script against a fake `npm` on PATH, because the property
 * that matters is a control-flow one: after a failed publish the script must
 * keep asking the registry whether the version showed up, and only give up
 * after a bounded number of attempts. A source-text guard cannot see that —
 * an early version of this suite asserted the presence of a `while` loop and
 * passed happily when the wait budget was set to zero.
 *
 * The fake npm keeps its state in files so each scenario is a fresh, explicit
 * starting condition rather than a sequence of side effects.
 */

function repoPath(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return p
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const SCRIPT = repoPath('scripts/npm-publish-idempotent.sh')

interface Fake {
  /** Directory holding the fake `npm` and the scenario state. */
  dir: string
  /** Directory used as the package to publish. */
  pkgDir: string
  /** How many times `npm view` was consulted. */
  viewCalls(): number
  /** How many times `npm publish` was attempted. */
  publishCalls(): number
  /** Exit code of the script under test. */
  exitCode: number
  stdout: string
  stderr: string
}

interface Scenario {
  /** `npm view` succeeds from this call onward; 0 means "never". */
  visibleAfterViewCall?: number
  /** `npm view` already succeeds on the very first call (pre-check). */
  alreadyPublished?: boolean
  /** `npm publish` exits 0 instead of the E409 conflict. */
  publishSucceeds?: boolean
  waitSeconds?: number
  intervalSeconds?: number
}

/**
 * Build a scenario and run the script once.
 *
 * The fake npm implements exactly the three subcommands the script uses:
 * `view <spec> version` and `publish <dir> ...`. `npm publish` fails with the
 * E409 message npm actually emitted on v0.121.0 so the test exercises the
 * real error path, not a synthetic one.
 */
function runScenario(opts: Scenario = {}): Fake {
  const dir = mkdtempSync(join(tmpdir(), 'npm-idem-'))
  const binDir = join(dir, 'bin')
  const pkgDir = join(dir, 'pkg')
  mkdirSync(binDir, { recursive: true })
  mkdirSync(pkgDir, { recursive: true })

  writeFileSync(
    join(pkgDir, 'package.json'),
    JSON.stringify({ name: '@scope/fake-pkg', version: '1.2.3' }, null, 2)
  )

  const viewLog = join(dir, 'view.count')
  const publishLog = join(dir, 'publish.count')
  writeFileSync(viewLog, '0')
  writeFileSync(publishLog, '0')

  const fakeNpm = `#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  view)
    COUNT=$(( $(cat "${viewLog}") + 1 ))
    echo "$COUNT" > "${viewLog}"
    VISIBLE_AFTER=${opts.visibleAfterViewCall ?? 0}
    if [ "${opts.alreadyPublished ? 1 : 0}" = "1" ]; then
      echo "1.2.3"
      exit 0
    fi
    if [ "$VISIBLE_AFTER" -gt 0 ] && [ "$COUNT" -ge "$VISIBLE_AFTER" ]; then
      echo "1.2.3"
      exit 0
    fi
    exit 1
    ;;
  publish)
    COUNT=$(( $(cat "${publishLog}") + 1 ))
    echo "$COUNT" > "${publishLog}"
    if [ "${opts.publishSucceeds ? 1 : 0}" = "1" ]; then
      echo "+ @scope/fake-pkg@1.2.3"
      exit 0
    fi
    echo "npm error code E409" >&2
    echo "npm error 409 Conflict - Cannot publish over previously staged version \\"1.2.3\\"." >&2
    exit 1
    ;;
  *)
    echo "unexpected npm invocation: $*" >&2
    exit 1
    ;;
esac
`
  const fakePath = join(binDir, 'npm')
  writeFileSync(fakePath, fakeNpm)
  chmodSync(fakePath, 0o755)

  let exitCode = 0
  let stdout = ''
  let stderr = ''
  try {
    stdout = execFileSync(SCRIPT, [pkgDir, '--access', 'public'], {
      encoding: 'utf8',
      env: {
        ...process.env,
        PATH: `${binDir}:${process.env.PATH ?? ''}`,
        NPM_PUBLISH_RACE_WAIT_SECONDS: String(opts.waitSeconds ?? 0.3),
        NPM_PUBLISH_RACE_INTERVAL_SECONDS: String(opts.intervalSeconds ?? 0.05),
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
  } catch (err) {
    const e = err as { status?: number; stdout?: string; stderr?: string }
    exitCode = e.status ?? 1
    stdout = e.stdout ?? ''
    stderr = e.stderr ?? ''
  }

  return {
    dir,
    pkgDir,
    exitCode,
    stdout,
    stderr,
    viewCalls: () => Number(readFileSync(viewLog, 'utf8').trim()),
    publishCalls: () => Number(readFileSync(publishLog, 'utf8').trim()),
  }
}

describe('npm-publish-idempotent.sh — already published', () => {
  it('skips publishing entirely when the version is on the registry', () => {
    const r = runScenario({ alreadyPublished: true })
    expect(r.exitCode).toBe(0)
    // The pre-check is the whole point: republishing an immutable version is
    // what produced the red job. It must not even attempt the publish.
    expect(r.publishCalls()).toBe(0)
    expect(r.stdout).toContain('already published')
  })
})

describe('npm-publish-idempotent.sh — first publish succeeds', () => {
  it('exits 0 without polling when npm publish itself succeeds', () => {
    const r = runScenario({ publishSucceeds: true })
    expect(r.exitCode).toBe(0)
    expect(r.publishCalls()).toBe(1)
    // Only the pre-check consulted the registry; no retry loop on success.
    expect(r.viewCalls()).toBe(1)
  })
})

describe('npm-publish-idempotent.sh — concurrent publish race', () => {
  it('treats a version that appears later as success', () => {
    // Call 1 is the pre-check (not yet published), call 2 is the first poll —
    // the version shows up there, mimicking another run winning the race.
    const r = runScenario({ visibleAfterViewCall: 2, waitSeconds: 1, intervalSeconds: 0.05 })
    expect(r.exitCode).toBe(0)
    expect(r.publishCalls()).toBe(1)
    expect(r.viewCalls()).toBeGreaterThanOrEqual(2)
    expect(r.stdout).toContain('published by a concurrent run')
  })

  it('keeps polling for the whole wait budget, not just once', () => {
    // Appears on the 6th view call: 1 pre-check + 5 polls. An implementation
    // that checks once and gives up (the shape that shipped) stops at 2.
    const r = runScenario({ visibleAfterViewCall: 6, waitSeconds: 1, intervalSeconds: 0.05 })
    expect(r.exitCode).toBe(0)
    expect(r.viewCalls()).toBeGreaterThanOrEqual(6)
  })

  it('fails when the version never appears', () => {
    // A genuine failure (bad tarball, wrong scope) must not be swallowed by
    // the race handling — otherwise every broken publish looks green.
    const r = runScenario({ visibleAfterViewCall: 0, waitSeconds: 0.3, intervalSeconds: 0.05 })
    expect(r.exitCode).toBe(1)
    // `::error::` is a GitHub annotation, which the runner reads from stdout.
    expect(r.stdout).toContain('::error::')
    expect(r.stdout).toContain('@scope/fake-pkg@1.2.3')
    // npm's own output is preserved on stderr so the real reason stays visible.
    expect(r.stderr).toContain('E409')
    // It did try more than once before giving up.
    expect(r.viewCalls()).toBeGreaterThan(1)
  })

  it('does not publish twice while waiting', () => {
    // The retry is a re-read of the registry, never a second publish: a second
    // attempt would just produce another E409 and could publish a different
    // tarball if the tree changed.
    const r = runScenario({ visibleAfterViewCall: 4, waitSeconds: 1, intervalSeconds: 0.05 })
    expect(r.publishCalls()).toBe(1)
  })
})

describe('npm-publish-idempotent.sh — input handling', () => {
  it('rejects a directory with no package.json', () => {
    const dir = mkdtempSync(join(tmpdir(), 'npm-idem-empty-'))
    let code = 0
    let stderr = ''
    try {
      execFileSync(SCRIPT, [dir], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] })
    } catch (err) {
      const e = err as { status?: number; stderr?: string }
      code = e.status ?? 1
      stderr = e.stderr ?? ''
    }
    expect(code).toBe(1)
    expect(stderr).toContain('package.json not found')
  })

  it('derives name and version from the manifest', () => {
    // The skip check and the publish must agree on the package; deriving both
    // from the manifest is what guarantees that.
    const src = readFileSync(SCRIPT, 'utf8')
    expect(src).toMatch(/NAME=\$\(node -p/)
    expect(src).toMatch(/VERSION=\$\(node -p/)
  })
})
