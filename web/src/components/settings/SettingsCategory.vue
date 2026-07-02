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
    <template v-for="entry in renderList" :key="entry.type === 'group' ? entry.spec.groupId : entry.spec.key">
      <!-- Config group panel -->
      <SettingsGroupPanel
        v-if="entry.type === 'group'"
        :group="entry.spec"
        :field-values="getGroupFieldValues(entry.spec)"
        :field-options="getGroupFieldOptions(entry.spec)"
        :force-close="activeKey !== null && activeKey !== entry.spec.groupId"
        @save-result="handleGroupSaveResult"
        @expand-toggle="(open: boolean) => handleGroupExpandToggle(entry.spec.groupId, open)"
      />
      <!-- Standalone item -->
      <SettingsItem
        v-else
        :label="entry.spec.label || t(entry.spec.labelKey)"
        :description="entry.spec.descriptionKey ? t(entry.spec.descriptionKey) : ''"
        :type="entry.spec.type"
        :model-value="getItemValue(entry.spec)"
        :options="resolveItemOptions(entry.spec)"
        :min="entry.spec.min"
        :max="entry.spec.max"
        :step="entry.spec.step"
        :needs-restart="entry.spec.needsRestart"
        :force-close="activeKey !== null && activeKey !== entry.spec.key"
        :no-divider="false"
        @update:model-value="(v: any) => handleUpdate(entry.spec, v)"
        @click="handleClick(entry.spec)"
        @edit-toggle="(open: boolean) => handleEditToggle(entry.spec.key, open)"
        @desc-toggle="(open: boolean) => handleDescToggle(entry.spec.key, open)"
        @discard="handleDiscard"
      />
    </template>
    <!-- Password change dialog -->
    <PasswordChangeDialog
      v-if="showPasswordDialog"
      @close="showPasswordDialog = false"
      @changed="handlePasswordChanged"
    />
    <!-- iOS install instructions sheet -->
    <IosInstallDrawer :open="showIosSheet" @close="showIosSheet = false" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingsItem from './SettingsItem.vue'
import SettingsGroupPanel from './SettingsGroupPanel.vue'
import PasswordChangeDialog from './PasswordChangeDialog.vue'
import SettingsAgentsIndex from './SettingsAgentsIndex.vue'
import SettingsAgentDetail from './SettingsAgentDetail.vue'
import IosInstallDrawer from '@/components/common/IosInstallDrawer.vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useAgents } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useAppMode } from '@/composables/useAppMode'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { categoryItems, categoryGroups, engineVoiceOptions, fieldBelongsToGroup, type ItemSpec, type ConfigGroup, type DependsOn } from './settingsFieldMap'

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
const pwaInstall = usePwaInstall()
const { pushRegistered } = useGlobalEvents()

const activeKey = ref<string | null>(null)
const showPasswordDialog = ref(false)
const showIosSheet = ref(false)

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

// ── Render list: mixed groups + standalone items ──

interface RenderItem {
  type: 'item'
  spec: any
}
interface RenderGroup {
  type: 'group'
  spec: ConfigGroup
}

const renderList = computed(() => {
  const groups = categoryGroups[props.categoryId] ?? []
  const raw = categoryItems[props.categoryId] ?? []
  const result: (RenderItem | RenderGroup)[] = []
  const emittedGroups = new Set<string>()

  for (const item of raw) {
    if (!isDependsOnMet(item.dependsOn)) continue
    // Hide appVersion row when not in Android App mode
    if (item.key === 'appVersion' && !isAppMode.value) continue
    if (item.key === 'addToHomeScreen' && !pwaInstall.showPwaInstall.value) continue
    if (item.key === 'downloadAndroidApp' && !pwaInstall.showApkDownload.value) continue

    // Check if this item belongs to a group
    const owningGroup = groups.find(g => fieldBelongsToGroup(g, item.key))
    if (owningGroup && !emittedGroups.has(owningGroup.groupId)) {
      // Inject push registration status before the JPush group
      if (owningGroup.groupId === 'push-jpush-group') {
        result.push({
          type: 'item',
          spec: {
            key: 'push-registration-status',
            label: t('settings.items.pushStatus'),
            labelKey: 'settings.items.pushStatus',
            type: 'info' as const,
            source: 'local' as const,
            modelValue: pushRegistered.value ? t('settings.items.pushStatusRegistered') : t('settings.items.pushStatusNotRegistered'),
          },
        })
      }
      result.push({ type: 'group', spec: owningGroup })
      emittedGroups.add(owningGroup.groupId)
    } else if (!owningGroup) {
      // Standalone item (may have sectionHeader — inject header pseudo-item)
      if (item.sectionHeader) {
        result.push({
          type: 'item',
          spec: {
            key: `header-${item.key}`,
            label: t(item.sectionHeader),
            labelKey: item.sectionHeader,
            type: 'header' as const,
            source: 'local' as const,
          },
        })
      }
      result.push({ type: 'item', spec: item })
    }
  }

  // Emit any groups not yet emitted (for categories where categoryItems is empty)
  for (const g of groups) {
    if (!emittedGroups.has(g.groupId)) {
      // Inject push registration status before the JPush group
      if (g.groupId === 'push-jpush-group') {
        result.push({
          type: 'item',
          spec: {
            key: 'push-registration-status',
            label: t('settings.items.pushStatus'),
            labelKey: 'settings.items.pushStatus',
            type: 'info' as const,
            source: 'local' as const,
            modelValue: pushRegistered.value ? t('settings.items.pushStatusRegistered') : t('settings.items.pushStatusNotRegistered'),
          },
        })
      }
      result.push({ type: 'group', spec: g })
    }
  }

  return result
})

