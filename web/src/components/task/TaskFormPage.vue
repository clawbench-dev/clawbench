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

          <!-- The watched repository is not configurable: an event task always
               watches the project's bound repository. Show the resolved binding
               so the user can confirm what it will watch. -->
          <div class="form-group">
            <label class="form-label">{{ t('task.form.eventRepo') }}</label>
            <div class="event-repo-readonly" :class="{ unbound: !boundRepoLabel }">
              <GitBranch :size="14" />
              <span>{{ boundRepoLabel || t('task.form.eventRepoUnbound') }}</span>
            </div>
            <div v-if="boundRepoLabel" class="form-hint">{{ t('task.form.eventRepoHint') }}</div>
            <!-- Unbound is a soft warning, not a validation error: the task can
                 still be saved, but it can never fire. -->
            <div v-else class="form-warning">
              <AlertTriangle :size="13" />
              <span>{{ t('task.form.eventRepoUnboundWarn') }}</span>
            </div>
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
            <MenuSelect v-model="minute" :options="minuteOptions" />
          </div>

          <!-- Daily: hour + minute -->
          <div v-if="preset === 'daily'" class="time-row">
            <MenuSelect v-model="hour" :options="hourOptions" />
            <span class="time-sep">:</span>
            <MenuSelect v-model="minute" :options="minuteStepOptions" />
          </div>

          <!-- Weekly: weekday + hour + minute -->
          <div v-if="preset === 'weekly'" class="time-column">
            <div class="weekday-buttons">
              <button v-for="(label, idx) in weekdayLabels" :key="idx" class="weekday-btn" :class="{ active: weekday === idx }" @click="weekday = idx">
                {{ label }}
              </button>
            </div>
            <div class="time-row mt-2">
              <MenuSelect v-model="hour" :options="hourOptions" />
              <span class="time-sep">:</span>
              <MenuSelect v-model="minute" :options="minuteStepOptions" />
            </div>
          </div>

          <!-- Monthly: month day + hour + minute -->
          <div v-if="preset === 'monthly'" class="time-column">
            <div class="time-row">
              <span class="time-label">{{ t('task.form.date') }}</span>
              <MenuSelect v-model="monthDay" :options="monthDayOptions" />
            </div>
            <div v-if="monthDay >= 29" class="form-hint warning">{{ t('task.form.monthDaySkipHint') }}</div>
            <div class="time-row mt-2">
              <MenuSelect v-model="hour" :options="hourOptions" />
              <span class="time-sep">:</span>
              <MenuSelect v-model="minute" :options="minuteStepOptions" />
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
import { AlertTriangle, ChevronDown, GitBranch, Save } from 'lucide-vue-next'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'
import MenuSelect from '@/components/common/MenuSelect.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useAgents } from '@/composables/useAgents'
import { useTaskForm } from '@/composables/useTaskForm.ts'
import { fetchForgeBinding } from '@/utils/forgeApi'
import { FORGE_EVENT_TRANSITIONS, expandStoredEventTypes, offeredEventValues, eventKindLabel, eventTransitionLabel } from '@/utils/forgeEventLabels'
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
// independent triggers. merged is PR-only: an issue has no merge, so offering
// it under Issues would create a subscription that can never fire.
//
// pipeline_done is deliberately absent: nothing derives a pipeline event yet,
// so subscribing would never fire. It stays valid on the backend so a task that
// already stores one is not rejected, and the shared label map still renders it.
const eventTypeGroups = computed(() => Object.entries(FORGE_EVENT_TRANSITIONS).map(([kind, transitions]) => ({
  kind,
  label: eventKindLabel(kind),
  options: transitions.map(tr => ({
    value: `${kind}.${tr}`,
    label: eventTransitionLabel(tr),
  })),
})))

// Every kind-scoped value the checkboxes can represent.
const OFFERED_EVENT_VALUES = computed(() => offeredEventValues())

// Keys with no checkbox (a retired or unknown subscription). They must survive
// an edit untouched, or saving would drop something the user cannot even see.
function unrepresentableEventKeys() {
  return expandStoredEventTypes(form.value.eventTypes)
    .filter(k => !OFFERED_EVENT_VALUES.value.has(k))
}

// selectedEventTypes is a view over form.eventTypes (comma-separated).
const selectedEventTypes = computed({
  get: () => expandStoredEventTypes(form.value.eventTypes)
    .filter(k => OFFERED_EVENT_VALUES.value.has(k)),
  set: (vals) => {
    // Re-append anything the UI cannot represent so it is never lost.
    form.value.eventTypes = [...vals, ...unrepresentableEventKeys()].join(',')
  },
})

// The watched repository is the project's binding, so there is nothing to
// choose — the form only displays it. An empty label means the project has no
// binding, which is what drives the warning below.
const boundRepoLabel = ref('')

