<template>
  <BottomSheet :open="open" auto :title="t('group.settings')" @close="close">
    <template #header>
      <div class="gm-header">
        <span class="bs-header-title">{{ t('group.settings') }}</span>
        <!-- Add members lives in the HEADER, not as a full-width pill at the
             bottom of the body: adding is the sheet's primary action and belongs
             at the top edge where the roster begins, next to the title it
             extends. Plain icon + text (no pill) to match AttachDrawer's header
             action. @click.stop is required — the whole header is a close
             target (BottomSheet's own click handler). -->
        <button class="gm-header-add" data-action="add-members" @click.stop="openAdd">
          <Plus :size="16" />
          <span>{{ t('group.addMembers') }}</span>
        </button>
      </div>
    </template>

    <div class="group-settings-sheet">
      <!-- Member roster -->
      <ul class="gm-list">
        <li
          v-for="m in orderedMembers"
          :key="m.id"
          class="gm-row"
          :class="{ 'is-left': m.left }"
        >
          <!-- The row's main region is a BUTTON (opens the member's context
               panel). The remove control is a SIBLING, never a child: a
               <button> inside a <button> is invalid HTML and the browser
               hoists the inner one out, which makes both targets behave
               unpredictably. Left members render a plain div — there is
               nothing live to inspect and the frozen usage would mislead. -->
          <component
            :is="m.left ? 'div' : 'button'"
            class="gm-row-main"
            :class="{ 'gm-row-main--static': m.left }"
            :type="m.left ? undefined : 'button'"
            :data-action="m.left ? undefined : 'member-context'"
            :title="m.left ? undefined : t('group.memberDetail.open')"
            @click="m.left ? undefined : openMember(m)"
          >
            <span class="gm-avatar">
              <AgentIcon :backend="m.backend" :name="m.name" :avatar="m.avatar || getAgentAvatar(m.agentId)" size="lg" />
            </span>
            <span class="gm-identity">
              <span class="gm-name">{{ m.name }}</span>
              <!-- Real-agent disclosure: a member whose nickname was auto-derived
                   (pool name) shows the underlying agent's real name right after
                   it, the same convention as the message speaker label. -->
              <RealAgentNameTag class="gm-real-name" :agent-name="m.agentName" :display-name="m.name" />
              <!-- Host / left pills sit INSIDE the identity group, right after
                   the name + real name. They describe the member, so they belong
                   next to who it is — pinned to the far right they read like a
                   row-level action and broke the alignment across rows (the
                   usage chip is the only right-hand column). -->
              <span v-if="m.isHost" class="gm-tag gm-tag--host"><Crown :size="11" class="gm-tag-crown" />{{ t('group.host') }}</span>
              <span v-else-if="m.left" class="gm-tag gm-tag--left">{{ t('group.left') }}</span>
            </span>
            <!-- Context usage chip: the member's OWN window, not the group's.
                 A member that never reported usage shows an em dash — 0% would
                 claim a measurement that does not exist. Left members show
                 nothing at all. -->
            <span v-if="!m.left" class="gm-usage" :class="{ 'gm-usage--empty': !usageOf(m) }" :title="usageTitle(m)">
              <Activity :size="11" class="gm-usage-icon" />
              <template v-if="usageOf(m)">
                <span class="gm-usage-bar">
                  <span class="gm-usage-bar-fill" :style="{ width: Math.min(usagePctOf(usageOf(m)), 100) + '%', background: usageColorOf(usagePctOf(usageOf(m))) }"></span>
                </span>
                <span class="gm-usage-pct">{{ usagePctOf(usageOf(m)) }}%</span>
              </template>
              <span v-else class="gm-usage-pct">—</span>
            </span>
          </component>
          <!-- Every live member has a remove control, including the host: a
               disabled one keeps the column aligned across rows (a missing
               button shifted the usage chip left on the host row). The host's
               is greyed and inert — it is the group's only router, so removing
               it would strand the group. -->
          <button
            v-if="!m.left"
            class="gm-remove"
            :class="{ 'gm-remove--disabled': m.isHost }"
            :disabled="m.isHost"
            :title="m.isHost ? t('group.hostNotRemovable') : t('common.remove')"
            :aria-label="m.isHost ? t('group.hostNotRemovable') : t('common.remove')"
            @click="m.isHost ? undefined : remove(m)"
          >
            <Trash2 :size="14" />
          </button>
        </li>
      </ul>

      <!-- Clicking a member opens the SAME settings drawer a single chat uses
           (model / thinking effort / mode / transport / usage), addressed at
           that member's session row. This is what makes the two paths
           consistent: one component, one mental model — a member is a session.
           `target-session-id` is the member row id, which is exactly the
           session id that owns the agent binding and the member's own context
           window. -->
      <SessionDrawer
        :open="memberSheetOpen"
        :agent-id="selectedMember?.agentId || ''"
        :target-session-id="selectedMemberId"
        :initial-tab="memberDrawerTab"
        @close="memberSheetOpen = false"
        @changed="onMemberChanged"
      />

      <!-- Settings: concurrency. Free mode only: in host mode the host routes
           (the user never @-names speakers), so the switch would be a dead
           setting there. When on, the members the user @-names (or the whole
           roster when they name nobody) run concurrently; it never affects an
           agent's own mode="parallel" mentions. -->
      <div v-if="mode === 'free'" class="gm-setting">
        <div class="gm-setting-text">
          <label class="gm-setting-label" for="group-parallel">{{ t('group.parallelLabel') }}</label>
          <span class="gm-setting-desc">{{ t('group.parallelHint') }}</span>
        </div>
        <label class="settings-item__switch">
          <input
            id="group-parallel"
            class="settings-item__switch-input"
            type="checkbox"
            :checked="parallelDefault"
            @change="onToggleParallel"
          />
          <span class="settings-item__switch-track" />
        </label>
      </div>

      <!-- Settings: auto-approve. Group sessions have no per-agent model/mode
           chrome (hidden in ChatInputBar), so the SessionDrawer's switch — the
           only other entry point — is unreachable here. This row is the group's
           entry. The backend fans the flag out to EVERY member row (decision
           #61): each member owns its own ACP connection and reads its own row,
           so a single switch covers the whole roster. -->
      <div class="gm-setting">
        <div class="gm-setting-text">
          <label class="gm-setting-label" for="group-auto-approve">{{ t('chat.autoApprove.title') }}</label>
          <span class="gm-setting-desc">{{ t('group.autoApproveHint') }}</span>
        </div>
        <label class="settings-item__switch">
          <input
            id="group-auto-approve"
            class="settings-item__switch-input"
            type="checkbox"
            :checked="autoApprove"
            @change="onToggleAutoApprove"
          />
          <span class="settings-item__switch-track" />
        </label>
      </div>

      <!-- Settings: auto-cruise. Available in BOTH modes: it changes only WHEN
           the turn ends (host mode: the host must confirm; free mode: at least
           half the members must confirm — >= ceil(n/2)), not who routes. The
           hint differs by mode so the two rule sets are legible. Placed AFTER
           auto-approve so the auto-approve row keeps its position in the switch
           order. -->
      <div class="gm-setting">
        <div class="gm-setting-text">
          <label class="gm-setting-label" for="group-cruise">{{ t('group.cruiseLabel') }}</label>
          <span class="gm-setting-desc">{{ mode === 'free' ? t('group.cruiseHintFree') : t('group.cruiseHintHost') }}</span>
        </div>
        <label class="settings-item__switch">
          <input
            id="group-cruise"
            class="settings-item__switch-input"
            type="checkbox"
            :checked="cruise"
            @change="onToggleCruise"
          />
          <span class="settings-item__switch-track" />
        </label>
      </div>
    </div>

    <AgentSelectorDrawer
      :open="pickerOpen"
      multiple
      allow-instances
      :max-instances="store.state.groupMaxMembers"
      :used-instances="activeMemberCount"
      :showAgentActions="false"
      :modelValue="[]"
      :title="t('group.addMembers')"
      :confirm-label="t('group.confirm')"
      @update:open="v => (pickerOpen = v)"
      @select="onAgentsPicked"
    />
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Trash2, Plus, Crown, Activity } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import RealAgentNameTag from '@/components/common/RealAgentNameTag.vue'
import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'
import SessionDrawer from '@/components/chat/SessionDrawer.vue'
import { getAgentAvatar } from '@/composables/useAgents'
import { addGroupMembers, generateMemberAvatars, removeGroupMember, setGroupParallelDefault, setGroupCruise, type GroupMemberInfo } from '@/composables/useGroupChat'
import { toggleAutoApprove, applyCruiseAutoApproveDefault } from '@/composables/useSessionIdentity'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { usagePct as usagePctOf, usageColor as usageColorOf } from '@/utils/usageDisplay'
import { appLog } from '@/utils/appLog'
import { store } from '@/stores/app.ts'

