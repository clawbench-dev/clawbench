<template>
  <ModalDialog :open="open" :title="t('sessionMove.title')" :z-index="2600" @close="close">
    <div class="session-move-dialog">
      <!-- The current project stays visible: the list below only offers the
           OTHER projects, and without this line it is not obvious which one the
           conversation is leaving. It reads as a small card rather than a bare
           label so the eye separates "where it is" from "where it can go". -->
      <div class="sm-current">
        <Folder :size="16" class="sm-current-icon" />
        <div class="sm-current-main">
          <span class="sm-current-label">{{ t('sessionMove.currentLabel') }}</span>
          <span class="sm-current-name" :title="currentPath">{{ currentName }}</span>
        </div>
      </div>

      <div v-if="errorMsg" class="sm-error">{{ errorMsg }}</div>

      <SearchInput v-model="query" :placeholder="t('sessionMove.searchPlaceholder')" />

      <div class="sm-target-label">{{ t('sessionMove.chooseTarget') }}</div>

      <PanelSkeleton
        v-if="loading"
        class="sm-skeleton"
        variant="list"
        :rows="4"
        :meta="false"
        :label="t('common.loading')"
      />
      <div v-else-if="visibleTargets.length === 0" class="sm-empty">
        <FolderSearch :size="22" class="sm-empty-icon" />
        <span>{{ targets.length === 0 ? t('sessionMove.empty') : t('sessionMove.noMatch') }}</span>
      </div>
      <ul v-else class="sm-list">
        <li v-for="p in visibleTargets" :key="p.path" class="sm-row">
          <button
            type="button"
            class="sm-item"
            :data-project-path="p.path"
            :disabled="moving"
            @click="pick(p.path)"
          >
            <Folder :size="16" class="sm-item-icon" />
            <span class="sm-item-main">
              <span class="sm-item-name">{{ baseName(p.path) }}</span>
              <span class="sm-item-path" :title="p.path">{{ p.path }}</span>
            </span>
            <ChevronRight :size="16" class="sm-item-arrow" />
          </button>
        </li>
      </ul>
    </div>

    <template #footer>
      <button class="fbtn" type="button" @click="close">{{ t('common.cancel') }}</button>
    </template>
  </ModalDialog>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronRight, Folder, FolderSearch } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
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
/**
 * A PATCH is in flight. The list is clickable for the whole round trip, so a
 * second tap would send a second move: the first one succeeds, the second
 * answers 409/404 and the dialog reports "移动失败" for a move that worked.
 */
const moving = ref(false)

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
  if (!props.sessionId || moving.value) return
  moving.value = true
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
  } finally {
    moving.value = false
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
/* ModalDialog's .modal-body has no padding of its own (see modal-card.css),
   so the dialog owns its inset like every other dialog root. */
.session-move-dialog {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6) var(--space-7);
}

/* "Where it is" — a restrained info card, not a section header: it must be
   readable at a glance without competing with the target list below. */
.sm-current {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
}

.sm-current-icon {
  flex-shrink: 0;
  color: var(--accent-color);
}

.sm-current-main {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.sm-current-label {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.sm-current-name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Same error row as the other dialogs (see SessionTagDialog `.st-error`). */
.sm-error {
  font-size: var(--font-size-md);
  color: #e74c3c;
  padding: var(--space-4) var(--space-6);
  background: rgba(231, 76, 60, 0.1);
  border-radius: var(--radius-sm);
}

/* Section label for the list, matching the tag dialog's labels. */
.sm-target-label {
  margin-top: var(--space-1);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary);
}

.sm-skeleton {
  --panel-skeleton-inset: calc(10 * var(--dp));
  padding: var(--space-1) 0;
}

.sm-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-8) var(--space-6);
  font-size: var(--font-size-md);
  color: var(--text-muted);
  text-align: center;
}

.sm-empty-icon {
  color: var(--text-muted);
  opacity: var(--opacity-soft);
}

/* The target list is one bounded card so the dialog keeps a stable height and
   the rows read as one group rather than a loose column of labels. */
.sm-list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: min(calc(360 * var(--dp)), 46vh);
  overflow-y: auto;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
}

.sm-row + .sm-row {
  border-top: 1px solid var(--border-color);
}

.sm-item {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  min-height: calc(44 * var(--dp));
  padding: var(--space-3) var(--space-5);
  border: none;
  background: none;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .sm-item:hover {
    background: color-mix(in srgb, var(--text-primary) 7%, transparent);
  }
}

.sm-item:active {
  background: var(--bg-active);
}

.sm-item:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: calc(-2 * var(--dp));
}

/* A move is in flight: the rows stop responding rather than queueing a second
   PATCH that could only fail. */
.sm-item:disabled {
  opacity: 0.6;
  cursor: default;
  pointer-events: none;
}

.sm-item-icon {
  flex-shrink: 0;
  color: var(--text-muted);
}

.sm-item-main {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.sm-item-name {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sm-item-path {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Trailing affordance: this row IS the action (there is no confirm step), so a
   chevron is the hint that clicking leaves the dialog. */
.sm-item-arrow {
  flex-shrink: 0;
  color: var(--text-muted);
  opacity: var(--opacity-soft);
  transition: opacity var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .sm-item:hover .sm-item-arrow {
    opacity: 1;
    color: var(--accent-color);
  }
}
</style>
