<template>
  <!-- Discovered skills, in its own card so the configuration above (switch,
       directories, git sources) is not visually glued to a read-only listing. -->
  <div class="skills-discovered">
    <div class="skills-discovered__head">
      <span class="skills-discovered__count">
        {{ t('settings.items.skillsDiscovered', { count: filteredSkills.length }) }}
      </span>
      <!-- Wrapper is required: a fallthrough class on SearchInput merges onto
           its root (.search-pill), so sizing it directly needs this element. -->
      <div class="skills-discovered__search">
        <SearchInput
          v-model="searchQuery"
          :placeholder="t('settings.items.skillsSearchPlaceholder')"
        />
      </div>
      <!-- Pure-local rescan (no git IO). The git card owns "sync now"; this
           card owns "re-read the filesystem" — they are different actions. -->
      <button class="fbtn skills-discovered__rescan" :disabled="rescanning" @click="rescan">
        <LoadingIndicator v-if="rescanning" size="sm" inline class="skills-spin" />
        <RefreshCw v-else :size="13" />
        {{ rescanning ? t('settings.items.skillsRescanning') : t('settings.items.skillsRescan') }}
      </button>
    </div>

    <div v-if="!skills.length" class="skills-discovered__empty">
      {{ t('settings.items.skillsEmpty') }}
    </div>
    <div v-else-if="!filteredSkills.length" class="skills-discovered__empty">
      {{ t('settings.items.skillsNoMatch', { query: searchQuery }) }}
    </div>

    <div v-for="s in filteredSkills" :key="s.path" class="skills-item">
      <div class="skills-item-head">
        <span class="skills-item-name">{{ s.name }}</span>
        <!-- Native skills name their AI backend(s) on their own line below
             (icon + name, one chip each); every other source keeps its text
             label here. -->
        <span v-if="!s.backends?.length" class="skills-item-source">{{ sourceLabel(s) }}</span>
        <!-- The spec requires the frontmatter name to match the directory name;
             an agent resolves a skill by directory, so a mismatch means this
             skill cannot be offered as a slash command. -->
        <span
          v-if="s.name_mismatch"
          class="skills-item-warn"
          :title="t('settings.items.skillsNameMismatchDesc')"
        >
          <AlertTriangle :size="12" />
          {{ t('settings.items.skillsNameMismatch') }}
        </span>
      </div>
      <!-- One chip per backend, wrapping when the line runs out of width. No
           "agent"/"backend" prefix: the icon already says which tool it is. -->
      <div v-if="s.backends?.length" class="skills-item-backends">
        <span v-for="b in s.backends" :key="b" class="skills-backend-chip">
          <AgentIcon :backend="b" size="sm" />
          {{ getBackendDisplayName(b) }}
        </span>
      </div>
      <div class="skills-item-desc">{{ s.description }}</div>
      <!-- Jump affordance, same contract as chat path annotations: the path
           verifies asynchronously, is clickable only when it exists, and the
           jump records a return origin so Back comes back to this page. -->
      <button
        class="skills-item-path"
        type="button"
        :disabled="!exists(s.path)"
        :title="exists(s.path) ? t('settings.items.skillsOpenPath') : t('settings.items.skillsPathMissing')"
        @click="openPath(s.path)"
      >
        <span class="skills-item-path__text">{{ s.path }}</span>
        <FolderOpen :size="12" class="skills-item-path__icon" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, FolderOpen, RefreshCw } from 'lucide-vue-next'
import SearchInput from '@/components/common/SearchInput.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import '@/assets/modal-footer-btn.css'
import {
  useSkillsState,
  loadSkills,
  rescanSkills,
  skillSourceLabel,
  type SkillRow,
} from '@/composables/useSkillsState'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { getBackendDisplayName } from '@/utils/backendNames'
import { revealInFileManager, verifyFilePaths } from '@/composables/useFilePathAnnotation'

const { t } = useI18n()
const { getServerValueWithDefault } = useSettingsConfig()
const { skills, rescanning } = useSkillsState()

const searchQuery = ref('')

// Case-insensitive substring match on the skill name — the same "narrow the
// list as you type" behavior as the other search boxes in the app.
const filteredSkills = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return skills.value
  return skills.value.filter((s) => s.name.toLowerCase().includes(q))
})

function sourceLabel(s: SkillRow): string {
  return skillSourceLabel(s, t)
}

async function rescan() {
  await rescanSkills(getServerValueWithDefault('skills.enabled') !== false)
}

