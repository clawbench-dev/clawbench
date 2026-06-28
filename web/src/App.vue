<template>
  <div>
    <!-- Loading state: show nothing while checking auth -->
    <div v-if="isAuthenticated === null" style="display:none" />

    <!-- Login -->
    <LoginView v-else-if="!isAuthenticated" @login-success="handleLoginSuccess" />

    <!-- Main app -->
    <div v-else class="app-container" :class="{ 'chat-keyboard-open': chatKeyboardActive, 'terminal-keyboard-open': terminalKeyboardNeedsShrink, 'project-switching': switchingProject }" :key="projectKey">
      <WelcomeOverlay ref="welcomeOverlay" />
      <AppHeader
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
                @navigate-back="handleNavigateBack"
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
                :file-loading="store.state.fileLoading"
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
            <div class="dock-active-indicator" :style="dockIndicatorStyle"></div>
            <div class="dock-btn-wrap">
              <button class="dock-btn" :class="{ active: activeTab === 'chat', 'has-unread': store.state.chatUnreadCount > 0 && activeTab !== 'chat', 'has-running': store.state.chatRunning && activeTab !== 'chat' }" @click.stop="switchTab('chat')" :title="t('nav.chat')">
                <MessageSquare />
              </button>
              <span v-if="store.state.chatUnreadCount > 0 && activeTab !== 'chat'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': chatBadgeAnim }" @animationend="chatBadgeAnim = false">{{ formatBadgeCount(store.state.chatUnreadCount) }}</span>
            </div>
            <button class="dock-btn" :class="{ active: activeTab === 'browse' }" @click.stop="switchTab('browse')" :title="t('nav.fileManager')">
              <FolderOpen />
            </button>
            <div class="dock-btn-wrap">
              <button class="dock-btn" :class="{ active: activeTab === 'history' }" @click.stop="switchTab('history')" :title="t('git.history.projectHistory')">
                <GitBranch />
              </button>
              <span v-if="store.state.gitWorkingTreeChangeCount > 0 && activeTab !== 'history'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': historyBadgeAnim }" @animationend="historyBadgeAnim = false">{{ formatBadgeCount(store.state.gitWorkingTreeChangeCount) }}</span>
            </div>
            <div class="dock-btn-wrap">
              <button class="dock-btn" :class="{ active: activeTab === dockSlot4Tab, 'has-unread': dockSlot4Tab === 'tasks' && store.state.taskUnreadCount > 0 && activeTab !== 'tasks', 'just-completed': dockSlot4Tab === 'tasks' && store.state.taskJustCompleted && activeTab !== 'tasks', 'has-running': dockSlot4Tab === 'tasks' && store.state.taskRunning && activeTab !== 'tasks' }" @click.stop="handleDockSlot4Click" :title="dockSlot4Title">
                <component :is="dockSlot4Icon" />
              </button>
              <span v-if="dockSlot4Tab === 'tasks' && store.state.taskUnreadCount > 0 && activeTab !== 'tasks'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': taskBadgeAnim }" @animationend="taskBadgeAnim = false">{{ formatBadgeCount(store.state.taskUnreadCount) }}</span>
              <span v-if="dockSlot4Tab === 'terminal' && store.state.terminalSessionCount > 0 && activeTab !== 'terminal'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': terminalBadgeAnim }" @animationend="terminalBadgeAnim = false">{{ formatBadgeCount(store.state.terminalSessionCount) }}</span>
              <span v-if="dockSlot4Tab === 'proxy' && store.state.portForwardActiveCount > 0 && activeTab !== 'proxy'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': proxyBadgeAnim }" @animationend="proxyBadgeAnim = false">{{ formatBadgeCount(store.state.portForwardActiveCount) }}</span>
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
              <span v-if="overflowBadgeCount > 0 && !isOverflowTabActive" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': overflowBadgeAnim }" @animationend="overflowBadgeAnim = false">{{ formatBadgeCount(overflowBadgeCount) }}</span>
            </div>
          </div>
        </div>
        <div class="dock-safe-area"></div>
      </div>
    </div>

    <Teleport to="body">
      <Transition name="dock-popup">
        <div v-if="overflowMenuOpen" class="dock-overflow-popup" :style="overflowPopupStyle" @keydown.escape="overflowMenuOpen = false">
          <button v-if="dockSlot4Tab !== 'tasks'" class="dock-overflow-item" :class="{ active: activeTab === 'tasks' }" @click.stop="handleOverflowSelect('tasks')">
            <CalendarClock :size="16" />
            <span>{{ t('nav.tasks') }}</span>
            <span v-if="store.state.taskUnreadCount > 0" class="dock-overflow-count" :class="{ 'dock-badge-pop': taskBadgeAnim }" @animationend="taskBadgeAnim = false">{{ formatBadgeCount(store.state.taskUnreadCount) }}</span>
          </button>
          <button v-if="!isSSHDisabled && dockSlot4Tab !== 'proxy'" class="dock-overflow-item" :class="{ active: activeTab === 'proxy' }" @click.stop="handleOverflowSelect('proxy')">
            <EthernetPort :size="16" />
            <span>{{ t('nav.portForward') }}</span>
            <span v-if="store.state.portForwardActiveCount > 0" class="dock-overflow-count" :class="{ 'dock-badge-pop': proxyBadgeAnim }" @animationend="proxyBadgeAnim = false">{{ formatBadgeCount(store.state.portForwardActiveCount) }}</span>
          </button>
          <button v-if="!isTerminalDisabled && dockSlot4Tab !== 'terminal'" class="dock-overflow-item" :class="{ active: activeTab === 'terminal' }" @click.stop="handleOverflowSelect('terminal')">
            <TerminalIcon :size="16" />
            <span>{{ t('terminal.title') }}</span>
            <span v-if="store.state.terminalSessionCount > 0" class="dock-overflow-count" :class="{ 'dock-badge-pop': terminalBadgeAnim }" @animationend="terminalBadgeAnim = false">{{ formatBadgeCount(store.state.terminalSessionCount) }}</span>
          </button>
          <button v-if="dockSlot4Tab !== 'settings'" class="dock-overflow-item" :class="{ active: activeTab === 'settings' }" @click.stop="handleOverflowSelect('settings')">
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
import { appLog } from '@/utils/appLog'
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
import WelcomeOverlay from './components/WelcomeOverlay.vue'
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
import { loadSessionsOnce, resetChatSessionState } from './composables/useChatSession.ts'
import { resetTaskTabState } from './composables/useTaskTab.ts'
import { clearPlanState } from './composables/usePlanProgress.ts'
import { useToast } from './composables/useToast.ts'
import { gt } from './composables/useLocale'
import { useAppMode } from './composables/useAppMode.ts'
import { useTerminalKeyboard } from './composables/useTerminalKeyboard.ts'
import { useChatKeyboard } from './composables/useChatKeyboard.ts'
import { usePortForward } from './composables/usePortForward.ts'
import { useTerminalStatus } from './composables/useTerminalStatus.ts'
import { useFileWatch } from './composables/useFileWatch.ts'
import { useFileNavStack } from './composables/useFileNavStack'
import { refreshCurrentFile } from './composables/useFileRefresh.ts'
import { useGlobalEvents } from './composables/useGlobalEvents'
import { useEdgeSwipeBack, useFeatureBackHandler, PRIORITY_OVERLAY } from './composables/useEdgeSwipeBack'
import { handleBackNavigation, requestExitConfirm } from './composables/useBackHandler'
import { store } from './stores/app.ts'
import { setPendingCommitNavigation } from './composables/useCommitNavigation.ts'
import { initMermaid, reRenderMermaid } from './utils/mermaid.ts'
import { getFileType } from './utils/fileType.ts'
import { formatBadgeCount } from './utils/format.ts'
import 'highlight.js/styles/github.css'
import 'highlight.js/styles/github-dark.css'
import './assets/hljs-light-override.css'

