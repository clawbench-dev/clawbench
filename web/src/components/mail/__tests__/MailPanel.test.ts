import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import SplitView from '@/components/common/SplitView.vue'
import type { MailAccount, MailFolder, MailReplyContext } from '@/utils/mailTypes'
import MailPanel from '../MailPanel.vue'

/**
 * The composer used to open as a modal on top of the message list. It is now
 * the panel's third drill-down view (list / reader / compose), so what this
 * guards is the view stack: compose replaces the current view, and the back
 * arrow returns to wherever it was started from (list or reader).
 */

const apiGet = vi.hoisted(() => vi.fn())
const apiPost = vi.hoisted(() => vi.fn())
const apiPatch = vi.hoisted(() => vi.fn())

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => apiGet(...args),
  apiPost: (...args: unknown[]) => apiPost(...args),
  apiPatch: (...args: unknown[]) => apiPatch(...args),
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
vi.mock('@/composables/useMailUnread', () => ({
  refreshMailUnread: vi.fn(),
}))
vi.mock('lucide-vue-next', async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
}))

const mocks = vi.hoisted(() => {
  const accounts: { value: MailAccount[] } = { value: [] }
  const folders: { value: MailFolder[] } = {
    value: [{ id: 1, accountId: 1, path: 'INBOX', name: 'INBOX', delimiter: '/', specialUse: 'inbox', unread: 0, total: 0 }],
  }
  const messages: { value: unknown[] } = { value: [] }
  return {
    accounts,
    folders,
    messages,
    activeFolderId: { value: 1 },
    query: { value: '' },
    loading: { value: false },
    loadingMore: { value: false },
    error: { value: '' },
    hasMore: { value: false },
    loadFolders: vi.fn(async () => undefined),
    selectFolder: vi.fn(async () => undefined),
    reload: vi.fn(async () => undefined),
    loadMore: vi.fn(async () => undefined),
    setQuery: vi.fn(async () => undefined),
    removeMessage: vi.fn(),
    patchMessage: vi.fn(),
    load: vi.fn(async () => undefined),
    sync: vi.fn(async () => undefined),
  }
})

vi.mock('@/composables/useMailAccounts', () => ({
  useMailAccounts: () => ({
    accounts: mocks.accounts,
    load: mocks.load,
    sync: mocks.sync,
  }),
}))
vi.mock('@/composables/useMailMessages', () => ({
  useMailMessages: () => ({
    folders: mocks.folders,
    messages: mocks.messages,
    activeFolderId: mocks.activeFolderId,
    loading: mocks.loading,
    loadingMore: mocks.loadingMore,
    error: mocks.error,
    hasMore: mocks.hasMore,
    query: mocks.query,
    loadFolders: mocks.loadFolders,
    selectFolder: mocks.selectFolder,
    reload: mocks.reload,
    loadMore: mocks.loadMore,
    setQuery: mocks.setQuery,
    removeMessage: mocks.removeMessage,
    patchMessage: mocks.patchMessage,
  }),
}))

// Reader and list are stubbed so the test is only about the view stack.
vi.mock('../MailList.vue', () => ({
  default: defineComponent({
    props: { activeId: { type: Number, default: 0 } },
    emits: ['open', 'load-more', 'retry'],
    template:
      '<div class="list-stub" :data-active-id="activeId"><button class="list-stub-open" @click="$emit(\'open\', 7)">open</button></div>',
  }),
}))
vi.mock('../MailReader.vue', () => ({
  default: defineComponent({
    props: { messageId: { type: Number, required: true } },
    emits: ['back', 'reply', 'deleted', 'updated'],
    template:
      '<div class="reader-stub" :data-msg="messageId"><button class="reader-stub-reply" @click="$emit(\'reply\', \'reply\')">reply</button></div>',
  }),
}))

function mountPanel() {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })
  return mount(MailPanel, { global: { plugins: [i18n] } })
}

