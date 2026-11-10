<template>
  <div class="fp">
    <!-- Shortcut rows: the two things a chat user almost always wants — the
         file they are looking at and the folder it lives in. Kept as the first
         thing in the picker because replacing the old "当前" tab with a browser
         would otherwise turn a one-tap attach into "hunt for it in the list". -->
    <div v-if="showQuick" class="fp-quick">
      <button
        v-if="currentDirPath"
        type="button"
        class="fp-row fp-quick-row"
        :class="{ 'fp-row-selected': isSelected(currentDirPath) }"
        data-role="fp-current-dir"
        @click="toggle(currentDirPath, true)"
      >
        <div class="fp-icon-wrap">
          <FileIcon path="" :is-dir="true" :size="26" />
        </div>
        <div class="fp-info">
          <span class="fp-name">
            <span class="fp-label">{{ t('chat.attach.currentDir') }}</span>{{ currentDirName }}
          </span>
          <span class="fp-sub">{{ currentDirPath }}</span>
        </div>
        <Check v-if="isSelected(currentDirPath)" :size="16" class="fp-check" />
      </button>
      <button
        v-if="currentFile"
        type="button"
        class="fp-row fp-quick-row"
        :class="{ 'fp-row-selected': isSelected(currentFile) }"
        data-role="fp-current-file"
        @click="toggle(currentFile)"
      >
        <div class="fp-icon-wrap">
          <img v-if="canThumb(currentFile) && !thumbErrors.has(currentFile)" class="fp-thumb"
            :src="thumbUrl(currentFile)" alt="" loading="lazy" @error="onThumbError(currentFile)" />
          <FileIcon v-else :path="currentFile" :size="26" />
        </div>
        <div class="fp-info">
          <span class="fp-name">
            <span class="fp-label">{{ t('chat.attach.currentFile') }}</span>{{ baseName(currentFile) }}
          </span>
          <span class="fp-sub">{{ dirName(currentFile) || '/' }}</span>
        </div>
        <Check v-if="isSelected(currentFile)" :size="16" class="fp-check" />
      </button>
      <div v-if="showQuick" class="fp-quick-sep" />
    </div>

    <!-- Location: up + breadcrumb. Same component as the file manager, so an
         external (project-outside) browse keeps its filesystem-root crumb. -->
    <div class="fp-nav">
      <button class="fp-up" type="button" :disabled="!hasParent" :title="t('file.nav.parentDir')" @click="goUp">
        <ArrowLeft :size="14" />
      </button>
      <DirBreadcrumb :path="dir" @navigate="navigateTo" />
    </div>

    <!-- Result count while searching (the listing below is then search hits,
         not the current directory). -->
    <div v-if="queryActive" class="fp-count">
      {{ search.state.truncated
        ? t('file.search.resultCountPlus', { limit: search.getDisplayLimit() })
        : t('file.search.resultCount', { count: search.state.total }) }}
    </div>

    <div ref="listRef" class="fp-list">
      <PanelSkeleton v-if="loading || (search.state.searching && rows.length === 0)"
        variant="list" :rows="7" :meta="false" :label="t('common.loading')" />
      <div v-else-if="rows.length === 0" class="fp-empty" data-role="fp-empty">
        <FileX :size="28" class="fp-empty-icon" />
        <span>{{ queryActive ? t('file.search.noResults') : t('file.emptyDir') }}</span>
      </div>
      <!-- A div, not a button: a directory row contains its own pick button, and
           nested <button>s are invalid HTML (the inner one silently swallows the
           outer one's activation on some engines). -->
      <div
        v-for="(row, i) in rows"
        :key="row.path"
        class="fp-row"
        :class="{ 'fp-row-selected': isSelected(row.path), 'fp-row-hl': i === highlightIndex, 'fp-row-dir': row.type === 'dir' }"
        role="button"
        tabindex="0"
        :data-role="row.type === 'dir' ? 'fp-dir' : 'fp-file'"
        :data-path="row.path"
        @click="onRowClick(row)"
        @keydown.enter.prevent="onRowClick(row)"
        @keydown.space.prevent="onRowClick(row)"
        @mouseenter="highlightIndex = i"
      >
        <div class="fp-icon-wrap">
          <img v-if="row.type !== 'dir' && canThumb(row.path) && !thumbErrors.has(row.path)" class="fp-thumb"
            :src="thumbUrl(row.path)" alt="" loading="lazy" @error="onThumbError(row.path)" />
          <FileIcon v-else :path="row.path" :is-dir="row.type === 'dir'" :size="26" />
        </div>
        <div class="fp-info">
          <span class="fp-name" v-html="row.html"></span>
          <span v-if="row.parentDir" class="fp-sub">{{ row.parentDir }}</span>
        </div>
        <!-- Directories need BOTH "enter" and "attach this folder", so the row
             tap enters and this trailing button selects. Files have only one
             meaning, so the whole row toggles them. -->
        <button
          v-if="row.type === 'dir'"
          type="button"
          class="fp-dir-pick"
          :class="{ 'fp-dir-pick-on': isSelected(row.path) }"
          :aria-pressed="isSelected(row.path)"
          :title="isSelected(row.path) ? t('chat.attach.unpickDir') : t('chat.attach.pickDir')"
          data-action="fp-pick-dir"
          @click.stop="toggle(row.path, true)"
        >
          <Check v-if="isSelected(row.path)" :size="14" />
          <Plus v-else :size="14" />
        </button>
        <Check v-else-if="isSelected(row.path)" :size="16" class="fp-check" />
      </div>
    </div>

    <!-- Search dock: the same component the file manager uses, so the two
         look and behave identically (flat field inside a tertiary bar). -->
    <FileSearchDock
      :query="search.state.query"
      :placeholder="searchPlaceholder"
      :exact="search.state.exact"
      :recursive-active="isRecursiveEffective"
      :recursive-disabled="isGlobalScope"
      :global-active="isGlobalScope"
      @update:query="search.state.query = $event"
      @enter="confirmHighlighted"
      @down="moveHighlight(1)"
      @up="moveHighlight(-1)"
      @toggle-exact="search.state.exact = !search.state.exact"
      @toggle-recursive="search.state.recursive = !search.state.recursive"
      @toggle-global="toggleScope"
    />
  </div>
</template>

<script setup lang="ts">
/**
 * Standalone file picker: browse the project (or an allowed external root),
 * multi-select files and folders, search by name from the bottom dock.
 *
 * WHY IT OWNS ITS STATE
 * ---------------------
 * It deliberately does NOT read or write the file manager's store
 * (`store.state.currentDir` / `dirEntries`). That state is the LEFT PANEL's
 * location: browsing here would drag the file manager along, and two panes
 * would fight over one scroll position. `resolveDirRequest` is shared with the
 * store instead, so the path rules (project-relative vs. external) cannot drift.
 *
 * It is also NOT a stripped-down copy of FileManagerContent: no rename, delete,
 * upload, context menu, multi-select toolbar or preview pane. Only "list a
 * directory, search names, pick entries".
 */
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, Check, FileX, Plus } from 'lucide-vue-next'
import FileIcon from '@/components/common/FileIcon.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import DirBreadcrumb from './DirBreadcrumb.vue'
import FileSearchDock from './FileSearchDock.vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { resolveDirRequest } from '@/utils/dirPath'
import { baseName, dirName, isAbsolutePath, joinPath } from '@/utils/path'
import { buildPathThumbUrl } from '@/utils/fileIcon'
import { isThumbableExt } from '@/utils/fileManager'
import { highlightName, toDisplayEntry } from '@/utils/fileSearchMark'
import { useFileSearch } from '@/composables/useFileSearch'
import { store } from '@/stores/app'
import type { FileEntry } from '@/utils/fileAttachmentUtils'

