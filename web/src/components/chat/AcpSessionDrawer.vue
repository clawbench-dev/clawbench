<template>
  <BottomSheet :open="open" auto :title="drawerTitle" @close="$emit('close')">
    <template #header>
      <!-- Wrapped, not classed: .bs-header-icon carries 4dp padding for lucide
           glyphs; applying it to AgentIcon's root would shrink its own sized
           box instead of adding breathing room. -->
      <span class="bs-header-icon">
        <AgentIcon :backend="backendId" :name="backendDisplayName" :avatar="agentAvatar" size="md" />
      </span>
      <span class="bs-header-title">{{ drawerTitle }}</span>
    </template>
    <div class="acp-session-search-row">
      <SearchInput v-model="searchQuery" :placeholder="t('chat.acpSession.searchPlaceholder')" />
      <button
        v-if="hasUntitledNonCodex"
        type="button"
        class="acp-session-filter-chip"
        :class="{ active: hideUntitled }"
        @click="toggleHideUntitled"
      >
        {{ t('chat.acpSession.hideUntitled') }}
      </button>
    </div>
    <div class="acp-session-list">
      <div v-if="acpSessionsLoading && acpSessions.length === 0" class="acp-session-skeleton">
        <PanelSkeleton variant="list" :rows="8" :meta="true" :label="t('chat.acpSession.loading')" />
      </div>
      <div v-else-if="acpSessionsNotSupported" class="acp-session-empty">
        {{ t('chat.acpSession.notSupported') }}
      </div>
      <div v-else-if="acpSessions.length === 0" class="acp-session-empty">
        {{ t('chat.acpSession.empty') }}
      </div>
      <div v-else-if="filteredSessions.length === 0" class="acp-session-empty">
        {{ t('chat.acpSession.noResults') }}
        <div v-if="hiddenLocalCount > 0" class="acp-session-hidden-actions">
          <button type="button" class="acp-session-link-btn" @click="showHidden = !showHidden">
            {{ showHidden ? t('chat.acpSession.hideHidden', { count: hiddenLocalCount }) : t('chat.acpSession.showHidden', { count: hiddenLocalCount }) }}
          </button>
          <button type="button" class="acp-session-link-btn" @click="restoreAllHidden">
            {{ t('chat.acpSession.restoreAllHidden') }}
          </button>
        </div>
      </div>
      <template v-else>
        <div
          v-for="session in filteredSessions"
          :key="session.sessionId"
          class="acp-session-item"
          :class="{ 'is-hidden-local': isLocallyHidden(session.sessionId) }"
        >
            <div class="acp-session-item-info">
              <span class="acp-session-item-title">{{ session.title || t('chat.acpSession.untitled') }}</span>
              <div class="acp-session-item-meta">
                <span v-if="session.updatedAt" class="acp-session-item-time">{{ formatRelativeTime(session.updatedAt) }}</span>
                <span class="acp-session-item-id" :title="session.sessionId">{{ session.sessionId }}</span>
              </div>
            </div>
          <button
            type="button"
            class="acp-session-hide-btn"
            :title="isLocallyHidden(session.sessionId) ? t('chat.acpSession.unhideSession') : t('chat.acpSession.hideSession')"
            :aria-label="isLocallyHidden(session.sessionId) ? t('chat.acpSession.unhideSession') : t('chat.acpSession.hideSession')"
            @click.stop="toggleLocalHide(session.sessionId)"
          >
            <Eye v-if="isLocallyHidden(session.sessionId)" :size="14" />
            <EyeOff v-else :size="14" />
          </button>
          <button
            class="acp-session-resume-btn"
            :disabled="acpResuming"
            :title="t('chat.acpSession.title')"
            @click.stop="handleSelect(session)"
          >
            <LoadingIndicator v-if="resumingId === session.sessionId" size="sm" inline />
            <ImportIcon v-else :size="14" />
          </button>
        </div>
        <div v-if="acpSessionsLoading && acpSessions.length > 0" class="acp-session-loading-more">
          <LoadingIndicator size="sm" inline :label="t('chat.acpSession.loading')" />
        </div>
        <div v-if="hiddenLocalCount > 0" class="acp-session-hidden-actions">
          <button type="button" class="acp-session-link-btn" @click="showHidden = !showHidden">
            {{ showHidden ? t('chat.acpSession.hideHidden', { count: hiddenLocalCount }) : t('chat.acpSession.showHidden', { count: hiddenLocalCount }) }}
          </button>
          <button type="button" class="acp-session-link-btn" @click="restoreAllHidden">
            {{ t('chat.acpSession.restoreAllHidden') }}
          </button>
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
    <Transition name="loading-fade">
      <LoadingIndicator v-if="acpResuming" fixed size="md" :label="t('chat.acpSession.resuming')" />
    </Transition>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, computed, toRef, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Eye, EyeOff, Import as ImportIcon } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import { useAcpSession, type AcpSessionInfo } from '@/composables/useAcpSession'
