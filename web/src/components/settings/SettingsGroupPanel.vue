<template>
  <div class="group-panel">
    <div class="group-panel__card">
    <!-- Panel title inside the card, distinct from card body and page background -->
    <div v-if="showTitle && config.titleKey" class="group-panel__header">
      {{ t(config.titleKey) }}
    </div>
    <!-- Enable toggle row -->
    <div v-if="config.enableKey" class="group-panel__enable-row">
      <div class="group-panel__enable-left">
        <span class="group-panel__enable-label">{{ t(config.enableLabelKey!) }}</span>
      </div>
      <label class="group-panel__switch" @click.stop>
        <input
          type="checkbox"
          class="group-panel__switch-input"
          :checked="!!localValues[config.enableKey]"
          @change="onEnableToggle"
        />
        <span class="group-panel__switch-track"></span>
      </label>
    </div>

    <!-- Entry selector row -->
    <div
      v-if="config.entrySelector"
      class="group-panel__entry-row"
      :class="{ 'group-panel__entry-row--disabled': fieldsDisabled }"
      @click="!fieldsDisabled && entryPicker.open()"
    >
      <span class="group-panel__entry-label">{{ t(config.entrySelector.labelKey) }}</span>
      <div class="group-panel__entry-right">
        <span class="group-panel__entry-value">{{ entryDisplayLabel }}</span>
        <ChevronRight :size="14" class="group-panel__entry-chevron" />
      </div>
    </div>

    <!-- Field list with section headers -->
    <template v-for="(entry, idx) in renderList" :key="entry.key">
      <div v-if="entry.type === 'header'" class="group-panel__section-header">{{ entry.label }}</div>
      <SettingsItem
        v-else
        :label="t(entry.field.labelKey)"
        :description="entry.field.descriptionKey ? t(entry.field.descriptionKey) : ''"
        :type="entry.field.type"
        :model-value="getLocalValue(entry.field)"
        :options="resolveFieldOptions(entry.field)"
        :option-previews="entry.field.key === 'terminalTheme' ? terminalThemePreviews : undefined"
        :terminal-theme-load-error="entry.field.key === 'terminalTheme' ? (terminalThemeLoadState === 'error' && terminalThemeRetriesExhausted) : false"
        :on-retry-terminal-themes="entry.field.key === 'terminalTheme' ? retryTerminalThemes : undefined"
        :min="entry.field.min"
        :max="entry.field.max"
        :step="entry.field.step"
        :needs-restart="entry.field.needsRestart"
        :disabled="isFieldDisabled(entry.field)"
        :force-close="activeKey !== null && activeKey !== entry.field.key"
        :default-value="entry.field.defaultValue"
        :display-format="entry.field.displayFormat"
        :display-transform="entry.field.displayTransform"
        :progress="resolveProgress(entry.field)"
        :refreshable="isRagProgressField(entry.field)"
        :refreshing="ragRefreshing"
        :no-divider="isLastInSection(idx)"
        @update:model-value="(v: unknown) => setLocalValue(entry.field.key, v)"
        @edit-toggle="(open: boolean) => handleEditToggle(entry.field.key, open)"
        @desc-toggle="(open: boolean) => handleEditToggle(entry.field.key, open)"
        @click="handleFieldClick(entry.field)"
        @refresh="handleRagRefresh"
      />
      <!-- FRP auto_port info injection -->
      <template v-if="entry.type === 'field' && entry.field.key === 'frp.auto_port' && isFrpAutoPortActive">
        <SettingsItem
          :label="t('settings.items.frpAssignedPort')"
          :description="t('settings.items.frpAssignedPortDesc')"
          type="number"
          :model-value="frpHttpPortDisplay"
          :disabled="true"
        />
        <SettingsItem
          v-if="frpSshPortDisplay"
          :label="t('settings.items.frpAssignedSSHPort')"
          :description="t('settings.items.frpAssignedSSHPortDesc')"
          type="number"
          :model-value="frpSshPortDisplay"
          :disabled="true"
        />
      </template>
    </template>

    <!-- Footer inside the card -->
    <div class="group-panel__footer">
      <div v-if="serverError" class="group-panel__error">{{ serverError }}</div>
      <div v-if="hotReloadWarning" class="group-panel__warning">{{ hotReloadWarning }}</div>
      <div v-if="needsRestartHint" class="group-panel__restart-hint">
        {{ t('settings.panel.needsRestartHint') }}
      </div>
      <div v-if="isRagPanel" class="group-panel__rag-actions">
        <button
          class="fbtn fbtn-danger refresh-spin"
          :class="{ 'refresh-spin--active': ragFtsRebuilding }"
          :disabled="ragFtsRebuilding || ragVectorRebuilding"
          @click="handleRagRebuild('fts')"
        >
          <RotateCcw :size="14" />
          {{ t('settings.items.ragFtsRebuild') }}
        </button>
        <button
          class="fbtn fbtn-danger refresh-spin"
          :class="{ 'refresh-spin--active': ragVectorRebuilding }"
          :disabled="ragFtsRebuilding || ragVectorRebuilding || !localValues['rag.vector_enabled']"
          @click="handleRagRebuild('vector')"
        >
          <RotateCcw :size="14" />
          {{ t('settings.items.ragVectorRebuild') }}
        </button>
      </div>
      <div class="group-panel__save-row">
        <button
          v-if="showTestButton"
          class="fbtn group-panel__test-btn"
          :disabled="connectivityTesting"
          @click="handleConnectivityTest"
        >
          {{ connectivityTesting ? t('settings.panel.testing') : t('settings.panel.testConnectivity') }}
        </button>
        <button
          class="fbtn group-panel__save-btn"
          :class="{ 'fbtn-primary group-panel__save-btn--accent': hasChanges }"
          :disabled="!hasChanges || !canSave || saving"
          @click="onSave"
        >
          {{ saving ? t('settings.panel.saving') : t('settings.panel.save') }}
        </button>
      </div>
    </div>
    </div>

    <!-- Connectivity test results -->
    <template v-for="(result, _idx) in connectivityTestResults" :key="_idx">
      <div
        class="group-panel__test-result"
        :class="result.success ? 'group-panel__test-result--success' : 'group-panel__test-result--error'"
      >
        {{ result.message }}
      </div>
    </template>

    <!-- Entry selector BottomSheet -->
    <BottomSheet
      v-if="config.entrySelector"
      :open="entryPicker.effectiveOpen.value"
      auto
      @close="entryPicker.close()"
    >
      <template #header>
        <ListChecks :size="16" class="bs-header-icon" />
        <span class="bs-header-title">{{ t(config.entrySelector.labelKey) }}</span>
      </template>
      <div
        v-for="opt in entryOptions"
        :key="opt.value as PropertyKey"
        class="group-panel__option"
        :class="{ 'group-panel__option--active': localValues[config.entrySelector!.key] === opt.value }"
        @click="handleEntrySelect(opt.value)"
      >
        <span class="group-panel__option-label">{{ t(opt.labelKey) }}</span>
        <span v-if="localValues[config.entrySelector!.key] === opt.value" class="group-panel__option-check">&#10003;</span>
      </div>
    </BottomSheet>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronRight, ListChecks, RotateCcw } from 'lucide-vue-next'
