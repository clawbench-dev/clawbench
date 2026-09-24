<template>
  <BottomSheet
    :open="open"
    auto
    panel-class="content-search-sheet"
    @close="handleClose"
  >
    <template #header>
      <SearchCode :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('file.contentSearch.title') }}</span>
      <!-- Scope is surfaced in the header so the search root is never a
           surprise; the buttons below toggle it. -->
      <span class="cs-header-scope">
        {{ scopeLabel }}
      </span>
    </template>

    <div class="cs-body">
      <!-- Search row: query + the option toggles. -->
      <div class="cs-input-row">
        <SearchInput
          ref="inputRef"
          v-model="search.state.query"
          :placeholder="t('file.contentSearch.placeholder')"
          @enter="listNav.confirm"
          @down="moveSelection(1)"
          @up="moveSelection(-1)"
        />
        <button
          class="cs-toggle-btn"
          :class="{ active: search.state.caseSensitive }"
          :title="t('file.contentSearch.caseSensitive')"
          :aria-pressed="search.state.caseSensitive"
          @click="toggle('caseSensitive')"
        >
          <CaseSensitive :size="15" />
        </button>
        <button
          class="cs-toggle-btn"
          :class="{ active: search.state.wholeWord }"
          :title="t('file.contentSearch.wholeWord')"
          :aria-pressed="search.state.wholeWord"
          @click="toggle('wholeWord')"
        >
          <WholeWord :size="15" />
        </button>
        <button
          class="cs-toggle-btn"
          :class="{ active: search.state.regex }"
          :title="t('file.contentSearch.regex')"
          :aria-pressed="search.state.regex"
          @click="toggle('regex')"
        >
          <Regex :size="15" />
        </button>
        <button
          class="cs-toggle-btn"
          :class="{ active: isRecursiveEffective }"
          :disabled="isGlobalScope"
          :title="t('file.search.recursive')"
          :aria-pressed="isRecursiveEffective"
          @click="toggle('recursive')"
        >
          <FolderTree :size="15" />
        </button>
        <button
          class="cs-toggle-btn"
          :class="{ active: isGlobalScope }"
          :title="t('file.search.scopeGlobal')"
          :aria-pressed="isGlobalScope"
          @click="toggleScope"
        >
          <Globe :size="15" />
        </button>
        <button
          class="cs-toggle-btn"
          :class="{ active: filtersOpen }"
          :title="t('file.contentSearch.filters')"
          :aria-expanded="filtersOpen"
          @click="filtersOpen = !filtersOpen"
        >
          <ListFilter :size="15" />
        </button>
      </div>

      <!-- Include / exclude globs — collapsed by default (VSCode hides these
           behind a disclosure for the same reason: they are rarely needed). -->
      <div v-if="filtersOpen" class="cs-filters">
        <label class="cs-filter-row">
          <span class="cs-filter-label">{{ t('file.contentSearch.includeLabel') }}</span>
          <input
            v-model="search.state.include"
            class="cs-filter-input"
            type="text"
            spellcheck="false"
            :placeholder="t('file.contentSearch.includePlaceholder')"
            @keydown.enter="rerun"
          />
        </label>
        <label class="cs-filter-row">
          <span class="cs-filter-label">{{ t('file.contentSearch.excludeLabel') }}</span>
          <input
            v-model="search.state.exclude"
            class="cs-filter-input"
            type="text"
            spellcheck="false"
            :placeholder="t('file.contentSearch.excludePlaceholder')"
            @keydown.enter="rerun"
          />
        </label>
      </div>

      <div class="cs-content">
        <!-- Invalid pattern: the backend reports this in-band because an
             EventSource cannot read a 4xx body. -->
        <div v-if="search.state.error" class="cs-error">
          <TriangleAlert :size="16" />
          <span>{{ search.state.error }}</span>
        </div>

        <template v-else-if="!hasQuery">
          <div class="cs-empty">{{ t('file.contentSearch.hint') }}</div>
        </template>

        <template v-else-if="search.state.searching && search.state.results.length === 0">
          <LoadingIndicator size="md" :label="t('file.search.searching')" />
        </template>

        <template v-else-if="search.state.results.length === 0">
          <div class="cs-empty">{{ t('file.search.noResults') }}</div>
        </template>

        <template v-else>
          <div class="cs-summary">
            {{ search.state.truncated
              ? t('file.contentSearch.summaryPlus', { files: search.getDisplayLimit(), matches: search.state.matches })
              : t('file.contentSearch.summary', { files: search.state.files, matches: search.state.matches }) }}
          </div>

          <div class="cs-results">
            <div v-for="file in search.state.results" :key="file.path" class="cs-file">
              <button
                class="cs-file-head"
                :class="{ collapsed: isCollapsed(file.path) }"
                :title="file.path"
                @click="toggleCollapse(file.path)"
              >
                <ChevronRight :size="13" class="cs-chevron" />
                <FileIcon :path="file.path" :size="15" class="cs-file-icon" />
                <span class="cs-file-name">{{ file.name }}</span>
                <span v-if="parentDirOf(file.path)" class="cs-file-dir">{{ parentDirOf(file.path) }}</span>
                <span class="cs-file-count" :class="{ 'cs-count-plus': file.truncated }">
                  {{ file.truncated ? `${file.matches.length}+` : file.matches.length }}
                </span>
              </button>

              <div v-show="!isCollapsed(file.path)" class="cs-file-matches">
                <div
                  v-for="(m, idx) in file.matches"
                  :key="`${m.line}-${idx}`"
                  class="cs-match"
                  :class="{ 'cs-match-active': listNav.activeIndex.value === flatIndex(file.path, idx) }"
                  :data-flat-index="flatIndex(file.path, idx)"
                  :title="`${file.path}:${m.line}`"
                  @click="openMatch(file.path, m.line)"
                >
                  <span class="cs-match-line">{{ m.line }}</span>
                  <span class="cs-match-text" v-html="highlightRanges(m.text, m.ranges)"></span>
                </div>
                <div v-if="file.truncated" class="cs-match-more">
                  {{ t('file.contentSearch.fileTruncated', { total: file.total }) }}
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  SearchCode, CaseSensitive, WholeWord, Regex, FolderTree, Globe,
  ListFilter, ChevronRight, TriangleAlert,
} from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useFileContentSearch } from '@/composables/useFileContentSearch'
import { useListNav } from '@/composables/useListNav'
import { highlightRanges, parentDirOf } from '@/utils/contentSearchMark'

