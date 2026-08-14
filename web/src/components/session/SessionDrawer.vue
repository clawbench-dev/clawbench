<template>
  <BottomSheet ref="bottomSheetRef" :open="open" auto :title="t('session.title')" @close="$emit('close')">
    <template #header>
      <SessionListHeader
        :session-count="sessionCount"
        :session-max-count="sessionMaxCount"
        @open-search="$emit('open-session-search')"
        @create="handleCreateClick"
      >
        <template #actions>
          <button v-if="isWideScreen" class="header-action-btn" data-action="pin" @click.stop="$emit('pin')" :title="t('session.pinToSidebar')">
            <Pin :size="16" />
          </button>
        </template>
      </SessionListHeader>
    </template>

    <SessionList
      ref="listRef"
      :current-session-id="currentSessionId"
      :running-session-ids="runningSessionIds"
      :is-active="open"
      @select="handleSelect"
      @archive="handleArchive"
      @destroy="$emit('destroy', $event)"
    />
  </BottomSheet>

  <!-- Agent selector drawer -->
  <AgentSelectorDrawer
    ref="agentSelectorRef"
    :open="agentSelectorDrawer.effectiveOpen.value"
    :title="t('session.selectAgent')"
    :default-badge="t('chat.sessionSetting.defaultBadge')"
    :set-default-title="t('session.setAsDefaultAgent')"
    @update:open="v => v ? agentSelectorDrawer.open() : agentSelectorDrawer.close()"
    @select="createSession"
  />
</template>

<script setup>
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Pin } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import SessionList from '@/components/session/SessionList.vue'
import SessionListHeader from '@/components/session/SessionListHeader.vue'
import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'
import { useAgents } from '@/composables/useAgents'
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
const listRef = ref(null)
const { agents, loadAgents } = useAgents()
const agentSelectorDrawer = useTabDrawer('chat', { autoRestore: false })

const sessionCount = computed(() => store.state.sessionCount)
const sessionMaxCount = computed(() => store.state.sessionMaxCount)

defineExpose({ openAgentSelector, addSessionLocally })

async function openAgentSelector() {
  await loadAgents()
  if (agents.value.length === 1) {
    emit('create', agents.value[0].id)
    bottomSheetRef.value?.close()
    return
  }
  agentSelectorDrawer.open()
}

async function handleCreateClick() {
  await loadAgents()
  if (agents.value.length === 1) {
    emit('create', agents.value[0].id)
    bottomSheetRef.value?.close()
    return
  }
  agentSelectorDrawer.open()
}

function createSession(agentId) {
  agentSelectorDrawer.close()
  emit('create', agentId)
  bottomSheetRef.value?.close()
}

function handleSelect(sessionId, backend) {
  emit('select', sessionId, backend)
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
