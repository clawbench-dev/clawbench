import { baseName } from '@/utils/path.ts'

/**
 * Extract a display name from an image src URL.
 *
 * Handles every shape the render pipelines emit:
 *   - `/api/fs/raw/<rel>?t=…`            → basename of the path
 *   - `/api/fs/raw/?target=<abs>`        → basename of the target
 *   - `/api/fs/thumb?target=<rel>&w=…`   → basename of the target
 *   - anything else                      → basename of the URL path
 *
 * The query forms matter: their own pathname has no useful last segment (the
 * `?target=` form ends in `/api/fs/raw/`, the thumbnail endpoint is literally
 * `thumb`), so reading the pathname alone yields an empty or extension-less
 * name — and a download then lands as a file with no extension.
 */
export function extractImageName(src: string): string {
  try {
    const url = new URL(src, window.location.origin)
    // `searchParams` decodes already — decoding again would turn `%25` into an
    // error and lose the whole name.
    const target = url.searchParams.get('target') || url.searchParams.get('path')
    if (target) return baseName(target)
    const path = decodeURIComponent(url.pathname)
    const localPrefix = '/api/fs/raw/'
    if (path.startsWith(localPrefix)) {
      return baseName(path.slice(localPrefix.length))
    }
    return baseName(path)
  } catch {
    return ''
  }
}
