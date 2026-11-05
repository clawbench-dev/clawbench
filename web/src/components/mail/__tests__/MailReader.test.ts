import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import MailReader from '../MailReader.vue'

const apiGet = vi.fn()
const apiPost = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => apiGet(...args),
  apiPost: (...args: unknown[]) => apiPost(...args),
}))
vi.mock('@/composables/useMailUnread', () => ({
  refreshMailUnread: vi.fn(),
}))

// Handing an email to the chat goes through the shared quote bar and the shared
// upload pipeline; both are stubbed so the test asserts WHAT was handed over.
const composer = vi.hoisted(() => ({ openComposer: vi.fn() }))
vi.mock('@/composables/useQuoteQuestion', () => ({ useQuoteQuestion: () => composer }))
const uploader = vi.hoisted(() => ({ uploadAndAttach: vi.fn(async () => {}) }))
vi.mock('@/composables/useFileUpload', () => ({ useFileUpload: () => uploader }))
const toastShow = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: toastShow }) }))
vi.mock('lucide-vue-next', async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
}))

const detail = {
  id: 5,
  accountId: 1,
  folderId: 1,
  uid: 9,
  messageId: 'm5@example.com',
  subject: '接口变更说明',
  fromName: '李四',
  fromAddr: 'li@example.com',
  to: [{ name: '', addr: 'me@example.com' }],
  cc: [],
  replyTo: '',
  inReplyTo: '',
  references: '',
  sentAt: Math.floor(Date.now() / 1000),
  size: 2048,
  seen: true,
  flagged: false,
  answered: false,
  hasAttachments: true,
  snippet: '',
  bodyFetched: true,
  bodyText: '正文第一行\n正文第二行',
  bodyHtml: '',
  attachments: [
    { id: 11, messageId: 5, filename: 'report.pdf', contentType: 'application/pdf', size: 2048, contentId: '', inline: false },
    { id: 12, messageId: 5, filename: 'logo.png', contentType: 'image/png', size: 100, contentId: 'logo', inline: true },
  ],
}

function mountReader() {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
  return mount(MailReader, {
    props: { messageId: 5 },
    global: { plugins: [i18n] },
  })
}

