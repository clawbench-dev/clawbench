import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import ShareLinkDialog from '../ShareLinkDialog.vue'

// Mock ModalDialog — simple passthrough (slot rendering issues in VTU).
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: {
    name: 'ModalDialog',
    props: ['open', 'title', 'zIndex'],
    emits: ['close'],
    template: '<div v-if="open" class="modal-dialog"><div class="modal-header"><slot name="header"><span class="modal-title">{{ title }}</span></slot></div><div class="modal-body"><slot /></div><div class="modal-footer"><slot name="footer" /></div></div>',
  },
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

// Mock global confirm dialog (no actual dialog UI in unit tests).
const mockConfirm = vi.fn()
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: mockConfirm }),
}))

// Mock clipboard copy.
const mockCopyText = vi.fn()
vi.mock('@/utils/clipboard.ts', () => ({
  copyText: (text: string, onSuccess?: () => void) => {
    mockCopyText(text)
    onSuccess?.()
  },
}))

const messages = {
  en: {
    common: { copy: 'Copy', copied: 'Copied', close: 'Close', loading: 'Loading...' },
    shareDialog: {
      title: 'Share link',
      noFile: 'No file open',
      explain: 'Explain',
      active: 'Active',
      securityHint: 'Security hint',
      generate: 'Generate link',
      regenerate: 'Regenerate',
      regenerateTip: 'Regenerate tip',
      copyTip: 'Copy tip',
      revoke: 'Revoke',
      revoked: 'Revoked',
      openPage: 'Open page',
      confirmRegenerate: 'Confirm regenerate',
      confirmRevoke: 'Confirm revoke',
    },
  },
}

const i18n = createI18n({ legacy: false, locale: 'en', messages, missingWarn: false, fallbackWarn: false })

