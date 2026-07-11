<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <div class="ad-header">
        <Paperclip :size="16" class="bs-header-icon" />
        <span class="bs-header-title">{{ t('chat.attach.drawerTitle') }}</span>
        <button class="ad-upload-btn" @click="handleUpload" :title="t('chat.attach.uploadFile')">
          <Upload :size="16" />
        </button>
      </div>
    </template>

    <!-- Tab bar (horizontal scroll) -->
    <div class="ad-tab-bar">
      <button
        v-for="tab in tabs" :key="tab.key"
        class="ad-tab" :class="{ 'ad-tab-active': activeTab === tab.key }"
        @click="activeTab = tab.key"
      >{{ tab.label }}</button>
    </div>

    <!-- Tab content -->
    <div class="ad-content">
      <!-- Current (file + directory) -->
      <template v-if="activeTab === 'current'">
        <!-- Current directory -->
        <button v-if="effectiveCurrentDir && !isAttached(effectiveCurrentDir)"
          class="ad-file-row ad-current-item" @click="$emit('add-attached', effectiveCurrentDir)">
          <Folder :size="16" class="ad-file-icon" />
          <div class="ad-file-info">
            <span class="ad-file-name">
              <span class="ad-label">{{ t('chat.attach.currentDir') }}</span>
              {{ currentDirDisplayName }}
            </span>
            <span class="ad-file-meta">{{ effectiveCurrentDir }}</span>
          </div>
          <Check :size="14" class="ad-file-check" />
        </button>
        <!-- Current file -->
        <button v-if="currentFile && !isAttached(currentFile)"
          class="ad-file-row ad-current-item" @click="$emit('add-attached', currentFile)">
          <FileText :size="16" class="ad-file-icon" />
          <div class="ad-file-info">
            <span class="ad-file-name">
              <span class="ad-label">{{ t('chat.attach.currentFile') }}</span>
              {{ baseName(currentFile) }}
            </span>
            <span class="ad-file-meta">{{ dirName(currentFile) }}</span>
          </div>
          <Check :size="14" class="ad-file-check" />
        </button>
        <div v-if="!currentFile && !effectiveCurrentDir" class="ad-empty">{{ t('chat.attach.emptyCurrent') }}</div>
      </template>

      <!-- Recently referenced -->
      <template v-if="activeTab === 'references'">
        <div v-if="!recentReferencedFiles?.length" class="ad-empty">{{ t('chat.attach.emptyReferences') }}</div>
        <button
          v-for="item in recentReferencedFiles" :key="item.path"
          class="ad-file-row" :class="{ 'ad-file-attached': isAttached(item.path) }"
          @click="$emit('add-attached', item.path)"
        >
          <FileText :size="16" class="ad-file-icon" />
          <div class="ad-file-info">
            <span class="ad-file-name">{{ baseName(item.path) }}</span>
            <span class="ad-file-meta">{{ dirName(item.path) }} · x{{ item.count }}</span>
          </div>
          <Check :size="14" class="ad-file-check" />
        </button>
      </template>

      <!-- Recently shared -->
      <template v-if="activeTab === 'shares'">
        <div v-if="recentShares.length === 0" class="ad-empty">{{ t('chat.attach.emptyShares') }}</div>
        <button
          v-for="item in recentShares" :key="item.path"
          class="ad-file-row" :class="{ 'ad-file-attached': isAttached(item.path) }"
          @click="$emit('add-attached', item.path)"
        >
          <Share2 :size="16" class="ad-file-icon" />
          <div class="ad-file-info">
            <span class="ad-file-name">{{ item.name }}</span>
            <span class="ad-file-meta">{{ dirName(item.path) }} · {{ formatModTime(item.modTime) }} · {{ formatFileSize(item.size) }}</span>
          </div>
          <Check :size="14" class="ad-file-check" />
        </button>
      </template>

      <!-- Recently uploaded -->
      <template v-if="activeTab === 'uploads'">
        <div v-if="recentUploads.length === 0" class="ad-empty">{{ t('chat.attach.emptyUploads') }}</div>
        <button
          v-for="item in recentUploads" :key="item.path"
          class="ad-file-row" :class="{ 'ad-file-attached': isAttached(item.path) }"
          @click="$emit('add-attached', item.path)"
        >
          <Upload :size="16" class="ad-file-icon" />
          <div class="ad-file-info">
            <span class="ad-file-name">{{ item.name }}</span>
            <span class="ad-file-meta">{{ dirName(item.path) }} · {{ formatModTime(item.modTime) }} · {{ formatFileSize(item.size) }}</span>
          </div>
          <Check :size="14" class="ad-file-check" />
        </button>
      </template>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { Paperclip, Upload, FileText, Folder, Share2, Check } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { useI18n } from 'vue-i18n'
