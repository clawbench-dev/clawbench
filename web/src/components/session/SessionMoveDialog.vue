<template>
  <ModalDialog :open="open" :title="t('sessionMove.title')" :z-index="2600" @close="close">
    <div class="session-move-dialog">
      <!-- The current project stays visible: the list below only offers the
           OTHER projects, and without this line it is not obvious which one the
           conversation is leaving. -->
      <div class="sm-current">
        <span class="sm-current-label">{{ t('sessionMove.currentLabel') }}</span>
        <span class="sm-current-path" :title="currentPath">{{ currentName }}</span>
      </div>

      <div v-if="errorMsg" class="sm-error">{{ errorMsg }}</div>

      <div class="sm-search-row">
        <input
          v-model="query"
          class="sm-input"
          type="text"
          :placeholder="t('sessionMove.searchPlaceholder')"
          :aria-label="t('sessionMove.searchPlaceholder')"
        />
      </div>
      <PanelSkeleton v-if="loading" class="sm-skeleton" variant="text" :rows="3" :label="t('common.loading')" />
      <div v-else-if="visibleTargets.length === 0" class="sm-empty">
        {{ targets.length === 0 ? t('sessionMove.empty') : t('sessionMove.noMatch') }}
      </div>
      <div v-else class="sm-list">
        <button
          v-for="p in visibleTargets"
          :key="p.path"
          type="button"
          class="sm-item"
          :data-project-path="p.path"
          @click="pick(p.path)"
        >
          <span class="sm-item-name">{{ baseName(p.path) }}</span>
          <span class="sm-item-path" :title="p.path">{{ p.path }}</span>
        </button>
      </div>
    </div>

    <template #footer>
      <button class="fbtn" type="button" @click="close">{{ t('common.cancel') }}</button>
    </template>
  </ModalDialog>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import { apiGet, apiPatch } from '@/utils/api.ts'
import { normalizeSlashes } from '@/utils/path.ts'
import { appLog } from '@/utils/appLog'

// One row of GET /api/projects/list: {id, path, exists, ...}. Only path and
// exists are used here.
const props = defineProps({
  open: { type: Boolean, default: false },
  sessionId: { type: String, default: '' },
  /** The conversation's current project path, so it can be left out of the list. */
  currentPath: { type: String, default: '' },
})

const emit = defineEmits(['close', 'moved'])

const { t } = useI18n()

const targets = ref([])
const loading = ref(false)
const errorMsg = ref('')
const query = ref('')

/** Trailing slashes dropped, so two spellings of one directory compare equal. */
function normPath(p) {
  return normalizeSlashes(p || '').replace(/\/+$/, '')
}

function baseName(path) {
  const norm = normPath(path)
  const idx = norm.lastIndexOf('/')
  return idx >= 0 ? norm.slice(idx + 1) || norm : norm
}

const currentName = computed(() => baseName(props.currentPath) || props.currentPath)

const visibleTargets = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return targets.value
  return targets.value.filter(p =>
    p.path.toLowerCase().includes(q) || baseName(p.path).toLowerCase().includes(q),
  )
})

async function load() {
  loading.value = true
  errorMsg.value = ''
  query.value = ''
  try {
    const data = await apiGet('/api/projects/list')
    const cur = normPath(props.currentPath)
    // Only registered projects that still exist on disk, minus the one the
    // conversation is already in. A missing directory would hand the agent a
    // working directory it cannot use.
    targets.value = (data?.projects || []).filter(p => p.exists && normPath(p.path) !== cur)
  } catch (err) {
    appLog.e('SessionMoveDialog', 'failed to load project list', err)
    targets.value = []
    errorMsg.value = t('sessionMove.loadFailed')
  } finally {
    loading.value = false
  }
}

async function pick(path) {
  if (!props.sessionId) return
  errorMsg.value = ''
  try {
    await apiPatch(
      `/api/ai/session/update?session_id=${encodeURIComponent(props.sessionId)}`,
      { projectPath: path },
    )
    emit('moved', path)
    emit('close')
  } catch (err) {
    // The refusals are distinct user situations, and a generic "failed" would
    // leave them guessing whether a retry could ever work.
    const msgKey = err?.msgKey || ''
    if (msgKey === 'MoveTargetInvalid') {
      errorMsg.value = t('sessionMove.targetInvalid')
    } else if (msgKey === 'MoveNotAllowed') {
      errorMsg.value = t('sessionMove.notAllowed')
    } else if (msgKey === 'MoveMembersUnavailable') {
      errorMsg.value = t('sessionMove.membersUnavailable', { members: unavailableMembersText(err) })
    } else if (msgKey === 'SessionRunning' || msgKey === 'GroupRunning') {
      errorMsg.value = t('sessionMove.running')
    } else {
      appLog.e('SessionMoveDialog', 'failed to move session', err)
      errorMsg.value = t('sessionMove.moveFailed')
    }
  }
}

/**
 * Names of the members the server refused to move. `detail.members` is the
 * machine-readable list; the message itself is already localized for the
 * request and is only a fallback when an older server returns no list.
 */
function unavailableMembersText(err) {
  const detail = err?.detail
  const names = Array.isArray(detail?.members) ? detail.members.filter(Boolean) : []
  if (names.length > 0) return names.join(t('common.listSeparator'))
  return typeof detail?.Members === 'string' ? detail.Members : ''
}

function close() {
  emit('close')
}

watch(() => props.open, (open) => {
  if (open) load()
})
</script>

<style scoped>
.session-move-dialog {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.sm-current {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}

.sm-current-label {
  flex-shrink: 0;
}

.sm-current-path {
  color: var(--text-primary);
  font-weight: var(--font-weight-medium);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sm-error {
  font-size: var(--font-size-md);
  color: var(--danger-color, #e74c3c);
  padding: var(--space-4) var(--space-6);
  background: rgba(231, 76, 60, 0.1);
  border-radius: var(--radius-sm);
}

.sm-search-row {
  display: flex;
}

/* Native inputs carry no styling of their own (the global reset zeroes
   padding), so the border and hit area must be spelled out. */
.sm-input {
  flex: 1;
  min-width: 0;
  height: calc(32 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm);
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  font-size: var(--font-size-md);
}

.sm-input:focus {
  outline: none;
  border-color: var(--accent-color, #0066cc);
}

.sm-input::placeholder {
  color: var(--text-muted);
}

.sm-empty {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  padding: var(--space-3) 0;
}

.sm-list {
  display: flex;
  flex-direction: column;
  max-height: 45vh;
  overflow-y: auto;
}

.sm-item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  width: 100%;
  padding: var(--space-4) var(--space-5);
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  text-align: left;
  cursor: pointer;
}

.sm-item:hover,
.sm-item:focus-visible {
  background: var(--bg-secondary, #f1f3f5);
  outline: none;
}

.sm-item-name {
  font-size: var(--font-size-md);
  color: var(--text-primary);
}

.sm-item-path {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}
</style>
