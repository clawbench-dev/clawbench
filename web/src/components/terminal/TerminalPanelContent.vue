<template>
  <div class="terminal-panel">

    <!-- Platform unsupported state (top priority) -->
    <div v-if="platformUnsupported" class="terminal-empty-state terminal-platform-unsupported">
      <TerminalIcon :size="40" class="terminal-empty-icon" />
      <p class="terminal-empty-text">{{ t('terminal.platformUnsupported') }}</p>
    </div>

    <!-- Empty state when all tabs are closed -->
    <div v-else-if="tabs.length === 0" class="terminal-empty-state">
      <TerminalIcon :size="40" class="terminal-empty-icon" />
      <p class="terminal-empty-text">{{ t('terminal.noSessions') }}</p>
      <button class="terminal-empty-create-btn" @click="handleCreateTab">
        <PlusIcon :size="16" />
        <span>{{ t('terminal.createSession') }}</span>
      </button>
    </div>

    <template v-else>
    <!-- Tab bar (replaces old header) -->
    <div class="terminal-tab-bar">
      <div class="terminal-tab-list">
        <div
          v-for="tab in tabs"
          :key="tab.id"
          class="terminal-tab"
          :class="{ active: tab.id === activeTabId }"
          @click="handleTabClick(tab.id)"
        >
          <span class="terminal-tab-title" :title="tab.cwd">{{ tab.title }}</span>
          <button class="terminal-tab-menu-btn" @click.stop="openTabMenu($event, tab)" :title="t('terminal.title')">
            <MoreVerticalIcon :size="12" />
          </button>
        </div>
      </div>
      <button
        class="terminal-tab-add"
        :class="{ disabled: !canCreateMore }"
        :disabled="!canCreateMore"
        @click="handleCreateTab"
        :title="canCreateMore ? t('terminal.newTab') : t('terminal.tabLimitReached')"
      >
        <PlusIcon :size="14" />
      </button>
      <template v-if="isPC">
      <button
        class="terminal-tab-add terminal-theme-btn"
        @click="openThemeMenu"
        :title="t('terminal.theme')"
      >
        <PaletteIcon :size="14" />
      </button>
      <button
        class="terminal-tab-add"
        ref="cmdBtnTopRef"
        @click="openCommands"
        :title="t('terminal.quickCommands')"
      >
        <ZapIcon :size="14" />
      </button>
      </template>
    </div>

    <!-- Terminal viewport — one container per tab -->
    <div class="terminal-viewport">
      <div
        v-for="tab in tabs"
        :key="tab.id"
        v-show="tab.id === activeTabId"
        :ref="(el) => setTabContainer(tab.id, el as HTMLElement | null)"
        class="terminal-container"
        @click.self="focusTerminal"
      >
        <!-- Rebuild overlay (per-tab) -->
        <div v-if="rebuildingTabId === tab.id" class="terminal-rebuild-overlay">
          <LoadingIndicator size="md" />
          <span>{{ t('terminal.rebuilding') }}</span>
        </div>

        <!-- Error overlay (per-tab) -->
        <div v-if="tab.id === activeTabId && isTabError(tab)" class="terminal-error-overlay">
          <p>{{ getTabErrorMessage(tab) }}</p>
          <button v-if="isTabCanReconnect(tab)" class="terminal-reconnect-btn" @click="handleReconnect(tab)">{{ t('terminal.reconnect') }}</button>
        </div>

        <!-- Gesture hint overlay -->
        <Transition name="gesture-hint">
          <div v-if="gestureHint" class="gesture-hint">{{ gestureHint }}</div>
        </Transition>
      </div>
    </div>

    <Transition name="copy-bar">
      <div v-if="selectionActive" class="selection-copy-bar">
        <span class="selection-copy-count">{{ t('terminal.selectedChars', { n: selectedText.length }) }}</span>
        <button class="selection-copy-btn" @click="handleCopySelection" @contextmenu.prevent>{{ t('common.copy') }}</button>
        <button class="selection-copy-close" @click="handleDismissSelection" @contextmenu.prevent :aria-label="t('terminal.close')">✕</button>
      </div>
    </Transition>

    <!-- Virtual key toolbar -->
    <div class="terminal-toolbar" v-show="!isPC">
      <!-- Symbol bar (toggleable, above main toolbar) -->
      <Transition name="symbol-bar">
        <div v-if="showSymbolBar" class="symbol-bar">
          <div class="scroll-wrapper" :class="{ 'scroll-fade-left': symbolBarScrollFade.left, 'scroll-fade-right': symbolBarScrollFade.right }">
            <div ref="symbolBarScrollRef" class="symbol-bar-scroll" @scroll="updateSymbolBarScrollFade">
              <button v-for="sym in selectedSymbols" :key="sym.id" class="toolbar-btn btn-symbol" @click="handleSymbolClick(sym.char!)">{{ sym.label }}</button>
            </div>
          </div>
        </div>
      </Transition>

      <!-- Main toolbar row -->
      <div class="main-toolbar-row">
        <button class="toolbar-btn modifier gesture-toggle btn-func" :class="{ active: gestures.mode.value === 'gesture', 'mode-selection': gestures.mode.value === 'selection' }" @click="handleModeCycle" @contextmenu.prevent :title="t('terminal.modes')">
          <EyeIcon v-if="gestures.mode.value === 'browse'" :size="14" />
          <HandIcon v-else-if="gestures.mode.value === 'gesture'" :size="14" />
          <TextCursorInputIcon v-else :size="14" />
        </button>
        <button class="toolbar-btn modifier gesture-toggle btn-func" :class="{ active: showSymbolBar }" @click="toggleSymbolBar()" @contextmenu.prevent :title="t('terminal.symbols')">
          <OmegaIcon :size="14" />
        </button>
        <div class="scroll-wrapper" :class="{ 'scroll-fade-left': toolbarScrollFade.left, 'scroll-fade-right': toolbarScrollFade.right }">
          <div ref="toolbarScrollRef" class="toolbar-scroll" @scroll="updateToolbarScrollFade">
          <button
            v-for="def in visibleKeys"
            :key="def.id"
            class="toolbar-btn"
            :class="toolbarBtnClass(def)"
            @click="handleToolbarKeyClick(def)"
            @contextmenu.prevent
            :title="def.label"
          >
            <template v-if="def.id === 'shift_tab'"><span class="shift-tab-label">Shift</span><span class="shift-tab-label">Tab</span></template>
            <template v-else>{{ def.label }}</template>
          </button>
          <!-- Quick commands / theme / settings buttons -->
          <div class="key-group btn-func-group">
            <button ref="clipboardBtnRef" class="toolbar-btn btn-action btn-func" @click="openInput" :title="t('terminal.input')">
              <PenLineIcon :size="14" />
            </button>
            <button ref="cmdBtnRef" class="toolbar-btn btn-action btn-func" @click="openCommands" :title="t('terminal.quickCommands')">
              <ZapIcon :size="14" />
            </button>
            <button class="toolbar-btn btn-action btn-func" @click="openThemeMenu" :title="t('terminal.theme')">
              <PaletteIcon :size="14" />
            </button>
            <!-- Settings button (always present) -->
            <button class="toolbar-btn btn-action btn-func" @click="keyConfigDrawer.open()" :title="t('terminal.keyConfigTitle')">
              <KeyboardIcon :size="14" />
            </button>
            <!-- Help button -->
            <button class="toolbar-btn btn-action btn-func" @click="helpDrawer.open()" :title="t('terminal.helpTitle')">
              <CircleHelpIcon :size="14" />
            </button>
          </div>
        </div>
        </div>
      </div>
    </div>
    </template>

    <!-- Quick commands popup -->
    <PopupMenu v-model:show="showCommands" :target-element="isPC ? cmdBtnTopRef : cmdBtnRef" :max-width="220" :max-height="280" :menu-items-count="visibleCommands.length + 1">
      <div class="quick-send-title">{{ t('terminal.quickCommands') }}</div>
      <button v-for="cmd in visibleCommands" :key="cmd.id" class="quick-send-item" @click="executeCommand(cmd)">
        {{ cmd.label }}
      </button>
      <div class="quick-send-divider" />
      <button class="quick-send-item" @click="openEditDialog">
        <SettingsIcon :size="14" /> {{ t('terminal.editCommands') }}
      </button>
    </PopupMenu>

    <!-- Terminal input drawer -->
    <TerminalInputDrawer :open="inputDrawer.effectiveOpen.value" @close="inputDrawer.close()" @input="inputToTerminal" />

    <!-- Terminal help drawer -->
    <TerminalHelpDrawer
      :open="helpDrawer.effectiveOpen.value"
      :gestures="!isPC"
      :app-mode="isAppMode"
      @close="helpDrawer.close()"
    />

    <!-- Tab three-dot menu -->
    <TerminalTabMenu
      v-model:show="showTabMenu"
      :target-element="tabMenuTarget"
      :cwd="tabMenuCwd"
      @close="handleTabMenuClose"
      @copy-path="handleTabMenuCopyPath"
      @close-all="handleTabMenuCloseAll"
    />

    <!-- Quick command edit dialog — only open when terminal tab is active -->
    <QuickCommandDrawer :open="quickCmdDrawer.effectiveOpen.value" @close="quickCmdDrawer.close()" />

    <!-- Key config drawer — only open when terminal tab is active -->
    <KeyConfigDrawer
      :open="keyConfigDrawer.effectiveOpen.value"
      @close="keyConfigDrawer.close()"
      @saved="onKeyConfigSaved"
    />

    <!-- Terminal theme picker -->
    <PopupMenu
      v-model:show="themeMenuOpen"
      :target-element="themeMenuTarget"
      :max-width="240"
      :max-height="320"
      :menu-items-count="6"
      anchor="right"
    >
      <div class="theme-picker" @click.stop>
        <div class="theme-picker-title">{{ t('terminal.theme') }}</div>
        <div v-if="themeLoading" class="theme-picker-status">{{ t('terminal.themeLoading') }}</div>
        <div v-else-if="themeLoadError" class="theme-picker-status theme-picker-error">
          <span>{{ t('terminal.themeLoadFailed') }}</span>
          <button class="theme-retry-btn" @click="ensureThemesLoaded">{{ t('common.retry') }}</button>
        </div>
        <div v-else class="theme-picker-list">
          <button
            class="theme-item"
            :class="{ active: themeSelection === TERMINAL_THEME_AUTO }"
            :style="autoThemePreviewStyle"
            @click="selectTheme(TERMINAL_THEME_AUTO)"
          >
            <span class="theme-item-check">{{ themeSelection === TERMINAL_THEME_AUTO ? '✓' : '' }}</span>
            <span class="theme-item-name">{{ t('terminal.themeFollowApp') }}</span>
            <component :is="autoThemeIsDark ? Moon : Sun" :size="12" class="theme-item-base-icon" />
          </button>
          <button
            v-for="id in THEME_IDS"
            :key="id"
            class="theme-item"
            :class="{ active: themeSelection === id }"
            :style="getTerminalThemePreviewStyle(id)"
            @click="selectTheme(id)"
          >
            <span class="theme-item-check">{{ themeSelection === id ? '✓' : '' }}</span>
            <span class="theme-item-name">{{ formatThemeName(id) }}</span>
            <component :is="isTerminalThemeDark(id) ? Moon : Sun" :size="12" class="theme-item-base-icon" />
          </button>
        </div>
      </div>
    </PopupMenu>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, reactive, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import '@xterm/xterm/css/xterm.css'

