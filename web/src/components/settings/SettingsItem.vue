<template>
  <div v-if="type === 'header'" class="settings-item__header">{{ label }}</div>
  <div v-else class="settings-item" :class="{ 'settings-item--disabled': disabled, 'settings-item--no-divider': noDivider }" @click="handleClick">
    <div class="settings-item__left">
      <div class="settings-item__text">
        <span class="settings-item__label">{{ label }}</span>
        <span v-if="needsRestart" class="settings-item__badge">{{ t('settings.needsRestart') }}</span>
      </div>
    </div>
    <div class="settings-item__right">
      <template v-if="type === 'switch'">
        <label class="settings-item__switch">
          <input
            type="checkbox"
            class="settings-item__switch-input"
            :checked="!!modelValue"
            :disabled="disabled"
            @change="onSwitchChange"
            @click.stop
          />
          <span class="settings-item__switch-track"></span>
        </label>
      </template>
      <template v-else-if="type === 'slider'">
        <span class="settings-item__slider-value">{{ sliderDisplayValue }}</span>
        <input
          type="range"
          class="settings-item__slider"
          :value="modelValue"
          :min="min"
          :max="max"
          :step="step"
          :disabled="disabled"
          @input="onSliderInput"
          @click.stop
        />
        <button v-if="defaultValue !== undefined && modelValue !== defaultValue" class="settings-item__slider-reset" @click.stop="resetSlider" :title="t('settings.items.resetToDefault')">↺</button>
      </template>
      <template v-else-if="type === 'password'">
        <span class="settings-item__value">{{ displayValue }}</span>
      </template>
      <template v-else-if="type === 'select' || type === 'number' || type === 'text'">
        <ProviderIcon v-if="selectedOptionModelName" :model-name="selectedOptionModelName" :size="14" />
        <span class="settings-item__value">{{ displayValue }}</span>
      </template>
      <template v-else-if="type === 'textarea'">
        <span class="settings-item__value">{{ displayValue }}</span>
      </template>
      <template v-else-if="type === 'action'">
      </template>
      <template v-else-if="type === 'info'">
        <!-- info value shown in description area below, nothing on the right -->
      </template>
    </div>
    <!-- Inline description (always visible below label row) -->
    <div v-if="description" class="settings-item__desc">{{ description }}</div>
    <!-- Info-type: detail line with action icons (quantity on left, icons on right) -->
    <div v-if="type === 'info' && displayValue" class="settings-item__info-row">
      <span class="settings-item__info-detail">{{ displayValue }}</span>
      <span v-if="refreshable" class="settings-item__refresh" :class="{ 'settings-item__refresh--active': refreshing }" @click.stop="emit('refresh')">
        <RefreshCw :size="12" />
      </span>
      <span v-if="rebuildable" class="settings-item__rebuild" :class="{ 'settings-item__rebuild--active': rebuilding }" :title="rebuildTitle" @click.stop="emit('rebuild')">
        <RotateCcw :size="12" />
      </span>
    </div>
    <!-- Progress bar for info-type items (only when data exists) -->
    <div v-if="type === 'info' && progress && progress.max > 0" class="settings-item__progress">
      <div class="settings-item__progress-track">
        <div class="settings-item__progress-bar" :class="{ 'settings-item__progress-bar--active': !disabled && progress.value < progress.max }" :style="{ width: Math.min((progress.value / progress.max) * 100, 100) + '%' }" />
      </div>
    </div>
  </div>
  <!-- Inline editor (non-select types) -->
  <div v-if="editing && type !== 'select'" class="settings-item__editor" @click.stop>
    <!-- Number editor -->
    <template v-if="type === 'number'">
      <div class="settings-item__input-row">
        <input
          type="number"
          class="settings-item__number-input"
          :value="String(editValue ?? '')"
          :min="min"
          :max="max"
          :step="step"
          @input="editValue = ($event.target as HTMLInputElement).value"
          @keydown.enter="confirmEdit"
        />
        <button class="settings-item__editor-confirm" @click="confirmEdit">{{ t('common.ok') }}</button>
      </div>
    </template>
    <!-- Text editor -->
    <template v-else-if="type === 'text'">
      <div class="settings-item__input-row">
        <input
          type="text"
          class="settings-item__text-input"
          :value="(editValue as string | number | readonly string[] | null | undefined)"
          :placeholder="placeholder"
          @input="editValue = ($event.target as HTMLInputElement).value"
          @keydown.enter="confirmEdit"
        />
        <button class="settings-item__editor-confirm" @click="confirmEdit">{{ t('common.ok') }}</button>
      </div>
    </template>
    <!-- Password editor -->
    <template v-else-if="type === 'password'">
      <div class="settings-item__input-row">
        <input
          :type="showPassword ? 'text' : 'password'"
          class="settings-item__text-input"
          :value="editValue"
          :placeholder="placeholder"
          autocomplete="off"
          @input="editValue = ($event.target as HTMLInputElement).value"
          @keydown.enter="confirmEdit"
        />
        <button class="settings-item__editor-toggle" @click="showPassword = !showPassword">
          <EyeOff v-if="showPassword" :size="16" />
          <Eye v-else :size="16" />
        </button>
        <button class="settings-item__editor-confirm" @click="confirmEdit">{{ t('common.ok') }}</button>
      </div>
    </template>
    <!-- Textarea editor -->
    <template v-else-if="type === 'textarea'">
      <div class="settings-item__textarea-row">
        <textarea
          class="settings-item__textarea-input"
          :value="editValue as string | number | readonly string[] | null | undefined"
          :placeholder="placeholder"
          rows="6"
          @input="editValue = ($event.target as HTMLTextAreaElement).value"
        ></textarea>
        <div class="settings-item__textarea-actions">
          <button class="settings-item__editor-confirm" @click="confirmEdit">{{ t('common.ok') }}</button>
        </div>
      </div>
      <div v-if="warning" class="settings-item__textarea-warning">{{ warning }}</div>
    </template>
  </div>
  <!-- Select option picker BottomSheet -->
  <BottomSheet
    v-if="type === 'select'"
    :open="selectPicker.effectiveOpen.value"
    auto
    @close="selectPicker.close()"
  >
    <template #header>
      <ChevronsUpDown :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ label }}</span>
    </template>
    <div
      v-for="opt in options"
      :key="opt.value as PropertyKey"
      class="settings-item__option"
      :class="{ 'settings-item__option--active': modelValue === opt.value }"
      @click="selectOption(opt.value)"
    >
      <ProviderIcon v-if="opt.modelName" :model-name="opt.modelName" :size="14" />
      <span class="settings-item__option-label">{{ opt.label }}</span>
      <span v-if="modelValue === opt.value" class="settings-item__option-check">✓</span>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Eye, EyeOff, RefreshCw, RotateCcw, ChevronsUpDown } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'