const isAuthenticated = ref(null)
const { t } = useI18n()
const TAG = 'ClawBench'

// SPA hot project switch: key forces Vue to destroy/rebuild the app-container subtree
const projectKey = ref('initial')
const switchingProject = ref(false)

async function hotSwitchProject(newProjectPath, pendingSessionId) {
  // ── Phase 1: Fade out ──
  switchingProject.value = true
  await nextTick()
  await new Promise(r => setTimeout(r, 150))

  // ── Phase 2: POST to backend — now returns full init data (roots, homeDir, config) ──
  try {
    await store.setProject(newProjectPath)
  } catch (err) {
    // Project doesn't exist — revert fade-out and show error
    switchingProject.value = false
    const msgKey = err?.msgKey
    if (msgKey === 'NotADirectory') {
      toast.show(t('appHeader.projectPathNotFound'), { icon: '⚠️', type: 'error', duration: 3000 })
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
  resetIdentity()
  resetAgents()
  resetChatSessionState()
  clearPlanState()
  resetTaskTabState()
  fileNav.closeOverlay()

  // ── Phase 4: Change key → Vue destroys old component tree & builds new one ──
  projectKey.value = newProjectPath

  // ── Phase 5: Fade in EARLY — UI is visible while data loads in background ──
  //  store.setProject() already filled projectRoot, rootPaths, homeDir, config from the
  //  expanded POST response, so no need for loadProject(). ChatPanel's
  //  watch({ immediate: true }) will call loadHistory which recovers session identity
  //  AND messages in one request, so initSessionFromAPI() is redundant here.
  switchingProject.value = false

  // ── Phase 6: Background data loading — all independent, fully parallel, non-blocking ──
  Promise.allSettled([
    store.loadFiles(''),
    sessionIdentity.initSessionFromAPI(),
    loadSessionsOnce(),
    store.loadGitBranch(),
    loadTasks(),
    loadConfig(),
    loadSSHInfo(),
    loadTerminalStatus(),
  ])
  if (isAppMode.value) syncToNative().catch(() => {})

  // ── Phase 7: Handle cross-project pending navigation ──
  if (pendingSessionId) {
    // Watch for session identity to be ready instead of polling
    const stopWatch = watch(
      () => sessionIdentity.currentSessionId.value,
      (id) => {
        if (id) {
          stopWatch()
          switchTab('chat')
          sessionIdentity.switchSession(pendingSessionId)
        }
      },
      { immediate: true }
    )
  }
}

const activeTab = ref('chat')

// Dock active indicator — water-drop sliding highlight
// 5 buttons evenly spaced: btn_width=34, gap=12, step=46
// Index: chat=0, browse=1, history=2, slot4=3, overflow=4
const DOCK_STEP = 46 // 34 (btn width) + 12 (gap)

const dockActiveIndex = computed(() => {
  if (['chat', 'browse', 'history'].includes(activeTab.value)) {
    return ['chat', 'browse', 'history'].indexOf(activeTab.value)
  }
  if (activeTab.value === dockSlot4Tab.value) return 3
  return 4 // overflow
})

const dockIndicatorStyle = computed(() => ({
  transform: `translateX(${dockActiveIndex.value * DOCK_STEP}px)`,
}))

function switchTab(tab) {
  if (activeTab.value === tab) return
  activeTab.value = tab
  // Close file browser panels when leaving browse tab — they are teleported
  // to <body> via BottomSheet so v-show hiding the tab-panel doesn't affect them.
  if (activeTab.value !== 'browse') {
    tocOpen.value = false
    searchOpen.value = false
    fileHistoryOpen.value = false
    detailsOpen.value = false
  }
  if (tab === 'browse') {
    store.loadFiles(store.state.currentDir)
  }
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
    loadTasks()
  }
  // Close overflow menu when switching to a main tab
  if (!overflowTabs.value.includes(tab)) {
    overflowMenuOpen.value = false
  }
}

/** Handle clawbench-open-session event from Android push notification tap */
function handleOpenSession(e) {
  const detail = e?.detail
  appLog.d(TAG, 'clawbench-open-session event received, detail=', detail)
  if (!detail?.sessionId) {
    appLog.w(TAG, 'clawbench-open-session: no sessionId in detail, ignoring')
    return
  }
  const { sessionId, projectPath } = detail
  appLog.d(TAG, 'clawbench-open-session: sessionId=', sessionId, 'projectPath=', projectPath, 'currentProject=', store.state.projectRoot)
  if (projectPath && projectPath !== store.state.projectRoot) {
    // Cross-project: hot switch without page reload
    appLog.d(TAG, 'cross-project navigation, switching to', projectPath)
    hotSwitchProject(projectPath, sessionId).catch(() => {
      // If project switch fails, try same-project switch as fallback
      appLog.w(TAG, 'project switch failed, falling back to same-project switch')
      switchTab('chat')
      sessionIdentity.switchSession(sessionId)
    })
  } else {
    // Same project: lightweight switch
    appLog.d(TAG, 'same-project navigation, switching to session', sessionId)
    switchTab('chat')
    sessionIdentity.switchSession(sessionId)
  }
}

/** Handle clawbench-open-task event from Android push notification tap (task execution) */
function handleOpenTask(e) {
  const detail = e?.detail
  appLog.d(TAG, 'clawbench-open-task event received, detail=', detail)
  if (!detail?.taskId) {
    appLog.w(TAG, 'clawbench-open-task: no taskId in detail, ignoring')
    return
  }
  const { taskId, executionId, projectPath } = detail
  appLog.d(TAG, 'clawbench-open-task: taskId=', taskId, 'executionId=', executionId, 'currentProject=', store.state.projectRoot)

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
    appLog.d(TAG, 'cross-project navigation, switching to', projectPath)
    localStorage.setItem('clawbenchPendingNav', JSON.stringify({ taskId, executionId }))
    fetch('/api/project', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: projectPath }),
    }).then(() => {
      window.location.reload()
    }).catch(() => {
      appLog.w(TAG, 'project switch failed, falling back to same-project switch')
      navigateToTask()
    })
  } else {
    // Same project: lightweight switch
    appLog.d(TAG, 'same-project navigation, switching to task', taskId)
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

function closeOverlayAndSync() {
  fileNav.closeOverlay()
  tocOpen.value = false
  detailsOpen.value = false
  searchOpen.value = false
  fileHistoryOpen.value = false
}

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
    // Only refresh after initialization is complete — during cold start
    // the onMounted handler loads fresh data; refreshing here with stale
    // state (e.g. old currentDir from WebView cache) would show wrong dir.
    if (!isAuthenticated.value) return
    // Full state pull — refresh everything that may have changed while backgrounded
    loadSessionsOnce()
    store.loadFiles(store.state.currentDir)
    store.loadGitBranch()
    loadTasks()
    loadTerminalStatus()
    if (store.state.currentFile?.path) {
        refreshCurrentFile()
    }
}

