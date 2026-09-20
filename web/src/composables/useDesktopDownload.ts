import { ref } from 'vue'
import { useAppMode } from './useAppMode'
import { isAndroidUA, isIOSUA } from './usePlatformDetect'
import { apiGet } from '@/utils/api'
import { downloadByUrl } from '@/utils/download'

interface DesktopLatest {
  version: string
  downloads: Record<string, string>
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

  function currentDownloadUrl(): string {
    const key = detectPlatformKey()
    if (!key || !latest.value) return ''
    return latest.value.downloads[key] || ''
  }

  function downloadDesktop(): void {
    const url = currentDownloadUrl()
    if (!url) return
    downloadByUrl(url, `clawbench-desktop-${latest.value?.version || 'latest'}.tgz`)
  }

  return { latest, loading, isDesktop, loadLatest, currentDownloadUrl, downloadDesktop }
}
