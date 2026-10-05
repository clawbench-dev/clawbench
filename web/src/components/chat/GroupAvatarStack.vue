<template>
  <div
    v-if="isGroup"
    class="agent-stack"
    role="button"
    tabindex="0"
    :title="t('group.addMembers')"
    @click="openSheet"
    @keydown.enter.prevent="openSheet"
    @keydown.space.prevent="openSheet"
  >
    <span
      v-for="m in members"
      :key="m.id"
      class="stack-item"
      :class="{ 'is-host': m.isHost }"
      :title="m.name + (m.isHost ? ' (Host)' : '')"
    >
      <AgentIcon :backend="m.backend" :name="m.name" :avatar="getAgentAvatar(m.agentId)" size="lg" />
    </span>
    <GroupMemberSheet
      ref="sheetRef"
      :groupId="sessionId"
      :members="members"
      :hostMemberId="hostMemberId"
      @changed="$emit('changed')"
    />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AgentIcon from '@/components/common/AgentIcon.vue'
import GroupMemberSheet from './GroupMemberSheet.vue'
import { getAgentAvatar } from '@/composables/useAgents'
import type { GroupMemberInfo } from '@/composables/useGroupChat'

defineProps<{
  sessionId: string
  members: GroupMemberInfo[]
  hostMemberId: string
  isGroup: boolean
}>()
defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const sheetRef = ref<InstanceType<typeof GroupMemberSheet> | null>(null)

function openSheet() {
  sheetRef.value?.open()
}
</script>

<style scoped>
/* The whole stack is the click target — no separate "+" button. */
.agent-stack {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  cursor: pointer;
  outline: none;
}
.stack-item {
  width: var(--icon-size-lg, 24px);
  height: var(--icon-size-lg, 24px);
  padding: 0;
  border-radius: var(--radius-full);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-tertiary);
  /* Ring separates overlapping circles. --border-color (not --bg-secondary)
     because in dark themes the latter is nearly the same value as the disc
     background, so the ring vanished against a black/dark wallpaper. The ring
     is uniform for every disc; only its colour differs for the host. */
  box-shadow: 0 0 0 2px var(--border-color);
  margin-left: -8px;
  position: relative;
  transition: transform var(--duration-base) ease;
}
.stack-item:first-child { margin-left: 0; }
/* Avatar fills the disc and crops to a circle (AgentIcon defaults to a
   rounded square). This file IS scoped, so :deep is valid. */
.stack-item :deep(.agent-icon-img),
.stack-item :deep(.agent-icon-svg),
.stack-item :deep(.agent-icon-initial) {
  width: 100%;
  height: 100%;
  border-radius: var(--radius-full);
}
.stack-item :deep(.agent-icon-img) { object-fit: cover; }
/* Host keeps the accent ring but NO z-index: every disc stacks by DOM order
   (each later one over the previous), so the overlap direction is uniform. */
.stack-item.is-host {
  box-shadow: 0 0 0 2px var(--accent-color, #0066cc);
}
.agent-stack:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 2px;
  border-radius: var(--radius-full);
}
@media (hover: hover) {
  .agent-stack:hover .stack-item { transform: translateY(-2px); }
  .agent-stack:hover .stack-item:hover { z-index: 6; }
}
</style>