// Edge swipe back gesture detection (right-edge-left-swipe → go back)
useEdgeSwipeBack()

// 文件覆盖层的返回手势：overlay 优先级高于 browse，无论 mount 顺序如何
useFeatureBackHandler(
  'file-overlay',
  () => activeTab.value === 'browse' && fileNav.overlayOpen.value,
  () => {
    if (fileNav.canGoBack.value) {
      const prevPath = fileNav.goBack()
      if (prevPath) store.selectFile(prevPath)
    } else {
      closeOverlayAndSync()
    }
  },
  PRIORITY_OVERLAY,
)

// Android hardware back button / predictive back gesture → delegate to JS
window.addEventListener('clawbench-back-press', () => {
    // If any feature can handle back, do it and prevent the default Android behavior
    const handled = handleBackNavigation()
    if (handled) {
        window.__clawbenchBackHandled = true
    } else {
        // No back stack — double-back-to-exit pattern
        if (requestExitConfirm()) {
            // Second press within timeout → allow native exit
            window.__clawbenchBackHandled = false
        } else {
            // First press → show tip, prevent exit
            window.__clawbenchBackHandled = true
            toast.show(t('toast.swipeAgainToExit'), { icon: '👋', type: 'info', duration: 2000 })
        }
    }
})
window.addEventListener('clawbench-foreground', handleForeground)
const terminalRequestedCwd = ref(null)