import { useAgents } from '@/composables/useAgents'
import { getBackendDisplayName } from '@/utils/backendNames'
import { formatRelativeTime } from '@/utils/format'
import { storageGet, storageRemove, storageSet } from '@/utils/safeStorage'
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
const { getAgent, getAgentBackend, getAgentAvatar } = useAgents()
const resumingId = ref('')
const searchQuery = ref('')

const backendId = computed(() => getAgentBackend(props.agentId))
const agentAvatar = computed(() => getAgentAvatar(props.agentId))
const backendDisplayName = computed(() => getBackendDisplayName(backendId.value))
const drawerTitle = computed(() => t('chat.acpSession.resumeTitle', { agent: backendDisplayName.value }))

function normalizeProjectPath(p: string): string {
  const normalized = p.trim().replace(/\\/g, '/')
  const isUnc = normalized.startsWith('//')
  const isWindows = isUnc || /^[A-Za-z]:\//.test(normalized)
  const parts: string[] = []
  for (const part of normalized.split('/')) {
    if (!part || part === '.') continue
    if (part === '..') {
      parts.pop()
    } else {
      parts.push(part)
    }
  }
  let result = parts.join('/')
  if (isUnc) result = `//${result}`
  else if (normalized.startsWith('/')) result = `/${result}`
  if (isWindows) result = result.toLowerCase()
  return result.replace(/\/+$/, '')
}

const HIDE_UNTITLED_STORAGE_KEY = 'clawbench_acp_hide_untitled'

function hiddenStorageKey(agentId: string): string {
  return `clawbench_hidden_acp_sessions:${agentId}`
}

function readHiddenIds(agentId: string): Set<string> {
  if (!agentId) return new Set()
  const raw = storageGet(hiddenStorageKey(agentId))
  if (!raw) return new Set()
  try {
    const parsed = JSON.parse(raw)
    return new Set(Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === 'string') : [])
  } catch {
    return new Set()
  }
}

function writeHiddenIds(agentId: string, ids: Set<string>): void {
  if (!agentId) return
  if (ids.size === 0) {
    storageRemove(hiddenStorageKey(agentId))
  } else {
    storageSet(hiddenStorageKey(agentId), JSON.stringify([...ids]))
  }
}

const hiddenSessionIds = ref<Set<string>>(readHiddenIds(props.agentId))
const showHidden = ref(false)
const hideUntitled = ref(storageGet(HIDE_UNTITLED_STORAGE_KEY) === '1')

watch(() => props.agentId, (aid) => {
  hiddenSessionIds.value = readHiddenIds(aid)
  showHidden.value = false
})

function isLocallyHidden(sessionId: string): boolean {
  return hiddenSessionIds.value.has(sessionId)
}

function toggleLocalHide(sessionId: string): void {
  const next = new Set(hiddenSessionIds.value)
  if (next.has(sessionId)) {
    next.delete(sessionId)
  } else {
    next.add(sessionId)
  }
  hiddenSessionIds.value = next
  writeHiddenIds(props.agentId, next)
}

function restoreAllHidden(): void {
  hiddenSessionIds.value = new Set()
  showHidden.value = false
  writeHiddenIds(props.agentId, hiddenSessionIds.value)
}

function toggleHideUntitled(): void {
  hideUntitled.value = !hideUntitled.value
  storageSet(HIDE_UNTITLED_STORAGE_KEY, hideUntitled.value ? '1' : '0')
}

/**
 * Codex's on-disk session list contains untitled placeholder rollouts that its
 * own UI never shows, so they are hidden here. The fact comes from the server
 * (computed from the backend's spec) instead of matching on the backend id, so
 * a future backend with the same quirk only has to declare it once.
 */
const displayableSessions = computed(() => {
  if (getAgent(props.agentId)?.listSessionsOmitsEmptyTitle !== true) return acpSessions.value
  return acpSessions.value.filter((session) => session.title.trim() !== '')
})

