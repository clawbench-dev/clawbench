import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import path from 'node:path'
import fs from 'node:fs'
import os from 'node:os'
import { deflateRawSync } from 'node:zlib'
import { createHash } from 'node:crypto'

// The installer touches `app` only in restartInto(); mock electron so the pure
// extraction/pointer logic can be exercised without a running Electron process.
vi.mock('electron', () => ({
  app: { getVersion: () => '0.1.0', relaunch: vi.fn(), exit: vi.fn() },
}))

// ESM module namespaces are not configurable, so `vi.spyOn(childProcess, 'spawn')`
// throws. Mock the module instead: the handoff must never actually launch a
// process during tests.
// vi.hoisted because the factory below is hoisted above ordinary declarations;
// referencing a plain `const` there would throw "Cannot access before
// initialization". This mirrors how tunnel.test.ts exposes its FakeClient.
const { spawnMock } = vi.hoisted(() => ({
  // Typed loosely: the assertions only inspect the first argument (the binary
  // path), and giving vi.fn a precise overload here would fight the spread of
  // child_process' own signatures.
  spawnMock: vi.fn((..._args: unknown[]) => ({ unref: () => {} })),
}))
vi.mock('node:child_process', () => ({
  spawn: spawnMock,
  // Pass through everything else the module graph may need.
  default: { spawn: spawnMock },
}))

import {
  extractZip,
  writePointer,
  readCurrentVersion,
  pointerPath,
  versionDir,
  downloadFirstAvailable,
  selectStartupVersion,
  handOffToPointedVersion,
} from './install'

/**
 * Build a zip archive from entries.
 *
 * Entries are `{ name, content, method?, mode? }`; a trailing slash marks a
 * directory. Paths are written verbatim so tests can include the release
 * wrapper directory and deliberately hostile names.
 */
function makeZip(entries: Array<{ name: string; content?: string | Buffer; method?: number; mode?: number }>): Buffer {
  const locals: Buffer[] = []
  const centrals: Buffer[] = []
  let offset = 0

  for (const e of entries) {
    const isDir = e.name.endsWith('/')
    const data = Buffer.isBuffer(e.content) ? e.content : Buffer.from(e.content ?? '')
    const nameBuf = Buffer.from(e.name, 'utf8')
    const method = e.method ?? (isDir ? 0 : 8)
    const payload = method === 8 ? deflateRawSync(data) : data
    const crc = crc32(data)
    // Default: directories 0o40755, files 0o100644.
    const mode = e.mode ?? (isDir ? 0o040755 : 0o100644)

    const lh = Buffer.alloc(30)
    lh.writeUInt32LE(0x04034b50, 0)
    lh.writeUInt16LE(20, 4)
    lh.writeUInt16LE(0, 6)
    lh.writeUInt16LE(method, 8)
    lh.writeUInt32LE(crc, 14)
    lh.writeUInt32LE(payload.length, 18)
    lh.writeUInt32LE(data.length, 22)
    lh.writeUInt16LE(nameBuf.length, 26)
    lh.writeUInt16LE(0, 28)

    const ch = Buffer.alloc(46)
    ch.writeUInt32LE(0x02014b50, 0)
    ch.writeUInt16LE(20, 4)
    ch.writeUInt16LE(20, 6)
    ch.writeUInt16LE(0, 8)
    ch.writeUInt16LE(method, 10)
    ch.writeUInt32LE(crc, 16)
    ch.writeUInt32LE(payload.length, 20)
    ch.writeUInt32LE(data.length, 24)
    ch.writeUInt16LE(nameBuf.length, 28)
    ch.writeUInt16LE(0, 30)
    ch.writeUInt16LE(0, 32)
    ch.writeUInt16LE(0, 34)
    ch.writeUInt16LE(0, 36)
    ch.writeUInt32LE((mode << 16) >>> 0, 38)
    ch.writeUInt32LE(offset, 42)

    locals.push(lh, nameBuf, payload)
    centrals.push(ch, nameBuf)
    offset += lh.length + nameBuf.length + payload.length
  }

  const centralBuf = Buffer.concat(centrals)
  const eocd = Buffer.alloc(22)
  eocd.writeUInt32LE(0x06054b50, 0)
  eocd.writeUInt16LE(entries.length, 8)
  eocd.writeUInt16LE(entries.length, 10)
  eocd.writeUInt32LE(centralBuf.length, 12)
  eocd.writeUInt32LE(offset, 16)

  return Buffer.concat([...locals, centralBuf, eocd])
}

