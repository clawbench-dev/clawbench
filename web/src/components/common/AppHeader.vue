<template>
  <Teleport to="body">
  <header class="header">
    <!-- Logo: hidden in APP mode -->
    <img class="header-logo" src="/logo.png" alt="ClawBench">

    <div class="badge-capsule">
      <div class="project-dropdown-wrapper" ref="dropdownRef">
        <button class="project-switch-btn" @click="toggleDropdown" :title="t('appHeader.switchProject')">
          <Projector :size="12" />
          <span class="project-name">{{ projectName }}</span>
        </button>
      </div>
      <div v-if="gitBranch" class="badge-capsule-divider"></div>
      <div v-if="gitBranch" class="branch-badge" :class="{ 'branch-switch': branchAnimating }" :title="gitBranch" @click="openHistory" @animationend="branchAnimating = false">
        <GitBranch :size="12" class="branch-icon" />
        <span class="branch-name">{{ gitBranch }}</span>
      </div>
    </div>
    <Teleport to="body">
      <Transition name="dropdown">
        <div v-if="dropdownOpen" class="project-dropdown" :style="dropdownStyle" ref="dropdownPanelRef">
          <div v-if="loadingRecent" class="dropdown-loading">{{ t('common.loading') }}</div>
          <template v-else>
            <div v-if="recentItems.length === 0" class="dropdown-empty">{{ t('appHeader.noRecentProjects') }}</div>
            <div v-else class="dropdown-scroll-area">
              <div
                v-for="item in recentItems"
                :key="item.path"
                class="dropdown-item"
                :class="{ active: item.path === projectRoot }"
                @click="selectRecent(item)"
              >
                <Projector :size="14" class="item-icon" />
                <span class="item-label">{{ item.name }}</span>
                <span class="item-path" @mousedown.prevent="onPathMouseDown" @click="onPathClick">{{ item.displayPath }}</span>
              </div>
            </div>
            <div class="dropdown-divider"></div>
            <div class="dropdown-item other-item" @click="openBrowse">
              <Search :size="14" class="item-icon" />
              <span class="item-label">{{ t('appHeader.browse') }}</span>
            </div>
          </template>
        </div>
      </Transition>
    </Teleport>

    <!-- Status dot: in APP mode it doubles as server switcher, in web mode it shows connection status -->
    <button ref="statusBtnRef" class="status-toggle" @click="onStatusDotClick" :title="isAppMode ? t('login.switchServer') : t('appHeader.connectionStatus')">
      <span class="status-dot" :class="statusDotClass"></span>
    </button>

    <!-- Web mode: simple connection status popup -->
    <PopupMenu v-if="!isAppMode" v-model:show="statusMenuOpen" :target-element="statusBtnRef" :max-width="200" :max-height="120" :menu-items-count="2">
      <div class="status-menu-item">
        <span class="status-indicator" :class="statusDotClass"></span>
        <span class="status-value">{{ serverStatusLabel }}</span>
      </div>
    </PopupMenu>

    <!-- APP mode: server switcher dropdown from status dot -->
    <Teleport to="body">
      <Transition name="dropdown">
        <div v-if="isAppMode && serverDropdownOpen" class="project-dropdown" :style="serverDropdownStyle" ref="serverDropdownPanelRef">
          <!-- Current server (always first, not clickable) -->
          <div class="dropdown-item current-server-item">
            <Server :size="14" class="item-icon" />
            <span class="item-label">{{ currentServerName }}</span>
            <span class="status-indicator" :class="statusDotClass" style="margin-left:auto;"></span>
          </div>
          <div class="dropdown-divider"></div>
          <!-- Server list (exclude current server) -->
          <div v-if="otherServers.length > 0" class="dropdown-scroll-area">
            <div
              v-for="srv in otherServers"
              :key="srv.url"
              class="dropdown-item"
              @click="switchServer(srv.url)"
            >
              <Server :size="14" class="item-icon" />
              <span class="item-label">{{ formatServerHost(srv.url) }}</span>
            </div>
          </div>
          <div class="dropdown-divider"></div>
          <!-- Logout at bottom, like "Browse..." in project selector -->
          <div class="dropdown-item other-item" @click="handleLogout">
            <LogOut :size="14" class="item-icon" />
            <span class="item-label">{{ t('login.logout') }}</span>
          </div>
        </div>
      </Transition>
    </Teleport>
  </header>
  </Teleport>
</template>

