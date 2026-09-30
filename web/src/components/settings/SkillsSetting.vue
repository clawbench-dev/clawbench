<template>
  <div class="skills-setting">
    <div class="skills-desc">{{ description }}</div>

    <!-- Master switch. Off stops injecting the skill table into any system
         prompt; scanning and the listing below keep working. -->
    <SettingsItem
      :label="t('settings.items.skillsEnabled')"
      :description="t('settings.items.skillsEnabledDesc')"
      type="switch"
      :model-value="enabled"
      @update:model-value="onToggleEnabled"
    />

    <!-- User's own skill directories. A list, because skills often live in
         several places (a personal collection plus a shared team checkout).
         Each row is an absolute path, like fonts.dir. -->
    <div class="skills-block">
      <div class="skills-block-title">{{ t('settings.items.skillsDirs') }}</div>
      <div class="skills-dirs-desc">{{ t('settings.items.skillsDirsDesc') }}</div>

      <div v-for="(dir, idx) in dirs" :key="dir + idx" class="skills-dir">
        <input
          class="skills-input"
          :value="dir"
          @change="onDirChange(idx, ($event.target as HTMLInputElement).value)"
        />
        <button class="sbtn" @click="removeDir(idx)">
          <Trash2 :size="13" />
          {{ t('settings.items.skillsRepoRemove') }}
        </button>
      </div>

      <div class="skills-dir-add">
        <input
          v-model="newDir"
          class="skills-input"
          :placeholder="t('settings.items.skillsDirPlaceholder')"
        />
        <button class="sbtn sbtn-primary" :disabled="!newDir.trim() || saving" @click="addDir">
          {{ t('settings.items.skillsRepoAdd') }}
        </button>
      </div>
    </div>

    <!-- Remote git repositories. -->
    <div class="skills-block">
      <div class="skills-block-title">{{ t('settings.items.skillsRepos') }}</div>

      <div v-for="(repo, idx) in repos" :key="repo.url + idx" class="skills-repo">
        <div class="skills-repo-head">
          <span class="skills-repo-url">{{ repo.url }}</span>
          <span v-if="repo.has_token" class="skills-repo-badge">
            <KeyRound :size="12" />
            {{ t('settings.items.skillsRepoTokenSet') }}
          </span>
          <button class="sbtn" @click="removeRepo(idx)">
            <Trash2 :size="13" />
            {{ t('settings.items.skillsRepoRemove') }}
          </button>
        </div>
        <div v-if="repo.last_error" class="skills-repo-error">
          <AlertCircle :size="13" />
          <span>{{ repo.last_error }}</span>
        </div>
        <!-- Token is write-only: the server reports only has_token, so this
             field starts empty and means "replace the stored token". -->
        <input
          v-model="repoTokens[idx]"
          class="skills-input"
          type="password"
          autocomplete="off"
          :placeholder="t('settings.items.skillsRepoTokenPlaceholder')"
        />
      </div>

      <div class="skills-repo-add">
        <input
          v-model="newRepoUrl"
          class="skills-input"
          :placeholder="t('settings.items.skillsRepoUrlPlaceholder')"
        />
        <input
          v-model="newRepoToken"
          class="skills-input"
          type="password"
          autocomplete="off"
          :placeholder="t('settings.items.skillsRepoTokenPlaceholder')"
        />
        <button class="sbtn sbtn-primary" :disabled="!newRepoUrl.trim() || saving" @click="addRepo">
          {{ t('settings.items.skillsRepoAdd') }}
        </button>
      </div>
    </div>

    <!-- Manual sync + status. -->
    <div class="skills-sync">
      <button class="sbtn" :disabled="refreshing" @click="refresh">
        <LoadingIndicator v-if="refreshing" size="sm" inline class="skills-spin" />
        <RefreshCw v-else :size="13" />
        {{ refreshing ? t('settings.items.skillsRefreshing') : t('settings.items.skillsRefresh') }}
      </button>
      <span v-if="lastSyncText" class="skills-sync-meta">{{ lastSyncText }}</span>
    </div>

    <div v-if="error" class="skills-error">
      <AlertCircle :size="13" />
      <span>{{ error }}</span>
    </div>

    <!-- Discovered skills, grouped by source. -->
    <div v-if="skills.length" class="skills-block">
      <div class="skills-block-title">
        {{ t('settings.items.skillsDiscovered', { count: skills.length }) }}
      </div>
      <div v-for="s in skills" :key="s.path" class="skills-item">
        <div class="skills-item-head">
          <span class="skills-item-name">{{ s.name }}</span>
          <span class="skills-item-source">{{ sourceLabel(s) }}</span>
          <!-- The spec requires the frontmatter name to match the directory
               name; an agent resolves a skill by directory, so a mismatch means
               this skill cannot be offered as a slash command. -->
          <span v-if="s.name_mismatch" class="skills-item-warn" :title="t('settings.items.skillsNameMismatchDesc')">
            <AlertTriangle :size="12" />
            {{ t('settings.items.skillsNameMismatch') }}
          </span>
        </div>
        <div class="skills-item-desc">{{ s.description }}</div>
        <div class="skills-item-path">{{ s.path }}</div>
      </div>
    </div>
    <div v-else class="skills-empty">{{ t('settings.items.skillsEmpty') }}</div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, AlertTriangle, KeyRound, RefreshCw, Trash2 } from 'lucide-vue-next'
