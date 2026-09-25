import { describe, it, expect, vi } from 'vitest'

vi.mock('electron', () => ({
  app: { getVersion: () => '1.0.0' },
}))

vi.mock('./install', () => ({
  httpGetBuffer: vi.fn(),
}))

import { compareVersions, platformKey, latestInfoUrl, checkForUpdate } from './updater'
import { httpGetBuffer } from './install'
import { getStore } from './store'

vi.mock('./store', () => ({
  getStore: vi.fn(),
}))

describe('compareVersions', () => {
  it('orders by each numeric segment, not lexically', () => {
    // The whole point: a naive string compare puts "1.10.0" before "1.9.0".
    expect(compareVersions('1.10.0', '1.9.0')).toBeGreaterThan(0)
    expect(compareVersions('1.9.0', '1.10.0')).toBeLessThan(0)
  })

  it('treats equal versions as equal', () => {
    expect(compareVersions('1.2.3', '1.2.3')).toBe(0)
  })

  it('pads missing segments with zero', () => {
    expect(compareVersions('1.2', '1.2.0')).toBe(0)
    expect(compareVersions('1.2.1', '1.2')).toBeGreaterThan(0)
  })

  it('reports a downgrade as NOT an update', () => {
    // Regression guard: an inequality check reported "update available" when
    // the server was older than the running build.
    expect(compareVersions('1.0.0', '1.2.0')).toBeLessThan(0)
  })

  it('handles major/minor/patch boundaries', () => {
    expect(compareVersions('2.0.0', '1.99.99')).toBeGreaterThan(0)
    expect(compareVersions('1.0.1', '1.0.0')).toBeGreaterThan(0)
  })

  it('compares v-prefixed versions, which is how the server reports them', () => {
    // Regression guard: the server sends `v0.99.1` (git describe) while
    // app.getVersion() sends `0.99.1`. Without normalization parseInt('v1') is
    // NaN -> 0, so compareVersions('v1.0.0', '0.99.1') was negative and the
    // update check would go permanently silent at the v1.0.0 release.
    expect(compareVersions('v1.0.0', '0.99.1')).toBeGreaterThan(0)
    expect(compareVersions('v0.99.2', '0.99.1')).toBeGreaterThan(0)
    expect(compareVersions('v0.99.1', '0.99.1')).toBe(0)
    expect(compareVersions('v0.99.0', '0.99.1')).toBeLessThan(0)
  })
})

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

describe('checkForUpdate', () => {
  const mockStore = (serverUrl: string) => {
    ;(getStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      get: (k: string) => (k === 'serverUrl' ? serverUrl : undefined),
    })
  }

  it('offers the update when the server reports a newer version', async () => {
    mockStore('https://example.com:20000')
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({
        version: '2.0.0',
        tag: 'v2.0.0',
        downloads: { 'linux-x64': ['https://mirror/x.zip', 'https://github.com/x.zip'] },
      })),
    )

    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(true)
    expect(info.version).toBe('2.0.0')
    // Order is preserved so the client tries the mirror before github.com.
    expect(info.urls).toEqual(['https://mirror/x.zip', 'https://github.com/x.zip'])
  })

  it('does not offer an update when the server is on the same version', async () => {
    mockStore('https://example.com:20000')
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({ version: '1.0.0', tag: 'v1.0.0', downloads: { 'linux-x64': ['https://x'] } })),
    )
    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(false)
  })

  it('does not offer a downgrade', async () => {
    mockStore('https://example.com:20000')
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({ version: '0.9.0', tag: 'v0.9.0', downloads: { 'linux-x64': ['https://x'] } })),
    )
    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(false)
  })

  it('offers nothing when the server has no release for this platform', async () => {
    // A dev server answers with an empty downloads map; there is nothing to
    // install even though the version string differs.
    mockStore('https://example.com:20000')
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({ version: 'dev', tag: '', downloads: {} })),
    )
    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(false)
    expect(info.urls).toEqual([])
  })

  it('offers nothing when the server is unreachable', async () => {
    mockStore('https://example.com:20000')
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('ECONNREFUSED'))
    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(false)
  })

  it('offers nothing when no server is configured', async () => {
    mockStore('')
    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(false)
  })

  it('offers nothing on a malformed response', async () => {
    mockStore('https://example.com:20000')
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(Buffer.from('<html>not json</html>'))
    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(false)
  })

  it('surfaces the payload URLs for the running platform', async () => {
    // Keyed off the RUNNING platform so the assertion does not silently depend
    // on the suite executing on Linux.
    const key = platformKey(process.platform, process.arch)
    mockStore('https://example.com:20000')
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({
        version: '2.0.0',
        tag: 'v2.0.0',
        downloads: { [key]: ['https://mirror/full.zip'] },
        payloads: { [key]: ['https://mirror/payload.zip', 'https://github.com/payload.zip'] },
      })),
    )

    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(true)
    // Order preserved so the client tries the mirror before github.com.
    expect(info.payloadUrls).toEqual([
      'https://mirror/payload.zip',
      'https://github.com/payload.zip',
    ])
    expect(info.urls).toEqual(['https://mirror/full.zip'])
  })

  it('reports no payload for a platform the server does not publish one for', async () => {
    // macOS: the server omits the key entirely. That must read as "download the
    // full package", not as an error or an update-less state.
    const key = platformKey(process.platform, process.arch)
    const other = key === 'win32-x64' ? 'linux-x64' : 'win32-x64'
    mockStore('https://example.com:20000')
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({
        version: '2.0.0',
        tag: 'v2.0.0',
        downloads: { [key]: ['https://mirror/full.zip'] },
        payloads: { [other]: ['https://mirror/payload.zip'] },
      })),
    )

    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(true)
    expect(info.payloadUrls).toEqual([])
  })

  it('reports no payload against a server that predates the field', async () => {
    // Backward compatibility: an older server returns no `payloads` at all.
    const key = platformKey(process.platform, process.arch)
    mockStore('https://example.com:20000')
    ;(httpGetBuffer as ReturnType<typeof vi.fn>).mockResolvedValue(
      Buffer.from(JSON.stringify({
        version: '2.0.0',
        tag: 'v2.0.0',
        downloads: { [key]: ['https://mirror/full.zip'] },
      })),
    )

    const info = await checkForUpdate()
    expect(info.hasUpdate).toBe(true)
    expect(info.payloadUrls).toEqual([])
  })
})
