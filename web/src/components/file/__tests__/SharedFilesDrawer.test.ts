import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import SharedFilesDrawer from '@/components/file/SharedFilesDrawer.vue'
import { onTabSwitch } from '@/composables/useTabDrawer'

// ── Mocks ──

// Mock BottomSheet component — simple passthrough.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    props: ['open', 'auto', 'title'],
    emits: ['close'],
    template: '<div class="bottom-sheet-stub" v-if="$props.open"><slot name="header" /><slot /></div>',
  }),
}))

// Hoisted shared mock fns (vi.mock factories are hoisted above the body).
const h = vi.hoisted(() => ({
  toastShow: vi.fn(),
  copyText: vi.fn((_t: string, onSuccess?: () => void) => onSuccess?.()),
  confirm: vi.fn().mockResolvedValue(true),
  markUnshared: vi.fn(),
  resetFileShareState: vi.fn(),
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: h.toastShow }),
}))

vi.mock('@/utils/clipboard.ts', () => ({
  copyText: (text: string, onSuccess?: () => void) => { h.copyText(text); onSuccess?.() },
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: h.confirm }),
}))

vi.mock('@/composables/useFileShare', () => ({
  useFileShare: () => ({ markUnshared: h.markUnshared, resetFileShareState: h.resetFileShareState }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// The open-in-new-tab control must route through the shared external-link
// primitive. A bare same-origin `target="_blank"` anchor is a dead click in the
// desktop shell (the window-open handler denies the popup without opening
// anything) and in the Android WebView (no multi-window support).
const mockOpenExternalUrl = vi.fn()
vi.mock('@/utils/externalLink', () => ({
  openExternalUrl: (url: string) => mockOpenExternalUrl(url),
}))

const messages = {
  en: {
    common: { retry: 'Retry' },
    sharedFiles: {
      button: 'Shared files',
      title: 'Shared files',
      empty: 'No shared files yet',
      openFile: 'Open file',
      openInNewTab: 'Open link in new tab',
      copyLink: 'Copy link',
      copied: 'Link copied',
      revoke: 'Revoke share',
      revoked: 'Share revoked',
      fileDeleted: 'File deleted',
      confirmRevoke: 'Revoke "{name}"?',
      clearAll: 'Clear all',
      confirmClearAll: 'Clear all shared files?',
    },
  },
}
const i18n = createI18n({ legacy: false, locale: 'en', messages, missingWarn: false, fallbackWarn: false })

function mountDrawer() {
  return mount(SharedFilesDrawer, { global: { plugins: [i18n] } })
}

function jsonResponse(body: unknown, ok = true): Response {
  return { ok, json: () => Promise.resolve(body) } as unknown as Response
}

describe('SharedFilesDrawer', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    // Drawer is bound to the browse tab via useTabDrawer; activate that tab.
    onTabSwitch('browse')
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    Object.defineProperty(window, 'location', { value: { origin: 'https://host.example', pathname: '/' }, writable: true })
    vi.clearAllMocks()
    h.confirm.mockResolvedValue(true)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('loads the share list when opened and renders rows', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [
        { token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: '2026-01-01', exists: true },
        { token: 'tok2', name: 'b.png', path: '/abs/other/b.png', createdAt: '2026-01-02', exists: false },
      ],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    expect(fetchMock).toHaveBeenCalledWith('/api/share/list')
    expect(wrapper.text()).toContain('a.md')
    expect(wrapper.text()).toContain('docs/a.md')
    expect(wrapper.text()).toContain('b.png')
    expect(wrapper.text()).toContain('File deleted') // exists=false badge
  })

  // The row renders createdAt through the app's single relative-time formatter,
  // not the raw server string. The API now sends an RFC3339 UTC instant.
  it('renders createdAt as a friendly relative time', async () => {
    const twoHoursAgo = new Date(Date.now() - 2 * 3600000).toISOString()
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: twoHoursAgo, exists: true }],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    expect(wrapper.text()).toContain('2 hr ago')
    expect(wrapper.text()).not.toContain(twoHoursAgo)
  })

  // Both drawers render name + one meta line, so the two lists read as one
  // family. This pins the merged path · time line (the file row used to stack
  // path and time on separate lines, making it a taller three-line row).
  it('renders path and time on a single meta line', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: '2026-01-01', exists: true }],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    const meta = wrapper.find('.shared-file-meta')
    expect(meta.exists()).toBe(true)
    expect(meta.find('.shared-file-path').exists()).toBe(true)
    expect(meta.find('.shared-file-sep').exists()).toBe(true)
    expect(meta.find('.shared-file-time').exists()).toBe(true)
  })

  it('shows the empty state when there are no shares', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ shares: [] }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()
    expect(wrapper.text()).toContain('No shared files yet')
  })

  it('shows an error state when the request fails', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, statusText: 'Error' })
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()
    expect(wrapper.text()).toContain('Error')
  })

  it('emits selectFile when the row is clicked', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: 'x', exists: true }],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    await wrapper.find('.shared-file-row').trigger('click')
    expect(wrapper.emitted('selectFile')).toBeTruthy()
    expect(wrapper.emitted('selectFile')![0]).toEqual(['docs/a.md'])
  })

  it('does not open a deleted file when its row is clicked', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok2', name: 'b.md', path: 'b.md', createdAt: 'x', exists: false }],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    await wrapper.find('.shared-file-row').trigger('click')
    expect(wrapper.emitted('selectFile')).toBeUndefined()
  })

  it('provides a new-tab share link for existing files only', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [
        { token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: 'x', exists: true },
        { token: 'tok2', name: 'b.md', path: 'b.md', createdAt: 'x', exists: false },
      ],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    const externalLinks = wrapper.findAll('a[title="Open link in new tab"]')
    expect(externalLinks).toHaveLength(1)
    expect(externalLinks[0].attributes('href')).toBe('https://host.example/share/tok1')
    expect(externalLinks[0].attributes('target')).toBe('_blank')
    expect(externalLinks[0].attributes('rel')).toBe('noopener noreferrer')
  })

  it('routes the open-in-new-tab click through openExternalUrl', async () => {
    // See the mock note above: the raw same-origin anchor cannot open in either
    // native host, so the click has to go through the bridge-aware primitive.
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: 'x', exists: true }],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    const openLink = wrapper.find('a[title="Open link in new tab"]')
    const ev = new MouseEvent('click', { bubbles: true, cancelable: true })
    openLink.element.dispatchEvent(ev)
    await nextTick()

    expect(mockOpenExternalUrl).toHaveBeenCalledWith('https://host.example/share/tok1')
    expect(ev.defaultPrevented).toBe(true)
  })

  it('revokes a share after confirmation and removes the row', async () => {
    // The drawer fetches on open via openDrawer() AND the effectiveOpen watch;
    // return the list for every GET and success for every DELETE.
    fetchMock.mockImplementation((url: string, init?: RequestInit) =>
      Promise.resolve(init?.method === 'DELETE'
        ? jsonResponse({ ok: true })
        : jsonResponse({
            shares: [{ token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: 'x', exists: true }],
          }))
    )

    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    const revokeBtn = wrapper.findAll('.shared-file-btn').find(b => b.attributes('title') === 'Revoke share')
    expect(revokeBtn).toBeTruthy()
    await revokeBtn!.trigger('click')
    await flushPromises()
    await nextTick()

    expect(h.confirm).toHaveBeenCalledWith('Revoke "a.md"?', expect.anything())
    // DELETE sent with the token.
    const delCall = fetchMock.mock.calls.find((c: unknown[]) => c[1]?.method === 'DELETE')
    expect(delCall).toBeTruthy()
    expect(JSON.parse(delCall![1].body)).toEqual({ token: 'tok1' })
    expect(wrapper.text()).not.toContain('a.md')
    expect(h.markUnshared).toHaveBeenCalledWith('docs/a.md')
  })

  it('copies the link when copy is clicked', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok9', name: 'c.md', path: 'c.md', createdAt: 'x', exists: true }],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    const copyBtn = wrapper.findAll('.shared-file-btn').find(b => b.attributes('title') === 'Copy link')
    expect(copyBtn).toBeTruthy()
    await copyBtn!.trigger('click')
    expect(h.copyText).toHaveBeenCalledWith('https://host.example/share/tok9')
    // Feedback is the shared check glyph, not a toast.
    expect(copyBtn!.classes()).toContain('is-copied')
    expect(h.toastShow).not.toHaveBeenCalled()
  })

  it('does not revoke when the confirm dialog is cancelled', async () => {
    h.confirm.mockResolvedValueOnce(false)
    fetchMock.mockResolvedValueOnce(jsonResponse({
      shares: [{ token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: 'x', exists: true }],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    const revokeBtn = wrapper.findAll('.shared-file-btn').find(b => b.attributes('title') === 'Revoke share')
    await revokeBtn!.trigger('click')
    await flushPromises()

    const delCall = fetchMock.mock.calls.find((c: unknown[]) => c[1]?.method === 'DELETE')
    expect(delCall).toBeUndefined()
    expect(wrapper.text()).toContain('a.md')
  })

  it('clears all shares after confirmation', async () => {
    // GET returns a list; DELETE {all:true} clears.
    fetchMock.mockImplementation((url: string, init?: RequestInit) =>
      Promise.resolve(init?.method === 'DELETE'
        ? jsonResponse({ ok: true })
        : jsonResponse({
            shares: [
              { token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: 'x', exists: true },
              { token: 'tok2', name: 'b.md', path: 'b.md', createdAt: 'x', exists: true },
            ],
          }))
    )

    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    expect(wrapper.find('.shared-files-clear').exists()).toBe(true)
    await wrapper.find('.shared-files-clear').trigger('click')
    await flushPromises()
    await nextTick()

    expect(h.confirm).toHaveBeenCalledWith('Clear all shared files?', expect.anything())
    const delCall = fetchMock.mock.calls.find((c: unknown[]) => c[1]?.method === 'DELETE')
    expect(delCall).toBeTruthy()
    expect(JSON.parse(delCall![1].body)).toEqual({ all: true })
    expect(wrapper.text()).not.toContain('a.md')
    expect(h.resetFileShareState).toHaveBeenCalled()
  })

  it('does not clear all when the confirm dialog is cancelled', async () => {
    h.confirm.mockResolvedValueOnce(false)
    fetchMock.mockResolvedValueOnce(jsonResponse({
      shares: [{ token: 'tok1', name: 'a.md', path: 'docs/a.md', createdAt: 'x', exists: true }],
    }))
    const wrapper = mountDrawer()
    ;(wrapper.vm as any).open()
    await flushPromises()
    await nextTick()

    await wrapper.find('.shared-files-clear').trigger('click')
    await flushPromises()

    const delCalls = fetchMock.mock.calls.filter((c: unknown[]) => c[1]?.method === 'DELETE')
    expect(delCalls).toHaveLength(0)
    expect(wrapper.text()).toContain('a.md')
  })
})
