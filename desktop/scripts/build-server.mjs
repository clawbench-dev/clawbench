#!/usr/bin/env node
/**
 * Builds the ClawBench server that ships INSIDE the desktop app.
 *
 * The "all-in-one" desktop build starts a server of its own from
 * `resources/server/` (see src/main/localServer.ts). Shipping a prebuilt binary
 * rather than downloading one on first run is deliberate: the app is usable
 * offline, the server version can never drift from the app's, and a failed
 * first-run download can never leave an app that opens to nothing.
 *
 * Only Windows is built so far — that is the platform the embedded mode is
 * specified for. Linux/macOS packages keep using the login page and a server
 * elsewhere, so this script exits successfully without producing anything there
 * instead of failing their builds.
 *
 * Output: desktop/assets/server/clawbench.exe (generated, not committed).
 */
import { execFileSync } from 'node:child_process'
import { copyFileSync, existsSync, mkdirSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const repoRoot = join(here, '..', '..')
const outDir = join(here, '..', 'assets', 'server')
const out = join(outDir, 'clawbench.exe')

/**
 * A binary the release workflow already built (artifact `windows-server`,
 * downloaded to `clawbench-server/` at the repository root).
 *
 * Preferred over building here for two reasons: that copy has the frontend
 * embedded (this repository has no `internal/frontend/dist` checked in) and the
 * release's version stamp, both produced exactly once by the standalone Windows
 * job. It is still a `go build`, so a build from source below is equivalent —
 * just slower and easy to stamp differently.
 */
const prebuilt = join(repoRoot, 'clawbench-server', 'clawbench.exe')

// Always create the directory, even on hosts that produce no server: the
// electron-builder config points `extraFiles` at it, and a missing source is
// noisier than an empty one.
mkdirSync(outDir, { recursive: true })

if (process.platform !== 'win32') {
  // CI builds the Windows package on windows-latest, where this branch never
  // runs; a Linux/macOS desktop package does not embed a server at all.
  console.log('server: not a Windows build host — skipping the embedded server')
  process.exit(0)
}

if (existsSync(prebuilt)) {
  copyFileSync(prebuilt, out)
  assertBinary(out)
  console.log(`server: reused the release's build (${mb(out)} MB)`)
  process.exit(0)
}

// Same tags and ldflags as release.yml's server job:
//  - `fts5` is required, not cosmetic: the build fails to compile search
//    without it on the drivers this project uses.
//  - Version is read from the same `git describe` the standalone packages use,
//    so the embedded server reports the release it belongs to. The desktop job
//    has not set the package version at prebuild time, so it comes from git
//    here rather than from package.json.
function describe() {
  try {
    return execFileSync('git', ['describe', '--tags', '--always'], { cwd: repoRoot, encoding: 'utf8' }).trim()
  } catch {
    // A source tarball with no .git: keep the build working, as release.yml
    // does for the standalone binaries.
    return 'dev'
  }
}

/**
 * electron-builder copies extraResources from the source directory, so the file
 * must be final here. A zero-byte or truncated artifact (a failed download saved
 * as a file) packages cleanly and only fails at the user's first launch.
 */
function assertBinary(file) {
  if (!existsSync(file) || statSync(file).size < 1_000_000) {
    throw new Error(`server: ${file} is missing or implausibly small`)
  }
}

function mb(file) {
  return (statSync(file).size / 1024 / 1024).toFixed(1)
}

const version = describe()
console.log(`server: building ${version}`)
execFileSync('go', [
  'build',
  '-tags', 'fts5',
  '-trimpath',
  '-ldflags', `-s -w -X clawbench/internal/version.Version=${version}`,
  '-o', out,
  './cmd/server',
], { cwd: repoRoot, stdio: 'inherit' })

assertBinary(out)
console.log(`server: built ${out} (${mb(out)} MB)`)
