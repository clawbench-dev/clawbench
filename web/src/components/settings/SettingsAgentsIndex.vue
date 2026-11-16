<template>
  <div class="settings-agents-index">
    <!-- The actions get their own card: "add something" is a different job
         from managing what is already installed. -->
    <SettingsCard>
      <!-- Create a custom ACP agent (issue #557). Sits above the rescan row so
           the two "add something" actions read together: rescan discovers
           installed CLIs, this one adds a tool that has no built-in backend. -->
      <div class="settings-agents-index__action-row" data-action="agent-create" @click="openCreate">
        <span class="settings-agents-index__action-label">{{ t('settings.items.agentCreateAcp') }}</span>
        <Plus :size="16" class="settings-agents-index__action-icon" />
      </div>
      <!-- Rescan row -->
      <div class="settings-agents-index__action-row refresh-spin" data-action="agent-rescan" :class="{ 'settings-agents-index__action-row--disabled': rescanning, 'refresh-spin--active': rescanning }" @click="handleRescan">
        <span class="settings-agents-index__action-label">{{ rescanning ? t('settings.items.agentRescanning') : t('settings.items.agentRescan') }}</span>
        <RefreshCw :size="16" class="settings-agents-index__action-icon" />
      </div>
    </SettingsCard>

    <SettingsCard :title="installedTitle">
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
          <span class="settings-agents-index__drag-handle" :title="t('settings.items.agentDragHandle')" @click.stop><GripVertical :size="16" /></span>
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
        </div>
      </VueDraggable>
      <div v-else class="settings-agents-index__empty">
        {{ t('settings.items.agentNoAgents') }}
      </div>
    </SettingsCard>

    <!-- Backends the server knows how to install but that are not on this
         machine yet. Sits after the installed list; when there is nothing to
         install it still reports that instead of disappearing and making the
         feature look absent. -->
    <SettingsCard
      v-if="!catalogLoading && catalogBackends.length > 0"
      :title="installableTitle"
    >
      <template v-if="installableBackends.length > 0">
        <div class="settings-agents-index__install-hint">{{ t('settings.items.agentInstallHint') }}</div>
        <div
          v-for="backend in installableBackends"
          :key="backend.id"
          class="settings-agents-index__install-row"
          :data-backend="backend.id"
        >
          <span class="settings-agents-index__icon">
            <AgentIcon :backend="backend.id" :name="backend.name" size="md" />
          </span>
          <div class="settings-agents-index__text">
            <span class="settings-agents-index__name">{{ backend.name }}</span>
            <span class="settings-agents-index__specialty">{{ backend.specialty }}</span>
          </div>
          <button class="fbtn settings-agents-index__install-btn" type="button" @click="startInstall(backend)">
            {{ t('settings.items.agentInstall') }}
          </button>
        </div>
      </template>
      <div v-else class="settings-agents-index__install-empty">
        {{ t('settings.items.agentInstallNone') }}
      </div>
    </SettingsCard>

    <CreateAcpAgentDialog
      :open="createOpen"
      :error-message="createError"
      @close="createOpen = false"
      @confirmed="handleCreateConfirmed"
    />

    <AgentInstallDialog
      v-if="installDialogBackend"
      :backend-id="installDialogBackend.id"
      :backend-name="installDialogBackend.name"
      :install-cmd="installDialogBackend.install_cmd || ''"
      @close="installDialogBackend = null"
      @installed="handleInstalled"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { GripVertical, Plus, RefreshCw } from 'lucide-vue-next'
import { VueDraggable } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import { useAgents } from '@/composables/useAgents'
import { useBackendCatalog, type BackendInfo } from '@/composables/useBackendCatalog'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useToast } from '@/composables/useToast'
import CreateAcpAgentDialog from './CreateAcpAgentDialog.vue'
import AgentInstallDialog from '@/components/AgentInstallDialog.vue'
import SettingsCard from './SettingsCard.vue'
import '@/assets/modal-footer-btn.css'

const emit = defineEmits<{
  navigate: [categoryId: string]
}>()

const { t } = useI18n()
const toast = useToast()
const { agents, defaultAgentId, loadAgents, rescanAgents, reorderAgents, createAcpAgent } = useAgents()
const { backends: catalogBackends, loading: catalogLoading, load: loadCatalog } = useBackendCatalog()

const rescanning = ref(false)
const createOpen = ref(false)
const installDialogBackend = ref<BackendInfo | null>(null)
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
  void loadCatalog()
})

