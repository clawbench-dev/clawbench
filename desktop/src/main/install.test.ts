import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import path from 'node:path'
import fs from 'node:fs'
import os from 'node:os'
import { deflateRawSync, gzipSync, gunzipSync } from 'node:zlib'
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
const { spawnMock, makeSpawnedChild } = vi.hoisted(() => {
  /**
   * Minimal ChildProcess stand-in. `install.ts` waits for 'spawn' (success) or
   * 'error' (the binary could not start) before it exits, so the fake must
   * deliver one of them asynchronously like the real module does.
   */
  interface FakeChild {
    unref: () => void
    once: (event: string, cb: (...args: unknown[]) => void) => FakeChild
  }

  const makeSpawnedChild = (fail?: Error): FakeChild => {
    const handlers = new Map<string, ((...args: unknown[]) => void)[]>()
    const child: FakeChild = {
      unref: () => {},
      once: (event, cb) => {
        const list = handlers.get(event) ?? []
        list.push(cb)
        handlers.set(event, list)
        if (event === 'spawn' && !fail) queueMicrotask(() => cb())
        if (event === 'error' && fail) queueMicrotask(() => cb(fail))
        return child
      },
    }
    return child
  }
  // Typed loosely: the assertions only inspect the first argument (the binary
  // path), and giving vi.fn a precise overload here would fight the spread of
  // child_process' own signatures.
  return { spawnMock: vi.fn((..._args: unknown[]) => makeSpawnedChild()), makeSpawnedChild }
})
vi.mock('node:child_process', () => ({
  spawn: spawnMock,
  // Pass through everything else the module graph may need.
  default: { spawn: spawnMock },
}))

// Downloads go through install.ts's own httpGetBuffer, which
// calls `lib.get(url, cb)` and then reads `res.statusCode` and the `data`/`end`
// events. Stand in for node:https (and node:http, which httpGetBuffer picks for
// http: URLs) with a minimal server that honours that contract, so a real
// archive body can be served without touching the network.
//
// `download` is the control: return a Buffer to serve it as a 200 body, or
// throw to simulate an unreachable host (which downloadFirstAvailable must
// report for every candidate).
const { download, downloadSpy, downloadOptsSpy, redirect, transfer } = vi.hoisted(() => ({
  downloadSpy: vi.fn(),
  downloadOptsSpy: vi.fn(),
  // One-shot 302 target, so a redirect chain can be exercised (and cannot loop).
  redirect: { to: '' },
  // Per-URL transfer shape for the progress-guard tests. `stall` accepts the
  // response and then sends nothing (a mirror that connected and went quiet);
  // `trickle` sends a few bytes at a time (alive but unusably slow).
  transfer: { stall: new Set<string>(), trickle: new Set<string>() },
  download: { impl: (() => Buffer.alloc(0)) as (url: string) => Buffer },
}))

function makeGetMock() {
  const get = (url: string, optsOrCb: unknown, maybeCb?: unknown) => {
    // `httpGetBuffer` may pass node's optional options argument.
    const cb = (typeof optsOrCb === 'function' ? optsOrCb : maybeCb) as (res: unknown) => void
    downloadSpy(url)
    downloadOptsSpy(url, typeof optsOrCb === 'function' ? undefined : optsOrCb)
    const handlers: Record<string, Array<(arg?: unknown) => void>> = {}
    const timers: Array<ReturnType<typeof setInterval>> = []
    const fail = (err: unknown) => {
      for (const t of timers) clearInterval(t)
      queueMicrotask(() => {
        for (const fn of handlers.error ?? []) fn(err)
      })
    }
    const req = {
      on(ev: string, fn: (arg?: unknown) => void) {
        ;(handlers[ev] ||= []).push(fn)
        return req
      },
      // `httpGetBuffer` destroys the request when its progress guard gives up,
      // which surfaces as an 'error' event just like a real abort.
      destroy(err?: unknown) {
        fail(err)
        return req
      },
    }
    let body: Buffer
    try {
      body = download.impl(url)
    } catch (err) {
      fail(err)
      return req
    }
    const location = redirect.to
    redirect.to = ''
    const res = {
      statusCode: location ? 302 : 200,
      headers: location ? { location } : {},
      resume: () => {},
      on(ev: string, fn: (arg?: unknown) => void) {
        ;(handlers[ev] ||= []).push(fn)
        return res
      },
    }
    cb(res)
    if (transfer.stall.has(url)) return req
    if (transfer.trickle.has(url)) {
      let sent = 0
      timers.push(
        setInterval(() => {
          if (sent++ >= 10) return
          for (const fn of handlers.data ?? []) fn(Buffer.alloc(64))
        }, 15),
      )
      return req
    }
    queueMicrotask(() => {
      for (const fn of handlers.data ?? []) fn(body)
      for (const fn of handlers.end ?? []) fn()
    })
    return req
  }
  return { get, default: { get } }
}

vi.mock('node:https', makeGetMock)
vi.mock('node:http', makeGetMock)

import {
  extractZip,
  extractTarGz,
  extractArchive,
  looksLikeArchive,
  writePointer,
  readCurrentVersion,
  pointerPath,
  versionDir,
  downloadFirstAvailable,
  httpGetBuffer,
  downloadAndInstall,
  isSecureDownloadUrl,
  selectStartupVersion,
  handOffToPointedVersion,
  restartInto,
  assetNameFromUrl,
  sha256Hex,
} from './install'
import { app } from 'electron'