const { t } = useI18n()

interface Props {
  label: string
  description?: string
  type: 'switch' | 'select' | 'number' | 'text' | 'slider' | 'action' | 'info' | 'header' | 'password' | 'textarea'
  modelValue?: unknown
  options?: { label: string; value: unknown; modelName?: string }[]
  min?: number
  max?: number
  step?: number
  placeholder?: string
  needsRestart?: boolean
  disabled?: boolean
  forceClose?: boolean
  warning?: string
  noDivider?: boolean
  defaultValue?: unknown
  displayFormat?: 'percent' | 'raw'
  displayTransform?: (value: unknown) => unknown
  /** Progress bar for info-type items: { value, max }. Bar hidden when value >= max. */
  progress?: { value: number; max: number }
  /** Show a refresh icon inside the progress bar area */
  refreshable?: boolean
  /** Refresh animation state */
  refreshing?: boolean
  /** Show a rebuild icon inside the progress bar area */
  rebuildable?: boolean
  /** Rebuild animation state */
  rebuilding?: boolean
  /** Tooltip text for rebuild icon */
  rebuildTitle?: string
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: undefined,
  options: undefined,
  min: undefined,
  max: undefined,
  step: undefined,
  placeholder: '',
  description: '',
  needsRestart: false,
  disabled: false,
  forceClose: false,
  warning: '',
  noDivider: false,
  defaultValue: undefined,
})