import { useShareIn } from '@/composables/useShareIn'
import { useUploadRecent } from '@/composables/useUploadRecent'
import { baseName, dirName } from '@/utils/path'
import { formatFileSize } from '@/utils/fileType'

interface ReferencedFile {
  path: string
  count: number
}

const props = withDefaults(defineProps<{
  open: boolean
  currentFile?: string | null
  currentDir?: string | null
  attachedFiles?: string[]
  recentReferencedFiles?: ReferencedFile[]
}>(), {
  currentFile: null,
  currentDir: null,
  attachedFiles: () => [],
  recentReferencedFiles: () => [],
})

const emit = defineEmits<{
  close: []
  'add-attached': [path: string]
  upload: []
}>()

const { t } = useI18n()
const { recentShares, fetchRecentShares } = useShareIn()
const { recentUploads, fetchRecentUploads } = useUploadRecent()

const activeTab = ref('current')

const tabs = [
  { key: 'current', label: '' },
  { key: 'references', label: '' },
  { key: 'shares', label: '' },
  { key: 'uploads', label: '' },
]

// Lazy-init tab labels
tabs[0].label = t('chat.attach.currentTab')
tabs[1].label = t('chat.attach.recentReferences')
tabs[2].label = t('chat.attach.recentShares')
tabs[3].label = t('chat.attach.recentUploads')

function isAttached(path: string) {
  return props.attachedFiles?.includes(path) ?? false
}

// Effective current dir: always show something (even at project root)
const effectiveCurrentDir = computed(() => props.currentDir || '.')
const currentDirDisplayName = computed(() => {
  const dir = effectiveCurrentDir.value
  return dir === '.' ? '/' : baseName(dir)
})

function formatModTime(iso: string) {
  if (!iso) return ''
  const d = new Date(iso)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h`
  const diffDay = Math.floor(diffHr / 24)
  if (diffDay < 7) return `${diffDay}d`
  return `${d.getMonth() + 1}/${d.getDate()}`
}

function handleUpload() {
  emit('upload')
}

// Fetch data when drawer opens
watch(() => props.open, (v) => {
  if (v) {
    fetchRecentShares()
    fetchRecentUploads()
  }
})
</script>

<style>
.ad-header {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}
.ad-upload-btn {
  margin-left: auto;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  border: none;
  background: var(--bg-hover);
  color: var(--text-secondary);
  cursor: pointer;
}
.ad-upload-btn:active {
  background: var(--accent-color);
  color: #fff;
}

/* Tab bar */
.ad-tab-bar {
  display: flex;
  gap: 0;
  padding: 0 12px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border-color);
  -webkit-overflow-scrolling: touch;
}
.ad-tab {
  padding: 8px 14px;
  font-size: 13px;
  font-weight: 500;
  border: none;
  background: none;
  color: var(--text-muted);
  cursor: pointer;
  white-space: nowrap;
  border-bottom: 2px solid transparent;
  transition: color 0.15s, border-color 0.15s;
}
.ad-tab-active {
  color: var(--accent-color);
  border-bottom-color: var(--accent-color);
}

/* Content area */
.ad-content {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}
.ad-empty {
  padding: 24px 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
}

/* File row */
.ad-file-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  width: 100%;
  border: none;
  background: none;
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
  transition: background 0.1s;
}
.ad-file-row:active {
  background: var(--bg-hover);
}
.ad-file-attached {
  opacity: 0.45;
}
.ad-file-icon {
  flex-shrink: 0;
  color: var(--text-muted);
}
.ad-file-info {
  flex: 1;
  min-width: 0;
  overflow: hidden;
}
.ad-file-name {
  display: block;
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ad-file-meta {
  display: block;
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
}
.ad-file-check {
  flex-shrink: 0;
  color: var(--accent-color);
  visibility: hidden;
}
.ad-file-attached .ad-file-check {
  visibility: visible;
}

/* Current item label */
.ad-label {
  display: inline-block;
  font-size: 10px;
  font-weight: 600;
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  padding: 1px 5px;
  border-radius: 3px;
  margin-right: 6px;
  vertical-align: middle;
  letter-spacing: 0.3px;
}
.ad-current-item {
  background: var(--bg-secondary);
}
</style>