import SettingsItem from './SettingsItem.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { appLog } from '@/utils/appLog'

defineProps<{ description?: string }>()

const TAG = 'SkillsSetting'
const { t } = useI18n()
const { getServerValueWithDefault, setServerValue } = useSettingsConfig()

interface RepoRow {
  url: string
  slug: string
  has_token: boolean
  last_error?: string
}

interface SkillRow {
  name: string
  description: string
  path: string
  source_kind: 'own' | 'user' | 'git' | 'other'
  source_label: string
  agent_id?: string
  /** Frontmatter name disagrees with the directory name (spec violation). */
  name_mismatch?: boolean
}

const enabled = ref(true)
const dirs = ref<string[]>([])
const newDir = ref('')
const repos = ref<RepoRow[]>([])
const repoTokens = ref<Record<number, string>>({})
const skills = ref<SkillRow[]>([])
const lastSyncAt = ref(0)
const refreshing = ref(false)
const saving = ref(false)
const error = ref('')
const newRepoUrl = ref('')
const newRepoToken = ref('')

const lastSyncText = computed(() => {
  if (!lastSyncAt.value) return ''
  const d = new Date(lastSyncAt.value * 1000)
  return t('settings.items.skillsLastSync', { time: d.toLocaleString() })
})

/** Human-readable origin for a discovered skill. */
function sourceLabel(s: SkillRow): string {
  switch (s.source_kind) {
    case 'own':
      return t('settings.items.skillsSourceOwn')
    case 'user':
      return t('settings.items.skillsSourceUser')
    case 'git':
      return t('settings.items.skillsSourceGit', { name: s.source_label })
    case 'other':
      return t('settings.items.skillsSourceOther', { agent: s.agent_id || s.source_label })
    default:
      return s.source_label
  }
}

async function load() {
  try {
    // enabled comes from the config store so a PATCH elsewhere is reflected
    // here; the dirs + repo list + discovered skills come from GET /api/skills,
    // which reports the RESOLVED directories (including the default fallback).
    enabled.value = getServerValueWithDefault('skills.enabled') !== false

    const res = await fetch('/api/skills')
    if (!res.ok) return
    const data = await res.json()
    dirs.value = Array.isArray(data?.dirs) ? data.dirs : []
    repos.value = Array.isArray(data?.repos) ? data.repos : []
    skills.value = Array.isArray(data?.skills) ? data.skills : []
    lastSyncAt.value = Number(data?.last_sync_at) || 0
    repoTokens.value = {}
  } catch (err) {
    appLog.w(TAG, 'load failed', err)
  }
}

async function onToggleEnabled(v: unknown) {
  enabled.value = Boolean(v)
  await patch('skills.enabled', enabled.value)
}

/**
 * Persist the whole directory list. The endpoint replaces the array wholesale,
 * so every mutation sends the full list.
 */