// Terminal keyboard height for detecting when soft keyboard is open in terminal tab.
// Dock is hidden only when keyboard is open.
const terminalActive = computed(() => activeTab.value === 'terminal')
const { keyboardHeight: terminalKeyboardHeight, isAdjustResize: terminalIsAdjustResize } = useTerminalKeyboard()
const terminalKeyboardActive = computed(() => terminalActive.value && terminalKeyboardHeight.value > 0)
// In PWA standalone / iOS (no adjustResize), position:fixed app-container doesn't
// auto-shrink when keyboard opens. We must compensate with CSS bottom shrink,
// same as the chat-keyboard-open mechanism. On Android native (adjustResize),
// innerHeight shrinks so the fixed container auto-adjusts — no CSS needed.
const terminalKeyboardNeedsShrink = computed(() => terminalKeyboardActive.value && !terminalIsAdjustResize.value)

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

/** Register global DOM event listeners (idempotent — safe to call multiple times). */
let appEventListenersRegistered = false
function registerAppEventListeners() {
  if (appEventListenersRegistered) return
  appEventListenersRegistered = true
  window.addEventListener('open-file-manager', handleOpenFileManager)
  window.addEventListener('open-file-overlay', handleOpenFileOverlay)
  window.addEventListener('close-file-overlay', handleOverlayClose)
  window.addEventListener('navigate-to-commit', handleNavigateToCommit)
  window.addEventListener('quote-sent', playQuoteEmitAnimation)
  window.addEventListener('attach-to-chat', playQuoteEmitAnimation)
  window.addEventListener('scroll-to-line', (e) => { scrollToLine(e.detail.line, e.detail.lineEnd) })
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
}

