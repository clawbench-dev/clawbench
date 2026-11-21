import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  AVATAR_UPLOAD_MAX_BYTES,
  AVATAR_UPLOAD_SIZE,
  AvatarImageError,
  centerCropRect,
  fileToAvatarDataUri,
} from '@/utils/avatarImage'

function makeFile(type = 'image/png', size = 1024): File {
  const file = new File(['x'], 'avatar.bin', { type })
  Object.defineProperty(file, 'size', { value: size })
  return file
}

describe('centerCropRect', () => {
  it('crops the longer side around the centre', () => {
    expect(centerCropRect(400, 300)).toEqual({ sx: 50, sy: 0, side: 300 })
    expect(centerCropRect(300, 400)).toEqual({ sx: 0, sy: 50, side: 300 })
  })

  it('leaves a square untouched', () => {
    expect(centerCropRect(192, 192)).toEqual({ sx: 0, sy: 0, side: 192 })
  })
})

describe('fileToAvatarDataUri', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('rejects an unsupported MIME type before decoding', async () => {
    await expect(fileToAvatarDataUri(makeFile('image/gif'))).rejects.toMatchObject({
      code: 'unsupported',
    })
  })

  it('rejects a file above the size cap', async () => {
    await expect(fileToAvatarDataUri(makeFile('image/png', AVATAR_UPLOAD_MAX_BYTES + 1))).rejects.toMatchObject({
      code: 'too-large',
    })
  })

  it('centre-crops, fills a background and returns a WebP data URI', async () => {
    const close = vi.fn()
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue({ width: 400, height: 300, close }))

    const ctx = {
      imageSmoothingEnabled: false,
      imageSmoothingQuality: '',
      fillStyle: '',
      fillRect: vi.fn(),
      drawImage: vi.fn(),
    }
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(ctx as unknown as CanvasRenderingContext2D)
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue('data:image/webp;base64,AAA')

    const uri = await fileToAvatarDataUri(makeFile(), '#E3ECF7')

    expect(uri).toBe('data:image/webp;base64,AAA')
    expect(ctx.fillStyle).toBe('#E3ECF7')
    expect(ctx.fillRect).toHaveBeenCalledWith(0, 0, AVATAR_UPLOAD_SIZE, AVATAR_UPLOAD_SIZE)
    expect(ctx.drawImage).toHaveBeenCalledWith(expect.anything(), 50, 0, 300, 300, 0, 0, AVATAR_UPLOAD_SIZE, AVATAR_UPLOAD_SIZE)
    expect(close).toHaveBeenCalled()
  })

  it('falls back to JPEG when the browser cannot encode WebP', async () => {
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue({ width: 200, height: 200, close: vi.fn() }))

    const ctx = {
      imageSmoothingEnabled: false,
      imageSmoothingQuality: '',
      fillStyle: '',
      fillRect: vi.fn(),
      drawImage: vi.fn(),
    }
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(ctx as unknown as CanvasRenderingContext2D)
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockImplementation((type?: string) =>
      type === 'image/webp' ? 'data:image/png;base64,AAA' : 'data:image/jpeg;base64,BBB',
    )

    await expect(fileToAvatarDataUri(makeFile())).resolves.toBe('data:image/jpeg;base64,BBB')
  })

  it('uses PNG when neither WebP nor JPEG is encodable', async () => {
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue({ width: 200, height: 200, close: vi.fn() }))

    const ctx = {
      imageSmoothingEnabled: false,
      imageSmoothingQuality: '',
      fillStyle: '',
      fillRect: vi.fn(),
      drawImage: vi.fn(),
    }
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(ctx as unknown as CanvasRenderingContext2D)
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockImplementation((type?: string) =>
      type === 'image/png' ? 'data:image/png;base64,CCC' : 'data:image/png;base64,AAA',
    )

    await expect(fileToAvatarDataUri(makeFile())).resolves.toBe('data:image/png;base64,CCC')
  })

  it('falls back to <img> when createImageBitmap is unavailable', async () => {
    vi.stubGlobal('createImageBitmap', undefined)
    const revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:avatar'), revokeObjectURL })

    class FakeImage {
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      naturalWidth = 200
      naturalHeight = 100
      width = 200
      height = 100
      set src(_value: string) {
        queueMicrotask(() => this.onload?.())
      }
    }
    vi.stubGlobal('Image', FakeImage)

    const ctx = {
      imageSmoothingEnabled: false,
      imageSmoothingQuality: '',
      fillStyle: '',
      fillRect: vi.fn(),
      drawImage: vi.fn(),
    }
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(ctx as unknown as CanvasRenderingContext2D)
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockReturnValue('data:image/webp;base64,AAA')

    await expect(fileToAvatarDataUri(makeFile())).resolves.toBe('data:image/webp;base64,AAA')
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:avatar')
  })

  it('rejects an image with no dimensions', async () => {
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue({ width: 0, height: 0, close: vi.fn() }))

    await expect(fileToAvatarDataUri(makeFile())).rejects.toMatchObject({ code: 'decode-failed' })
  })

  it('rejects when the canvas context is unavailable', async () => {
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue({ width: 10, height: 10, close: vi.fn() }))
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)

    await expect(fileToAvatarDataUri(makeFile())).rejects.toMatchObject({ code: 'decode-failed' })
  })
})