const emit = defineEmits<{
  'update:modelValue': [value: unknown]
  click: []
  editToggle: [open: boolean]
  discard: []
  refresh: []
  rebuild: []
}>()

const editing = ref(false)
const editValue = ref<unknown>(null)
const showPassword = ref(false)
const selectPicker = useTabDrawer('settings', { autoRestore: false })

// Slider debounce: only emit final value after 300ms of inactivity
let sliderDebounceTimer: ReturnType<typeof setTimeout> | null = null
const SLIDER_DEBOUNCE_MS = 300

onUnmounted(() => {
  if (sliderDebounceTimer) {
    clearTimeout(sliderDebounceTimer)
    sliderDebounceTimer = null
  }
})

// Close editor when parent forces close (another editor opened)
watch(() => props.forceClose, (val) => {
  if (val && editing.value) {
    // Password editor with modified input: notify parent so it can show feedback
    if (props.type === 'password' && editValue.value !== props.modelValue) {
      emit('discard')
    }
    editing.value = false
    emit('editToggle', false)
  }
  if (val && selectPicker.isOpen.value) {
    selectPicker.close()
    emit('editToggle', false)
  }
})

const displayValue = computed(() => {
  if (props.type === 'password') {
    if (props.modelValue !== undefined && props.modelValue !== '') {
      return '••••••'
    }
    return props.placeholder
  }
  if (props.type === 'textarea') {
    if (props.modelValue && String(props.modelValue).length > 50) {
      return String(props.modelValue).substring(0, 50) + '…'
    }
    return props.modelValue ? String(props.modelValue) : props.placeholder
  }
  if (props.type === 'select' && props.options?.length) {
    const opt = props.options.find(o => o.value === props.modelValue)
    return opt?.label ?? props.modelValue ?? props.placeholder
  }
  if (props.modelValue !== undefined && props.modelValue !== '') {
    const v = props.displayTransform ? props.displayTransform(props.modelValue) : props.modelValue
    return String(v)
  }
  return props.placeholder
})

/** modelName of the currently selected option (for ProviderIcon rendering). */
const selectedOptionModelName = computed(() => {
  if (props.type !== 'select' || !props.options?.length) return null
  const opt = props.options.find(o => o.value === props.modelValue)
  return opt?.modelName ?? null
})

function onSwitchChange(e: Event) {
  const checked = (e.target as HTMLInputElement).checked
  emit('update:modelValue', checked)
}

function onSliderInput(e: Event) {
  const value = Number((e.target as HTMLInputElement).value)
  // Debounce: cancel previous timer, only emit after user stops dragging
  if (sliderDebounceTimer) clearTimeout(sliderDebounceTimer)
  sliderDebounceTimer = setTimeout(() => {
    emit('update:modelValue', value)
    sliderDebounceTimer = null
  }, SLIDER_DEBOUNCE_MS)
}

const sliderDisplayValue = computed(() => {
  if (props.modelValue == null) return ''
  if (props.displayFormat === 'percent') return `${Math.round((props.modelValue as number) * 100)}%`
  return String(props.modelValue)
})

function resetSlider() {
  if (sliderDebounceTimer) {
    clearTimeout(sliderDebounceTimer)
    sliderDebounceTimer = null
  }
  emit('update:modelValue', props.defaultValue)
}