describe('MailReader', () => {
  it('renders the subject, body and non-inline attachments', async () => {
    apiGet.mockResolvedValue({ message: detail })
    const wrapper = mountReader()
    await flushPromises()

    expect(wrapper.text()).toContain('接口变更说明')
    expect(wrapper.find('.mail-reader__text').text()).toContain('正文第一行')
    const links = wrapper.findAll('a.mail-attachment')
    expect(links).toHaveLength(1)
    expect(links[0].text()).toContain('report.pdf')
    expect(links[0].attributes('href')).toBe('/api/mail/messages/5/attachments/11')
  })

  it('renders HTML mail inside a sandboxed iframe', async () => {
    apiGet.mockResolvedValue({
      message: { ...detail, bodyText: '', bodyHtml: '<p>rich</p><img src="https://track.test/p.png">' },
    })
    const wrapper = mountReader()
    await flushPromises()

    const frame = wrapper.find('iframe.mail-reader__frame')
    expect(frame.exists()).toBe(true)
    const sandbox = frame.attributes('sandbox') ?? ''
    expect(sandbox).toContain('allow-same-origin')
    expect(sandbox).not.toContain('allow-scripts')
    // Remote images are blocked until the user asks for them.
    expect(wrapper.text()).toContain('Remote images are blocked')
  })

  it('emits reply modes and back', async () => {
    apiGet.mockResolvedValue({ message: detail })
    const wrapper = mountReader()
    await flushPromises()

    const button = (label: string) => wrapper.findAll('button').find((b) => b.text() === label)!
    await button('Reply').trigger('click')
    await button('Reply all').trigger('click')
    await button('Forward').trigger('click')

    expect(wrapper.emitted('reply')).toEqual([['reply'], ['replyAll'], ['forward']])

    await wrapper.find('.mail-icon-btn').trigger('click')
    expect(wrapper.emitted('back')).toHaveLength(1)
  })

  it('toggles the star through the flags endpoint', async () => {
    apiGet.mockResolvedValue({ message: detail })
    apiPost.mockResolvedValue({ message: { ...detail, flagged: true } })
    const wrapper = mountReader()
    await flushPromises()

    const star = wrapper.findAll('.mail-icon-btn')[1]
    await star.trigger('click')
    await flushPromises()

    expect(apiPost).toHaveBeenCalledWith('/api/mail/messages/5/flags', { flagged: true })
    expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ flagged: true })
  })

  it('deletes through the delete endpoint and reports it', async () => {
    apiGet.mockResolvedValue({ message: detail })
    apiPost.mockResolvedValue({ ok: true })
    const wrapper = mountReader()
    await flushPromises()

    const del = wrapper.findAll('button').find((b) => b.text() === 'Delete')!
    await del.trigger('click')
    await flushPromises()

    expect(apiPost).toHaveBeenCalledWith('/api/mail/messages/5/delete', {})
    expect(wrapper.emitted('deleted')).toHaveLength(1)
  })

  it('shows the error state when the message cannot be loaded', async () => {
    apiGet.mockRejectedValue({ msgKey: 'MailServerError', message: 'nope' })
    const wrapper = mountReader()
    await flushPromises()
    expect(wrapper.text()).toContain('Could not open the message')
  })

  // ── Handing the email to the chat ──
  it('quotes the email through the shared composer, with its locator', async () => {
    apiGet.mockResolvedValue({ message: detail })
    composer.openComposer.mockClear()
    const wrapper = mountReader()
    await flushPromises()

    const quoteBtn = wrapper.findAll('button').find((b) => b.text() === 'Quote in chat')!
    expect(quoteBtn, 'the reader must offer a quote action').toBeTruthy()
    await quoteBtn.trigger('click')

    expect(composer.openComposer).toHaveBeenCalledTimes(1)
    const ctx = composer.openComposer.mock.calls[0][0] as { quote: Record<string, unknown>; label: string }
    // The body is inlined: the AI cannot fetch an email from `mail: <id>`.
    expect(ctx.quote.text).toBe('正文第一行\n正文第二行')
    expect(ctx.quote).toMatchObject({ sourceKind: 'mail', language: 'mail', mailId: 5 })
    // The label is what the card shows: subject + sender.
    expect(ctx.quote.filePath).toContain('接口变更说明')
    expect(ctx.quote.filePath).toContain('李四')
    expect(ctx.label).toBe(ctx.quote.filePath)
  })

  it('adds an attachment to the chat by re-uploading its bytes', async () => {
    apiGet.mockResolvedValue({ message: detail })
    uploader.uploadAndAttach.mockClear()
    const fetchMock = vi.fn(async () => ({
      ok: true,
      blob: async () => new Blob(['%PDF-1.4'], { type: 'application/pdf' }),
    }))
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mountReader()
    await flushPromises()

    await wrapper.find('.mail-attachment__chat').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/mail/messages/5/attachments/11')
    expect(uploader.uploadAndAttach).toHaveBeenCalledTimes(1)
    const files = uploader.uploadAndAttach.mock.calls[0][0] as File[]
    expect(files).toHaveLength(1)
    expect(files[0].name).toBe('report.pdf')
    vi.unstubAllGlobals()
  })

  it('reports a failed attachment copy instead of silently doing nothing', async () => {
    apiGet.mockResolvedValue({ message: detail })
    uploader.uploadAndAttach.mockClear()
    toastShow.mockClear()
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500 })))

    const wrapper = mountReader()
    await flushPromises()
    await wrapper.find('.mail-attachment__chat').trigger('click')
    await flushPromises()

    expect(uploader.uploadAndAttach).not.toHaveBeenCalled()
    expect(toastShow).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})
