import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import MailPanel from '../MailPanel.vue'

// A mailbox added in Settings belongs to a different composable instance, so
// the panel holds a stale (usually empty) account list until it refetches.
// Coming back to the tab used to show "还没有配置邮箱" until the app reloaded.
const accounts = ref<Array<Record<string, unknown>>>([])
const serverAccounts = ref<Array<Record<string, unknown>>>([])
const loadAccounts = vi.fn(async () => {
  accounts.value = serverAccounts.value
})
const syncAccount = vi.fn(async () => {})

vi.mock('@/composables/useMailAccounts', () => ({
  useMailAccounts: () => ({
    accounts,
    presets: ref([]),
    loading: ref(false),
    error: ref(''),
    syncingId: ref(null),
    load: loadAccounts,
    loadPresets: vi.fn(async () => {}),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    test: vi.fn(),
    sync: syncAccount,
  }),
}))

const folders = ref<Array<Record<string, unknown>>>([])
const loadFolders = vi.fn(async (_id: number) => {})
const selectFolder = vi.fn(async (_id: number) => {})

vi.mock('@/composables/useMailMessages', () => ({
  useMailMessages: () => ({
    folders,
    activeFolderId: ref(0),
    messages: ref([]),
    loading: ref(false),
    loadingMore: ref(false),
    error: ref(''),
    hasMore: ref(false),
    query: ref(''),
    loadFolders,
    selectFolder,
    reload: vi.fn(async () => {}),
    loadMore: vi.fn(),
    setQuery: vi.fn(),
  }),
}))

vi.mock('@/composables/useMailUnread', () => ({ refreshMailUnread: vi.fn() }))
vi.mock('@/utils/api', () => ({ apiGet: vi.fn(async () => ({})) }))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function mountPanel(active = true) {
  return mount(MailPanel, {
    props: { active },
    global: {
      plugins: [i18n],
      stubs: {
        MailFolderBar: { template: '<div class="bar-stub" />' },
        MailList: { template: '<div class="list-stub" />' },
        MailReader: { template: '<div class="reader-stub" />' },
        MailCompose: { template: '<div class="compose-stub" />' },
        PanelSkeleton: { template: '<div class="skeleton-stub" />' },
      },
    },
  })
}

function account(id: number, lastSyncAt: number) {
  return { id, name: `Box ${id}`, email: `box${id}@example.com`, lastSyncAt, lastError: '', enabled: true }
}

describe('MailPanel account refresh on activation', () => {
  beforeEach(() => {
    accounts.value = []
    serverAccounts.value = []
    folders.value = []
    loadAccounts.mockClear()
    syncAccount.mockClear()
    loadFolders.mockClear()
    selectFolder.mockClear()
  })

  it('shows the empty state when no mailbox is configured', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-action="mail-empty-settings"]').exists()).toBe(true)
  })

  it('picks up a mailbox added in Settings when the tab becomes active again', async () => {
    const wrapper = mountPanel(false)
    await flushPromises()
    expect(wrapper.find('.bar-stub').exists()).toBe(false)

    // Configured on another screen: only the server knows, the panel's own list
    // is still empty.
    serverAccounts.value = [account(7, 1700000000)]
    folders.value = [{ id: 1, path: 'INBOX', name: 'Inbox', specialUse: 'inbox' }]

    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(loadAccounts).toHaveBeenCalled()
    expect(wrapper.find('.bar-stub').exists()).toBe(true)
    expect(wrapper.find('[data-action="mail-empty-settings"]').exists()).toBe(false)
    expect(selectFolder).toHaveBeenCalledWith(1)
  })

  it('syncs a mailbox that has never been synced instead of showing an empty inbox', async () => {
    const wrapper = mountPanel(false)
    await flushPromises()

    serverAccounts.value = [account(9, 0)]
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(syncAccount).toHaveBeenCalledWith(9)
  })

  it('does not run a full sync on every tab switch once the mailbox has synced', async () => {
    const wrapper = mountPanel(false)
    await flushPromises()

    serverAccounts.value = [account(3, 1700000000)]
    await wrapper.setProps({ active: true })
    await flushPromises()
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(syncAccount).not.toHaveBeenCalled()
  })
})
