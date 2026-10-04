<template>
  <div class="project-detail">
    <div v-if="loading" class="project-detail__empty">{{ t('common.loading') }}</div>
    <div v-else-if="!detail" class="project-detail__empty">{{ t('settings.items.projectLoadFailed') }}</div>
    <template v-else>
      <div class="project-detail__head">
        <div class="project-detail__name">{{ baseName(detail.path) }}</div>
        <div class="project-detail__path">{{ detail.path }}</div>
      </div>

      <dl class="project-detail__stats">
        <div class="project-detail__stat">
          <dt>{{ t('settings.items.projectDetailSessions') }}</dt>
          <dd>{{ detail.session_count }}</dd>
        </div>
        <div class="project-detail__stat">
          <dt>{{ t('settings.items.projectDetailLastActive') }}</dt>
          <dd>{{ lastActiveLabel }}</dd>
        </div>
        <div class="project-detail__stat">
          <dt>{{ t('settings.items.projectDetailCreatedAt') }}</dt>
          <dd>{{ formatRelativeTime(detail.created_at) || '-' }}</dd>
        </div>
        <div class="project-detail__stat">
          <dt>{{ t('settings.items.projectDetailDirStatus') }}</dt>
          <dd>{{ detail.exists ? t('settings.items.projectDetailDirExists') : t('settings.items.projectDetailDirMissing') }}</dd>
        </div>
        <div class="project-detail__stat">
          <dt>{{ t('settings.items.projectDetailRepoKind') }}</dt>
          <dd>{{ repoKindLabel }}</dd>
        </div>
      </dl>

      <div class="project-detail__actions">
        <button
          class="fbtn project-detail__btn project-detail__btn--primary"
          :disabled="isCurrent"
          @click="switchToProject"
        >
          {{ isCurrent ? t('settings.items.projectSwitchCurrent') : t('settings.items.projectSwitchTo') }}
        </button>
        <button
          class="fbtn project-detail__btn project-detail__btn--danger"
          :disabled="isCurrent"
          @click="deleteProject"
        >
          {{ t('settings.items.projectDelete') }}
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { store } from '@/stores/app'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { appLog } from '@/utils/appLog'
import { formatRelativeTime } from '@/utils/format'
import { baseName, normalizeSlashes } from '@/utils/path'

const TAG = 'ProjectDetailSetting'

/** One row of GET /api/projects/detail. */
interface ProjectDetail {
  id: number
  path: string
  session_count: number
  last_active_at: string
  created_at: string
  exists: boolean
  repo_kind: string
}

const props = defineProps<{ projectId: number }>()
const emit = defineEmits<{ deleted: [] }>()

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()

// Injected from App.vue (see hotSwitchProject / switchTab providers).
const hotSwitchProject = inject<((path: string) => Promise<void>) | null>('hotSwitchProject', null)
const switchTab = inject<((tab: string) => void) | undefined>('switchTab', undefined)

const detail = ref<ProjectDetail | null>(null)
const loading = ref(true)

const isCurrent = computed(() => {
  if (!detail.value) return false
  const cur = store.state.projectRoot
  return !!cur && normalizeSlashes(cur).replace(/\/+$/, '') === normalizeSlashes(detail.value.path).replace(/\/+$/, '')
})

const lastActiveLabel = computed(() => {
  if (!detail.value) return '-'
  return formatRelativeTime(detail.value.last_active_at) || t('settings.items.projectDetailNever')
})

const repoKindLabel = computed(() => {
  const kind = detail.value?.repo_kind ?? ''
  const map: Record<string, string> = {
    main: t('settings.items.projectRepoMain'),
    worktree: t('settings.items.projectRepoWorktree'),
    subdir: t('settings.items.projectRepoSubdir'),
    plain: t('settings.items.projectRepoPlain'),
    unknown: t('settings.items.projectRepoUnknown'),
  }
  return map[kind] ?? kind
})

async function load() {
  loading.value = true
  try {
    const resp = await fetch(`/api/projects/detail?id=${props.projectId}`)
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
    detail.value = (await resp.json()) as ProjectDetail
  } catch (err) {
    appLog.e(TAG, 'failed to load project detail', err)
    toast.show(t('settings.items.projectLoadFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  } finally {
    loading.value = false
  }
}

async function switchToProject() {
  if (!detail.value || isCurrent.value) return
  if (!hotSwitchProject) return
  try {
    await hotSwitchProject(detail.value.path)
    // Close the settings panel and land on the chat tab for the new project.
    switchTab?.('chat')
  } catch (err) {
    appLog.e(TAG, 'failed to switch project', err)
    toast.show(t('settings.items.projectSwitchFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

async function deleteProject() {
  if (!detail.value || isCurrent.value) return
  const name = baseName(detail.value.path)

  // A project whose directory is already gone needs only a confirmation: the
  // typed-name step exists to stop someone nuking a live project by misclick,
  // and there is nothing live left here to protect. A still-present directory
  // keeps the stricter typed-name gate.
  if (!detail.value.exists) {
    const ok = await dialog.confirm(
      t('settings.items.projectDeleteConfirmPromptMissing', { name }),
      {
        title: t('settings.items.projectDeleteConfirmTitle'),
        confirmText: t('settings.items.projectDelete'),
        dangerous: true,
      },
    )
    if (!ok) return
  } else {
    const input = await dialog.prompt(
      t('settings.items.projectDeleteConfirmPrompt', { name }),
      {
        title: t('settings.items.projectDeleteConfirmTitle'),
        confirmText: t('settings.items.projectDelete'),
        dangerous: true,
      },
    )
    if (input === null) return
    if (input.trim() !== name) {
      toast.show(t('settings.items.projectDeleteNameMismatch'), { icon: '⚠️', type: 'error', duration: 3000 })
      return
    }
  }

  try {
    const resp = await fetch(`/api/projects/detail?id=${props.projectId}`, { method: 'DELETE' })
    if (!resp.ok) {
      const body = await resp.json().catch(() => ({}))
      const msg = body.error || t('settings.items.projectDeleteFailed')
      toast.show(msg, { icon: '⚠️', type: 'error', duration: 3000 })
      return
    }
    emit('deleted')
  } catch (err) {
    appLog.e(TAG, 'failed to delete project', err)
    toast.show(t('settings.items.projectDeleteFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  }
}

onMounted(load)

// Re-fetch when the route's project id changes without a remount (e.g. a
// deep-link or pendingSettingsCategory navigating project:1 → project:2). The
// component is keyed by id in the parent, but a watcher keeps it correct even
// if that ever changes, and avoids rendering the previous project's stats.
watch(() => props.projectId, load)
</script>

<style scoped>
.project-detail {
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
}

.project-detail__empty {
  padding: var(--space-7);
  color: var(--text-muted);
  font-size: var(--font-size-md);
  text-align: center;
}

.project-detail__head {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.project-detail__name {
  color: var(--text-primary);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
}

.project-detail__path {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  word-break: break-all;
}

.project-detail__stats {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.project-detail__stat {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-5);
}

.project-detail__stat dt {
  color: var(--text-secondary);
  font-size: var(--font-size-md);
}

.project-detail__stat dd {
  margin: 0;
  color: var(--text-primary);
  font-size: var(--font-size-md);
}

.project-detail__actions {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.project-detail__btn {
  width: 100%;
}

.project-detail__btn--primary {
  background: var(--accent-color);
  color: #fff;
}

.project-detail__btn--danger {
  background: var(--danger-color, #ef4444);
  color: #fff;
}

.project-detail__btn:disabled {
  opacity: var(--opacity-disabled);
  cursor: not-allowed;
}
</style>
