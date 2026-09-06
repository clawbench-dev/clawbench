<template>
  <Teleport to="body">
  <header class="header">
    <!-- Logo: hidden in APP mode -->
    <img class="header-logo" src="/logo-64.png" alt="ClawBench">

    <div class="badge-capsule">
      <div class="project-dropdown-wrapper" ref="dropdownRef" :class="segmentClass('project', true)">
        <button class="project-switch-btn" @click="toggleDropdown" :title="t('appHeader.switchProject')">
          <Projector :size="12" />
          <span class="project-name">{{ projectName }}</span>
        </button>
      </div>
      <div v-if="gitBranch" class="badge-capsule-divider"></div>
      <div v-if="gitBranch" class="branch-badge" :title="gitBranch" @click="toggleBranchDropdown" :class="segmentClass('branch', true)">
        <GitBranch :size="12" class="branch-icon" />
        <span class="branch-name">{{ gitBranch }}</span>
      </div>
      <div v-if="currentFileName || recentFilesAvailable > 0" class="badge-capsule-divider"></div>
      <button
        v-if="currentFileName || recentFilesAvailable > 0"
        class="current-file-badge"
        :class="segmentClass('file', !!currentFileName)"
        :title="currentFileName || t('appHeader.noFileOpen')"
        :disabled="recentFilesAvailable === 0"
        @click="toggleFileDropdown"
      >
        <FileText :size="12" class="file-icon" />
        <span v-if="currentFileName" class="current-file-name">{{ currentFileName }}</span>
        <span v-else class="current-file-name no-file-name">{{ t('appHeader.noFileOpen') }}</span>
      </button>
    </div>

    <!-- Reveal card: shows the FULL text of a badge segment that just changed
         (the capsule only has room for a truncated name on phones). Teleported
         to body + position:fixed so it isn't clipped by the capsule/header.
         Appears under the changed segment and auto-dismisses. -->
    <Teleport to="body">
      <Transition name="reveal">
        <div
          v-if="reveal"
          class="badge-reveal"
          :style="{ left: reveal.left + 'px', top: reveal.top + 'px' }"
        >
          <span class="badge-reveal-title">
            <span class="badge-reveal-icon">
              <Projector v-if="reveal.source === 'project'" :size="13" />
              <GitBranch v-else-if="reveal.source === 'branch'" :size="13" />
              <FileText v-else :size="13" />
            </span>
            <span class="badge-reveal-name">{{ reveal.title }}</span>
          </span>
          <span v-if="reveal.subtitle" class="badge-reveal-subtitle">{{ reveal.subtitle }}</span>
        </div>
      </Transition>
    </Teleport>

    <!-- Shortcut tips marquee: fills the empty middle area (PC / web only) -->
    <ShortcutTipTicker
      v-if="isWideScreen && !isAppMode && localConfig.headerShortcutTips"
      :context="shortcutContext"
      class="header-tips"
      :title="t('appHeader.shortcutTipsDialog.openTip')"
      @click="shortcutTipsOpen = true"
    />
    <ShortcutTipsDialog :open="shortcutTipsOpen" @close="shortcutTipsOpen = false" />

    <AppMenuPanel :open="dropdownOpen" :panel-style="dropdownStyle" :register-panel="setProjectPanelRef">
      <div class="app-menu-title">{{ t('appHeader.projects') }}</div>
      <div v-if="loadingRecent" class="app-menu-message">{{ t('common.loading') }}</div>
      <template v-else>
        <div v-if="recentItems.length === 0" class="app-menu-message">{{ t('appHeader.noRecentProjects') }}</div>
        <div v-else class="app-menu-scroll">
          <div
            v-for="item in recentItems"
            :key="item.path"
            class="app-menu-item app-menu-item--stacked"
            :class="{ active: item.path === projectRoot }"
            @click="selectRecent(item)"
          >
            <Projector :size="14" class="item-icon" />
            <span class="item-body">
              <span class="item-name">{{ item.name }}</span>
              <span class="item-path">{{ item.displayPath }}</span>
            </span>
            <HintTooltip :content="item.path" />
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
    </AppMenuPanel>

    <!-- Recent files quick-index dropdown -->
    <AppMenuPanel :open="fileDropdownOpen" :panel-style="fileDropdownStyle" :register-panel="setFilePanelRef">
      <div class="app-menu-title">{{ t('appHeader.recentFiles') }}</div>
      <div v-if="recentFileEntries.length === 0" class="app-menu-message">{{ t('appHeader.noRecentFiles') }}</div>
      <div v-else class="app-menu-scroll">
        <div
          v-for="entry in recentFileEntries"
          :key="entry.path"
          class="app-menu-item app-menu-item--stacked"
          :class="{ active: entry.path === currentFilePath }"
          @click="selectRecentFile(entry)"
        >
          <FileIcon :path="entry.path" :size="16" class="item-icon" />
          <span class="item-body">
            <span class="item-name">{{ baseName(entry.path) }}</span>
            <span class="item-path">{{ dirName(entry.path) }}</span>
          </span>
          <HintTooltip :content="entry.path" />
          <button
            class="item-remove-btn"
            type="button"
            :title="t('appHeader.removeRecentFile')"
            :aria-label="t('appHeader.removeRecentFile')"
            @click.stop="removeRecentFile(entry.path)"
          >
            <X :size="12" />
          </button>
        </div>
      </div>
      <div class="menu-divider"></div>
      <div class="app-menu-item other-item" @click="openFileManager">
        <FolderOpen :size="14" class="item-icon" />
        <span class="item-label">{{ t('appHeader.openFileManager') }}</span>
      </div>
    </AppMenuPanel>

    <!-- Branch quick-index dropdown -->
    <AppMenuPanel :open="branchDropdownOpen" :panel-style="branchDropdownStyle" :register-panel="setBranchPanelRef">
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
    </AppMenuPanel>

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
      <Palette :size="18" />
    </button>
    <PopupMenu v-model:show="themeMenuOpen" :target-element="themeBtnRef" :max-width="200" :max-height="440" :menu-items-count="1 + THEME_IDS.length" anchor="right" app-surface>
      <div class="theme-picker app-menu-column">
        <div class="app-menu-title">{{ t('terminal.theme') }}</div>
        <div class="app-menu-scroll">
          <button
            v-for="opt in themeOptions"
            :key="opt.value"
            class="theme-item"
            role="menuitem"
            tabindex="-1"
            :class="{ active: currentThemeValue === opt.value }"
            :style="getThemePreviewStyle(opt.value)"
            @click="selectTheme(opt.value)"
            @keydown.enter="selectTheme(opt.value)"
            @keydown.space.prevent="selectTheme(opt.value)"
          >
            <span class="theme-item-check">{{ currentThemeValue === opt.value ? '✓' : '' }}</span>
            <span class="theme-item-name">{{ opt.label }}</span>
            <component :is="getThemeBaseIcon(opt.value)" :size="12" class="theme-item-base-icon" />
          </button>
        </div>
        <div class="menu-divider"></div>
        <div class="app-menu-item other-item" role="menuitem" tabindex="-1" @click="openMoreAppearanceOptions">
          <SlidersHorizontal :size="14" class="item-icon" />
          <span class="item-label">{{ t('appHeader.moreAppearanceOptions') }}</span>
        </div>
      </div>
    </PopupMenu>

    <!-- Server button: merged gauge + status dot. Icon color reflects connection status -->
    <button ref="serverBtnRef" class="server-toggle" :class="[statusDotClass, { 'pressure-alert': isUnderPressure && showMetricIcon }]" @click="toggleResourcesMenu" :title="t('systemResources.title')">
      <Server v-if="!isUnderPressure || !showMetricIcon" :size="18" />
      <component v-else :is="PressureIcon" :size="18" class="pressure-icon" />
    </button>

    <!-- Server info + resources popup (both Web and APP mode) -->
    <PopupMenu v-model:show="resourcesMenuOpen" :target-element="serverBtnRef" :max-width="320" :max-height="440" :menu-items-count="10" anchor="right">
      <SystemResourcesPanel ref="resourcesPanelRef" :show-logout="isAppMode" :ws-status="wsStatus" @logout="handleLogout" />
    </PopupMenu>
  </header>
  </Teleport>