const props = defineProps<{
  groupId: string
  members: GroupMemberInfo[]
  /** The group's current auto-approve flag (server-authoritative: the backend
   *  mirrors it across every member row, decision #61). */
  autoApprove: boolean
  /** The group's mode. "free" shows the concurrency switch; host mode hides it
   *  (the host routes, so the user never @-names a speaker). */
  mode?: 'host' | 'free'
  /** Free-mode concurrency switch value (server-authoritative). Shown only in
   *  free mode — in host mode the host routes, so it would be a dead setting. */
  parallelDefault?: boolean
  /** Auto-cruise switch value (server-authoritative). Shown in BOTH modes. */
  cruise?: boolean
  /** True while the group has a turn in flight. Compaction is refused then
   *  (every member connection is busy), so the panel disables the action. */
  groupRunning?: boolean
}>()
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const open = ref(false)
const pickerOpen = ref(false)

// The host always leads the roster. The server orders members by created_at, so
// a host that joined later (or was removed and re-added) would otherwise appear
// mid-list, which reads as "not in charge". Everyone else keeps their incoming
// order, and the removed members stay wherever they were.
const orderedMembers = computed(() => {
  const host = props.members.find(m => m.isHost)
  if (!host) return props.members
  return [host, ...props.members.filter(m => m !== host)]
})

