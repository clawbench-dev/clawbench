import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupMemberPanel from '../GroupMemberPanel.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}|${Object.values(params).join(',')}` : key,
  }),
}))

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', props: ['backend', 'name', 'avatar', 'size'], template: '<span class="agent-icon-stub" />' },
}))

vi.mock('@/composables/useAgents', () => ({
  getAgentAvatar: (agentId: string) => (agentId ? `<svg>agent-${agentId}</svg>` : ''),
}))

vi.mock('@/utils/format', () => ({
  formatRelativeTime: (date: string) => `rel(${date})`,
}))

const MEMBERS = [
  { id: 'h', agentId: 'a-h', name: 'Host', backend: 'codebuddy', avatar: '', agentName: 'Host', isHost: true, disabled: false },
  { id: 'b', agentId: 'a-b', name: 'B', backend: 'claude', avatar: '', agentName: 'B', isHost: false, disabled: false },
  { id: 'c', agentId: 'a-c', name: 'C', backend: 'claude', avatar: '', agentName: 'C', isHost: false, disabled: false },
  { id: 'd', agentId: 'a-d', name: 'D', backend: 'claude', avatar: '', agentName: 'D', isHost: false, disabled: true },
]

const MESSAGES: any[] = [
  { role: 'user', id: 1, content: 'go' },
  { role: 'assistant', id: 11, agentId: 'b', streaming: true, content: '' },
  { role: 'assistant', id: 12, agentId: 'c', content: 'done', createdAt: '2026-11-12T10:00:00Z' },
]

function mountPanel(props: Record<string, unknown> = {}) {
  return mount(GroupMemberPanel, {
    props: {
      sessionId: 'g1',
      members: MEMBERS,
      messages: MESSAGES,
      hostMemberId: 'h',
      running: true,
      ...props,
    },
  })
}

function rowOf(w: ReturnType<typeof mountPanel>, name: string) {
  const rows = w.findAll('.member-row')
  const row = rows.find(r => r.find('.member-row__name').text() === name)
  if (!row) throw new Error(`row not found: ${name}`)
  return row
}