/**
 * Fingerprint the test shells advertise. Its VALUE is irrelevant to these
 * tests — only whether the two sides agree — so a literal keeps them readable.
 */
const SHELL_FP = 'sha256-test-shell-identity'

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

/**
 * Build a gzipped tar archive, the way an npm tarball is shaped.
 *
 * Entries are `{ name, content, type?, prefix?, mode? }`. `prefix` exercises the
 * ustar long-path mechanism npm uses (it splits a long path across the `name`
 * and `prefix` fields rather than emitting a PAX header).
 *
 * This writer exists only to build well-formed fixtures. The malformed-input
 * tests below build their bytes by hand instead — feeding a corrupt archive to
 * the same code that wrote it would prove nothing.
 */
function makeTarGz(
  entries: Array<{
    name: string
    content?: string | Buffer
    type?: string
    prefix?: string
    mode?: number
  }>,
): Buffer {
  const blocks: Buffer[] = []

  for (const e of entries) {
    const data = Buffer.isBuffer(e.content) ? e.content : Buffer.from(e.content ?? '')
    const type = e.type ?? '0'
    const isDir = type === '5'

    const header = Buffer.alloc(512)
    header.write(e.name, 0, 100, 'utf8')
    header.write((e.mode ?? (isDir ? 0o755 : 0o644)).toString(8).padStart(7, '0') + '\0', 100, 8, 'utf8')
    header.write('0000000\0', 108, 8, 'utf8') // uid
    header.write('0000000\0', 116, 8, 'utf8') // gid
    header.write(data.length.toString(8).padStart(11, '0') + '\0', 124, 12, 'utf8')
    header.write('00000000000\0', 136, 12, 'utf8') // mtime
    header.write('        ', 148, 8, 'utf8') // checksum placeholder
    header.write(type, 156, 1, 'utf8')
    header.write('ustar\0', 257, 6, 'utf8')
    header.write('00', 263, 2, 'utf8')
    if (e.prefix) header.write(e.prefix, 345, 155, 'utf8')

    // Checksum: sum of all header bytes with the checksum field treated as spaces.
    let sum = 0
    for (const b of header) sum += b
    header.write(sum.toString(8).padStart(6, '0') + '\0 ', 148, 8, 'utf8')

    blocks.push(header)
    if (!isDir && data.length > 0) {
      blocks.push(data)
      const pad = (512 - (data.length % 512)) % 512
      if (pad) blocks.push(Buffer.alloc(pad))
    }
  }

  blocks.push(Buffer.alloc(1024)) // two zero blocks terminate the archive
  return gzipSync(Buffer.concat(blocks))
}

/** A PAX extended header block carrying the given records, for the next entry. */
function makePaxBlock(records: Record<string, string>): Buffer {
  const body = Object.entries(records)
    .map(([k, v]) => {
      // Length-prefixed: LEN includes its own digits and the trailing newline.
      const base = `${k}=${v}\n`
      let len = base.length + 1
      while (String(len).length + base.length !== len) len = String(len).length + base.length
      return `${len} ${base}`
    })
    .join('')
  const header = Buffer.alloc(512)
  header.write('PaxHeader', 0, 100, 'utf8')
  header.write('0000644\0', 100, 8, 'utf8')
  header.write('0000000\0', 108, 8, 'utf8')
  header.write('0000000\0', 116, 8, 'utf8')
  header.write(Buffer.byteLength(body).toString(8).padStart(11, '0') + '\0', 124, 12, 'utf8')
  header.write('00000000000\0', 136, 12, 'utf8')
  header.write('        ', 148, 8, 'utf8')
  header.write('x', 156, 1, 'utf8')
  header.write('ustar\0', 257, 6, 'utf8')
  header.write('00', 263, 2, 'utf8')
  let sum = 0
  for (const b of header) sum += b
  header.write(sum.toString(8).padStart(6, '0') + '\0 ', 148, 8, 'utf8')

  const bodyBuf = Buffer.from(body, 'utf8')
  const pad = (512 - (bodyBuf.length % 512)) % 512
  return Buffer.concat([header, bodyBuf, Buffer.alloc(pad)])
}

/** Recursively read a directory into a path → content map, for tree comparison. */
function readTree(root: string, base = root, out: Record<string, Buffer> = {}): Record<string, Buffer> {
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    const abs = path.join(root, entry.name)
    if (entry.isDirectory()) readTree(abs, base, out)
    else if (entry.isFile()) out[path.relative(base, abs)] = fs.readFileSync(abs)
  }
  return out
}

/** `process.noAsar` as seen from the test: untyped under plain Node. */
function noAsar(): boolean {
  return (process as { noAsar?: boolean }).noAsar === true
}

function setNoAsar(value: boolean | undefined): void {
  ;(process as { noAsar?: boolean }).noAsar = value
}

/**
 * Whether the shim would treat `p` as an asar archive.
 *
 * Electron's real rule is a path PREFIX test — anything under an `.asar` is
 * also intercepted. Last-component matching is enough here because the
 * installer never addresses a path inside an archive: it only ever creates,
 * copies or deletes the archive itself.
 */