const { t } = useI18n()

const props = defineProps<{
  open: boolean
  /** Directory the search starts from (project-relative or absolute). */
  currentDir: string
}>()

const emit = defineEmits<{
  close: []
  /** Open a file at a line, then dismiss the dialog. */
  openFile: [path: string, line: number]
}>()

const search = useFileContentSearch()
const inputRef = ref<InstanceType<typeof SearchInput> | null>(null)
const filtersOpen = ref(false)
/** Paths the user has folded shut; everything else starts expanded. */
const collapsed = ref(new Set<string>())

const hasQuery = computed(() => !!search.state.query.trim())
const isGlobalScope = computed(() => search.state.scope === 'global')
const isRecursiveEffective = computed(() => search.effectiveRecursive.value)

const scopeLabel = computed(() =>
  isGlobalScope.value ? t('file.search.wordGlobal') : t('file.search.wordCurrent'),
)

function isCollapsed(path: string) {
  return collapsed.value.has(path)
}

function toggleCollapse(path: string) {
  const next = new Set(collapsed.value)
  if (next.has(path)) next.delete(path)
  else next.add(path)
  collapsed.value = next
}

/**
 * Toggle one search option and immediately re-run. Re-running on toggle (rather
 * than waiting for the next keystroke) matches VSCode, where flipping Aa/ab/.*
 * re-queries instantly.
 */
function toggle(key: 'caseSensitive' | 'wholeWord' | 'regex' | 'recursive') {
  search.state[key] = !search.state[key]
  rerun()
}

function toggleScope() {
  search.state.scope = isGlobalScope.value ? 'current' : 'global'
  rerun()
}

function rerun() {
  if (hasQuery.value) search.startSearch(props.currentDir, true)
}

/** Re-run when include/exclude settle (debounced by the composable). */
watch(() => [search.state.include, search.state.exclude], () => {
  if (props.open && hasQuery.value) search.startSearch(props.currentDir)
})

/**
 * Debounced search while typing. Folding state is reset per query because the
 * file set is replaced wholesale — a collapse left over from the previous
 * query would apply to an unrelated file.
 */
watch(() => search.state.query, () => {
  collapsed.value = new Set()
  search.startSearch(props.currentDir)
})

// Reset folding whenever the result set is replaced.
watch(() => search.state.results, () => {
  collapsed.value = new Set()
})

// Focus the input once the sheet has finished sliding in. Focusing during the
// animation makes the browser scroll the still-animating panel, which reads as
// the dialog jumping — see the SearchDrawer/UserMsgIndexDrawer precedent.
watch(() => props.open, async (isOpen) => {
  if (isOpen) {
    await new Promise(r => setTimeout(r, 300))
    nextTick(() => inputRef.value?.focus())
    if (hasQuery.value) search.startSearch(props.currentDir, true)
  } else {
    search.cancelSearch()
  }
})

// A directory change invalidates the search root.
watch(() => props.currentDir, () => {
  if (props.open && hasQuery.value) search.startSearch(props.currentDir, true)
})

function openMatch(path: string, line: number) {
  emit('openFile', path, line)
}

/**
 * Flattened (file, match) list — the arrow-key navigation unit.
 *
 * Navigation is over individual matches rather than files so Enter can open the
 * exact line. Collapsed files are still included: folding is a display choice,
 * and skipping them would make the highlight and the arrow keys disagree.
 */
const flatMatches = computed(() => {
  const out: Array<{ path: string; line: number }> = []
  for (const file of search.state.results) {
    for (const m of file.matches) out.push({ path: file.path, line: m.line })
  }
  return out
})

/** Index of one match within flatMatches, or -1 when absent. */
function flatIndex(path: string, matchIdx: number): number {
  let index = 0
  for (const file of search.state.results) {
    if (file.path === path) return index + matchIdx
    index += file.matches.length
  }
  return -1
}