import SettingsItem from './SettingsItem.vue'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { engineVoiceOptions, isDependsOnMet, type ItemSpec, type GroupPanelConfig } from './settingsFieldMap'
import { usePanelSnapshot } from '@/composables/usePanelSnapshot'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useSettingsNavigation } from '@/composables/useSettingsNavigation'
import { useConnectivityTest } from '@/composables/useConnectivityTest'
import { useToast } from '@/composables/useToast'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { useFrp } from '@/composables/useFrp'
import { useRagStatus } from '@/composables/useRagStatus'
import { useDialog } from '@/composables/useDialog'
import { apiPost } from '@/utils/api'
import { formatFileSize } from '@/utils/fileType'
import '@/assets/modal-footer-btn.css'
import { SORTED_THEME_IDS, buildTerminalThemePreviews, formatThemeName, loadThemesModule } from '@/utils/terminalThemes'
import type { TerminalPreview } from './SettingsItem.vue'

// ── Props & Emits ──

const props = defineProps<{
  config: GroupPanelConfig
  showTitle: boolean
}>()

const emit = defineEmits<{
  restartNeeded: [fields: string[]]
}>()

const { t } = useI18n()
const toast = useToast()
const { registerGuard, unregisterGuard } = useSettingsNavigation()
const { testing: connectivityTesting, testResults: connectivityTestResults, runTests: runConnectivityTests, clearResults: clearConnectivityResults } = useConnectivityTest()
const { frpState } = useFrp()
const { status: ragStatus, refresh: refreshRagStatus } = useRagStatus()
const dialog = useDialog()

