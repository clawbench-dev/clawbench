<template>
  <div v-if="parts.length > 0" class="dir-breadcrumb" data-horizontal-scroll="true">
    <!-- Filesystem root, shown ONLY while browsing project-external. The Home
         crumb below always means "project root", so an external browse needs a
         separate way back up to "/" — otherwise the only exit from /var/log
         would be the project root and the filesystem could not be walked. -->
    <template v-if="showRootCrumb">
      <span
        class="crumb crumb-fs-root"
        :draggable="isWideScreen"
        :title="fsRootTitle"
        @dragstart="onCrumbDragStart(fsRootPath, 'FSRoot', $event)"
        @dragend="cleanupDragGhost()"
        @click="$emit('navigate', fsRootPath)"
      >
        <HardDrive :size="14" />
      </span>
      <span class="crumb-sep">/</span>
    </template>
    <span
      class="crumb crumb-home"
      :draggable="isWideScreen"
      :title="homeTitle"
      @dragstart="onCrumbDragStart(homeDragPath, 'Home', $event)"
      @dragend="cleanupDragGhost()"
      @click="$emit('navigate', homePath)"
      :class="{ external: isExternalBrowse }"
    >
      <Home :size="14" />
    </span>
    <template v-for="(part, i) in parts" :key="i">
      <span class="crumb-sep">/</span>
      <span
        class="crumb"
        :class="{ current: i === parts.length - 1 }"
        :draggable="isWideScreen"
        @dragstart="onCrumbDragStart(crumbPath(parts.slice(0, i + 1)), part, $event)"
        @dragend="cleanupDragGhost()"
        @click="i < parts.length - 1 && $emit('navigate', crumbPath(parts.slice(0, i + 1)))"
      >{{ part }}</span>
    </template>
    <span class="crumb-sep" />
    <button class="crumb-copy-btn" :class="{ copied }" :title="t('jump.copyPath')" @click.stop="copyFullPath">
      <Copy :size="13" />
    </button>
  </div>
</template>

<script setup>
import { computed, inject, ref } from 'vue'
import { Home, Copy, HardDrive } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { splitPath, normalizeSlashes, isAbsolutePath } from '@/utils/path.ts'
import { copyText } from '@/utils/clipboard.ts'
import { store } from '@/stores/app.ts'
import { setAttachDragData, buildAttachDragImage, cleanupDragGhost } from '@/utils/attachDrag.ts'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout.ts'

const props = defineProps({
  path: { type: String, default: '' },
  /**
   * Whether "" means the PROJECT root (the file manager) or the filesystem's
   * top level (ProjectDialog's picker, which has no project concept). Decides
   * what the Home crumb targets and whether a filesystem-root crumb is needed.
   */
  projectScoped: { type: Boolean, default: true },
})
defineEmits(['navigate'])
const { t } = useI18n()
const toast = inject('toast', null)
const copied = ref(false)
const { isWideScreen } = useWideScreenLayout()

/** Browsing a directory outside the project (only meaningful when scoped). */
const isExternalBrowse = computed(() => props.projectScoped && isAbsolutePath(props.path))

