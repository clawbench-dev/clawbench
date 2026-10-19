<template>
  <!-- Fallback for a stale reference: the deep link (or a lingering nav-stack
       entry) can point at an agent that no longer exists — it was deleted, or
       dropped by a rescan. Without this branch the page rendered an empty shell
       (no cards) with only a copy/delete pair, and delete silently no-opped.
       Gated on agentsLoaded so a valid agent does not flash this before the
       list has been fetched. -->
  <div v-if="agentMissing" class="settings-agent-detail settings-agent-detail--missing">
    <UserX :size="32" class="settings-agent-detail__missing-icon" />
    <span class="settings-agent-detail__missing-title">{{ t('settings.items.agentNotFound') }}</span>
    <span class="settings-agent-detail__missing-hint">{{ t('settings.items.agentNotFoundHint') }}</span>
    <button class="fbtn fbtn-primary" @click="emit('back')">
      <ArrowLeft :size="14" />
      <span>{{ t('settings.items.agentBackToList') }}</span>
    </button>
  </div>
  <div v-else class="settings-agent-detail">
    <SettingsCard
      v-for="group in groups"
      :key="group.title"
      :title="group.title"
    >
      <template v-for="item in group.items" :key="item.key">
        <!-- Custom avatar: a dedicated row (SettingsItem has no image control).
             It leads the Identity card so the avatar sits with the name. -->
        <div v-if="item.type === 'avatar'" class="settings-agent-detail__avatar-row">
          <AgentIcon :backend="agent?.backend || ''" :name="agent?.name" :avatar="agent?.avatar" size="xl" />
          <button class="fbtn settings-agent-detail__avatar-btn" @click="avatarPickerOpen = true">
            {{ t('settings.items.agentAvatarChange') }}
          </button>
        </div>
        <SettingsItem
          v-else
          :label="item.label"
          :description="item.description"
          :note="item.note"
          :type="item.type"
          :model-value="getItemValue(item)"
          :options="item.options"
          :warning="item.warning"
          :disabled="item.disabled"
          :force-close="activeKey !== null && activeKey !== item.key"
          @update:model-value="(v: unknown) => handleUpdate(item, v)"
          @edit-toggle="(open: boolean) => handleEditToggle(item.key, open)"
        />
      </template>
    </SettingsCard>
    <!-- Integration self-test. Runs the backend's checks against this agent's
         real configuration and prints one row per check, so a broken setup is
         diagnosable without reading server logs. -->
    <div class="settings-agent-detail__selftest">
      <div class="settings-agent-detail__selftest-actions">
        <button
          class="fbtn settings-agent-detail__selftest-btn"
          :disabled="selfTest.testing.value"
          @click="handleSelfTest"
        >
          <Activity :size="16" class="settings-agent-detail__action-icon" />
          <span>{{ selfTest.testing.value ? t('settings.items.agentSelfTestRunning') : t('settings.items.agentSelfTest') }}</span>
        </button>
        <!-- Opt-in real turn. Separate button because it costs tokens: a user
             checking configuration should not pay for a model call. -->
        <button
          class="fbtn settings-agent-detail__selftest-btn"
          :disabled="selfTest.testing.value"
          :title="t('settings.items.agentSelfTestDeepHint')"
          @click="handleSelfTestDeep"
        >
          <Zap :size="16" class="settings-agent-detail__action-icon" />
          <span>{{ t('settings.items.agentSelfTestDeep') }}</span>
        </button>
      </div>

      <!-- Deep run outcome: the model's own words are the proof. -->
      <div v-if="selfTest.deepResult.value" class="settings-agent-detail__selftest-deep">
        <div class="settings-agent-detail__selftest-deep-line">
          {{ t('settings.items.agentSelfTestDeepReply', { reply: selfTest.deepResult.value.reply ?? '' }) }}
        </div>
        <div class="settings-agent-detail__selftest-deep-meta">
          {{ t('settings.items.agentSelfTestDeepMeta', {
            transport: selfTest.deepResult.value.transport ?? '',
            stopReason: selfTest.deepResult.value.stopReason ?? '',
            ms: selfTest.deepResult.value.durationMs ?? 0,
          }) }}
        </div>
      </div>

      <div v-if="selfTest.error.value" class="settings-agent-detail__selftest-error">
        {{ selfTest.error.value }}
      </div>

      <!-- NOT an `v-else-if`: a deep run reports BOTH an error and the static
           checks it rode along with, and the checks are the part that explains
           the failure. Gating them on "no error" hid exactly the detail the
           user needs at the one moment they need it. -->
      <div
        v-if="selfTest.result.value"
        class="settings-agent-detail__selftest-result"
        :class="{ 'settings-agent-detail__selftest-result--fail': !selfTest.result.value.ok }"
      >
        <div class="settings-agent-detail__selftest-summary">
          {{ t('settings.items.agentSelfTestSummary', {
            passed: selfTest.result.value.passed,
            warned: selfTest.result.value.warned,
            failed: selfTest.result.value.failed,
          }) }}
        </div>
        <ul class="settings-agent-detail__selftest-list">
          <li
            v-for="check in selfTest.result.value.checks"
            :key="check.key"
            class="settings-agent-detail__selftest-check"
            :class="`settings-agent-detail__selftest-check--${check.status}`"
          >
            <span class="settings-agent-detail__selftest-status">{{ selfTestStatusLabel(check.status) }}</span>
            <span class="settings-agent-detail__selftest-key">{{ t(`settings.items.agentSelfTestCheck.${check.key}`) }}</span>
            <span v-if="check.detail" class="settings-agent-detail__selftest-detail">{{ check.detail }}</span>
          </li>
        </ul>
      </div>
    </div>
    <!-- Copy / disable / delete on the same row -->
    <div class="settings-agent-detail__actions">
      <button class="fbtn settings-agent-detail__action-btn" @click="startCopy">
        <Copy :size="16" class="settings-agent-detail__action-icon" />
        <span>{{ t('settings.items.agentCopy') }}</span>
      </button>
      <!-- Disable / enable toggle. The default agent cannot be disabled (the
           backend refuses it too), so its button is inert with a tooltip. -->
      <button
        class="fbtn settings-agent-detail__action-btn"
        :disabled="isDefaultAgentValue"
        :title="isDefaultAgentValue ? t('settings.items.agentCannotDisableDefault') : undefined"
        @click="handleToggleDisabled"
      >
        <component :is="agent?.disabled ? Play : Ban" :size="16" class="settings-agent-detail__action-icon" />
        <span>{{ agent?.disabled ? t('settings.items.agentEnable') : t('settings.items.agentDisable') }}</span>
      </button>
      <button class="fbtn fbtn-danger settings-agent-detail__delete-btn" @click="handleDelete">
        <Trash2 :size="16" class="settings-agent-detail__delete-icon" />
        <span>{{ t('settings.items.agentDelete') }}</span>
      </button>
    </div>
    <CopyAgentDialog
      :open="copying"
      :source-name="agent?.name ?? ''"
      :error-message="copyError"
      @close="copying = false"
      @confirmed="handleCopyConfirmed"
    />
    <AgentAvatarPicker
      :open="avatarPickerOpen"
      :agent-name="agent?.name ?? ''"
      @close="avatarPickerOpen = false"
      @saved="handleAvatarSaved"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { Activity, ArrowLeft, Ban, Copy, Play, Trash2, UserX, Zap } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import SettingsItem from './SettingsItem.vue'
