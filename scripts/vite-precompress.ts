import { gzipSync } from 'node:zlib'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import type { Plugin } from 'vite'

/**
 * Emit a gzip copy next to every compressible build output.
 *
 * The Go server serves `<file>.gz` when the client sends
 * `Accept-Encoding: gzip` (internal/frontend/compress.go). Doing the work at
 * build time instead of per request keeps the serving path allocation-free and
 * lets the same bytes be embedded in the single-binary build.
 *
 * Measured on the current bundle: 4.64 MB of first-load JS+CSS becomes 1.22 MB
 * (26%), which is the difference between ~2 s and ~0.5 s of transfer on a
 * mobile connection.
 */

/** Extensions that shrink under gzip; mirrors compressibleExts on the Go side. */
export const COMPRESSIBLE_RE = /\.(?:js|mjs|css|html|svg|json|txt)$/i

/**
 * Below this size the gzip header/footer can eat the entire saving, and the
 * bundle contains hundreds of tiny highlight.js grammar chunks.
 */
export const MIN_COMPRESS_BYTES = 1024

/**
 * Write `<file>.gz` when compressing actually helps. Returns whether a sibling
 * was written.
 */
export function compressIfWorthwhile(file: string): boolean {
  const raw = readFileSync(file)
  if (raw.length < MIN_COMPRESS_BYTES) return false

  const gz = gzipSync(raw, { level: 9 })
  if (gz.length >= raw.length) return false

  writeFileSync(`${file}.gz`, gz)
  return true
}

export function precompress(): Plugin {
  return {
    name: 'clawbench-precompress',
    apply: 'build',
    writeBundle(options, bundle) {
      const dir = options.dir
      if (!dir) return
      for (const output of Object.values(bundle)) {
        if (!COMPRESSIBLE_RE.test(output.fileName)) continue
        compressIfWorthwhile(resolve(dir, output.fileName))
      }
    },
  }
}