function mountDialog(props = {}) {
  return mount(ShareLinkDialog, {
    props: {
      open: true,
      file: { name: 'a.md', path: '/proj/a.md' },
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

function jsonResponse(body: unknown, ok = true, status = 200): Response {
  return {
    ok,
    status,
    statusText: ok ? 'OK' : 'Error',
    json: () => Promise.resolve(body),
  } as unknown as Response
}

describe('ShareLinkDialog', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    // Default origin used by the dialog to build the absolute URL.
    Object.defineProperty(window, 'location', {
      value: { origin: 'https://host.example', pathname: '/' },
      writable: true,
    })
    mockConfirm.mockReset()
    mockConfirm.mockResolvedValue(true)
    mockCopyText.mockClear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  describe('notice banner', () => {
    it('shows one info banner whose warning line is embedded without a warning icon', async () => {
      fetchMock.mockResolvedValue(jsonResponse({}))
      const wrapper = mountDialog()
      await flushPromises()
      await nextTick()
      const notice = wrapper.find('.share-notice-info')
      expect(notice.exists()).toBe(true)
      const warn = notice.find('.share-notice-warning')
      expect(warn.exists()).toBe(true)
      expect(warn.text()).toContain('Security hint')
      // No separate warning banner and no shield icon next to the warning.
      expect(wrapper.find('.share-notice-warning.share-notice').exists()).toBe(false)
      expect(warn.find('svg').exists()).toBe(false)
    })

    it('shows the embedded warning in the active-share state too', async () => {
      fetchMock.mockResolvedValue(jsonResponse({ token: 'tok1', path: '/share/tok1' }))
      const wrapper = mountDialog()
      await flushPromises()
      await nextTick()
      expect(wrapper.find('.share-notice-info .share-notice-warning').exists()).toBe(true)
    })
  })

  describe('file identity', () => {
    it('shows the file name prominently over the muted full path in the body', async () => {
      fetchMock.mockResolvedValue(jsonResponse({}))
      const wrapper = mountDialog()
      await flushPromises()
      await nextTick()
      const block = wrapper.find('.share-dialog-file-block')
      expect(block.exists()).toBe(true)
      expect(block.find('.share-dialog-file-name').text()).toBe('a.md')
      expect(block.find('.share-dialog-file-path').text()).toBe('/proj/a.md')
      // No file-name chip in the header anymore — the header is plain again.
      expect(wrapper.find('.modal-header .share-dialog-header-file').exists()).toBe(false)
    })
  })

  describe('generate state (no active share)', () => {
    it('shows the info notice and the generate button in the footer', async () => {
      fetchMock.mockResolvedValue(jsonResponse({}))
      const wrapper = mountDialog()
      await flushPromises()
      await nextTick()
      expect(wrapper.find('.share-notice-info').exists()).toBe(true)
      // Generate action lives in the footer now.
      expect(wrapper.find('.modal-footer .share-dialog-primary').exists()).toBe(true)
      expect(wrapper.find('.share-dialog-primary').text()).toContain('Generate link')
    })
  })

  describe('active-share state', () => {
    beforeEach(() => {
      fetchMock.mockResolvedValue(jsonResponse({ token: 'tok1', path: '/share/tok1' }))
    })

    it('shows the existing link in a dedicated full-width input row', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      await nextTick()
      const input = wrapper.find('input.share-dialog-link-input')
      expect((input.element as HTMLInputElement).value).toBe('https://host.example/share/tok1')
      // The link row wraps only the input; the row container is one column.
      expect(wrapper.find('.share-dialog-link-bar').exists()).toBe(true)
    })

    it('embeds copy and regenerate icon buttons inside the input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      await nextTick()
      const copyBtn = wrapper.find('.share-dialog-link-btn[title="Copy tip"]')
      const regenBtn = wrapper.find('.share-dialog-link-btn[title="Regenerate tip"]')
      expect(copyBtn.exists()).toBe(true)
      expect(regenBtn.exists()).toBe(true)
    })

    it('moves open-page and revoke into the footer and renders no text next to the input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      await nextTick()
      const footer = wrapper.find('.modal-footer')
      expect(footer.find('a.share-dialog-btn').exists()).toBe(true)
      expect(footer.find('a.share-dialog-btn').attributes('href')).toBe('https://host.example/share/tok1')
      expect(footer.find('a.share-dialog-btn').attributes('target')).toBe('_blank')
      expect(footer.find('a.share-dialog-btn').attributes('rel')).toBe('noopener noreferrer')
      expect(footer.find('.share-dialog-secondary.danger').exists()).toBe(true)
      // No footer close button anymore.
      expect(footer.find('.share-dialog-cancel').exists()).toBe(false)
    })
  })

  it('copies the link via the embedded copy button', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ token: 'tok1', path: '/share/tok1' }))
    const wrapper = mountDialog()
    await flushPromises()
    await nextTick()

    await wrapper.find('.share-dialog-link-btn[title="Copy tip"]').trigger('click')
    expect(mockCopyText).toHaveBeenCalledWith('https://host.example/share/tok1')
  })

  it('creates a link via POST and copies it', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse({ token: 'newtok', path: '/share/newtok' }))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('.share-dialog-primary').trigger('click')
    await flushPromises()
    await nextTick()

    const input = wrapper.find('input.share-dialog-link-input')
    expect((input.element as HTMLInputElement).value).toBe('https://host.example/share/newtok')
    // POST body carries the file path.
    const postCall = fetchMock.mock.calls.find((c: unknown[]) => c[1]?.method === 'POST')
    expect(postCall).toBeTruthy()
    expect(JSON.parse(postCall![1].body)).toEqual({ path: '/proj/a.md' })
    expect(mockCopyText).toHaveBeenCalledWith('https://host.example/share/newtok')
  })

  it('revokes an active share via DELETE after confirmation', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ token: 'tok1', path: '/share/tok1' }))
      .mockResolvedValueOnce(jsonResponse({ ok: true }))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('.modal-footer .share-dialog-secondary.danger').trigger('click')
    await flushPromises()
    await nextTick()

    expect(mockConfirm).toHaveBeenCalledWith('Confirm revoke', { dangerous: true })
    const delCall = fetchMock.mock.calls.find((c: unknown[]) => c[1]?.method === 'DELETE')
    expect(delCall).toBeTruthy()
    // Back to the generate state.
    expect(wrapper.find('input.share-dialog-link-input').exists()).toBe(false)
    expect(wrapper.text()).toContain('Generate link')
  })

  it('does not revoke when the confirmation is dismissed', async () => {
    mockConfirm.mockResolvedValue(false)
    fetchMock.mockResolvedValueOnce(jsonResponse({ token: 'tok1', path: '/share/tok1' }))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('.modal-footer .share-dialog-secondary.danger').trigger('click')
    await flushPromises()

    const delCall = fetchMock.mock.calls.find((c: unknown[]) => c[1]?.method === 'DELETE')
    expect(delCall).toBeUndefined()
    expect(wrapper.find('input.share-dialog-link-input').exists()).toBe(true)
  })

  it('regenerates the link only after confirmation', async () => {
    mockConfirm.mockResolvedValue(false)
    fetchMock.mockResolvedValueOnce(jsonResponse({ token: 'tok1', path: '/share/tok1' }))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('.share-dialog-link-btn[title="Regenerate tip"]').trigger('click')
    await flushPromises()

    // Cancelled — no POST issued and the old link stays.
    const postCall = fetchMock.mock.calls.find((c: unknown[]) => c[1]?.method === 'POST')
    expect(postCall).toBeUndefined()
    expect((wrapper.find('input.share-dialog-link-input').element as HTMLInputElement).value).toBe('https://host.example/share/tok1')
  })

  it('regenerates the link when confirmation is accepted', async () => {
    mockConfirm.mockResolvedValue(true)
    fetchMock.mockResolvedValueOnce(jsonResponse({ token: 'tok1', path: '/share/tok1' }))
      .mockResolvedValueOnce(jsonResponse({ token: 'newtok', path: '/share/newtok' }))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('.share-dialog-link-btn[title="Regenerate tip"]').trigger('click')
    await flushPromises()
    await nextTick()

    const postCall = fetchMock.mock.calls.find((c: unknown[]) => c[1]?.method === 'POST')
    expect(postCall).toBeTruthy()
    expect((wrapper.find('input.share-dialog-link-input').element as HTMLInputElement).value).toBe('https://host.example/share/newtok')
  })
})
