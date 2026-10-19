<template>
  <div class="settings-agents-index">
    <!-- Create a custom ACP agent (issue #557). Sits above the rescan row so
         the two "add something" actions read together: rescan discovers
         installed CLIs, this one adds a tool that has no built-in backend. -->
    <div class="settings-agents-index__rescan-row" data-action="agent-create" @click="openCreate">
      <span class="settings-agents-index__rescan-label">{{ t('settings.items.agentCreateAcp') }}</span>
      <Plus :size="16" class="settings-agents-index__rescan-icon" />
    </div>
    <!-- Rescan row -->
    <div class="settings-agents-index__rescan-row refresh-spin" data-action="agent-rescan" :class="{ 'settings-agents-index__rescan-row--disabled': rescanning, 'refresh-spin--active': rescanning }" @click="handleRescan">
      <span class="settings-agents-index__rescan-label">{{ rescanning ? t('settings.items.agentRescanning') : t('settings.items.agentRescan') }}</span>
      <RefreshCw :size="16" class="settings-agents-index__rescan-icon" />
    </div>
    <!-- Run the integration self-test for EVERY agent at once: the "is
         everything still working?" action, as opposed to the per-agent button
         on the detail page. Each check is state-only, so testing all of them is
         as cheap as testing one. -->
    <div
      class="settings-agents-index__rescan-row"
      data-action="agent-selftest-all"
      :class="{ 'settings-agents-index__rescan-row--disabled': testingAll }"
      @click="handleTestAll"
    >
      <span class="settings-agents-index__rescan-label">{{ testingAll ? t('settings.items.agentSelfTestAllRunning') : t('settings.items.agentSelfTestAll') }}</span>
      <Activity :size="16" class="settings-agents-index__rescan-icon" />
    </div>
    <!-- Per-agent results, worst first so a failure is never below the fold. -->
    <div v-if="testAllResults.length > 0" class="settings-agents-index__selftest" data-testid="agent-selftest-results">
      <div class="settings-agents-index__selftest-summary">
        {{ t('settings.items.agentSelfTestAllSummary', {
          ok: testAllSummary.ok,
          warn: testAllSummary.warn,
          fail: testAllSummary.fail,
        }) }}
      </div>
      <div
        v-for="row in testAllResults"
        :key="row.agentId"
        class="settings-agents-index__selftest-row"
        :class="`settings-agents-index__selftest-row--${row.level}`"
      >
        <span class="settings-agents-index__selftest-status">{{ testAllStatusLabel(row.level) }}</span>
        <span class="settings-agents-index__selftest-name">{{ row.name }}</span>
        <span class="settings-agents-index__selftest-detail">{{ row.detail }}</span>
      </div>
    </div>
    <VueDraggable
      v-if="localAgents.length > 0"
      v-model="localAgents"
      handle=".settings-agents-index__drag-handle"
      @end="onDragEnd"
    >
      <div
        v-for="agent in localAgents"
        :key="agent.id"
        class="settings-agents-index__row"
        @click="$emit('navigate', `agents:${agent.id}`)"
      >
        <span class="settings-agents-index__drag-handle" :title="t('settings.items.agentDragHandle')" @click.stop>≡</span>
        <div class="settings-agents-index__left">
          <span class="settings-agents-index__icon"><AgentIcon :backend="agent.backend" :name="agent.name" :avatar="agent.avatar" size="md" /></span>
          <div class="settings-agents-index__text">
            <div class="settings-agents-index__name-row">
              <span class="settings-agents-index__name">{{ agent.name }}</span>
              <span v-if="agent.id === defaultAgentId" class="settings-agents-index__default-badge">{{ t('chat.sessionSetting.defaultBadge') }}</span>
            </div>
            <span v-if="agent.specialty" class="settings-agents-index__specialty">{{ agent.specialty }}</span>
          </div>
        </div>
        <ChevronRight class="settings-agents-index__arrow" :size="18" />
      </div>
    </VueDraggable>
    <div v-if="agentList.length === 0" class="settings-agents-index__empty">
      {{ t('settings.items.agentNoAgents') }}
    </div>
    <CreateAcpAgentDialog
      :open="createOpen"
      :error-message="createError"
      @close="createOpen = false"
      @confirmed="handleCreateConfirmed"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Activity, ChevronRight, Plus, RefreshCw } from 'lucide-vue-next'
import { VueDraggable } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import { useAgents } from '@/composables/useAgents'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useToast } from '@/composables/useToast'
import { appLog } from '@/utils/appLog'
import CreateAcpAgentDialog from './CreateAcpAgentDialog.vue'
import { useAgentSelfTest, type AgentSelfTestResult } from '@/composables/useAgentSelfTest'

const emit = defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()
const toast = useToast()
const { agents, defaultAgentId, loadAgents, rescanAgents, reorderAgents, createAcpAgent } = useAgents()