import SettingsCard from './SettingsCard.vue'
import CopyAgentDialog from './CopyAgentDialog.vue'
import AgentAvatarPicker from './AgentAvatarPicker.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useAgents, populateACPStateFromCache } from '@/composables/useAgents'
import { patchAgentField } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useAgentSelfTest, type AgentSelfTestCheck } from '@/composables/useAgentSelfTest'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{
  agentId: string
}>()

const emit = defineEmits<{
  deleted: []
  /** Leave the detail page without deleting anything (stale-reference fallback). */
  back: []
  /**
   * Switch to another agent's detail page in place — fired after a successful
   * copy so the panel shows the new agent's config rather than staying on the
   * source. Carries the full category id (`agents:<id>`); the parent REPLACES
   * the current stack entry (a push would grow the breadcrumb).
   */
  navigateReplace: [categoryId: string]
}>()

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()
const selfTest = useAgentSelfTest()
const { loadAgents, getAgent, agentsLoaded, updateAgentField, deleteAgent, duplicateAgent, defaultAgentId, setDefaultAgent, getAgentThinkingEffortLevelsIncludingACP } = useAgents()
const activeKey = ref<string | null>(null)
const copying = ref(false)
// Server-side copy failure (e.g. the name is already taken) shown inside the
// dialog; the dialog stays open so the user can pick another name.
const copyError = ref('')
const avatarPickerOpen = ref(false)

