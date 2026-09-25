import { describe, expect, it } from 'vitest'
import { hasActivePort, tunnelStatusFromPorts, buildPortUrl, buildServerAddress, isReversePort, enabledPorts, sshInstallHint } from '@/utils/portForwardUtils'
import type { ForwardedPort } from '@/utils/portForwardUtils'

describe('portForwardUtils', () => {
  // --- hasActivePort ---

  describe('hasActivePort', () => {
    it('returns true when at least one port is active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: false, localPort: 0, enabled: true },
        { port: 4000, host: '', name: 'B', protocol: 'http', active: true, localPort: 0, enabled: true },
      ]
      expect(hasActivePort(ports)).toBe(true)
    })

    it('returns false when no ports are active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: false, localPort: 0, enabled: true },
        { port: 4000, host: '', name: 'B', protocol: 'http', active: false, localPort: 0, enabled: true },
      ]
      expect(hasActivePort(ports)).toBe(false)
    })

    it('returns false for empty array', () => {
      expect(hasActivePort([])).toBe(false)
    })

    it('returns true when all ports are active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: true, localPort: 0, enabled: true },
        { port: 4000, host: '', name: 'B', protocol: 'http', active: true, localPort: 0, enabled: true },
      ]
      expect(hasActivePort(ports)).toBe(true)
    })

    it('ignores disabled ports when checking active backend', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: true, localPort: 0, enabled: false },
        { port: 4000, host: '', name: 'B', protocol: 'http', active: false, localPort: 0, enabled: true },
      ]
      expect(hasActivePort(ports)).toBe(false)
    })
  })

  describe('enabledPorts', () => {
    it('returns only enabled ports', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: false, localPort: 0, enabled: true },
        { port: 4000, host: '', name: 'B', protocol: 'http', active: false, localPort: 0, enabled: false },
      ]
      expect(enabledPorts(ports).map(p => p.port)).toEqual([3000])
    })
  })

  // --- tunnelStatusFromPorts ---

  describe('tunnelStatusFromPorts', () => {
    it('returns "ok" when there are no ports', () => {
      expect(tunnelStatusFromPorts([])).toBe('ok')
    })

    it('returns "ok" when there are ports and at least one is active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: true, localPort: 0, enabled: true },
        { port: 4000, host: '', name: 'B', protocol: 'http', active: false, localPort: 0, enabled: true },
      ]
      expect(tunnelStatusFromPorts(ports)).toBe('ok')
    })

    it('returns "degraded" when there are ports but none are active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: false, localPort: 0, enabled: true },
        { port: 4000, host: '', name: 'B', protocol: 'http', active: false, localPort: 0, enabled: true },
      ]
      expect(tunnelStatusFromPorts(ports)).toBe('degraded')
    })

    it('returns "ok" when all ports are active', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: true, localPort: 0, enabled: true },
      ]
      expect(tunnelStatusFromPorts(ports)).toBe('ok')
    })

    it('returns "ok" when the only active ports are disabled', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: true, localPort: 0, enabled: false },
      ]
      expect(tunnelStatusFromPorts(ports)).toBe('ok')
    })

    it('returns "ok" when all registered ports are disabled', () => {
      const ports: ForwardedPort[] = [
        { port: 3000, host: '', name: 'A', protocol: 'http', active: false, localPort: 0, enabled: false },
        { port: 4000, host: '', name: 'B', protocol: 'http', active: false, localPort: 0, enabled: false },
      ]
      expect(tunnelStatusFromPorts(ports)).toBe('ok')
    })
  })

  // --- buildPortUrl ---

  describe('buildPortUrl', () => {
    it('builds http URL by default', () => {
      expect(buildPortUrl(3000)).toBe('http://localhost:3000/')
    })

    it('builds https URL when protocol is https', () => {
      expect(buildPortUrl(3000, 'https')).toBe('https://localhost:3000/')
    })

    it('builds http URL when protocol is http', () => {
      expect(buildPortUrl(3000, 'http')).toBe('http://localhost:3000/')
    })

    it('handles different port numbers', () => {
      expect(buildPortUrl(8080)).toBe('http://localhost:8080/')
      expect(buildPortUrl(443, 'https')).toBe('https://localhost/')
    })

    it('omits port 80 for http (default port)', () => {
      expect(buildPortUrl(80, 'http')).toBe('http://localhost/')
    })

    it('omits port 443 for https (default port)', () => {
      expect(buildPortUrl(443, 'https')).toBe('https://localhost/')
    })

    it('keeps port 80 for https (non-default)', () => {
      expect(buildPortUrl(80, 'https')).toBe('https://localhost:80/')
    })

    it('keeps port 443 for http (non-default)', () => {
      expect(buildPortUrl(443, 'http')).toBe('http://localhost:443/')
    })

    it('defaults to localhost when host is empty', () => {
      expect(buildPortUrl(3000, 'http')).toBe('http://localhost:3000/')
    })

    it('includes path when provided', () => {
      expect(buildPortUrl(3000, 'http', '/api/status')).toBe('http://localhost:3000/api/status')
    })

    it('defaults to / when path is empty', () => {
      expect(buildPortUrl(3000, 'http', '')).toBe('http://localhost:3000/')
    })

    it('includes path with default port', () => {
      expect(buildPortUrl(443, 'https', '/dashboard')).toBe('https://localhost/dashboard')
    })
  })

  // --- sshInstallHint ---

  describe('sshInstallHint', () => {
    it('returns Windows OpenSSH download page for Windows', () => {
      expect(sshInstallHint({ windows: true, macDesktop: false, linuxDesktop: false })).toEqual({
        kind: 'windows',
        url: 'https://learn.microsoft.com/windows-server/administration/openssh/openssh_install_firstuse',
      })
    })

    it('returns mac noInstall marker for macOS desktop', () => {
      expect(sshInstallHint({ windows: false, macDesktop: true, linuxDesktop: false })).toEqual({
        kind: 'mac',
        noInstall: true,
      })
    })

    it('returns install command for Linux desktop', () => {
      const hint = sshInstallHint({ windows: false, macDesktop: false, linuxDesktop: true })
      expect(hint?.kind).toBe('linux')
      expect(hint && 'command' in hint ? hint.command : '').toContain('apt install openssh-client')
    })

    it('returns null for unknown platform', () => {
      expect(sshInstallHint({ windows: false, macDesktop: false, linuxDesktop: false })).toBeNull()
    })

    it('prefers windows when multiple flags set (windows wins by specificity)', () => {
      const hint = sshInstallHint({ windows: true, macDesktop: true, linuxDesktop: true })
      expect(hint?.kind).toBe('windows')
    })
  })
})