import PopupMenu from '@/components/common/PopupMenu.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import QuickCommandDrawer from '@/components/terminal/QuickCommandDrawer.vue'
import KeyConfigDrawer from '@/components/terminal/KeyConfigDrawer.vue'
import TerminalInputDrawer from '@/components/terminal/TerminalInputDrawer.vue'
import TerminalHelpDrawer from '@/components/terminal/TerminalHelpDrawer.vue'
import TerminalTabMenu from '@/components/terminal/TerminalTabMenu.vue'
import { useTerminalTabs, type TerminalTab } from '@/composables/useTerminalTabs'
import type { Terminal as TerminalType } from '@xterm/xterm'
import { copyText } from '@/utils/clipboard.ts'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { useTerminalViewport } from '@/composables/useTerminalViewport'
import { useTerminalKeys, type ModifierKey } from '@/composables/useTerminalKeys'
import { selectionCellsToSelect, shouldPreventTerminalContextMenu, useTerminalGestures } from '@/composables/useTerminalGestures'
import { useToast } from '@/composables/useToast'
import { useQuickCommands } from '@/composables/useQuickCommands'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { useAppMode } from '@/composables/useAppMode'
import { getNative } from '@/utils/clawbenchNative'
import { useKeyConfig } from '@/composables/useKeyConfig'
import { useDialog } from '@/composables/useDialog'
import { store } from '@/stores/app'
import {
  DEFAULT_FONT_SIZE,
  canReconnect as canReconnectUtil,
  errorDisplayMessage as errorDisplayMessageUtil,
  showErrorOverlay as showErrorOverlayUtil,
} from '@/utils/terminalFontUtils'
import { localConfig, setLocalConfig, useSettingsConfig } from '@/composables/useSettingsConfig'
import { shouldAutoRefocusTerminal, shouldInstallTerminalBlurRefocus } from '@/utils/terminalBlurUtils'
import type { KeyDef } from '@/utils/terminalKeyDefs'
import {
  TERMINAL_THEME_AUTO,
  TERMINAL_THEME_STORAGE_KEY,
  THEME_IDS,
  formatThemeName,
  loadThemesModule,
  resolveTheme,
  isAppDarkTheme,
  darkTheme,
  lightTheme,
} from '@/utils/terminalThemes'

import { Zap as ZapIcon, Hand as HandIcon, Omega as OmegaIcon, Plus as PlusIcon, MoreVertical as MoreVerticalIcon, SquareTerminal as TerminalIcon, Keyboard as KeyboardIcon, PenLine as PenLineIcon, Eye as EyeIcon, TextCursorInput as TextCursorInputIcon, Palette as PaletteIcon, CircleHelp as CircleHelpIcon, Settings as SettingsIcon, Sun, Moon } from 'lucide-vue-next'
const props = defineProps<{
  requestedCwd?: string | null
  active?: boolean
  platformUnsupported?: boolean
}>()

const emit = defineEmits<{
  open: []
  'cwd-handled': []
}>()

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()
const { getServerValueWithDefault } = useSettingsConfig()

// Font size with persistence
const fontSize = ref<number>((localConfig.terminalFontSize as number) || DEFAULT_FONT_SIZE)

// Max sessions from server config
const maxSessions = computed(() => {
  const val = getServerValueWithDefault('terminal.max_sessions')
  return typeof val === 'number' ? val : 10
})

function applyFontSize(size: number) {
  const MIN = 8, MAX = 28
  const clamped = Math.max(MIN, Math.min(MAX, size))
  fontSize.value = clamped
  setLocalConfig('terminalFontSize', clamped)
  tabManager.updateFontSize(clamped)
  // Fit active terminal after font change
  const active = tabManager.activeTab.value
  if (active?.fitAddon) {
    requestAnimationFrame(() => {
      try { active.fitAddon?.fit() } catch { /* ignore */ }
    })
  }
}

// Refs
const gestureHint = ref('')
let gestureHintTimer: ReturnType<typeof setTimeout> | null = null
const selectionActive = ref(false)
const selectedText = ref('')
const showCommands = ref(false)
const cmdBtnRef = ref<HTMLElement | null>(null)
const cmdBtnTopRef = ref<HTMLElement | null>(null)
const clipboardBtnRef = ref<HTMLElement | null>(null)
const showSymbolBar = ref(false)
const rebuildingTabId = ref<string | null>(null)