/** Filesystem root of the browsed absolute path: "/" on POSIX, "C:/" on Windows. */
const fsRootPath = computed(() => {
  const norm = normalizeSlashes(props.path)
  const drive = norm.match(/^([A-Za-z]:)\//)
  return drive ? `${drive[1]}/` : '/'
})

/**
 * The Home crumb ALWAYS means "project root" ("" — the backend resolves it),
 * never the browsed directory's own root. That gives the user one predictable
 * exit no matter how deep an external tree they wandered into.
 */
const homePath = computed(() => '')

/**
 * An external browse also needs a crumb for the FILESYSTEM root, because Home
 * has been repurposed as "project root" — without it there would be no way back
 * up to "/" (Back alone walks up and eventually strands the user there).
 */
const showRootCrumb = computed(() => isExternalBrowse.value)

/** Tooltips make the two adjacent roots unambiguous in an external browse. */
const homeTitle = computed(() => (isExternalBrowse.value ? t('file.nav.backToProject') : t('file.nav.projectRoot')))
const fsRootTitle = computed(() => t('file.nav.fsRoot'))

function onCrumbDragStart(path, name, e) {
  if (!isWideScreen.value) return
  setAttachDragData(e.dataTransfer, path, true)
  e.dataTransfer.effectAllowed = 'move'
  const ghost = buildAttachDragImage(name || '/', true)
  e.dataTransfer.setDragImage(ghost, 14, 16)
}

function copyFullPath() {
  const value = props.path
  if (!value) return
  // props.path is either project-relative (FileManager) or already absolute
  // (ProjectDialog browsing arbitrary dirs). Only combine with the project
  // root for relative paths; copy absolute paths as-is (separators normalized).
  const normValue = normalizeSlashes(value)
  const root = normalizeSlashes(store.state.projectRoot || '')
  const absPath = isAbsolutePath(value)
    ? normValue
    : root ? root.replace(/\/+$/, '') + '/' + normValue.replace(/^\/+/, '') : normValue.replace(/^\/+/, '')
  const doCopy = () => {
    copied.value = true
    setTimeout(() => { copied.value = false }, 800)
    if (toast) toast.show(t('common.copied'), { icon: '📋', type: 'success', duration: 1500 })
  }
  copyText(absPath, doCopy, doCopy)
}

// Reconstruct a path from breadcrumb segments,
// using the appropriate separator for the platform.
function reconstructPath(segments) {
  if (segments.length === 0) return ''
  // Windows: first segment like "C:\" already includes the root separator
  if (/^[A-Za-z]:\\$/.test(segments[0])) {
    return segments[0] + segments.slice(1).join('\\')
  }
  // Join with "/" (relative path, no leading slash)
  return segments.join('/')
}

const parts = computed(() => {
  if (!props.path || props.path === '.') return []
  const segments = splitPath(props.path).filter(p => p !== '')
  // On Windows, merge bare drive letter "C:" into "C:\"
  // so it displays as a single root crumb, not a broken segment
  if (segments.length > 0 && /^[A-Za-z]:$/.test(segments[0])) {
    segments[0] = segments[0] + '\\'
  }
  return segments
})

/**
 * Reconstruct a path from breadcrumb segments, preserving the FORM of the
 * input path. `props.path` is either project-relative (the file manager inside
 * the project) or absolute (ProjectDialog's directory picker, and the file
 * manager browsing a project-external directory) — a crumb must stay in
 * whichever form the input used, or the jump resolves against the wrong root.
 *
 * Both consumers accept either form: ProjectDialog's onBreadcrumbNavigate
 * re-normalizes both, and the file manager passes the value straight to
 * navigateToDir (which routes absolute paths through /api/projects).
 */
function crumbPath(segments) {
  if (segments.length === 0) return ''
  const joined = reconstructPath(segments)
  // Windows: reconstructPath already restored the "C:\" root, so `joined` is
  // absolute as-is (prepending "/" would produce "/C:\Users").
  if (/^[A-Za-z]:[\\/]/.test(joined)) return joined
  return isAbsolutePath(props.path) ? '/' + joined.replace(/^\/+/, '') : joined
}

/**
 * Path carried by the Home crumb's drag payload. The click target is "" (the
 * project root, a backend-resolved notion), but the attach flow reads a real
 * filesystem path — so it gets the project root directory itself.
 */
const homeDragPath = computed(() => normalizeSlashes(store.state.projectRoot || '') || '/')
</script>

<style scoped>
.dir-breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  overflow-x: auto;
  font-size: var(--font-size-md);
  color: var(--text-muted, #999);
  scrollbar-width: none;
}
.dir-breadcrumb::-webkit-scrollbar {
  display: none;
}

.crumb {
  padding:3px var(--space-3);
  border-radius: var(--radius-xs);
  cursor: pointer;
  white-space: nowrap;
  transition: background var(--duration-base);
  display: inline-flex;
  align-items: center;
}

@media (hover: hover) {
  .crumb:hover {
    background: var(--bg-secondary, #e0e0e0);
    color: var(--accent-color, #4a90d9);
  }
}

.crumb.current {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
  cursor: default;
}

@media (hover: hover) {
  .crumb.current:hover {
    background: none;
    color: var(--text-primary, #1a1a1a);
  }
}

/* ── Project-external browsing ──────────────────────────────────────────────
   Orange is the established "outside the project" colour (see
   annotation-buttons.css / code-viewer.css). Here it marks the two things that
   change meaning once you leave the project: the Home crumb no longer means
   "the browsed tree's root" but "back to the project", and the extra crumb on
   its left is the filesystem root. Colouring them apart keeps the two adjacent
   roots from reading as the same control. */
.crumb.crumb-home.external {
  color: var(--color-orange, #d9730d);
}

@media (hover: hover) {
  .crumb.crumb-home.external:hover {
    color: var(--color-orange, #d9730d);
    background: color-mix(in srgb, var(--color-orange, #d9730d) 15%, transparent);
  }
}

.crumb.crumb-fs-root {
  color: var(--text-muted, #999);
}

@media (hover: hover) {
  .crumb.crumb-fs-root:hover {
    background: var(--bg-secondary, #e0e0e0);
    color: var(--text-primary, #1a1a1a);
  }
}

.crumb-sep {
  color: var(--text-muted, #999);
  font-size: var(--font-size-md);
}

.crumb-copy-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding:3px var(--space-3);
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  transition: background var(--duration-base), color var(--duration-base);
}
@media (hover: hover) {
  .crumb-copy-btn:hover {
    background: var(--bg-secondary, #e0e0e0);
    color: var(--accent-color, #4a90d9);
  }
}
.crumb-copy-btn.copied {
  color: #22c55e;
}
</style>