function isAsarArchivePath(p: string): boolean {
  const trimmed = p.replace(/[\\/]+$/, '')
  const last = trimmed.slice(Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\')) + 1)
  return last.endsWith('.asar')
}

interface AsarShim {
  /** Every intercepted call, in order, with the `process.noAsar` it observed. */
  observed: Array<{ op: string; path: string; noAsar: boolean }>
  restore(): void
}

/**
 * Emulate Electron's asar fs shim for the duration of one test.
 *
 * Under plain Node `process.noAsar` is inert — there is no shim to disable — so
 * a test that just ran the installer would pass even with the fix reverted.
 * These patches reproduce what Electron 44.4.3 was measured doing (see
 * `withoutAsar`'s comment in install.ts):
 *
 *   - opening, writing or copying an `.asar` path throws `Invalid package`;
 *   - `existsSync` reports false for a valid archive.
 *
 * Notably NOT modelled, because the real shim does not do it either: `readdir`
 * still lists the archive as a regular FILE, and `linkSync`, `chmodSync`,
 * `rmSync` and `renameSync` work normally on it (or on a directory containing
 * it). Modelling those would have let a test "prove" a hazard that does not
 * exist.
 *
 * Each intercepted call records the flag it saw, so tests can assert the
 * installer disabled interception around its filesystem burst — and, just as
 * importantly, that it did not leave it disabled afterwards.
 */
function installAsarShim(): AsarShim {
  const observed: AsarShim['observed'] = []
  const record = (op: string, p: string) => observed.push({ op, path: p, noAsar: noAsar() })
  const blocked = (p: string) => !noAsar() && isAsarArchivePath(p)

  const real = {
    writeFileSync: fs.writeFileSync,
    copyFileSync: fs.copyFileSync,
    linkSync: fs.linkSync,
    existsSync: fs.existsSync,
  }

  const spies = [
    vi.spyOn(fs, 'writeFileSync').mockImplementation(((file: unknown, ...rest: unknown[]) => {
      const p = String(file)
      record('write', p)
      if (blocked(p)) throw new Error(`Invalid package ${p}`)
      return (real.writeFileSync as (...a: unknown[]) => void)(file, ...rest)
    }) as typeof fs.writeFileSync),

    vi.spyOn(fs, 'copyFileSync').mockImplementation(((from: unknown, to: unknown, ...rest: unknown[]) => {
      const p = String(from)
      record('copy', p)
      if (blocked(p)) throw new Error(`Invalid package ${p}`)
      return (real.copyFileSync as (...a: unknown[]) => void)(from, to, ...rest)
    }) as typeof fs.copyFileSync),

    // Recorded but never blocked: measured on 44.4.3, `linkSync` on an archive
    // succeeds even with the shim on. Tests assert on the flag it saw.
    vi.spyOn(fs, 'linkSync').mockImplementation(((from: unknown, to: unknown) => {
      record('link', String(from))
      return (real.linkSync as (...a: unknown[]) => void)(from, to)
    }) as typeof fs.linkSync),

    vi.spyOn(fs, 'existsSync').mockImplementation(((p: unknown) => {
      const target = String(p)
      record('exists', target)
      // The shim answers from the archive's index, which does not contain the
      // archive itself. No current code path reaches this: cloneTree skips the
      // containing directory wholesale when the payload provides it, so the
      // archive's own existence is never probed. Modelled anyway — a refactor
      // that started probing `.asar` paths directly would otherwise look
      // harmless here and silently overwrite the payload's archive.
      if (blocked(target)) return false
      return real.existsSync(p as fs.PathLike)
    }) as typeof fs.existsSync),
  ]

  return {
    observed,
    restore() {
      for (const s of spies) s.mockRestore()
    },
  }
}

/**
 * Run `fn` with `process.platform` reporting `platform`.
 *
 * `platform` is a configurable (non-writable) data property, so it is redefined
 * rather than spied on — same technique the appRoot test uses for
 * `process.resourcesPath`.
 */
function withPlatform<T>(platform: string, fn: () => T): T {
  const original = Object.getOwnPropertyDescriptor(process, 'platform')
  Object.defineProperty(process, 'platform', { value: platform, configurable: true })
  try {
    return fn()
  } finally {
    if (original) Object.defineProperty(process, 'platform', original)
  }
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
    // Every candidate unreachable: downloadFirstAvailable must surface all of
    // them, not just the first, so a dead mirror is diagnosable from the error.
    download.impl = () => {
      throw new Error('ECONNREFUSED')
    }
    try {
      await expect(
        downloadFirstAvailable(['https://mirror-a/a.zip', 'https://github.com/b.zip']),
      ).rejects.toThrow(/all download sources failed/)
      await expect(
        downloadFirstAvailable(['https://mirror-a/a.zip', 'https://github.com/b.zip']),
      ).rejects.toThrow(/mirror-a[\s\S]*github\.com/)
    } finally {
      download.impl = () => Buffer.alloc(0)
    }
  })

  // A mirror that is alive but useless never "fails", and the walk only
  // advances on failure. These two cases are the shapes that actually happen in
  // the wild (measured 2026-11-11): a proxy that accepts the connection and
  // sends nothing, and one that trickles at tens of KB/s.
  it('abandons a mirror that accepts the connection and then goes quiet', async () => {
    const stalled = 'https://mirror-a/stalled.zip'
    const good = 'https://mirror-b/good.zip'
    const zip = makeZip([{ name: 'app/index.html', content: 'hi' }])
    downloadSpy.mockClear()
    transfer.stall.add(stalled)
    download.impl = (url) => (url === stalled ? Buffer.alloc(0) : zip)
    try {
      const buf = await downloadFirstAvailable([stalled, good], {}, { idleMs: 40, minBytesPerSecond: 0 })
      expect(buf.equals(zip)).toBe(true)
      expect(downloadSpy.mock.calls.map((c) => c[0])).toEqual([stalled, good])
    } finally {
      transfer.stall.clear()
      download.impl = () => Buffer.alloc(0)
    }
  })

  it('abandons a mirror that trickles below the rate floor', async () => {
    const slow = 'https://mirror-a/slow.zip'
    const good = 'https://mirror-b/good.zip'
    const zip = makeZip([{ name: 'app/index.html', content: 'hi' }])
    downloadSpy.mockClear()
    transfer.trickle.add(slow)
    download.impl = (url) => (url === slow ? Buffer.alloc(0) : zip)
    try {
      const buf = await downloadFirstAvailable([slow, good], {}, { idleMs: 40, minBytesPerSecond: 100 * 1024 })
      expect(buf.equals(zip)).toBe(true)
      expect(downloadSpy.mock.calls.map((c) => c[0])).toEqual([slow, good])
    } finally {
      transfer.trickle.clear()
      download.impl = () => Buffer.alloc(0)
    }
  })
})

