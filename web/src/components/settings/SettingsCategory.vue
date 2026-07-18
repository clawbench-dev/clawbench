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
  <!-- Sub-page routes (data-driven: any colon-separated ID except agents) -->
  <div v-else-if="subPagePanel" class="settings-category">
    <SettingsGroupPanel
      :config="subPagePanel"
      :show-title="false"
      @restart-needed="(fields) => $emit('restartNeeded', fields)"
    />
  </div>
  <!-- Standard settings category with mixed items + panels -->
  <div v-else class="settings-category">
    <template v-for="entry in renderList" :key="entry.type === 'item' ? entry.spec.key : entry.config.panelId">
      <!-- Section header for flat items -->
      <template v-if="entry.type === 'item' && entry.spec.sectionHeader">
        <div class="settings-category__section-header">{{ t(entry.spec.sectionHeader) }}</div>
      </template>
      <!-- Flat item -->
      <SettingsItem
        v-if="entry.type === 'item'"
        :label="getItemLabel(entry.spec)"
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
        :default-value="entry.spec.defaultValue"
        :display-format="entry.spec.displayFormat"
        :display-transform="entry.spec.displayTransform"
        @update:model-value="(v: unknown) => handleUpdate(entry.spec, v)"
        @click="handleClick(entry.spec)"
        @edit-toggle="(open: boolean) => handleEditToggle(entry.spec.key, open)"
        @discard="handleDiscard"
      />
      <!-- Group panel — C2 fix: arrow closure passes panelId -->
      <SettingsGroupPanel
        v-else
        :config="entry.config"
        :show-title="shouldShowPanelTitle(entry.config)"
        @restart-needed="(fields) => $emit('restartNeeded', fields)"
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
import { startFlushTimer, stopFlushTimer } from '@/utils/appLog'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { categoryItems, isPanelOnlyCategory, getCategoryPanels, isDependsOnMet, isSubPageRoute, getSubPagePanel, type ItemSpec, type CategoryEntry, type GroupPanelConfig } from './settingsFieldMap'

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

// ── Sub-page panel (data-driven) ──

const subPagePanel = computed((): GroupPanelConfig | undefined => {
  if (isSubPageRoute(props.categoryId)) {
    return getSubPagePanel(props.categoryId)
  }
  return undefined
})

// ── Render list: mixed items + panels with dependsOn filtering ──

const renderList = computed(() => {
  const raw = categoryItems[props.categoryId] ?? []
  const result: CategoryEntry[] = []

  for (const entry of raw) {
    if (entry.type === 'item') {
      if (!isDependsOnMet(entry.spec.dependsOn, resolveConfigValue)) continue
      if (entry.spec.appOnly && !isAppMode.value) continue
      if (entry.spec.key === 'appVersion' && !isAppMode.value) continue
      if (entry.spec.key === 'addToHomeScreen' && !pwaInstall.showPwaInstall.value) continue
      if (entry.spec.key === 'downloadAndroidApp' && !pwaInstall.showApkDownload.value) continue
      result.push(entry)
    } else {
      // Panel entries always render
      result.push(entry)
    }
  }

  return result
})

// ── Panel title visibility ──

function shouldShowPanelTitle(_config: GroupPanelConfig): boolean {
  // Single-panel-only category: panel title = category title, no redundant header
  if (isPanelOnlyCategory(props.categoryId)) {
    const panels = getCategoryPanels(props.categoryId)
    return panels.length > 1
  }
  // Mixed category: always show panel title to distinguish from flat items
  return true
}

// ── Flat item helpers ──

function getItemLabel(entry: ItemSpec): string {
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
  if (item.navigateTo) {
    emit('navigate', item.navigateTo)
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

function handleDiscard() {
  toast.show(t('settings.passwordDiscarded'), { icon: 'ℹ️', type: 'info', duration: 3000 })
}
</script>

<style scoped>
.settings-category {
  padding: 0;
  background: var(--bg-secondary);
  min-height: 100%;
}

.settings-category__section-header {
  font-size: 12px;
  color: var(--text-muted);
  padding: 10px 16px 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
}
</style>
