import { describe, expect, it, beforeEach } from 'vitest'
import {
  useTeamState,
  updateTeamState,
  clearTeamState,
  toggleTeamCollapse,
  type TeamState,
} from '@/composables/useTeamState'

// Payload shapes mirror the wire (docs/dev/codebuddy_acp_team_integration.md §1.1).
function snapshot(over: Partial<TeamState> = {}): TeamState {
  return {
    type: 'member_status_change',
    teamName: 'clawbench-probe',
    isAutoTeam: false,
    hasLiveMembers: true,
    members: [
      {
        name: 'probe-alpha',
        color: 'blue',
        status: 'running',
        activity: 'working',
        lifecycle: 'alive',
        toolCallCount: 2,
        tokenUsage: { inputTokens: 56763, outputTokens: 229 },
      },
      {
        name: 'probe-beta',
        color: 'green',
        status: 'completed',
        activity: 'idle',
        lifecycle: 'terminated',
        toolCallCount: 1,
      },
    ],
    ...over,
  }
}

describe('useTeamState', () => {
  beforeEach(() => {
    clearTeamState()
  })

  it('starts empty so the panel renders nothing', () => {
    const { hasTeam, teamState } = useTeamState()
    expect(hasTeam.value).toBe(false)
    expect(teamState.value).toBeNull()
  })

  it('applies a snapshot and derives counts', () => {
    updateTeamState(snapshot())
    const { hasTeam, teamName, members, activeCount, completedCount } = useTeamState()
    expect(hasTeam.value).toBe(true)
    expect(teamName.value).toBe('clawbench-probe')
    expect(members.value).toHaveLength(2)
    // running status OR working activity counts as active
    expect(activeCount.value).toBe(1)
    expect(completedCount.value).toBe(1)
  })

  it('replaces the roster on each snapshot (no merge)', () => {
    updateTeamState(snapshot())
    updateTeamState({
      type: 'member_status_change',
      teamName: 'clawbench-probe',
      members: [{ name: 'probe-alpha', status: 'completed', lifecycle: 'terminated' }],
    })
    expect(useTeamState().members.value).toHaveLength(1)
  })

  it('clears everything on team_deleted', () => {
    updateTeamState(snapshot())
    updateTeamState({ type: 'team_deleted', teamName: 'clawbench-probe', members: [] })
    expect(useTeamState().teamState.value).toBeNull()
    expect(useTeamState().hasTeam.value).toBe(false)
  })

  it('keeps the roster visible on team_idle (members no longer live)', () => {
    updateTeamState(snapshot())
    updateTeamState({
      type: 'team_idle',
      teamName: 'clawbench-probe',
      hasLiveMembers: false,
      members: [{ name: 'probe-alpha', status: 'completed', activity: 'idle' }],
    })
    const { hasTeam, members } = useTeamState()
    expect(hasTeam.value).toBe(true)
    expect(members.value).toHaveLength(1)
  })

  it('tolerates a missing members array', () => {
    updateTeamState({ type: 'team_created', teamName: 't' } as TeamState)
    expect(useTeamState().members.value).toEqual([])
  })

  it('starts collapsed on first appearance and preserves choice afterwards', () => {
    const { teamCollapsed } = useTeamState()
    expect(teamCollapsed.value).toBe(true)
    updateTeamState(snapshot())
    expect(teamCollapsed.value).toBe(true)
    // User expands...
    toggleTeamCollapse()
    expect(teamCollapsed.value).toBe(false)
    // ...a later snapshot must not collapse it again.
    updateTeamState(snapshot({ members: [{ name: 'probe-alpha', status: 'completed' }] }))
    expect(teamCollapsed.value).toBe(false)
  })

  it('resets collapsed state when the team is cleared', () => {
    updateTeamState(snapshot())
    toggleTeamCollapse()
    expect(useTeamState().teamCollapsed.value).toBe(false)
    clearTeamState()
    expect(useTeamState().teamCollapsed.value).toBe(true)
  })
})
