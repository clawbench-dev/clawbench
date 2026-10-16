import { describe, it, expect } from 'vitest'
import { decideCertificateAction, isLocalhostHost, originOf } from './certificatePolicy'

describe('originOf', () => {
  it('returns scheme://host:port for a full URL', () => {
    expect(originOf('https://example.com:20000/login')).toBe('https://example.com:20000')
  })

  it('returns the default port when the URL omits it', () => {
    expect(originOf('https://example.com/')).toBe('https://example.com')
  })

  it('returns an empty string for an unparseable URL', () => {
    expect(originOf('not a url')).toBe('')
    expect(originOf('')).toBe('')
  })
})

describe('isLocalhostHost', () => {
  it('matches every loopback spelling', () => {
    for (const h of ['localhost', '127.0.0.1', '::1', '[::1]']) {
      expect(isLocalhostHost(h), h).toBe(true)
    }
  })

  it('does not match a real host that merely contains a loopback word', () => {
    for (const h of ['example.com', 'localhost.example.com', '10.0.0.1', '127.0.0.1.example.com']) {
      expect(isLocalhostHost(h), h).toBe(false)
    }
  })
})

describe('decideCertificateAction', () => {
  it('ignores a subframe failure regardless of host', () => {
    // A subresource must never raise a blocking prompt.
    expect(decideCertificateAction('https://example.com/', false)).toBe('ignore')
    expect(decideCertificateAction('https://127.0.0.1:23999/', false)).toBe('ignore')
  })

  it('trusts a localhost main-frame navigation without asking', () => {
    // A tunneled connection reaches the server on localhost, where the
    // certificate can never match the host the client typed.
    for (const u of ['https://127.0.0.1:20000/', 'https://localhost:20000/', 'https://[::1]:20000/']) {
      expect(decideCertificateAction(u, true), u).toBe('auto-trust')
    }
  })

  it('prompts for a remote main-frame navigation', () => {
    expect(decideCertificateAction('https://example.com/', true)).toBe('prompt')
    expect(decideCertificateAction('https://192.168.1.50:20000/', true)).toBe('prompt')
  })

  it('ignores an unparseable URL rather than prompting', () => {
    // A prompt for a URL we cannot even key trust on would be unanswerable.
    expect(decideCertificateAction('garbage', true)).toBe('ignore')
  })
})
