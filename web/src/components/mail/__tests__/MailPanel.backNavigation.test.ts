import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import MailPanel from '../MailPanel.vue'

// The panel is a drill-down: list → reader → compose. Edge swipe / Android back
// must climb one level and stop at the list, otherwise the gesture either does
// nothing (the tab is left as-is) or exits the whole tab from a sub-page.

interface Captured {
  id: string
  canGoBack: () => boolean
  goBack: () => void
  priority: number
}
let captured: Captured | null = null

vi.mock('@/composables/useEdgeSwipeBack', () => ({
  PRIORITY_PAGE: 100,
  useFeatureBackHandler: (id: string, canGoBack: () => boolean, goBack: () => void, priority: number) => {
    captured = { id, canGoBack, goBack, priority }
  },
}))

vi.mock('@/composables/useMailAccounts', () => ({
  useMailAccounts: () => ({
    accounts: ref([{ id: 1, name: 'Box', email: 'b@e.com', lastSyncAt: 1700000000, enabled: true }]),
    presets: ref([]),
    loading: ref(false),
    error: ref(''),
    syncingId: ref(null),
    load: vi.fn(async () => {}),
    loadPresets: vi.fn(async () => {}),
    create: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    test: vi.fn(),
    sync: vi.fn(async () => {}),
  }),
}))

vi.mock('@/composables/useMailMessages', () => ({
  useMailMessages: () => ({
    folders: ref([{ id: 1, path: 'INBOX', name: 'Inbox', specialUse: 'inbox' }]),
    activeFolderId: ref(1),
    messages: ref([]),
    loading: ref(false),
    loadingMore: ref(false),
    error: ref(''),
    hasMore: ref(false),
    query: ref(''),
    loadFolders: vi.fn(async () => {}),
    selectFolder: vi.fn(async () => {}),
    reload: vi.fn(async () => {}),
    loadMore: vi.fn(),
    setQuery: vi.fn(),
    removeMessage: vi.fn(),
    patchMessage: vi.fn(),
  }),
}))

vi.mock('@/composables/useMailUnread', () => ({ refreshMailUnread: vi.fn() }))
vi.mock('@/utils/api', () => ({ apiGet: vi.fn(async () => ({ context: {} })) }))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function mountPanel() {
  return mount(MailPanel, {
    props: { active: true },
    global: {
      plugins: [i18n],
      stubs: {
        MailFolderBar: { name: 'MailFolderBar', template: '<div class="bar-stub" />', emits: ['compose'] },
        MailList: { name: 'MailList', template: '<div class="list-stub" />', emits: ['open'] },
        MailReader: { name: 'MailReader', template: '<div class="reader-stub" />', emits: ['reply'] },
        MailCompose: { name: 'MailCompose', template: '<div class="compose-stub" />', emits: ['sent'] },
        PanelSkeleton: { template: '<div class="skeleton-stub" />' },
      },
    },
  })
}

describe('MailPanel back navigation', () => {
  beforeEach(() => {
    captured = null
  })

  it('registers a page-level back handler', async () => {
    mountPanel()
    await flushPromises()
    expect(captured?.id).toBe('mail')
    expect(captured?.priority).toBe(100)
    // At the list there is nothing to go back to.
    expect(captured?.canGoBack()).toBe(false)
  })

  it('returns from the reader to the list', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findComponent({ name: 'MailList' }).vm.$emit('open', 7)
    await flushPromises()
    expect(wrapper.find('.reader-stub').exists()).toBe(true)
    expect(captured?.canGoBack()).toBe(true)

    captured!.goBack()
    await flushPromises()
    expect(wrapper.find('.list-stub').exists()).toBe(true)
    expect(captured?.canGoBack()).toBe(false)
  })

  it('returns from the composer to the list', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findComponent({ name: 'MailFolderBar' }).vm.$emit('compose')
    await flushPromises()
    expect(wrapper.find('.compose-stub').exists()).toBe(true)

    captured!.goBack()
    await flushPromises()
    expect(wrapper.find('.list-stub').exists()).toBe(true)
  })

  it('returns from a reply to the message it came from', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findComponent({ name: 'MailList' }).vm.$emit('open', 7)
    await flushPromises()
    await wrapper.findComponent({ name: 'MailReader' }).vm.$emit('reply', 'reply')
    await flushPromises()
    expect(wrapper.find('.compose-stub').exists()).toBe(true)

    captured!.goBack()
    await flushPromises()
    // Back means "where I came from", not "the list".
    expect(wrapper.find('.reader-stub').exists()).toBe(true)
  })
})
