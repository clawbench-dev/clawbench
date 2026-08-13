import { createHash } from 'crypto'

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
