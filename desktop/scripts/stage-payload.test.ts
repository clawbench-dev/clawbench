import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { toPosixPath, listFiles, REQUIRED, stagePayload } from './stage-payload.mjs'

/**
 * These tests exist because of a real Windows-only release failure (v0.102.0).
 *
 * `listFiles` produced paths with `path.relative`, which uses the platform
 * separator — backslash on Windows. `REQUIRED` and the release.yml verification
 * step spell the same paths with forward slashes, so on Windows every entry
 * compared unequal: the job reported all six required files missing and exited
 * 1, while Linux and macOS passed. Nothing in CI runs the desktop tests, so the
 * defect reached a tagged release and had to be found by the release pipeline
 * itself.
 *
 * The regression is therefore about the SEPARATOR, not about the copy. Since
 * `path.sep` is '/' on Linux (where the conversion is a no-op), the Windows
 * behaviour has to be exercised by passing the separator explicitly.
 */

let tmp: string

beforeEach(() => {
  tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'stage-payload-test-'))
})

afterEach(() => {
  fs.rmSync(tmp, { recursive: true, force: true })
})

/** Lay down files (keys are POSIX relative paths) under `root`. */
function writeTree(root: string, files: Record<string, string>): void {
  for (const [rel, content] of Object.entries(files)) {
    const abs = path.join(root, ...rel.split('/'))
    fs.mkdirSync(path.dirname(abs), { recursive: true })
    fs.writeFileSync(abs, content)
  }
}

/** The identity inputs `computeShellFingerprint` requires, under a fake desktop root. */
function makeIdentityInputs(desktopRoot: string): void {
  writeTree(desktopRoot, {
    'electron-builder.yml': 'appId: test\n',
    'build/icon.png': 'png',
    'build/icon.icns': 'icns',
    'node_modules/electron/package.json': JSON.stringify({ version: '44.4.3' }),
  })
}

/** The resources/ tree the payload copies, including one dotfile. */
function makeResources(unpackedDir: string): void {
  writeTree(path.join(unpackedDir, 'resources'), {
    'app.asar': 'asar',
    'login.html': '<html></html>',
    'logo.png': 'png',
    'app.asar.unpacked/node_modules/ssh2/lib/protocol/crypto/build/Release/sshcrypto.node': 'native',
    'app.asar.unpacked/node_modules/cpu-features/build/Release/cpufeatures.node': 'native',
    'app.asar.unpacked/node_modules/cpu-features/.eslintrc.js': 'dotfile',
  })
}

describe('toPosixPath', () => {
  it('converts Windows separators to forward slashes', () => {
    expect(toPosixPath('resources\\app.asar', '\\')).toBe('resources/app.asar')
    expect(
      toPosixPath(
        'resources\\app.asar.unpacked\\node_modules\\cpu-features\\.eslintrc.js',
        '\\',
      ),
    ).toBe('resources/app.asar.unpacked/node_modules/cpu-features/.eslintrc.js')
  })

  it('is a no-op on POSIX (the separator is already "/")', () => {
    expect(toPosixPath('resources/app.asar', '/')).toBe('resources/app.asar')
  })

  it('leaves a bare filename alone on both platforms', () => {
    expect(toPosixPath('payload.json', '\\')).toBe('payload.json')
    expect(toPosixPath('payload.json', '/')).toBe('payload.json')
  })
})

describe('REQUIRED', () => {
  it('is spelled with POSIX separators, matching what listFiles emits', () => {
    // The bug was a separator MISMATCH between these two. If someone ever
    // re-spells REQUIRED with backslashes, this fails.
    for (const entry of REQUIRED) {
      expect(entry).not.toContain('\\')
      expect(entry.startsWith('resources/')).toBe(true)
    }
  })

  it('includes a dotfile, the case a glob-based copy would silently drop', () => {
    expect(REQUIRED.some((f) => f.includes('/.eslintrc.js'))).toBe(true)
  })
})

describe('listFiles', () => {
  it('emits POSIX-separated relative paths for a nested tree', () => {
    writeTree(tmp, {
      'a.txt': 'a',
      'nested/b.txt': 'b',
      'nested/deeper/.hidden': 'h',
    })
    const got = listFiles(tmp)
    expect(got).toEqual(['a.txt', 'nested/b.txt', 'nested/deeper/.hidden'])
  })

  it('never emits a backslash, on any platform', () => {
    writeTree(tmp, { 'nested/deeper/c.txt': 'c' })
    expect(listFiles(tmp).some((f) => f.includes('\\'))).toBe(false)
  })

  it('rejects a symlink rather than silently skipping it', () => {
    writeTree(tmp, { 'real.txt': 'x' })
    fs.symlinkSync(path.join(tmp, 'real.txt'), path.join(tmp, 'link.txt'))
    expect(() => listFiles(tmp)).toThrow(/unsupported entry/)
  })
})

