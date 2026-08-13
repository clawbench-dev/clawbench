<template>
  <BottomSheet :open="open" auto :title="drawerTitle" @close="$emit('close')">
    <template #header>
      <AgentIcon :backend="backendId" :name="backendDisplayName" :size="18" class="bs-header-icon" />
      <span class="bs-header-title">{{ drawerTitle }}</span>
    </template>
    <div class="acp-session-search-row">
      <SearchInput v-model="searchQuery" :placeholder="t('chat.acpSession.searchPlaceholder')" />
    </div>
    <div class="acp-session-list">
      <div v-if="acpSessionsLoading && acpSessions.length === 0" class="acp-session-empty">
        <Loader2Icon :size="18" class="spin acp-session-loading-icon" />
        <span>{{ t('chat.acpSession.loading') }}</span>
      </div>
      <div v-else-if="acpSessionsNotSupported" class="acp-session-empty">
        {{ t('chat.acpSession.notSupported') }}
      </div>
      <div v-else-if="acpSessions.length === 0" class="acp-session-empty">
        {{ t('chat.acpSession.empty') }}
      </div>
      <div v-else-if="filteredSessions.length === 0" class="acp-session-empty">
        {{ t('chat.acpSession.noResults') }}
      </div>
      <template v-else>
        <div
          v-for="session in filteredSessions"
          :key="session.sessionId"
          class="acp-session-item"
        >
            <div class="acp-session-item-info">
              <span class="acp-session-item-title">{{ session.title || t('chat.acpSession.untitled') }}</span>
              <div class="acp-session-item-meta">
                <span v-if="session.updatedAt" class="acp-session-item-time">{{ formatTime(session.updatedAt) }}</span>
                <span class="acp-session-item-id" :title="session.sessionId">{{ session.sessionId }}</span>
              </div>
            </div>
          <button
            class="acp-session-resume-btn"
            :disabled="acpResuming"
            :title="t('chat.acpSession.title')"
            @click.stop="handleSelect(session)"
          >
            <Loader2Icon v-if="resumingId === session.sessionId" :size="14" class="spin" />
            <ImportIcon v-else :size="14" />
          </button>
        </div>
        <div v-if="acpSessionsLoading && acpSessions.length > 0" class="acp-session-loading-more">
          <Loader2Icon :size="14" class="spin" />
          <span>{{ t('chat.acpSession.loading') }}</span>
        </div>
        <div v-if="acpSessions.length > 0 && hiddenOtherProjectCount > 0" class="acp-session-hidden-hint">
          {{ t('chat.acpSession.hiddenInOtherProjects', { count: hiddenOtherProjectCount }) }}
        </div>
        <div
          ref="sentinelRef"
          class="acp-session-sentinel"
        />
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
import { ref, watch, computed, toRef, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Import as ImportIcon, Loader2 as Loader2Icon } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import { useAcpSession, type AcpSessionInfo } from '@/composables/useAcpSession'
import { useAgents } from '@/composables/useAgents'
import { getBackendDisplayName } from '@/utils/backendNames'
import { store } from '@/stores/app.ts'

const props = defineProps<{
  open: boolean
  agentId: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'select', sessionId: string): void
}>()

const { t } = useI18n()
const { getAgentBackend } = useAgents()
const resumingId = ref('')
const searchQuery = ref('')

const backendId = computed(() => getAgentBackend(props.agentId))
const backendDisplayName = computed(() => getBackendDisplayName(backendId.value))
const drawerTitle = computed(() => t('chat.acpSession.resumeTitle', { agent: backendDisplayName.value }))

function trimTrailingSlash(p: string): string {
  return p.replace(/\/+$/, '')
}

const sessionsInCurrentProject = computed(() => {
  const root = trimTrailingSlash(store.state.projectRoot || '')
  if (!root) return []
  return acpSessions.value.filter((s) => trimTrailingSlash(s.cwd || '') === root)
})

const hiddenOtherProjectCount = computed(
  () => acpSessions.value.length - sessionsInCurrentProject.value.length
)

const filteredSessions = computed(() => {
  const base = sessionsInCurrentProject.value
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return base
  return base.filter((s) => s.title.toLowerCase().includes(q))
})

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
    searchQuery.value = ''
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
  if (!nextCursor.value || acpSessionsLoading.value) return
  loadAcpSessions(props.agentId, true)
}

// Infinite scroll via IntersectionObserver on sentinel element
const sentinelRef = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | null = null

function setupObserver() {
  teardownObserver()
  if (!sentinelRef.value) return
  observer = new IntersectionObserver(([entry]) => {
    if (entry.isIntersecting && nextCursor.value && !acpSessionsLoading.value) {
      loadMore()
    }
  }, { root: sentinelRef.value.parentElement, threshold: 0 })
  observer.observe(sentinelRef.value)
}

function teardownObserver() {
  if (observer) {
    observer.disconnect()
    observer = null
  }
}

watch(sentinelRef, (el) => {
  if (el) setupObserver()
  else teardownObserver()
})

onBeforeUnmount(teardownObserver)

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
.acp-session-search-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.acp-session-search-row :deep(.search-pill) {
  flex: 1;
}

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
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.acp-session-item {
  position: relative;
  display: flex;
  align-items: flex-start;
  padding: 12px 14px;
  border-top: 1px solid var(--border-color, #dee2e6);
}

.acp-session-item-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  flex: 1;
  padding-top: 2px;
}

.acp-session-item-title {
  font-size: 13px;
  color: var(--text-primary, #1a1a1a);
  font-weight: 500;
  line-height: 1.4;
  word-break: break-word;
}

.acp-session-item-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  min-width: 0;
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
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-secondary, #495057);
  font-family: monospace;
  word-break: break-all;
}

.acp-session-resume-btn {
  flex-shrink: 0;
  margin-left: 8px;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary, #495057);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: color 0.15s, background 0.15s;
}

.acp-session-resume-btn:hover {
  background: rgba(0, 102, 204, 0.08);
  color: var(--accent-color, #0066cc);
}

.acp-session-resume-btn:active {
  background: rgba(0, 102, 204, 0.14);
}

.acp-session-resume-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.acp-session-sentinel {
  height: 1px;
  width: 100%;
  flex-shrink: 0;
}

.acp-session-loading-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 8px 0;
  font-size: 12px;
  color: var(--text-muted, #999);
}

.acp-session-hidden-hint {
  padding: 8px 14px;
  font-size: 11px;
  color: var(--text-muted, #999);
  text-align: center;
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