onMounted(async () => {
  await loadAgents(true)
  // Merge ACP-reported models into agent.models so the preferred-model selector
  // shows the same stable list as the chat model drawer (Issue #404). Without
  // this, /api/agents now returns the pure CLI list and ACP-only models would
  // be missing when this agent isn't the current session's agent.
  await populateACPStateFromCache(props.agentId)
})

const agent = computed(() => getAgent(props.agentId))

// Whether this is the global default agent. The default agent can neither be
// deleted nor disabled (the backend refuses both), so the disable button is
// inert for it.
const isDefaultAgentValue = computed(() => agent.value?.id === defaultAgentId.value)

// The requested agent is gone (deleted, or dropped by a rescan). Requiring
// agentsLoaded keeps the first paint of a valid agent from being reported as
// missing — the list starts empty, so before the fetch lands every id is absent.
const agentMissing = computed(() => !agent.value && agentsLoaded.value)

// Determine if agent is ACP-only (has acpCommand but no CLI backend implementation)
const isACPOnly = computed(() => {
  const a = agent.value
  if (!a) return false
  return a.acpCommand && !a.supportsCLI
})

// A user-defined ACP agent (issue #557). Its backend has no registry entry, so
// `supportsCLI` is false and every capability comes from the agent at runtime —
// the same shape as an ACP-only built-in, which is why it needs no special
// handling beyond keeping the command field editable when it is empty.
const isCustomAcpAgent = computed(() => agent.value?.backend === 'custom-acp')

/**
 * Whether auto-discovery owns this agent's `acp_command`.
 *
 * The server computes this (`backendHasSpec`, from model.FindSpecByBackend) and
 * applies the exact same predicate in its PATCH guard, so the UI cannot offer
 * an edit the API rejects — or hide one it would accept.
 */
const hasBackendSpec = computed(() => agent.value?.backendHasSpec === true)

// Determine if agent supports both ACP and CLI (dual transport)
const isDualTransport = computed(() => {
  const a = agent.value
  if (!a) return false
  return a.acpCommand && a.supportsCLI !== false
})

interface AgentItem {
  key: string
  label: string
  description?: string
  /** Caveat about when an edit takes effect (see SettingsItem's `note`). */
  note?: string
  type: 'switch' | 'select' | 'number' | 'text' | 'slider' | 'action' | 'info' | 'header' | 'password' | 'textarea' | 'avatar'
  options?: { label: string; value: unknown }[]
  warning?: string
  value?: unknown
  patchField?: string
  disabled?: boolean
}

