import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

// Minimal mocks so ChatMessageItem mounts. Group speaker rendering only needs
// the template path; ContentBlocks and the heavy composables are stubbed.
vi.mock('@/composables/useDoubleClickCopy', () => ({ useDoubleClickCopy: () => ({ handleDblClick: vi.fn() }) }))
vi.mock('@/composables/useFilePathAnnotation', () => ({ useFilePathAnnotation: () => ({ openFilePath: vi.fn(), readLineTargetFromEl: vi.fn() }), openFilePath: vi.fn() }))
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
  messages: { en: { chat: { message: {}, contentBlocks: { cancelled: 'cancelled' }, fileChanges: { title: 'Files' }, pending: {}, speech: {}, busy: {} }, group: { host: 'Host' }, common: {} } },
})

function mountItem(msg: Record<string, unknown>, props: Record<string, unknown> = {}) {
  return mount(ChatMessageItem, {
    props: { msg, index: 0, ...props },
    global: {
      plugins: [i18n],
      provide: {
        autoSpeech: { isActive: () => false, isGeneratingText: () => false, isPlayingAudio: () => false, playAudio: vi.fn(), stopAudio: vi.fn(), speakText: vi.fn(), getSummary: () => null, getPhaseLabel: () => '' },
        chatRender: { renderTextBlock: vi.fn(), toolCallSummary: vi.fn(), formatToolInput: vi.fn(), humanizeCron: vi.fn(), repeatLabel: vi.fn(), truncate: vi.fn(), hasImagesInContent: () => false },
        chatSession: { getAgentBackend: () => '', getAgentName: () => '', getAgentAvatar: () => '' },
      },
    },
  })
}

describe('ChatMessageItem group speaker', () => {
  it('renders the speaker name for a group member message', () => {
    const resolveSpeaker = (id: string) => (id === 'member-1' ? { name: 'Alice', backend: 'claude' } : null)
    const w = mountItem(
      { role: 'assistant', id: 1, content: '', blocks: [], agentId: 'member-1' },
      { resolveSpeaker },
    )
    expect(w.find('.msg-speaker').exists()).toBe(true)
    expect(w.find('.msg-speaker-name').text()).toBe('Alice')
  })

  it('marks the host message', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      { role: 'assistant', id: 2, content: '', blocks: [], agentId: 'host-1' },
      { resolveSpeaker, hostMemberId: 'host-1' },
    )
    expect(w.find('.msg-speaker-host-tag').exists()).toBe(true)
    expect(w.find('.msg-card-host').exists()).toBe(true)
  })

  it('renders no speaker header for an ordinary message', () => {
    const w = mountItem({ role: 'assistant', id: 3, content: '', blocks: [] })
    expect(w.find('.msg-speaker').exists()).toBe(false)
  })

  it('renders routing targets as @-mention chips with avatars', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const resolveSpeakerByName = (n: string) => ({ name: n, backend: 'claude', avatar: '<svg/>' })
    const w = mountItem(
      {
        role: 'assistant',
        id: 4,
        content: '',
        blocks: [{ type: 'text', text: '<clawbench-speaker>A, B</clawbench-speaker> 请表态' }],
        agentId: 'host-1',
      },
      { resolveSpeaker, resolveSpeakerByName, hostMemberId: 'host-1' },
    )
    expect(w.find('.msg-routing-card').exists()).toBe(true)
    // @name chips, no speaker label or arrow.
    const chips = w.findAll('.msg-routing-chip')
    expect(chips.map(c => c.find('.msg-routing-at').text())).toEqual(['@A', '@B'])
    expect(w.find('.msg-routing-label').exists()).toBe(false)
    expect(w.find('.msg-routing-arrow').exists()).toBe(false)
    // Each chip carries an avatar, wrapped in a fixed-size disc so every chip is
    // the same height regardless of the avatar's render mode.
    expect(w.findAll('.msg-routing-chip .agent-icon-stub').length).toBe(2)
    expect(w.findAll('.msg-routing-chip .msg-routing-avatar').length).toBe(2)
  })

  it('renders an unresolved routing target as a plain @name', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const resolveSpeakerByName = () => null
    const w = mountItem(
      {
        role: 'assistant',
        id: 5,
        content: '',
        blocks: [{ type: 'text', text: '<clawbench-speaker>Ghost</clawbench-speaker> 请表态' }],
        agentId: 'host-1',
      },
      { resolveSpeaker, resolveSpeakerByName, hostMemberId: 'host-1' },
    )
    expect(w.find('.msg-routing-at').text()).toBe('@Ghost')
    expect(w.find('.msg-routing-chip .agent-icon-stub').exists()).toBe(false)
  })
})

describe('ChatMessageItem group gating', () => {
  // A finished assistant message shows the fork + rewind buttons in a normal
  // session. Fork/rewind operate on ONE agent's history, so both must be hidden
  // in a group (the timeline aggregates several members' sessions).
  const finishedAssistant = { role: 'assistant', id: 10, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] }

  function actionTitles(w: ReturnType<typeof mountItem>) {
    return w.findAll('.chat-action-btn').map(b => b.attributes('title') || '')
  }

  it('shows fork + rewind for a normal session', () => {
    const w = mountItem(finishedAssistant)
    const titles = actionTitles(w)
    expect(titles.some(t => t.includes('forkSession'))).toBe(true)
    expect(titles.some(t => t.includes('rewindSession'))).toBe(true)
  })

  it('hides fork + rewind in a group session', () => {
    const w = mountItem(finishedAssistant, { isGroupSession: true })
    const titles = actionTitles(w)
    expect(titles.some(t => t.includes('forkSession'))).toBe(false)
    expect(titles.some(t => t.includes('rewindSession'))).toBe(false)
    // Generic per-message actions stay.
    expect(w.findAll('.chat-action-btn').length).toBeGreaterThan(0)
  })

  it('forwards isGroupSession to ContentBlocks', () => {
    const w = mountItem(finishedAssistant, { isGroupSession: true })
    const cb = w.findComponent({ name: 'ContentBlocks' })
    expect(cb.props('isGroupSession')).toBe(true)
  })
})
