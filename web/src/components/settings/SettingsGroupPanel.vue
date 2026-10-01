<template>
  <div class="group-panel">
    <div class="group-panel__card">
    <!-- Panel title inside the card, distinct from card body and page background -->
    <div v-if="showTitle && config.titleKey" class="group-panel__header">
      {{ t(config.titleKey) }}
    </div>
    <!-- Enable toggle row. Its label is transport-aware: the key gates the SSH
         listener, not port mapping as a whole (see sshOnlyFieldsHidden). -->
    <div v-if="config.enableKey" class="group-panel__enable-row">
      <div class="group-panel__enable-left">
        <span class="group-panel__enable-label">{{ t(enableLabelKey!) }}</span>
      </div>
      <label class="settings-item__switch" @click.stop>
        <input
          type="checkbox"
          class="settings-item__switch-input"
          :checked="!!localValues[config.enableKey]"
          @change="onEnableToggle"
        />
        <span class="settings-item__switch-track"></span>
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

    <!-- Android-only h2 tunnel toggle. A dedicated row rather than a
         commonFields entry: the truth source is Android SharedPreferences, so
         it must not flow through usePanelSnapshot's save routing (which would
         write localStorage for 'local' or 400 the whole save for 'server').
         Rendered only once the async initial value has arrived, and hidden
         entirely on a host that lacks the bridge method (see h2ToggleVisible).
         The value is a draft until Save writes it and reconnects. -->
    <template v-if="h2ToggleVisible">
      <SettingsItem
        :label="t('settings.items.portForwardH2')"
        :description="t('settings.items.portForwardH2Desc')"
        type="switch"
        :model-value="h2Enabled"
        :no-divider="true"
        @update:model-value="onH2Toggle"
      />
    </template>

    <!-- Desktop (Electron) transport picker. Same dedicated-row treatment as
         the Android toggle above, for the same reason: the truth source is the
         Electron store, not the server config. Three values rather than a
         boolean because the desktop transport keeps its 'both' (h2 first, SSH
         fallback) mode. Draft until Save. -->
    <template v-if="desktopTransportVisible">
      <SettingsItem
        :label="t('settings.items.portForwardTransport')"
        :description="t('settings.items.portForwardTransportDesc')"
        type="select"
        :model-value="desktopTransport"
        :options="desktopTransportOptions"
        :no-divider="true"
        @update:model-value="onDesktopTransportChange"
      />
    </template>

    <!-- Tunnel status, shown while the transport controls are on screen.
         Applying a transport change tears the tunnel down and rebuilds it,
         which can take up to CONNECT_TIMEOUT_MS (20s) in 'both' mode — without
         this the Save button spins with no explanation of what is happening or
         whether it worked. Reads the same usePortForward state the port-forward
         dock panel shows, so the two can never disagree. -->
    <template v-if="tunnelStatusVisible">
      <div class="group-panel__tunnel-status" :class="`group-panel__tunnel-status--${tunnelStatusState}`">
        <span class="group-panel__tunnel-dot" aria-hidden="true"></span>
        <span class="group-panel__tunnel-text">{{ tunnelStatusText }}</span>
        <button
          type="button"
          class="fbtn group-panel__tunnel-retry"
          :disabled="tunnelChecking"
          @click="onRefreshTunnelStatus"
        >
          {{ t('proxy.retryCheck') }}
        </button>
      </div>
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
          :class="{ 'refresh-spin--active': ragRebuildKind === 'fts' }"
          :disabled="ragRebuilding"
          @click="handleRagRebuild('fts')"
        >
          <RotateCcw :size="14" />
          {{ t('settings.items.ragFtsRebuild') }}
        </button>
        <button
          class="fbtn fbtn-danger refresh-spin"
          :class="{ 'refresh-spin--active': ragRebuildKind === 'vector' }"
          :disabled="ragRebuilding || !localValues['rag.vector_enabled']"
          @click="handleRagRebuild('vector')"
        >
          <RotateCcw :size="14" />
          {{ t('settings.items.ragVectorRebuild') }}
        </button>
        <button
          class="fbtn fbtn-danger refresh-spin"
          :class="{ 'refresh-spin--active': ragRebuildKind === 'full' }"
          :disabled="ragRebuilding"
          @click="handleRagRebuild('full')"
        >
          <RotateCcw :size="14" />
          {{ t('settings.items.ragFullRebuild') }}
        </button>
        <!-- Live progress for the running rebuild. The message-index progress
             rows elsewhere in this panel describe backlog, not rebuild progress,
             so the rebuild needs its own indicator. -->
        <span v-if="ragRebuilding" class="group-panel__rag-progress">
          {{ ragRebuildProgressText }}
        </span>
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
          :class="{ 'fbtn-primary group-panel__save-btn--accent': panelDirty }"
          :disabled="!panelDirty || !canSave || saving"
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
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { getNative, reconnectTunnel } from '@/utils/clawbenchNative'
import { usePortForward } from '@/composables/usePortForward'
import { useRagStatus } from '@/composables/useRagStatus'
import { useDialog } from '@/composables/useDialog'
import { startRebuild, rebuildStatus, type RebuildKind } from '@/composables/useRagRebuild'
import { formatFileSize } from '@/utils/fileType'
import '@/assets/modal-footer-btn.css'
import { SORTED_THEME_IDS, buildTerminalThemePreviews, formatThemeName, loadThemesModule } from '@/utils/terminalThemes'
import type { TerminalPreview } from './SettingsItem.vue'
import { appLog } from '@/utils/appLog'

