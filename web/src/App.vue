<template>
  <div>
    <!-- Loading state: show nothing while checking auth -->
    <div v-if="isAuthenticated === null" style="display:none" />

    <!-- Login -->
    <LoginView v-else-if="!isAuthenticated" @login-success="handleLoginSuccess" />

    <!-- Setup wizard (after login, before main UI) -->
    <SetupWizard v-else-if="needsSetup" @complete="handleSetupComplete" />

    <!-- Main app -->
    <div v-else class="app-container" :class="{ 'chrome-hidden': terminalActive, 'chat-keyboard-open': chatKeyboardActive, 'project-switching': switchingProject }" :key="projectKey">
      <AppHeader
        :hidden="terminalActive"
        :project-root="projectRoot"
        :home-dir="homeDir"
        @open-project-dialog="handleOpenProjectDialog"
      />

      <main class="main-content">
        <div class="content-area" id="contentArea">
          <!-- Chat Tab -->
          <TabPanel tabId="chat" :activeTab="activeTab">
            <template #header>
              <span class="bs-header-title">{{ sessionIdentity.agentHeaderTitle.value }}</span>
              <div v-if="sessionIdentity.currentSessionTitle.value" class="bs-header-description">
                <HeaderMarquee :text="sessionIdentity.currentSessionTitle.value">{{ sessionIdentity.currentSessionTitle.value }}</HeaderMarquee>
              </div>
            </template>
            <ChatPanelContent
              :active="activeTab === 'chat'"
              :current-file="currentFile"
              :current-dir="currentDir"
              @open="switchTab('chat')"
              @open-file="handleSelectFile"
              @task-card-click="onTaskCardClick"
            />
          </TabPanel>

          <!-- File Browse Tab (合一：目录浏览 + 文件覆盖预览) -->
          <TabPanel tabId="browse" :activeTab="activeTab" :noHeader="true">
            <div class="browse-panel">
              <FileManagerContent
                ref="fileManagerRef"
                :entries="dirEntries"
                :current-dir="currentDir"
                :current-file="currentFile"
                :show-hidden="showHidden"
                :sort-field="sortField"
                :sort-dir="sortDir"
                :dir-loading="store.state.dirLoading"
                @navigate-dir="handleNavigateDir"
                @select-file="handleBrowseSelectFile"
                @toggle-sort="handleToggleSort"
                @toggle-hidden="toggleHidden"
                @rename="handleRename"
                @delete="handleDelete"
                @batch-delete="handleBatchDelete"
                @refresh="handleRefresh"
                @open-terminal="handleOpenTerminal"
              />
              <FileOverlay
                ref="fileOverlayRef"
                :overlay-open="fileNav.overlayOpen.value"
                :current-file="currentFile"
                :toc-open="tocOpen"
                :search-open="searchOpen"
                :markdown-view-mode="markdownViewMode"
                :file-history-open="fileHistoryOpen"
                :toc-file="tocFile"
                :pdf-outline="pdfOutline"
                @delete="handleDelete($event)"
                @show-details="detailsOpen = true"
                @open-git-history="openFileHistory"
                @toggle-toc="tocOpen = !tocOpen"
                @toggle-search="currentFile?.content && (searchOpen = !searchOpen)"
                @toggle-view="markdownViewMode = markdownViewMode === 'rendered' ? 'raw' : 'rendered'"
                @refresh="handleRefresh"
                @jump="scrollToLine"
                @jump-page="handleJumpPdfPage"
                @close-git-history="fileHistoryOpen = false"
                @open-file="handleOverlayOpenFile"
                @overlay-close="handleOverlayClose"
                @overlay-go-back="handleOverlayGoBack"
              />
            </div>
          </TabPanel>

          <!-- History Tab -->
          <TabPanel tabId="history" :activeTab="activeTab" :noHeader="true">
            <GitHistoryContent
              mode="project"
              :active="activeTab === 'history'"
              @open-file="handleSelectFile"
            />
          </TabPanel>

          <!-- Proxy Tab -->
          <TabPanel tabId="proxy" :activeTab="activeTab" :noHeader="true">
            <ProxyPanelContent />
          </TabPanel>

          <!-- Terminal Tab -->
          <TabPanel tabId="terminal" :activeTab="activeTab" :noHeader="true">
            <TerminalPanelContent
              :requested-cwd="terminalRequestedCwd"
              :active="activeTab === 'terminal'"
              @cwd-handled="terminalRequestedCwd = null"
            />
          </TabPanel>

          <!-- Tasks Tab -->
          <TabPanel tabId="tasks" :activeTab="activeTab" :noHeader="true">
            <TaskTab :active="activeTab === 'tasks'" @open-file="handleTaskOpenFile" />
          </TabPanel>

          <!-- Settings Tab -->
          <TabPanel tabId="settings" :activeTab="activeTab" :noHeader="true">
            <SettingsPage :active="activeTab === 'settings'" />
          </TabPanel>
        </div>
      </main>

      <Lightbox />

      <ProjectDialog
        :open="projectDialogOpen"
        @close="projectDialogOpen = false"
      />

      <FileDetailsDialog
        :file="currentFile"
        :open="activeTab === 'browse' && fileNav.overlayOpen.value && detailsOpen"
        @close="detailsOpen = false"
      />

      <!-- Quote question floating bar -->
      <QuoteQuestionBar
        :visible="quoteQuestion.visible.value"
        :quoteData="quoteQuestion.quoteData.value"
        :sessionLabel="sessionIdentity.agentHeaderTitle.value"
        :sessionTitle="sessionIdentity.currentSessionTitle.value"
        :currentSessionId="sessionIdentity.currentSessionId.value"
        @send="quoteQuestion.sendMessage($event)"
        @close="quoteQuestion.closeSheet()"
        @pin="quoteQuestion.pinBar()"
        @unpin="quoteQuestion.unpinBar()"
        @open-sessions="handleQuoteOpenSessions"
      />

      <!-- Global session drawer — accessible from any tab -->
      <SessionDrawer
        ref="sessionDrawerRef"
        :open="sessionIdentity.sessionDrawerOpen.value"
        :currentSessionId="sessionIdentity.currentSessionId.value"
        :runningSessionIds="sessionIdentity.runningSessions.value"
        @close="sessionIdentity.sessionDrawerOpen.value = false"
        @select="handleSessionSelect"
        @create="handleSessionCreate"
        @delete="handleSessionDelete"
      />

      <!-- Bottom dock (tab bar) -->
      <div v-if="isAuthenticated" v-show="!anyKeyboardActive" class="bottom-dock-wrapper">
        <div class="bottom-dock">
          <div class="dock-center">
            <div class="dock-btn-wrap">
              <button class="dock-btn" :class="{ active: activeTab === 'chat', 'has-unread': store.state.chatUnreadCount > 0 && activeTab !== 'chat', 'has-running': store.state.chatRunning && activeTab !== 'chat' }" @click.stop="switchTab('chat')" :title="t('nav.chat')">
                <MessageSquare />
              </button>
              <span v-if="store.state.chatUnreadCount > 0 && activeTab !== 'chat'" class="dock-badge dock-badge-count">{{ store.state.chatUnreadCount }}</span>
            </div>
            <button class="dock-btn" :class="{ active: activeTab === 'browse' }" @click.stop="switchTab('browse')" :title="t('nav.fileManager')">
              <FolderOpen />
            </button>
            <button class="dock-btn" :class="{ active: activeTab === 'history' }" @click.stop="switchTab('history')" :title="t('git.history.projectHistory')">
              <GitBranch />
            </button>
            <div class="dock-btn-wrap">
              <button class="dock-btn" :class="{ active: activeTab === dockSlot4Tab, 'has-unread': dockSlot4Tab === 'tasks' && store.state.taskUnreadCount > 0 && activeTab !== 'tasks', 'just-completed': dockSlot4Tab === 'tasks' && store.state.taskJustCompleted && activeTab !== 'tasks', 'has-running': dockSlot4Tab === 'tasks' && store.state.taskRunning && activeTab !== 'tasks' }" @click.stop="handleDockSlot4Click" :title="dockSlot4Title">
                <component :is="dockSlot4Icon" />
              </button>
              <span v-if="dockSlot4Tab === 'tasks' && store.state.taskUnreadCount > 0 && activeTab !== 'tasks'" class="dock-badge dock-badge-count">{{ store.state.taskUnreadCount }}</span>
              <span v-if="dockSlot4Tab === 'terminal' && store.state.terminalSessionCount > 0 && activeTab !== 'terminal'" class="dock-badge dock-badge-count">{{ store.state.terminalSessionCount }}</span>
              <span v-if="dockSlot4Tab === 'proxy' && store.state.portForwardActiveCount > 0 && activeTab !== 'proxy'" class="dock-badge dock-badge-count">{{ store.state.portForwardActiveCount }}</span>
            </div>
            <div class="dock-overflow-wrapper">
              <button
                ref="overflowBtnRef"
                class="dock-btn dock-overflow-btn"
                :class="{ active: isOverflowTabActive }"
                @click.stop="toggleOverflowMenu"
                :title="overflowButtonTitle"
                :aria-expanded="overflowMenuOpen"
                aria-haspopup="menu"
              >
                <component :is="overflowButtonIcon" />
              </button>
            </div>
          </div>
        </div>
        <div class="dock-safe-area"></div>
      </div>
    </div>

    <Teleport to="body">
      <Transition name="dock-popup">
        <div v-if="overflowMenuOpen" class="dock-overflow-popup" :style="overflowPopupStyle" @keydown.escape="overflowMenuOpen = false">
          <button class="dock-overflow-item" :class="{ active: activeTab === 'tasks' }" @click.stop="handleOverflowSelect('tasks')">
            <CalendarClock :size="16" />
            <span>{{ t('nav.tasks') }}</span>
            <span v-if="store.state.taskUnreadCount > 0" class="dock-overflow-count">{{ store.state.taskUnreadCount }}</span>
          </button>
          <button v-if="!isSSHDisabled" class="dock-overflow-item" :class="{ active: activeTab === 'proxy' }" @click.stop="handleOverflowSelect('proxy')">
            <EthernetPort :size="16" />
            <span>{{ t('nav.portForward') }}</span>
            <span v-if="store.state.portForwardActiveCount > 0" class="dock-overflow-count">{{ store.state.portForwardActiveCount }}</span>
          </button>
          <button v-if="!isTerminalDisabled" class="dock-overflow-item" :class="{ active: activeTab === 'terminal' }" @click.stop="handleOverflowSelect('terminal')">
            <TerminalIcon :size="16" />
            <span>{{ t('terminal.title') }}</span>
            <span v-if="store.state.terminalSessionCount > 0" class="dock-overflow-count">{{ store.state.terminalSessionCount }}</span>
          </button>
          <div class="dock-overflow-divider"></div>
          <button class="dock-overflow-item" @click.stop="handleOverflowSettings">
            <Settings :size="16" />
            <span>{{ t('nav.settings') }}</span>
          </button>
        </div>
      </Transition>
    </Teleport>

    <ToastNotification :toast="toast" />
    <DialogOverlay />
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted, provide, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { MessageSquare, FolderOpen, GitBranch, EthernetPort, Terminal as TerminalIcon, CalendarClock, MoreHorizontal, Settings } from 'lucide-vue-next'
import AppHeader from './components/common/AppHeader.vue'
import TabPanel from './components/common/TabPanel.vue'
import FileOverlay from './components/file/FileOverlay.vue'
import Lightbox from './components/media/Lightbox.vue'
import ChatPanelContent from './components/chat/ChatPanelContent.vue'
import FileManagerContent from './components/file/FileManagerContent.vue'
import GitHistoryContent from './components/git/GitHistoryContent.vue'
import ProxyPanelContent from './components/proxy/ProxyPanelContent.vue'
import TerminalPanelContent from './components/terminal/TerminalPanelContent.vue'
import ProjectDialog from './components/ProjectDialog.vue'
import LoginView from './components/LoginView.vue'
import SetupWizard from './components/setup/SetupWizard.vue'
import TocDrawer from './components/TocDrawer.vue'
import FileDetailsDialog from './components/file/FileDetailsDialog.vue'
import GitHistoryDrawer from './components/git/GitHistoryDrawer.vue'
import SearchDrawer from './components/common/SearchDrawer.vue'
import ToastNotification from './components/common/ToastNotification.vue'
import DialogOverlay from './components/common/DialogOverlay.vue'
import SessionDrawer from './components/session/SessionDrawer.vue'
import QuoteQuestionBar from './components/common/QuoteQuestionBar.vue'
import HeaderMarquee from './components/common/HeaderMarquee.vue'
import SettingsPage from './components/settings/SettingsPage.vue'
import TaskTab from '@/components/task/TaskTab.vue'
import { useQuoteQuestion } from './composables/useQuoteQuestion.ts'
import { useTaskTab, registerSwitchTab, onTaskEvent } from '@/composables/useTaskTab.ts'
import { resetAgents } from '@/composables/useAgents'
import { useSessionIdentity, registerSessionDrawerRef, resetIdentity } from './composables/useSessionIdentity.ts'
import { loadSessionsOnce } from './composables/useChatSession.ts'
import { useToast } from './composables/useToast.ts'
import { useAppMode } from './composables/useAppMode.ts'
import { useTerminalKeyboard } from './composables/useTerminalKeyboard.ts'
import { useChatKeyboard } from './composables/useChatKeyboard.ts'
import { usePortForward } from './composables/usePortForward.ts'
import { useTerminalStatus } from './composables/useTerminalStatus.ts'
import { useFileWatch } from './composables/useFileWatch.ts'
import { useFileNavStack } from './composables/useFileNavStack'
import { refreshCurrentFile } from './composables/useFileRefresh.ts'
import { useGlobalEvents } from './composables/useGlobalEvents'
import { useEdgeSwipeBack, useFeatureBackHandler } from './composables/useEdgeSwipeBack'
import { handleBackNavigation } from './composables/useBackHandler'
import { store } from './stores/app.ts'
import { setPendingCommitNavigation } from './composables/useCommitNavigation.ts'
import { initMermaid, reRenderMermaid } from './utils/mermaid.ts'
import { getFileType } from './utils/fileType.ts'
import 'highlight.js/styles/github.css'
import 'highlight.js/styles/github-dark.css'
import './assets/hljs-light-override.css'

