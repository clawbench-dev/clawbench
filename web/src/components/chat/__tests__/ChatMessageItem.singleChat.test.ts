import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

// ── Single-chat speaker header ──
// A 1:1 session reuses the GROUP speaker header verbatim (same markup, same
// styles) by supplying `sessionSpeaker` instead of a per-message member row.
// These tests pin the three things that make that safe:
//   1. the header renders with the session's own agent identity,
//   2. it is still a single-agent reply — group routing tags must NOT parse,
//   3. a group message whose member no longer resolves must NOT borrow the
//      session agent (that would silently misattribute speech to the host).

vi.mock('@/composables/useDoubleClickCopy', () => ({ useDoubleClickCopy: () => ({ handleDblClick: vi.fn() }) }))
vi.mock('@/composables/useFilePathAnnotation', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useFilePathAnnotation')>()
  return {
    ...actual,
    useFilePathAnnotation: () => ({ openFilePath: vi.fn(), readLineTargetFromEl: vi.fn() }),
    openFilePath: vi.fn(),
  }
})
vi.mock('@/composables/useLocalhostAnnotation', () => ({ useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: vi.fn() }) }))
vi.mock('@/composables/useAutoSpeech', () => ({
  extractSpeakableText: (blocks: Array<{ type: string; text?: string }>) =>
    (blocks || []).filter(b => b.type === 'text' && b.text).map(b => b.text).join('\n'),
}))
vi.mock('@/composables/useDialog', () => ({ useDialog: () => ({ confirm: vi.fn() }) }))
vi.mock('@/utils/chatStreamUtils', () => ({
  extractFileChanges: () => ({ created: [], modified: [] }),
  getAgentSvg: () => '',
  shouldShowSummary: () => false,
  stripBlockForSummary: (b: unknown) => b,
}))
vi.mock('@/utils/format', () => ({ formatDuration: (ms: number) => `${ms}ms`, formatRelativeTime: () => 'now' }))
vi.mock('@/utils/clipboard', () => ({ copyText: vi.fn() }))
vi.mock('@/stores/app', () => ({ store: { state: { projectRoot: '/p' } } }))
vi.mock('@/composables/useTabDrawer', () => ({ useTabDrawer: () => ({ open: vi.fn(), close: vi.fn(), isOpen: { value: false }, effectiveOpen: { value: false } }) }))
vi.mock('@/composables/useFileChanges', () => ({ useFileChanges: () => ({ open: vi.fn() }) }))

vi.mock('@/components/chat/ContentBlocks.vue', () => ({ default: { name: 'ContentBlocks', props: ['isGroupSession'], template: '<div class="cb-stub" />' } }))
vi.mock('@/components/common/AgentIcon.vue', () => ({ default: { name: 'AgentIcon', props: ['backend', 'name'], template: '<span class="agent-icon-stub" />' } }))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({ default: { template: '<span />' } }))
vi.mock('@/components/common/SummaryToggle.vue', () => ({ default: { template: '<span />' } }))
vi.mock('@/components/common/CopyButton.vue', () => ({ default: { template: '<span />' } }))
vi.mock('@/components/chat/FileAttachmentList.vue', () => ({ default: { template: '<span />' } }))
vi.mock('@/components/chat/FileChangesDrawer.vue', () => ({ default: { template: '<span />' } }))
vi.mock('@/components/chat/FileDiffsDrawer.vue', () => ({ default: { template: '<span />' } }))

import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { chat: { message: {}, contentBlocks: { cancelled: 'cancelled' }, fileChanges: { title: 'Files' }, pending: {}, speech: {}, busy: {} }, group: { host: 'Host', realAgent: 'Agent', you: 'you', goalSatisfied: 'Done', bcc: { title: 'Private note', to: 'To', toYou: 'Private note to you' } }, session: { configAgent: 'Agent settings' }, common: { listSeparator: ', ' } } },
})

const SESSION_SPEAKER = { name: 'Claude', backend: 'claude', avatar: '', agentName: '', agentId: 'agent-1' }

function mountItem(msg: Record<string, unknown>, props: Record<string, unknown> = {}) {
  return mount(ChatMessageItem, {
    props: { msg, index: 0, ...props },
    global: {
      plugins: [i18n],
      provide: {
        autoSpeech: { isActive: () => false, isGeneratingText: () => false, isPlayingAudio: () => false, playAudio: vi.fn(), stopAudio: vi.fn(), speakText: vi.fn(), getSummary: () => null, getPhaseLabel: () => '' },
        chatRender: { renderTextBlock: () => '', toolCallSummary: vi.fn(), formatToolInput: vi.fn(), humanizeCron: vi.fn(), repeatLabel: vi.fn(), truncate: vi.fn(), hasImagesInContent: () => false },
        chatSession: { getAgentBackend: () => '', getAgentName: () => '', getAgentAvatar: () => '' },
      },
    },
  })
}

describe('ChatMessageItem — single-chat speaker header', () => {
  it('renders the session agent avatar + name above an assistant reply', () => {
    const w = mountItem(
      { role: 'assistant', id: 1, content: '', blocks: [], agentId: '' },
      { sessionSpeaker: SESSION_SPEAKER },
    )
    expect(w.find('.msg-speaker').exists()).toBe(true)
    expect(w.find('.msg-speaker-name').text()).toBe('Claude')
    expect(w.find('.msg-speaker .agent-icon-stub').exists()).toBe(true)
  })

  it('does not render the header on a user bubble', () => {
    const w = mountItem(
      { role: 'user', id: 2, content: 'hello', blocks: [] },
      { sessionSpeaker: SESSION_SPEAKER },
    )
    expect(w.find('.msg-speaker').exists()).toBe(false)
  })

  it('stays headerless when the host supplies no session speaker', () => {
    // Share page / task execution detail / /btw drawer pass no identity.
    const w = mountItem({ role: 'assistant', id: 3, content: '', blocks: [], agentId: '' })
    expect(w.find('.msg-speaker').exists()).toBe(false)
  })

  it('opens the agent config page from the avatar (real agent id)', async () => {
    const w = mountItem(
      { role: 'assistant', id: 4, content: '', blocks: [], agentId: '' },
      { sessionSpeaker: SESSION_SPEAKER },
    )
    const btn = w.find('.msg-speaker-btn')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(w.emitted('open-agent-config')?.[0]).toEqual(['agent-1'])
  })

  it('never parses group routing tags in a 1:1 reply', () => {
    // A single-agent reply is not a routing turn. If the parse were admitted,
    // the mention summary row would appear and private-note cards would render.
    const w = mountItem(
      {
        role: 'assistant',
        id: 5,
        content: '',
        blocks: [{ type: 'text', text: '<clawbench-mention targets="Bob">你来补充</clawbench-mention>' }],
        agentId: '',
      },
      { sessionSpeaker: SESSION_SPEAKER, resolveSpeakerByName: (n: string) => ({ name: n, backend: 'claude' }) },
    )
    expect(w.find('.msg-speaker').exists()).toBe(true)
    expect(w.find('.msg-mention-summary').exists()).toBe(false)
  })

  it('does not borrow the session agent for an unresolved group member', () => {
    // msg.agentId is set → this is a group attribution. resolveSpeaker misses
    // (member removed) → no header, rather than falling back to the host agent
    // the session belongs to.
    const w = mountItem(
      { role: 'assistant', id: 6, content: '', blocks: [], agentId: 'member-gone' },
      { sessionSpeaker: SESSION_SPEAKER, isGroupSession: true, resolveSpeaker: () => null },
    )
    expect(w.find('.msg-speaker').exists()).toBe(false)
  })
})