// Scroll fade state for toolbar and symbol bar
const toolbarScrollFade = reactive({ left: false, right: false })
const symbolBarScrollFade = reactive({ left: false, right: false })

function computeScrollFade(el: HTMLElement): { left: boolean; right: boolean } {
  const { scrollLeft, scrollWidth, clientWidth } = el
  return {
    left: scrollLeft > 2,
    right: scrollLeft + clientWidth < scrollWidth - 2,
  }
}

function updateToolbarScrollFade(e: Event) {
  const el = e.currentTarget as HTMLElement
  const fade = computeScrollFade(el)
  toolbarScrollFade.left = fade.left
  toolbarScrollFade.right = fade.right
}

function updateSymbolBarScrollFade(e: Event) {
  const el = e.currentTarget as HTMLElement
  const fade = computeScrollFade(el)
  symbolBarScrollFade.left = fade.left
  symbolBarScrollFade.right = fade.right
}

// Refs for scroll containers
const toolbarScrollRef = ref<HTMLElement | null>(null)
const symbolBarScrollRef = ref<HTMLElement | null>(null)

function refreshToolbarFade() {
  const el = toolbarScrollRef.value
  if (!el) return
  const fade = computeScrollFade(el)
  toolbarScrollFade.left = fade.left
  toolbarScrollFade.right = fade.right
}

// Tab menu state
const showTabMenu = ref(false)
const tabMenuTarget = ref<HTMLElement | null>(null)
const tabMenuTabId = ref<string | null>(null)
const tabMenuCwd = ref('')

// Symbol bar — config-driven
const { selectedKeys, selectedSymbols, fetchConfig: fetchKeyConfig } = useKeyConfig()
const keyConfigDrawer = useTabDrawer('terminal')
const inputDrawer = useTabDrawer('terminal')
const helpDrawer = useTabDrawer('terminal')

/** Keys visible in the toolbar — always show all keys; gesture inputs display on-screen hints via onGestureHint. */
const visibleKeys = computed(() => selectedKeys.value)

function handleSymbolClick(sym: string) {
  activeTab.value?.session.sendInput(sym)
  focusTerminal()
}

function toggleSymbolBar() {
  showSymbolBar.value = !showSymbolBar.value
  focusTerminal()
}

function openCommands() {
  showCommands.value = !showCommands.value
}

function onKeyConfigSaved() {
  keyConfigDrawer.close()
}

async function openInput() {
  inputDrawer.open()
}

function inputToTerminal(text: string) {
  if (!text) return
  activeTab.value?.session.sendInput(text + '\r')
  toast.show(t('terminal.inputSent'), { icon: '📋', type: 'success', duration: 1200 })
}

function handleModeCycle() {
  gestures.cycleMode()
  const m = gestures.mode.value
  const label = m === 'browse' ? t('terminal.modeBrowse') : m === 'gesture' ? t('terminal.modeGesture') : t('terminal.modeSelection')
  const icon = m === 'browse' ? '👁️' : m === 'gesture' ? '✋' : '✂️'
  toast.show(label, { icon, type: 'info', duration: 1200 })
  focusTerminal()
}

// Build WS URL for a given CWD, with optional initial terminal dimensions.
// cols/rows are sent as query params so the PTY starts at the correct size
// instead of the default 80×24 — TUI apps (vim, claude, htop) then render
// full-size from the start without waiting for fit()+resize.
function getWsUrl(cwd?: string, cols?: number, rows?: number) {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params: string[] = []
  if (cwd) params.push(`cwd=${encodeURIComponent(cwd)}`)
  if (cols && cols > 0) params.push(`cols=${cols}`)
  if (rows && rows > 0) params.push(`rows=${rows}`)
  const query = params.length ? `?${params.join('&')}` : ''
  return `${proto}//${location.host}/api/terminal/ws${query}`
}

// Theme
function getXtermTheme(): Record<string, unknown> {
  return (isAppDarkTheme() ? darkTheme : lightTheme) as Record<string, unknown>
}

// Terminal theme state + selection (persisted to localConfig)
const themeSelection = ref<string>((localConfig.terminalTheme as string) || TERMINAL_THEME_AUTO)
const themeMenuOpen = ref(false)
const themeMenuTarget = ref<HTMLElement | null>(null)
const themeLoading = ref(false)
const themeLoadError = ref(false)
const allThemes = ref<Record<string, unknown> | null>(null)

async function ensureThemesLoaded() {
  if (allThemes.value || themeLoading.value) return
  themeLoading.value = true
  themeLoadError.value = false
  try {
    allThemes.value = await loadThemesModule()
  } catch {
    themeLoadError.value = true
  } finally {
    themeLoading.value = false
  }
}

async function applyTheme(selection: string) {
  themeSelection.value = selection
  setLocalConfig(TERMINAL_THEME_STORAGE_KEY, selection)
  const theme = await resolveTheme(selection, isAppDarkTheme())
  tabManager.updateTheme(theme as Record<string, unknown>)
  document.documentElement.style.setProperty('--terminal-bg', theme.background || '')
}

function openThemeMenu(e: Event) {
  themeMenuTarget.value = e.currentTarget as HTMLElement
  themeMenuOpen.value = true
  ensureThemesLoaded()
}

function selectTheme(selection: string) {
  themeMenuOpen.value = false
  applyTheme(selection)
}

// Theme preview helpers
const autoThemeIsDark = computed(() => isAppDarkTheme())

const autoThemePreviewStyle = computed(() => {
  const t = autoThemeIsDark.value ? darkTheme : lightTheme
  return {
    '--tterm-preview-bg': t.background,
    '--tterm-preview-fg': t.foreground,
    '--tterm-preview-accent': t.cursor || t.foreground,
  }
})

function isTerminalThemeDark(id: string): boolean {
  if (!allThemes.value) return true
  const t = allThemes.value[id]
  if (!t || !t.background) return true
  const bg = t.background as string
  // Parse hex color luminance
  const hex = bg.replace('#', '')
  if (hex.length !== 6) return true
  const r = parseInt(hex.substring(0, 2), 16)
  const g = parseInt(hex.substring(2, 4), 16)
  const b = parseInt(hex.substring(4, 6), 16)
  const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255
  return luminance < 0.5
}

function getTerminalThemePreviewStyle(id: string): Record<string, string> | undefined {
  if (!allThemes.value) return undefined
  const t = allThemes.value[id] as { background?: string; foreground?: string; cursor?: string } | undefined
  if (!t || !t.background) return undefined
  return {
    '--tterm-preview-bg': t.background,
    '--tterm-preview-fg': t.foreground || (isTerminalThemeDark(id) ? '#e6edf3' : '#1f2328'),
    '--tterm-preview-accent': t.cursor || t.foreground || (isTerminalThemeDark(id) ? '#89b4fa' : '#1e66f5'),
  }
}

// Tab manager
// Terminal keys — create early so processInput can be passed to tabManager.
// sendInput uses a wrapper that reads activeTab at call time (no cycle).
const terminalKeys = useTerminalKeys((data: string) => {
  activeTab.value?.session.sendInput(data)
})

function updateSelectionFromTerm(term: TerminalType) {
  const text = term.getSelection() ?? ''
  selectionActive.value = text.length > 0
  selectedText.value = text
}

/** Read the real CSS cell height from xterm's renderer, falling back to font-size×line-height. */
function getXtermCellHeight(term: TerminalType | null): number {
  if (!term) return 0
  const core = (term as unknown as { _core?: { _renderService?: { dimensions?: { css?: { cell?: { height?: number } } } } } })._core
  const h = core?._renderService?.dimensions?.css?.cell?.height
  if (h && h > 0) return h
  const lineHeight = typeof term.options.lineHeight === 'number' ? term.options.lineHeight : 1
  const fontSize = typeof term.options.fontSize === 'number' ? term.options.fontSize : 14
  return fontSize * lineHeight
}