// ── Panel snapshot ──

const {
  localValues,
  saving,
  serverError,
  hotReloadWarning,
  hasFailedSave,
  hasChanges,
  canSave,
  needsRestartHint,
  initSnapshot,
  handleSave,
} = usePanelSnapshot(props.config)

const activeKey = ref<string | null>(null)
const entryPicker = useTabDrawer('settings', { autoRestore: false })

// ── Terminal theme lazy loading ──

const loadedTerminalThemes = ref<Record<string, import('@xterm/xterm').ITheme> | null>(null)
const terminalThemeLoadState = ref<'idle' | 'loading' | 'loaded' | 'error'>('idle')
const terminalThemeLoadAttempts = ref(0)
/** 自动重试是否已耗尽（此后失败需要用户手动重试）。 */
const terminalThemeRetriesExhausted = ref(false)

/** 自动重试次数上限（首次加载 + 最多 3 次自动重试，之后才需要手动）。 */
const TERMINAL_THEME_MAX_AUTO_RETRIES = 3
const TERMINAL_THEME_RETRY_DELAY_MS = 1500
let terminalThemeRetryTimer: ReturnType<typeof setTimeout> | null = null

/**
 * 懒加载终端主题配色（首次打开终端主题网格时触发）。失败后自动延迟重试
 * （最多 TERMINAL_THEME_MAX_AUTO_RETRIES 次），耗尽后才显示手动重试横幅。
 * 偶发的 chunk 加载失败会在重试中自愈，无需重启页面。
 */
async function ensureTerminalThemesLoaded() {
  if (terminalThemeLoadState.value === 'loaded' || terminalThemeLoadState.value === 'loading') return
  terminalThemeLoadState.value = 'loading'
  terminalThemeLoadAttempts.value++
  try {
    loadedTerminalThemes.value = await loadThemesModule()
    terminalThemeLoadState.value = 'loaded'
    terminalThemeRetriesExhausted.value = false
  } catch {
    terminalThemeLoadState.value = 'error'
    if (terminalThemeLoadAttempts.value <= TERMINAL_THEME_MAX_AUTO_RETRIES) {
      scheduleTerminalThemeRetry()
    } else {
      terminalThemeRetriesExhausted.value = true
    }
  }
}

/** 延迟后自动重试（幂等：已加载/加载中时跳过）。 */
function scheduleTerminalThemeRetry() {
  if (terminalThemeRetryTimer) return
  terminalThemeRetryTimer = setTimeout(() => {
    terminalThemeRetryTimer = null
    void ensureTerminalThemesLoaded()
  }, TERMINAL_THEME_RETRY_DELAY_MS)
}

/** 终端主题加载失败后的手动重试（自动重试耗尽后兜底）。重置计数开启新一轮自动重试。 */
function retryTerminalThemes() {
  if (terminalThemeLoadState.value === 'loaded' || terminalThemeLoadState.value === 'loading') return
  terminalThemeRetriesExhausted.value = false
  terminalThemeLoadAttempts.value = 0
  void ensureTerminalThemesLoaded()
}

onUnmounted(() => {
  if (terminalThemeRetryTimer) {
    clearTimeout(terminalThemeRetryTimer)
    terminalThemeRetryTimer = null
  }
})

