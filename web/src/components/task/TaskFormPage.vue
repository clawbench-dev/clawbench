<template>
  <div class="task-form-page">
    <!-- Compact header: breadcrumb -->
    <div class="form-header">
      <TaskBreadcrumb />
    </div>

    <!-- Scrollable form content -->
    <div class="form-scroll">
      <div v-if="saving" class="saving-indicator">
        <Loader2 class="spin-icon" :size="14" />
        {{ t('task.form.saving') }}
      </div>

      <div class="form-section">
        <h3 class="section-title">{{ t('task.form.basicInfo') }}</h3>
        <!-- Task name -->
        <div class="form-group">
          <label class="form-label">{{ t('task.form.taskName') }} <span class="required">*</span></label>
          <input type="text" class="form-input" v-model="form.name" :placeholder="t('task.form.taskNamePlaceholder')" />
          <div v-if="errors.name" class="form-error">{{ errors.name }}</div>
        </div>

        <!-- Agent -->
        <div class="form-group">
          <label class="form-label">{{ t('task.form.executeAgent') }} <span class="required">*</span></label>
          <button class="agent-display" @click="openAgentSelector">
            <template v-if="form.agentId && selectedAgent">
              <AgentIcon :backend="selectedAgent.backend" :name="selectedAgent.name" :size="16" />
              <div class="agent-display-detail">
                <span class="agent-display-name">{{ selectedAgent.name }}</span>
                <div class="agent-display-tags">
                  <span class="agent-display-tag backend-tag">{{ selectedAgent.backend }}</span>
                  <span v-if="selectedModelName" class="agent-display-tag model-tag">{{ selectedModelName }}</span>
                </div>
              </div>
            </template>
            <template v-else>
              <span class="agent-display-placeholder">{{ t('task.form.selectAgent') }}</span>
            </template>
            <ChevronDown class="agent-display-icon" :size="14" />
          </button>
          <div v-if="errors.agentId" class="form-error">{{ errors.agentId }}</div>
        </div>
      </div>

      <div class="form-section">
        <h3 class="section-title">{{ t('task.form.scheduleInfo') }}</h3>
        <!-- Frequency preset -->
        <div class="form-group">
          <label class="form-label">{{ t('task.form.frequency') }}</label>
          <div class="preset-buttons">
            <button v-for="p in presets" :key="p.value" class="preset-btn" :class="{ active: preset === p.value }" @click="setPreset(p.value)">
              {{ p.label }}
            </button>
            <button class="preset-btn" :class="{ active: preset === 'custom' }" @click="setPreset('custom')">
              {{ t('task.form.custom') }}
            </button>
          </div>
        </div>

        <!-- Time selectors based on preset -->
        <div v-if="preset !== 'custom'" class="form-group time-selectors">
          <!-- Hourly: minute only -->
          <div v-if="preset === 'hourly'" class="time-row">
            <span class="time-label">{{ t('task.form.minute') }}</span>
            <div class="select-wrapper inline">
              <select class="form-select time-select" v-model.number="minute">
                <option v-for="m in 60" :key="m - 1" :value="m - 1">{{ String(m - 1).padStart(2, '0') }}</option>
              </select>
            </div>
          </div>

          <!-- Daily: hour + minute -->
          <div v-if="preset === 'daily'" class="time-row">
            <div class="select-wrapper inline">
              <select class="form-select time-select" v-model.number="hour">
                <option v-for="h in 24" :key="h - 1" :value="h - 1">{{ String(h - 1).padStart(2, '0') }}</option>
              </select>
            </div>
            <span class="time-sep">:</span>
            <div class="select-wrapper inline">
              <select class="form-select time-select" v-model.number="minute">
                <option v-for="m in 12" :key="(m - 1) * 5" :value="(m - 1) * 5">{{ String((m - 1) * 5).padStart(2, '0') }}</option>
              </select>
            </div>
          </div>

          <!-- Weekly: weekday + hour + minute -->
          <div v-if="preset === 'weekly'" class="time-column">
            <div class="weekday-buttons">
              <button v-for="(label, idx) in weekdayLabels" :key="idx" class="weekday-btn" :class="{ active: weekday === idx }" @click="weekday = idx">
                {{ label }}
              </button>
            </div>
            <div class="time-row mt-2">
              <div class="select-wrapper inline">
                <select class="form-select time-select" v-model.number="hour">
                  <option v-for="h in 24" :key="h - 1" :value="h - 1">{{ String(h - 1).padStart(2, '0') }}</option>
                </select>
              </div>
              <span class="time-sep">:</span>
              <div class="select-wrapper inline">
                <select class="form-select time-select" v-model.number="minute">
                  <option v-for="m in 12" :key="(m - 1) * 5" :value="(m - 1) * 5">{{ String((m - 1) * 5).padStart(2, '0') }}</option>
                </select>
              </div>
            </div>
          </div>

          <!-- Monthly: month day + hour + minute -->
          <div v-if="preset === 'monthly'" class="time-column">
            <div class="time-row">
              <span class="time-label">{{ t('task.form.date') }}</span>
              <div class="select-wrapper inline">
                <select class="form-select time-select" v-model.number="monthDay">
                  <option v-for="d in 31" :key="d" :value="d">{{ d }}</option>
                </select>
              </div>
            </div>
            <div v-if="monthDay >= 29" class="form-hint warning">{{ t('task.form.monthDaySkipHint') }}</div>
            <div class="time-row mt-2">
              <div class="select-wrapper inline">
                <select class="form-select time-select" v-model.number="hour">
                  <option v-for="h in 24" :key="h - 1" :value="h - 1">{{ String(h - 1).padStart(2, '0') }}</option>
                </select>
              </div>
              <span class="time-sep">:</span>
              <div class="select-wrapper inline">
                <select class="form-select time-select" v-model.number="minute">
                  <option v-for="m in 12" :key="(m - 1) * 5" :value="(m - 1) * 5">{{ String((m - 1) * 5).padStart(2, '0') }}</option>
                </select>
              </div>
            </div>
          </div>
        </div>

        <!-- Generated cron expression -->
        <div class="form-group">
          <label class="form-label">{{ t('task.form.cronExpression') }}</label>
          <input
            v-if="preset === 'custom'"
            type="text"
            class="form-input font-mono"
            v-model="customCron"
            placeholder="0 9 * * *"
          />
          <div v-else class="cron-display">
            <code>{{ generatedCron }}</code>
            <span class="cron-humanize">{{ humanizeCron(generatedCron) }}</span>
          </div>
          <div v-if="preset === 'custom'" class="form-hint">{{ t('task.form.cronHint') }}</div>
          <div v-if="errors.cronExpr" class="form-error">{{ errors.cronExpr }}</div>
        </div>

        <!-- Repeat mode -->
        <div class="form-group">
          <label class="form-label">{{ t('task.form.repeatMode') }}</label>
          <div class="radio-group">
            <label class="radio-label">
              <input type="radio" v-model="form.repeatMode" value="once" />
              <span>{{ t('task.form.repeatOnce') }}</span>
            </label>
            <label class="radio-label">
              <input type="radio" v-model="form.repeatMode" value="limited" />
              <span>{{ t('task.form.repeatLimited') }}</span>
            </label>
            <label class="radio-label">
              <input type="radio" v-model="form.repeatMode" value="unlimited" />
              <span>{{ t('task.form.repeatUnlimited') }}</span>
            </label>
          </div>
        </div>

        <!-- Max runs (limited mode) -->
        <div v-if="form.repeatMode === 'limited'" class="form-group slide-down">
          <label class="form-label">{{ t('task.form.maxRuns') }}</label>
          <input type="number" class="form-input" v-model.number="form.maxRuns" min="1" />
        </div>
      </div>

      <div class="form-section flex-fill">
        <h3 class="section-title">{{ t('task.form.promptInfo') }}</h3>
        <!-- Prompt -->
        <div class="form-group prompt-group">
          <textarea class="form-textarea prompt-textarea" v-model="form.prompt" :placeholder="t('task.form.promptPlaceholder')"></textarea>
          <div v-if="errors.prompt" class="form-error">{{ errors.prompt }}</div>
        </div>
      </div>

      <!-- General form error (ISS-012: network/server errors) -->
      <div v-if="formError" class="form-error form-error-general">{{ formError }}</div>
    </div>

    <!-- Fixed bottom bar -->
    <div class="form-footer">
      <button class="action-btn secondary" @click="$emit('close')">{{ t('common.cancel') }}</button>
      <button class="action-btn primary" :disabled="saving" @click="submit">
        <Save v-if="!saving" :size="14" />
        <Loader2 v-else class="spin-icon" :size="14" />
        {{ mode === 'create' ? t('task.form.create') : t('task.form.save') }}
      </button>
    </div>
  </div>

  <!-- Agent selector drawer -->
  <AgentSelectorDrawer
    ref="agentSelectorRef"
    v-model:open="agentSelectorOpen"
    :model-value="form.agentId"
    :title="t('session.selectAgent')"
    :default-badge="t('chat.sessionSetting.defaultBadge')"
    :set-default-title="t('session.setAsDefaultAgent')"
    @select="handleAgentSelect"
  />
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, Loader2, Save } from 'lucide-vue-next'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'
import { useAgents } from '@/composables/useAgents'
import { useTaskForm } from '@/composables/useTaskForm.ts'
import { humanizeCron } from '@/utils/format.ts'