async function saveDirs(next: string[]) {
  saving.value = true
  error.value = ''
  try {
    await setServerValue('skills.dirs', next)
    newDir.value = ''
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

async function onDirChange(idx: number, value: string) {
  const next = [...dirs.value]
  next[idx] = value.trim()
  await saveDirs(next)
}

async function addDir() {
  const dir = newDir.value.trim()
  if (!dir) return
  await saveDirs([...dirs.value, dir])
}

async function removeDir(idx: number) {
  await saveDirs(dirs.value.filter((_, i) => i !== idx))
}

/** Send a scalar skills setting, surfacing server-side validation errors. */
async function patch(path: string, value: unknown) {
  error.value = ''
  try {
    await setServerValue(path, value)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

/**
 * Persist the whole repo list. The endpoint replaces the array wholesale, so
 * every mutation sends the full list — including any tokens the user typed in
 * the per-row fields.
 */
async function saveRepos(next: RepoRow[]) {
  saving.value = true
  error.value = ''
  try {
    const payload = next.map((r, idx) => ({
      url: r.url,
      slug: r.slug,
      // Empty means "keep the stored token" (the field never round-trips one).
      token: repoTokens.value[idx] || '',
    }))
    await setServerValue('skills.repos', payload)
    newRepoUrl.value = ''
    newRepoToken.value = ''
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

async function addRepo() {
  const url = newRepoUrl.value.trim()
  if (!url) return
  // A new repo's token is taken from the add-row field; per-row tokens are
  // keyed by index and would not line up with the appended entry.
  const next = [...repos.value, { url, slug: '', has_token: newRepoToken.value !== '' }]
  const tokens = { ...repoTokens.value, [next.length - 1]: newRepoToken.value }
  repoTokens.value = tokens
  await saveRepos(next)
}

async function removeRepo(idx: number) {
  const next = repos.value.filter((_, i) => i !== idx)
  await saveRepos(next)
}

async function refresh() {
  refreshing.value = true
  error.value = ''
  try {
    const res = await fetch('/api/skills/refresh', { method: 'POST' })
    if (!res.ok) {
      error.value = `HTTP ${res.status}`
      return
    }
    const data = await res.json()
    const errs = data?.errors && typeof data.errors === 'object' ? Object.values(data.errors) : []
    if (errs.length) {
      error.value = errs.map(String).join('; ')
    }
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    refreshing.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.skills-setting {
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}
.skills-desc {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.skills-block {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.skills-block-title {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
}
.skills-repo {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-5) var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}
.skills-repo-head {
  display: flex;
  align-items: center;
  gap: var(--space-5);
}
.skills-repo-url {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: var(--font-size-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.skills-repo-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  padding: var(--space-1) var(--space-4);
  border-radius: var(--radius-full);
  color: var(--color-success);
  background: color-mix(in srgb, var(--color-success) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-success) 35%, transparent);
}
.skills-repo-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--color-red);
  font-size: var(--font-size-sm);
}
.skills-repo-add {
  display: flex;
  gap: var(--space-4);
  flex-wrap: wrap;
  align-items: center;
}
/* One configured local skill directory: path field + remove. */
.skills-dir,
.skills-dir-add {
  display: flex;
  gap: var(--space-4);
  align-items: center;
  flex-wrap: wrap;
}
.skills-dirs-desc {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
}
.skills-input {
  flex: 1;
  min-width: 150px;
  box-sizing: border-box;
  height: 30px;
  padding: 0 var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
}
.skills-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 2px var(--focus-ring);
}
.skills-sync {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  flex-wrap: wrap;
}
.skills-sync-meta {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
.skills-spin {
  --li-color: currentColor;
}
.skills-item {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-4) 0;
  border-top: 1px solid var(--border-color);
}
.skills-item-head {
  display: flex;
  align-items: center;
  gap: var(--space-5);
}
.skills-item-name {
  font-family: var(--font-mono);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
}
.skills-item-source {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  padding: var(--space-1) var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
}
.skills-item-warn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  padding: var(--space-1) var(--space-3);
  border-radius: var(--radius-full);
  color: var(--color-yellow);
  background: color-mix(in srgb, var(--color-yellow) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-yellow) 35%, transparent);
}
.skills-item-desc {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  line-height: var(--line-height-normal);
}
.skills-item-path {
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.skills-empty {
  color: var(--text-muted);
  font-size: var(--font-size-md);
}
.skills-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  color: var(--color-red);
  font-size: var(--font-size-md);
}
.sbtn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  height: 30px;
  padding: 0 var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
}
.sbtn:hover:not(:disabled) {
  background: var(--bg-tertiary);
}
.sbtn:disabled {
  opacity: var(--opacity-disabled);
  cursor: default;
}
.sbtn-primary {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: var(--text-on-accent);
}
.sbtn-primary:hover:not(:disabled) {
  background: var(--accent-hover);
}
</style>