describe('GroupMemberPanel', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('renders nothing without members', () => {
    const w = mountPanel({ members: [] })
    expect(w.find('.member-panel').exists()).toBe(false)
  })

  it('starts collapsed and shows one avatar per speaker plus the progress', () => {
    const w = mountPanel()
    expect(w.find('.member-chip').exists()).toBe(true)
    expect(w.find('.member-expanded').exists()).toBe(false)
    expect(w.findAll('.member-chip__avatar')).toHaveLength(1)
    expect(w.find('.member-chip__text').text()).toContain('group.memberPanel.speakingOne')
    expect(w.find('.member-chip__text').text()).toContain('B')
    expect(w.find('.member-chip__count').text()).toContain('group.memberPanel.progress|1,3')
  })

  it('lists every simultaneous speaker and caps the chip avatars at three', () => {
    const many = [
      { id: 'h', agentId: 'a-h', name: 'Host', backend: 'codebuddy', avatar: '', agentName: 'Host', isHost: true, disabled: false },
      { id: 'b', agentId: 'a-b', name: 'B', backend: 'claude', avatar: '', agentName: 'B', isHost: false, disabled: false },
      { id: 'c', agentId: 'a-c', name: 'C', backend: 'claude', avatar: '', agentName: 'C', isHost: false, disabled: false },
      { id: 'e', agentId: 'a-e', name: 'E', backend: 'claude', avatar: '', agentName: 'E', isHost: false, disabled: false },
    ]
    const w = mountPanel({
      members: many,
      messages: [
        { role: 'user', id: 1, content: 'go' },
        { role: 'assistant', id: 11, agentId: 'b', streaming: true },
        { role: 'assistant', id: 12, agentId: 'c', streaming: true },
        { role: 'assistant', id: 13, agentId: 'e', streaming: true },
      ],
    })
    expect(w.findAll('.member-chip__avatar')).toHaveLength(3)
    expect(w.find('.member-chip__text').text()).toContain('group.memberPanel.speakingMany|3')
  })

  it('expands into a roster with the host first and one status per member', async () => {
    const w = mountPanel()
    await w.find('.member-chip').trigger('click')

    expect(w.find('.member-chip').exists()).toBe(false)
    expect(w.find('.member-expanded').exists()).toBe(true)

    const rows = w.findAll('.member-row')
    expect(rows).toHaveLength(4)
    expect(rows[0].find('.member-row__name').text()).toBe('Host')
    expect(rows[0].find('.member-row__host').exists()).toBe(true)

    expect(rowOf(w, 'B').find('.member-row__pill').classes()).toContain('is-speaking')
    expect(rowOf(w, 'C').find('.member-row__pill').classes()).toContain('is-done')
    expect(rowOf(w, 'Host').find('.member-row__pill').classes()).toContain('is-waiting')
    expect(rowOf(w, 'D').find('.member-row__pill').classes()).toContain('is-disabled')
  })

  it('offers a stop button only for a speaking member that is not the host', async () => {
    const w = mountPanel({
      messages: [
        { role: 'user', id: 1, content: 'go' },
        { role: 'assistant', id: 10, agentId: 'h', streaming: true },
        { role: 'assistant', id: 11, agentId: 'b', streaming: true },
      ],
    })
    await w.find('.member-chip').trigger('click')

    expect(rowOf(w, 'B').find('.member-row__stop').exists()).toBe(true)
    // The host drives the round — stopping it is the whole-turn cancel.
    expect(rowOf(w, 'Host').find('.member-row__stop').exists()).toBe(false)
    expect(rowOf(w, 'C').find('.member-row__stop').exists()).toBe(false)
  })

  it('emits stop-member with the member row id and does not jump', async () => {
    const w = mountPanel()
    await w.find('.member-chip').trigger('click')
    await rowOf(w, 'B').find('.member-row__stop').trigger('click')

    expect(w.emitted('stop-member')).toEqual([['b']])
    expect(w.emitted('jump-message')).toBeUndefined()
  })

  it('emits jump-message with the member latest reply id when the row is clicked', async () => {
    const w = mountPanel()
    await w.find('.member-chip').trigger('click')
    await rowOf(w, 'C').find('.member-row__main').trigger('click')

    expect(w.emitted('jump-message')).toEqual([[12]])
  })

  it('does nothing when a member has never spoken', async () => {
    const w = mountPanel()
    await w.find('.member-chip').trigger('click')
    await rowOf(w, 'Host').find('.member-row__main').trigger('click')

    expect(w.emitted('jump-message')).toBeUndefined()
    expect(rowOf(w, 'Host').find('.member-row__jump').exists()).toBe(false)
  })

  it('shows relative times instead of status pills when no round is running', async () => {
    const w = mountPanel({ running: false })
    expect(w.find('.member-chip__count').text()).toContain('group.memberPanel.roundEnded')
    expect(w.find('.member-chip__text').text()).toContain('group.memberPanel.idleWithTime')

    await w.find('.member-chip').trigger('click')
    expect(w.find('.member-row__pill').exists()).toBe(false)
    expect(rowOf(w, 'C').find('.member-row__time').text()).toBe('rel(2026-11-12T10:00:00Z)')
    expect(rowOf(w, 'Host').find('.member-row__time').text()).toBe('group.memberPanel.noReply')
  })

  it('collapses again when the session changes', async () => {
    const w = mountPanel()
    await w.find('.member-chip').trigger('click')
    expect(w.find('.member-expanded').exists()).toBe(true)

    await w.setProps({ sessionId: 'g2' })
    expect(w.find('.member-chip').exists()).toBe(true)
    expect(w.find('.member-expanded').exists()).toBe(false)
  })

  it('opens the group settings from the footer', async () => {
    const w = mountPanel()
    await w.find('.member-chip').trigger('click')
    await w.find('.member-expanded__manage').trigger('click')
    expect(w.emitted('open-settings')).toHaveLength(1)
  })
})
