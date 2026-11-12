<template>
  <div class="group-panel">
    <div class="group-panel__card">
    <!-- Panel title inside the card, distinct from card body and page background -->
    <div v-if="showTitle && config.titleKey" class="group-panel__header">
      {{ t(config.titleKey) }}
    </div>
    <!-- Enable toggle row. The port-forward panel has no switch: the feature is
         always available, and its `port_forward.transport` field decides which
         listener the server runs. -->
    <div v-if="config.enableKey" class="group-panel__enable-row">
      <div class="group-panel__enable-left">
        <span class="group-panel__enable-label">{{ t(config.enableLabelKey!) }}</span>
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

    <!-- Entry selector row.
         The description sits BELOW the label/value line (same position and
         type scale as a field row's description) rather than inside it: these
         descriptions run to a full sentence, and squeezing one into the label
         cell would either truncate it or push the current value off the row.
         The row was already carrying a `descriptionKey` in its config for
         every panel that uses it, but nothing rendered it — the usage meter's
         "switching the metric clears the quota" warning was written into i18n
         and then silently dropped. -->
    <div
      v-if="config.entrySelector"
      class="group-panel__entry-row"
      :class="{ 'group-panel__entry-row--disabled': fieldsDisabled }"
      @click="!fieldsDisabled && entryPicker.open()"
    >
      <div class="group-panel__entry-main">
        <span class="group-panel__entry-label">{{ t(config.entrySelector.labelKey) }}</span>
        <div class="group-panel__entry-right">
          <span class="group-panel__entry-value">{{ entryDisplayLabel }}</span>
          <ChevronRight :size="14" class="group-panel__entry-chevron" />
        </div>
      </div>
      <div v-if="config.entrySelector.descriptionKey" class="group-panel__entry-desc">
        {{ t(config.entrySelector.descriptionKey) }}
      </div>
    </div>

    <NativeNotificationStatus v-if="config.panelId === 'push'" />

    <!-- WeChat QR login. Rendered only while push_mode is wechat, directly
         below the mode selector, because the scan IS the configuration for that
         channel — there are no credentials to type. All of its state (session
         id, QR payload, connection status) lives inside the component, never in
         localValues: a panel field would be diffed and PATCHed back to the
         server, which must not receive any of it. -->
    <WeChatLoginPanel v-if="isWeChatPushMode" />

    <!-- Field list with section headers -->
    <template v-for="(entry, idx) in renderList" :key="entry.key">
      <div v-if="entry.type === 'header'" class="group-panel__section-header">{{ entry.label }}</div>
      <SettingsItem
        v-else
        :label="t(entry.field.labelKey)"
        :description="entry.field.descriptionKey ? t(entry.field.descriptionKey) : ''"
        :type="panelFieldType(entry.field)"
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
        :display-transform="resolveDisplayTransform(entry.field)"
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
    </template>

    <!-- Tunnel status, shown on every host now that the transport is a server
         setting. Switching it tears the tunnel down and rebuilds it on the new
         wire, which can take up to CONNECT_TIMEOUT_MS (20s) — without this the
         Save button spins with no explanation of what is happening or whether it
         worked. Reads the same usePortForward state the port-forward dock panel
         shows, so the two can never disagree. -->
    <template v-if="tunnelStatusVisible">
      <div class="group-panel__tunnel-status" :class="`group-panel__tunnel-status--${tunnelStatusState}`">
        <span class="group-panel__tunnel-dot" aria-hidden="true"></span>
        <span class="group-panel__tunnel-text">{{ tunnelStatusText }}</span>
        <button
          type="button"
          class="fbtn group-panel__tunnel-retry"
          :disabled="tunnelChecking || probingTunnel"
          @click="canProbeTunnel ? onProbeTunnel() : onRefreshTunnelStatus()"
        >
          {{ tunnelActionLabel }}
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
        :class="{ 'group-panel__option--active': effectiveEngine === opt.value }"
        @click="handleEntrySelect(opt.value)"
      >
        <span class="group-panel__option-label">{{ t(opt.labelKey) }}</span>
        <span v-if="effectiveEngine === opt.value" class="group-panel__option-check">&#10003;</span>
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
import { probeSystemSpeech, systemSpeechErrorKey } from '@/utils/systemSpeech'
import { getNative } from '@/utils/clawbenchNative'
import { usePanelSnapshot } from '@/composables/usePanelSnapshot'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useSettingsNavigation } from '@/composables/useSettingsNavigation'
import { useConnectivityTest } from '@/composables/useConnectivityTest'
import { useToast } from '@/composables/useToast'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { reconnectTunnel } from '@/utils/clawbenchNative'
import { usePortForward } from '@/composables/usePortForward'
import { useRagStatus } from '@/composables/useRagStatus'
import { useDialog } from '@/composables/useDialog'
import { startRebuild, rebuildStatus, type RebuildKind } from '@/composables/useRagRebuild'
import { formatFileSize } from '@/utils/fileType'
import '@/assets/modal-footer-btn.css'
import { SORTED_THEME_IDS, buildTerminalThemePreviews, formatThemeName, loadThemesModule } from '@/utils/terminalThemes'
import type { TerminalPreview } from './SettingsItem.vue'
import WeChatLoginPanel from './WeChatLoginPanel.vue'
import NativeNotificationStatus from './NativeNotificationStatus.vue'
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
  canSave: snapshotCanSave,
  needsRestartHint,
  initSnapshot,
  handleSave,
} = usePanelSnapshot(props.config)

