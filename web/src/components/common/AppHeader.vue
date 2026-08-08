<template>
  <Teleport to="body">
  <header class="header">
    <!-- Logo: hidden in APP mode -->
    <img class="header-logo" src="/logo-64.png" alt="ClawBench">

    <div class="badge-capsule">
      <div class="project-dropdown-wrapper" ref="dropdownRef">
        <button class="project-switch-btn" @click="toggleDropdown" :title="t('appHeader.switchProject')">
          <Projector :size="12" />
          <span class="project-name">{{ projectName }}</span>
        </button>
      </div>
      <div v-if="gitBranch" class="badge-capsule-divider"></div>
      <div v-if="gitBranch" class="branch-badge" :class="{ 'branch-switch': branchAnimating }" :title="gitBranch" @click="toggleBranchDropdown" @animationend="branchAnimating = false">
        <GitBranch :size="12" class="branch-icon" />
        <span class="branch-name">{{ gitBranch }}</span>
      </div>
      <div v-if="currentFileName || recentFilesAvailable > 0" class="badge-capsule-divider"></div>
      <button
        v-if="currentFileName || recentFilesAvailable > 0"
        class="current-file-badge"
        :class="{ 'no-file': !currentFileName }"
        :title="currentFileName || t('appHeader.noFileOpen')"
        :disabled="recentFilesAvailable === 0"
        @click="toggleFileDropdown"
      >
        <FileText :size="12" class="file-icon" />
        <span v-if="currentFileName" class="current-file-name">{{ currentFileName }}</span>
        <span v-else class="current-file-name no-file-name">{{ t('appHeader.noFileOpen') }}</span>
      </button>
    </div>
    <Teleport to="body">
      <Transition name="dropdown">
        <div v-if="dropdownOpen" class="app-menu" :style="dropdownStyle" ref="dropdownPanelRef">
          <div class="app-menu-title">{{ t('appHeader.projects') }}</div>
          <div v-if="loadingRecent" class="app-menu-message">{{ t('common.loading') }}</div>
          <template v-else>
            <div v-if="recentItems.length === 0" class="app-menu-message">{{ t('appHeader.noRecentProjects') }}</div>
            <div v-else class="app-menu-scroll">
              <div
                v-for="item in recentItems"
                :key="item.path"
                class="app-menu-item"
                :class="{ active: item.path === projectRoot }"
                @click="selectRecent(item)"
              >
                <Projector :size="14" class="item-icon" />
                <span class="item-label">{{ item.name }}</span>
                <span class="item-path" @mousedown.prevent="onPathMouseDown" @click="onPathClick">{{ item.displayPath }}</span>
              </div>
            </div>
            <div class="menu-divider"></div>
            <div class="app-menu-item other-item" @click="openBrowse">
              <Search :size="14" class="item-icon" />
              <span class="item-label">{{ t('appHeader.browse') }}</span>
            </div>
          </template>
        </div>
      </Transition>
    </Teleport>

    <!-- Recent files quick-index dropdown -->
    <Teleport to="body">
      <Transition name="dropdown">
        <div v-if="fileDropdownOpen" class="app-menu" :style="fileDropdownStyle" ref="fileDropdownPanelRef">
          <div class="app-menu-title">{{ t('appHeader.recentFiles') }}</div>
          <div v-if="recentFileEntries.length === 0" class="app-menu-message">{{ t('appHeader.noRecentFiles') }}</div>
          <div v-else class="app-menu-scroll">
            <div
              v-for="entry in recentFileEntries"
              :key="entry.path"
              class="app-menu-item"
              :class="{ active: entry.path === currentFilePath }"
              @click="selectRecentFile(entry)"
            >
              <FileIcon :path="entry.path" :size="16" class="item-icon" />
              <span class="item-label">{{ baseName(entry.path) }}</span>
              <span class="item-path">{{ dirName(entry.path) }}</span>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- Branch quick-index dropdown -->
    <Teleport to="body">
      <Transition name="dropdown">
        <div v-if="branchDropdownOpen" class="app-menu" :style="branchDropdownStyle" ref="branchDropdownPanelRef">
          <div class="app-menu-title">{{ t('appHeader.branches') }}</div>
          <div v-if="branchDropdownLoading" class="app-menu-message">{{ t('common.loading') }}</div>
          <div v-else class="app-menu-scroll">
            <div
              v-for="b in branchList"
              :key="b.name"
              class="app-menu-item"
              :class="{ active: b.name === gitBranch }"
              @click="selectBranch(b)"
            >
              <GitBranch :size="14" class="item-icon" />
              <span class="item-label">{{ b.name }}</span>
            </div>
          </div>
          <div class="menu-divider"></div>
          <div class="app-menu-item other-item" @click="openHistory">
            <Settings2 :size="14" class="item-icon" />
            <span class="item-label">{{ t('appHeader.moreBranches') }}</span>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- Server button: merged gauge + status dot. Icon color reflects connection status -->
    <button ref="serverBtnRef" class="server-toggle" :class="statusDotClass" @click="toggleResourcesMenu" :title="t('systemResources.title')">
      <Server :size="15" />
    </button>

    <!-- Server info + resources popup (both Web and APP mode) -->
    <PopupMenu v-model:show="resourcesMenuOpen" :target-element="serverBtnRef" :max-width="320" :max-height="440" :menu-items-count="10" anchor="right">
      <SystemResourcesPanel ref="resourcesPanelRef" :show-logout="isAppMode" :ws-status="wsStatus" @logout="handleLogout" />
    </PopupMenu>
  </header>
  </Teleport>
