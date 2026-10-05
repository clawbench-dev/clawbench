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

vi.mock('@/components/chat/ContentBlocks.vue', () => ({ default: { name: 'ContentBlocks', template: '<div class="cb-stub" />' } }))
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

  it('renders a routing card for the host message', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      {
        role: 'assistant',
        id: 4,
        content: '',
        blocks: [{ type: 'text', text: '<clawbench-speaker>A, B</clawbench-speaker> 请表态' }],
        agentId: 'host-1',
      },
      { resolveSpeaker, hostMemberId: 'host-1' },
    )
    expect(w.find('.msg-routing-card').exists()).toBe(true)
    const chips = w.findAll('.msg-routing-chip').map(c => c.text())
    expect(chips).toEqual(['A', 'B'])
  })
})
