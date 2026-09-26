import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'

// ── Child component mocks ──────────────────────────────────────────────────
// SessionShareView mounts the real chat render chain (ChatMessageItem →
// ContentBlocks → markdown pipeline). Stub the heavy leaves so this test can
// focus on what it actually owns: snapshot loading, the share-mode data
// provider, and the guarantee that no authenticated endpoint is ever called.

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', props: ['size'], template: '<div class="loading-indicator-stub" />' },
}))

// ChatMessageItem is stubbed with a template that exercises the two injected
// dependencies SessionShareView must provide (chatRender + autoSpeech), plus the
// summary toggle path. It deliberately does NOT reimplement the component.
vi.mock('@/components/chat/ChatMessageItem.vue', () => ({
  default: {
    name: 'ChatMessageItem',
    props: [
      'msg', 'index', 'expandedTools', 'blockTasks', 'blockAskQuestions', 'agents',
      'staticBlockCache', 'active', 'isLastAssistant', 'isLastMessage',
      'hideSessionActions', 'readOnly',
    ],
    inject: ['chatRender', 'autoSpeech', 'chatSession'],
    template: `
      <div class="chat-message-stub" :data-msg-id="msg.id" :data-msg-key="msg.id ? 'db-' + msg.id : null" :data-role="msg.role" :data-read-only="readOnly">
        <span class="block-count">{{ (msg.blocks || []).length }}</span>
        <span class="thinking-text">{{ (msg.blocks || []).filter(b => b.type === 'thinking').map(b => b.text).join('|') }}</span>
        <span class="tool-output">{{ (msg.blocks || []).filter(b => b.type === 'tool_use').map(b => b.output).join('|') }}</span>
        <span class="tool-input-path">{{ (msg.blocks || []).filter(b => b.type === 'tool_use').map(b => b.input && b.input.file_path).join('|') }}</span>
        <span class="has-render">{{ typeof chatRender.renderTextBlock }}</span>
        <span class="auto-speech-active">{{ autoSpeech.isActive(1) }}</span>
        <span class="agent-backend">{{ chatSession.getAgentBackend() }}</span>
      </div>`,
  },
}))

vi.mock('@/components/chat/ToolDetailDrawer.vue', () => ({
  default: { name: 'ToolDetailDrawer', props: ['show', 'toolName'], template: '<div class="tool-drawer-stub" :data-open="show" />' },
}))
vi.mock('@/components/chat/ChatMetadataModal.vue', () => ({
  default: { name: 'ChatMetadataModal', props: ['show', 'data'], template: '<div class="metadata-modal-stub" :data-open="show" />' },
}))

// useChatRender pulls the markdown pipeline + task-block store; stub it so the
// test asserts on wiring rather than on rendering internals.
vi.mock('@/composables/useChatRender', () => ({
  useChatRender: () => ({
    expandedTools: { value: {} },
    blockTasks: {},
    blockAskQuestions: {},
    staticBlockCache: {},
    toggleToolDetail: vi.fn(),
    renderTextBlock: vi.fn(() => '<p>rendered</p>'),
    formatMessageTime: vi.fn(() => ''),
    formatDetailTime: vi.fn(() => ''),
    toolCallSummary: vi.fn(() => ''),
    formatToolInput: vi.fn(() => '<div>input</div>'),
    truncate: vi.fn((s: string) => s),
    hasImagesInContent: vi.fn(() => false),
    parseAssistantContent: (content: string) => {
      // Mirror the real parser closely enough for the assertions below.
      try {
        const parsed = JSON.parse(content)
        return { blocks: parsed.blocks || [], metadata: parsed.metadata || null, cancelled: false }
      } catch {
        return { blocks: content ? [{ type: 'text', text: content }] : [], metadata: null }
      }
    },
  }),
}))

vi.mock('@/composables/useToolDetailDrawer', () => ({
  useToolDetailDrawer: () => ({
    // Mirrors the real return shape: the view reads effectiveOpen (a ref) for
    // the drawer's :show, exactly like ChatPanelContent does.
    effectiveOpen: { value: false },
    toolDetailOverlay: { show: false, name: '', subagentType: '', summary: '', inputHtml: '', outputHtml: '', status: '', done: true, duration: 0, displayNameOverride: '' },
    closeOverlay: vi.fn(),
    handleShowToolDetail: vi.fn(),
    handleOverlayRetryClick: vi.fn(),
  }),
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    isOpen: { value: false },
    effectiveOpen: { value: false },
    open: vi.fn(),
    close: vi.fn(),
  }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '', homeDir: '' } },
}))

