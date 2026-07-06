<template>
  <Transition name="welcome-fade">
    <div v-if="visible" class="welcome-overlay" @click.self="close">
      <div class="welcome-panel">
        <div class="welcome-header">
          <h3>{{ t('welcomeInfo.title') }}</h3>
          <button class="welcome-close" @click="close">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
              <path d="M18 6L6 18M6 6l12 12"/>
            </svg>
          </button>
        </div>
        <p class="welcome-desc">{{ t('welcomeInfo.desc') }}<span class="desc-highlight">{{ t('welcomeInfo.descHighlight') }}</span></p>
        <div class="backends-list">
          <div
            v-for="b in sortedBackends"
            :key="b.id"
            class="backend-item"
            :class="{ 'backend-not-detected': !detectedBackends.has(b.id) && !installingBackendIds.has(b.id) }"
          >
            <div class="backend-icon">{{ b.icon }}</div>
            <div class="backend-info">
              <div class="backend-name">{{ b.name }}</div>
              <div class="backend-specialty">{{ b.specialty }}</div>
            </div>
            <span
              v-if="detectedBackends.has(b.id)"
              class="backend-badge badge-installed"
            >
              {{ t('welcomeInfo.detected') }}
            </span>
            <button
              v-if="!detectedBackends.has(b.id) && b.install_cmd && !installingBackendIds.has(b.id)"
              class="btn-install"
              @click="startInstall(b)"
            >{{ t('welcomeInfo.install') }}</button>
            <span
              v-if="!detectedBackends.has(b.id) && b.install_cmd && installingBackendIds.has(b.id)"
              class="btn-installing"
            >
              <span class="install-spinner"></span>
              {{ t('welcomeInfo.installing') }}
            </span>
          </div>
        </div>
        <!-- Install section (mobile web only) -->
        <div v-if="showInstallSection" class="welcome-install">
          <div v-if="pwaInstall.showPwaInstall.value" class="welcome-install-row" role="button" tabindex="0" @click="handlePwaInstall" @keydown.enter="handlePwaInstall">
            <MonitorSmartphone :size="16" />
            <span>{{ t('pwa.addToHomeScreen') }}</span>
          </div>
          <div v-if="pwaInstall.showApkDownload.value" class="welcome-install-row" role="button" tabindex="0" @click="handleApkDownload" @keydown.enter="handleApkDownload">
            <Smartphone :size="16" />
            <span>{{ t('pwa.downloadAndroidApp') }}</span>
          </div>
        </div>
        <div class="welcome-footer">
          <button class="btn-ok" @click="close">
            {{ t('welcomeInfo.ok') }}
          </button>
          <div class="footer-secondary">
            <button class="btn-rescan" :disabled="rescanning" @click="rescan">
              {{ rescanning ? t('welcomeInfo.rescanning') : t('welcomeInfo.rescan') }}
            </button>
            <button class="btn-dont-show" @click="dontShowAgain">
              {{ t('welcomeInfo.dontShowAgain') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </Transition>

  <!-- iOS install instructions sheet -->
  <IosInstallDrawer :open="showIosSheet" @close="showIosSheet = false" />

  <!-- Agent install dialogs (one per concurrent install; hidden ones are v-show'd to keep alive) -->
  <AgentInstallDialog
    v-for="d in installDialogs"
    :key="d.backendId"
    :backend-id="d.backendId"
    :backend-name="d.backendName"
    :install-cmd="d.installCmd"
    :visible="!d.hidden"
    @close="closeInstallDialog(d.backendId)"
    @success="handleInstallSuccess(d.backendId)"
    @failed="handleInstallFailed(d.backendId)"
  />
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { useAgents } from '@/composables/useAgents'
import { MonitorSmartphone, Smartphone } from 'lucide-vue-next'
import IosInstallDrawer from './common/IosInstallDrawer.vue'
import AgentInstallDialog from './AgentInstallDialog.vue'
import { appLog } from '@/utils/appLog'

interface BackendInfo {
  id: string
  name: string
  icon: string
  specialty: string
  default_cmd: string
  thinking_effort_levels?: string[]
  install_cmd?: string
}

interface InstallDialogEntry {
  backendId: string
  backendName: string
  installCmd: string
  hidden?: boolean // user closed the dialog UI but install is still running
}

const STORAGE_KEY = 'clawbench_welcome_dismissed'

defineExpose({ show, forceShow })

const emit = defineEmits<{
  dismissed: []
}>()

const { t } = useI18n()
const pwaInstall = usePwaInstall()
const { rescanAgents } = useAgents()
const visible = ref(false)
const backends = ref<BackendInfo[]>([])
const detectedBackends = ref<Set<string>>(new Set())
const showIosSheet = ref(false)
const installingBackendIds = ref<Set<string>>(new Set())
const installDialogs = ref<InstallDialogEntry[]>([])
const rescanning = ref(false)

// Sort: installed first, installing second, not-installed last
const sortedBackends = computed(() => {
  return [...backends.value].sort((a, b) => {
    const aInstalling = installingBackendIds.value.has(a.id)
    const bInstalling = installingBackendIds.value.has(b.id)
    const aDetected = detectedBackends.value.has(a.id)
    const bDetected = detectedBackends.value.has(b.id)
    if (aDetected !== bDetected) return aDetected ? -1 : 1
    if (aInstalling !== bInstalling) return aInstalling ? -1 : 1
    return 0
  })
})

const showInstallSection = computed(() => pwaInstall.showPwaInstall.value || pwaInstall.showApkDownload.value)

async function loadBackends() {
  try {
    const [backendsResp, agentsResp] = await Promise.all([
      fetch('/api/backends'),
      fetch('/api/agents'),
    ])
    if (backendsResp.ok) {
      const data = await backendsResp.json()
      backends.value = data.backends || []
    }
    if (agentsResp.ok) {
      const data = await agentsResp.json()
      const agentBackends = (data.agents || data || []).map((a: { backend?: string; id?: string }) => a.backend || a.id)
      detectedBackends.value = new Set(agentBackends)
    }
  } catch { /* will show empty list */ }
}

function show() {
  if (localStorage.getItem(STORAGE_KEY) === 'true') return
  visible.value = true
}

function close() {
  visible.value = false
}

function dontShowAgain() {
  localStorage.setItem(STORAGE_KEY, 'true')
  visible.value = false
  emit('dismissed')
}

async function rescan() {
  if (rescanning.value) return
  rescanning.value = true
  try {
    await rescanAgents()
    await loadBackends()
  } catch (e) {
    appLog.w('WelcomeOverlay', 'rescan failed', e)
  } finally {
    rescanning.value = false
  }
}

function startInstall(b: BackendInfo) {
  installingBackendIds.value = new Set([...installingBackendIds.value, b.id])
  installDialogs.value = [...installDialogs.value, {
    backendId: b.id,
    backendName: b.name,
    installCmd: b.install_cmd || '',
  }]
}

function closeInstallDialog(backendId: string) {
  // Hide the dialog UI but keep the component alive so it can finish the install
  // and emit success/failed to update the installing indicator.
  installDialogs.value = installDialogs.value.map(d =>
    d.backendId === backendId ? { ...d, hidden: true } : d
  )
}

async function handleInstallSuccess(backendId: string) {
  // Install verified — remove installing indicator and dialog entry
  installingBackendIds.value = new Set([...installingBackendIds.value].filter(id => id !== backendId))
  installDialogs.value = installDialogs.value.filter(d => d.backendId !== backendId)
  try {
    await rescan()
  } catch (e) {
    appLog.w('WelcomeOverlay', 'rescan after install failed', e)
  }
}

function handleInstallFailed(backendId: string) {
  // Install failed or not detected — remove installing indicator so user can retry
  installingBackendIds.value = new Set([...installingBackendIds.value].filter(id => id !== backendId))
  // Keep the dialog entry removed too (component already unmounted if hidden)
  installDialogs.value = installDialogs.value.filter(d => d.backendId !== backendId)
}

async function handlePwaInstall() {
  if (pwaInstall.canInstallPwa.value) {
    await pwaInstall.installPwa()
  } else if (pwaInstall.isIOS.value) {
    showIosSheet.value = true
  }
}

function handleApkDownload() {
  window.location.href = '/api/apk'
}

function forceShow() {
  visible.value = true
}

onMounted(() => {
  loadBackends()
  window.addEventListener('clawbench-show-welcome', forceShow)
})

onUnmounted(() => {
  window.removeEventListener('clawbench-show-welcome', forceShow)
})
</script>

<style scoped>
.welcome-overlay {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--bg-primary) 80%, transparent);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
  padding: 16px;
}

.welcome-panel {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  width: 100%;
  max-width: 420px;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  box-shadow: var(--shadow-lg, 0 8px 32px rgba(0,0,0,0.15));
  overflow: hidden;
}

.welcome-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 10px;
}

