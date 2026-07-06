<template>
  <div class="drill-down">
    <!-- Enable toggle row -->
    <div v-if="config.enableKey" class="drill-down__enable-row">
      <div class="drill-down__enable-left">
        <span class="drill-down__enable-label">{{ t(config.enableLabelKey!) }}</span>
        <span
          v-if="sideEffects.frpStatusDot.value"
          class="drill-down__status-dot"
          :class="'drill-down__status-dot--' + sideEffects.frpStatusDot.value"
        />
      </div>
      <label class="drill-down__switch" @click.stop>
        <input
          type="checkbox"
          class="drill-down__switch-input"
          :checked="!!localValues[config.enableKey]"
          @change="onEnableToggle"
        />
        <span class="drill-down__switch-track"></span>
      </label>
    </div>

    <!-- Entry selector row -->
    <div
      v-if="config.entrySelector"
      class="drill-down__entry-row"
      :class="{ 'drill-down__entry-row--disabled': fieldsDisabled }"
      @click="!fieldsDisabled && entryPicker.open()"
    >
      <span class="drill-down__entry-label">{{ t(config.entrySelector.labelKey) }}</span>
      <div class="drill-down__entry-right">
        <span class="drill-down__entry-value">{{ entryDisplayLabel }}</span>
        <ChevronRight :size="14" class="drill-down__entry-chevron" />
      </div>
    </div>

    <!-- Field list with section headers -->
    <template v-for="entry in renderList" :key="entry.key">
      <div v-if="entry.type === 'header'" class="drill-down__section-header">{{ entry.label }}</div>
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
        :no-divider="false"
        @update:model-value="(v: any) => setLocalValue(entry.field.key, v)"
        @edit-toggle="(open: boolean) => handleEditToggle(entry.field.key, open)"
        @desc-toggle="(open: boolean) => handleEditToggle(entry.field.key, open)"
      />
      <!-- FRP auto_port info injection -->
      <template v-if="entry.type === 'field' && entry.field.key === 'frp.auto_port' && isFrpAutoPortActive">
        <SettingsItem
          label=""
          :description="''"
          type="info"
          :model-value="frpHttpPortDisplay"
          :disabled="false"
        />
        <SettingsItem
          v-if="frpSshPortDisplay"
          label=""
          :description="''"
          type="info"
          :model-value="frpSshPortDisplay"
          :disabled="false"
        />
      </template>
    </template>

    <!-- Fixed bottom save bar -->
    <div class="drill-down__save-bar">
      <div v-if="needsRestartHint" class="drill-down__restart-hint">
        {{ t('settings.drillDown.needsRestartHint') }}
      </div>
      <button
        class="drill-down__save-btn"
        :class="{ 'drill-down__save-btn--accent': hasChanges }"
        :disabled="!hasChanges || !canSave || saving"
        @click="handleSave"
      >
        {{ saving ? t('settings.drillDown.saving') : t('settings.drillDown.save') }}
      </button>
    </div>

    <!-- Server error -->
    <div v-if="serverError" class="drill-down__error">{{ serverError }}</div>

    <!-- Required field empty indicator (visual, no text) -->

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
        :key="opt.value"
        class="drill-down__option"
        :class="{ 'drill-down__option--active': localValues[config.entrySelector!.key] === opt.value }"
        @click="handleEntrySelect(opt.value)"
      >
        <span class="drill-down__option-label">{{ t(opt.labelKey) }}</span>
        <span v-if="localValues[config.entrySelector!.key] === opt.value" class="drill-down__option-check">&#10003;</span>
      </div>
    </BottomSheet>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronRight } from 'lucide-vue-next'
import SettingsItem from './SettingsItem.vue'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { drillDownCategories, engineVoiceOptions, type ItemSpec, type DrillDownCategory } from './settingsFieldMap'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useDrillDownSideEffects } from '@/composables/useDrillDownSideEffects'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useTabDrawer } from '@/composables/useTabDrawer'

// ── Props & Emits ──

const props = defineProps<{
  categoryId: string
}>()

const emit = defineEmits<{
  'restartNeeded': [fields: string[]]
  'back': []
}>()

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()
const { patchConfig, getServerValueWithDefault, setLocalConfig, localConfig } = useSettingsConfig()
const sideEffects = useDrillDownSideEffects(props.categoryId)

// ── Config ──

const config = computed((): DrillDownCategory => {
  return drillDownCategories[props.categoryId] ?? { categoryId: props.categoryId, commonFields: [] }
})

// ── State ──

