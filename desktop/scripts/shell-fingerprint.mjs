#!/usr/bin/env node
/**
 * Compute and record the SHELL FINGERPRINT for a desktop build.
 *
 * Why this exists
 * ---------------
 * A payload archive reuses the Electron runtime from the installed shell and
 * replaces only `resources/`. That is safe only if the shell is the one the
 * payload was built against. Two things can invalidate it:
 *
 *   1. a different Electron version  — gated separately, by ABI major;
 *   2. a different shell IDENTITY    — appId, productName, executableName, the
 *      app icon. None of these live in `resources/`; they are baked into the
 *      executable and the packaging, so a payload cannot update them.
 *
 * (2) is the subtle one: nothing about it is visible at runtime. The running app
 * cannot detect that its icon is stale, because the icon is inside the binary
 * it is already running. So the build has to record its own identity, and the
 * client compares that record against the payload's.
 *
 * What is hashed, and what is NOT
 * -------------------------------
 * Only stable, shell-defining INPUTS: `electron-builder.yml` plus the icon
 * sources. Deliberately NOT the built binary:
 *
 *   electron-builder writes the app VERSION into the Windows executable
 *   (FileVersion / ProductVersion), and CI rewrites `package.json` from the
 *   release tag on every release. Hashing the binary would therefore change on
 *   every single release, no payload would ever match, and every upgrade would
 *   silently fall back to the full ~150MB download — defeating the feature.
 *   `electron-builder.yml` carries no version (verified), so it is stable.
 *
 * The hash is platform-INDEPENDENT on purpose: all platforms hash the same
 * inputs, so there is one value to reason about and no per-platform drift. The
 * cost is that a macOS-only icon change also invalidates the Linux/Windows
 * payloads, which costs those users one full download. That is rare and
 * conservative (never reuse a shell whose identity changed).
 *
 * `js-yaml` is only a transitive dependency here (see
 * scripts/__tests__/releaseAssets.test.ts, which reads these same workflow
 * files as text for that reason), so the raw file bytes are hashed rather than
 * parsed. A comment edit therefore changes the fingerprint — conservative, and
 * an edit to this file is a deliberate packaging change anyway.
 *
 * Usage: node scripts/shell-fingerprint.mjs <unpackedDir>
 *   Writes <unpackedDir>/shell-fingerprint.txt.
 *
 * The file name is NOT dot-prefixed: the Windows packaging step uses
 * `Compress-Archive -Path <dir>/*`, and PowerShell's `*` glob silently skips
 * hidden items — a dotfile would be missing from the Windows full package and
 * every Windows payload install would then fail to verify.
 */
import fs from 'node:fs'
import path from 'node:path'
import { createHash } from 'node:crypto'
import { fileURLToPath } from 'node:url'

/** Name of the sidecar, as it appears at the app root of an installed shell. */
export const SHELL_FINGERPRINT_FILE = 'shell-fingerprint.txt'

/**
 * Inputs that define the shell's identity, relative to `desktop/`.
 *
 * Add to this list when a new file starts shaping the packaged shell (a new
 * icon, an entitlement plist, …). Anything listed here must be checked into
 * git and free of per-release values.
 */
export const SHELL_IDENTITY_FILES = [
  'electron-builder.yml',
  'build/icon.png',
  'build/icon.icns',
]

const here = path.dirname(fileURLToPath(import.meta.url))
const desktopDir = path.resolve(here, '..')

/**
 * Hash the shell-identity inputs into a stable fingerprint string.
 *
 * Each file contributes its relative path and its bytes, length-prefixed by the
 * separator, so a rename or a moved byte cannot hash to the same value.
 */
export function computeShellFingerprint(root = desktopDir) {
  const h = createHash('sha256')
  for (const rel of SHELL_IDENTITY_FILES) {
    const abs = path.join(root, rel)
    if (!fs.existsSync(abs)) {
      throw new Error(`shell identity input is missing: ${rel} (looked in ${root})`)
    }
    h.update(rel)
    h.update('\0')
    h.update(fs.readFileSync(abs))
    h.update('\0')
  }
  return 'sha256-' + h.digest('hex')
}

/** Write the fingerprint sidecar into an unpacked app directory. */
export function writeShellFingerprint(unpackedDir, root = desktopDir) {
  const target = path.join(unpackedDir, SHELL_FINGERPRINT_FILE)
  fs.writeFileSync(target, computeShellFingerprint(root) + '\n', 'utf8')
  return target
}

/** Read the fingerprint an installed shell recorded, or '' when absent. */
export function readShellFingerprint(dir) {
  try {
    return fs.readFileSync(path.join(dir, SHELL_FINGERPRINT_FILE), 'utf8').trim()
  } catch {
    return ''
  }
}

// CLI: write the sidecar for an unpacked build directory.
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const unpackedDir = process.argv[2]
  if (!unpackedDir) {
    console.error('shell-fingerprint: usage: node scripts/shell-fingerprint.mjs <unpackedDir>')
    process.exit(1)
  }
  if (!fs.existsSync(unpackedDir)) {
    console.error(`shell-fingerprint: no such directory: ${unpackedDir}`)
    process.exit(1)
  }
  const target = writeShellFingerprint(unpackedDir)
  console.log(`shell-fingerprint: ${readShellFingerprint(unpackedDir)} -> ${target}`)
}
