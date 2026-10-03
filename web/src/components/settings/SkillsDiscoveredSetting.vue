<template>
  <!-- Discovered skills, in its own card so the configuration above (switch,
       directories, git sources) is not visually glued to a read-only listing. -->
  <div class="skills-discovered">
    <div class="skills-discovered__head">
      <span class="skills-discovered__count">
        {{ t('settings.items.skillsDiscovered', { count: skills.length }) }}
      </span>
    </div>

    <div v-if="!skills.length" class="skills-discovered__empty">
      {{ t('settings.items.skillsEmpty') }}
    </div>

    <div v-for="s in skills" :key="s.path" class="skills-item">
      <div class="skills-item-head">
        <span class="skills-item-name">{{ s.name }}</span>
        <span class="skills-item-source">{{ sourceLabel(s) }}</span>
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
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, FolderOpen } from 'lucide-vue-next'
import {
  useSkillsState,
  loadSkills,
  skillSourceLabel,
  type SkillRow,
} from '@/composables/useSkillsState'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { revealInFileManager, verifyFilePaths } from '@/composables/useFilePathAnnotation'

const { t } = useI18n()
const { getServerValueWithDefault } = useSettingsConfig()
const { skills } = useSkillsState()

function sourceLabel(s: SkillRow): string {
  return skillSourceLabel(s, t)
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
  justify-content: space-between;
  padding: var(--space-4) var(--space-7);
  border-bottom: 1px solid var(--border-color);
}
.skills-discovered__count {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
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