interface DirEntry {
  name: string
  type: 'dir' | 'file' | 'image'
  size?: number
  modified?: string
  ignored?: boolean
}

/** One rendered row, in the shape both browse and search produce. */
interface Row {
  name: string
  type: 'dir' | 'file'
  /** Project-relative, or absolute when browsing project-external. */
  path: string
  parentDir: string
  html: string
}

const props = withDefaults(defineProps<{
  /** The sheet is open — the picker re-anchors to `startDir` each time. */
  open?: boolean
  /** Where to start browsing (the caller's current directory). */
  startDir?: string | null
  /** The file the caller is looking at, offered as a one-tap shortcut. */
  currentFile?: string | null
  /** The current selection, owned by the caller (the sheet's draft). */
  selected?: FileEntry[]
  /** Whether a folder may be attached (mail packs it into one zip). */
  allowDir?: boolean
}>(), {
  open: false,
  startDir: null,
  currentFile: null,
  selected: () => [],
  allowDir: true,
})

const emit = defineEmits<{
  'update:selected': [entries: FileEntry[]]
}>()

const { t } = useI18n()
const TAG = 'FilePicker'

const dir = ref('')
const entries = ref<DirEntry[]>([])
const loading = ref(false)
const thumbErrors = ref(new Set<string>())
const highlightIndex = ref(-1)
const listRef = ref<HTMLElement | null>(null)

