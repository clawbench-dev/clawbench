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
        <button
          v-for="m in members"
          :key="m.name"
          type="button"
          class="team-member"
          :class="memberRowClass(m)"
          :title="memberTooltip(m)"
          :aria-label="t('chat.team.detail.open', { name: m.name })"
          data-action="team-member-detail"
          :data-member-name="m.name"
          @click="openMemberDetail(m)"
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
          <!-- Name + type share ONE grid track so the numeric columns keep
               their fixed tracks (that alignment is what made the roster
               readable). Inside the track the TYPE yields first: it is
               metadata, while the name is the only thing distinguishing two
               rows. See .team-member__ident. -->
          <span class="team-member__ident">
            <span class="team-member__name" :style="{ color: memberColor(m) }">{{ m.name }}</span>
            <span v-if="m.agentType" class="team-member__agent-type">{{ m.agentType }}</span>
          </span>
          <!-- Always rendered, even when the member has no tool calls: the cell
               keeps its width so the numbers in every row land on the same
               column (a v-if here is what made the roster look ragged). -->
          <span class="team-member__tools">
            <template v-if="toolCount(m) > 0">
              <Wrench :size="12" aria-hidden="true" />{{ toolCount(m) }}
            </template>
          </span>
          <span class="team-member__tokens">{{ tokenLabel(m) }}</span>
        </button>
      </div>
    </div>

    <TeamMemberDetail :open="detailOpen" :member="detailMember" @close="closeMemberDetail" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronUp, CircleCheck, CircleDashed, CircleSlash, CircleX, Users, Wrench } from 'lucide-vue-next'
import { useTeamState, type TeamMember } from '@/composables/useTeamState'
import { teamMemberColorVar } from '@/utils/teamMemberColor'
import TeamMemberDetail from './TeamMemberDetail.vue'

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

// ── Member detail ──
//
// Clicking a member opens a sheet with everything the wire reports about it.
// It needs NO extra lookup: the roster already carries the member's full
// instructions in `description` (verified byte-for-byte against the spawn
// frame's `prompt` — see TeamMemberDetail's roleHtml comment), so the sheet
// renders straight from the snapshot it was handed.

const detailOpen = ref(false)
const detailMember = ref<TeamMember | null>(null)

function openMemberDetail(m: TeamMember) {
  detailMember.value = m
  detailOpen.value = true
}

function closeMemberDetail() {
  detailOpen.value = false
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
  /* Container for the narrow-panel rule below. The panel width follows the
     chat column (which is resizable down to ~320px and is the full screen on a
     phone), so a viewport @media query would answer the wrong question — same
     reasoning as .notif-body. */
  container-type: inline-size;
  container-name: team-roster;
}

/* Narrow roster: drop the agent type entirely.
 *
 * Threshold derived from MEASURED widths in a real browser (Chromium, the app's
 * own build), not from font metrics on paper:
 *
 *   fixed tracks   status 12 + tools 3.5em + tokens 6ch   ≈  87px
 *   gaps + padding 3 × 6 + 2 × 4 (row) + 2 × 12 (roster)  ≈  50px
 *   → identity track = roster − 137px
 *
 * The longest real member name in the production data ("review-htmlpreview",
 * 18 chars) measures 110px; the type ("general-purpose") 75px; gap 4px. So the
 * pair needs 189px, i.e. a 326px roster.
 *
 * Measured confirmation (same build, same names):
 *   roster 334px → identity 197px → pair fits, type shown   ✅
 *   roster 314px → identity 177px → pair does NOT fit        → hide
 * 330px sits between the two, so the type disappears exactly when it would
 * start truncating to "general-…" — which costs width without answering
 * anything the name does not. A first attempt used 380px and hid the type while
 * the row still had 60px of slack; that is the mistake this number corrects.
 *
 * The NAME is never hidden here: it is the only thing distinguishing two rows,
 * while the type is near-constant across an auto-team.
 *
 * `display: none` rather than `visibility`/`opacity`: it must release the space,
 * and it must not be read out as an empty element (verified against the a11y
 * tree: the hidden type does not appear in it).
 *
 * A long name still truncates via its own ellipsis, and a long type is capped by
 * `max-width` — the container query is only the coarse end of the scale. */
@container team-roster (max-width: 330px) {
  .team-member__agent-type { display: none; }
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
  /* The row is a <button> (it opens the member detail sheet), so reset the UA
     chrome: a button's default padding/background/centred text would break the
     grid alignment the roster depends on. `text-align: left` is required —
     a button centres its content regardless of the grid. */
  width: 100%;
  padding: var(--space-1) var(--space-2);
  margin: 0;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  font: inherit;
  text-align: left;
  cursor: pointer;
  color: inherit;
}

.team-member:hover {
  background: var(--bg-tertiary);
}

/* Keyboard focus must stay visible: this is the only way to reach the detail
   sheet without a pointer. */
.team-member:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
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

/* Identity track: name + agent type.
 *
 * A nested flex box, not two grid tracks. The grid's numeric columns must stay
 * on fixed tracks so they line up across rows, which leaves ONE track for the
 * identity — so the priority between name and type has to be settled inside it.
 *
 * The rule is "type yields first": the name is the only thing distinguishing
 * two rows, while the type is metadata that is usually identical across the
 * whole team. `min-width: 0` on the container is what allows the children to
 * actually shrink below their content width instead of overflowing the row. */
.team-member__ident {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  min-width: 0;
}

.team-member__name {
  /* Shrinks only after the type is gone: flex-shrink 0 would make a long name
     overflow the track, but a name that never truncates would push the type
     out even when there IS room for it. Both shrink, the type far faster. */
  flex: 0 1 auto;
  min-width: 0;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Agent type — metadata, and the FIRST thing to go when space runs out.
 *
 * Three mechanisms, in order of how narrow the row gets:
 *   1. `flex-shrink: 999` + `min-width: 0` — as the track narrows, this box
 *      gives up width long before the name does, so the type truncates first.
 *   2. `max-width: 9em` — a long type name (some agents use multi-word types)
 *      cannot eat the name's space in the first place.
 *   3. The container query below removes it entirely on a narrow panel, where
 *      even a truncated type costs more than it explains.
 *
 * Deliberately NOT a `v-if` on a width condition: that would need JS measuring
 * on every resize/streaming tick, and a ResizeObserver per row for a purely
 * presentational call. */
.team-member__agent-type {
  flex: 0 999 auto;
  min-width: 0;
  max-width: 9em;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
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