const { t } = useI18n()

const props = defineProps({
  mode: { type: String, default: 'create' },  // 'create' | 'edit'
  task: Object,  // required for edit mode
})

const emit = defineEmits(['close', 'saved'])

const { agents, loadAgents, getAgent, getAgentDefaultModelName } = useAgents()

// Agent selector state
const agentSelectorOpen = ref(false)
const agentSelectorRef = ref(null)

const selectedAgent = computed(() => form.value.agentId ? getAgent(form.value.agentId) : null)
const selectedModelName = computed(() => form.value.agentId ? getAgentDefaultModelName(form.value.agentId) : '')

async function openAgentSelector() {
  await loadAgents()
  agentSelectorOpen.value = true
}

function handleAgentSelect(agentId) {
  form.value.agentId = agentId
  if (errors.value.agentId) {
    const e = { ...errors.value }
    delete e.agentId
    errors.value = e
  }
}

// Task form composable (ISS-011 + ISS-012)
const { form, errors, formError, saving, submit: _submit, init } = useTaskForm({
  mode: computed(() => props.mode),
  onSuccess: (taskId) => emit('saved', taskId),
})

// Frequency preset
const presets = computed(() => [
  { value: 'hourly', label: t('task.form.presets.hourly') },
  { value: 'daily', label: t('task.form.presets.daily') },
  { value: 'weekly', label: t('task.form.presets.weekly') },
  { value: 'monthly', label: t('task.form.presets.monthly') },
])

