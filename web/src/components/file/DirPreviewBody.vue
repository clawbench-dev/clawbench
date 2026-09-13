<template>
  <!-- Directory listing body for the docked preview pane. Mirrors the file
       preview card: same card background, same meta toolbar, and a body that
       scrolls under the fixed toolbar. -->
  <div class="dir-preview-body" :class="{ 'is-loading': loading }">
    <!-- Toolbar: same metrics/colors as .code-preview-meta so the two pane
         bodies read as one component. -->
    <div class="dir-preview-meta">
      <div class="dir-preview-meta-info">
        <span class="dir-preview-title">{{ dirName }}</span>
        <span class="dir-preview-count">{{ t('file.dirPreview.count', { n: shown.length }) }}</span>
      </div>
      <div class="dir-preview-actions">
        <button
          type="button"
          class="dir-preview-btn"
          :title="t('file.dirPreview.close')"
          :aria-label="t('file.dirPreview.close')"
          @click="emit('closed')"
        >
          <X :size="12" />
        </button>
      </div>
    </div>

    <!-- Scroll pane: the toolbar above stays put, like the file card's header. -->
    <div class="dir-preview-scroll">
      <div v-if="loading && !entries.length" class="dir-preview-state">
        <LoadingIndicator size="sm" />
      </div>
      <div v-else-if="error" class="dir-preview-state dir-preview-error">
        <AlertTriangle :size="20" />
        <span>{{ t('file.dirPreview.loadFailed') }}</span>
      </div>

      <div v-else-if="!shown.length" class="dir-preview-state">
        <FolderOpen :size="20" />
        <span>{{ t('file.dirPreview.empty') }}</span>
      </div>

      <!-- Multi-column grid: `auto-fill` + a min track width means the browser
           picks the column count from the pane's width, no JS measurement. -->
      <div v-else class="dir-preview-grid" role="list">
        <button
          v-for="entry in shown"
          :key="entry.name"
          type="button"
          role="listitem"
          class="dir-preview-item"
          :class="{ 'is-dir': entry.type === 'dir' }"
          :title="entry.name"
          @click="onEntryClick(entry)"
        >
          <FileIcon
            :path="entry.name"
            :is-dir="entry.type === 'dir'"
            :size="16"
            class="dir-preview-icon"
          />
          <span class="dir-preview-name">{{ entry.name }}</span>
          <span v-if="entry.symlink" class="dir-preview-symlink" :title="entry.broken ? t('file.symlinkBroken') : t('file.symlink')">
            <Link2 :size="11" />
          </span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, FolderOpen, Link2, X } from 'lucide-vue-next'
import FileIcon from '@/components/common/FileIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import type { DirPreviewEntry } from '@/composables/useDirPreview'

const props = defineProps<{
  entries: DirPreviewEntry[]
  loading: boolean
  error: boolean
  /** Predicate from useDirPreview — applies the "show hidden files" toggle. */
  visible: (entry: DirPreviewEntry) => boolean
  /** Display name of the directory being listed (its own base name). */
  dirName: string
}>()

const emit = defineEmits<{
  /** A file was clicked — the caller opens it in the full-screen viewer. */
  (e: 'open-file', name: string): void
  /** A directory was clicked — the caller navigates the main list into it. */
  (e: 'open-dir', name: string): void
  /** The pane's close control was pressed. */
  (e: 'closed'): void
}>()

const { t } = useI18n()

const shown = computed(() => props.entries.filter(e => props.visible(e)))

function onEntryClick(entry: DirPreviewEntry) {
  if (entry.type === 'dir') {
    emit('open-dir', entry.name)
  } else {
    emit('open-file', entry.name)
  }
}
</script>

<style scoped>
.dir-preview-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  /* Same card background as the file preview card. */
  background: var(--bg-primary, #ffffff);
}

/* Scroll pane under the fixed toolbar. */
.dir-preview-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 8px;
}

/* ── Toolbar: mirrors .code-preview-meta (same height, colors, metrics) so the
   two docked pane bodies look like one component. ── */
.dir-preview-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 3px 6px 3px 12px;
  min-height: 28px;
  background: var(--bg-secondary, #f8f9fa);
  border-bottom: 1px solid var(--border-color, #e0e0e0);
  user-select: none;
  flex-shrink: 0;
}

.dir-preview-meta-info {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
  font-family: var(--font-ui);
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #5f6368);
  overflow: hidden;
}

.dir-preview-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: var(--font-weight-medium);
}

.dir-preview-count {
  flex-shrink: 0;
  color: var(--text-muted, #999);
}

.dir-preview-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}

/* Same button metrics as .code-preview-btn. */
.dir-preview-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 3px 6px;
  height: 24px;
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #5f6368);
  background: transparent;
  border: 1px solid transparent;
  border-radius: 4px;
  cursor: pointer;
  user-select: none;
  transition: background-color 0.15s, color 0.15s;
}

.dir-preview-btn:hover {
  background: var(--bg-hover, rgba(0, 0, 0, 0.05));
  color: var(--text-primary, #202124);
}

.dir-preview-btn:focus-visible {
  outline: 2px solid var(--primary-color, #1a73e8);
  outline-offset: -1px;
}

.dir-preview-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 100%;
  min-height: 60px;
  color: var(--text-secondary, #888);
  font-size: var(--font-size-sm);
}

.dir-preview-error {
  color: var(--danger-color, #e05252);
}

/* `auto-fill` + a min track width lets the browser derive the column count
   from the pane width; no JS measurement or ResizeObserver needed. */
.dir-preview-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 2px;
}

.dir-preview-item {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  padding: 4px 6px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-primary, #222);
  font-size: var(--font-size-sm);
  text-align: left;
  cursor: pointer;
}

.dir-preview-item:hover {
  background: var(--hover-bg, rgba(128, 128, 128, 0.12));
}

.dir-preview-icon {
  flex-shrink: 0;
}

/* Long names truncate instead of widening the track (which would push the
   column count down). */
.dir-preview-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir-preview-symlink {
  flex-shrink: 0;
  display: inline-flex;
  color: var(--text-secondary, #888);
}
</style>
