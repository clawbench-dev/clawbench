<template>
  <BottomSheet :open="open" auto :title="t('group.members')" @close="close">
    <div class="group-member-sheet">
      <!-- Member roster -->
      <ul class="gm-list">
        <li
          v-for="m in orderedMembers"
          :key="m.id"
          class="gm-row"
          :class="{ 'is-left': m.left }"
        >
          <span class="gm-avatar">
            <AgentIcon :backend="m.backend" :name="m.name" :avatar="getAgentAvatar(m.agentId)" size="lg" />
          </span>
          <span class="gm-name">{{ m.name }}</span>
          <span v-if="m.isHost" class="gm-tag gm-tag--host">{{ t('group.host') }}</span>
          <span v-else-if="m.left" class="gm-tag gm-tag--left">{{ t('group.left') }}</span>
          <button
            v-else
            class="gm-remove"
            :title="t('common.remove')"
            :aria-label="t('common.remove')"
            @click="remove(m)"
          >
            <Trash2 :size="14" />
          </button>
        </li>
      </ul>

      <!-- Settings: max rounds -->
      <div class="gm-setting">
        <label class="gm-setting-label" for="group-max-rounds">{{ t('group.maxRounds') }}</label>
        <input
          id="group-max-rounds"
          v-model.number="maxRounds"
          type="number"
          min="1"
          class="gm-setting-input"
          @change="saveMaxRounds"
        />
      </div>

      <!-- Add members -->
      <button class="fbtn fbtn-primary gm-add" @click="openAdd">
        <Plus :size="14" />
        <span>{{ t('group.addMembers') }}</span>
      </button>
    </div>

    <AgentSelectorDrawer
      :open="pickerOpen"
      multiple
      :showAgentActions="false"
      :excludedAgentIds="activeAgentIds"
      :addedLabel="t('group.alreadyMember')"
      :modelValue="[]"
      :title="t('group.addMembers')"
      :confirm-label="t('group.confirm')"
      @update:open="v => (pickerOpen = v)"
      @select="onAgentsPicked"
    />
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
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
  /** The group's current maxRounds from the server (roster endpoint). */
  maxRounds: number
}>()
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const open = ref(false)
const pickerOpen = ref(false)
// Seed from the server value and keep it in sync: a hardcoded default made the
// sheet show 10 after the user had changed it (there was no read-back).
const maxRounds = ref(props.maxRounds)
watch(() => props.maxRounds, (v) => { maxRounds.value = v })

// Active members' agent ids: shown dimmed + unpickable in the add-members
// picker (the backend rejoins/no-ops, so offering them would be misleading).
const activeAgentIds = computed(() =>
  props.members.filter(m => !m.left).map(m => m.agentId),
)

// The host always leads the roster. The server orders members by created_at, so
// a host that joined later (or was removed and re-added) would otherwise appear
// mid-list, which reads as "not in charge". Everyone else keeps their incoming
// order, and the removed members stay wherever they were.
const orderedMembers = computed(() => {
  const host = props.members.find(m => m.isHost)
  if (!host) return props.members
  return [host, ...props.members.filter(m => m !== host)]
})

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
  gap: var(--space-5);
  padding: var(--space-3) var(--space-7) var(--space-7);
}

/* ── Member roster ── */
.gm-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}
.gm-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  min-height: 40px;
  padding: var(--space-2) 0;
}
.gm-row + .gm-row {
  border-top: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);
}
.gm-row.is-left .gm-avatar,
.gm-row.is-left .gm-name {
  opacity: var(--opacity-disabled, 0.4);
}
.gm-avatar {
  flex-shrink: 0;
  width: var(--icon-size-lg, 24px);
  height: var(--icon-size-lg, 24px);
  border-radius: var(--radius-full);
  overflow: hidden;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.gm-name {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-md);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Pills: host (accent) / left (muted). */
.gm-tag {
  flex-shrink: 0;
  padding: 1px var(--space-3);
  border-radius: var(--radius-full);
  font-size: var(--font-size-2xs);
  line-height: 16px;
}
.gm-tag--host {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
  color: var(--accent-color, #0066cc);
}
.gm-tag--left {
  background: var(--bg-tertiary);
  color: var(--text-muted);
}

/* Remove: icon button, muted at rest, red on hover. */
.gm-remove {
  flex-shrink: 0;
  width: 26px;
  height: 26px;
  padding: 0;
  border: none;
  background: none;
  color: var(--text-muted);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-sm);
  transition: background var(--duration-base), color var(--duration-base);
}
@media (hover: hover) {
  .gm-remove:hover {
    color: var(--color-red, #dc2626);
    background: color-mix(in srgb, var(--color-red, #dc2626) 12%, transparent);
  }
}
.gm-remove:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 1px;
}

/* ── Settings row ── */
.gm-setting {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
}
.gm-setting-label {
  font-size: var(--font-size-md);
  color: var(--text-primary);
}
.gm-setting-input {
  width: 72px;
  height: 30px;
  text-align: center;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
}
.gm-setting-input:focus {
  outline: none;
  border-color: var(--accent-color, #0066cc);
  box-shadow: 0 0 0 2px var(--focus-ring, rgba(0, 102, 204, 0.2));
}

/* ── Add button (full-width pill) ── */
.gm-add {
  width: 100%;
}
</style>