const weekdayLabels = computed(() => t('task.form.weekdays'))

const preset = ref('daily')
const minute = ref(0)
const hour = ref(9)
const weekday = ref(1)     // 0=Sun, 1=Mon, ..., 6=Sat
const monthDay = ref(1)
const customCron = ref('')

// Generate cron from preset
const generatedCron = computed(() => {
  const m = String(minute.value).padStart(2, '0')
  const h = String(hour.value).padStart(2, '0')
  switch (preset.value) {
    case 'hourly':  return `${m} * * * *`
    case 'daily':   return `${m} ${h} * * *`
    case 'weekly':  return `${m} ${h} * * ${weekday.value}`
    case 'monthly': return `${m} ${h} ${monthDay.value} * *`
    default:        return customCron.value
  }
})

// Effective cron expression (for submission)
const effectiveCron = computed(() => {
  return preset.value === 'custom' ? customCron.value.trim() : generatedCron.value
})

// Set preset with smart defaults
function setPreset(p) {
  if (preset.value !== 'custom' && p === 'custom') {
    // Switching to custom: pre-fill with current generated cron
    customCron.value = generatedCron.value
  }
  preset.value = p
}

// Detect preset from existing cron expression (for edit mode)
function detectPreset(cron) {
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return 'custom'

  const [m, h, dom, mon, dow] = parts
  const isNumeric = (s) => /^\d+$/.test(s)

  // Hourly: M * * * * (M must be numeric, not step like */5)
  if (isNumeric(m) && h === '*' && dom === '*' && mon === '*' && dow === '*') {
    minute.value = parseInt(m)
    return 'hourly'
  }
  // Daily: M H * * *
  if (isNumeric(m) && isNumeric(h) && dom === '*' && mon === '*' && dow === '*') {
    minute.value = parseInt(m)
    hour.value = parseInt(h)
    return 'daily'
  }
  // Weekly: M H * * DOW
  if (isNumeric(m) && isNumeric(h) && dom === '*' && mon === '*' && isNumeric(dow)) {
    minute.value = parseInt(m)
    hour.value = parseInt(h)
    weekday.value = parseInt(dow)
    return 'weekly'
  }
  // Monthly: M H DOM * *
  if (isNumeric(m) && isNumeric(h) && isNumeric(dom) && mon === '*' && dow === '*') {
    minute.value = parseInt(m)
    hour.value = parseInt(h)
    monthDay.value = parseInt(dom)
    return 'monthly'
  }

  customCron.value = cron
  return 'custom'
}

