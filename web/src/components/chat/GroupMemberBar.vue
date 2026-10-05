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
        <AgentIcon :backend="m.backend" :name="m.name" :avatar="getAgentAvatar(m.agentId)" :size="18" />
      </button>
    </div>
    <button class="group-member-add" :title="t('group.addMembers')" @click="openSheet">
      <Plus :size="16" />
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
.group-member-bar {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-1) var(--space-2);
  overflow-x: auto;
}
.group-member-bar-avatars {
  display: flex;
  align-items: center;
  gap: var(--space-1);
}
.group-member-chip {
  border: none;
  background: none;
  padding: 0;
  cursor: pointer;
  border-radius: var(--radius-full);
  display: flex;
}
.group-member-chip.is-host {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 1px;
  border-radius: var(--radius-full);
}
.group-member-chip.is-left {
  opacity: var(--opacity-disabled, 0.4);
}
.group-member-add {
  margin-left: auto;
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
}
</style>