</template>

<script setup lang="ts">
import { Projector, Search, GitBranch, Server, FileText, Settings2, SlidersHorizontal, FolderOpen, X, Palette, Sun, Moon } from 'lucide-vue-next'
import { ref, computed, onMounted, onUnmounted, inject, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { useAppMode } from '@/composables/useAppMode'
import { baseName, dirName } from '@/utils/path.ts'
import { store } from '@/stores/app.ts'
import { setPendingManageNavigation } from '@/composables/useCommitNavigation.ts'
import { setPendingSettingsCategory } from '@/composables/useSettingsNavigation'
import PopupMenu from '@/components/common/PopupMenu.vue'
import SystemResourcesPanel from '@/components/common/SystemResourcesPanel.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import HintTooltip from '@/components/common/HintTooltip.vue'
import ShortcutTipTicker from '@/components/common/ShortcutTipTicker.vue'
import { useRecentFiles } from '@/composables/useRecentFiles'
import { useMenuKeyboard } from '@/composables/useMenuKeyboard'
import { useDialog } from '@/composables/useDialog.ts'
import { apiGet, apiPost } from '@/utils/api'
import { localConfig, setLocalConfig } from '@/composables/useSettingsConfig'
import { useSystemResources } from '@/composables/useSystemResources'
import { useSystemPressure } from '@/composables/useSystemPressure'
import { useBadgeHighlight, type BadgeSegment } from '@/composables/useBadgeHighlight'
import { deferPosition, estimatePanelWidth as estimatePanelWidthFn, positionDropdown as positionDropdownPure } from '@/utils/dropdownPosition'
import { recentProjectDisplayPath } from '@/utils/recentProjects'
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { isDarkTheme, resolveThemeId, THEME_IDS, getThemeLabelKey, getThemePreviewColor } from '@/utils/themeMeta'
import ShortcutTipsDialog from '@/components/common/ShortcutTipsDialog.vue'
import AppMenuPanel from '@/components/common/AppMenuPanel.vue'
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
const autoPreviewColors = computed(() => getThemePreviewColor(resolveThemeId('auto')))

const themeOptions = computed(() => [
  { label: t('settings.items.themeAuto'), value: 'auto' },
  ...THEME_IDS.map(id => ({ label: t(getThemeLabelKey(id)), value: id })),
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

/** Deep-link into the full appearance settings (theme grid + fonts + UI scale).
 *  Mirrors the branch-badge → history deep-link: a module-level pending request
 *  is set first, then the settings tab is switched to. SettingsPage consumes
 *  the request whether it was already mounted or is mounted lazily by the tab. */
function openMoreAppearanceOptions() {
  themeMenuOpen.value = false
  setPendingSettingsCategory('appearance')
  switchTab?.('settings')
}

function getThemePreviewStyle(value: string) {
  const c = value === 'auto' ? autoPreviewColors.value : getThemePreviewColor(value)
  if (!c) return undefined
  return { '--tterm-preview-bg': c.bg, '--tterm-preview-fg': c.text, '--tterm-preview-accent': c.accent }
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
/** Receives the rendered recent-files panel element (from AppMenuPanel). */
function setFilePanelRef(el: HTMLElement | null) {
    fileDropdownPanelRef.value = el
}
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

// Branch quick-index dropdown
const branchDropdownOpen = ref(false)
const branchDropdownLoading = ref(false)
const branchList = ref<BranchEntry[]>([])
const branchDropdownPanelRef = ref<HTMLElement | null>(null)
/** Receives the rendered branch panel element (from AppMenuPanel). */
function setBranchPanelRef(el: HTMLElement | null) {
    branchDropdownPanelRef.value = el
}
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
 * Position a dropdown centered under its anchor button. Called twice:
 *  1. Synchronously on open, with an estimated width, so the panel never
 *     renders at the browser's default (far-left) position — this is what
 *     caused the visible flash.
 *  2. After a double rAF, with the measured real width, for a precise fit.
 * Shared pure geometry lives in @/utils/dropdownPosition.
 */
function positionDropdown(anchorEl: Element | null, panelRef: { value: HTMLElement | null }, styleRef: { value: Record<string, string> }, estimatedWidth?: number) {
    if (!anchorEl) return
    const rect = anchorEl.getBoundingClientRect()
    const panel = panelRef?.value
    positionDropdownPure(
        { left: rect.left, right: rect.right, bottom: rect.bottom, top: rect.top, width: rect.width },
        panel ? { offsetWidth: panel.offsetWidth, offsetHeight: panel.offsetHeight } : undefined,
        styleRef,
        estimatedWidth,
    )
}

/** Estimated initial width — matches the minWidth set in positionDropdown. */
function estimatePanelWidth(anchorEl: Element | null) {
    if (!anchorEl) return 200
    return estimatePanelWidthFn(anchorEl.getBoundingClientRect(), window.innerWidth)
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

// System pressure alert — derived from the shared resources snapshot; blinks
// the metric icon while under pressure (see useSystemPressure).
const {
    isUnderPressure,
    showMetricIcon,
    PressureIcon,
    onVisibilityChange: onBlinkVisibilityChange,
} = useSystemPressure({ resources })

const projectName = computed(() => {
    if (!props.projectRoot) return t('appHeader.selectProject')
    return baseName(props.projectRoot) || props.projectRoot
})

// Git branch
const gitBranch = computed(() => store.state.gitBranch)

// Badge capsule feedback — when a segment's content changes (project name,
// current file, branch) it flashes the accent highlight AND shows a floating
// "reveal card" under that segment with the complete text (the capsule itself
// is width-constrained on phones, so it can only show a truncated name).
// The highlight timing lives in useBadgeHighlight; the reveal card state is
// assembled here and consumed by the template (see BadgeReveal in template).
const prefersReducedMotion = ref(
    typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches === true,
)
let motionQuery: MediaQueryList | null = null
const onMotionPreferenceChange = () => {
    prefersReducedMotion.value = motionQuery?.matches === true
}

const {
    highlightBadge,
    pulseBadge,
    segmentClass,
    dispose: disposeBadgeHighlight,
} = useBadgeHighlight({ prefersReducedMotion })

// ── Reveal card state ──
// A small fixed card shown under the changed badge segment with the full text.
// `source` distinguishes the segment type (drives the icon/colors/labels);
// `title`/`subtitle` carry the complete display text; `left`/`top` position
// the card under the segment rect. Auto-dismiss after REVEAL_MS; a newer
// pulse replaces the card and restarts the timer.
const reveal = ref<{
    source: BadgeSegment
    title: string
    subtitle?: string
    left: number
    top: number
} | null>(null)
let revealTimer: ReturnType<typeof setTimeout> | null = null
const REVEAL_MS = 1500
const REVEAL_OFFSET_Y = 6

/** DOM node of the currently-highlighted badge segment (for card anchoring). */
function badgeSegmentEl(source: BadgeSegment): HTMLElement | null {
    return document.querySelector(
        source === 'project' ? '.project-switch-btn'
            : source === 'branch' ? '.branch-badge' : '.current-file-badge',
    )
}

/** Show the reveal card under `source`'s badge with the given text. */
function showReveal(source: BadgeSegment, title: string, subtitle?: string) {
    if (revealTimer) { clearTimeout(revealTimer); revealTimer = null }
    const place = () => {
        const el = badgeSegmentEl(source)
        if (!el) return
        const r = el.getBoundingClientRect()
        // Anchor under the segment's left edge; the card is width-capped in
        // CSS, so clamp against the right viewport edge to avoid overflow.
        const maxLeft = Math.max(4, window.innerWidth - 20)
        reveal.value = {
            source,
            title,
            subtitle,
            left: Math.max(4, Math.min(r.left, maxLeft)),
            top: r.bottom + REVEAL_OFFSET_Y,
        }
    }
    // The changed segment is usually already in the DOM (file/project/branch
    // existed before the content swap) — place it synchronously. When the
    // change itself v-if's the segment into existence (e.g. the branch badge
    // appears only once a branch is set), it is NOT queryable during the watch
    // callback (flush: 'pre', before the render) → defer one tick.
    if (!badgeSegmentEl(source)) {
        void nextTick(place)
    } else {
        place()
    }
    // Dismiss after the card has been readable; matches the visual rhythm of
    // the accent highlight flash.
    revealTimer = setTimeout(() => {
        reveal.value = null
        revealTimer = null
    }, REVEAL_MS)
}

watch(gitBranch, (newVal, oldVal) => {
    if (newVal !== oldVal) {
        pulseBadge('branch')
        if (newVal) showReveal('branch', newVal)
    }
})

watch(() => props.currentFileName, (newVal, oldVal) => {
    // Covers both file swaps AND opening a file when none was open
    // (oldVal undefined → a real name), while the non-immediate watch never
    // fires on the initial value.
    if (newVal !== oldVal) {
        pulseBadge('file')
        if (newVal) showReveal('file', newVal, props.currentFilePath ? dirName(props.currentFilePath) : undefined)
    }
})

watch(projectName, (newVal, oldVal) => {
    if (newVal !== oldVal) {
        pulseBadge('project')
        if (props.projectRoot) showReveal('project', newVal, props.projectRoot)
    }
})

function dismissReveal() {
    if (revealTimer) { clearTimeout(revealTimer); revealTimer = null }
    reveal.value = null
}

// Expose the reactive highlight state for programmatic access (used by tests
// and debugging); the template reads `segmentClass` for the per-segment
// highlight classes.
defineExpose({ highlightBadge, reveal })

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
/** Receives the rendered project-dropdown panel element (from AppMenuPanel). */
function setProjectPanelRef(el: HTMLElement | null) {
    dropdownPanelRef.value = el
}
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
            // Display relative to home directory for cleaner paths (shared pure
            // helper handles separator normalization + prefix matching).
            const displayPath = recentProjectDisplayPath(p, props.homeDir || '')
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
    motionQuery = typeof window !== 'undefined' && window.matchMedia
        ? window.matchMedia('(prefers-reduced-motion: reduce)')
        : null
    prefersReducedMotion.value = motionQuery?.matches === true
    if (motionQuery?.addEventListener) {
        motionQuery.addEventListener('change', onMotionPreferenceChange)
    }
})

onUnmounted(() => {
    document.removeEventListener('click', onClickOutside)
    document.removeEventListener('visibilitychange', onBlinkVisibilityChange)
    stopBackgroundPolling()
    motionQuery?.removeEventListener?.('change', onMotionPreferenceChange)
    motionQuery = null
    dismissReveal()
    disposeBadgeHighlight()
})

// Keyboard navigation (↑/↓ select, Enter confirm, Esc close) for the three
// teleported dropdowns — project switch, recent files, branch quick-index.
useMenuKeyboard({ panelRef: dropdownPanelRef, isOpen: dropdownOpen })
useMenuKeyboard({ panelRef: fileDropdownPanelRef, isOpen: fileDropdownOpen })
useMenuKeyboard({ panelRef: branchDropdownPanelRef, isOpen: branchDropdownOpen })
</script>

<style scoped>
.header-logo {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    flex-shrink: 0;
}

/* Badge capsule: combines project + branch into one pill shape.
   overflow:hidden clips the segments (and their highlight/hover accent
   backgrounds) to the pill's rounded ends — without it, a highlighted
   end segment (border-radius:0) would show a square corner sticking out of
   the semicircular right/left edge. Dropdown/menu/reveal surfaces are all
   teleported to <body>, so nothing inside needs to overflow this box. */
.badge-capsule {
    display: flex;
    align-items: center;
    border: 1px solid var(--border-color);
    background: var(--bg-secondary);
    border-radius: 999px;
    flex: 0 1 auto;
    min-width: 0;
    max-width: calc(100% - 50px); /* leave room for logo + server button */
    overflow: hidden;
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
    max-width: 1px;
    align-self: stretch;
    background: var(--border-color);
    flex-shrink: 0;
}

.project-dropdown-wrapper {
    position: relative;
    flex: 0 1 auto;
    min-width: 0;
    max-width: 220px;
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
    transition: background 0.15s, border-color 0.15s, color 0.25s ease;
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
    transition: background 0.15s, border-color 0.15s, color 0.25s ease;
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

/* Highlight — the changed segment briefly flashes the accent background with
   white foreground (text + icon), then fades back. Pure background/color
   animation: no layout churn (the old fill/collapse collapsed sibling
   segments via max-width and re-ran flex layout every frame). The complete
   text is shown in the floating reveal card (see .badge-reveal). */
.badge-capsule .badge-highlight {
    /* !important: must beat the segment's own hover background rule
       (e.g. .project-switch-btn:hover) which would otherwise win. */
    background: var(--accent-color) !important;
    color: #fff;
    border-radius: 0;
}

.badge-capsule .badge-highlight .project-name,
.badge-capsule .badge-highlight .branch-name,
.badge-capsule .badge-highlight .current-file-name {
    color: #fff;
}

/* !important: icons (e.g. .project-switch-btn svg:first-child) set
   --accent-color with equal-or-higher specificity otherwise. */
.badge-capsule .badge-highlight svg {
    color: #fff !important;
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
    padding: 10px;
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
    padding: 10px;
    margin-left: -8px;
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

/* Stacked (two-line) rows: name on line 1, path on line 2. Tight, compact
   spacing — row padding is reduced and the two lines share a 3px gap. */
.app-menu-item--stacked {
    padding-top: 4px;
    padding-bottom: 4px;
    align-items: center;
}

.app-menu-item.app-menu-item--stacked .item-body {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
    line-height: 1.3;
}

.app-menu-item.app-menu-item--stacked .item-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
}

.app-menu-item.app-menu-item--stacked .item-path {
    font-size: 11px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl; /* right-align the visible tail of long paths */
    text-align: left;
}

/* Active (current project/file) row: the stacked block above is declared later
   than the generic `.active .item-path` rule with equal specificity, so these
   overrides restore the white-on-accent text. */
.app-menu-item.active.app-menu-item--stacked .item-body,
.app-menu-item.active.app-menu-item--stacked .item-name {
    color: #fff;
}

.app-menu-item.active.app-menu-item--stacked .item-path {
    color: rgba(255, 255, 255, 0.6);
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

.app-menu-item.active .item-name,
.app-menu-item.active .item-body {
    color: #fff;
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
    width: 18px;
    height: 18px;
    padding: 0;
    border: 0;
    border-radius: 3px;
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

/* Reveal card — a small fixed popover shown under a badge segment that just
   changed (project / branch / file), carrying the segment's FULL text since
   the capsule itself truncates long names. Compositor-friendly animation:
   only opacity + transform (no layout properties). pointer-events: none so it
   never blocks taps on whatever sits beneath. */
.badge-reveal {
    position: fixed;
    z-index: 10000;
    pointer-events: none;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    max-width: min(78vw, 320px);
    padding: 8px 12px;
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: 10px;
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.12);
    transform-origin: top center;
}

.badge-reveal-title {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    line-height: 1.35;
    color: var(--text-primary);
    word-break: break-word;
    overflow-wrap: anywhere;
}

.badge-reveal-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 16px;
    height: 16px;
    margin-top: 1px;
    color: var(--accent-color);
}

.badge-reveal-subtitle {
    padding-left: 22px;
    font-size: 11px;
    line-height: 1.3;
    color: var(--text-muted);
    word-break: break-word;
    overflow-wrap: anywhere;
    direction: rtl; /* keep the visible tail of long paths, like .item-path */
    text-align: left;
}

/* Pop in / fade out (only transform + opacity). */
.reveal-enter-active,
.reveal-leave-active {
    transition: opacity 0.2s ease, transform 0.2s cubic-bezier(0.2, 0.9, 0.3, 1.15);
}

.reveal-enter-from,
.reveal-leave-to {
    opacity: 0;
    transform: translateY(-4px) scale(0.96);
}

@media (prefers-reduced-motion: reduce) {
    .reveal-enter-active,
    .reveal-leave-active {
        transition: opacity 0.1s ease;
    }
    .reveal-enter-from,
    .reveal-leave-to {
        transform: none;
    }
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

/* ─── Theme picker popup (unified with terminal theme picker) ───────────
   Container surface is the PopupMenu appSurface (.app-menu look). The inner
   column lays out title + scrollable list + pinned footer (the "more
   appearance options" row reuses the project dropdown's divider + other-item
   structure so it looks identical to the "browse" row). */
.theme-picker { min-width: 160px; }
/* Column layout shared by app-menu-styled pickers (app-header theme picker +
   terminal theme picker). max-height: inherit ties it to the PopupMenu root's
   computed inline max-height, so on short viewports the scrollable middle
   shrinks and the pinned footer never gets clipped. */
.app-menu-column {
  display: flex;
  flex-direction: column;
  max-height: inherit;
  min-height: 0;
}
.app-menu-column > .app-menu-scroll {
  flex: 1 1 auto;
  min-height: 0;
}
/* Header (title) / divider / footer row must never shrink — the scrollable
   middle absorbs any height deficit first. */
.app-menu-column > :not(.app-menu-scroll) {
  flex-shrink: 0;
}
.theme-item + .theme-item { border-top: 1px solid var(--border-color); }
.theme-item {
  display: flex; align-items: center; gap: 6px;
  width: 100%; padding: 5px 10px; border: none; border-radius: 0;
  background: var(--tterm-preview-bg, transparent);
  color: var(--tterm-preview-fg, var(--text-primary));
  font-size: 12px; text-align: left; cursor: pointer;
  transition: background 0.1s, box-shadow 0.1s;
}
.theme-item:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: -2px;
}
/* 预览底色不变，hover 加 accent 全边框高亮 */
@media (hover: hover) {
  .theme-item:hover {
    background: var(--tterm-preview-bg, transparent);
    box-shadow: inset 0 0 0 1px var(--accent-color);
  }
}
.theme-item.active { background: var(--tterm-preview-bg, transparent); color: var(--tterm-preview-fg, var(--text-primary)); }
.theme-item-check { flex-shrink: 0; width: 16px; height: 16px; display: flex; align-items: center; justify-content: center; font-size: 10px; border-radius: 50%; }
.theme-item.active .theme-item-check { background: var(--accent-color); color: #fff; }
.theme-item-name { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
.theme-item-base-icon { flex-shrink: 0; color: var(--tterm-preview-accent, var(--text-muted)); }
.theme-item.active .theme-item-base-icon { color: var(--tterm-preview-accent, var(--text-muted)); }
</style>
