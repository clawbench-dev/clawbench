<template>
  <!-- Remote git skill sources. A separate card from the local directories:
       these are cloned and periodically pulled, so they carry sync state and
       per-repo errors that local paths do not. -->
  <div class="skills-repos-setting">
    <div v-for="(repo, idx) in repos" :key="repo.url + idx" class="skills-repo">
      <div class="skills-repo-head">
        <span class="skills-repo-url">{{ repo.url }}</span>
        <span v-if="repo.has_token" class="skills-repo-badge">
          <KeyRound :size="12" />
          {{ t('settings.items.skillsRepoTokenSet') }}
        </span>
        <button class="fbtn" @click="removeRepo(idx)">
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
      <button class="fbtn fbtn-primary" :disabled="!newRepoUrl.trim() || reposSaving" @click="addRepo">
        {{ t('settings.items.skillsRepoAdd') }}
      </button>
    </div>

    <!-- Manual sync + status. The button belongs to THIS card: it only pulls
         the git remotes (local directories need no syncing). -->
    <div class="skills-sync">
      <button class="fbtn" :disabled="refreshing" @click="refresh">
        <LoadingIndicator v-if="refreshing" size="sm" inline class="skills-spin" />
        <RefreshCw v-else :size="13" />
        {{ refreshing ? t('settings.items.skillsRefreshing') : t('settings.items.skillsRefresh') }}
      </button>
      <span v-if="lastSyncText" class="skills-sync-meta">{{ lastSyncText }}</span>
    </div>

    <div v-if="reposError" class="skills-error">
      <AlertCircle :size="13" />
      <span>{{ reposError }}</span>
    </div>
    <div v-if="syncError" class="skills-error">
      <AlertCircle :size="13" />
      <span>{{ syncError }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, KeyRound, RefreshCw, Trash2 } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import '@/assets/modal-footer-btn.css'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import {
  useSkillsState,
  loadSkills,
  refreshSkills,
  type RepoRow,
} from '@/composables/useSkillsState'

const { t } = useI18n()
const { getServerValueWithDefault, setServerValue } = useSettingsConfig()
const { repos, lastSyncAt, refreshing, syncError, reposError, reposSaving } = useSkillsState()

const repoTokens = ref<Record<number, string>>({})
const newRepoUrl = ref('')
const newRepoToken = ref('')

const lastSyncText = computed(() => {
  if (!lastSyncAt.value) return ''
  const d = new Date(lastSyncAt.value * 1000)
  return t('settings.items.skillsLastSync', { time: d.toLocaleString() })
})

function load() {
  repoTokens.value = {}
  return loadSkills(getServerValueWithDefault('skills.enabled') !== false)
}

/**
 * Persist the whole repo list. The endpoint replaces the array wholesale, so
 * every mutation sends the full list — including any tokens the user typed in
 * the per-row fields.
 *
 * Returns true when the server accepted the list. Callers that add a repo need
 * this to decide whether to kick a sync (a failed save must not).
 */
async function saveRepos(next: RepoRow[]): Promise<boolean> {
  reposSaving.value = true
  reposError.value = ''
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
    return true
  } catch (err) {
    reposError.value = err instanceof Error ? err.message : String(err)
    return false
  } finally {
    reposSaving.value = false
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
  const saved = await saveRepos(next)
  if (!saved) return
  // A newly added repo has no local checkout yet: the config PATCH only
  // persists it (the server deliberately does not clone during a PATCH, which
  // would put network IO behind the config write lock). Sync now so the new
  // remote is cloned and scanned immediately — same path as the manual button,
  // so the user gets the spinner and any per-repo error inline.
  await refresh()
}

async function removeRepo(idx: number) {
  await saveRepos(repos.value.filter((_, i) => i !== idx))
}

async function refresh() {
  await refreshSkills()
  // Reload even on a per-repo failure: a partial sync still updated whatever
  // remotes did succeed, and the discovered list must show that.
  await load()
}

onMounted(load)
</script>

<style scoped>
.skills-repos-setting {
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
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
/* Same field geometry as the directory card (see SkillsDirsSetting). */
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
/* Keep the inline spinner the button's own colour. */
.skills-spin {
  --li-color: currentColor;
}
.skills-repo-error,
.skills-error {
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
</style>