const items = computed<AgentItem[]>(() => {
  const a = agent.value
  if (!a) return []

  const result: AgentItem[] = []

  // -- Identity section (leading card): avatar + name + specialty + system prompt.
  // Avatar, identity and the system prompt used to be three separate groups;
  // they now form the single leading "Identity" card.
  result.push({ key: 'header-identity', label: t('settings.items.agentSectionIdentity'), type: 'header' })

  // Custom avatar: a dedicated row type — SettingsItem has no image control.
  result.push({ key: 'avatar', label: t('settings.items.agentAvatar'), type: 'avatar' })

  result.push({
    key: 'name',
    label: t('settings.items.agentName'),
    type: 'text',
    patchField: 'name',
  })

  result.push({
    key: 'specialty',
    label: t('settings.items.agentSpecialty'),
    type: 'text',
    patchField: 'specialty',
  })

  // System Prompt: editable for CLI/dual agents, read-only info for ACP-only.
  if (isACPOnly.value) {
    result.push({
      key: 'custom_system_prompt',
      label: t('settings.items.agentSystemPrompt'),
      description: t('settings.items.agentSystemPromptACPNote'),
      type: 'info',
    })
  } else {
    result.push({
      key: 'custom_system_prompt',
      label: t('settings.items.agentSystemPrompt'),
      description: t('settings.items.agentSystemPromptDesc'),
      note: t('settings.items.agentSystemPromptNote'),
      type: 'textarea',
      warning: t('settings.items.agentSystemPromptWarning'),
      patchField: 'custom_system_prompt',
    })
  }

  // -- Preference section --
  result.push({ key: 'header-preference', label: t('settings.items.agentSectionPreference'), type: 'header' })

  // Set as Default Agent
  result.push({
    key: 'is_default_agent',
    label: t('settings.items.agentIsDefault'),
    description: t('settings.items.agentIsDefaultDesc'),
    type: 'switch',
  })

  // Preferred Model
  if (a.models && a.models.length > 0) {
    result.push({
      key: 'preferred_model',
      label: t('settings.items.agentPreferredModel'),
      type: 'select',
      options: a.models.map((m: { name: string; id: string }) => ({
        label: m.name || m.id,
        value: m.id,
        modelName: m.name || m.id,
        // CodeBuddy ships models that share a display name (deepseek-v4-pro and
        // deepseek-v4-pro-exclusive are both "Deepseek-V4-Pro"), so the id is
        // the only way to tell them apart in the picker. Some ACP agents carry
        // a JSON-shaped wire id; the shared ModelRow shows it readably on a
        // second line (the value stays untouched).
        modelId: m.id,
      })),
      patchField: 'preferred_model',
    })
  }

  // Preferred Thinking Effort
  if (a.thinkingEffortLevels && a.thinkingEffortLevels.length > 0) {
    result.push({
      key: 'preferred_thinking_effort',
      label: t('settings.items.agentPreferredThinkingEffort'),
      type: 'select',
      options: a.thinkingEffortLevels.map((l: string) => ({ label: l, value: l })),
      patchField: 'preferred_thinking_effort',
    })
  }

  // Transport (only for dual-transport agents — both ACP and CLI supported)
  if (isDualTransport.value) {
    result.push({
      key: 'transport',
      label: t('settings.items.agentTransport'),
      type: 'select',
      options: [
        { label: 'CLI', value: 'cli' },
        { label: 'ACP-stdio', value: 'acp-stdio' },
      ],
      patchField: 'transport',
    })
  }

  // Preferred Mode (only for ACP agents with available modes)
  if (a.acpCommand && a.acpAvailableModes && a.acpAvailableModes.length > 0) {
    result.push({
      key: 'preferred_mode',
      label: t('settings.items.agentPreferredMode'),
      type: 'select',
      options: a.acpAvailableModes.map((m: { name: string; id: string }) => ({ label: m.name || m.id, value: m.id })),
      patchField: 'preferred_mode',
    })
  }

  // New-session auto-approve default
  result.push({
    key: 'auto_approve',
    label: t('settings.items.agentAutoApprove'),
    description: t('settings.items.agentAutoApproveDesc'),
    type: 'switch',
    patchField: 'auto_approve',
  })

  // Scope: 仅当前项目 (only visible in the current project). The default agent
  // cannot be scoped (the backend refuses it too), so its switch is disabled
  // with a tooltip — mirroring the disable-default rule.
  result.push({
    key: 'project_scope',
    label: t('settings.items.agentProjectOnly'),
    description: t('settings.items.agentProjectOnlyDesc'),
    type: 'switch',
    patchField: 'project_scope',
    warning: isDefaultAgentValue.value ? t('settings.items.agentCannotScopeDefault') : undefined,
    disabled: isDefaultAgentValue.value,
  })

  // -- Information section --
  result.push({ key: 'header-info', label: t('settings.items.agentSectionInfo'), type: 'header' })

  result.push({
    key: 'backend',
    label: t('settings.items.agentBackend'),
    type: 'info',
  })

  if (a.command) {
    result.push({
      key: 'command',
      label: t('settings.items.agentCommand'),
      type: 'info',
    })
  }

  const modelCount = a.models?.length ?? 0
  result.push({
    key: 'models_count',
    label: t('settings.items.agentModels'),
    type: 'info',
    value: t('settings.items.agentModelCount', { count: modelCount }),
  })

  // Usage counts: interactive sessions (archived included) and tasks bound to
  // this agent. Both are derived server-side and gate the delete button.
  result.push({
    key: 'session_count',
    label: t('settings.items.agentSessionCountLabel'),
    description: t('settings.items.agentSessionCountDesc'),
    type: 'info',
    value: t('settings.items.agentSessionCount', { count: a.sessionCount ?? 0 }),
  })

  result.push({
    key: 'task_count',
    label: t('settings.items.agentTaskCountLabel'),
    description: t('settings.items.agentTaskCountDesc'),
    type: 'info',
    value: t('settings.items.agentTaskCount', { count: a.taskCount ?? 0 }),
  })

  // ACP command. Editable for any agent whose command is NOT owned by
  // auto-discovery.
  //
  // For a spec-backed agent the field is read-only on purpose. Discovery owns
  // `acp_command` there — model.RefreshAgents' syncAgentInfrastructure does
  // `UPDATE agents SET acp_command = <spec>, transport = ... WHERE backend = ?`
  // — so any edit a user made would be silently reverted on the next startup or
  // "Rescan", including a deliberate clear. Offering an input that quietly
  // undoes itself is worse than showing the effective value.
  //
  // Agents with no registry spec (a YAML-defined one like the E2E acp-mock, or
  // a custom ACP agent) are not touched by that sync, and the API accepts the
  // PATCH for them, so the field stays editable there.
  if (a.acpCommand || isCustomAcpAgent.value) {
    const editable = !hasBackendSpec.value
    result.push({
      key: 'acp_command',
      label: t('settings.items.agentAcpCommand'),
      description: t(
        editable
          ? 'settings.items.agentAcpCommandDesc'
          : 'settings.items.agentAcpCommandDescBuiltin'
      ),
      // `note` (not `warning`): the latter only renders inside the textarea
      // editor, and this caution must be visible before the user opens the
      // field — the command runs with the server's privileges.
      note: t('settings.items.agentAcpCommandWarning'),
      type: editable ? 'text' : 'info',
      patchField: editable ? 'acp_command' : undefined,
    })
  }

  return result
})

