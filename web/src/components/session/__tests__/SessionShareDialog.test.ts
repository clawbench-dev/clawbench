import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'

vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: {
    name: 'ModalDialog',
    props: ['open', 'title', 'zIndex', 'maxWidth'],
    template: '<div class="modal-stub" :data-open="open"><slot /><div class="footer"><slot name="footer" /></div></div>',
  },
}))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: confirmMock }),
}))
vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn() }),
}))
vi.mock('@/utils/clipboard.ts', () => ({
  copyText: vi.fn((_t: string, cb?: () => void) => cb?.()),
}))

const confirmMock = vi.fn(async () => true)

import SessionShareDialog from '@/components/session/SessionShareDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { loading: 'Loading...' },
      sessionShare: {
        title: 'Share conversation',
        explain: 'Create a link',
        active: 'Sharing is on',
        securityHint: 'No login needed',
        selectAll: 'Select all',
        deselectAll: 'Deselect all',
        selectedCount: '{selected} of {total} selected',
        viewFullInLink: 'View the full conversation in the share link',
        generatingCannotShare: 'Generating — cannot share yet',
        empty: 'No shareable messages',
        generate: 'Generate link',
        regenerate: 'Regenerate',
        regenerateTip: 'Regenerate',
        copyTip: 'Copy',
        copied: 'Copied',
        revoke: 'Revoke',
        revoked: 'Revoked',
        openPage: 'Open page',
        confirmRegenerate: 'Regenerate?',
        confirmRevoke: 'Revoke?',
        loadFailed: 'Failed to load',
        roleUser: 'User',
        roleAssistant: 'Reply',
      },
    },
  },
})

const originalFetch = globalThis.fetch
const mounted: Array<{ unmount: () => void }> = []
let fetchCalls: Array<{ url: string; method: string; body: unknown }> = []

/** Build a message-list response; the first N are finalized, the rest in-flight. */
function messagesResponse(count: number, inFlight = 0) {
  const messages = []
  for (let i = 0; i < count; i++) {
    const isLast = i >= count - inFlight
    messages.push({
      id: 100 + i,
      role: i % 2 === 0 ? 'user' : 'assistant',
      preview: `message ${i}`,
      streaming: isLast && inFlight > 0,
      queued: false,
      createdAt: '2026-09-22T09:00:00Z',
    })
  }
  return { token: '', path: '', messages }
}

beforeEach(() => {
  confirmMock.mockClear()
  confirmMock.mockResolvedValue(true)
  fetchCalls = []
  globalThis.fetch = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method || 'GET'
    fetchCalls.push({ url: u, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (method === 'POST') {
      return { ok: true, json: async () => ({ token: 'newtok', path: '/share/newtok', messageCount: 4 }) } as Response
    }
    if (method === 'DELETE') {
      return { ok: true, json: async () => ({ ok: true }) } as Response
    }
    return { ok: true, json: async () => messagesResponse(4) } as Response
  }) as unknown as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
  vi.restoreAllMocks()
  for (const w of mounted.splice(0)) w.unmount()
})

async function mountDialog(props: Record<string, unknown> = {}) {
  const wrapper = mount(SessionShareDialog, {
    props: { open: true, sessionId: 'sess-1', ...props },
    global: { plugins: [i18n] },
  })
  mounted.push(wrapper)
  await flushPromises()
  await nextTick()
  await flushPromises()
  return wrapper
}

/** Read the checkbox states in list order. */
function checks(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('.session-share-dialog-check').map((c) => ({
    checked: (c.element as HTMLInputElement).checked,
    disabled: (c.element as HTMLInputElement).disabled,
  }))
}

