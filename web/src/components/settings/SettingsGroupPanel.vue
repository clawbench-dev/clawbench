<template>
  <div class="settings-group-wrapper">
  <!-- ── Entry Row (always rendered) ── -->
  <div class="settings-group" :class="{ 'settings-group--expanded': expanded }">
    <!-- Select entry: label | current-value | chevron -->
    <div v-if="group.entryType === 'select'" class="settings-group__entry" @click="handleEntryClick">
      <div class="settings-group__entry-left">
        <span class="settings-group__entry-label">{{ t(group.entryField.labelKey) }}</span>
      </div>
      <div class="settings-group__entry-right">
        <span class="settings-group__entry-value">{{ entryDisplayLabel }}</span>
        <component :is="expanded ? ChevronDown : ChevronRight" :size="16" class="settings-group__chevron" />
      </div>
    </div>
    <!-- Switch entry: label | on/off value | chevron (same layout as select) -->
    <div v-else-if="group.entryType === 'switch'" class="settings-group__entry" @click="handleEntryClick">
      <div class="settings-group__entry-left">
        <span class="settings-group__entry-label">{{ t(group.entryField.labelKey) }}</span>
      </div>
      <div class="settings-group__entry-right">
        <span class="settings-group__entry-value">{{ committedEntryValue ? t('settings.items.switchOn') : t('settings.items.switchOff') }}</span>
        <component :is="expanded ? ChevronDown : ChevronRight" :size="16" class="settings-group__chevron" />
      </div>
    </div>
    <!-- Header entry: title | chevron -->
    <div v-else-if="group.entryType === 'header'" class="settings-group__entry settings-group__entry--header" @click="handleEntryClick">
      <div class="settings-group__entry-left">
        <span class="settings-group__entry-label">{{ t(group.titleKey || group.entryField.labelKey) }}</span>
      </div>
      <div class="settings-group__entry-right">
        <component :is="expanded ? ChevronDown : ChevronRight" :size="16" class="settings-group__chevron" />
      </div>
    </div>
  </div>
  <!-- ── Expanded Panel ── -->
  <div v-if="expanded" class="settings-group__panel">
    <!-- Entry selector inside panel (select: radio list; switch: toggle + label) -->
    <template v-if="group.entryType === 'select'">
      <div
        v-for="opt in entryOptions"
        :key="opt.value"
        class="settings-group__option"
        :class="{ 'settings-group__option--active': localEntryValue === opt.value }"
        @click="handleEntrySelect(opt.value)"
      >
        <span class="settings-group__option-label">{{ t(opt.labelKey) }}</span>
        <span v-if="localEntryValue === opt.value" class="settings-group__option-check">✓</span>
      </div>
    </template>
    <template v-else-if="group.entryType === 'switch'">
      <div class="settings-group__switch-row">
        <span class="settings-group__switch-label">{{ t(group.entryField.labelKey) }}</span>
        <label class="settings-group__switch" @click.stop>
          <input
            type="checkbox"
            class="settings-group__switch-input"
            :checked="!!localEntryValue"
            @change="handlePanelSwitchToggle"
          />
          <span class="settings-group__switch-track"></span>
        </label>
      </div>
    </template>
    <!-- Render all visible fields with section headers injected -->
    <template v-for="entry in panelFields" :key="entry.type === 'header' ? entry.headerKey : entry.field.key">
      <div v-if="entry.type === 'header'" class="settings-group__section-header">{{ entry.label }}</div>
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
        @update:model-value="(v: any) => setLocalValue(entry.field.key, v)"
        @edit-toggle="() => {}"
      />
    </template>
    <!-- Empty hint -->
    <div v-if="panelFields.length === 0" class="settings-group__empty">
      {{ t('settings.items.groupNoConfig') }}
    </div>
    <!-- Save / Cancel -->
    <div class="settings-group__actions">
      <button class="settings-group__btn settings-group__btn--cancel" @click="cancel">{{ t('settings.items.groupCancel') }}</button>
      <button class="settings-group__btn settings-group__btn--save" :disabled="saving" @click="save">
        {{ saving ? t('settings.items.groupSaving') : t('settings.items.groupSave') }}
      </button>
    </div>
  </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'
