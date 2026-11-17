import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

// ShareRoot is the only place that reads /api/share/{token}/meta, so this is
// where the two share RULES (allow_download / show_project_name) must be shown
// to arrive at the view. The child views are stubbed: their own rendering is
// covered in ShareView.test.ts / DirShareView.test.ts.
vi.mock('@/components/common/PanelSkeleton.vue', () => ({
  default: { name: 'PanelSkeleton', template: '<div class="skeleton-stub" />' },
}))
vi.mock('@/components/common/DownloadProgressBar.vue', () => ({
  default: { name: 'DownloadProgressBar', template: '<div class="progress-stub" />' },
}))
vi.mock('@/components/common/DialogOverlay.vue', () => ({
  default: { name: 'DialogOverlay', template: '<div class="dialog-stub" />' },
}))
vi.mock('@/components/common/BlockLandscape.vue', () => ({
  default: { name: 'BlockLandscape', template: '<div class="landscape-stub" />' },
}))
vi.mock('@/composables/useDownloadProgress', () => ({
  useDownloadProgress: () => ({
    downloadVisible: false,
    downloadFileName: '',
    downloadReceived: 0,
    downloadTotal: 0,
    downloadSpeed: 0,
    cancelDownload: vi.fn(),
  }),
}))

import ShareRoot from '@/share/ShareRoot.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      share: {
        loading: 'Loading...',
        download: 'Download original',
        invalidTitle: 'Cannot view this share',
        invalidUrl: 'Invalid link format',
        notFound: 'Not found',
        fromProject: 'From {name}',
      },
      common: { loading: 'Loading...' },
    },
  },
})

const originalFetch = globalThis.fetch

/** Serve the meta response, then a plain binary file payload. */
function serve(meta: Record<string, unknown>) {
  globalThis.fetch = vi.fn(async (url: string | URL | Request) => {
    const href = String(url)
    if (href.endsWith('/meta')) {
      return { ok: true, json: async () => meta }
    }
    // A binary file renders the "no preview" branch — no heavy preview
    // component is mounted, which keeps this test about the rules only.
    return {
      ok: true,
      json: async () => ({ name: 'blob.bin', path: '/repo/blob.bin', content: '', isBinary: true }),
    }
  }) as unknown as typeof fetch
}

async function mountRoot() {
  window.history.replaceState({}, '', '/share/tokShareRootTest')
  const wrapper = mount(ShareRoot, {
    global: {
      plugins: [i18n],
      stubs: {
        LiveShareView: true,
        SessionShareView: true,
        DirShareView: true,
        ShareView: false,
      },
    },
  })
  await flushPromises()
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  window.history.replaceState({}, '', '/share/tokShareRootTest')
})

afterEach(() => {
  globalThis.fetch = originalFetch
  window.history.replaceState({}, '', '/')
  vi.restoreAllMocks()
})

describe('ShareRoot — share rules from meta', () => {
  it('hides the download link when the server forbids downloads', async () => {
    serve({ kind: 'file', title: 'blob.bin', allowDownload: false })
    const wrapper = await mountRoot()

    expect(wrapper.find('.share-view').exists()).toBe(true)
    expect(wrapper.find('.share-btn--download').exists()).toBe(false)
    expect(wrapper.find('.share-download-btn').exists()).toBe(false)
  })

  it('allows downloads when the field is absent (older server)', async () => {
    serve({ kind: 'file', title: 'blob.bin' })
    const wrapper = await mountRoot()

    // The fallback card is the only download affordance for a binary payload.
    expect(wrapper.find('.share-download-btn').exists()).toBe(true)
  })

  it('shows the source project when the server includes it', async () => {
    serve({ kind: 'file', title: 'blob.bin', projectName: 'clawbench' })
    const wrapper = await mountRoot()

    const chip = wrapper.find('[data-role="share-project"]')
    expect(chip.exists()).toBe(true)
    expect(chip.text()).toContain('From clawbench')
  })

  it('omits the project chip when the server hides the name', async () => {
    serve({ kind: 'file', title: 'blob.bin' })
    const wrapper = await mountRoot()

    expect(wrapper.find('[data-role="share-project"]').exists()).toBe(false)
  })
})
