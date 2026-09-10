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
      <span v-if="refreshable" class="settings-item__refresh refresh-spin" :class="{ 'refresh-spin--active': refreshing }" @click.stop="emit('refresh')">
        <RefreshCw :size="12" />
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
    @close="closeSelectPicker()"
  >
    <template #header>
      <ChevronsUpDown :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ label }}</span>
    </template>
    <template v-if="selectGroups">
      <template v-for="(group, gi) in selectGroups" :key="group.groupKey ?? gi">
        <div v-if="group.groupKey" class="settings-item__option-group">
          {{ t(group.groupKey) }}
        </div>
        <div
          v-for="opt in group.options"
          :key="opt.value as PropertyKey"
          class="settings-item__option"
          :class="{ 'settings-item__option--active': modelValue === opt.value }"
          @click="selectOption(opt.value)"
        >
          <span class="settings-item__option-label" :style="opt.previewFont ? { fontFamily: opt.previewFont } : undefined">{{ opt.label }}</span>
          <span v-if="opt.badgeKey" class="settings-item__option-badge">{{ t(opt.badgeKey) }}</span>
          <span v-if="modelValue === opt.value" class="settings-item__option-check">✓</span>
        </div>
      </template>
    </template>
    <template v-else>
      <div
        v-for="opt in renderOptions"
        :key="opt.value as PropertyKey"
        class="settings-item__option"
        :class="{ 'settings-item__option--active': modelValue === opt.value }"
        @click="selectOption(opt.value)"
      >
        <ProviderIcon v-if="opt.modelName" :model-name="opt.modelName" :size="14" />
        <span class="settings-item__option-label" :style="opt.previewFont ? { fontFamily: opt.previewFont } : undefined">{{ opt.label }}</span>
        <span v-if="modelValue === opt.value" class="settings-item__option-check">✓</span>
      </div>
    </template>
  </BottomSheet>
  <!-- Theme picker BottomSheet with color preview swatches -->
  <BottomSheet
    v-if="type === 'select' && isThemeSelect"
    :open="themePicker.effectiveOpen.value"
    auto
    @close="themePicker.close()"
  >
    <template #header>
      <Palette :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ label }}</span>
    </template>
    <div class="theme-picker-grid" :class="{ 'theme-picker-grid--wide': isTerminalThemeSelect }">
      <div
        v-if="terminalThemeLoadError"
        class="theme-picker-error"
      >
        <span class="theme-picker-error-text">{{ t('settings.items.terminalThemeLoadFailed') }}</span>
        <button class="theme-picker-error-retry" @click.stop="onRetryTerminalThemes?.()">
          {{ t('common.retry') }}
        </button>
      </div>
      <div
        v-for="opt in options"
        :key="opt.value as PropertyKey"
        class="theme-picker-cell"
        :class="{ 'theme-picker-cell--active': modelValue === opt.value }"
        @click="selectOption(opt.value)"
      >
        <div
          v-if="terminalPreviewFor(opt)"
          class="theme-picker-swatch theme-picker-swatch--terminal"
        >
          <TerminalPreviewCard
            :theme="terminalPreviewFor(opt)?.theme"
            :auto="opt.value === 'auto'"
          />
        </div>
        <div
          v-else
          class="theme-picker-swatch"
          :class="{ 'theme-picker-swatch--auto': opt.value === 'auto', 'theme-picker-swatch--label': previewFor(opt)?.type === 'color' }"
          :style="previewStyleFor(opt)"
        >
          <span class="theme-picker-swatch-label">
            <span class="theme-picker-swatch-label-text">{{ opt.label }}</span>
          </span>
          <component
            :is="themeBaseIconFor(opt)"
            :size="12"
            class="theme-picker-swatch-base-icon"
          />
        </div>
        <span v-if="terminalPreviewFor(opt)" class="theme-picker-cell-label">{{ opt.label }}</span>
        <span v-if="modelValue === opt.value" class="theme-picker-cell-check">✓</span>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Eye, EyeOff, RefreshCw, ChevronsUpDown, Sun, Moon, Palette } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'
import TerminalPreviewCard from '@/components/common/TerminalPreviewCard.vue'
import { isDarkTheme, resolveThemeId } from '@/utils/themeMeta'

const { t } = useI18n()

