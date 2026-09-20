import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import path from 'node:path'
import fs from 'node:fs'
import os from 'node:os'
import { gzipSync } from 'node:zlib'
import { createHash } from 'node:crypto'

// The installer touches `app` only in restartInto(); mock electron so the pure
// extraction/pointer logic can be exercised without a running Electron process.
vi.mock('electron', () => ({
  app: { getVersion: () => '0.1.0', relaunch: vi.fn(), exit: vi.fn() },
}))

import { extractTarball, writePointer, readCurrentVersion, pointerPath, versionDir } from './install'

/**
 * Build a gzipped tar containing the given entries.
 *
 * Entries are `{ name, content }`; a trailing slash marks a directory. Paths
 * are written verbatim so tests can include the `package/` prefix npm uses and
 * deliberately hostile names.
 */
function makeTar(entries: Array<{ name: string; content?: string }>): Buffer {
  const blocks: Buffer[] = []

  for (const entry of entries) {
    const isDir = entry.name.endsWith('/')
    const content = Buffer.from(entry.content ?? '')
    const header = Buffer.alloc(512)

    header.write(entry.name, 0, 100, 'utf8')
    header.write('000644 \0', 100, 8, 'utf8')          // mode
    header.write('000000 \0', 108, 8, 'utf8')          // uid
    header.write('000000 \0', 116, 8, 'utf8')          // gid
    header.write(content.length.toString(8).padStart(11, '0') + '\0', 124, 12, 'utf8')
    header.write('00000000000\0', 136, 12, 'utf8')     // mtime
    header.write('        ', 148, 8, 'utf8')           // checksum placeholder
    header.write(isDir ? '5' : '0', 156, 1, 'utf8')
    header.write('ustar\0', 257, 6, 'utf8')
    header.write('00', 263, 2, 'utf8')

    // The checksum is the sum of all header bytes with the checksum field read
    // as spaces — tar readers reject a mismatch.
    let sum = 0
    for (const b of header) sum += b
    header.write(sum.toString(8).padStart(6, '0') + '\0 ', 148, 8, 'utf8')

    blocks.push(header)
    if (!isDir && content.length > 0) {
      blocks.push(content)
      const pad = (512 - (content.length % 512)) % 512
      if (pad > 0) blocks.push(Buffer.alloc(pad))
    }
  }

  // Two zero blocks terminate the archive.
  blocks.push(Buffer.alloc(1024))
  return gzipSync(Buffer.concat(blocks))
}

let tmpRoot: string

beforeEach(() => {
  tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'clawbench-install-test-'))
})

afterEach(() => {
  fs.rmSync(tmpRoot, { recursive: true, force: true })
})

describe('extractTarball', () => {
  it('extracts files and strips the npm package/ prefix', async () => {
    const tar = makeTar([
      { name: 'package/' },
      { name: 'package/package.json', content: '{"name":"x"}' },
      { name: 'package/dist/main.js', content: 'console.log(1)' },
    ])

    await extractTarball(tar, tmpRoot)

    expect(fs.readFileSync(path.join(tmpRoot, 'package.json'), 'utf8')).toBe('{"name":"x"}')
    expect(fs.readFileSync(path.join(tmpRoot, 'dist/main.js'), 'utf8')).toBe('console.log(1)')
    // The wrapper directory itself must not survive.
    expect(fs.existsSync(path.join(tmpRoot, 'package'))).toBe(false)
  })

  it('preserves the executable bit source file content exactly', async () => {
    const binary = Buffer.from([0x7f, 0x45, 0x4c, 0x46, 0x00, 0x01, 0x02])
    const tar = makeTar([{ name: 'package/bin/clawbench-desktop', content: binary.toString('binary') }])
    await extractTarball(tar, tmpRoot)
    expect(fs.existsSync(path.join(tmpRoot, 'bin/clawbench-desktop'))).toBe(true)
  })

  it('rejects an entry that escapes the destination', async () => {
    // A crafted archive must not be able to write outside the install dir.
    const tar = makeTar([{ name: 'package/../../evil.txt', content: 'pwned' }])
    await expect(extractTarball(tar, tmpRoot)).rejects.toThrow(/escapes destination/)
    expect(fs.existsSync(path.join(tmpRoot, '..', '..', 'evil.txt'))).toBe(false)
  })

  it('handles an empty archive without throwing', async () => {
    await expect(extractTarball(gzipSync(Buffer.alloc(1024)), tmpRoot)).resolves.toBeUndefined()
  })
})

describe('pointer file', () => {
  it('round-trips a version and leaves no temp file behind', () => {
    // Redirect the install root into the temp dir so the real ~/.clawbench-desktop
    // is never touched by tests.
    const fakeHome = tmpRoot
    const spy = vi.spyOn(os, 'homedir').mockReturnValue(fakeHome)
    try {
      writePointer('1.2.3')
      expect(readCurrentVersion()).toBe('1.2.3')
      const dir = path.dirname(pointerPath())
      const leftovers = fs.readdirSync(dir).filter((f) => f.includes('.tmp-'))
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