const localValues = reactive<Record<string, any>>({})
const snapshot = ref<Record<string, any>>({})
const saving = ref(false)
const serverError = ref('')
const hasFailedSave = ref(false)
const activeKey = ref<string | null>(null)
const entryPickerOpen = ref(false)
const entryPicker = useTabDrawer('settings', entryPickerOpen)

// Guard against useTabDrawer preserving openRef on tab switch back
watch(() => entryPicker.effectiveOpen.value, (val) => {
  if (!val && entryPickerOpen.value) {
    entryPickerOpen.value = false
  }
})

// ── Snapshot on mount ──

onMounted(() => {
  const cfg = config.value
  const snap: Record<string, any> = {}

  // Enable key
  if (cfg.enableKey) {
    snap[cfg.enableKey] = getServerValueWithDefault(cfg.enableKey)
  }

  // Entry selector key
  if (cfg.entrySelector) {
    snap[cfg.entrySelector.key] = getServerValueWithDefault(cfg.entrySelector.key)
  }

  // Common fields
  for (const f of cfg.commonFields) {
    snap[f.key] = f.source === 'server' ? getServerValueWithDefault(f.key) : localConfig[f.key]
  }

  // Option sub-fields (snapshot all possible sub-fields)
  for (const osf of cfg.optionSubFields ?? []) {
    for (const f of osf.fields) {
      snap[f.key] = f.source === 'server' ? getServerValueWithDefault(f.key) : localConfig[f.key]
    }
  }

  snapshot.value = snap
  Object.assign(localValues, snap)

  // Init side effects (e.g., fetch FRP info)
  sideEffects.init()
})

// ── Enable toggle ──

const fieldsDisabled = computed(() => {
  if (!config.value.enableKey) return false
  return !localValues[config.value.enableKey]
})

function onEnableToggle(e: Event) {
  const checked = (e.target as HTMLInputElement).checked
  if (config.value.enableKey) {
    localValues[config.value.enableKey] = checked
  }
}

// ── Entry selector ──

const entryOptions = computed(() => {
  return config.value.entrySelector?.options ?? []
})

const entryDisplayLabel = computed(() => {
  const es = config.value.entrySelector
  if (!es) return ''
  const val = localValues[es.key]
  const opt = entryOptions.value.find(o => o.value === val)
  return opt ? t(opt.labelKey) : String(val ?? '')
})

