import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

// ── Child mocks ────────────────────────────────────────────────────────────
// The file view pulls the whole preview chain (markdown / pdfjs / codemirror);
// this test only exercises the folder listing and the hand-off to it.
vi.mock('@/share/ShareView.vue', () => ({
  default: {
    name: 'ShareView',
    // Typed props (not an array) so the valueless `can-back-out` attribute
    // resolves to the boolean true, exactly as in the real component.
    props: { initialPath: String, canBackOut: Boolean },
    emits: ['back-out'],
    template: '<div class="file-view-stub" :data-path="initialPath" />',
  },
}))
vi.mock('@/share/ShareThemeButton.vue', () => ({
  default: { name: 'ShareThemeButton', template: '<button class="theme-stub" />' },
}))
vi.mock('@/components/common/FileIcon.vue', () => ({
  default: { name: 'FileIcon', props: ['path', 'isDir'], template: '<i class="file-icon-stub">{{ path }}</i>' },
}))
vi.mock('@/components/common/PanelSkeleton.vue', () => ({
  default: { name: 'PanelSkeleton', props: ['variant', 'rows', 'label'], template: '<div class="skeleton-stub" />' },
}))

const { mockDownloadUrlWithProgress } = vi.hoisted(() => ({
  mockDownloadUrlWithProgress: vi.fn(),
}))
vi.mock('@/utils/download.ts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/utils/download.ts')>()),
  downloadUrlWithProgress: mockDownloadUrlWithProgress,
}))

import DirShareView from '@/share/DirShareView.vue'
import { setShareToken } from '@/share/shareMode'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { loading: 'Loading...' },
      share: {
        loading: 'Loading...',
        invalidTitle: 'Cannot view this share',
        invalidUrl: 'Invalid link format',
        dir: {
          title: 'Shared folder',
          up: 'Up one level',
          empty: 'This folder is empty',
          download: 'Download',
          notFound: 'This folder does not exist or was removed',
          backToRoot: 'Back to folder',
        },
      },
    },
  },
})

const originalLocation = window.location
const originalFetch = globalThis.fetch
let fetchCalls: string[] = []
let assign: ReturnType<typeof vi.fn>
let responses: Record<string, unknown> = {}
/** Every wrapper mounted by a test, so its listeners die with the test. */
let mounted: Array<ReturnType<typeof mount>> = []

function useLocation(search = '') {
  assign = vi.fn()
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: {
      pathname: '/share/tokabc',
      search,
      assign,
      href: 'http://localhost/share/tokabc' + search,
    },
  })
}

beforeEach(() => {
  fetchCalls = []
  responses = {}
  mockDownloadUrlWithProgress.mockReset()
  setShareToken(null)
  useLocation()
  globalThis.fetch = vi.fn(async (url: string | URL | Request) => {
    const key = String(url)
    fetchCalls.push(key)
    const body = responses[key]
    if (body === undefined) {
      return { ok: false, status: 404, json: async () => ({}) } as Response
    }
    return { ok: true, json: async () => body } as Response
  }) as unknown as typeof fetch
})

afterEach(() => {
  // Every mount registers a `popstate` listener; leaving them attached made a
  // later test's dispatch fan out to every earlier component as well.
  for (const w of mounted) w.unmount()
  mounted = []
  globalThis.fetch = originalFetch
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: originalLocation,
  })
  setShareToken(null)
  vi.restoreAllMocks()
})

const ROOT = '/proj/bundle'
const LIST_URL = '/api/share/tokabc/list'

function rootListing(overrides: Record<string, unknown> = {}) {
  return {
    kind: 'dir',
    name: 'bundle',
    path: ROOT,
    root: ROOT,
    parent: null,
    atRoot: true,
    crumbs: [{ name: 'bundle', path: ROOT }],
    entries: [
      { name: 'sub', type: 'dir' },
      { name: 'a.md', type: 'file', size: 1536, modified: '2026-05-01T10:00:00Z' },
      { name: '.env', type: 'file', size: 9 },
    ],
    ...overrides,
  }
}

async function mountView() {
  const wrapper = mount(DirShareView, { global: { plugins: [i18n] } })
  mounted.push(wrapper)
  await flushPromises()
  return wrapper
}

