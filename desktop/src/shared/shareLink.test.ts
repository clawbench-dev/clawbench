import { describe, expect, it } from 'vitest'
import { isShareUrl } from './shareLink'

const SERVER = 'http://192.168.1.5:20000'

describe('isShareUrl', () => {
  it('accepts a share link on the configured server', () => {
    expect(isShareUrl(`${SERVER}/share/abc123`, SERVER)).toBe(true)
    expect(isShareUrl(`${SERVER}/share/abc123/`, SERVER)).toBe(true)
    // A different port is a different server, and a different origin.
    expect(isShareUrl('https://example.com:8443/share/abc123', 'https://example.com')).toBe(false)
  })

  it('rejects a share link from another origin', () => {
    // The window it opens carries the app's preload: credentials, screen
    // capture and remote control. Shape alone is not a trust decision.
    expect(isShareUrl('https://evil.tld/share/abc123', SERVER)).toBe(false)
    expect(isShareUrl('http://192.168.1.5:20001/share/abc123', SERVER)).toBe(false)
    // http vs https on the same host is a different origin too.
    expect(isShareUrl('https://192.168.1.5:20000/share/abc123', SERVER)).toBe(false)
  })

  it('rejects any share link when no server is configured', () => {
    expect(isShareUrl(`${SERVER}/share/abc123`, '')).toBe(false)
    expect(isShareUrl(`${SERVER}/share/abc123`, undefined)).toBe(false)
  })

  it('rejects everything else', () => {
    // The window it opens can capture the screen; only share pages qualify.
    expect(isShareUrl(`${SERVER}/`, SERVER)).toBe(false)
    expect(isShareUrl(`${SERVER}/share/abc/extra`, SERVER)).toBe(false)
    expect(isShareUrl('file:///etc/passwd', SERVER)).toBe(false)
    expect(isShareUrl('javascript:alert(1)', SERVER)).toBe(false)
    expect(isShareUrl('not a url', SERVER)).toBe(false)
    expect(isShareUrl('', SERVER)).toBe(false)
  })
})
