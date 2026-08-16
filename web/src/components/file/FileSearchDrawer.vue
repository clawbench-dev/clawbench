<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <Search :size="16" class="bs-header-icon" />
      <div class="bs-header-title">
        <TransitionGroup name="title-word" tag="span" class="title-sentence">
          <span
            v-for="seg in titleSegments"
            :key="seg.key"
            class="title-seg"
            :class="{ 'title-seg-accent': seg.highlight }"
          >{{ seg.text }}</span>
        </TransitionGroup>
      </div>
      <div v-if="search.state.searchBasePath && search.state.scope === 'current'" class="bs-header-description">
        <HeaderMarquee :text="search.state.searchBasePath">{{ search.state.searchBasePath }}</HeaderMarquee>
      </div>
    </template>

    <div class="fs-body">
      <div class="fs-input-row">
        <SearchInput
          ref="inputRef"
          v-model="search.state.query"
          :placeholder="t('file.search.placeholder')"
          @enter="listNav.confirm"
          @down="listNav.down"
          @up="listNav.up"
        />
        <button
          class="fs-toggle-btn"
          :class="{ active: search.state.recursive }"
          :title="t('file.search.recursive')"
          @click="toggleRecursive"
        >
          <FolderTree :size="16" />
        </button>
        <button
          class="fs-toggle-btn"
          :class="{ active: search.state.exact }"
          :title="t('file.search.exact')"
          @click="toggleExact"
        >
          <WholeWord :size="16" />
        </button>
        <button
          class="fs-toggle-btn"
          :class="{ active: search.state.scope === 'global' }"
          :title="t('file.search.scopeGlobal')"
          @click="toggleScope"
        >
          <Globe :size="16" />
        </button>
        <button class="fs-toggle-btn" :title="t('file.search.reset')" @click="handleReset">
          <RotateCcw :size="14" />
        </button>
      </div>

      <div class="fs-content">
        <LoadingIndicator v-if="search.state.searching && search.state.results.length === 0" size="md" :label="t('file.search.searching')" />
        <div v-else-if="!search.state.query.trim()" class="fs-empty">
          {{ t('file.search.placeholder') }}
        </div>
        <div v-else-if="search.state.results.length === 0 && !search.state.searching" class="fs-empty">
          {{ t('file.search.noResults') }}
        </div>
        <template v-else>
          <div class="fs-results-count">
            {{ search.state.truncated ? t('file.search.resultCountPlus', { limit: search.getDisplayLimit() }) : t('file.search.resultCount', { count: search.state.total }) }}
          </div>
          <div class="fs-results">
            <div
              v-for="(r, idx) in search.state.results"
              :key="r.path"
              class="fs-result-item"
              :class="{ 'fs-result-item-active': listNav.activeIndex.value === idx }"
              @click="onResultClick(r)"
            >
              <div class="fs-result-icon">
                <img v-if="isThumbableExt(r.path) && !thumbErrors.has(r.path)" class="fs-result-thumb" :src="thumbUrl(r)" :alt="r.name" loading="lazy" @error="onThumbError(r)" />
                <FileIcon v-else :path="r.name" :is-dir="r.type === 'dir'" :size="22" />
              </div>
              <div class="fs-result-info">
                <span class="fs-result-name" v-html="highlightName(r.name, r.matchedIndices)" />
                <span class="fs-result-path">{{ formatPath(r.path) }}</span>
              </div>
              <button
                class="fs-result-dir-btn"
                :title="t('chat.attach.openDirectory')"
                @click.stop="onOpenDir(r)"
              >
                <LocateFixed :size="16" />
              </button>
            </div>
            <div v-if="search.state.truncated" class="fs-truncated">
              {{ t('file.search.truncated') }}
            </div>
          </div>
        </template>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, FolderTree, Globe, RotateCcw, LocateFixed, WholeWord } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import HeaderMarquee from '@/components/common/HeaderMarquee.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import { useFileSearch, type FileSearchResult } from '@/composables/useFileSearch'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { navToFileInManager } from '@/composables/useFilePathAnnotation'
import { appLog } from '@/utils/appLog'
import { isThumbableExt } from '@/utils/fileManager'

const { t, locale } = useI18n()

const props = defineProps<{
  open: boolean
  currentDir: string
}>()

