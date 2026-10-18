import { ref, computed } from 'vue'
import { appLog } from '@/utils/appLog'
import { REF_TOKEN_END_RE } from '@/utils/completionMatch.ts'

const TAG = 'SkillsState'

/** One discovered skill, as reported by GET /api/skills. */
export interface SkillRow {
  name: string
  description: string
  path: string
  source_kind: 'own' | 'project' | 'user' | 'git' | 'other'
  source_label: string
  agent_id?: string
  /**
   * AI backend IDs whose native directory holds this skill (e.g. ['claude']).
   * `agent_id` is an AGENT id (a custom agent's id may differ from its backend),
   * so the UI renders these instead — one icon+name chip per backend. Empty for
   * non-native sources (user dir, git repo, shared dir), which keep their label.
   */
  backends?: string[]
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
// Mirrors the server default (skills.enabled defaults to false — auto-injection
// is opt-in). Overwritten by loadSkills() with the server's actual value; this
// only avoids showing "on" for the instant before the first load resolves.
const enabled = ref(false)
const dirs = ref<string[]>([])
const repos = ref<RepoRow[]>([])
const skills = ref<SkillRow[]>([])
const lastSyncAt = ref(0)
const refreshing = ref(false)
// Pure-local rescan (no git IO) — separate from `refreshing` so the two buttons
// in the two cards spin independently.
const rescanning = ref(false)
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
    case 'project':
      return t('settings.items.skillsSourceProject')
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

/**
 * Re-read every local source WITHOUT any network IO, then reload the list.
 * Backed by POST /api/skills/rescan; use `refreshSkills` to also pull git
 * remotes. Returns normally on failure — the reload keeps the last good list.
 */
export async function rescanSkills(enabledFallback: boolean): Promise<void> {
  rescanning.value = true
  try {
    await fetch('/api/skills/rescan', { method: 'POST' })
  } catch (err) {
    appLog.w(TAG, 'rescan failed', err)
  } finally {
    rescanning.value = false
  }
  await loadSkills(enabledFallback)
}

/**
 * Load the discovered skills if they have not been loaded yet. This is the
 * chat input's lazy path for the `#` skill menu: the list is fetched the first
 * time the trigger appears, then reused.
 *
 * It deliberately does NOT touch `enabled`. The master switch governs AUTOMATIC
 * injection (the system-prompt table); an explicit `#name` reference works
 * either way, so the menu must list skills even when the switch is off.
 */
export async function ensureSkillsLoaded(): Promise<void> {
  if (loaded.value) return
  try {
    const res = await fetch('/api/skills')
    if (!res.ok) return
    const data = await res.json()
    skills.value = Array.isArray(data?.skills) ? data.skills : []
    // Mark loaded even on an empty list: otherwise every keystroke after "#"
    // would re-fetch. A later rescan/refresh still repopulates it.
    loaded.value = true
  } catch (err) {
    appLog.w(TAG, 'ensure load failed', err)
  }
}

/**
 * Source priority for the `#` menu's dedupe, mirroring the backend's
 * SourceKind ordering (internal/skill/types.go): a lower index wins.
 *
 * It must match the server's `sourceLess`, or the row the menu shows could be
 * a different skill than the one the backend resolves for the same name.
 * Exported so a parity test can pin it against the Go SourceKind values — a
 * comment cannot catch a reorder.
 *
 * Only the CROSS-kind order is mirrored. For two entries of the SAME kind, the
 * backend breaks the tie by directory path while this keeps the first seen;
 * since the menu only inserts the name (the server re-resolves it on send), the
 * difference is limited to which description is displayed.
 */
export const SOURCE_KIND_PRIORITY: SkillRow['source_kind'][] = [
  'own',
  'project',
  'user',
  'git',
  'other',
]

function sourceRank(kind: SkillRow['source_kind']): number {
  const i = SOURCE_KIND_PRIORITY.indexOf(kind)
  return i < 0 ? SOURCE_KIND_PRIORITY.length : i
}

/**
 * The rows the `#` menu offers: every discovered skill, deduped by canonical
 * name (case-insensitive, leading "/" stripped — the server's identity rule),
 * keeping the highest-priority source.
 *
 * Skills whose frontmatter name disagrees with their directory are dropped, the
 * same way the backend refuses to advertise them: an agent resolves a skill by
 * directory, so such an entry would do nothing when picked.
 *
 * A name containing whitespace, "#" or a reference terminator is also dropped:
 * the reference grammar is `#<name>` with the name running to the next
 * whitespace or punctuation (see `skillRefPunct` in
 * internal/handler/skill_command.go), so such a name cannot be expressed —
 * offering it would insert a reference that resolves to nothing.
 */
export function skillMenuRows(): SkillRow[] {
  const best = new Map<string, SkillRow>()
  for (const s of skills.value) {
    if (s.name_mismatch) continue
    if (s.name.includes('#') || REF_TOKEN_END_RE.test(s.name)) continue
    const key = s.name.trim().replace(/^\//, '').toLowerCase()
    if (!key) continue
    const cur = best.get(key)
    if (!cur || sourceRank(s.source_kind) < sourceRank(cur.source_kind)) {
      best.set(key, s)
    }
  }
  return [...best.values()].sort((a, b) => a.name.localeCompare(b.name))
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
    rescanning,
    syncError,
    dirsError,
    reposError,
    dirsSaving,
    reposSaving,
    loaded,
    skillCount: computed(() => skills.value.length),
  }
}
