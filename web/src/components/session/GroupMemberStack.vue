<template>
  <AvatarStack
    v-if="members.length"
    class="group-member-stack"
    :members="members"
    size="sm"
    :max="max"
    :tooltip="tooltip"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AvatarStack from '@/components/common/AvatarStack.vue'

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

// The whole stack carries one tooltip listing every member (including any past
// the cap), so the hidden ones are still discoverable on hover.
const tooltip = computed(() => {
  const names = props.members.map(m => m.name).join(', ')
  return `${t('group.members')}: ${names}`
})
</script>