function handleClick() {
  if (props.type === 'header') return
  if (props.type === 'action') {
    emit('click')
    return
  }
  // switch / slider / info: no click action (controls handle their own input)
  if (props.type === 'switch' || props.type === 'slider' || props.type === 'info') {
    return
  }
  // select: open BottomSheet picker
  if (props.type === 'select') {
    selectPicker.open()
    emit('editToggle', true)
    return
  }
  // number / text / password / textarea: toggle inline editor
  editing.value = !editing.value
  if (editing.value) {
    editValue.value = props.modelValue
    showPassword.value = false
  }
  emit('editToggle', editing.value)
}

function selectOption(value: unknown) {
  emit('update:modelValue', value)
  selectPicker.close()
  emit('editToggle', false)
}

function confirmEdit() {
  if (props.type === 'number') {
    const num = Number(editValue.value)
    if (!isNaN(num)) {
      emit('update:modelValue', num)
    }
  } else {
    emit('update:modelValue', editValue.value)
  }
  editing.value = false
  emit('editToggle', false)
}
</script>

<style scoped>
.settings-item {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  min-height: 0;
  cursor: pointer;
  gap: 4px;
  background: var(--bg-primary);
  position: relative;
}

.settings-item::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.settings-item--no-divider::after {
  display: none;
}

.settings-item--disabled {
  opacity: 0.5;
  pointer-events: none;
}

.settings-item__left {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 1;
  min-width: 0;
}

.settings-item__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.settings-item__label {
  font-size: 15px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-item__badge {
  font-size: 11px;
  padding: 1px 6px;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted);
  white-space: nowrap;
  flex-shrink: 0;
}

/* Inline description (always visible below label row) */
.settings-item__desc {
  width: 100%;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.5;
  word-break: break-word;
  margin-top: 0;
}