describe('SessionShareDialog', () => {
  it('loads the message list on open', async () => {
    const wrapper = await mountDialog()
    expect(fetchCalls[0].url).toBe('/api/share/session?session_id=sess-1')
    expect(wrapper.findAll('.session-share-dialog-row')).toHaveLength(4)
  })

  // The default is "share everything" — the user unchecks what they do not want,
  // rather than opting in message by message.
  it('checks every selectable message by default', async () => {
    const wrapper = await mountDialog()
    const state = checks(wrapper)
    expect(state).toHaveLength(4)
    expect(state.every((c) => c.checked)).toBe(true)
    expect(state.every((c) => !c.disabled)).toBe(true)
  })

  it('renders in-flight messages disabled with a reason instead of hiding them', async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => messagesResponse(4, 1),
    } as Response)) as unknown as typeof fetch

    const wrapper = await mountDialog()
    const state = checks(wrapper)
    expect(state).toHaveLength(4, )
    // The last one is streaming.
    expect(state[3].disabled).toBe(true)
    expect(state[3].checked).toBe(false)
    expect(wrapper.text()).toContain('Generating — cannot share yet')
    // The count reflects only what can actually be shared.
    expect(wrapper.text()).toContain('3 of 3 selected')
  })

  it('sends only the checked ids, in list order', async () => {
    const wrapper = await mountDialog()
    // Uncheck the second message.
    await wrapper.findAll('.session-share-dialog-check')[1].trigger('change')
    await nextTick()

    const generate = wrapper.findAll('button').find((b) => b.text().includes('Generate link'))
    expect(generate).toBeTruthy()
    await generate!.trigger('click')
    await flushPromises()

    const post = fetchCalls.find((c) => c.method === 'POST')
    expect(post).toBeTruthy()
    expect(post!.url).toBe('/api/share/session')
    expect(post!.body).toEqual({ sessionId: 'sess-1', messageIds: [100, 102, 103] })
  })

  it('disables the generate button when nothing is selected', async () => {
    const wrapper = await mountDialog()
    // Deselect all.
    const toggle = wrapper.findAll('button').find((b) => b.text().includes('Deselect all'))
    await toggle!.trigger('click')
    await nextTick()

    const generate = wrapper.findAll('button').find((b) => b.text().includes('Generate link'))
    expect(generate!.attributes('disabled')).toBeDefined()
  })

  it('select-all and deselect-all toggle every selectable row', async () => {
    const wrapper = await mountDialog()
    const deselect = wrapper.findAll('button').find((b) => b.text().includes('Deselect all'))
    await deselect!.trigger('click')
    await nextTick()
    expect(checks(wrapper).every((c) => !c.checked)).toBe(true)

    const select = wrapper.findAll('button').find((b) => b.text().includes('Select all'))
    await select!.trigger('click')
    await nextTick()
    expect(checks(wrapper).every((c) => c.checked)).toBe(true)
  })

  // The hint is what tells the reader the link carries more than the dialog
  // shows — it must appear only when the list is actually longer than the
  // preview window.
  it('shows the truncation hint only for long conversations', async () => {
    const short = await mountDialog()
    expect(short.text()).not.toContain('View the full conversation in the share link')

    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => messagesResponse(25),
    } as Response)) as unknown as typeof fetch

    const long = await mountDialog()
    expect(long.text()).toContain('View the full conversation in the share link')
    // Every message is still listed and selectable — only the hint is capped.
    expect(long.findAll('.session-share-dialog-row')).toHaveLength(25)
  })

  it('shows the existing link and revokes after confirmation', async () => {
    globalThis.fetch = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
      const method = init?.method || 'GET'
      fetchCalls.push({ url: String(url), method, body: undefined })
      if (method === 'DELETE') return { ok: true, json: async () => ({ ok: true }) } as Response
      return { ok: true, json: async () => ({ ...messagesResponse(3), token: 'ex', path: '/share/ex' }) } as Response
    }) as unknown as typeof fetch

    const wrapper = await mountDialog()
    expect(wrapper.find('input[readonly]').exists()).toBe(true)

    const revoke = wrapper.findAll('button').find((b) => b.text().includes('Revoke'))
    await revoke!.trigger('click')
    await flushPromises()

    // Destructive actions confirm first.
    expect(confirmMock).toHaveBeenCalledWith('Revoke?', { dangerous: true })
    expect(fetchCalls.some((c) => c.method === 'DELETE')).toBe(true)
  })

  it('confirms before regenerating (rotating kills the old link)', async () => {
    globalThis.fetch = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
      const method = init?.method || 'GET'
      fetchCalls.push({ url: String(url), method, body: undefined })
      if (method === 'POST') return { ok: true, json: async () => ({ token: 't2', path: '/share/t2' }) } as Response
      return { ok: true, json: async () => ({ ...messagesResponse(3), token: 't1', path: '/share/t1' }) } as Response
    }) as unknown as typeof fetch

    const wrapper = await mountDialog()
    const regen = wrapper.find('button[title="Regenerate"]')
    expect(regen.exists()).toBe(true)
    await regen.trigger('click')
    await flushPromises()

    expect(confirmMock).toHaveBeenCalledWith('Regenerate?', { dangerous: true })
    expect(fetchCalls.some((c) => c.method === 'POST')).toBe(true)
  })

  it('does not regenerate when the confirmation is declined', async () => {
    confirmMock.mockResolvedValue(false)
    globalThis.fetch = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
      const method = init?.method || 'GET'
      fetchCalls.push({ url: String(url), method, body: undefined })
      return { ok: true, json: async () => ({ ...messagesResponse(3), token: 't1', path: '/share/t1' }) } as Response
    }) as unknown as typeof fetch

    const wrapper = await mountDialog()
    await wrapper.find('button[title="Regenerate"]').trigger('click')
    await flushPromises()

    expect(fetchCalls.some((c) => c.method === 'POST')).toBe(false)
  })

  // ── Row-badge sync ──
  //
  // The session list renders a share badge from useSessionShare. This dialog
  // is where a single session is shared/unshared, so it must update that set
  // or the row stays wrong until the list remounts.
  it('marks the session shared after creating a link (keeps the row badge in sync)', async () => {
    const { useSessionShare } = await import('@/composables/useSessionShare')
    const { resetSessionShareState, isSessionShared } = useSessionShare()
    resetSessionShareState()

    const wrapper = await mountDialog()
    const generate = wrapper.findAll('button').find((b) => b.text().includes('Generate link'))
    await generate!.trigger('click')
    await flushPromises()

    expect(isSessionShared('sess-1')).toBe(true)
    resetSessionShareState()
  })

  it('clears the mark after revoking (keeps the row badge in sync)', async () => {
    const { useSessionShare } = await import('@/composables/useSessionShare')
    const { resetSessionShareState, isSessionShared, markShared } = useSessionShare()
    resetSessionShareState()

    globalThis.fetch = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
      const method = init?.method || 'GET'
      fetchCalls.push({ url: String(url), method, body: undefined })
      if (method === 'DELETE') return { ok: true, json: async () => ({ ok: true }) } as Response
      return { ok: true, json: async () => ({ ...messagesResponse(3), token: 'ex', path: '/share/ex' }) } as Response
    }) as unknown as typeof fetch

    const wrapper = await mountDialog()
    // Opening with an existing link marks it.
    expect(isSessionShared('sess-1')).toBe(true)

    const revoke = wrapper.findAll('button').find((b) => b.text().includes('Revoke'))
    await revoke!.trigger('click')
    await flushPromises()

    expect(isSessionShared('sess-1')).toBe(false)
    resetSessionShareState()
  })

  // The server is authoritative: a share created or revoked elsewhere (another
  // tab, the management drawer) must be reconciled when the dialog opens.
  it('reconciles the mark with the server when opened without a link', async () => {
    const { useSessionShare } = await import('@/composables/useSessionShare')
    const { resetSessionShareState, isSessionShared, markShared } = useSessionShare()
    resetSessionShareState()
    markShared('sess-1') // stale: believes it is shared

    // The server reports no share (path empty).
    await mountDialog()

    expect(isSessionShared('sess-1')).toBe(false)
    resetSessionShareState()
  })

  it('reports an empty conversation instead of offering a link', async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({ token: '', path: '', messages: [] }),
    } as Response)) as unknown as typeof fetch

    const wrapper = await mountDialog()
    expect(wrapper.text()).toContain('No shareable messages')
    expect(wrapper.findAll('.session-share-dialog-row')).toHaveLength(0)
  })

  it('reports a load failure', async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) } as Response)) as unknown as typeof fetch
    const wrapper = await mountDialog()
    expect(wrapper.text()).toContain('Failed to load')
  })

  it('does not load anything while closed', async () => {
    await mountDialog({ open: false })
    expect(fetchCalls).toEqual([])
  })

  it('surfaces the server error message when creation fails', async () => {
    globalThis.fetch = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
      const method = init?.method || 'GET'
      if (method === 'POST') {
        return { ok: false, status: 400, json: async () => ({ error: 'some messages are still generating' }) } as Response
      }
      return { ok: true, json: async () => messagesResponse(3) } as Response
    }) as unknown as typeof fetch

    const wrapper = await mountDialog()
    const generate = wrapper.findAll('button').find((b) => b.text().includes('Generate link'))
    await generate!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('some messages are still generating')
  })
})
