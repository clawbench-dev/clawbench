import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'

// Minimal mocks so ChatMessageItem mounts. Group speaker rendering only needs
// the template path; ContentBlocks and the heavy composables are stubbed.
vi.mock('@/composables/useDoubleClickCopy', () => ({ useDoubleClickCopy: () => ({ handleDblClick: vi.fn() }) }))
// Spread the real module: the private-note card now imports the real markdown
// renderer, whose dependency chain pulls `registerWorktreeCacheClearter` out of
// this module. A hand-written partial mock would leave it undefined and crash
// the import (the classic whitelist-mock trap).
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
  messages: { en: { chat: { message: {}, contentBlocks: { cancelled: 'cancelled' }, fileChanges: { title: 'Files' }, pending: {}, speech: {}, busy: {} }, group: { host: 'Host', you: 'you', bcc: { title: 'Private note', to: 'To', toYou: 'Private note to you' } }, common: {} } },
})

function mountItem(msg: Record<string, unknown>, props: Record<string, unknown> = {}) {
  return mount(ChatMessageItem, {
    props: { msg, index: 0, ...props },
    global: {
      plugins: [i18n],
      provide: {
        autoSpeech: { isActive: () => false, isGeneratingText: () => false, isPlayingAudio: () => false, playAudio: vi.fn(), stopAudio: vi.fn(), speakText: vi.fn(), getSummary: () => null, getPhaseLabel: () => '' },
        // renderTextBlock is used by the private-note (密送) card to render the
        // note body through the same markdown pipeline as the bubble. Wire the
        // REAL renderer so the card assertions test actual markdown output
        // rather than a stub's return value (which would make them tautological).
        chatRender: { renderTextBlock: (text: string) => renderMarkdownHtml(text || '', { skipEnhancements: true }), toolCallSummary: vi.fn(), formatToolInput: vi.fn(), humanizeCron: vi.fn(), repeatLabel: vi.fn(), truncate: vi.fn(), hasImagesInContent: () => false },
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
    // The host tag carries a crown icon alongside the label.
    expect(w.find('.msg-speaker-host-tag .msg-speaker-host-crown').exists()).toBe(true)
    expect(w.find('.msg-speaker-host-tag .lucide-crown').exists()).toBe(true)
  })

  it('renders no speaker header for an ordinary message', () => {
    const w = mountItem({ role: 'assistant', id: 3, content: '', blocks: [] })
    expect(w.find('.msg-speaker').exists()).toBe(false)
  })

  it('renders the mention summary row above the bubble', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const resolveSpeakerByName = (n: string) => ({ name: n, backend: 'claude', avatar: '<svg/>' })
    const w = mountItem(
      {
        role: 'assistant',
        id: 4,
        content: '',
        blocks: [{ type: 'text', text: '<clawbench-mention targets="A,B">请表态</clawbench-mention>' }],
        agentId: 'host-1',
      },
      { resolveSpeaker, resolveSpeakerByName, hostMemberId: 'host-1' },
    )
    // The summary row lives OUTSIDE the bubble, above it.
    const summary = w.find('.msg-mention-summary')
    expect(summary.exists()).toBe(true)
    expect(summary.text()).toContain('@')
    expect(summary.text()).toContain('A')
    expect(summary.text()).toContain('B')
    expect(w.find('.msg-card .msg-mention-summary').exists()).toBe(false)
  })

  it('shows the summary row for a free-mode member too (no host)', () => {
    const resolveSpeaker = (id: string) => (id === 'member-1' ? { name: 'Alice', backend: 'claude' } : null)
    const resolveSpeakerByName = (n: string) => ({ name: n, backend: 'claude', avatar: '' })
    const w = mountItem(
      {
        role: 'assistant',
        id: 7,
        content: '',
        blocks: [{ type: 'text', text: '<clawbench-mention targets="Bob">你来补充</clawbench-mention>' }],
        agentId: 'member-1',
      },
      { resolveSpeaker, resolveSpeakerByName },
    )
    expect(w.find('.msg-mention-summary').exists()).toBe(true)
    expect(w.find('.msg-mention-summary').text()).toContain('Bob')
  })

  it('renders no summary row when the message mentions nobody', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      {
        role: 'assistant',
        id: 5,
        content: '',
        blocks: [{ type: 'text', text: '只是普通发言' }],
        agentId: 'host-1',
      },
      { resolveSpeaker, hostMemberId: 'host-1' },
    )
    expect(w.find('.msg-mention-summary').exists()).toBe(false)
  })

  it('shows the configured user nickname for the reserved human target', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      {
        role: 'assistant',
        id: 8,
        content: '',
        blocks: [{ type: 'text', text: '<clawbench-mention targets="老板">该你说了</clawbench-mention>' }],
        agentId: 'host-1',
      },
      { resolveSpeaker, hostMemberId: 'host-1', userNickname: '老板' },
    )
    const summary = w.find('.msg-mention-summary')
    expect(summary.exists()).toBe(true)
    expect(summary.text()).toContain('老板')
  })

  it('falls back to the built-in reserved name when no nickname prop is given', () => {
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      {
        role: 'assistant',
        id: 9,
        content: '',
        blocks: [{ type: 'text', text: '<clawbench-mention targets="User">该你说了</clawbench-mention>' }],
        agentId: 'host-1',
      },
      { resolveSpeaker, hostMemberId: 'host-1' },
    )
    const summary = w.find('.msg-mention-summary')
    expect(summary.exists()).toBe(true)
    expect(summary.text()).toContain('User')
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
      hostWithBcc('<clawbench-mention targets="A"> 表态 </clawbench-mention><clawbench-mention targets="A" private>只给A看</clawbench-mention>'),
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
    // Realistic resolvers: the id resolver knows only the host's row id, the
    // name resolver only real display names. (A catch-all "return Host for
    // anything" mock would make every target resolve to Host.)
    const resolveSpeaker = (id: string) => (id === 'host-1' ? { name: 'Host', backend: 'codebuddy' } : null)
    const resolveSpeakerByName = (n: string) => (['A', 'B'].includes(n) ? { name: n, backend: 'claude', avatar: '' } : null)
    const w = mountItem(
      hostWithBcc('<clawbench-mention targets="A"> 表态 </clawbench-mention><clawbench-mention targets="A,B" private>机密内容</clawbench-mention>'),
      { resolveSpeaker, resolveSpeakerByName, hostMemberId: 'host-1' },
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
      hostWithBcc('<clawbench-mention targets="A">表态</clawbench-mention>'),
      { resolveSpeaker, hostMemberId: 'host-1' },
    )
    expect(w.find('.msg-bcc').exists()).toBe(false)
  })

  it('does not strip the note from msgText used for parsing (card still resolves)', () => {
    // The raw tag must survive in msgText, or groupRouting could not parse it.
    const resolveSpeaker = () => ({ name: 'Host', backend: 'codebuddy' })
    const w = mountItem(
      hostWithBcc('<clawbench-mention targets="A"> 表态 </clawbench-mention><clawbench-mention targets="A" private>解析用</clawbench-mention>'),
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
      hostWithBcc('<clawbench-mention targets="A"> 表态 </clawbench-mention><clawbench-mention targets="A,B" private>机密</clawbench-mention>'),
      opts,
    )
    const header = w.find('.msg-bcc-header')
    expect(header.text()).toContain('Private note')
    expect(header.text()).not.toContain('A')
    expect(header.text()).not.toContain('B')
  })

  it('renders a note addressed to User expanded, labelled "to you", never collapsed', () => {
    const w = mountItem(
      hostWithBcc('<clawbench-mention targets="A"> 表态 </clawbench-mention><clawbench-mention targets="User" private>你的词是西瓜</clawbench-mention>'),
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
      hostWithBcc('<clawbench-mention targets="A,User"> 表态 </clawbench-mention><clawbench-mention targets="A" private>给A</clawbench-mention> <clawbench-mention targets="User" private>给你</clawbench-mention>'),
      opts,
    )
    expect(w.find('.msg-bcc-user').text()).toContain('给你')
    const memberCard = w.find('.msg-bcc:not(.msg-bcc-user)')
    expect(memberCard.exists()).toBe(true)
    expect((memberCard.find('.msg-bcc-body').element as HTMLElement).style.display).toBe('none')
  })

  it('treats a note to the configured nickname as "to you"', () => {
    // With a nickname configured, the human target is the nickname, not "User".
    // A note addressed to it renders expanded under the "to you" header.
    const w = mountItem(
      hostWithBcc('<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention targets="老板" private>你的词是西瓜</clawbench-mention>'),
      { ...opts, userNickname: '老板' },
    )
    const card = w.find('.msg-bcc-user')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain('Private note to you')
    expect(card.text()).toContain('你的词是西瓜')
  })
})