.welcome-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  color: var(--text-primary);
}

.welcome-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 50%;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  cursor: pointer;
  transition: background 0.2s;
}

.welcome-close:hover {
  background: var(--border-color);
}

.welcome-desc {
  margin: 0 16px 10px;
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.5;
}

.desc-highlight {
  color: var(--accent-color);
  font-weight: 600;
}

.backends-list {
  flex: 1;
  max-height: 40vh;
  overflow-y: auto;
  padding: 0 12px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.backend-item {
  position: relative;
  display: flex;
  gap: 8px;
  padding: 8px 10px;
  border-radius: 8px;
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  text-align: left;
  align-items: center;
}

.backend-not-detected {
  opacity: 0.5;
}

.backend-icon {
  font-size: 20px;
  line-height: 1;
  flex-shrink: 0;
  width: 24px;
  text-align: center;
}

.backend-info {
  flex: 1;
  min-width: 0;
}

.backend-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.3;
}

.backend-specialty {
  font-size: 11px;
  color: var(--text-muted);
  line-height: 1.3;
  margin-top: 1px;
}

.backend-badge {
  position: absolute;
  right: 6px;
  bottom: 4px;
  font-size: 9px;
  font-weight: 600;
  padding: 1px 5px;
  border-radius: 6px;
  white-space: nowrap;
}

