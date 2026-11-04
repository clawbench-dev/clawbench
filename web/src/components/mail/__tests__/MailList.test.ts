import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import MailList from '../MailList.vue'
import type { MailMessage } from '@/utils/mailTypes'

// Narrow lucide mock: the whole registry, so adding an icon to the component
// does not break module loading here.
vi.mock('lucide-vue-next', async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
}))

function makeMessage(overrides: Partial<MailMessage> = {}): MailMessage {
  return {
    id: 1,
    accountId: 1,
    folderId: 1,
    uid: 1,
    messageId: 'm1@example.com',
    subject: '项目周报',
    fromName: '张三',
    fromAddr: 'zhang@example.com',
    to: [],
    cc: [],
    replyTo: '',
    inReplyTo: '',
    references: '',
    sentAt: Math.floor(Date.now() / 1000),
    size: 1024,
    seen: false,
    flagged: false,
    answered: false,
    hasAttachments: false,
    snippet: '本周进展…',
    bodyFetched: true,
    ...overrides,
  }
}

function mountList(props: Partial<InstanceType<typeof MailList>['$props']> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
  return mount(MailList, {
    props: {
      messages: [],
      loading: false,
      loadingMore: false,
      error: '',
      hasMore: false,
      query: '',
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

describe('MailList', () => {
  it('shows a skeleton on the first load', () => {
    const wrapper = mountList({ loading: true })
    expect(wrapper.find('.panel-skeleton').exists()).toBe(true)
  })

  it('shows the empty state when there is no mail', () => {
    const wrapper = mountList()
    expect(wrapper.text()).toContain('No mail here yet')
    expect(wrapper.find('.mail-row').exists()).toBe(false)
  })

  it('shows the no-results copy while searching', () => {
    const wrapper = mountList({ query: '发票' })
    expect(wrapper.text()).toContain('No matching mail')
  })

  it('shows the error state with a retry action', async () => {
    const wrapper = mountList({ error: 'boom' })
    expect(wrapper.text()).toContain('Could not load mail')
    const retry = wrapper.findAll('button').find((b) => b.text().includes('Retry'))
    expect(retry).toBeTruthy()
    await retry!.trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('renders rows and emits open with the message id', async () => {
    const messages = [
      makeMessage({ id: 7, subject: '第一封' }),
      makeMessage({ id: 8, subject: '第二封', seen: true }),
    ]
    const wrapper = mountList({ messages })
    const rows = wrapper.findAll('.mail-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].classes()).toContain('is-unread')
    expect(rows[1].classes()).not.toContain('is-unread')

    await rows[0].trigger('click')
    expect(wrapper.emitted('open')?.[0]).toEqual([7])
  })

  it('marks rows with attachments and stars', () => {
    const wrapper = mountList({
      messages: [makeMessage({ hasAttachments: true, flagged: true })],
    })
    expect(wrapper.find('.mail-row__clip').exists()).toBe(true)
    expect(wrapper.find('.mail-row__star').exists()).toBe(true)
  })
})