/**
 * Full app initialization: load project cookie, session identity,
 * agents, config, and register all infrastructure.
 * Must complete BEFORE isAuthenticated is set to true (which triggers
 * ChatPanelContent mount and loadHistory).
 * Returns false if a fatal error occurred (callers should not set isAuthenticated).
 */
async function initializeApp() {
  // 1. Prerequisite data — must complete before UI renders
  //    loadProject sets clawbench_project cookie (needed by loadHistory).
  //    initSessionFromAPI sets session identity (needed by ChatPanelContent).
  try { await store.loadProject() } catch (_) {
      toast.show(t('toast.projectLoadFailed'), { icon: '⚠️', type: 'error', duration: 0, onClick: () => location.reload() }); return false
  }
  await sessionIdentity.initSessionFromAPI()

  // 2. Infrastructure — global events, rendering, config
  initGlobalEvents()
  initMermaid()
  loadTasks()
  loadConfig()
  registerAppEventListeners()

  // 3. Secondary data — non-blocking, can load in parallel with UI render
  loadSessionsOnce()
  if (isAppMode.value) syncToNative().catch(() => {})
  if (isAppMode.value && localConfig.androidLogCapture) {
    try { if (window.AndroidNative?.startLogCapture) window.AndroidNative.startLogCapture() } catch {}
  }
  loadSSHInfo().catch(() => {})
  loadTerminalStatus().catch(() => {})
  store.loadGitBranch().catch(() => {})
  try { await store.loadFiles('') } catch (_) {
      toast.show(t('toast.fileListLoadFailed'), { icon: '⚠️', type: 'error', duration: 6000 })
  }
  return true
}

async function handleLoginSuccess() {
    // Full initialization BEFORE setting isAuthenticated — ensures
    // clawbench_project cookie, session identity, and all infrastructure
    // are ready before ChatPanelContent mounts and calls loadHistory().
    if (!(await initializeApp())) return
    // Clean up legacy localStorage keys (no longer used)
    Object.keys(localStorage).filter(k => k.startsWith('clawbenchLastFile_') || k.startsWith('clawbenchLastDir_')).forEach(k => localStorage.removeItem(k))
    isAuthenticated.value = true
    await nextTick()
    welcomeOverlay.value?.show()
}

const projectDialogOpen = ref(false)
const welcomeOverlay = ref(null)

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
    await store.navigateToDir(path)
}

async function handleNavigateBack() {
    await store.navigateToParentDir()
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
    closeOverlayAndSync()
}

async function handleOverlayGoBack() {
    if (fileNav.canGoBack.value) {
        const prevPath = fileNav.goBack()
        if (prevPath) {
            await store.selectFile(prevPath)
        }
    } else {
        handleOverlayClose()
    }
}

async function handleOverlayOpenFile(payload) {
    const { path, lineStart, lineEnd } = typeof payload === 'string' ? { path: payload } : payload
    // Try as directory first — navigate into dir and close overlay
    if (!path.startsWith('/')) {
        try {
            const resp = await fetch(`/api/dir?path=${encodeURIComponent(path)}`)
            if (resp.ok) {
                await store.navigateToDir(path)
                window.dispatchEvent(new CustomEvent('close-file-overlay'))
                window.dispatchEvent(new CustomEvent('open-file-manager'))
                return
            }
        } catch {
            // Not a directory, fall through to open as file
        }
    }
    // Open as file in the overlay nav stack
    const isExternal = path.startsWith('/')
    const ok = await store.selectFile(path)
    if (ok) {
        fileNav.openFile(path)
        if (lineStart) scrollToLine(lineStart, lineEnd)
        if (isExternal) {
            toast.show(gt('file.toast.externalFile'), { type: 'info', duration: 2000 })
        }
    }
}