const emit = defineEmits<{
  close: []
  navigateDir: [path: string]
  selectFile: [path: string]
}>()

const search = useFileSearch()
const inputRef = ref<InstanceType<typeof SearchInput> | null>(null)

interface TitleSegment {
  key: string
  text: string
  highlight: boolean
}

// Build the header title from highlighted modifier segments. Each segment maps
// to a toggle button (exact / recursive / scope) and is highlighted + animated
// so the user can perceive the connection between the buttons and the title.
// Word order and spacing follow the active locale (zh: no spaces; en: SVO).
const titleSegments = computed<TitleSegment[]>(() => {
  const s = search.state
  const isEn = locale.value.toLowerCase().startsWith('en')
  const segs: TitleSegment[] = []

  if (isEn) {
    if (s.exact) segs.push({ key: 'exact', text: t('file.search.wordExact'), highlight: true })
    if (s.recursive) {
      const w = t('file.search.wordRecursive')
      const text = s.exact ? `${w.charAt(0).toLowerCase()}${w.slice(1)}` : w
      segs.push({ key: 'recursive', text, highlight: true })
    }
    const hasMod = s.exact || s.recursive
    const verb = t('file.search.wordVerb')
    const verbText = hasMod ? verb : `${verb.charAt(0).toUpperCase()}${verb.slice(1)}`
    const scope = s.scope === 'global' ? t('file.search.wordGlobal') : t('file.search.wordCurrent')
    segs.push({ key: `base-${scope}`, text: `${verbText} ${scope}`, highlight: false })
  } else {
    const scope = s.scope === 'global' ? t('file.search.wordGlobal') : t('file.search.wordCurrent')
    segs.push({ key: `scope-${scope}`, text: scope, highlight: true })
    if (s.exact) segs.push({ key: 'exact', text: t('file.search.wordExact'), highlight: true })
    if (s.recursive) segs.push({ key: 'recursive', text: t('file.search.wordRecursive'), highlight: true })
    segs.push({ key: 'verb', text: t('file.search.wordVerb'), highlight: false })
  }

  return segs
})

// Focus input when drawer opens
watch(() => props.open, async (val) => {
  if (val) {
    // Wait for BottomSheet slide-up animation (250ms) to complete before focusing
    await new Promise(r => setTimeout(r, 300))
    inputRef.value?.focus()
    // Re-run search if query exists (results may be stale)
    if (search.state.query.trim()) {
      search.startSearch(props.currentDir)
    }
  } else {
    search.cancelSearch()
  }
})

// Cancel search when directory changes while drawer is open
watch(() => props.currentDir, () => {
  if (props.open) {
    search.cancelSearch()
    search.state.results = []
    search.state.total = 0
    search.state.truncated = false
    if (search.state.query.trim()) {
      search.startSearch(props.currentDir)
    }
  }
})

// Debounced search on query change
watch(() => search.state.query, () => {
  if (!props.open) return
  search.startSearch(props.currentDir)
})

function toggleRecursive() {
  search.state.recursive = !search.state.recursive
  if (search.state.query.trim()) {
    search.startSearch(props.currentDir)
  }
}

function toggleExact() {
  search.state.exact = !search.state.exact
  if (search.state.query.trim()) {
    search.startSearch(props.currentDir)
  }
}

function toggleScope() {
  search.state.scope = search.state.scope === 'current' ? 'global' : 'current'
  if (search.state.query.trim()) {
    search.startSearch(props.currentDir)
  }
}

function handleReset() {
  search.reset()
}

// ── Keyboard ↑/↓ + Enter navigation over results ──
const listNav = useListNav({
  getCount: () => search.state.results.length,
  onConfirm: (idx) => onResultClick(search.state.results[idx]),
  onActiveChange: scrollActiveIntoView,
})
// Document-level keys so navigation also works when focus leaves the search box
useListKeys({ isOpen: () => props.open, nav: listNav })

function scrollActiveIntoView(index: number) {
  const items = document.querySelectorAll('.fs-result-item')
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
  }
}

watch(() => search.state.results, () => listNav.reset())

function handleClose() {
  search.cancelSearch()
  emit('close')
}