.badge-installed {
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
  color: var(--accent-color);
}

.badge-not-installed {
  background: var(--bg-tertiary);
  color: var(--text-muted);
}

.btn-install {
  position: absolute;
  right: 6px;
  top: 4px;
  font-size: 9px;
  font-weight: 600;
  padding: 2px 6px;
  border: none;
  border-radius: 6px;
  background: var(--accent-color);
  color: #fff;
  cursor: pointer;
  transition: opacity 0.2s;
}

.btn-install:hover {
  opacity: 0.85;
}

.btn-installing {
  position: absolute;
  right: 6px;
  top: 4px;
  font-size: 9px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 6px;
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
  color: var(--accent-color);
  display: flex;
  align-items: center;
  gap: 4px;
}

.install-spinner {
  display: inline-block;
  width: 10px;
  height: 10px;
  border: 1.5px solid color-mix(in srgb, var(--accent-color) 40%, transparent);
  border-top-color: var(--accent-color);
  border-radius: 50%;
  animation: install-spin 0.6s linear infinite;
}

@keyframes install-spin {
  to { transform: rotate(360deg); }
}

/* Install section */
.welcome-install {
  padding: 8px 12px 4px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.welcome-install-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--accent-color) 8%, var(--bg-primary));
  border: 1px solid color-mix(in srgb, var(--accent-color) 20%, var(--border-color));
  color: var(--accent-color);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s;
}

.welcome-install-row:hover {
  background: color-mix(in srgb, var(--accent-color) 15%, var(--bg-primary));
}

.welcome-footer {
  padding: 10px 16px 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
}

.footer-secondary {
  display: flex;
  gap: 12px;
  align-items: center;
}

.btn-ok {
  width: 100%;
  padding: 8px 16px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color);
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.2s;
}

.btn-ok:hover {
  opacity: 0.9;
}

.btn-rescan {
  background: none;
  border: 1px solid var(--border-color);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  padding: 4px 10px;
  border-radius: 6px;
  transition: all 0.2s;
}

.btn-rescan:hover {
  border-color: var(--accent-color);
  color: var(--accent-color);
}

.btn-rescan:disabled {
  opacity: 0.6;
  cursor: not-allowed;
  border-color: var(--accent-color);
  color: var(--accent-color);
}

.btn-dont-show {
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 12px;
  cursor: pointer;
  padding: 4px 8px;
  transition: color 0.2s;
}

.btn-dont-show:hover {
  color: var(--text-secondary);
}

/* ── Transition ── */

.welcome-fade-enter-active {
  transition: opacity 0.2s ease;
}
.welcome-fade-leave-active {
  transition: opacity 0.15s ease;
}
.welcome-fade-enter-from,
.welcome-fade-leave-to {
  opacity: 0;
}
</style>