import SettingsItem from './SettingsItem.vue'
import { type ConfigGroup, type ItemSpec } from './settingsFieldMap'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'

type GroupField = Omit<ItemSpec, 'dependsOn'>

/** Flattened render entry for panel fields (field or injected section header) */
interface PanelFieldEntry {
  type: 'field'
  field: GroupField
}
interface PanelHeaderEntry {
  type: 'header'
  headerKey: string
  label: string
}
type PanelEntry = PanelFieldEntry | PanelHeaderEntry

const props = defineProps<{
  group: ConfigGroup
  /** Resolved config values for all group fields */
  fieldValues: Record<string, any>
  /** Dynamic options for select fields (e.g., tts.voice per engine) */
  fieldOptions?: Record<string, { label: string; value: any }[]>
  /** Accordion: true when another group/item is active */
  forceClose: boolean
}>()

const emit = defineEmits<{
  'save-result': [result: { needsRestart: boolean; changedColdFields: string[] }]
  'expand-toggle': [open: boolean]
}>()

const { t } = useI18n()
const toast = useToast()
const { patchConfig } = useSettingsConfig()

// ── State ──
const expanded = ref(false)
const snapshot = ref<Record<string, any> | null>(null)
const localValues = ref<Record<string, any>>({})
const saving = ref(false)

// ── Computed ──
const entryFieldKey = computed(() => props.group.entryField.key)
const committedEntryValue = computed(() => props.fieldValues[entryFieldKey.value])
const localEntryValue = computed(() => localValues.value[entryFieldKey.value] ?? committedEntryValue.value)
const entryOptions = computed(() => props.group.entryType === 'select' ? props.group.entryField.options ?? [] : [])

/** Display label for the current entry value (collapsed row) */
const entryDisplayLabel = computed(() => {
  if (props.group.entryType !== 'select') return ''
  const val = committedEntryValue.value
  const opt = entryOptions.value.find(o => o.value === val)
  return opt ? t(opt.labelKey) : String(val ?? '')
})

/** Whether the current entry value allows panel expansion */
const canExpand = computed(() => {
  const val = committedEntryValue.value
  return !(props.group.nonExpandValues ?? []).includes(val)
})

/** Common fields visible for the current local entry value */
const commonFieldsVisible = computed((): GroupField[] => {
  const entryVal = localEntryValue.value
  const common = props.group.commonFields ?? []
  const visibleWhen = props.group.commonFieldsVisibleWhen
  if (!visibleWhen) return common
  return visibleWhen.includes(entryVal) ? common : []
})

/** Option-specific fields visible for the current local entry value */
const optionFieldsVisible = computed((): GroupField[] => {
  const entryVal = localEntryValue.value
  const osf = (props.group.optionSubFields ?? []).find(o => o.when === entryVal)
  return osf?.fields ?? []
})

/** Flattened panel fields with section headers injected */
const panelFields = computed((): PanelEntry[] => {
  const result: PanelEntry[] = []
  const common = commonFieldsVisible.value
  const option = optionFieldsVisible.value

  // Common fields
  for (const f of common) {
    result.push({ type: 'field', field: f })
  }

  // Option-specific fields, with section headers injected
  for (const f of option) {
    if (f.sectionHeader) {
      result.push({ type: 'header', headerKey: `header-${f.key}`, label: t(f.sectionHeader) })
    }
    result.push({ type: 'field', field: f })
  }

  return result
})

// ── Local value helpers ──
function getLocalValue(field: GroupField): any {
  const k = field.key
  if (k in localValues.value) return localValues.value[k]
  return props.fieldValues[k] ?? ''
}

function setLocalValue(key: string, value: any) {
  localValues.value[key] = value
}

