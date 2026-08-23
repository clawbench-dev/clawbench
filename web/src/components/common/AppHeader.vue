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

    <!-- Shortcut tips marquee: fills the empty middle area (PC / web only) -->
    <ShortcutTipTicker
      v-if="isWideScreen && !isAppMode && localConfig.headerShortcutTips"
      :context="shortcutContext"
      class="header-tips"
      :title="t('appHeader.shortcutTipsDialog.openTip')"
      @click="shortcutTipsOpen = true"
    />
    <ShortcutTipsDialog :open="shortcutTipsOpen" @close="shortcutTipsOpen = false" />

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
                <button
                  class="item-remove-btn"
                  type="button"
                  :title="t('appHeader.removeProject')"
                  :aria-label="t('appHeader.removeProject')"
                  @click.stop="removeRecent(item)"
                >
                  <X :size="14" />
                </button>
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
              <button
                class="item-remove-btn"
                type="button"
                :title="t('appHeader.removeRecentFile')"
                :aria-label="t('appHeader.removeRecentFile')"
                @click.stop="removeRecentFile(entry.path)"
              >
                <X :size="14" />
              </button>
            </div>
          </div>
          <div class="menu-divider"></div>
          <div class="app-menu-item other-item" @click="openFileManager">
            <FolderOpen :size="14" class="item-icon" />
            <span class="item-label">{{ t('appHeader.openFileManager') }}</span>
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

    <!-- Dirty worktree checkout modal -->
    <Teleport to="body">
      <div v-if="dirtyModalOpen" class="ht-dirty-overlay" @click.self="dirtyModalOpen = false">
        <div class="ht-dirty-dialog">
          <div class="ht-dirty-title">
            <span class="ht-dirty-title-icon"><GitBranch :size="16" /></span>
            <span>{{ t('git.manage.switchBranch') }}</span>
          </div>
          <p class="ht-dirty-msg">{{ t('git.manage.dirty', { count: dirtyCount }) }}</p>
          <div class="ht-dirty-actions">
            <button class="ht-dirty-btn ht-dirty-stash" @click="doDirtyCheckout('stash')">{{ t('git.manage.stashSwitch') }}</button>
            <button class="ht-dirty-btn ht-dirty-force" @click="doDirtyCheckout('force')">{{ t('git.manage.forceSwitch') }}</button>
            <button class="ht-dirty-btn ht-dirty-cancel" @click="dirtyModalOpen = false">{{ t('common.cancel') }}</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Quick theme picker -->
    <button ref="themeBtnRef" class="theme-quick-toggle" :title="t('appHeader.themePicker')" :aria-label="t('appHeader.themePicker')" @click="toggleThemeMenu">
      <Palette :size="14" />
    </button>
    <PopupMenu v-model:show="themeMenuOpen" :target-element="themeBtnRef" :max-width="200" :max-height="440" :menu-items-count="1 + THEME_IDS.length" anchor="right">
      <div class="theme-picker-menu">
        <div
          v-for="opt in themeOptions"
          :key="opt.value"
          class="theme-picker-item"
          role="menuitem"
          tabindex="-1"
          :class="{ active: currentThemeValue === opt.value }"
          :style="getThemePreviewStyle(opt.value)"
          @click="selectTheme(opt.value)"
          @keydown.enter="selectTheme(opt.value)"
          @keydown.space.prevent="selectTheme(opt.value)"
        >
          <span class="theme-picker-check">{{ currentThemeValue === opt.value ? '✓' : '' }}</span>
          <span class="theme-picker-label">{{ opt.label }}</span>
          <component :is="getThemeBaseIcon(opt.value)" :size="12" class="theme-picker-base-icon" />
        </div>
      </div>
    </PopupMenu>

    <!-- Server button: merged gauge + status dot. Icon color reflects connection status -->
    <button ref="serverBtnRef" class="server-toggle" :class="[statusDotClass, { 'pressure-alert': isUnderPressure && showMetricIcon }]" @click="toggleResourcesMenu" :title="t('systemResources.title')">
      <Server v-if="!isUnderPressure || !showMetricIcon" :size="15" />
      <component v-else :is="PressureIcon" :size="15" class="pressure-icon" />
    </button>

    <!-- Server info + resources popup (both Web and APP mode) -->
    <PopupMenu v-model:show="resourcesMenuOpen" :target-element="serverBtnRef" :max-width="320" :max-height="440" :menu-items-count="10" anchor="right">
      <SystemResourcesPanel ref="resourcesPanelRef" :show-logout="isAppMode" :ws-status="wsStatus" @logout="handleLogout" />
    </PopupMenu>
  </header>
  </Teleport>