</template>

<script setup>
import { Projector, Search, GitBranch, Server, FileText, Settings2 } from 'lucide-vue-next'
import { ref, computed, onMounted, onUnmounted, inject, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { useAppMode } from '@/composables/useAppMode'
import { baseName } from '@/utils/path.ts'
import { store } from '@/stores/app.ts'
import { setPendingManageNavigation } from '@/composables/useCommitNavigation.ts'
import PopupMenu from '@/components/common/PopupMenu.vue'
import SystemResourcesPanel from '@/components/common/SystemResourcesPanel.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import { useRecentFiles } from '@/composables/useRecentFiles'
import { useDialog } from '@/composables/useDialog.ts'
import { apiGet, apiPost } from '@/utils/api'
import { toFixedCSS } from '@/composables/useSettingsConfig'

const { t } = useI18n()
const { wsStatus } = useGlobalEvents()
const { isAppMode } = useAppMode()
const switchTab = inject('switchTab')

const props = defineProps({
    projectRoot: String,
    homeDir: String,
    currentFileName: String,
    currentFilePath: String,
    recentFilesAvailable: { type: Number, default: 0 },
})
const emit = defineEmits(['openProjectDialog', 'selectRecentFile'])

const { entries: recentFileEntries } = useRecentFiles()

// Recent files quick-index dropdown state
const fileDropdownOpen = ref(false)
const fileDropdownPanelRef = ref(null)
const fileDropdownStyle = ref({})

function toggleFileDropdown() {
    if (fileDropdownOpen.value) {
        fileDropdownOpen.value = false
        return
    }
    branchDropdownOpen.value = false
    dropdownOpen.value = false
    fileDropdownOpen.value = true
    deferPosition(updateFileDropdownPosition)
}

function updateFileDropdownPosition() {
    const el = document.querySelector('.current-file-badge')
    positionDropdown(el, fileDropdownPanelRef, fileDropdownStyle)
}

function selectRecentFile(entry) {
    fileDropdownOpen.value = false
    emit('selectRecentFile', entry.path)
}

function dirName(path) {
    const idx = path.lastIndexOf('/')
    return idx > 0 ? path.substring(0, idx) : ''
}

// Branch quick-index dropdown
const branchDropdownOpen = ref(false)
const branchDropdownLoading = ref(false)
const branchList = ref([])
const branchDropdownPanelRef = ref(null)
const branchDropdownStyle = ref({})

function toggleBranchDropdown() {
    if (branchDropdownOpen.value) {
        branchDropdownOpen.value = false
        return
    }
    fileDropdownOpen.value = false
    dropdownOpen.value = false
    branchDropdownOpen.value = true
    loadBranches()
    deferPosition(updateBranchDropdownPosition)
}

function updateBranchDropdownPosition() {
    const el = document.querySelector('.branch-badge')
    positionDropdown(el, branchDropdownPanelRef, branchDropdownStyle)
}

async function loadBranches() {
    branchDropdownLoading.value = true
    try {
        const data = await apiGet('/api/git/branches')
        branchList.value = data.branches || []
    } catch {
        branchList.value = []
    } finally {
        branchDropdownLoading.value = false
        // Re-center after content settles (loading state → loaded list changes width)
        if (branchDropdownOpen.value) {
            deferPosition(updateBranchDropdownPosition)
        }
    }
}

async function selectBranch(b) {
    branchDropdownOpen.value = false
    if (b.name === gitBranch.value) return
    const ok = await dialog.confirm(
        t('appHeader.switchBranchConfirm', { branch: b.name }),
        { title: t('git.manage.switchBranch'), confirmText: t('common.confirm'), cancelText: t('common.cancel') },
    )
    if (!ok) return
    try {
        const result = await apiPost('/api/git/checkout', { branch: b.name })
        if (result.success) {
            await store.loadGitBranch()
            await store.loadFiles(store.state.currentDir)
        } else if (result.error === 'dirty_worktree') {
            toast?.show(t('appHeader.branchDirtyWorktree'), { icon: '⚠️', type: 'error', duration: 3000 })
            openHistory()
        } else if (result.error) {
            toast?.show(t('appHeader.switchBranchFailed', { error: result.errorDetail || result.error }), { icon: '⚠️', type: 'error', duration: 3000 })
        }
    } catch {
        toast?.show(t('appHeader.switchBranchFailed', { error: t('appHeader.switchBranchNetworkError') }), { icon: '⚠️', type: 'error', duration: 3000 })
    }
}

/**
 * Position a dropdown after the current render + a double rAF so that
 * the panel's content width/layout is stable before measuring it. Without
 * this, the panel width read on first open (e.g. while branch/project lists
 * are still loading) differs from the settled width, causing the horizontally
 * centered position to jump between opens.
 */
function deferPosition(update) {
    requestAnimationFrame(() => {
        requestAnimationFrame(() => {
            update()
        })
    })
}

function positionDropdown(anchorEl, panelRef, styleRef) {
    if (!anchorEl || !panelRef?.value) return
    const anchorRect = anchorEl.getBoundingClientRect()
    const margin = 8
    const vw = window.innerWidth
    const vh = window.innerHeight

    // Panel width must never exceed the viewport (accounting for margins).
    // Hard-capped at 320px, but shrinks on narrow screens so content can't overflow.
    const maxPanelWidth = Math.min(320, vw - 2 * margin)
    // Use the measured width, clamped to the safe max for position math.
    const panelWidth = Math.min(panelRef.value.offsetWidth || maxPanelWidth, maxPanelWidth)
    const panelHeight = panelRef.value.offsetHeight || 320

    // Center horizontally on the anchor, then clamp so the panel stays in view
    let left = anchorRect.left + anchorRect.width / 2 - panelWidth / 2
    left = Math.max(margin, Math.min(vw - panelWidth - margin, left))

    let top = anchorRect.bottom + 4
    // Flip upward if there isn't room below
    if (top + panelHeight > vh - margin) {
        top = Math.max(margin, anchorRect.top - panelHeight - 4)
    }
    top = Math.max(margin, Math.min(vh - margin - panelHeight, top))

    styleRef.value = {
        position: 'fixed',
        top: `${toFixedCSS(top)}px`,
        left: `${toFixedCSS(left)}px`,
        minWidth: `${Math.min(Math.max(180, anchorRect.width), maxPanelWidth)}px`,
        maxWidth: `${toFixedCSS(maxPanelWidth)}px`,
    }
}

const dialog = useDialog()

const toast = inject('toast')
const hotSwitchProject = inject('hotSwitchProject')

// Status color class for the Server icon
const statusDotClass = computed(() => {
    if (wsStatus.value === 'disconnected') return 'status-dot-disconnected'
    if (wsStatus.value === 'reconnecting') return 'status-dot-reconnecting'
    return 'status-dot-connected'
})

const projectName = computed(() => {
    if (!props.projectRoot) return t('appHeader.selectProject')
    return baseName(props.projectRoot) || props.projectRoot
})

// Git branch
const gitBranch = computed(() => store.state.gitBranch)
const branchAnimating = ref(false)

// Trigger animation when branch changes (skip initial value)
watch(gitBranch, (newVal, oldVal) => {
    if (oldVal !== undefined && newVal !== oldVal) {
        branchAnimating.value = false
        nextTick(() => { branchAnimating.value = true })
    }
})

function openHistory() {
    branchDropdownOpen.value = false
    setPendingManageNavigation()
    switchTab?.('history')
}

// Refresh branch when project changes
watch(() => props.projectRoot, (newRoot) => {
    if (newRoot) store.loadGitBranch()
}, { immediate: true })

// Dropdown state
const dropdownOpen = ref(false)
const dropdownRef = ref(null)
const dropdownPanelRef = ref(null)
const loadingRecent = ref(false)
const recentItems = ref([])

// Dynamic dropdown positioning (teleported to body, needs fixed positioning)
const dropdownStyle = ref({})

function updateDropdownPosition() {
    const el = document.querySelector('.project-switch-btn')
    positionDropdown(el, dropdownPanelRef, dropdownStyle)
}

function toggleDropdown() {
    if (dropdownOpen.value) {
        dropdownOpen.value = false
    } else {
        fileDropdownOpen.value = false
        branchDropdownOpen.value = false
        loadRecentProjects()
        dropdownOpen.value = true
        deferPosition(updateDropdownPosition)
    }
}

async function loadRecentProjects() {
    loadingRecent.value = true
    try {
        const resp = await fetch('/api/recent-projects')
        const paths = await resp.json()
        recentItems.value = paths.map(p => {
            const name = baseName(p)
            // Display relative to home directory for cleaner paths
            // Normalize separators for comparison (Windows uses backslashes)
            const homeDir = props.homeDir || ''
            const normHome = homeDir.replace(/\\/g, '/')
            const normP = p.replace(/\\/g, '/')
            const displayPath = (normHome && normP.startsWith(normHome + '/'))
                ? p.slice(homeDir.length + 1)
                : p
            return { name, path: p, displayPath }
        })
    } catch {
        recentItems.value = []
    } finally {
        loadingRecent.value = false
        // Re-center after content settles (loading → loaded list changes width)
        if (dropdownOpen.value) {
            deferPosition(updateDropdownPosition)
        }
    }
}

async function selectRecent(item) {
    dropdownOpen.value = false
    if (item.path === props.projectRoot) return
    try {
        if (hotSwitchProject) {
            await hotSwitchProject(item.path)
        } else {
            // Fallback: legacy full reload
            const resp = await fetch('/api/project', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: item.path })
            })
            if (resp.ok) {
                window.location.reload()
                return
            }
            const text = await resp.text()
            let msg = text
            let msgKey = ''
            try {
                const parsed = JSON.parse(text)
                msg = parsed.error || msg
                msgKey = parsed.msgKey || ''
            } catch {}
            if (msgKey === 'NotADirectory') {
                toast?.show(t('appHeader.projectPathNotFound'), { icon: '⚠️', type: 'error', duration: 3000 })
                fetch('/api/recent-projects', {
                    method: 'DELETE',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ path: item.path })
                }).catch(() => {})
                recentItems.value = recentItems.value.filter(r => r.path !== item.path)
            } else {
                toast?.show(t('appHeader.switchProjectFailed', { error: msg }), { icon: '⚠️', type: 'error', duration: 3000 })
            }
        }
    } catch {
        toast?.show(t('appHeader.switchProjectNetworkError'), { icon: '⚠️', type: 'error', duration: 3000 })
    }
}

