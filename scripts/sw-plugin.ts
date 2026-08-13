import { createHash } from 'crypto'
import { readFileSync, existsSync } from 'fs'
import { resolve } from 'path'
import type { Plugin } from 'vite'

// Static files guaranteed present (index.html lives in the bundle; icons come
// from the vite publicDir `assets/`; manifest.json lives in `web/`).
export const STATIC_PRECACHE_CANDIDATES = [
  '/index.html',
  '/manifest.json',
  '/favicon.png',
  '/logo-180.png',
  '/logo-512.png',
  '/logo.png',
]

// Content-derived cache version: changes whenever the output file set changes,
// which triggers the SW to swap to a new cache name and purge old ones.
export function computeVersion(fileNames: string[]): string {
  return createHash('sha1')
    .update(fileNames.sort().join('|'))
    .digest('hex')
    .slice(0, 10)
}

// Build the precache list from the bundle's JS/CSS plus static files that pass
// the `exists` check. Only existing files are included so `addAll` never fails
// on a 404 (which would reject the whole install).
export function buildPrecacheList(
  outputFiles: string[],
  exists: (path: string) => boolean,
): string[] {
  const jsCss = outputFiles
    .filter((f) => f !== 'sw.js' && /\.(js|css)$/.test(f) && exists(f))
    .map((f) => '/' + f)
  const statics = STATIC_PRECACHE_CANDIDATES.filter((p) => exists(p.slice(1)))
  return ['/', ...jsCss, ...statics]
}

// Render the SW template with the injected version and precache array.
export function renderSw(template: string, version: string, precache: string[]): string {
  return template
    .replace('__VERSION__', version)
    .replace('__PRECACHE__', JSON.stringify(precache))
}

export function serviceWorkerPlugin(): Plugin {
  return {
    name: 'clawbench-service-worker',
    apply: 'build',
    enforce: 'post',
    generateBundle(_options, bundle) {
      const outFiles = Object.keys(bundle)
      const exists = (p: string): boolean =>
        bundle[p] !== undefined ||
        existsSync(resolve('assets', p)) ||
        existsSync(resolve('web', p))

      const version = computeVersion(outFiles)
      const precache = buildPrecacheList(outFiles, exists)
      const template = readFileSync(resolve('web/sw-template.js'), 'utf8')
      const sw = renderSw(template, version, precache)

      this.emitFile({ type: 'asset', fileName: 'sw.js', source: sw })

      // Ensure /manifest.json is served (PWA install metadata). It is referenced
      // by index.html but missing from the build output (a 404 today).
      if (bundle['manifest.json'] === undefined) {
        const manifest = readFileSync(resolve('web/manifest.json'), 'utf8')
        this.emitFile({ type: 'asset', fileName: 'manifest.json', source: manifest })
      }
    },
  }
}
