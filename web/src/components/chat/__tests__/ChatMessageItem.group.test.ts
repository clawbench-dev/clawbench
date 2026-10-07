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
  messages: { en: { chat: { message: {}, contentBlocks: { cancelled: 'cancelled' }, fileChanges: { title: 'Files' }, pending: {}, speech: {}, busy: {} }, group: { host: 'Host', bcc: { title: 'Private note', to: 'To', toYou: 'Private note to you' } }, common: {} } },
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

  it('renders routing targets as @-mention chips in the speaker (avatar) row', () => {
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
    // The chips live in the avatar row, NOT inside the bubble.
    const speakerRow = w.find('.msg-speaker')
    expect(speakerRow.exists()).toBe(true)
    expect(speakerRow.find('.msg-routing-targets').exists()).toBe(true)
    expect(w.find('.msg-card .msg-routing-targets').exists()).toBe(false)
    // Two items, each: "@" + a pill (icon + name). No speaker label or arrow.
    const items = w.findAll('.msg-routing-item')
    expect(items.length).toBe(2)
    expect(w.findAll('.msg-routing-chip .msg-routing-name').map(n => n.text())).toEqual(['A', 'B'])
    expect(w.find('.msg-routing-label').exists()).toBe(false)
    expect(w.find('.msg-routing-arrow').exists()).toBe(false)
    // Each pill carries an avatar, wrapped in a fixed-size disc so every pill is
    // the same height regardless of the avatar's render mode.
    expect(w.findAll('.msg-routing-chip .agent-icon-stub').length).toBe(2)
    expect(w.findAll('.msg-routing-chip .msg-routing-avatar').length).toBe(2)
  })

  // The "@" sigil must sit OUTSIDE the pill: the pill carries only the icon +
  // agent name. This keeps the sigil from looking like part of the agent's
  // name/label.
  it('puts the @ sigil outside the pill, which holds only icon + name', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const resolveSpeakerByName = (n: string) => ({ name: n, backend: 'claude', avatar: '<svg/>' })
    const w = mountItem(
      {
        role: 'assistant',
        id: 6,
        content: '',
        blocks: [{ type: 'text', text: '<clawbench-speaker>A</clawbench-speaker> 请表态' }],
        agentId: 'host-1',
      },
      { resolveSpeaker, resolveSpeakerByName, hostMemberId: 'host-1' },
    )
    const item = w.find('.msg-routing-item')
    expect(item.exists()).toBe(true)
    const at = item.find('.msg-routing-at')
    const pill = item.find('.msg-routing-chip')
    // "@" is a sibling of the pill, not nested inside it.
    expect(at.element.parentElement).toBe(item.element)
    expect(pill.element.parentElement).toBe(item.element)
    expect(at.text()).toBe('@')
    // The pill must NOT contain the "@" — its text is the name alone.
    expect(pill.find('.msg-routing-at').exists()).toBe(false)
    expect(pill.text()).toBe('A')
    expect(pill.text()).not.toContain('@')
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
    expect(w.find('.msg-routing-at').text()).toBe('@')
    expect(w.find('.msg-routing-chip .msg-routing-name').text()).toBe('Ghost')
    expect(w.find('.msg-routing-chip .agent-icon-stub').exists()).toBe(false)
  })
})

