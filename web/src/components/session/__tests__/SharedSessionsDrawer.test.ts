import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import SharedSessionsDrawer from '@/components/session/SharedSessionsDrawer.vue'
import { onTabSwitch } from '@/composables/useTabDrawer'

// ── Mocks ──

// Mock BottomSheet — passthrough that only renders while open.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    props: ['open', 'auto', 'title'],
    emits: ['close'],
    template: '<div class="bottom-sheet-stub" v-if="$props.open"><slot name="header" /><slot /></div>',
  }),
}))

// AgentIcon pulls in a large SVG registry; a stub is enough for these tests.
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: defineComponent({
    props: ['backend', 'name', 'size'],
    template: '<span class="agent-icon-stub" />',
  }),
}))

const h = vi.hoisted(() => ({
  toastShow: vi.fn(),
  copyText: vi.fn((_t: string, onSuccess?: () => void) => onSuccess?.()),
  confirm: vi.fn().mockResolvedValue(true),
  markShared: vi.fn(),
  markUnshared: vi.fn(),
  resetSessionShareState: vi.fn(),
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

vi.mock('@/composables/useSessionShare', () => ({
  useSessionShare: () => ({
    markShared: h.markShared,
    markUnshared: h.markUnshared,
    resetSessionShareState: h.resetSessionShareState,
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const messages = {
  en: {
    common: { retry: 'Retry', loading: 'Loading...' },
    share: { sharedConversation: 'Shared conversation' },
    sharedSessions: {
      button: 'Shared conversations',
      title: 'Shared conversations',
      empty: 'No shared conversations yet',
      openConversation: 'Open conversation',
      openInNewTab: 'Open link in new tab',
      copyLink: 'Copy link',
      copied: 'Link copied',
      revoke: 'Revoke share',
      revoked: 'Share revoked',
      clearAll: 'Clear all',
      clear: 'Clear',
      confirmClearAll: 'Clear every shared conversation?',
      archived: 'Conversation archived',
      archivedHint: 'Archived, but the link still works.',
      messageCount: '{count} messages',
      confirmRevoke: 'Revoke "{name}"?',
    },
  },
}
const i18n = createI18n({ legacy: false, locale: 'en', messages, missingWarn: false, fallbackWarn: false })

function mountDrawer() {
  return mount(SharedSessionsDrawer, { global: { plugins: [i18n] } })
}

function jsonResponse(body: unknown, ok = true): Response {
  return { ok, json: () => Promise.resolve(body) } as unknown as Response
}

async function openAndLoad(wrapper: ReturnType<typeof mountDrawer>) {
  ;(wrapper.vm as any).open()
  await flushPromises()
  await nextTick()
}

describe('SharedSessionsDrawer', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    // The drawer is bound to the chat tab via useTabDrawer; activate it.
    onTabSwitch('chat')
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    Object.defineProperty(window, 'location', { value: { origin: 'https://host.example', pathname: '/' }, writable: true })
    vi.clearAllMocks()
    h.confirm.mockResolvedValue(true)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('loads the list when opened and renders a row per share', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [
        { token: 'tok1', sessionId: 's1', title: 'Fix login', backend: 'codebuddy', messageCount: 4, createdAt: '2026-01-01', archived: false },
        { token: 'tok2', sessionId: 's2', title: 'Refactor api', backend: 'claude', messageCount: 9, createdAt: '2026-01-02', archived: false },
      ],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)

    expect(fetchMock).toHaveBeenCalledWith('/api/share/session/list')
    expect(wrapper.text()).toContain('Fix login')
    expect(wrapper.text()).toContain('Refactor api')
    expect(wrapper.text()).toContain('4 messages')
    expect(wrapper.text()).toContain('9 messages')
  })

  // The row badge in SessionList reads this module-level set, so loading the
  // list must seed it — otherwise the badge would only appear for sessions
  // shared during this page view.
  it('seeds the shared-session set from the loaded list', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [
        { token: 'tok1', sessionId: 's1', title: 'A', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false },
        { token: 'tok2', sessionId: 's2', title: 'B', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false },
      ],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)

    expect(h.markShared).toHaveBeenCalledWith('s1')
    expect(h.markShared).toHaveBeenCalledWith('s2')
  })

  it('shows the empty state when there are no shares', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ shares: [] }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)
    expect(wrapper.text()).toContain('No shared conversations yet')
  })

  it('shows an error state with a retry when the request fails', async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, statusText: 'Error' })
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)
    expect(wrapper.text()).toContain('Error')
    expect(wrapper.find('.shared-sessions-retry').exists()).toBe(true)
  })

  // An archived conversation keeps a working link but is not addressable by id,
  // so the row must be marked and must NOT be clickable.
  it('marks an archived share and makes its row non-clickable', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [
        { token: 'tok1', sessionId: 's1', title: 'Live one', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false },
        { token: 'tok2', sessionId: 's2', title: 'Old one', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: true },
      ],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)

    const rows = wrapper.findAll('.shared-session-row')
    expect(rows).toHaveLength(2)

    // The live row is clickable; the archived one is not.
    expect(rows[0].classes()).toContain('clickable')
    expect(rows[1].classes()).toContain('archived')
    expect(rows[1].classes()).not.toContain('clickable')

    expect(wrapper.text()).toContain('Conversation archived')

    // Clicking the archived row must not emit a selection.
    await rows[1].trigger('click')
    expect(wrapper.emitted('selectSession')).toBeUndefined()

    // Clicking the live one does.
    await rows[0].trigger('click')
    expect(wrapper.emitted('selectSession')).toEqual([['s1']])
  })

  it('copies the absolute share URL', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok1', sessionId: 's1', title: 'A', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false }],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)

    await wrapper.findAll('.shared-session-btn')[1].trigger('click')
    expect(h.copyText).toHaveBeenCalledWith('https://host.example/share/tok1')
    expect(h.toastShow).toHaveBeenCalled()
  })

  it('revokes one share by token after confirmation', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({
      shares: [{ token: 'tok1', sessionId: 's1', title: 'A', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false }],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)

    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true }))
    await wrapper.findAll('.shared-session-btn')[2].trigger('click')
    await flushPromises()

    const [url, init] = fetchMock.mock.calls[1]
    expect(url).toBe('/api/share/session/list')
    expect(init.method).toBe('DELETE')
    expect(JSON.parse(init.body)).toEqual({ token: 'tok1' })

    expect(h.markUnshared).toHaveBeenCalledWith('s1')
    expect(wrapper.findAll('.shared-session-row')).toHaveLength(0)
  })

  it('does not revoke when the confirmation is dismissed', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({
      shares: [{ token: 'tok1', sessionId: 's1', title: 'A', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false }],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)

    h.confirm.mockResolvedValue(false)
    await wrapper.findAll('.shared-session-btn')[2].trigger('click')
    await flushPromises()

    // Only the initial list call was made.
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(wrapper.findAll('.shared-session-row')).toHaveLength(1)
  })

  it('clears all shares of the project via all=true', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({
      shares: [
        { token: 'tok1', sessionId: 's1', title: 'A', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false },
        { token: 'tok2', sessionId: 's2', title: 'B', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false },
      ],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)

    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true }))
    await wrapper.find('.shared-sessions-clear').trigger('click')
    await flushPromises()

    const [url, init] = fetchMock.mock.calls[1]
    expect(url).toBe('/api/share/session/list')
    expect(init.method).toBe('DELETE')
    // Project-scoped on the server; the client just asks for "all".
    expect(JSON.parse(init.body)).toEqual({ all: true })

    expect(h.resetSessionShareState).toHaveBeenCalled()
    expect(wrapper.findAll('.shared-session-row')).toHaveLength(0)
  })

  // ── Keyboard: action buttons must not bubble Enter to the row ──
  //
  // The row opens the conversation on Enter (it is the row's role=button
  // affordance). The action buttons live INSIDE that row, so without
  // keydown.stop on their container, Enter on a focused button did two things
  // at once: the button's own action AND a jump into the conversation.
  it('pressing keyboard Enter on an action button does not open the conversation', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok1', sessionId: 's1', title: 'A', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false }],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)

    // The revoke button (last action). Confirmation is dismissed so the only
    // observable effect of the keystroke is whether it reached the row.
    h.confirm.mockResolvedValue(false)
    const buttons = wrapper.findAll('.shared-session-btn')
    const revoke = buttons[buttons.length - 1]
    await revoke.trigger('keydown.enter')
    await flushPromises()

    expect(wrapper.emitted('selectSession'),
      'Enter on an action button must not open the conversation').toBeUndefined()
  })

  // The row itself must still open the conversation on Enter — the fix must
  // not have broken the row's own keyboard affordance.
  it('still opens the conversation when Enter is pressed on the row itself', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok1', sessionId: 's1', title: 'A', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false }],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)

    await wrapper.find('.shared-session-row').trigger('keydown.enter')
    await flushPromises()

    expect(wrapper.emitted('selectSession')).toEqual([['s1']])
  })

  it('falls back to a generic title when a share has none', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      shares: [{ token: 'tok1', sessionId: 's1', title: '', backend: 'codebuddy', messageCount: 1, createdAt: '', archived: false }],
    }))
    const wrapper = mountDrawer()
    await openAndLoad(wrapper)
    expect(wrapper.text()).toContain('Shared conversation')
  })
})
