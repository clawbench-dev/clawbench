import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { detectPlatformKey } from '../useDesktopDownload'

const originalUA = navigator.userAgent
const originalUserAgentData = (navigator as unknown as { userAgentData?: unknown }).userAgentData

function setUA(ua: string, arch?: string) {
  Object.defineProperty(navigator, 'userAgent', { value: ua, configurable: true })
  Object.defineProperty(navigator, 'userAgentData', { value: arch ? { architecture: arch } : undefined, configurable: true })
}

const mockIsWebApp = ref(true)
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: ref(false), isDesktopApp: { value: false } }),
}))

// Spread the REAL module so newly-added exports (isMobileOSUA) stay defined —
// a hand-written whitelist silently breaks every test in this file the moment
// the subject imports one more helper. Only the two things this file needs to
// control are overridden.
vi.mock('@/composables/usePlatformDetect', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/usePlatformDetect')>()
  return {
    ...actual,
    // The UA constants are captured at module load, so a test that rewrites
    // navigator.userAgent afterwards still sees the original values. Keep the
    // real ones (jsdom = desktop) and let each case set the UA it needs; only
    // the host axis is stubbed.
    usePlatformDetect: () => ({ isWebApp: mockIsWebApp }),
  }
})

const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
}))

const mockDownloadByUrl = vi.fn()
vi.mock('@/utils/download', () => ({
  downloadByUrl: (...args: any[]) => mockDownloadByUrl(...args),
}))

beforeEach(() => { setUA(originalUA) })
afterEach(() => {
  Object.defineProperty(navigator, 'userAgent', { value: originalUA, configurable: true })
  Object.defineProperty(navigator, 'userAgentData', { value: originalUserAgentData, configurable: true })
  vi.restoreAllMocks()
  vi.resetModules()
})

describe('detectPlatformKey', () => {
  it('detects windows', () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    expect(detectPlatformKey()).toBe('win32-x64')
  })
  it('detects intel mac', () => {
    setUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')
    expect(detectPlatformKey()).toBe('darwin-x64')
  })
  it('detects apple silicon mac via userAgentData', () => {
    setUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 'arm')
    expect(detectPlatformKey()).toBe('darwin-arm64')
  })
  it('detects linux x64', () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    expect(detectPlatformKey()).toBe('linux-x64')
  })
  it('detects linux arm64 via UA regex', () => {
    setUA('Mozilla/5.0 (X11; Linux aarch64)')
    expect(detectPlatformKey()).toBe('linux-arm64')
  })
  it('returns empty for mobile', () => {
    setUA('Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X)')
    expect(detectPlatformKey()).toBe('')
  })
  // The iPadOS desktop-mode case (Macintosh UA + maxTouchPoints > 0) cannot be
  // exercised here: `isMobileOSUA` is captured when usePlatformDetect loads, so
  // rewriting navigator.userAgent inside this file has no effect. That
  // regression is pinned in usePlatformDetect.test.ts instead, where the module
  // is re-imported per case.
})

