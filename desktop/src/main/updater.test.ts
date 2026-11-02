import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('electron', () => ({
  app: { getVersion: () => '1.0.0', isPackaged: true },
}))

vi.mock('./httpGet', () => ({
  httpGetBuffer: vi.fn(),
}))

vi.mock('./clientLog', () => ({
  recordError: vi.fn(),
}))

import { platformKey, latestInfoUrl, fetchDesktopLatest, classifyFetchFailure } from './updater'
import { httpGetBuffer } from './httpGet'
import { recordError } from './clientLog'

describe('platformKey', () => {
  it('maps the platforms the release workflow builds', () => {
    expect(platformKey('linux', 'x64')).toBe('linux-x64')
    expect(platformKey('linux', 'arm64')).toBe('linux-arm64')
    expect(platformKey('win32', 'x64')).toBe('win32-x64')
    expect(platformKey('darwin', 'arm64')).toBe('darwin-arm64')
    expect(platformKey('darwin', 'x64')).toBe('darwin-x64')
  })

  it('returns empty for platforms with no desktop build', () => {
    expect(platformKey('freebsd', 'x64')).toBe('')
  })
})

describe('latestInfoUrl', () => {
  it('joins onto the server URL without doubling slashes', () => {
    expect(latestInfoUrl('https://example.com:20000')).toBe('https://example.com:20000/api/desktop/latest')
    expect(latestInfoUrl('https://example.com:20000/')).toBe('https://example.com:20000/api/desktop/latest')
  })
})