const isTerminalThemeField = computed(() =>
  props.config.commonFields.some(f => f.key === 'terminalTheme')
)

const terminalThemePreviews = computed<Record<string, TerminalPreview> | undefined>(() => {
  if (!isTerminalThemeField.value) return undefined
  return buildTerminalThemePreviews(loadedTerminalThemes.value)
})

// ── Lifecycle ──

onMounted(() => {
  initSnapshot()
  registerGuard(`panel-${props.config.panelId}`, () => !hasChanges.value && !hasFailedSave.value)
  // Fetch RAG status on mount so progress counts are visible immediately
  if (props.config.panelId === 'rag') {
    refreshRagStatus()
  }
})

onUnmounted(() => {
  unregisterGuard(`panel-${props.config.panelId}`)
})

// ── Enable toggle ──

// ── Connectivity test visibility ──

const showTestButton = computed(() => {
  const val = props.config.hasConnectivityTest
  if (typeof val === 'function') return val(localValues)
  return !!val
})

// ── Enable toggle ──

const fieldsDisabled = computed(() => {
  if (!props.config.enableKey) return false
  return !localValues[props.config.enableKey]
})

/** Check if a specific field should be disabled due to its disableUnless condition. */
function isFieldDisabled(field: ItemSpec): boolean {
  if (fieldsDisabled.value) return true
  if (field.disableUnless) {
    return !isDependsOnMet(field.disableUnless, (k) => localValues[k])
  }
  return false
}

function onEnableToggle(e: Event) {
  const checked = (e.target as HTMLInputElement).checked
  if (props.config.enableKey) {
    localValues[props.config.enableKey] = checked
  }
}

// ── Entry selector ──

const entryOptions = computed(() => {
  return props.config.entrySelector?.options ?? []
})

const entryDisplayLabel = computed(() => {
  const es = props.config.entrySelector
  if (!es) return ''
  const val = localValues[es.key]
  const opt = entryOptions.value.find(o => o.value === val)
  return opt ? t(opt.labelKey) : String(val ?? '')
})

function handleEntrySelect(value: unknown) {
  const es = props.config.entrySelector
  if (!es) return
  const prevValue = localValues[es.key]
  localValues[es.key] = value

  // Auto-reset TTS voice when engine changes
  if (props.config.needsVoiceReset && es.key === 'tts.engine' && value !== prevValue) {
    const voiceOpts = engineVoiceOptions[value as string] ?? []
    localValues['tts.voice'] = voiceOpts.length > 0 ? voiceOpts[0].value : ''
  }

  entryPicker.close()
}

// ── Render list ──

interface RenderFieldEntry {
  type: 'field'
  key: string
  field: ItemSpec
}

interface RenderHeaderEntry {
  type: 'header'
  key: string
  label: string
}

type RenderEntry = RenderFieldEntry | RenderHeaderEntry

const renderList = computed((): RenderEntry[] => {
  const cfg = props.config
  const result: RenderEntry[] = []

  // Common fields (filtered by dependsOn)
  for (const f of cfg.commonFields) {
    if (!isDependsOnMet(f.dependsOn, (k) => localValues[k])) continue
    if (f.sectionHeader) {
      result.push({ type: 'header', key: `header-${f.key}`, label: t(f.sectionHeader) })
    }
    result.push({ type: 'field', key: f.key, field: f })
  }

  // Option sub-fields: use optionSubFieldsKey if specified (I1 fix)
  const subFieldKey = cfg.optionSubFieldsKey ?? cfg.entrySelector?.key
  const subFieldValue = subFieldKey ? localValues[subFieldKey] : undefined
  const osf = (cfg.optionSubFields ?? []).find(o => o.when === subFieldValue)
  if (osf) {
    for (const f of osf.fields) {
      if (f.sectionHeader) {
        result.push({ type: 'header', key: `header-${f.key}`, label: t(f.sectionHeader) })
      }
      result.push({ type: 'field', key: f.key, field: f })
    }
  }

  return result
})

/** A field is the last in its section if the next entry is a section header or it's the last entry. */
function isLastInSection(idx: number): boolean {
  const next = renderList.value[idx + 1]
  return !next || next.type === 'header'
}

