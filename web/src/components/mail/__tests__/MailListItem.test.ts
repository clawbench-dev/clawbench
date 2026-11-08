import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import en from '@/i18n/locales/en'
import MailListItem from '../MailListItem.vue'
import type { MailMessage } from '@/utils/mailTypes'
import { readQuoteDragData } from '@/utils/quoteDrag'

// Quoting from the row fetches the full body (the list only has a snippet), so
// the API layer is mocked. The bar and the toast are singletons with their own
// suites; here we only assert what the row hands them.
const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({ apiGet: (...a: unknown[]) => mockApiGet(...a) }))

const mockOpenComposer = vi.fn()
vi.mock('@/composables/useQuoteQuestion', () => ({
  useQuoteQuestion: () => ({ openComposer: mockOpenComposer }),
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function message(sentAt: number): MailMessage {
  return {
    id: 1,
    accountId: 1,
    folderId: 1,
    uid: 1,
    messageId: 'x@y',
    subject: '周报',
    fromName: '老板',
    fromAddr: 'boss@corp.com',
    to: ['me@corp.com'],
    cc: [],
    replyTo: [],
    inReplyTo: '',
    references: [],
    sentAt,
    size: 1024,
    seen: false,
    flagged: false,
    answered: false,
    hasAttachments: false,
    snippet: '本周进展',
    bodyFetched: true,
  } as MailMessage
}

function mountRow(sentAt: number) {
  return mount(MailListItem, {
    props: { msg: message(sentAt) },
    global: { plugins: [i18n] },
  })
}

describe('MailListItem time', () => {
  it('shows the friendly day and the exact clock side by side', () => {
    const now = Math.floor(Date.now() / 1000)
    const wrapper = mountRow(now)

    const day = wrapper.find('.mail-row__day')
    const clock = wrapper.find('.mail-row__clock')
    expect(day.exists()).toBe(true)
    expect(clock.exists()).toBe(true)
    // Both are rendered at once — showing only one answers half the question.
    expect(day.text().trim().length).toBeGreaterThan(0)
    expect(clock.text()).toMatch(/^\d{2}:\d{2}$/)
  })

  it('keeps an older message readable: date label plus its clock time', () => {
    const threeDaysAgo = Math.floor(Date.now() / 1000) - 3 * 86400
    const wrapper = mountRow(threeDaysAgo)

    expect(wrapper.find('.mail-row__day').text().trim().length).toBeGreaterThan(0)
    expect(wrapper.find('.mail-row__clock').text()).toMatch(/^\d{2}:\d{2}$/)
  })

  it('carries the full timestamp as a tooltip', () => {
    const now = Math.floor(Date.now() / 1000)
    const wrapper = mountRow(now)
    const title = wrapper.find('.mail-row__time').attributes('title') || ''
    expect(title).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
  })

  it('drags as a mail quote: identity only, the body is fetched on drop', async () => {
    const wrapper = mountRow(Math.floor(Date.now() / 1000))
    const store = new Map<string, string>()
    const dt = {
      setData: (type: string, value: string) => { store.set(type, value) },
      getData: (type: string) => store.get(type) || '',
      get types() { return [...store.keys()] },
      setDragImage: () => {},
      effectAllowed: '',
    }

    await wrapper.find('.mail-row').trigger('dragstart', { dataTransfer: dt })

    const payload = readQuoteDragData(dt as unknown as DataTransfer)
    expect(payload, 'the row must write a quote payload').toBeTruthy()
    expect(payload).toMatchObject({ mailId: 1, language: 'mail', sourceKind: 'mail', text: '' })
    expect(payload!.filePath).toBe('周报 · 老板')
  })
})

describe('MailListItem quote action', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('offers the same row action as the task and forge lists', () => {
    const wrapper = mountRow(Math.floor(Date.now() / 1000))
    expect(wrapper.find('.mail-row__quote').exists(), 'the row must expose a quote action').toBe(true)
  })

  it('fetches the body and opens the quote bar without opening the mail', async () => {
    // The list only carries a snippet; the quote is for the AI, which cannot
    // read a mailbox from an id, so the body must be fetched before staging.
    mockApiGet.mockResolvedValue({
      message: { ...message(Math.floor(Date.now() / 1000)), bodyText: '本周进展全文' },
    })
    const wrapper = mountRow(Math.floor(Date.now() / 1000))

    await wrapper.find('.mail-row__quote').trigger('click')
    await flushPromises()

    expect(mockApiGet).toHaveBeenCalledWith('/api/mail/messages/1')
    expect(mockOpenComposer).toHaveBeenCalledTimes(1)
    const ctx = mockOpenComposer.mock.calls[0][0] as {
      quote?: { language?: string; text?: string; mailId?: number }
      label?: string
    }
    expect(ctx.quote?.language).toBe('mail')
    expect(ctx.quote?.text).toBe('本周进展全文')
    expect(ctx.quote?.mailId).toBe(1)
    expect(ctx.label).toBe('周报 · 老板')
    // `@click.stop`: quoting must not also open the mail.
    expect(wrapper.emitted('open')).toBeFalsy()
  })

  it('still opens the mail from the row itself', async () => {
    const wrapper = mountRow(Math.floor(Date.now() / 1000))
    await wrapper.find('.mail-row__open').trigger('click')
    expect(wrapper.emitted('open')).toEqual([[1]])
  })

  it('reports a failed fetch instead of silently doing nothing', async () => {
    mockApiGet.mockRejectedValue(new Error('boom'))
    const wrapper = mountRow(Math.floor(Date.now() / 1000))

    await wrapper.find('.mail-row__quote').trigger('click')
    await flushPromises()

    expect(mockOpenComposer).not.toHaveBeenCalled()
    expect(mockToastShow).toHaveBeenCalled()
  })
})

describe('MailListItem quote placement', () => {
  // jsdom has no layout, so this pins the INTENT in the stylesheet: the action
  // floats over the snippet's right end. Laid out as a flex sibling it took a
  // full-height column, shrinking the sender / subject / meta lines too — the
  // user asked for it to cost only the snippet area.
  const src = readFileSync(join(__dirname, '..', 'MailListItem.vue'), 'utf8')
  const rule = (selector: string) =>
    new RegExp(`\\${selector}\\s*\\{([^}]*)\\}`).exec(src)?.[1] ?? ''

  it('positions the action in the corner instead of giving it a column', () => {
    const quote = rule('.mail-row__quote')
    expect(quote).toContain('position: absolute')
    expect(quote).toContain('bottom:')
  })

  it('reserves the corner only on the snippet line', () => {
    expect(rule('.mail-row__snippet')).toContain('padding-right')
  })
})
