import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import NativeNotificationStatus from '../NativeNotificationStatus.vue'
import type { ClawBenchNative } from '@/utils/clawbenchNative'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

const resumeListeners = vi.hoisted(() => new Set<() => void>())
const language = vi.hoisted(() => ({ locale: 'zh' }))
vi.mock('@/composables/useAppForeground', () => ({
  onAppResume: (callback: () => void) => {
    resumeListeners.add(callback)
    return () => resumeListeners.delete(callback)
  },
}))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      const messages: unknown = language.locale === 'en' ? en : zh
      return key.split('.').reduce<unknown>((value, part) =>
        value && typeof value === 'object' ? (value as Record<string, unknown>)[part] : undefined, messages) ?? key
    },
  }),
}))

const bridgeWindow = window as Window & {
  ClawBenchHarmony?: Partial<ClawBenchNative>
  ClawBenchNative?: Partial<ClawBenchNative>
}
const originalHarmony = bridgeWindow.ClawBenchHarmony
const originalNative = bridgeWindow.ClawBenchNative
const statusEvent = 'clawbench-native-notification-status'
const allowed = {
  enabled: true, connected: false, permission: 'granted', error: '',
  backgroundDelivery: 'while-process-running',
}
const denied = { ...allowed, enabled: false, permission: 'denied', error: 'Permission was denied' }
const readStatus = vi.fn()
let wrappers: VueWrapper[] = []

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

function renderStatus(locale: 'zh' | 'en' = 'zh') {
  language.locale = locale
  const wrapper = mount(NativeNotificationStatus)
  wrappers.push(wrapper)
  return wrapper
}

function values(wrapper: VueWrapper): string[] {
  return wrapper.findAll('dd').map(row => row.text())
}

function pushStatus(detail: unknown) {
  window.dispatchEvent(new CustomEvent(statusEvent, { detail }))
}

beforeEach(() => {
  readStatus.mockReset().mockResolvedValue(JSON.stringify(allowed))
  delete bridgeWindow.ClawBenchNative
  bridgeWindow.ClawBenchHarmony = {
    isNativeApp: () => true,
    getHostPlatform: () => 'harmonyos',
    getNativePushStatus: readStatus,
  }
})

afterEach(() => {
  wrappers.forEach(wrapper => wrapper.unmount())
  wrappers = []
  expect(resumeListeners.size).toBe(0)
  if (originalHarmony) bridgeWindow.ClawBenchHarmony = originalHarmony
  else delete bridgeWindow.ClawBenchHarmony
  if (originalNative) bridgeWindow.ClawBenchNative = originalNative
  else delete bridgeWindow.ClawBenchNative
  vi.restoreAllMocks()
})

