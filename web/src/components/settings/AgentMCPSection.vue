<template>
  <!-- Per-agent MCP enable policy: which of the GLOBAL servers (configured in
       Settings → MCP) this agent may use. Only the policy lives here; the
       definitions are global so a key is edited in one place.

       Transport support is per backend and was measured, not assumed:
         - dsh / deepseek / pi accept the field and ignore it entirely
         - codex and commandcode do not support SSE; sending one fails session/new
       Entries that cannot work are grayed out AND disabled rather than left
       tickable: a tick that silently does nothing is worse than a disabled one. -->
  <div class="agent-mcp">
    <div v-if="unsupportedBackend" class="agent-mcp__warn">
      <AlertCircle :size="13" />
      <span>{{ t('settings.items.mcpAgentUnsupported') }}</span>
    </div>

    <template v-else>
      <div v-if="servers.length === 0" class="agent-mcp__hint">
        {{ t('settings.items.mcpEmpty') }}
      </div>

      <template v-else>
        <div class="agent-mcp__modes">
          <label v-for="opt in modeOptions" :key="opt.value" class="agent-mcp__mode">
            <input
              type="radio"
              :value="opt.value"
              :checked="mode === opt.value"
              @change="setMode(opt.value)"
            />
            <span>{{ opt.label }}</span>
          </label>
        </div>

        <!-- The checklist only matters in `selected` mode; in `all` every entry
             is enabled and in `none` none is, so showing checkboxes would imply
             a control that does not exist. -->
        <div v-if="mode === 'selected'" class="agent-mcp__list">
          <label
            v-for="srv in servers"
            :key="srv.name"
            class="agent-mcp__row"
            :class="{ 'agent-mcp__row--off': !transportSupported(srv.transport) }"
          >
            <input
              type="checkbox"
              :checked="selectedNames.includes(srv.name)"
              :disabled="!transportSupported(srv.transport)"
              @change="toggle(srv.name, ($event.target as HTMLInputElement).checked)"
            />
            <span class="agent-mcp__name">{{ srv.name }}</span>
            <span class="agent-mcp__transport">{{ srv.transport }}</span>
            <span
              v-if="!transportSupported(srv.transport)"
              class="agent-mcp__note"
            >{{ t('settings.items.mcpAgentTransportUnsupported', { transport: srv.transport }) }}</span>
          </label>
        </div>
      </template>

      <div v-if="showsSlowStartHint" class="agent-mcp__hint agent-mcp__hint--warn">
        {{ t('settings.items.mcpAgentSlowStart') }}
      </div>

      <div v-if="error" class="agent-mcp__error">
        <AlertCircle :size="13" />
        <span>{{ error }}</span>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle } from 'lucide-vue-next'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { appLog } from '@/utils/appLog'

interface AgentLike {
  id: string
  backend: string
  transport?: string
  mcpServers?: { mode?: string; names?: string[] }
}

interface McpServerLike {
  name: string
  transport: string
}

const props = defineProps<{ agent: AgentLike | null }>()
const { t } = useI18n()
const { getServerValue, patchAgentField } = useSettingsConfig()

const error = ref('')
const saving = ref(false)

/** Backends the probe measured as accepting mcpServers and ignoring it. */
const UNSUPPORTED_BACKENDS = new Set(['dsh', 'deepseek', 'pi'])
/** Backends whose adapter rejects an sse server outright (session/new fails). */
const NO_SSE_BACKENDS = new Set(['codex', 'commandcode'])
/** Backends that attach MCP servers asynchronously after session/new. */
const SLOW_START_BACKENDS = new Set(['hermes'])

const servers = computed<McpServerLike[]>(() => {
  const raw = getServerValue('mcp.servers')
  return Array.isArray(raw) ? (raw as McpServerLike[]) : []
})

const backend = computed(() => props.agent?.backend ?? '')
const unsupportedBackend = computed(() => UNSUPPORTED_BACKENDS.has(backend.value))

const mode = computed(() => {
  // Zero value means "all" — see model.MCPServerPolicy.Normalized. Reading it
  // as "none" here would show the wrong mode for an agent that never set one.
  const m = props.agent?.mcpServers?.mode
  return m === 'none' || m === 'selected' ? m : 'all'
})

const selectedNames = computed(() => props.agent?.mcpServers?.names ?? [])

const modeOptions = computed(() => [
  { value: 'all', label: t('settings.items.mcpAgentAll') },
  { value: 'none', label: t('settings.items.mcpAgentNone') },
  { value: 'selected', label: t('settings.items.mcpAgentSelected') },
])

const showsSlowStartHint = computed(
  () => SLOW_START_BACKENDS.has(backend.value) && mode.value !== 'none',
)

function transportSupported(transport: string): boolean {
  if (transport === 'sse') return !NO_SSE_BACKENDS.has(backend.value)
  // stdio and http are supported everywhere the probe found MCP working; an
  // unsupported backend was already short-circuited above.
  return true
}

async function save(policy: { mode: string; names: string[] }) {
  if (!props.agent || saving.value) return
  saving.value = true
  error.value = ''
  try {
    await patchAgentField(props.agent.id, 'mcp_servers', policy)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
    appLog.w('AgentMCPSection', 'save failed', error.value)
  } finally {
    saving.value = false
  }
}

function setMode(next: string) {
  // Keep the existing selection when switching to `selected`, so toggling away
  // and back does not lose the user's ticks.
  void save({ mode: next, names: selectedNames.value })
}

function toggle(name: string, checked: boolean) {
  const next = checked
    ? [...selectedNames.value, name]
    : selectedNames.value.filter((n) => n !== name)
  void save({ mode: 'selected', names: next })
}

// A policy change re-fetches the agent list (patchAgentField does that for
// mcp_servers), so props.agent is replaced and the computeds above follow. No
// local mirror is kept on purpose: mirroring would let the UI show a mode the
// server normalized differently.
watch(
  () => props.agent?.id,
  () => {
    error.value = ''
  },
)
</script>

<style scoped>
.agent-mcp {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6) var(--space-7);
}
.agent-mcp__hint {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.agent-mcp__hint--warn {
  color: var(--color-orange);
}
.agent-mcp__warn,
.agent-mcp__error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
}
.agent-mcp__warn {
  color: var(--color-orange);
}
.agent-mcp__error {
  color: var(--color-red);
}
.agent-mcp__modes {
  display: flex;
  gap: var(--space-6);
  flex-wrap: wrap;
}
.agent-mcp__mode {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-md);
  color: var(--text-primary);
  cursor: pointer;
}
.agent-mcp__list {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
.agent-mcp__row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  font-size: var(--font-size-md);
}
/* Grayed AND disabled: a tick that cannot work is worse than no tick. */
.agent-mcp__row--off {
  opacity: 0.55;
  cursor: not-allowed;
}
.agent-mcp__name {
  color: var(--text-primary);
}
.agent-mcp__transport {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
}
.agent-mcp__note {
  font-size: var(--font-size-sm);
  color: var(--color-orange);
}
</style>