/** App 主题卡片预览（三色色块 + 主题名样例文字）。 */
export interface ColorPreview { type: 'color'; bg: string; text: string; accent: string; themeId: string }
/** 终端主题卡片预览（迷你终端）。theme 懒加载完成前可为 undefined（渲染骨架占位）。 */
export interface TerminalPreview { type: 'terminal'; themeId: string; theme?: import('@xterm/xterm').ITheme }
export type OptionPreview = ColorPreview | TerminalPreview

interface Props {
  label: string
  description?: string
  type: 'switch' | 'select' | 'number' | 'text' | 'slider' | 'action' | 'info' | 'header' | 'password' | 'textarea'
  modelValue?: unknown
  options?: SelectOption[]
  /** Async filter applied to options each time the select picker opens
   *  (e.g. font availability probing). When absent, options render as-is. */
  optionsFilter?: (options: SelectOption[]) => Promise<SelectOption[]>
  /** Optional per-option previews — when provided, the select renders as a
   *  theme grid picker instead of a plain option list. Two kinds supported:
   *  color (bg/text/accent swatch) and terminal (mini terminal preview). */
  optionPreviews?: Record<string, OptionPreview>
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
  /** Terminal theme lazy-load failed — show a retry banner in the terminal grid. */
  terminalThemeLoadError?: boolean
  /** Retry handler for a failed terminal theme lazy-load. */
  onRetryTerminalThemes?: () => void
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: undefined,
  options: undefined,
  optionPreviews: undefined,
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
}>()

const editing = ref(false)
const editValue = ref<unknown>(null)
const showPassword = ref(false)
const selectPicker = useTabDrawer('settings', { autoRestore: false })
// Same tab id as selectPicker so effectiveOpen resolves on the settings page.
// Only one of the two drawers is used per select (themed grid vs plain list).
const themePicker = useTabDrawer('settings', { autoRestore: false })

/** Whether this select should render the theme grid picker (has per-option previews). */
const isThemeSelect = computed(() => !!props.optionPreviews)

/** Whether this theme select uses terminal preview cards (any option is terminal-typed). */
const isTerminalThemeSelect = computed(() =>
  !!props.optionPreviews &&
  Object.values(props.optionPreviews).some(p => p.type === 'terminal')
)

export interface SelectOption {
  label: string
  value: unknown
  modelName?: string
  groupKey?: string
  badgeKey?: string
  /** CSS font-family used to render this option's label in its own font. */
  previewFont?: string
}
interface SelectGroup { groupKey?: string; options: SelectOption[] }

/** Options actually rendered: props.options, replaced by the async filter
 *  result while the picker is open (null until a filter has produced output). */
const displayOptions = ref<SelectOption[] | null>(null)

/** The options list used for rendering (filtered when a filter produced output). */
const renderOptions = computed<SelectOption[] | undefined>(() => displayOptions.value ?? (props.options as SelectOption[] | undefined))

/** Reset the filtered snapshot (called whenever the plain select closes). */
function resetDisplayOptions() {
  displayOptions.value = null
}

/** Kick off the async options filter (if any) and store its result. */
function refreshDisplayOptions() {
  resetDisplayOptions()
  const opts = props.options as SelectOption[] | undefined
  if (!props.optionsFilter || !opts) return
  void props.optionsFilter(opts).then((filtered) => {
    displayOptions.value = filtered
  }).catch(() => { /* keep unfiltered list on failure */ })
}

/** Close the plain select picker and clear the filtered snapshot. */
function closeSelectPicker() {
  selectPicker.close()
  resetDisplayOptions()
}

/** Group the plain select options by groupKey (when present). Options without a
 *  groupKey render as a single flat list — unchanged legacy behavior. */
const selectGroups = computed<SelectGroup[] | null>(() => {
  const opts = renderOptions.value
  if (!opts?.some(o => o.groupKey)) return null
  const groups: SelectGroup[] = []
  for (const opt of opts) {
    const last = groups[groups.length - 1]
    if (!last || last.groupKey !== opt.groupKey) groups.push({ groupKey: opt.groupKey, options: [opt] })
    else last.options.push(opt)
  }
  return groups
})

/** Look up the preview payload for an option value. */
function previewFor(opt: { label: string; value: unknown }): OptionPreview | undefined {
  return props.optionPreviews?.[String(opt.value)]
}

/** Narrow an option's preview to a terminal preview (undefined for color/absent). */
function terminalPreviewFor(opt: { label: string; value: unknown }): TerminalPreview | undefined {
  const p = previewFor(opt)
  return p?.type === 'terminal' ? p : undefined
}

