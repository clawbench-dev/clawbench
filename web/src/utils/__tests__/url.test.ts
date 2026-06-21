import { describe, expect, it } from 'vitest'
import { formatServerHost } from '@/utils/url'

describe('formatServerHost', () => {
  it('returns host:port for a URL with non-default port', () => {
    expect(formatServerHost('http://192.168.1.100:8080')).toBe('192.168.1.100:8080')
  })

  it('returns host without port for HTTP on port 80', () => {
    expect(formatServerHost('http://example.com:80')).toBe('example.com')
  })

  it('returns host without port for HTTPS on port 443', () => {
    expect(formatServerHost('https://example.com:443')).toBe('example.com')
  })

  it('returns host with port for HTTPS on non-default port', () => {
    expect(formatServerHost('https://example.com:8443')).toBe('example.com:8443')
  })

  it('returns host without port for HTTP URL without explicit port', () => {
    expect(formatServerHost('http://example.com')).toBe('example.com')
  })

  it('returns host without port for HTTPS URL without explicit port', () => {
    expect(formatServerHost('https://example.com')).toBe('example.com')
  })

  it('returns localhost with port', () => {
    expect(formatServerHost('http://localhost:3000')).toBe('localhost:3000')
  })

  it('returns the raw string for invalid URLs', () => {
    expect(formatServerHost('not-a-url')).toBe('not-a-url')
  })

  it('returns the raw string for empty input', () => {
    expect(formatServerHost('')).toBe('')
  })

  it('handles IP addresses with ports', () => {
    expect(formatServerHost('http://10.0.0.1:9090')).toBe('10.0.0.1:9090')
  })
})