const isAuthenticated = ref(null)
const needsSetup = ref(false)
const { t } = useI18n()

// SPA hot project switch: key forces Vue to destroy/rebuild the app-container subtree
const projectKey = ref('initial')
const switchingProject = ref(false)

async function hotSwitchProject(newProjectPath, pendingSessionId) {
  // ── Phase 1: Fade out ──
  switchingProject.value = true
  await nextTick()
  await new Promise(r => setTimeout(r, 150))

  // ── Phase 2: POST to backend to set new project cookie ──
  try {
    await store.setProject(newProjectPath)
  } catch (err) {
    // Project doesn't exist — revert fade-out and show error
    switchingProject.value = false
    const msgKey = err?.msgKey
    if (msgKey === 'NotADirectory') {
      toast.show(t('appHeader.projectPathNotFound'), { icon: '⚠️', type: 'error', duration: 3000 })
      // Remove stale project from recent list
      fetch('/api/recent-projects', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: newProjectPath })
      }).catch(() => {})
    } else {
      toast.show(t('appHeader.switchProjectFailed', { error: err.message }), { icon: '⚠️', type: 'error', duration: 3000 })
    }
    return
  }

  // ── Phase 3: Reset module-level singletons ──
  // (store state already reset by setProject, but identity/agents need explicit reset)
  resetIdentity()
  resetAgents()
  fileNav.closeOverlay()

  // ── Phase 4: Change key → Vue destroys old component tree & builds new one ──
  projectKey.value = newProjectPath

  // ── Phase 5: Reload all project-scoped data (mirrors onMounted after auth) ──
  try { await store.loadProject() } catch (_) {
    toast.show(t('toast.projectLoadFailed'), { icon: '⚠️', type: 'error', duration: 0, onClick: () => location.reload() })
  }
  await sessionIdentity.initSessionFromAPI()
  loadSessionsOnce()
  try { await store.loadFiles('') } catch (_) {}
  store.loadGitBranch().catch(() => {})
  loadTasks()
  loadConfig()
  loadSSHInfo().catch(() => {})
  loadTerminalStatus().catch(() => {})
  if (isAppMode.value) syncToNative().catch(() => {})

  // ── Phase 6: Restore last opened file for the new project ──
  const lastFile = localStorage.getItem('clawbenchLastFile_' + store.state.projectRoot)
  if (lastFile && lastFile !== store.state.currentFile?.path) {
    const lastSlash = lastFile.lastIndexOf('/')
    store.state.currentDir = lastSlash > 0 ? lastFile.slice(0, lastSlash) : ''
    await store.loadFiles(store.state.currentDir)
    await store.selectFile(lastFile)
    if (store.state.currentFile?.error) store.state.currentFile = null
  }

  // ── Phase 7: Handle cross-project pending navigation ──
  if (pendingSessionId) {
    const checkReady = () => {
      if (sessionIdentity.currentSessionId.value) {
        switchTab('chat')
        sessionIdentity.switchSession(pendingSessionId)
      } else {
        setTimeout(checkReady, 100)
      }
    }
    checkReady()
  }

  // ── Phase 8: Fade in ──
  switchingProject.value = false
}

