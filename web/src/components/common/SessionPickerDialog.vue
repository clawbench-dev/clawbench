<template>
  <BottomSheet :open="open" auto :title="t('quoteBar.chooseSession')" @close="handleClose">
    <template #header>
      <MessageSquare :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('quoteBar.chooseSession') }}</span>
    </template>

    <div class="session-picker-list">
      <LoadingIndicator v-if="loading" size="md" :label="t('common.loading')" />

      <template v-else>
        <div
          v-for="(session, idx) in orderedSessions"
          :key="session.id"
          class="sp-row"
          :class="{
            'sp-row-running': isRunning(session.id),
            'sp-row-current': session.id === currentSessionId,
            'sp-row-active': listNav.activeIndex.value === idx,
          }"
          :title="isRunning(session.id) ? t('session.executing') : undefined"
          role="button"
          tabindex="0"
          @click="handleSelect(session.id)"
          @keydown.enter="handleSelect(session.id)"
          @keydown.space.prevent="handleSelect(session.id)"
        >
          <AgentIcon
            :backend="getAgentBackend(session.agentId || '')"
            :name="getAgentName(session.agentId || '')"
            :size="16"
          />
          <span class="sp-title">{{ session.title || t('session.unnamed') }}</span>
          <!-- The current session gets an explicit label, not just the tint/rail:
               "which one am I already in?" was the one question the row could not
               answer at a glance. It sits next to the title so it is read with it,
               before the trailing spinner/arrow. -->
          <span v-if="session.id === currentSessionId" class="sp-current-chip">
            {{ t('quoteBar.current') }}
          </span>
          <!-- Running cue: a trailing spinner, no text. The row's title carries
               the meaning for assistive tech, and the spinner itself is
               decorative (one live region per row would be noise). -->
          <span v-if="isRunning(session.id)" class="sp-run-spinner" aria-hidden="true">
            <LoadingIndicator inline size="sm" />
          </span>
          <!-- The row click adds WITHOUT leaving this screen. This button is the
               other half of the pair: same delivery, then open that session.
               Two explicit actions beat one action plus a mode to remember. -->
          <button
            class="sp-goto"
            type="button"
            :title="t('quoteBar.addAndOpen')"
            :aria-label="t('quoteBar.addAndOpen')"
            @click.stop="handleSelectAndOpen(session.id)"
          >
            <ArrowRight :size="14" />
          </button>
        </div>

        <div v-if="orderedSessions.length === 0" class="sp-empty">{{ t('session.noSessions') }}</div>

        <!-- Create a session with the DEFAULT agent and send the payload there.
             Deliberately no agent choice: the user is picking a destination for
             an already-composed message, not configuring a conversation.
             Same pair as the session rows: the row adds without leaving, the
             trailing button adds and opens the new session. -->
        <div
          class="sp-row sp-row-create"
          role="button"
          tabindex="0"
          @click="handleCreate"
          @keydown.enter="handleCreate"
          @keydown.space.prevent="handleCreate"
        >
          <Plus :size="15" />
          <span class="sp-title">{{ t('quoteBar.newSession') }}</span>
          <button
            class="sp-goto"
            type="button"
            :title="t('quoteBar.createAndOpen')"
            :aria-label="t('quoteBar.createAndOpen')"
            @click.stop="handleCreateAndOpen"
          >
            <ArrowRight :size="14" />
          </button>
        </div>
      </template>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageSquare, Plus, ArrowRight } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { useAgents } from '@/composables/useAgents'
import { useSessionIdentity, runningSessions } from '@/composables/useSessionIdentity.ts'
import { coalescedJson } from '@/utils/inflightGet.ts'
import { appLog } from '@/utils/appLog'

/**
 * Compact destination picker for "quote to a conversation" / "attach to a
 * conversation" when the chat panel is NOT visible.
 *
 * Two explicit actions per row, so the user never has to remember a mode:
 *  - clicking the row ADDS to that session and keeps the current screen;
 *  - the trailing button ADDS and then OPENS that session.
 * The create row follows the same pair ("add and send" vs "create and open").
 *
 * The row language is the app's simplest: one line, agent icon, ellipsised
 * title, and a trailing spinner while a session is running. The session
 * currently open is pinned to the top and tinted, so the common "just put it in
 * the conversation I already have" case is the first row and needs no scanning.
 */

