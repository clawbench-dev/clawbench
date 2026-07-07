<template>
  <!-- Agent config sub-routes -->
  <SettingsAgentsIndex
    v-if="categoryId === 'agents'"
    @navigate="(id: string) => $emit('navigate', id)"
  />
  <SettingsAgentDetail
    v-else-if="categoryId.startsWith('agents:')"
    :agent-id="categoryId.slice(7)"
    @deleted="$emit('navigate', 'agents')"
  />
  <!-- Standard settings category -->
  <div v-else class="settings-category">
    <SettingsItem
      v-for="(item, index) in items"
      :key="item.key"
      :label="item.label"
      :description="item.description"
      :type="item.type"
      :model-value="getItemValue(item)"
      :options="item.options"
      :min="item.min"
      :max="item.max"
      :step="item.step"
      :needs-restart="item.needsRestart"
      :force-close="activeKey !== null && activeKey !== item.key"
      :no-divider="isLastInSection(items, index)"
      @update:model-value="(v: any) => handleUpdate(item, v)"
      @click="handleClick(item)"
      @edit-toggle="(open: boolean) => handleEditToggle(item.key, open)"
      @desc-toggle="(open: boolean) => handleDescToggle(item.key, open)"
      @discard="handleDiscard"
    />
    <!-- Password change dialog -->
    <PasswordChangeDialog
      v-if="showPasswordDialog"
      @close="showPasswordDialog = false"
      @changed="handlePasswordChanged"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingsItem from './SettingsItem.vue'
import PasswordChangeDialog from './PasswordChangeDialog.vue'
import SettingsAgentsIndex from './SettingsAgentsIndex.vue'
import SettingsAgentDetail from './SettingsAgentDetail.vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useAgents } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useAppMode } from '@/composables/useAppMode'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { categoryItems, engineVoiceOptions, type ItemSpec, type DependsOn } from './settingsFieldMap'

const props = defineProps<{
  categoryId: string
}>()

const emit = defineEmits<{
  navigate: [categoryId: string]
  restartNeeded: [changedFields: string[]]
  restartRequested: []
}>()

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()
const { localConfig, serverConfig, setLocalConfig, getServerValueWithDefault, setServerValue } = useSettingsConfig()
const { agents, loadAgents } = useAgents()
const { isAppMode } = useAppMode()
const { pushRegistered } = useGlobalEvents()

const activeKey = ref<string | null>(null)
const showPasswordDialog = ref(false)

// Load agents when chat or agents category is shown
watch(() => props.categoryId, (id) => {
  if (id === 'chat' || id === 'agents' || id.startsWith('agents:')) loadAgents(true)
}, { immediate: true })

function resolveConfigValue(key: string): any {
  if (key in localConfig) return localConfig[key]
  return getServerValueWithDefault(key)
}

function isSingleDependsOnMet(dep: DependsOn): boolean {
  const currentValue = resolveConfigValue(dep.key)
  if ('value' in dep) return currentValue === dep.value
  return dep.values!.includes(currentValue)
}

function isDependsOnMet(dependsOn: ItemSpec['dependsOn']): boolean {
  if (!dependsOn) return true
  if (Array.isArray(dependsOn)) return dependsOn.every(isSingleDependsOnMet)
  return isSingleDependsOnMet(dependsOn)
}

// categoryItems is imported from the shared settingsFieldMap module
// (single source of truth — also used by SettingsRestartDialog for field translation)

// Resolve i18n labels at runtime, and dynamically inject agent options
const items = computed(() => {
  const raw = categoryItems[props.categoryId] ?? []
  // Filter by dependsOn and inject header pseudo-items
  const expanded: any[] = []
  for (const item of raw) {
    if (!isDependsOnMet(item.dependsOn)) continue
    // Hide appVersion row when not in Android App mode (no AndroidNative bridge)
    if (item.key === 'appVersion' && !isAppMode.value) continue
    // Inject push registration status as a standalone info row at the top of push category
    if (item.key === 'push.jpush.enabled') {
      expanded.push({
        key: 'push-registration-status',
        label: t('settings.items.pushStatus'),
        labelKey: 'settings.items.pushStatus',
        type: 'info' as const,
        source: 'local' as const,
        modelValue: pushRegistered.value ? t('settings.items.pushStatusRegistered') : t('settings.items.pushStatusNotRegistered'),
      })
    }
    if (item.sectionHeader) {
      expanded.push({
        key: `header-${item.key}`,
        label: t(item.sectionHeader),
        labelKey: item.sectionHeader,
        type: 'header' as const,
        source: 'local' as const,
      })
    }
    expanded.push(item)
  }
  return expanded.map(item => {
    // Dynamically build options for default_agent from the agents list
    let resolvedOptions = item.options
    if (item.key === 'default_agent') {
      resolvedOptions = agents.value.map(a => ({
        labelKey: '',
        value: a.id,
        label: `${a.icon} ${a.name}`,
      }))
    }
    // Dynamically build options for tts.voice based on current engine
    if (item.key === 'tts.voice') {
      const engine = resolveConfigValue('tts.engine') || 'edge'
      resolvedOptions = engineVoiceOptions[engine] ?? []
    }

    return {
      ...item,
      label: item.label || t(item.labelKey),
      description: item.descriptionKey ? t(item.descriptionKey) : '',
      options: resolvedOptions?.map((opt: any) => ({
        ...opt,
        label: opt.label || resolveOptionLabel(item.key, opt),
      })),
    }
  })
})