</template>

<script setup lang="ts">
import { Projector, Search, GitBranch, Server, FileText, Settings2, FolderOpen, Cpu, Activity, MemoryStick, Database, X, Palette, Sun, Moon } from 'lucide-vue-next'
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
import ShortcutTipTicker from '@/components/common/ShortcutTipTicker.vue'
import { useRecentFiles } from '@/composables/useRecentFiles'
import { useMenuKeyboard } from '@/composables/useMenuKeyboard'
import { useDialog } from '@/composables/useDialog.ts'
import { apiGet, apiPost } from '@/utils/api'
import { toFixedCSS } from '@/composables/useSettingsConfig'
import { localConfig, setLocalConfig } from '@/composables/useSettingsConfig'
import { useSystemResources } from '@/composables/useSystemResources'
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { isDarkTheme, resolveThemeId, THEME_IDS, THEME_PREVIEW_COLORS } from '@/utils/themeMeta'
import ShortcutTipsDialog from '@/components/common/ShortcutTipsDialog.vue'
import type { ShortcutContext } from '@/config/shortcutTips'
import { resolveShortcutContext } from '@/config/shortcutTips'
import type { Ref } from 'vue'

const { t } = useI18n()
const { wsStatus } = useGlobalEvents()
const { isAppMode } = useAppMode()
const { resources, startBackgroundPolling, stopBackgroundPolling } = useSystemResources()
const switchTab = inject<(tab: string) => void>('switchTab')
const { isWideScreen, leftTab, activePane } = useWideScreenLayout()
const activeTab = inject<Ref<string>>('activeTab', ref('chat'))
const shortcutContext = computed<ShortcutContext>(() =>
  resolveShortcutContext({
    isWideScreen: isWideScreen.value,
    activePane: activePane.value,
    leftTab: leftTab.value,
    activeTab: activeTab.value,
  }),
)
const shortcutTipsOpen = ref(false)

// Quick theme picker
const themeBtnRef = ref<HTMLElement | null>(null)
const themeMenuOpen = ref(false)
const currentThemeValue = computed(() => localConfig.theme || 'auto')
const autoPreviewColors = computed(() => {
  const resolved = resolveThemeId('auto')
  return THEME_PREVIEW_COLORS[resolved] ?? null
})

const themeOptions = computed(() => [
  { label: t('settings.items.themeAuto'), value: 'auto' },
  ...THEME_IDS.map(id => {
    // Map theme ID to its i18n label key, e.g. 'github-light' -> 'settings.items.themeGithubLight'
    const key = 'settings.items.theme' + id
      .split('-')
      .map(s => s.charAt(0).toUpperCase() + s.slice(1))
      .join('')
    return { label: t(key), value: id }
  }),
])

function toggleThemeMenu() {
  if (themeMenuOpen.value) {
    themeMenuOpen.value = false
    return
  }
  dropdownOpen.value = false
  fileDropdownOpen.value = false
  branchDropdownOpen.value = false
  themeMenuOpen.value = true
}

function selectTheme(value: string) {
  themeMenuOpen.value = false
  setLocalConfig('theme', value)
}

function getThemePreviewStyle(value: string) {
  if (value === 'auto') {
    const c = autoPreviewColors.value
    if (!c) return undefined
    return { '--theme-preview-bg': c.bg, '--theme-preview-fg': c.text, '--theme-preview-accent': c.accent }
  }
  const c = THEME_PREVIEW_COLORS[value]
  if (!c) return undefined
  return { '--theme-preview-bg': c.bg, '--theme-preview-fg': c.text, '--theme-preview-accent': c.accent }
}

function getThemeBaseIcon(value: string) {
  const resolved = value === 'auto' ? resolveThemeId('auto') : value
  return isDarkTheme(resolved) ? Moon : Sun
}