interface PickerSession {
  id: string
  title?: string
  agentId?: string
}

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  /** Add to this session and stay where the user is. */
  (e: 'select', sessionId: string): void
  /** Add to this session, then open it. */
  (e: 'select-and-open', sessionId: string): void
  (e: 'create'): void
  /** Create a session, then open it. */
  (e: 'create-and-open'): void
  (e: 'close'): void
}>()

const { t } = useI18n()
const { getAgentBackend, getAgentName } = useAgents()
const identity = useSessionIdentity()
const currentSessionId = identity.currentSessionId
const runningSessionsVersion = identity.runningSessionsVersion

const sessions = ref<PickerSession[]>([])
const loading = ref(false)

/**
 * The current session first (so it is reachable without scanning), then the
 * server's own order. The server order is meaningful — pinned/sortOrder/tags —
 * so it is preserved verbatim rather than re-sorted by time.
 */
const orderedSessions = computed(() => {
  const current = currentSessionId.value
  if (!current) return sessions.value
  const idx = sessions.value.findIndex(s => s.id === current)
  if (idx <= 0) return sessions.value
  const rest = sessions.value.slice()
  const [hit] = rest.splice(idx, 1)
  return [hit, ...rest]
})

/**
 * Running state comes from the shared live Set, not the response snapshot: the
 * snapshot is taken once on open, while a session can start or finish while the
 * picker is up.
 */
function isRunning(id: string): boolean {
  void runningSessionsVersion.value
  return runningSessions.value.has(id)
}

async function loadSessions() {
  loading.value = true
  try {
    // Coalesced with the sidebar/drawer: same URL, so an open picker does not
    // issue a duplicate request for identical data.
    const data = await coalescedJson<{ sessions?: PickerSession[] }>('/api/ai/sessions')
    sessions.value = data.sessions || []
  } catch (err) {
    appLog.e('SessionPicker', 'Failed to load sessions:', err)
    sessions.value = []
  } finally {
    loading.value = false
  }
}

// Reset the highlight whenever the list is (re)loaded.
watch(sessions, () => listNav.reset())

watch(() => props.open, (val) => {
  if (val) void loadSessions()
}, { immediate: true })

const listNav = useListNav({
  // The "create session" row is a list item too, so keyboard nav reaches it.
  getCount: () => orderedSessions.value.length + (loading.value ? 0 : 1),
  onConfirm: (idx) => {
    if (idx < orderedSessions.value.length) handleSelect(orderedSessions.value[idx].id)
    else handleCreate()
  },
  onActiveChange: scrollActiveIntoView,
})
useListKeys({ isOpen: () => props.open, nav: listNav })

function scrollActiveIntoView(index: number) {
  const items = document.querySelectorAll('.session-picker-list .sp-row')
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
  }
}

function handleSelect(sessionId: string) {
  emit('select', sessionId)
  handleClose()
}

function handleSelectAndOpen(sessionId: string) {
  emit('select-and-open', sessionId)
  handleClose()
}

function handleCreate() {
  emit('create')
  handleClose()
}

function handleCreateAndOpen() {
  emit('create-and-open')
  handleClose()
}

function handleClose() {
  emit('close')
}
</script>

<style scoped>
.session-picker-list {
  display: flex;
  flex-direction: column;
  /* No vertical padding: the rows run edge to edge between the header and the
     sheet bottom. The row height carries the breathing room instead, and the
     first row's separator is already suppressed (see below) so the flush top
     does not read as a stray edge. */
  padding: 0;
  overflow-y: auto;
}

/* Single line per row. `min-height` rather than a fixed `height` so a taller
   trailing spinner (or a longer fallback title) grows the row instead of being
   clipped.
   The separator is a TOP border (the session list's convention) so it spans the
   full row width including the trailing gutter. */
