import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zhMessages from '@/i18n/locales/zh'
import HttpsNotice from '../HttpsNotice.vue'

/**
 * The one HTTPS notice, in both shapes.
 *
 * What matters is that the two entry points cannot drift: the copy differs by
 * variant, but the button must run the same three steps (generate → restart →
 * hand over) and keep every failure inside the notice.
 */

const native = vi.hoisted(() => ({ value: null as Record<string, unknown> | null }))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => native.value,
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: { zh: zhMessages },
})

const ORIGIN = 'http://192.168.1.5:20000'
const HTTPS_URL = 'https://192.168.1.5:20000'

let replace: ReturnType<typeof vi.fn>
let fetchMock: ReturnType<typeof vi.fn>
let originalLocation: Location

function ok(body: unknown): Response {
  return { ok: true, status: 200, json: async () => body } as unknown as Response
}

function mountNotice(variant: 'block' | 'banner') {
  return mount(HttpsNotice, {
    props: { variant },
    global: { plugins: [i18n] },
  })
}

beforeEach(() => {
  native.value = null
  replace = vi.fn()
  originalLocation = window.location
  Object.defineProperty(window, 'location', {
    value: { origin: ORIGIN, href: `${ORIGIN}/`, protocol: 'http:', replace },
    writable: true,
  })

  fetchMock = vi.fn(async (url: string) => {
    if (url === '/api/config/tls/self-signed') return ok({ httpsUrl: HTTPS_URL })
    if (url === '/api/config/restart') return ok({ status: 'restarting' })
    return ok({})
  })
  vi.stubGlobal('fetch', fetchMock)
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
})

describe('HttpsNotice copy', () => {
  it('explains the insecure origin and names the address in the block shape', () => {
    const w = mountNotice('block')
    expect(w.find('.https-notice__title').text()).toBe('这台设备无法共享屏幕')
    expect(w.find('.https-notice__text').text()).toContain(ORIGIN)
    expect(w.find('.https-notice__hint').text()).toContain('继续访问')
  })

  it('talks about the people joining, not this device, in the banner shape', () => {
    const w = mountNotice('banner')
    expect(w.find('.https-notice__title').text()).toBe('当前是 HTTP 连接，别人加入会议会受限')
    expect(w.find('.https-notice__text').text()).toContain('别人')
  })
})

describe('HttpsNotice one-click fix', () => {
  it('generates, restarts, then moves the browser to https', async () => {
    const w = mountNotice('block')
    await w.find('[data-action="https-notice-generate"]').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/config/tls/self-signed',
      expect.objectContaining({ method: 'POST' }),
    )
    expect(fetchMock).toHaveBeenCalledWith('/api/config/restart', expect.objectContaining({ method: 'POST' }))
    // Still on the old protocol for a moment: the listener has to come back.
    expect(replace).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(4000)
    expect(replace).toHaveBeenCalledWith(HTTPS_URL)
  })

  it('asks for a second click before overwriting a CA-issued certificate', async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url === '/api/config/tls/self-signed') {
        return { ok: false, status: 409, json: async () => ({}) } as unknown as Response
      }
      return ok({ status: 'restarting' })
    })

    const w = mountNotice('block')
    await w.find('[data-action="https-notice-generate"]').trigger('click')
    await flushPromises()

    const force = w.find('[data-action="https-notice-force"]')
    expect(force.exists()).toBe(true)
    expect(w.find('[data-role="https-notice-message"]').text()).toContain('不可恢复')

    await force.trigger('click')
    await flushPromises()

    const bodies = fetchMock.mock.calls
      .filter(([url]) => url === '/api/config/tls/self-signed')
      .map(([, init]) => JSON.parse((init as RequestInit).body as string))
    expect(bodies).toEqual([{ hosts: [], force: false }, { hosts: [], force: true }])
  })

  it('keeps a failure inside the notice and re-enables the button', async () => {
    fetchMock.mockImplementation(async () => {
      return { ok: false, status: 500, json: async () => ({}) } as unknown as Response
    })

    const w = mountNotice('block')
    await w.find('[data-action="https-notice-generate"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-role="https-notice-message"]').text()).toBe('生成证书失败，请稍后重试。')
    expect(w.find('[data-action="https-notice-generate"]').attributes('disabled')).toBeUndefined()
  })

  it('hands the desktop shell the https address instead of navigating itself', async () => {
    const switchServerUrl = vi.fn(async () => {})
    native.value = { isDesktopApp: () => true, switchServerUrl }

    const w = mountNotice('banner')
    await w.find('[data-action="https-notice-generate"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(4000)
    await flushPromises()

    expect(switchServerUrl).toHaveBeenCalledWith(HTTPS_URL)
    // A plain navigation would be classified as external and open the system
    // browser, leaving the shell on the dead http URL.
    expect(replace).not.toHaveBeenCalled()
  })

  it('falls back to connectToServer for a shell without the dedicated channel', async () => {
    const connectToServer = vi.fn(async () => {})
    native.value = { isDesktopApp: () => true, connectToServer }

    const w = mountNotice('banner')
    await w.find('[data-action="https-notice-generate"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(4000)
    await flushPromises()

    expect(connectToServer).toHaveBeenCalledWith(HTTPS_URL, '')
    expect(replace).not.toHaveBeenCalled()
  })
})