const props = defineProps({
    projectRoot: String,
    homeDir: String,
    currentFileName: String,
    currentFilePath: String,
    recentFilesAvailable: { type: Number, default: 0 },
})
const emit = defineEmits(['openProjectDialog', 'selectRecentFile'])

const { entries: recentFileEntries, removeRecentFile } = useRecentFiles()

// Recent files quick-index dropdown state
const fileDropdownOpen = ref(false)
const fileDropdownPanelRef = ref<HTMLElement | null>(null)
const fileDropdownStyle = ref<Record<string, string>>({})

function toggleFileDropdown() {
    if (fileDropdownOpen.value) {
        fileDropdownOpen.value = false
        return
    }
    branchDropdownOpen.value = false
    dropdownOpen.value = false
    themeMenuOpen.value = false
    fileDropdownOpen.value = true
    // Position synchronously (estimated width) so the panel never flashes at the
    // default far-left position; refine to the real width after it renders.
    updateFileDropdownPosition(true)
    deferPosition(() => updateFileDropdownPosition(false))
}

function updateFileDropdownPosition(useEstimate = false) {
    const el = document.querySelector('.current-file-badge')
    positionDropdown(el, fileDropdownPanelRef, fileDropdownStyle, useEstimate ? estimatePanelWidth(el) : undefined)
}

function selectRecentFile(entry: { path: string }) {
    fileDropdownOpen.value = false
    emit('selectRecentFile', entry.path)
}

function openFileManager() {
    fileDropdownOpen.value = false
    if (store.state.currentFile?.path) store.closeCurrentFile()
    switchTab?.('browse')
}

function dirName(path: string) {
    const idx = path.lastIndexOf('/')
    return idx > 0 ? path.substring(0, idx) : ''
}

// Branch quick-index dropdown
const branchDropdownOpen = ref(false)
const branchDropdownLoading = ref(false)
const branchList = ref<BranchEntry[]>([])
const branchDropdownPanelRef = ref<HTMLElement | null>(null)
const branchDropdownStyle = ref<Record<string, string>>({})

function toggleBranchDropdown() {
    if (branchDropdownOpen.value) {
        branchDropdownOpen.value = false
        return
    }
    fileDropdownOpen.value = false
    dropdownOpen.value = false
    themeMenuOpen.value = false
    branchDropdownOpen.value = true
    loadBranches()
    updateBranchDropdownPosition(true)
    deferPosition(() => updateBranchDropdownPosition(false))
}

function updateBranchDropdownPosition(useEstimate = false) {
    const el = document.querySelector('.branch-badge')
    positionDropdown(el, branchDropdownPanelRef, branchDropdownStyle, useEstimate ? estimatePanelWidth(el) : undefined)
}

async function loadBranches() {
    branchDropdownLoading.value = true
    try {
        const data = await apiGet('/api/git/branches') as { branches?: BranchEntry[] }
        branchList.value = data.branches || []
    } catch {
        branchList.value = []
    } finally {
        branchDropdownLoading.value = false
        // Re-center after content settles (loading state → loaded list changes width)
        if (branchDropdownOpen.value) {
            deferPosition(() => updateBranchDropdownPosition(false))
        }
    }
}

// Dirty worktree checkout state (shown at the first moment, before the confirm step)
const dirtyModalOpen = ref(false)
const dirtyBranch = ref('')
const dirtyCount = ref(0)

async function selectBranch(b: BranchEntry) {
    branchDropdownOpen.value = false
    if (b.name === gitBranch.value) return
    // Warn about a dirty worktree up front instead of only after confirming.
    if (store.state.gitDirty) {
        dirtyBranch.value = b.name
        dirtyCount.value = store.state.gitWorkingTreeChangeCount || 0
        dirtyModalOpen.value = true
        return
    }
    const ok = await dialog.confirm(
        t('appHeader.switchBranchConfirm', { branch: b.name }),
        { title: t('git.manage.switchBranch'), confirmText: t('common.confirm'), cancelText: t('common.cancel') },
    )
    if (!ok) return
    await doCheckout(b.name)
}