function openSheet() { open.value = true }
function close() {
  // Closing the roster also drops the nested member panel: leaving it open
  // would strand a sheet whose owner is gone (it would reappear over the next
  // sheet the user opens).
  memberSheetOpen.value = false
  open.value = false
}
function openAdd() { pickerOpen.value = true }

// ── Member context panel ──
// Nested inside this sheet (not a replacement): the roster stays behind it, so
// comparing members is "close, tap the next row". The selected member is looked
// up by id against the CURRENT props, so a roster refresh (usage arrived, a
// member left) is reflected without reopening the panel.
const memberSheetOpen = ref(false)
const selectedMemberId = ref('')
// Always opens on the usage tab: the roster chip the user just tapped IS a
// usage number, so that is what they came to see. Model/mode are one tap away
// in the same drawer.
const memberDrawerTab = ref('usage')
const selectedMember = computed(() => props.members.find(m => m.id === selectedMemberId.value) || null)

function openMember(m: GroupMemberInfo) {
  selectedMemberId.value = m.id
  memberDrawerTab.value = 'usage'
  memberSheetOpen.value = true
}

// A write from the member drawer (settings change or compaction) may alter what
// the roster chip shows, so refresh it.
function onMemberChanged() {
  emit('changed')
}

// ── Live refresh while the panel is open ──
// A member turn emits usage_update / stream_finalize on the GROUP timeline (its
// session_id is the group), but nothing pushes a roster update — the numbers
// would freeze the moment the sheet opens. Re-fetch on those events, debounced:
// a single member turn emits several usage_updates per 500ms flush window, and
// an un-debounced handler would turn one reply into a burst of roster queries.
const REFRESH_DEBOUNCE_MS = 1500
let refreshTimer: ReturnType<typeof setTimeout> | null = null
let stopListening: (() => void) | null = null

function scheduleRefresh() {
  if (refreshTimer) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    refreshTimer = null
    emit('changed')
  }, REFRESH_DEBOUNCE_MS)
}

function startListening() {
  if (stopListening) return
  const { onEvent } = useGlobalEvents()
  stopListening = onEvent((event, data) => {
    if (event !== 'chat_stream') return
    const d = data as { session_id?: string; event_type?: string } | undefined
    if (!d || d.session_id !== props.groupId) return
    if (d.event_type === 'usage_update' || d.event_type === 'stream_finalize') scheduleRefresh()
  })
}

function stopListeningNow() {
  if (refreshTimer) {
    clearTimeout(refreshTimer)
    refreshTimer = null
  }
  stopListening?.()
  stopListening = null
}

// The listener is bound to the MEMBER DRAWER, not the sheet: the roster chip and
// the drawer's usage tab only need fresh numbers while someone is looking at
// them, and a group turn emits these events constantly.
watch(memberSheetOpen, (open) => {
  if (open) {
    emit('changed') // fetch once on open, so the panel never shows a stale value
    startListening()
  } else {
    stopListeningNow()
  }
})