/** Read the real CSS cell width from xterm's renderer, falling back to a font-size estimate. */
function getXtermCellWidth(term: TerminalType | null): number {
  if (!term) return 0
  const core = (term as unknown as { _core?: { _renderService?: { dimensions?: { css?: { cell?: { width?: number } } } } } })._core
  const w = core?._renderService?.dimensions?.css?.cell?.width
  if (w && w > 0) return w
  const fontSize = typeof term.options.fontSize === 'number' ? term.options.fontSize : 14
  return fontSize * 0.6
}

function handleSelectionExtend(anchorCol: number, anchorRow: number, currentCol: number, currentRow: number) {
  const term = activeTab.value?.xterm
  if (!term) return
  const sel = selectionCellsToSelect(anchorCol, anchorRow, currentCol, currentRow, term.buffer.active.viewportY, term.cols)
  term.select(sel.col, sel.row, sel.length)
  updateSelectionFromTerm(term)
}

function handleDismissSelection() {
  activeTab.value?.xterm?.clearSelection()
  selectionActive.value = false
  selectedText.value = ''
}

function handleCopySelection() {
  const text = selectedText.value
  if (!text) return
  copyText(text, () => {
    toast.show(t('terminal.copied'), { icon: '✅', type: 'success' })
    activeTab.value?.xterm?.clearSelection()
    selectionActive.value = false
    selectedText.value = ''
    gestures.setMode('browse')
  }, () => {
    toast.show(t('terminal.copyFailed'), { icon: '⚠️', type: 'error' })
  })
}

// Quick commands
const {
  visibleCommands,
  autoExecCommand,
  fetchCommands,
  showEditDialog,
} = useQuickCommands()
const quickCmdDrawer = useTabDrawer('terminal', showEditDialog)

const tabManager = useTerminalTabs(getWsUrl, {
  fontSize,
  getXtermTheme,
  errorMessages: {
    shellStartFailed: t('terminal.shellStartFailed'),
    websocketFailed: t('terminal.websocketFailed'),
    platformUnsupported: t('terminal.platformUnsupported'),
  },
  onTermCreated: (term) => {
    term.onSelectionChange(() => updateSelectionFromTerm(term))
  },
  onCloseSessionViaHttp: (sessionId: string) => {
    fetch(`/api/terminal/close?session=${encodeURIComponent(sessionId)}`, { method: 'POST' }).catch(() => {})
  },
  onExit: (_tabId) => {
    toast.show(t('terminal.ptyExited'), { icon: 'ℹ️', type: 'info' })
  },
  onError: () => {
    // Error displayed via overlay
  },
  autoExecCommand,
  onAutoExec: (tabId, command) => {
    tabManager.getTab(tabId)?.session.sendInput(command + '\r')
  },
  processInput: terminalKeys.processInput,
})

const { tabs, activeTabId, activeTab } = tabManager

// Terminal viewport — uses the active tab's xterm and container
const viewport = useTerminalViewport(
  computed(() => activeTab.value?.xterm || null),
  computed(() => activeTab.value?.container || null),
)

let touchScrollRemainder = 0

function handleTerminalTouchScroll(deltaY: number) {
  const term = activeTab.value?.xterm
  if (!term) return

  const lineHeightOption = typeof term.options.lineHeight === 'number' ? term.options.lineHeight : 1
  const rowHeight = Math.max(1, fontSize.value * lineHeightOption)
  touchScrollRemainder += deltaY / rowHeight
  const lines = Math.trunc(touchScrollRemainder)
  if (lines === 0) return

  term.scrollLines(-lines)
  touchScrollRemainder -= lines
}

// Gestures
const gestures = useTerminalGestures(
  computed(() => activeTab.value?.container || null),
  {
    sendArrowUp: terminalKeys.sendArrowUp,
    sendArrowDown: terminalKeys.sendArrowDown,
    sendArrowLeft: terminalKeys.sendArrowLeft,
    sendArrowRight: terminalKeys.sendArrowRight,
    sendPageUp: terminalKeys.sendPageUp,
    sendPageDown: terminalKeys.sendPageDown,
    sendTab: terminalKeys.sendTab,
    onPinchZoom: (delta: number) => applyFontSize(fontSize.value + delta),
    onTouchScroll: handleTerminalTouchScroll,
    getCellHeight: () => getXtermCellHeight(activeTab.value?.xterm ?? null),
    getCellWidth: () => getXtermCellWidth(activeTab.value?.xterm ?? null),
    onSelectionExtend: handleSelectionExtend,
    onGestureHint: (symbol: string) => {
      gestureHint.value = symbol
      if (gestureHintTimer) clearTimeout(gestureHintTimer)
      gestureHintTimer = setTimeout(() => { gestureHint.value = '' }, 600)
    },
  },
)

// Re-evaluate fade when gesture toggle changes visible buttons
watch(() => gestures.mode.value, (m) => {
  nextTick(refreshToolbarFade)
  if (m !== 'selection') {
    activeTab.value?.xterm?.clearSelection()
    selectionActive.value = false
    selectedText.value = ''
  }
})

// Re-bind gesture listeners when switching/creating tabs (container element changes).
// Use double nextTick to ensure mountTabToContainer has already run.
watch(activeTabId, () => {
  activeTab.value?.xterm?.clearSelection()
  selectionActive.value = false
  selectedText.value = ''
  nextTick(() => nextTick(() => gestures.attach()))
})

const { isPC } = usePlatformDetect()

;(window as unknown as { __onVolumeKey?: (direction: 'up' | 'down') => void }).__onVolumeKey = (direction: 'up' | 'down') => {
  if (direction === 'up') terminalKeys.sendArrowUp()
  else terminalKeys.sendArrowDown()
}

// Volume keys (Android)
const { isAppMode } = useAppMode()

function enableVolumeKeys() {
  if (!isAppMode.value) return
  getNative()?.setVolumeKeyMode?.(true)
}

function disableVolumeKeys() {
  if (!isAppMode.value) return
  getNative()?.setVolumeKeyMode?.(false)
}

// Computed
const canCreateMore = computed(() => tabs.value.length < maxSessions.value)

// Sync terminal session count to Android notification and store
watch(() => tabs.value.length, (count) => {
  store.state.terminalSessionCount = count
  if (isAppMode.value) {
    try {
      getNative()?.setTerminalSessionCount?.(count)
    } catch { /* ignore */ }
  }
}, { immediate: true })

// Per-tab error state helpers
// NOTE: tab is a reactive() proxy which auto-unwraps Refs, so we MUST
// access tab.session.connectionState directly (no .value). TypeScript
// doesn't model reactive() auto-unwrapping, but using .value would
// read the .value property of the already-unwrapped string (undefined).
function isTabError(tab: TerminalTab): boolean {
  return showErrorOverlayUtil(tab.session.connectionState as unknown as string)
}

function isTabCanReconnect(tab: TerminalTab): boolean {
  return canReconnectUtil(tab.session.errorCode as unknown as string)
}

function getTabErrorMessage(tab: TerminalTab): string {
  return errorDisplayMessageUtil(
    tab.session.errorCode as unknown as string,
    tab.session.errorMessage as unknown as string,
    t('terminal.websocketFailed'),
  )
}

// Tab container ref management
// We need to store container refs for each tab so xterm can mount
const tabContainerRefs = new Map<string, HTMLElement>()

function setTabContainer(tabId: string, el: HTMLElement | null) {
  if (el) {
    tabContainerRefs.set(tabId, el)
  } else {
    tabContainerRefs.delete(tabId)
  }
}

