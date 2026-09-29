<template>
  <!-- Single root wrapper. Every child here is an overlay that Teleports to
       <body> (BottomSheet, AgentSelectorDrawer, SharedSessionsDrawer), so this
       wrapper is empty in the DOM and `display: contents` keeps it
       layout-transparent. Without it the template is a FRAGMENT, and a fragment
       root cannot receive fallthrough attributes: a host binding `v-show` (which
       compiles to a `style` fallthrough) gets "Extraneous non-props attributes
       (style)" and the attribute is silently dropped, so the drawer would never
       hide. No host does that today (they all use the :open prop), but the trap
       is one attribute away — SessionSidebar hit exactly this when a sibling
       drawer was added beside its root div. -->
  <div class="session-drawer-roots">
    <BottomSheet ref="bottomSheetRef" :open="open" auto panel-class="session-drawer-sheet" :title="t('session.title')" @close="$emit('close')">
      <template #header>
        <SessionListHeader
          :session-count="sessionCount"
          :session-max-count="sessionMaxCount"
          @open-search="$emit('open-session-search')"
          @create="handleCreateClick"
        >
          <template #actions>
            <!-- Only when this project actually has a shared conversation:
                 with nothing to manage the button is pure header clutter.
                 hasAnySharedSession stays false until the list loads, so it
                 does not flash in for the common "nothing shared" case. -->
            <button v-if="hasAnySharedSession" class="header-action-btn" data-action="shared-sessions" :title="t('sharedSessions.button')" @click.stop="sharedSessionsRef?.open()">
              <MessageSquareShare :size="16" />
            </button>
          </template>
          <!-- Pin sits in the trailing slot so it is the right-most control in
               the header, after search/create. -->
          <template #actions-end>
            <button v-if="isWideScreen" class="header-action-btn" data-action="pin" @click.stop="$emit('pin')" :title="t('session.pinToSidebar')">
              <PanelRight :size="16" />
            </button>
          </template>
        </SessionListHeader>
      </template>

      <SessionList
        ref="listRef"
        v-model:active-tab="activeTab"
        :current-session-id="currentSessionId"
        :running-session-ids="runningSessionIds"
        :is-active="open"
        @select="handleSelect"
        @archive="handleArchive"
        @destroy="$emit('destroy', $event)"
      />

      <!-- Tab bar lives in the footer slot, NOT inside SessionList: BottomSheet's
           auto mode sizes the panel to its content, so an in-flow tab bar at the
           bottom of the list would be pushed off-screen by the growing scroll
           area. .bs-footer is flex-shrink:0, so it stays pinned. -->
      <template #footer>
        <SessionListTabs v-model:active-tab="activeTab" />
      </template>
    </BottomSheet>

    <!-- Agent selector drawer -->
    <AgentSelectorDrawer
      ref="agentSelectorRef"
      :open="agentSelectorDrawer.effectiveOpen.value"
      :title="t('session.selectAgent')"
      :default-badge="t('chat.sessionSetting.defaultBadge')"
      :set-default-title="t('session.setAsDefaultAgent')"
      :config-title="t('session.configAgent')"
      @update:open="v => v ? agentSelectorDrawer.open() : agentSelectorDrawer.close()"
      @select="createSession"
    />
    <SharedSessionsDrawer ref="sharedSessionsRef" @select-session="$emit('select', $event)" />
  </div>
</template>

<script setup>
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { PanelRight, MessageSquareShare } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import SessionList from '@/components/session/SessionList.vue'
import SessionListHeader from '@/components/session/SessionListHeader.vue'
import SharedSessionsDrawer from '@/components/session/SharedSessionsDrawer.vue'
import SessionListTabs from '@/components/session/SessionListTabs.vue'
import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'
import { useAgents } from '@/composables/useAgents'
import { useSessionShare } from '@/composables/useSessionShare'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { store } from '@/stores/app.ts'

const { t } = useI18n()
const props = defineProps({
  open: Boolean,
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  currentAgentId: String,
})

const emit = defineEmits(['close', 'select', 'create', 'archive', 'destroy', 'open-session-search', 'pin'])

const { isWideScreen } = useWideScreenLayout()

const bottomSheetRef = ref(null)
const agentSelectorRef = ref(null)
const { hasAnySharedSession } = useSessionShare()
const listRef = ref(null)
const sharedSessionsRef = ref(null)
// Which pane the list shows. Owned here (not in SessionList) because the tab bar
// is rendered in the BottomSheet footer, outside the list's scroll area.
const activeTab = ref('project')
const { loadAgents } = useAgents()
const agentSelectorDrawer = useTabDrawer('chat', { autoRestore: false })

const sessionCount = computed(() => store.state.sessionCount)
const sessionMaxCount = computed(() => store.state.sessionMaxCount)

defineExpose({ openAgentSelector, addSessionLocally })

async function openAgentSelector() {
  await loadAgents()
  // Always open the selector, even with a single agent — a direct create
  // is a one-tap action easily mis-tapped on mobile, creating an empty
  // session. Requiring an explicit selection prevents accidental creation.
  // 始终打开选择器（哪怕只有一个智能体）——直接创建是一键动作，
  // 移动端容易误触生成空会话；强制选择可避免误建。
  agentSelectorDrawer.open()
}

async function handleCreateClick() {
  await loadAgents()
  agentSelectorDrawer.open()
}

function createSession(agentId) {
  agentSelectorDrawer.close()
  emit('create', agentId)
  bottomSheetRef.value?.close()
}

function handleSelect(sessionId, backend, projectPath) {
  emit('select', sessionId, backend, projectPath)
  bottomSheetRef.value?.close()
}

function handleArchive(sessionId, backend) {
  emit('archive', sessionId, backend)
}

function addSessionLocally(session) {
  listRef.value?.addSessionLocally(session)
}

watch(() => props.open, async (val) => {
  if (val) {
    await Promise.all([loadAgents(), listRef.value?.loadSessions()])
  }
})
watch(() => store.state.sessionCount, async () => {
  if (props.open) listRef.value?.loadSessions()
})
</script>

<style scoped>
/* Footer padding is zeroed in the non-scoped block below. A scoped :deep rule
   cannot reach it: BottomSheet renders inside <Teleport to="body">, so this
   component's scope attribute never lands on .bs-footer's ancestors and the
   selector fails to match. panelClass is bound on .bs-panel itself, so the
   non-scoped selector can target it reliably. */

/* The single-root wrapper exists only to keep this component out of FRAGMENT
   territory (see the template comment). All of its children teleport to <body>,
   so it holds nothing — `display: contents` removes it from the layout box tree
   entirely, guaranteeing it cannot introduce a flex/grid item in either host
   (.app-container and .chat-input-wrapper are both flex columns). */
.session-drawer-roots {
  display: contents;
}
</style>

<style>
/* Zero out BottomSheet's default footer padding/border so the tab bar spans the
   full panel width edge-to-edge (the tabs component draws its own top border,
   so the footer's would double up). Scoped to this drawer's panelClass so other
   BottomSheet consumers (e.g. SessionSearchDrawer) keep their own footer.
   .bs-panel is included in the selector to outrank the base
   `.bs-panel > .bs-footer` rule regardless of stylesheet order. */
.bs-panel.session-drawer-sheet > .bs-footer {
  padding: 0;
  border-top: none;
  gap: 0;
  /* The bar is a full-width row of tabs; the base rule's flex-end alignment
     would pack it to the right. */
  justify-content: flex-start;
}
</style>