function openBrowse() {
    dropdownOpen.value = false
    emit('openProjectDialog')
}

// Close dropdown on outside click
function onClickOutside(e) {
    if (dropdownRef.value && dropdownRef.value.contains(e.target)) return
    if (dropdownPanelRef.value && dropdownPanelRef.value.contains(e.target)) return
    if (fileDropdownPanelRef.value && fileDropdownPanelRef.value.contains(e.target)) return
    if (branchDropdownPanelRef.value && branchDropdownPanelRef.value.contains(e.target)) return
    const currentFileBadge = document.querySelector('.current-file-badge')
    if (currentFileBadge && currentFileBadge.contains(e.target)) return
    const branchBadge = document.querySelector('.branch-badge')
    if (branchBadge && branchBadge.contains(e.target)) return
    dropdownOpen.value = false
    fileDropdownOpen.value = false
    branchDropdownOpen.value = false
}

// Track whether the path element was dragged, so click can decide to bubble or not
let pathDragged = false

function onPathMouseDown(e) {
    const el = e.currentTarget
    pathDragged = false
    if (el.scrollWidth <= el.clientWidth) return
    let startX = e.pageX
    let scrollLeft = el.scrollLeft

    function onMouseMove(ev) {
        const dx = ev.pageX - startX
        if (Math.abs(dx) > 2) pathDragged = true
        el.scrollLeft = scrollLeft - dx
    }
    function onMouseUp() {
        document.removeEventListener('mousemove', onMouseMove)
        document.removeEventListener('mouseup', onMouseUp)
    }
    document.addEventListener('mousemove', onMouseMove)
    document.addEventListener('mouseup', onMouseUp)
}

