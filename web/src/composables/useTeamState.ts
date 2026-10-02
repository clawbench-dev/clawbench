import { ref, computed } from 'vue'

// ───────────────────────────────────────────────────────────
// Module-level singleton state — shared across the whole app.
// Agent Team state is session-level live state (like the plan):
// ChatPanelContent renders it and useChatStream feeds it, without
// prop drilling.
// ───────────────────────────────────────────────────────────

/** One member of a CodeBuddy Agent Team (wire shape from teamUpdate). */
export interface TeamMember {
  name: string
  agentType?: string
  color?: string
  description?: string
  /** pending | running | completed | failed | killed */
  status?: string
  /** starting | working | idle */
  activity?: string
  /** alive | terminated */
  lifecycle?: string
  taskId?: string
  sessionId?: string
  tokenUsage?: { inputTokens: number; outputTokens: number; lastContextWindow?: number }
  toolCallCount?: number
}

/** A full team snapshot (wire shape from session_info_update teamUpdate meta). */
export interface TeamState {
  type: string // team_created | team_deleted | member_status_change | team_idle | team_busy
  teamName: string
  isAutoTeam?: boolean
  hasLiveMembers?: boolean
  members: TeamMember[]
}

// Null means "no team" — the panel does not render (it must not occupy space).
const teamState = ref<TeamState | null>(null)
const teamCollapsed = ref(true)

/**
 * Apply a team snapshot.
 *
 * Replace semantics (the wire always sends the full member list). A
 * team_deleted snapshot clears the state entirely so the panel disappears;
 * team_idle keeps the members visible but marks them not-live.
 *
 * On first appearance the panel starts collapsed so it does not push the
 * conversation around, mirroring the plan panel.
 */
export function updateTeamState(state: TeamState | null) {
  if (!state || state.type === 'team_deleted') {
    teamState.value = null
    teamCollapsed.value = true
    return
  }
  const wasEmpty = !teamState.value
  teamState.value = { ...state, members: state.members ?? [] }
  if (wasEmpty) {
    teamCollapsed.value = true
  }
}

/** Reset all team state — called on session switch or clear. */
export function clearTeamState() {
  teamState.value = null
  teamCollapsed.value = true
}

/** Toggle collapsed state. */
export function toggleTeamCollapse() {
  teamCollapsed.value = !teamCollapsed.value
}

// ───────────────────────────────────────────────────────────
// E2E test bridge — expose team operations on window.__clawbench
// so Playwright/browser-automation can inject team data.
// ───────────────────────────────────────────────────────────
if (typeof window !== 'undefined') {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const bridge = (window as any).__clawbench || ((window as any).__clawbench = {})
  bridge.updateTeamState = updateTeamState
  bridge.clearTeamState = clearTeamState
}

// ───────────────────────────────────────────────────────────
// Public composable
// ───────────────────────────────────────────────────────────

export function useTeamState() {
  const hasTeam = computed(() => !!teamState.value && (teamState.value.members?.length ?? 0) > 0)
  const teamName = computed(() => teamState.value?.teamName ?? '')
  const members = computed(() => teamState.value?.members ?? [])
  const activeCount = computed(
    () => members.value.filter(m => m.status === 'running' || m.activity === 'working').length,
  )
  const completedCount = computed(
    () => members.value.filter(m => m.status === 'completed').length,
  )
  const isAutoTeam = computed(() => teamState.value?.isAutoTeam === true)
  /**
   * The team is over. The wire signals this with `hasLiveMembers:false` (it
   * arrives on a member_status_change, not only on team_idle — verified against
   * a real process), and no member is still alive. `undefined` means the
   * snapshot did not say, so we do NOT treat a missing flag as "ended".
   */
  const isEnded = computed(
    () =>
      hasTeam.value &&
      teamState.value?.hasLiveMembers === false &&
      !members.value.some(m => m.lifecycle === 'alive'),
  )

  return {
    teamState,
    teamCollapsed,
    hasTeam,
    teamName,
    members,
    activeCount,
    completedCount,
    isAutoTeam,
    isEnded,
    updateTeamState,
    clearTeamState,
    toggleTeamCollapse,
  }
}