const activeTab = ref('chat')

function switchTab(tab) {
  if (activeTab.value === tab) return
  activeTab.value = tab
  if (tab === 'chat') {
    // Recalculate instead of blindly clearing — if the user switches to chat
    // but hasn't opened the unread session, the indicator should keep flashing.
    // loadSessionsOnce checks unreadCount per session (excluding current), so
    // it only clears when all sessions are actually read.
    loadSessionsOnce()
  }
  if (tab === 'tasks') {
    // Only stop dock button flash — don't clear per-task unread badges.
    // Per-task badges are cleared when the user enters that task's execution history.
    store.state.taskUnreadCount = 0
  }
  // Close overflow menu when switching to a main tab
  if (!overflowTabs.value.includes(tab)) {
    overflowMenuOpen.value = false
  }
}

/** Handle clawbench-open-session event from Android push notification tap */
function handleOpenSession(e) {
  const detail = e?.detail
  console.log('[ClawBench] clawbench-open-session event received, detail=', detail)
  if (!detail?.sessionId) {
    console.warn('[ClawBench] clawbench-open-session: no sessionId in detail, ignoring')
    return
  }
  const { sessionId, projectPath } = detail
  console.log('[ClawBench] clawbench-open-session: sessionId=', sessionId, 'projectPath=', projectPath, 'currentProject=', store.state.projectRoot)
  if (projectPath && projectPath !== store.state.projectRoot) {
    // Cross-project: hot switch without page reload
    console.log('[ClawBench] cross-project navigation, switching to', projectPath)
    hotSwitchProject(projectPath, sessionId).catch(() => {
      // If project switch fails, try same-project switch as fallback
      console.warn('[ClawBench] project switch failed, falling back to same-project switch')
      switchTab('chat')
      sessionIdentity.switchSession(sessionId)
    })
  } else {
    // Same project: lightweight switch
    console.log('[ClawBench] same-project navigation, switching to session', sessionId)
    switchTab('chat')
    sessionIdentity.switchSession(sessionId)
  }
}

/** Handle clawbench-open-task event from Android push notification tap (task execution) */
function handleOpenTask(e) {
  const detail = e?.detail
  console.log('[ClawBench] clawbench-open-task event received, detail=', detail)
  if (!detail?.taskId) {
    console.warn('[ClawBench] clawbench-open-task: no taskId in detail, ignoring')
    return
  }
  const { taskId, executionId, projectPath } = detail
  console.log('[ClawBench] clawbench-open-task: taskId=', taskId, 'executionId=', executionId, 'currentProject=', store.state.projectRoot)

  const navigateToTask = () => {
    switchTab('tasks')
    navigateToTaskHistory(Number(taskId))
    if (executionId) {
      // openExecDetail without execData will auto-fetch from API via refreshExecDetail
      openExecDetail(executionId)
    }
  }

  if (projectPath && projectPath !== store.state.projectRoot) {
    // Cross-project: switch project, store pending task navigation, then reload
    console.log('[ClawBench] cross-project navigation, switching to', projectPath)
    localStorage.setItem('clawbenchPendingNav', JSON.stringify({ taskId, executionId }))
    fetch('/api/project', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: projectPath }),
    }).then(() => {
      window.location.reload()
    }).catch(() => {
      console.warn('[ClawBench] project switch failed, falling back to same-project switch')
      navigateToTask()
    })
  } else {
    // Same project: lightweight switch
    console.log('[ClawBench] same-project navigation, switching to task', taskId)
    navigateToTask()
  }
}

const detailsOpen = ref(false)
const tocOpen = ref(false)
const searchOpen = ref(false)
const fileHistoryOpen = ref(false)

function openFileHistory() {
  fileHistoryOpen.value = true
}

const markdownViewMode = ref('rendered')

const toast = useToast()
provide('toast', toast)

const sessionIdentity = useSessionIdentity()

