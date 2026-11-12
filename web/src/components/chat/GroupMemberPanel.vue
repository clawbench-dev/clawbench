<template>
  <div v-if="members.length" class="member-panel">
    <!-- Collapsed chip. Same collapse pattern as the plan / team cards, but the
         leading slot holds the avatars of whoever is speaking RIGHT NOW instead
         of an icon: the team card already uses a people glyph, and avatars also
         answer "who" without a second line. -->
    <div
      v-if="collapsed"
      class="member-chip"
      role="button"
      tabindex="0"
      :aria-expanded="false"
      :aria-label="t('group.memberPanel.label')"
      @click="collapsed = false"
      @keydown.enter.prevent="collapsed = false"
      @keydown.space.prevent="collapsed = false"
    >
      <span v-if="chipAvatars.length" class="member-chip__avatars" aria-hidden="true">
        <span v-for="m in chipAvatars" :key="m.id" class="member-chip__avatar">
          <AgentIcon :backend="m.backend" :name="m.name" :avatar="avatarOf(m)" size="sm" />
        </span>
      </span>
      <span class="member-chip__text">{{ chipText }}</span>
      <span v-if="progressText" class="member-chip__count">{{ progressText }}</span>
      <ChevronDown :size="12" class="member-chip__toggle" aria-hidden="true" />
    </div>

    <!-- Expanded roster -->
    <div v-else class="member-expanded">
      <div
        class="member-expanded__header"
        role="button"
        tabindex="0"
        :aria-expanded="true"
        @click="collapsed = true"
        @keydown.enter.prevent="collapsed = true"
        @keydown.space.prevent="collapsed = true"
      >
        <span class="member-expanded__title">{{ t('group.memberPanel.title') }}</span>
        <span v-if="progressText" class="member-expanded__count">{{ progressText }}</span>
        <ChevronUp :size="12" class="member-expanded__toggle" aria-hidden="true" />
      </div>

      <ul class="member-list">
        <li
          v-for="m in orderedMembers"
          :key="m.id"
          class="member-row"
          :class="{
            'member-row--speaking': statusOf(m) === 'speaking',
            'member-row--disabled': statusOf(m) === 'disabled',
          }"
        >
          <!-- The whole row is the jump target; the trailing slot is a real
               button only while this member can be stopped. -->
          <div
            class="member-row__main"
            role="button"
            tabindex="0"
            :title="jumpTitle(m)"
            :aria-label="jumpTitle(m)"
            @click="jump(m)"
            @keydown.enter.prevent="jump(m)"
          >
            <span class="member-row__avatar">
              <AgentIcon :backend="m.backend" :name="m.name" :avatar="avatarOf(m)" size="md" />
            </span>
            <span class="member-row__identity">
              <span class="member-row__name">{{ m.name }}</span>
              <Crown v-if="m.isHost" :size="10" class="member-row__host" aria-hidden="true" />
              <!-- Extra identity detail: the underlying agent name (only when the
                   member's display name was auto-derived — RealAgentNameTag owns
                   that rule) and the model it actually runs. Hidden on a narrow
                   panel via the container query below, so the name and status
                   never get squeezed by it. -->
              <span v-if="hasMeta(m)" class="member-row__meta">
                <RealAgentNameTag :agent-name="m.agentName" :display-name="m.name" />
                <span v-if="modelLabel(m)" class="member-row__model">· {{ modelLabel(m) }}</span>
              </span>
            </span>
            <span v-if="running" class="member-row__pill" :class="`is-${statusOf(m)}`">
              {{ statusLabel(m) }}
            </span>
            <span v-else class="member-row__time">{{ timeLabel(m) }}</span>
          </div>

          <button
            v-if="canStop(m)"
            type="button"
            class="member-row__stop"
            :title="t('group.memberPanel.stop', { name: m.name })"
            :aria-label="t('group.memberPanel.stop', { name: m.name })"
            data-action="stop-member"
            @click.stop="$emit('stop-member', m.id)"
          >
            <Square :size="9" fill="currentColor" aria-hidden="true" />
          </button>
          <span
            v-else-if="hasReply(m)"
            class="member-row__jump"
            :title="t('group.memberPanel.jump', { name: m.name })"
            aria-hidden="true"
          >
            <LocateFixed :size="13" />
          </span>
        </li>
      </ul>

      <button type="button" class="member-expanded__manage" @click="$emit('open-settings')">
        {{ t('group.memberPanel.manage') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronUp, Crown, LocateFixed, Square } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import RealAgentNameTag from '@/components/common/RealAgentNameTag.vue'
import { getAgentAvatar, useAgents } from '@/composables/useAgents'
import { formatRelativeTime } from '@/utils/format'
import { formatModelIdForDisplay } from '@/utils/modelDisplay'
import { collectGroupMemberStatus, type GroupMemberRoundStatus } from '@/utils/groupMemberStatus'
import type { GroupMemberInfo } from '@/composables/useGroupChat'
import type { ChatMessage } from '@/utils/chatStreamUtils'

const props = withDefaults(defineProps<{
  /** Current session id — a session switch collapses the panel again. */
  sessionId: string
  /** Group roster (removed members already filtered out by the caller). */
  members: GroupMemberInfo[]
  /** Rendered messages of THIS session; the status derivation reads them. */
  messages: ChatMessage[]
  /** Host member row id. The host can never be stopped on its own: it drives
   *  the routing, so "stop the host" is the whole-turn cancel (input bar). */
  hostMemberId?: string
  /** Whether a group round is in flight. Idle panels show "last reply …"
   *  instead of per-member pills — everyone being "done" is not information. */
  running?: boolean
}>(), {
  hostMemberId: '',
  running: false,
})

const emit = defineEmits<{
  (e: 'stop-member', id: string): void
  (e: 'jump-message', id: string | number): void
  (e: 'open-settings'): void
}>()

const { t } = useI18n()
const { getAgentModel, getDefaultModelId } = useAgents()

const collapsed = ref(true)
watch(() => props.sessionId, () => { collapsed.value = true })

const statuses = computed(() => collectGroupMemberStatus(props.members, props.messages))
const statusById = computed(() => new Map(statuses.value.map(s => [s.id, s])))

function entryOf(m: GroupMemberInfo) {
  return statusById.value.get(String(m.id))
}
function statusOf(m: GroupMemberInfo): GroupMemberRoundStatus {
  return entryOf(m)?.status || 'waiting'
}
function hasReply(m: GroupMemberInfo): boolean {
  return entryOf(m)?.lastMessageId != null
}
function avatarOf(m: GroupMemberInfo): string {
  // The member's own frozen avatar wins; older members fall back to the agent.
  return m.avatar || getAgentAvatar(m.agentId) || ''
}

/** The model this member actually runs (its own override, else the agent's
 *  default). Same resolution as the group settings roster line, so the two
 *  surfaces can never name different models for the same member. */
function modelLabel(m: GroupMemberInfo): string {
  const id = m.model || getDefaultModelId(m.agentId)
  if (!id) return ''
  return getAgentModel(m.agentId, id)?.name || formatModelIdForDisplay(id)
}

/** Whether the row has any extra identity detail (real agent name and/or model).
 *  When it has none, the meta slot is not rendered at all — an empty gap would
 *  read as a missing value. */
function hasMeta(m: GroupMemberInfo): boolean {
  return (!!m.agentName && m.agentName !== m.name) || !!modelLabel(m)
}

// Host first, like the header stack: the host is the group's router, so it
// must not be buried mid-list. Everyone else keeps the roster order.
const orderedMembers = computed(() => {
  const host = props.members.find(m => m.isHost)
  if (!host) return props.members
  return [host, ...props.members.filter(m => m !== host)]
})

const speakingMembers = computed(() => orderedMembers.value.filter(m => statusOf(m) === 'speaking'))
const chipAvatars = computed(() => speakingMembers.value.slice(0, 3))

const totalCount = computed(() => props.members.filter(m => !m.disabled).length)
const doneCount = computed(() => props.members.filter(m => statusOf(m) === 'done').length)
const hasAnyReply = computed(() => statuses.value.some(s => s.lastMessageId != null))

/** Timestamp of the newest assistant reply in the session (idle summary). */
const latestReplyAt = computed(() => {
  for (let i = props.messages.length - 1; i >= 0; i--) {
    const m = props.messages[i]
    if (m.role === 'assistant' && m.createdAt) return m.createdAt
  }
  return ''
})
/** Rendered "last reply …" label. `formatRelativeTime` returns '' for a
 *  zero/invalid timestamp, which must not leave the sentence half-finished. */
const latestReplyLabel = computed(() =>
  latestReplyAt.value ? formatRelativeTime(latestReplyAt.value) : '',
)

const progressText = computed(() => {
  if (props.running) {
    return t('group.memberPanel.progress', { done: doneCount.value, total: totalCount.value })
  }
  return hasAnyReply.value ? t('group.memberPanel.roundEnded') : ''
})

const chipText = computed(() => {
  if (props.running) {
    const speakers = speakingMembers.value
    if (speakers.length === 0) return t('group.memberPanel.waitingNext')
    if (speakers.length === 1) return t('group.memberPanel.speakingOne', { name: speakers[0].name })
    if (speakers.length === 2) {
      return t('group.memberPanel.speakingTwo', {
        names: speakers.map(m => m.name).join(t('common.listSeparator')),
      })
    }
    return t('group.memberPanel.speakingMany', { count: speakers.length })
  }
  // Idle: the roster count includes disabled rows (they are still members);
  // the progress denominator does not, because they cannot answer.
  return latestReplyLabel.value
    ? t('group.memberPanel.idleWithTime', {
        count: props.members.length,
        time: latestReplyLabel.value,
      })
    : t('group.memberPanel.idle', { count: props.members.length })
})

function statusLabel(m: GroupMemberInfo): string {
  return t(`group.memberPanel.status.${statusOf(m)}`)
}

function timeLabel(m: GroupMemberInfo): string {
  const at = entryOf(m)?.lastMessageAt
  const label = at ? formatRelativeTime(at) : ''
  return label || t('group.memberPanel.noReply')
}

function jumpTitle(m: GroupMemberInfo): string {
  return hasReply(m)
    ? t('group.memberPanel.jump', { name: m.name })
    : t('group.memberPanel.noReply')
}

/** Only a speaking NON-host member can be stopped on its own. */
function canStop(m: GroupMemberInfo): boolean {
  if (!props.running || statusOf(m) !== 'speaking') return false
  if (m.isHost) return false
  return !props.hostMemberId || String(m.id) !== String(props.hostMemberId)
}

function jump(m: GroupMemberInfo) {
  const id = entryOf(m)?.lastMessageId
  if (id == null) return
  emit('jump-message', id)
}
</script>

<style scoped>
.member-panel {
  width: auto;
  /* --chat-content-card-gutter: centres the card once the pane is wider than
     the chat measure, and keeps the phone's original 10px gutter. */
  margin: 0 var(--chat-content-card-gutter) var(--space-4);
  /* Size container: the identity detail (real agent name + model) yields when
     the CARD is narrow, not when the viewport is — the same approach the team
     roster uses with its `team-roster` container. */
  container-type: inline-size;
  container-name: member-panel;
}

/* ── Collapsed chip ── */
.member-chip {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-5);
  border-radius: var(--radius-lg);
  background: var(--bg-tertiary, #e9ecef);
  border: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  outline: none;
  transition: border-color var(--duration-base) ease;
}
.member-chip:hover {
  border-color: color-mix(in srgb, var(--accent-color) 45%, var(--border-color));
}
.member-chip:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: calc(2 * var(--dp));
}