const search = useFileSearch()

const thumbUrl = buildPathThumbUrl
function onThumbError(path: string) {
  const next = new Set(thumbErrors.value)
  next.add(path)
  thumbErrors.value = next
}

function canThumb(path: string) {
  return isThumbableExt(path)
}

// ── Listing ──

let loadSeq = 0

/**
 * Load `target`. A stale response (a newer navigation started meanwhile) is
 * dropped rather than rendered — otherwise a slow parent listing lands after
 * the child one and the list shows the wrong directory.
 */
async function loadDir(target: string) {
  const seq = ++loadSeq
  const req = resolveDirRequest(target, store.state.projectRoot)
  loading.value = true
  highlightIndex.value = -1
  try {
    const data = await apiGet<{ items: DirEntry[] }>(req.url)
    if (seq !== loadSeq) return
    dir.value = req.dir
    entries.value = data.items || []
  } catch (e) {
    if (seq !== loadSeq) return
    appLog.w(TAG, 'list directory failed', req.url, e)
    // Keep the previous listing on screen: a transient failure should not blank
    // the picker while the user is mid-selection. `dir` is only advanced on
    // success, so the breadcrumb still matches what is shown.
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

function navigateTo(target: string) {
  // Entering a folder from search results: clear the query, otherwise the
  // listing stays replaced by hits from the directory we just left.
  if (queryActive.value) search.reset()
  loadDir(target)
}

function goUp() {
  if (!hasParent.value) return
  navigateTo(dirName(dir.value))
}

const hasParent = computed(() => {
  // The project root's parent is the filesystem, which this picker has no
  // business walking into; an external browse keeps going up to "/".
  if (!dir.value) return false
  if (isAbsolutePath(dir.value)) return dir.value !== '/'
  // Any non-empty project-relative directory has the project root above it.
  return true
})

// ── Rows (browse or search hits) ──

const queryActive = computed(() => !!search.state.query.trim())
const isGlobalScope = computed(() => search.state.scope === 'global')
const isRecursiveEffective = computed(() => search.effectiveRecursive.value)

const rows = computed<Row[]>(() => {
  if (queryActive.value) {
    return search.state.results.map(r => {
      const d = toDisplayEntry(r)
      return {
        name: d.name,
        type: d.type === 'dir' ? 'dir' : 'file',
        path: d.path,
        // Only recursive / global hits need their origin shown; a hit from the
        // directory being browsed already states it in the breadcrumb.
        parentDir: search.effectiveRecursive.value ? d.parentDir : '',
        html: highlightName(d.name, d.matchedIndices),
      }
    })
  }
  // Directories first, then by name — the same order users expect from a file
  // dialog. The backend's own order is filesystem-dependent.
  return [...entries.value]
    .sort((a, b) => {
      const ad = a.type === 'dir' ? 0 : 1
      const bd = b.type === 'dir' ? 0 : 1
      if (ad !== bd) return ad - bd
      return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
    })
    .map(e => ({
      name: e.name,
      type: e.type === 'dir' ? 'dir' : 'file',
      path: joinPath(dir.value, e.name),
      parentDir: '',
      html: highlightName(e.name, undefined),
    }))
})

// ── Selection (owned by the caller; we emit a new array) ──

const selectedPaths = computed(() => new Set((props.selected || []).map(e => e.path)))

function isSelected(path: string) {
  return selectedPaths.value.has(path)
}

/**
 * Add or remove one path.
 *
 * Rebuilt from the caller's list (never mutated in place) so quote entries and
 * line-range references in the same draft survive untouched — this picker only
 * ever owns the plain path entries it shows.
 */
function toggle(path: string, isDir = false) {
  if (!props.allowDir && isDir) return
  const current = props.selected || []
  const next = current.some(e => e.path === path)
    ? current.filter(e => e.path !== path)
    : [...current, { path, isDir } as FileEntry]
  emit('update:selected', next)
}

function onRowClick(row: Row) {
  if (row.type === 'dir') navigateTo(row.path)
  else toggle(row.path)
}

// ── Quick rows ──

const currentDirPath = computed(() => {
  const raw = props.startDir || ''
  if (!raw) return ''
  // An external absolute path keeps its root; a project-relative one is shown
  // without the decorative slashes.
  return isAbsolutePath(raw) ? raw.replace(/\/+$/, '') || '/' : raw.replace(/^\/+|\/+$/g, '')
})
const currentDirName = computed(() => (currentDirPath.value ? baseName(currentDirPath.value) : '/'))
const showQuick = computed(() => !!(currentDirPath.value || props.currentFile))

// ── Search ──

const searchPlaceholder = computed(() => {
  const s = search.state
  const scope = isGlobalScope.value ? t('file.search.wordGlobal') : t('file.search.wordCurrent')
  const words = [scope]
  if (s.exact) words.push(t('file.search.wordExact'))
  if (isRecursiveEffective.value && !isGlobalScope.value) words.push(t('file.search.wordRecursive'))
  return `${t('file.search.wordVerb')}${words.join('·')}`
})

function toggleScope() {
  search.state.scope = search.state.scope === 'current' ? 'global' : 'current'
}

watch(() => search.state.query, () => {
  highlightIndex.value = -1
  search.startSearch(dir.value)
})

// A scope/recursion change re-runs the same query — the query itself did not
// change, so the watcher above does not fire.
watch([() => search.state.scope, () => search.state.recursive, () => search.state.exact], () => {
  if (queryActive.value) search.startSearch(dir.value, true)
})

function moveHighlight(delta: number) {
  const total = rows.value.length
  if (total === 0) return
  const next = highlightIndex.value + delta
  highlightIndex.value = next < 0 ? total - 1 : next >= total ? 0 : next
  nextTick(() => {
    const el = listRef.value?.querySelector('.fp-row-hl') as HTMLElement | null
    el?.scrollIntoView({ block: 'nearest' })
  })
}

function confirmHighlighted() {
  const row = rows.value[highlightIndex.value]
  if (row) onRowClick(row)
}

// ── Lifecycle ──

// Re-anchor on every open. The sheet keeps its children mounted (v-show), so
// without this the picker would still show wherever the user browsed last
// time, which contradicts the "当前目录" shortcut at the top.
watch(() => props.open, (open) => {
  if (!open) {
    search.cancelSearch()
    return
  }
  thumbErrors.value = new Set()
  search.reset()
  loadDir(props.startDir || '')
}, { immediate: true })

onBeforeUnmount(() => search.cancelSearch())
</script>

<style scoped>
.fp {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  width: 100%;
}

/* ── Quick rows ── */
.fp-quick {
  flex-shrink: 0;
  padding-top: var(--space-2);
}
.fp-quick-sep {
  height: 1px;
  background: var(--border-color);
  margin: var(--space-2) 0 0;
}
.fp-label {
  color: var(--text-muted);
  margin-right: var(--space-2);
}

/* ── Location bar ── */
.fp-nav {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3) calc(14 * var(--dp));
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  min-width: 0;
}
.fp-up {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  flex-shrink: 0;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--text-secondary);
  cursor: pointer;
}
.fp-up:active:not(:disabled) {
  background: var(--bg-hover);
}
.fp-up:disabled {
  opacity: var(--opacity-disabled);
  cursor: default;
}