onBeforeUnmount(stopListeningNow)

// Instances ALREADY in the group (active only; left members do not occupy a
// slot). The picker counts these against the group-wide cap so "+" disables at
// the right point, and the footer total includes them.
const activeMemberCount = computed(() => props.members.filter(m => !m.left).length)

async function onAgentsPicked(ids: string | string[]) {
  const list = Array.isArray(ids) ? ids : [ids]
  if (list.length > 0) {
    try {
      const newIds = await addGroupMembers(props.groupId, list)
      // Show the new members immediately, then backfill their avatars in the
      // background and refresh AGAIN when done. The roster is fetched before
      // generation completes, so without the second refresh the fresh members
      // would keep their fallback icons until a session switch reloaded them.
      emit('changed')
      void generateMemberAvatars(props.groupId, newIds)
        .then(() => emit('changed'))
        .catch(e => appLog.w('Group', 'member avatar generation failed', e))
    } catch { /* ignore */ }
  }
  pickerOpen.value = false
}

async function remove(m: GroupMemberInfo) {
  try {
    await removeGroupMember(props.groupId, m.id)
    emit('changed')
  } catch { /* ignore */ }
}

/** A member's reported usage, or null when it never reported any. */
function usageOf(m: GroupMemberInfo) {
  const u = m.usage
  return u && (u.size ?? 0) > 0 ? u : null
}

// Tooltip for the usage chip: the exact numbers (the row itself only has space
// for a bar and a percentage), or the reason there is nothing to show.
function usageTitle(m: GroupMemberInfo) {
  const u = usageOf(m)
  if (!u) return t('group.memberDetail.noUsageShort')
  return t('chat.sessionInfo.contextUsageTooltip', {
    used: (u.used ?? 0).toLocaleString(),
    size: u.size.toLocaleString(),
    pct: usagePctOf(u),
  })
}

// Delegate to the shared toggle: it PATCHes the current session (the group) and
// the backend fans the flag out to every member row. We deliberately do NOT
// keep local state — the parent passes the server value back down, so the
// checkbox reflects the persisted truth (and reverts if the request fails).
function onToggleAutoApprove(e: Event) {
  toggleAutoApprove((e.target as HTMLInputElement).checked)
}

// Persist the free-mode concurrency switch, then refresh the roster so the
// switch reflects the SERVER value (server-authoritative, same contract as
// auto-approve: a failed PATCH reverts the checkbox on the next read-back).
async function onToggleParallel(e: Event) {
  const enabled = (e.target as HTMLInputElement).checked
  try {
    await setGroupParallelDefault(props.groupId, enabled)
    emit('changed')
  } catch { /* the parent's refresh reverts the checkbox to the server value */ }
}

// Persist the auto-cruise switch, then refresh so the checkbox reflects the
// SERVER value (same server-authoritative contract as the other switches).
async function onToggleCruise(e: Event) {
  const enabled = (e.target as HTMLInputElement).checked
  try {
    await setGroupCruise(props.groupId, enabled)
    // Hard default: enabling cruise forces auto-approve on (the backend does the
    // same for the group's member rows); mirror it into the checkbox at once.
    applyCruiseAutoApproveDefault(enabled)
    emit('changed')
  } catch { /* the parent's refresh reverts the checkbox to the server value */ }
}

defineExpose({ open: openSheet })
</script>

<style scoped>
.group-settings-sheet {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  padding: var(--space-3) var(--space-7) var(--space-7);
}

/* ── Member roster ── */
.gm-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}
.gm-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-height: 40px;
  padding: var(--space-2) 0;
}
.gm-row + .gm-row {
  border-top: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);
}
/* The row's main region: a button so the whole identity+usage area is one tap
   target that opens the member's context panel. Reset the UA button styles —
   most importantly `text-align: center`, which is the default for <button> and
   would centre the name and usage chip (the flex children inherit it). */
