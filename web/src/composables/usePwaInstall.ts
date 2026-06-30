import { ref, computed } from 'vue'
import { useAppMode } from './useAppMode'
import { appLog } from '@/utils/appLog'

const TAG = 'PwaInstall'

interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<{ outcome: 'accepted' | 'dismissed' }>
}

// Module-level singleton — all consumers share the same deferred prompt & installed state
const deferredPrompt = ref<BeforeInstallPromptEvent | null>(null)
const installed = ref(false)

// UA detection (only meaningful in web browser — not APP mode)
const ua = typeof navigator !== 'undefined' ? navigator.userAgent : ''
const isAndroidUA = /Android/i.test(ua) && !/Windows Phone/i.test(ua)
const isIOSUA = /iPhone|iPad|iPod/i.test(ua)

// Listen for beforeinstallprompt / appinstalled (once, module-level)
if (typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (e: Event) => {
    e.preventDefault()
    deferredPrompt.value = e as BeforeInstallPromptEvent
    appLog.d(TAG, 'beforeinstallprompt captured')
  })
  window.addEventListener('appinstalled', () => {
    installed.value = true
    deferredPrompt.value = null
    appLog.i(TAG, 'PWA installed')
  })
}

/**
 * Composable for PWA install and APK download functionality.
 *
 * Platform visibility rules:
 * - Android browser: show PWA install + APK download
 * - iOS browser: show PWA install (manual iOS steps)
 * - Desktop browser: show PWA install (if beforeinstallprompt available)
 * - APP mode (native Android): show nothing
 */
export function usePwaInstall() {
  const { isAppMode } = useAppMode()

  const isAndroid = computed(() => isAndroidUA && !isAppMode.value)
  const isIOS = computed(() => isIOSUA && !isAppMode.value)
  const isMobile = computed(() => (isAndroidUA || isIOSUA) && !isAppMode.value)

  const canInstallPwa = computed(() => !!deferredPrompt.value)
  // Show PWA install on all platforms except native APP mode
  // Desktop Chrome/Edge: beforeinstallprompt triggers canInstallPwa
  // iOS: no beforeinstallprompt, but isIOSUA is true → show manual steps
  const showPwaInstall = computed(() => !isAppMode.value && !installed.value && (canInstallPwa.value || isIOSUA))
  const showApkDownload = computed(() => isAndroid.value && !installed.value)

  async function installPwa(): Promise<boolean> {
    if (!deferredPrompt.value) return false
    try {
      const result = await deferredPrompt.value.prompt()
      deferredPrompt.value = null
      const accepted = result.outcome === 'accepted'
      appLog.i(TAG, accepted ? 'PWA install accepted' : 'PWA install dismissed')
      return accepted
    } catch (err) {
      appLog.e(TAG, 'PWA install prompt failed', err)
      deferredPrompt.value = null
      return false
    }
  }

  return {
    isAndroid,
    isIOS,
    isMobile,
    showPwaInstall,
    showApkDownload,
    canInstallPwa,
    installPwa,
  }
}