// ── Settings config for getLocalValue fallback ──

const { getServerValueWithDefault, localConfig: settingsLocalConfig } = useSettingsConfig()

// ── Field value helpers ──

function getLocalValue(field: ItemSpec): unknown {
  const k = field.key
  if (k in localValues) return localValues[k]
  // RAG status fields: resolve from polled RAG status
  if (k.startsWith('rag.status.')) return getRagStatusValue(k)
  return field.source === 'server' ? getServerValueWithDefault(k) : settingsLocalConfig[k]
}

// ── RAG status field resolution ──

function getRagStatusValue(key: string): unknown {
  const s = ragStatus.value
  switch (key) {
    case 'rag.status.embedder_healthy':
      return s.embedder_healthy ? t('settings.items.ragEmbedderHealthy') : t('settings.items.ragEmbedderUnhealthy')
    case 'rag.status.mode':
      return t(`settings.items.ragMode_${s.mode}`)
    case 'rag.status.index_progress':
      return s.total_messages > 0 ? t('settings.items.ragProgressFormat', { done: Math.min(s.indexed_messages, s.total_messages), total: s.total_messages }) : '—'
    case 'rag.status.embed_progress':
      return s.total_messages > 0 ? t('settings.items.ragProgressFormat', { done: Math.min(s.embedded_messages, s.total_messages), total: s.total_messages }) : '—'
    case 'rag.status.fts_size':
      return s.has_fts_data ? formatFileSize(s.fts_size_bytes) : '—'
    case 'rag.status.vec_size':
      return s.has_vec_data ? formatFileSize(s.vec_size_bytes) : '—'
    default:
      return ''
  }
}

function resolveProgress(field: ItemSpec): { value: number; max: number } | undefined {
  const s = ragStatus.value
  switch (field.key) {
    case 'rag.status.index_progress':
      return s.total_messages > 0 ? { value: s.indexed_messages, max: s.total_messages } : undefined
    case 'rag.status.embed_progress':
      return s.total_messages > 0 ? { value: s.embedded_messages, max: s.total_messages } : undefined
    default:
      return field.progress
  }
}

function isRagProgressField(field: ItemSpec): boolean {
  return field.key === 'rag.status.index_progress' || field.key === 'rag.status.embed_progress'
}

function setLocalValue(key: string, value: unknown) {
  localValues[key] = value
}

function resolveFieldOptions(field: ItemSpec): { label: string; value: unknown }[] | undefined {
  // Dynamic voice options based on current engine
  if (field.key === 'tts.voice') {
    const engine = (localValues['tts.engine'] as string) || 'edge'
    const voiceOpts = engineVoiceOptions[engine as string] ?? []
    if (voiceOpts.length > 0) {
      return voiceOpts.map(o => ({ label: t(o.labelKey), value: o.value }))
    }
  }
  // Dynamic terminal theme options (sorted light → dark by background)
  if (field.key === 'terminalTheme') {
    return [
      { label: t('terminal.themeFollowApp'), value: 'auto' },
      ...SORTED_THEME_IDS.map(id => ({ label: formatThemeName(id), value: id })),
    ]
  }
  // Static options from field spec
  if (field.options) {
    return field.options.map(o => ({ label: t(o.labelKey), value: o.value }))
  }
  return undefined
}

function handleEditToggle(key: string, open: boolean) {
  if (open) {
    activeKey.value = key
    if (key === 'terminalTheme') void ensureTerminalThemesLoaded()
  } else if (activeKey.value === key) {
    activeKey.value = null
  }
}

// ── FRP auto_port info ──

const isFrpAutoPortActive = computed(() => {
  if (props.config.panelId !== 'frp') return false
  return localValues['frp.auto_port'] === true && localValues['frp.enabled'] === true
})

const frpHttpPortDisplay = computed(() => {
  const port = frpState.state === 'running' && frpState.remotePort > 0 ? frpState.remotePort : 0
  return port
})