.fp-count {
  flex-shrink: 0;
  padding: var(--space-2) calc(14 * var(--dp));
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}

/* ── List ── */
.fp-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.fp-row {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  width: 100%;
  padding: var(--space-4) calc(14 * var(--dp));
  border: none;
  background: none;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
  position: relative;
}
.fp-row:active {
  background: var(--bg-hover);
}
@media (hover: hover) {
  .fp-row:hover {
    background: var(--bg-hover);
  }
}
/* Selected: accent bar + tint (same three-signal pattern as the sheet's rows). */
.fp-row-selected {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}
.fp-row-selected::before {
  content: '';
  position: absolute;
  left: 0;
  top: calc(6 * var(--dp));
  bottom: calc(6 * var(--dp));
  width: calc(2 * var(--dp));
  background: var(--accent-color);
}
/* Keyboard highlight from the search box — outline, not tint, so it stays
   distinguishable from "selected". */
.fp-row-hl,
.fp-row:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: calc(-2 * var(--dp));
}
.fp-icon-wrap {
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.fp-thumb {
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  object-fit: cover;
  border-radius: var(--radius-xs);
}
.fp-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: calc(2 * var(--dp));
}
.fp-name {
  font-size: var(--font-size-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.fp-sub {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.fp-check {
  color: var(--accent-color);
  flex-shrink: 0;
}
.fp-dir-pick {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  flex-shrink: 0;
  padding: 0;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: none;
  color: var(--text-muted);
  cursor: pointer;
}
.fp-dir-pick-on {
  border-color: var(--accent-color);
  color: var(--accent-color);
}
.fp-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-4);
  padding: calc(32 * var(--dp)) var(--space-7);
  color: var(--text-muted);
  font-size: var(--font-size-md);
}
.fp-empty-icon {
  opacity: var(--opacity-muted);
}

</style>
