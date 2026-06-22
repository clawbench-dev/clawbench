import { describe, expect, it } from 'vitest'
import { formatServerHost } from '@/utils/url'

describe('formatServerHost', () => {
  it('returns origin for a URL with non-default port', () => {
    expect(formatServerHost('http://192.168.1.100:8080')).toBe('http://192.168.1.100:8080')
  })

  it('returns origin for HTTP on default port 80', () => {
    expect(formatServerHost('http://example.com:80')).toBe('http://example.com')
  })

  it('returns origin for HTTPS on default port 443', () => {
    expect(formatServerHost('https://example.com:443')).toBe('https://example.com')
  })

  it('returns origin for HTTPS on non-default port', () => {
    expect(formatServerHost('https://example.com:8443')).toBe('https://example.com:8443')
  })

  it('returns origin for HTTP URL without explicit port', () => {
    expect(formatServerHost('http://example.com')).toBe('http://example.com')
  })

  it('returns origin for HTTPS URL without explicit port', () => {
    expect(formatServerHost('https://example.com')).toBe('https://example.com')
  })

  it('returns origin for localhost with port', () => {
    expect(formatServerHost('http://localhost:3000')).toBe('http://localhost:3000')
  })

  it('returns the raw string for invalid URLs', () => {
    expect(formatServerHost('not-a-url')).toBe('not-a-url')
  })

  it('returns the raw string for empty input', () => {
    expect(formatServerHost('')).toBe('')
  })

  it('handles IP addresses with ports', () => {
    expect(formatServerHost('http://10.0.0.1:9090')).toBe('http://10.0.0.1:9090')
  })

  it('includes protocol for https URLs', () => {
    expect(formatServerHost('https://192.168.1.100:20300')).toBe('https://192.168.1.100:20300')
  })
})
