import { ref, computed } from 'vue'
import { appLog } from '@/utils/appLog'

const TAG = 'SkillsState'

/** One discovered skill, as reported by GET /api/skills. */
export interface SkillRow {
  name: string
  description: string
  path: string
  source_kind: 'own' | 'user' | 'git' | 'other'
  source_label: string
  agent_id?: string
  /** Frontmatter name disagrees with the directory name (spec violation). */
  name_mismatch?: boolean
  /**
   * The skill lives in the cross-tool shared directory (.agents/skills). Such a
   * skill is "generic": it belongs to no single agent, and the server reports it
   * as own/other depending on who is asking, so the UI must not name an agent.
   * The server also omits `agent_id` for these.
   */
  shared?: boolean
}

/** One configured git skill source. */
export interface RepoRow {
  url: string
  slug: string
  has_token: boolean
  last_error?: string
}

// ── Module-level singleton ──
//
// The skills page renders the settings card and the discovered-skills card as
// two sibling components (they are deliberately separate cards), but they read
// the SAME GET /api/skills response. Keeping the fetch and the refs here means:
//   - one request per refresh instead of one per card,
//   - a sync triggered in the settings card updates the list card too (a
//     per-component fetch would leave the list stale until the page reopened).
const enabled = ref(true)
const dirs = ref<string[]>([])
const repos = ref<RepoRow[]>([])
const skills = ref<SkillRow[]>([])
const lastSyncAt = ref(0)
const refreshing = ref(false)
// Sync (the button lives in the git-repos card, so its error belongs there).
const syncError = ref('')
const loaded = ref(false)

// Per-card error state. These used to be one shared `error` ref, but the cards
// persist different keys (skills.dirs vs skills.repos) — a validation failure in
// one must not paint an error inside the other.
const dirsError = ref('')
const reposError = ref('')
const dirsSaving = ref(false)
const reposSaving = ref(false)

/**
 * Human-readable origin for a discovered skill.
 *
 * A shared (.agents/skills) skill is labelled "generic" rather than by agent:
 * the directory is read by several backends, so naming whichever one happens to
 * be installed would be misleading.
 */
export function skillSourceLabel(
  s: Pick<SkillRow, 'shared' | 'source_kind' | 'source_label' | 'agent_id'>,
  t: (key: string, named?: Record<string, unknown>) => string,
): string {
  if (s.shared) return t('settings.items.skillsSourceShared')
  switch (s.source_kind) {
    case 'own':
      return t('settings.items.skillsSourceOwn')
    case 'user':
      return t('settings.items.skillsSourceUser')
    case 'git':
      return t('settings.items.skillsSourceGit', { name: s.source_label })
    case 'other':
      // No agent id (a shared source, or a directory no agent declared) — fall
      // back to the server's label rather than rendering an empty "Agent ".
      return t('settings.items.skillsSourceOther', { agent: s.agent_id || s.source_label })
    default:
      return s.source_label
  }
}

/**
 * Load the skills state. `enabled` comes from the shared settings store so a
 * PATCH elsewhere is reflected here; the directories, repos and discovered
 * skills come from GET /api/skills, which reports RESOLVED directories
 * (including the default fallback).
 *
 * `enabledFallback` is passed in by the caller because the settings store is a
 * composable bound to the component, not module state.
 */
export async function loadSkills(enabledFallback: boolean): Promise<void> {
  try {
    enabled.value = enabledFallback
    const res = await fetch('/api/skills')
    if (!res.ok) return
    const data = await res.json()
    dirs.value = Array.isArray(data?.dirs) ? data.dirs : []
    repos.value = Array.isArray(data?.repos) ? data.repos : []
    skills.value = Array.isArray(data?.skills) ? data.skills : []
    lastSyncAt.value = Number(data?.last_sync_at) || 0
    loaded.value = true
  } catch (err) {
    appLog.w(TAG, 'load failed', err)
  }
}

/** Force a git sync, then reload. Per-repo failures surface as `last_error`. */
export async function refreshSkills(): Promise<void> {
  refreshing.value = true
  syncError.value = ''
  try {
    const res = await fetch('/api/skills/refresh', { method: 'POST' })
    if (!res.ok) {
      syncError.value = `HTTP ${res.status}`
      return
    }
    const data = await res.json().catch(() => null)
    // A per-repo failure is reported as a slug→message map and is deliberately
    // NOT an HTTP error (the request succeeded; some remotes did not). Surface
    // it so the user can tell a partial sync from a clean one.
    const errs = data?.errors && typeof data.errors === 'object' ? Object.values(data.errors) : []
    if (errs.length > 0) {
      syncError.value = errs.map(String).join('; ')
    }
  } catch (err) {
    syncError.value = err instanceof Error ? err.message : String(err)
  } finally {
    refreshing.value = false
  }
}

/** Shared, read-only view of the skills state for both cards. */
export function useSkillsState() {
  return {
    enabled,
    dirs,
    repos,
    skills,
    lastSyncAt,
    refreshing,
    syncError,
    dirsError,
    reposError,
    dirsSaving,
    reposSaving,
    loaded,
    skillCount: computed(() => skills.value.length),
  }
}