function mountTabToContainer(tab: TerminalTab, container: HTMLElement) {
  // Clean up previous handlers from a prior mount on the same container
  const oldWheel = (container as unknown as { __terminalWheelHandler?: ((e: Event) => void) | null }).__terminalWheelHandler
  if (oldWheel) {
    container.removeEventListener('wheel', oldWheel)
    delete (container as unknown as { __terminalWheelHandler?: ((e: Event) => void) | null }).__terminalWheelHandler
  }
  const oldCtx = (container as unknown as { __terminalContextMenuHandler?: ((e: Event) => void) | null }).__terminalContextMenuHandler
  if (oldCtx) {
    container.removeEventListener('contextmenu', oldCtx)
    delete (container as unknown as { __terminalContextMenuHandler?: ((e: Event) => void) | null }).__terminalContextMenuHandler
  }

  tabManager.mountTabXterm(tab, container)

  // Add Ctrl+Wheel zoom handler
  const wheelHandler = (e: WheelEvent) => {
    if (e.ctrlKey || e.metaKey) {
      e.preventDefault()
      const delta = e.deltaY < 0 ? 1 : -1
      applyFontSize(fontSize.value + delta)
    }
  }
  container.addEventListener('wheel', wheelHandler, { passive: false })
  ;(container as unknown as { __terminalWheelHandler?: ((e: WheelEvent) => void) | null }).__terminalWheelHandler = wheelHandler

  // Context menu handler — suppress long-press context menu while gestures are enabled
  const contextMenuHandler = (e: Event) => {
    if (shouldPreventTerminalContextMenu(gestures.mode.value !== 'browse')) {
      e.preventDefault()
    }
  }
  container.addEventListener('contextmenu', contextMenuHandler)
  ;(container as unknown as { __terminalContextMenuHandler?: ((e: Event) => void) | null }).__terminalContextMenuHandler = contextMenuHandler

  // Mobile keyboard stability: on Android WebView, touching the terminal surface
  // blurs the focused xterm textarea BEFORE touchstart is dispatched, collapsing
  // the soft keyboard; xterm then re-focuses on the synthesized mousedown,
  // reopening it — a visible collapse-then-reopen on every tap. The blur happens
  // before the touch event so it can't be blocked with preventDefault(). Instead,
  // restore focus the moment the textarea blurs to body/document while the
  // terminal panel is still active, keeping the keyboard up. If a real control
  // (toolbar/dock button, input) takes focus, we leave it alone.
  //
  // NOTE: this is a deliberate workaround for a platform quirk (blur fires before
  // touchstart and is uncancellable). It re-acquires focus rather than preventing
  // the blur, so it is safe only while the terminal panel is active. Residual
  // risk: if the panel is active but the user genuinely intends to dismiss the
  // keyboard by tapping the surface (e.g. a future in-panel "collapse keyboard"
  // interaction), this guard will fight that intent by re-showing it. The
  // decision logic lives in shouldAutoRefocusTerminal() (utils/terminalBlurUtils.ts)
  // and is unit-tested; keep any new "should dismiss" exceptions gated there.
  const installBlurRefocus = () => {
    if (!shouldInstallTerminalBlurRefocus(isPC.value)) return
    const textareaEl = tab.xterm?.textarea
    if (!textareaEl || (textareaEl as unknown as { __blurRefocus?: boolean }).__blurRefocus) return
    ;(textareaEl as unknown as { __blurRefocus?: boolean }).__blurRefocus = true
    textareaEl.addEventListener('blur', () => {
      const next = document.activeElement
      if (!shouldAutoRefocusTerminal(!!props.active, next)) return
      // Refocus as a microtask: runs after the current event (and the browser's
      // touch-down default that blurred us) but before the next paint, so the
      // keyboard never visibly collapses. Faster than requestAnimationFrame.
      // Re-validate here: by microtask time focus may have settled on a real
      // control (e.g. an input inside a modal/drawer), which must not be stolen.
      queueMicrotask(() => {
        const ta = tab.xterm?.textarea
        if (!ta || !props.active) return
        if (!shouldAutoRefocusTerminal(!!props.active, document.activeElement)) return
        if (document.activeElement !== ta) {
          ta.focus()
        }
      })
    })
  }
  installBlurRefocus()

  // Fit the terminal after mounting
  requestAnimationFrame(() => {
    try { tab.fitAddon?.fit() } catch { /* ignore */ }
  })
}

function focusTerminal() {
  activeTab.value?.xterm?.focus()
}

// Tab bar actions
function handleTabClick(tabId: string) {
  if (tabId === activeTabId.value) return
  tabManager.switchTab(tabId)

  // Connect the newly active tab if it's disconnected (e.g. after panel reactivation)
  const tab = tabManager.getTab(tabId)
  if (tab && (tab.session.connectionState as unknown as string) === 'disconnected') {
    // Fit before connect only for new sessions; reconnects get fit() in onReplay
    const isNewSession = !(tab.session.sessionId as unknown as string)
    if (isNewSession) {
      try { tab.fitAddon?.fit() } catch { /* ignore */ }
    }
    tab.session.connect().then(() => {
      tabManager.syncTabSessionId(tabId)
    }).catch(() => { /* error shown via overlay */ })
  }
}

function handleCreateTab() {
  if (props.platformUnsupported) return
  if (!canCreateMore.value) return
  // Default new tab uses project root (empty cwd), not current directory
  const tab = tabManager.createTab()
  // Mount the new tab's xterm after next tick (DOM needs to render the container)
  nextTick(() => {
    const container = tabContainerRefs.get(tab.id)
    if (container && !tab.container) {
      mountTabToContainer(tab, container)
    }
    // Fit before connect so cols/rows are correct for the WS URL
    try { tab.fitAddon?.fit() } catch { /* ignore */ }
    // Connect the new tab
    if (props.active && (tab.session.connectionState as unknown as string) === 'disconnected') {
      tab.session.connect().then(() => {
        tabManager.syncTabSessionId(tab.id)
      }).catch(() => { /* error shown via overlay */ })
    }
  })
}

// Tab three-dot menu
function openTabMenu(event: Event, tab: TerminalTab) {
  event.stopPropagation()
  tabMenuTabId.value = tab.id
  tabMenuCwd.value = tab.cwd
  tabMenuTarget.value = (event.currentTarget as HTMLElement)
  showTabMenu.value = true
}

function handleTabMenuClose() {
  const tabId = tabMenuTabId.value
  if (!tabId) return
  const result = tabManager.closeTab(tabId)
  if (result.switchToId) {
    nextTick(() => {
      const container = tabContainerRefs.get(result.switchToId!)
      const tab = tabManager.getTab(result.switchToId!)
      if (container && tab && !tab.container) {
        mountTabToContainer(tab, container)
      }
      if (props.active && tab && (tab.session.connectionState as unknown as string) === 'disconnected') {
        // Fit before connect so cols/rows are correct for the WS URL
        try { tab.fitAddon?.fit() } catch { /* ignore */ }
        tab.session.connect().then(() => {
          tabManager.syncTabSessionId(tab.id)
        }).catch(() => {})
      }
    })
  }
}

function handleTabMenuCopyPath() {
  // Already handled by TerminalTabMenu
}

async function handleTabMenuCloseAll() {
  const confirmed = await dialog.confirm(t('terminal.confirmCloseAll'), {
    title: t('terminal.closeAllTabs'),
    dangerous: true,
  })
  if (confirmed) tabManager.disposeAll()
}

// Reconnect for a specific tab
function handleReconnect(tab: TerminalTab) {
  tab.session.disconnect()
  tab.session.connect().then(() => {
    tabManager.syncTabSessionId(tab.id)
    focusTerminal()
  }).catch(() => { /* error shown via overlay */ })
}

// Rebuild (re-create) the active tab's session
function executeCommand(cmd: { id: number; label: string; command: string }) {
  activeTab.value?.session.sendInput(cmd.command + '\r')
  showCommands.value = false
  focusTerminal()
}

