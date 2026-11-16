<template>
  <!-- Tool grant for the built-in agent.

       This is the one place a user decides what the agent can touch, so the
       wording is blunt about the trade: `exec` can write files, which is why
       ticking it ticks (and locks) `fs.write`. Showing a tickable "run
       commands" next to an unticked "write files" would be a lie the agent
       itself would then disprove. -->
  <div class="agent-tools">
    <div class="agent-tools__hint">
      {{ t('settings.items.toolPolicyIntro') }}
    </div>

    <label
      v-for="cap in capabilityRows"
      :key="cap.id"
      class="agent-tools__row"
      :class="{ 'agent-tools__row--locked': cap.locked }"
    >
      <input
        type="checkbox"
        :checked="granted.includes(cap.id)"
        :disabled="cap.locked || saving"
        @change="toggle(cap.id, ($event.target as HTMLInputElement).checked)"
      />
      <span class="agent-tools__body">
        <span class="agent-tools__name">{{ cap.label }}</span>
        <span class="agent-tools__desc">{{ cap.description }}</span>
      </span>
    </label>

    <!-- The consequence of granting exec, stated where the grant is made. -->
    <div v-if="granted.includes(CAP_EXEC)" class="agent-tools__warn">
      <AlertTriangle :size="13" />
      <span>{{ t('settings.items.toolPolicyExecWarning') }}</span>
    </div>

    <div v-if="granted.length === 0" class="agent-tools__hint">
      {{ t('settings.items.toolPolicyNoTools') }}
    </div>

    <!-- Whether the FILE tools may leave the project. Stated as the permission
         ("allow access outside") because that is the question a user asks;
         the stored field is the restriction, so the checkbox is its negation.
         Shown only when a file tool is granted: with no tools there is nothing
         for the switch to affect, and a live-looking toggle would imply it
         does something. -->
    <div v-if="granted.length > 0" class="agent-tools__scope">
      <label class="agent-tools__row">
        <input
          type="checkbox"
          :checked="allowOutsideWorkDir"
          :disabled="saving"
          @change="saveOutsideAccess(($event.target as HTMLInputElement).checked)"
        />
        <span class="agent-tools__body">
          <span class="agent-tools__name">{{ t('settings.items.toolPolicyOutside') }}</span>
          <span class="agent-tools__desc">{{ t('settings.items.toolPolicyOutsideDesc') }}</span>
        </span>
      </label>
      <div v-if="granted.includes(CAP_EXEC)" class="agent-tools__hint agent-tools__hint--nested">
        {{ t('settings.items.toolPolicyOutsideExecNote') }}
      </div>
    </div>

    <!-- Per-turn budgets. A real repository task routinely needs dozens of
         rounds, so these are settings rather than constants: too small and the
         agent stops mid-task, too large and one turn can run for a long time.
         Empty / 0 means "use the server default", which is what the
         placeholders show. -->
    <div class="agent-tools__budgets">
      <label class="agent-tools__budget">
        <span class="agent-tools__name">{{ t('settings.items.toolPolicyMaxRounds') }}</span>
        <input
          type="number"
          min="0"
          step="1"
          inputmode="numeric"
          :value="props.agent?.toolPolicy?.maxRounds || ''"
          :placeholder="String(DEFAULT_MAX_ROUNDS)"
          :disabled="saving"
          @change="saveBudget('maxRounds', $event)"
        />
        <span class="agent-tools__desc">{{ t('settings.items.toolPolicyMaxRoundsDesc') }}</span>
      </label>
      <label class="agent-tools__budget">
        <span class="agent-tools__name">{{ t('settings.items.toolPolicyMaxToolCalls') }}</span>
        <input
          type="number"
          min="0"
          step="1"
          inputmode="numeric"
          :value="props.agent?.toolPolicy?.maxToolCalls || ''"
          :placeholder="String(DEFAULT_MAX_TOOL_CALLS)"
          :disabled="saving"
          @change="saveBudget('maxToolCalls', $event)"
        />
        <span class="agent-tools__desc">{{ t('settings.items.toolPolicyMaxToolCallsDesc') }}</span>
      </label>
    </div>

    <div v-if="error" class="agent-tools__error">
      <AlertCircle :size="13" />
      <span>{{ error }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, AlertTriangle } from 'lucide-vue-next'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { CAP_EXEC, CAP_FS_READ, CAP_FS_WRITE } from '@/composables/useAgents'
