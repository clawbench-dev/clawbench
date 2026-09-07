<template>
  <BottomSheet :open="drawer.effectiveOpen.value" auto @close="drawer.close()">
    <template #header>
      <ScreenShare :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('sharedFiles.title') }}</span>
      <button
        v-if="items.length > 0 && !busy"
        class="shared-files-clear"
        :disabled="clearing"
        :title="t('sharedFiles.clearAll')"
        @click.stop="clearAll"
      >
        <RefreshCw v-if="clearing" :size="13" class="shared-files-clear-spin" />
        <Trash2 v-else :size="13" />
        {{ clearing ? t('common.loading') : t('sharedFiles.clear') }}
      </button>
    </template>

    <div class="shared-files-body">
      <!-- Loading -->
      <div v-if="busy" class="shared-files-hint">
        <LoadingIndicator size="md" />
      </div>

      <!-- Error -->
      <div v-else-if="errorMsg" class="shared-files-hint shared-files-error">
        {{ errorMsg }}
        <button class="shared-files-retry" @click="loadList">{{ t('common.retry') }}</button>
      </div>

      <!-- Empty -->
      <div v-else-if="items.length === 0" class="shared-files-hint">
        {{ t('sharedFiles.empty') }}
      </div>

      <!-- List -->
      <div v-else class="shared-files-list">
        <div
          v-for="item in items"
          :key="item.token"
          class="shared-file-row"
          :class="{ deleted: !item.exists, clickable: item.exists }"
          role="button"
          tabindex="0"
          @click="item.exists && openFile(item)"
          @keydown.enter="item.exists && openFile(item)"
        >
          <div class="shared-file-main">
            <FileIcon :path="item.name" :size="22" />
            <div class="shared-file-info">
              <div class="shared-file-name-row">
                <span class="shared-file-name" :title="item.name">{{ item.name }}</span>
                <span v-if="!item.exists" class="shared-file-badge deleted-badge">{{ t('sharedFiles.fileDeleted') }}</span>
              </div>
              <span class="shared-file-path" :title="item.path">{{ item.path }}</span>
              <span v-if="item.createdAt" class="shared-file-time">{{ item.createdAt }}</span>
            </div>
          </div>

          <div class="shared-file-actions" @click.stop>
            <a
              v-if="item.exists"
              class="shared-file-btn"
              :href="shareUrl(item)"
              target="_blank"
              rel="noopener noreferrer"
              :title="t('sharedFiles.openInNewTab')"
            >
              <ExternalLink :size="14" />
            </a>
            <button class="shared-file-btn" :title="t('sharedFiles.copyLink')" @click="copyLink(item)">
              <Copy :size="14" />
            </button>
            <button
              class="shared-file-btn danger"
              :disabled="revokingToken === item.token"
              :title="t('sharedFiles.revoke')"
              @click="revoke(item)"
            >
              <Trash2 :size="14" />
            </button>
          </div>
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { ref, watch } from 'vue'
import { ScreenShare, Copy, ExternalLink, Trash2, RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast.ts'
import { copyText } from '@/utils/clipboard.ts'
import { useFileShare } from '@/composables/useFileShare'

const emit = defineEmits(['selectFile', 'close'])

const { t } = useI18n()
const dialog = useDialog()
const toast = useToast()
const { markUnshared, resetFileShareState } = useFileShare()

// Bound to the browse tab: hides automatically when the user leaves it and
// won't auto-reopen when returning (it is an action popover, not a panel).
const drawer = useTabDrawer('browse', { autoRestore: false })

const busy = ref(false)
const errorMsg = ref('')
const items = ref([])
const revokingToken = ref('')
const clearing = ref(false)

function openDrawer() {
  drawer.open()
}

// Absolute public URL for a share link.
function shareUrl(item) {
  return window.location.origin + '/share/' + item.token
}

// Load the list whenever the drawer becomes visible (first open, or returning
// to the browse tab after autoRestore:false closed it).
watch(() => drawer.effectiveOpen.value, (isOpen) => {
  if (isOpen) void loadList()
})

async function loadList() {
  busy.value = true
  errorMsg.value = ''
  try {
    const resp = await fetch('/api/share/list')
    if (!resp.ok) throw new Error(resp.statusText)
    const data = await resp.json()
    items.value = data.shares || []
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

function openFile(item) {
  // Absolute path for out-of-project files is handled by store.selectFile
  // (external files), and App.vue switches to the view tab on success.
  emit('selectFile', item.path)
  drawer.close()
}

function copyLink(item) {
  copyText(shareUrl(item), () => {
    toast.show(t('sharedFiles.copied'), { icon: '✅', type: 'success', duration: 2000 })
  })
}

async function revoke(item) {
  const confirmed = await dialog.confirm(t('sharedFiles.confirmRevoke', { name: item.name }), { dangerous: true })
  if (!confirmed) return
  revokingToken.value = item.token
  try {
    const resp = await fetch('/api/share/list', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: item.token }),
    })
    if (!resp.ok) throw new Error(resp.statusText)
    items.value = items.value.filter(i => i.token !== item.token)
    markUnshared(item.path)
    toast.show(t('sharedFiles.revoked'), { icon: '🔗', type: 'success', duration: 2000 })
  } catch (err) {
    toast.show(err instanceof Error ? err.message : String(err), { icon: '⚠️', type: 'error', duration: 3000 })
  } finally {
    revokingToken.value = ''
  }
}