describe('NativeNotificationStatus', () => {
  it('renders loading until the host answers, then shows foreground readiness without a connection error', async () => {
    const pending = deferred<string>()
    readStatus.mockReturnValueOnce(pending.promise)
    const wrapper = renderStatus()
    expect(values(wrapper)).toEqual([zh.common.loading, zh.common.loading])
    expect(wrapper.find('[role="status"]').attributes('aria-live')).toBe('polite')
    expect(wrapper.findAll('p')).toHaveLength(0)

    pending.resolve(JSON.stringify(allowed))
    await flushPromises()
    expect(values(wrapper)).toEqual([
      zh.settings.items.nativeNotificationGranted,
      zh.settings.items.nativeNotificationReady,
    ])
    expect(wrapper.findAll('dt').map(row => row.text())).toEqual([
      zh.settings.items.nativeNotificationPermission,
      zh.settings.items.nativeNotificationDelivery,
    ])
    expect(wrapper.find('p').text()).toBe(zh.settings.items.nativeNotificationProcessLimit)
    expect(wrapper.text()).not.toContain(zh.settings.items.nativeNotificationUnavailable)
    expect(wrapper.findAll('button, input, select')).toHaveLength(0)
  })

  it.each(['zh', 'en'] as const)('updates denied permission to an active connection in %s', async (locale) => {
    readStatus.mockResolvedValueOnce(JSON.stringify(denied))
    const wrapper = renderStatus(locale)
    const messages = locale === 'en' ? en.settings.items : zh.settings.items
    await flushPromises()
    expect(values(wrapper)).toEqual([messages.nativeNotificationDenied, messages.nativeNotificationBlocked])
    expect(wrapper.findAll('p').map(row => row.text())).toEqual([
      messages.nativeNotificationDeniedHint, messages.nativeNotificationProcessLimit,
    ])

    pushStatus({ ...allowed, connected: true })
    await flushPromises()
    expect(values(wrapper)).toEqual([messages.nativeNotificationGranted, messages.nativeNotificationConnected])
    expect(wrapper.findAll('p').map(row => row.text())).toEqual([messages.nativeNotificationProcessLimit])
  })

  it('shows delivery disabled separately from permission granted', async () => {
    readStatus.mockResolvedValueOnce(JSON.stringify({ ...allowed, enabled: false }))
    const wrapper = renderStatus()
    await flushPromises()
    expect(values(wrapper)).toEqual([
      zh.settings.items.nativeNotificationGranted, zh.settings.items.nativeNotificationDisabled,
    ])
    expect(wrapper.text()).not.toContain(zh.settings.items.nativeNotificationDeniedHint)
    expect(wrapper.text()).not.toContain(zh.settings.items.nativeNotificationConnected)
  })

  it('renders an unknown native permission with a service error without inventing a rejection', async () => {
    readStatus.mockResolvedValueOnce(JSON.stringify({
      ...allowed, enabled: false, permission: 'unknown', error: 'System service unavailable',
      backgroundDelivery: 'unknown',
    }))
    const wrapper = renderStatus()
    await flushPromises()
    expect(values(wrapper)).toEqual([
      zh.settings.items.nativeNotificationUnknown, zh.settings.items.nativeNotificationUnavailable,
    ])
    expect(wrapper.findAll('p')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('System service unavailable')
  })

  it('does not show a stale permission-denied hint if a resumed read fails, and recovers on a push', async () => {
    readStatus.mockResolvedValueOnce(JSON.stringify(denied))
    const wrapper = renderStatus()
    await flushPromises()
    expect(wrapper.text()).toContain(zh.settings.items.nativeNotificationDeniedHint)

    readStatus.mockRejectedValueOnce(new Error('Bridge unavailable'))
    resumeListeners.forEach(callback => callback())
    await flushPromises()
    expect(values(wrapper)).toEqual([
      zh.settings.items.nativeNotificationUnavailable, zh.settings.items.nativeNotificationUnavailable,
    ])
    expect(wrapper.text()).not.toContain(zh.settings.items.nativeNotificationDeniedHint)
    expect(wrapper.text()).not.toContain('Bridge unavailable')
    pushStatus(JSON.stringify(allowed))
    await flushPromises()
    expect(values(wrapper)).toEqual([
      zh.settings.items.nativeNotificationGranted, zh.settings.items.nativeNotificationReady,
    ])
  })

  it.each(['web', 'android', 'legacy-harmony'])('renders no status UI or subscriptions for %s', async (platform) => {
    if (platform === 'web') delete bridgeWindow.ClawBenchHarmony
    else if (platform === 'android') {
      delete bridgeWindow.ClawBenchHarmony
      bridgeWindow.ClawBenchNative = { isNativeApp: () => true, getNativePushStatus: readStatus }
    } else {
      delete bridgeWindow.ClawBenchHarmony!.getNativePushStatus
    }
    const wrapper = renderStatus()
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.text()).toBe('')
    expect(readStatus).not.toHaveBeenCalled()
    expect(resumeListeners.size).toBe(0)
  })

  it('unsubscribes the real component and leaves no effect for a late bridge response', async () => {
    const addListener = vi.spyOn(window, 'addEventListener')
    const removeListener = vi.spyOn(window, 'removeEventListener')
    const pending = deferred<string>()
    readStatus.mockReturnValueOnce(pending.promise)
    const wrapper = renderStatus()
    const listener = addListener.mock.calls.find(([name]) => name === statusEvent)?.[1]
    expect(listener).toBeTypeOf('function')
    expect(resumeListeners.size).toBe(1)
    const element = wrapper.element
    const before = element.textContent
    wrapper.unmount()
    expect(removeListener).toHaveBeenCalledWith(statusEvent, listener)
    expect(resumeListeners.size).toBe(0)

    pushStatus(denied)
    pending.resolve(JSON.stringify(allowed))
    await flushPromises()
    expect(element.textContent).toBe(before)
    expect(readStatus).toHaveBeenCalledTimes(1)
  })
})
