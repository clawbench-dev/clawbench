import { describe, it, expect, vi } from 'vitest'

vi.mock('electron', () => ({
  app: { getVersion: () => '1.0.0', isPackaged: true },
}))

vi.mock('./install', () => ({
  httpGetBuffer: vi.fn(),
}))

import { platformKey, latestInfoUrl, fetchDesktopLatest } from './updater'
import { httpGetBuffer } from './install'

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