function resolveFieldOptions(field: GroupField): { label: string; value: any }[] | undefined {
  // Dynamic options from fieldOptions prop (e.g., tts.voice)
  if (props.fieldOptions && field.key in props.fieldOptions) {
    return props.fieldOptions[field.key]
  }
  // Static options from field spec
  if (field.options) {
    return field.options.map(o => ({ label: t(o.labelKey), value: o.value }))
  }
  return undefined
}

// ── Entry interactions ──
function handleEntryClick() {
  if (expanded.value) {
    cancel()
  } else if (canExpand.value) {
    expand()
  } else if (props.group.entryType === 'switch') {
    // Switch is OFF (in nonExpandValues) — click entry row to turn ON and expand
    expand()
    localValues.value[entryFieldKey.value] = true
  }
}

function handleEntrySelect(value: any) {
  localValues.value[entryFieldKey.value] = value
  // If selected value is in nonExpandValues, auto-cancel (e.g., selected "disabled")
  if ((props.group.nonExpandValues ?? []).includes(value)) {
    cancel()
  }
}

// ── Expand / Cancel / Save ──
function expand() {
  snapshot.value = JSON.parse(JSON.stringify(props.fieldValues))
  localValues.value = { ...props.fieldValues }
  expanded.value = true
  emit('expand-toggle', true)
}

/** Switch toggle inside expanded panel */
function handlePanelSwitchToggle(e: Event) {
  const checked = (e.target as HTMLInputElement).checked
  localValues.value[entryFieldKey.value] = checked
}

function cancel() {
  snapshot.value = null
  expanded.value = false
  emit('expand-toggle', false)
}

