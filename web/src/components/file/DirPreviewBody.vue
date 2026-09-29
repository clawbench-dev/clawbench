<template>
  <!-- Directory listing body for the docked preview pane. Mirrors the file
       preview card: same card background, same meta toolbar, and a body that
       scrolls under the fixed toolbar. -->
  <div class="dir-preview-body" :class="{ 'is-loading': loading }">
    <!-- Toolbar: same metrics/colors as .code-preview-meta so the two pane
         bodies read as one component. Suppressed with `chromeless` when the
         host already renders a title/meta row (the floating preview card does),
         which would otherwise stack a third, redundant row. -->
    <div v-if="!chromeless" class="dir-preview-meta">
      <div class="dir-preview-meta-info">
        <span class="dir-preview-title">{{ dirName }}</span>
        <!-- Preview can list a directory outside the project (the pane fetches
             whatever path it is handed), so the title alone would not tell the
             user where they are. -->
        <ExternalBadge v-if="isExternalDir" kind="dir" />
        <span class="dir-preview-count">{{ t('file.dirPreview.count', { n: shown.length }) }}</span>
      </div>
      <div class="dir-preview-actions">
        <!-- Open directory: same icon as the file card's "open file" control
             (ExternalLink), so the two panes' primary action reads the same.
             The listing is already the directory, so this opens the directory
             ITSELF in the file manager rather than revealing its parent. -->
        <button
          type="button"
          class="dir-preview-btn"
          :title="t('file.codePreview.revealInTree')"
          :aria-label="t('file.codePreview.revealInTree')"
          @click="emit('open-self')"
        >
          <ExternalLink :size="12" />
        </button>
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
          <!-- Thumbable images get a real thumbnail from the same endpoint the
               file manager list uses; everything else (and any image whose
               thumbnail fails) falls back to the type icon. -->
          <img
            v-if="isThumbLoaded(entry)"
            class="dir-preview-thumb"
            :src="thumbUrlFor(entry)"
            :alt="entry.name"
            loading="lazy"
            @error="onThumbError(entry)"
          />
          <FileIcon
            v-else
            :path="entry.name"
            :is-dir="entry.type === 'dir'"
            :size="20"
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
import { computed, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, ExternalLink, FolderOpen, Link2, X } from 'lucide-vue-next'
import FileIcon from '@/components/common/FileIcon.vue'
import ExternalBadge from '@/components/file/ExternalBadge.vue'
import { isAbsolutePath } from '@/utils/path.ts'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { buildThumbUrl, isThumbable } from '@/utils/fileManager'
import { mediaVersionFor } from '@/composables/useMediaWatch.ts'
import type { DirPreviewEntry } from '@/composables/useDirPreview'

const props = defineProps<{
  entries: DirPreviewEntry[]
  loading: boolean
  error: boolean
  /** Predicate from useDirPreview — applies the "show hidden files" toggle. */
  visible: (entry: DirPreviewEntry) => boolean
  /** Display name of the directory being listed (its own base name). */
  dirName: string
  /**
   * Project-relative path of the directory being listed. Needed to build
   * thumbnail URLs (`/api/fs/thumb?target=…`); when absent, entries fall back to
   * type icons instead of thumbnails.
   */
  dirPath?: string
  /**
   * Render only the listing, without this component's own toolbar. Set by hosts
   * that already show a title/meta row (the floating preview card), so the pane
   * doesn't end up with three stacked bars.
   */
  chromeless?: boolean
}>()

/** The listed directory is outside the project root (absolute dirPath). */
const isExternalDir = computed(() => isAbsolutePath(props.dirPath || ''))

const emit = defineEmits<{
  /** A file was clicked — the caller opens it in the full-screen viewer. */
  (e: 'open-file', name: string): void
  /** A directory was clicked — the caller navigates the main list into it. */
  (e: 'open-dir', name: string): void
  /** "Open directory" was pressed — the caller opens the listed directory
   *  itself (not a child), matching the file card's reveal-in-tree action. */
  (e: 'open-self'): void
  /** The pane's close control was pressed. */
  (e: 'closed'): void
}>()

const { t } = useI18n()

const shown = computed(() => props.entries.filter(e => props.visible(e)))

// ── Thumbnails ──
// Same mechanism as the file manager list: ask /api/fs/thumb for decodable
// raster images and remember failures so a 404 isn't re-requested on every
// re-render. Keyed by the FULL path (not the bare name) — two directories can
// both hold `logo.png`, and a name-only key would suppress a valid thumbnail
// after the pane moves to a sibling directory.
const thumbErrors = reactive(new Set<string>())

function thumbKey(entry: DirPreviewEntry): string {
  return `${props.dirPath || ''}/${entry.name}`
}

function thumbUrlFor(entry: DirPreviewEntry): string {
  const url = buildThumbUrl(props.dirPath || '', entry.name)
  // Append the shared media version so a thumbnail whose source image was
  // rewritten in the background re-fetches rather than staying cached. Version
  // 0 (never changed) leaves the URL untouched.
  const v = mediaVersionFor(thumbKey(entry))
  return v ? `${url}&t=${v}` : url
}

function onThumbError(entry: DirPreviewEntry) {
  thumbErrors.add(thumbKey(entry))
}

function isThumbLoaded(entry: DirPreviewEntry): boolean {
  if (!props.dirPath) return false
  return isThumbable(entry) && !thumbErrors.has(thumbKey(entry))
}

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
  padding: var(--space-4);
}

/* ── Toolbar: mirrors .code-preview-meta (same height, colors, metrics) so the
   two docked pane bodies look like one component. ── */
.dir-preview-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding:3px var(--space-3) 3px var(--space-6);
  min-height: 28px;
  background: var(--bg-secondary, #f8f9fa);
  border-bottom: 1px solid var(--border-color, #e0e0e0);
  user-select: none;
  flex-shrink: 0;
}

.dir-preview-meta-info {
  display: flex;
  align-items: baseline;
  gap: var(--space-4);
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
  gap: var(--space-1);
  flex-shrink: 0;
}

/* Same button metrics as .code-preview-btn. */
.dir-preview-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding:3px var(--space-3);
  height: 24px;
  font-size: var(--font-size-xs);
  color: var(--text-secondary, #5f6368);
  background: transparent;
  border: 1px solid transparent;
  border-radius: var(--radius-xs);
  cursor: pointer;
  user-select: none;
  transition: background-color var(--duration-base), color var(--duration-base);
}

.dir-preview-btn:hover {
  background: var(--bg-hover, rgba(0, 0, 0, 0.05));
  color: var(--text-primary, #202124);
}

.dir-preview-btn:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: -1px;
}

.dir-preview-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-4);
  height: 100%;
  min-height: 60px;
  color: var(--text-secondary, #888);
  font-size: var(--font-size-sm);
}

.dir-preview-error {
  color: var(--color-red);
}

/* `auto-fill` + a min track width lets the browser derive the column count
   from the pane width; no JS measurement or ResizeObserver needed. */
.dir-preview-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: var(--space-1);
}

.dir-preview-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
  padding: var(--space-3) var(--space-4);
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-primary, #222);
  font-size: var(--font-size-md);
  text-align: left;
  cursor: pointer;
}

.dir-preview-item:hover {
  background: var(--bg-hover);
}

.dir-preview-icon {
  flex-shrink: 0;
}

/* Image thumbnails, from the same endpoint the file manager list uses. Sized
   to the icon it replaces so rows keep a uniform height. */
.dir-preview-thumb {
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  border-radius: var(--radius-xs);
  object-fit: cover;
  background: var(--bg-secondary, #f8f9fa);
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
