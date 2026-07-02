import { baseName } from '@/utils/path.ts'

/**
 * Extract a display name from an image src URL.
 * Strips /api/local-file/ prefix and returns the basename.
 */
export function extractImageName(src: string): string {
  try {
    const url = new URL(src, window.location.origin)
    const path = decodeURIComponent(url.pathname)
    const localPrefix = '/api/local-file/'
    if (path.startsWith(localPrefix)) {
      return baseName(path.slice(localPrefix.length))
    }
    return baseName(path)
  } catch {
    return ''
  }
}
