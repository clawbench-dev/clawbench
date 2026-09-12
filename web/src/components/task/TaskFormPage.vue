<template>
  <div class="task-form-page">
    <!-- Compact header: breadcrumb -->
    <div class="form-header">
      <TaskBreadcrumb />
    </div>

    <!-- Scrollable form content -->
    <div class="form-scroll">
      <div v-if="saving" class="saving-indicator">
        <LoadingIndicator class="saving-spinner" size="sm" inline />
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
        <!-- Trigger mode: cron schedule or forge event -->
        <div class="form-group">
          <label class="form-label">{{ t('task.form.triggerMode') }}</label>
          <div class="preset-buttons">
            <button class="preset-btn" :class="{ active: form.triggerMode !== 'event' }" @click="form.triggerMode = 'cron'">
              {{ t('task.form.triggerCron') }}
            </button>
            <button class="preset-btn" :class="{ active: form.triggerMode === 'event' }" @click="form.triggerMode = 'event'">
              {{ t('task.form.triggerEvent') }}
            </button>
          </div>
        </div>

        <!-- Event configuration (event mode) -->
        <template v-if="form.triggerMode === 'event'">
          <div class="form-group">
            <label class="form-label">{{ t('task.form.eventTypes') }}</label>
            <!-- Grouped by item kind: "a new issue" and "a new PR" are
                 distinct triggers, so each kind lists its own applicable
                 events. merged / pipeline only exist for PRs. -->
            <div v-for="group in eventTypeGroups" :key="group.kind" class="event-type-group">
              <div class="event-type-group-label">{{ group.label }}</div>
              <div class="event-type-checks">
                <label v-for="et in group.options" :key="et.value" class="checkbox-label">
                  <input type="checkbox" :value="et.value" v-model="selectedEventTypes" />
                  <span>{{ et.label }}</span>
                </label>
              </div>
            </div>
            <div v-if="errors.eventTypes" class="form-error">{{ errors.eventTypes }}</div>
          </div>

          <div class="form-group">
            <label class="form-label">{{ t('task.form.eventRepo') }}</label>
            <select class="form-select" v-model="form.eventRepo">
              <option value="">{{ t('task.form.eventRepoAny') }}</option>
              <option v-for="repo in boundRepos" :key="repo.value" :value="repo.value">{{ repo.label }}</option>
            </select>
            <div class="form-hint">{{ t('task.form.eventRepoHint') }}</div>
          </div>

          <!-- Read-only event context block: shows exactly what will be injected.
               Not editable — the variables are filled from the triggering event. -->
          <div class="form-group">
            <label class="form-label">{{ t('task.form.eventContext') }}</label>
            <pre class="event-context-block">{{ eventContextTemplate }}</pre>
            <div class="form-hint">{{ t('task.form.eventContextHint') }}</div>
          </div>
        </template>

        <template v-else>
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
        </template>
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
      <button class="fbtn" @click="$emit('close')">{{ t('common.cancel') }}</button>
      <button class="fbtn fbtn-primary" :disabled="saving" @click="submit">
        <Save v-if="!saving" :size="14" />
        <LoadingIndicator v-else class="action-btn-spinner" size="sm" inline />
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
    :config-title="t('session.configAgent')"
    @select="handleAgentSelect"
  />
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, Save } from 'lucide-vue-next'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useAgents } from '@/composables/useAgents'
import { useTaskForm } from '@/composables/useTaskForm.ts'
import { fetchForgeBinding } from '@/utils/forgeApi'
import { humanizeCron } from '@/utils/format.ts'
import '@/assets/modal-footer-btn.css'

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

// ── Event trigger configuration ──
// The event-type list mirrors the backend's validForgeEventTypes set.
// Event keys are kind-scoped ("issue.opened" / "pr.opened") so the two are
// independent triggers. merged and pipeline_done are PR-only: an issue has no
// merge and no CI, so offering them under Issues would create a subscription
// that can never fire.
const TRANSITIONS = {
  issue: ['opened', 'closed', 'reopened', 'commented'],
  pr: ['opened', 'closed', 'merged', 'reopened', 'commented', 'pipeline_done'],
}
const TRANSITION_LABELS = {
  opened: 'task.form.eventOpened',
  closed: 'task.form.eventClosed',
  merged: 'task.form.eventMerged',
  reopened: 'task.form.eventReopened',
  commented: 'task.form.eventCommented',
  pipeline_done: 'task.form.eventPipeline',
}
const eventTypeGroups = computed(() => [
  { kind: 'issue', label: t('task.form.eventKindIssue') },
  { kind: 'pr', label: t('task.form.eventKindPr') },
].map(g => ({
  ...g,
  options: TRANSITIONS[g.kind].map(tr => ({ value: `${g.kind}.${tr}`, label: t(TRANSITION_LABELS[tr]) })),
})))