function onPathClick(e) {
    if (pathDragged) {
        e.stopPropagation()
    }
    // If not dragged, let the click bubble up to the parent .dropdown-item's selectRecent
}

// --- Logout (APP mode) ---
function handleLogout() {
    resourcesMenuOpen.value = false
    if (window.AndroidNative?.showServerDialog) {
        window.AndroidNative.showServerDialog()
    } else {
        window.location.href = '/login'
    }
}

// --- System resources monitor (Server icon button) ---
const serverBtnRef = ref(null)
const resourcesMenuOpen = ref(false)
const resourcesPanelRef = ref(null)

function toggleResourcesMenu() {
    resourcesMenuOpen.value = !resourcesMenuOpen.value
}

watch(resourcesMenuOpen, (open) => {
    if (open) {
        // Use nextTick because PopupMenu uses v-if — the panel component
        // doesn't exist in DOM until after the next render cycle
        nextTick(() => {
            resourcesPanelRef.value?.startPolling?.()
        })
    } else {
        resourcesPanelRef.value?.stopPolling?.()
    }
})

onMounted(() => {
    document.addEventListener('click', onClickOutside)
})

onUnmounted(() => {
    document.removeEventListener('click', onClickOutside)
})
</script>

<style scoped>
.header-logo {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    flex-shrink: 0;
}

