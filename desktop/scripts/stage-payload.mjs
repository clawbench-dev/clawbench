#!/usr/bin/env node
/**
 * Stage a desktop PAYLOAD archive for an electron-builder `dir` output.
 *
 * The full desktop package is ~150MB because it bundles the Electron runtime.
 * Our own code is only the `resources/` directory (~3MB), so upgrades can ship
 * just that and reuse the runtime the user already has — provided the Electron
 * ABI matches AND the shell's identity is unchanged, both of which
 * `payload.json` records for the client to check.
 *
 * Layout produced inside `<unpackedDir>-payload/`:
 *
 *   payload.json          { "electron": "44.4.3", "shell": "sha256-…" }
 *   resources/…           the app: app.asar, app.asar.unpacked/, login.html, …
 *
 * The directory is a SIBLING of the unpacked build, not a child of it. A child
 * would be swept into the full-package archive by the same `zip -r .` /
 * `Compress-Archive -Path <dir>/*` step that packages the shell, so the full
 * package would silently grow a redundant ~4MB copy of the payload — and it
 * would only be caught by whichever step happened to run first.
 *
 * The archive must be built from INSIDE this directory so its entries are
 * `payload.json` and `resources/…`. Those share no top-level directory, which
 * is what stops install.ts's extractZip from stripping a prefix — a zip whose
 * entries were all under `resources/` would have that prefix removed and
 * `app.asar` would land at the app root instead of inside `resources/`.
 *
 * Usage: node scripts/stage-payload.mjs <unpackedDir>
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { computeShellFingerprint } from './shell-fingerprint.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const desktopDir = path.resolve(here, '..')

/** Recursively collect file paths relative to `root`, sorted for comparison. */
function listFiles(root, base = root, out = []) {
  for (const entry of fs.readdirSync(root, { withFileTypes: true }).sort((a, b) => (a.name < b.name ? -1 : 1))) {
    const abs = path.join(root, entry.name)
    if (entry.isDirectory()) {
      listFiles(abs, base, out)
    } else if (entry.isFile()) {
      out.push(path.relative(base, abs))
    } else {
      throw new Error(`unsupported entry in resources: ${abs}`)
    }
  }
  return out
}

function fail(msg) {
  console.error(`stage-payload: ${msg}`)
  process.exit(1)
}

const unpackedDir = process.argv[2]
if (!unpackedDir) fail('usage: node scripts/stage-payload.mjs <unpackedDir>')

const resources = path.join(unpackedDir, 'resources')
if (!fs.existsSync(resources)) fail(`no resources/ directory in ${unpackedDir}`)

// Read the Electron version from the installed devDependency rather than from
// the unpacked output: `<unpackedDir>/version` exists on Linux but NOT on
// Windows, so reading it would break the Windows job.
const electronPkg = path.join(desktopDir, 'node_modules/electron/package.json')
if (!fs.existsSync(electronPkg)) fail(`electron is not installed at ${electronPkg} (run npm ci first)`)
const electron = JSON.parse(fs.readFileSync(electronPkg, 'utf8')).version
if (!electron) fail('could not determine the Electron version')

// Sibling of the unpacked dir, so the full-package packaging step cannot sweep
// it up (see the header).
const payloadDir = path.resolve(unpackedDir) + '-payload'
fs.rmSync(payloadDir, { recursive: true, force: true })
fs.mkdirSync(payloadDir, { recursive: true })

// The shell's identity, recorded so the client can refuse a payload that was
// built against a different icon / appId / productName. Those live in the
// executable and packaging, not in resources/, so a payload cannot update them
// — reusing the wrong shell would silently leave them stale.
const shell = computeShellFingerprint()

// Copied with Node rather than `cp -a` / `Copy-Item *`: PowerShell's `*` glob
// SKIPS hidden items, and resources/ contains dotfiles (cpu-features ships
// .eslintrc.js, .clang-format, …). That would silently produce an incomplete
// payload on Windows only.
fs.cpSync(resources, path.join(payloadDir, 'resources'), { recursive: true })
fs.writeFileSync(
  path.join(payloadDir, 'payload.json'),
  JSON.stringify({ electron, shell }, null, 2) + '\n',
  'utf8',
)

// Files the client and the app require. Asserting these catches a build that
// did not produce them, which is the realistic failure: a missing native module
// or a dropped dotfile installs cleanly and only breaks at runtime.
const REQUIRED = [
  'resources/app.asar',
  'resources/login.html',
  'resources/logo.png',
  'resources/app.asar.unpacked/node_modules/ssh2/lib/protocol/crypto/build/Release/sshcrypto.node',
  'resources/app.asar.unpacked/node_modules/cpu-features/build/Release/cpufeatures.node',
  // A dotfile: the case a glob-based copy silently drops.
  'resources/app.asar.unpacked/node_modules/cpu-features/.eslintrc.js',
]
const staged = new Set(listFiles(payloadDir))
const missing = REQUIRED.filter((f) => !staged.has(f))
if (missing.length > 0) {
  fail(`payload is missing required files: ${missing.join(', ')}`)
}

// Guards the COPY itself: the comparison is only informative if the copy method
// ever stops being a faithful recursive copy (e.g. a glob-based one that skips
// dotfiles). cpSync copies everything, so today this cannot fail — it exists to
// make such a change loud rather than silent.
const want = listFiles(resources)
const got = listFiles(path.join(payloadDir, 'resources'))
if (want.length !== got.length || want.some((f, i) => f !== got[i])) {
  const absent = want.filter((f) => !got.includes(f))
  fail(`payload/resources does not match resources/ (${absent.length} missing, e.g. ${absent.slice(0, 5).join(', ')})`)
}

const bytes = listFiles(payloadDir).reduce(
  (n, f) => n + fs.statSync(path.join(payloadDir, f)).size,
  0,
)
console.log(
  `stage-payload: ${got.length} files, ${(bytes / 1048576).toFixed(2)} MiB, electron ${electron}, shell ${shell} -> ${payloadDir}`,
)
