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
      <div class="chat-message-stub" :data-msg-id="msg.id" :data-role="msg.role" :data-read-only="readOnly">
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

async function mountView() {
  const wrapper = mount(SessionShareView, { global: { plugins: [i18n] } })
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

  // Layout contract: the title must live INSIDE the same centred column as the
  // messages, so it reads as a heading for the thread. It must not be a
  // left-title/right-actions toolbar (.share-topbar).
  // The chat area frames its conversation column with full-height vertical
  // rules that start at the title and run to the bottom. The header and the
  // body must both live inside that frame, or the rules would stop short.
  it('frames the column so the vertical rules span title to bottom', async () => {
    const wrapper = await mountView()
    const column = wrapper.find('.session-share-column')
    expect(column.exists()).toBe(true)
    // Both children live inside the frame.
    expect(column.find('.session-share-header').exists()).toBe(true)
    expect(column.find('.session-share-body').exists()).toBe(true)
  })

  it('declares the vertical rules on the column, not the header', async () => {
    // Scoped CSS is not evaluated by jsdom, so read the component source.
    const src = readFileSync(
      join(__dirname, '..', 'SessionShareView.vue'),
      'utf8',
    )
    const col = src.match(/\.session-share-column\s*\{([\s\S]*?)\}/)
    expect(col, '.session-share-column rule must exist').not.toBeNull()
    expect(col![1]).toContain("border-left: 1px solid")
    expect(col![1]).toContain("border-right: 1px solid")
    expect(col![1]).toContain("max-width: 900px")
    // The column must stretch, so the rules reach the bottom of the page.
    expect(col![1]).toContain("flex: 1")
  })
  it('puts the title in the header block, not a share-topbar', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('.session-share-header').exists()).toBe(true)
    expect(wrapper.find('.session-share-title').exists()).toBe(true)
    expect(wrapper.find('.share-topbar').exists()).toBe(false)
    // The title and the message column are siblings in the same flex column.
    expect(wrapper.find('.session-share-header-inner').exists()).toBe(true)
  })

  it('uses an h1 for the title so it is the page heading', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('h1.session-share-title').exists()).toBe(true)
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
})