/* Badge capsule: combines project + branch into one pill shape */
.badge-capsule {
    display: flex;
    align-items: center;
    border: 1px solid var(--border-color);
    background: var(--bg-secondary);
    border-radius: 999px;
    flex: 0 1 auto;
    min-width: 0;
    max-width: calc(100% - 50px); /* leave room for logo + server button */
    transition: background 0.15s, border-color 0.15s;
}

.badge-capsule:hover {
    background: var(--bg-primary);
    border-color: var(--text-muted);
}

/* Divider between project and branch inside capsule */
.badge-capsule-divider {
    width: 1px;
    align-self: stretch;
    background: var(--border-color);
    flex-shrink: 0;
}

.project-dropdown-wrapper {
    position: relative;
    flex: 0 1 auto;
    min-width: 0;
}

.project-switch-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    height: 24px;
    border: none;
    background: transparent;
    cursor: pointer;
    color: #fff;
    border-radius: 0;
    font-size: 12px;
    font-weight: 500;
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    transition: background 0.15s, border-color 0.15s;
    line-height: 1;
}

.project-switch-btn:hover {
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    border-color: transparent;
}

.project-switch-btn svg:first-child {
    color: var(--accent-color);
    flex-shrink: 0;
}

.project-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    line-height: 1.4;
    color: #fff;
}

/* Branch badge */
.branch-badge {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    height: 24px;
    background: transparent;
    border: none;
    border-radius: 0;
    font-size: 12px;
    font-weight: 500;
    color: #fff;
    flex: 0 1 auto;
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
    line-height: 1;
}

.branch-badge:hover {
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    border-color: transparent;
}

/* Current file capsule — third segment of the badge capsule */
.current-file-badge {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    height: 24px;
    background: transparent;
    border: none;
    border-radius: 0;
    font-size: 12px;
    font-weight: 500;
    color: #fff;
    flex: 0 1 auto;
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    cursor: pointer;
    transition: background 0.15s, color 0.3s;
    line-height: 1;
}