// downloadBlob is mocked so the export test can assert on the exact bytes,
// filename and MIME type instead of driving a real browser download.
const downloadBlobMock = vi.hoisted(() => vi.fn())
vi.mock('@/utils/download.ts', () => ({
  downloadBlob: downloadBlobMock,
}))

import SessionShareView from '@/share/SessionShareView.vue'
import { setShareToken, clearShareSessionData, getShareThinking, getShareToolCall } from '@/share/shareMode'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      share: {
        loading: 'Loading...',
        invalidTitle: 'Cannot view this share',
        notFound: 'Not found',
        sharedConversation: 'Shared conversation',
        messageCount: '{count} messages',
        totalDuration: 'Total {duration}',
        exportJson: 'Export snapshot as JSON',
        exportFailed: 'Could not export the snapshot',
      },
      common: { loading: 'Loading...' },
      chat: {
        contentBlocks: { detailsUnavailable: 'unavailable', detailsLoadFailed: 'failed' },
      },
    },
  },
})

/** A snapshot payload shaped exactly like the Go builder emits. */
function makePayload() {
  return {
    version: 1,
    createdAt: '2026-09-22T10:00:00Z',
    session: {
      title: 'Fix the login bug',
      backend: 'codebuddy',
      agentId: 'codebuddy',
      model: 'claude-sonnet-4',
    },
    messages: [
      { id: 11, role: 'user', content: 'please fix it', createdAt: '2026-09-22T09:00:00Z' },
      {
        id: 12,
        role: 'assistant',
        content: JSON.stringify({
          blocks: [
            { type: 'thinking', think_id: 'th_1', text: 'inlined reasoning', done: true },
            { type: 'tool_use', id: 'toolu_1', name: 'Read', input: { file_path: './src/a.ts' }, output: 'inlined output', status: 'success', done: true, duration_ms: 12 },
            { type: 'text', text: 'Fixed.' },
          ],
          metadata: { model: 'claude-sonnet-4', wallMs: 1234 },
        }),
        summary: 'A short summary.',
        summaryCards: { tools: [{ name: 'Read', id: 'toolu_1' }] },
        createdAt: '2026-09-22T09:01:00Z',
      },
    ],
  }
}

const originalFetch = globalThis.fetch
const mounted: Array<{ unmount: () => void }> = []
let fetchCalls: string[] = []

beforeEach(() => {
  window.history.replaceState({}, '', '/share/tokSessionTest')
  setShareToken('tokSessionTest')
  clearShareSessionData()
  fetchCalls = []
  globalThis.fetch = vi.fn(async (url: string | URL | Request) => {
    const u = String(url)
    fetchCalls.push(u)
    if (u.includes('/session')) {
      return { ok: true, json: async () => makePayload() } as Response
    }
    // Any other URL is a bug in this test's setup, but returning 404 makes the
    // failure legible rather than hanging.
    return { ok: false, status: 404, json: async () => ({}) } as Response
  }) as unknown as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
  window.history.replaceState({}, '', '/')
  clearShareSessionData()
  vi.restoreAllMocks()
  for (const w of mounted.splice(0)) w.unmount()
})

async function mountView(opts: { attach?: boolean } = {}) {
  const wrapper = mount(SessionShareView, {
    global: { plugins: [i18n] },
    attachTo: opts.attach ? document.body : undefined,
  })
  mounted.push(wrapper)
  await flushPromises()
  await nextTick()
  await flushPromises()
  return wrapper
}