const listNav = useListNav({
  getCount: () => flatMatches.value.length,
  onConfirm: (idx) => {
    const hit = flatMatches.value[idx]
    if (hit) openMatch(hit.path, hit.line)
  },
  onActiveChange: scrollActiveIntoView,
})

function moveSelection(delta: number) {
  if (delta > 0) listNav.down()
  else listNav.up()
}

function scrollActiveIntoView(index: number) {
  nextTick(() => {
    const el = document.querySelector(`.cs-match[data-flat-index="${index}"]`)
    if (el && typeof el.scrollIntoView === 'function') {
      el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
    }
  })
}

// Drop the highlight whenever the result set is replaced, so Enter never opens
// a match that no longer exists.
watch(() => search.state.results, () => listNav.reset())

function handleClose() {
  search.cancelSearch()
  emit('close')
}

defineExpose({
  focusInput() {
    inputRef.value?.focus()
  },
  searchState: search.state,
})
</script>

<style scoped>
.cs-body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

/* ── Search row ── */
.cs-input-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-6);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.cs-input-row :deep(.search-pill) {
  flex: 1;
  min-width: 0;
}

.cs-toggle-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  padding: 0;
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .cs-toggle-btn:hover:not(:disabled) {
    background: var(--bg-hover, rgba(0, 0, 0, 0.06));
  }
}

.cs-toggle-btn.active {
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
}

.cs-toggle-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: default;
}

/* ── Include / exclude ── */
.cs-filters {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-6);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.cs-filter-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
}

.cs-filter-label {
  flex-shrink: 0;
  width: 64px;
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}

.cs-filter-input {
  flex: 1;
  min-width: 0;
  padding: var(--space-2) var(--space-4);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-family: var(--font-mono);
  outline: none;
}

.cs-filter-input:focus {
  border-color: var(--accent-color);
}

/* ── Content ── */
.cs-content {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

.cs-empty {
  padding: var(--space-9) var(--space-6);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-md);
}

.cs-error {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin: var(--space-5) var(--space-6) 0;
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red);
  font-size: var(--font-size-sm);
  word-break: break-word;
}

.cs-summary {
  position: sticky;
  top: 0;
  z-index: 1;
  padding: var(--space-3) var(--space-6);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  background: var(--bg-tertiary, #f8f8f8);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

/* ── Grouped results ── */
.cs-results {
  flex: 1;
}

.cs-file {
  border-bottom: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);
}

.cs-file-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-3) var(--space-5);
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  text-align: left;
  cursor: pointer;
}

@media (hover: hover) {
  .cs-file-head:hover {
    background: var(--bg-hover, rgba(0, 0, 0, 0.04));
  }
}

.cs-chevron {
  flex-shrink: 0;
  color: var(--text-muted);
  transform: rotate(90deg);
  transition: transform var(--duration-base);
}

.cs-file-head.collapsed .cs-chevron {
  transform: rotate(0deg);
}

.cs-file-icon {
  flex-shrink: 0;
}

.cs-file-name {
  flex-shrink: 0;
  font-weight: var(--font-weight-medium);
}

/* The directory is secondary and truncates first — the file name is the
   identity, so it keeps its width. */
.cs-file-dir {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  direction: rtl;
  text-align: left;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.cs-file-count {
  flex-shrink: 0;
  min-width: 22px;
  padding: 0 var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary, #eee);
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  text-align: center;
}

.cs-file-count.cs-count-plus {
  background: color-mix(in srgb, var(--accent-color) 16%, transparent);
  color: var(--accent-color);
}

/* ── Matches ── */
.cs-file-matches {
  padding-bottom: var(--space-2);
}

.cs-match {
  display: flex;
  align-items: baseline;
  gap: var(--space-4);
  padding: var(--space-2) var(--space-5) var(--space-2) var(--space-10);
  cursor: pointer;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-snug);
}

@media (hover: hover) {
  .cs-match:hover {
    background: var(--bg-hover, rgba(0, 0, 0, 0.04));
  }
}

.cs-match.cs-match-active {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

.cs-match-line {
  flex-shrink: 0;
  min-width: 34px;
  text-align: right;
  color: var(--text-muted);
  user-select: none;
}

.cs-match-text {
  flex: 1;
  min-width: 0;
  white-space: pre;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--text-secondary);
}

.cs-match-text :deep(mark) {
  background: color-mix(in srgb, var(--accent-color) 28%, transparent);
  color: var(--text-primary);
  border-radius: 2px;
}

.cs-match-more {
  padding: var(--space-2) var(--space-5) var(--space-2) var(--space-10);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  font-style: italic;
}
</style>

<style>
/* Wide-screen: the content search benefits from extra width — code lines and
   paths wrap badly in a narrow card. Mirrors .session-search-sheet. Only takes
   effect in BottomSheet's wide-screen card mode; the narrow-mode bottom sheet
   ignores --modal-max-width and stays a full-height drawer. */
.content-search-sheet {
  --modal-max-width: 900px;
}
</style>
