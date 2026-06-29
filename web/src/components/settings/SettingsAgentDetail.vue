<template>
  <div class="settings-agent-detail">
    <SettingsItem
      v-for="(item, index) in items"
      :key="item.key"
      :label="item.label"
      :description="item.description"
      :type="item.type"
      :model-value="getItemValue(item)"
      :options="item.options"
      :warning="item.warning"
      :force-close="activeKey !== null && activeKey !== item.key"
      :no-divider="isLastInSection(items, index)"
      @update:model-value="(v: any) => handleUpdate(item, v)"
      @edit-toggle="(open: boolean) => handleEditToggle(item.key, open)"
    />
    <!-- Delete agent -->
    <div class="settings-agent-detail__delete-row" @click="handleDelete">
      <Trash2 :size="16" class="settings-agent-detail__delete-icon" />
      <span class="settings-agent-detail__delete-label">{{ t('settings.items.agentDelete') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { Trash2 } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import SettingsItem from './SettingsItem.vue'
import { useAgents } from '@/composables/useAgents'
import { patchAgentField } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { apiGet } from '@/utils/api'

const props = defineProps<{
  agentId: string
}>()

const emit = defineEmits<{
  deleted: []
}>()

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()
const { loadAgents, getAgent, deleteAgent, defaultAgentId } = useAgents()

const activeKey = ref<string | null>(null)
const commonPrompt = ref('')
const commonPromptLoaded = ref(false)

onMounted(() => {
  loadAgents(true)
})

// Lazy-load common prompt (only needed for system prompt editing)
async function loadCommonPrompt() {
  if (commonPromptLoaded.value) return
  try {
    const res = await apiGet<{ commonPrompt: string }>('/api/agents/common-prompt')
    commonPrompt.value = res.commonPrompt || ''
    commonPromptLoaded.value = true
  } catch {
    // Non-critical — system prompt editing will still work with raw value
    commonPromptLoaded.value = true
  }
}

const agent = computed(() => getAgent(props.agentId))

// Determine if agent is ACP-only (has acpCommand and no CLI model discovery)
const isACPOnly = computed(() => {
  const a = agent.value
  if (!a) return false
  return a.acpCommand && !a.canRefreshModels
})

interface AgentItem {
  key: string
  label: string
  description?: string
  type: 'switch' | 'select' | 'number' | 'text' | 'slider' | 'action' | 'info' | 'header' | 'password' | 'textarea'
  options?: { label: string; value: any }[]
  warning?: string
  value?: any
  patchField?: string
}

const items = computed<AgentItem[]>(() => {
  const a = agent.value
  if (!a) return []

  const result: AgentItem[] = []

  // -- Preference section --
  // Preferred Model
  if (a.models && a.models.length > 0) {
    result.push({
      key: 'preferred_model',
      label: t('settings.items.agentPreferredModel'),
      type: 'select',
      options: a.models.map((m: any) => ({ label: m.name || m.id, value: m.id })),
      patchField: 'preferred_model',
    })
  }

  // Preferred Thinking Effort
  if (a.thinkingEffortLevels && a.thinkingEffortLevels.length > 0) {
    result.push({
      key: 'preferred_thinking_effort',
      label: t('settings.items.agentPreferredThinkingEffort'),
      type: 'select',
      options: a.thinkingEffortLevels.map((l: string) => ({ label: l, value: l })),
      patchField: 'preferred_thinking_effort',
    })
  }

  // Transport (only for dual-transport agents)
  if (a.acpCommand) {
    result.push({
      key: 'transport',
      label: t('settings.items.agentTransport'),
      type: 'select',
      options: [
        { label: 'CLI', value: 'cli' },
        { label: 'ACP-stdio', value: 'acp-stdio' },
      ],
      patchField: 'transport',
    })
  }

  // -- Identity section --
  result.push({ key: 'header-identity', label: t('settings.items.agentSectionIdentity'), type: 'header' })

  result.push({
    key: 'name',
    label: t('settings.items.agentName'),
    type: 'text',
    patchField: 'name',
  })

  result.push({
    key: 'icon',
    label: t('settings.items.agentIcon'),
    type: 'text',
    patchField: 'icon',
  })

  result.push({
    key: 'specialty',
    label: t('settings.items.agentSpecialty'),
    type: 'text',
    patchField: 'specialty',
  })

  // -- Advanced section --
  result.push({ key: 'header-advanced', label: t('settings.items.agentSectionAdvanced'), type: 'header' })

  // System Prompt: editable for CLI/dual agents, read-only info for ACP-only
  if (isACPOnly.value) {
    result.push({
      key: 'custom_system_prompt',
      label: t('settings.items.agentSystemPrompt'),
      description: t('settings.items.agentSystemPromptACPNote'),
      type: 'info',
    })
  } else {
    result.push({
      key: 'custom_system_prompt',
      label: t('settings.items.agentSystemPrompt'),
      description: t('settings.items.agentSystemPromptDesc'),
      type: 'textarea',
      warning: t('settings.items.agentSystemPromptWarning'),
      patchField: 'custom_system_prompt',
    })
  }

  // -- Information section --
  result.push({ key: 'header-info', label: t('settings.items.agentSectionInfo'), type: 'header' })

  result.push({
    key: 'backend',
    label: t('settings.items.agentBackend'),
    type: 'info',
  })

  if (a.command) {
    result.push({
      key: 'command',
      label: t('settings.items.agentCommand'),
      type: 'info',
    })
  }

  result.push({
    key: 'source',
    label: t('settings.items.agentSource'),
    type: 'info',
  })

  const modelCount = a.models?.length ?? 0
  result.push({
    key: 'models_count',
    label: t('settings.items.agentModels'),
    type: 'info',
    value: t('settings.items.agentModelCount', { count: modelCount }),
  })

  if (a.acpCommand) {
    result.push({
      key: 'acp_command',
      label: t('settings.items.agentAcpCommand'),
      type: 'info',
    })
  }

  return result
})

function getItemValue(item: AgentItem): any {
  if (item.type === 'header') return undefined
  if (item.value !== undefined) return item.value

  const a = agent.value
  if (!a) return ''

  switch (item.key) {
    case 'preferred_model':
      return a.preferredModel || (a.models?.length ? a.models.find((m: any) => m.default)?.id || a.models[0]?.id : '')
    case 'preferred_thinking_effort':
      return a.preferredThinkingEffort || ''
    case 'transport':
      return a.transport || 'cli'
    case 'name':
      return a.name || ''
    case 'icon':
      return a.icon || ''
    case 'specialty':
      return a.specialty || ''
    case 'custom_system_prompt':
      // Return customSystemPrompt if available, otherwise strip common prompt from systemPrompt
      if (a.customSystemPrompt !== undefined) return a.customSystemPrompt
      if (commonPrompt.value && a.systemPrompt?.startsWith(commonPrompt.value + '\n\n')) {
        return a.systemPrompt.substring(commonPrompt.value.length + 2)
      }
      if (commonPrompt.value && a.systemPrompt === commonPrompt.value) return ''
      return a.systemPrompt || ''
    case 'backend':
      return a.backend || ''
    case 'command':
      return a.command || ''
    case 'source':
      return a.source || ''
    case 'acp_command':
      return a.acpCommand || ''
    default:
      return ''
  }
}

async function handleUpdate(item: AgentItem, value: any) {
  if (!item.patchField) return

  // Lazy-load common prompt before editing system prompt
  if (item.key === 'custom_system_prompt') {
    await loadCommonPrompt()
  }

  try {
    await patchAgentField(props.agentId, item.patchField, value)
  } catch {
    toast.show(t('settings.saveFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

function handleEditToggle(key: string, open: boolean) {
  if (open) {
    activeKey.value = key
    // Lazy-load common prompt when opening system prompt editor
    if (key === 'custom_system_prompt') {
      loadCommonPrompt()
    }
  } else if (activeKey.value === key) {
    activeKey.value = null
  }
}

/** Check if item at index is the last in its section (last item overall, or next item is a header). */
function isLastInSection(items: AgentItem[], index: number): boolean {
  if (index >= items.length - 1) return true
  const nextItem = items[index + 1]
  return nextItem?.type === 'header'
}

async function handleDelete() {
  const a = agent.value
  if (!a) return
  if (a.id === defaultAgentId.value) {
    toast.show(t('settings.items.agentDeleteDefault'), { icon: '⚠️', type: 'error', duration: 3000 })
    return
  }
  const confirmed = await dialog.confirm(
    t('settings.items.agentDeleteConfirm', { name: a.name }),
    { title: t('settings.items.agentDelete'), dangerous: true }
  )
  if (!confirmed) return
  try {
    await deleteAgent(a.id)
    toast.show(t('settings.items.agentDeleted'), { icon: '✓', type: 'success', duration: 3000 })
    emit('deleted')
  } catch {
    toast.show(t('settings.items.agentDeleteFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}
</script>

<style scoped>
.settings-agent-detail {
  padding: 8px 0;
  background: var(--bg-secondary);
  min-height: 100%;
}

.settings-agent-detail__delete-row {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 48px;
  padding: 8px 16px;
  cursor: pointer;
  background: var(--bg-primary);
  color: #e74c3c;
  font-size: 15px;
  font-weight: 500;
}

@media (hover: hover) {
  .settings-agent-detail__delete-row:hover {
    background: var(--bg-secondary);
  }
}

.settings-agent-detail__delete-row:active {
  background: var(--bg-tertiary);
}

.settings-agent-detail__delete-icon {
  flex-shrink: 0;
}
</style>
