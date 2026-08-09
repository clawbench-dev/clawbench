<template>
  <div class="proxy-panel-content">
    <!-- Compact header: icon + title + scan + create buttons (matches chat/app header: flush border, bg-secondary) -->
    <div class="proxy-header">
      <span class="proxy-header-icon">
        <NetworkIcon :size="14" />
      </span>
      <span class="proxy-header-title">{{ t('nav.portForward') }}</span>
      <button class="header-btn" @click="handleOpenScan" :title="t('proxy.scanTitle')">
        <Search :size="14" />
      </button>
      <button class="create-btn" @click="openAddForm" :title="t('proxy.addPort')">
        <Plus :size="16" />
      </button>
    </div>

    <div class="proxy-panel">
      <!-- App mode: tunnel status banners -->
      <template v-if="isAppMode">
        <div v-if="tunnelStatus === 'disconnected'" class="tunnel-banner error">
          <XCircle :size="16" />
          <div class="tunnel-banner-content">
            <span class="tunnel-banner-title">{{ t('proxy.tunnelDisconnected') }}</span>
            <span class="tunnel-banner-detail">{{ tunnelErrorDetail }}</span>
          </div>
          <button class="tunnel-retry-btn" :class="{ spinning: tunnelChecking }" :disabled="tunnelChecking" @click="handleRetryTunnel" :title="t('proxy.retryCheck')">
            <RotateCcw :size="14" />
          </button>
        </div>
        <div v-else-if="tunnelStatus === 'degraded'" class="tunnel-banner warning">
          <AlertTriangle :size="16" />
          <div class="tunnel-banner-content">
            <span class="tunnel-banner-title">{{ t('proxy.portsNoResponse') }}</span>
            <span class="tunnel-banner-detail">{{ t('proxy.tunnelConnectedButNoResponse') }}</span>
          </div>
          <button class="tunnel-retry-btn" :class="{ spinning: tunnelChecking }" :disabled="tunnelChecking" @click="handleRetryTunnel" :title="t('proxy.retryCheck')">
            <RotateCcw :size="14" />
          </button>
        </div>

        <!-- App mode: background service tip -->
        <div v-if="tunnelStatus === 'ok'" class="tunnel-banner tip">
          <Info :size="16" />
          <div class="tunnel-banner-content">
            <span class="tunnel-banner-detail">{{ t('proxy.backgroundTip') }}</span>
          </div>
        </div>
      </template>

      <!-- Web mode: app recommendation banner -->
      <div v-if="!isAppMode" class="tunnel-banner tip">
        <Smartphone :size="16" />
        <div class="tunnel-banner-content">
          <span class="tunnel-banner-detail">{{ t('proxy.appRecommendation') }}</span>
        </div>
      </div>

      <!-- Web mode: manual SSH tunnel guide -->
      <div v-if="!isAppMode && sshInfo && sshInfo.enabled" class="tunnel-guide">
        <div class="tunnel-guide-header" @click="tunnelGuideExpanded = !tunnelGuideExpanded">
          <Lock :size="14" />
          <span>{{ t('proxy.tunnelGuide') }}</span>
          <ChevronDown :size="14" class="tunnel-guide-chevron" :class="{ expanded: tunnelGuideExpanded }" />
        </div>
        <div v-if="tunnelGuideExpanded" class="tunnel-guide-body">
          <template v-if="sshInfo.command">
            <div class="tunnel-guide-intro">{{ t('proxy.tunnelGuideIntro') }}</div>
            <div class="tunnel-guide-steps">
              <div class="tunnel-guide-step">{{ t('proxy.tunnelGuideStep1') }}</div>
              <div class="tunnel-guide-step">{{ t('proxy.tunnelGuideStep2') }}</div>
              <div class="tunnel-guide-step">{{ t('proxy.tunnelGuideStep3') }}</div>
            </div>
            <div class="tunnel-guide-command">
              <code>{{ sshInfo.command }}</code>
              <button class="tunnel-guide-copy" @click="copySSHCommand" :title="t('proxy.copyCommand')">
                <Copy :size="12" />
                {{ sshCopied ? t('common.copied') : t('proxy.copyCommand') }}
              </button>
            </div>
            <div v-if="sshInfo.fingerprint" class="tunnel-guide-fingerprint">
              <span class="fingerprint-label">Fingerprint:</span>
              <span class="fingerprint-value">{{ sshInfo.fingerprint }}</span>
            </div>
          </template>
          <div v-else class="tunnel-guide-intro">{{ t('proxy.tunnelNoCommand') }}</div>
        </div>
      </div>

      <!-- Web mode: SSH not enabled -->
      <div v-if="!isAppMode && sshInfo && !sshInfo.enabled" class="tunnel-banner warning">
        <AlertTriangle :size="16" />
        <div class="tunnel-banner-content">
          <span class="tunnel-banner-detail">{{ t('proxy.tunnelNoSsh') }}</span>
        </div>
      </div>

      <!-- Registered ports list -->
      <div class="proxy-zones">
        <div class="proxy-zone-registered">
          <!-- Loading -->
          <div v-if="loading" class="proxy-loading">{{ t('common.loading') }}</div>

          <!-- Port list -->
          <div v-else-if="ports.length > 0" class="proxy-list">
            <ProxyPortItem
              v-for="p in ports"
              :key="p.localPort"
              :port="p.port"
              :local-port="p.localPort"
              :host="p.host || ''"
              :name="p.name"
              :protocol="p.protocol"
              :active="p.active"
              :enabled="p.enabled"
              :tunnel-disconnected="tunnelStatus === 'disconnected'"
              :reconnecting="reconnectingPorts.has(p.localPort)"
              :connecting="connectingPorts.has(p.localPort)"
              :toggling="togglingPorts.has(p.localPort)"
              @open="openPortWithCheck"
              @open-external="openInExternalBrowser"
              @reconnect="handleReconnect"
              @edit="handleEdit"
              @remove="handleRemove"
              @toggle-enabled="handleToggleEnabled"
            />
          </div>

          <!-- Empty state -->
          <div v-else class="proxy-empty">
            <NetworkIcon :size="40" class="proxy-empty-icon" />
            <div class="proxy-empty-text">{{ t('proxy.noPorts') }}</div>
            <div class="proxy-empty-hint">{{ t('proxy.emptyHint') }}</div>
          </div>
        </div>
      </div>

      <!-- Port scan drawer (bound to proxy tab: auto-hides on tab switch) -->
      <BottomSheet :open="scanDrawer.effectiveOpen.value" auto @close="scanDrawer.close()">
        <template #header>
          <Search :size="16" class="bs-header-icon" />
          <span class="bs-header-title">{{ t('proxy.scanTitle') }}</span>
          <span class="port-scan-header-spacer"></span>
          <button class="port-scan-rescan-icon" :disabled="scanning" :title="t('proxy.rescan')" @click.stop="rescanPorts">
            <RefreshCw :size="16" />
          </button>
        </template>
        <div class="port-scan-content">
          <div v-if="scanning" class="port-scan-loading">
            <div class="port-scan-spinner"></div>
            <span>{{ t('proxy.scanInProgress') }}</span>
          </div>

          <template v-else>
            <div v-if="detectedPortsNotRegistered.length > 0" class="port-scan-results">
              <div class="port-scan-count">{{ t('proxy.scanCount', { count: detectedPortsNotRegistered.length }) }}</div>
              <button
                v-for="p in detectedPortsNotRegistered"
                :key="p.port"
                class="port-scan-item"
                :class="p.protocol"
                @click="handleQuickAdd(p.port, p.protocol, p.processName)"
              >
                <span class="port-scan-item-port">{{ p.port }}</span>
                <span class="port-scan-item-proto">{{ p.protocol }}</span>
                <span v-if="p.processName" class="port-scan-item-process">
                  {{ p.processName }}<span v-if="p.processArgs"> {{ p.processArgs }}</span>
                </span>
                <Plus :size="14" class="port-scan-item-add" />
              </button>
            </div>

            <div v-else class="port-scan-empty">
              <Search :size="24" />
              <span>{{ t('proxy.scanNoResults') }}</span>
            </div>
          </template>
        </div>
      </BottomSheet>

      <!-- Add/Edit Modal (shared) -->
      <ModalDialog :open="showForm" :title="isEditMode ? t('proxy.editPort') : t('proxy.addPort')" @close="showForm = false">
        <div class="port-add-content">
          <div v-if="formError" class="port-add-error">{{ formError }}</div>
          <div class="port-add-row">
            <label class="port-add-label">{{ t('proxy.protocolLabel') }}</label>
            <select v-model="formProtocol" class="port-add-select">
              <option value="http">HTTP</option>
              <option value="https">HTTPS</option>
            </select>
          </div>
          <div class="port-add-row">
            <label class="port-add-label">{{ t('proxy.portPlaceholder') }} *</label>
            <input
              ref="portInputRef"
              v-model="formPort"
              type="number"
              class="port-add-input"
              :placeholder="t('proxy.portPlaceholder')"
              min="1"
              max="65535"
              :readonly="isEditMode"
              @keydown.enter="handleSave"
            />
          </div>
          <div class="port-add-row">
            <label class="port-add-label">{{ t('proxy.hostPlaceholder') }}</label>
            <input
              v-model="formHost"
              type="text"
              class="port-add-input"
              :placeholder="t('proxy.hostPlaceholder')"
              @keydown.enter="handleSave"
            />
          </div>
          <div class="port-add-row">
            <label class="port-add-label">{{ t('proxy.namePlaceholder') }}</label>
            <input
              v-model="formName"
              type="text"
              class="port-add-input"
              :placeholder="t('proxy.namePlaceholder')"
              @keydown.enter="handleSave"
            />
          </div>
        </div>
        <template #footer>
          <button class="port-add-cancel" @click="showForm = false">{{ t('common.cancel') }}</button>
          <button class="port-add-confirm" @click="handleSave" :disabled="!isValidPort || saving">{{ saving ? '...' : t('common.confirm') }}</button>
        </template>
      </ModalDialog>

    </div>
  </div>