const frpSshPortDisplay = computed(() => {
  const port = frpState.state === 'running' && frpState.sshRemotePort > 0 ? frpState.sshRemotePort : null
  return port
})

// ── Save ──

async function onSave() {
  const result = await handleSave()
  if (result.needsRestart && result.changedColdFields.length > 0) {
    emit('restartNeeded', result.changedColdFields)
  }
  if (!serverError.value) {
    toast.show(t('settings.panel.saved'), { icon: '✅', type: 'success', duration: 3000 })
  }
}

// ── Custom field actions (RAG progress) ──

const ragVectorRebuilding = ref(false)
const ragFtsRebuilding = ref(false)
const ragRefreshing = ref(false)

const isRagPanel = computed(() => props.config.panelId === 'rag')

async function handleRagRefresh() {
  ragRefreshing.value = true
  try {
    await refreshRagStatus()
  } finally {
    ragRefreshing.value = false
  }
}

/**
 * Rebuild one RAG index independently.
 * - fts: regenerates the full-text index from existing chunks (vectors untouched)
 * - vector: re-embeds all chunks with the current model (FTS/chunks untouched)
 */
async function handleRagRebuild(kind: 'fts' | 'vector') {
  const isVector = kind === 'vector'
  const rebuildingRef = isVector ? ragVectorRebuilding : ragFtsRebuilding
  if (rebuildingRef.value) return
  const confirmKey = isVector ? 'settings.items.ragVectorRebuildConfirm' : 'settings.items.ragFtsRebuildConfirm'
  const titleKey = isVector ? 'settings.items.ragVectorRebuild' : 'settings.items.ragFtsRebuild'
  const successKey = isVector ? 'settings.items.ragVectorRebuildSuccess' : 'settings.items.ragFtsRebuildSuccess'

  const confirmed = await dialog.confirm(
    t(confirmKey),
    { title: t(titleKey), dangerous: true },
  )
  if (!confirmed) return

  rebuildingRef.value = true
  try {
    const endpoint = isVector ? '/api/rag/reset-vector' : '/api/rag/rebuild-fts'
    await apiPost(endpoint, {})
    toast.show(t(successKey), { icon: '✅', type: 'success', duration: 3000 })
    refreshRagStatus()
  } catch {
    toast.show(t('settings.items.ragRebuildFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  } finally {
    rebuildingRef.value = false
  }
}

async function handleFieldClick(_field: ItemSpec) {
  // No action items in RAG panel now — rebuilds are triggered via progress bar icons
}

// ── Connectivity test ──

async function handleConnectivityTest() {
  clearConnectivityResults()
  const values = { ...localValues } as Record<string, unknown>
  const tests = props.config.getTestCategories?.(values) ?? [{ category: props.config.panelId, values }]
  if (tests.length === 0) return
  await runConnectivityTests(tests)
}

// Auto-clear test results when form values change (results become stale)
watch(localValues, () => {
  if (connectivityTestResults.value.length > 0) clearConnectivityResults()
}, { deep: true })
</script>


<style scoped>
.group-panel {
  background: transparent;
}

/* Panel title inside the card, distinct from card body and page background */
.group-panel__header {
  font-size: 12px;
  color: var(--text-muted);
  padding: 5px 16px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
  position: relative;
  background: var(--bg-tertiary);
}
.group-panel__header::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 16px;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

/* Compact iOS-style card container */
.group-panel__card {
  border-radius: 0;
  overflow: hidden;
  background: var(--bg-primary);
  margin-bottom: 8px;
}
.group-panel__card :deep(.settings-item) {
  background: transparent;
  padding: 8px 16px;
}
.group-panel__card :deep(.settings-item::after) {
  left: 16px;
}
.group-panel__card :deep(.settings-item:last-child::after) {
  display: none;
}

/* Enable toggle row (first row of the card) */
.group-panel__enable-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  min-height: 48px;
  background: transparent;
  position: relative;
}

.group-panel__enable-row::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.group-panel__enable-left {
  display: flex;
  align-items: center;
  gap: 6px;
}

.group-panel__enable-label {
  font-size: 15px;
  color: var(--text-primary);
}

/* iOS-style switch toggle */
.group-panel__switch {
  position: relative;
  display: inline-block;
  width: 51px;
  height: 31px;
  cursor: pointer;
}

.group-panel__switch-input {
  opacity: 0;
  width: 0;
  height: 0;
  position: absolute;
}

.group-panel__switch-track {
  position: absolute;
  inset: 0;
  border-radius: 15.5px;
  background: var(--bg-tertiary);
  transition: background 0.2s ease;
}

.group-panel__switch-track::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 27px;
  height: 27px;
  border-radius: 50%;
  background: var(--bg-primary);
  transition: transform 0.2s ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
}

