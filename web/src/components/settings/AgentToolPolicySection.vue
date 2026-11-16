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
