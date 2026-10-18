import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string, args?: Record<string, unknown>) => (args ? `${k}|${JSON.stringify(args)}` : k) }),
  createI18n: () => ({ global: { t: (key: string) => key, locale: { value: 'en' } } }),
}))

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', props: ['backend', 'name', 'avatar', 'size'], template: '<span class="agent-icon-stub" />' },
}))
vi.mock('@/components/common/RealAgentNameTag.vue', () => ({
  default: { name: 'RealAgentNameTag', props: ['agentName', 'displayName'], template: '<span class="real-name-stub" />' },
}))
// BottomSheet teleports to <body> and gates on everOpened; a pass-through stub
// keeps the sheet's own content queryable in place.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    props: ['open'],
    template: '<div class="bs-stub"><div class="bs-header-stub"><slot name="header" /></div><slot /></div>',
  },
}))
// The panel body is covered by its own test file; here we only need to know
// whether it was rendered and what it was given.
vi.mock('../ContextUsageDetail.vue', () => ({
  default: {
    name: 'ContextUsageDetail',
    props: ['usage', 'showCompact', 'compacting'],
    template: '<div class="detail-stub" :data-compact="String(!!showCompact)" :data-compacting="String(!!compacting)" />',
  },
}))
vi.mock('@/composables/useAgents', () => ({
  getAgentAvatar: () => '',
  useAgents: () => ({ getAgentDefaultModelName: () => 'agent-default-model' }),
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const mockCompact = vi.fn()
vi.mock('@/composables/useGroupChat', () => ({
  compactGroupMember: (...a: unknown[]) => mockCompact(...a),
}))

import MemberContextSheet from '../MemberContextSheet.vue'

function member(over: Record<string, unknown> = {}) {
  return {
    id: 'm1',
    agentId: 'a1',
    name: 'Alice',
    backend: 'claude',
    avatar: '',
    agentName: 'Claude',
    left: false,
    isHost: false,
    usage: { used: 50000, size: 200000 },
    model: '',
    ...over,
  }
}

function mountSheet(props: Record<string, unknown> = {}) {
  return mount(MemberContextSheet, {
    props: { open: true, member: member(), groupId: 'g1', ...props },
  })
}

describe('MemberContextSheet', () => {
  beforeEach(() => {
    mockCompact.mockReset()
    mockCompact.mockResolvedValue({ used: 1000, size: 200000 })
  })

  it('renders the shared usage panel with the member usage', () => {
    const w = mountSheet()
    const detail = w.find('.detail-stub')
    expect(detail.exists()).toBe(true)
    expect((w.findComponent({ name: 'ContextUsageDetail' }).props('usage') as { used: number }).used).toBe(50000)
  })

  it('shows the member model override, falling back to the agent default', () => {
    expect(mountSheet().text()).toContain('agent-default-model')
    expect(mountSheet({ member: member({ model: 'claude-sonnet-4' }) }).text()).toContain('claude-sonnet-4')
  })

  it('shows an empty state (no panel) when the member never reported usage', () => {
    const w = mountSheet({ member: member({ usage: null }) })
    expect(w.find('.detail-stub').exists()).toBe(false)
    expect(w.find('.mcs-empty').exists()).toBe(true)
  })

  it('treats a zero window as "no usage" rather than 0%', () => {
    const w = mountSheet({ member: member({ usage: { used: 0, size: 0 } }) })
    expect(w.find('.detail-stub').exists()).toBe(false)
  })

  it('offers compaction for a live ACP member and reports the result', async () => {
    const w = mountSheet()
    expect(w.find('.detail-stub').attributes('data-compact')).toBe('true')
    w.findComponent({ name: 'ContextUsageDetail' }).vm.$emit('compact')
    await flushPromises()
    expect(mockCompact).toHaveBeenCalledWith('g1', 'm1')
    expect(w.emitted('compacted')).toBeTruthy()
  })

  it('disables compaction while the group is mid-turn', () => {
    const w = mountSheet({ groupRunning: true })
    expect(w.find('.detail-stub').attributes('data-compact')).toBe('false')
    expect(w.find('.mcs-hint').exists()).toBe(true)
  })

  it('disables compaction for a member that left', () => {
    const w = mountSheet({ member: member({ left: true }) })
    expect(w.find('.detail-stub').attributes('data-compact')).toBe('false')
  })

  it('keeps the panel busy until the request settles, then clears it', async () => {
    // The reported "clicking compact does nothing": the request DID run (18s),
    // but nothing in the UI changed. The busy flag must reach the shared panel
    // so it can show a spinner.
    let resolve: (v: unknown) => void = () => {}
    mockCompact.mockImplementation(() => new Promise(r => { resolve = r }))

    const w = mountSheet()
    w.findComponent({ name: 'ContextUsageDetail' }).vm.$emit('compact')
    await flushPromises()
    expect(w.find('.detail-stub').attributes('data-compacting')).toBe('true')
    expect(w.find('.mcs-progress').exists()).toBe(true)

    resolve({ used: 1, size: 2 })
    await flushPromises()
    expect(w.find('.detail-stub').attributes('data-compacting')).toBe('false')
    expect(w.find('.mcs-progress').exists()).toBe(false)
  })

  it('surfaces the server error message when compaction is refused', async () => {
    mockCompact.mockRejectedValue(new Error('GroupRunning'))
    const w = mountSheet()
    w.findComponent({ name: 'ContextUsageDetail' }).vm.$emit('compact')
    await flushPromises()
    expect(w.find('.mcs-error').text()).toContain('GroupRunning')
    expect(w.emitted('compacted')).toBeFalsy()
  })

  it('clears a previous error when another member is selected', async () => {
    mockCompact.mockRejectedValue(new Error('GroupRunning'))
    const w = mountSheet()
    w.findComponent({ name: 'ContextUsageDetail' }).vm.$emit('compact')
    await flushPromises()
    expect(w.find('.mcs-error').exists()).toBe(true)

    await w.setProps({ member: member({ id: 'm2', name: 'Bob' }) })
    expect(w.find('.mcs-error').exists()).toBe(false)
  })

  it('tags the host in the header', () => {
    const w = mountSheet({ member: member({ isHost: true }) })
    expect(w.find('.mcs-tag--host').exists()).toBe(true)
  })
})
