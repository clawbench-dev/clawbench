import { describe, expect, it } from 'vitest'
import { avatarToSrc } from '@/utils/avatarSrc'

describe('avatarToSrc', () => {
  it('encodes a raw SVG', () => {
    const svg = '<svg><rect fill="#123"/></svg>'
    expect(avatarToSrc(svg)).toBe('data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg))
  })

  it('passes an uploaded raster data URI through untouched', () => {
    const uri = 'data:image/webp;base64,UklGRg=='
    expect(avatarToSrc(uri)).toBe(uri)
  })

  it('returns an empty string for an empty avatar', () => {
    expect(avatarToSrc('')).toBe('')
  })
})
