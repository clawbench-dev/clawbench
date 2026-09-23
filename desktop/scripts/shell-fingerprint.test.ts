import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {
  computeShellFingerprint,
  writeShellFingerprint,
  readShellFingerprint,
  SHELL_FINGERPRINT_FILE,
  SHELL_IDENTITY_FILES,
} from './shell-fingerprint.mjs'

/**
 * The fingerprint answers one question: "was this payload built against the
 * shell I am running?" Two failure modes matter, and they pull in opposite
 * directions:
 *
 *   - too UNSTABLE (changes every release) → no payload ever matches, every
 *     upgrade silently falls back to the full ~150MB download;
 *   - too INSENSITIVE (ignores a real shell change) → a stale icon/appId is
 *     reused with nothing able to detect it.
 *
 * The stability test below is the one that would have caught the obvious wrong
 * implementation: hashing the built executable. electron-builder writes the app
 * version into it, and CI rewrites package.json from the release tag each time,
 * so a binary hash changes on every release.
 */
let tmp: string

beforeEach(() => {
  tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'shell-fp-test-'))
})

afterEach(() => {
  fs.rmSync(tmp, { recursive: true, force: true })
})

/** Lay down the identity inputs with given contents. */
function makeInputs(files: Record<string, string>): void {
  for (const [rel, content] of Object.entries(files)) {
    const abs = path.join(tmp, rel)
    fs.mkdirSync(path.dirname(abs), { recursive: true })
    fs.writeFileSync(abs, content)
  }
}

/** The default input set, so a test can override just the parts it cares about. */
function defaultInputs(overrides: Record<string, string> = {}): Record<string, string> {
  const base: Record<string, string> = {}
  for (const rel of SHELL_IDENTITY_FILES) base[rel] = `content of ${rel}`
  return { ...base, ...overrides }
}

describe('computeShellFingerprint', () => {
  it('is stable across releases (the property that makes payloads usable)', () => {
    // Same shell-defining inputs → same fingerprint. This is what a version
    // bump or a rebuilt binary must NOT disturb.
    makeInputs(defaultInputs())
    const first = computeShellFingerprint(tmp)
    const second = computeShellFingerprint(tmp)
    expect(first).toBe(second)
    expect(first).toMatch(/^sha256-[0-9a-f]{64}$/)
  })

  it('does not depend on the app version or build output', () => {
    // Regression guard for the tempting-but-wrong implementation: hashing the
    // executable. If the input set ever grows to include a built artifact, this
    // fails — because a rebuilt output with a different version must not change
    // the shell identity.
    makeInputs(defaultInputs())
    const before = computeShellFingerprint(tmp)

    // Simulate what CI does per release: rewrite the version, rebuild the
    // binary (different bytes, different embedded version).
    fs.writeFileSync(path.join(tmp, 'package.json'), JSON.stringify({ version: '0.100.0' }))
    fs.writeFileSync(path.join(tmp, 'clawbench-desktop'), 'REBUILT-BINARY-v0.100.0')

    expect(computeShellFingerprint(tmp)).toBe(before)
  })

  it('changes when an icon changes', () => {
    // The real case this feature exists for: a new icon is baked into the
    // executable, so a payload must not be installed over the old shell.
    makeInputs(defaultInputs())
    const before = computeShellFingerprint(tmp)

    makeInputs({ 'build/icon.png': 'DIFFERENT ICON BYTES' })

    expect(computeShellFingerprint(tmp)).not.toBe(before)
  })

  it('changes when the packaging config changes', () => {
    // appId / productName / executableName all live here.
    makeInputs(defaultInputs())
    const before = computeShellFingerprint(tmp)

    makeInputs({ 'electron-builder.yml': 'appId: com.other.app\n' })

    expect(computeShellFingerprint(tmp)).not.toBe(before)
  })

  it('ignores unrelated files in the tree', () => {
    // Only the listed inputs define identity. A stray file (or the build
    // output, which is not listed) must not perturb the fingerprint, or
    // payloads would stop matching for reasons unrelated to the shell.
    makeInputs(defaultInputs())
    const before = computeShellFingerprint(tmp)

    fs.writeFileSync(path.join(tmp, 'README.md'), 'unrelated')
    fs.mkdirSync(path.join(tmp, 'dist'), { recursive: true })
    fs.writeFileSync(path.join(tmp, 'dist/main.js'), 'built output')

    expect(computeShellFingerprint(tmp)).toBe(before)
  })

  it('fails loudly when an identity input is missing', () => {
    // Silently skipping a missing input would make two different shells hash
    // the same, which is the dangerous direction.
    makeInputs({ 'electron-builder.yml': 'x' })
    expect(() => computeShellFingerprint(tmp)).toThrow(/identity input is missing/)
  })
})

describe('fingerprint sidecar', () => {
  it('round-trips through the install directory', () => {
    makeInputs(defaultInputs())
    const install = path.join(tmp, 'install')
    fs.mkdirSync(install)

    writeShellFingerprint(install, tmp)

    expect(readShellFingerprint(install)).toBe(computeShellFingerprint(tmp))
  })

  it('returns empty (not a match) when no sidecar was recorded', () => {
    // Every install predating this file. Callers must treat '' as "cannot
    // verify" and refuse the payload, never as compatible.
    const install = path.join(tmp, 'old-install')
    fs.mkdirSync(install)
    expect(readShellFingerprint(install)).toBe('')
  })

  it('is not a dotfile, so a Windows glob cannot skip it', () => {
    // The Windows full package is built with `Compress-Archive -Path <dir>/*`,
    // and PowerShell's `*` omits hidden items. A dot-prefixed name would be
    // silently absent from every Windows full install, so no Windows payload
    // could ever verify.
    expect(SHELL_FINGERPRINT_FILE.startsWith('.')).toBe(false)
  })

  it('writes a file whose bytes are the fingerprint and a newline', () => {
    makeInputs(defaultInputs())
    const install = path.join(tmp, 'install2')
    fs.mkdirSync(install)
    writeShellFingerprint(install, tmp)

    const raw = fs.readFileSync(path.join(install, SHELL_FINGERPRINT_FILE), 'utf8')
    expect(raw).toBe(computeShellFingerprint(tmp) + '\n')
  })
})