// ── Phone voice (a per-device option inside the engine list) ──
//
// Kept as a draft like every other field, so the panel's Save button means the
// same thing everywhere. `localValues['tts.engine']` stays the SERVER engine:
// a device-local choice must never overwrite what every other device uses.
const { getServerValueWithDefault, setLocalConfig, localConfig: settingsLocalConfig } = useSettingsConfig()

const phoneVoiceDraft = ref(settingsLocalConfig.systemSpeech === true)
const phoneVoiceDirty = computed(() =>
  phoneVoiceDraft.value !== (settingsLocalConfig.systemSpeech === true),
)
const phoneVoiceActive = computed(() => props.config.panelId === 'tts' && phoneVoiceDraft.value)
// The phone choice alone is a valid change even when the server engine's
// credentials are incomplete.
const canSave = computed(() => snapshotCanSave.value || phoneVoiceDirty.value)

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
  phoneVoiceDraft.value = settingsLocalConfig.systemSpeech === true
  registerGuard(`panel-${props.config.panelId}`, () => !panelDirty.value && !hasFailedSave.value)
  // Fetch RAG status on mount so progress counts are visible immediately
  if (props.config.panelId === 'rag') {
    refreshRagStatus()
  }
  // The port-forward panel needs the server's resolved SSH port in two places:
  // the readiness row (web mode has no client tunnel to probe) and the auto
  // port row (0 = auto resolves to a concrete port the server chose). Both read
  // `sshInfo`, so it is loaded on every host — not just web — while the
  // "checking" flag stays web-only (native hosts get their status from the
  // client tunnel via checkTunnelHealth).
  if (props.config.panelId === 'portForward') {
    if (isWebApp.value) webStatusChecking.value = true
    loadSSHInfo().finally(() => {
      if (isWebApp.value) webStatusChecking.value = false
    })
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
    return !isDependsOnMet(field.disableUnless, dependencyValue)
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

/** The engine the list should show as selected (phone voice shadows the server one). */
const effectiveEngine = computed<unknown>(() =>
  phoneVoiceActive.value ? 'system' : localValues['tts.engine'],
)

/** dependsOn resolver that sees the effective engine, not the server one. */
function dependencyValue(key: string): unknown {
  return key === 'tts.engine' ? effectiveEngine.value : localValues[key]
}

const entryDisplayLabel = computed(() => {
  const es = props.config.entrySelector
  if (!es) return ''
  const val = dependencyValue(es.key)
  const opt = entryOptions.value.find(o => o.value === val)
  return opt ? t(opt.labelKey) : String(val ?? '')
})

async function handleEntrySelect(value: unknown) {
  const es = props.config.entrySelector
  if (!es) return

  if (value === 'system') {
    // Turning the phone voice on is a promise that this phone can read Chinese.
    // If it cannot, keep the current choice instead of accepting silently.
    // Each engine attempt gets 2s natively; leave room for a few of them.
    const probe = await probeSystemSpeech(8000)
    if (!probe.ok) {
      toast.show(t(systemSpeechErrorKey(probe.reason)), { icon: 'blocked', type: 'error', duration: 5000 })
      return
    }
    phoneVoiceDraft.value = true
    entryPicker.close()
    return
  }

  // Picking a real engine means "this device goes back to the server voice".
  phoneVoiceDraft.value = false
  // Only the selected value changes. Each engine owns its own settings, so
  // switching must not touch another engine's saved voice / credentials —
  // resetting them here overwrote the Edge voice whenever the user tried the
  // OpenAI engine.
  localValues[es.key] = value
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
    if (!isDependsOnMet(f.dependsOn, dependencyValue)) continue
    if (f.sectionHeader) {
      result.push({ type: 'header', key: `header-${f.key}`, label: t(f.sectionHeader) })
    }
    result.push({ type: 'field', key: f.key, field: f })
  }

  // Option sub-fields: matched against the entry selector's current value
  const subFieldKey = cfg.entrySelector?.key
  const subFieldValue = subFieldKey ? dependencyValue(subFieldKey) : undefined
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

// ── Field value helpers ──

function getLocalValue(field: ItemSpec): unknown {
  const k = field.key
  if (k in localValues) return localValues[k]
  // RAG status fields: resolve from polled RAG status
  if (k.startsWith('rag.status.')) return getRagStatusValue(k)
  return field.source === 'server' ? getServerValueWithDefault(k) : settingsLocalConfig[k]
}

/**
 * Display transform for a row, with one override.
 *
 * `port_forward.port` is the only row whose display depends on live server
 * state: 0 means "auto-assign", and the server resolves that to a concrete
 * port (main port + 1). The field spec's own transform can only say "自动"
 * because it has no access to `/api/ssh/info/full`; here we have `sshInfo`, so
 * the auto row reads "自动（20001）" — the user sees both that it is automatic
 * and which port is actually listening. Before the probe resolves (or when SSH
 * is disabled) it delegates to the field's own transform, which is the single
 * source for the plain "自动" wording.
 */
function resolveDisplayTransform(field: ItemSpec): ((value: unknown) => unknown) | undefined {
  const base = field.displayTransform
  if (field.key === 'port_forward.port') {
    return (value: unknown) => {
      if (value === 0) {
        const resolved = sshInfo.value?.enabled ? sshInfo.value.port : 0
        if (resolved > 0) {
          return t('settings.items.portForwardPortAutoResolved', { port: resolved })
        }
      }
      return base ? base(value) : value
    }
  }
  return base
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

/** Panels never render the summary-model row (SettingsCategory does), so the
 *  narrower type keeps SettingsItem's prop union satisfied. */
function panelFieldType(field: ItemSpec): Exclude<ItemSpec['type'], 'summaryModel'> {
  return field.type as Exclude<ItemSpec['type'], 'summaryModel'>
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

// ── Tunnel transport ──
//
// The transport is a plain server field (`port_forward.transport`, declared in
// settingsFieldMap). The server enforces it — h2 means no SSH listener at all,
// ssh means the h2 endpoints refuse — and every client follows, so there is no
// host-side picker or bridge write left in this panel: it edits the same draft
// and goes through the same Save as every other server field.

const { isWebApp } = usePlatformDetect()

// The WeChat QR login block replaces the credential fields for that channel:
// its "configuration" is a scan, not a value to type.
const isWeChatPushMode = computed(() => localValues.push_mode === 'wechat')

/** True while the configured transport is h2.
 *
 *  `port_forward.port` is the SSH listener's port — it feeds `ssh.NewServer`
 *  and nothing else (cmd/server/main.go). The h2 wire rides the main HTTP port,
 *  and with transport h2 the server does not open that listener at all, so the
 *  row configures a port nothing dials. Reads the draft, so the row appears and
 *  disappears as the user edits, before anything is committed.
 */
const sshOnlyFieldsHidden = computed(() =>
  props.config.panelId === 'portForward' && localValues['port_forward.transport'] === 'h2'
)

/** The SSH-server rows, minus the ones this transport does not use. */
const visibleCommonFields = computed(() =>
  sshOnlyFieldsHidden.value
    ? props.config.commonFields.filter(f => f.key !== 'port_forward.port')
    : props.config.commonFields
)

/** True while the panel has unsaved edits. The probe saves them first, so it
 *  measures the wire the user is looking at rather than the previous one. */
const transportDirty = computed(() => hasChanges.value)

/** The panel's own dirty flag (the transport is an ordinary field now, so
 *  `hasChanges` already covers it). */
const panelDirty = computed(() => hasChanges.value || phoneVoiceDirty.value)

// ── Tunnel status ──

const {
  tunnelStatus, tunnelChecking, tunnelMessage, checkTunnelHealth,
  sshInfo, transportAllowsH2, loadSSHInfo, ports,
  probingTunnel, probeTunnelLink, transportAnnotation,
} = usePortForward()

/**
 * Shown on every host now that the transport is a server setting: it is the
 * single readiness signal that replaced the removed "Test Connection" button.
 *
 * Web mode has no client tunnel, but the row still answers the question the old
 * test button answered: can this server carry forwards at all? It reads server
 * readiness (SSH listener up, or an h2-capable transport) instead of a
 * client-side tunnel, so the derivations below have a web branch.
 */
const tunnelStatusVisible = computed(() => props.config.panelId === 'portForward')

/**
 * Whether this server can carry forwards at all, independent of any client. An
 * SSH listener up or an h2-capable transport is enough — the same predicate the
 * port-forward panel's "unavailable" banner uses.
 */
const serverCarriesForwards = computed(() =>
  !!sshInfo.value?.enabled || transportAllowsH2.value
)

/**
 * Whether the native tunnel has any enabled mapping to carry — the same
 * predicate the port panel's banners and `syncToNative` use.
 *
 * With none, the native service stops itself on purpose (it would otherwise
 * hold an SSH/h2 session and burn battery for no work), so `tunnelStatus` reads
 * `disconnected` in a perfectly healthy idle state. Reporting that as a failure
 * told users "port mapping unavailable" when the only thing missing was a port.
 */
const hasEnabledPorts = computed(() => ports.value.some(p => p.enabled))

/** Web-mode probe in flight. Separate from `tunnelChecking` because the web
 *  branch re-reads SSH info rather than running the client-side health check. */
const webStatusChecking = ref(false)

/** Busy in either mode — drives the dot and the label. */
const tunnelStatusBusy = computed(() => webStatusChecking.value || tunnelChecking.value)

/**
 * Coarse state for the dot's colour. `unknown` covers both "not checked yet"
 * and the transient `checkTunnelHealth` reset; the two look identical to the
 * user and both mean "we have not established anything".
 *
 * Web mode derives its own state from server readiness: there is no client
 * tunnel to be ok/degraded/disconnected, so "ready" is the only healthy answer.
 *
 * Native mode has two states that are NOT failures and must not borrow the red
 * dot: an unsaved transport draft (the verdict on screen still describes the
 * old wire, so showing it would contradict the picker right above) and an empty
 * mapping list (the tunnel is idle by design).
 */
const tunnelStatusState = computed(() => {
  if (tunnelStatusBusy.value) return 'checking'
  if (isWebApp.value) return serverCarriesForwards.value ? 'ok' : 'disconnected'
  if (transportDirty.value) return 'unknown'
  if (!hasEnabledPorts.value) return 'unknown'
  if (tunnelStatus.value === 'ok') return 'ok'
  if (tunnelStatus.value === 'degraded') return 'degraded'
  if (tunnelStatus.value === 'disconnected') return 'disconnected'
  return 'unknown'
})

/**
 * The status line. In web mode it names which wire makes the server ready; in
 * native mode it uses `tunnelMessage` when the composable produced one — it is
 * already localized and carries the transport annotation (e.g.
 * `隧道已连接（HTTP/2）`) — and falls back to a plain label for the states it
 * leaves unset (`ok`, and `unknown` before the first check).
 */
const tunnelStatusText = computed(() => {
  if (tunnelStatusBusy.value) return t('settings.items.tunnelStatusChecking')
  if (isWebApp.value) {
    if (sshInfo.value?.enabled) return t('settings.items.tunnelStatusServerReady')
    if (transportAllowsH2.value) return t('settings.items.tunnelStatusServerReadyH2')
    return t('settings.items.tunnelStatusUnavailable')
  }
  if (transportDirty.value) return t('settings.items.tunnelStatusDraftPending')
  if (!hasEnabledPorts.value) return t('settings.items.tunnelStatusNoPorts')
  if (tunnelStatus.value === 'ok') return t('settings.items.tunnelStatusConnected')
  if (tunnelMessage.value) return tunnelMessage.value
  return t('settings.items.tunnelStatusUnknown')
})

async function onRefreshTunnelStatus() {
  // Web mode has no client tunnel: re-read the server's readiness rather than
  // running the client-side health check (which would have nothing to report).
  if (isWebApp.value) {
    if (webStatusChecking.value) return
    webStatusChecking.value = true
    try {
      await loadSSHInfo()
    } finally {
      webStatusChecking.value = false
    }
    return
  }
  if (tunnelChecking.value) return
  await checkTunnelHealth()
}

// ── Link probe ──

/**
 * The probe replaces "重新检测" whenever there is nothing to carry: with zero
 * enabled mappings the native service is stopped by design, so a plain health
 * check can only ever answer "disconnected" and the user has no way to tell
 * whether the chosen transport actually works. The probe opens a temporary
 * mapping to the server itself, reports the verdict, and removes it.
 */
const canProbeTunnel = computed(() => !isWebApp.value && !hasEnabledPorts.value)

/** Label for the single action button in the status row. */
const tunnelActionLabel = computed(() => {
  if (probingTunnel.value) return t('proxy.probeLinkBusy')
  if (!canProbeTunnel.value) return t('proxy.retryCheck')
  // A pending draft would be tested on the OLD wire; make the implicit save
  // explicit instead of reporting a verdict for a transport the user is no
  // longer looking at.
  return transportDirty.value ? t('proxy.probeLinkAndSave') : t('proxy.probeLink')
})

async function onProbeTunnel() {
  if (probingTunnel.value || tunnelChecking.value) return
  if (transportDirty.value) {
    saving.value = true
    try {
      const saved = await handleSave()
      if (saved.failed) return
    } finally {
      saving.value = false
    }
  }

  const verdict = await probeTunnelLink()
  if (verdict === 'unsupported') {
    // Host predates the port primitives — fall back to the plain check rather
    // than reporting a verdict it never measured.
    await checkTunnelHealth()
    return
  }

  const transport = transportAnnotation()
  if (verdict === 'ok') {
    toast.show(t('portForward.tunnelProbeOk', { transport }), { icon: 'link', type: 'success', duration: 4000 })
  } else {
    toast.show(t('portForward.tunnelProbeFailed', { transport }), { icon: 'blocked', type: 'error', duration: 6000 })
  }
  // The probe removed its temporary mapping, so the tunnel is idle again; this
  // only refreshes the transport label and the idle-state copy.
  await checkTunnelHealth(true)
}

// ── Save ──

async function onSave() {
  const result = await handleSave()
  if (result.needsRestart && result.changedColdFields.length > 0) {
    emit('restartNeeded', result.changedColdFields)
  }
  if (result.failed || serverError.value) return

  // The phone-voice choice is per device, so it is applied here rather than
  // sent to the server — but still only when the user presses Save.
  if (phoneVoiceDirty.value) {
    setLocalConfig('systemSpeech', phoneVoiceDraft.value)
  }

  // Switching the transport makes the server start or stop its SSH listener,
  // so the live tunnel has to be rebuilt on the new wire. The native monitor
  // would notice within its 15s tick, but doing it here puts the wait under the
  // Save button's own spinner instead of looking like a hang. Web mode has no
  // client tunnel, so there is nothing to rebuild.
  const switchingTransport = result.changedKeys.includes('port_forward.transport')
  if (switchingTransport && !isWebApp.value) {
    saving.value = true
    try {
      // With zero enabled mappings the native service is stopped on purpose, so
      // a reconnect has nothing to rebuild and reports failure. The PATCH above
      // is the whole job then; the next check or link probe speaks for the new
      // wire.
      if (hasEnabledPorts.value) await reconnectTunnel()
    } catch (err) {
      appLog.w(TAG, 'tunnel reconnect after transport change failed', err)
      toast.show(t('settings.panel.transportApplyFailed'), { icon: 'warning', type: 'error', duration: 5000 })
      return
    } finally {
      saving.value = false
    }
    // The verdict on screen was rendered at check time, so it still names the
    // OLD wire. Silent keeps the row's current verdict instead of flashing
    // "unknown".
    await checkTunnelHealth(true)
  }

  toast.show(t('settings.panel.saved'), { icon: 'success', type: 'success', duration: 3000 })
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
      toast.show(t('settings.items.ragRebuildStillRunning'), { icon: 'pending', type: 'info', duration: 5000 })
      return
    }
    if (status.status === 'done') {
      toast.show(t(RAG_REBUILD_SUCCESS[kind]), { icon: 'success', type: 'success', duration: 5000 })
    } else if (status.status === 'blocked') {
      // The work cannot finish (e.g. embedding service down during a vector
      // rebuild). Distinct from an error: nothing is broken, it just needs the
      // dependency back.
      toast.show(
        t('settings.items.ragRebuildBlocked', { reason: status.error || '' }),
        { icon: 'pause', type: 'info', duration: 8000 },
      )
    } else if (status.status === 'error') {
      toast.show(
        status.error || t('settings.items.ragRebuildFailed'),
        { icon: 'warning', type: 'error', duration: 5000 },
      )
    } else if (status.status === 'cancelled') {
      toast.show(t('settings.items.ragRebuildCancelled'), { icon: 'stop', type: 'info', duration: 3000 })
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
      { icon: 'warning', type: 'error', duration: 5000 },
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

  // The phone voice has no server config to probe: test it end to end by
  // speaking a short sample and waiting for the engine's terminal event.
  if (phoneVoiceActive.value) {
    connectivityTesting.value = true
    try {
      connectivityTestResults.value = [await testSystemSpeech()]
    } finally {
      connectivityTesting.value = false
    }
    return
  }

  const values = { ...localValues } as Record<string, unknown>
  const tests = props.config.getTestCategories?.(values) ?? [{ category: props.config.panelId, values }]
  if (tests.length === 0) return
  await runConnectivityTests(tests)
}

/**
 * Speak a fixed short sentence on the phone and report what happened.
 *
 * A readiness probe alone would pass on an engine that accepts requests and
 * then fails to synthesize (OPPO's does exactly that, ~30s later), so the test
 * has to wait for the real terminal event.
 */
async function testSystemSpeech(): Promise<{ success: boolean; message: string }> {
  const native = getNative()
  if (!native?.ttsSpeak) {
    return { success: false, message: t(systemSpeechErrorKey('no_bridge')) }
  }
  const probe = await probeSystemSpeech(8000)
  if (!probe.ok) {
    return { success: false, message: t(systemSpeechErrorKey(probe.reason)) }
  }

  return await new Promise((resolve) => {
    const id = 'tts-test-' + Date.now()
    let settled = false
    const finish = (success: boolean, message: string) => {
      if (settled) return
      settled = true
      window.removeEventListener('clawbench-tts-state', onState)
      clearTimeout(timer)
      resolve({ success, message })
    }
    const onState = (e: Event) => {
      const detail = (e as CustomEvent).detail as { id?: string; state?: string; message?: string }
      if (detail?.id !== id) return
      if (detail.state === 'done') {
        finish(true, t('settings.panel.systemSpeechTestOk'))
      } else if (detail.state === 'error') {
        finish(false, t('settings.panel.systemSpeechTestFailed', { reason: detail.message || '' }))
      }
    }
    window.addEventListener('clawbench-tts-state', onState)
    const timer = setTimeout(
      () => finish(false, t('settings.panel.systemSpeechTestFailed', { reason: 'timeout' })),
      20000,
    )
    const accepted = native.ttsSpeak!(t('settings.panel.systemSpeechTestPhrase'), id)
    if (accepted !== 'ready' && accepted !== 'initializing') {
      finish(false, t('settings.panel.systemSpeechTestFailed', { reason: accepted }))
    }
  })
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
  padding: calc(5 * var(--dp)) var(--space-7);
  text-transform: uppercase;
  letter-spacing: calc(0.5 * var(--dp));
  font-weight: var(--font-weight-medium);
  position: relative;
  background: var(--bg-tertiary);
}
.group-panel__header::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: calc(16 * var(--dp));
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

/* Compact iOS-style card container */
.group-panel__card {
  border-radius: var(--radius-md);
  overflow: hidden;
  background: var(--bg-primary);
  margin-bottom: var(--space-4);
}
.group-panel__card :deep(.settings-item) {
  background: transparent;
  padding: var(--space-4) var(--space-7);
}
.group-panel__card :deep(.settings-item::after) {
  left: calc(16 * var(--dp));
}
.group-panel__card :deep(.settings-item:last-child::after) {
  display: none;
}

/* Enable toggle row (first row of the card) */
.group-panel__enable-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--space-7);
  min-height: calc(38 * var(--dp));
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
  flex-direction: column;
  padding: var(--space-4) var(--space-7);
  cursor: pointer;
  background: transparent;
  position: relative;
}

/* Label/value line. Kept as its own flex row so the description below can span
   the full width instead of wrapping under the value column. */
.group-panel__entry-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  min-height: calc(30 * var(--dp));
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

/* Description under an entry row. Same type scale and colour as a field row's
   `.settings-item__desc` so the two read as the same kind of text. */
.group-panel__entry-desc {
  width: 100%;
  margin-top: var(--space-2);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
  word-break: break-word;
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
  max-width: calc(160 * var(--dp));
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
  padding: calc(5 * var(--dp)) var(--space-7) calc(3 * var(--dp));
  text-transform: uppercase;
  letter-spacing: calc(0.5 * var(--dp));
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
  width: calc(8 * var(--dp));
  height: calc(8 * var(--dp));
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
  min-height: calc(38 * var(--dp));
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
