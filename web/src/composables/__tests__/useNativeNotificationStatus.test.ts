import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { useNativeNotificationStatus } from '../useNativeNotificationStatus'
import NativeNotificationStatus from '@/components/settings/NativeNotificationStatus.vue'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

const mocks = vi.hoisted(() => ({
  getStatus: vi.fn(),
  platform: 'harmonyos',
  bridgeAvailable: true,
  resume: new Set<() => void>(),
  locale: 'zh',
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNativeHostPlatform: () => mocks.platform,
  getNative: () => mocks.bridgeAvailable ? { getNativePushStatus: mocks.getStatus } : undefined,
}))
vi.mock('@/composables/useAppForeground', () => ({
  onAppResume: (callback: () => void) => {
    mocks.resume.add(callback)
    return () => mocks.resume.delete(callback)
  },
}))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      const messages: unknown = mocks.locale === 'en' ? en : zh
      return key.split('.').reduce<unknown>((value, part) =>
        value && typeof value === 'object' ? (value as Record<string, unknown>)[part] : undefined, messages) ?? key
    },
  }),
}))

const eventName = 'clawbench-native-notification-status'
const allowed = {
  enabled: true, connected: false, permission: 'granted', error: '',
  backgroundDelivery: 'while-process-running',
}
const denied = { ...allowed, enabled: false, permission: 'denied', error: 'Permission was denied' }
let wrappers: VueWrapper[] = []

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

function mountStatus() {
  let result!: ReturnType<typeof useNativeNotificationStatus>
  const wrapper = mount(defineComponent({
    setup() {
      result = useNativeNotificationStatus()
      return () => h('div')
    },
  }))
  wrappers.push(wrapper)
  return { result, wrapper }
}

function pushStatus(detail: unknown) {
  window.dispatchEvent(new CustomEvent(eventName, { detail }))
}

beforeEach(() => {
  mocks.getStatus.mockReset().mockResolvedValue(JSON.stringify(allowed))
  mocks.platform = 'harmonyos'
  mocks.bridgeAvailable = true
  mocks.locale = 'zh'
  mocks.resume.clear()
})

afterEach(() => {
  wrappers.forEach(wrapper => wrapper.unmount())
  wrappers = []
  vi.restoreAllMocks()
})

