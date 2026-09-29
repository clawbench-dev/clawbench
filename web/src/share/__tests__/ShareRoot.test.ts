import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'

// ShareRoot only decides which view to mount, so both views are stubbed.
vi.mock('@/share/ShareView.vue', () => ({
  default: { name: 'ShareView', template: '<div class="file-view-stub" />' },
}))
vi.mock('@/share/SessionShareView.vue', () => ({
  default: { name: 'SessionShareView', template: '<div class="session-view-stub" />' },
}))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', props: ['size'], template: '<div class="loading-stub" />' },
}))

import ShareRoot from '@/share/ShareRoot.vue'
import { getShareToken, setShareToken } from '@/share/shareMode'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      share: {
        invalidTitle: 'Cannot view this share',
        invalidUrl: 'Invalid link format',
        notFound: 'Link not found',
      },
    },
  },
})

const originalFetch = globalThis.fetch
const mounted: Array<{ unmount: () => void }> = []
let fetchCalls: string[] = []

beforeEach(() => {
  fetchCalls = []
  setShareToken(null)
  globalThis.fetch = vi.fn(async (url: string | URL | Request) => {
    fetchCalls.push(String(url))
    return { ok: true, json: async () => ({ kind: 'session' }) } as Response
  }) as unknown as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
  window.history.replaceState({}, '', '/')
  setShareToken(null)
  vi.restoreAllMocks()
  for (const w of mounted.splice(0)) w.unmount()
})

async function mountRoot(path: string) {
  window.history.replaceState({}, '', path)
  const wrapper = mount(ShareRoot, { global: { plugins: [i18n] } })
  mounted.push(wrapper)
  await flushPromises()
  await nextTick()
  await flushPromises()
  return wrapper
}

describe('ShareRoot', () => {
  it('installs the token before mounting a child view', async () => {
    await mountRoot('/share/tokabc')
    // The child views build token-scoped URLs, so the token must be set first.
    expect(getShareToken()).toBe('tokabc')
  })

  it('asks the meta endpoint which kind of share this is', async () => {
    await mountRoot('/share/tokabc')
    expect(fetchCalls).toEqual(['/api/share/tokabc/meta'])
  })

  it('dispatches a session share to SessionShareView', async () => {
    const wrapper = await mountRoot('/share/tokabc')
    expect(wrapper.find('.session-view-stub').exists()).toBe(true)
    expect(wrapper.find('.file-view-stub').exists()).toBe(false)
  })

  it('dispatches a file share to ShareView', async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: true, json: async () => ({ kind: 'file' }) } as Response)) as unknown as typeof fetch
    const wrapper = await mountRoot('/share/tokabc')
    expect(wrapper.find('.file-view-stub').exists()).toBe(true)
    expect(wrapper.find('.session-view-stub').exists()).toBe(false)
  })

  // An unrecognised kind must fall back to the file view rather than rendering
  // nothing: the file view already reports a clean not-found state.
  it('treats an unknown kind as a file share', async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: true, json: async () => ({ kind: 'something-else' }) } as Response)) as unknown as typeof fetch
    const wrapper = await mountRoot('/share/tokabc')
    expect(wrapper.find('.file-view-stub').exists()).toBe(true)
  })

  it('reports a revoked or unknown link without mounting a view', async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: false, status: 404, json: async () => ({}) } as Response)) as unknown as typeof fetch
    const wrapper = await mountRoot('/share/tokabc')
    expect(wrapper.text()).toContain('Link not found')
    expect(wrapper.find('.session-view-stub').exists()).toBe(false)
    expect(wrapper.find('.file-view-stub').exists()).toBe(false)
  })

  it('rejects a malformed URL without any request', async () => {
    const wrapper = await mountRoot('/share/')
    expect(wrapper.text()).toContain('Invalid link format')
    expect(fetchCalls).toEqual([])
  })

  it('decodes a percent-encoded token', async () => {
    await mountRoot('/share/tok%2Dabc')
    expect(getShareToken()).toBe('tok-abc')
  })

  it('reports a network failure as a not-found link', async () => {
    globalThis.fetch = vi.fn(async () => { throw new Error('offline') }) as unknown as typeof fetch
    const wrapper = await mountRoot('/share/tokabc')
    expect(wrapper.text()).toContain('Link not found')
  })
})