// ── Group helpers ──

/** Resolve all field values for a group from server/local config */
function getGroupFieldValues(group: ConfigGroup): Record<string, any> {
  const values: Record<string, any> = {}
  values[group.entryField.key] = resolveConfigValue(group.entryField.key)
  for (const f of group.commonFields ?? []) {
    values[f.key] = resolveConfigValue(f.key)
  }
  for (const osf of group.optionSubFields ?? []) {
    for (const f of osf.fields) {
      values[f.key] = resolveConfigValue(f.key)
    }
  }
  return values
}

/** Resolve dynamic field options for a group (e.g., tts.voice per engine) */
function getGroupFieldOptions(group: ConfigGroup): Record<string, { label: string; value: any }[]> {
  const options: Record<string, { label: string; value: any }[]> = {}
  // TTS voice options — reactive to current engine
  if (group.groupId === 'tts-group') {
    const engine = resolveConfigValue('tts.engine') || 'edge'
    const voiceOpts = engineVoiceOptions[engine] ?? []
    options['tts.voice'] = voiceOpts.map(o => ({ label: t(o.labelKey), value: o.value }))
  }
  return options
}

/** Handle group save result (needsRestart check) */
function handleGroupSaveResult(result: { needsRestart: boolean; changedColdFields: string[] }) {
  if (result.needsRestart && result.changedColdFields.length > 0) {
    emit('restartNeeded', result.changedColdFields)
  }
}

/** Handle group expand/collapse (accordion) */
function handleGroupExpandToggle(groupId: string, open: boolean) {
  if (open) {
    activeKey.value = groupId
  } else if (activeKey.value === groupId) {
    activeKey.value = null
  }
}

// ── Standalone item helpers ──

function resolveItemOptions(item: any): any {
  let resolvedOptions = item.options
  if (item.key === 'default_agent') {
    resolvedOptions = agents.value.map(a => ({
      labelKey: '',
      value: a.id,
      label: `${a.icon} ${a.name}`,
    }))
  }
  if (resolvedOptions) {
    return resolvedOptions.map((opt: any) => ({
      ...opt,
      label: opt.label || resolveOptionLabel(item.key, opt),
    }))
  }
  return undefined
}

function resolveOptionLabel(_itemKey: string, opt: { labelKey: string; value: any }): string {
  if (opt.labelKey) return t(opt.labelKey)
  return String(opt.value)
}

function getItemValue(item: any): any {
  if (item.type === 'header') return undefined
  if (item.modelValue !== undefined && item.source === 'local' && item.type === 'info') {
    return item.modelValue
  }
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
  if (item.type === 'password') {
    if (!value || value.includes('•')) return
  }
  if (item.key === 'localhost_auth_exempt' && value === false) {
    const confirmed = await dialog.confirm(
      t('settings.items.localhostAuthExemptConfirm'),
      { title: t('settings.items.localhostAuthExempt'), dangerous: true }
    )
    if (!confirmed) return
  }
  if (item.source === 'local') {
    setLocalConfig(item.key, value)
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
  try {
    const result = await setServerValue(item.key, value)
    if (item.key === 'terminal.enabled') {
      loadTerminalStatus()
    }
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
  if (item.key === 'addToHomeScreen') {
    handleAddToHomeScreen()
  }
  if (item.key === 'downloadAndroidApp') {
    window.location.href = '/api/apk'
  }
}

async function handleAddToHomeScreen() {
  if (pwaInstall.canInstallPwa.value) {
    await pwaInstall.installPwa()
  } else if (pwaInstall.isIOS.value) {
    showIosSheet.value = true
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
</script>

<style scoped>
.settings-category {
  padding: 8px 0;
  background: var(--bg-secondary);
  min-height: 100%;
}
</style>