describe('ChatMessageItem bcc card: note body renders Markdown', () => {
  const hostWithBcc = (text: string) => ({
    role: 'assistant',
    id: 10,
    content: '',
    blocks: [{ type: 'text', text }],
    agentId: 'host-1',
  })
  const opts = { resolveSpeaker: () => ({ name: 'Host', backend: 'codebuddy' }), hostMemberId: 'host-1' }

  // A private note's body is prose written by an agent, so it must go through
  // the SAME markdown pipeline as the bubble body — otherwise `**bold**` shows
  // as literal asterisks. Both audiences (a note to the user, and a note to an
  // AI member behind the collapsed header) must render.
  it('renders Markdown in a note addressed to the user', () => {
    const w = mountItem(
      hostWithBcc('<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention targets="User" private>**你的词**是 `西瓜`</clawbench-mention>'),
      opts,
    )
    const body = w.find('.msg-bcc-user .msg-bcc-entry-content')
    expect(body.exists()).toBe(true)
    // Rendered, not literal: the markers are consumed into real elements.
    expect(body.find('strong').exists()).toBe(true)
    expect(body.find('code').exists()).toBe(true)
    expect(body.text()).toContain('西瓜')
    expect(body.text()).not.toContain('**')
    expect(body.text()).not.toContain('`')
  })

  it('renders Markdown in a note addressed to AI members (behind the collapsed header)', async () => {
    const w = mountItem(
      hostWithBcc('<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention targets="A" private>- 第一点\n- 第二点</clawbench-mention>'),
      opts,
    )
    await w.find('.msg-bcc-header').trigger('click')
    const body = w.find('.msg-bcc-entry-content')
    expect(body.exists()).toBe(true)
    // A list is a block element: proof the body was parsed, not shown verbatim.
    expect(body.find('ul').exists()).toBe(true)
    expect(body.findAll('li')).toHaveLength(2)
  })

  it('does not render raw HTML from the note (sanitized by the pipeline)', () => {
    const w = mountItem(
      hostWithBcc('<clawbench-mention targets="A">表态</clawbench-mention><clawbench-mention targets="A" private><img src=x onerror="alert(1)">正文</clawbench-mention>'),
      opts,
    )
    const body = w.find('.msg-bcc-entry-content')
    // DOMPurify drops the event handler; the payload cannot execute.
    expect(body.find('img[onerror]').exists()).toBe(false)
    expect(body.text()).toContain('正文')
  })
})

