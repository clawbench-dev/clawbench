import { describe, expect, it } from 'vitest'
import { isShareUrl } from './shareLink'

describe('isShareUrl', () => {
  it('accepts a share link', () => {
    expect(isShareUrl('http://192.168.1.5:20000/share/abc123')).toBe(true)
    expect(isShareUrl('https://example.com/share/abc123/')).toBe(true)
  })

  it('rejects everything else', () => {
    // The window it opens can capture the screen; only share pages qualify.
    expect(isShareUrl('http://192.168.1.5:20000/')).toBe(false)
    expect(isShareUrl('http://192.168.1.5:20000/share/abc/extra')).toBe(false)
    expect(isShareUrl('file:///etc/passwd')).toBe(false)
    expect(isShareUrl('javascript:alert(1)')).toBe(false)
    expect(isShareUrl('not a url')).toBe(false)
    expect(isShareUrl('')).toBe(false)
  })
})