import type { ToolPolicy } from '@/composables/useAgents'
import { appLog } from '@/utils/appLog'

/** Only the fields this section reads; the settings page passes its agent record. */
interface AgentLike {
  id: string
  toolPolicy?: ToolPolicy
}

const props = defineProps<{ agent: AgentLike | null }>()
const { t } = useI18n()
const { patchAgentField } = useSettingsConfig()

const error = ref('')
const saving = ref(false)

/**
 * Mirrors model.DefaultMaxRounds / DefaultMaxToolCalls.
 *
 * Shown as the placeholder (not written into the field) so an untouched agent
 * keeps "unset" semantics and picks up a future default change instead of
 * being pinned to today's number.
 */
const DEFAULT_MAX_ROUNDS = 100
const DEFAULT_MAX_TOOL_CALLS = 300

/**
 * The granted set, normalized the same way the server does it.
 *
 * Mirroring the closure locally (rather than waiting for the round-trip) is
 * what makes the UI respond instantly when exec is ticked: the dependent
 * checkbox must become checked and locked in the same frame, not after a
 * network round-trip during which it would still look unticked.
 */
const granted = computed<string[]>(() => {
  const allow = props.agent?.toolPolicy?.allow ?? []
  const have = new Set(allow)
  if (have.has(CAP_EXEC)) {
    have.add(CAP_FS_WRITE)
    have.add(CAP_FS_READ)
  }
  if (have.has(CAP_FS_WRITE)) have.add(CAP_FS_READ)
  return [CAP_FS_READ, CAP_FS_WRITE, CAP_EXEC].filter((c) => have.has(c))
})

const capabilityRows = computed(() => [
  {
    id: CAP_FS_READ,
    label: t('settings.items.toolPolicyRead'),
    description: t('settings.items.toolPolicyReadDesc'),
    locked: false,
  },
  {
    id: CAP_FS_WRITE,
    label: t('settings.items.toolPolicyWrite'),
    description: t('settings.items.toolPolicyWriteDesc'),
    // Implied by exec, so it cannot be unticked while exec is on.
    locked: granted.value.includes(CAP_EXEC),
  },
  {
    id: CAP_EXEC,
    label: t('settings.items.toolPolicyExec'),
    description: t('settings.items.toolPolicyExecDesc'),
    locked: false,
  },
])

/**
 * Persist the grant.
 *
 * The whole policy object is sent (not just `allow`) so the budget fields keep
 * whatever the user set elsewhere; the server treats a missing field as "leave
 * alone", which would silently drop them if this sent a partial object.
 */
async function save(nextAllow: string[]) {
  if (!props.agent || saving.value) return
  saving.value = true
  error.value = ''
  try {
    await patchAgentField(props.agent.id, 'tool_policy', {
      ...(props.agent.toolPolicy ?? {}),
      allow: nextAllow,
    })
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
    appLog.w('AgentToolPolicySection', 'save failed', error.value)
  } finally {
    saving.value = false
  }
}

/**
 * Persist one budget field.
 *
 * 0 / empty is stored as 0, which the server reads as "use the default". The
 * field is reset to what is actually stored when the entry is not a positive
 * number, so the UI never shows a budget the agent is not running with.
 *
 * Sending the whole policy (not just the changed field) matches save() above:
 * a partial object would rely on the server's "missing means leave alone"
 * behaviour, which is exactly what drops the other budget.
 */
async function saveBudget(field: 'maxRounds' | 'maxToolCalls', event: Event) {
  const input = event.target as HTMLInputElement
  const stored = props.agent?.toolPolicy?.[field] ?? 0
  if (!props.agent || saving.value) {
    input.value = stored > 0 ? String(stored) : ''
    return
  }
  const raw = input.value.trim()
  const parsed = Number.parseInt(raw, 10)
  // Empty means "use the default"; anything else must be a positive integer.
  // A negative or non-numeric entry is rejected outright rather than silently
  // rewritten to 0, which would look like the user had cleared the field.
  if (raw !== '' && (!Number.isFinite(parsed) || parsed <= 0)) {
    input.value = stored > 0 ? String(stored) : ''
    return
  }
  const value = raw === '' ? 0 : parsed
  input.value = value > 0 ? String(value) : ''
  if (value === stored) return

  saving.value = true
  error.value = ''
  try {
    await patchAgentField(props.agent.id, 'tool_policy', {
      ...(props.agent.toolPolicy ?? {}),
      allow: granted.value,
      [field]: value,
    })
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
    appLog.w('AgentToolPolicySection', 'save budget failed', error.value)
    // A failed save must not leave the typed number looking accepted.
    input.value = stored > 0 ? String(stored) : ''
  } finally {
    saving.value = false
  }
}