const rescanning = ref(false)
const testingAll = ref(false)
// One row per agent after a full run. Kept as a flat list (not a map) so the
// template can render it in the order produced — worst first.
interface TestAllRow {
  agentId: string
  name: string
  level: 'ok' | 'warn' | 'fail'
  detail: string
}
const testAllResults = ref<TestAllRow[]>([])

const testAllSummary = computed(() => {
  const counts = { ok: 0, warn: 0, fail: 0 }
  for (const r of testAllResults.value) counts[r.level]++
  return counts
})

/** Run the self-test for every loaded agent. */
async function handleTestAll() {
  if (testingAll.value) return
  testingAll.value = true
  testAllResults.value = []
  try {
    const list = [...agentList.value]
    // Each run is a separate composable instance: the composable holds per-call
    // state, so sharing one instance would have every call overwrite the last.
    const rows = await Promise.all(list.map(async (a): Promise<TestAllRow> => {
      const t0 = useAgentSelfTest()
      await t0.run(a.id)
      if (t0.error.value) {
        return { agentId: a.id, name: a.name, level: 'fail', detail: t0.error.value }
      }
      const r: AgentSelfTestResult | null = t0.result.value
      if (!r) {
        return { agentId: a.id, name: a.name, level: 'fail', detail: t('settings.items.agentSelfTestNoResult') }
      }
      const level = r.failed > 0 ? 'fail' : r.warned > 0 ? 'warn' : 'ok'
      const firstProblem = r.checks.find(c => c.status === 'fail') ?? r.checks.find(c => c.status === 'warn')
      return {
        agentId: a.id,
        name: a.name,
        level,
        detail: firstProblem?.detail
          ?? t('settings.items.agentSelfTestPassedCount', { passed: r.passed }),
      }
    }))
    // Worst first: a failing agent must not be buried under passing ones.
    const rank = { fail: 0, warn: 1, ok: 2 } as const
    testAllResults.value = rows.sort((x, y) => rank[x.level] - rank[y.level])
  } catch (err: unknown) {
    appLog.e('SettingsAgentsIndex', 'test-all failed', err)
    toast.show(t('settings.items.agentSelfTestAllFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  } finally {
    testingAll.value = false
  }
}

/** Label for an aggregate level (ok / warn / fail). */
function testAllStatusLabel(level: TestAllRow['level']): string {
  switch (level) {
    case 'ok':
      return t('settings.items.agentSelfTestStatusOk')
    case 'warn':
      return t('settings.items.agentSelfTestStatusWarn')
    default:
      return t('settings.items.agentSelfTestStatusFail')
  }
}

const createOpen = ref(false)
// Server-side create failure (name collision, empty command) shown inside the
// dialog, which stays open so the user can correct the input and retry.
const createError = ref('')

/** Open the create dialog with a clean slate.
 *
 * Clearing here (not only when a submit starts) is what keeps a stale red
 * banner from the previous attempt off the next one — the dialog is unmounted
 * content-wise while closed, so it would otherwise reappear over empty fields. */
function openCreate() {
  createError.value = ''
  createOpen.value = true
}

async function handleCreateConfirmed({ name, command }: { name: string; command: string }) {
  createError.value = ''
  try {
    const newId = await createAcpAgent(name, command)
    createOpen.value = false
    toast.show(t('settings.items.agentCreateAcpSuccess'), { icon: 'success', type: 'success', duration: 3000 })
    // Land on the new agent's detail page so the user sees what was created and
    // can add an avatar / adjust settings. Guard on a non-empty id for the same
    // reason as the copy flow: an empty id would route to `agents:`.
    if (newId) emit('navigate', `agents:${newId}`)
  } catch (err) {
    // Every 4xx here is correctable in the dialog: a taken name, a reserved
    // name, an over-long name, an empty or over-long command. Only a genuine
    // server failure closes it.
    const msgKey = (err as { msgKey?: string } | null)?.msgKey
    switch (msgKey) {
      case 'AgentNameTaken':
        createError.value = t('settings.items.agentCopyNameTaken')
        return
      case 'AgentNameReserved':
        createError.value = t('settings.items.agentNameReserved')
        return
      case 'InvalidAgentName':
        createError.value = t('settings.items.agentCreateAcpInvalidName')
        return
      case 'InvalidAcpCommand':
        createError.value = t('settings.items.agentCreateAcpInvalidCommand')
        return
      default:
        createOpen.value = false
        toast.show(t('settings.items.agentCreateAcpFailed'), { icon: 'warning', type: 'error', duration: 3000 })
    }
  }
}

onMounted(() => {
  loadAgents(true)
})

const agentList = computed(() =>
  [...agents.value].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0))
)