function openEditDialog() {
  showCommands.value = false
  quickCmdDrawer.open()
}

/** Map KeyDef properties to toolbar CSS classes */
function toolbarBtnClass(def: KeyDef): Record<string, boolean> {
  if (def.isModifier) {
    const key = def.id as ModifierKey
    const state = terminalKeys.activeModifiers.value[key]
    return {
      'btn-modifier': true,
      'modifier': true,
      'active': state !== 'inactive',
      'locked': state === 'locked',
    }
  }
  if (def.id === 'shift_tab') return { 'btn-modifier': true, 'btn-shift-tab': true }
  if (def.group === 'shortcut') return { 'btn-modifier': true, 'shortcut': true }
  if (def.group === 'navigation') return { 'btn-nav': true }
  if (def.group === 'arrow') return { 'btn-arrow': true }
  if (def.group === 'editing') return { 'btn-nav': true }
  if (def.group === 'function') return { 'btn-modifier': true, 'shortcut': true }
  return {}
}

// Gesture-backed keys map to the gesture method that produces the same input,
// so clicking them shows "how to do this with a gesture" as on-screen feedback.
const GESTURE_KEY_LABELS: Record<string, string> = {
  tab: 'gestureDoubleTap',
  pgup: 'gestureTwoFingerUp',
  pgdn: 'gestureTwoFingerDown',
  arrow_up: 'gestureSwipeUp',
  arrow_down: 'gestureSwipeDown',
  arrow_left: 'gestureSwipeLeft',
  arrow_right: 'gestureSwipeRight',
}

function showGestureHint(symbol: string) {
  gestureHint.value = symbol
  if (gestureHintTimer) clearTimeout(gestureHintTimer)
  gestureHintTimer = setTimeout(() => { gestureHint.value = '' }, 600)
}

/** Handle click on a config-driven toolbar key */
function handleToolbarKeyClick(def: KeyDef) {
  if (def.isModifier) {
    terminalKeys.toggleModifier(def.id as ModifierKey, false)
  } else {
    terminalKeys.send(def.id)
  }
  // In gesture mode, clicking a gesture-backed key shows which gesture can
  // produce the same input, so the user knows they can swipe/tap instead.
  if (gestures.mode.value === 'gesture') {
    const labelKey = GESTURE_KEY_LABELS[def.id]
    if (labelKey) showGestureHint(t('terminal.' + labelKey))
  }
  focusTerminal()
}

// Whether the component has been mounted (DOM is available)
const isMounted = ref(false)

// Lifecycle
watch(() => props.active, async (isActive) => {
  if (!isMounted.value) return // Defer to onMounted for initial activation
  if (props.platformUnsupported) return // No session management on unsupported platforms
   if (isActive) {
     emit('open')
     enableVolumeKeys()
     await nextTick()
    // Attach the visualViewport keyboard listener whenever the panel becomes
    // active — even if every session tab is closed (activeTab is null). Otherwise
    // re-opening the terminal with no sessions skips startWatching and the Dock
    // never hides when the soft keyboard opens on mobile.
    viewport.startWatching()
    const tab = activeTab.value
    if (tab) {
      const container = tabContainerRefs.get(tab.id)
      if (container && !tab.container) {
        mountTabToContainer(tab, container)
      }
      // Fit before connect ONLY for new sessions (no sessionId yet) so
      // the WS URL includes correct cols/rows for PTY initialization.
      // For reconnects, onReplay handles fit() — a premature fit() here
      // sends resize before the replay flow is ready, racing with SIGWINCH.
      const isNewSession = !(tab.session.sessionId as unknown as string)
      if (isNewSession) {
        try { tab.fitAddon?.fit() } catch { /* ignore */ }
      }
      if ((tab.session.connectionState as unknown as string) === 'disconnected') {
        try {
          await tab.session.connect()
          tabManager.syncTabSessionId(tab.id)
        } catch { /* error shown via overlay */ }
      }
      gestures.attach()
      focusTerminal()
    }
   } else {
     disableVolumeKeys()
     tabManager.disconnectAll()
     terminalKeys.reset()
     showCommands.value = false
     showTabMenu.value = false
     viewport.stopWatching()
     gestures.detach()
   }
})

// Watch requestedCwd — when the file manager emits "open terminal here",
// create a new tab in the specified directory.
watch(() => props.requestedCwd, async (cwd) => {
  if (!cwd || !props.active || !isMounted.value) return
  if (!canCreateMore.value) return
  const tab = tabManager.createTab(cwd)
  await nextTick()
  const container = tabContainerRefs.get(tab.id)
  if (container && !tab.container) {
    mountTabToContainer(tab, container)
  }
  if ((tab.session.connectionState as unknown as string) === 'disconnected') {
    // Fit before connect so cols/rows are correct for the WS URL
    try { tab.fitAddon?.fit() } catch { /* ignore */ }
    tab.session.connect().then(() => {
      tabManager.syncTabSessionId(tab.id)
    }).catch(() => { /* error shown via overlay */ })
  }
  // Signal parent to clear requestedCwd so re-triggering the same directory works
  emit('cwd-handled')
})

// Theme observer
let themeObserver: MutationObserver | null = null

onMounted(async () => {
  isMounted.value = true

  applyTheme(themeSelection.value).catch(() => {})
  // Fetch quick commands before the initial connect so auto-execute commands
  // are available when the first 'status' message arrives.
  await fetchCommands().catch(() => { /* ignore */ })

  // Fetch key config in the background
  fetchKeyConfig().catch(() => { /* ignore */ })

  // Initialize scroll fade state
  nextTick(refreshToolbarFade)

  themeObserver = new MutationObserver(() => {
    if (themeSelection.value === TERMINAL_THEME_AUTO) {
      tabManager.updateTheme(getXtermTheme())
    }
  })
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme', 'data-theme-base'],
  })

  // Mount and connect the active tab (only if terminal panel is active)
  if (props.active && !props.platformUnsupported) {
    emit('open')
    enableVolumeKeys()
    // Wait for v-for :ref callbacks to populate tabContainerRefs
    await nextTick()
    viewport.startWatching()
    const tab = activeTab.value
    if (tab) {
      const container = tabContainerRefs.get(tab.id)
      if (container && !tab.container) {
        mountTabToContainer(tab, container)
      }
      // Fit BEFORE connect so term.cols/rows are correct when the WS URL
      // is built with initial dimensions for the PTY.
      try { tab.fitAddon?.fit() } catch { /* ignore */ }
      if ((tab.session.connectionState as unknown as string) === 'disconnected') {
        try {
          await tab.session.connect()
          tabManager.syncTabSessionId(tab.id)
        } catch { /* error shown via overlay */ }
      }
      gestures.attach()
      focusTerminal()
    }
  }
})

onBeforeUnmount(() => {
  themeObserver?.disconnect()
  viewport.stopWatching()
  gestures.detach()
  disableVolumeKeys()
  delete (window as unknown as { __onVolumeKey?: unknown }).__onVolumeKey
  tabManager.disposeAll()
})

defineExpose({ activate: () => {}, deactivate: () => {} })
</script>

<style scoped>
.terminal-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  position: relative;
}

/* Empty state */
.terminal-empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 12px;
  padding: 32px;
}

.terminal-empty-icon {
  color: var(--text-muted);
  opacity: 0.5;
}

.terminal-empty-text {
  font-size: 14px;
  color: var(--text-muted);
  margin: 0;
}

.terminal-empty-create-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 14px;
  cursor: pointer;
  transition: background 0.15s ease;
}

.terminal-empty-create-btn:active {
  background: var(--bg-tertiary);
}

/* Tab bar */
.terminal-tab-bar {
  display: flex;
  align-items: stretch;
  height: 28px;
  padding: 0;
  flex-shrink: 0;
  background: var(--bg-secondary);
  border-bottom: 1px solid color-mix(in srgb, var(--text-primary) 8%, transparent);
  position: relative;
  z-index: 2;
  gap: 0;
}

