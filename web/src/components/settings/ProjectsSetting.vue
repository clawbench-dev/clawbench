<template>
  <div class="projects-setting">
    <!-- Search box: the registry can hold many projects, so filtering is the
         primary way to find one. -->
    <div class="projects-search">
      <Search :size="15" class="projects-search__icon" />
      <input
        v-model="query"
        class="projects-search__input"
        type="text"
        :placeholder="t('settings.items.projectListSearchPlaceholder')"
      />
    </div>

    <div v-if="loading" class="projects-empty">{{ t('common.loading') }}</div>
    <div v-else-if="filtered.length === 0" class="projects-empty">
      {{ query ? t('settings.items.projectListEmpty') : t('settings.items.projectListEmpty') }}
    </div>
    <ul v-else class="projects-list">
      <li
        v-for="p in filtered"
        :key="p.id"
        class="projects-row"
        :class="{ 'projects-row--current': isCurrent(p) }"
        :data-project-id="p.id"
        @click="open(p)"
      >
        <span
          class="projects-row__dot"
          :class="p.exists ? 'projects-row__dot--ok' : 'projects-row__dot--gone'"
          :title="p.exists ? t('settings.items.projectDetailDirExists') : t('settings.items.projectDetailDirMissing')"
        />
        <div class="projects-row__main">
          <div class="projects-row__name">
            {{ baseName(p.path) }}
            <span v-if="isCurrent(p)" class="projects-row__badge">{{ t('settings.items.projectListCurrent') }}</span>
          </div>
          <div class="projects-row__path">{{ p.path }}</div>
        </div>
        <span class="projects-row__time">{{ lastActiveLabel(p) }}</span>
        <ChevronRight :size="16" class="projects-row__arrow" />
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronRight, Search } from 'lucide-vue-next'
import { store } from '@/stores/app'
import { useToast } from '@/composables/useToast'
import { appLog } from '@/utils/appLog'
import { formatRelativeTime } from '@/utils/format'
import { normalizeSlashes } from '@/utils/path'

const TAG = 'ProjectsSetting'

/** One row of GET /api/projects/list. */
interface ProjectListItem {
  id: number
  path: string
  session_count: number
  last_active_at: string
  created_at: string
  exists: boolean
}

const emit = defineEmits<{ navigate: [categoryId: string] }>()

const { t } = useI18n()
const toast = useToast()

const projects = ref<ProjectListItem[]>([])
const loading = ref(true)
const query = ref('')

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return projects.value
  return projects.value.filter(p =>
    p.path.toLowerCase().includes(q) || baseName(p.path).toLowerCase().includes(q),
  )
})

/** basename of a path, tolerating both separators. */
function baseName(path: string): string {
  const norm = normalizeSlashes(path).replace(/\/+$/, '')
  const idx = norm.lastIndexOf('/')
  return idx >= 0 ? norm.slice(idx + 1) || norm : norm
}

/** Whether this project is the one currently open in the app. */
function isCurrent(p: ProjectListItem): boolean {
  const cur = store.state.projectRoot
  return !!cur && normalizeSlashes(cur).replace(/\/+$/, '') === normalizeSlashes(p.path).replace(/\/+$/, '')
}

/** Last-active label: relative time, falling back to created-at when unused. */
function lastActiveLabel(p: ProjectListItem): string {
  const rel = formatRelativeTime(p.last_active_at || p.created_at)
  return rel
}

function open(p: ProjectListItem) {
  emit('navigate', `project:${p.id}`)
}

async function load() {
  loading.value = true
  try {
    const resp = await fetch('/api/projects/list')
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
    const data = await resp.json()
    projects.value = (data.projects || []) as ProjectListItem[]
  } catch (err) {
    appLog.e(TAG, 'failed to load project list', err)
    toast.show(t('settings.items.projectListLoadFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.projects-setting {
  display: flex;
  flex-direction: column;
}

.projects-search {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-7);
  border-bottom: 1px solid var(--border-color);
}

.projects-search__icon {
  flex-shrink: 0;
  color: var(--text-muted);
}

.projects-search__input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font-size: var(--font-size-md);
}

.projects-empty {
  padding: var(--space-7);
  color: var(--text-muted);
  font-size: var(--font-size-md);
  text-align: center;
}

.projects-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.projects-row {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  padding: var(--space-5) var(--space-7);
  cursor: pointer;
  position: relative;
}

.projects-row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 40px;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  .projects-row:hover {
    background: var(--bg-tertiary);
  }
}

.projects-row:active {
  background: var(--bg-tertiary);
}

.projects-row--current .projects-row__name {
  color: var(--accent-color);
  font-weight: var(--font-weight-semibold);
}

.projects-row__dot {
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.projects-row__dot--ok {
  background: var(--success-color, #22c55e);
}

.projects-row__dot--gone {
  background: var(--text-muted);
  opacity: var(--opacity-muted);
}

.projects-row__main {
  flex: 1;
  min-width: 0;
}

.projects-row__name {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.projects-row__badge {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
  padding: 1px var(--space-3);
  border-radius: var(--radius-full);
}

.projects-row__path {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.projects-row__time {
  flex-shrink: 0;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

.projects-row__arrow {
  flex-shrink: 0;
  color: var(--text-muted);
}
</style>