describe('stagePayload', () => {
  it('stages the tree and reports the file count', () => {
    const desktopRoot = path.join(tmp, 'desktop')
    const unpacked = path.join(tmp, 'linux-unpacked')
    makeIdentityInputs(desktopRoot)
    makeResources(unpacked)

    const result = stagePayload(unpacked, desktopRoot)

    expect(result.payloadDir).toBe(path.resolve(unpacked) + '-payload')
    // The count covers resources/ only, matching the `118 files` the CI log
    // reports: payload.json is written separately.
    expect(result.fileCount).toBe(6)
    expect(result.electron).toBe('44.4.3')
    expect(result.shell).toMatch(/^sha256-[0-9a-f]{64}$/)
    expect(fs.existsSync(path.join(result.payloadDir, 'payload.json'))).toBe(true)
    expect(
      fs.existsSync(
        path.join(result.payloadDir, 'resources/app.asar.unpacked/node_modules/cpu-features/.eslintrc.js'),
      ),
    ).toBe(true)
  })

  it('passes its own REQUIRED check — the assertion the Windows job failed', () => {
    // On Windows the pre-fix code threw `payload is missing required files`
    // here even though every file was present, purely because of separators.
    const desktopRoot = path.join(tmp, 'desktop')
    const unpacked = path.join(tmp, 'win-unpacked')
    makeIdentityInputs(desktopRoot)
    makeResources(unpacked)

    expect(() => stagePayload(unpacked, desktopRoot)).not.toThrow()
  })

  it('writes a payload.json carrying both electron and shell', () => {
    const desktopRoot = path.join(tmp, 'desktop')
    const unpacked = path.join(tmp, 'linux-unpacked')
    makeIdentityInputs(desktopRoot)
    makeResources(unpacked)

    const { payloadDir } = stagePayload(unpacked, desktopRoot)
    const manifest = JSON.parse(fs.readFileSync(path.join(payloadDir, 'payload.json'), 'utf8'))
    expect(manifest.electron).toBe('44.4.3')
    expect(manifest.shell).toMatch(/^sha256-/)
  })

  it('throws (not exits) when a required file is absent', () => {
    const desktopRoot = path.join(tmp, 'desktop')
    const unpacked = path.join(tmp, 'linux-unpacked')
    makeIdentityInputs(desktopRoot)
    // app.asar deliberately omitted.
    writeTree(path.join(unpacked, 'resources'), { 'login.html': '<html></html>' })

    expect(() => stagePayload(unpacked, desktopRoot)).toThrow(/missing required files.*app\.asar/)
  })

  it('throws when resources/ is absent', () => {
    const desktopRoot = path.join(tmp, 'desktop')
    const unpacked = path.join(tmp, 'linux-unpacked')
    makeIdentityInputs(desktopRoot)

    expect(() => stagePayload(unpacked, desktopRoot)).toThrow(/no resources\/ directory/)
  })

  it('throws when electron is not installed', () => {
    const desktopRoot = path.join(tmp, 'desktop')
    const unpacked = path.join(tmp, 'linux-unpacked')
    // identity inputs minus node_modules/electron.
    writeTree(desktopRoot, {
      'electron-builder.yml': 'appId: test\n',
      'build/icon.png': 'png',
      'build/icon.icns': 'icns',
    })
    makeResources(unpacked)

    expect(() => stagePayload(unpacked, desktopRoot)).toThrow(/electron is not installed/)
  })
})

/**
 * The conversion is a no-op on Linux, so no runtime test above can fail if
 * `toPosixPath` is dropped from `listFiles`. This reads the source and pins the
 * call site, which is the only check that stays honest across platforms.
 *
 * `?raw` rather than `fs.readFileSync(import.meta.url)`: vitest does not expose
 * a file: URL here, so the source is pulled through Vite instead (the same
 * pattern the frontend source guards use).
 */
describe('source guard: listFiles must normalize its output', () => {
  it('wraps path.relative in toPosixPath', async () => {
    const source = String((await import('./stage-payload.mjs?raw')).default)
    expect(source).toMatch(/out\.push\(toPosixPath\(path\.relative\(/)
    // The raw form is what broke Windows.
    expect(source).not.toMatch(/out\.push\(path\.relative\(/)
  })
})