.terminal-tab-list {
  display: flex;
  align-items: stretch;
  gap: 0;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
  padding: 0;
}

.terminal-tab-list::-webkit-scrollbar {
  display: none;
}

.terminal-tab {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 6px 0 10px;
  border-radius: 0;
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.1s ease;
  user-select: none;
  -webkit-user-select: none;
  max-width: 120px;
}

.terminal-tab:hover {
  background: var(--bg-tertiary);
}

.terminal-tab.active {
  background: color-mix(in srgb, var(--text-primary) 12%, transparent);
  box-shadow: inset 0 -2px 0 var(--accent-color);
}

.terminal-tab-title {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
}

.terminal-tab.active .terminal-tab-title {
  color: var(--text-primary);
  font-weight: 700;
}

.terminal-tab-menu-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border: none;
  border-radius: 0;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  padding: 0;
  opacity: 0;
  transition: opacity 0.1s ease, background 0.1s ease;
}

.terminal-tab:hover .terminal-tab-menu-btn,
.terminal-tab.active .terminal-tab-menu-btn {
  opacity: 1;
}

.terminal-tab-menu-btn:hover {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}

.terminal-tab-add {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 0;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  margin: 0 6px 0 0;
  transition: background 0.1s ease, color 0.1s ease;
}

.terminal-tab-add:hover:not(.disabled) {
  background: var(--bg-tertiary);
  color: var(--text-primary);
}

.terminal-tab-add:active:not(.disabled) {
  transform: scale(0.9);
}

.terminal-tab-add.disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.terminal-theme-btn { color: var(--text-muted); }

/* Symbol bar transition */
.symbol-bar-enter-active {
  transition: all 0.15s ease-out;
}
.symbol-bar-leave-active {
  transition: all 0.12s ease-in;
}
.symbol-bar-enter-from,
.symbol-bar-leave-to {
  opacity: 0;
  max-height: 0;
  padding-top: 0;
  margin-top: 0;
  overflow: hidden;
}
.symbol-bar-enter-to,
.symbol-bar-leave-from {
  max-height: 44px;
}

/* Terminal viewport container */
.terminal-viewport {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  position: relative;
}

.terminal-container {
  position: absolute;
  inset: 0;
  overflow: hidden;
  background: var(--terminal-bg, #1e1e2e);
}

.terminal-container :deep(.xterm-scrollable-element > .scrollbar.vertical),
.terminal-container :deep(.xterm-scrollbar) {
  width: 2px !important;
  right: 1px !important;
  background: transparent !important;
}

.terminal-container :deep(.xterm-scrollable-element > .scrollbar > .slider) {
  width: 2px !important;
  left: 0 !important;
  border-radius: 999px !important;
}

[data-theme-base="dark"] .terminal-container {
  background: var(--terminal-bg, #1e1e2e);
}

:root:not([data-theme-base="dark"]) .terminal-container {
  background: var(--terminal-bg, #eff1f5);
}

.terminal-rebuild-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: rgba(0, 0, 0, 0.6);
  color: rgba(255, 255, 255, 0.8);
  font-size: 13px;
  z-index: 8;
  user-select: none;
  -webkit-user-select: none;
}


.gesture-hint {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  font-size: 48px;
  color: rgba(255, 255, 255, 0.7);
  text-shadow: 0 2px 8px rgba(0, 0, 0, 0.5);
  pointer-events: none;
  z-index: 5;
  user-select: none;
  -webkit-user-select: none;
}

.gesture-hint-enter-active {
  transition: opacity 0.1s ease;
}
.gesture-hint-leave-active {
  transition: opacity 0.4s ease;
}
.gesture-hint-enter-from,
.gesture-hint-leave-to {
  opacity: 0;
}

.terminal-error-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.8);
  color: #fff;
  z-index: 10;
  padding: 20px;
  text-align: center;
}

.terminal-prompt-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: center;
}

.terminal-reconnect-btn {
  margin-top: 12px;
  padding: 6px 16px;
  border: 1px solid rgba(255, 255, 255, 0.4);
  border-radius: 6px;
  background: transparent;
  color: #fff;
  cursor: pointer;
  font-size: 13px;
}

.terminal-reconnect-btn:hover {
  background: rgba(255, 255, 255, 0.1);
}

/* Toolbar styles (unchanged) */
.terminal-toolbar {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  background: var(--bg-secondary);
  border-top: 1px solid color-mix(in srgb, var(--border-color) 40%, transparent);
  --toolbar-key-hover: color-mix(in srgb, var(--text-primary) 7%, transparent);
  --toolbar-key-active: color-mix(in srgb, var(--text-primary) 12%, transparent);
  --toolbar-key-text: color-mix(in srgb, var(--text-primary) 72%, transparent);
  --toolbar-key-muted: color-mix(in srgb, var(--text-muted) 72%, transparent);
  --toolbar-key-selected-bg: color-mix(in srgb, var(--text-primary) 14%, transparent);
  --toolbar-key-selected-text: var(--text-primary);
  --toolbar-divider: color-mix(in srgb, var(--border-color) 48%, transparent);
}

[data-theme-base="dark"] .terminal-toolbar {
  background: var(--bg-secondary);
  --toolbar-key-hover: color-mix(in srgb, var(--text-primary) 9%, transparent);
  --toolbar-key-active: color-mix(in srgb, var(--text-primary) 16%, transparent);
  --toolbar-key-text: color-mix(in srgb, var(--text-primary) 64%, transparent);
  --toolbar-key-muted: color-mix(in srgb, var(--text-muted) 64%, transparent);
  --toolbar-key-selected-bg: color-mix(in srgb, var(--text-primary) 18%, transparent);
  --toolbar-key-selected-text: var(--text-primary);
  --toolbar-divider: color-mix(in srgb, var(--border-color) 52%, transparent);
}

.symbol-bar {
  padding: 3px 6px 3px;
  background: color-mix(in srgb, var(--bg-primary) 60%, var(--bg-secondary));
  border-top: 1px solid color-mix(in srgb, var(--text-primary) 10%, transparent);
  border-bottom: 1px solid color-mix(in srgb, var(--text-primary) 8%, transparent);
  border-radius: 0;
}

.symbol-bar-scroll {
  display: flex;
  align-items: center;
  gap: 3px;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
}
.symbol-bar-scroll::-webkit-scrollbar { display: none; }

.scroll-wrapper {
  position: relative;
  overflow: hidden;
  flex: 1;
  min-width: 0;
}
.scroll-wrapper::before,
.scroll-wrapper::after {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  width: 0;
  pointer-events: none;
  z-index: 1;
  transition: width 200ms ease;
}
.scroll-wrapper::before {
  left: 0;
  background: linear-gradient(to right, var(--bg-secondary) 25%, transparent);
}
.scroll-wrapper::after {
  right: 0;
  background: linear-gradient(to left, var(--bg-secondary) 25%, transparent);
}
.scroll-wrapper.scroll-fade-left::before { width: 36px; }
.scroll-wrapper.scroll-fade-right::after { width: 36px; }

/* Symbol bar scroll-wrapper uses a slightly different background for its gradient */
.symbol-bar .scroll-wrapper::before {
  background: linear-gradient(to right, color-mix(in srgb, var(--text-primary) 3%, var(--bg-secondary)) 25%, transparent);
}
.symbol-bar .scroll-wrapper::after {
  background: linear-gradient(to left, color-mix(in srgb, var(--text-primary) 3%, var(--bg-secondary)) 25%, transparent);
}

.main-toolbar-row {
  display: flex;
  align-items: center;
  padding: 4px 6px;
  gap: 2px;
}

.gesture-toggle { flex-shrink: 0; }

.gesture-toggle.mode-selection {
  outline: 2px solid var(--accent-color);
  outline-offset: -2px;
  border-radius: 6px;
}