const CRC_TABLE = (() => {
  const t = new Uint32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    t[n] = c >>> 0
  }
  return t
})()

function crc32(buf: Buffer): number {
  let c = 0xffffffff
  for (const b of buf) c = CRC_TABLE[(c ^ b) & 0xff] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}

let tmpRoot: string

beforeEach(() => {
  tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'clawbench-install-test-'))
})

afterEach(() => {
  fs.rmSync(tmpRoot, { recursive: true, force: true })
})

describe('extractZip', () => {
  it('strips the single top-level wrapper directory', () => {
    // Release assets wrap everything in e.g. linux-unpacked/, which must not
    // survive: the launcher expects the app root.
    const zip = makeZip([
      { name: 'linux-unpacked/' },
      { name: 'linux-unpacked/clawbench-desktop', content: 'BIN' },
      { name: 'linux-unpacked/resources/app.asar', content: 'ASAR' },
    ])

    extractZip(zip, tmpRoot)

    expect(fs.readFileSync(path.join(tmpRoot, 'clawbench-desktop'), 'utf8')).toBe('BIN')
    expect(fs.readFileSync(path.join(tmpRoot, 'resources/app.asar'), 'utf8')).toBe('ASAR')
    expect(fs.existsSync(path.join(tmpRoot, 'linux-unpacked'))).toBe(false)
  })

  it('keeps paths as-is when entries share no wrapper', () => {
    const zip = makeZip([
      { name: 'clawbench-desktop', content: 'BIN' },
      { name: 'resources/app.asar', content: 'ASAR' },
    ])
    extractZip(zip, tmpRoot)
    expect(fs.readFileSync(path.join(tmpRoot, 'clawbench-desktop'), 'utf8')).toBe('BIN')
  })

  it('does not strip a prefix that only some entries share', () => {
    // A stray top-level file must not be dropped by stripping a prefix the
    // other entries happen to have.
    const zip = makeZip([
      { name: 'mac/ClawBench', content: 'BIN' },
      { name: 'README.txt', content: 'top level' },
    ])
    extractZip(zip, tmpRoot)
    expect(fs.readFileSync(path.join(tmpRoot, 'mac/ClawBench'), 'utf8')).toBe('BIN')
    expect(fs.readFileSync(path.join(tmpRoot, 'README.txt'), 'utf8')).toBe('top level')
  })

  it('restores the executable bit from the archive', () => {
    // Load-bearing: without it the Linux binary extracts non-executable and
    // the app will not start after an upgrade.
    const zip = makeZip([
      { name: 'linux-unpacked/clawbench-desktop', content: 'BIN', mode: 0o100755 },
      { name: 'linux-unpacked/icudtl.dat', content: 'DATA', mode: 0o100644 },
    ])
    extractZip(zip, tmpRoot)

    const binMode = fs.statSync(path.join(tmpRoot, 'clawbench-desktop')).mode
    const dataMode = fs.statSync(path.join(tmpRoot, 'icudtl.dat')).mode
    expect(binMode & 0o111).toBeTruthy()
    expect(dataMode & 0o111).toBeFalsy()
  })

  it('handles stored (uncompressed) entries', () => {
    const zip = makeZip([{ name: 'pkg/file.txt', content: 'plain', method: 0 }])
    extractZip(zip, tmpRoot)
    expect(fs.readFileSync(path.join(tmpRoot, 'file.txt'), 'utf8')).toBe('plain')
  })

  it('round-trips binary content exactly', () => {
    const bytes = Buffer.from(Array.from({ length: 4096 }, (_, i) => i % 256))
    const zip = makeZip([{ name: 'pkg/blob.bin', content: bytes }])
    extractZip(zip, tmpRoot)
    expect(fs.readFileSync(path.join(tmpRoot, 'blob.bin')).equals(bytes)).toBe(true)
  })

  it('rejects an entry that escapes the destination', () => {
    const zip = makeZip([{ name: 'pkg/../../evil.txt', content: 'pwned' }])
    expect(() => extractZip(zip, tmpRoot)).toThrow(/escapes destination/)
    expect(fs.existsSync(path.join(tmpRoot, '..', '..', 'evil.txt'))).toBe(false)
  })

  it('rejects a non-zip buffer', () => {
    expect(() => extractZip(Buffer.from('not a zip at all'), tmpRoot)).toThrow(/not a zip/)
  })
})