describe('useNativeNotificationStatus', () => {
  it('reads confirmed permission without treating a foreground socket as disconnected', async () => {
    const { result } = mountStatus()
    await flushPromises()
    expect(result.status.value).toMatchObject({ enabled: true, connected: false, permission: 'granted' })
    expect(result.permissionLabelKey.value).toBe('settings.items.nativeNotificationGranted')
    expect(result.deliveryLabelKey.value).toBe('settings.items.nativeNotificationReady')
    expect(result.readError.value).toBe(false)
  })

  it('does not read or subscribe on Android, web, or an older Harmony host', async () => {
    for (const platform of ['android', 'web', 'harmonyos']) {
      mocks.platform = platform
      mocks.bridgeAvailable = platform !== 'harmonyos'
      const { result } = mountStatus()
      expect(result.supported).toBe(false)
    }
    await flushPromises()
    expect(mocks.getStatus).not.toHaveBeenCalled()
    expect(mocks.resume.size).toBe(0)
  })

  it('keeps a pushed denial when an older read resolves or rejects later', async () => {
    for (const reject of [false, true]) {
      const read = deferred<string>()
      mocks.getStatus.mockReturnValueOnce(read.promise)
      const { result, wrapper } = mountStatus()
      pushStatus(denied)
      if (reject) read.reject(new Error('stale request'))
      else read.resolve(JSON.stringify(allowed))
      await flushPromises()
      expect(result.status.value?.permission).toBe('denied')
      expect(result.deliveryLabelKey.value).toBe('settings.items.nativeNotificationBlocked')
      expect(result.readError.value).toBe(false)
      expect(result.refreshing.value).toBe(false)
      wrapper.unmount()
    }
  })

  it('lets the latest refresh win when multiple reads finish out of order', async () => {
    const first = deferred<string>()
    const second = deferred<string>()
    mocks.getStatus.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const { result } = mountStatus()
    const refreshing = result.refresh()
    second.resolve(JSON.stringify(denied))
    await refreshing
    first.resolve(JSON.stringify(allowed))
    await flushPromises()
    expect(result.status.value?.permission).toBe('denied')
  })

  it('refreshes authorization after returning from system settings', async () => {
    const { result } = mountStatus()
    await flushPromises()
    mocks.getStatus.mockResolvedValue(JSON.stringify(denied))
    mocks.resume.forEach(callback => callback())
    await flushPromises()
    expect(mocks.getStatus).toHaveBeenCalledTimes(2)
    expect(result.permissionLabelKey.value).toBe('settings.items.nativeNotificationDenied')
  })

  it('does not replace valid state with a malformed push', async () => {
    const { result } = mountStatus()
    await flushPromises()
    for (const detail of [null, 'not JSON', [], { enabled: 'true' }, { ...allowed, error: 42 }]) {
      pushStatus(detail)
    }
    expect(result.status.value?.permission).toBe('granted')
    expect(result.readError.value).toBe(false)
  })

  it('reports read failure without claiming permission was denied, then recovers', async () => {
    mocks.getStatus.mockResolvedValueOnce('invalid JSON')
    const { result } = mountStatus()
    await flushPromises()
    expect(result.status.value).toBeNull()
    expect(result.permissionLabelKey.value).toBe('settings.items.nativeNotificationUnavailable')
    pushStatus(JSON.stringify(allowed))
    expect(result.readError.value).toBe(false)
    expect(result.permissionLabelKey.value).toBe('settings.items.nativeNotificationGranted')
  })

  it('distinguishes unknown permission, native delivery off, and a transport failure', async () => {
    const { result } = mountStatus()
    await flushPromises()
    pushStatus({ ...allowed, enabled: false, permission: undefined })
    expect(result.permissionLabelKey.value).toBe('settings.items.nativeNotificationUnknown')
    expect(result.deliveryLabelKey.value).toBe('settings.items.nativeNotificationDisabled')
    pushStatus({ ...allowed, connected: false, error: 'connection failed' })
    expect(result.permissionLabelKey.value).toBe('settings.items.nativeNotificationGranted')
    expect(result.deliveryLabelKey.value).toBe('settings.items.nativeNotificationUnavailable')
    pushStatus({ ...allowed, connected: true })
    expect(result.deliveryLabelKey.value).toBe('settings.items.nativeNotificationConnected')
  })

  it('removes subscriptions and ignores pending work after unmount', async () => {
    const read = deferred<string>()
    mocks.getStatus.mockReturnValueOnce(read.promise)
    const { result, wrapper } = mountStatus()
    expect(mocks.resume.size).toBe(1)
    wrapper.unmount()
    expect(mocks.resume.size).toBe(0)
    pushStatus(denied)
    read.resolve(JSON.stringify(allowed))
    await flushPromises()
    await result.refresh()
    expect(result.status.value).toBeNull()
    expect(mocks.getStatus).toHaveBeenCalledTimes(1)
  })
})

describe('native notification settings status', () => {
  it.each(['zh', 'en'])('renders permission and process limits in %s without editing push mode', async (locale) => {
    mocks.locale = locale
    mocks.getStatus.mockResolvedValue(JSON.stringify(denied))
    const wrapper = mount(NativeNotificationStatus)
    wrappers.push(wrapper)
    await flushPromises()
    const messages = locale === 'en' ? en.settings.items : zh.settings.items
    expect(wrapper.text()).toContain(messages.nativeNotificationDenied)
    expect(wrapper.text()).toContain(messages.nativeNotificationBlocked)
    expect(wrapper.text()).toContain(messages.nativeNotificationProcessLimit)
    expect(wrapper.findAll('select, input, button')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('settings.items.')
  })
})
