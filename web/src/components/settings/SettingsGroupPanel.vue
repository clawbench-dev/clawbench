<template>
  <div class="group-panel">
    <!-- Panel title separator -->
    <div v-if="showTitle && config.titleKey" class="group-panel__title">
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
    <template v-for="entry in renderList" :key="entry.key">
      <div v-if="entry.type === 'header'" class="group-panel__section-header">{{ entry.label }}</div>
      <SettingsItem
        v-else
        :label="t(entry.field.labelKey)"
        :description="entry.field.descriptionKey ? t(entry.field.descriptionKey) : ''"
        :type="entry.field.type"
        :model-value="getLocalValue(entry.field)"
        :options="resolveFieldOptions(entry.field)"
        :min="entry.field.min"
        :max="entry.field.max"
        :step="entry.field.step"
        :needs-restart="entry.field.needsRestart"
        :disabled="fieldsDisabled"
        :force-close="activeKey !== null && activeKey !== entry.field.key"
        :default-value="entry.field.defaultValue"
        :display-format="entry.field.displayFormat"
        :display-transform="entry.field.displayTransform"
        :no-divider="false"
        @update:model-value="(v: unknown) => setLocalValue(entry.field.key, v)"
        @edit-toggle="(open: boolean) => handleEditToggle(entry.field.key, open)"
        @desc-toggle="(open: boolean) => handleEditToggle(entry.field.key, open)"
      />
      <!-- FRP auto_port info injection -->
      <template v-if="entry.type === 'field' && entry.field.key === 'frp.auto_port' && isFrpAutoPortActive">
        <SettingsItem
          :label="t('settings.items.frpAssignedPort')"
          :description="''"
          type="info"
          :model-value="frpHttpPortDisplay"
          :disabled="false"
        />
        <SettingsItem
          v-if="frpSshPortDisplay"
          :label="t('settings.items.frpAssignedSSHPort')"
          :description="''"
          type="info"
          :model-value="frpSshPortDisplay"
          :disabled="false"
        />
      </template>
    </template>

    <!-- Sticky bottom save bar -->
    <div class="group-panel__save-bar">
      <div v-if="serverError" class="group-panel__error">{{ serverError }}</div>
      <div v-if="hotReloadWarning" class="group-panel__warning">{{ hotReloadWarning }}</div>
      <div v-if="needsRestartHint" class="group-panel__restart-hint">
        {{ t('settings.panel.needsRestartHint') }}
      </div>
      <div class="group-panel__save-row">
        <button
          v-if="config.hasConnectivityTest"
          class="group-panel__test-btn"
          :disabled="connectivityTesting"
          @click="handleConnectivityTest"
        >
          {{ connectivityTesting ? t('settings.panel.testing') : t('settings.panel.testConnectivity') }}
        </button>
        <button
          class="group-panel__save-btn"
          :class="{ 'group-panel__save-btn--accent': hasChanges }"
          :disabled="!hasChanges || !canSave || saving"
          @click="onSave"
        >
          {{ saving ? t('settings.panel.saving') : t('settings.panel.save') }}
        </button>
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
      :title="t(config.entrySelector.labelKey)"
      compact
      @close="entryPicker.close()"
    >
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
import { ChevronRight } from 'lucide-vue-next'
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

// ── Lifecycle ──

onMounted(() => {
  initSnapshot()

  // Register unsaved-changes guard with panel-specific ID (C3 fix)
  registerGuard(`panel-${props.config.panelId}`, () => !hasChanges.value && !hasFailedSave.value)
})

onUnmounted(() => {
  unregisterGuard(`panel-${props.config.panelId}`)
})

// ── Enable toggle ──

const fieldsDisabled = computed(() => {
  if (!props.config.enableKey) return false
  return !localValues[props.config.enableKey]
})

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

// ── Settings config for getLocalValue fallback ──

const { getServerValueWithDefault, localConfig: settingsLocalConfig } = useSettingsConfig()

// ── Field value helpers ──

function getLocalValue(field: ItemSpec): unknown {
  const k = field.key
  if (k in localValues) return localValues[k]
  return field.source === 'server' ? getServerValueWithDefault(k) : settingsLocalConfig[k]
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
  // Static options from field spec
  if (field.options) {
    return field.options.map(o => ({ label: t(o.labelKey), value: o.value }))
  }
  return undefined
}

function handleEditToggle(key: string, open: boolean) {
  if (open) {
    activeKey.value = key
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
  const info = { state: frpState.state, remotePort: frpState.remotePort, sshRemotePort: frpState.sshRemotePort }
  const port = info.state === 'running' && info.remotePort > 0 ? info.remotePort : 0
  return port > 0 ? port : '—'
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
    toast.show(t('settings.panel.saved'), { icon: '✓', type: 'success', duration: 3000 })
  }
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
  background: var(--bg-secondary);
  padding: 4px 0;
}

.group-panel__title {
  font-size: 12px;
  color: var(--text-muted);
  padding: 10px 16px 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
  border-top: 0.5px solid var(--border-color);
  margin-top: 4px;
}

/* Enable toggle row */
.group-panel__enable-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  min-height: 48px;
  background: var(--bg-primary);
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
  background: var(--color-green);
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
  background: var(--bg-primary);
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
  padding: 10px 16px 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
}

/* Sticky save bar (I3 fix) */
.group-panel__save-bar {
  position: sticky;
  bottom: 0;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  background: var(--bg-primary);
  border-top: 0.5px solid var(--border-color);
  padding: 8px 16px;
  padding-bottom: calc(8px + env(safe-area-inset-bottom, 0px));
  z-index: 10;
}

.group-panel__save-row {
  display: flex;
  gap: 8px;
}

.group-panel__restart-hint {
  font-size: 12px;
  color: var(--text-muted);
  text-align: center;
  margin-bottom: 6px;
}

.group-panel__save-btn {
  flex: 1;
  padding: 10px 16px;
  border: none;
  border-radius: 8px;
  font-size: 15px;
  font-weight: 500;
  cursor: pointer;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  transition: background 0.15s ease, color 0.15s ease;
}

.group-panel__save-btn--accent {
  background: var(--accent-color);
  color: #fff;
}

.group-panel__save-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

@media (hover: hover) {
  .group-panel__save-btn--accent:hover:not(:disabled) {
    background: var(--accent-hover);
  }
}

.group-panel__save-btn--accent:active:not(:disabled) {
  background: var(--accent-hover);
}

/* Test button */
.group-panel__test-btn {
  padding: 10px 16px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  font-size: 15px;
  font-weight: 500;
  cursor: pointer;
  background: var(--bg-primary);
  color: var(--text-primary);
  transition: background 0.15s ease;
  white-space: nowrap;
}

.group-panel__test-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.group-panel__test-btn:active:not(:disabled) {
  background: var(--bg-tertiary);
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