async function doCheckout(name: string) {
    try {
        const result = await apiPost('/api/git/checkout', { branch: name }) as { success?: boolean; error?: string; errorDetail?: string; untrackedCount?: number }
        if (result.success) {
            await store.loadGitBranch()
            await store.loadFiles(store.state.currentDir, false, 0, true)
        } else if (result.error === 'dirty_worktree') {
            // Worktree became dirty after the upfront check — surface the modal here.
            dirtyBranch.value = name
            dirtyCount.value = result.untrackedCount || store.state.gitWorkingTreeChangeCount || 0
            dirtyModalOpen.value = true
        } else if (result.error) {
            toast?.show(t('appHeader.switchBranchFailed', { error: result.errorDetail || result.error }), { icon: '⚠️', type: 'error', duration: 3000 })
        }
    } catch {
        toast?.show(t('appHeader.switchBranchFailed', { error: t('appHeader.switchBranchNetworkError') }), { icon: '⚠️', type: 'error', duration: 3000 })
    }
}

async function doDirtyCheckout(mode: 'stash' | 'force') {
    if (mode === 'force') {
        dirtyModalOpen.value = false
        const ok = await dialog.confirm(
            t('git.manage.forceSwitchConfirm'),
            { title: t('git.manage.switchBranch'), confirmText: t('common.confirm'), cancelText: t('common.cancel') },
        )
        if (!ok) return
    } else {
        dirtyModalOpen.value = false
    }
    const name = dirtyBranch.value
    dirtyBranch.value = ''
    try {
        await apiPost('/api/git/checkout', { branch: name, stash: mode === 'stash', force: mode === 'force' })
        await store.loadGitBranch()
        await store.loadFiles(store.state.currentDir, false, 0, true)
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
function deferPosition(update: () => void) {
    requestAnimationFrame(() => {
        requestAnimationFrame(() => {
            update()
        })
    })
}

/**
 * Position a dropdown centered under its anchor button. Called twice:
 *  1. Synchronously on open, with an estimated width, so the panel never
 *     renders at the browser's default (far-left) position — this is what
 *     caused the visible flash.
 *  2. After a double rAF, with the measured real width, for a precise fit.
 */
function positionDropdown(anchorEl: Element | null, panelRef: { value: HTMLElement | null }, styleRef: { value: Record<string, string> }, estimatedWidth?: number) {
    if (!anchorEl) return
    const anchorRect = anchorEl.getBoundingClientRect()
    const margin = 8
    const vw = window.innerWidth
    const vh = window.innerHeight

    // Panel width must never exceed the viewport (accounting for margins).
    const maxPanelWidth = Math.min(320, vw - 2 * margin)
    // Measured width (fall back to an estimate when the panel isn't laid out yet).
    const measured = panelRef?.value?.offsetWidth || 0
    const panelWidth = Math.min(measured || estimatedWidth || maxPanelWidth, maxPanelWidth)
    const panelHeight = panelRef?.value?.offsetHeight || 320

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

/** Estimated initial width — matches the minWidth set in positionDropdown. */
function estimatePanelWidth(anchorEl: Element | null) {
    if (!anchorEl) return 200
    const vw = window.innerWidth
    const maxPanelWidth = Math.min(320, vw - 16)
    return Math.min(Math.max(180, anchorEl.getBoundingClientRect().width), maxPanelWidth)
}

const dialog = useDialog()

const toast = inject<{ show: (msg: string, opts?: Record<string, unknown>) => void }>('toast')
const hotSwitchProject = inject<(path: string) => Promise<void>>('hotSwitchProject')

// Status color class for the Server icon
const statusDotClass = computed(() => {
    if (wsStatus.value === 'disconnected') return 'status-dot-disconnected'
    if (wsStatus.value === 'reconnecting') return 'status-dot-reconnecting'
    return 'status-dot-connected'
})

// --- System pressure alert ---
const CRITICAL_THRESHOLD = 90

type MetricKey = 'cpu' | 'memory' | 'disk' | 'load'

const metricIcons: Record<MetricKey, typeof Cpu> = { cpu: Cpu, memory: MemoryStick, disk: Database, load: Activity }

const criticalMetric = computed<MetricKey | null>(() => {
    const r = resources.value
    const cores = r.cpu.core_count || 1
    const loadPercent = (r.load.load1 / cores) * 100
    const metrics: { key: MetricKey; percent: number }[] = [
        { key: 'cpu', percent: r.cpu.percent },
        { key: 'memory', percent: r.memory.percent },
        { key: 'disk', percent: r.disk.percent },
        { key: 'load', percent: Math.min(loadPercent, 100) },
    ]
    // Filter to metrics at or above threshold
    const critical = metrics.filter(m => m.percent >= CRITICAL_THRESHOLD)
    if (critical.length === 0) return null
    // Pick the one with highest excess ratio (denominator is same, just sort by raw excess)
    critical.sort((a, b) => (b.percent - CRITICAL_THRESHOLD) - (a.percent - CRITICAL_THRESHOLD))
    return critical[0].key
})

const isUnderPressure = computed(() => criticalMetric.value !== null)

// Blinking state: toggles between Server icon and the critical metric icon
const showMetricIcon = ref(false)
let blinkTimer: ReturnType<typeof setInterval> | null = null

function startBlinking() {
    if (blinkTimer) return
    showMetricIcon.value = false
    blinkTimer = setInterval(() => {
        showMetricIcon.value = !showMetricIcon.value
    }, 1000)
}

function stopBlinking() {
    if (blinkTimer) {
        clearInterval(blinkTimer)
        blinkTimer = null
    }
    showMetricIcon.value = false
}

watch(isUnderPressure, (under) => {
    if (under) {
        startBlinking()
    } else {
        stopBlinking()
    }
}, { immediate: true })

// Pause blinking when tab is hidden, resume when visible
function onBlinkVisibilityChange() {
    if (document.hidden) {
        stopBlinking()
    } else if (isUnderPressure.value) {
        startBlinking()
    }
}

const PressureIcon = computed(() => {
    const key = criticalMetric.value
    return key ? metricIcons[key] : null
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
const dropdownRef = ref<HTMLElement | null>(null)
const dropdownPanelRef = ref<HTMLElement | null>(null)
const loadingRecent = ref(false)
interface RecentItem {
  name: string
  path: string
  displayPath: string
}

interface BranchEntry {
  name: string
}

const recentItems = ref<RecentItem[]>([])

// Dynamic dropdown positioning (teleported to body, needs fixed positioning)
const dropdownStyle = ref<Record<string, string>>({})

function updateDropdownPosition(useEstimate = false) {
    const el = document.querySelector('.project-switch-btn')
    positionDropdown(el, dropdownPanelRef, dropdownStyle, useEstimate ? estimatePanelWidth(el) : undefined)
}

function toggleDropdown() {
    if (dropdownOpen.value) {
        dropdownOpen.value = false
    } else {
        fileDropdownOpen.value = false
        branchDropdownOpen.value = false
        themeMenuOpen.value = false
        loadRecentProjects()
        dropdownOpen.value = true
        updateDropdownPosition(true)
        deferPosition(() => updateDropdownPosition(false))
    }
}

async function loadRecentProjects() {
    loadingRecent.value = true
    try {
        const resp = await fetch('/api/recent-projects')
        const paths = await resp.json()
        recentItems.value = paths.map((p: string) => {
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
            deferPosition(() => updateDropdownPosition(false))
        }
    }
}

async function selectRecent(item: RecentItem) {
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

async function removeRecent(item: RecentItem) {
    // Close the dropdown first: it is teleported with z-index 9999, higher than
    // the confirm dialog overlay (3000), so leaving it open would cover the dialog.
    dropdownOpen.value = false
    const confirmed = await dialog.confirm(
        t('appHeader.removeProjectConfirm', { name: item.name }),
        { dangerous: true },
    )
    if (!confirmed) return

    try {
        const resp = await fetch('/api/recent-projects', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: item.path }),
        })
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
        recentItems.value = recentItems.value.filter(r => r.path !== item.path)
        toast?.show(t('appHeader.projectRemoved'), { icon: '✅', type: 'success', duration: 2000 })
    } catch (error) {
        appLog.w('AppHeader', 'Failed to remove recent project', { path: item.path, error })
        toast?.show(t('appHeader.removeProjectFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
    }
}

function openBrowse() {
    dropdownOpen.value = false
    emit('openProjectDialog')
}

// Close dropdown on outside click
function onClickOutside(e: MouseEvent) {
    const target = e.target as Node | null
    if (dropdownRef.value && dropdownRef.value.contains(target)) return
    if (dropdownPanelRef.value && dropdownPanelRef.value.contains(target)) return
    if (fileDropdownPanelRef.value && fileDropdownPanelRef.value.contains(target)) return
    if (branchDropdownPanelRef.value && branchDropdownPanelRef.value.contains(target)) return
    const currentFileBadge = document.querySelector('.current-file-badge')
    if (currentFileBadge && currentFileBadge.contains(target)) return
    const branchBadge = document.querySelector('.branch-badge')
    if (branchBadge && branchBadge.contains(target)) return
    if (themeBtnRef.value && themeBtnRef.value.contains(target)) return
    dropdownOpen.value = false
    fileDropdownOpen.value = false
    branchDropdownOpen.value = false
    themeMenuOpen.value = false
}

// Track whether the path element was dragged, so click can decide to bubble or not
let pathDragged = false

function onPathMouseDown(e: MouseEvent) {
    const el = e.currentTarget as HTMLElement
    pathDragged = false
    if (el.scrollWidth <= el.clientWidth) return
    let startX = e.pageX
    let scrollLeft = el.scrollLeft

    function onMouseMove(ev: MouseEvent) {
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

function onPathClick(e: MouseEvent) {
    if (pathDragged) {
        e.stopPropagation()
    }
    // If not dragged, let the click bubble up to the parent .dropdown-item's selectRecent
}

// --- Logout (APP mode) ---
function handleLogout() {
    resourcesMenuOpen.value = false
    if (getNative()?.showServerDialog) {
        getNative()?.showServerDialog()
    } else {
        window.location.href = '/login'
    }
}

// --- System resources monitor (Server icon button) ---
const serverBtnRef = ref<HTMLElement | null>(null)
const resourcesMenuOpen = ref(false)
const resourcesPanelRef = ref<{ startPolling?: () => void; stopPolling?: () => void } | null>(null)

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
    document.addEventListener('visibilitychange', onBlinkVisibilityChange)
    startBackgroundPolling()
})

onUnmounted(() => {
    document.removeEventListener('click', onClickOutside)
    document.removeEventListener('visibilitychange', onBlinkVisibilityChange)
    stopBackgroundPolling()
    stopBlinking()
})

// Keyboard navigation (↑/↓ select, Enter confirm, Esc close) for the three
// teleported dropdowns — project switch, recent files, branch quick-index.
useMenuKeyboard({ panelRef: dropdownPanelRef, isOpen: dropdownOpen })
useMenuKeyboard({ panelRef: fileDropdownPanelRef, isOpen: fileDropdownOpen })
useMenuKeyboard({ panelRef: branchDropdownPanelRef, isOpen: branchDropdownOpen })
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

@media (hover: hover) {
  .badge-capsule:hover {
    background: var(--bg-primary);
    border-color: var(--text-muted);
  }
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

@media (hover: hover) {
  .project-switch-btn:hover {
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    border-color: transparent;
  }
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
    color: var(--text-primary);
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
    color: var(--text-primary);
    flex: 0 1 auto;
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
    line-height: 1;
}

@media (hover: hover) {
  .branch-badge:hover {
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    border-color: transparent;
  }
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
    color: var(--text-primary);
    flex: 0 1 auto;
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    cursor: pointer;
    transition: background 0.15s, color 0.3s;
    line-height: 1;
}

@media (hover: hover) {
  .current-file-badge:hover {
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    border-color: transparent;
  }
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
    color: var(--text-primary);
}

/* Empty state text when no file is open — visually distinct from a real file name */
.current-file-badge .no-file-name {
    color: color-mix(in srgb, var(--text-primary) 55%, transparent);
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

/* Quick theme picker */
.theme-quick-toggle {
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
    color: var(--accent-color);
    margin-left: auto;
}

@media (hover: hover) {
    .theme-quick-toggle:hover {
        background: var(--bg-tertiary);
        color: var(--text-primary);
    }
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
}

/* Shortcut tips fill the empty middle of the header (PC/web only) */
.header-tips {
    flex: 1;
    min-width: 0;
    margin: 0 8px;
    height: 100%;
    overflow: hidden;
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

/* Pressure alert — red metric icon during blink */
.server-toggle.pressure-alert {
    color: var(--color-red, #ef4444) !important;
}

.server-toggle .pressure-icon {
    animation: pressure-icon-enter 0.15s ease-out;
}

@keyframes pressure-icon-enter {
    from { opacity: 0; transform: scale(0.8); }
    to { opacity: 1; transform: scale(1); }
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

@media (hover: hover) {
  .app-menu-item:hover {
    background: var(--bg-tertiary);
  }
}

/* Keyboard highlight (↑/↓ navigation) — mirrors the hover state so the
   selected row stays visible even when the pointer isn't over it. */
.app-menu-item.keyboard-hover {
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

/* Selected state: give the file-type icon a background chip (file-manager style)
   so its colored glyph stays readable instead of blending into the accent row. */
.app-menu-item.active .item-icon.file-type-icon {
    background: rgba(255, 255, 255, 0.18);
    border-radius: 3px;
    padding: 1px;
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

.app-menu-item .item-remove-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
}

@media (hover: hover) {
  .app-menu-item .item-remove-btn:hover {
    color: var(--color-red, #ef4444);
    background: color-mix(in srgb, var(--color-red, #ef4444) 12%, transparent);
  }

  .app-menu-item.active .item-remove-btn:hover {
    color: #fff;
    background: rgba(255, 255, 255, 0.18);
  }
}

.app-menu-item.active .item-remove-btn {
    color: rgba(255, 255, 255, 0.75);
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

/* ─── Dirty worktree checkout modal ──────────────────────────────── */

.ht-dirty-overlay {
    position: fixed;
    inset: 0;
    z-index: 2000;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.4);
}

.ht-dirty-dialog {
    background: var(--bg-primary, #fff);
    border-radius: 12px;
    padding: 20px;
    width: min(320px, 85vw);
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
}

.ht-dirty-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 16px;
    font-weight: 600;
    color: var(--text-primary, #1a1a1a);
    margin-bottom: 8px;
}

.ht-dirty-title-icon {
    flex-shrink: 0;
    width: 24px;
    height: 24px;
    border-radius: 7px;
    color: var(--accent-color, #0066cc);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
    display: inline-flex;
    align-items: center;
    justify-content: center;
}

.ht-dirty-msg {
    font-size: 13px;
    color: var(--text-secondary, #666);
    margin: 0 0 16px;
    line-height: 1.5;
    white-space: pre-line;
    word-break: break-word;
    overflow-wrap: break-word;
}

.ht-dirty-actions {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.ht-dirty-btn {
    width: 100%;
    padding: 10px;
    border-radius: 8px;
    border: 1px solid;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    text-align: center;
    background: transparent;
    transition: opacity 0.15s;
}

.ht-dirty-btn:active {
    opacity: 0.7;
}

.ht-dirty-stash {
    border-color: var(--accent-color, #4a90d9);
    color: var(--accent-color, #4a90d9);
}

.ht-dirty-force {
    border-color: var(--danger-color, #dc3545);
    color: var(--danger-color, #dc3545);
}

.ht-dirty-cancel {
    border-color: var(--border-color, #dee2e6);
    color: var(--text-secondary, #666);
}

/* ─── Theme picker popup ─────────────────────────────────────────────── */

.theme-picker-menu {
    padding: 0;
    min-width: 160px;
}

.theme-picker-item {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 10px;
    cursor: pointer;
    transition: background 0.1s;
    font-size: 12px;
    color: var(--text-primary);
    background: var(--theme-preview-bg, transparent);
    color: var(--theme-preview-fg, var(--text-primary));
}

@media (hover: hover) {
  .theme-picker-item:hover {
    background: var(--bg-tertiary);
  }
}

.theme-picker-item.active {
    background: var(--theme-preview-bg, transparent);
    color: var(--theme-preview-fg, var(--text-primary));
}

.theme-picker-check {
    flex-shrink: 0;
    width: 16px;
    height: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 10px;
    border-radius: 50%;
}

.theme-picker-item.active .theme-picker-check {
    background: var(--accent-color);
    color: #fff;
}

.theme-picker-label {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
}

.theme-picker-base-icon {
    flex-shrink: 0;
    color: var(--theme-preview-accent, var(--text-muted));
}

.theme-picker-item.active .theme-picker-base-icon {
    color: var(--theme-preview-accent, var(--text-muted));
}
</style>