describe('useDesktopDownload', () => {
  beforeEach(() => {
    mockIsWebApp.value = true
    mockApiGet.mockReset()
    mockDownloadByUrl.mockReset()
  })

  it('marks isDesktop true in web mode on a desktop UA', async () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { isDesktop } = useDesktopDownload()
    expect(isDesktop).toBe(true)
  })

  it('marks isDesktop false in a native host (no download offer)', async () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    mockIsWebApp.value = false
    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { isDesktop } = useDesktopDownload()
    expect(isDesktop).toBe(false)
  })

  it('loadLatest fetches and stores latest when on desktop', async () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    const latest = { version: '1.2.3', downloads: { 'linux-x64': ['/dl/linux.tgz'] } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, latest: latestRef, loading } = useDesktopDownload()

    const promise = loadLatest()
    expect(loading.value).toBe(true)
    await promise

    expect(mockApiGet).toHaveBeenCalledWith('/api/desktop/latest')
    expect(latestRef.value).toEqual(latest)
    expect(loading.value).toBe(false)
  })

  it('loadLatest sets latest null and clears loading on error', async () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    mockApiGet.mockRejectedValue(new Error('Network error'))

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, latest: latestRef, loading } = useDesktopDownload()

    await loadLatest()

    expect(latestRef.value).toBeNull()
    expect(loading.value).toBe(false)
  })

  it('loadLatest does nothing when not on desktop', async () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    mockIsWebApp.value = false
    mockApiGet.mockResolvedValue({ version: '1.0', downloads: {} })

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, latest: latestRef, loading } = useDesktopDownload()

    await loadLatest()

    expect(mockApiGet).not.toHaveBeenCalled()
    expect(latestRef.value).toBeNull()
    expect(loading.value).toBe(false)
  })

  it('currentDownloadUrl returns the URL for the detected platform key', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const latest = { version: '1.0', downloads: { 'win32-x64': ['/dl/win.tgz'] } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, currentDownloadUrl } = useDesktopDownload()
    await loadLatest()

    expect(currentDownloadUrl()).toBe('/dl/win.tgz')
  })

  it('currentDownloadUrl returns empty string when no latest loaded', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { currentDownloadUrl } = useDesktopDownload()
    expect(currentDownloadUrl()).toBe('')
  })

  it('currentDownloadUrl returns empty string when platform not detected', async () => {
    setUA('Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X)')
    const latest = { version: '1.0', downloads: { 'win32-x64': ['/dl/win.tgz'] } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, currentDownloadUrl } = useDesktopDownload()
    await loadLatest()

    expect(currentDownloadUrl()).toBe('')
  })

  it('currentDownloadUrl returns empty string when key missing from downloads', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const latest = { version: '1.0', downloads: { 'linux-x64': ['/dl/linux.tgz'] } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, currentDownloadUrl } = useDesktopDownload()
    await loadLatest()

    expect(currentDownloadUrl()).toBe('')
  })

  it('downloadDesktop calls downloadByUrl with versioned filename', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const latest = { version: '2.0.0', downloads: { 'win32-x64': ['/dl/win.tgz'] } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, downloadDesktop } = useDesktopDownload()
    await loadLatest()

    downloadDesktop()
    expect(mockDownloadByUrl).toHaveBeenCalledWith('/dl/win.tgz', 'clawbench-desktop-2.0.0.zip')
  })

  it('downloadDesktop falls back to "latest" version when version missing', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const latest = { version: '', downloads: { 'win32-x64': ['/dl/win.tgz'] } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, downloadDesktop } = useDesktopDownload()
    await loadLatest()

    downloadDesktop()
    expect(mockDownloadByUrl).toHaveBeenCalledWith('/dl/win.tgz', 'clawbench-desktop-latest.zip')
  })

  it('downloadDesktop does nothing when no download URL available', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const latest = { version: '1.0', downloads: { 'linux-x64': ['/dl/linux.tgz'] } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, downloadDesktop } = useDesktopDownload()
    await loadLatest()

    downloadDesktop()
    expect(mockDownloadByUrl).not.toHaveBeenCalled()
  })

  // The server returns an ordered candidate list (mirror first in China,
  // github.com last). We take the first entry — the server has already ordered
  // them for the region, so re-ordering here would fight it.
  it('uses the first candidate URL when the server offers several', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    mockApiGet.mockResolvedValue({
      version: '2.0.0',
      tag: 'v2.0.0',
      downloads: { 'win32-x64': ['https://mirror/x.zip', 'https://github.com/x.zip'] },
    })

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, currentDownloadUrl, currentDownloadUrls } = useDesktopDownload()
    await loadLatest()

    expect(currentDownloadUrls()).toEqual(['https://mirror/x.zip', 'https://github.com/x.zip'])
    expect(currentDownloadUrl()).toBe('https://mirror/x.zip')
  })

  // A dev server answers 200 with an empty downloads map. That must read as
  // "nothing to offer", not crash on a missing key.
  it('handles a dev-server response with no downloads', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    mockApiGet.mockResolvedValue({ version: 'dev', tag: '', downloads: {} })

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, currentDownloadUrl } = useDesktopDownload()
    await loadLatest()

    expect(currentDownloadUrl()).toBe('')
  })

  it('handles a response with downloads missing entirely', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    mockApiGet.mockResolvedValue({ version: 'dev' })

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, currentDownloadUrl } = useDesktopDownload()
    await loadLatest()

    expect(currentDownloadUrl()).toBe('')
  })
})