function handleOpenFileOverlay(e) {
    const { path, lineStart, lineEnd } = e.detail || {}
    if (!path) return
    activeTab.value = 'browse'
    fileNav.openFile(path)
    if (lineStart) scrollToLine(lineStart, lineEnd)
}

function onTaskCardClick(taskId) {
    navigateToTaskSettings(taskId)
    switchTab('tasks')
}

async function handleRename({ path, name }) {
    try {
        await store.renameFile(path, name)
    } catch (err) {
        appLog.e(TAG, '[handleRename] error:', err)
    }
}

async function handleDelete(path) {
    appLog.d(TAG, '[handleDelete] called, path:', path)
    const wasOverlay = fileNav.overlayOpen.value
    try {
        await store.deleteFile(path)
        appLog.d(TAG, '[handleDelete] store.deleteFile resolved')
    } catch (err) {
        appLog.e(TAG, '[handleDelete] unhandled error:', err)
    }
    if (wasOverlay) {
        if (fileNav.canGoBack.value) {
            const prevPath = fileNav.goBack()
            if (prevPath) {
                await store.selectFile(prevPath)
            }
        } else {
            handleOverlayClose()
        }
    }
}

async function handleBatchDelete(paths) {
    try {
        await store.deleteFiles(paths)
    } catch (err) {
        appLog.e(TAG, '[handleBatchDelete] unhandled error:', err)
    }
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

// Dock badge change animations
const chatBadgeAnim = ref(false)
const historyBadgeAnim = ref(false)
const taskBadgeAnim = ref(false)
const terminalBadgeAnim = ref(false)
const proxyBadgeAnim = ref(false)
const overflowBadgeAnim = ref(false)

function triggerBadgeAnim(animRef) {
  animRef.value = false
  nextTick(() => { animRef.value = true })
}

watch(() => store.state.chatUnreadCount, (n, o) => { if (o !== undefined && n !== o) triggerBadgeAnim(chatBadgeAnim) })
watch(() => store.state.gitWorkingTreeChangeCount, (n, o) => { if (o !== undefined && n !== o) triggerBadgeAnim(historyBadgeAnim) })
watch(() => store.state.taskUnreadCount, (n, o) => {
  if (o !== undefined && n !== o) {
    triggerBadgeAnim(taskBadgeAnim)
    if (dockSlot4Tab.value !== 'tasks') triggerBadgeAnim(overflowBadgeAnim)
  }
})
watch(() => store.state.terminalSessionCount, (n, o) => {
  if (o !== undefined && n !== o) {
    triggerBadgeAnim(terminalBadgeAnim)
    if (dockSlot4Tab.value !== 'terminal') triggerBadgeAnim(overflowBadgeAnim)
  }
})
watch(() => store.state.portForwardActiveCount, (n, o) => {
  if (o !== undefined && n !== o) {
    triggerBadgeAnim(proxyBadgeAnim)
    if (dockSlot4Tab.value !== 'proxy') triggerBadgeAnim(overflowBadgeAnim)
  }
})

const overflowBadgeCount = computed(() => {
  let count = store.state.taskUnreadCount + store.state.portForwardActiveCount + store.state.terminalSessionCount
  // Subtract the count shown on slot4 to avoid double-counting
  if (dockSlot4Tab.value === 'tasks') count -= store.state.taskUnreadCount
  else if (dockSlot4Tab.value === 'proxy') count -= store.state.portForwardActiveCount
  else if (dockSlot4Tab.value === 'terminal') count -= store.state.terminalSessionCount
  return count
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
  // Remember this tab as the dock slot 4 shortcut
  setDockSlot4(tab)
  if (tab === 'terminal') {
    handleDockTerminal()
  } else {
    switchTab(tab)
  }
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

function scrollToLine(line, lineEnd) {
    const startLine = Math.max(1, line)
    const endLine = Math.min(lineEnd && lineEnd > startLine ? lineEnd : startLine, startLine + 200)
    const selector = `.code-line[data-line="${startLine}"]`
    const maxAttempts = 30
    let attempts = 0
    function tryScroll() {
        attempts++
        const firstEl = document.querySelector(selector)
        if (firstEl) {
            // Cancel any pending scroll-position restore in FileViewer
            // so it doesn't override our scroll target
            window.dispatchEvent(new CustomEvent('cancel-scroll-restore'))
            firstEl.scrollIntoView({ behavior: 'smooth', block: 'center' })
            // Flash the range
            for (let i = startLine; i <= endLine; i++) {
                const el = document.querySelector(`.code-line[data-line="${i}"]`)
                if (el) {
                    el.classList.add('line-flash')
                    el.addEventListener('animationend', () => el.classList.remove('line-flash'), { once: true })
                }
            }
            return
        }
        if (attempts < maxAttempts) {
            nextTick(tryScroll)
        }
    }
    nextTick(tryScroll)
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
    let authed = false
    if (resp.ok) {
        authed = true
    } else if (resp.status === 401 || resp.status === 403) {
        if (isAppMode.value && window.AndroidNative?.getPassword?.()) {
            const savedPwd = window.AndroidNative.getPassword()
            if (savedPwd) {
                try {
                    const loginRes = await fetch('/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: savedPwd }) })
                    if (loginRes.ok) {
                        authed = true
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

    // ── Main app initialization ──
    // Complete ALL initialization BEFORE setting isAuthenticated = true,
    // so that ChatPanelContent mounts only when the clawbench_project cookie
    // and session identity are already available. This prevents loadHistory()
    // from firing with missing cookies (Android first-login bug).
    if (!(await initializeApp())) return
    isAuthenticated.value = true
    await nextTick()
    welcomeOverlay.value?.show()

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
          appLog.d(TAG, 'getPendingNavigation poll result:', nav)
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
})

onUnmounted(() => {
    removeTaskHandler()
    window.removeEventListener('clawbench-foreground', handleForeground)
    destroyGlobalEvents()
    window.removeEventListener('open-file-manager', handleOpenFileManager)
    window.removeEventListener('open-file-overlay', handleOpenFileOverlay)
    window.removeEventListener('close-file-overlay', handleOverlayClose)
    window.removeEventListener('navigate-to-commit', handleNavigateToCommit)
    window.removeEventListener('quote-sent', playQuoteEmitAnimation)
    window.removeEventListener('attach-to-chat', playQuoteEmitAnimation)
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

/* When chat keyboard is open on iOS/PWA (no adjustResize), shrink the app container
   from the bottom so content stays above the keyboard. */
.chat-keyboard-open {
    bottom: v-bind(chatKeyboardHeight + 'px') !important;
}

/* When terminal keyboard is open in PWA standalone / iOS (no adjustResize),
   shrink the app container from the bottom so the terminal content stays
   above the keyboard. On Android native (adjustResize), innerHeight shrinks
   automatically so this class is not applied. */
.terminal-keyboard-open {
    bottom: v-bind(terminalKeyboardHeight + 'px') !important;
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
    gap: 12px;
    position: relative;
    /* Use margin:auto instead of justify-content:center so absolute-positioned
       indicator at left:0 aligns exactly with the first button */
    margin-inline: auto;
    width: fit-content;
}

/* Water-drop sliding indicator — accent background that drifts to the active button */
.dock-active-indicator {
    position: absolute;
    width: 34px;
    height: 34px;
    border-radius: 50%;
    background: var(--accent-color);
    /* Water-drop feel: slightly overshoot then settle */
    transition: transform 0.35s cubic-bezier(0.34, 1.56, 0.64, 1);
    z-index: 0;
    pointer-events: none;
}

.dock-btn {
    position: relative;
    width: 34px;
    height: 34px;
    border: none;
    border-radius: 50%;
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: color 0.25s, transform 0.15s;
    z-index: 1;
}

.dock-btn:hover {
    color: var(--text-primary);
}

.dock-btn:active {
    transform: scale(0.92);
}

.dock-btn.active {
    color: #fff;
}

.dock-btn.active:hover {
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

/* Dock badge pop animation on count change */
.dock-badge-pop {
    animation: badge-pop 0.4s cubic-bezier(0.34, 1.56, 0.64, 1);
}

@keyframes badge-pop {
    0% {
        transform: scale(1);
    }
    40% {
        transform: scale(1.35);
        box-shadow: 0 0 8px 2px color-mix(in srgb, var(--accent-color) 50%, transparent);
    }
    70% {
        transform: scale(0.9);
    }
    100% {
        transform: scale(1);
        box-shadow: 0 0 0 0 transparent;
    }
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
    background: var(--bg-primary);
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
.dock-overflow-wrapper {
    position: relative;
}

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
