<template>
  <div v-if="isGroup" class="agent-stack">
    <button
      v-for="m in members"
      :key="m.id"
      class="stack-item"
      :class="{ 'is-host': m.isHost, 'is-left': m.left }"
      :title="m.name + (m.isHost ? ' (Host)' : '') + (m.left ? ' · ' + t('group.left') : '')"
      @click="openSheet"
    >
      <AgentIcon :backend="m.backend" :name="m.name" :avatar="getAgentAvatar(m.agentId)" size="lg" />
    </button>
    <button class="stack-add" :title="t('group.addMembers')" @click="openSheet">
      <Plus :size="14" />
    </button>
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
import { Plus } from 'lucide-vue-next'
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
.agent-stack {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
}
.stack-item {
  width: var(--icon-size-lg, 24px);
  height: var(--icon-size-lg, 24px);
  padding: 0;
  border: none;
  border-radius: var(--radius-full);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-tertiary);
  cursor: pointer;
  /* Ring separates overlapping circles; matches the bar background so it reads
     as a gap, and (unlike a border) does not change the circle's real size. */
  box-shadow: 0 0 0 2px var(--bg-secondary, #fff);
  margin-left: -8px;
  position: relative;
  transition: transform var(--duration-base) ease;
}
.stack-item:first-child { margin-left: 0; }
/* Avatar fills the disc and crops to a circle (AgentIcon defaults to a
   rounded square). Non-scoped note: this file IS scoped, so :deep is valid. */
.stack-item :deep(.agent-icon-img),
.stack-item :deep(.agent-icon-svg),
.stack-item :deep(.agent-icon-initial) {
  width: 100%;
  height: 100%;
  border-radius: var(--radius-full);
}
.stack-item :deep(.agent-icon-img) { object-fit: cover; }
.stack-item.is-host {
  box-shadow: 0 0 0 2px var(--accent-color, #0066cc);
  z-index: 1;
}
.stack-item.is-left { opacity: var(--opacity-disabled, 0.4); }

.stack-add {
  width: var(--icon-size-lg, 24px);
  height: var(--icon-size-lg, 24px);
  padding: 0;
  border: 1.5px dashed var(--border-color);
  border-radius: var(--radius-full);
  background: var(--bg-secondary, #fff);
  color: var(--accent-color, #0066cc);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  margin-left: -8px;
  position: relative;
  flex-shrink: 0;
  transition: background var(--duration-base);
}
@media (hover: hover) {
  .stack-item:hover { transform: translateY(-2px); z-index: 6; }
  .stack-add:hover { background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent); }
}
</style>
