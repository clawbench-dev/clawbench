<template>
  <div v-if="hasTeam" class="team-panel">
    <!-- Collapsed chip -->
    <div v-if="teamCollapsed" class="team-chip" @click="toggleTeamCollapse()">
      <!-- The team glyph replaces the bare status dot: it says "this is a team"
           at a glance. Motion still carries "in progress" (see design-guide
           §动效) — no colour-only signal. -->
      <Users
        :size="14"
        class="team-chip__icon"
        :class="{ 'team-chip__icon--running': activeCount > 0 }"
        aria-hidden="true"
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
          <!-- Running uses the app-wide ring (.li-spinner, declared globally in
               css/components.css) instead of a rotating lucide icon: there is
               exactly one loading indicator in this codebase. -->
          <span
            v-if="statusKind(m) === 'running'"
            class="li-spinner team-member__status-icon team-member__status-icon--running"
            :data-status="statusKind(m)"
            :aria-label="statusLabel(m)"
            role="img"
          />
          <component
            v-else
            :is="statusIcon(m)"
            :size="12"
            class="team-member__status-icon"
            :class="`team-member__status-icon--${statusKind(m)}`"
            :data-status="statusKind(m)"
            :aria-label="statusLabel(m)"
            role="img"
          />
          <span class="team-member__name" :style="{ color: memberColor(m) }">{{ m.name }}</span>
          <!-- Always rendered, even when the member has no tool calls: the cell
               keeps its width so the numbers in every row land on the same
               column (a v-if here is what made the roster look ragged). -->
          <span class="team-member__tools">
            <template v-if="toolCount(m) > 0">
              <Wrench :size="12" aria-hidden="true" />{{ toolCount(m) }}
            </template>
          </span>
          <span class="team-member__tokens">{{ tokenLabel(m) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronUp, CircleCheck, CircleDashed, CircleSlash, CircleX, Users, Wrench } from 'lucide-vue-next'
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

// Status is a shape, not a hue: every kind gets its own glyph so the roster
// stays readable without colour (36 themes, 3 of which make accent and orange
// identical — see design-guide §图标). `running` is not in the map: it renders
// the shared loading ring (see the template), which is the one spinner in the
// app — a lucide LoaderCircle here would be a second implementation.
const STATUS_ICONS: Record<string, typeof CircleCheck> = {
  completed: CircleCheck,
  failed: CircleX,
  terminated: CircleSlash,
  pending: CircleDashed,
}

function statusIcon(m: TeamMember) {
  return STATUS_ICONS[statusKind(m)] ?? CircleDashed
}

function memberRowClass(m: TeamMember): Record<string, boolean> {
  return {
    [`team-member--${statusKind(m)}`]: true,
    'team-member--terminated': m.lifecycle === 'terminated',
  }
}

function toolCount(m: TeamMember): number {
  return m.toolCallCount ?? 0
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

/* The team glyph is the chip's leading marker. It pulses while a member is
   working — same motion channel as the roster dots. */
.team-chip__icon {
  flex-shrink: 0;
  color: var(--text-muted, #6c757d);
}

.team-chip__icon--running {
  color: var(--color-info, #3b82f6);
  animation: team-pulse 1.5s ease-in-out infinite;
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
/* A grid, not a flex row: the two numeric columns must occupy the SAME x in
   every row. With flex they are content-sized, so a member with no tool calls
   (or a shorter token total) shifts every cell to its left and the roster reads
   as ragged. Every row uses this identical template, and the rows are all the
   same width, so the columns line up across the whole roster. */
.team-member {
  display: grid;
  grid-template-columns: 12px minmax(0, 1fr) 3.5em 6ch;
  align-items: center;
  gap: var(--space-3);
  min-height: 24px;
}

.team-member--terminated {
  opacity: var(--opacity-disabled);
}

/* Status is carried by the glyph's SHAPE first; colour only reinforces it.
   The shape survives colour-blindness and the three themes where accent and
   orange are identical. Running spins — the motion channel for "in progress"
   (design-guide §动效) — with no prefers-reduced-motion opt-out, matching
   .session-status: when motion is the only signal for "still working",
   freezing it leaves running and pending indistinguishable. */
.team-member__status-icon {
  flex-shrink: 0;
}

/* `running` is the shared ring: set only the variables it exposes, never the
   shape. The ring is 12px here (the grid track), so the derived border is
   calc(12px / 6) — the single ratio the whole app uses. The selector is
   (0,2,0) thanks to the scoped attribute, which beats the global
   `.li-spinner` (0,1,0), so `--li-size` actually wins. */
.team-member__status-icon--running {
  --li-size: 12px;
  --li-color: var(--color-info, #3b82f6);
}

.team-member__status-icon--completed {
  color: var(--color-green, #16a34a);
}

.team-member__status-icon--failed {
  color: var(--color-red, #ef4444);
}

.team-member__status-icon--pending {
  color: var(--text-muted, #6c757d);
}

.team-member__status-icon--terminated {
  color: var(--text-muted, #6c757d);
}

.team-member__name {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.team-member__tools,
.team-member__tokens {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #6c757d);
  white-space: nowrap;
  text-align: right;
}

/* Right-aligned numbers: the ones digit is what lines up in a numeric column.
   The wrench stays glued to its count (both inside the cell, flushed right). */
.team-member__tools {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-1);
}

.team-member__tools svg {
  flex-shrink: 0;
}

@keyframes team-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: var(--opacity-disabled); }
}

</style>