/**
 * The macOS release archive is the one platform that keeps a wrapper
 * directory, and the two halves of that decision live in different files:
 * release.yml produces the wrapper, and the extraction above relies on it to
 * be the stripped prefix.
 *
 * The trap is that dropping the wrapper looks like a harmless cleanup — it is
 * what Windows and Linux do. On macOS it is not: every entry would then share
 * the top level `ClawBench.app/`, which the stripper would remove, and the
 * installer would go looking for an executable inside a bundle it had just
 * thrown away. Keeping the wrapper is also what lets clients that shipped
 * before this change (and run the unconditional-strip installer) upgrade.
 */
describe('macOS archive keeps its wrapper directory', () => {
  it('extracts the .app bundle, not its contents, when the wrapper is present', () => {
    const zip = makeZip([
      { name: 'mac-arm64/' },
      { name: 'mac-arm64/ClawBench.app/Contents/MacOS/clawbench-desktop', content: 'BIN' },
      { name: 'mac-arm64/ClawBench.app/Contents/Info.plist', content: 'PLIST' },
    ])

    extractZip(zip, tmpRoot)

    // The wrapper is gone, but the bundle survived intact — this is the shape
    // executableIn() expects on darwin.
    expect(fs.existsSync(path.join(tmpRoot, 'mac-arm64'))).toBe(false)
    expect(
      fs.readFileSync(path.join(tmpRoot, 'ClawBench.app/Contents/MacOS/clawbench-desktop'), 'utf8'),
    ).toBe('BIN')
  })

  it('documents what goes wrong if the wrapper is dropped', () => {
    // Same payload, archived from inside the wrapper the way Windows/Linux are.
    // This is the layout that must NOT be produced for macOS: the stripper
    // removes the single shared top level, which here is the bundle itself.
    const zip = makeZip([
      { name: 'ClawBench.app/Contents/MacOS/clawbench-desktop', content: 'BIN' },
      { name: 'ClawBench.app/Contents/Info.plist', content: 'PLIST' },
    ])

    extractZip(zip, tmpRoot)

    // Asserting the loss explicitly: this is why release.yml does not flatten
    // the macOS archive, and the test fails loudly if someone does.
    expect(fs.existsSync(path.join(tmpRoot, 'ClawBench.app'))).toBe(false)
    expect(fs.existsSync(path.join(tmpRoot, 'Contents/MacOS/clawbench-desktop'))).toBe(true)
  })
})

describe('downloadFirstAvailable', () => {
  it('reports every failed candidate so a broken mirror is diagnosable', async () => {
    await expect(
      downloadFirstAvailable(['http://127.0.0.1:1/a.zip', 'http://127.0.0.1:1/b.zip']),
    ).rejects.toThrow(/all download sources failed/)
  })
})

describe('pointer file', () => {
  it('round-trips a version and leaves no temp file behind', () => {
    const spy = vi.spyOn(os, 'homedir').mockReturnValue(tmpRoot)
    try {
      writePointer('1.2.3')
      expect(readCurrentVersion()).toBe('1.2.3')
      const leftovers = fs.readdirSync(path.dirname(pointerPath())).filter((f) => f.includes('.tmp-'))
      expect(leftovers).toEqual([])
    } finally {
      spy.mockRestore()
    }
  })

  it('returns empty string when the pointer is absent', () => {
    const spy = vi.spyOn(os, 'homedir').mockReturnValue(tmpRoot)
    try {
      expect(readCurrentVersion()).toBe('')
    } finally {
      spy.mockRestore()
    }
  })
})

/**
 * Startup handoff. The pointer is written by downloadAndInstall and, before
 * desktop distribution moved to GitHub Releases, was read only by the npm
 * launcher. Users now run the unpacked binary directly, so the app has to
 * resolve the pointer itself — otherwise an upgrade is written but never
 * applied and the next cold start silently reverts.
 */
