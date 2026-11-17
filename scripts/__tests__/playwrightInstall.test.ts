import { describe, expect, it } from 'vitest'
import { spawnSync } from 'node:child_process'
import { existsSync, mkdtempSync, writeFileSync, chmodSync, rmSync, readFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

/**
 * Behaviour test for scripts/playwright-install.sh.
 *
 * The script decides whether a failed browser install fails the job. Two
 * outcomes matter and they are easy to get backwards:
 *
 *   - "Playwright does not support <browser> on <distro>" means the RUNNER is
 *     the problem (ubuntu-latest moved to 26.04; the pinned Playwright refuses
 *     to install). Nine E2E shards used to go red for a reason no patch could
 *     fix, so this must be a loud skip.
 *   - Every other failure (a broken package, a missing binary, apt down for
 *     long enough) is OUR problem and must fail the job — a silent skip here
 *     would disable the browser suite without anyone noticing.
 *
 * The script is run for real against a fake `npx` on PATH, so the assertions
 * are about exit codes and output rather than about the script's text.
 */

/**
 * Resolved from the process CWD, not from this file's URL: vitest replaces
 * `__dirname` with `/__mock__` when it transforms test files, so a __dirname
 * relative path silently points at a file that does not exist.
 */
function repoFile(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const candidate = resolve(base, rel)
    if (existsSync(candidate)) return candidate
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const SCRIPT = repoFile('scripts/playwright-install.sh')

interface RunResult {
  status: number | null
  stdout: string
}

/**
 * Run the installer with a fake `npx` that prints `stderrText` and exits with
 * `exitCode`. `sudo` and `tee` are stubbed too: the script pipes through tee and
 * writes an apt config, neither of which the test should touch.
 */
function runWith(fakeNpx: string): RunResult {
  const dir = mkdtempSync(join(tmpdir(), 'pwinstall-'))
  try {
    const npx = join(dir, 'npx')
    writeFileSync(npx, `#!/bin/sh\n${fakeNpx}\n`)
    chmodSync(npx, 0o755)
    // sudo: drop the command, just run it.
    writeFileSync(join(dir, 'sudo'), '#!/bin/sh\nshift\n"$@"\n')
    chmodSync(join(dir, 'sudo'), 0o755)

    const res = spawnSync('bash', [SCRIPT, 'chromium'], {
      env: {
        ...process.env,
        PATH: `${dir}:${process.env.PATH ?? ''}`,
        // Keep the retry loop short: the script sleeps between attempts, and a
        // test must not take 40 seconds to prove a policy.
        PWINSTALL_TEST_NO_SLEEP: '1',
      },
      encoding: 'utf8',
      timeout: 30_000,
    })
    return { status: res.status, stdout: `${res.stdout ?? ''}${res.stderr ?? ''}` }
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
}

describe('scripts/playwright-install.sh', () => {
  it('succeeds on the first attempt when the install works', () => {
    const res = runWith('echo "installed chromium"; exit 0')
    expect(res.status).toBe(0)
  })

  it('skips (exit 0) with a warning when the RUNNER image is unsupported', () => {
    const res = runWith('echo "Error: ERROR: Playwright does not support chromium on ubuntu26.04-x64" >&2; exit 1')
    // A red E2E job here would be reporting the runner's distro, not the patch.
    expect(res.status).toBe(0)
    expect(res.stdout).toContain('::warning::')
    expect(res.stdout).toContain('unsupported distro')
  })

  it('fails (exit 1) on a real install error after retrying', () => {
    const res = runWith('echo "Error: download failed: ECONNRESET" >&2; exit 1')
    // Anything that is not the unsupported-image sentence must fail the job.
    expect(res.status).toBe(1)
    expect(res.stdout).toContain('::error::')
  })

  it('documents the skip policy where the workflow points at it', () => {
    // The CI step delegates to this script; if the file moves, the workflow's
    // comment becomes a lie and this test is the cheapest place to notice.
    expect(readFileSync(SCRIPT, 'utf8')).toContain('unsupported distro')
  })
})