describe('sha256 verification', () => {
  beforeEach(() => {
    downloadSpy.mockReset()
    redirect.to = ''
  })

  it('accepts a candidate whose bytes match the published hash', async () => {
    const good = makeZip([{ name: 'a.txt', content: 'OK' }])
    download.impl = () => good

    const buf = await downloadFirstAvailable(['https://mirror/x.zip'], { 'x.zip': sha256Hex(good) })
    expect(buf.equals(good)).toBe(true)
  })

  it('rejects a mismatch and tries the next candidate', async () => {
    const good = makeZip([{ name: 'a.txt', content: 'OK' }])
    const evil = makeZip([{ name: 'a.txt', content: 'EVIL' }])
    download.impl = (url) => (url.includes('bad') ? evil : good)

    const buf = await downloadFirstAvailable(
      ['https://bad/x.zip', 'https://good/x.zip'],
      { 'x.zip': sha256Hex(good) },
    )

    expect(buf.equals(good)).toBe(true)
    expect(downloadSpy).toHaveBeenCalledTimes(2)
  })

  it('proceeds when the server did not publish a checksum', async () => {
    const good = makeZip([{ name: 'a.txt', content: 'OK' }])
    download.impl = () => good

    const buf = await downloadFirstAvailable(['https://mirror/x.zip'], {})
    expect(buf.equals(good)).toBe(true)
  })

  it('derives the asset name from a proxied URL', () => {
    expect(assetNameFromUrl('https://gh-proxy.com/https://github.com/o/r/releases/download/v1/y.zip')).toBe('y.zip')
    expect(assetNameFromUrl('not a url')).toBe('')
  })
})

describe('httpGetBuffer TLS options', () => {
  afterEach(() => {
    downloadOptsSpy.mockReset()
    download.impl = () => Buffer.from('ok')
  })

  it('disables chain verification only for a pinned request', async () => {
    // The server's own certificate may be self-signed: the window was told to
    // trust it, but Node's TLS stack would still reject /api/desktop/latest,
    // making the upgrade check read as "nothing to offer". The pin (checked in
    // httpGet.test.ts) is what keeps that from accepting ANY certificate.
    await httpGetBuffer('https://192.168.1.50:20000/api/desktop/latest', { pinnedFingerprint: 'sha256/AAAA' })
    expect(downloadOptsSpy).toHaveBeenLastCalledWith(
      'https://192.168.1.50:20000/api/desktop/latest',
      { rejectUnauthorized: false },
    )

    await httpGetBuffer('https://github.com/x.zip')
    expect(downloadOptsSpy).toHaveBeenLastCalledWith('https://github.com/x.zip', {})
  })

  it('keeps the pin across a redirect within the same origin', async () => {
    // A self-signed server that redirects (http → https, or a host alias) must
    // not lose the pin on the hop, or the follow-up request fails exactly
    // where the first one succeeded.
    redirect.to = 'https://192.168.1.50:20000/login'
    download.impl = () => Buffer.from('ok')
    await httpGetBuffer('https://192.168.1.50:20000/api/desktop/latest', { pinnedFingerprint: 'sha256/AAAA' })
    expect(downloadOptsSpy).toHaveBeenLastCalledWith('https://192.168.1.50:20000/login', { rejectUnauthorized: false })
  })
})