const showHidden = ref(false)
const { localConfig, setLocalConfig: setSetting, loadConfig, getServerValueWithDefault } = useSettingsConfig()
// Initialize from settings config (which handles legacy key migration)
showHidden.value = !!localConfig.showHidden
const sortField = ref(localConfig.sortField || null)
const sortDir = ref(localConfig.sortDir || 'asc')

useFileWatch({
  fileManagerOpen: computed(() => activeTab.value === 'browse'),
  currentDir: computed(() => store.state.currentDir),
  currentFile: computed(() => store.state.currentFile),
})

const fileNav = useFileNavStack()

const { isAppMode } = useAppMode()
const { syncToNative, sshInfo, loadSSHInfo } = usePortForward()
const { terminalRuntimeEnabled, loadTerminalStatus } = useTerminalStatus()
const isSSHDisabled = computed(() => sshInfo.value?.enabled === false)
// Use runtime status (actual server state) not config value — mirrors SSH pattern.
// Config may say enabled=true before restart; the runtime API returns false until
// the terminal manager actually exists.  `null` means "not yet loaded" → treat as
// disabled to avoid a flash of the terminal button on first mount.
const isTerminalDisabled = computed(() => terminalRuntimeEnabled.value !== true)
watch(isSSHDisabled, (disabled) => {
  if (disabled && activeTab.value === 'proxy') {
    switchTab('chat')
  }
})
watch(isTerminalDisabled, (disabled) => {
  if (disabled && activeTab.value === 'terminal') {
    switchTab('chat')
  }
})
const { navigateToTaskSettings, navigateToTaskHistory, openExecDetail, loadTasks } = useTaskTab()
registerSwitchTab(switchTab)

// Wire up WS global events
const { onEvent, init: initGlobalEvents, destroy: destroyGlobalEvents } = useGlobalEvents()
const removeTaskHandler = onEvent((event, data) => {
    if (event === 'task_update') {
        onTaskEvent(data)
    }
})

const handleForeground = () => {
    // Full state pull — 3rd defense layer
    loadSessionsOnce()
}

// Edge swipe back gesture detection (right-edge-left-swipe → go back)
useEdgeSwipeBack()

// 文件覆盖层的返回手势：文件栈优先
useFeatureBackHandler(
  'file-overlay',
  () => activeTab.value === 'browse' && fileNav.overlayOpen.value,
  () => {
    if (fileNav.canGoBack.value) {
      const prevPath = fileNav.goBack()
      if (prevPath) store.selectFile(prevPath)
    } else {
      fileNav.closeOverlay()
      tocOpen.value = false
      detailsOpen.value = false
      searchOpen.value = false
      fileHistoryOpen.value = false
    }
  },
)

// Android hardware back button / predictive back gesture → delegate to JS
window.addEventListener('clawbench-back-press', () => {
    // If any feature can handle back, do it and prevent the default Android behavior
    const handled = handleBackNavigation()
    // Set flag for the Android native code to check
    window.__clawbenchBackHandled = !!handled
})
window.addEventListener('clawbench-foreground', handleForeground)
const terminalRequestedCwd = ref(null)

// Hide AppHeader when terminal tab is active (always); remove padding-top too
// so terminal fills the full screen. Dock is hidden only when keyboard is open.
const terminalActive = computed(() => activeTab.value === 'terminal')
const { keyboardHeight: terminalKeyboardHeight } = useTerminalKeyboard()
const terminalKeyboardActive = computed(() => terminalActive.value && terminalKeyboardHeight.value > 0)

// Chat keyboard — on iOS WKWebView there's no adjustResize, so we detect
// keyboard via visualViewport and compensate in the web layer.
const { chatKeyboardHeight } = useChatKeyboard()
const chatKeyboardActive = computed(() => activeTab.value === 'chat' && chatKeyboardHeight.value > 0)

// Unified: any soft keyboard is open (terminal or chat)
const anyKeyboardActive = computed(() => terminalKeyboardActive.value || chatKeyboardActive.value)

const quoteQuestion = useQuoteQuestion()
const sessionDrawerRef = ref(null)

// Register SessionDrawer ref so identity.openAgentSelector() works
watch(sessionDrawerRef, (ref) => {
  if (ref) registerSessionDrawerRef(ref)
}, { immediate: true })

// Register identity actions (switchSession, createSession, etc.)
// These will be overwritten by ChatPanelContent when it mounts, but
// openAgentSelector is NOT registered here — it's handled via
// registerSessionDrawerRef above, which is independent.
function handleQuoteOpenSessions() {
  sessionIdentity.openSessionTab()
}

function handleSessionSelect(sessionId, backend) {
  sessionIdentity.switchSession(sessionId)
  sessionIdentity.sessionDrawerOpen.value = false
}

async function handleSessionCreate(agentId) {
  await sessionIdentity.createSession(agentId)
  // If drawer is still open, add the new session to the local list
  if (sessionDrawerRef.value && sessionIdentity.sessionDrawerOpen.value) {
    const id = sessionIdentity.currentSessionId.value
    if (id) {
      sessionDrawerRef.value.addSessionLocally({
        id,
        title: sessionIdentity.currentSessionTitle.value || '',
        backend: sessionIdentity.currentBackend.value || '',
        agentId: sessionIdentity.currentAgentId.value || '',
        model: sessionIdentity.currentModelName.value || '',
        updatedAt: new Date().toISOString(),
        unreadCount: 0,
      })
    }
  }
  sessionIdentity.sessionDrawerOpen.value = false
}

function handleSessionDelete(sessionId, backend) {
  sessionIdentity.deleteSession(sessionId, backend)
}

async function handleLoginSuccess() {
    // Load project BEFORE setting isAuthenticated so the backend sets the
    // clawbench_project cookie first. Without this, ChatPanelContent mounts
    // and calls loadHistory() which fails with NoProjectSelected (no cookie).
    try { await store.loadProject() } catch (_) { /* loadProject has its own error handling */ }
    // Check if setup wizard is needed (no agents + embedded Pi binary)
    try {
      const resp = await fetch('/api/setup/status')
      if (resp.ok) {
        const data = await resp.json()
        if (data.needs_setup) {
          isAuthenticated.value = true
          needsSetup.value = true
          return
        }
      }
    } catch { /* proceed to normal app if check fails */ }
    isAuthenticated.value = true
    initMermaid()
    await store.loadFiles('')
}