describe('selectStartupVersion', () => {
  it('switches to a pointed version that differs from the running one', () => {
    expect(selectStartupVersion('1.2.3', '1.0.0')).toBe('1.2.3')
  })

  it('does nothing when the pointer names the running version', () => {
    // The normal case right after a restart. Returning a version here would
    // make the handoff re-exec itself forever.
    expect(selectStartupVersion('1.2.3', '1.2.3')).toBe('')
  })

  it('does nothing when there is no pointer', () => {
    expect(selectStartupVersion('', '1.0.0')).toBe('')
  })
})

describe('handOffToPointedVersion', () => {
  let homedirSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    homedirSpy = vi.spyOn(os, 'homedir').mockReturnValue(tmpRoot)
    spawnMock.mockClear()
  })

  afterEach(() => {
    homedirSpy.mockRestore()
  })

  /** Create the version directory with an executable inside it. */
  function installVersion(version: string): string {
    const dir = versionDir(version)
    fs.mkdirSync(dir, { recursive: true })
    const bin = path.join(dir, process.platform === 'win32' ? 'clawbench-desktop.exe' : 'clawbench-desktop')
    fs.writeFileSync(bin, 'x')
    return bin
  }

  it('starts the pointed version and reports the handoff', () => {
    installVersion('1.2.3')
    writePointer('1.2.3')

    expect(handOffToPointedVersion('1.0.0')).toBe(true)
    expect(spawnMock).toHaveBeenCalledTimes(1)
    expect(String(spawnMock.mock.calls[0]?.[0])).toContain('1.2.3')
  })

  it('does not hand off when the pointer names the running version', () => {
    installVersion('1.2.3')
    writePointer('1.2.3')

    expect(handOffToPointedVersion('1.2.3')).toBe(false)
    expect(spawnMock).not.toHaveBeenCalled()
  })

  it('does not hand off when there is no pointer', () => {
    expect(handOffToPointedVersion('1.0.0')).toBe(false)
    expect(spawnMock).not.toHaveBeenCalled()
  })

  it('clears a pointer whose version directory is gone, instead of refusing to start', () => {
    // A deleted or half-written upgrade must not brick the app: the pointer is
    // dropped and the current version keeps running.
    writePointer('9.9.9') // never installed

    expect(handOffToPointedVersion('1.0.0')).toBe(false)
    expect(spawnMock).not.toHaveBeenCalled()
    expect(readCurrentVersion()).toBe('')
  })

  it('clears a pointer whose version directory exists but has no executable', () => {
    fs.mkdirSync(versionDir('9.9.9'), { recursive: true })
    writePointer('9.9.9')

    expect(handOffToPointedVersion('1.0.0')).toBe(false)
    expect(spawnMock).not.toHaveBeenCalled()
    expect(readCurrentVersion()).toBe('')
  })

  it('does not re-exec when the pointed binary IS the running process', () => {
    // Happens when someone unpacks a release on top of the version store: the
    // pointer would name the file we are already running, and spawning it
    // would loop. Compare against process.execPath, which the guard realpaths.
    const bin = installVersion('1.2.3')
    writePointer('1.2.3')
    const execSpy = vi.spyOn(process, 'execPath', 'get').mockReturnValue(bin)

    try {
      expect(handOffToPointedVersion('1.0.0')).toBe(false)
      expect(spawnMock).not.toHaveBeenCalled()
      // The pointer is dropped so the next start is a normal one.
      expect(readCurrentVersion()).toBe('')
    } finally {
      execSpy.mockRestore()
    }
  })
})

describe('integrity gate', () => {
  it('accepts a matching sha512 and rejects a mismatch', async () => {
    const { verifyIntegrity } = await import('../shared/integrity')
    const buf = Buffer.from('hello')
    const good = 'sha512-' + createHash('sha512').update(buf).digest('base64')
    expect(verifyIntegrity(buf, good)).toBe(true)
    expect(verifyIntegrity(buf, 'sha512-' + Buffer.alloc(64).toString('base64'))).toBe(false)
  })
})

describe('versionDir', () => {
  it('namespaces each version so upgrades can coexist', () => {
    const spy = vi.spyOn(os, 'homedir').mockReturnValue(tmpRoot)
    try {
      expect(versionDir('1.0.0')).toBe(path.join(tmpRoot, '.clawbench-desktop', 'app-1.0.0'))
      expect(versionDir('1.0.1')).not.toBe(versionDir('1.0.0'))
    } finally {
      spy.mockRestore()
    }
  })
})