.group-panel__switch-input:checked + .group-panel__switch-track {
  background: var(--accent-color);
}

.group-panel__switch-input:checked + .group-panel__switch-track::after {
  transform: translateX(20px);
}

/* Entry selector row */
.group-panel__entry-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  min-height: 48px;
  cursor: pointer;
  background: transparent;
  position: relative;
}

.group-panel__entry-row::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.group-panel__entry-row--disabled {
  opacity: 0.5;
  pointer-events: none;
}

@media (hover: hover) {
  .group-panel__entry-row:hover {
    background: var(--bg-tertiary);
  }
}

.group-panel__entry-row:active {
  background: var(--bg-tertiary);
}

.group-panel__entry-label {
  font-size: 15px;
  color: var(--text-primary);
  flex-shrink: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-panel__entry-right {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.group-panel__entry-value {
  font-size: 14px;
  color: var(--text-secondary);
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-panel__entry-chevron {
  color: var(--text-muted);
  flex-shrink: 0;
}

/* Section header */
.group-panel__section-header {
  font-size: 12px;
  color: var(--text-muted);
  padding: 5px 16px 3px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
  background: var(--bg-tertiary);
}

/* Sticky save bar (I3 fix) */
/* Footer row inside the card (save / test buttons) */
.group-panel__footer {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  background: transparent;
  border-top: 0.5px solid var(--border-color);
  padding: 8px 16px;
}

.group-panel__save-row {
  display: flex;
  gap: 8px;
}

/* RAG index rebuild actions — destructive, so separated from the save row */
.group-panel__rag-actions {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}

.group-panel__rag-actions .fbtn {
  flex: 1;
}

.group-panel__restart-hint {
  font-size: 12px;
  color: var(--text-muted);
  text-align: center;
  margin-bottom: 6px;
}

/* Layout only — visuals come from the shared .fbtn pills. */
.group-panel__save-btn {
  flex: 1;
}

/* Test results */
.group-panel__test-result {
  font-size: 13px;
  margin: 6px 16px 0;
  padding: 6px 10px;
  border-radius: 6px;
  line-height: 1.4;
}

.group-panel__test-result--success {
  color: #22c55e;
  background: color-mix(in srgb, #22c55e 10%, var(--bg-primary));
}

.group-panel__test-result--error {
  color: #ef4444;
  background: color-mix(in srgb, #ef4444 10%, var(--bg-primary));
}

/* Server error */
.group-panel__error {
  font-size: 13px;
  color: #ef4444;
  margin-bottom: 6px;
}

/* Hot-reload warning */
.group-panel__warning {
  font-size: 13px;
  color: #f59e0b;
  margin-bottom: 6px;
  white-space: pre-line;
}
</style>

<!-- Non-scoped styles for BottomSheet-teleported option rows -->
<style>
.group-panel__option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  cursor: pointer;
  min-height: 44px;
  position: relative;
}

.group-panel__option::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.group-panel__option:last-child::after {
  display: none;
}

@media (hover: hover) {
  .group-panel__option:hover {
    background: var(--bg-tertiary);
  }
}

.group-panel__option:active {
  background: var(--bg-tertiary);
}

.group-panel__option--active {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, var(--bg-primary, #fff));
}

.group-panel__option-label {
  font-size: 15px;
  color: var(--text-primary);
}

.group-panel__option-check {
  font-size: 15px;
  color: var(--accent-color);
  font-weight: 600;
}
</style>