const TAG = 'SettingsPanel'

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
  registerGuard(`panel-${props.config.panelId}`, () => !panelDirty.value && !hasFailedSave.value)
  // Fetch RAG status on mount so progress counts are visible immediately
  if (props.config.panelId === 'rag') {
    refreshRagStatus()
  }
})

onUnmounted(() => {
  unregisterGuard(`panel-${props.config.panelId}`)
  // Stop polling for the FTS rebuild. The rebuild itself keeps running
  // server-side; only the client's progress tracking is abandoned.
  ragRebuildUnmounted = true
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
  for (const f of visibleCommonFields.value) {
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
    // Sizes are 0 when the index is empty or dbstat is unavailable; show a
    // dash rather than a misleading "0 B".
    case 'rag.status.fts_size':
      return s.fts_size_bytes > 0 ? formatFileSize(s.fts_size_bytes) : '—'
    case 'rag.status.vec_size':
      return s.vec_size_bytes > 0 ? formatFileSize(s.vec_size_bytes) : '—'
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

// ── Tunnel transport preference (both native hosts) ──
//
// The transport choice is a DRAFT, saved by the panel's own Save button rather
// than written the moment the control moves. Two reasons:
//
//  1. Writing on change made the Save button lie — the control is a dedicated
//     row (its truth source is the host's own store, not the server config, so
//     it must not go through usePanelSnapshot's save routing), which meant the
//     preference was already persisted while `hasChanges` still reported the
//     panel clean.
//  2. Applying a transport switch means tearing down the live tunnel, so it
//     must happen at a moment the user chose. Save is that moment.
//
// The draft feeds `sshOnlyFieldsHidden` too, so the SSH-only rows appear and
// disappear as the user edits — before anything is committed.

// Host axis: which shell is running (see usePlatformDetect). Android gets the
// boolean toggle, Electron the two-valued picker, the browser neither.
const { isElectron: isDesktopShell, isAndroidApp } = usePlatformDetect()

// ── Android: boolean h2 toggle ──

const h2Enabled = ref(false)
/** The persisted value, kept separate from the draft so Save can tell whether
 *  anything changed and so a failed write can roll back to the truth. */
const h2EnabledInitial = ref(false)
/** Set once the async initial value has been read; gates the row's v-if so it
 *  never flashes a false "off" before the real value arrives. */
const h2ToggleLoaded = ref(false)
/** The host lacks the new bridge getter (an older APK) — hide the row instead
 *  of showing a toggle that would not persist. */
const h2ToggleUnsupported = ref(false)

const h2ToggleVisible = computed(() =>
  isAndroidApp.value
  && props.config.panelId === 'portForward'
  && h2ToggleLoaded.value
  && !h2ToggleUnsupported.value
)

onMounted(async () => {
  if (!isAndroidApp.value || props.config.panelId !== 'portForward') return
  const native = getNative()
  // Legacy host: no persisted toggle exists, so there is nothing truthful to
  // render. Keep it hidden rather than fall back to a non-persisting setter.
  if (!native?.getTunnelTransportH2Enabled) {
    h2ToggleUnsupported.value = true
    return
  }
  try {
    const stored = !!(await native.getTunnelTransportH2Enabled())
    h2Enabled.value = stored
    h2EnabledInitial.value = stored
  } catch {
    h2ToggleUnsupported.value = true
    return
  }
  h2ToggleLoaded.value = true
})

/** Draft only — nothing is written until Save. */
function onH2Toggle(value: unknown) {
  h2Enabled.value = !!value
}

// ── Desktop: three-valued picker ──

/** 'ssh' | 'h2' | 'both', or null while the initial value is still loading. */
const desktopTransport = ref<'ssh' | 'h2' | 'both' | null>(null)
const desktopTransportInitial = ref<'ssh' | 'h2' | 'both' | null>(null)
const desktopTransportUnsupported = ref(false)

/** True while the user is on an h2-ONLY tunnel, on either host.
 *
 *  `port_forward.enabled` and `port_forward.port` are SSH-server settings — they
 *  feed `ssh.NewServer` and nothing else (cmd/server/main.go). The h2 wire
 *  rides the main HTTP port, so on an h2-only install those two rows control a
 *  listener that is deliberately off and a port nothing reads, and the enable
 *  switch reads "port mapping is off" on a perfectly working setup.
 *
 *  Both native hosts report the same two-family answer, so this one predicate
 *  covers them: the desktop picker stores 'ssh' | 'h2' | 'both', and Android's
 *  boolean switch reports 'h2' when on. Only an unambiguous h2 hides the rows —
 *  'both' still runs SSH as the fallback, and an unknown value (older host, web
 *  mode) keeps today's layout rather than guessing.
 */
const sshOnlyFieldsHidden = computed(() => {
  if (props.config.panelId !== 'portForward') return false
  if (isDesktopShell.value) return desktopTransport.value === 'h2'
  if (isAndroidApp.value) return h2ToggleLoaded.value && h2Enabled.value
  return false
})

/** Label for the enable toggle. It is an SSH-server switch, so on an h2-only
 *  install the generic "enable port mapping" would be actively wrong — the
 *  panel's other controls (add port, scan, forward list) all still work. */
const enableLabelKey = computed(() =>
  sshOnlyFieldsHidden.value
    ? 'settings.items.portForwardSshEnabled'
    : props.config.enableLabelKey
)

/** The SSH-server rows, minus the ones this transport does not use. */
const visibleCommonFields = computed(() =>
  sshOnlyFieldsHidden.value
    ? props.config.commonFields.filter(f => f.key !== 'port_forward.port')
    : props.config.commonFields
)

const desktopTransportVisible = computed(() =>
  isDesktopShell.value
  && props.config.panelId === 'portForward'
  && desktopTransport.value !== null
)

/** Labels reuse `proxy.transportSsh` / `transportH2` / `transportAuto`, the
 *  same keys the port-forward panel uses for the live "current transport"
 *  line, so the two never drift. */
const desktopTransportOptions = computed(() => [
  { label: t('proxy.transportSsh'), value: 'ssh' },
  { label: t('proxy.transportH2'), value: 'h2' },
  { label: t('proxy.transportAuto'), value: 'both' },
])

onMounted(async () => {
  if (!isDesktopShell.value || props.config.panelId !== 'portForward') return
  const native = getNative()
  // Older desktop build: no setter exists, so there is nothing to write and
  // the row must stay hidden rather than offer a choice that cannot persist.
  if (!native?.setTunnelTransport || !native?.getTunnelTransport) {
    desktopTransportUnsupported.value = true
    return
  }
  try {
    const value = await native.getTunnelTransport()
    if (value === 'ssh' || value === 'h2' || value === 'both') {
      desktopTransport.value = value
      desktopTransportInitial.value = value
    } else {
      desktopTransportUnsupported.value = true
    }
  } catch {
    desktopTransportUnsupported.value = true
  }
})

/** Draft only — nothing is written until Save. */
function onDesktopTransportChange(value: unknown) {
  if (value !== 'ssh' && value !== 'h2' && value !== 'both') return
  desktopTransport.value = value
}

// ── Draft → save wiring ──

/** True when the transport draft differs from what the host has persisted. */
const transportDirty = computed(() => {
  if (isDesktopShell.value && desktopTransportVisible.value) {
    return desktopTransport.value !== desktopTransportInitial.value
  }
  if (isAndroidApp.value && h2ToggleVisible.value) {
    return h2Enabled.value !== h2EnabledInitial.value
  }
  return false
})

/** The panel's own dirty flag, widened to include the transport draft — without
 *  this the Save button stays disabled (and the unsaved-changes guard stays
 *  open) when the transport is the only thing the user changed. */
const panelDirty = computed(() => hasChanges.value || transportDirty.value)

/**
 * Write the transport preference and reconnect, returning false when the host
 * rejected the write (nothing was stored, so the live tunnel is untouched).
 *
 * A failed RECONNECT is deliberately not rolled back: the preference was
 * accepted and persisted, so reverting the stored value would make the host and
 * the UI disagree. The user is told instead, and the monitor retries on its own.
 */
async function applyTransportDraft(): Promise<boolean> {
  if (!transportDirty.value) return true
  const native = getNative()

  if (isDesktopShell.value) {
    const value = desktopTransport.value
    if (value === null) return true
    const setter = native?.setTunnelTransport
    // A host without the setter cannot persist the choice. Reporting success
    // would mark the draft clean and claim a preference that was never stored,
    // so treat it as a failed apply instead.
    if (typeof setter !== 'function') return false
    let accepted: unknown = undefined
    try {
      accepted = await setter.call(native, value)
    } catch (err) {
      appLog.w(TAG, 'desktop transport write failed', err)
      return false
    }
    // The host validates too; a false answer means it stored nothing.
    if (accepted === false) return false
    desktopTransportInitial.value = value
  } else if (isAndroidApp.value) {
    const value = h2Enabled.value
    const setter = native?.setTunnelTransportH2Enabled
    if (typeof setter !== 'function') return false
    try {
      await setter.call(native, value)
    } catch (err) {
      appLog.w(TAG, 'h2 toggle write failed', err)
      return false
    }
    h2EnabledInitial.value = value
  } else {
    return true
  }

  try {
    return await reconnectTunnel()
  } catch (err) {
    appLog.w(TAG, 'tunnel reconnect after transport change failed', err)
    return false
  }
}

// ── Tunnel status ──

const { tunnelStatus, tunnelChecking, tunnelMessage, checkTunnelHealth } = usePortForward()

/**
 * Shown whenever a transport control is on screen — i.e. on a host that can
 * actually report and switch its wire. Web mode has no tunnel to report, and
 * a legacy host cannot say which one it is running, so the row stays out
 * rather than showing an "unknown" the user cannot act on.
 */
const tunnelStatusVisible = computed(() =>
  props.config.panelId === 'portForward'
  && (h2ToggleVisible.value || desktopTransportVisible.value)
)

/**
 * Coarse state for the dot's colour. `unknown` covers both "not checked yet"
 * and the transient `checkTunnelHealth` reset; the two look identical to the
 * user and both mean "we have not established anything".
 */
const tunnelStatusState = computed(() => {
  if (tunnelChecking.value) return 'checking'
  if (tunnelStatus.value === 'ok') return 'ok'
  if (tunnelStatus.value === 'degraded') return 'degraded'
  if (tunnelStatus.value === 'disconnected') return 'disconnected'
  return 'unknown'
})

/**
 * The status line. Uses `tunnelMessage` when the composable produced one —
 * it is already localized and carries the transport annotation (e.g.
 * `隧道已连接（HTTP/2）`) — and falls back to a plain label for the states it
 * leaves unset (`ok`, and `unknown` before the first check).
 */
const tunnelStatusText = computed(() => {
  if (tunnelChecking.value) return t('settings.items.tunnelStatusChecking')
  if (tunnelStatus.value === 'ok') return t('settings.items.tunnelStatusConnected')
  if (tunnelMessage.value) return tunnelMessage.value
  return t('settings.items.tunnelStatusUnknown')
})

async function onRefreshTunnelStatus() {
  if (tunnelChecking.value) return
  await checkTunnelHealth()
}

// ── Save ──

async function onSave() {
  const result = await handleSave()
  if (result.needsRestart && result.changedColdFields.length > 0) {
    emit('restartNeeded', result.changedColdFields)
  }
  if (serverError.value) return

  // Applying the transport switch can take a while — 'both' probes h2 before
  // falling back to SSH, bounded by CONNECT_TIMEOUT_MS (20s). Reuse the Save
  // button's own spinner so the wait is visible rather than looking like a hang.
  const switchingTransport = transportDirty.value
  if (switchingTransport) saving.value = true
  try {
    if (!(await applyTransportDraft())) {
      if (switchingTransport) {
        // The preference may or may not have been stored; either way the tunnel
        // is not on it, so say so instead of the plain "saved" toast.
        toast.show(t('settings.panel.transportApplyFailed'), { icon: '⚠️', type: 'error', duration: 5000 })
      }
      return
    }
  } finally {
    if (switchingTransport) saving.value = false
  }

  toast.show(t('settings.panel.saved'), { icon: '✅', type: 'success', duration: 3000 })
}

// ── Custom field actions (RAG progress) ──

// Which rebuild kind is currently running, or null when idle. A single ref
// because the server allows only one rebuild at a time across all kinds.
const ragRebuildKind = ref<RebuildKind | null>(null)
const ragRebuilding = computed(() => ragRebuildKind.value !== null)
const ragRefreshing = ref(false)
// Set on unmount so an in-flight status poller stops instead of looping after
// the panel is gone. The rebuild continues server-side regardless.
let ragRebuildUnmounted = false

// Confirm/title/success keys per kind, kept as maps so the handler stays flat.
const RAG_REBUILD_CONFIRM: Record<RebuildKind, string> = {
  fts: 'settings.items.ragFtsRebuildConfirm',
  vector: 'settings.items.ragVectorRebuildConfirm',
  full: 'settings.items.ragFullRebuildConfirm',
}
const RAG_REBUILD_TITLE: Record<RebuildKind, string> = {
  fts: 'settings.items.ragFtsRebuild',
  vector: 'settings.items.ragVectorRebuild',
  full: 'settings.items.ragFullRebuild',
}
const RAG_REBUILD_SUCCESS: Record<RebuildKind, string> = {
  fts: 'settings.items.ragFtsRebuildSuccess',
  vector: 'settings.items.ragVectorRebuildSuccess',
  full: 'settings.items.ragFullRebuildSuccess',
}

// Live progress text for the running rebuild, e.g. "12300/44434 · 28%".
const ragRebuildProgressText = computed(() => {
  const s = rebuildStatus.value
  if (s.total <= 0) return t('settings.items.ragRebuildStarting')
  const label = `${s.processed}/${s.total}`
  return `${label} · ${s.progress_pct}%`
})

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
 * Rebuild one layer of the RAG index.
 *
 * All three kinds run in the background on the server (202 + status polling);
 * they differ in how much work is redone:
 * - fts:    re-segments existing chunks. Used after a segmenter/dictionary change,
 *           or to repair chunks indexed while the segmenter was unavailable.
 * - vector: re-embeds existing chunks. Used after switching embedding models.
 * - full:   deletes all chunks and rebuilds from the source messages, so it is the
 *           only kind that re-chunks — required after changing chunk_size/overlap.
 *
 * The work is never awaited inline: a full re-segmentation takes minutes on a
 * large store and the shared API timeout is 10s, which previously made a
 * successful rebuild report as a failure.
 */
async function handleRagRebuild(kind: RebuildKind) {
  if (ragRebuilding.value) return
  const confirmKey = RAG_REBUILD_CONFIRM[kind]
  const titleKey = RAG_REBUILD_TITLE[kind]

  const confirmed = await dialog.confirm(
    t(confirmKey),
    { title: t(titleKey), dangerous: true },
  )
  if (!confirmed) return

  ragRebuildKind.value = kind
  try {
    const status = await startRebuild(kind, () => ragRebuildUnmounted)
    if (status === null) {
      // Polling gave up (unmounted or runaway). Not a server failure: the
      // rebuild is still running server-side.
      toast.show(t('settings.items.ragRebuildStillRunning'), { icon: '⏳', type: 'info', duration: 5000 })
      return
    }
    if (status.status === 'done') {
      toast.show(t(RAG_REBUILD_SUCCESS[kind]), { icon: '✅', type: 'success', duration: 5000 })
    } else if (status.status === 'blocked') {
      // The work cannot finish (e.g. embedding service down during a vector
      // rebuild). Distinct from an error: nothing is broken, it just needs the
      // dependency back.
      toast.show(
        t('settings.items.ragRebuildBlocked', { reason: status.error || '' }),
        { icon: '⏸️', type: 'info', duration: 8000 },
      )
    } else if (status.status === 'error') {
      toast.show(
        status.error || t('settings.items.ragRebuildFailed'),
        { icon: '⚠️', type: 'error', duration: 5000 },
      )
    } else if (status.status === 'cancelled') {
      toast.show(t('settings.items.ragRebuildCancelled'), { icon: '⏹️', type: 'info', duration: 3000 })
    }
    refreshRagStatus()
  } catch (err) {
    // The server refuses the trigger when the segmenter is unavailable or a
    // rebuild is already running, and returns a localized explanation — prefer
    // it over the generic message.
    const serverMsg = (err as Error & { msgKey?: string })?.msgKey
      ? (err as Error).message
      : ''
    toast.show(
      serverMsg || t('settings.items.ragRebuildFailed'),
      { icon: '⚠️', type: 'error', duration: 5000 },
    )
  } finally {
    ragRebuildKind.value = null
  }
}

async function handleFieldClick(_field: ItemSpec) {
  // The RAG panel has no clickable action items — index rebuilds live in the
  // panel footer buttons, not in per-row action fields.
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
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  padding:5px var(--space-7);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: var(--font-weight-medium);
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
  margin-bottom: var(--space-4);
}
.group-panel__card :deep(.settings-item) {
  background: transparent;
  padding: var(--space-4) var(--space-7);
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
  padding:0 var(--space-7);
  min-height: 38px;
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
  gap: var(--space-3);
}

.group-panel__enable-label {
  font-size: var(--font-size-md);
  color: var(--text-primary);
}

/* Entry selector row */
.group-panel__entry-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding:0 var(--space-7);
  min-height: 38px;
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
  opacity: var(--opacity-muted);
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
  font-size: var(--font-size-md);
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
  gap: var(--space-2);
  flex-shrink: 0;
}

.group-panel__entry-value {
  font-size: var(--font-size-md);
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
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  padding:5px var(--space-7) 3px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: var(--font-weight-medium);
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
  padding: var(--space-4) var(--space-7);
}

.group-panel__save-row {
  display: flex;
  gap: var(--space-4);
}

/* RAG index rebuild actions — destructive, so separated from the save row */
.group-panel__rag-actions {
  display: flex;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
  /* Three rebuild buttons plus a progress label wrap on narrow panels. */
  flex-wrap: wrap;
}

.group-panel__rag-actions .fbtn {
  flex: 1;
}

/* Live progress for the running rebuild, e.g. "12300/44434 · 28%". Takes its own
   row so it does not squeeze the buttons. */
.group-panel__rag-progress {
  flex-basis: 100%;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  text-align: center;
  font-variant-numeric: tabular-nums;
}

.group-panel__restart-hint {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  text-align: center;
  margin-bottom: var(--space-3);
}

/* Tunnel status row. Sits inside the card, aligned with the SettingsItem rows'
   16px horizontal padding (a container must not add its own — see the design
   guide's "自定义卡片块的横向内边距"). */
.group-panel__tunnel-status {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-7);
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}

.group-panel__tunnel-dot {
  width: 8px;
  height: 8px;
  flex-shrink: 0;
  border-radius: var(--radius-full);
  background: var(--text-muted);
}

/* The dot is the fast read; the text says the same thing for anyone who cannot
   use colour. Both channels are present, so neither is load-bearing alone. */
.group-panel__tunnel-status--ok .group-panel__tunnel-dot {
  background: var(--color-green);
}

.group-panel__tunnel-status--degraded .group-panel__tunnel-dot {
  background: var(--color-yellow);
}

.group-panel__tunnel-status--disconnected .group-panel__tunnel-dot {
  background: var(--color-red);
}

/* A pulse, not a spin: the row is reporting a probe in flight, and a ring here
   would read as "the tunnel itself is reconnecting". Not gated on
   prefers-reduced-motion for the same reason as BusyBar — the movement is the
   only channel that distinguishes "checking" from "stuck". */
.group-panel__tunnel-status--checking .group-panel__tunnel-dot {
  background: var(--accent-color);
  animation: group-panel-tunnel-pulse 1s ease-in-out infinite;
}

@keyframes group-panel-tunnel-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: var(--opacity-muted); }
}

.group-panel__tunnel-text {
  flex: 1;
  min-width: 0;
}

.group-panel__tunnel-retry {
  flex-shrink: 0;
}

/* Layout only — visuals come from the shared .fbtn pills. */
.group-panel__save-btn {
  flex: 1;
}

/* Test results */
.group-panel__test-result {
  font-size: var(--font-size-md);
  margin: var(--space-3) var(--space-7) 0;
  padding: var(--space-3) var(--space-5);
  border-radius: var(--radius-sm);
  line-height: var(--line-height-snug);
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
  font-size: var(--font-size-md);
  color: #ef4444;
  margin-bottom: var(--space-3);
}

/* Hot-reload warning */
.group-panel__warning {
  font-size: var(--font-size-md);
  color: #f59e0b;
  margin-bottom: var(--space-3);
  white-space: pre-line;
}
</style>

<!-- Non-scoped styles for BottomSheet-teleported option rows -->
<style>
.group-panel__option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-4) var(--space-7);
  cursor: pointer;
  min-height: 38px;
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
  font-size: var(--font-size-md);
  color: var(--text-primary);
}

.group-panel__option-check {
  font-size: var(--font-size-xl);
  color: var(--accent-color);
  font-weight: var(--font-weight-semibold);
}
</style>
