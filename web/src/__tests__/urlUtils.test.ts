import { describe, expect, it } from 'vitest'
import { normalizeServerUrl } from '@/utils/url'

/**
 * normalizeServerUrl is what the web add-server form saves. It must land on a
 * URL the native connectToServer() and the clients' login page accept, which
 * for IPv6 means the full `scheme://[literal]:port` — `new URL()` refuses the
 * bare form (it reads `240e:` as a scheme), and a half-written literal is
 * exactly the address that could never load (issue #696). Anything short of
 * the full form is rejected rather than completed by guessing.
 */
describe('normalizeServerUrl', () => {
  const CASES: Array<[string, string]> = [
    // Already complete: only the path/query is dropped.
    ['https://192.168.1.100:20000', 'https://192.168.1.100:20000'],
    ['https://host:8443/chat?x=1', 'https://host:8443'],
    ['HTTPS://Host:8443', 'https://host:8443'],
    // Scheme-less hostname / IPv4 keeps the historical https + port 20000.
    ['new.example.com', 'https://new.example.com:20000'],
    ['192.168.1.10:8080', 'https://192.168.1.10:8080'],
    // IPv6: the form the manual documents.
    ['http://[240e:390:1234:5678::123]:20000', 'http://[240e:390:1234:5678::123]:20000'],
    ['https://[::1]:8443/', 'https://[::1]:8443'],
    // Whitespace from a paste.
    ['  http://[::1]:20000  ', 'http://[::1]:20000'],
  ]
  for (const [input, expected] of CASES) {
    it(`${JSON.stringify(input)} -> ${JSON.stringify(expected)}`, () => {
      expect(normalizeServerUrl(input)).toBe(expected)
    })
  }

  const REJECTED = [
    '',
    '   ',
    'not a url',
    'ftp://host:21',
    'https://user:pass@host:8443',
    'http://',
    'http://[240e::123:20000', // malformed literal
    '[host]:8080',
    '20800:20000', // all-digit host: `new URL` would read it as a compressed IPv4
    // IPv6 is never completed by guessing: every part must be written out.
    '240e:390:1234:5678::123', // no brackets, so no way to attach a port
    '[240e:390:1234:5678::123]:20000', // no scheme
    'http://[240e:390:1234:5678::123]', // no port
    'http://240e:390:1234:5678::123:20000', // no brackets
  ]
  for (const input of REJECTED) {
    it(`rejects ${JSON.stringify(input)}`, () => {
      expect(normalizeServerUrl(input)).toBe('')
    })
  }
})
