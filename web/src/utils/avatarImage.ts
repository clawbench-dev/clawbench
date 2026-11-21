/**
 * Turn a user-selected image file into a small, square, self-contained avatar.
 *
 * The output is a data URI stored in the same `agents.avatar` text column as a
 * generated SVG. That keeps the whole existing pipeline unchanged: the server
 * validates the string, every surface renders it through a sandboxed `<img>`,
 * and the public share payload can carry it to an anonymous viewer.
 *
 * The original file never leaves the browser. Resizing here is what keeps a
 * multi-megabyte phone photo from bloating the agents list, the group-member
 * snapshots and every share payload.
 */

export type AvatarImageErrorCode = 'unsupported' | 'too-large' | 'decode-failed'

export class AvatarImageError extends Error {
  readonly code: AvatarImageErrorCode

  constructor(code: AvatarImageErrorCode) {
    super(code)
    this.name = 'AvatarImageError'
    this.code = code
  }
}

/** Files larger than this are rejected before any decoding work. */
export const AVATAR_UPLOAD_MAX_BYTES = 10 * 1024 * 1024

/** Output edge length. The largest avatar surface is 56 CSS px; 192 keeps it
 *  crisp on high-density screens without making the stored string large. */
export const AVATAR_UPLOAD_SIZE = 192

const ACCEPTED_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp'])

/** Centre-crop rectangle for a source image. Exported for tests. */
export function centerCropRect(width: number, height: number): { sx: number; sy: number; side: number } {
  const side = Math.min(width, height)
  return {
    sx: Math.floor((width - side) / 2),
    sy: Math.floor((height - side) / 2),
    side,
  }
}

function loadWithImageElement(file: File): Promise<{ image: HTMLImageElement; cleanup: () => void }> {
  const url = URL.createObjectURL(file)
  const image = new Image()
  return new Promise((resolve, reject) => {
    image.onload = () => resolve({ image, cleanup: () => URL.revokeObjectURL(url) })
    image.onerror = () => {
      URL.revokeObjectURL(url)
      reject(new AvatarImageError('decode-failed'))
    }
    image.src = url
  })
}

async function decodeFile(file: File): Promise<{ source: CanvasImageSource; width: number; height: number; cleanup: () => void }> {
  // `imageOrientation: 'from-image'` applies EXIF rotation, so a phone photo is
  // not cropped sideways. createImageBitmap is available in every browser the
  // app targets; the <img> path is a safety net for older WebViews.
  if (typeof createImageBitmap === 'function') {
    try {
      const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' })
      return { source: bitmap, width: bitmap.width, height: bitmap.height, cleanup: () => bitmap.close() }
    } catch {
      // Fall through to the <img> path.
    }
  }
  const { image, cleanup } = await loadWithImageElement(file)
  return { source: image, width: image.naturalWidth || image.width, height: image.naturalHeight || image.height, cleanup }
}

/**
 * Convert a file to a square data URI (WebP where supported, PNG otherwise).
 *
 * `background` fills the canvas before drawing, so an uploaded PNG with
 * transparency still comes out as a solid tile instead of a floating cutout.
 *
 * Throws `AvatarImageError` with a code the UI can map to a localized message.
 */
export async function fileToAvatarDataUri(file: File, background = '#F1F3F5'): Promise<string> {
  if (!ACCEPTED_TYPES.has(file.type)) throw new AvatarImageError('unsupported')
  if (file.size > AVATAR_UPLOAD_MAX_BYTES) throw new AvatarImageError('too-large')

  const { source, width, height, cleanup } = await decodeFile(file)
  try {
    if (!width || !height) throw new AvatarImageError('decode-failed')
    const { sx, sy, side } = centerCropRect(width, height)

    const canvas = document.createElement('canvas')
    canvas.width = AVATAR_UPLOAD_SIZE
    canvas.height = AVATAR_UPLOAD_SIZE
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new AvatarImageError('decode-failed')

    ctx.imageSmoothingEnabled = true
    ctx.imageSmoothingQuality = 'high'
    ctx.fillStyle = background
    ctx.fillRect(0, 0, AVATAR_UPLOAD_SIZE, AVATAR_UPLOAD_SIZE)
    ctx.drawImage(source, sx, sy, side, side, 0, 0, AVATAR_UPLOAD_SIZE, AVATAR_UPLOAD_SIZE)

    // WebP keeps a 192px photo around a few KB. Browsers that cannot encode it
    // (older Safari/WebView) fall back to JPEG, which is far smaller than PNG
    // for a photo and stays well under the server's decoded-size cap. PNG is
    // the last resort.
    const webp = canvas.toDataURL('image/webp', 0.86)
    if (webp.startsWith('data:image/webp')) return webp
    const jpeg = canvas.toDataURL('image/jpeg', 0.9)
    if (jpeg.startsWith('data:image/jpeg')) return jpeg
    return canvas.toDataURL('image/png')
  } finally {
    cleanup()
  }
}
