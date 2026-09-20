<template>
  <div v-if="parts.length > 0" class="dir-breadcrumb" data-horizontal-scroll="true">
    <span
      class="crumb crumb-home"
      :draggable="isWideScreen"
      @dragstart="onCrumbDragStart(homeDragPath, 'Home', $event)"
      @dragend="cleanupDragGhost()"
      @click="$emit('navigate', homePath)"
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
import { Home, Copy } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { splitPath, normalizeSlashes, isAbsolutePath } from '@/utils/path.ts'
import { copyText } from '@/utils/clipboard.ts'
import { store } from '@/stores/app.ts'
import { setAttachDragData, buildAttachDragImage, cleanupDragGhost } from '@/utils/attachDrag.ts'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout.ts'

const props = defineProps({
  path: { type: String, default: '' },
})
defineEmits(['navigate'])
const { t } = useI18n()
const toast = inject('toast', null)
const copied = ref(false)
const { isWideScreen } = useWideScreenLayout()

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
 * Target of the home crumb. For a project-relative browse that is the project
 * root ("" — /api/dir resolves it); for an absolute browse it must stay
 * absolute, or the jump would land back inside the project. The root of an
 * absolute path is "/" on POSIX and the drive root ("C:/") on Windows.
 */
const homePath = computed(() => {
  if (!isAbsolutePath(props.path)) return ''
  const norm = normalizeSlashes(props.path)
  const drive = norm.match(/^([A-Za-z]:)\//)
  return drive ? `${drive[1]}/` : '/'
})

/**
 * Path carried by the home crumb's drag payload. Unlike the click target, the
 * project-relative case keeps the legacy "/" — the attach flow reads a
 * filesystem path here, and an empty string would attach nothing.
 */
const homeDragPath = computed(() => homePath.value || '/')
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
