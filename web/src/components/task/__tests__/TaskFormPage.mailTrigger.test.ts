import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import TaskFormPage from '../TaskFormPage.vue'

const formRef = ref({
  id: 0,
  name: '邮件任务',
  prompt: '处理这封邮件',
  agentId: 'coder',
  cronExpr: '',
  triggerMode: 'email',
  eventTypes: '',
  mailAccountId: 0,
  mailFolder: '',
  mailFrom: '',
  mailSubject: '',
  mailBody: '',
  mailHasAttachment: false,
  repeatMode: 'unlimited',
  maxRuns: 0,
  script: '',
  scriptTimeout: '',
})

vi.mock('@/composables/useTaskForm', () => ({
  useTaskForm: () => ({
    form: formRef,
    errors: ref({}),
    formError: ref(''),
    saving: ref(false),
    submit: vi.fn(),
    init: vi.fn(),
  }),
}))

vi.mock('@/utils/forgeApi', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('@/utils/forgeApi')
  return { ...actual, fetchForgeBinding: vi.fn().mockResolvedValue({ binding: null }) }
})

const apiGet = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => apiGet(...args),
}))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function mountForm() {
  return mount(TaskFormPage, {
    props: { mode: 'create' },
    global: { plugins: [i18n], stubs: { RefreshButton: true } },
  })
}

describe('TaskFormPage email trigger', () => {
  beforeEach(() => {
    formRef.value.triggerMode = 'email'
    formRef.value.mailAccountId = 0
    formRef.value.mailFolder = ''
    formRef.value.mailFrom = ''
    formRef.value.mailSubject = ''
    formRef.value.mailBody = ''
    formRef.value.mailHasAttachment = false
    apiGet.mockReset()
    apiGet.mockImplementation((url: string) => {
      if (url === '/api/mail/accounts') {
        return Promise.resolve({ accounts: [{ id: 7, name: '工作邮箱', email: 'me@example.com' }] })
      }
      if (url.startsWith('/api/mail/folders')) {
        return Promise.resolve({ folders: [{ id: 1, path: 'INBOX', name: '收件箱', specialUse: 'inbox' }] })
      }
      return Promise.resolve({})
    })
  })

  it('offers three trigger modes and marks the active one', async () => {
    const wrapper = mountForm()
    await flushPromises()
    const buttons = wrapper.findAll('.preset-btn')
    const labels = buttons.map((b) => b.text())
    expect(labels).toContain('Schedule')
    expect(labels).toContain('Event')
    expect(labels).toContain('Email')
    const emailBtn = buttons.find((b) => b.text() === 'Email')!
    expect(emailBtn.classes()).toContain('active')
  })

  it('loads mailboxes and renders the gate inputs', async () => {
    const wrapper = mountForm()
    await flushPromises()

    expect(apiGet).toHaveBeenCalledWith('/api/mail/accounts')
    // The mailbox picker is the app's MenuSelect, not a native <select>:
    // its trigger shows the current label.
    const triggers = wrapper.findAll('.menu-select')
    expect(triggers.length).toBeGreaterThanOrEqual(2)
    expect(triggers[0].text()).toContain('Select a mailbox')

    // Three substring gates + the attachment checkbox.
    expect(wrapper.findAll('.mail-gates input').length).toBe(3)
    expect(wrapper.find('.mail-attach-check input').exists()).toBe(true)
    expect(wrapper.text()).toContain('All filled conditions must match')
  })

  it('loads the folder list for the selected mailbox', async () => {
    const wrapper = mountForm()
    await flushPromises()

    formRef.value.mailAccountId = 7
    await flushPromises()

    expect(apiGet).toHaveBeenCalledWith('/api/mail/folders?accountId=7')
    const triggers = wrapper.findAll('.menu-select')
    // Account trigger now shows the selected mailbox; the folder trigger keeps
    // its "Inbox (default)" label.
    expect(triggers[0].text()).toContain('工作邮箱')
    expect(triggers[1].text()).toContain('Inbox (default)')
  })

  it('switches back to the cron form when Schedule is clicked', async () => {
    const wrapper = mountForm()
    await flushPromises()
    const schedule = wrapper.findAll('.preset-btn').find((b) => b.text() === 'Schedule')!
    await schedule.trigger('click')
    expect(formRef.value.triggerMode).toBe('cron')
    await flushPromises()
    expect(wrapper.find('.mail-gates').exists()).toBe(false)
  })
})
