import { describe, expect, it, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TeamPanel from '@/components/chat/TeamPanel.vue'
import { updateTeamState, clearTeamState, type TeamState } from '@/composables/useTeamState'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        team: {
          title: 'Team',
          active: '{count} active',
          tools: 'tools',
          status: {
            pending: 'pending',
            running: 'running',
            completed: 'completed',
            failed: 'failed',
            terminated: 'ended',
          },
        },
      },
    },
  },
})

function mountPanel() {
  return mount(TeamPanel, { global: { plugins: [i18n] } })
}

function snapshot(over: Partial<TeamState> = {}): TeamState {
  return {
    type: 'member_status_change',
    teamName: 'clawbench-probe',
    hasLiveMembers: true,
    members: [
      { name: 'probe-alpha', color: 'blue', status: 'running', activity: 'working', lifecycle: 'alive', toolCallCount: 2, tokenUsage: { inputTokens: 56763, outputTokens: 229 } },
      { name: 'probe-beta', color: 'green', status: 'completed', activity: 'idle', lifecycle: 'terminated', toolCallCount: 1 },
    ],
    ...over,
  }
}

describe('TeamPanel', () => {
  beforeEach(() => {
    clearTeamState()
  })

  it('renders nothing when there is no team', () => {
    const wrapper = mountPanel()
    expect(wrapper.find('.team-panel').exists()).toBe(false)
  })

  it('shows the collapsed chip with active/total count', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.team-chip').exists()).toBe(true)
    expect(wrapper.get('.team-chip__count').text()).toBe('1/2')
  })

  it('expands to a roster with one row per member', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const rows = wrapper.findAll('.team-member')
    expect(rows).toHaveLength(2)
    expect(rows[0].get('.team-member__name').text()).toBe('probe-alpha')
    expect(rows[0].get('.team-member__status').text()).toBe('running')
    expect(rows[1].get('.team-member__status').text()).toBe('completed')
  })

  it('marks a terminated member row', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const rows = wrapper.findAll('.team-member')
    expect(rows[0].classes()).not.toContain('team-member--terminated')
    expect(rows[1].classes()).toContain('team-member--terminated')
  })

  it('renders the running dot with the pulsing class', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const dots = wrapper.findAll('.team-member__dot')
    expect(dots[0].classes()).toContain('team-member__dot--running')
    expect(dots[1].classes()).toContain('team-member__dot--completed')
  })

  it('marks a killed member as terminated', async () => {
    updateTeamState({
      type: 'member_status_change',
      teamName: 't',
      members: [{ name: 'probe-x', status: 'killed', lifecycle: 'terminated' }],
    })
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    expect(wrapper.get('.team-member__dot').classes()).toContain('team-member__dot--terminated')
  })

  it('formats token totals compactly', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    // (56763 + 229) / 1000 = 57.0k
    expect(wrapper.findAll('.team-member__tokens')[0].text()).toBe('57.0k')
  })

  it('collapses again when the expanded header is clicked', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    expect(wrapper.find('.team-expanded').exists()).toBe(true)
    await wrapper.get('.team-expanded__header').trigger('click')
    expect(wrapper.find('.team-chip').exists()).toBe(true)
  })

  it('disappears when the team is deleted', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.team-panel').exists()).toBe(true)
    updateTeamState({ type: 'team_deleted', teamName: 'clawbench-probe', members: [] })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.team-panel').exists()).toBe(false)
  })
})