// Group the agent items into cards by header items.
const groups = computed<{ title: string; items: AgentItem[] }[]>(() => {
  const out: { title: string; items: AgentItem[] }[] = []
  let cur: { title: string; items: AgentItem[] } | null = null
  for (const item of items.value) {
    if (item.type === 'header') {
      if (cur) out.push(cur)
      cur = { title: item.label, items: [] }
    } else {
      if (!cur) cur = { title: t('settings.items.agentSectionPreference'), items: [] }
      cur.items.push(item)
    }
  }
  if (cur) out.push(cur)
  return out
})

function getItemValue(item: AgentItem): unknown {
  if (item.type === 'header') return undefined
  if (item.value !== undefined) return item.value

  const a = agent.value
  if (!a) return ''

  switch (item.key) {
    case 'is_default_agent':
      return a.id === defaultAgentId.value
    case 'preferred_model':
      return a.preferredModel || (a.models?.length ? a.models.find((m: { default: boolean }) => m.default)?.id || a.models[0]?.id : '')
    case 'preferred_thinking_effort':
      return a.preferredThinkingEffort || ''
    case 'preferred_mode':
      return a.preferredMode || ''
    case 'transport':
      return a.transport || (a.acpCommand ? 'acp-stdio' : 'cli')
    case 'auto_approve':
      return !!a.autoApprove
    case 'project_scope':
      // Scoped iff the agent carries a project id. The list is already filtered
      // to this project, so a truthy projectId means "only this project".
      return a.projectId != null
    case 'name':
      return a.name || ''
    case 'specialty':
      return a.specialty || ''
    case 'custom_system_prompt':
      // The user's own text is the only prompt value stored; the shared prefix is
      // composed at runtime and never round-trips through the client.
      return a.customSystemPrompt || ''
    case 'backend':
      return a.backend || ''
    case 'command':
      return a.command || ''
    case 'acp_command':
      return a.acpCommand || ''
    default:
      return ''
  }
}