describe('fetchDesktopLatest', () => {
  beforeEach(() => {
    // Call history must not leak between cases: the pinning tests assert how
    // many attempts were made.
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockClear()
  })

  it('returns the server version and platform URLs', async () => {
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({
        version: 'v2.0.0',
        tag: 'v2.0.0',
        downloads: { 'linux-x64': ['https://mirror/x.zip', 'https://github.com/x.zip'] },
      })),
    )

    const info = await fetchDesktopLatest('https://example.com:20000')
    expect(info).not.toBeNull()
    expect(info!.version).toBe('v2.0.0')
    // Order is preserved so the client tries the mirror before github.com.
    expect(info!.urls).toEqual(['https://mirror/x.zip', 'https://github.com/x.zip'])
  })

  it('verifies the server certificate normally when none is remembered', async () => {
    // A self-hosted server may be self-signed, but the main process must not
    // simply stop checking: the answer decides which binary is downloaded and
    // run. With no accepted fingerprint the request is verified like any other
    // and a self-signed server reads as "nothing to offer" — the behaviour
    // before the upgrade check ever gained an insecure switch.
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({ version: 'v2.0.0', downloads: { 'linux-x64': ['https://mirror/x.zip'] } })),
    )
    await fetchDesktopLatest('https://192.168.1.50:20000')
    expect(httpGetBuffer).toHaveBeenCalledWith('https://192.168.1.50:20000/api/desktop/latest')
    expect(httpGetBuffer).toHaveBeenCalledTimes(1)
  })

  it('retries pinned to the accepted certificate when plain verification fails', async () => {
    const fn = httpGetBuffer as ReturnType<typeof vi.fn>
    fn.mockRejectedValueOnce(new Error('self signed certificate'))
    fn.mockResolvedValueOnce(
      Buffer.from(JSON.stringify({ version: 'v2.0.0', downloads: { 'linux-x64': ['https://mirror/x.zip'] } })),
    )

    const info = await fetchDesktopLatest('https://192.168.1.50:20000', 'sha256/AAAA')

    expect(info).not.toBeNull()
    expect(info!.version).toBe('v2.0.0')
    expect(fn).toHaveBeenLastCalledWith('https://192.168.1.50:20000/api/desktop/latest', {
      pinnedFingerprint: 'sha256/AAAA',
    })
  })

  it('returns null when the pinned certificate does not match either', async () => {
    // The whole point: an answer from a certificate the user never accepted must
    // not become a version + download URL to run.
    const fn = httpGetBuffer as ReturnType<typeof vi.fn>
    fn.mockRejectedValueOnce(new Error('self signed certificate'))
    const mismatch = new Error('certificate does not match the pinned one for https://192.168.1.50:20000')
    mismatch.name = 'CertificatePinMismatchError'
    fn.mockRejectedValueOnce(mismatch)

    expect(await fetchDesktopLatest('https://192.168.1.50:20000', 'sha256/AAAA')).toBeNull()
    // And it must leave a trace: "upgrades silently stopped being offered" is
    // otherwise indistinguishable from "this server publishes no release".
    const logged = (recordError as ReturnType<typeof vi.fn>).mock.calls.at(-1)?.[1] as string
    expect(logged).toContain('pinned certificate mismatch')
  })

  it('returns null when the server has no release for this platform', async () => {
    // A dev server answers with an empty downloads map; there is nothing to
    // install even though the version string differs.
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({ version: 'dev', tag: '', downloads: {} })),
    )
    expect(await fetchDesktopLatest('https://example.com:20000')).toBeNull()
  })

  it('returns null when the server is unreachable', async () => {
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('ECONNREFUSED'))
    expect(await fetchDesktopLatest('https://example.com:20000')).toBeNull()
  })

  it('returns null when no server is configured', async () => {
    expect(await fetchDesktopLatest('')).toBeNull()
  })

  it('returns null on a malformed response', async () => {
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(Buffer.from('<html>not json</html>'))
    expect(await fetchDesktopLatest('https://example.com:20000')).toBeNull()
  })

  it('surfaces the payload URLs for the running platform', async () => {
    // Keyed off the RUNNING platform so the assertion does not silently depend
    // on the suite executing on Linux.
    const key = platformKey(process.platform, process.arch)
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({
        version: 'v2.0.0',
        tag: 'v2.0.0',
        downloads: { [key]: ['https://mirror/full.zip'] },
        payloads: { [key]: ['https://mirror/payload.zip', 'https://github.com/payload.zip'] },
      })),
    )

    const info = await fetchDesktopLatest('https://example.com:20000')
    expect(info!.payloadUrls).toEqual([
      'https://mirror/payload.zip',
      'https://github.com/payload.zip',
    ])
    expect(info!.urls).toEqual(['https://mirror/full.zip'])
  })

  it('reports no payload for a platform the server does not publish one for', async () => {
    // macOS: the server omits the key entirely. That must read as "download the
    // full package", not as an error or an update-less state.
    const key = platformKey(process.platform, process.arch)
    const other = key === 'win32-x64' ? 'linux-x64' : 'win32-x64'
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({
        version: 'v2.0.0',
        tag: 'v2.0.0',
        downloads: { [key]: ['https://mirror/full.zip'] },
        payloads: { [other]: ['https://mirror/payload.zip'] },
      })),
    )

    const info = await fetchDesktopLatest('https://example.com:20000')
    expect(info!.payloadUrls).toEqual([])
  })

  it('reports no payload against a server that predates the field', async () => {
    // Backward compatibility: an older server returns no `payloads` at all.
    const key = platformKey(process.platform, process.arch)
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({
        version: 'v2.0.0',
        tag: 'v2.0.0',
        downloads: { [key]: ['https://mirror/full.zip'] },
      })),
    )

    const info = await fetchDesktopLatest('https://example.com:20000')
    expect(info!.payloadUrls).toEqual([])
  })
})

describe('classifyFetchFailure', () => {
  it('separates a changed certificate from an untrusted one and from a dead server', () => {
    // All three end in "no update offered"; only the log can tell them apart,
    // and each points at a different fix.
    const pin = Object.assign(new Error('certificate does not match the pinned one'), {
      name: 'CertificatePinMismatchError',
    })
    expect(classifyFetchFailure(pin)).toContain('pinned certificate mismatch')

    expect(classifyFetchFailure(new Error('self signed certificate in certificate chain'))).toContain(
      'TLS verification failed',
    )
    expect(classifyFetchFailure(new Error('connect ECONNREFUSED 192.168.1.50:20000'))).toContain(
      'server unreachable',
    )
    // Anything else is passed through unchanged rather than mislabelled.
    expect(classifyFetchFailure(new Error('unexpected thing'))).toBe('unexpected thing')
  })
})