// Local mirror the draggable mutates in place. Synced from the server order so
// a failed reorder (rolled back) or a background reload lands here too.
const localAgents = ref([...agentList.value])
watch(agentList, (val) => {
  localAgents.value = [...val]
}, { deep: true })

async function onDragEnd() {
  const ids = localAgents.value.map(a => a.id)
  try {
    await reorderAgents(ids)
  } catch {
    toast.show(t('settings.items.agentReorderFailed'), { icon: 'warning', type: 'error', duration: 3000 })
    localAgents.value = [...agentList.value]
  }
}

async function handleRescan() {
  rescanning.value = true
  try {
    await rescanAgents()
    toast.show(t('settings.items.agentRescanSuccess'), { icon: 'success', type: 'success', duration: 3000 })
  } catch {
    toast.show(t('settings.items.agentRescanFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  } finally {
    rescanning.value = false
  }
}
</script>

<style scoped>
.settings-agents-index {
  padding: var(--space-4) 0;
  background: var(--bg-secondary);
  min-height: 100%;
}

.settings-agents-index__rescan-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 38px;
  padding: var(--space-4) var(--space-7);
  cursor: pointer;
  background: var(--bg-primary);
  position: relative;
}

.settings-agents-index__rescan-row::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  .settings-agents-index__rescan-row:hover {
    background: var(--bg-secondary);
  }
}

.settings-agents-index__rescan-row:active {
  background: var(--bg-tertiary);
}

.settings-agents-index__rescan-label {
  font-size: var(--font-size-md);
  color: var(--text-primary);
}

.settings-agents-index__rescan-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}

.settings-agents-index__rescan-row--disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
  pointer-events: none;
}

/* ── Test-all results ───────────────────────────────────────────────────── */

.settings-agents-index__selftest {
  padding: var(--space-4) var(--space-7);
  background: var(--bg-primary);
  border-bottom: 0.5px solid var(--border-color);
}

.settings-agents-index__selftest-summary {
  font-size: var(--font-size-md);
  color: var(--text-secondary);
  margin-bottom: var(--space-3);
}

.settings-agents-index__selftest-row {
  display: grid;
  grid-template-columns: 3.5em minmax(6em, auto) 1fr;
  gap: 0 var(--space-4);
  font-size: var(--font-size-md);
  line-height: var(--line-height-snug);
  padding: var(--space-1) 0;
}

/* Status colors follow the settings panel's connectivity-test results
   (#22c55e / #f59e0b / #ef4444); variables.css has no --color-warning token. */
.settings-agents-index__selftest-status {
  font-weight: 600;
  color: var(--text-muted);
}

.settings-agents-index__selftest-row--ok .settings-agents-index__selftest-status {
  color: #22c55e;
}

.settings-agents-index__selftest-row--warn .settings-agents-index__selftest-status {
  color: #f59e0b;
}

.settings-agents-index__selftest-row--fail .settings-agents-index__selftest-status {
  color: #ef4444;
}

.settings-agents-index__selftest-name {
  color: var(--text-primary);
}

.settings-agents-index__selftest-detail {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  overflow-wrap: anywhere;
}

.settings-agents-index__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 38px;
  padding: var(--space-4) var(--space-7);
  cursor: pointer;
  gap: var(--space-6);
  background: var(--bg-primary);
  position: relative;
}

.settings-agents-index__row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  /* Inset to the text start: row padding + drag handle + row gap + icon + gap. */
  left: calc(var(--space-7) + var(--icon-size-md) + var(--space-6) + var(--icon-size-md) + var(--space-6));
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  .settings-agents-index__row:hover {
    background: var(--bg-secondary);
  }
}

.settings-agents-index__row:active {
  background: var(--bg-tertiary);
}

.settings-agents-index__drag-handle {
  flex-shrink: 0;
  /* Fixed width (the icon's own size) so the divider inset below stays aligned
     no matter how wide the glyph renders. */
  width: var(--icon-size-md);
  text-align: center;
  cursor: grab;
  color: var(--text-muted);
  font-size: var(--font-size-2xl);
  line-height: 1;
  user-select: none;
}

.settings-agents-index__drag-handle:active {
  cursor: grabbing;
}

.settings-agents-index__left {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  min-width: 0;
  flex: 1;
}

.settings-agents-index__icon {
  flex-shrink: 0;
  line-height: 1;
}

.settings-agents-index__text {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.settings-agents-index__name-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.settings-agents-index__name {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-agents-index__default-badge {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  line-height: 1;
  padding: 1px 5px;
  border-radius: var(--radius-xs);
  background: var(--accent-color, #0066cc);
  color: #fff;
  white-space: nowrap;
}

.settings-agents-index__specialty {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-agents-index__arrow {
  flex-shrink: 0;
  color: var(--text-muted);
  margin-left: var(--space-2);
}

.settings-agents-index__empty {
  padding:24px var(--space-7);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-lg);
}
</style>
