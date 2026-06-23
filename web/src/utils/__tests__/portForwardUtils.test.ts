import { describe, expect, it } from 'vitest'
import { hasActivePort, tunnelStatusFromPorts, buildPortUrl } from '@/utils/portForwardUtils'
import type { ForwardedPort } from '@/utils/portForwardUtils'

describe('portForwardUtils', () => {
  // --- hasActivePort ---

  describe('hasActivePort', () => {
    it('returns true when at least one port is active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', autoDetect: false, active: false, localPort: 0 },
        { port: 4000, host: '', name: 'B', protocol: 'http', autoDetect: false, active: true, localPort: 0 },
      ]
      expect(hasActivePort(ports)).toBe(true)
    })

    it('returns false when no ports are active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', autoDetect: false, active: false, localPort: 0 },
        { port: 4000, host: '', name: 'B', protocol: 'http', autoDetect: false, active: false, localPort: 0 },
      ]
      expect(hasActivePort(ports)).toBe(false)
    })

    it('returns false for empty array', () => {
      expect(hasActivePort([])).toBe(false)
    })

    it('returns true when all ports are active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', autoDetect: false, active: true, localPort: 0 },
        { port: 4000, host: '', name: 'B', protocol: 'http', autoDetect: false, active: true, localPort: 0 },
      ]
      expect(hasActivePort(ports)).toBe(true)
    })
  })

  // --- tunnelStatusFromPorts ---

  describe('tunnelStatusFromPorts', () => {
    it('returns "ok" when there are no ports', () => {
      expect(tunnelStatusFromPorts([])).toBe('ok')
    })

    it('returns "ok" when there are ports and at least one is active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', autoDetect: false, active: true, localPort: 0 },
        { port: 4000, host: '', name: 'B', protocol: 'http', autoDetect: false, active: false, localPort: 0 },
      ]
      expect(tunnelStatusFromPorts(ports)).toBe('ok')
    })

    it('returns "degraded" when there are ports but none are active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', autoDetect: false, active: false, localPort: 0 },
        { port: 4000, host: '', name: 'B', protocol: 'http', autoDetect: false, active: false, localPort: 0 },
      ]
      expect(tunnelStatusFromPorts(ports)).toBe('degraded')
    })

    it('returns "ok" when all ports are active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', autoDetect: false, active: true, localPort: 0 },
      ]
      expect(tunnelStatusFromPorts(ports)).toBe('ok')
    })
  })

  // --- buildPortUrl ---

  describe('buildPortUrl', () => {
    it('builds http URL by default', () => {
      expect(buildPortUrl(3000)).toBe('http://localhost:3000')
    })

    it('builds https URL when protocol is https', () => {
      expect(buildPortUrl(3000, 'https')).toBe('https://localhost:3000')
    })

    it('builds http URL when protocol is http', () => {
      expect(buildPortUrl(3000, 'http')).toBe('http://localhost:3000')
    })

    it('handles different port numbers', () => {
      expect(buildPortUrl(8080)).toBe('http://localhost:8080')
      expect(buildPortUrl(443, 'https')).toBe('https://localhost')
    })

    it('handles different ports', () => {
      expect(buildPortUrl(8080)).toBe('http://localhost:8080')
      expect(buildPortUrl(443, 'https')).toBe('https://localhost')
    })

    it('omits port 80 for http (default port)', () => {
      expect(buildPortUrl(80, 'http')).toBe('http://localhost')
    })

    it('omits port 443 for https (default port)', () => {
      expect(buildPortUrl(443, 'https')).toBe('https://localhost')
    })

    it('keeps port 80 for https (non-default)', () => {
      expect(buildPortUrl(80, 'https')).toBe('https://localhost:80')
    })

    it('keeps port 443 for http (non-default)', () => {
      expect(buildPortUrl(443, 'http')).toBe('http://localhost:443')
    })

    it('defaults to localhost when host is empty', () => {
      expect(buildPortUrl(3000, 'http')).toBe('http://localhost:3000')
      expect(buildPortUrl(3000, 'http')).toBe('http://localhost:3000')
    })
  })
})