async function clearAll() {
  const confirmed = await dialog.confirm(t('sharedFiles.confirmClearAll'), { dangerous: true })
  if (!confirmed) return
  clearing.value = true
  try {
    const resp = await fetch('/api/share/list', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ all: true }),
    })
    if (!resp.ok) throw new Error(resp.statusText)
    items.value = []
    resetFileShareState()
    toast.show(t('sharedFiles.revoked'), { icon: '🔗', type: 'success', duration: 2000 })
  } catch (err) {
    toast.show(err instanceof Error ? err.message : String(err), { icon: '⚠️', type: 'error', duration: 3000 })
  } finally {
    clearing.value = false
  }
}

defineExpose({ open: openDrawer })
</script>

<style scoped>
.shared-files-body {
  display: flex;
  flex-direction: column;
  max-height: 60vh;
  overflow-y: auto;
  padding: 4px 16px 16px;
}

.shared-files-hint {
  padding: 24px 0;
  text-align: center;
  font-size: 13px;
  color: var(--text-muted, #656d76);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}

.shared-files-error { color: #cf222e; }
.shared-files-retry {
  padding: 6px 14px;
  border-radius: 6px;
  border: 1px solid var(--border-color, #dee2e6);
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  font-size: 13px;
  cursor: pointer;
}

.shared-files-list {
  display: flex;
  flex-direction: column;
}

.shared-file-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 4px;
  border-bottom: 1px solid var(--border-color, rgba(128,128,128,.15));
}
.shared-file-row:last-child { border-bottom: none; }
.shared-file-row.clickable { cursor: pointer; }
.shared-file-row.clickable:hover { background: var(--bg-tertiary, #eaeef2); }
.shared-file-row.deleted .shared-file-name,
.shared-file-row.deleted .shared-file-path {
  opacity: 0.5;
}

.shared-file-main {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.shared-file-info {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 2px;
}

.shared-file-name-row {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.shared-file-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1f2328);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.shared-file-badge {
  flex-shrink: 0;
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 8px;
  background: rgba(128,128,128,.15);
  color: var(--text-secondary, #57606a);
}
.deleted-badge { color: #cf222e; }

.shared-file-path {
  font-size: 11px;
  color: var(--text-muted, #656d76);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.shared-file-time {
  font-size: 11px;
  color: var(--text-muted, #656d76);
}

.shared-file-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}

.shared-file-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary, #57606a);
  cursor: pointer;
}
.shared-file-btn:hover { background: var(--bg-tertiary, #eaeef2); color: var(--accent-color, #0969da); }
.shared-file-btn.danger:hover { color: #cf222e; background: #fef2f2; }
.shared-file-btn:disabled { opacity: 0.4; cursor: default; }
.shared-files-clear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  height: 28px;
  padding: 0 8px;
  margin-left: auto;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: #cf222e;
  font-size: 13px;
  cursor: pointer;
  flex-shrink: 0;
}
.shared-files-clear:disabled { opacity: 0.5; cursor: default; }
.shared-files-clear-spin { animation: shared-files-clear-spin 0.8s linear infinite; }
@keyframes shared-files-clear-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
@media (hover: hover) {
  .shared-files-clear:not(:disabled):hover { background: #fef2f2; }
}

/* Dark themes: the light-pink hover surfaces above were tuned for light
   themes. Keep the red tint but adapt it to the dark surface so the icon/text
   stays legible (same convention as .context-menu-item.danger hover). */
[data-theme-base="dark"] .shared-file-btn.danger:hover {
  color: #fca5a5;
  background: rgba(239, 68, 68, 0.15);
}
[data-theme-base="dark"] .shared-files-clear {
  color: #fca5a5;
}
[data-theme-base="dark"] .shared-files-clear:not(:disabled):hover {
  color: #fca5a5;
  background: rgba(239, 68, 68, 0.15);
}
</style>
