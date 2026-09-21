import { ref } from 'vue'
import { useAppMode } from './useAppMode'
import { isAndroidUA, isIOSUA } from './usePlatformDetect'
import { apiGet } from '@/utils/api'
import { downloadByUrl } from '@/utils/download'

interface DesktopLatest {
  version: string
  /** GitHub release tag; empty on a dev server (downloads is then empty too). */
  tag: string
  /**
   * Platform key -> candidate URLs, best-first. The server orders mirrors ahead
   * of github.com for mainland China; we just take the first entry.
   */
  downloads: Record<string, string[]>
}

/** Detect the current desktop OS+arch platform key, mirroring spec §8.1. */
export function detectPlatformKey(): string {
  const ua = navigator.userAgent
  const archHint = (navigator as unknown as { userAgentData?: { platform?: string; architecture?: string } }).userAgentData
  if (/Windows/i.test(ua)) return 'win32-x64'
  if (/Macintosh/i.test(ua)) {
    const isArm = archHint?.architecture === 'arm' || /arm64|aarch64/i.test(ua)
    return isArm ? 'darwin-arm64' : 'darwin-x64'
  }
  if (/Linux/i.test(ua)) {
    const isArm = archHint?.architecture === 'arm' || /arm64|aarch64/i.test(ua)
    return isArm ? 'linux-arm64' : 'linux-x64'
  }
  return ''
}

export function useDesktopDownload() {
  const { isAppMode } = useAppMode()
  const latest = ref<DesktopLatest | null>(null)
  const loading = ref(false)

  const isDesktop = !isAppMode.value && !isAndroidUA && !isIOSUA

  async function loadLatest(): Promise<void> {
    if (!isDesktop) return
    loading.value = true
    try {
      const data = await apiGet<DesktopLatest>('/api/desktop/latest')
      latest.value = data
    } catch {
      latest.value = null
    } finally {
      loading.value = false
    }
  }

  /** The platform's candidate URLs, best-first (empty when none apply). */
  function currentDownloadUrls(): string[] {
    const key = detectPlatformKey()
    if (!key || !latest.value) return []
    return latest.value.downloads?.[key] ?? []
  }

  function currentDownloadUrl(): string {
    return currentDownloadUrls()[0] || ''
  }

  function downloadDesktop(): void {
    const url = currentDownloadUrl()
    if (!url) return
    // Release assets are zips (they wrap the unpacked app directory), so the
    // saved filename must end in .zip — a .tgz name would mislead the user and
    // make the file fail to open on double-click.
    const version = latest.value?.version || 'latest'
    downloadByUrl(url, `clawbench-desktop-${version}.zip`)
  }

  return { latest, loading, isDesktop, loadLatest, currentDownloadUrl, currentDownloadUrls, downloadDesktop }
}
