<template>
  <ModalDialog :open="open" :title="t('projectDialog.title')" :z-index="2500" full-height @close="$emit('close')">
    <template #header>
      <Folder :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ t('projectDialog.title') }}</span>
    </template>
    <!-- Browse nav -->
    <div class="dialog-nav">
      <div class="dialog-toolbar-row">
        <button v-if="!isRootLevel" class="toolbar-btn" @click="doNewFolder" :title="t('projectDialog.newFolder')">
          <FolderPlus :size="16" />
        </button>
        <button class="toolbar-btn" @click="showHidden = !showHidden" :title="showHidden ? t('projectDialog.hideHiddenFiles') : t('projectDialog.showHiddenFiles')">
          <EyeOff v-if="!showHidden" :size="16" />
          <Eye v-else :size="16" />
        </button>
        <button class="toolbar-btn" @click="loadBrowse" :title="t('nav.refresh')">
          <RotateCw :size="16" />
        </button>
        <SearchInput v-model="searchQuery" :placeholder="t('projectDialog.search')" />
      </div>
      <DirBreadcrumb :path="browsePath === '/' ? '' : browsePath" @navigate="onBreadcrumbNavigate" />
    </div>

    <!-- Content -->
    <div class="dialog-content">
      <div v-if="loading" class="dialog-loading">{{ t('common.loading') }}</div>
      <div v-else-if="displayItems.length === 0" class="dialog-empty">{{ searchQuery ? t('projectDialog.noMatchDirs') : t('projectDialog.emptyDir') }}</div>
      <div
        v-else
        v-for="item in displayItems"
        :key="item.path"
        class="dialog-item"
        :class="{ selected: selectedPath === item.path }"
        @click="enterDir(item)"
      >
        <Folder :size="28" class="item-icon-svg" />
        <span class="item-name">{{ item.name }}</span>
        <template v-if="!isRootLevel">
          <button class="item-action-btn" @click.stop="doRename(item)" :title="t('common.rename')">
            <Pencil :size="14" />
          </button>
          <button class="item-action-btn danger" @click.stop="doDelete(item)" :title="t('common.delete')">
            <Trash2 :size="14" />
          </button>
        </template>
      </div>
    </div>

    <template #footer>
      <button class="cancel-btn" @click="$emit('close')">{{ t('common.cancel') }}</button>
      <button class="confirm-btn" @click="confirm">
        <span>{{ t('common.confirm') }}</span>
      </button>
    </template>
  </ModalDialog>
</template>

<script setup>
import { Folder, FolderPlus, Eye, EyeOff, Pencil, Trash2, RotateCw } from 'lucide-vue-next'
import { ref, computed, watch, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from './common/ModalDialog.vue'
import SearchInput from './common/SearchInput.vue'
import DirBreadcrumb from './file/DirBreadcrumb.vue'
import { baseName } from '@/utils/path.ts'
import { useDialog } from '@/composables/useDialog.ts'

const { t } = useI18n()
const dialog = useDialog()

const props = defineProps({
  open: Boolean,
})
const emit = defineEmits(['close'])
const toast = inject('toast', null)
const hotSwitchProject = inject('hotSwitchProject', null)

const loading = ref(false)
const selectedPath = ref('')
const searchQuery = ref('')
const showHidden = ref(false)

// Browse state
const browsePath = ref('/')
const browseItems = ref([])
const currentBrowseAbs = ref('')

// Detect the separator used by the current base path (backslash on Windows, forward slash on Unix)
function pathSep(base) {
    return base && base.includes('\\') ? '\\' : '/'
}

// Join a base path and a name using the same separator as the base
function pathJoin(base, name) {
    if (!base) return name
    const sep = pathSep(base)
    return base.replace(/[/\\]$/, '') + sep + name
}

// Reload data when dialog opens (only first time)
let initialized = false

watch(() => props.open, (isOpen) => {
    if (isOpen) {
        searchQuery.value = ''
        if (!initialized) {
            initialized = true
            loadBrowse()
        }
    }
})

function onBreadcrumbNavigate(path) {
  if (!path) {
    // Navigate to root
    browseNavigate('')
  } else if (/^[A-Za-z]:/.test(path)) {
    // Windows path from breadcrumb — normalize forward slashes to backslashes
    browseNavigate(path.replace(/\//g, '\\'))
  } else {
    // Unix: reconstruct absolute path by prepending "/" (guard against double slash)
    browseNavigate(path.startsWith('/') ? path : '/' + path)
  }
}

// Whether we're at the root/drive-list level (no parent directory above).
// On Windows this is the drive letter listing; on Unix this is "/".
const isRootLevel = computed(() => !currentBrowseAbs.value)

const displayItems = computed(() => {
    const q = searchQuery.value.trim().toLowerCase()
    let dirs = browseItems.value.filter(d => !q || d.name.toLowerCase().includes(q))
    if (!showHidden.value) dirs = dirs.filter(d => !d.name.startsWith('.'))
    return dirs.map(d => {
        const name = d.name
        // Build absolute path for the directory entry using correct separator
        const absBase = currentBrowseAbs.value || ''
        const path = absBase ? pathJoin(absBase, name) : name
        return { name, path }
    })
})

function enterDir(item) {
    browseNavigate(item.path)
}

function browseNavigate(path) {
    browsePath.value = path
    selectedPath.value = path
    loadBrowse()
}

async function loadBrowse() {
    loading.value = true
    searchQuery.value = ''
    try {
        const rawPath = browsePath.value
        const apiPath = (!rawPath || rawPath === '/') ? '' : rawPath
        const resp = await fetch('/api/projects?path=' + encodeURIComponent(apiPath))
        const data = await resp.json()
        // data.path is "" at the Windows drive-list level, or an absolute path otherwise.
        // Preserve "" as-is so displayItems knows we're at root level (no pathJoin).
        currentBrowseAbs.value = data.path ?? ''
        browsePath.value = currentBrowseAbs.value || '/'
        browseItems.value = (data.items || []).filter(i => i.type === 'dir')
    } catch (_) {
        browseItems.value = []
        if (toast) toast.show(t('projectDialog.loadFailed'), { icon: '⚠️', type: 'error', duration: 5000 })
    } finally {
        loading.value = false
    }
}

async function doNewFolder() {
    const name = await dialog.prompt(t('projectDialog.promptFolderName'))
    if (!name || !name.trim()) return
    const dir = browsePath.value
    try {
        const resp = await fetch('/api/projects', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: dir, name: name.trim() })
        })
        if (resp.ok) await loadBrowse()
        else await dialog.alert(t('projectDialog.createFailed'))
    } catch (_) { await dialog.alert(t('projectDialog.createFailed')) }
}

