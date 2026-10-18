<template>
  <BottomSheet :open="open" auto :title="member?.name || ''" @close="$emit('close')">
    <template #header>
      <AgentIcon
        v-if="member"
        :backend="member.backend"
        :name="member.name"
        :avatar="member.avatar || getAgentAvatar(member.agentId)"
        size="md"
        class="bs-header-icon"
      />
      <span class="bs-header-title">{{ member?.name || t('group.members') }}</span>
      <RealAgentNameTag
        v-if="member"
        class="mcs-real-name"
        :agent-name="member.agentName"
        :display-name="member.name"
      />
      <span v-if="member?.isHost" class="mcs-tag mcs-tag--host"><Crown :size="11" class="mcs-tag-crown" />{{ t('group.host') }}</span>
      <span v-else-if="member?.left" class="mcs-tag mcs-tag--left">{{ t('group.left') }}</span>
    </template>

    <div v-if="member" class="member-context-sheet">
      <!-- Identity block: what this member actually runs. Deliberately minimal —
           model / mode switching belongs to the agent's own config page, not
           here; this sheet answers "how full is its context". -->
      <div class="mcs-facts">
        <div class="mcs-fact">
          <span class="mcs-fact-label">{{ t('group.memberDetail.agent') }}</span>
          <span class="mcs-fact-value">{{ member.agentName || member.name }}</span>
        </div>
        <div class="mcs-fact">
          <span class="mcs-fact-label">{{ t('group.memberDetail.model') }}</span>
          <span class="mcs-fact-value">{{ modelLabel }}</span>
        </div>
      </div>

      <div class="mcs-section-title">{{ t('chat.sessionInfo.contextUsage') }}</div>
      <p v-if="!hasUsage" class="mcs-empty">{{ t('group.memberDetail.noUsage') }}</p>
      <template v-else>
        <ContextUsageDetail :usage="member.usage" :show-compact="canCompact" :compacting="compacting" @compact="onCompact" />
        <p v-if="!canCompact" class="mcs-hint">{{ t('group.memberDetail.compactUnavailable') }}</p>
        <div v-else class="mcs-compact-note">{{ t('group.memberDetail.compactHint') }}</div>
      </template>
      <p v-if="compacting" class="mcs-progress">{{ t('group.memberDetail.compacting') }}</p>
      <p v-if="compactError" class="mcs-error">{{ compactError }}</p>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Crown } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import RealAgentNameTag from '@/components/common/RealAgentNameTag.vue'
import ContextUsageDetail from '@/components/chat/ContextUsageDetail.vue'
import { getAgentAvatar, useAgents } from '@/composables/useAgents'
import { compactGroupMember, type GroupMemberInfo } from '@/composables/useGroupChat'
import { appLog } from '@/utils/appLog'

/**
 * MemberContextSheet shows ONE group member's own context window.
 *
 * Nested inside the group settings sheet (not a drill-down replacement): the
 * roster stays open behind it, so the user can compare members by closing this
 * and tapping the next row. `drawerStack` puts it above the roster and the back
 * gesture closes this one first.
 *
 * The panel body is ContextUsageDetail — the same component the single-chat
 * context popup renders — so thresholds, rows and formatting cannot drift.
 */
const props = defineProps<{
  open: boolean
  /** The selected member, or null while nothing is selected. */
  member: GroupMemberInfo | null
  groupId: string
  /** True while the group has a turn in flight; compaction is refused then. */
  groupRunning?: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  /** Emitted after a successful compaction so the parent can refresh the roster. */
  (e: 'compacted', usage: unknown): void
}>()

const { t } = useI18n()
const { getAgentDefaultModelName } = useAgents()

const compacting = ref(false)
const compactError = ref('')

// The member row's own override wins; otherwise name the agent's default so the
// row is never blank for a member that simply never picked a model.
const modelLabel = computed(() => {
  if (!props.member) return '—'
  if (props.member.model) return props.member.model
  const fallback = getAgentDefaultModelName(props.member.agentId)
  return fallback || '—'
})

// A member that never reported usage has nothing to compact either — the agent
// is on a transport that does not report windows, and compacting blind is
// pointless. Left members cannot run a turn at all.
const hasUsage = computed(() => !!props.member?.usage && (props.member.usage.size ?? 0) > 0)
const canCompact = computed(() => hasUsage.value && !props.member?.left && !props.groupRunning)

watch(() => props.member?.id, () => { compactError.value = '' })

async function onCompact() {
  if (!props.member || compacting.value) return
  compacting.value = true
  compactError.value = ''
  try {
    const usage = await compactGroupMember(props.groupId, props.member.id)
    emit('compacted', usage)
  } catch (e) {
    // The server's message is already localized (GroupRunning / CompactNeedsACP
    // / …), so surface it verbatim rather than inventing client-side wording.
    compactError.value = e instanceof Error ? e.message : String(e)
    appLog.w('Group', 'member compact failed', e)
  } finally {
    compacting.value = false
  }
}
</script>

<style scoped>
.member-context-sheet {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-7) var(--space-7);
}

.mcs-facts {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.mcs-fact {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-4);
  font-size: var(--font-size-sm);
}

.mcs-fact-label {
  color: var(--text-muted);
  flex-shrink: 0;
}

.mcs-fact-value {
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Section heading above the usage block: same treatment as the roster's setting
   rows so the sheet reads as one family. */
.mcs-section-title {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-color);
}

.mcs-empty {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: 1.4;
}

.mcs-progress,
.mcs-hint,
.mcs-compact-note {
  margin: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  line-height: 1.4;
}

.mcs-progress {
  color: var(--text-secondary);
}

.mcs-error {
  margin: 0;
  font-size: var(--font-size-xs);
  color: var(--color-red, #dc2626);
  line-height: 1.4;
}

/* Header pills — mirrors the roster's host/left tags. */
.mcs-tag {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: 1px var(--space-3);
  border-radius: var(--radius-full);
  font-size: var(--font-size-xs);
  line-height: 16px;
}

.mcs-tag-crown {
  flex-shrink: 0;
}

.mcs-tag--host {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
  color: var(--accent-color, #0066cc);
}

.mcs-tag--left {
  background: var(--bg-tertiary);
  color: var(--text-muted);
}

.mcs-real-name {
  flex-shrink: 0;
}
</style>
