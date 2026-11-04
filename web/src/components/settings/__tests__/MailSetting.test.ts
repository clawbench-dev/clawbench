import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import MailSetting from '../MailSetting.vue'

const accounts = ref([
  {
    id: 1,
    name: '工作邮箱',
    email: 'work@example.com',
    username: 'work@example.com',
    imapHost: 'imap.example.com',
    imapPort: 993,
    imapSecurity: 'tls',
    smtpHost: 'smtp.example.com',
    smtpPort: 465,
    smtpSecurity: 'tls',
    insecureTls: false,
    enabled: true,
    sortOrder: 0,
    lastSyncAt: Math.floor(Date.now() / 1000),
    lastError: '',
    hasPassword: true,
    createdAt: 0,
  },
])

const apiLoad = vi.fn().mockResolvedValue(undefined)
const apiRemove = vi.fn().mockResolvedValue(undefined)
const apiTest = vi.fn().mockResolvedValue(undefined)
const confirm = vi.fn().mockResolvedValue(true)

vi.mock('@/composables/useMailAccounts', () => ({
  useMailAccounts: () => ({
    accounts,
    presets: ref([{ id: 'qq', label: 'QQ', imapHost: 'imap.qq.com', imapPort: 993, imapSecurity: 'tls', smtpHost: 'smtp.qq.com', smtpPort: 465, smtpSecurity: 'tls' }]),
    loading: ref(false),
    error: ref(''),
    syncingId: ref(null),
    load: apiLoad,
    loadPresets: vi.fn().mockResolvedValue(undefined),
    create: vi.fn(),
    update: vi.fn(),
    remove: apiRemove,
    test: apiTest,
    sync: vi.fn(),
  }),
}))

vi.mock('@/composables/useDialog', () => ({ useDialog: () => ({ confirm }) }))
vi.mock('@/composables/useMailUnread', () => ({ refreshMailUnread: vi.fn() }))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function mountSetting() {
  return mount(MailSetting, {
    global: {
      plugins: [i18n],
      stubs: {
        ModalDialog: { template: '<div class="modal-stub"><slot /></div>', props: ['open', 'title'] },
        MailAccountForm: { template: '<div class="form-stub" />' },
      },
    },
  })
}

describe('MailSetting', () => {
  beforeEach(() => {
    accounts.value = [
      {
        id: 1, name: '工作邮箱', email: 'work@example.com', username: 'work@example.com',
        imapHost: 'imap.example.com', imapPort: 993, imapSecurity: 'tls',
        smtpHost: 'smtp.example.com', smtpPort: 465, smtpSecurity: 'tls',
        insecureTls: false, enabled: true, sortOrder: 0,
        lastSyncAt: Math.floor(Date.now() / 1000), lastError: '', hasPassword: true, createdAt: 0,
      },
    ]
    apiLoad.mockClear()
    apiRemove.mockClear()
    apiTest.mockClear()
    confirm.mockClear()
    confirm.mockResolvedValue(true)
  })

  it('lists configured mailboxes with their address', async () => {
    const wrapper = mountSetting()
    await flushPromises()
    expect(apiLoad).toHaveBeenCalled()
    expect(wrapper.text()).toContain('工作邮箱')
    expect(wrapper.text()).toContain('work@example.com')
    expect(wrapper.findAll('.mail-row')).toHaveLength(1)
  })

  it('shows the empty hint when no mailbox exists', async () => {
    accounts.value = []
    const wrapper = mountSetting()
    await flushPromises()
    expect(wrapper.text()).toContain('No mailbox yet')
    expect(wrapper.find('[data-action="mail-add"]').exists()).toBe(true)
  })

  it('tests a mailbox from its row', async () => {
    const wrapper = mountSetting()
    await flushPromises()
    const testBtn = wrapper.find('[title="Test"]')
    await testBtn.trigger('click')
    await flushPromises()
    expect(apiTest).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('"工作邮箱" is reachable.')
  })

  it('asks for confirmation before deleting and reloads afterwards', async () => {
    const wrapper = mountSetting()
    await flushPromises()
    const del = wrapper.find('[title="Delete"]')
    await del.trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalled()
    expect(apiRemove).toHaveBeenCalledWith(1)
    // load() ran once on mount and once after the delete.
    expect(apiLoad.mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  it('does not delete when the confirmation is dismissed', async () => {
    confirm.mockResolvedValue(false)
    const wrapper = mountSetting()
    await flushPromises()
    const del = wrapper.find('[title="Delete"]')
    await del.trigger('click')
    await flushPromises()
    expect(apiRemove).not.toHaveBeenCalled()
  })

  it('opens the account form dialog from the add button', async () => {
    accounts.value = []
    const wrapper = mountSetting()
    await flushPromises()
    await wrapper.find('[data-action="mail-add"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.form-stub').exists()).toBe(true)
  })
})