// ── Path jump ──
//
// The click target is only enabled once the path is confirmed to exist, so a
// dead entry can never look clickable (the chat annotation contract). Paths are
// absolute and usually OUTSIDE the project root (~/.agents/skills, ~/.claude/…);
// the server stats absolute paths without project scoping, and the file manager
// browses them as ordinary directories, so this works for both cases.
const pathStates = ref<Record<string, 'file' | 'dir' | 'none'>>({})

function exists(p: string): boolean {
  return pathStates.value[p] === 'file' || pathStates.value[p] === 'dir'
}

onMounted(async () => {
  await loadSkills(getServerValueWithDefault('skills.enabled') !== false)
  await verifyPaths()
})

// The configuration card can add a source (a local dir is rescanned by the
// server on PATCH; a new git repo is synced right after being added), which
// reloads the shared skills list. Without re-verifying, the new entries' path
// buttons stay disabled — they were never verified — until the page reopens.
// Re-run whenever the path set actually changes, not on every list reload.
watch(
  () => skills.value.map((s) => s.path).join('\n'),
  () => { void verifyPaths() },
)

async function verifyPaths() {
  const paths = skills.value.map((s) => s.path)
  if (!paths.length) return

  // verifyFilePaths needs a container to mutate; give it an off-DOM node so we
  // can reuse its batching, caching and fallback logic without annotating the
  // live DOM (this list renders from reactive state, not from annotated HTML).
  const scratch = document.createElement('div')
  for (const p of paths) {
    const el = document.createElement('span')
    el.className = 'chat-file-path'
    el.setAttribute('data-file-path', p)
    scratch.appendChild(el)
  }
  await verifyFilePaths(paths, scratch)

  const next: Record<string, 'file' | 'dir' | 'none'> = {}
  for (const el of scratch.querySelectorAll<HTMLElement>('[data-file-path]')) {
    const p = el.getAttribute('data-file-path')
    const type = el.getAttribute('data-path-type')
    if (p && (type === 'file' || type === 'dir' || type === 'none')) next[p] = type
  }
  pathStates.value = next
}

function openPath(p: string) {
  if (!exists(p)) return
  // revealInFileManager (not navToFileInManager) because it records a return
  // origin through the navigation coordinator, so Back returns here.
  void revealInFileManager(p, 'settings')
}
</script>

<style scoped>
.skills-discovered {
  display: flex;
  flex-direction: column;
}
.skills-discovered__head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-7);
  border-bottom: 1px solid var(--border-color);
}
.skills-discovered__count {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
  white-space: nowrap;
}
/* The search box takes the remaining width; the rescan button stays compact. */
.skills-discovered__search {
  flex: 1;
  min-width: 0;
}
.skills-discovered__rescan {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
}
/* Keep the inline spinner the button's own colour. */
.skills-spin {
  --li-color: currentColor;
}
.skills-discovered__empty {
  padding: var(--space-6) var(--space-7);
  color: var(--text-muted);
  font-size: var(--font-size-md);
}
.skills-item {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-5) var(--space-7);
  position: relative;
}
.skills-item + .skills-item::before {
  content: '';
  position: absolute;
  top: 0;
  left: 16px;
  right: 0;
  height: 1px;
  background: var(--border-color);
}
.skills-item-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}
.skills-item-name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}
.skills-item-source {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  padding: 0 var(--space-3);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
}
/* Backend chips sit on their own line and wrap when the row is narrow. Each
   chip is icon + name with no prefix — the icon identifies the tool. */
.skills-item-backends {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}
.skills-backend-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  padding: 1px var(--space-4);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
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
/* The path is a button, not a chip: it carries the jump affordance. Its look
   matches the chat path annotation (mono, faint fill) so the two read alike. */
.skills-item-path {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  align-self: flex-start;
  max-width: 100%;
  padding: 1px var(--space-3);
  border: none;
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--text-muted) 8%, transparent);
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  text-align: left;
  cursor: pointer;
  transition: background var(--duration-base), color var(--duration-base);
}
.skills-item-path__text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.skills-item-path:not(:disabled):hover {
  background: color-mix(in srgb, var(--text-muted) 18%, transparent);
  color: var(--text-primary);
}
/* A path that failed verification keeps its text (still readable/copyable) but
   must not look clickable — same rule as the chat inert path chip. */
.skills-item-path:disabled {
  cursor: not-allowed;
  opacity: var(--opacity-disabled);
}
.skills-item-path__icon {
  flex-shrink: 0;
}
</style>
