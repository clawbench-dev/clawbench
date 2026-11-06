import { computed, onMounted, onUnmounted, ref, shallowRef } from 'vue'
import { getNative, getNativeHostPlatform } from '@/utils/clawbenchNative'
import { onAppResume } from '@/composables/useAppForeground'

export type NativeNotificationPermission = 'unknown' | 'granted' | 'denied'

export interface NativeNotificationStatus {
  enabled: boolean
  connected: boolean
  permission: NativeNotificationPermission
  error: boolean
  backgroundDelivery: 'while-process-running' | 'unknown'
}

const STATUS_EVENT = 'clawbench-native-notification-status'

function parseStatus(value: unknown): NativeNotificationStatus | null {
  try {
    if (typeof value === 'string') {
      if (value.length > 4096) return null
      value = JSON.parse(value)
    }
    if (!value || typeof value !== 'object' || Array.isArray(value)) return null
    const status = value as Record<string, unknown>
    if (typeof status.enabled !== 'boolean' || typeof status.connected !== 'boolean'
      || typeof status.error !== 'string') return null
    return {
      enabled: status.enabled,
      connected: status.connected,
      permission: status.permission === 'granted' || status.permission === 'denied'
        ? status.permission : 'unknown',
      error: status.error.length > 0,
      backgroundDelivery: status.backgroundDelivery === 'while-process-running'
        ? 'while-process-running' : 'unknown',
    }
  } catch {
    return null
  }
}

export function useNativeNotificationStatus() {
  const native = getNative()
  const supported = getNativeHostPlatform() === 'harmonyos'
    && typeof native?.getNativePushStatus === 'function'
  const status = shallowRef<NativeNotificationStatus | null>(null)
  const readError = ref(false)
  const refreshing = ref(false)
  let active = false
  let revision = 0
  let removeResumeListener: (() => void) | undefined

  async function refresh(): Promise<void> {
    if (!active || !supported) return
    const requestRevision = ++revision
    refreshing.value = true
    try {
      const next = parseStatus(await native!.getNativePushStatus!())
      if (!active || revision !== requestRevision) return
      if (!next) throw new Error('Invalid native notification status')
      status.value = next
      readError.value = false
    } catch {
      if (active && revision === requestRevision) readError.value = true
    } finally {
      if (active && revision === requestRevision) refreshing.value = false
    }
  }

  function onStatus(event: Event): void {
    const next = parseStatus((event as CustomEvent<unknown>).detail)
    if (!active || !next) return
    // A pushed change is newer than any read already in flight.
    revision++
    status.value = next
    readError.value = false
    refreshing.value = false
  }

  onMounted(() => {
    if (!supported) return
    active = true
    window.addEventListener(STATUS_EVENT, onStatus)
    removeResumeListener = onAppResume(() => { void refresh() })
    void refresh()
  })

  onUnmounted(() => {
    active = false
    revision++
    window.removeEventListener(STATUS_EVENT, onStatus)
    removeResumeListener?.()
  })

  const permissionLabelKey = computed(() => {
    if (readError.value) return 'settings.items.nativeNotificationUnavailable'
    if (!status.value) return 'common.loading'
    if (status.value.permission === 'granted') return 'settings.items.nativeNotificationGranted'
    if (status.value.permission === 'denied') return 'settings.items.nativeNotificationDenied'
    return 'settings.items.nativeNotificationUnknown'
  })
  const deliveryLabelKey = computed(() => {
    if (readError.value) return 'settings.items.nativeNotificationUnavailable'
    if (!status.value) return 'common.loading'
    if (status.value.permission === 'denied') return 'settings.items.nativeNotificationBlocked'
    if (status.value.error) return 'settings.items.nativeNotificationUnavailable'
    if (!status.value.enabled) return 'settings.items.nativeNotificationDisabled'
    if (status.value.connected) return 'settings.items.nativeNotificationConnected'
    // The native socket intentionally stops while the page is in the foreground.
    return 'settings.items.nativeNotificationReady'
  })

  return {
    supported, status, readError, refreshing, refresh, permissionLabelKey, deliveryLabelKey,
  }
}