describe('DirShareView', () => {
  it('lists the shared folder: folders first, sizes formatted', async () => {
    responses[LIST_URL] = rootListing()
    const wrapper = await mountView()

    expect(fetchCalls).toEqual([LIST_URL])
    const rows = wrapper.findAll('.dir-row')
    expect(rows).toHaveLength(3)
    expect(rows[0].attributes('data-entry-type')).toBe('dir')
    expect(rows[0].find('.dir-row-name').text()).toBe('sub')
    // Folders carry no size.
    expect(rows[0].find('.dir-row-size').text()).toBe('')
    expect(rows[1].find('.dir-row-size').text()).toBe('1.5 KB')
    // Dotfiles are shown: the share publishes the folder as it is.
    expect(wrapper.text()).toContain('.env')
  })

  it('hides the "up" button at the share root', async () => {
    responses[LIST_URL] = rootListing()
    const wrapper = await mountView()
    expect(wrapper.find('.dir-up-btn').exists()).toBe(false)
  })

  it('enters a subfolder in place and fetches its listing', async () => {
    responses[LIST_URL] = rootListing()
    const pushState = vi.spyOn(window.history, 'pushState')
    const wrapper = await mountView()

    await wrapper.findAll('.dir-row')[0].trigger('click')
    await flushPromises()

    expect(fetchCalls[1]).toBe(`${LIST_URL}?path=${encodeURIComponent(ROOT + '/sub')}`)
    // In place, not a page load: the URL must change so Back/refresh work.
    expect(pushState).toHaveBeenCalledTimes(1)
    expect(pushState.mock.calls[0][2]).toBe(
      `/share/tokabc?path=${encodeURIComponent(ROOT + '/sub')}`,
    )
  })

  it('reloads the listing on browser back/forward', async () => {
    responses[LIST_URL] = rootListing()
    responses[`${LIST_URL}?path=${encodeURIComponent(ROOT + '/sub')}`] = rootListing({
      name: 'sub',
      path: ROOT + '/sub',
      atRoot: false,
    })
    const wrapper = await mountView()

    await wrapper.findAll('.dir-row')[0].trigger('click')
    await flushPromises()
    expect(fetchCalls).toHaveLength(2)

    // The browser restores the previous entry: the URL loses ?path= and a
    // popstate fires. Deleting the listener (or reading pathname instead of
    // search) would leave the subfolder on screen — this is the regression.
    ;(window.location as unknown as { search: string }).search = ''
    window.dispatchEvent(new Event('popstate'))
    await flushPromises()

    expect(fetchCalls.at(-1)).toBe(LIST_URL)
  })

  it('opens a file through the share deep link (full navigation)', async () => {
    responses[LIST_URL] = rootListing()
    const wrapper = await mountView()

    await wrapper.findAll('.dir-row')[1].trigger('click')

    expect(assign).toHaveBeenCalledTimes(1)
    expect(assign.mock.calls[0][0]).toBe(
      `/share/tokabc?path=${encodeURIComponent(ROOT + '/a.md')}`,
    )
  })

  it('downloads a single file through the token-scoped endpoint', async () => {
    responses[LIST_URL] = rootListing()
    const wrapper = await mountView()

    const downloadBtn = wrapper.findAll('.dir-row')[1].find('.dir-row-download')
    expect(downloadBtn.exists()).toBe(true)
    await downloadBtn.trigger('click')

    expect(mockDownloadUrlWithProgress).toHaveBeenCalledWith(
      `/api/share/tokabc/download?path=${encodeURIComponent(ROOT + '/a.md')}`,
      'a.md',
    )
    // The row click must not fire as well (the button stops propagation).
    expect(assign).not.toHaveBeenCalled()
  })

  it('offers no download button on folders', async () => {
    responses[LIST_URL] = rootListing()
    const wrapper = await mountView()
    expect(wrapper.findAll('.dir-row')[0].find('.dir-row-download').exists()).toBe(false)
  })

  // The row is a role=button with its own Enter/Space handler. Those events
  // bubble from the nested download button, so without `.self` the row would
  // swallow the button's activation and open the file instead of downloading.
  it('does not open the file when Enter is pressed on its download button', async () => {
    responses[LIST_URL] = rootListing()
    const wrapper = await mountView()

    const downloadBtn = wrapper.findAll('.dir-row')[1].find('.dir-row-download')
    await downloadBtn.trigger('keydown', { key: 'Enter' })

    expect(assign).not.toHaveBeenCalled()
  })

  it('shows the empty state for an empty folder', async () => {
    responses[LIST_URL] = rootListing({ entries: [] })
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('This folder is empty')
    expect(wrapper.findAll('.dir-row')).toHaveLength(0)
  })

  it('reports a missing folder instead of rendering an empty one', async () => {
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('This folder does not exist or was removed')
    expect(wrapper.findAll('.dir-row')).toHaveLength(0)
    // Nothing to go back to at the root itself.
    expect(wrapper.find('.dir-error-back').exists()).toBe(false)
  })

  it('offers a way back to the root when a deep link is gone', async () => {
    const gone = ROOT + '/gone'
    useLocation(`?path=${encodeURIComponent(gone)}`)
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('This folder does not exist or was removed')
    const back = wrapper.find('.dir-error-back')
    expect(back.exists()).toBe(true)

    await back.trigger('click')
    expect(assign).toHaveBeenCalledWith('/share/tokabc')
  })

  it('renders a breadcrumb trail and navigates to an ancestor', async () => {
    const sub = ROOT + '/sub'
    responses[`${LIST_URL}?path=${encodeURIComponent(sub)}`] = rootListing({
      name: 'sub',
      path: sub,
      parent: null,
      atRoot: false,
      crumbs: [
        { name: 'bundle', path: ROOT },
        { name: 'sub', path: sub },
      ],
      entries: [],
    })
    useLocation(`?path=${encodeURIComponent(sub)}`)
    const wrapper = await mountView()

    const crumbs = wrapper.findAll('.dir-crumb')
    expect(crumbs.map(c => c.text())).toEqual(['bundle', 'sub'])
    // The current folder is not a button.
    expect(crumbs[1].element.tagName).not.toBe('BUTTON')
    // The "up" action appears inside a subfolder.
    expect(wrapper.find('.dir-up-btn').exists()).toBe(true)

    await crumbs[0].trigger('click')
    await flushPromises()
    expect(fetchCalls.at(-1)).toBe(`${LIST_URL}?path=${encodeURIComponent(ROOT)}`)
  })

  it('hands a file deep link to ShareView and returns to the containing folder', async () => {
    const file = ROOT + '/sub/a.md'
    responses[`${LIST_URL}?path=${encodeURIComponent(file)}`] = {
      kind: 'file',
      name: 'a.md',
      path: file,
      root: ROOT,
      parent: ROOT + '/sub',
      atRoot: false,
    }
    useLocation(`?path=${encodeURIComponent(file)}`)
    const wrapper = await mountView()

    const stub = wrapper.findComponent({ name: 'ShareView' })
    expect(stub.exists()).toBe(true)
    expect(stub.props('initialPath')).toBe(file)
    expect(stub.props('canBackOut')).toBe(true)

    stub.vm.$emit('back-out')
    await flushPromises()
    expect(assign).toHaveBeenCalledWith(
      `/share/tokabc?path=${encodeURIComponent(ROOT + '/sub')}`,
    )
  })

  it('returns to the bare share URL when the file sits in the root', async () => {
    const file = ROOT + '/a.md'
    responses[`${LIST_URL}?path=${encodeURIComponent(file)}`] = {
      kind: 'file',
      name: 'a.md',
      path: file,
      root: ROOT,
      parent: null,
      atRoot: false,
    }
    useLocation(`?path=${encodeURIComponent(file)}`)
    const wrapper = await mountView()

    wrapper.findComponent({ name: 'ShareView' }).vm.$emit('back-out')
    await flushPromises()
    expect(assign).toHaveBeenCalledWith('/share/tokabc')
  })

  it('reports a malformed URL without any request', async () => {
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: { pathname: '/share/', search: '', assign: vi.fn(), href: '' },
    })
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('Invalid link format')
    expect(fetchCalls).toEqual([])
  })

  /**
   * Two navigations can be in flight at once (click into a folder, then Back —
   * or open another folder from the breadcrumb). The network may answer them in
   * either order, and writing the older answer last left the listing showing
   * folder A while the URL said B, which reads as "I clicked the wrong thing"
   * and only clears on a reload.
   */
  it('ignores a superseded listing that arrives after the newer one', async () => {
    responses[LIST_URL] = rootListing()
    const wrapper = await mountView()

    // Every further request stays pending until the test answers it.
    const pending: Array<(body: unknown) => void> = []
    globalThis.fetch = vi.fn(
      (url: string | URL | Request) =>
        new Promise<Response>((resolve) => {
          fetchCalls.push(String(url))
          pending.push((body) => resolve({ ok: true, json: async () => body } as Response))
        }),
    ) as unknown as typeof fetch

    const setSearch = (path: string) => {
      ;(window.location as unknown as { search: string }).search = `?path=${encodeURIComponent(path)}`
      window.dispatchEvent(new Event('popstate'))
    }
    setSearch(ROOT + '/a')
    setSearch(ROOT + '/b')
    expect(pending).toHaveLength(2)

    // The newer request wins the race...
    pending[1](rootListing({ name: 'b', path: ROOT + '/b', atRoot: false }))
    await flushPromises()
    expect(wrapper.find('.share-file-name').text()).toBe('b')

    // ...and the older one must be discarded when it lands.
    pending[0](rootListing({ name: 'a', path: ROOT + '/a', atRoot: false }))
    await flushPromises()
    expect(wrapper.find('.share-file-name').text()).toBe('b')
    // The stale answer must not resurrect the skeleton either.
    expect(wrapper.find('.dir-skeleton').exists()).toBe(false)
  })
})