async function openCompose(wrapper: ReturnType<typeof mountPanel>) {
  await wrapper.find('button[title="Compose"]').trigger('click')
  await flushPromises()
}

describe('MailPanel quote jump', () => {
  it('opens the reader for the message a quote points at', async () => {
    mocks.accounts.value = [{ id: 1, lastSyncAt: 123 } as MailAccount]
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.reader-stub').exists()).toBe(false)

    // App switches the tab; the panel only has to open the reader. The id comes
    // from the quote, so the message need not be on the loaded list page.
    window.dispatchEvent(new CustomEvent('clawbench-open-mail', { detail: { messageId: 42 } }))
    await nextTick()

    const reader = wrapper.find('.reader-stub')
    expect(reader.exists(), 'the jump must land on the reader').toBe(true)
    expect(reader.attributes('data-msg')).toBe('42')
    wrapper.unmount()
  })
})

describe('MailPanel compose view', () => {
  it('opens the composer as a drill-down view, not a modal', async () => {
    mocks.accounts.value = [{ id: 1, lastSyncAt: 123 } as MailAccount]
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.mail-compose').exists()).toBe(false)
    await openCompose(wrapper)

    // The form is rendered inline in the panel — no ModalDialog shell around it.
    expect(wrapper.find('.mail-compose').exists()).toBe(true)
    expect(wrapper.find('.mail-compose__head').text()).toContain('New message')
    expect(wrapper.findAll('.mail-compose__input')).toHaveLength(2) // to + subject
    expect(wrapper.find('.mail-compose__actions').exists()).toBe(true)
  })

  it('returns to the list when the composer was started from the list', async () => {
    mocks.accounts.value = [{ id: 1, lastSyncAt: 123 } as MailAccount]
    const wrapper = mountPanel()
    await flushPromises()

    await openCompose(wrapper)
    await wrapper.find('.mail-compose__head .mail-icon-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('.mail-compose').exists()).toBe(false)
    expect(wrapper.find('.list-stub-open').exists()).toBe(true)
  })

  it('returns to the reader when the composer was started from a reply', async () => {
    mocks.accounts.value = [{ id: 1, lastSyncAt: 123 } as MailAccount]
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.list-stub-open').trigger('click')
    expect(wrapper.find('.reader-stub').exists()).toBe(true)
    // Narrow panel: the reader replaces the list instead of sitting beside it.
    expect(wrapper.find('.list-stub-open').exists()).toBe(false)
    expect(wrapper.find('.mail-panel__split').classes()).not.toContain('split-view--active')

    apiGet.mockResolvedValue({ context: { to: 'you@example.com' } as MailReplyContext })
    await wrapper.find('.reader-stub-reply').trigger('click')
    await flushPromises()

    expect(wrapper.find('.mail-compose').exists()).toBe(true)
    const toInput = wrapper.find('.mail-compose__input').element as HTMLInputElement
    expect(toInput.value).toBe('you@example.com')

    await wrapper.find('.mail-compose__head .mail-icon-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('.mail-compose').exists()).toBe(false)
    expect(wrapper.find('.reader-stub').exists()).toBe(true)
    expect(apiGet).toHaveBeenCalledWith('/api/mail/messages/7/reply-context?mode=reply')
  })

  it('flushes the draft when leaving the composer', async () => {
    mocks.accounts.value = [{ id: 1, lastSyncAt: 123 } as MailAccount]
    apiPost.mockResolvedValue({ draft: { id: 9 } })
    const wrapper = mountPanel()
    await flushPromises()

    await openCompose(wrapper)
    await wrapper.find('.mail-compose__input').setValue('someone@example.com')
    await wrapper.find('.mail-compose__head .mail-icon-btn').trigger('click')
    await flushPromises()

    expect(apiPost).toHaveBeenCalledWith('/api/mail/drafts', expect.objectContaining({
      accountId: 1,
      to: 'someone@example.com',
    }))
  })
})

describe('MailPanel wide layout', () => {
  class FakeResizeObserver {
    static width = 0
    private readonly cb: ResizeObserverCallback
    constructor(cb: ResizeObserverCallback) {
      this.cb = cb
    }
    observe(): void {
      this.cb(
        [{ contentRect: { width: FakeResizeObserver.width } } as ResizeObserverEntry],
        this as unknown as ResizeObserver,
      )
    }
    unobserve(): void {}
    disconnect(): void {}
  }

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('keeps the list beside the opened message when the panel is wide', async () => {
    mocks.accounts.value = [{ id: 1, lastSyncAt: 123 } as MailAccount]
    FakeResizeObserver.width = 700
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)

    const wrapper = mountPanel()
    await flushPromises()
    // Nothing selected: no split, so no column is reserved on the right.
    expect(wrapper.find('.mail-panel__split').classes()).not.toContain('split-view--active')
    expect(wrapper.find('.split-view__divider').exists()).toBe(false)

    await wrapper.find('.list-stub-open').trigger('click')

    // Both panes stay on screen: reading does not take the list away.
    expect(wrapper.find('.list-stub-open').exists()).toBe(true)
    expect(wrapper.find('.reader-stub').exists()).toBe(true)
    expect(wrapper.find('.mail-panel__split').classes()).toContain('split-view--active')
    // …and the row of the opened message is marked as selected.
    expect(wrapper.find('.list-stub').attributes('data-active-id')).toBe('7')
    wrapper.unmount()
  })

  it('separates the two panes with the shared draggable divider and remembers the ratio', async () => {
    mocks.accounts.value = [{ id: 1, lastSyncAt: 123 } as MailAccount]
    FakeResizeObserver.width = 700
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
    localStorage.setItem('clawbench-panel.mail-split-ratio', '0.5')

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('.list-stub-open').trigger('click')

    // The stored ratio drives the panes, and the divider is the shared
    // SplitDivider (its pointer-drag behaviour is covered by SplitView tests).
    expect(wrapper.find('.split-view__divider').exists()).toBe(true)
    expect(wrapper.find('.split-view__left').attributes('style')).toContain('width: 50%')

    // A drag ends in `update:ratio`, which has to reach the layout registry.
    wrapper.findComponent(SplitView).vm.$emit('update:ratio', 0.6)
    await nextTick()
    expect(localStorage.getItem('clawbench-panel.mail-split-ratio')).toBe('0.6')
    wrapper.unmount()
  })

  it('opens the composer beside the list on a wide panel', async () => {
    mocks.accounts.value = [{ id: 1, lastSyncAt: 123 } as MailAccount]
    FakeResizeObserver.width = 700
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)

    const wrapper = mountPanel()
    await flushPromises()
    await openCompose(wrapper)

    expect(wrapper.find('.mail-compose').exists()).toBe(true)
    expect(wrapper.find('.list-stub-open').exists()).toBe(true)
    wrapper.unmount()
  })
})

describe('MailPanel folder bar', () => {
  it('marks the current folder read through the folder endpoint and refetches', async () => {
    mocks.accounts.value = [{ id: 1, lastSyncAt: 123 } as MailAccount]
    mocks.folders.value = [{
      id: 1, accountId: 1, path: 'INBOX', name: 'INBOX', delimiter: '/',
      specialUse: 'inbox', unread: 4, total: 9,
    }]
    apiPost.mockResolvedValue({ ok: true })
    mocks.reload.mockClear()
    mocks.loadFolders.mockClear()
    const wrapper = mountPanel()
    await flushPromises()

    const button = wrapper.find('.mail-read-all')
    expect(button.text()).toBe('Mark all read')
    await button.trigger('click')
    await flushPromises()

    expect(apiPost).toHaveBeenCalledWith('/api/mail/folders/1/read-all', {})
    // The server marks the whole mailbox, so the cache is refetched rather
    // than patching the rows that happen to be loaded.
    expect(mocks.reload).toHaveBeenCalled()
    expect(mocks.loadFolders).toHaveBeenCalled()
    wrapper.unmount()
  })
})
