<template>
  <div v-if="parts.length > 0" class="dir-breadcrumb">
    <span
      class="crumb crumb-home"
      :draggable="isWideScreen"
      @dragstart="onCrumbDragStart('/', 'Home', $event)"
      @dragend="cleanupDragGhost()"
      @click="$emit('navigate', '')"
    >
      <Home :size="14" />
    </span>
    <template v-for="(part, i) in parts" :key="i">
      <span class="crumb-sep">/</span>
      <span
        class="crumb"
        :class="{ current: i === parts.length - 1 }"
        :draggable="isWideScreen"
        @dragstart="onCrumbDragStart(reconstructPath(parts.slice(0, i + 1)), part, $event)"
        @dragend="cleanupDragGhost()"
        @click="i < parts.length - 1 && $emit('navigate', reconstructPath(parts.slice(0, i + 1)))"
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
</script>

<style scoped>
.dir-breadcrumb {
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  font-size: 13px;
  color: var(--text-muted, #999);
  scrollbar-width: none;
}
.dir-breadcrumb::-webkit-scrollbar {
  display: none;
}

.crumb {
  padding: 3px 6px;
  border-radius: 4px;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.15s;
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
  font-weight: 600;
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
  font-size: 13px;
}

.crumb-copy-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 3px 6px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.15s, color 0.15s;
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