async function doRename(item) {
    const newName = await dialog.prompt(t('projectDialog.promptNewName'), { value: item.name })
    if (!newName || newName === item.name) return
    try {
        const resp = await fetch('/api/file/rename', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: pathJoin(currentBrowseAbs.value, item.name), name: newName })
        })
        if (resp.ok) await loadBrowse()
        else {
            const err = await resp.json()
            await dialog.alert(t('projectDialog.renameFailedDetail', { error: err.error || '' }))
        }
    } catch (_) { await dialog.alert(t('projectDialog.renameFailed')) }
}

async function doDelete(item) {
    if (!await dialog.confirm(t('projectDialog.confirmDelete', { name: item.name }), { dangerous: true })) return
    try {
        const resp = await fetch('/api/file/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: pathJoin(currentBrowseAbs.value, item.name) })
        })
        if (resp.ok) {
            selectedPath.value = ''
            await loadBrowse()
        } else {
            const err = await resp.json()
            await dialog.alert(t('projectDialog.deleteFailedDetail', { error: err.error || '' }))
        }
    } catch (_) { await dialog.alert(t('projectDialog.deleteFailed')) }
}

async function confirm() {
    let path = selectedPath.value
    if (!path && currentBrowseAbs.value) {
        path = currentBrowseAbs.value
    }
    if (!path) return
    try {
        if (hotSwitchProject) {
            emit('close')
            await hotSwitchProject(path)
        } else {
            // Fallback: legacy full reload
            const resp = await fetch('/api/project', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path })
            })
            if (resp.ok) {
                window.location.reload()
            } else {
                const text = await resp.text()
                let msg = text
                try { msg = JSON.parse(text).error || msg } catch (_) {}
                await dialog.alert(t('projectDialog.setProjectFailedDetail', { error: msg }))
            }
        }
    } catch (err) {
        await dialog.alert(t('projectDialog.setProjectFailedDetail', { error: err.message }))
    }
}
</script>

<style scoped>
/* Browse nav */
.dialog-nav {
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  background: var(--bg-tertiary, #f5f5f5);
}

.dialog-toolbar-row {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 10px 3px;
}

.dialog-toolbar-row :deep(.search-pill) {
    margin-left: auto;
    min-width: 80px;
    max-width: 200px;
}

.dialog-nav :deep(.dir-breadcrumb) {
  padding: 0 10px 3px;
}

/* Toolbar buttons */
.toolbar-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  cursor: pointer;
  transition: all 0.15s;
  flex-shrink: 0;
}
.toolbar-btn svg {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}
.toolbar-btn:hover {
  background: var(--bg-secondary, #e0e0e0);
  color: var(--accent-color, #0066cc);
}
.toolbar-btn:disabled { opacity: 0.35; cursor: not-allowed; }

/* Content */
.dialog-content {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
  min-height: 200px;
}

.dialog-item {
  display: flex;
  align-items: center;
  padding: 6px 8px;
  min-height: 44px;
  cursor: pointer;
  gap: 8px;
  transition: background 0.1s;
}
.dialog-item + .dialog-item {
  border-top: 1px solid var(--border-color, #e5e5e5);
}
.dialog-item:hover { background: var(--bg-tertiary, #f0f0f0); }
.dialog-item.selected { background: var(--accent-color, #0066cc); color: #fff; }
.dialog-item.selected .item-name { color: #fff; }

.item-icon-svg { flex-shrink: 0; width: 28px; height: 28px; color: var(--accent-color, #0066cc); }
.dialog-item.selected .item-icon-svg { color: #fff; }
.item-name { flex: 1; font-size: 13px; color: var(--text-primary, #1a1a1a); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

/* Item action buttons */
.item-action-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.15s, color 0.15s;
}
.item-action-btn:hover {
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-primary, #1a1a1a);
}
.item-action-btn.danger:hover {
  color: #dc2626;
}
.dialog-item.selected .item-action-btn {
  color: rgba(255,255,255,0.7);
}
.dialog-item.selected .item-action-btn:hover {
  background: rgba(255,255,255,0.15);
  color: #fff;
}

.dialog-empty, .dialog-loading {
  text-align: center;
  padding: 40px 20px;
  color: var(--text-muted, #999);
  font-size: 14px;
}

.cancel-btn {
  padding: 7px 14px;
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s;
  flex-shrink: 0;
}
.cancel-btn:hover { background: var(--bg-secondary); }

.confirm-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 14px;
  background: var(--accent-color, #0066cc);
  color: #fff;
  border: none;
  border-radius: var(--radius-sm, 6px);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s, opacity 0.15s;
  flex-shrink: 0;
}
.confirm-btn:hover { background: #0055aa; }
.confirm-btn:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