.current-file-badge:hover {
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    border-color: transparent;
}

.current-file-badge:disabled {
    cursor: default;
    opacity: 0.5;
    transform: none;
}

.current-file-badge .file-icon {
    flex-shrink: 0;
    color: var(--accent-color);
}

.current-file-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    line-height: 1.4;
    color: #fff;
}

/* Empty state text when no file is open — visually distinct from a real file name */
.current-file-badge .no-file-name {
    color: color-mix(in srgb, #fff 55%, transparent);
    font-weight: 400;
}

/* Branch switch animation — pulse + glow on the capsule */
.badge-capsule:has(.branch-switch) {
    animation: branch-pulse 0.6s cubic-bezier(0.34, 1.56, 0.64, 1);
}

@keyframes branch-pulse {
    0% {
        transform: scale(1);
        box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-color) 50%, transparent);
    }
    30% {
        transform: scale(1.18);
        box-shadow: 0 0 12px 3px color-mix(in srgb, var(--accent-color) 40%, transparent);
        border-color: var(--accent-color);
    }
    60% {
        transform: scale(0.95);
        box-shadow: 0 0 6px 1px color-mix(in srgb, var(--accent-color) 20%, transparent);
    }
    100% {
        transform: scale(1);
        box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-color) 0%, transparent);
    }
}

.branch-icon {
    flex-shrink: 0;
    color: var(--accent-color);
}

.branch-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    line-height: 1.4;
}

/* Server icon button — merged gauge + status dot */
.server-toggle {
    padding: 6px;
    border: none;
    background: transparent;
    cursor: pointer;
    border-radius: var(--radius-sm);
    transition: background 0.15s, color 0.3s;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-left: auto;
}

@media (hover: hover) {
    .server-toggle:hover {
        background: var(--bg-tertiary);
    }
}

.server-toggle.status-dot-connected {
    color: var(--accent-color);
}

.server-toggle.status-dot-reconnecting {
    color: var(--color-yellow, #eab308);
    animation: status-pulse 1.2s ease-in-out infinite;
}

.server-toggle.status-dot-disconnected {
    color: var(--color-red, #ef4444);
}

@keyframes status-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
}
</style>

<!-- Unscoped styles for teleported dropdown content (scoped styles won't reach it) -->
<style>
/* Unified dropdown menu (project / recent files / branches — teleported to body) */
.app-menu {
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    box-shadow: 0 4px 16px rgba(0,0,0,0.1);
    z-index: 9999;
    overflow: hidden;
    max-width: calc(100vw - 16px);
    padding: 3px 0;
    display: flex;
    flex-direction: column;
}

.app-menu-title {
    padding: 5px 12px 4px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.app-menu-scroll {
    overflow-y: auto;
    overflow-x: hidden;
    max-height: 300px;
}

.app-menu-message {
    text-align: center;
    padding: 10px 12px;
    color: var(--text-muted);
    font-size: 12px;
}

.app-menu-item {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 10px;
    cursor: pointer;
    transition: background 0.1s;
    font-size: 12px;
}

.app-menu-item:hover {
    background: var(--bg-tertiary);
}

.app-menu-item.active {
    background: var(--accent-color);
    color: #fff;
}

.app-menu-item.active .item-icon,
.app-menu-item.active .item-path {
    color: rgba(255,255,255,0.6);
}

.app-menu-item .item-icon {
    flex-shrink: 0;
    color: var(--accent-color);
}

.app-menu-item.active .item-icon {
    color: #fff;
}

.app-menu-item .item-label {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
}

.app-menu-item .item-path {
    flex: 1 1 auto;
    min-width: 0;
    color: var(--text-muted);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: default;
}

.app-menu-item.other-item .item-icon {
    color: var(--text-secondary);
}

.menu-divider {
    height: 1px;
    background: var(--border-color);
    margin: 2px 0;
}

/* Dropdown transition (teleported to body) */
.dropdown-enter-active,
.dropdown-leave-active {
    transition: opacity 0.15s, transform 0.15s;
}

.dropdown-enter-from,
.dropdown-leave-to {
    opacity: 0;
    transform: translateY(-4px);
}
</style>
