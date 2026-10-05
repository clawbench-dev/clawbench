<template>
  <span v-if="shown.length" class="group-member-stack" :title="tooltip">
    <span
      v-for="(m, i) in shown"
      :key="m.id"
      class="stack-disc"
      :style="{ zIndex: shown.length - i }"
    >
      <AgentIcon :backend="m.backend" :name="m.name" :avatar="getAgentAvatar(m.agentId)" size="sm" />
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { getAgentAvatar } from '@/composables/useAgents'

/** A compact preview of one active group member (see model.GroupMemberPreview). */
export interface GroupMemberPreview {
  id: string
  agentId: string
  name: string
  backend: string
}

const props = withDefaults(defineProps<{
  members: GroupMemberPreview[]
  /** Max discs shown; members past it are omitted entirely (no overflow badge). */
  max?: number
}>(), {
  max: 4,
})

const { t } = useI18n()

// Show at most `max` discs. The FIRST member is rendered foremost (see the
// inline z-index above) and each later one tucks behind the previous, peeking
// out to the right. Members past the cap are simply not rendered — there is no
// "+N" badge.
const shown = computed(() => props.members.slice(0, props.max))

// The whole stack carries one tooltip listing every member (including any past
// the cap), so the hidden ones are still discoverable on hover.
const tooltip = computed(() => {
  const names = props.members.map(m => m.name).join(', ')
  return `${t('group.members')}: ${names}`
})
</script>

<style scoped>
.group-member-stack {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
}

.stack-disc {
  /* 14px + a 1.5px ring = 17px, which fits the meta line's 18px box without
     being clipped by its overflow:hidden. */
  width: 14px;
  height: 14px;
  border-radius: var(--radius-full);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-tertiary);
  /* Ring separates overlapping discs. --border-color (NOT --bg-secondary): in
     dark themes --bg-secondary is nearly identical to the disc background, so
     the ring vanishes and the discs merge. Matches GroupAvatarStack.vue. */
  box-shadow: 0 0 0 1.5px var(--border-color);
  /* Each disc after the first slides LEFT under the previous one. z-index is set
     inline (first = highest) so the FIRST member stays fully visible and the
     rest are tucked behind it — the reverse of plain DOM-order stacking. */
  margin-left: -6px;
  position: relative;
}
.stack-disc:first-child { margin-left: 0; }

/* AgentIcon defaults to a 20%-rounded square; the disc crops it to a circle. */
.stack-disc :deep(.agent-icon-img),
.stack-disc :deep(.agent-icon-svg),
.stack-disc :deep(.agent-icon-initial) {
  width: 100%;
  height: 100%;
  border-radius: var(--radius-full);
}
.stack-disc :deep(.agent-icon-img) { object-fit: cover; }
</style>
