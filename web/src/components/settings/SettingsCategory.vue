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
    <template v-for="card in cards" :key="card.type === 'group' ? 'group-' + (card.title || 'basic') : card.config.panelId">
      <!-- Flat items grouped into a card -->
      <SettingsCard
        v-if="card.type === 'group'"
        :title="card.title"
      >
        <SettingsItem
          v-for="item in card.items"
          :key="item.key"
          :label="getItemLabel(item)"
          :description="item.descriptionKey ? t(item.descriptionKey) : ''"
          :type="item.type"
          :model-value="getItemValue(item)"
          :options="resolveItemOptions(item)"
          :min="item.min"
          :max="item.max"
          :step="item.step"
          :needs-restart="item.needsRestart"
          :force-close="activeKey !== null && activeKey !== item.key"
          :no-divider="false"
          :default-value="item.defaultValue"
          :display-format="item.displayFormat"
          :display-transform="item.displayTransform"
          @update:model-value="(v: unknown) => handleUpdate(item, v)"
          @click="handleClick(item)"
          @edit-toggle="(open: boolean) => handleEditToggle(item.key, open)"
          @discard="handleDiscard"
        />
      </SettingsCard>
      <!-- Group panel — C2 fix: arrow closure passes panelId -->
      <SettingsGroupPanel
        v-else
        :config="card.config"
        :show-title="shouldShowPanelTitle(card.config)"
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
    <!-- Upgrade dialog -->
    <UpgradeDialog ref="upgradeDialogRef" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingsItem from './SettingsItem.vue'
import SettingsGroupPanel from './SettingsGroupPanel.vue'
import SettingsCard from './SettingsCard.vue'
import PasswordChangeDialog from './PasswordChangeDialog.vue'
import UpgradeDialog from './UpgradeDialog.vue'
import SettingsAgentsIndex from './SettingsAgentsIndex.vue'
import SettingsAgentDetail from './SettingsAgentDetail.vue'
import IosInstallDrawer from '@/components/common/IosInstallDrawer.vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useAgents } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useAppMode } from '@/composables/useAppMode'
import { startFlushTimer, stopFlushTimer } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { useDesktopDownload } from '@/composables/useDesktopDownload'
import { downloadByUrl } from '@/utils/download'
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
const desktopDownload = useDesktopDownload()
const activeKey = ref<string | null>(null)
const showPasswordDialog = ref(false)
const showIosSheet = ref(false)
const upgradeDialogRef = ref<InstanceType<typeof UpgradeDialog> | null>(null)
const nativeAppVersion = ref('')

onMounted(() => {
  desktopDownload.loadLatest()
  void (async () => {
    try {
      const native = getNative()
      if (native?.getAppVersion) nativeAppVersion.value = (await native.getAppVersion()) ?? '-'
    } catch { /* not in app mode */ }
  })()
})

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
      if (entry.spec.key === 'downloadDesktopApp' && !(desktopDownload.isDesktop && !!desktopDownload.currentDownloadUrl())) continue
      result.push(entry)
    } else {
      // Panel entries always render
      result.push(entry)
    }
  }

  return result
})

// ── Group flat items into cards by section header ──

const basicGroupTitle = computed(() => t('settings.categories.basic'))

interface CardGroup { type: 'group'; title: string; items: ItemSpec[] }
interface CardPanel { type: 'panel'; config: GroupPanelConfig }
type RenderCard = CardGroup | CardPanel

const cards = computed<RenderCard[]>(() => {
  const out: RenderCard[] = []
  const otherItems: ItemSpec[] = []
  let cur: CardGroup | null = null
  const flush = () => { if (cur) { out.push(cur); cur = null } }
  for (const entry of renderList.value) {
    if (entry.type === 'item') {
      if (entry.spec.sectionHeader) {
        const header = t(entry.spec.sectionHeader)
        if (!cur || cur.title !== header) {
          flush()
          cur = { type: 'group', title: header, items: [] }
        }
        cur.items.push(entry.spec)
      } else {
        // Header-less items all merge into a single "其他" group (at the end),
        // so a page can never have more than one "其他" card.
        flush()
        otherItems.push(entry.spec)
      }
    } else {
      flush()
      out.push({ type: 'panel', config: entry.config })
    }
  }
  flush()
  if (otherItems.length > 0) {
    out.push({ type: 'group', title: basicGroupTitle.value, items: otherItems })
  }
  return out
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
    return nativeAppVersion.value || '-'
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
          getNative()?.startLogCapture?.()?.catch(() => {})
        } catch { /* not in app mode */ }
        startFlushTimer()
      } else {
        try {
          getNative()?.stopLogCapture?.()?.catch(() => {})
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
      getNative()?.showServerDialog?.()
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
    downloadByUrl('/api/apk', 'clawbench-android.apk')
  }
  if (item.key === 'downloadDesktopApp') {
    desktopDownload.downloadDesktop()
  }
  if (item.navigateTo) {
    emit('navigate', item.navigateTo)
  }
  if (item.key === 'showWelcome') {
    window.dispatchEvent(new CustomEvent('clawbench-show-welcome'))
  }
  if (item.key === 'checkUpgrade') {
    upgradeDialogRef.value?.show()
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
  toast.show(t('settings.passwordChanged'), { icon: '✅', type: 'success', duration: 3000 })
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
  padding: 8px;
  background: var(--bg-secondary);
}
</style>