async function loadBoundRepo() {
  try {
    const res = await fetchForgeBinding()
    const b = res.binding
    boundRepoLabel.value = b ? `${b.owner}/${b.repo}` : ''
  } catch {
    boundRepoLabel.value = ''
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

// ── Time option lists ──
// MenuSelect holds a flat option array, so the generated ranges live here
// rather than in the template's v-for. Values stay numbers to match the refs
// (and the cron builder's arithmetic).
const pad2 = (n) => String(n).padStart(2, '0')
const hourOptions = Array.from({ length: 24 }, (_, h) => ({ value: h, label: pad2(h) }))
// Hourly allows any minute; the other presets step by 5 (as the old <select>
// did), so the two lists are deliberately different.
const minuteOptions = Array.from({ length: 60 }, (_, m) => ({ value: m, label: pad2(m) }))
const minuteStepOptions = Array.from({ length: 12 }, (_, i) => {
  const m = i * 5
  return { value: m, label: pad2(m) }
})
const monthDayOptions = Array.from({ length: 31 }, (_, i) => ({ value: i + 1, label: String(i + 1) }))

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
  void loadBoundRepo()
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
  padding:0 var(--space-2) 0 var(--space-6);
  flex-shrink: 0;
  gap: var(--space-3);
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

/* Scrollable form content */
.form-scroll {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.saving-indicator {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  background: rgba(34, 197, 94, 0.1);
  color: #16a34a;
  padding: var(--space-3) var(--space-6);
  border-radius: 0;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  margin-bottom: var(--space-2);
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
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.form-section.flex-fill {
  flex: 1;
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  padding: var(--space-5);
  gap: var(--space-5);
}

.section-title {
  margin:0 0 var(--space-1) 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.prompt-group {
  flex: 1;
}

.form-label {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary, #4b5563);
}

.required {
  color: #ef4444;
}

.form-input,
.form-textarea {
  width: 100%;
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: 0;
  font-size: var(--font-size-md);
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  box-sizing: border-box;
  outline: none;
  transition: all var(--duration-slow) ease;
  font-family: inherit;
}

.form-input.font-mono {
  font-family: var(--font-mono);
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

/* Agent display button */
.agent-display {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  box-sizing: border-box;
  cursor: pointer;
  text-align: left;
  font-family: inherit;
  transition: all var(--duration-slow) ease;
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
  gap: var(--space-1);
  min-width: 0;
}

.agent-display-name {
  font-size: var(--font-size-md);
  color: var(--text-primary, #1a1a1a);
  font-weight: var(--font-weight-medium);
}

.agent-display-tags {
  display: flex;
  gap: var(--space-2);
}

.agent-display-tag {
  font-size: var(--font-size-2xs);
  padding:1px var(--space-2);
  border-radius: 0;
  font-weight: var(--font-weight-medium);
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
  font-size: var(--font-size-md);
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
  font-size: var(--font-size-xs);
  color: var(--text-muted, #6b7280);
}

.form-hint.warning {
  color: #ca8a04;
}

/* Read-only display of the project's bound repository. */
.event-repo-readonly {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-sm, 6px);
  background: var(--bg-secondary, #f8f9fa);
  color: var(--text-primary, #1a1a1a);
  font-size: var(--font-size-sm);
}
.event-repo-readonly.unbound {
  color: var(--text-muted, #999);
  font-style: italic;
}

/* Soft warning: saving is allowed, but the task can never fire. */
.form-warning {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-sm, 6px);
  background: color-mix(in srgb, var(--color-yellow, #eab308) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-yellow, #eab308) 35%, transparent);
  color: var(--color-yellow, #a16207);
  font-size: var(--font-size-xs);
}

.form-error {
  font-size: var(--font-size-xs);
  color: #ef4444;
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.form-error-general {
  background: rgba(239, 68, 68, 0.1);
  padding: var(--space-4) var(--space-5);
  border-radius: 0;
  margin-top: var(--space-3);
}

/* Preset buttons */
.preset-buttons {
  display: flex;
  gap: var(--space-3);
  flex-wrap: wrap;
}

/* Event trigger configuration */
.event-type-group + .event-type-group {
  margin-top: var(--space-5);
}

.event-type-group-label {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted);
  margin-bottom: var(--space-3);
}

.event-type-checks {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3) 14px;
}

.checkbox-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--font-size-md);
  color: var(--text-secondary, #4b5563);
  cursor: pointer;
}

/* Read-only event context block: visually distinct from editable inputs so the
   user can tell it will be injected verbatim and cannot be changed. */
.event-context-block {
  margin: 0;
  padding: var(--space-5) var(--space-6);
  background: var(--bg-secondary, #f9fafb);
  border: 1px dashed var(--border-color, #d1d5db);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-relaxed);
  color: var(--text-secondary, #4b5563);
  white-space: pre-wrap;
  word-break: break-word;
  overflow-x: auto;
}

.preset-btn {
  padding: var(--space-2) var(--space-6);
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: var(--radius-lg);
  background: var(--bg-primary, #fff);
  color: var(--text-secondary, #4b5563);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  transition: all var(--duration-slow) ease;
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
  padding: var(--space-5) var(--space-6);
  border: 1px solid var(--border-color, #e5e7eb);
}

.time-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.mt-2 {
  margin-top: var(--space-3);
}

.time-column {
  display: flex;
  flex-direction: column;
}

.time-label {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary, #4b5563);
  flex-shrink: 0;
}

.time-sep {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary, #4b5563);
}

/* Weekday buttons */
.weekday-buttons {
  display: flex;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.weekday-btn {
  width: 32px;
  height: 32px;
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: var(--radius-sm);
  background: var(--bg-primary, #fff);
  color: var(--text-secondary, #4b5563);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all var(--duration-slow) ease;
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
  gap: var(--space-5);
  padding: var(--space-4) var(--space-6);
  background: var(--bg-tertiary, #f3f4f6);
  border: 1px solid var(--border-color, #e5e7eb);
  border-radius: 0;
}

.cron-display code {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--accent-color, #0066cc);
  font-family: var(--font-mono);
}

.cron-humanize {
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #6b7280);
}

/* Radio group */
.radio-group {
  display: flex;
  flex-direction: row;
  gap: var(--space-7);
  flex-wrap: wrap;
}

.radio-label {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-md);
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
  gap: var(--space-4);
  padding: var(--space-3) var(--space-4);
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