/** Build the inline style for a color swatch. Auto option gets a light/dark split. */
function previewStyleFor(opt: { label: string; value: unknown }): Record<string, string> {
  if (opt.value === 'auto') {
    return {
      background:
        'linear-gradient(135deg, #ffffff 0%, #ffffff 48%, #1a1a2e 52%, #1a1a2e 100%)',
    }
  }
  const p = props.optionPreviews?.[String(opt.value)]
  if (!p || p.type !== 'color') return {}
  return {
    background: p.bg,
    color: p.text,
    '--swatch-accent': p.accent,
  }
}

/**
 * Light/dark indicator icon for a color swatch (sun for light, moon for dark).
 * Auto follows the resolved system theme, mirroring the AppHeader theme menu.
 */
function themeBaseIconFor(opt: { label: string; value: unknown }) {
  const themeId = opt.value === 'auto' ? resolveThemeId('auto') : String(opt.value)
  return isDarkTheme(themeId) ? Moon : Sun
}

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
    closeSelectPicker()
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
  // select: open BottomSheet picker (theme grid when previews are provided)
  if (props.type === 'select') {
    if (isThemeSelect.value) {
      themePicker.open()
      selectPicker.close()
    } else {
      selectPicker.open()
      themePicker.close()
      refreshDisplayOptions()
    }
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
  closeSelectPicker()
  themePicker.close()
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

.settings-item__refresh.refresh-spin--active {
  color: var(--accent-color);
}

@media (hover: hover) {
  .settings-item__refresh:hover {
    color: var(--accent-color);
  }
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

/* Group header inside the select option list (font picker). */
.settings-item__option-group {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  padding: 10px 16px 4px;
  background: var(--bg-secondary);
}

/* Small badge (e.g. "内置" for bundled fonts). */
.settings-item__option-badge {
  font-size: 11px;
  line-height: 1;
  padding: 3px 6px;
  border-radius: 6px;
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  flex-shrink: 0;
  white-space: nowrap;
}

/* ── Theme picker grid ── */
.theme-picker-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 10px;
  padding: 12px 16px 20px;
}

.theme-picker-grid--wide {
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
}

/* Terminal theme lazy-load failure banner (spans full grid width) */
.theme-picker-error {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 8px;
  background: color-mix(in srgb, #ef4444 10%, var(--bg-secondary));
  border: 1px solid color-mix(in srgb, #ef4444 30%, transparent);
}

.theme-picker-error-text {
  font-size: 12px;
  color: #ef4444;
  line-height: 1.4;
}

.theme-picker-error-retry {
  flex-shrink: 0;
  padding: 4px 12px;
  border: none;
  border-radius: 6px;
  background: var(--accent-color);
  color: #fff;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}

@media (hover: hover) {
  .theme-picker-error-retry:hover {
    background: var(--accent-hover);
  }
}

.theme-picker-swatch--terminal {
  height: 88px;
  border: none;
  background: transparent;
}

.theme-picker-cell {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 6px;
  cursor: pointer;
  position: relative;
}

.theme-picker-swatch {
  height: 64px;
  border-radius: 8px;
  border: 1px solid var(--border-color);
  position: relative;
  overflow: hidden;
  transition: box-shadow 0.15s ease, transform 0.15s ease;
}

.theme-picker-swatch--auto {
  /* light/dark split is set via inline background */
  border: 2px dashed var(--border-color);
}

/* Light/dark indicator icon in the top-right corner of a color swatch */
.theme-picker-swatch-base-icon {
  position: absolute;
  top: 5px;
  right: 5px;
  color: var(--swatch-accent, var(--text-muted));
  flex-shrink: 0;
}

.theme-picker-swatch-label {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 4px 6px;
}

.theme-picker-swatch-label-text {
  font-size: 11px;
  font-weight: 500;
  line-height: 1.3;
  color: inherit;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
}

.theme-picker-swatch--auto .theme-picker-swatch-label-text {
  color: #666666;
}

.theme-picker-cell--active .theme-picker-swatch {
  box-shadow: 0 0 0 2px var(--accent-color);
  transform: scale(1.02);
}

.theme-picker-cell-label {
  font-size: 12px;
  color: var(--text-secondary);
  text-align: center;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.theme-picker-cell-check {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--accent-color);
  color: #fff;
  font-size: 12px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1;
}

@media (hover: hover) {
  .theme-picker-cell:hover .theme-picker-swatch {
    box-shadow: 0 0 0 1px var(--accent-color);
  }
}
</style>
