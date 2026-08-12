import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { detectPlatformKey } from '../useDesktopDownload'

const originalUA = navigator.userAgent
const originalUserAgentData = (navigator as unknown as { userAgentData?: unknown }).userAgentData

function setUA(ua: string, arch?: string) {
  Object.defineProperty(navigator, 'userAgent', { value: ua, configurable: true })
  Object.defineProperty(navigator, 'userAgentData', { value: arch ? { architecture: arch } : undefined, configurable: true })
}

const mockIsAppMode = ref(false)
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: mockIsAppMode }),
}))

vi.mock('@/composables/usePlatformDetect', () => ({
  isAndroidUA: false,
  isIOSUA: false,
  isIPadOSUA: false,
  usePlatformDetect: () => ({ isPC: ref(true) }),
}))

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
})

describe('useDesktopDownload', () => {
  beforeEach(() => {
    mockIsAppMode.value = false
    mockApiGet.mockReset()
    mockDownloadByUrl.mockReset()
  })

  it('marks isDesktop true in web mode on a desktop UA', async () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { isDesktop } = useDesktopDownload()
    expect(isDesktop).toBe(true)
  })

  it('marks isDesktop false in app mode', async () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    mockIsAppMode.value = true
    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { isDesktop } = useDesktopDownload()
    expect(isDesktop).toBe(false)
  })

  it('loadLatest fetches and stores latest when on desktop', async () => {
    setUA('Mozilla/5.0 (X11; Linux x86_64)')
    const latest = { version: '1.2.3', downloads: { 'linux-x64': '/dl/linux.tgz' } }
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
    mockIsAppMode.value = true
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
    const latest = { version: '1.0', downloads: { 'win32-x64': '/dl/win.tgz' } }
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
    const latest = { version: '1.0', downloads: { 'win32-x64': '/dl/win.tgz' } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, currentDownloadUrl } = useDesktopDownload()
    await loadLatest()

    expect(currentDownloadUrl()).toBe('')
  })

  it('currentDownloadUrl returns empty string when key missing from downloads', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const latest = { version: '1.0', downloads: { 'linux-x64': '/dl/linux.tgz' } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, currentDownloadUrl } = useDesktopDownload()
    await loadLatest()

    expect(currentDownloadUrl()).toBe('')
  })

  it('downloadDesktop calls downloadByUrl with versioned filename', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const latest = { version: '2.0.0', downloads: { 'win32-x64': '/dl/win.tgz' } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, downloadDesktop } = useDesktopDownload()
    await loadLatest()

    downloadDesktop()
    expect(mockDownloadByUrl).toHaveBeenCalledWith('/dl/win.tgz', 'clawbench-desktop-2.0.0.tgz')
  })

  it('downloadDesktop falls back to "latest" version when version missing', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const latest = { version: '', downloads: { 'win32-x64': '/dl/win.tgz' } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, downloadDesktop } = useDesktopDownload()
    await loadLatest()

    downloadDesktop()
    expect(mockDownloadByUrl).toHaveBeenCalledWith('/dl/win.tgz', 'clawbench-desktop-latest.tgz')
  })

  it('downloadDesktop does nothing when no download URL available', async () => {
    setUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')
    const latest = { version: '1.0', downloads: { 'linux-x64': '/dl/linux.tgz' } }
    mockApiGet.mockResolvedValue(latest)

    const { useDesktopDownload } = await import('../useDesktopDownload')
    const { loadLatest, downloadDesktop } = useDesktopDownload()
    await loadLatest()

    downloadDesktop()
    expect(mockDownloadByUrl).not.toHaveBeenCalled()
  })
})