<script setup>
import { Projector, Search, GitBranch, Server, LogOut } from 'lucide-vue-next'
import { ref, computed, onMounted, onUnmounted, inject, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { formatServerHost } from '@/utils/url'
import { useAppMode } from '@/composables/useAppMode'
import { useServerList } from '@/composables/useServerList'
import { baseName } from '@/utils/path.ts'
import { store } from '@/stores/app.ts'
import { setPendingManageNavigation } from '@/composables/useCommitNavigation.ts'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { getZoomedViewport, toFixedCSS } from '@/composables/useSettingsConfig'

const { t } = useI18n()
const { wsStatus } = useGlobalEvents()
const { isAppMode } = useAppMode()
const { servers: serverList, load: loadServerList } = useServerList()
const switchTab = inject('switchTab')

const props = defineProps({
    projectRoot: String,
    homeDir: String,
})
const emit = defineEmits(['openProjectDialog'])

const toast = inject('toast')
const hotSwitchProject = inject('hotSwitchProject')

// Connection status menu state (web mode only)
const statusBtnRef = ref(null)
const statusMenuOpen = ref(false)

// Status dot class for the button indicator and popup
const statusDotClass = computed(() => {
    if (wsStatus.value === 'disconnected') return 'status-dot-disconnected'
    if (wsStatus.value === 'reconnecting') return 'status-dot-reconnecting'
    return 'status-dot-connected'
})

// Simplified server status label
const serverStatusLabel = computed(() => {
    if (wsStatus.value === 'connected') return t('appHeader.serverConnected')
    if (wsStatus.value === 'reconnecting') return t('appHeader.serverReconnecting')
    return t('appHeader.serverDisconnected')
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
    if (!dropdownRef.value) return
    const rect = dropdownRef.value.getBoundingClientRect()
    dropdownStyle.value = {
        position: 'fixed',
        top: `${toFixedCSS(rect.bottom + 4)}px`,
        left: `${toFixedCSS(rect.left)}px`,
        minWidth: `${Math.max(220, rect.width)}px`,
        maxWidth: '280px',
    }
}

function toggleDropdown() {
    if (dropdownOpen.value) {
        dropdownOpen.value = false
    } else {
        loadRecentProjects()
        updateDropdownPosition()
        dropdownOpen.value = true
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
    } catch (_) {
        recentItems.value = []
    } finally {
        loadingRecent.value = false
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
            } catch (_) {}
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
    } catch (err) {
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
    dropdownOpen.value = false
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

// --- Status dot click handler (APP mode = server switcher, web mode = connection status) ---
function onStatusDotClick() {
    if (isAppMode.value) {
        toggleServerDropdown()
    } else {
        toggleStatusMenu()
    }
}

function toggleStatusMenu() {
    statusMenuOpen.value = !statusMenuOpen.value
}

// --- Server dropdown (APP mode, triggered from status dot) ---
const serverDropdownOpen = ref(false)
const serverDropdownPanelRef = ref(null)
const serverDropdownStyle = ref({})

const currentServerUrl = computed(() => window.location.origin)

const currentServerName = computed(() => formatServerHost(window.location.origin))

const otherServers = computed(() => serverList.value.filter(s => s.url !== currentServerUrl.value))

function toggleServerDropdown() {
    if (serverDropdownOpen.value) {
        serverDropdownOpen.value = false
    } else {
        loadServerList()
        updateServerDropdownPosition()
        serverDropdownOpen.value = true
    }
}

function updateServerDropdownPosition() {
    if (!statusBtnRef.value) return
    const rect = statusBtnRef.value.getBoundingClientRect()
    const vp = getZoomedViewport()
    serverDropdownStyle.value = {
        position: 'fixed',
        top: `${toFixedCSS(rect.bottom + 4)}px`,
        right: `${toFixedCSS(vp.width - rect.right)}px`,
        left: 'auto',
        minWidth: '200px',
        maxWidth: '260px',
    }
}

function switchServer(url) {
    serverDropdownOpen.value = false
    if (url === currentServerUrl.value) return
    // Use native connectToServer for pre-auth, SSL handling, and error recovery
    const srv = serverList.value.find(s => s.url === url)
    if (window.AndroidNative?.connectToServer && srv) {
        window.AndroidNative.connectToServer(url, srv.password)
    } else {
        window.location.href = url + '/'
    }
}

function handleLogout() {
    serverDropdownOpen.value = false
    // Use native showServerDialog to return to the static login page
    if (window.AndroidNative?.showServerDialog) {
        window.AndroidNative.showServerDialog()
    } else {
        window.location.href = '/login'
    }
}

// Close server dropdown on outside click
function onServerClickOutside(e) {
    if (statusBtnRef.value && statusBtnRef.value.contains(e.target)) return
    if (serverDropdownPanelRef.value && serverDropdownPanelRef.value.contains(e.target)) return
    serverDropdownOpen.value = false
}

onMounted(() => {
    document.addEventListener('click', onClickOutside)
    document.addEventListener('click', onServerClickOutside)
})

onUnmounted(() => {
    document.removeEventListener('click', onClickOutside)
    document.removeEventListener('click', onServerClickOutside)
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
    max-width: calc(100% - 44px); /* leave room for logo + status dot */
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
    color: var(--text-primary);
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
    background: transparent;
    border-color: transparent;
}

.project-switch-btn:active {
    transform: scale(0.96);
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
    color: var(--accent-color);
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

.branch-badge:active {
    transform: scale(0.96);
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
}

.branch-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    line-height: 1.4;
}

/* Connection status button / server switcher dot */
.status-toggle {
    padding: 6px;
    border: none;
    background: transparent;
    cursor: pointer;
    border-radius: var(--radius-sm);
    transition: background 0.15s;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-left: auto;
}

@media (hover: hover) {
    .status-toggle:hover {
        background: var(--bg-tertiary);
    }
}

.status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    transition: background-color 0.3s;
}

.status-dot-connected {
    background: var(--color-green, #22c55e);
}

.status-dot-reconnecting {
    background: var(--color-yellow, #eab308);
    animation: status-pulse 1.2s ease-in-out infinite;
}

.status-dot-disconnected {
    background: var(--color-red, #ef4444);
}

@keyframes status-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
}
</style>

<!-- Unscoped styles for teleported status menu content (PopupMenu uses Teleport to body, scoped styles won't reach it) -->
<style>
/* Connection status menu (teleported to body, needs unscoped styles) */
.status-menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    font-size: 12px;
    white-space: nowrap;
}

/* Status header in server dropdown (APP mode) */
.status-menu-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    font-size: 12px;
    white-space: nowrap;
    color: var(--text-muted);
}

.status-dot-connected,
.status-indicator.status-dot-connected {
    background: var(--color-green, #22c55e);
}

.status-dot-reconnecting,
.status-indicator.status-dot-reconnecting {
    background: var(--color-yellow, #eab308);
    animation: status-pulse 1.2s ease-in-out infinite;
}

.status-dot-disconnected,
.status-indicator.status-dot-disconnected {
    background: var(--color-red, #ef4444);
}

.status-indicator {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
}

.status-value {
    color: var(--text-primary, #333);
}

/* Project dropdown (teleported to body, positioned via JS) */
.project-dropdown {
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    box-shadow: 0 4px 16px rgba(0,0,0,0.1);
    z-index: 9999;
    overflow: hidden;
    padding: 3px 0;
    display: flex;
    flex-direction: column;
}

.project-dropdown .dropdown-scroll-area {
    overflow-y: auto;
    overflow-x: hidden;
    max-height: 300px;
}

.project-dropdown .dropdown-loading,
.project-dropdown .dropdown-empty {
    text-align: center;
    padding: 10px 12px;
    color: var(--text-muted);
    font-size: 12px;
}

.project-dropdown .dropdown-item {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 10px;
    cursor: pointer;
    transition: background 0.1s;
    font-size: 12px;
}

.project-dropdown .dropdown-item:hover {
    background: var(--bg-tertiary);
}

.project-dropdown .dropdown-item.current-server-item {
    cursor: default;
}

.project-dropdown .dropdown-item.current-server-item:hover {
    background: transparent;
}

.project-dropdown .dropdown-item.active {
    background: var(--accent-color);
    color: #fff;
}

.project-dropdown .dropdown-item.active .item-icon {
    color: #fff;
}

.project-dropdown .dropdown-item.active .item-path {
    color: rgba(255,255,255,0.6);
}

.project-dropdown .item-icon {
    flex-shrink: 0;
    color: var(--accent-color);
}

.project-dropdown .dropdown-item.active .item-icon {
    color: #fff;
}

.project-dropdown .item-label {
    flex-shrink: 0;
    font-weight: 500;
    white-space: nowrap;
}

.project-dropdown .item-path {
    flex: 1 1 auto;
    color: var(--text-muted);
    font-size: 11px;
    overflow-x: auto;
    overflow-y: hidden;
    white-space: nowrap;
    cursor: default;
    scrollbar-width: none;
    -ms-overflow-style: none;
}

.project-dropdown .item-path::-webkit-scrollbar {
    display: none;
}

.project-dropdown .other-item .item-icon {
    color: var(--text-secondary);
}

.project-dropdown .dropdown-divider {
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
