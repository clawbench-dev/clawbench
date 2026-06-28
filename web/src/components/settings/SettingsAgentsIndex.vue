<template>
  <div class="settings-agents-index">
    <!-- Rescan button -->
    <div class="settings-agents-index__rescan-row">
      <button
        class="settings-agents-index__rescan-btn"
        :disabled="rescanning"
        @click="handleRescan"
      >
        <RefreshCw :size="16" :class="{ 'spin': rescanning }" />
        <span>{{ rescanning ? t('settings.items.agentRescanning') : t('settings.items.agentRescan') }}</span>
      </button>
    </div>
    <div
      v-for="agent in agentList"
      :key="agent.id"
      class="settings-agents-index__row"
      @click="$emit('navigate', `agents:${agent.id}`)"
    >
      <div class="settings-agents-index__left">
        <span class="settings-agents-index__icon">{{ agent.icon }}</span>
        <div class="settings-agents-index__text">
          <span class="settings-agents-index__name">{{ agent.name }}</span>
          <span v-if="agent.specialty" class="settings-agents-index__specialty">{{ agent.specialty }}</span>
        </div>
      </div>
      <div class="settings-agents-index__actions">
        <button
          class="settings-agents-index__icon-btn"
          :title="t('settings.items.agentCopy')"
          @click.stop="startCopy(agent)"
        >
          <Copy :size="16" />
        </button>
        <button
          class="settings-agents-index__icon-btn settings-agents-index__icon-btn--danger"
          :title="t('settings.items.agentDelete')"
          @click.stop="handleDelete(agent)"
        >
          <Trash2 :size="16" />
        </button>
        <ChevronRight class="settings-agents-index__arrow" :size="18" />
      </div>
    </div>
    <div v-if="agentList.length === 0" class="settings-agents-index__empty">
      {{ t('settings.items.agentNoAgents') }}
    </div>
    <CopyAgentDialog
      v-if="copyingAgent"
      :source-name="copyingAgent.name"
      @close="copyingAgent = null"
      @confirmed="handleCopyConfirmed"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ChevronRight, Copy, Trash2, RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useAgents } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import CopyAgentDialog from './CopyAgentDialog.vue'

defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()
const { agents, defaultAgentId, loadAgents, duplicateAgent, deleteAgent, rescanAgents } = useAgents()

onMounted(() => {
  loadAgents(true)
})

const agentList = computed(() =>
  [...agents.value].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0))
)

const copyingAgent = ref<{ id: string; name: string } | null>(null)
const rescanning = ref(false)

function startCopy(agent: { id: string; name: string }) {
  copyingAgent.value = { id: agent.id, name: agent.name }
}

async function handleCopyConfirmed(newName: string) {
  if (!copyingAgent.value) return
  const sourceId = copyingAgent.value.id
  copyingAgent.value = null
  try {
    await duplicateAgent(sourceId, newName)
    toast.show(t('settings.items.agentCopied'), { icon: '✓', type: 'success', duration: 3000 })
  } catch {
    toast.show(t('settings.items.agentCopyFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

async function handleDelete(agent: { id: string; name: string }) {
  if (agent.id === defaultAgentId.value) {
    toast.show(t('settings.items.agentDeleteDefault'), { icon: '⚠️', type: 'error', duration: 3000 })
    return
  }
  const confirmed = await dialog.confirm(
    t('settings.items.agentDeleteConfirm', { name: agent.name }),
    { title: t('settings.items.agentDelete'), dangerous: true }
  )
  if (!confirmed) return
  try {
    await deleteAgent(agent.id)
    toast.show(t('settings.items.agentDeleted'), { icon: '✓', type: 'success', duration: 3000 })
  } catch {
    toast.show(t('settings.items.agentDeleteFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

async function handleRescan() {
  rescanning.value = true
  try {
    await rescanAgents()
    toast.show(t('settings.items.agentRescanSuccess'), { icon: '✓', type: 'success', duration: 3000 })
  } catch {
    toast.show(t('settings.items.agentRescanFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  } finally {
    rescanning.value = false
  }
}
</script>

<style scoped>
.settings-agents-index {
  padding: 8px 0;
  background: var(--bg-secondary);
  min-height: 100%;
}

.settings-agents-index__rescan-row {
  padding: 8px 16px;
  display: flex;
  justify-content: flex-end;
}

.settings-agents-index__rescan-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border: none;
  border-radius: 8px;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
  font-weight: 500;
}

.settings-agents-index__rescan-btn:hover:not(:disabled) {
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.settings-agents-index__rescan-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.settings-agents-index__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 48px;
  padding: 8px 16px;
  cursor: pointer;
  gap: 12px;
  background: var(--bg-primary);
  position: relative;
}

.settings-agents-index__row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 48px;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  .settings-agents-index__row:hover {
    background: var(--bg-secondary);
  }
}

.settings-agents-index__row:active {
  background: var(--bg-tertiary);
}

.settings-agents-index__left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  flex: 1;
}

.settings-agents-index__icon {
  flex-shrink: 0;
  font-size: 20px;
  line-height: 1;
}

.settings-agents-index__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.settings-agents-index__name {
  font-size: 15px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-agents-index__specialty {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-agents-index__actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.settings-agents-index__icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 0;
}

.settings-agents-index__icon-btn:hover {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.settings-agents-index__icon-btn:active {
  background: var(--bg-secondary);
}

.settings-agents-index__icon-btn--danger:hover {
  color: #e74c3c;
}

.settings-agents-index__arrow {
  flex-shrink: 0;
  color: var(--text-muted);
  margin-left: 4px;
}

.settings-agents-index__empty {
  padding: 24px 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