.settings-item__right {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.settings-item__value {
  font-size: 14px;
  color: var(--text-secondary);
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Info-type: detail row with quantity text and action icons */
.settings-item__info-row {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 6px;
}

.settings-item__info-detail {
  flex: 1;
  min-width: 0;
  font-size: 14px;
  color: var(--text-secondary);
  word-break: break-all;
  line-height: 1.4;
}

/* Progress bar for info-type items (rendered via parent, not in info-row) */
.settings-item__progress {
  width: 100%;
  margin-top: 8px;
}

.settings-item__progress-track {
  height: 3px;
  background: var(--bg-tertiary);
  border-radius: 2px;
  overflow: visible;
  position: relative;
}

.settings-item__progress-bar {
  height: 100%;
  background: var(--accent-color);
  border-radius: 2px;
  transition: width 0.5s ease;
}

.settings-item__progress-bar--active {
  background-image: linear-gradient(
    -45deg,
    rgba(255, 255, 255, 0.15) 25%,
    transparent 25%,
    transparent 50%,
    rgba(255, 255, 255, 0.15) 50%,
    rgba(255, 255, 255, 0.15) 75%,
    transparent 75%
  );
  background-size: 12px 12px;
  animation: progress-stripe 0.8s linear infinite;
}

@keyframes progress-stripe {
  0% { background-position: 0 0; }
  100% { background-position: 12px 0; }
}

/* Refresh icon in info row */
.settings-item__refresh {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 6px;
  margin: -6px 0;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  transition: color 0.15s ease;
}

.settings-item__refresh:hover {
  color: var(--accent-color);
}

.settings-item__refresh--active {
  animation: spin 0.8s linear infinite;
  pointer-events: none;
  color: var(--accent-color);
}

/* Rebuild icon beside progress bar */
.settings-item__rebuild {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 6px;
  margin: -6px 0;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  transition: color 0.15s ease;
}

.settings-item__rebuild:hover {
  color: var(--accent-color);
}

.settings-item__rebuild--active {
  animation: spin 0.8s linear infinite;
  pointer-events: none;
  color: var(--accent-color);
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Section header */
.settings-item__header {
  font-size: 12px;
  color: var(--text-muted);
  padding: 16px 16px 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
}

/* iOS-style switch toggle */
.settings-item__switch {
  position: relative;
  display: inline-block;
  width: 51px;
  height: 31px;
  cursor: pointer;
}

.settings-item__switch-input {
  opacity: 0;
  width: 0;
  height: 0;
  position: absolute;
}

.settings-item__switch-track {
  position: absolute;
  inset: 0;
  border-radius: 15.5px;
  background: var(--bg-tertiary);
  transition: background 0.2s ease;
}

.settings-item__switch-track::after {
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

.settings-item__switch-input:checked + .settings-item__switch-track {
  background: var(--accent-color);
}

.settings-item__switch-input:checked + .settings-item__switch-track::after {
  transform: translateX(20px);
}

/* Slider */
.settings-item__slider-value {
  font-size: 13px;
  color: var(--text-secondary);
  min-width: 36px;
  text-align: right;
}

.settings-item__slider {
  width: 120px;
  cursor: pointer;
  accent-color: var(--accent-color);
}

.settings-item__slider-reset {
  font-size: 14px;
  color: var(--text-muted);
  background: none;
  border: none;
  cursor: pointer;
  padding: 2px 4px;
  line-height: 1;
}

.settings-item__slider-reset:active {
  color: var(--accent-color);
}

/* ── Inline Editor ── */
.settings-item__editor {
  background: var(--bg-primary);
  border-top: 0.5px solid var(--border-color);
  padding: 4px 0;
}

/* Input row (number / text / password) */
.settings-item__input-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
}

.settings-item__number-input,
.settings-item__text-input {
  flex: 1;
  min-width: 0;
  padding: 8px 12px;
  font-size: 14px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  outline: none;
}

.settings-item__number-input:focus,
.settings-item__text-input:focus {
  border-color: var(--accent-color);
}

/* Password toggle button */
.settings-item__editor-toggle {
  flex-shrink: 0;
  padding: 8px;
  border: none;
  border-radius: 8px;
  background: var(--bg-tertiary);
  font-size: 16px;
  cursor: pointer;
  line-height: 1;
}

.settings-item__editor-confirm {
  flex-shrink: 0;
  padding: 8px 16px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color);
  color: #fff;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
}

@media (hover: hover) {
  .settings-item__editor-confirm:hover {
    background: var(--accent-hover);
  }
}

.settings-item__editor-confirm:active {
  background: var(--accent-hover);
}

/* Textarea editor */
.settings-item__textarea-row {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 8px 16px;
}

.settings-item__textarea-input {
  width: 100%;
  min-height: 120px;
  padding: 8px 12px;
  font-size: 13px;
  font-family: inherit;
  line-height: 1.5;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  outline: none;
  resize: vertical;
}

.settings-item__textarea-input:focus {
  border-color: var(--accent-color);
}

.settings-item__textarea-actions {
  display: flex;
  justify-content: flex-end;
}

.settings-item__textarea-warning {
  font-size: 12px;
  color: var(--text-muted);
  padding: 4px 16px 8px;
  line-height: 1.4;
}
</style>

<!-- Non-scoped styles for BottomSheet-teleported select option rows -->
<style>
.settings-item__option {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  cursor: pointer;
  min-height: 44px;
  position: relative;
}

.settings-item__option::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.settings-item__option:last-child::after {
  display: none;
}

@media (hover: hover) {
  .settings-item__option:hover {
    background: var(--bg-tertiary);
  }
}

.settings-item__option:active {
  background: var(--bg-tertiary);
}

.settings-item__option--active {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, var(--bg-primary, #fff));
}

.settings-item__option-label {
  font-size: 15px;
  color: var(--text-primary);
  flex: 1;
  min-width: 0;
}

.settings-item__option-check {
  font-size: 15px;
  color: var(--accent-color);
  font-weight: 600;
  flex-shrink: 0;
  margin-left: auto;
}
</style>
