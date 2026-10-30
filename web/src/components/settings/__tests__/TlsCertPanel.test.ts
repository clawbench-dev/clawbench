import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import TlsCertPanel from '../TlsCertPanel.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

const status = {
  active: true,
  selfSigned: true,
  serving: false,
  hosts: ['127.0.0.1', 'localhost'],
  notAfter: '2036-01-01T00:00:00Z',
  certDir: '/data/tls',
  httpsUrl: 'https://example.test:20000',
  restartRequired: true,
}

let fetchMock: ReturnType<typeof vi.fn>

function ok(body: unknown): Response {
  return { ok: true, status: 200, json: async () => body } as unknown as Response
}

beforeEach(() => {
  fetchMock = vi.fn(async (url: string) => {
    if (url === '/api/config/tls') return ok(status)
    if (url === '/api/config/restart') return ok({ ok: true })
    return ok({})
  })
  vi.stubGlobal('fetch', fetchMock)
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function mountPanel() {
  const w = mount(TlsCertPanel)
  await flushPromises()
  return w
}

describe('TlsCertPanel restart', () => {
  it('moves to the HTTPS address after the listener comes back', async () => {
    const replace = vi.fn()
    const original = window.location
    Object.defineProperty(window, 'location', { value: { ...original, replace }, writable: true })

    const w = await mountPanel()
    await w.find('.tls-panel__notice button').trigger('click')
    await flushPromises()

    // Still on the old protocol for a moment: the server needs to restart.
    expect(replace).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(4000)
    expect(replace).toHaveBeenCalledWith(status.httpsUrl)

    Object.defineProperty(window, 'location', { value: original, writable: true })
  })

  // Without an httpsUrl there is nowhere to navigate: leaving the button stuck on
  // "restarting" would be a dead end.
  it('falls back to a reload hint when the server came back over HTTP', async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url === '/api/config/tls') return ok({ ...status, httpsUrl: '' })
      return ok({ ok: true })
    })

    const w = await mountPanel()
    await w.find('.tls-panel__notice button').trigger('click')
    await flushPromises()

    expect(w.find('.tls-panel__info').text()).toBe('settings.items.tlsRestartedNoHTTPS')
    expect(w.find('.tls-panel__notice button').attributes('disabled')).toBeUndefined()
  })

  it('reports a failed restart and re-enables the button', async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url === '/api/config/tls') return ok(status)
      return { ok: false, status: 500, json: async () => ({}) } as unknown as Response
    })

    const w = await mountPanel()
    await w.find('.tls-panel__notice button').trigger('click')
    await flushPromises()

    expect(w.find('.tls-panel__error').text()).toBe('settings.items.tlsRestartFailed')
    expect(w.find('.tls-panel__notice button').attributes('disabled')).toBeUndefined()
  })
})