function onResultClick(r: FileSearchResult) {
  appLog.d('FileSearch', 'result clicked', r.path)
  handleClose()
  if (r.type === 'dir') {
    emit('navigateDir', r.path)
  } else {
    // Navigate to parent directory, then select file
    const lastSlash = r.path.lastIndexOf('/')
    const dir = lastSlash > 0 ? r.path.substring(0, lastSlash) : ''
    emit('navigateDir', dir)
    emit('selectFile', r.path)
  }
}

function onOpenDir(r: FileSearchResult) {
  appLog.d('FileSearch', 'open directory', r.path)
  handleClose()
  navToFileInManager(r.path)
}

const thumbErrors = ref(new Set<string>())

function thumbUrl(r: FileSearchResult): string {
  return `/api/file/thumb?path=${encodeURIComponent(r.path)}&w=80`
}

function onThumbError(r: FileSearchResult) {
  thumbErrors.value.add(r.path)
}

// SECURITY: Each character is individually escaped before HTML markup is applied.
// This prevents XSS via filenames containing <, >, &, etc.
function highlightName(name: string, indices: number[]): string {
  if (!indices || indices.length === 0) return escapeHtml(name)
  const indexSet = new Set(indices)
  let result = ''
  for (let i = 0; i < name.length; i++) {
    const ch = escapeHtml(name[i])
    if (indexSet.has(i)) {
      result += `<mark>${ch}</mark>`
    } else {
      result += ch
    }
  }
  return result
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

function formatPath(path: string): string {
  // Show directory portion of the path (excluding filename)
  const lastSlash = path.lastIndexOf('/')
  if (lastSlash <= 0) return ''
  return path.substring(0, lastSlash)
}

function focusSearchInput() {
  inputRef.value?.focus()
}

defineExpose({ focusSearchInput })
</script>

<style scoped>
.title-sentence {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}

.title-seg {
  display: inline-block;
}

.title-seg-accent {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 14%, transparent);
  border-radius: 4px;
  padding: 0 3px;
  font-weight: 700;
}

/* Highlighted modifier segments slide+fade in/out as their toggle buttons change. */
.title-word-enter-active,
.title-word-leave-active {
  transition: opacity 0.28s ease, transform 0.28s ease;
}

.title-word-enter-from {
  opacity: 0;
  transform: translateX(-6px);
}

.title-word-leave-to {
  opacity: 0;
  transform: translateX(6px);
}

.title-word-move {
  transition: transform 0.28s ease;
}

.fs-body {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.fs-input-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.fs-input-row :deep(.search-pill) {
  flex: 1;
}

.fs-toggle-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.15s, color 0.15s;
}

.fs-toggle-btn:hover {
  background: var(--bg-hover, rgba(0,0,0,0.06));
}

.fs-toggle-btn.active {
  color: var(--accent-color, #4a90d9);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 8%, transparent);
}

.fs-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.fs-empty {
  padding: 24px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 13px;
  flex-shrink: 0;
}

.fs-results-count {
  padding: 6px 14px;
  font-size: 11px;
  color: var(--text-muted, #999);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.fs-results {
  flex: 1;
  overflow-y: auto;
}

.fs-result-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  cursor: pointer;
  border-bottom: 1px solid var(--border-color, #f0f0f0);
  transition: background 0.1s;
}

.fs-result-item:hover {
  background: var(--bg-secondary, #f8f9fa);
}

.fs-result-item-active {
  background: var(--bg-secondary, #f8f9fa);
  border-radius: 0;
}

.fs-result-icon {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
}

.fs-result-thumb {
  width: 28px;
  height: 28px;
  object-fit: cover;
  border-radius: 4px;
}

.fs-result-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.fs-result-name {
  font-size: 13px;
  color: var(--text-primary, #212529);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.fs-result-name :deep(mark) {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent);
  color: inherit;
  padding: 0 1px;
}

.fs-result-path {
  font-size: 11px;
  color: var(--text-muted, #999);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.fs-result-dir-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.fs-result-dir-btn:hover {
  background: var(--bg-hover, rgba(0,0,0,0.06));
  color: var(--accent-color, #4a90d9);
}

.fs-truncated {
  padding: 10px 14px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 12px;
  background: var(--bg-secondary, #f8f9fa);
  border-top: 1px solid var(--border-color, #e5e5e5);
}

</style>

<style>
/* Dark theme for search highlights - non-scoped for [data-theme] */
[data-theme="dark"] .fs-result-name mark {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 28%, transparent);
  color: inherit;
}
</style>