async function handleUpdate(item: AgentItem, value: unknown) {
  // Handle set-as-default toggle
  if (item.key === 'is_default_agent') {
    if (value) {
      try {
        await setDefaultAgent(props.agentId)
      } catch {
        toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
      }
    }
    return
  }

  if (!item.patchField) return

  try {
    await patchAgentField(props.agentId, item.patchField, value as string | number | boolean | null)
    // When changing transport, clear preferred thinking effort if it's no longer valid
    if (item.patchField === 'transport') {
      const newTransport = value as string
      const a = getAgent(props.agentId)
      const currentPref = a?.preferredThinkingEffort || ''
      if (currentPref) {
        const validLevels = newTransport === 'acp-stdio'
          ? getAgentThinkingEffortLevelsIncludingACP(props.agentId)
          : (a?.thinkingEffortLevels || [])
        if (!validLevels.includes(currentPref)) {
          await patchAgentField(props.agentId, 'preferred_thinking_effort', '')
          updateAgentField(props.agentId, 'preferredThinkingEffort', '')
        }
      }
    }
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function handleAvatarSaved(svg: string) {
  try {
    await patchAgentField(props.agentId, 'avatar', svg)
    avatarPickerOpen.value = false
    toast.show(t('settings.items.agentAvatarSaved'), { icon: 'success', duration: 2000 })
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

function handleEditToggle(key: string, open: boolean) {
  if (open) {
    activeKey.value = key
  } else if (activeKey.value === key) {
    activeKey.value = null
  }
}

function startCopy() {
  copyError.value = ''
  copying.value = true
}

async function handleCopyConfirmed(newName: string) {
  // Clear any previous collision before the new attempt so a repeated failure
  // re-triggers the dialog's error watcher (an unchanged prop would not).
  copyError.value = ''
  try {
    const newId = await duplicateAgent(props.agentId, newName)
    copying.value = false
    toast.show(t('settings.items.agentCopied'), { icon: 'success', type: 'success', duration: 3000 })
    // Switch the panel to the freshly created agent's config page. Guard on a
    // non-empty id: an older backend that does not return the created agent
    // would otherwise navigate to `agents:` (a bogus, empty-id route).
    if (newId) emit('navigateReplace', `agents:${newId}`)
  } catch (err) {
    // A name collision is recoverable: keep the dialog open and show the
    // reason inline so the user can correct it, instead of a bare toast.
    const msgKey = (err as { msgKey?: string } | null)?.msgKey
    if (msgKey === 'AgentNameTaken') {
      copyError.value = t('settings.items.agentCopyNameTaken')
      return
    }
    copying.value = false
    toast.show(t('settings.items.agentCopyFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

// Toggle the disabled flag. The default agent is guarded client-side (the
// backend also refuses it). patchAgentField updates the reactive record, so the
// button label/icon flip immediately.
async function handleToggleDisabled() {
  const a = agent.value
  if (!a) return
  if (a.id === defaultAgentId.value) {
    toast.show(t('settings.items.agentCannotDisableDefault'), { icon: 'warning', type: 'error', duration: 3000 })
    return
  }
  const next = !a.disabled
  try {
    await patchAgentField(a.id, 'disabled', next)
    toast.show(
      next ? t('settings.items.agentDisabledToast') : t('settings.items.agentEnabledToast'),
      { icon: 'success', type: 'success', duration: 3000 },
    )
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

/** Run the integration self-test for the agent currently on screen. */
async function handleSelfTest() {
  const id = agent.value?.id
  if (!id) return
  await selfTest.run(id)
}

/**
 * Run one REAL turn. Confirms the agent can actually answer, not just that its
 * configuration is self-consistent — but it costs tokens, so it is a separate
 * button the user has to press deliberately.
 */
async function handleSelfTestDeep() {
  const id = agent.value?.id
  if (!id) return
  await selfTest.runDeep(id)
}

/** Short label for a check status (ok / warn / fail). */
function selfTestStatusLabel(status: AgentSelfTestCheck['status']): string {
  switch (status) {
    case 'ok':
      return t('settings.items.agentSelfTestStatusOk')
    case 'warn':
      return t('settings.items.agentSelfTestStatusWarn')
    default:
      return t('settings.items.agentSelfTestStatusFail')
  }
}

async function handleDelete() {
  const a = agent.value
  if (!a) return
  if (a.id === defaultAgentId.value) {
    toast.show(t('settings.items.agentDeleteDefault'), { icon: 'warning', type: 'error', duration: 3000 })
    return
  }
  // Block locally when the agent is still in use. `sessionCount` already
  // includes group chats the agent is a member of, so sessions + tasks is the
  // complete picture. The backend enforces the same rule authoritatively; this
  // avoids a pointless confirm dialog. Counts come from GET /api/agents and may
  // be stale, so the backend 409 below is still handled.
  if ((a.sessionCount ?? 0) > 0 || (a.taskCount ?? 0) > 0 || (a.membershipCount ?? 0) > 0) {
    toast.show(
      t('settings.items.agentDeleteBlocked', {
        sessions: a.sessionCount ?? 0,
        tasks: a.taskCount ?? 0,
      }),
      { icon: 'warning', type: 'error', duration: 4000 },
    )
    return
  }
  const confirmed = await dialog.confirm(
    t('settings.items.agentDeleteConfirm', { name: a.name }),
    { title: t('settings.items.agentDelete'), dangerous: true }
  )
  if (!confirmed) return
  try {
    await deleteAgent(a.id)
    toast.show(t('settings.items.agentDeleted'), { icon: 'success', type: 'success', duration: 3000 })
    emit('deleted')
  } catch (err) {
    // The backend is authoritative: a 409 AgentInUse means the local counts
    // were stale (e.g. a session was created since the last load).
    const msgKey = (err as { msgKey?: string } | null)?.msgKey
    if (msgKey === 'AgentInUse') {
      const detail = (err as { detail?: { SessionCount?: number; TaskCount?: number } } | null)?.detail
      toast.show(
        t('settings.items.agentDeleteBlocked', {
          sessions: detail?.SessionCount ?? (a.sessionCount ?? 0),
          tasks: detail?.TaskCount ?? (a.taskCount ?? 0),
        }),
        { icon: 'warning', type: 'error', duration: 4000 },
      )
      return
    }
    toast.show(t('settings.items.agentDeleteFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}
</script>

<style scoped>
.settings-agent-detail {
  padding: var(--space-4);
  background: var(--bg-secondary);
  min-height: 100%;
}

/* Avatar row inside the Identity card: transparent like a settings row, with
   the same inset divider so it reads as the card's first item. */
.settings-agent-detail__avatar-row {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  padding: var(--space-4) var(--space-7);
  background: transparent;
  position: relative;
}

.settings-agent-detail__avatar-row::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 16px;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

.settings-agent-detail__avatar-btn {
  flex-shrink: 0;
  margin-left: auto;
}

/* Stale-reference fallback: centred block, matching the task panel's missing
   state so a dead link reads as part of the settings surface, not a dead end. */
.settings-agent-detail--missing {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-6);
  height: 100%;
  min-height: 100%;
  color: var(--text-muted);
}

.settings-agent-detail__missing-icon {
  color: var(--text-muted);
  opacity: var(--opacity-muted);
}

.settings-agent-detail__missing-title {
  font-size: var(--font-size-lg);
  color: var(--text-muted);
}

.settings-agent-detail__missing-hint {
  font-size: var(--font-size-sm);
  color: var(--text-hint);
  text-align: center;
  max-width: 32em;
}

.settings-agent-detail__actions {
  display: flex;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}

.settings-agent-detail__action-btn,
.settings-agent-detail__delete-btn {
  flex: 1;
}

.settings-agent-detail__action-icon,
.settings-agent-detail__delete-icon {
  flex-shrink: 0;
}

/* ── Integration self-test ──────────────────────────────────────────────── */

.settings-agent-detail__selftest {
  margin-bottom: var(--space-4);
}

.settings-agent-detail__selftest-actions {
  display: flex;
  gap: var(--space-4);
}

.settings-agent-detail__selftest-btn {
  flex: 1;
}

.settings-agent-detail__selftest-deep {
  margin-top: var(--space-4);
  padding: var(--space-4) var(--space-6);
  background: var(--bg-primary);
  border-radius: var(--radius-sm);
  border-left: 2px solid #22c55e;
}

.settings-agent-detail__selftest-deep-line {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  line-height: var(--line-height-snug);
  overflow-wrap: anywhere;
}

.settings-agent-detail__selftest-deep-meta {
  margin-top: var(--space-2);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}

.settings-agent-detail__selftest-error {
  margin-top: var(--space-4);
  padding: var(--space-4) var(--space-6);
  font-size: var(--font-size-md);
  color: #ef4444;
  background: color-mix(in srgb, #ef4444 10%, var(--bg-primary));
  border-radius: var(--radius-sm);
}

/* Status colors follow the settings panel's connectivity-test results
   (SettingsGroupPanel: #22c55e / #f59e0b / #ef4444) rather than introducing new
   tokens — there is no --color-warning in variables.css, and a var() with a
   fallback silently resolves to nothing when the token is absent. */
.settings-agent-detail__selftest-result {
  margin-top: var(--space-4);
  padding: var(--space-4) var(--space-6);
  background: var(--bg-primary);
  border-radius: var(--radius-sm);
  border-left: 2px solid #22c55e;
}

/* A failing run is the case the user must not miss, so it changes the rail. */
.settings-agent-detail__selftest-result--fail {
  border-left-color: #ef4444;
}

.settings-agent-detail__selftest-summary {
  font-size: var(--font-size-md);
  color: var(--text-secondary);
  margin-bottom: var(--space-3);
}

.settings-agent-detail__selftest-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.settings-agent-detail__selftest-check {
  display: grid;
  grid-template-columns: 3.5em 1fr;
  gap: 0 var(--space-4);
  font-size: var(--font-size-md);
  line-height: var(--line-height-snug);
}

.settings-agent-detail__selftest-status {
  font-weight: 600;
  color: var(--text-muted);
}

.settings-agent-detail__selftest-check--ok .settings-agent-detail__selftest-status {
  color: #22c55e;
}

.settings-agent-detail__selftest-check--warn .settings-agent-detail__selftest-status {
  color: #f59e0b;
}

.settings-agent-detail__selftest-check--fail .settings-agent-detail__selftest-status {
  color: #ef4444;
}

.settings-agent-detail__selftest-key {
  color: var(--text-primary);
}

/* Detail spans the full row: it explains WHY a check failed, and wrapping it
   under the status column made long paths unreadable. */
.settings-agent-detail__selftest-detail {
  grid-column: 2;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

</style>
