<template>
  <div v-if="hasTeam" class="team-panel">
    <!-- Collapsed chip -->
    <div v-if="teamCollapsed" class="team-chip" @click="toggleTeamCollapse()">
      <span
        class="team-chip__dot"
        :class="`team-chip__dot--${activeCount > 0 ? 'running' : 'idle'}`"
      />
      <span class="team-chip__text">{{ chipText }}</span>
      <span class="team-chip__count">{{ activeCount }}/{{ members.length }}</span>
      <ChevronDown :size="12" class="team-chip__toggle" />
    </div>

    <!-- Expanded roster -->
    <div v-else class="team-expanded">
      <div class="team-expanded__header" @click="toggleTeamCollapse()">
        <span class="team-expanded__title">{{ t('chat.team.title') }} · {{ teamName }}</span>
        <span v-if="isAutoTeam" class="team-expanded__auto">{{ t('chat.team.autoTeam') }}</span>
        <span class="team-expanded__count">{{ headerCount }}</span>
        <ChevronUp :size="12" class="team-expanded__toggle" />
      </div>
      <div class="team-expanded__roster">
        <div
          v-for="m in members"
          :key="m.name"
          class="team-member"
          :class="memberRowClass(m)"
          :title="memberTooltip(m)"
        >
          <span class="team-member__dot" :class="`team-member__dot--${statusKind(m)}`" />
          <span class="team-member__name" :style="{ color: memberColor(m) }">{{ m.name }}</span>
          <span v-if="m.agentType" class="team-member__agent-type">{{ m.agentType }}</span>
          <span class="team-member__status">{{ statusLabel(m) }}</span>
          <span class="team-member__tools">{{ toolLabel(m) }}</span>
          <span class="team-member__tokens">{{ tokenLabel(m) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronUp } from 'lucide-vue-next'
import { useTeamState, type TeamMember } from '@/composables/useTeamState'
import { teamMemberColorVar } from '@/utils/teamMemberColor'

const { t } = useI18n()
const { teamCollapsed, hasTeam, teamName, members, activeCount, completedCount, isAutoTeam, isEnded, toggleTeamCollapse } =
  useTeamState()

// Member colour comes from the shared palette so the roster and the permission
// card agree (see teamMemberColor.ts).
function memberColor(m: TeamMember): string {
  return teamMemberColorVar(m.color)
}

// ── Status semantics ──
// status / activity / lifecycle evolve independently on the wire. status is
// the primary signal; lifecycle=terminated only dims the row (it says the
// member was reclaimed, not that it failed). See integration plan §4.4.
function statusKind(m: TeamMember): string {
  switch (m.status) {
    case 'running':
      return 'running'
    case 'completed':
      return 'completed'
    case 'failed':
      return 'failed'
    case 'killed':
      return 'terminated'
    default:
      return m.activity === 'working' ? 'running' : 'pending'
  }
}

function statusLabel(m: TeamMember): string {
  const kind = statusKind(m)
  return t(`chat.team.status.${kind}`)
}

function memberRowClass(m: TeamMember): Record<string, boolean> {
  return {
    [`team-member--${statusKind(m)}`]: true,
    'team-member--terminated': m.lifecycle === 'terminated',
  }
}

function toolLabel(m: TeamMember): string {
  const n = m.toolCallCount ?? 0
  return n > 0 ? `${n} ${t('chat.team.tools')}` : ''
}

// Compact token total (input + output), e.g. 85200 -> "85.2k".
function tokenLabel(m: TeamMember): string {
  const total = tokenTotal(m)
  if (total <= 0) return ''
  return total >= 1000 ? `${(total / 1000).toFixed(1)}k` : String(total)
}

function tokenTotal(m: TeamMember): number {
  const u = m.tokenUsage
  if (!u) return 0
  return (u.inputTokens ?? 0) + (u.outputTokens ?? 0)
}

// The wire only gives us `description`, `agentType`, the token split and the
// last context window — none fit the row (too wide / secondary), so they go in
// a native tooltip (doc §4.1). Empty lines are dropped so a member without
// metadata does not get a blank tooltip.
function memberTooltip(m: TeamMember): string {
  const lines: string[] = []
  if (m.description) lines.push(m.description)
  if (m.agentType) lines.push(`${t('chat.team.agentType')}: ${m.agentType}`)
  const u = m.tokenUsage
  if (u) {
    lines.push(`${t('chat.team.tokens')}: ${u.inputTokens ?? 0} in / ${u.outputTokens ?? 0} out`)
    if (u.lastContextWindow) {
      lines.push(`${t('chat.team.contextWindow')}: ${u.lastContextWindow}`)
    }
  }
  return lines.join('\n')
}

// Header count: while running show "N active"; once the team is over show the
// completed tally instead, so the panel does not read "0 active" forever.
const headerCount = computed(() =>
  isEnded.value
    ? `${completedCount.value}/${members.value.length}`
    : t('chat.team.active', { count: activeCount.value }),
)

const chipText = computed(() => {
  // Ended: the roster is history — show the team name, not a stale member.
  if (isEnded.value) return `${teamName.value} · ${t('chat.team.ended')}`
  const working = members.value.find(m => m.status === 'running' || m.activity === 'working')
  if (working) return `${working.name} · ${statusLabel(working)}`
  return teamName.value
})
</script>

<style scoped>
.team-panel {
  width: auto;
  margin: 0 var(--space-5) var(--space-4);
}

/* ── Collapsed chip ── */
.team-chip {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-5);
  border-radius: var(--radius-lg);
  background: var(--bg-tertiary, #e9ecef);
  border: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
}

.team-chip__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.team-chip__dot--running {
  background: var(--color-info, #3b82f6);
  animation: team-pulse 1.5s ease-in-out infinite;
}

.team-chip__dot--idle {
  background: var(--text-muted, #6c757d);
}

.team-chip__text {
  flex: 1;
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #495057);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.team-chip__count {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted, #6c757d);
  white-space: nowrap;
}

.team-chip__toggle {
  color: var(--text-muted, #6c757d);
  flex-shrink: 0;
}

/* ── Expanded roster ── */
.team-expanded {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm);
  padding: var(--space-4) var(--space-6);
}

.team-expanded__header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
  cursor: pointer;
}

.team-expanded__title {
  flex: 1;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #212529);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.team-expanded__count {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #6c757d);
  white-space: nowrap;
}

.team-expanded__auto {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted, #6c757d);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
  white-space: nowrap;
}

.team-expanded__toggle {
  color: var(--text-muted, #6c757d);
  flex-shrink: 0;
}

.team-expanded__roster {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  max-height: 240px;
  overflow-y: auto;
}

/* ── Member row ── */
.team-member {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-height: 24px;
}

.team-member--terminated {
  opacity: var(--opacity-disabled);
}

.team-member__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

/* Running pulses; the others are static. The motion is the information
   channel for "in progress" (see design-guide §动效) — no
   prefers-reduced-motion opt-out, matching .session-status. */
.team-member__dot--running {
  background: var(--color-info, #3b82f6);
  animation: team-pulse 1.5s ease-in-out infinite;
}

.team-member__dot--completed {
  background: var(--color-green, #16a34a);
}

.team-member__dot--failed {
  background: var(--color-red, #ef4444);
}

.team-member__dot--pending {
  background: var(--text-muted, #6c757d);
}

.team-member__dot--terminated {
  background: var(--text-muted, #6c757d);
}

.team-member__name {
  flex: 1;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.team-member__status,
.team-member__tools,
.team-member__tokens,
.team-member__agent-type {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted, #6c757d);
  white-space: nowrap;
}

/* The agent type is metadata, not identity — cap its width so a long type name
   cannot push the status/tools columns out of the row. */
.team-member__agent-type {
  max-width: 8em;
  overflow: hidden;
  text-overflow: ellipsis;
}

@keyframes team-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: var(--opacity-disabled); }
}
</style>
