<template>
  <BottomSheet :open="open" auto @close="handleClose">
    <template #header>
      <FileStack :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('file.recent.title') }}</span>
    </template>

    <div class="rf-body">
      <div v-if="checking" class="rf-empty">
        {{ t('common.loading') }}
      </div>
      <div v-else-if="files.length === 0" class="rf-empty">
        {{ t('file.recent.empty') }}
      </div>
      <div v-else class="rf-results">
        <div
          v-for="entry in files"
          :key="entry.path"
          class="rf-result-item"
          :class="{ 'rf-missing': missingPaths.has(entry.path) }"
          @click="onSelect(entry)"
        >
          <div class="rf-result-icon">
            <FileIcon :path="entry.path" :size="22" />
          </div>
          <div class="rf-result-info">
            <span class="rf-result-name">{{ fileName(entry.path) }}</span>
            <span class="rf-result-path">{{ dirPath(entry.path) }}</span>
          </div>
          <button
            v-if="missingPaths.has(entry.path)"
            class="rf-remove-btn"
            :title="t('common.delete')"
            @click.stop="onRemove(entry.path)"
          >
            <X :size="14" />
          </button>
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileStack, X } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import { useRecentFiles, removeRecentFile, type RecentFileEntry } from '@/composables/useRecentFiles'
import { appLog } from '@/utils/appLog'

const TAG = 'RecentFilesDrawer'

const { t } = useI18n()

const props = defineProps<{
  open: boolean
  currentFilePath?: string | null
}>()

const emit = defineEmits<{
  close: []
  selectFile: [path: string]
}>()

const { recentFilesExcluding } = useRecentFiles()
const files = recentFilesExcluding(computed(() => props.currentFilePath ?? null))

const checking = ref(false)
const missingPaths = ref(new Set<string>())
let fetchId = 0

// When drawer opens, batch-check file existence
watch(() => props.open, async (val) => {
  if (!val) {
    missingPaths.value = new Set()
    return
  }
  const paths = files.value.map(e => e.path)
  if (paths.length === 0) return

  const currentFetch = ++fetchId
  checking.value = true
  try {
    const resp = await fetch('/api/file/batch-exists', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths }),
    })
    if (currentFetch !== fetchId) return // stale fetch
    if (resp.ok) {
      const data = await resp.json()
      const results: Record<string, string> = data.results || {}
      const missing = new Set<string>()
      for (const p of paths) {
        if (results[p] === 'none' || !results[p]) {
          missing.add(p)
        }
      }
      missingPaths.value = missing
    }
  } catch (e) {
    if (currentFetch !== fetchId) return
    appLog.w(TAG, 'batch-exists check failed:', e)
  } finally {
    if (currentFetch === fetchId) {
      checking.value = false
    }
  }
})

function handleClose() {
  emit('close')
}

function onSelect(entry: RecentFileEntry) {
  if (missingPaths.value.has(entry.path)) {
    // Click on missing entry → remove it from recent list
    removeRecentFile(entry.path)
    const updated = new Set(missingPaths.value)
    updated.delete(entry.path)
    missingPaths.value = updated
    return
  }
  handleClose()
  emit('selectFile', entry.path)
}

function onRemove(path: string) {
  removeRecentFile(path)
  const updated = new Set(missingPaths.value)
  updated.delete(path)
  missingPaths.value = updated
}

function fileName(path: string): string {
  const lastSlash = path.lastIndexOf('/')
  return lastSlash >= 0 ? path.substring(lastSlash + 1) : path
}

function dirPath(path: string): string {
  const lastSlash = path.lastIndexOf('/')
  if (lastSlash <= 0) return ''
  return path.substring(0, lastSlash)
}
</script>

<style scoped>
.rf-body {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.rf-empty {
  padding: 24px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 13px;
  flex-shrink: 0;
}

.rf-results {
  flex: 1;
  overflow-y: auto;
}

.rf-result-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  cursor: pointer;
  border-bottom: 1px solid var(--border-color, #f0f0f0);
  transition: background 0.1s;
}

.rf-result-item:hover {
  background: var(--bg-secondary, #f8f9fa);
}

.rf-result-item.rf-missing {
  opacity: 0.45;
}

.rf-result-item.rf-missing:hover {
  opacity: 0.7;
}

.rf-result-icon {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
}

.rf-result-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.rf-result-name {
  font-size: 13px;
  color: var(--text-primary, #212529);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.rf-result-path {
  font-size: 11px;
  color: var(--text-muted, #999);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.rf-remove-btn {
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

.rf-remove-btn:hover {
  background: rgba(239, 68, 68, 0.1);
  color: #ef4444;
}
</style>