// selectedEventTypes is a view over form.eventTypes (comma-separated).
const selectedEventTypes = computed({
  get: () => (form.value.eventTypes ? form.value.eventTypes.split(',').map(s => s.trim()).filter(Boolean) : []),
  set: (vals) => { form.value.eventTypes = vals.join(',') },
})

// The project has at most one binding (1:1), so the repo scope is a two-way
// choice: any bound repo of the project, or an explicit one. We offer the
// project's binding when it exists.
const boundRepos = ref([])

async function loadBoundRepos() {
  try {
    const res = await fetchForgeBinding()
    const b = res.binding
    if (b) {
      const key = `${b.platform}|${b.host}|${b.owner}/${b.repo}`
      boundRepos.value = [{ value: key, label: `${b.owner}/${b.repo}` }]
    } else {
      boundRepos.value = []
    }
  } catch {
    boundRepos.value = []
  }
}

// The read-only context block mirrors the backend's EventPromptTemplate: only
// variables relevant to the selected event types are listed.
const eventContextTemplate = computed(() => {
  const types = selectedEventTypes.value
  const showAll = types.length === 0
  // Keys are kind-scoped ("pr.commented"), so match on the transition suffix
  // (and on the bare legacy form, which a pre-split task may still carry).
  const show = (transition) => showAll || types.some(x => x === transition || x.endsWith('.' + transition))
  const lines = [
    `- ${t('task.form.varEventType')}：{{EVENT_TYPE}}`,
    `- ${t('task.form.varRepo')}：{{REPO}}`,
    `- ${t('task.form.varItem')}：{{ITEM_TYPE}} #{{ITEM_NUMBER}}`,
    `- ${t('task.form.varTitle')}：{{TITLE}}`,
    `- ${t('task.form.varUrl')}：{{URL}}`,
    `- ${t('task.form.varAuthor')}：{{AUTHOR}}`,
    `- ${t('task.form.varState')}：{{STATE}}`,
  ]
  if (show('commented')) lines.push(`- ${t('task.form.varCommentBody')}：{{COMMENT_BODY}}`)
  if (show('pipeline_done')) {
    lines.push(`- ${t('task.form.varPipelineStatus')}：{{PIPELINE_STATUS}}`)
    lines.push(`- ${t('task.form.varPipelineUrl')}：{{PIPELINE_URL}}`)
  }
  return `## ${t('task.form.eventContextHeader')}\n${lines.join('\n')}`
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
  if (form.value.triggerMode === 'event') {
    // An event task must subscribe to at least one event type.
    if (selectedEventTypes.value.length === 0) e.eventTypes = t('task.form.eventTypesRequired')
  } else if (preset.value === 'custom' && !customCron.value.trim()) {
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
  // Only needed when the user switches to event mode, but loading it eagerly
  // avoids a visible delay on that switch.
  void loadBoundRepos()
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

/* Compact header — unified with list/detail/settings/proxy headers */
.form-header {
  display: flex;
  align-items: center;
  height: var(--header-height);
  padding: 0 4px 0 12px;
  flex-shrink: 0;
  gap: 6px;
  background: var(--bg-primary);
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

/* Accent (green) saving strip keeps its own tint; the primary button's
   accent background needs the white arc override instead. */
.saving-indicator .saving-spinner {
  --li-color: #16a34a;
}

.fbtn-primary .action-btn-spinner {
  --li-color: #fff;
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
  font-family: var(--font-mono, 'SF Mono', 'Menlo', monospace);
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

/* Event trigger configuration */
.event-type-group + .event-type-group {
  margin-top: 10px;
}

.event-type-group-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  margin-bottom: 6px;
}

.event-type-checks {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
}

.checkbox-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 13px;
  color: var(--text-secondary, #4b5563);
  cursor: pointer;
}

/* Read-only event context block: visually distinct from editable inputs so the
   user can tell it will be injected verbatim and cannot be changed. */
.event-context-block {
  margin: 0;
  padding: 10px 12px;
  background: var(--bg-secondary, #f9fafb);
  border: 1px dashed var(--border-color, #d1d5db);
  border-radius: 8px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-secondary, #4b5563);
  white-space: pre-wrap;
  word-break: break-word;
  overflow-x: auto;
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
  font-family: var(--font-mono, 'SF Mono', 'Menlo', monospace);
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

/* Animations */
.slide-down {
  animation: slideDown 0.2s ease-out;
}

@keyframes slideDown {
  from { opacity: 0; transform: translateY(-10px); }
  to { opacity: 1; transform: translateY(0); }
}
</style>
