<template>
  <div
    v-if="isGroup"
    class="group-avatar-stack"
    role="button"
    tabindex="0"
    :title="t('group.addMembers')"
    @click="openSheet"
    @keydown.enter.prevent="openSheet"
    @keydown.space.prevent="openSheet"
  >
    <AvatarStack :members="stackMembers" size="md" :max="4" />
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
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AvatarStack from '@/components/common/AvatarStack.vue'
import GroupMemberSheet from './GroupMemberSheet.vue'
import type { GroupMemberInfo } from '@/composables/useGroupChat'

const props = defineProps<{
  sessionId: string
  members: GroupMemberInfo[]
  hostMemberId: string
  isGroup: boolean
}>()
defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const sheetRef = ref<InstanceType<typeof GroupMemberSheet> | null>(null)

// Map the group roster to the generic stack shape (isHost drives the ring).
const stackMembers = computed(() =>
  props.members.map(m => ({
    id: m.id,
    agentId: m.agentId,
    name: m.name,
    backend: m.backend,
    isHost: m.isHost,
  })),
)

function openSheet() {
  sheetRef.value?.open()
}
</script>

<style scoped>
/* The whole stack is the click target — no separate "+" button. */
.group-avatar-stack {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  cursor: pointer;
  outline: none;
}
.group-avatar-stack:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 2px;
  border-radius: var(--radius-full);
}
@media (hover: hover) {
  .group-avatar-stack:hover :deep(.avatar-disc) { transform: translateY(-2px); }
  .group-avatar-stack:hover :deep(.avatar-disc:hover) { z-index: 6; }
}
</style>