.gm-row-main {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-1) var(--space-2);
  margin: 0 calc(-1 * var(--space-2));
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background var(--duration-base);
}
@media (hover: hover) {
  .gm-row-main:not(.gm-row-main--static):hover {
    background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  }
}
.gm-row-main:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 1px;
}
/* Left members render a <div>: nothing to inspect, no hover/press affordance. */
.gm-row-main--static {
  cursor: default;
}
.gm-row.is-left .gm-avatar,
.gm-row.is-left .gm-name,
.gm-row.is-left :deep(.gm-real-name) {
  opacity: var(--opacity-disabled, 0.4);
}
.gm-avatar {
  flex-shrink: 0;
  width: var(--icon-size-lg, 24px);
  height: var(--icon-size-lg, 24px);
  border-radius: var(--radius-full);
  overflow: hidden;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
/* Nickname + real-agent label sit together; the wrapper takes the flex slack
   so the host tag / remove button stay pinned to the trailing edge. */
.gm-identity {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  /* The host/left pill rides with the name, so the identity group must be
     allowed to shrink the NAME (ellipsis) before the pill overflows. */
  overflow: hidden;
}
.gm-name {
  min-width: 0;
  font-size: var(--font-size-md);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Context usage chip ──
   The member's OWN window. Same 28×6 bar geometry as the single-chat session
   info bar so the two read as one measurement; the percentage is the value the
   eye lands on, the bar is the magnitude. Fixed hues (see usageDisplay.ts) —
   the accent is orange in ~3/36 themes and would collide with the warning step. */
.gm-usage {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.gm-usage-icon {
  flex-shrink: 0;
  opacity: var(--opacity-muted, 0.6);
}

.gm-usage-bar {
  position: relative;
  width: 28px;
  height: 6px;
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--text-primary) 18%, transparent);
  overflow: hidden;
  flex-shrink: 0;
}

.gm-usage-bar-fill {
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  border-radius: var(--radius-xs);
  transition: width 0.3s ease, background 0.3s ease;
}

.gm-usage-pct {
  font-variant-numeric: tabular-nums;
  min-width: 26px;
  text-align: right;
}

/* No data: the chip is just an em dash — never a 0% bar, which would claim a
   measurement that does not exist. */
.gm-usage--empty .gm-usage-pct {
  text-align: center;
}

/* Pills: host (accent) / left (muted). */
.gm-tag {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: 1px var(--space-3);
  border-radius: var(--radius-full);
  font-size: var(--font-size-xs);
  line-height: 16px;
}
.gm-tag-crown {
  flex-shrink: 0;
}
.gm-tag--host {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
  color: var(--accent-color, #0066cc);
}
.gm-tag--left {
  background: var(--bg-tertiary);
  color: var(--text-muted);
}

/* Remove: icon button, muted at rest, red on hover. */
.gm-remove {
  flex-shrink: 0;
  width: 26px;
  height: 26px;
  padding: 0;
  border: none;
  background: none;
  color: var(--text-muted);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-sm);
  transition: background var(--duration-base), color var(--duration-base);
}
@media (hover: hover) {
  .gm-remove:hover {
    color: var(--color-red, #dc2626);
    background: color-mix(in srgb, var(--color-red, #dc2626) 12%, transparent);
  }
}
/* Host's remove control: present but inert, so every live row has the same
   trailing column (the usage chip keeps its x-position across rows). */
.gm-remove--disabled,
.gm-remove--disabled:hover {
  color: var(--text-muted);
  background: none;
  opacity: var(--opacity-disabled);
  cursor: not-allowed;
}

.gm-remove:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 1px;
}

/* ── Settings row ── */
.gm-setting {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
}
.gm-setting-label {
  font-size: var(--font-size-md);
  color: var(--text-primary);
}
/* Two-line variant: a title + a muted hint under it (the auto-approve row).
   The switch (a shared .settings-item__switch) sits at the right edge. */
.gm-setting-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.gm-setting-desc {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  line-height: 1.3;
}
/* Layout only — the switch's shape stays in the shared global rule. Without
   this the switch would shrink when the hint text is long. */
.gm-setting .settings-item__switch {
  flex-shrink: 0;
}

/* ── Header: add-members action ──
   Mirrors AttachDrawer's header (`.ad-header` / `.ad-upload-btn`): a full-width
   flex row, the title on the left and a plain icon + text button pushed to the
   right by margin-left:auto. No pill/background — the accent-coloured text is
   the whole affordance, same as the attachment drawer's upload action. */
.gm-header {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
}

.gm-header-add {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0 var(--space-2);
  height: 28px;
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  font-size: var(--font-size-sm);
  white-space: nowrap;
  transition: opacity var(--duration-fast);
}

.gm-header-add:active {
  opacity: var(--opacity-muted);
}
</style>