.sp-row {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-4);
  /* 40px, not the previous 32px: 32 was tighter than every other tappable list
     row in the app and left no room for the current-session chip without the
     title and the chip colliding. Still under the 44px session-list row — this
     picker is a short, one-shot list, not the main navigation. */
  min-height: 40px;
  padding: 0 var(--space-6);
  border: none;
  border-top: 1px solid var(--border-color, #dee2e6);
  background: none;
  cursor: pointer;
  text-align: left;
  color: var(--text-primary);
  transition: background var(--duration-base);
}

/* No rule above the first row: the list's own top padding already separates it
   from the header, so a line there just reads as a stray extra edge. */
.session-picker-list > .sp-row:first-child {
  border-top: none;
}

.sp-title {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (hover: hover) {
  .sp-row:hover {
    background: color-mix(in srgb, var(--text-primary) 7%, transparent);
  }
}

.sp-row:active {
  background: var(--bg-active);
}

/* Keyboard-highlighted row. Distinct from :hover so a mouse hover and the
   arrow-key cursor never look like the same state. */
.sp-row-active {
  background: color-mix(in srgb, var(--text-primary) 7%, transparent);
}

/* The already-open session. Three cues, because the row has to answer "which
   one am I already in?" while also being a button that does something else:
   1. the label chip (`.sp-current-chip`, below) — the unambiguous part;
   2. a 3px accent rail (the theme picker's selected language) — the rail is
      load-bearing, the tint alone is ~1.09:1 against the panel in light themes
      and reads as nothing;
   3. the accent tint.
   The rail spans the FULL row height, matching .theme-item.active::before. A
   shorter centred dash was tried and read as a stray floating mark rather than
   a selection edge. */
.sp-row-current {
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
}

.sp-row-current::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: var(--accent-color);
}

/* "Current" chip on the open session's row. The tint + rail say "special", the
   chip says WHICH kind of special — the row is both the session you are in and
   a destination you can pick, and only the word disambiguates the two. Sized
   from the fork-generation chip so the two read as the same family. */
.sp-current-chip {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  line-height: 16px;
  padding: 0 var(--space-2);
  border-radius: var(--radius-xs);
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
  white-space: nowrap;
}

/* Running cue: the shared LoadingIndicator spinner, parked on the trailing
   edge. It carries the state on its own — no dot on the left, no "执行中"
   label. The left side is reserved for the agent icon so the eye scans one
   column of identities.
   No `color` here: the spinner paints itself from `--li-color` (defaulting to
   the accent), so a color on this wrapper would be inert. */
.sp-run-spinner {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
}

/* "Add and open this session". Hidden until the row is hovered or the button
   itself is focused, so the resting list stays as quiet as possible while the
   affordance is still reachable by keyboard and on touch (where a hover never
   happens, so :focus-within/the tap keeps it usable). */
.sp-goto {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--text-muted);
  cursor: pointer;
  opacity: 0;
  transition: opacity var(--duration-base), background var(--duration-base), color var(--duration-base);
}

.sp-goto svg {
  flex-shrink: 0;
}

@media (hover: hover) {
  .sp-row:hover .sp-goto {
    opacity: 1;
  }

  .sp-goto:hover {
    background: var(--bg-hover);
    color: var(--accent-color);
  }
}

.sp-goto:focus-visible {
  opacity: 1;
  outline: 2px solid var(--focus-ring);
  outline-offset: -2px;
}

/* Touch devices have no hover, so the button would be invisible AND
   untappable-looking. Keep it always visible there. */
@media (hover: none) {
  .sp-goto {
    opacity: var(--opacity-soft);
  }
}

.sp-row-create {
  color: var(--text-secondary);
  gap: var(--space-4);
}

.sp-row-create svg {
  flex-shrink: 0;
  color: var(--text-muted);
}

/* The create row's trailing button is always visible: unlike a session row
   (where the arrow is an optional shortcut past the row's own action), "create
   and open" is the ONLY way to reach the new session, so hiding it behind a
   hover would make it undiscoverable. */
.sp-row-create .sp-goto {
  opacity: 1;
}

.sp-row-create .sp-goto svg {
  color: inherit;
}

.sp-empty {
  padding: var(--space-6);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

@media (prefers-reduced-motion: reduce) {
  .sp-row {
    transition: none;
  }
}
</style>