const agentList = computed(() =>
  [...agents.value].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0))
)

/**
 * Backends already represented by at least one agent row. A backend can have
 * several agents (copies); one is enough to make it "installed".
 */
const installedBackendIds = computed(() => new Set(agents.value.map(a => a.backend)))

/**
 * Catalogue entries that are not installed here but carry a built-in install
 * command. Registry order is kept — it is the server's own display order, and
 * re-sorting would make the list jump between visits.
 */
const installableBackends = computed(() =>
  catalogBackends.value.filter(b => !!b.install_cmd && !installedBackendIds.value.has(b.id)),
)

const installableTitle = computed(() =>
  `${t('settings.items.agentInstallable')} · ${installableBackends.value.length}`,
)

const installedTitle = computed(() =>
  `${t('settings.items.agentInstalled')} · ${agents.value.length}`,
)

function startInstall(backend: BackendInfo) {
  installDialogBackend.value = backend
}

/**
 * A successful install makes the backend usable; re-read the agent list so the
 * row moves from "available" to "installed" without a manual rescan. The
 * catalogue itself does not change (the install command is a static spec).
 */
async function handleInstalled() {
  await loadAgents(true)
}

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
  padding: var(--space-4);
  /* Shared page surface (see --panel-page-bg). */
  background: var(--panel-page-bg);
  min-height: 100%;
}

/* The trailing gap after the last card is redundant page padding (same rule as
   SettingsIndex). */
.settings-agents-index > :deep(.settings-card:last-child) {
  margin-bottom: 0;
}

/* Rows are transparent like every other in-card settings row: the card body
   owns the surface, so hover has to mix against --bg-secondary (a
   --bg-secondary hover would vanish into the card). */
.settings-agents-index__action-row,
.settings-agents-index__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: calc(38 * var(--dp));
  padding: var(--space-4) var(--space-7);
  cursor: pointer;
  gap: var(--space-6);
  background: transparent;
  position: relative;
}

/* Action rows carry no leading glyph, so their divider starts at the row's own
   text inset; agent rows start after the drag handle + avatar. */
.settings-agents-index__action-row:not(:last-child)::after,
.settings-agents-index__row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.settings-agents-index__action-row:not(:last-child)::after {
  left: calc(16 * var(--dp));
}

.settings-agents-index__row:not(:last-child)::after {
  /* Inset to the text start: row padding + drag handle + row gap + icon + gap. */
  left: calc(var(--space-7) + var(--icon-size-md) + var(--space-6) + var(--icon-size-md) + var(--space-6));
}

@media (hover: hover) {
  .settings-agents-index__action-row:hover,
  .settings-agents-index__row:hover {
    background: color-mix(in srgb, var(--text-primary) 5%, var(--bg-secondary));
  }
}

.settings-agents-index__action-row:active,
.settings-agents-index__row:active {
  background: color-mix(in srgb, var(--text-primary) 10%, var(--bg-secondary));
}

.settings-agents-index__action-label {
  font-size: var(--font-size-md);
  color: var(--text-primary);
}

.settings-agents-index__action-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}

.settings-agents-index__action-row--disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
  pointer-events: none;
}

.settings-agents-index__drag-handle {
  flex-shrink: 0;
  /* Fixed width (the icon's own size) so the divider inset below stays aligned
     no matter how wide the glyph renders. */
  width: var(--icon-size-md);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: grab;
  color: var(--text-muted);
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
  padding: 1px calc(5 * var(--dp));
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

.settings-agents-index__empty {
  padding: calc(24 * var(--dp)) var(--space-7);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-lg);
}

/* ── Installable backends ──
   A read-only catalogue row: only the trailing button acts, so the row has no
   hover/press state of its own. The divider starts at the text column, same as
   the installed rows. */
.settings-agents-index__install-hint {
  padding: var(--space-4) var(--space-7) var(--space-3);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
}

.settings-agents-index__install-empty {
  padding: var(--space-5) var(--space-7);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

.settings-agents-index__install-row {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-6);
  min-height: calc(38 * var(--dp));
  padding: var(--space-4) var(--space-7);
}

.settings-agents-index__install-row .settings-agents-index__text {
  flex: 1;
}

.settings-agents-index__install-row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  right: 0;
  left: calc(var(--space-7) + var(--icon-size-md) + var(--space-6));
  height: 0.5px;
  background: var(--border-color);
}

.settings-agents-index__install-btn {
  margin-left: auto;
}
</style>