.toolbar-scroll {
  display: flex;
  align-items: center;
  gap: 0;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
  width: 100%;
  scrollbar-width: none;
}
.toolbar-scroll::-webkit-scrollbar { display: none; }

.key-group { display: flex; align-items: center; gap: 3px; }
.key-group + .key-group { position: relative; margin-left: 6px; }
.key-group + .key-group::before {
  content: '';
  position: absolute;
  left: -4px;
  width: 1px;
  height: 16px;
  border-radius: 999px;
  background: var(--toolbar-divider);
}

.toolbar-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 32px;
  height: 32px;
  padding: 0 5px;
  border: none;
  border-radius: 0;
  background: transparent;
  color: var(--toolbar-key-text);
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.01em;
  cursor: pointer;
  flex-shrink: 0;
  user-select: none;
  -webkit-user-select: none;
  touch-action: manipulation;
  transition: background 100ms ease, color 100ms ease;
}
.toolbar-btn:hover { background: var(--toolbar-key-hover); }
.toolbar-btn:active { background: var(--toolbar-key-active); }
.toolbar-btn:focus-visible { outline: 2px solid color-mix(in srgb, var(--text-primary) 36%, transparent); outline-offset: 2px; }
.toolbar-btn.modifier.active { background: var(--toolbar-key-selected-bg); color: var(--accent-color); box-shadow: inset 0 -2px 0 var(--accent-color); }
.toolbar-btn.modifier.locked { background: var(--toolbar-key-selected-bg); color: var(--accent-color); box-shadow: inset 0 -2px 0 var(--accent-color); }
.toolbar-btn.shortcut { background: transparent; color: var(--toolbar-key-text); font-weight: 800; font-size: 11px; }
.toolbar-btn.shortcut:active { background: var(--toolbar-key-active); }
.toolbar-btn.danger { color: var(--toolbar-key-text); opacity: 0.78; }
.toolbar-btn.danger:hover { opacity: 1; background: var(--toolbar-key-hover); }
.toolbar-btn.gesture-toggle { min-width: 32px; border-radius: 0; }

.btn-shift-tab {
  display: flex !important;
  flex-direction: column !important;
  gap: 0;
  line-height: 1;
  padding: 3px 5px;
}
.shift-tab-label { font-size: 9px; font-weight: 700; line-height: 1.3; }

@media (max-width: 768px) {
  .main-toolbar-row { padding-bottom: max(4px, env(safe-area-inset-bottom)); }
}

@media (hover: none) {
  .toolbar-btn:hover { background: transparent; }
  .toolbar-btn.shortcut:hover { background: transparent; }
  .toolbar-btn.modifier.active:hover, .toolbar-btn.modifier.locked:hover { background: var(--toolbar-key-selected-bg); }
  .toolbar-btn.btn-func:hover { background: transparent; }
  .toolbar-btn.btn-func.modifier.active:hover, .toolbar-btn.btn-func.modifier.locked:hover { background: color-mix(in srgb, var(--accent-color) 14%, transparent); }
  .toolbar-btn:active { background: var(--toolbar-key-active); }
  .toolbar-btn.btn-func:active { background: color-mix(in srgb, var(--accent-color) 18%, transparent); }
}

.toolbar-btn.btn-func {
  color: var(--accent-color);
  border-radius: 6px;
}
.toolbar-btn.btn-func:hover { background: color-mix(in srgb, var(--accent-color) 10%, transparent); }
.toolbar-btn.btn-func:active { background: color-mix(in srgb, var(--accent-color) 18%, transparent); }
/* Mode-selection keeps its outline, override the gesture-toggle active style for btn-func */
.toolbar-btn.btn-func.modifier.active { background: color-mix(in srgb, var(--accent-color) 14%, transparent); color: var(--accent-color); box-shadow: none; }
.toolbar-btn.btn-func.modifier.locked { background: color-mix(in srgb, var(--accent-color) 14%, transparent); color: var(--accent-color); box-shadow: none; }
.btn-func-group + .key-group { position: relative; margin-left: 6px; }
.btn-func-group + .key-group::before {
  content: '';
  position: absolute;
  left: -4px;
  width: 1px;
  height: 16px;
  border-radius: 999px;
  background: var(--toolbar-divider);
}

.toolbar-btn.btn-modifier, .toolbar-btn.btn-nav, .toolbar-btn.btn-arrow, .toolbar-btn.btn-symbol, .toolbar-btn.btn-action { background: transparent; }
.toolbar-btn.btn-symbol { color: var(--toolbar-key-text); font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 15px; font-weight: 700; }

/* WebView bold compensation — same mechanism as chat markdown bold
 * (markdown-common.css): font-weight alone renders lighter/softer in Android
 * WebView (bold synthesized by fattening outlines), so thicken glyphs with a
 * thin uniform -webkit-text-stroke under [data-app-mode] only. Applies to all
 * virtual keys and symbol buttons; shift-tab labels inherit it from .toolbar-btn. */
[data-app-mode] .toolbar-btn {
  -webkit-text-stroke: 0.12px currentColor;
}
[data-app-mode] .toolbar-btn.shortcut {
  -webkit-text-stroke: 0.1px currentColor;
}

.selection-copy-bar {
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: 10px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--accent-color) 90%, black);
  color: #fff;
  font-size: 12px;
  z-index: 20;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.25);
}
.selection-copy-count {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.selection-copy-btn {
  border: none;
  background: rgba(255, 255, 255, 0.2);
  color: #fff;
  padding: 4px 14px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
}
.selection-copy-close {
  border: none;
  background: transparent;
  color: rgba(255, 255, 255, 0.85);
  font-size: 14px;
  line-height: 1;
  padding: 4px 6px;
  border-radius: 6px;
}
.selection-copy-close:active {
  background: rgba(255, 255, 255, 0.2);
}
.copy-bar-enter-active,
.copy-bar-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.copy-bar-enter-from,
.copy-bar-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
</style>

<style>
/* Quick commands popup divider (unscoped because PopupMenu teleports to body) */
.quick-send-divider {
  height: 1px;
  background: var(--border-color);
  margin: 4px 0;
}

/* Terminal theme picker (unscoped because PopupMenu teleports to body) */
.theme-picker { padding: 0; min-width: 160px; }
.theme-picker-title { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); padding: 5px 10px 4px; border-bottom: 1px solid var(--border-color); }
.theme-picker-status { padding: 10px 12px; text-align: center; color: var(--text-muted); font-size: 12px; }
.theme-picker-error { display: flex; flex-direction: column; gap: 8px; align-items: center; }
.theme-retry-btn { padding: 4px 12px; border: 1px solid var(--border-color); border-radius: 4px; background: transparent; color: var(--text-primary); cursor: pointer; font-size: 12px; }
.theme-picker-list { max-height: 220px; overflow-y: auto; }
.theme-item {
  display: flex; align-items: center; gap: 6px;
  width: 100%; padding: 5px 10px; border: none; border-radius: 0;
  background: var(--tterm-preview-bg, transparent);
  color: var(--tterm-preview-fg, var(--text-primary));
  font-size: 12px; text-align: left; cursor: pointer;
  transition: background 0.1s;
}
.theme-item:hover { background: var(--bg-tertiary); }
.theme-item.active { background: var(--tterm-preview-bg, transparent); color: var(--tterm-preview-fg, var(--text-primary)); }
.theme-item-check { flex-shrink: 0; width: 16px; height: 16px; display: flex; align-items: center; justify-content: center; font-size: 10px; border-radius: 50%; }
.theme-item.active .theme-item-check { background: var(--accent-color); color: #fff; }
.theme-item-name { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
.theme-item-base-icon { flex-shrink: 0; color: var(--tterm-preview-accent, var(--text-muted)); }
.theme-item.active .theme-item-base-icon { color: var(--tterm-preview-accent, var(--text-muted)); }
</style>