/* Speaking avatars. The ring uses the chip's own fill so overlapping discs read
   as separate, exactly like the header stack does with its background. */
.member-chip__avatars {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}
.member-chip__avatar {
  width: calc(18 * var(--dp));
  height: calc(18 * var(--dp));
  border-radius: var(--radius-full);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-secondary, #f8f9fa);
  box-shadow: 0 0 0 calc(1.5 * var(--dp)) var(--bg-tertiary, #e9ecef);
  margin-left: calc(-5 * var(--dp));
}
.member-chip__avatar:first-child { margin-left: 0; }

.member-chip__text {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #495057);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.member-chip__count {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted, #6c757d);
  white-space: nowrap;
}
.member-chip__toggle {
  color: var(--text-muted, #6c757d);
  flex-shrink: 0;
}

/* ── Expanded roster ── */
.member-expanded {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm);
  padding: var(--space-4) var(--space-6);
}
.member-expanded__header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-2);
  cursor: pointer;
  outline: none;
}
.member-expanded__header:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: calc(2 * var(--dp));
  border-radius: var(--radius-xs);
}
.member-expanded__title {
  flex: 1;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #212529);
}
.member-expanded__count {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #6c757d);
  white-space: nowrap;
}
.member-expanded__toggle {
  color: var(--text-muted, #6c757d);
  flex-shrink: 0;
}

.member-list {
  list-style: none;
  margin: 0;
  padding: 0 var(--space-3) 0 0;
  display: flex;
  flex-direction: column;
  /* A little breathing room between rows — they used to sit flush against each
     other and read as one block. */
  row-gap: var(--space-1);
  max-height: calc(240 * var(--dp));
  overflow-y: auto;
}

.member-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
}
.member-row:hover { background: var(--bg-tertiary, #e9ecef); }
.member-row--speaking { background: color-mix(in srgb, var(--accent-color) 8%, transparent); }
.member-row--speaking:hover { background: color-mix(in srgb, var(--accent-color) 13%, transparent); }
.member-row--disabled { opacity: var(--opacity-disabled); }

.member-row__main {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  cursor: pointer;
  outline: none;
}
.member-row__main:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: calc(2 * var(--dp));
  border-radius: var(--radius-xs);
}

.member-row__avatar {
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  border-radius: var(--radius-full);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-tertiary, #e9ecef);
  flex-shrink: 0;
}
.member-row--speaking .member-row__avatar {
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--accent-color, #0066cc);
}

/* AgentIcon defaults to a 20%-rounded square; the disc crops it to a circle. */
.member-chip__avatar :deep(.agent-icon-img),
.member-chip__avatar :deep(.agent-icon-svg),
.member-chip__avatar :deep(.agent-icon-initial),
.member-row__avatar :deep(.agent-icon-img),
.member-row__avatar :deep(.agent-icon-svg),
.member-row__avatar :deep(.agent-icon-initial) {
  width: 100%;
  height: 100%;
  border-radius: var(--radius-full);
}
.member-chip__avatar :deep(.agent-icon-img),
.member-row__avatar :deep(.agent-icon-img) { object-fit: cover; }

/* Name + (real agent name · model). The identity block absorbs the leftover
   width; its children keep their natural size and only the name/model ellipsize. */
.member-row__identity {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.member-row__meta {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}
.member-row__model {
  min-width: 0;
  font-size: var(--font-size-2xs);
  color: var(--text-muted, #6c757d);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Narrow card: drop the extra identity detail entirely so the name and the
   status pill keep their room. 28rem = 420 design px of CARD width; a phone
   card is ~370, a desktop pane is ~860. */
@container member-panel (max-width: 28rem) {
  .member-row__meta { display: none; }
}

.member-row__name {
  flex: 0 1 auto;
  min-width: 0;
  font-size: var(--font-size-sm);
  color: var(--text-primary, #212529);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.member-row__host {
  flex-shrink: 0;
  color: var(--accent-color, #0066cc);
}

.member-row__pill {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  line-height: calc(16 * var(--dp));
  padding: 0 var(--space-3);
  border-radius: var(--radius-full);
  white-space: nowrap;
}
.member-row__pill.is-speaking {
  background: color-mix(in srgb, var(--accent-color) 16%, transparent);
  color: var(--accent-color);
}
.member-row__pill.is-done {
  background: color-mix(in srgb, var(--color-success) 16%, transparent);
  color: var(--color-success);
}
.member-row__pill.is-stopped {
  background: color-mix(in srgb, var(--color-orange) 16%, transparent);
  color: var(--color-orange);
}
.member-row__pill.is-waiting { color: var(--text-muted, #6c757d); }
.member-row__pill.is-disabled {
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-muted, #6c757d);
}

.member-row__time {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  color: var(--text-muted, #6c757d);
  white-space: nowrap;
}

/* One trailing slot: stop while it can be stopped, otherwise the jump hint.
   Fixed width keeps the status pills aligned across rows. */
.member-row__stop {
  flex-shrink: 0;
  width: calc(20 * var(--dp));
  height: calc(20 * var(--dp));
  border: none;
  padding: 0;
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--color-red) 14%, transparent);
  color: var(--color-red);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background var(--duration-fast) ease;
}
.member-row__stop:hover {
  background: color-mix(in srgb, var(--color-red) 30%, transparent);
}
.member-row__jump {
  flex-shrink: 0;
  width: calc(20 * var(--dp));
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #6c757d);
  opacity: 0.6;
  transition: opacity var(--duration-fast) ease, color var(--duration-fast) ease;
}
.member-row:hover .member-row__jump {
  opacity: 1;
  color: var(--accent-color, #0066cc);
}

.member-expanded__manage {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  margin-top: var(--space-2);
  padding: var(--space-3) 0 0;
  border: none;
  border-top: 1px solid var(--border-color, #dee2e6);
  background: transparent;
  color: var(--text-secondary, #495057);
  font-size: var(--font-size-xs);
  cursor: pointer;
}
.member-expanded__manage:hover { color: var(--accent-color, #0066cc); }
</style>
