<template>
  <div>
    <!-- Loading state: show nothing while checking auth -->
    <div v-if="isAuthenticated === null" style="display:none" />

    <!-- Login -->
    <LoginView v-else-if="!isAuthenticated" @login-success="handleLoginSuccess" />

    <!-- Main app -->
    <div v-else class="app-container" :class="{ 'chat-keyboard-open': chatKeyboardActive, 'terminal-keyboard-open': terminalKeyboardNeedsShrink, 'project-switching': switchingProject }" :key="projectKey">
      <WelcomeOverlay ref="welcomeOverlay" />
      <VersionMismatchOverlay ref="versionMismatchOverlay" />
      <UpgradePromptOverlay ref="upgradePromptOverlay" />
      <UpgradeDialog ref="upgradeDialogRef" />
      <AppHeader
        :project-root="projectRoot"
        :home-dir="homeDir"
        @open-project-dialog="handleOpenProjectDialog"
      />
      <ConnectionOverlay />

      <main class="main-content" :class="{ 'big-screen': isBigScreen }">
        <!-- Big-screen vertical dock (non-chat tabs only) -->
        <div v-show="isBigScreen" class="big-dock">
          <div class="big-dock-center">
            <div class="dock-active-indicator big-dock-active-indicator" :style="bigDockIndicatorStyle"></div>
            <div v-for="tab in BIG_SCREEN_DOCK_TABS" :key="tab" class="dock-btn-wrap">
              <button class="dock-btn" :class="bigDockBtnClass(tab)" @click.stop="handleBigDockTabClick(tab)" :title="bigDockTabTitle(tab)">
                <component :is="bigDockTabIcon(tab)" />
              </button>
              <span v-if="bigDockBadgeVisible(tab)" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': bigDockBadgeAnim(tab) }" @animationend="bigDockBadgeAnimEnd(tab)">{{ formatBadgeCount(bigDockBadgeCount(tab)) }}</span>
            </div>
          </div>
        </div>

        <div class="content-area" id="contentArea">
          <SplitView
            :enabled="isBigScreen"
            :ratio="splitRatio"
            @update:ratio="onSplitRatioChange"
          >
            <template #left>
              <div class="col-left" v-show="isBigScreen || activeTab !== 'chat'" @pointerdown="setActivePane('left')" @focusin="setActivePane('left')">
                <!-- File Browse Tab (合一：目录浏览 + 文件覆盖预览) -->
                <TabPanel tabId="browse" :activeTab="leftPanelActive" :noHeader="true">
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
                      :search-drawer="fileSearchDrawer"
                      :recent-drawer="recentFilesDrawer"
                      :keyboard-active="fileManagerShortcutActive"
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
                      :toc-open="tocDrawer.effectiveOpen.value"
                      :search-open="searchDrawer.effectiveOpen.value"
                      :markdown-view-mode="markdownViewMode"
                      :file-history-open="fileHistoryDrawer.effectiveOpen.value"
                      :toc-file="tocFile"
                      :pdf-outline="pdfOutline"
                      @delete="handleDelete($event)"
                      @show-details="detailsDrawer.open()"
                      @open-git-history="openFileHistory"
                      @toggle-toc="tocDrawer.toggle()"
                      @toggle-search="currentFile?.content && searchDrawer.toggle()"
                      @toggle-view="markdownViewMode = markdownViewMode === 'rendered' ? 'raw' : 'rendered'"
                      @refresh="handleRefresh"
                      @jump="scrollToLine"
                      @jump-page="handleJumpPdfPage"
                      @close-git-history="fileHistoryDrawer.close()"
                      @open-file="handleOverlayOpenFile"
                      @overlay-close="handleOverlayClose"
                      @open-recent-files="recentFilesDrawer.open()"
                    />
                  </div>
                </TabPanel>

                <!-- History Tab -->
                <TabPanel tabId="history" :activeTab="leftPanelActive" :noHeader="true">
                  <GitHistoryContent
                    mode="project"
                    :active="panelIsActive('history')"
                    @open-file="handleSelectFile"
                  />
                </TabPanel>

                <!-- Proxy Tab -->
                <TabPanel tabId="proxy" :activeTab="leftPanelActive" :noHeader="true">
                  <ProxyPanelContent />
                </TabPanel>

                <!-- Terminal Tab -->
                <TabPanel tabId="terminal" :activeTab="leftPanelActive" :noHeader="true">
                  <TerminalPanelContent
                    :requested-cwd="terminalRequestedCwd"
                    :active="panelIsActive('terminal')"
                    :platform-unsupported="isPlatformUnsupported"
                    @cwd-handled="terminalRequestedCwd = null"
                  />
                </TabPanel>

                <!-- Tasks Tab -->
                <TabPanel tabId="tasks" :activeTab="leftPanelActive" :noHeader="true">
                  <TaskTab :active="panelIsActive('tasks')" @open-file="handleTaskOpenFile" />
                </TabPanel>

                <!-- Settings Tab -->
                <TabPanel tabId="settings" :activeTab="leftPanelActive" :noHeader="true">
                  <SettingsPage :active="panelIsActive('settings')" />
                </TabPanel>
              </div>
            </template>

            <template #right>
              <div class="col-right" v-show="isBigScreen || activeTab === 'chat'" :class="{ 'chat-drop-active': chatDropActive }" @pointerdown="setActivePane('right')" @focusin="setActivePane('right')" @dragenter="onChatColDragEnter" @dragover="onChatColDragOver" @dragleave="onChatColDragLeave" @drop="onChatColDrop">
                <!-- Chat Tab -->
                <TabPanel tabId="chat" :activeTab="chatActive">
                  <template #header>
                    <span class="bs-header-title"><AgentIcon v-if="sessionIdentity.currentAgentId.value" :backend="getAgentBackend(sessionIdentity.currentAgentId.value)" :name="getAgentName(sessionIdentity.currentAgentId.value)" :size="18" />{{ sessionIdentity.agentHeaderTitle.value }}</span>
                    <div v-if="sessionIdentity.currentSessionTitle.value" class="bs-header-description">
                      <HeaderMarquee :text="sessionIdentity.currentSessionTitle.value">{{ sessionIdentity.currentSessionTitle.value }}</HeaderMarquee>
                    </div>
                  </template>
                  <ChatPanelContent
                    :active="isBigScreen || activeTab === 'chat'"
                    :keyboard-active="chatShortcutActive"
                    :current-file="currentFile"
                    :current-dir="currentDir"
                    @open="switchTab('chat')"
                    @open-file="handleSelectFile"
                    @task-card-click="onTaskCardClick"
                    @open-acp-sessions="acpSessionDrawer.open()"
                    @open-session-search="sessionSearchDrawer.open()"
                  />
                </TabPanel>
                <div v-if="chatDropActive" class="chat-drop-hint">
                  <Paperclip :size="16" />
                  {{ t('file.dropToAttach') }}
                </div>
              </div>
            </template>
          </SplitView>
        </div>
      </main>

      <Lightbox ref="lightboxRef" />

      <ProjectDialog
        :open="projectDialogOpen"
        @close="projectDialogOpen = false"
      />

      <FileDetailsDrawer
        :file="currentFile"
        :open="detailsDrawer.effectiveOpen.value && fileNav.overlayOpen.value"
        @close="detailsDrawer.close()"
      />

      <RecentFilesDrawer
        :open="recentFilesDrawer.effectiveOpen.value"
        :current-file-path="fileNav.overlayOpen.value ? currentFile?.path : null"
        @close="recentFilesDrawer.close()"
        @select-file="handleRecentFileSelect"
      />

      <!-- Quote question floating bar -->
      <QuoteQuestionBar
        :visible="quoteQuestion.visible.value"
        :quoteData="quoteQuestion.quoteData.value"
        @send="quoteQuestion.sendMessage($event)"
        @close="quoteQuestion.closeSheet()"
        @pin="quoteQuestion.pinBar()"
        @unpin="quoteQuestion.unpinBar()"
      />

      <!-- Session drawer — bound to chat tab, auto-closes when leaving chat -->
      <SessionDrawer
        ref="sessionDrawerRef"
        :open="sessionIdentity.sessionDrawer.effectiveOpen.value"
        :currentSessionId="sessionIdentity.currentSessionId.value"
        :runningSessionIds="sessionIdentity.runningSessions.value"
        :currentAgentId="sessionIdentity.currentAgentId.value"
        @close="sessionIdentity.sessionDrawer.close()"
        @select="handleSessionSelect"
        @create="handleSessionCreate"
        @archive="handleSessionArchive"
        @destroy="handleSessionDestroy"
        @open-session-search="sessionSearchDrawer.open()"
      />

      <!-- Session search drawer -->
      <SessionSearchDrawer
        ref="sessionSearchDrawerRef"
        :open="sessionSearchDrawer.effectiveOpen.value"
        @close="sessionSearchDrawer.close()"
        @open="handleOpenFromSearch"
        @resume="handleResumeFromSearch"
      />

      <!-- ACP session resume drawer -->
      <AcpSessionDrawer
        :open="acpSessionDrawer.effectiveOpen.value"
        :agentId="sessionIdentity.currentAgentId.value"
        @close="acpSessionDrawer.close()"
        @select="handleAcpSessionSelect"
      />

      <!-- Bottom dock (tab bar) -->
      <div v-if="isAuthenticated" v-show="!anyKeyboardActive && !isBigScreen" class="bottom-dock-wrapper">
        <div ref="dockRef" class="bottom-dock">
          <div class="dock-center">
            <div class="dock-active-indicator" :style="dockIndicatorStyle"></div>
            <div class="dock-btn-wrap">
              <button class="dock-btn" :class="{ active: activeTab === 'chat', 'has-unread': store.state.chatUnreadCount > 0 && activeTab !== 'chat', 'has-running': sessionIdentity.runningSessions.value.size > 0 && activeTab !== 'chat' }" @click.stop="switchTab('chat')" :title="t('nav.chat')">
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
            <!-- Inline overflow tabs (rendered in overflowTabs order — settings always last) -->
            <div v-for="tab in inlineOverflowTabs" :key="tab" class="dock-btn-wrap">
              <button class="dock-btn" :class="dockInlineOverflowBtnClass(tab)" @click.stop="handleInlineOverflowClick(tab)" :title="dockTabTitle(tab)">
                <component :is="dockTabIcon(tab)" />
              </button>
              <span v-if="tab === 'tasks' && store.state.taskUnreadCount > 0 && activeTab !== 'tasks'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': taskBadgeAnim }" @animationend="taskBadgeAnim = false">{{ formatBadgeCount(store.state.taskUnreadCount) }}</span>
              <span v-if="tab === 'terminal' && store.state.terminalSessionCount > 0 && activeTab !== 'terminal'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': terminalBadgeAnim }" @animationend="terminalBadgeAnim = false">{{ formatBadgeCount(store.state.terminalSessionCount) }}</span>
              <span v-if="tab === 'proxy' && store.state.portForwardActiveCount > 0 && activeTab !== 'proxy'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': proxyBadgeAnim }" @animationend="proxyBadgeAnim = false">{{ formatBadgeCount(store.state.portForwardActiveCount) }}</span>
            </div>
            <!-- Single remaining popup item shown directly (no overflow menu) -->
            <div v-if="singleDirectTab" :key="'single-' + singleDirectTab" class="dock-btn-wrap">
              <button class="dock-btn" :class="dockInlineOverflowBtnClass(singleDirectTab)" @click.stop="handleInlineOverflowClick(singleDirectTab)" :title="dockTabTitle(singleDirectTab)">
                <component :is="dockTabIcon(singleDirectTab)" />
              </button>
              <span v-if="singleDirectTab === 'tasks' && store.state.taskUnreadCount > 0 && activeTab !== 'tasks'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': taskBadgeAnim }" @animationend="taskBadgeAnim = false">{{ formatBadgeCount(store.state.taskUnreadCount) }}</span>
              <span v-if="singleDirectTab === 'terminal' && store.state.terminalSessionCount > 0 && activeTab !== 'terminal'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': terminalBadgeAnim }" @animationend="terminalBadgeAnim = false">{{ formatBadgeCount(store.state.terminalSessionCount) }}</span>
              <span v-if="singleDirectTab === 'proxy' && store.state.portForwardActiveCount > 0 && activeTab !== 'proxy'" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': proxyBadgeAnim }" @animationend="proxyBadgeAnim = false">{{ formatBadgeCount(store.state.portForwardActiveCount) }}</span>
            </div>
            <!-- Overflow button (popup has >1 items) -->
            <div v-if="showOverflowButton" class="dock-overflow-wrapper">
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
          <button v-if="popupOverflowTabs.includes('tasks')" class="dock-overflow-item" :class="{ active: activeTab === 'tasks' }" @click.stop="handleOverflowSelect('tasks')">
            <CalendarClock :size="16" />
            <span>{{ t('nav.tasks') }}</span>
            <span v-if="store.state.taskUnreadCount > 0" class="dock-overflow-count" :class="{ 'dock-badge-pop': taskBadgeAnim }" @animationend="taskBadgeAnim = false">{{ formatBadgeCount(store.state.taskUnreadCount) }}</span>
          </button>
          <button v-if="popupOverflowTabs.includes('proxy')" class="dock-overflow-item" :class="{ active: activeTab === 'proxy' }" @click.stop="handleOverflowSelect('proxy')">
            <EthernetPort :size="16" />
            <span>{{ t('nav.portForward') }}</span>
            <span v-if="store.state.portForwardActiveCount > 0" class="dock-overflow-count" :class="{ 'dock-badge-pop': proxyBadgeAnim }" @animationend="proxyBadgeAnim = false">{{ formatBadgeCount(store.state.portForwardActiveCount) }}</span>
          </button>
          <button v-if="popupOverflowTabs.includes('terminal')" class="dock-overflow-item" :class="{ active: activeTab === 'terminal' }" @click.stop="handleOverflowSelect('terminal')">
            <TerminalIcon :size="16" />
            <span>{{ t('terminal.title') }}</span>
            <span v-if="store.state.terminalSessionCount > 0" class="dock-overflow-count" :class="{ 'dock-badge-pop': terminalBadgeAnim }" @animationend="terminalBadgeAnim = false">{{ formatBadgeCount(store.state.terminalSessionCount) }}</span>
          </button>
          <button v-if="popupOverflowTabs.includes('settings')" class="dock-overflow-item" :class="{ active: activeTab === 'settings' }" @click.stop="handleOverflowSelect('settings')">
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
import { ref, computed, watch, onMounted, onUnmounted, provide, nextTick, defineAsyncComponent } from 'vue'
import { appLog, startFlushTimer, stopFlushTimer } from '@/utils/appLog'
import { useDockOverflow } from '@/composables/useDockOverflow'
import { useI18n } from 'vue-i18n'
import { useSettingsConfig, applyUIScale, getZoomedViewport, toFixedCSS } from '@/composables/useSettingsConfig'
import { MessageSquare, FolderOpen, GitBranch, EthernetPort, SquareTerminal as TerminalIcon, CalendarClock, MoreHorizontal, Settings, Paperclip } from 'lucide-vue-next'
import AppHeader from './components/common/AppHeader.vue'
import TabPanel from './components/common/TabPanel.vue'
import FileOverlay from './components/file/FileOverlay.vue'
import Lightbox from './components/media/Lightbox.vue'
import ChatPanelContent from './components/chat/ChatPanelContent.vue'
import FileManagerContent from './components/file/FileManagerContent.vue'
import GitHistoryContent from './components/git/GitHistoryContent.vue'
import ProxyPanelContent from './components/proxy/ProxyPanelContent.vue'
const TerminalPanelContent = defineAsyncComponent(() => import('./components/terminal/TerminalPanelContent.vue'))
import ProjectDialog from './components/ProjectDialog.vue'
import LoginView from './components/LoginView.vue'
import WelcomeOverlay from './components/WelcomeOverlay.vue'
import VersionMismatchOverlay from './components/VersionMismatchOverlay.vue'
import UpgradePromptOverlay from './components/UpgradePromptOverlay.vue'
import UpgradeDialog from './components/settings/UpgradeDialog.vue'
import FileDetailsDrawer from './components/file/FileDetailsDrawer.vue'
import RecentFilesDrawer from './components/file/RecentFilesDrawer.vue'
import ToastNotification from './components/common/ToastNotification.vue'
import DialogOverlay from './components/common/DialogOverlay.vue'
import SessionDrawer from './components/session/SessionDrawer.vue'
import SessionSearchDrawer from './components/session/SessionSearchDrawer.vue'
import AcpSessionDrawer from './components/chat/AcpSessionDrawer.vue'
import QuoteQuestionBar from './components/common/QuoteQuestionBar.vue'
import HeaderMarquee from './components/common/HeaderMarquee.vue'
import AgentIcon from './components/common/AgentIcon.vue'
import SettingsPage from './components/settings/SettingsPage.vue'
import TaskTab from '@/components/task/TaskTab.vue'
import { useQuoteQuestion } from './composables/useQuoteQuestion.ts'
import { useTaskTab, registerSwitchTab, onTaskEvent } from '@/composables/useTaskTab.ts'
import { useTabDrawer, onTabSwitch, resetTabDrawerState } from '@/composables/useTabDrawer.ts'
import { resetAgents, useAgents } from '@/composables/useAgents'
import { useSessionIdentity, registerSessionDrawerRef, resetIdentity } from './composables/useSessionIdentity.ts'
import { loadSessionsOnce, resetChatSessionState } from './composables/useChatSession.ts'
import { resetTaskTabState } from './composables/useTaskTab.ts'
import { clearPlanState } from './composables/usePlanProgress.ts'
import { useToast } from './composables/useToast.ts'
import { useDialog } from './composables/useDialog.ts'
import { gt } from './composables/useLocale'
import { useAppMode } from './composables/useAppMode.ts'
import { requestNotificationPermission } from './composables/useNotification'
import { useTerminalKeyboard } from './composables/useTerminalKeyboard.ts'
import { useChatKeyboard } from './composables/useChatKeyboard.ts'
import { usePortForward } from './composables/usePortForward.ts'
import { useTerminalStatus } from './composables/useTerminalStatus.ts'
import { useFileWatch } from './composables/useFileWatch.ts'
import { useFileNavStack } from './composables/useFileNavStack'
import { removeRecentFile } from './composables/useRecentFiles'
import { refreshCurrentFile } from './composables/useFileRefresh.ts'
import { useGlobalEvents } from './composables/useGlobalEvents'
import ConnectionOverlay from './components/common/ConnectionOverlay.vue'
import { useUpgrade } from './composables/useUpgrade'
import { useEdgeSwipeBack, useFeatureBackHandler, PRIORITY_OVERLAY } from './composables/useEdgeSwipeBack'
import { handleBackNavigation, requestExitConfirm } from './composables/useBackHandler'
import { store, loadBrowseDir } from './stores/app.ts'
import { setPendingCommitNavigation } from './composables/useCommitNavigation.ts'
import { getFileType } from './utils/fileType.ts'
import { formatBadgeCount } from './utils/format.ts'
import { useChatContext } from './composables/useChatContext.ts'
import { readAttachDragData, hasAttachDragData } from './utils/attachDrag'
import SplitView from './components/common/SplitView.vue'
import {
  useBigScreenLayout,
  resolveLeftTabOnEnter,
  setActivePane,
  resolveActivePaneOnEnter,
  switchLeftTab,
  setSplitRatio,
  registerBigScreenCallbacks,
  BIG_SCREEN_DOCK_TABS,
} from './composables/useBigScreenLayout'
import 'highlight.js/styles/github.css'
import 'highlight.js/styles/github-dark.css'
import './assets/hljs-light-override.css'
import './assets/annotation-buttons.css'
import './assets/mono-icon-colors.css'
import './assets/chat-actions.css'

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
  resetTabDrawerState()
  fileNav.closeOverlay()
  activeTab.value = 'chat'

  // ── Phase 4: Change key → Vue destroys old component tree & builds new one ──
  projectKey.value = newProjectPath

  // ── Phase 5: Fade in EARLY — UI is visible while data loads in background ──
  //  store.setProject() already filled projectRoot, rootPaths, homeDir, config from the
  //  expanded POST response, so no need for loadProject(). ChatPanel's
  //  watch({ immediate: true }) will call loadHistory which recovers session identity
  //  AND messages in one request. However, initSessionFromAPI() is still required
  //  because loadSessionsOnce() depends on currentSessionId being set — without it,
  //  chatUnreadCount would be computed incorrectly (no session excluded from count).
  switchingProject.value = false

  // ── Phase 6: Background data loading — all independent, fully parallel, non-blocking ──
  const restoreBrowseDir = async () => {
    const savedDir = loadBrowseDir()
    if (savedDir) {
      try { await store.loadFiles(savedDir, true) } catch {
        // Directory no longer exists — fall back to project root
        await store.loadFiles('')
      }
    } else {
      await store.loadFiles('')
    }
  }
  await sessionIdentity.initSessionFromAPI()
  Promise.allSettled([
    restoreBrowseDir(),
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

// ── Big-screen layout state ──
const { isBigScreen, leftTab, splitRatio, activePane } = useBigScreenLayout()

const chatActive = computed(() => (isBigScreen.value ? 'chat' : activeTab.value))
const leftPanelActive = computed(() => (isBigScreen.value ? leftTab.value : activeTab.value))
const panelIsActive = (tabId) =>
  isBigScreen.value ? leftTab.value === tabId : activeTab.value === tabId

// Focus-aware keyboard gating: a panel's global shortcuts only fire when the
// user is actually working in that pane (big-screen) or that tab (narrow).
const chatShortcutActive = computed(() => (isBigScreen.value ? activePane.value === 'right' : activeTab.value === 'chat'))
const fileManagerShortcutActive = computed(() => (isBigScreen.value ? activePane.value === 'left' : activeTab.value === 'browse'))

function onSplitRatioChange(ratio) {
  setSplitRatio(ratio)
}

// Dock active indicator — water-drop sliding highlight
// Dynamic button count: chat, browse, history, [inline overflow...], [overflow btn]
const DOCK_STEP = 46 // 34 (btn width) + 12 (gap)

const dockActiveIndex = computed(() => {
  const visibleTabs = ['chat', 'browse', 'history', ...inlineOverflowTabs.value]
  if (singleDirectTab.value) visibleTabs.push(singleDirectTab.value)
  if (showOverflowButton.value) visibleTabs.push('__overflow__')
  const idx = visibleTabs.indexOf(activeTab.value)
  if (idx >= 0) return idx
  // Active tab is in the overflow popup — highlight the overflow button
  if (showOverflowButton.value) return visibleTabs.length - 1
  return 0
})

const dockIndicatorStyle = computed(() => ({
  transform: `translateX(${dockActiveIndex.value * DOCK_STEP}px)`,
}))

function switchTab(tab) {
  if (isBigScreen.value) {
    // Big-screen: chat is always visible; non-chat tabs route to the left column
    if (tab === 'chat') return
    switchLeftTab(tab)
    return
  }
  if (activeTab.value === tab) return
  activeTab.value = tab
  // Auto-close all drawers not belonging to the new tab
  onTabSwitch(tab)
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
  // Close overflow menu on any tab switch
  overflowMenuOpen.value = false
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

// Register browse-scoped drawers with tab-drawer binding
const detailsDrawer = useTabDrawer('browse')
const tocDrawer = useTabDrawer('browse')
const searchDrawer = useTabDrawer('browse')
const fileHistoryDrawer = useTabDrawer('browse')
const fileSearchDrawer = useTabDrawer('browse', { autoRestore: false })
const recentFilesDrawer = useTabDrawer('browse', { autoRestore: false })

function openFileHistory() {
  fileHistoryDrawer.open()
}

const markdownViewMode = ref('rendered')

const toast = useToast()
provide('toast', toast)

const sessionIdentity = useSessionIdentity()
const { getAgentBackend, getAgentName } = useAgents()

// Register chat-scoped drawers with tab-drawer binding
// Session drawer is now owned by useSessionIdentity (encapsulated TabDrawer)

const showHidden = ref(false)
const { localConfig, setLocalConfig: setSetting, loadConfig } = useSettingsConfig()
// Initialize from settings config (which handles legacy key migration)
showHidden.value = !!localConfig.showHidden
const sortField = ref(localConfig.sortField || null)
const sortDir = ref(localConfig.sortDir || 'asc')

useFileWatch({
  fileManagerOpen: computed(() => leftPanelActive.value === 'browse'),
  currentDir: computed(() => store.state.currentDir),
  currentFile: computed(() => store.state.currentFile),
})

const fileNav = useFileNavStack()

function closeOverlayAndSync() {
  fileNav.closeOverlay()
  tocDrawer.close()
  detailsDrawer.close()
  searchDrawer.close()
  fileHistoryDrawer.close()
}

const { isAppMode } = useAppMode()
const { syncToNative, sshInfo, loadSSHInfo } = usePortForward()
const { terminalRuntimeEnabled, platformSupported, loadTerminalStatus } = useTerminalStatus()
const isSSHDisabled = computed(() => sshInfo.value?.enabled === false)
// Platform unsupported: PTY cannot run on this OS (e.g. Windows lacks ConPTY).
// The terminal tab is still shown so users see a clear "unsupported" empty state.
const isPlatformUnsupported = computed(() => platformSupported.value === false)
// Use runtime status (actual server state) not config value — mirrors SSH pattern.
// Config may say enabled=true before restart; the runtime API returns false until
// the terminal manager actually exists.  `null` means "not yet loaded" → treat as
// disabled to avoid a flash of the terminal button on first mount.
const isTerminalDisabled = computed(() => terminalRuntimeEnabled.value !== true)
watch(isSSHDisabled, (disabled) => {
  if (disabled && panelIsActive('proxy')) {
    switchTab(isBigScreen.value ? 'browse' : 'chat')
  }
})
watch(isTerminalDisabled, (disabled) => {
  // Only force-switch when terminal is config-disabled (not platform unsupported).
  // Platform unsupported shows a dedicated empty state — user can stay on the tab.
  if (disabled && !isPlatformUnsupported.value && panelIsActive('terminal')) {
    switchTab(isBigScreen.value ? 'browse' : 'chat')
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
  () => panelIsActive('browse') && fileNav.overlayOpen.value,
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
const chatKeyboardActive = computed(() => chatActive.value === 'chat' && chatKeyboardHeight.value > 0)

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

function handleSessionSelect(sessionId, _backend) {
  sessionIdentity.switchSession(sessionId)
  sessionIdentity.sessionDrawer.close()
}

async function handleSessionCreate(agentId) {
  await sessionIdentity.createSession(agentId)
  // If drawer is still open, add the new session to the local list
  if (sessionDrawerRef.value && sessionIdentity.sessionDrawer.isOpen.value) {
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
  sessionIdentity.sessionDrawer.close()
}

function handleSessionArchive(sessionId, backend) {
  sessionIdentity.archiveSession(sessionId, backend)
}

function handleSessionDestroy(sessionId) {
  sessionIdentity.destroySession(sessionId)
}

// ── ACP Session Resume ──
const acpSessionDrawer = useTabDrawer('chat')

// ── Session Search ──
const sessionSearchDrawer = useTabDrawer('chat', { autoRestore: false })
const searchConfirmDialog = useDialog()

function handleOpenFromSearch(session) {
  if (!session?.session_id) return
  sessionSearchDrawer.close()
  handleSessionSelect(session.session_id, session.backend)
}

async function handleResumeFromSearch(session) {
  if (!session?.session_id) return
  const title = session.session_title || gt('sessionSearch.untitledSession')
  const confirmed = await searchConfirmDialog.confirm(
    gt('sessionSearch.resumeConfirm', { title }),
    { title: gt('sessionSearch.resume'), confirmText: gt('common.confirm') }
  )
  if (!confirmed) return
  try {
    const resp = await fetch('/api/ai/session/resume', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: session.session_id }),
    })
    if (!resp.ok) {
      const data = await resp.json().catch(() => ({}))
      if (resp.status === 403) {
        toast.show(gt('sessionSearch.resumeProjectMismatch'), { icon: '⚠️', type: 'error' })
      } else {
        toast.show(data.error || gt('sessionSearch.resumeFailed'), { icon: '⚠️', type: 'error' })
      }
      return
    }
    sessionSearchDrawer.close()
    handleSessionSelect(session.session_id, session.backend)
  } catch {
    toast.show(gt('sessionSearch.resumeFailed'), { icon: '⚠️', type: 'error' })
  }
}

async function handleAcpSessionSelect(sessionId) {
  await sessionIdentity.switchSession(sessionId)
  acpSessionDrawer.close()
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
  window.addEventListener('clawbench-theme-change', async (e) => {
      const resolved = e.detail
      theme.value = resolved
      const { initMermaid, reRenderMermaid } = await import('./utils/mermaid.ts')
      await initMermaid()
      await reRenderMermaid()
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
  try { await store.loadProject() } catch {
      toast.show(t('toast.projectLoadFailed'), { icon: '⚠️', type: 'error', duration: 0, onClick: () => location.reload() }); return false
  }
  await sessionIdentity.initSessionFromAPI()

  // 2. Infrastructure — global events, rendering, config
  initGlobalEvents()
  loadTasks()
  loadConfig()
  registerAppEventListeners()

  // Request browser notification permission (web mode only).
  // In app mode, Android manages its own notification permission.
  if (!isAppMode.value) {
    requestNotificationPermission().catch(() => {})
  }

  // 3. Secondary data — non-blocking, can load in parallel with UI render
  loadSessionsOnce()
  if (isAppMode.value) syncToNative().catch(() => {})
  if (isAppMode.value && localConfig.logCapture) {
    try { if (window.AndroidNative?.startLogCapture) window.AndroidNative.startLogCapture() } catch {}
  }
  if (localConfig.logCapture) startFlushTimer()
  loadSSHInfo().catch(() => {})
  loadTerminalStatus().catch(() => {})
  store.loadGitBranch().catch(() => {})
  // Restore last browsed directory; fall back to project root if the dir no longer exists
  const savedDir = loadBrowseDir()
  if (savedDir) {
    try { await store.loadFiles(savedDir, true) } catch {
      // Directory no longer exists — fall back to project root
      try { await store.loadFiles('') } catch {}
    }
  } else {
    try { await store.loadFiles('') } catch {
      toast.show(t('toast.fileListLoadFailed'), { icon: '⚠️', type: 'error', duration: 6000 })
    }
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
    dismissSplash()
    await nextTick()
    applyUIScale(localConfig.uiScale ?? 1)
    startDockResize()
    // Measure dock height and set --dock-height CSS variable for fixed-position elements
    const dockWrapper = document.querySelector('.bottom-dock-wrapper')
    if (dockWrapper) {
      const updateDockHeight = () => {
        const h = dockWrapper.offsetHeight
        document.documentElement.style.setProperty('--dock-height', h ? `${h}px` : '0px')
      }
      updateDockHeight()
      const dockResizeObs = new ResizeObserver(updateDockHeight)
      dockResizeObs.observe(dockWrapper)
      onUnmounted(() => dockResizeObs.disconnect())
    }
    welcomeOverlay.value?.show()
    versionMismatchOverlay.value?.show()
    checkForUpgrade()

    // Handle pending navigation
}

const projectDialogOpen = ref(false)
const welcomeOverlay = ref(null)
const versionMismatchOverlay = ref(null)
const upgradePromptOverlay = ref(null)
const upgradeDialogRef = ref(null)

async function checkForUpgrade() {
  const { checkForUpgradePrompt, state } = useUpgrade()
  const latestVer = await checkForUpgradePrompt()
  if (latestVer) {
    const currentVer = state.current_version
    if (upgradePromptOverlay.value && typeof (upgradePromptOverlay.value).show === 'function') {
      upgradePromptOverlay.value.show(latestVer, currentVer)
    }
  }
}

// Watch for upgrade start — auto-open UpgradeDialog to show progress
const { showProgressDialog, clearShowProgressDialog } = useUpgrade()
watch(showProgressDialog, (val) => {
  if (val && upgradeDialogRef.value && typeof (upgradeDialogRef.value).show === 'function') {
    upgradeDialogRef.value.show()
    clearShowProgressDialog()
  }
})

function handleOpenProjectDialog() {
    projectDialogOpen.value = true
}

const theme = ref(localConfig.theme === 'auto'
    ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
    : (localConfig.theme || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')))

const dirEntries = computed(() => store.state.dirEntries)
const currentDir = computed(() => store.state.currentDir)
const currentFile = computed(() => store.state.currentFile)
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
const sessionSearchDrawerRef = ref(null)
const pdfOutline = computed(() => fileOverlayRef.value?.pdfOutline || [])
function handleJumpPdfPage(pageNum) {
    fileOverlayRef.value?.pdfScrollToPage(pageNum)
}

watch(() => currentFile.value, (_f) => {
    tocDrawer.close()
    detailsDrawer.close()
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
        switchTab('browse')
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
        switchTab('browse')
        fileNav.openFile(filePath)
        if (lineStart) scrollToLine(lineStart)
    }
}

function handleOverlayClose() {
    closeOverlayAndSync()
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
        if (lineStart) markdownViewMode.value = 'raw'
        fileNav.openFile(path)
        if (lineStart) scrollToLine(lineStart, lineEnd)
        if (isExternal) {
            toast.show(gt('file.toast.externalFile'), { icon: 'ℹ️', type: 'info', duration: 2000 })
        }
    }
}

async function handleRecentFileSelect(path) {
    recentFilesDrawer.close()
    const ok = await store.selectFile(path)
    if (ok) {
        fileNav.openFile(path)
    }
}

function handleOpenFileOverlay(e) {
    const { path, lineStart, lineEnd } = e.detail || {}
    if (!path) return
    switchTab('browse')
    if (lineStart) markdownViewMode.value = 'raw'
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
        removeRecentFile(path)
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
        for (const p of paths) removeRecentFile(p)
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

// Responsive dock overflow — ResizeObserver drives inline promotion
const dockRef = ref(null)
const {
  inlineOverflowTabs,
  popupOverflowTabs,
  singleDirectTab,
  showOverflowButton,
  allInlineOverflowTabs,
  startObserving: startDockResize,
  stopObserving: stopDockResize,
} = useDockOverflow(
  () => dockRef.value,
  () => overflowTabs.value,
)

// Close overflow popup when layout changes (resize promotes/demotes items)
watch(() => inlineOverflowTabs.value.length, () => {
  overflowMenuOpen.value = false
})

// Safety net: re-measure dock when it becomes visible again after keyboard closes.
// ResizeObserver should handle this, but Android WebView may miss the callback
// after display:none → display:flex transitions (especially with CSS zoom applied).
watch(anyKeyboardActive, (active) => {
  if (!active) {
    nextTick(() => startDockResize())
  }
})

// Safety net: re-measure dock when UI scale (CSS zoom) changes.
// ResizeObserver may not fire when CSS zoom on <html> changes, so we
// must explicitly re-measure to recalculate overflow layout.
// Use requestAnimationFrame to ensure browser has reflowed after the zoom change.
watch(() => localConfig.uiScale, () => {
  requestAnimationFrame(() => {
    startDockResize()
    // Also update --dock-height CSS variable for fixed-position elements
    const dw = document.querySelector('.bottom-dock-wrapper')
    if (dw) {
      const h = dw.offsetHeight
      document.documentElement.style.setProperty('--dock-height', h ? `${h}px` : '0px')
    }
  })
})

// Helpers for dynamic inline overflow buttons
function dockTabIcon(tab) {
  return overflowTabMeta[tab]?.icon ?? CalendarClock
}
function dockTabTitle(tab) {
  return overflowTabMeta[tab] ? t(overflowTabMeta[tab].titleKey) : ''
}
function dockInlineOverflowBtnClass(tab) {
  return {
    active: activeTab.value === tab,
    'has-unread': tab === 'tasks' && store.state.taskUnreadCount > 0 && activeTab.value !== 'tasks',
    'just-completed': tab === 'tasks' && store.state.taskJustCompleted && activeTab.value !== 'tasks',
    'has-running': tab === 'tasks' && store.state.taskRunning && activeTab.value !== 'tasks',
  }
}
function handleInlineOverflowClick(tab) {
  if (tab === 'terminal') {
    handleDockTerminal()
  } else {
    switchTab(tab)
  }
}

// Big-screen mode transitions: keep useTabDrawer's currentTab coherent
// (chat drawers work in wide mode; collapse returns to the last active tab).
watch(isBigScreen, (val) => {
  if (val) {
    // Continuity-first (Q1A): adopt activeTab if non-chat, else keep persisted leftTab
    const next = resolveLeftTabOnEnter(activeTab.value, leftTab.value)
    if (leftTab.value !== next) switchLeftTab(next)
    onTabSwitch('chat')
    overflowMenuOpen.value = false
    // Focus continuity: the pane the user was working in becomes the active one.
    setActivePane(resolveActivePaneOnEnter(activeTab.value))
    // Big-screen: the bottom dock is hidden, so bottom-sheet drawers must sit
    // flush with the screen bottom — don't let a stale --dock-height leave a gap.
    document.documentElement.style.setProperty('--dock-height', '0px')
  } else {
    onTabSwitch(activeTab.value)
    // Bottom dock visible again — re-measure (ResizeObserver may miss the
    // display:none → visible transition, see the keyboard safety-net comment).
    nextTick(() => {
      startDockResize()
      const dw = document.querySelector('.bottom-dock-wrapper')
      if (dw) {
        const h = dw.offsetHeight
        document.documentElement.style.setProperty('--dock-height', h ? `${h}px` : '0px')
      }
    })
  }
}, { immediate: true })

// Route leftTab side-effects (reuse narrow-mode behaviors) and sync activeTab (Q3B)
registerBigScreenCallbacks({
  setActiveTab: (tab) => { activeTab.value = tab },
  sideEffects: (tab) => {
    if (tab === 'browse') store.loadFiles(store.state.currentDir)
    if (tab === 'tasks') { store.state.taskUnreadCount = 0; loadTasks() }
  },
})

// ── Big-screen vertical dock helpers ──
function handleBigDockTabClick(tab) {
  // Clicking a dock item means the user intends to work in the left pane.
  setActivePane('left')
  switchLeftTab(tab)
}

// ── Drag file/dir from the left panel → attach to chat (big-screen only) ──
const { addAttachedFile } = useChatContext()
const chatDropActive = ref(false)
let chatDropCounter = 0

function onChatColDragEnter(e) {
  if (!isBigScreen.value || !hasAttachDragData(e.dataTransfer)) return
  chatDropCounter++
  chatDropActive.value = true
}

function onChatColDragOver(e) {
  // Allow the drop only for internal attach drags (don't hijack OS file drops)
  if (isBigScreen.value && hasAttachDragData(e.dataTransfer)) e.preventDefault()
}

function onChatColDragLeave() {
  chatDropCounter--
  if (chatDropCounter <= 0) {
    chatDropCounter = 0
    chatDropActive.value = false
  }
}

function onChatColDrop(e) {
  chatDropCounter = 0
  chatDropActive.value = false
  if (!isBigScreen.value) return
  const data = readAttachDragData(e.dataTransfer)
  if (!data) return
  e.preventDefault()
  addAttachedFile(data.path, data.isDir)
  toast.show(t('chat.attach.addedToChat'), { icon: '📎', type: 'success', duration: 1500 })
}

const bigScreenTabMeta = {
  browse: { icon: FolderOpen, titleKey: 'nav.fileManager' },
  history: { icon: GitBranch, titleKey: 'git.history.projectHistory' },
  tasks: overflowTabMeta.tasks,
  proxy: overflowTabMeta.proxy,
  terminal: overflowTabMeta.terminal,
  settings: overflowTabMeta.settings,
}

function bigDockTabIcon(tab) {
  return bigScreenTabMeta[tab]?.icon ?? FolderOpen
}
function bigDockTabTitle(tab) {
  return bigScreenTabMeta[tab] ? t(bigScreenTabMeta[tab].titleKey) : ''
}
function bigDockBtnClass(tab) {
  return {
    active: leftTab.value === tab,
    'has-unread': tab === 'tasks' && store.state.taskUnreadCount > 0 && leftTab.value !== 'tasks',
    'just-completed': tab === 'tasks' && store.state.taskJustCompleted && leftTab.value !== 'tasks',
    'has-running': tab === 'tasks' && store.state.taskRunning && leftTab.value !== 'tasks',
  }
}
function bigDockBadgeCount(tab) {
  switch (tab) {
    case 'history': return store.state.gitWorkingTreeChangeCount
    case 'tasks': return store.state.taskUnreadCount
    case 'terminal': return store.state.terminalSessionCount
    case 'proxy': return store.state.portForwardActiveCount
    default: return 0
  }
}
function bigDockBadgeVisible(tab) {
  return bigDockBadgeCount(tab) > 0 && leftTab.value !== tab
}
function bigDockBadgeAnim(tab) {
  switch (tab) {
    case 'history': return historyBadgeAnim.value
    case 'tasks': return taskBadgeAnim.value
    case 'terminal': return terminalBadgeAnim.value
    case 'proxy': return proxyBadgeAnim.value
    default: return false
  }
}
function bigDockBadgeAnimEnd(tab) {
  switch (tab) {
    case 'history': historyBadgeAnim.value = false; break
    case 'tasks': taskBadgeAnim.value = false; break
    case 'terminal': terminalBadgeAnim.value = false; break
    case 'proxy': proxyBadgeAnim.value = false; break
  }
}
const bigDockActiveIndex = computed(() => {
  const i = BIG_SCREEN_DOCK_TABS.indexOf(leftTab.value)
  return i >= 0 ? i : 0
})
const bigDockIndicatorStyle = computed(() => ({
  transform: `translateY(${bigDockActiveIndex.value * DOCK_STEP}px)`,
}))

const isOverflowTabActive = computed(() => popupOverflowTabs.value.includes(activeTab.value))

const overflowPopupStyle = computed(() => {
  const btn = overflowBtnRef.value
  if (!btn) return {}
  const rect = btn.getBoundingClientRect()
  const vp = getZoomedViewport()
  return {
    position: 'fixed',
    bottom: `${toFixedCSS(vp.height - rect.top + 8)}px`,
    right: `${toFixedCSS(vp.width - rect.right)}px`,
  }
})

const overflowButtonIcon = computed(() => {
  // If active tab is in the popup, show its icon on the overflow button
  if (popupOverflowTabs.value.includes(activeTab.value)) {
    return overflowTabMeta[activeTab.value]?.icon ?? MoreHorizontal
  }
  return MoreHorizontal
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
    if (!allInlineOverflowTabs.value.includes('tasks')) triggerBadgeAnim(overflowBadgeAnim)
  }
})
watch(() => store.state.terminalSessionCount, (n, o) => {
  if (o !== undefined && n !== o) {
    triggerBadgeAnim(terminalBadgeAnim)
    if (!allInlineOverflowTabs.value.includes('terminal')) triggerBadgeAnim(overflowBadgeAnim)
  }
})
watch(() => store.state.portForwardActiveCount, (n, o) => {
  if (o !== undefined && n !== o) {
    triggerBadgeAnim(proxyBadgeAnim)
    if (!allInlineOverflowTabs.value.includes('proxy')) triggerBadgeAnim(overflowBadgeAnim)
  }
})

const overflowBadgeCount = computed(() => {
  let count = store.state.taskUnreadCount
  if (!isSSHDisabled.value) count += store.state.portForwardActiveCount
  if (!isTerminalDisabled.value) count += store.state.terminalSessionCount
  // Subtract counts for ALL inline overflow tabs
  for (const tab of allInlineOverflowTabs.value) {
    if (tab === 'tasks') count -= store.state.taskUnreadCount
    else if (tab === 'proxy') count -= store.state.portForwardActiveCount
    else if (tab === 'terminal') count -= store.state.terminalSessionCount
  }
  return Math.max(0, count)
})

const overflowButtonTitle = computed(() => {
  if (popupOverflowTabs.value.includes(activeTab.value)) {
    return overflowTabMeta[activeTab.value] ? t(overflowTabMeta[activeTab.value].titleKey) : t('nav.more')
  }
  return t('nav.more')
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



async function applyTheme(t) {
    document.documentElement.setAttribute('data-theme', t)
    setSetting('theme', t)
    document.documentElement.setAttribute('data-hljs-theme', t)
    const { initMermaid, reRenderMermaid } = await import('./utils/mermaid.ts')
    await initMermaid()
    await reRenderMermaid()
}

/** Dismiss the native splash overlay in APP mode. */
function dismissSplash() {
    window.AndroidNative?.dismissSplash?.()
}

provide('theme', theme)
provide('applyTheme', applyTheme)
provide('activeTab', activeTab)
provide('switchTab', switchTab)
provide('hotSwitchProject', hotSwitchProject)

// Lightbox — expose open/openMdImages/openSvg via ref and provide at App level
// so TableRowModal (in <main> subtree, not Lightbox's subtree) can inject them
const lightboxRef = ref(null)
provide('openLightbox', (url, svg) => lightboxRef.value?.open(url, svg))
provide('openSvgLightbox', (svg) => lightboxRef.value?.openSvg(svg))
provide('openMdImages', (imgs, idx) => lightboxRef.value?.openMdImages(imgs, idx))

function handleOpenFileManager() {
    switchTab('browse')
}

function handleNavigateToCommit(e) {
    const sha = e?.detail?.sha
    if (sha) {
        setPendingCommitNavigation(sha)
    }
    switchTab('history')
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
    } catch {
        isAuthenticated.value = false
        if (isAppMode.value) {
            toast.show(t('toast.serverUnreachableApp'), { icon: '⚠️', type: 'error', duration: 5000 })
        } else {
            toast.show(t('toast.serverUnreachableWeb'), { icon: '⚠️', type: 'error', duration: 0, onClick: () => location.reload() })
        }
        return
    }
    if (!resp.ok) {
        if (resp.status === 401 || resp.status === 403) {
            if (isAppMode.value && window.AndroidNative?.getPassword?.()) {
                const savedPwd = window.AndroidNative.getPassword()
                if (savedPwd) {
                    try {
                        const loginRes = await fetch('/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: savedPwd }) })
                        if (loginRes.ok) {
                            if (window.AndroidNative?.setSSHPassword) window.AndroidNative.setSSHPassword(savedPwd)
                        } else { isAuthenticated.value = false; return }
                    } catch { isAuthenticated.value = false; return }
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
    }

    // ── Main app initialization ──
    // Complete ALL initialization BEFORE setting isAuthenticated = true,
    // so that ChatPanelContent mounts only when the clawbench_project cookie
    // and session identity are already available. This prevents loadHistory()
    // from firing with missing cookies (Android first-login bug).
    if (!(await initializeApp())) return
    isAuthenticated.value = true
    dismissSplash()
    await nextTick()
    applyUIScale(localConfig.uiScale ?? 1)
    startDockResize()
    welcomeOverlay.value?.show()
    versionMismatchOverlay.value?.show()
    checkForUpgrade()

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
      } catch {
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
      } catch {} // for cold-start pending navigation
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
        } catch {} // and then every 500ms for up to 3 seconds
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

// ── Ctrl+F / Cmd+F: open context-aware search drawer ──
const _dlg = useDialog()
function openChatSearchDrawer() {
  if (sessionSearchDrawer.isOpen.value) {
    sessionSearchDrawerRef.value?.focusSearchInput()
  } else {
    sessionSearchDrawer.open()
  }
}
function openBrowseSearchDrawer() {
  if (fileNav.overlayOpen.value) {
    if (searchDrawer.isOpen.value) {
      fileOverlayRef.value?.focusSearchInput()
    } else if (currentFile.value?.content) {
      searchDrawer.open()
    }
  } else {
    if (fileSearchDrawer.isOpen.value) {
      fileManagerRef.value?.focusSearchInput()
    } else {
      fileSearchDrawer.open()
    }
  }
}
function handleCtrlF(e) {
    if (!(e.ctrlKey || e.metaKey) || e.key !== 'f') return
    // Skip when focus is in input/textarea/contenteditable/terminal
    const tag = e.target?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA') return
    if (e.target?.isContentEditable) return
    if (e.target?.closest?.('.terminal-panel')) return
    // Skip when modal dialog or project dialog is open
    if (_dlg.state.value.visible || projectDialogOpen.value) return

    if (isBigScreen.value) {
        // Focus-aware: route Ctrl+F to the pane the user is working in
        if (activePane.value === 'right') {
            e.preventDefault()
            openChatSearchDrawer()
        } else if (panelIsActive('browse')) {
            e.preventDefault()
            openBrowseSearchDrawer()
        }
        // Left pane focused on a non-searchable tab → native Ctrl+F
    } else if (activeTab.value === 'chat') {
        e.preventDefault()
        openChatSearchDrawer()
    } else if (activeTab.value === 'browse') {
        e.preventDefault()
        openBrowseSearchDrawer()
    }
    // Other tabs: don't preventDefault — let browser handle Ctrl+F natively
}

onMounted(() => {
    document.addEventListener('keydown', handleCtrlF)
})

onUnmounted(() => {
    stopDockResize()
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
    document.removeEventListener('keydown', handleCtrlF)
    stopFlushTimer()
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

/* Big-screen split panes — positioned ancestors for the absolute TabPanels */
.col-left,
.col-right {
    position: relative;
    height: 100%;
}

/* Drag file/dir onto the chat column (big-screen) — highlight the drop target */
.chat-drop-active::after {
    content: '';
    position: absolute;
    inset: 4px;
    border: 2px dashed var(--accent-color, #0066cc);
    border-radius: 12px;
    background: color-mix(in srgb, var(--accent-color, #0066cc) 6%, transparent);
    pointer-events: none;
    z-index: 10;
}

.chat-drop-hint {
    position: absolute;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 10px 18px;
    border-radius: 999px;
    background: var(--bg-primary);
    border: 1px solid var(--accent-color, #0066cc);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
    color: var(--accent-color, #0066cc);
    font-size: 14px;
    font-weight: 600;
    pointer-events: none;
    z-index: 11;
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

/* Big-screen vertical dock (left edge) — VS Code activity-bar style */
.big-dock {
    flex-shrink: 0;
    width: 48px;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    background: var(--bg-primary);
    border-right: 1px solid var(--border-color);
    -webkit-tap-highlight-color: transparent;
    user-select: none;
}

.big-dock-center {
    position: relative;
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
}

/* VS Code activity-bar style active highlight: faint translucent theme tint
   spanning the dock width + a thin theme-colored bar on the left edge.
   Scoped under .big-dock so it outranks the base .dock-active-indicator
   (same single-class specificity — a bare .big-dock-active-indicator would
   lose to the later base rule and render as the circular water-drop). */
.big-dock .big-dock-active-indicator {
    position: absolute;
    left: 0;
    top: 0;
    width: 100%;
    height: 34px;
    border-radius: 0;
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
    /* Base uses a springy overshoot (for the bottom-dock water-drop); a smooth
       ease-out reads better on a full-width highlight. */
    transition: transform 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}
.big-dock .big-dock-active-indicator::before {
    content: '';
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 3px;
    background: var(--accent-color);
}

/* Active icon follows the theme color (VS Code activity bar) */
.big-dock .dock-btn.active {
    color: var(--accent-color);
}
.big-dock .dock-btn.active:hover {
    color: var(--accent-color);
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