async function save() {
  if (!snapshot.value) return

  // Diff localValues vs snapshot for visible fields only.
  // IMPORTANT: Diff against snapshot, NOT serverConfig. Snapshot captures committed
  // state at expand time. External changes to non-diffed fields are preserved by
  // deepAssign in patchConfig. External changes to diffed fields are last-write-wins
  // (user intent).
  const visibleKeys = getVisibleFieldKeys()
  const changes: Record<string, any> = {}

  for (const key of visibleKeys) {
    const localVal = localValues.value[key]
    const snapVal = snapshot.value[key]
    // Skip password fields that are empty (user didn't re-enter)
    const fieldSpec = findFieldSpec(key)
    if (fieldSpec?.type === 'password' && (localVal === '' || localVal === null || localVal === undefined)) continue
    if (localVal !== snapVal) {
      deepSetByDotPath(changes, key, localVal)
    }
  }

  if (Object.keys(changes).length === 0) {
    cancel()
    return
  }

  saving.value = true
  try {
    const result = await patchConfig(changes)
    snapshot.value = null
    expanded.value = false
    emit('expand-toggle', false)
    emit('save-result', result)
  } catch {
    toast.show(t('settings.saveFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
    // Keep panel open so user can retry or cancel
  } finally {
    saving.value = false
  }
}

// ── Helpers ──
/** Get all field keys visible for the current local entry value */
function getVisibleFieldKeys(): string[] {
  const keys: string[] = [entryFieldKey.value]
  const entryVal = localEntryValue.value
  // Common fields
  const commonVisible = props.group.commonFieldsVisibleWhen
    ? props.group.commonFieldsVisibleWhen.includes(entryVal)
    : true
  if (commonVisible) {
    for (const f of props.group.commonFields ?? []) keys.push(f.key)
  }
  // Option-specific fields
  const osf = (props.group.optionSubFields ?? []).find(o => o.when === entryVal)
  if (osf) {
    for (const f of osf.fields) keys.push(f.key)
  }
  return keys
}

/** Find field spec by key across all group fields */
function findFieldSpec(key: string): GroupField | ItemSpec | undefined {
  if (props.group.entryField.key === key) return props.group.entryField
  for (const f of props.group.commonFields ?? []) {
    if (f.key === key) return f
  }
  for (const osf of props.group.optionSubFields ?? []) {
    for (const f of osf.fields) {
      if (f.key === key) return f
    }
  }
  return undefined
}

/** Set a value in a nested object by dot-path: deepSetByDotPath(obj, 'a.b.c', 1) → { a: { b: { c: 1 } } } */
function deepSetByDotPath(obj: Record<string, any>, dotPath: string, value: any) {
  const parts = dotPath.split('.')
  let current: any = obj
  for (let i = 0; i < parts.length - 1; i++) {
    if (current[parts[i]] == null) current[parts[i]] = {}
    current = current[parts[i]]
  }
  current[parts[parts.length - 1]] = value
}

// ── forceClose watch ──
watch(() => props.forceClose, (val) => {
  if (val && expanded.value) {
    cancel()
  }
})
</script>

<style scoped>
.settings-group__entry {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  min-height: 48px;
  cursor: pointer;
  gap: 12px;
  background: var(--bg-primary);
  position: relative;
}

.settings-group__entry::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.settings-group__entry--header {
  font-size: 12px;
  color: var(--text-muted);
  padding: 16px 16px 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
}

.settings-group__entry-left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 1;
  min-width: 0;
}

.settings-group__entry-label {
  font-size: 15px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-group__entry-right {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.settings-group__entry-value {
  font-size: 14px;
  color: var(--text-secondary);
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.settings-group__chevron {
  color: var(--text-muted);
  flex-shrink: 0;
}

/* Panel */
.settings-group__panel {
  background: var(--bg-secondary);
  padding: 4px 0;
  border-top: 0.5px solid var(--border-color);
}

/* Select option rows inside panel */
.settings-group__option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  cursor: pointer;
  min-height: 40px;
  background: var(--bg-primary);
}

@media (hover: hover) {
  .settings-group__option:hover {
    background: var(--bg-secondary);
  }
}

.settings-group__option:active {
  background: var(--bg-tertiary);
}

.settings-group__option--active {
  background: var(--bg-secondary);
}

.settings-group__option-label {
  font-size: 14px;
  color: var(--text-primary);
}

.settings-group__option-check {
  font-size: 15px;
  color: var(--accent-color);
  font-weight: 600;
}

/* Switch inside panel */
.settings-group__switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  background: var(--bg-primary);
}

.settings-group__switch-label {
  font-size: 15px;
  color: var(--text-primary);
}

/* iOS-style switch toggle */
.settings-group__switch {
  position: relative;
  display: inline-block;
  width: 51px;
  height: 31px;
  cursor: pointer;
}

.settings-group__switch-input {
  opacity: 0;
  width: 0;
  height: 0;
  position: absolute;
}

.settings-group__switch-track {
  position: absolute;
  inset: 0;
  border-radius: 15.5px;
  background: var(--bg-tertiary);
  transition: background 0.2s ease;
}

.settings-group__switch-track::after {
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

.settings-group__switch-input:checked + .settings-group__switch-track {
  background: var(--color-green);
}

.settings-group__switch-input:checked + .settings-group__switch-track::after {
  transform: translateX(20px);
}

/* Section header inside panel */
.settings-group__section-header {
  font-size: 12px;
  color: var(--text-muted);
  padding: 12px 16px 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
}

/* Empty hint */
.settings-group__empty {
  padding: 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
}

/* Save / Cancel buttons */
.settings-group__actions {
  display: flex;
  gap: 8px;
  padding: 8px 16px;
  padding-bottom: calc(8px + env(safe-area-inset-bottom, 0px));
  justify-content: flex-end;
}

.settings-group__btn {
  padding: 8px 16px;
  border: none;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
}

.settings-group__btn--cancel {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

@media (hover: hover) {
  .settings-group__btn--cancel:hover {
    background: var(--bg-secondary);
  }
}

.settings-group__btn--save {
  background: var(--accent-color);
  color: #fff;
}

.settings-group__btn--save:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

@media (hover: hover) {
  .settings-group__btn--save:hover:not(:disabled) {
    background: var(--accent-hover);
  }
}

.settings-group__btn--save:active:not(:disabled) {
  background: var(--accent-hover);
}
</style>