async function handleSetupComplete() {
    // Reset cached agents so fresh data is loaded
    resetAgents()

    // Register event listeners that were skipped during wizard (onMounted skipped them)
    window.addEventListener('open-file-manager', handleOpenFileManager)
    window.addEventListener('open-file-overlay', handleOpenFileOverlay)
    window.addEventListener('navigate-to-commit', handleNavigateToCommit)
    window.addEventListener('quote-sent', playQuoteEmitAnimation)
    window.addEventListener('scroll-to-line', (e) => { scrollToLine(e.detail.line) })
    window.addEventListener('clawbench-open-session', handleOpenSession)
    window.addEventListener('clawbench-open-task', handleOpenTask)
    document.addEventListener('click', handleOverflowOutsideClick)
    window.addEventListener('clawbench-theme-change', (e) => {
        const resolved = e.detail
        theme.value = resolved
        initMermaid()
        reRenderMermaid()
    })
    window.addEventListener('clawbench-showhidden-change', (e) => {
        showHidden.value = e.detail
    })
    window.addEventListener('clawbench-sort-change', (e) => {
        if (e.detail.field !== undefined) sortField.value = e.detail.field
        if (e.detail.dir !== undefined) sortDir.value = e.detail.dir
    })
    loadTasks()
    loadConfig()

    // Load project first so backend sets clawbench_project cookie
    try { await store.loadProject() } catch (_) { /* best effort */ }

    // Load agents and session data BEFORE switching to main UI
    // to prevent error flashes (e.g., "no agent configured")
    try {
        await sessionIdentity.initSessionFromAPI()
    } catch { /* best effort */ }
    loadSessionsOnce().catch(() => {})
    store.loadGitBranch().catch(() => {})

    // Now switch to main UI — agents and session are loaded
    needsSetup.value = false
    initMermaid()

    // Continue with remaining init that was deferred
    if (isAppMode.value) syncToNative().catch(() => {})
    loadSSHInfo().catch(() => {})
    loadTerminalStatus().catch(() => {})
    try { await store.loadFiles('') } catch (_) {}
}

const projectDialogOpen = ref(false)

function handleOpenProjectDialog() {
    projectDialogOpen.value = true
}