// Validate form (delegates to composable + cron-specific check)
function validateForm() {
  const e = {}
  if (!form.value.name.trim()) e.name = t('task.form.nameRequired')
  if (!form.value.agentId) e.agentId = t('task.form.agentRequired')
  if (!form.value.prompt.trim()) e.prompt = t('task.form.promptRequired')
  if (preset.value === 'custom' && !customCron.value.trim()) {
    e.cronExpr = t('task.form.cronRequired')
  }
  errors.value = e
  return Object.keys(e).length === 0
}

// Submit (delegates to composable, but updates cron_expr from preset)
async function submit() {
  if (!validateForm()) return
  form.value.cronExpr = effectiveCron.value
  await _submit()
}

// Initialize form on mount
onMounted(() => {
  init(props.mode === 'edit' ? props.task : null)

  if (props.mode === 'edit' && props.task) {
    preset.value = detectPreset(props.task.cronExpr)
  } else {
    preset.value = 'daily'
    const now = new Date()
    hour.value = now.getHours()
    minute.value = 0
    weekday.value = 1
    monthDay.value = 1
    customCron.value = ''
  }

  if (agents.value.length === 0) {
    loadAgents()
  }
})
</script>

<style scoped>
.task-form-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

/* Compact header */
.form-header {
  display: flex;
  align-items: center;
  padding: 4px 8px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

/* Scrollable form content */
.form-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.saving-indicator {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: rgba(34, 197, 94, 0.1);
  color: #16a34a;
  padding: 6px 12px;
  border-radius: 0;
  font-size: 12px;
  font-weight: 500;
  margin-bottom: 4px;
}

.spin-icon {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  100% { transform: rotate(360deg); }
}

.form-section {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.form-section.flex-fill {
  flex: 1;
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  padding: 10px;
  gap: 10px;
}

.section-title {
  margin: 0 0 2px 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.prompt-group {
  flex: 1;
}

.form-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary, #4b5563);
}

.required {
  color: #ef4444;
}

.form-input,
.form-select,
.form-textarea {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: 0;
  font-size: 13px;
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  box-sizing: border-box;
  outline: none;
  transition: all 0.2s ease;
  font-family: inherit;
}

.form-input.font-mono {
  font-family: 'SF Mono', 'Menlo', monospace;
}

.form-input:focus,
.form-select:focus,
.form-textarea:focus {
  border-color: var(--accent-color, #0066cc);
  box-shadow: 0 0 0 3px rgba(0, 102, 204, 0.1);
}

.form-input::placeholder,
.form-textarea::placeholder {
  color: var(--text-muted, #9ca3af);
}

.select-wrapper {
  position: relative;
  display: block;
}

.select-wrapper.inline {
  display: inline-block;
}

.select-wrapper .form-select {
  appearance: none;
  padding-right: 32px;
  cursor: pointer;
}

.select-wrapper.inline .form-select {
  padding-right: 24px;
  padding-left: 8px;
  padding-top: 6px;
  padding-bottom: 6px;
}

.select-icon {
  position: absolute;
  right: 10px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-muted, #9ca3af);
  pointer-events: none;
}

/* Agent display button */
.agent-display {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: 6px;
  font-size: 13px;
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  box-sizing: border-box;
  cursor: pointer;
  text-align: left;
  font-family: inherit;
  transition: all 0.2s ease;
}

.agent-display:focus {
  border-color: var(--accent-color, #0066cc);
  box-shadow: 0 0 0 3px rgba(0, 102, 204, 0.1);
  outline: none;
}

@media (hover: hover) {
  .agent-display:hover {
    border-color: var(--accent-color, #0066cc);
  }
}

.agent-display-detail {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.agent-display-name {
  font-size: 13px;
  color: var(--text-primary, #1a1a1a);
  font-weight: 500;
}

.agent-display-tags {
  display: flex;
  gap: 4px;
}

.agent-display-tag {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 0;
  font-weight: 500;
  flex-shrink: 0;
}

.agent-display-tag.backend-tag {
  background: rgba(0, 102, 204, 0.1);
  color: var(--accent-color, #0066cc);
  text-transform: lowercase;
}

.agent-display-tag.model-tag {
  background: rgba(100, 100, 100, 0.08);
  color: var(--text-muted, #999);
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-display-placeholder {
  flex: 1;
  color: var(--text-muted, #9ca3af);
  font-size: 13px;
}

.agent-display-icon {
  flex-shrink: 0;
  color: var(--text-muted, #9ca3af);
}

.form-textarea {
  resize: vertical;
  min-height: 80px;
}

.prompt-textarea {
  height: 100%;
  min-height: 280px;
}

.form-hint {
  font-size: 11px;
  color: var(--text-muted, #6b7280);
}

.form-hint.warning {
  color: #ca8a04;
}

.form-error {
  font-size: 11px;
  color: #ef4444;
  display: flex;
  align-items: center;
  gap: 4px;
}

.form-error-general {
  background: rgba(239, 68, 68, 0.1);
  padding: 8px 10px;
  border-radius: 0;
  margin-top: 6px;
}

/* Preset buttons */
.preset-buttons {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.preset-btn {
  padding: 4px 12px;
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: 16px;
  background: var(--bg-primary, #fff);
  color: var(--text-secondary, #4b5563);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

@media (hover: hover) {
  .preset-btn:hover {
    border-color: var(--accent-color, #0066cc);
    color: var(--accent-color, #0066cc);
  }
}

.preset-btn.active {
  background: var(--accent-color, #0066cc);
  color: #fff;
  border-color: var(--accent-color, #0066cc);
}

/* Time selectors */
.time-selectors {
  background: var(--bg-tertiary, #f3f4f6);
  border-radius: 0;
  padding: 10px 12px;
  border: 1px solid var(--border-color, #e5e7eb);
}

.time-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.mt-2 {
  margin-top: 6px;
}

.time-column {
  display: flex;
  flex-direction: column;
}

.time-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary, #4b5563);
  flex-shrink: 0;
}

.time-sep {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary, #4b5563);
}

/* Weekday buttons */
.weekday-buttons {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}

.weekday-btn {
  width: 32px;
  height: 32px;
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: 6px;
  background: var(--bg-primary, #fff);
  color: var(--text-secondary, #4b5563);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease;
}

@media (hover: hover) {
  .weekday-btn:hover {
    border-color: var(--accent-color, #0066cc);
    color: var(--accent-color, #0066cc);
  }
}

.weekday-btn.active {
  background: var(--accent-color, #0066cc);
  color: #fff;
  border-color: var(--accent-color, #0066cc);
}

/* Cron display */
.cron-display {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  background: var(--bg-tertiary, #f3f4f6);
  border: 1px solid var(--border-color, #e5e7eb);
  border-radius: 0;
}

.cron-display code {
  font-size: 13px;
  font-weight: 500;
  color: var(--accent-color, #0066cc);
  font-family: 'SF Mono', 'Menlo', monospace;
}

.cron-humanize {
  font-size: 12px;
  color: var(--text-secondary, #6b7280);
}

/* Radio group */
.radio-group {
  display: flex;
  flex-direction: row;
  gap: 16px;
  flex-wrap: wrap;
}

.radio-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--text-primary, #1a1a1a);
  cursor: pointer;
}

.radio-label input[type="radio"] {
  width: 14px;
  height: 14px;
  accent-color: var(--accent-color, #0066cc);
  cursor: pointer;
}

/* Fixed bottom bar */
.form-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 6px 8px;
  background: var(--bg-primary, #ffffff);
  border-top: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.action-btn {
  height: 30px;
  border: none;
  border-radius: 15px;
  cursor: pointer;
  transition: all 0.2s ease;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 0 16px;
  flex-shrink: 0;
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
}

.action-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.action-btn.primary {
  background: var(--accent-color, #0066cc);
  color: #fff;
}

@media (hover: hover) {
  .action-btn.primary:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 85%, black);
    transform: translateY(-1px);
  }
}

.action-btn.secondary {
  background: var(--bg-tertiary, #f1f3f5);
  color: var(--text-secondary, #4b5563);
}

@media (hover: hover) {
  .action-btn.secondary:hover {
    background: #e5e7eb;
    color: var(--text-primary, #1a1a1a);
  }
}

.action-btn:active:not(:disabled) {
  transform: scale(0.96);
}

/* Animations */
.slide-down {
  animation: slideDown 0.2s ease-out;
}

@keyframes slideDown {
  from { opacity: 0; transform: translateY(-10px); }
  to { opacity: 1; transform: translateY(0); }
}
</style>
