import { describe, it, expect } from 'vitest'
import { decideCertificateAction, isLocalhostHost, isRememberedCertificate, originOf } from './certificatePolicy'

describe('originOf', () => {
  it('returns scheme://host:port for a full URL', () => {
    expect(originOf('https://example.com:20000/login')).toBe('https://example.com:20000')
  })

  it('returns the default port when the URL omits it', () => {
    expect(originOf('https://example.com/')).toBe('https://example.com')
  })

  it('folds a WebSocket URL onto its HTTP origin', () => {
    // Same server, same certificate, same trust decision. Keeping `wss:` as a
    // distinct key left the WebSocket rejected after the page was trusted.
    expect(originOf('wss://example.com:20000/ws')).toBe('https://example.com:20000')
    expect(originOf('ws://example.com:20000/ws')).toBe('http://example.com:20000')
    // Default ports normalise the same way on both sides.
    expect(originOf('wss://example.com/ws')).toBe('https://example.com')
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

describe('isRememberedCertificate', () => {
  const remembered = { 'https://192.168.1.50:20000': 'sha256/AAAA' }

  it('accepts the same certificate the user confirmed earlier', () => {
    expect(isRememberedCertificate(remembered, 'https://192.168.1.50:20000/api/chat', 'sha256/AAAA')).toBe(true)
  })

  it('covers the WebSocket of a trusted page', () => {
    // `originOf` folds wss onto https, so the socket is the same entry.
    expect(isRememberedCertificate(remembered, 'wss://192.168.1.50:20000/ws', 'sha256/AAAA')).toBe(true)
  })

  it('rejects a DIFFERENT certificate on the same origin', () => {
    // The whole point of binding to the fingerprint: a reinstalled server, a
    // rotated certificate or a man-in-the-middle must prompt again.
    expect(isRememberedCertificate(remembered, 'https://192.168.1.50:20000/', 'sha256/BBBB')).toBe(false)
  })

  it('rejects an unknown origin', () => {
    expect(isRememberedCertificate(remembered, 'https://other:20000/', 'sha256/AAAA')).toBe(false)
  })

  it('never trusts an empty fingerprint', () => {
    // An empty value means we could not read the certificate; asking is the
    // only safe answer, and it must not match an entry stored as ''.
    expect(isRememberedCertificate({ 'https://h:1': '' }, 'https://h:1/', '')).toBe(false)
  })

  it('rejects an unparseable URL', () => {
    expect(isRememberedCertificate(remembered, 'garbage', 'sha256/AAAA')).toBe(false)
  })
})

describe('decideCertificateAction', () => {
  it('trusts every request to an origin the user already approved', () => {
    // The regression this guards: trust used to apply to the main-frame
    // navigation only. Verification runs per socket, so the WebSocket and most
    // API calls raised their own certificate-error with isMainFrame=false and
    // were rejected — the window rendered, then nothing worked.
    expect(decideCertificateAction('https://example.com/api/chat', false, true)).toBe('auto-trust')
    expect(decideCertificateAction('wss://example.com/ws', false, true)).toBe('auto-trust')
    expect(decideCertificateAction('https://example.com/', true, true)).toBe('auto-trust')
  })

  it('trusts a localhost subresource without asking', () => {
    // A tunneled connection reaches the server on loopback, where the
    // certificate can never match the host the client typed. Restricting this
    // to the main frame broke every API call over SSH / h2 tunnels.
    for (const u of ['https://127.0.0.1:20000/api/chat', 'https://localhost:20000/ws', 'https://[::1]:20000/x']) {
      expect(decideCertificateAction(u, false), u).toBe('auto-trust')
    }
  })

  it('ignores a third-party subresource rather than prompting', () => {
    // A subresource on an origin the user never approved must never raise a
    // blocking prompt; Chromium rejects it on its own.
    expect(decideCertificateAction('https://cdn.example.net/a.png', false)).toBe('ignore')
    expect(decideCertificateAction('https://cdn.example.net/a.png', false, false)).toBe('ignore')
  })

  it('trusts a localhost main-frame navigation without asking', () => {
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
    expect(decideCertificateAction('garbage', true, true)).toBe('ignore')
  })
})