function handleEntrySelect(value: any) {
  const es = config.value.entrySelector
  if (!es) return
  const prevValue = localValues[es.key]
  localValues[es.key] = value

  // Auto-reset TTS voice when engine changes
  if (sideEffects.needsVoiceReset.value && es.key === 'tts.engine' && value !== prevValue) {
    const voiceOpts = engineVoiceOptions[value] ?? []
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
  const cfg = config.value
  const result: RenderEntry[] = []

  // Common fields
  for (const f of cfg.commonFields) {
    if (f.sectionHeader) {
      result.push({ type: 'header', key: `header-${f.key}`, label: t(f.sectionHeader) })
    }
    result.push({ type: 'field', key: f.key, field: f })
  }

  // Option sub-fields based on current entry selector value or frp.auto_port
  const entryVal = cfg.entrySelector ? localValues[cfg.entrySelector.key] : undefined
  // For FRP: optionSubFields is keyed by frp.auto_port value
  const subFieldKey = cfg.categoryId === 'frp' ? localValues['frp.auto_port'] : entryVal
  const osf = (cfg.optionSubFields ?? []).find(o => o.when === subFieldKey)
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

// ── Field value helpers ──

function getLocalValue(field: ItemSpec): any {
  const k = field.key
  if (k in localValues) return localValues[k]
  return field.source === 'server' ? getServerValueWithDefault(k) : localConfig[k]
}

function setLocalValue(key: string, value: any) {
  localValues[key] = value
}

function resolveFieldOptions(field: ItemSpec): { label: string; value: any }[] | undefined {
  // Dynamic voice options based on current engine
  if (field.key === 'tts.voice') {
    const engine = localValues['tts.engine'] || 'edge'
    const voiceOpts = engineVoiceOptions[engine] ?? []
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
  if (props.categoryId !== 'frp') return false
  return localValues['frp.auto_port'] === true && localValues['frp.enabled'] === true
})

const frpHttpPortDisplay = computed(() => {
  const info = sideEffects.frpAutoPortInfo.value
  if (!info) return ''
  const port = info.state === 'running' && info.remotePort > 0 ? info.remotePort : 0
  return port > 0 ? port : '—'
})

const frpSshPortDisplay = computed(() => {
  const info = sideEffects.frpAutoPortInfo.value
  if (!info) return 0
  return info.state === 'running' && info.sshRemotePort > 0 ? info.sshRemotePort : 0
})

// ── Diff & validation ──

/** Find field spec by key across all config fields */
function findFieldSpec(key: string): ItemSpec | undefined {
  const cfg = config.value
  if (cfg.entrySelector?.key === key) return cfg.entrySelector
  for (const f of cfg.commonFields) {
    if (f.key === key) return f
  }
  for (const osf of cfg.optionSubFields ?? []) {
    for (const f of osf.fields) {
      if (f.key === key) return f
    }
  }
  return undefined
}

/** Get all field keys in the config */
function getAllFieldKeys(): string[] {
  const cfg = config.value
  const keys: string[] = []
  if (cfg.enableKey) keys.push(cfg.enableKey)
  if (cfg.entrySelector) keys.push(cfg.entrySelector.key)
  for (const f of cfg.commonFields) keys.push(f.key)
  for (const osf of cfg.optionSubFields ?? []) {
    for (const f of osf.fields) keys.push(f.key)
  }
  return keys
}

const hasChanges = computed(() => {
  const snap = snapshot.value
  for (const key of getAllFieldKeys()) {
    const localVal = localValues[key]
    const snapVal = snap[key]
    // Skip password fields that are empty (user didn't re-enter)
    const spec = findFieldSpec(key)
    if (spec?.type === 'password' && (localVal === '' || localVal === null || localVal === undefined)) continue
    if (localVal !== snapVal) return true
  }
  return false
})

const canSave = computed(() => {
  const cfg = config.value
  const required = cfg.requiredFields ?? []
  for (const key of required) {
    const val = localValues[key]
    if (val === '' || val === null || val === undefined) return false
  }
  return true
})

const needsRestartHint = computed(() => {
  const snap = snapshot.value
  for (const key of getAllFieldKeys()) {
    if (localValues[key] !== snap[key]) {
      const spec = findFieldSpec(key)
      if (spec?.needsRestart) return true
    }
  }
  return false
})

// ── Deep set by dot-path ──

function deepSetByDotPath(obj: Record<string, any>, dotPath: string, value: any) {
  const parts = dotPath.split('.')
  let current: any = obj
  for (let i = 0; i < parts.length - 1; i++) {
    if (current[parts[i]] == null) current[parts[i]] = {}
    current = current[parts[i]]
  }
  current[parts[parts.length - 1]] = value
}

// ── Save ──

async function handleSave() {
  const snap = snapshot.value
  serverError.value = ''

  const allKeys = getAllFieldKeys()
  const serverChanges: Record<string, any> = {}
  const localChanges: [string, any][] = []
  const changedKeys: string[] = []

  for (const key of allKeys) {
    const localVal = localValues[key]
    const snapVal = snap[key]
    const spec = findFieldSpec(key)

    // Skip password fields with empty value — preserve existing server-side value
    if (spec?.type === 'password' && (localVal === '' || localVal === null || localVal === undefined)) continue

    if (localVal !== snapVal) {
      changedKeys.push(key)
      if (spec?.source === 'server') {
        deepSetByDotPath(serverChanges, key, localVal)
      } else {
        localChanges.push([key, localVal])
      }
    }
  }

  // Flush local changes
  for (const [key, value] of localChanges) {
    setLocalConfig(key, value)
  }

  // Flush server changes
  if (Object.keys(serverChanges).length > 0) {
    saving.value = true
    try {
      const result = await patchConfig(serverChanges)
      saving.value = false

      // Update snapshot to committed state
      for (const key of changedKeys) {
        snapshot.value[key] = localValues[key]
      }

      toast.show(t('settings.drillDown.saved'), { icon: '✓', type: 'success', duration: 3000 })

      // Side effects
      sideEffects.afterSave(changedKeys)

      // Emit restart needed from server response
      if (result.needsRestart && result.changedColdFields.length > 0) {
        emit('restartNeeded', result.changedColdFields)
      }

      hasFailedSave.value = false
      emit('back')
    } catch (err: any) {
      saving.value = false
      serverError.value = err?.message || t('settings.saveFailed')
      hasFailedSave.value = true
      // Stay on page so user can retry
    }
  } else {
    // Only local changes — already flushed, just finalize
    for (const key of changedKeys) {
      snapshot.value[key] = localValues[key]
    }

    toast.show(t('settings.drillDown.saved'), { icon: '✓', type: 'success', duration: 3000 })
    sideEffects.afterSave(changedKeys)
    hasFailedSave.value = false
    emit('back')
  }
}

// ── Discard confirmation on back ──

function requestBack() {
  if (hasChanges.value || hasFailedSave.value) {
    dialog.confirm(
      t('settings.drillDown.unsavedMessage'),
      {
        title: t('settings.drillDown.unsavedTitle'),
        confirmText: t('settings.drillDown.discard'),
        cancelText: t('settings.drillDown.continueEditing'),
      },
    ).then((confirmed) => {
      if (confirmed) emit('back')
    })
  } else {
    emit('back')
  }
}

// ── Expose ──

defineExpose({ requestBack })
</script>

<style scoped>
.drill-down {
  background: var(--bg-secondary);
  padding: 4px 0;
  padding-bottom: calc(60px + env(safe-area-inset-bottom, 0px));
  min-height: 100%;
}

/* Enable toggle row */
.drill-down__enable-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  min-height: 48px;
  background: var(--bg-primary);
  position: relative;
}

.drill-down__enable-row::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.drill-down__enable-left {
  display: flex;
  align-items: center;
  gap: 6px;
}

.drill-down__enable-label {
  font-size: 15px;
  color: var(--text-primary);
}

/* Status dot */
.drill-down__status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.drill-down__status-dot--green {
  background: #22c55e;
}

.drill-down__status-dot--yellow {
  background: #eab308;
  animation: drill-down-pulse 1.5s ease-in-out infinite;
}

.drill-down__status-dot--red {
  background: #ef4444;
}

@keyframes drill-down-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

/* iOS-style switch toggle */
.drill-down__switch {
  position: relative;
  display: inline-block;
  width: 51px;
  height: 31px;
  cursor: pointer;
}

.drill-down__switch-input {
  opacity: 0;
  width: 0;
  height: 0;
  position: absolute;
}

.drill-down__switch-track {
  position: absolute;
  inset: 0;
  border-radius: 15.5px;
  background: var(--bg-tertiary);
  transition: background 0.2s ease;
}

.drill-down__switch-track::after {
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

.drill-down__switch-input:checked + .drill-down__switch-track {
  background: var(--color-green);
}

.drill-down__switch-input:checked + .drill-down__switch-track::after {
  transform: translateX(20px);
}

/* Entry selector row */
.drill-down__entry-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  min-height: 48px;
  cursor: pointer;
  background: var(--bg-primary);
  position: relative;
}

.drill-down__entry-row::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.drill-down__entry-row--disabled {
  opacity: 0.5;
  pointer-events: none;
}

@media (hover: hover) {
  .drill-down__entry-row:hover {
    background: var(--bg-tertiary);
  }
}

.drill-down__entry-row:active {
  background: var(--bg-tertiary);
}

.drill-down__entry-label {
  font-size: 15px;
  color: var(--text-primary);
  flex-shrink: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.drill-down__entry-right {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.drill-down__entry-value {
  font-size: 14px;
  color: var(--text-secondary);
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.drill-down__entry-chevron {
  color: var(--text-muted);
  flex-shrink: 0;
}

/* Section header */
.drill-down__section-header {
  font-size: 12px;
  color: var(--text-muted);
  padding: 10px 16px 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
}

/* Fixed bottom save bar */
.drill-down__save-bar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  background: var(--bg-primary);
  border-top: 0.5px solid var(--border-color);
  padding: 8px 16px;
  padding-bottom: calc(8px + env(safe-area-inset-bottom, 0px));
  z-index: 10;
}

.drill-down__restart-hint {
  font-size: 12px;
  color: var(--text-muted);
  text-align: center;
  margin-bottom: 6px;
}

.drill-down__save-btn {
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

.drill-down__save-btn--accent {
  background: var(--accent-color);
  color: #fff;
}

.drill-down__save-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

@media (hover: hover) {
  .drill-down__save-btn--accent:hover:not(:disabled) {
    background: var(--accent-hover);
  }
}

.drill-down__save-btn--accent:active:not(:disabled) {
  background: var(--accent-hover);
}

/* Server error */
.drill-down__error {
  padding: 8px 16px;
  font-size: 13px;
  color: #ef4444;
  background: var(--bg-primary);
}
</style>

<!-- Non-scoped styles for BottomSheet-teleported option rows -->
<style>
.drill-down__option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  cursor: pointer;
  min-height: 44px;
  position: relative;
}

.drill-down__option::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.drill-down__option:last-child::after {
  display: none;
}

@media (hover: hover) {
  .drill-down__option:hover {
    background: var(--bg-tertiary);
  }
}

.drill-down__option:active {
  background: var(--bg-tertiary);
}

.drill-down__option--active {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, var(--bg-primary, #fff));
}

.drill-down__option-label {
  font-size: 15px;
  color: var(--text-primary);
}

.drill-down__option-check {
  font-size: 15px;
  color: var(--accent-color);
  font-weight: 600;
}
</style>
