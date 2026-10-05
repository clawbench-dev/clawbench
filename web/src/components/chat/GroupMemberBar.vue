<template>
  <div v-if="isGroup" class="group-member-bar">
    <div class="group-member-bar-avatars">
      <button
        v-for="m in members"
        :key="m.id"
        class="group-member-chip"
        :class="{ 'is-host': m.isHost, 'is-left': m.left }"
        :title="m.name + (m.isHost ? ' (Host)' : '') + (m.left ? ' · ' + t('group.left') : '')"
        @click="openSheet"
      >
        <span class="group-member-avatar">
          <AgentIcon :backend="m.backend" :name="m.name" :avatar="getAgentAvatar(m.agentId)" size="lg" />
        </span>
      </button>
    </div>
    <button class="group-member-add" :title="t('group.addMembers')" @click="openSheet">
      <Plus :size="18" />
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
/* Capsule banner: a rounded, bordered strip pinned above the messages. */
.group-member-bar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin: var(--space-2) var(--space-2) 0;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-full);
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  overflow-x: auto;
}
.group-member-bar-avatars {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.group-member-chip {
  border: none;
  background: none;
  padding: 0;
  cursor: pointer;
  display: flex;
  flex-shrink: 0;
}
/* Fixed-size disc so the avatar always fills it exactly; the host ring is a
   border on THIS disc (box-sizing: border-box keeps the outer size stable), so
   the 30px icon fills the 30px inner box with no gap. */
.group-member-avatar {
  width: 34px;
  height: 34px;
  box-sizing: border-box;
  border: 2px solid transparent;
  border-radius: var(--radius-full);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  background: var(--bg-tertiary);
}
.group-member-avatar :deep(.agent-icon-img),
.group-member-avatar :deep(.agent-icon-svg),
.group-member-avatar :deep(.agent-icon-initial) {
  border-radius: var(--radius-full);
}
.group-member-chip.is-host .group-member-avatar {
  border-color: var(--accent-color, #0066cc);
}
.group-member-chip.is-left {
  opacity: var(--opacity-disabled, 0.4);
}
.group-member-add {
  margin-left: auto;
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  border: 1px dashed var(--border-color);
  border-radius: var(--radius-full);
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background var(--duration-base);
}
@media (hover: hover) {
  .group-member-add:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
}
</style>
