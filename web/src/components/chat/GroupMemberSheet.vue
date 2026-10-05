<template>
  <BottomSheet :open="open" auto :title="t('group.members')" @close="close">
    <div class="group-member-sheet">
      <div v-for="m in members" :key="m.id" class="group-member-row" :class="{ 'is-left': m.left }">
        <AgentIcon :backend="m.backend" :name="m.name" :avatar="getAgentAvatar(m.agentId)" size="lg" />
        <span class="group-member-name">{{ m.name }}</span>
        <span v-if="m.isHost" class="group-member-host-tag">{{ t('group.host') }}</span>
        <span v-if="m.left" class="group-member-left-tag">{{ t('group.left') }}</span>
        <button v-else-if="!m.isHost" class="group-member-remove" :title="t('common.remove')" @click="remove(m)">
          <Trash2 :size="14" />
        </button>
      </div>

      <div class="group-setting-row">
        <label class="group-setting-label" for="group-max-rounds">{{ t('group.maxRounds') }}</label>
        <input id="group-max-rounds" v-model.number="maxRounds" type="number" min="1" class="group-setting-input" @change="saveMaxRounds" />
      </div>

      <button class="group-add-members-btn" @click="openAdd">
        <Plus :size="16" />
        <span>{{ t('group.addMembers') }}</span>
      </button>
    </div>

    <AgentSelectorDrawer
      :open="pickerOpen"
      multiple
      :modelValue="[]"
      :title="t('group.addMembers')"
      :confirm-label="t('group.confirm')"
      @update:open="v => (pickerOpen = v)"
      @select="onAgentsPicked"
    />
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Trash2, Plus } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'
import { getAgentAvatar } from '@/composables/useAgents'
import { addGroupMembers, removeGroupMember, updateGroupSettings, type GroupMemberInfo } from '@/composables/useGroupChat'

const props = defineProps<{
  groupId: string
  members: GroupMemberInfo[]
  hostMemberId: string
}>()
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const open = ref(false)
const pickerOpen = ref(false)
const maxRounds = ref(10)

function openSheet() { open.value = true }
function close() { open.value = false }
function openAdd() { pickerOpen.value = true }

async function onAgentsPicked(ids: string | string[]) {
  const list = Array.isArray(ids) ? ids : [ids]
  if (list.length > 0) {
    try {
      await addGroupMembers(props.groupId, list)
      emit('changed')
    } catch { /* ignore */ }
  }
  pickerOpen.value = false
}

async function remove(m: GroupMemberInfo) {
  try {
    await removeGroupMember(props.groupId, m.id)
    emit('changed')
  } catch { /* ignore */ }
}

async function saveMaxRounds() {
  const n = Number(maxRounds.value)
  if (n > 0) {
    try { await updateGroupSettings(props.groupId, n) } catch { /* ignore */ }
  }
}

defineExpose({ open: openSheet })
</script>

<style scoped>
.group-member-sheet {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4);
}
.group-member-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.group-member-row.is-left {
  opacity: var(--opacity-disabled, 0.4);
}
.group-member-name {
  flex: 1;
  text-align: left;
}
.group-member-host-tag,
.group-member-left-tag {
  font-size: var(--font-size-2xs);
  padding: 0 var(--space-1);
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
  color: var(--accent-color, #0066cc);
}
.group-member-left-tag {
  background: var(--bg-tertiary, #eee);
  color: var(--text-muted, #999);
}
.group-member-remove {
  border: none;
  background: none;
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
}
.group-setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}
.group-setting-input {
  width: 72px;
  text-align: center;
}
.group-add-members-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-1);
  padding: var(--space-2);
  border: 1px dashed var(--border-color, #ccc);
  border-radius: var(--radius-md, 8px);
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
}
</style>