describe('buildServerAddress', () => {
  it('builds a loopback URL with the server-side port', () => {
    expect(buildServerAddress(9000, 'http')).toBe('http://127.0.0.1:9000')
  })

  it('uses https when the protocol is https', () => {
    expect(buildServerAddress(9443, 'https')).toBe('https://127.0.0.1:9443')
  })

  it('omits the port when it is the protocol default', () => {
    expect(buildServerAddress(80, 'http')).toBe('http://127.0.0.1')
    expect(buildServerAddress(443, 'https')).toBe('https://127.0.0.1')
  })

  it('defaults to http for an unknown protocol', () => {
    expect(buildServerAddress(8080, undefined)).toBe('http://127.0.0.1:8080')
  })
})

describe('isReversePort', () => {
  const base: ForwardedPort = {
    port: 3000, localPort: 9000, host: '', name: 'svc',
    protocol: 'http', active: false, enabled: true,
  }

  it('is true only for direction=reverse', () => {
    expect(isReversePort({ ...base, direction: 'reverse' })).toBe(true)
  })

  it('is false for direction=forward', () => {
    expect(isReversePort({ ...base, direction: 'forward' })).toBe(false)
  })

  it('treats a missing direction as forward (older backends)', () => {
    expect(isReversePort(base)).toBe(false)
  })
})
