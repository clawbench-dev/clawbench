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
    <template v-for="entry in renderList" :key="entry.key">
      <SettingsItem
        :label="getItemLabel(entry)"
        :description="entry.descriptionKey ? t(entry.descriptionKey) : ''"
        :type="entry.type"
        :model-value="getItemValue(entry)"
        :options="resolveItemOptions(entry)"
        :min="entry.min"
        :max="entry.max"
        :step="entry.step"
        :needs-restart="entry.needsRestart"
        :force-close="activeKey !== null && activeKey !== entry.key"
        :no-divider="false"
        :default-value="entry.defaultValue"
        :display-format="entry.displayFormat"
        :display-transform="entry.displayTransform"
        @update:model-value="(v: unknown) => handleUpdate(entry, v)"
        @click="handleClick(entry)"
        @edit-toggle="(open: boolean) => handleEditToggle(entry.key, open)"
        @desc-toggle="(open: boolean) => handleDescToggle(entry.key, open)"
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
import PasswordChangeDialog from './PasswordChangeDialog.vue'
import SettingsAgentsIndex from './SettingsAgentsIndex.vue'
import SettingsAgentDetail from './SettingsAgentDetail.vue'
import IosInstallDrawer from '@/components/common/IosInstallDrawer.vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useAgents } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useAppMode } from '@/composables/useAppMode'
import { startFlushTimer, stopFlushTimer } from '@/utils/appLog'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { categoryItems, type ItemSpec, type DependsOn } from './settingsFieldMap'

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
const { loadAgents } = useAgents()
const { isAppMode } = useAppMode()
const pwaInstall = usePwaInstall()
const activeKey = ref<string | null>(null)
const showPasswordDialog = ref(false)
const showIosSheet = ref(false)

// Load agents when chat or agents category is shown
watch(() => props.categoryId, (id) => {
  if (id === 'chat' || id === 'agents' || id.startsWith('agents:')) loadAgents(true)
}, { immediate: true })

function resolveConfigValue(key: string): unknown {
  if (key in localConfig) return localConfig[key]
  return getServerValueWithDefault(key)
}

function isSingleDependsOnMet(dep: DependsOn): boolean {
  const currentValue = resolveConfigValue(dep.key)
  if ('value' in dep) return currentValue === dep.value
  return dep.values!.includes(currentValue as unknown)
}

function isDependsOnMet(dependsOn: ItemSpec['dependsOn']): boolean {
  if (!dependsOn) return true
  // Array means OR: show the field if ANY of the conditions match
  if (Array.isArray(dependsOn)) return dependsOn.some(isSingleDependsOnMet)
  return isSingleDependsOnMet(dependsOn)
}

// ── Render list: standalone items with dependsOn filtering ──

const renderList = computed(() => {
  const raw = categoryItems[props.categoryId] ?? []
  const result: ItemSpec[] = []

  for (const item of raw) {
    if (!isDependsOnMet(item.dependsOn)) continue
    // Hide appOnly items when not in Android App mode
    if (item.appOnly && !isAppMode.value) continue
    // Hide appVersion row when not in Android App mode
    if (item.key === 'appVersion' && !isAppMode.value) continue
    if (item.key === 'addToHomeScreen' && !pwaInstall.showPwaInstall.value) continue
    if (item.key === 'downloadAndroidApp' && !pwaInstall.showApkDownload.value) continue

    // Inject section header pseudo-item before the field
    if (item.sectionHeader) {
      result.push({
        key: `header-${item.key}`,
        label: t(item.sectionHeader),
        labelKey: item.sectionHeader,
        type: 'header',
        source: 'local',
      } as ItemSpec & { label: string })
    }
    result.push(item)
  }

  return result
})

// ── Standalone item helpers ──

function getItemLabel(entry: ItemSpec): string {
  // Section header items have a 'label' field set at runtime
  const extended = entry as ItemSpec & { label?: string }
  return extended.label || t(entry.labelKey)
}

function resolveItemOptions(item: ItemSpec): { label: string; value: unknown }[] | undefined {
  const resolvedOptions = item.options
  if (resolvedOptions) {
    return resolvedOptions.map((opt) => ({
      ...opt,
      label: (opt as { label?: string }).label || resolveOptionLabel(item.key, opt),
    }))
  }
  return undefined
}

function resolveOptionLabel(_itemKey: string, opt: { labelKey: string; value: unknown }): string {
  if (opt.labelKey) return t(opt.labelKey)
  return String(opt.value)
}

function getItemValue(item: ItemSpec): unknown {
  if (item.type === 'header') return undefined
  if ((item as ItemSpec & { modelValue?: unknown }).modelValue !== undefined && item.source === 'local' && item.type === 'info') {
    return (item as ItemSpec & { modelValue?: unknown }).modelValue
  }
  if (item.key === 'serverVersion') {
    return serverConfig.value?.version ?? '-'
  }
  if (item.key === 'appVersion') {
    try {
      const native = (window as unknown as { AndroidNative?: { getAppVersion?: () => string } }).AndroidNative
      if (native?.getAppVersion) return native.getAppVersion() ?? '-'
    } catch { /* not in app mode */ }
    return '-'
  }
  if (item.source === 'local') {
    return localConfig[item.key]
  }
  return getServerValueWithDefault(item.key)
}

async function handleUpdate(item: ItemSpec, value: unknown) {
  if (item.type === 'password') {
    if (!value) return
  }

  if (item.key === 'localhost_auth_exempt' && value === false) {
    const confirmed = await dialog.confirm(
      t('settings.items.localhostAuthExemptConfirm'),
      { title: t('settings.items.localhostAuthExempt'), dangerous: true }
    )
    if (!confirmed) return
  }
  if (item.source === 'local') {
    setLocalConfig(item.key, value as string | number | boolean)
    if (item.key === 'logCapture') {
      if (value) {
        try {
          ;(window as unknown as { AndroidNative?: { startLogCapture?: () => void } }).AndroidNative?.startLogCapture?.()
        } catch { /* not in app mode */ }
        startFlushTimer()
      } else {
        try {
          ;(window as unknown as { AndroidNative?: { stopLogCapture?: () => void } }).AndroidNative?.stopLogCapture?.()
        } catch { /* not in app mode */ }
        stopFlushTimer()
      }
    }
    return
  }
  try {
    const result = await setServerValue(item.key, value as string | number | boolean)
    if (result.needsRestart && result.changedColdFields.length > 0) {
      emit('restartNeeded', result.changedColdFields)
    }
  } catch {
    toast.show(t('settings.saveFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

function handleClick(item: ItemSpec) {
  if (item.key === 'reconfigureServer') {
    try {
      ;(window as unknown as { AndroidNative?: { showServerDialog?: () => void } }).AndroidNative?.showServerDialog?.()
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
  if (item.key === 'showWelcome') {
    window.dispatchEvent(new CustomEvent('clawbench-show-welcome'))
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
