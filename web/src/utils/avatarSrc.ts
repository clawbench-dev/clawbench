/**
 * Turn a stored avatar value into an `<img src>`.
 *
 * Stored avatars come in two shapes:
 *   - a raw SVG string (DiceBear-generated, or a user's custom avatar);
 *   - a ready data URI (an uploaded raster image).
 *
 * Only the raw SVG needs encoding. A data URI must be passed through untouched
 * — wrapping it in `svgToDataUri` again would produce a broken nested URI.
 */
import { svgToDataUri } from '@/utils/svgDataUri'

export function avatarToSrc(avatar: string): string {
  if (!avatar) return ''
  return avatar.startsWith('data:image/') ? avatar : svgToDataUri(avatar)
}