describe('ChatMessageItem group system events', () => {
  it('renders a system event as a centered thin row, not a bubble', () => {
    const w = mountItem({ role: 'system', id: 20, content: 'Alice（产品经理）加入了讨论' })
    const row = w.find('.chat-system-row')
    expect(row.exists()).toBe(true)
    expect(row.text()).toBe('Alice（产品经理）加入了讨论')
    // Not a bubble, no speaker header, no meta bar, no avatar.
    expect(w.find('.msg-card').exists()).toBe(false)
    expect(w.find('.msg-speaker').exists()).toBe(false)
    expect(w.find('.chat-meta-bar').exists()).toBe(false)
    expect(w.find('.agent-icon-stub').exists()).toBe(false)
  })

  it('renders system text verbatim (no markdown parsing)', () => {
    const w = mountItem({ role: 'system', id: 21, content: '**Alice** joined' })
    const row = w.find('.chat-system-row')
    // Interpolated as text: markers survive literally and no <strong> is created.
    expect(row.text()).toBe('**Alice** joined')
    expect(row.find('strong').exists()).toBe(false)
    // ContentBlocks (the markdown pipeline) must not run for system rows.
    expect(w.find('.cb-stub').exists()).toBe(false)
  })

  it('still renders ordinary assistant messages as a bubble', () => {
    const w = mountItem({ role: 'assistant', id: 22, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] })
    expect(w.find('.chat-system-row').exists()).toBe(false)
    expect(w.find('.msg-card').exists()).toBe(true)
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

  // Decision #52: the rewind entry itself must not render in a group. Asserting
  // on the ICON (not just the title) pins the actual entry element: rewinding a
  // group timeline truncates rows the members' monotonic seen_cursor still
  // points past, so the members would silently lose memory. The title check
  // above would still pass if the button kept rendering with a blank title.
  it('renders no rewind entry (icon) for a group session', () => {
    const normal = mountItem(finishedAssistant)
    expect(normal.find('.lucide-rewind').exists()).toBe(true)

    const group = mountItem(finishedAssistant, { isGroupSession: true })
    expect(group.find('.lucide-rewind').exists()).toBe(false)
    // The rewind BUTTON (not just its icon) is gone.
    expect(group.findAll('.chat-action-btn').some(b => b.attributes('title')?.includes('rewindSession'))).toBe(false)
  })

  it('forwards isGroupSession to ContentBlocks', () => {
    const w = mountItem(finishedAssistant, { isGroupSession: true })
    const cb = w.findComponent({ name: 'ContentBlocks' })
    expect(cb.props('isGroupSession')).toBe(true)
  })
})

describe('ChatMessageItem group bcc card', () => {
  const hostWithBcc = (text: string) => ({
    role: 'assistant',
    id: 10,
    content: '',
    blocks: [{ type: 'text', text }],
    agentId: 'host-1',
  })

  it('renders a collapsed private-note card inside the bubble', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      hostWithBcc('<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A">只给A看</clawbench-bcc>'),
      { resolveSpeaker, hostMemberId: 'host-1' },
    )
    // Card lives INSIDE .msg-card, not in the speaker row above the bubble.
    expect(w.find('.msg-card .msg-bcc').exists()).toBe(true)
    expect(w.find('.msg-speaker .msg-bcc').exists()).toBe(false)
    // Collapsed by default: the body is hidden via v-show.
    const body = w.find('.msg-bcc-body')
    expect(body.exists()).toBe(true)
    expect((body.element as HTMLElement).style.display).toBe('none')
  })

  it('expands on click to reveal the note content and targets', async () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      hostWithBcc('<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A,B">机密内容</clawbench-bcc>'),
      { resolveSpeaker, hostMemberId: 'host-1' },
    )
    await w.find('.msg-bcc-header').trigger('click')
    const body = w.find('.msg-bcc-body')
    expect((body.element as HTMLElement).style.display).not.toBe('none')
    expect(body.text()).toContain('机密内容')
    expect(body.text()).toContain('A、B')
  })

  it('renders no card when the host has no private note', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      hostWithBcc('<clawbench-speaker>A</clawbench-speaker> 表态'),
      { resolveSpeaker, hostMemberId: 'host-1' },
    )
    expect(w.find('.msg-bcc').exists()).toBe(false)
  })

  it('does not strip the note from msgText used for parsing (card still resolves)', () => {
    // The raw tag must survive in msgText, or hostRouting could not parse it.
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      hostWithBcc('<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A">解析用</clawbench-bcc>'),
      { resolveSpeaker, hostMemberId: 'host-1' },
    )
    expect(w.find('.msg-bcc').exists()).toBe(true)
  })
})

describe('ChatMessageItem bcc card: target names hidden, user note expanded', () => {
  const hostWithBcc = (text: string) => ({
    role: 'assistant',
    id: 10,
    content: '',
    blocks: [{ type: 'text', text }],
    agentId: 'host-1',
  })
  const opts = { resolveSpeaker: () => ({ name: 'Host', backend: 'codebuddy' }), hostMemberId: 'host-1' }

  it('does NOT list target names in the collapsed header (who got a note is a hint)', () => {
    const w = mountItem(
      hostWithBcc('<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A,B">机密</clawbench-bcc>'),
      opts,
    )
    const header = w.find('.msg-bcc-header')
    expect(header.text()).toContain('Private note')
    expect(header.text()).not.toContain('A')
    expect(header.text()).not.toContain('B')
  })

  it('renders a note addressed to User expanded, labelled "to you", never collapsed', () => {
    const w = mountItem(
      hostWithBcc('<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="User">你的词是西瓜</clawbench-bcc>'),
      opts,
    )
    const card = w.find('.msg-bcc-user')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('Private note to you')
    expect(card.text()).toContain('你的词是西瓜')
    // No toggle: the body is always visible.
    const body = card.find('.msg-bcc-body')
    expect(body.exists()).toBe(true)
    expect((body.element as HTMLElement).style.display).not.toBe('none')
  })

  it('splits a mixed message: user note expanded, member note still collapsed', () => {
    const w = mountItem(
      hostWithBcc('<clawbench-speaker>A,User</clawbench-speaker> 表态 <clawbench-bcc targets="A">给A</clawbench-bcc> <clawbench-bcc targets="User">给你</clawbench-bcc>'),
      opts,
    )
    expect(w.find('.msg-bcc-user').text()).toContain('给你')
    const memberCard = w.find('.msg-bcc:not(.msg-bcc-user)')
    expect(memberCard.exists()).toBe(true)
    expect((memberCard.find('.msg-bcc-body').element as HTMLElement).style.display).toBe('none')
  })
})