function resolveOptionLabel(_itemKey: string, opt: { labelKey: string; value: any }): string {
  // All select options should have labelKey set to the i18n key
  if (opt.labelKey) return t(opt.labelKey)
  return String(opt.value)
}

function getItemValue(item: any): any {
  // Header pseudo-items have no value
  if (item.type === 'header') return undefined
  // Dynamically injected items with explicit modelValue (e.g. push-registration-status)
  if (item.modelValue !== undefined && item.source === 'local' && item.type === 'info') {
    return item.modelValue
  }
  // Version info items
  if (item.key === 'serverVersion') {
    return serverConfig.value?.version ?? '-'
  }
  if (item.key === 'appVersion') {
    try {
      const native = (window as any).AndroidNative
      if (native?.getAppVersion) return native.getAppVersion() ?? '-'
    } catch { /* not in app mode */ }
    return '-'
  }
  // Port forward port: 0 means auto-assign
  if (item.key === 'port_forward.port') {
    const val = getServerValueWithDefault(item.key)
    return val === 0 ? t('settings.items.portForwardPortAuto') : val
  }
  if (item.source === 'local') {
    return localConfig[item.key]
  }
  return getServerValueWithDefault(item.key)
}

async function handleUpdate(item: any, value: any) {
  // Password type: skip if empty or still masked (contains bullet chars)
  if (item.type === 'password') {
    if (!value || value.includes('•')) return
  }
  // Confirm before disabling localhost auth exempt (CLI tools will be affected)
  if (item.key === 'localhost_auth_exempt' && value === false) {
    const confirmed = await dialog.confirm(
      t('settings.items.localhostAuthExemptConfirm'),
      { title: t('settings.items.localhostAuthExempt'), dangerous: true }
    )
    if (!confirmed) return
  }
  if (item.source === 'local') {
    setLocalConfig(item.key, value)
    // Bridge androidLogCapture switch to Android native AppLog
    if (item.key === 'androidLogCapture') {
      try {
        if (value) {
          ;(window as any).AndroidNative?.startLogCapture?.()
        } else {
          ;(window as any).AndroidNative?.stopLogCapture?.()
        }
      } catch { /* not in app mode */ }
    }
    return
  }
  // Server config: auto-save immediately
  try {
    const result = await setServerValue(item.key, value)
    if (result.needsRestart && result.changedColdFields.length > 0) {
      emit('restartNeeded', result.changedColdFields)
    }
  } catch {
    toast.show(t('settings.saveFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

function handleClick(item: any) {
  if (item.key === 'reconfigureServer') {
    try {
      ;(window as any).AndroidNative?.showServerDialog?.()
    } catch { /* not in app mode */ }
  }
  if (item.key === 'changePassword') {
    showPasswordDialog.value = true
  }
  if (item.key === 'restartServer') {
    handleRestartServer()
  }
}

async function handleRestartServer() {
  const confirmed = await dialog.confirm(
    t('settings.items.restartServerConfirm'),
    { title: t('settings.items.restartServer'), dangerous: true }
  )
  if (confirmed) {
    emit('restartRequested')
  }
}

function handlePasswordChanged(needsRestart: boolean) {
  showPasswordDialog.value = false
  toast.show(t('settings.passwordChanged'), { icon: '✓', type: 'success', duration: 3000 })
  if (needsRestart) {
    emit('restartNeeded', ['password'])
  }
}

function handleEditToggle(key: string, open: boolean) {
  if (open) {
    activeKey.value = key
  } else if (activeKey.value === key) {
    activeKey.value = null
  }
}

function handleDescToggle(key: string, open: boolean) {
  if (open) {
    activeKey.value = key
  } else if (activeKey.value === key) {
    activeKey.value = null
  }
}

function handleDiscard() {
  toast.show(t('settings.passwordDiscarded'), { icon: 'ℹ️', type: 'info', duration: 3000 })
}

/** Check if item at index is the last in its section (last item overall, or next item is a header). */
function isLastInSection(items: any[], index: number): boolean {
  if (index >= items.length - 1) return true
  const nextItem = items[index + 1]
  return nextItem?.type === 'header'
}
</script>

<style scoped>
.settings-category {
  padding: 8px 0;
  background: var(--bg-secondary);
  min-height: 100%;
}
</style>