const sessionsInCurrentProject = computed(() => {
  const root = normalizeProjectPath(store.state.projectRoot || '')
  if (!root) return []
  return displayableSessions.value.filter((s) => normalizeProjectPath(s.cwd || '') === root)
})

const hidesEmptyTitlesByDefault = computed(
  () => getAgent(props.agentId)?.listSessionsOmitsEmptyTitle === true,
)

const hasUntitledNonCodex = computed(() =>
  !hidesEmptyTitlesByDefault.value &&
  sessionsInCurrentProject.value.some((s) => s.title.trim() === ''),
)

const hiddenOtherProjectCount = computed(
  () => displayableSessions.value.length - sessionsInCurrentProject.value.length
)

const hiddenLocalCount = computed(() =>
  sessionsInCurrentProject.value.filter((s) => hiddenSessionIds.value.has(s.sessionId)).length
)

const filteredSessions = computed(() => {
  let base = sessionsInCurrentProject.value
  // Capability-driven, not backend-name-driven: a backend that already drops
  // untitled rows server-side has nothing for this toggle to do, and any future
  // backend declaring the same capability gets the same treatment.
  if (hideUntitled.value && !hidesEmptyTitlesByDefault.value) {
    base = base.filter((s) => s.title.trim() !== '')
  }
  if (!showHidden.value) {
    base = base.filter((s) => !hiddenSessionIds.value.has(s.sessionId))
  }
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
</script>

<style scoped>
.acp-session-search-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-5) calc(14 * var(--dp));
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
  gap: var(--space-4);
  color: var(--text-muted, #999);
  font-size: var(--font-size-md);
}

/* First load: preview the session rows; the empty states stay centred. */
.acp-session-skeleton {
  /* 14dp is `.acp-session-item`'s inset. */
  --panel-skeleton-inset: calc(14 * var(--dp));
  padding: var(--space-2) 0;
}

.acp-session-item {
  position: relative;
  display: flex;
  align-items: flex-start;
  padding: var(--space-6) calc(14 * var(--dp));
  border-top: 1px solid var(--border-color, #dee2e6);
}

.acp-session-item-info {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
  flex: 1;
  padding-top: var(--space-1);
}

.acp-session-item-title {
  font-size: var(--font-size-md);
  color: var(--text-primary, #1a1a1a);
  font-weight: var(--font-weight-medium);
  line-height: var(--line-height-snug);
  word-break: break-word;
}

.acp-session-item-meta {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
  min-width: 0;
}

.acp-session-item-time {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
}

.acp-session-item-id {
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  font-weight: var(--font-weight-medium);
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-secondary, #495057);
  font-family: var(--font-mono);
  word-break: break-all;
}

.acp-session-resume-btn {
  flex-shrink: 0;
  margin-left: var(--space-4);
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary, #495057);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: color var(--duration-base), background var(--duration-base);
}

@media (hover: hover) {
  .acp-session-resume-btn:hover {
    background: rgba(0, 102, 204, 0.08);
    color: var(--accent-color, #0066cc);
  }
}

.acp-session-resume-btn:active {
  background: rgba(0, 102, 204, 0.14);
}

.acp-session-resume-btn:disabled {
  opacity: var(--opacity-muted);
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
  gap: var(--space-3);
  padding: var(--space-4) 0;
  font-size: var(--font-size-sm);
  color: var(--text-muted, #999);
}

.acp-session-hidden-hint {
  padding: var(--space-4) calc(14 * var(--dp));
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  text-align: center;
}

.acp-session-filter-chip {
  flex-shrink: 0;
  padding: var(--space-2) var(--space-4);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary, #495057);
  font-size: var(--font-size-xs);
  cursor: pointer;
  white-space: nowrap;
}

.acp-session-filter-chip.active {
  border-color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
  color: var(--accent-color, #0066cc);
}

.acp-session-item.is-hidden-local {
  opacity: 0.6;
}

.acp-session-hide-btn {
  flex-shrink: 0;
  margin-left: var(--space-2);
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: color var(--duration-base), background var(--duration-base);
}

@media (hover: hover) {
  .acp-session-hide-btn:hover {
    background: rgba(0, 0, 0, 0.06);
    color: var(--text-primary, #1a1a1a);
  }
}

.acp-session-hidden-actions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-5);
  padding: var(--space-3) calc(14 * var(--dp));
}

.acp-session-link-btn {
  border: none;
  background: transparent;
  color: var(--accent-color, #0066cc);
  font-size: var(--font-size-xs);
  cursor: pointer;
  padding: 0;
}
</style>
