#!/usr/bin/env node
/**
 * Stages the ClawBench server that ships INSIDE the desktop app.
 *
 * The integrated desktop build (整合包) starts a server of its own from the app's
 * content directory (see src/main/localServer.ts). Shipping a prebuilt binary
 * rather than downloading one on first run is deliberate: the app is usable
 * offline, the server version can never drift from the app's, and a failed
 * first-run download can never leave an app that opens to nothing.
 *
 * Every desktop platform is supported now. The target is chosen by
 * `--platform`/`--arch` (or CLAWBENCH_SERVER_PLATFORM / CLAWBENCH_SERVER_ARCH),
 * defaulting to the host — the macOS job builds two architectures in one job
 * and must stage the matching server before each electron-builder run.
 *
 * Source, in order:
 *  1. `clawbench-server/<platform>-<arch>/clawbench[.exe]` — the release
 *     workflow's own build, downloaded as an artifact. Preferred because it
 *     carries the embedded frontend and the release's version stamp, both
 *     produced exactly once by the standalone server job.
 *  2. A local `go build` when the host matches the target.
 *
 * Output: desktop/assets/server/clawbench[.exe] (generated, not committed).
 */
import { execFileSync } from 'node:child_process'
import { copyFileSync, existsSync, mkdirSync, rmSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const repoRoot = join(here, '..', '..')
const outDir = join(here, '..', 'assets', 'server')

function argValue(flag) {
  const i = process.argv.indexOf(flag)
  return i >= 0 ? process.argv[i + 1] : undefined
}

const platform = argValue('--platform') || process.env.CLAWBENCH_SERVER_PLATFORM || process.platform
const arch = argValue('--arch') || process.env.CLAWBENCH_SERVER_ARCH || process.arch
const exe = platform === 'win32' ? 'clawbench.exe' : 'clawbench'
const out = join(outDir, exe)
const prebuilt = join(repoRoot, 'clawbench-server', `${platform}-${arch}`, exe)

// Always recreate the directory: a stale binary for another platform (a local
// macOS build after a Windows one, or vice versa) would be packaged silently.
rmSync(outDir, { recursive: true, force: true })
mkdirSync(outDir, { recursive: true })

if (platform === 'darwin') {
  // macOS does not put the server inside the app bundle (that would invalidate
  // its code signature). The integrated zip carries `server/` beside
  // `ClawBench.app`, assembled by release.yml from the server artifact. There
  // is nothing for electron-builder to stage.
  console.log('server: macOS keeps the server beside the bundle — nothing to stage')
  process.exit(0)
}

if (existsSync(prebuilt)) {
  copyFileSync(prebuilt, out)
  assertBinary(out)
  console.log(`server: reused the release's ${platform}/${arch} build (${mb(out)} MB)`)
  process.exit(0)
}

if (platform !== process.platform || arch !== process.arch) {
  throw new Error(
    `server: no prebuilt binary for ${platform}/${arch} at ${prebuilt}, and it cannot be cross-built here`,
  )
}

// Same tags and ldflags as release.yml's server jobs:
//  - `fts5` is required, not cosmetic: the build fails to compile search
//    without it on the drivers this project uses.
//  - Version is read from the same `git describe` the standalone packages use,
//    so the embedded server reports the release it belongs to.
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
 * electron-builder copies the staged directory, so the file must be final
 * here. A zero-byte or truncated artifact (a failed download saved as a file)
 * packages cleanly and only fails at the user's first launch.
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
console.log(`server: building ${version} for ${platform}/${arch}`)
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