describe('download transport', () => {
  beforeEach(() => downloadSpy.mockReset())

  afterEach(() => {
    downloadSpy.mockReset()
    download.impl = () => Buffer.alloc(0)
  })

  it('refuses a plaintext candidate instead of installing its body', async () => {
    // Everything installed here is executable content; a http mirror URL can be
    // rewritten by anyone on the path and the archive carries no signature.
    download.impl = () => makeZip([{ name: 'clawbench-desktop', content: 'BIN' }])
    await expect(downloadFirstAvailable(['http://mirror.example/x.zip'], {})).rejects.toThrow(
      /refused \(not https\)/,
    )
    // It never even reached the network.
    expect(downloadSpy).not.toHaveBeenCalled()
  })

  it('still allows loopback, where the transport is this machine', async () => {
    download.impl = () => makeZip([{ name: 'clawbench-desktop', content: 'BIN' }])
    const buf = await downloadFirstAvailable(['http://127.0.0.1:4873/x.zip'], {})
    expect(buf.length).toBeGreaterThan(0)
  })

  it('falls through a plaintext candidate to an https one', async () => {
    download.impl = () => makeZip([{ name: 'clawbench-desktop', content: 'BIN' }])
    const buf = await downloadFirstAvailable(['http://mirror.example/x.zip', 'https://github.com/x.zip'], {})
    expect(buf.length).toBeGreaterThan(0)
    expect(downloadSpy).toHaveBeenCalledTimes(1)
    expect(downloadSpy).toHaveBeenCalledWith('https://github.com/x.zip')
  })

  it('classifies schemes by whether the transport is protected', () => {
    expect(isSecureDownloadUrl('https://github.com/x.zip')).toBe(true)
    expect(isSecureDownloadUrl('http://localhost:4873/x.zip')).toBe(true)
    expect(isSecureDownloadUrl('http://[::1]:4873/x.zip')).toBe(true)
    expect(isSecureDownloadUrl('http://mirror.example/x.zip')).toBe(false)
    expect(isSecureDownloadUrl('file:///etc/passwd')).toBe(false)
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

  it('treats a v-prefixed pointer and a bare running version as the same', () => {
    // Regression guard. The pointer holds the server's `v1.2.3` (git describe)
    // while app.getVersion() reports `1.2.3` (CI strips the prefix). A raw
    // comparison called these different, which routed the handoff into the
    // self-reference guard — and that guard DELETES the pointer, silently
    // reverting the upgrade on the next cold start.
    expect(selectStartupVersion('v1.2.3', '1.2.3')).toBe('')
    expect(selectStartupVersion('1.2.3', 'v1.2.3')).toBe('')
  })

  it('still returns the RAW pointed string when it does differ', () => {
    // The returned value feeds versionDir(), which must resolve the directory
    // the pointer actually names — normalizing the return would break it.
    expect(selectStartupVersion('v1.2.3', '1.0.0')).toBe('v1.2.3')
  })
})

describe('handOffToPointedVersion', () => {
  let homedirSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    homedirSpy = vi.spyOn(os, 'homedir').mockReturnValue(tmpRoot)
    spawnMock.mockReset()
    spawnMock.mockImplementation(() => makeSpawnedChild())
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

  it('starts the pointed version and reports the handoff', async () => {
    installVersion('1.2.3')
    writePointer('1.2.3')

    await expect(handOffToPointedVersion('1.0.0')).resolves.toBe(true)
    expect(spawnMock).toHaveBeenCalledTimes(1)
    expect(String(spawnMock.mock.calls[0]?.[0])).toContain('1.2.3')
  })

  it('does not hand off when the pointer names the running version', async () => {
    installVersion('1.2.3')
    writePointer('1.2.3')

    await expect(handOffToPointedVersion('1.2.3')).resolves.toBe(false)
    expect(spawnMock).not.toHaveBeenCalled()
  })

  it('does not hand off when there is no pointer', async () => {
    await expect(handOffToPointedVersion('1.0.0')).resolves.toBe(false)
    expect(spawnMock).not.toHaveBeenCalled()
  })

  it('clears a pointer whose version directory is gone, instead of refusing to start', async () => {
    // A deleted or half-written upgrade must not brick the app: the pointer is
    // dropped and the current version keeps running.
    writePointer('9.9.9') // never installed

    await expect(handOffToPointedVersion('1.0.0')).resolves.toBe(false)
    expect(spawnMock).not.toHaveBeenCalled()
    expect(readCurrentVersion()).toBe('')
  })

  it('clears a pointer whose version directory exists but has no executable', async () => {
    fs.mkdirSync(versionDir('9.9.9'), { recursive: true })
    writePointer('9.9.9')

    await expect(handOffToPointedVersion('1.0.0')).resolves.toBe(false)
    expect(spawnMock).not.toHaveBeenCalled()
    expect(readCurrentVersion()).toBe('')
  })

  it('does not re-exec when the pointed binary IS the running process, and keeps the pointer', async () => {
    // The pointer naming the file we are already running means this process is
    // the pointed install. Two ways to get here:
    //
    //   - someone unpacked a release on top of the version store (the old
    //     reason for dropping the pointer);
    //   - a FUSED upgrade installed an all-in-one package whose shell was
    //     reused from a previous release: the directory is named after the
    //     release tag, while the app inside reports the older build's version,
    //     so `selectStartupVersion` sees two different versions for the same
    //     install.
    //
    // Dropping the pointer in the second case sends the next cold start back
    // to the original install directory — a silent revert of the upgrade, and
    // of the server binary that came with it. Keeping it is harmless in the
    // first case (it just points at where we already are).
    const bin = installVersion('1.2.3')
    writePointer('1.2.3')
    const execSpy = vi.spyOn(process, 'execPath', 'get').mockReturnValue(bin)

    try {
      await expect(handOffToPointedVersion('1.0.0')).resolves.toBe(false)
      expect(spawnMock).not.toHaveBeenCalled()
      expect(readCurrentVersion()).toBe('1.2.3')
    } finally {
      execSpy.mockRestore()
    }
  })

  it('keeps running the current version when the pointed binary cannot start', async () => {
    // The binary exists but the OS refuses it (antivirus quarantine, a missing
    // runtime). Exiting here would close the app and open nothing — and the
    // pointer would make every later launch repeat it. The handoff must fail
    // recoverably: drop the pointer, keep the current process alive.
    installVersion('1.2.3')
    writePointer('1.2.3')
    spawnMock.mockImplementationOnce(() => makeSpawnedChild(new Error('spawn EACCES')))

    await expect(handOffToPointedVersion('1.0.0')).resolves.toBe(false)
    expect(readCurrentVersion()).toBe('')
  })
})

describe('restartInto', () => {
  let homedirSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    homedirSpy = vi.spyOn(os, 'homedir').mockReturnValue(tmpRoot)
    spawnMock.mockReset()
    spawnMock.mockImplementation(() => makeSpawnedChild())
    vi.mocked(app.exit).mockClear()
  })

  afterEach(() => {
    homedirSpy.mockRestore()
  })

  function installVersion(version: string): string {
    const dir = versionDir(version)
    fs.mkdirSync(dir, { recursive: true })
    const bin = path.join(dir, process.platform === 'win32' ? 'clawbench-desktop.exe' : 'clawbench-desktop')
    fs.writeFileSync(bin, 'x')
    return bin
  }

  it('starts the new version and only then quits', async () => {
    installVersion('1.2.3')
    writePointer('1.2.3')

    await restartInto('1.2.3')

    expect(spawnMock).toHaveBeenCalledTimes(1)
    expect(app.exit).toHaveBeenCalledWith(0)
  })

  it('reports a binary that cannot start, clears the pointer and does not quit', async () => {
    // Quitting after a failed spawn leaves the user with no window at all, and
    // the pointer would send the next launch down the same dead path.
    installVersion('1.2.3')
    writePointer('1.2.3')
    spawnMock.mockImplementationOnce(() => makeSpawnedChild(new Error('spawn EACCES')))

    await expect(restartInto('1.2.3')).rejects.toThrow('EACCES')
    expect(readCurrentVersion()).toBe('')
    expect(app.exit).not.toHaveBeenCalled()
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

describe('extractTarGz', () => {
  it('extracts files and strips the npm package/ wrapper', () => {
    const tar = makeTarGz([
      { name: 'package/', type: '5' },
      { name: 'package/resources/app.asar', content: 'ASAR' },
      { name: 'package/payload.json', content: '{"electron":"44.4.3"}' },
    ])

    extractTarGz(tar, tmpRoot)

    expect(fs.readFileSync(path.join(tmpRoot, 'resources/app.asar'), 'utf8')).toBe('ASAR')
    expect(fs.readFileSync(path.join(tmpRoot, 'payload.json'), 'utf8')).toBe('{"electron":"44.4.3"}')
    expect(fs.existsSync(path.join(tmpRoot, 'package'))).toBe(false)
  })

  it('joins the ustar prefix and name fields', () => {
    // npm splits a long path across the 100-byte `name` and 155-byte `prefix`
    // fields instead of emitting a PAX header, so prefix handling is the common
    // case, not an edge case. Verified against a real npm pack of the payload.
    const tar = makeTarGz([
      {
        name: 'cpu_features/patches/0001.patch',
        prefix: 'package/resources/app.asar.unpacked/node_modules/cpu-features/deps',
        content: 'PATCH',
      },
    ])

    extractTarGz(tar, tmpRoot)

    expect(
      fs.readFileSync(
        path.join(tmpRoot, 'resources/app.asar.unpacked/node_modules/cpu-features/deps/cpu_features/patches/0001.patch'),
        'utf8',
      ),
    ).toBe('PATCH')
  })

  it('uses the name as-is when the prefix field is empty', () => {
    const tar = makeTarGz([{ name: 'package/a.txt', content: 'A' }])
    extractTarGz(tar, tmpRoot)
    expect(fs.readFileSync(path.join(tmpRoot, 'a.txt'), 'utf8')).toBe('A')
  })

  it('applies a PAX path override to the following entry', () => {
    // npm falls back to PAX when a path cannot be split into name+prefix.
    const pax = makePaxBlock({ path: 'package/resources/deep.txt' })
    const tar = gzipSync(
      Buffer.concat([
        pax,
        (() => {
          const inner = makeTarGz([{ name: 'placeholder', content: 'PAXED' }])
          return gunzipSync(inner)
        })(),
      ]),
    )

    extractTarGz(tar, tmpRoot)

    expect(fs.readFileSync(path.join(tmpRoot, 'resources/deep.txt'), 'utf8')).toBe('PAXED')
    expect(fs.existsSync(path.join(tmpRoot, 'placeholder'))).toBe(false)
  })

  it('restores the executable bit', () => {
    // Load-bearing on Linux, and the tar header stores the mode directly
    // (unlike zip, which buries it in the external attributes).
    const tar = makeTarGz([{ name: 'package/bin/tool', content: 'BIN', mode: 0o755 }])
    extractTarGz(tar, tmpRoot)
    expect(fs.statSync(path.join(tmpRoot, 'bin/tool')).mode & 0o111).toBeTruthy()
  })

  it('rejects an entry that escapes the destination', () => {
    const tar = makeTarGz([{ name: 'package/../../evil.txt', content: 'pwned' }])
    expect(() => extractTarGz(tar, tmpRoot)).toThrow(/escapes destination/)
    expect(fs.existsSync(path.join(tmpRoot, '..', '..', 'evil.txt'))).toBe(false)
  })

  it('rejects an unsupported entry type instead of silently skipping it', () => {
    // A symlink or device in the payload means the layout changed; skipping it
    // would install a quietly incomplete tree.
    const tar = makeTarGz([{ name: 'package/link', type: '2', content: '' }])
    expect(() => extractTarGz(tar, tmpRoot)).toThrow(/unsupported entry type/)
  })

  it('rejects a truncated archive rather than installing what it can', () => {
    // The dangerous shape: payload.json intact but resources/ cut off. Without
    // a bounds check the manifest check downstream would pass and the install
    // would be silently incomplete.
    const full = makeTarGz([
      { name: 'package/payload.json', content: '{"electron":"44.4.3"}' },
      { name: 'package/resources/app.asar', content: 'X'.repeat(4096) },
    ])
    const raw = gunzipSync(full)
    const truncated = gzipSync(raw.subarray(0, raw.length - 2048))

    expect(() => extractTarGz(truncated, tmpRoot)).toThrow(/truncated archive/)
  })

  it('rejects a malformed size field', () => {
    const tar = makeTarGz([{ name: 'package/a.txt', content: 'A' }])
    const raw = gunzipSync(tar)
    // Corrupt the octal size field of the first header.
    raw.write('XXXXXXXXXXX\0', 124, 12, 'utf8')
    expect(() => extractTarGz(gzipSync(raw), tmpRoot)).toThrow(/malformed size field/)
  })

  it('rejects a body that is neither gzip nor zip', () => {
    expect(() => extractArchive(Buffer.from('<html>rate limited</html>'), tmpRoot)).toThrow(
      /unrecognized archive format/,
    )
  })
})

describe('looksLikeArchive', () => {
  it('recognizes gzip and zip magic bytes', () => {
    expect(looksLikeArchive(makeTarGz([{ name: 'a', content: 'x' }]))).toBe(true)
    expect(looksLikeArchive(makeZip([{ name: 'a', content: 'x' }]))).toBe(true)
  })

  it('rejects an HTML error page served with HTTP 200', () => {
    // The case this guard exists for: a mirror answering a miss or a rate limit
    // with a 200 HTML body. Accepting it would fail extraction later and, for a
    // payload, fall back to the ~150MB full download instead of trying the next
    // candidate.
    expect(looksLikeArchive(Buffer.from('<!DOCTYPE html><html>...'))).toBe(false)
    expect(looksLikeArchive(Buffer.alloc(0))).toBe(false)
    expect(looksLikeArchive(Buffer.from([0x1f]))).toBe(false)
  })
})

describe('zip and tar.gz produce identical installs', () => {
  it('yields the same tree from the same content, including dotfiles', () => {
    // Which candidate URL won (a registry mirror or the GitHub release) must not
    // change what gets installed. The two archives differ in wrapper directory
    // and in an npm-only root package.json; both are normalized away.
    const files = [
      { name: 'resources/app.asar', content: 'ASAR' },
      { name: 'resources/login.html', content: 'LOGIN' },
      // A dotfile: the case a glob-based copy silently drops.
      { name: 'resources/app.asar.unpacked/node_modules/cpu-features/.eslintrc.js', content: 'ESLINT' },
      { name: 'payload.json', content: '{"electron":"44.4.3"}' },
    ]

    const zipDir = path.join(tmpRoot, 'from-zip')
    const tarDir = path.join(tmpRoot, 'from-tar')
    fs.mkdirSync(zipDir)
    fs.mkdirSync(tarDir)

    extractArchive(makeZip(files), zipDir)
    extractArchive(
      makeTarGz([
        // npm wraps everything in package/ and adds its own root package.json.
        { name: 'package/package.json', content: '{"name":"x","version":"1.0.0"}' },
        ...files.map((f) => ({ name: 'package/' + f.name, content: f.content })),
      ]),
      tarDir,
    )

    // Remove the npm-only wrapper manifest, as the npm packer does, and
    // then the two trees must be byte-identical.
    fs.rmSync(path.join(tarDir, 'package.json'), { force: true })

    expect(readTree(tarDir)).toEqual(readTree(zipDir))
  })
})

describe('downloadFirstAvailable skips non-archive responses', () => {
  beforeEach(() => {
    downloadSpy.mockReset()
    redirect.to = ''
  })

  it('falls through to the next candidate when one serves HTML', async () => {
    const good = makeZip([{ name: 'a.txt', content: 'OK' }])
    download.impl = (url) => (url.includes('mirror') ? Buffer.from('<html>nope</html>') : good)

    const buf = await downloadFirstAvailable(['https://mirror/x.zip', 'https://github.com/x.zip'])

    expect(buf.equals(good)).toBe(true)
    expect(downloadSpy).toHaveBeenCalledTimes(2)
  })

  it('reports every failed candidate when none is an archive', async () => {
    download.impl = () => Buffer.from('<html>nope</html>')
    await expect(
      downloadFirstAvailable(['https://mirror/a.zip', 'https://mirror/b.zip']),
    ).rejects.toThrow(/all download sources failed/)
    await expect(
      downloadFirstAvailable(['https://mirror/a.zip', 'https://mirror/b.zip']),
    ).rejects.toThrow(/not a zip or tar\.gz archive/)
  })
})

/**
 * Electron's fs shim resolves any path ending in `.asar` INSIDE the archive, so
 * the installer cannot write the one file it exists to install. Reported in
 * #579 as `Invalid package <staging>\resources\app.asar`, on both the payload
 * and the full-download path, which made self-upgrade impossible on every
 * platform.
 *
 * These tests run under plain Node, where `process.noAsar` is inert and the
 * shim does not exist — the installer would succeed even without the fix. The
 * shim above is therefore what makes them meaningful: it reproduces the
 * measured behaviours (write/copy rejected, `existsSync` false) and records the
 * flag each call saw.
 */
describe('asar interception', () => {
  let shim: AsarShim
  let homedirSpy: ReturnType<typeof vi.spyOn>
  let savedElectron: string | undefined
  const versions = process.versions as { electron?: string }

  beforeEach(() => {
    homedirSpy = vi.spyOn(os, 'homedir').mockReturnValue(tmpRoot)
    savedElectron = versions.electron
    versions.electron = '44.4.3'
    downloadSpy.mockReset()
    download.impl = () => Buffer.alloc(0)
    setNoAsar(undefined)
    shim = installAsarShim()
  })

  afterEach(() => {
    shim.restore()
    setNoAsar(undefined)
    homedirSpy.mockRestore()
    downloadSpy.mockReset()
    if (savedElectron === undefined) delete versions.electron
    else versions.electron = savedElectron
  })

  it('writes app.asar on the full-download path', async () => {
    // The exact failure from the report: extraction dies while writing
    // resources/app.asar, so the version never lands.
    download.impl = () =>
      makeZip([
        { name: 'clawbench-desktop', content: 'BIN' },
        { name: 'resources/app.asar', content: 'ASAR' },
      ])

    const dest = await downloadAndInstall(['https://github.com/x.zip'], '2.0.0', {})

    expect(fs.readFileSync(path.join(dest, 'resources/app.asar'), 'utf8')).toBe('ASAR')
    expect(readCurrentVersion()).toBe('2.0.0')
  })

  it('re-enables interception afterwards, so the running app can read its archive', async () => {
    // Scoping is the whole point: leaving noAsar set would make the RUNNING
    // app unable to resolve anything inside its own app.asar. Asserted on the
    // calls the installer made — a plain "flag is false afterwards" check
    // would also pass if it had never been set at all.
    download.impl = () =>
      makeZip([
        { name: 'clawbench-desktop', content: 'BIN' },
        { name: 'resources/app.asar', content: 'ASAR' },
      ])

    await downloadAndInstall(['https://github.com/x.zip'], '2.0.0', {})

    const writes = shim.observed.filter((o) => o.op === 'write' && o.path.endsWith('app.asar'))
    expect(writes.length).toBeGreaterThan(0)
    // Every write of the archive itself saw interception off...
    expect(writes.every((o) => o.noAsar)).toBe(true)
    // ...and the flag is back to its original value once the install returns.
    expect(noAsar()).toBe(false)
  })

  it('keeps interception ON across the download, while the app is still running', async () => {
    // The boundary the whole design turns on. Holding noAsar across the
    // `await` would also "work" — the install completes and the flag ends up
    // restored — but for the entire download the running app would be unable
    // to read anything inside its own archive. Asserted from inside the
    // download callback, which is exactly when that window is open.
    let duringDownload: boolean | undefined
    download.impl = () => {
      duringDownload = noAsar()
      return makeZip([
        { name: 'clawbench-desktop', content: 'BIN' },
        { name: 'resources/app.asar', content: 'ASAR' },
      ])
    }

    await downloadAndInstall(['https://github.com/x.zip'], '2.0.0', {})

    expect(duringDownload).toBe(false)
  })

  it('restores a caller that had interception off before the install', async () => {
    // `--no-asar` starts the app with the flag already set. The helper must
    // restore the PREVIOUS value rather than clearing it, or the installer
    // would silently change how the rest of the app resolves its archive.
    setNoAsar(true)
    download.impl = () =>
      makeZip([
        { name: 'clawbench-desktop', content: 'BIN' },
        { name: 'resources/app.asar', content: 'ASAR' },
      ])

    await downloadAndInstall(['https://github.com/x.zip'], '2.0.0', {})

    expect(noAsar()).toBe(true)
  })
})
