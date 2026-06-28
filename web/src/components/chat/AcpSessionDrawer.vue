<template>
  <BottomSheet :open="open" auto :title="drawerTitle" @close="$emit('close')">
    <template #header>
      <RotateCwIcon v-if="acpSessionsLoading" :size="16" class="bs-header-icon spin" />
      <HistoryIcon v-else :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ drawerTitle }}</span>
    </template>
    <div class="acp-session-list">
      <div v-if="acpSessionsLoading && acpSessions.length === 0" class="acp-session-empty">
        {{ t('chat.acpSession.loading') }}
      </div>
      <div v-else-if="acpSessionsNotSupported" class="acp-session-empty">
        {{ t('chat.acpSession.notSupported') }}
      </div>
      <div v-else-if="acpSessions.length === 0" class="acp-session-empty">
        {{ t('chat.acpSession.empty') }}
      </div>
      <template v-else>
        <div
          v-for="session in acpSessions"
          :key="session.sessionId"
          class="acp-session-item"
        >
          <div class="acp-session-item-info">
            <div class="acp-session-item-title">{{ session.title || t('chat.acpSession.untitled') }}</div>
            <div class="acp-session-item-meta">
              <span v-if="session.updatedAt" class="acp-session-item-time">{{ formatTime(session.updatedAt) }}</span>
              <span class="acp-session-item-id">{{ session.sessionId.slice(0, 8) }}</span>
            </div>
          </div>
          <button
            class="acp-session-resume-btn"
            :disabled="acpResuming"
            :title="t('chat.acpSession.title')"
            @click.stop="handleSelect(session)"
          >
            <Loader2Icon v-if="resumingId === session.sessionId" :size="14" class="spin" />
            <RotateCwIcon v-else :size="14" />
          </button>
        </div>
        <button
          v-if="nextCursor && !acpSessionsLoading"
          class="acp-session-more"
          @click="loadMore"
        >
          {{ t('chat.acpSession.loadMore') }}
        </button>
      </template>
    </div>

    <!-- Loading overlay -->
    <Transition name="overlay-fade">
      <div v-if="acpResuming" class="acp-resume-overlay">
        <div class="acp-resume-overlay-content">
          <Loader2Icon :size="24" class="spin" />
          <span>{{ t('chat.acpSession.resuming') }}</span>
        </div>
      </div>
    </Transition>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, computed, toRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { History as HistoryIcon, RotateCw as RotateCwIcon, Loader2 as Loader2Icon } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { useAcpSession, type AcpSessionInfo } from '@/composables/useAcpSession'
import { useAgents } from '@/composables/useAgents'

const props = defineProps<{
  open: boolean
  agentId: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'select', sessionId: string): void
}>()

const { t } = useI18n()
const { getAgentName } = useAgents()
const resumingId = ref('')
const drawerTitle = computed(() => t('chat.acpSession.resumeTitle', { agent: getAgentName(props.agentId) }))

const {
  acpSessions,
  acpSessionsLoading,
  acpResuming,
  acpSessionsNotSupported,
  nextCursor,
  loadAcpSessions,
  acpLoadSession,
} = useAcpSession({ currentAgentId: toRef(props, 'agentId') })

// Load sessions when drawer opens
watch(() => props.open, (val) => {
  if (val && props.agentId) {
    loadAcpSessions(props.agentId)
  }
})

async function handleSelect(session: AcpSessionInfo) {
  if (acpResuming.value) return
  resumingId.value = session.sessionId
  const sessionId = await acpLoadSession(session.sessionId)
  resumingId.value = ''
  if (sessionId && sessionId !== 'not-found') {
    emit('select', sessionId)
    emit('close')
  }
}

function loadMore() {
  loadAcpSessions(props.agentId, true)
}

function formatTime(iso: string): string {
  try {
    const d = new Date(iso)
    const now = new Date()
    const diffMs = now.getTime() - d.getTime()
    const diffMin = Math.floor(diffMs / 60000)
    if (diffMin < 1) return t('chat.acpSession.justNow')
    if (diffMin < 60) return t('chat.acpSession.minutesAgo', { n: diffMin })
    const diffH = Math.floor(diffMin / 60)
    if (diffH < 24) return t('chat.acpSession.hoursAgo', { n: diffH })
    const diffD = Math.floor(diffH / 24)
    if (diffD < 30) return t('chat.acpSession.daysAgo', { n: diffD })
    return d.toLocaleDateString()
  } catch {
    return iso
  }
}
</script>

<style scoped>
.acp-session-list {
  display: flex;
  flex-direction: column;
  gap: 0;
  padding: 0;
  min-height: 0;
  overflow-y: auto;
  flex: 1;
  position: relative;
}

.acp-session-empty {
  min-height: 40vh;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.acp-session-item {
  position: relative;
  display: flex;
  align-items: center;
  min-height: 44px;
  padding: 10px 12px;
  border-top: 1px solid var(--border-color, #dee2e6);
}

.acp-session-item-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.acp-session-item-title {
  font-size: 13px;
  color: var(--text-primary, #1a1a1a);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.acp-session-item-meta {
  display: flex;
  align-items: center;
  gap: 6px;
}

.acp-session-item-time {
  font-size: 11px;
  color: var(--text-muted, #999);
}

.acp-session-item-id {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  font-weight: 500;
  flex-shrink: 0;
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-secondary, #495057);
  font-family: monospace;
}

.acp-session-resume-btn {
  flex-shrink: 0;
  margin-left: 8px;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: rgba(0, 102, 204, 0.08);
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s;
}

.acp-session-resume-btn:hover {
  background: rgba(0, 102, 204, 0.16);
}

.acp-session-resume-btn:active {
  background: rgba(0, 102, 204, 0.24);
}

.acp-session-resume-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.acp-session-more {
  display: block;
  width: 100%;
  padding: 10px;
  margin-top: 4px;
  border: none;
  background: none;
  cursor: pointer;
  font-size: 12px;
  color: var(--text-muted, #999);
  text-align: center;
}

.acp-session-more:hover {
  color: var(--text-secondary, #666);
}

/* Loading overlay */
.acp-resume-overlay {
  position: absolute;
  inset: 0;
  background: rgba(255, 255, 255, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10;
  border-radius: inherit;
}

:root[data-theme="dark"] .acp-resume-overlay {
  background: rgba(0, 0, 0, 0.6);
}

.acp-resume-overlay-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--accent-color, #0066cc);
  font-weight: 500;
}

.overlay-fade-enter-active,
.overlay-fade-leave-active {
  transition: opacity 0.2s ease;
}

.overlay-fade-enter-from,
.overlay-fade-leave-to {
  opacity: 0;
}

.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