</template>

<script setup>
import { XCircle, RotateCcw, AlertTriangle, Info, Plus, Search, Lock, Copy, Smartphone, ChevronDown, RefreshCw, Network as NetworkIcon } from 'lucide-vue-next'
import { ref, computed, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ProxyPortItem from './ProxyPortItem.vue'
import ModalDialog from '@/components/common/ModalDialog.vue'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { usePortForward } from '@/composables/usePortForward.ts'
import { useTabDrawer } from '@/composables/useTabDrawer.ts'
import { useToast } from '@/composables/useToast.ts'

const { t } = useI18n()

// Form state (shared for add & edit)
const showForm = ref(false)
const editingLocalPort = ref(null) // null = add mode, number = edit mode
const formPort = ref('')
const formName = ref('')
const formHost = ref('')
const formProtocol = ref('http')
const portInputRef = ref(null)
const formError = ref('')
const saving = ref(false)

const tunnelGuideExpanded = ref(false)
const togglingPorts = ref(new Set())

const isEditMode = computed(() => editingLocalPort.value !== null)

// Reset form and auto-focus port input when modal opens
watch(showForm, (val) => {
  if (val && !isEditMode.value) {
    formPort.value = ''
    formName.value = ''
    formHost.value = ''
    formProtocol.value = 'http'
    formError.value = ''
    saving.value = false
    nextTick(() => portInputRef.value?.focus())
  }
})

const { ports, detectedPorts, loading, isAppMode, sshInfo, tunnelStatus, tunnelChecking, tunnelError, tunnelErrorType, connectingPorts, scanning, hasScanned, registerPort, updatePort, unregisterPort, setPortEnabled, detectPorts, rescanPorts, checkTunnelHealth, openPortWithCheck, openInExternalBrowser, reconnectPort } = usePortForward()
const toast = useToast()

// Scan drawer is bound to the proxy tab: it auto-hides when switching tabs.
const scanDrawer = useTabDrawer('proxy')

// Open the drawer; auto-run a scan the first time it is opened.
function handleOpenScan() {
  scanDrawer.open()
  if (!hasScanned.value && !scanning.value) {
    detectPorts()
  }
}

const sshCopied = ref(false)

// Track which ports are currently reconnecting (for spinning button state)
const reconnectingPorts = ref(new Set())

// Compute contextual error detail based on error type from native bridge
const tunnelErrorDetail = computed(() => {
  if (!tunnelError.value && !tunnelErrorType.value) {
    return t('proxy.tunnelDisconnectedDetail')
  }
  const type = tunnelErrorType.value
  if (type === 'auth') return t('proxy.tunnelErrorAuth')
  if (type === 'network') return t('proxy.tunnelErrorNetwork')
  if (type === 'hostkey') return t('proxy.tunnelErrorHostKey')
  // Fallback: show the raw error message from native layer
  if (tunnelError.value) return tunnelError.value
  return t('proxy.tunnelDisconnectedDetail')
})

const isValidPort = computed(() => {
  const p = parseInt(formPort.value)
  return p > 0 && p <= 65535
})

const detectedPortsNotRegistered = computed(() => {
  const registered = new Set(ports.value.map(p => p.port))
  return detectedPorts.value
    .filter(p => !registered.has(p.port))
    .sort((a, b) => a.port - b.port)
})

function openAddForm() {
  editingLocalPort.value = null
  showForm.value = true
}

function handleEdit(localPort) {
  const port = ports.value.find(p => p.localPort === localPort)
  if (!port) return
  editingLocalPort.value = localPort
  formPort.value = String(port.port)
  formName.value = port.name || ''
  formHost.value = port.host || ''
  formProtocol.value = port.protocol || 'http'
  formError.value = ''
  saving.value = false
  showForm.value = true
}

async function handleSave() {
  if (!isValidPort.value) return
  saving.value = true
  formError.value = ''
  try {
    if (isEditMode.value) {
      await updatePort(editingLocalPort.value, parseInt(formPort.value), formHost.value || '', formName.value || '', formProtocol.value)
    } else {
      await registerPort(parseInt(formPort.value), formName.value || undefined, formProtocol.value, formHost.value || undefined)
    }
    showForm.value = false
    editingLocalPort.value = null
  } catch (e) {
    formError.value = e?.message || t('proxy.addPort') + ' failed'
  } finally {
    saving.value = false
  }
}

async function handleQuickAdd(port, protocol, processName) {
  try {
    await registerPort(port, processName || t('proxy.autoDetect'), protocol || 'http')
  } catch (e) {
    toast.show(e?.message || t('proxy.addPort') + ' failed', { type: 'error', icon: '❌' })
  }
}

async function handleRemove(localPort) {
  await unregisterPort(localPort)
}

async function handleToggleEnabled(localPort, enabled) {
  if (togglingPorts.value.has(localPort)) return
  togglingPorts.value.add(localPort)
  togglingPorts.value = new Set(togglingPorts.value)
  try {
    await setPortEnabled(localPort, enabled)
  } catch (e) {
    toast.show(e?.message || t('proxy.addPort') + ' failed', { type: 'error', icon: '❌' })
  } finally {
    togglingPorts.value.delete(localPort)
    togglingPorts.value = new Set(togglingPorts.value)
  }
}

async function handleReconnect(localPort) {
  if (reconnectingPorts.value.has(localPort)) return
  reconnectingPorts.value.add(localPort)
  // Trigger reactivity by replacing the Set
  reconnectingPorts.value = new Set(reconnectingPorts.value)
  try {
    await reconnectPort(localPort)
  } finally {
    reconnectingPorts.value.delete(localPort)
    reconnectingPorts.value = new Set(reconnectingPorts.value)
  }
}

async function copySSHCommand() {
  if (!sshInfo.value?.command) return
  try {
    await navigator.clipboard.writeText(sshInfo.value.command)
    sshCopied.value = true
    setTimeout(() => { sshCopied.value = false }, 2000)
  } catch {}
}

async function handleRetryTunnel() {
  const prevStatus = tunnelStatus.value
  try {
    await checkTunnelHealth()
  } catch {
    toast.show(t('proxy.toast.checkFailed'), { icon: '🚫', type: 'error' })
    return
  }
  if (tunnelStatus.value === 'ok') {
    toast.show(t('proxy.toast.tunnelRecovered'), { icon: '🔗', type: 'success' })
  } else if (tunnelStatus.value === 'degraded' && prevStatus === 'disconnected') {
    toast.show(t('proxy.toast.tunnelConnectedNoResponse'), { icon: 'ℹ️', type: 'info' })
  } else if (tunnelStatus.value === 'disconnected') {
    toast.show(t('proxy.toast.tunnelStillDisconnected'), { icon: '🚫', type: 'error' })
  } else if (tunnelStatus.value === 'degraded') {
    toast.show(t('proxy.toast.portsStillNoResponse'), { icon: '🚫', type: 'error' })
  }
}
</script>

<style scoped>
.proxy-panel-content {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  /* White container so the bg-secondary cards stand out, matching the
     scheduled-task list page look. */
  background: var(--bg-primary, #ffffff);
}

.proxy-panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 6px;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

/* Compact header — flush to panel edges, matches chat/app header style */
.proxy-header {
  display: flex;
  align-items: center;
  padding: 4px 6px;
  flex-shrink: 0;
  gap: 6px;
  background: var(--bg-secondary, #f8f9fa);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.proxy-header-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.proxy-header-icon {
  display: flex;
  align-items: center;
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}

/* Create button in header toolbar */
.create-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 14px;
  background: var(--accent-color, #0066cc);
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s ease;
}

/* Header icon button (scan, etc.) */
.header-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 14px;
  background: var(--bg-secondary, #f1f3f5);
  color: var(--text-secondary, #666);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s ease;
  position: relative;
}

.header-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (hover: hover) {
  .header-btn:hover:not(:disabled) {
    background: var(--bg-tertiary, #eef1f4);
    color: var(--accent-color, #0066cc);
  }
}

.header-btn:active:not(:disabled) {
  transform: scale(0.9);
}

@media (hover: hover) {
  .create-btn:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 85%, black);
    transform: translateY(-1px);
  }
}

.create-btn:active {
  transform: scale(0.9);
}

/* Tunnel status banner */
.tunnel-banner {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 8px 10px;
  border-radius: 6px;
  border-left: 3px solid;
  flex-shrink: 0;
}

.tunnel-banner.error {
  background: rgba(239, 68, 68, 0.08);
  border-left-color: #ef4444;
  color: #dc2626;
}

.tunnel-banner.warning {
  background: rgba(245, 158, 11, 0.08);
  border-left-color: #f59e0b;
  color: #d97706;
}

.tunnel-banner.tip {
  background: rgba(59, 130, 246, 0.06);
  border-left-color: #3b82f6;
  color: var(--text-secondary, #666);
}

.tunnel-banner.tip .tunnel-banner-detail {
  opacity: 1;
}

.tunnel-banner svg {
  flex-shrink: 0;
  margin-top: 1px;
}

.tunnel-banner-content {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.tunnel-banner-title {
  font-size: 13px;
  font-weight: 600;
}

.tunnel-banner-detail {
  font-size: 11px;
  opacity: 0.8;
}

.tunnel-retry-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.15);
  cursor: pointer;
  transition: background 0.15s;
  margin-left: auto;
  align-self: center;
}

.tunnel-retry-btn:active:not(:disabled) {
  background: rgba(255, 255, 255, 0.25);
}

.tunnel-retry-btn:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.tunnel-retry-btn.spinning svg {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.proxy-loading,
.proxy-empty {
  padding: 24px 12px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 13px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.proxy-empty-icon {
  color: var(--text-muted, #999);
  opacity: 0.5;
}

.proxy-empty-hint {
  font-size: 11px;
  margin-top: 4px;
  color: var(--text-muted, #999);
  opacity: 0.7;
}

.proxy-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* Two-zone layout: registered ports (top, 50–100%) + detected ports (bottom, 0–50%, pinned to bottom) */
.proxy-zones {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* Zone 1: registered ports — takes all space when no detected ports, at least 50% when detected ports exist */
.proxy-zone-registered {
  flex: 1 1 50%;
  min-height: 0;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
}

/* Tunnel guide (web mode) */
.tunnel-guide {
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 6px;
  overflow: hidden;
  flex-shrink: 0;
}

.tunnel-guide-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary, #666);
  cursor: pointer;
  transition: background 0.15s;
  user-select: none;
}

.tunnel-guide-header:hover {
  background: var(--bg-tertiary, #f5f5f5);
}

.tunnel-guide-header:active {
  background: var(--bg-secondary, #eee);
}

.tunnel-guide-chevron {
  margin-left: auto;
  transition: transform 0.2s;
}

.tunnel-guide-chevron.expanded {
  transform: rotate(180deg);
}

.tunnel-guide-body {
  padding: 0 10px 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.tunnel-guide-intro {
  font-size: 11px;
  color: var(--text-secondary, #666);
  line-height: 1.4;
}

.tunnel-guide-steps {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.tunnel-guide-step {
  font-size: 11px;
  color: var(--text-secondary, #666);
  line-height: 1.4;
}

.tunnel-guide-command {
  background: var(--bg-tertiary, #f5f5f5);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 4px;
  padding: 6px 8px;
  margin-top: 2px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.tunnel-guide-command code {
  font-family: monospace;
  font-size: 10px;
  color: var(--text-primary, #1a1a1a);
  word-break: break-all;
  line-height: 1.5;
  white-space: pre-wrap;
  display: block;
}

.tunnel-guide-copy {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 6px;
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 4px;
  background: none;
  color: var(--text-secondary, #666);
  font-size: 10px;
  cursor: pointer;
  transition: all 0.15s;
  align-self: flex-end;
}

.tunnel-guide-copy:hover {
  border-color: var(--accent-color, #0066cc);
  color: var(--accent-color, #0066cc);
}

.tunnel-guide-fingerprint {
  display: flex;
  align-items: baseline;
  gap: 4px;
  font-size: 9px;
  margin-top: 2px;
}

.fingerprint-label {
  color: var(--text-muted, #999);
  flex-shrink: 0;
}

.fingerprint-value {
  font-family: monospace;
  color: var(--text-muted, #999);
  word-break: break-all;
}
</style>

<style>
/* Port scan drawer */
.port-scan-content {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  min-height: 0;
}

.port-scan-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 40px 12px;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.port-scan-spinner {
  width: 28px;
  height: 28px;
  border: 3px solid var(--bg-tertiary, #e9ecef);
  border-top-color: var(--accent-color, #0066cc);
  border-radius: 50%;
  animation: port-scan-spin 0.8s linear infinite;
}

@keyframes port-scan-spin {
  to { transform: rotate(360deg); }
}

/* Rescan icon in drawer header */
.port-scan-header-spacer {
  flex: 1;
}

.port-scan-rescan-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  background: none;
  color: var(--text-muted, #999);
  cursor: pointer;
  border-radius: 0;
  transition: all 0.15s;
}

.port-scan-rescan-icon:hover:not(:disabled) {
  color: var(--accent-color, #0066cc);
  background: var(--bg-tertiary, #f0f0f0);
}

.port-scan-rescan-icon:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.port-scan-count {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary, #666);
  flex-shrink: 0;
}

.port-scan-results {
  display: flex;
  flex-direction: column;
  gap: 6px;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 2px;
}

.port-scan-item {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  text-align: left;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--border-color, #e5e5e5);
  border-left: 3px solid #3b82f6;
  border-radius: 0;
  background: var(--bg-secondary, #f8f9fa);
  color: var(--text-primary, #1a1a1a);
  cursor: pointer;
  transition: all 0.15s;
  min-width: 0;
}

.port-scan-item:hover {
  border-color: var(--accent-color, #0066cc);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
  transform: translateY(-1px);
}

.port-scan-item.https {
  border-left-color: #8b5cf6;
}

.port-scan-item-port {
  font-family: monospace;
  font-weight: 700;
  font-size: 15px;
  flex-shrink: 0;
}

.port-scan-item-proto {
  font-size: 9px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  padding: 2px 6px;
  border-radius: 0;
  background: rgba(59, 130, 246, 0.12);
  color: #3b82f6;
  flex-shrink: 0;
}

.port-scan-item.https .port-scan-item-proto {
  background: rgba(139, 92, 246, 0.12);
  color: #8b5cf6;
}

.port-scan-item-process {
  font-size: 11px;
  font-family: monospace;
  color: var(--text-muted, #999);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
  flex: 1;
}

.port-scan-item-add {
  color: var(--text-muted, #999);
  flex-shrink: 0;
  margin-left: auto;
}

.port-scan-item:hover .port-scan-item-add {
  color: var(--accent-color, #0066cc);
}

.port-scan-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 40px 12px;
  color: var(--text-muted, #999);
  font-size: 13px;
  opacity: 0.8;
}

.port-add-content {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 10px;
}

.port-add-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.port-add-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary, #666);
}

.port-add-input {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 6px;
  font-size: 14px;
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  font-family: inherit;
  box-sizing: border-box;
}

.port-add-input:focus {
  outline: none;
  border-color: var(--accent-color, #0066cc);
}

.port-add-input[readonly] {
  opacity: 0.6;
  cursor: not-allowed;
  background: var(--bg-tertiary, #f5f5f5);
}

.port-add-select {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 6px;
  font-size: 14px;
  background: var(--bg-primary, #fff);
  color: var(--text-primary, #1a1a1a);
  font-family: inherit;
  cursor: pointer;
  box-sizing: border-box;
}

.port-add-select:focus {
  outline: none;
  border-color: var(--accent-color, #0066cc);
}

.port-add-error {
  font-size: 12px;
  color: #dc2626;
  background: rgba(239, 68, 68, 0.08);
  padding: 6px 10px;
  border-radius: 4px;
}

.port-add-confirm {
  padding: 8px 16px;
  border: none;
  border-radius: 6px;
  font-size: 13px;
  cursor: pointer;
  background: var(--accent-color, #0066cc);
  color: #fff;
  font-weight: 600;
}

.port-add-confirm:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.port-add-cancel {
  padding: 8px 16px;
  border: none;
  border-radius: 6px;
  font-size: 13px;
  cursor: pointer;
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  font-weight: 600;
}
</style>