describe('ChatMessageItem: a USER message private note (C3)', () => {
  // The user's @ cards serialize to protocol tags on send, including `private`
  // ones (密送). The reader must be able to audit what they sent — so a USER
  // row must parse and render its own note card.
  //
  // This is the regression the design review caught: `msgText` returns '' for
  // non-assistant rows, and the parse was gated on "has a speaker" (a user row
  // has no agentId). Either alone leaves the card invisible. A test that only
  // asserted the parse gate (or only that msgText is non-empty) would pass while
  // the card stayed missing — so this asserts the RENDERED card.
  const userWithNote = (text: string) => ({
    role: 'user',
    id: 30,
    content: text,
    blocks: [{ type: 'text', text }],
  })

  it('renders the private-note card on a user bubble (collapsed, auditable)', async () => {
    // The user's note targets a member ROW ID; the card must show the NAME.
    const resolveSpeaker = (id: string) => (id === 'm-a' ? { name: 'Alice', backend: 'claude' } : null)
    const w = mountItem(
      userWithNote('<clawbench-mention targets="m-a"></clawbench-mention> <clawbench-mention targets="m-a" private>你的词是西瓜</clawbench-mention> 你先说'),
      { isGroupSession: true, resolveSpeaker },
    )
    // A note the USER sent to a MEMBER renders as the member card (collapsed by
    // default, consistent with the host's member notes) — the user can expand it
    // to audit what they sent. It is NOT the "to you" card: the reader is the
    // sender here, not the target.
    const card = w.find('.msg-bcc')
    expect(card.exists()).toBe(true)
    await w.find('.msg-bcc-header').trigger('click')
    expect(w.find('.msg-bcc-entry-content').text()).toContain('你的词是西瓜')
    // The target label must be the resolved NAME, not the raw row id — the whole
    // point of an auditable card. (Agents write names, so only user notes need
    // this, and only a test targeting a user note catches a missing resolver.)
    const targets = w.find('.msg-bcc-entry-targets').text()
    expect(targets).toContain('Alice')
    expect(targets).not.toContain('m-a')
  })

  it('does not render the mention summary row on a user bubble (chips are already inline)', () => {
    const resolveSpeakerByName = (n: string) => ({ name: n, backend: 'claude', avatar: '' })
    const w = mountItem(
      userWithNote('<clawbench-mention targets="m-a"></clawbench-mention> 你先说'),
      { isGroupSession: true, resolveSpeakerByName },
    )
    expect(w.find('.msg-mention-summary').exists()).toBe(false)
  })

  it('renders no note card for a user message without one', () => {
    const w = mountItem(
      userWithNote('<clawbench-mention targets="m-a"></clawbench-mention> 你先说'),
      { isGroupSession: true },
    )
    expect(w.find('.msg-bcc').exists()).toBe(false)
  })

  it('does not parse a user message protocol tag OUTSIDE a group session', () => {
    // A single-agent chat has no protocol; a user who literally types the tag
    // text must not get a note card.
    const w = mountItem(userWithNote('<clawbench-mention targets="m-a" private>字面文本</clawbench-mention>'))
    expect(w.find('.msg-bcc').exists()).toBe(false)
  })
})
