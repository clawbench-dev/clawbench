<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <Search :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ headerTitle }}</span>
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
          @enter="onEnter"
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
        <div v-if="search.state.searching && search.state.results.length === 0" class="fs-empty">
          {{ t('file.search.searching') }}
        </div>
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
              v-for="r in search.state.results"
              :key="r.path"
              class="fs-result-item"
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
                <FolderOpen :size="16" />
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
import { ref, watch, nextTick, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, FolderTree, Globe, RotateCcw, FolderOpen } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import HeaderMarquee from '@/components/common/HeaderMarquee.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import { useFileSearch, type FileSearchResult } from '@/composables/useFileSearch'
import { navToFileInManager } from '@/composables/useFilePathAnnotation'
import { appLog } from '@/utils/appLog'
import { isThumbableExt } from '@/utils/fileManager'

const { t } = useI18n()

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

const headerTitle = computed(() => {
  if (search.state.scope === 'global') {
    return search.state.recursive
      ? t('file.search.titleGlobalRecursive')
      : t('file.search.titleGlobal')
  }
  return search.state.recursive
    ? t('file.search.titleCurrentRecursive')
    : t('file.search.titleCurrent')
})

// Focus input when drawer opens
watch(() => props.open, async (val) => {
  if (val) {
    await nextTick()
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

function toggleScope() {
  search.state.scope = search.state.scope === 'current' ? 'global' : 'current'
  if (search.state.query.trim()) {
    search.startSearch(props.currentDir)
  }
}

function handleReset() {
  search.reset()
}

function handleClose() {
  search.cancelSearch()
  emit('close')
}

function onEnter() {
  // Immediate search on enter (skip debounce)
  search.startSearch(props.currentDir, true)
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
</script>

<style scoped>
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
  background: rgba(255, 230, 0, 0.5);
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
  background: rgba(255, 230, 0, 0.35);
  color: inherit;
}
</style>