const theme = ref(localConfig.theme === 'auto'
    ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
    : (localConfig.theme || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')))

const dirEntries = computed(() => store.state.dirEntries)
const currentDir = computed(() => store.state.currentDir)
const currentFile = computed(() => store.state.currentFile)
const currentFileIsMarkdown = computed(() => {
    const f = currentFile.value
    if (!f) return false
    const ft = getFileType(f.name)
    return ft?.isMarkdown || ft?.isHtml || false
})
const projectRoot = computed(() => store.state.projectRoot)
const homeDir = computed(() => store.state.homeDir)

const tocFile = computed(() => {
    const f = currentFile.value
    if (!f || f.isImage || f.isAudio) return null
    // PDF: pass file even without content (outline comes from pdfOutline prop)
    if (f.isPdf) return f
    if (!f.content) return null
    const ft = getFileType(f.name)
    if (ft.isImage || ft.isAudio) return null
    return f
})

// PDF TOC integration
const fileOverlayRef = ref(null)
const fileManagerRef = ref(null)
const pdfOutline = computed(() => fileOverlayRef.value?.pdfOutline || [])
function handleJumpPdfPage(pageNum) {
    fileOverlayRef.value?.pdfScrollToPage(pageNum)
}

watch(() => currentFile.value, (f) => {
    tocOpen.value = false
    detailsOpen.value = false
    markdownViewMode.value = 'rendered'
})

function toggleHidden() {
    showHidden.value = !showHidden.value
    setSetting('showHidden', showHidden.value)
    store.loadFiles(store.state.currentDir)
}

function handleToggleSort(field) {
    if (sortField.value === field) {
        if (sortDir.value === 'asc') {
            sortDir.value = 'desc'
        } else {
            sortField.value = null
            sortDir.value = 'asc'
        }
    } else {
        sortField.value = field
        sortDir.value = 'asc'
    }
    setSetting('sortField', sortField.value)
    setSetting('sortDir', sortDir.value)
}

async function handleNavigateDir(path) {
    if (store.state.dirLoading) return
    await store.navigateToDir(path)
}

async function handleSelectFile(path) {
    const ok = await store.selectFile(path)
    if (ok) {
        activeTab.value = 'browse'
        fileNav.openFile(path)
    }
}

async function handleBrowseSelectFile(path) {
    if (fileManagerRef.value?.multiSelectState?.active) return
    const ok = await store.selectFile(path)
    if (ok) {
        fileNav.openFile(path)
    }
}

async function handleTaskOpenFile(filePath, lineStart) {
    const ok = await store.selectFile(filePath)
    if (ok) {
        activeTab.value = 'browse'
        fileNav.openFile(filePath)
        if (lineStart) scrollToLine(lineStart)
    }
}

function handleOverlayClose() {
    fileNav.closeOverlay()
    tocOpen.value = false
    detailsOpen.value = false
    searchOpen.value = false
    fileHistoryOpen.value = false
}

async function handleOverlayGoBack() {
    const prevPath = fileNav.goBack()
    if (prevPath) {
        await store.selectFile(prevPath)
    }
}

async function handleOverlayOpenFile(path) {
    const ok = await store.selectFile(path)
    if (ok) {
        fileNav.openFile(path)
    }
}

function handleOpenFileOverlay(e) {
    const { path, lineStart } = e.detail || {}
    if (!path) return
    activeTab.value = 'browse'
    fileNav.openFile(path)
    if (lineStart) scrollToLine(lineStart)
}

function onTaskCardClick(taskId) {
    navigateToTaskSettings(taskId)
    switchTab('tasks')
}

async function handleRename({ path, name }) {
    await store.renameFile(path, name)
}

async function handleDelete(path) {
    await store.deleteFile(path)
}

async function handleBatchDelete(paths) {
    await store.deleteFiles(paths)
}

async function handleRefresh() {
    await refreshCurrentFile({ loadDir: true, clearOnError: true })
}

function handleDockTerminal() {
    terminalRequestedCwd.value = null
    switchTab('terminal')
}

// Overflow menu state
const overflowMenuOpen = ref(false)
const overflowBtnRef = ref(null)
const overflowTabs = computed(() => {
  const tabs = ['tasks']
  if (!isSSHDisabled.value) tabs.push('proxy')
  if (!isTerminalDisabled.value) tabs.push('terminal')
  tabs.push('settings')
  return tabs
})
const overflowTabMeta = {
  tasks:   { icon: CalendarClock, titleKey: 'nav.tasks' },
  proxy:   { icon: EthernetPort, titleKey: 'nav.portForward' },
  terminal:{ icon: TerminalIcon, titleKey: 'terminal.title' },
  settings:{ icon: Settings, titleKey: 'nav.settings' },
}

// Dock slot 4: dynamic slot showing user's selected overflow item
const STORAGE_KEY_DOCK_SLOT4 = 'clawbench_dock_slot4'
const dockSlot4Tab = ref(localStorage.getItem(STORAGE_KEY_DOCK_SLOT4) || 'tasks')
const dockSlot4Icon = computed(() => overflowTabMeta[dockSlot4Tab.value]?.icon ?? CalendarClock)
const dockSlot4Title = computed(() => overflowTabMeta[dockSlot4Tab.value] ? t(overflowTabMeta[dockSlot4Tab.value].titleKey) : t('nav.tasks'))

function setDockSlot4(tab) {
  dockSlot4Tab.value = tab
  localStorage.setItem(STORAGE_KEY_DOCK_SLOT4, tab)
}

function handleDockSlot4Click() {
  const tab = dockSlot4Tab.value
  if (tab === 'terminal') {
    handleDockTerminal()
  } else {
    switchTab(tab)
  }
}

const isOverflowTabActive = computed(() => overflowTabs.value.includes(activeTab.value) && activeTab.value !== dockSlot4Tab.value)

// If the saved dock-slot4 tab becomes unavailable (e.g. terminal disabled), fall back to tasks
watch(overflowTabs, (tabs) => {
  if (!tabs.includes(dockSlot4Tab.value)) {
    setDockSlot4('tasks')
  }
})

const overflowPopupStyle = computed(() => {
  const btn = overflowBtnRef.value
  if (!btn) return {}
  const rect = btn.getBoundingClientRect()
  return {
    position: 'fixed',
    bottom: `${window.innerHeight - rect.top + 8}px`,
    right: `${window.innerWidth - rect.right}px`,
  }
})

const overflowButtonIcon = computed(() => {
  // Show the active overflow tab's icon, unless it's the dock-slot4 tab (which has its own button)
  if (activeTab.value === dockSlot4Tab.value) return MoreHorizontal
  return overflowTabMeta[activeTab.value]?.icon ?? MoreHorizontal
})

const overflowButtonTitle = computed(() => {
  if (activeTab.value === dockSlot4Tab.value) return t('nav.more')
  return overflowTabMeta[activeTab.value] ? t(overflowTabMeta[activeTab.value].titleKey) : t('nav.more')
})

function toggleOverflowMenu() {
  if (isOverflowTabActive.value && !overflowMenuOpen.value) {
    // If already on an overflow tab, first click opens menu to allow switching
    overflowMenuOpen.value = true
  } else if (overflowMenuOpen.value) {
    overflowMenuOpen.value = false
  } else {
    overflowMenuOpen.value = true
  }
}

function handleOverflowSelect(tab) {
  if (activeTab.value === tab) {
    // Already on this tab, just close the menu
    overflowMenuOpen.value = false
    return
  }
  overflowMenuOpen.value = false
  // Remember this tab as the dock slot 4 shortcut (except settings)
  if (tab !== 'settings') {
    setDockSlot4(tab)
  }
  if (tab === 'terminal') {
    handleDockTerminal()
  } else {
    switchTab(tab)
  }
}

function handleOverflowSettings() {
  overflowMenuOpen.value = false
  switchTab('settings')
}

// Close overflow menu on outside click
function handleOverflowOutsideClick(e) {
  if (overflowMenuOpen.value && !e.target.closest('.dock-overflow-popup') && !e.target.closest('.dock-overflow-btn')) {
    overflowMenuOpen.value = false
  }
}

function handleOpenTerminal(cwd) {
    terminalRequestedCwd.value = cwd || null
    switchTab('terminal')
}

function scrollToLine(line) {
    nextTick(() => {
        const el = document.querySelector(`.code-line[data-line="${line}"]`)
        if (!el) return
        el.scrollIntoView({ behavior: 'smooth', block: 'center' })
        el.classList.add('line-flash')
        el.addEventListener('animationend', () => el.classList.remove('line-flash'), { once: true })
    })
}

function toggleTheme() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
    applyTheme(theme.value)
}

function applyTheme(t) {
    document.documentElement.setAttribute('data-theme', t)
    setSetting('theme', t)
    document.documentElement.setAttribute('data-hljs-theme', t)
    initMermaid()
    reRenderMermaid()
}

provide('theme', theme)
provide('applyTheme', applyTheme)
provide('activeTab', activeTab)
provide('switchTab', switchTab)
provide('hotSwitchProject', hotSwitchProject)

function handleOpenFileManager() {
    activeTab.value = 'browse'
}

function handleNavigateToCommit(e) {
    const sha = e?.detail?.sha
    if (sha) {
        setPendingCommitNavigation(sha)
    }
    activeTab.value = 'history'
}

function playQuoteEmitAnimation(e) {
  const { from, to } = e?.detail ?? {}
  if (!from || !to) return
  const x0 = from.x, y0 = from.y, x1 = to.x, y1 = to.y
  const mx = (x0 + x1) / 2
  const my = Math.min(y0, y1) - 30
  const dot = document.createElement('div')
  dot.className = 'quote-emit-dot'
  dot.style.cssText = `
    position: fixed; width: 8px; height: 8px; border-radius: 50%;
    background: var(--accent-color, #0066cc);
    box-shadow: 0 0 10px 3px color-mix(in srgb, var(--accent-color, #0066cc) 50%, transparent);
    z-index: 9999; pointer-events: none; left: 0; top: 0; will-change: transform, opacity;
  `
  document.body.appendChild(dot)
  const duration = 420, start = performance.now()
  function animate(now) {
    const t = Math.min((now - start) / duration, 1)
    const ease = 1 - Math.pow(1 - t, 3)
    const x = (1 - ease) ** 2 * x0 + 2 * (1 - ease) * ease * mx + ease ** 2 * x1
    const y = (1 - ease) ** 2 * y0 + 2 * (1 - ease) * ease * my + ease ** 2 * y1
    const scale = t < 0.1 ? t / 0.1 : t > 0.85 ? 1 - (t - 0.85) / 0.15 : 1
    const opacity = t < 0.08 ? t / 0.08 : t > 0.7 ? 1 - (t - 0.7) / 0.3 : 1
    dot.style.transform = `translate(${x - 4}px, ${y - 4}px) scale(${scale})`
    dot.style.opacity = opacity
    if (t < 1) requestAnimationFrame(animate)
    else {
      dot.remove()
      const chatDockBtn = document.querySelector('.dock-center')?.querySelector('.dock-btn')
      if (chatDockBtn) {
        chatDockBtn.classList.add('quote-emit-receive')
        chatDockBtn.addEventListener('animationend', () => chatDockBtn.classList.remove('quote-emit-receive'), { once: true })
      }
    }
  }
  requestAnimationFrame(animate)
}

onMounted(async () => {
    applyTheme(theme.value)
    let resp
    try {
        resp = await fetch('/api/me')
    } catch (_) {
        isAuthenticated.value = false
        if (isAppMode.value) {
            toast.show(t('toast.serverUnreachableApp'), { icon: '⚠️', type: 'error', duration: 5000 })
        } else {
            toast.show(t('toast.serverUnreachableWeb'), { icon: '⚠️', type: 'error', duration: 0, onClick: () => location.reload() })
        }
        return
    }
    if (resp.ok) {
        isAuthenticated.value = true
    } else if (resp.status === 401 || resp.status === 403) {
        if (isAppMode.value && window.AndroidNative?.getPassword?.()) {
            const savedPwd = window.AndroidNative.getPassword()
            if (savedPwd) {
                try {
                    const loginRes = await fetch('/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: savedPwd }) })
                    if (loginRes.ok) {
                        isAuthenticated.value = true
                        if (window.AndroidNative?.setSSHPassword) window.AndroidNative.setSSHPassword(savedPwd)
                    } else { isAuthenticated.value = false; return }
                } catch (_) { isAuthenticated.value = false; return }
            } else { isAuthenticated.value = false; return }
        } else { isAuthenticated.value = false; return }
    } else {
        isAuthenticated.value = false
        if (isAppMode.value) {
            toast.show(t('toast.serverError'), { icon: '⚠️', type: 'error', duration: 5000 })
        } else {
            toast.show(t('toast.serverError'), { icon: '⚠️', type: 'error', duration: 0, onClick: () => location.reload() })
        }
        return
    }
    // Check if setup wizard is needed BEFORE any main app initialization.
    // If needs_setup, show wizard and skip all main UI loading to prevent
    // error flashes (e.g., "no agent configured", "failed to load chat history").
    try {
      const setupResp = await fetch('/api/setup/status')
      if (setupResp.ok) {
        const setupData = await setupResp.json()
        if (setupData.needs_setup) {
          needsSetup.value = true
          initGlobalEvents() // Needed for WS connection (setup wizard uses API)
          return  // Skip ALL main app initialization — wizard will handle it
        }
      }
    } catch { /* proceed to normal app if check fails */ }

    // ── Main app initialization (only when setup is NOT needed) ──
    initGlobalEvents()
    initMermaid()
    loadTasks()
    loadConfig()
    window.addEventListener('open-file-manager', handleOpenFileManager)
    window.addEventListener('open-file-overlay', handleOpenFileOverlay)
    window.addEventListener('navigate-to-commit', handleNavigateToCommit)
    window.addEventListener('quote-sent', playQuoteEmitAnimation)
    window.addEventListener('scroll-to-line', (e) => { scrollToLine(e.detail.line) })
    window.addEventListener('clawbench-open-session', handleOpenSession)
    window.addEventListener('clawbench-open-task', handleOpenTask)
    document.addEventListener('click', handleOverflowOutsideClick)
    window.addEventListener('clawbench-theme-change', (e) => {
        const resolved = e.detail
        theme.value = resolved
        initMermaid()
        reRenderMermaid()
    })
    window.addEventListener('clawbench-showhidden-change', (e) => {
        showHidden.value = e.detail
    })
    window.addEventListener('clawbench-sort-change', (e) => {
        if (e.detail.field !== undefined) sortField.value = e.detail.field
        if (e.detail.dir !== undefined) sortDir.value = e.detail.dir
    })
    // Load project first so the backend sets the clawbench_project cookie.
    // Without this, subsequent chat/session API calls fail with NoProjectSelected
    // on first login (no cookie yet) and show "加载聊天记录失败".
    try { await store.loadProject() } catch (_) {
        toast.show(t('toast.projectLoadFailed'), { icon: '⚠️', type: 'error', duration: 0, onClick: () => location.reload() }); return
    }
    await sessionIdentity.initSessionFromAPI()
    // Use loadSessionsOnce() which correctly sets chatUnread to true OR false.
    // The old code only set chatUnread=true and never corrected a stale true.
    loadSessionsOnce()
    if (isAppMode.value) syncToNative().catch(() => {})
    // Resume Android log capture if previously enabled
    if (isAppMode.value && localConfig.androidLogCapture) {
      try { if (window.AndroidNative?.startLogCapture) window.AndroidNative.startLogCapture() } catch {}
    }
    loadSSHInfo().catch(() => {})
    loadTerminalStatus().catch(() => {})
    try { await store.loadFiles('') } catch (_) {
        toast.show(t('toast.fileListLoadFailed'), { icon: '⚠️', type: 'error', duration: 6000 })
    }
    // Handle pending navigation from push notification deep link
    // (cross-project reload or cold start via AndroidNative bridge)
    const processPendingSessionNav = (navSessionId) => {
      // Wait for sessions to load before switching (max 3 seconds)
      let attempts = 0
      const checkReady = () => {
        if (sessionIdentity.currentSessionId.value) {
          switchTab('chat')
          sessionIdentity.switchSession(navSessionId)
        } else if (attempts < 30) {
          attempts++
          setTimeout(checkReady, 100)
        }
      }
      checkReady()
    }

    const processPendingTaskNav = async (navTaskId, navExecutionId) => {
      // Ensure tasks are loaded before navigating
      try {
        await loadTasks()
      } catch (_) {
        // Proceed anyway — the task list may already be populated
      }
      switchTab('tasks')
      navigateToTaskHistory(Number(navTaskId))
      if (navExecutionId) {
        // openExecDetail without execData will auto-fetch from API via refreshExecDetail
        openExecDetail(navExecutionId)
      }
    }

    // Check localStorage for pending navigation (cross-project reload)
    const pendingNav = localStorage.getItem('clawbenchPendingNav')
    if (pendingNav) {
      localStorage.removeItem('clawbenchPendingNav')
      try {
        const nav = JSON.parse(pendingNav)
        if (nav.taskId) {
          processPendingTaskNav(nav.taskId, nav.executionId)
        } else if (nav.sessionId) {
          processPendingSessionNav(nav.sessionId)
        }
      } catch (_) {}
    }

    // Check AndroidNative bridge for cold-start pending navigation
    // Also poll briefly in case CustomEvent was dispatched while WebView was paused
    if (isAppMode.value && window.AndroidNative?.getPendingNavigation) {
      let pollCleared = false
      const pollPendingNav = () => {
        try {
          const nav = window.AndroidNative.getPendingNavigation()
          console.log('[ClawBench] getPendingNavigation poll result:', nav)
          if (nav) {
            const parsed = JSON.parse(nav)
            const { sessionId, taskId, executionId, projectPath } = parsed
            if (taskId) {
              // Task notification navigation
              pollCleared = true
              if (projectPath && projectPath !== store.state.projectRoot) {
                localStorage.setItem('clawbenchPendingNav', JSON.stringify({ taskId, executionId }))
                fetch('/api/project', {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({ path: projectPath }),
                }).then(() => window.location.reload())
              } else {
                processPendingTaskNav(taskId, executionId)
              }
            } else if (sessionId) {
              // Session notification navigation
              // Navigation data found — stop polling
              pollCleared = true
              if (projectPath && projectPath !== store.state.projectRoot) {
                // Need to switch project first — use hot switch instead of reload
                hotSwitchProject(projectPath, sessionId)
              } else {
                processPendingSessionNav(sessionId)
              }
            }
          }
        } catch (_) {}
      }
      // Poll immediately and then every 500ms for up to 3 seconds
      pollPendingNav()
      let pollCount = 0
      const pollInterval = setInterval(() => {
        if (pollCleared) { clearInterval(pollInterval); return }
        pollPendingNav()
        pollCount++
        if (pollCount >= 6) clearInterval(pollInterval) // 3 seconds total
      }, 500)
    }
    const lastFile = localStorage.getItem('clawbenchLastFile_' + store.state.projectRoot)
    if (lastFile && lastFile !== store.state.currentFile?.path) {
        const lastSlash = lastFile.lastIndexOf('/')
        store.state.currentDir = lastSlash > 0 ? lastFile.slice(0, lastSlash) : ''
        await store.loadFiles(store.state.currentDir)
        await store.selectFile(lastFile)
        if (store.state.currentFile?.error) store.state.currentFile = null
        // 不自动切换 Tab 或打开覆盖层，保持默认 tab（chat）
        // 用户切到 browse 时可以在 handleBrowseSelectFile 中打开覆盖层
    }
})

onUnmounted(() => {
    removeTaskHandler()
    window.removeEventListener('clawbench-foreground', handleForeground)
    destroyGlobalEvents()
    window.removeEventListener('open-file-manager', handleOpenFileManager)
    window.removeEventListener('open-file-overlay', handleOpenFileOverlay)
    window.removeEventListener('navigate-to-commit', handleNavigateToCommit)
    window.removeEventListener('quote-sent', playQuoteEmitAnimation)
    window.removeEventListener('clawbench-open-session', handleOpenSession)
    window.removeEventListener('clawbench-open-task', handleOpenTask)
    document.removeEventListener('click', handleOverflowOutsideClick)
})
</script>

<style scoped>
/* SPA hot project switch: fade transition to mask intermediate state */
.app-container {
    transition: opacity 0.15s ease;
}
.app-container.project-switching {
    opacity: 0;
}

.browse-panel {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* When terminal tab is active, remove header padding so content expands to top */
.chrome-hidden {
    padding-top: 0 !important;
}

/* When chat keyboard is open on iOS (no adjustResize), shrink the app container
   from the bottom so content stays above the keyboard. */
.chat-keyboard-open {
    bottom: v-bind(chatKeyboardHeight + 'px') !important;
}

.bottom-dock-wrapper {
    flex-shrink: 0;
    -webkit-tap-highlight-color: transparent;
    user-select: none;
}

.bottom-dock {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 6px 8px;
    background: var(--bg-primary);
    border-top: 1px solid color-mix(in srgb, var(--border-color) 40%, transparent);
    border-bottom: 1px solid color-mix(in srgb, var(--border-color) 40%, transparent);
}

.dock-safe-area {
    height: env(safe-area-inset-bottom, 0px);
}

.dock-center {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
}

.dock-btn {
    position: relative;
    width: 34px;
    height: 34px;
    border: none;
    border-radius: 50%;
    background: var(--bg-tertiary);
    color: var(--text-secondary);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.2s, color 0.2s, transform 0.15s;
}

.dock-btn:hover {
    background: var(--bg-secondary);
    color: var(--text-primary);
}

.dock-btn:active {
    transform: scale(0.92);
}

.dock-btn.active {
    background: var(--accent-color);
    color: #fff;
}

.dock-btn.active:hover {
    background: var(--accent-hover);
    color: #fff;
}

.dock-btn svg {
    width: 16px;
    height: 16px;
}

.dock-btn.disabled {
    opacity: 0.3;
    cursor: default;
}

/* Unread indicator — static badge dot (top-right corner).
 * Uses a real <span> element outside the button so it's not clipped by overflow:hidden.
 * Positioned on .dock-btn-wrap which wraps both button and badge. */
.dock-btn-wrap {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
}

.dock-badge {
    position: absolute;
    top: 0;
    right: 0;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent-color, #0066cc);
    z-index: 2;
    pointer-events: none;
}

.dock-badge-count {
    width: auto;
    height: auto;
    min-width: 16px;
    padding: 0 4px;
    border-radius: 8px;
    font-size: 10px;
    font-weight: 700;
    line-height: 16px;
    text-align: center;
    color: #fff;
    top: -4px;
    right: -6px;
}

.dock-btn.has-running {
    position: relative;
    isolation: isolate;
    overflow: hidden;
    border-color: transparent;
    box-shadow: 0 0 4px 1px color-mix(in srgb, var(--accent-color, #0066cc) 25%, transparent);
}
.dock-btn.has-running::before {
    content: '';
    position: absolute;
    inset: -2px;
    border-radius: inherit;
    background: conic-gradient(
        from 0deg,
        transparent 0%,
        color-mix(in srgb, var(--accent-color, #0066cc) 15%, rgba(255,255,255,0.1)) 8%,
        color-mix(in srgb, var(--accent-color, #0066cc) 50%, rgba(255,255,255,0.3)) 16%,
        var(--accent-color, #0066cc) 22%,
        color-mix(in srgb, var(--accent-color, #0066cc) 50%, rgba(255,255,255,0.3)) 28%,
        color-mix(in srgb, var(--accent-color, #0066cc) 15%, rgba(255,255,255,0.1)) 36%,
        transparent 50%
    );
    animation: dock-spin-light 2s linear infinite;
    z-index: -2;
}
.dock-btn.has-running::after {
    content: '';
    position: absolute;
    inset: 1.5px;
    border-radius: inherit;
    background: var(--bg-tertiary);
    z-index: -1;
}

@keyframes dock-spin-light {
    to { transform: rotate(360deg); }
}

.dock-btn.just-completed {
    animation: dock-completed-flash 0.5s ease-out;
}

@keyframes dock-completed-flash {
    0% { transform: scale(1); box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-color, #0066cc) 0%, transparent); }
    30% { transform: scale(1.2); box-shadow: 0 0 12px 4px color-mix(in srgb, var(--accent-color, #0066cc) 50%, transparent); }
    60% { transform: scale(1.1); box-shadow: 0 0 8px 2px color-mix(in srgb, var(--accent-color, #0066cc) 30%, transparent); }
    100% { transform: scale(1); box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-color, #0066cc) 0%, transparent); }
}

.dock-btn.quote-emit-receive {
    animation: quote-emit-pulse 0.4s ease-out;
}

@keyframes quote-emit-pulse {
    0% { transform: scale(1); box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-color, #0066cc) 60%, transparent); }
    40% { transform: scale(1.25); box-shadow: 0 0 14px 4px color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent); }
    100% { transform: scale(1); box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-color, #0066cc) 0%, transparent); }
}

/* Overflow menu */
.dock-overflow-popup {
    background: var(--bg-elevated, var(--bg-primary));
    border: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);
    border-radius: 12px;
    padding: 4px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
    z-index: 9999;
    min-width: 140px;
}

.dock-overflow-popup::after {
    content: '';
    position: absolute;
    bottom: -6px;
    right: 14px;
    width: 12px;
    height: 12px;
    background: var(--bg-elevated, var(--bg-primary));
    border-right: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);
    border-bottom: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);
    transform: rotate(45deg);
}

.dock-overflow-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px 12px;
    border: none;
    border-radius: 8px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 13px;
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
    white-space: nowrap;
}

.dock-overflow-item:hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
}

@media (hover: none) {
    .dock-overflow-item:hover {
        background: transparent;
        color: var(--text-secondary);
    }
}

.dock-overflow-item.active {
    background: color-mix(in srgb, var(--accent-color) 15%, transparent);
    color: var(--accent-color);
}

.dock-overflow-count {
    margin-left: auto;
    min-width: 18px;
    padding: 0 5px;
    border-radius: 9px;
    background: var(--accent-color);
    color: #fff;
    font-size: 11px;
    font-weight: 700;
    line-height: 18px;
    text-align: center;
    flex-shrink: 0;
}

.dock-overflow-divider {
    height: 1px;
    background: var(--border-color);
    margin: 4px 8px;
}

/* Popup transition */
.dock-popup-enter-active {
    transition: opacity 0.15s ease, transform 0.15s ease;
}
.dock-popup-leave-active {
    transition: opacity 0.1s ease, transform 0.1s ease;
}
.dock-popup-enter-from,
.dock-popup-leave-to {
    opacity: 0;
    transform: translateY(4px) scale(0.95);
}
</style>