/**
 * Whether the file tools may reach outside the working directory.
 *
 * The stored field is `restrictToWorkDir`, so this is its negation — and an
 * ABSENT value must read as "allowed" (true), matching the server's zero value
 * and the default this setting shipped with. Written as `!== true` rather than
 * `?? true` so a JSON `null` also lands on the allowed side.
 */
const allowOutsideWorkDir = computed<boolean>(
  () => props.agent?.toolPolicy?.restrictToWorkDir !== true,
)

/**
 * Persist the outside-access switch.
 *
 * The whole policy is sent for the same reason save() and saveBudget() do it:
 * a partial object relies on the server's "missing means leave alone"
 * behaviour, and a field the server does not treat that way would be reset.
 */
async function saveOutsideAccess(allow: boolean) {
  if (!props.agent || saving.value) return
  saving.value = true
  error.value = ''
  try {
    await patchAgentField(props.agent.id, 'tool_policy', {
      ...(props.agent.toolPolicy ?? {}),
      allow: granted.value,
      restrictToWorkDir: !allow,
    })
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
    appLog.w('AgentToolPolicySection', 'save outside access failed', error.value)
  } finally {
    saving.value = false
  }
}

function toggle(cap: string, checked: boolean) {
  const next = new Set(granted.value)
  if (checked) {
    next.add(cap)
  } else {
    next.delete(cap)
    // Unticking a prerequisite must untick its dependents, or the stored
    // policy would be reopened by the server's closure and the checkbox would
    // appear to refuse to turn off.
    if (cap === CAP_FS_READ) {
      next.delete(CAP_FS_WRITE)
      next.delete(CAP_EXEC)
    }
    if (cap === CAP_FS_WRITE) {
      next.delete(CAP_EXEC)
    }
  }
  // Close over the implications BEFORE sending, so the payload the user sees
  // reflected is the payload the server would store anyway. Relying on the
  // round-trip to add them would flash a state where exec is on and write is
  // off — the one combination this editor exists to make impossible.
  if (next.has(CAP_EXEC)) {
    next.add(CAP_FS_WRITE)
    next.add(CAP_FS_READ)
  }
  if (next.has(CAP_FS_WRITE)) {
    next.add(CAP_FS_READ)
  }
  void save([CAP_FS_READ, CAP_FS_WRITE, CAP_EXEC].filter((c) => next.has(c)))
}

// A policy change re-fetches the agent list, so props.agent is replaced and the
// computeds above follow. Clearing the error on a new agent prevents a stale
// failure from following the user to the next one.
watch(
  () => props.agent?.id,
  () => {
    error.value = ''
  },
)
</script>

<style scoped>
.agent-tools {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6) var(--space-7);
}
.agent-tools__hint {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.agent-tools__row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  cursor: pointer;
}
.agent-tools__row--locked {
  cursor: default;
  opacity: 0.75;
}
.agent-tools__body {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}
.agent-tools__name {
  font-size: var(--font-size-md);
  color: var(--text-primary);
}
.agent-tools__desc {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
}
.agent-tools__scope {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border-color);
}
.agent-tools__hint--nested {
  padding-left: calc(16px + var(--space-3));
  font-size: var(--font-size-sm);
}
.agent-tools__budgets {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border-color);
}
.agent-tools__budget {
  display: grid;
  grid-template-columns: 1fr 92px;
  gap: var(--space-2) var(--space-3);
  align-items: center;
}
.agent-tools__budget .agent-tools__desc {
  grid-column: 1 / -1;
}
.agent-tools__budget input {
  width: 100%;
  padding: var(--space-2) var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  text-align: right;
}
.agent-tools__budget input:disabled {
  opacity: 0.6;
}
.agent-tools__warn,
.agent-tools__error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  color: var(--color-orange);
}
.agent-tools__error {
  color: var(--color-red);
}
</style>