describe('SessionShareView', () => {
  it('fetches only the token-scoped snapshot endpoint', async () => {
    await mountView()
    expect(fetchCalls).toEqual(['/api/share/tokSessionTest/session'])
  })

  // The whole point of inlining tool I/O and thinking text: an anonymous viewer
  // must never reach for the authenticated detail endpoints. A regression here
  // would show up as 401s in production and empty tool cards.
  it('never calls an authenticated endpoint', async () => {
    await mountView()
    const forbidden = [
      '/api/ai/chat/tool-call',
      '/api/ai/chat/thinking',
      '/api/agents',
      '/api/tasks',
      '/api/file/batch-exists',
      '/api/dir',
      '/api/git/verify-commits',
      '/api/rag/message',
    ]
    for (const path of forbidden) {
      expect(fetchCalls.some((u) => u.includes(path))).toBe(false)
    }
  })

  it('renders the session title and agent byline from the snapshot', async () => {
    const wrapper = await mountView()
    const text = wrapper.text()
    expect(text).toContain('Fix the login bug')
    // The agent display name comes from the static backend map, not the raw id.
    expect(text).toContain('Codebuddy')
    expect(text).toContain('2 messages')
  })

  // The snapshot's `model` is SESSION-level, but the model is per-message (it
  // can change mid-thread), so a single header chip would misreport the
  // conversation. The real model stays on each message's metadata modal.
  it('does not show a model label in the header', async () => {
    const wrapper = await mountView()
    expect(wrapper.text()).not.toContain('claude-sonnet-4')
  })

  // The share page uses the same chrome skeleton as the file share (full-width
  // .share-topbar over .share-body), with a two-row stacked topbar. The
  // conversation column below keeps its centred 900px measure, with no side
  // rules, inside the scrolling content area.
  it('places the message column inside the shared share-body skeleton', async () => {
    const wrapper = await mountView()
    const column = wrapper.find('.session-share-column')
    expect(column.exists()).toBe(true)
    // The column lives inside the scrolling content area, not beside it.
    expect(wrapper.find('.share-body').exists()).toBe(true)
    expect(wrapper.find('.share-content .session-share-column').exists()).toBe(true)
    expect(column.find('.session-share-messages').exists()).toBe(true)
  })

  // Layout parity with the chat area. The share page reuses ChatMessageItem, so
  // its message layout must reproduce the chat list's spacing rules or the same
  // thread reads differently in the two places:
  //   .chat-messages      → padding: var(--space-6) 0; flex column; gap: var(--space-4)
  //   .chat-messages-list → flex column; gap: var(--space-8)
  // The share list has no wrapper list element, so it needs the LIST's gap
  // (space-8 = 20px). Without it the container is block layout and adjacent
  // messages touch (measured gap 0).
  it('matches the chat area message spacing and has no horizontal inset', () => {
    // Scoped CSS is not evaluated by jsdom, so read the component source.
    const src = readFileSync(join(__dirname, '..', 'SessionShareView.vue'), 'utf8')
    const rule = src.match(/\.session-share-messages\s*\{([\s\S]*?)\}/)
    expect(rule, '.session-share-messages rule must exist').not.toBeNull()
    const body = rule![1]
    // No horizontal padding: an assistant card must reach the column edge, as
    // it does in chat (the user bubble owns its own 10px inset).
    expect(body).toMatch(/padding:\s*var\(--space-6\)\s+0/)
    expect(body).not.toMatch(/padding:[^;]*\b\d+px\s+\d+px/)
    // Flex column + the chat list's message gap.
    expect(body).toContain('display: flex')
    expect(body).toContain('flex-direction: column')
    expect(body).toContain('gap: var(--space-8)')
  })

  it('keeps the column unframed (no side rules) but centred and full-height', async () => {
    // Scoped CSS is not evaluated by jsdom, so read the component source.
    const src = readFileSync(
      join(__dirname, '..', 'SessionShareView.vue'),
      'utf8',
    )
    const col = src.match(/\.session-share-column\s*\{([\s\S]*?)\}/)
    expect(col, '.session-share-column rule must exist').not.toBeNull()
    // The message area must NOT be boxed by vertical rules on either side.
    expect(col![1]).not.toContain('border-left')
    expect(col![1]).not.toContain('border-right')
    expect(col![1]).toContain("max-width: 900px")
    // The column must still fill the scroll container's height.
    expect(col![1]).toContain("min-height: 100%")
  })
  it('puts the title in the full-width stacked topbar', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('.share-topbar').exists()).toBe(true)
    expect(wrapper.find('.share-topbar--stacked').exists()).toBe(true)
    expect(wrapper.find('.share-topbar-title').exists()).toBe(true)
    // The byline shares the topbar, below the title row.
    expect(wrapper.find('.share-topbar .session-share-byline').exists()).toBe(true)
  })

  it('uses an h1 for the title so it is the page heading', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('h1.share-topbar-title').exists()).toBe(true)
  })

  it('renders the agent icon in the byline', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('.session-share-agent .agent-icon-svg, .session-share-agent .agent-icon-initial').exists()).toBe(true)
  })

  it('renders one message per snapshot entry with blocks parsed', async () => {
    const wrapper = await mountView()
    const rows = wrapper.findAll('.chat-message-stub')
    expect(rows).toHaveLength(2)
    expect(rows[0].attributes('data-role')).toBe('user')
    expect(rows[1].attributes('data-role')).toBe('assistant')
    expect(rows[1].find('.block-count').text()).toBe('3')
  })

  // Read-only is what suppresses the interactive tool actions that POST back.
  it('passes readOnly down to every message', async () => {
    const wrapper = await mountView()
    for (const row of wrapper.findAll('.chat-message-stub')) {
      expect(row.attributes('data-read-only')).toBe('true')
    }
  })

  it('exposes the inlined thinking text and tool payload to the render chain', async () => {
    const wrapper = await mountView()
    // Through the provider (what the lazy-fetch guards consult)…
    expect(getShareThinking(12, 'th_1')).toBe('inlined reasoning')
    expect(getShareToolCall(12, 'toolu_1')?.output).toBe('inlined output')
    // …and through to the rendered message itself.
    const assistant = wrapper.findAll('.chat-message-stub')[1]
    expect(assistant.find('.thinking-text').text()).toBe('inlined reasoning')
    expect(assistant.find('.tool-output').text()).toBe('inlined output')
    expect(assistant.find('.tool-input-path').text()).toBe('./src/a.ts')
  })

  // The share page is a read-only transcript. The summary/original switch is an
  // app-side reading preference and is suppressed via readOnly; the snapshot
  // still carries both fields, it is the VIEW that chooses original only.
  it('passes readOnly down so the summary toggle is suppressed', async () => {
    const wrapper = await mountView()
    const rows = wrapper.findAll('.chat-message-stub')
    expect(rows.length).toBeGreaterThan(0)
    for (const row of rows) {
      expect(row.attributes('data-read-only')).toBe('true')
    }
  })

  it('provides the injected dependencies the render chain requires', async () => {
    const wrapper = await mountView()
    const assistant = wrapper.findAll('.chat-message-stub')[1]
    expect(assistant.find('.has-render').text()).toBe('function')
    // autoSpeech must be callable, not undefined: ChatMessageItem reads it
    // during render and would throw otherwise.
    expect(assistant.find('.auto-speech-active').text()).toBe('false')
    // The agent identity comes from the snapshot, not from /api/agents.
    expect(assistant.find('.agent-backend').text()).toBe('codebuddy')
  })

  it('shows the not-found state when the token is unknown', async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: false, status: 404, json: async () => ({}) } as Response)) as unknown as typeof fetch
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('Not found')
    expect(wrapper.findAll('.chat-message-stub')).toHaveLength(0)
  })

  it('shows the not-found state when the payload is malformed', async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: true, json: async () => { throw new Error('bad json') } } as unknown as Response)) as unknown as typeof fetch
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('Not found')
  })

  it('falls back to a generic title when the snapshot has none', async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({ version: 1, session: {}, messages: [] }),
    } as unknown as Response)) as unknown as typeof fetch
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('Shared conversation')
    expect(wrapper.findAll('.chat-message-stub')).toHaveLength(0)
  })

  // ── Session total duration ──
  //
  // The per-message duration is rendered by the shared ChatMessageItem meta bar
  // and is not this view's concern. The view owns the SESSION total, summed
  // from each assistant turn's wallMs.
  it('shows the summed assistant duration in the byline', async () => {
    const wrapper = await mountView()
    // makePayload's single assistant turn carries wallMs: 1234 → "1.2s".
    expect(wrapper.find('.session-share-duration').exists()).toBe(true)
    expect(wrapper.find('.session-share-duration').text()).toContain('1.2s')
  })

  it('omits the duration entirely when no turn carries wallMs', async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({
        version: 1,
        session: { title: 'No timings', backend: 'codebuddy' },
        messages: [
          { id: 1, role: 'user', content: 'hi', createdAt: '2026-09-22T09:00:00Z' },
          {
            id: 2,
            role: 'assistant',
            content: JSON.stringify({ blocks: [{ type: 'text', text: 'ok' }], metadata: { model: 'x' } }),
            createdAt: '2026-09-22T09:01:00Z',
          },
        ],
      }),
    } as unknown as Response)) as unknown as typeof fetch
    const wrapper = await mountView()
    // Not "0ms" and not an empty chip — the element must be absent so the
    // byline's separator dots stay correct.
    expect(wrapper.find('.session-share-duration').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('0ms')
  })

  it('does not double-count user turns', async () => {
    // A user message with a wallMs in its metadata must be ignored: only the
    // assistant's own turn time is meaningful.
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({
        version: 1,
        session: { title: 'Mixed', backend: 'codebuddy' },
        messages: [
          { id: 1, role: 'user', content: 'hi', metadata: { wallMs: 999999 }, createdAt: '2026-09-22T09:00:00Z' },
          {
            id: 2,
            role: 'assistant',
            content: JSON.stringify({ blocks: [{ type: 'text', text: 'ok' }], metadata: { wallMs: 2000 } }),
            createdAt: '2026-09-22T09:01:00Z',
          },
        ],
      }),
    } as unknown as Response)) as unknown as typeof fetch
    const wrapper = await mountView()
    expect(wrapper.find('.session-share-duration').text()).toContain('2.0s')
  })

  // ── Conversation TOC ──
  //
  // The share page offers a conversation index so a long thread can be
  // navigated by jumping to a message. Entries cover EVERY message (both
  // roles), rendered with the same row component the in-app conversation
  // index uses.
  describe('conversation TOC', () => {
    it('lists every message, both roles, in order', async () => {
      const wrapper = await mountView()
      const rows = wrapper.findAll('.share-toc .msg-item')
      expect(rows).toHaveLength(2)
      expect(rows[0].find('.msg-role-tag').classes()).toContain('role-user')
      expect(rows[1].find('.msg-role-tag').classes()).toContain('role-assistant')
      // The node badge carries the 1-based conversation ordinal.
      expect(rows[0].find('.msg-index').text()).toBe('1')
      expect(rows[1].find('.msg-index').text()).toBe('2')
    })

    it('shows a preview derived from each message', async () => {
      const wrapper = await mountView()
      const rows = wrapper.findAll('.share-toc .msg-item')
      expect(rows[0].find('.msg-text').text()).toContain('please fix it')
      // The assistant row uses its stored summary when one exists.
      expect(rows[1].find('.msg-text').text()).toContain('A short summary.')
    })

    it('scrolls the content container to the clicked message and flashes it', async () => {
      // attachTo is required: flashElement early-returns for a detached element
      // (it cannot animate), so the class assertion would silently pass on [].
      const wrapper = await mountView({ attach: true })
      const content = wrapper.find('.share-content').element as HTMLElement
      const scrollTo = vi.fn()
      content.scrollTo = scrollTo
      const flashTargets: string[] = []
      // jsdom has no CSS engine; spy on classList.add to observe the flash.
      const rows = wrapper.findAll('.chat-message-stub')
      const target = rows[1].element as HTMLElement
      target.classList.add = vi.fn((c: string) => { flashTargets.push(c) })

      await wrapper.findAll('.share-toc .msg-item')[1].trigger('click')

      expect(scrollTo).toHaveBeenCalledTimes(1)
      expect(scrollTo.mock.calls[0][0]).toMatchObject({ behavior: 'smooth' })
      expect(flashTargets).toContain('chat-message-highlight')
    })

    it('marks the clicked entry active immediately', async () => {
      const wrapper = await mountView()
      const content = wrapper.find('.share-content').element as HTMLElement
      content.scrollTo = vi.fn()
      const rows = wrapper.findAll('.share-toc .msg-item')
      expect(rows[0].classes()).not.toContain('active')

      await rows[1].trigger('click')

      expect(wrapper.findAll('.share-toc .msg-item')[1].classes()).toContain('active')
    })

    it('toggles the rail from the topbar button', async () => {
      const wrapper = await mountView()
      expect(wrapper.find('.share-toc').exists()).toBe(true)
      await wrapper.find('.share-toc-toggle').trigger('click')
      expect(wrapper.find('.share-toc').exists()).toBe(false)
      await wrapper.find('.share-toc-toggle').trigger('click')
      expect(wrapper.find('.share-toc').exists()).toBe(true)
    })
  })

  // ── Snapshot JSON export ──
  //
  // The export contract is "the exact document this link serves". The critical
  // property is that it is NOT the parsed/mutated state: parseMessages assigns
  // `blocks`/`metadata` onto the payload's own message objects, so exporting
  // the live payload would duplicate every message's content (original JSON
  // string + derived blocks) and leak the viewer's summary-toggle state.
  describe('snapshot JSON export', () => {
    it('downloads the payload verbatim, without the parsed blocks', async () => {
      const wrapper = await mountView()
      const btn = wrapper.find('.session-share-export')
      expect(btn.exists()).toBe(true)

      await btn.trigger('click')

      expect(downloadBlobMock).toHaveBeenCalledTimes(1)
      const [content, filename, mime] = downloadBlobMock.mock.calls[0] as [string, string, string]
      expect(mime).toBe('application/json')
      expect(filename.endsWith('.json')).toBe(true)

      const exported = JSON.parse(content)
      // Same shape/values as the served payload.
      expect(exported.version).toBe(1)
      expect(exported.session.title).toBe('Fix the login bug')
      expect(exported.messages).toHaveLength(2)
      // The verbatim assistant message has NO derived fields…
      const assistant = exported.messages.find((m: { role: string }) => m.role === 'assistant')
      expect(assistant.blocks).toBeUndefined()
      expect(assistant.metadata).toBeUndefined()
      expect(assistant.showingSummary).toBeUndefined()
      // …but keeps the original content string the API sent.
      expect(typeof assistant.content).toBe('string')
      expect(JSON.parse(assistant.content).blocks).toHaveLength(3)
    })

    it('exports the same snapshot regardless of summary toggling', async () => {
      const wrapper = await mountView()
      // Toggle a message between summary and original — this writes
      // showingSummary onto the live (mutated) message object.
      const vm = wrapper.vm as unknown as { onToggleSummary: (id: number) => void }
      vm.onToggleSummary(12)
      await nextTick()

      await wrapper.find('.session-share-export').trigger('click')
      const [content] = downloadBlobMock.mock.calls[0] as [string]
      const exported = JSON.parse(content)
      const assistant = exported.messages.find((m: { role: string }) => m.role === 'assistant')
      expect(assistant.showingSummary).toBeUndefined()
    })

    it('sanitizes the filename derived from the session title', async () => {
      globalThis.fetch = vi.fn(async () => ({
        ok: true,
        json: async () => ({
          version: 1,
          // A path-like title with characters no filesystem accepts.
          session: { title: 'fix/a:b*c?"d<e>f|g', backend: 'codebuddy' },
          messages: [{ id: 1, role: 'user', content: 'hi', createdAt: '2026-09-22T09:00:00Z' }],
        }),
      } as unknown as Response)) as unknown as typeof fetch
      const wrapper = await mountView()
      await wrapper.find('.session-share-export').trigger('click')
      const [, filename] = downloadBlobMock.mock.calls[0] as [string, string]
      expect(filename.endsWith('.json')).toBe(true)
      // No path separators or reserved characters survive.
      expect(filename).not.toMatch(/[/\\:*?"<>|]/)
    })

    it('does not split a surrogate pair when truncating a long title', async () => {
      // 79 ASCII chars then an emoji: a naive 80-char slice cuts the emoji in
      // half, leaving a lone surrogate in the filename.
      globalThis.fetch = vi.fn(async () => ({
        ok: true,
        json: async () => ({
          version: 1,
          session: { title: 'a'.repeat(79) + '😀' + 'tail', backend: 'codebuddy' },
          messages: [{ id: 1, role: 'user', content: 'hi', createdAt: '2026-09-22T09:00:00Z' }],
        }),
      } as unknown as Response)) as unknown as typeof fetch
      const wrapper = await mountView()
      await wrapper.find('.session-share-export').trigger('click')
      const [, filename] = downloadBlobMock.mock.calls[0] as [string, string]
      // eslint-disable-next-line no-control-regex
      const loneSurrogate = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/
      expect(loneSurrogate.test(filename)).toBe(false)
    })

    it('hides the button until the snapshot has loaded', async () => {
      // Hold the response open so the view stays in its loading state, then
      // settle it at the end — a promise that never settles is reported as a
      // leak by the test runner.
      let release: (r: Response) => void = () => {}
      globalThis.fetch = vi.fn(
        () => new Promise<Response>((resolve) => { release = resolve }),
      ) as unknown as typeof fetch
      const wrapper = mount(SessionShareView, { global: { plugins: [i18n] } })
      mounted.push(wrapper)
      await nextTick()
      expect(wrapper.find('.session-share-export').exists()).toBe(false)
      release({ ok: false, status: 404, json: async () => ({}) } as Response)
      await flushPromises()
    })

    it('makes no extra request when exporting', async () => {
      const wrapper = await mountView()
      const before = fetchCalls.length
      await wrapper.find('.session-share-export').trigger('click')
      expect(fetchCalls).toHaveLength(before)
    })
  })
})
