import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { gunzipSync } from 'node:zlib'
import { randomBytes } from 'node:crypto'
import { COMPRESSIBLE_RE, MIN_COMPRESS_BYTES, compressIfWorthwhile, precompress } from './vite-precompress'

/** Content large enough to beat MIN_COMPRESS_BYTES and to actually compress. */
const BIG_JS = `export const payload = ${JSON.stringify('x'.repeat(8000))}\n`

let dir: string

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'cb-precompress-'))
})

afterEach(() => {
  rmSync(dir, { recursive: true, force: true })
})

function write(name: string, content: string | Buffer): string {
  const file = join(dir, name)
  mkdirSync(join(file, '..'), { recursive: true })
  writeFileSync(file, content)
  return file
}

describe('compressIfWorthwhile', () => {
  it('writes a sibling whose contents round-trip through gunzip', () => {
    const file = write('main-abc12345.js', BIG_JS)

    expect(compressIfWorthwhile(file)).toBe(true)

    const gz = readFileSync(`${file}.gz`)
    expect(gz.length).toBeLessThan(Buffer.byteLength(BIG_JS))
    expect(gunzipSync(gz).toString()).toBe(BIG_JS)
  })

  it('skips files below the size floor (hundreds of tiny grammar chunks)', () => {
    const file = write('abnf-abc12345.js', 'export default 1\n')
    expect(Buffer.byteLength('export default 1\n')).toBeLessThan(MIN_COMPRESS_BYTES)

    expect(compressIfWorthwhile(file)).toBe(false)
    expect(existsSync(`${file}.gz`)).toBe(false)
  })

  it('skips when the encoded copy would not be smaller', () => {
    // Random bytes are incompressible; gzip's header makes the result larger.
    const file = write('random.js', randomBytes(4096))

    expect(compressIfWorthwhile(file)).toBe(false)
    expect(existsSync(`${file}.gz`)).toBe(false)
  })
})

describe('precompress plugin', () => {
  function runBundle(bundle: Record<string, { fileName: string }>): void {
    const plugin = precompress()
    const hook = plugin.writeBundle as unknown as (options: { dir: string }, bundle: unknown) => void
    hook({ dir }, bundle)
  }

  it('compresses bundle outputs and leaves already-compressed types alone', () => {
    write('main-abc12345.js', BIG_JS)
    write('logo-abc12345.png', 'x'.repeat(8000))

    runBundle({
      'main-abc12345.js': { fileName: 'main-abc12345.js' },
      'logo-abc12345.png': { fileName: 'logo-abc12345.png' },
    })

    expect(existsSync(join(dir, 'main-abc12345.js.gz'))).toBe(true)
    expect(existsSync(join(dir, 'logo-abc12345.png.gz'))).toBe(false)
  })

  it('covers the file types the Go server will look for', () => {
    for (const name of ['a.js', 'a.mjs', 'a.css', 'a.html', 'a.svg', 'a.json', 'a.txt']) {
      expect(COMPRESSIBLE_RE.test(name)).toBe(true)
    }
    for (const name of ['a.png', 'a.woff2', 'a.gz', 'a.zip', 'a.wasm']) {
      expect(COMPRESSIBLE_RE.test(name)).toBe(false)
    }
  })

  it('is inert when the build has no output directory', () => {
    const plugin = precompress()
    const hook = plugin.writeBundle as unknown as (options: { dir?: string }, bundle: unknown) => void
    expect(() => hook({}, {})).not.toThrow()
  })
})
