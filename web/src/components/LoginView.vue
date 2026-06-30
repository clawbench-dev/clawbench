<template>
  <div class="login-page">
    <!-- Decorative background elements -->
    <div class="login-bg-gradient"></div>
    <div class="login-bg-grid"></div>

    <div class="login-content">
      <!-- Brand section -->
      <div class="login-brand">
        <div class="login-logo-wrapper">
          <img class="login-logo" src="/logo.png" alt="ClawBench">
          <div class="login-logo-ring"></div>
        </div>
        <h1 class="login-title">ClawBench</h1>
        <p class="login-slogan">{{ t('login.slogan') }}</p>
        <p class="login-subtitle">{{ t('login.subtitle') }}</p>
      </div>

      <!-- Form section -->
      <div class="login-form-card">
        <!-- Server selector (APP mode, >=2 servers) -->
        <div v-if="isAppMode && showServerSelector" class="server-selector">
          <div
            v-for="srv in servers"
            :key="srv.url"
            class="server-item"
            :class="{ active: srv.url === selectedServerUrl }"
            @click="selectServer(srv)"
          >
            <div class="server-info">
              <Server :size="14" class="server-icon" />
              <span class="server-url">{{ formatServerHost(srv.url) }}</span>
            </div>
            <button class="server-delete" @click.stop="deleteServer(srv.url)" :title="t('login.deleteServer')">
              <X :size="12" />
            </button>
          </div>
        </div>

        <!-- Login form (existing server) -->
        <form v-if="!showAddForm" @submit.prevent="handleLogin">
          <div class="input-group">
            <svg class="input-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
              <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
            </svg>
            <input
              type="password"
              v-model="password"
              :placeholder="t('login.passwordPlaceholder')"
              autocomplete="current-password"
              :disabled="loading"
            />
          </div>
          <button type="submit" :disabled="loading" class="login-btn">
            <span v-if="loading" class="btn-spinner"></span>
            <span>{{ loading ? t('login.verifying') : t('login.submit') }}</span>
          </button>
          <div v-if="error" class="error">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
              <circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/>
            </svg>
            {{ error }}
            <button v-if="isAppMode && networkError" class="reconfigure-link" @click="handleReconfigure">{{ t('appHeader.reconfigureServer') }}</button>
          </div>
        </form>

        <!-- Add server form -->
        <form v-else @submit.prevent="handleAddServer">
          <div class="input-group">
            <Server :size="18" class="input-icon" />
            <input
              type="url"
              v-model="newServerUrl"
              :placeholder="t('login.serverUrlPlaceholder')"
              autocomplete="off"
              :disabled="loading"
            />
          </div>
          <div class="input-group" style="margin-top: 10px;">
            <svg class="input-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
              <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
            </svg>
            <input
              type="password"
              v-model="newServerPassword"
              :placeholder="t('login.serverPasswordPlaceholder')"
              autocomplete="off"
              :disabled="loading"
            />
          </div>
          <button type="submit" :disabled="loading || !newServerUrl" class="login-btn">
            <span v-if="loading" class="btn-spinner"></span>
            <span>{{ loading ? t('login.verifying') : t('login.addServerSubmit') }}</span>
          </button>
          <button type="button" class="cancel-btn" @click="showAddForm = false">{{ t('common.cancel') }}</button>
          <div v-if="error" class="error">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
              <circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/>
            </svg>
            {{ error }}
          </div>
        </form>

        <!-- Add server button (APP mode) -->
        <button v-if="isAppMode && !showAddForm" class="add-server-btn" @click="showAddForm = true">
          <Plus :size="14" />
          <span>{{ t('login.addServer') }}</span>
        </button>
      </div>

      <!-- Install banner (mobile web only) -->
      <div v-if="showInstallBanner" class="install-banner">
        <div v-if="pwaInstall.showPwaInstall.value" class="install-row" role="button" tabindex="0" @click="handlePwaInstall" @keydown.enter="handlePwaInstall">
          <MonitorSmartphone :size="18" class="install-icon" />
          <div class="install-info">
            <span class="install-label">{{ t('pwa.addToHomeScreen') }}</span>
            <span class="install-desc">{{ t('pwa.installDescription') }}</span>
          </div>
          <ChevronRight :size="16" class="install-arrow" />
        </div>
        <div v-if="pwaInstall.showApkDownload.value" class="install-row" role="button" tabindex="0" @click="handleApkDownload" @keydown.enter="handleApkDownload">
          <Smartphone :size="18" class="install-icon" />
          <div class="install-info">
            <span class="install-label">{{ t('pwa.downloadAndroidApp') }}</span>
            <span class="install-desc">{{ t('pwa.downloadAndroidAppDesc') }}</span>
          </div>
          <ChevronRight :size="16" class="install-arrow" />
        </div>
      </div>
    </div>

    <!-- iOS install instructions sheet -->
    <IosInstallDrawer :open="showIosSheet" @close="showIosSheet = false" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppMode } from '@/composables/useAppMode'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { useServerList } from '@/composables/useServerList'
import { formatServerHost } from '@/utils/url'
import { Server, X, Plus, MonitorSmartphone, Smartphone, ChevronRight } from 'lucide-vue-next'
import IosInstallDrawer from './common/IosInstallDrawer.vue'

const { t } = useI18n()
const { isAppMode } = useAppMode()
const pwaInstall = usePwaInstall()
const emit = defineEmits(['loginSuccess'])

const { servers, load: loadServers, save: saveServer, remove: removeServer, getPassword } = useServerList()

const password = ref('')
const loading = ref(false)
const error = ref('')
const networkError = ref(false)
const selectedServerUrl = ref('')
const showAddForm = ref(false)
const newServerUrl = ref('')
const newServerPassword = ref('')
const showIosSheet = ref(false)

const showServerSelector = computed(() => servers.value.length >= 1)
const showInstallBanner = computed(() => pwaInstall.showPwaInstall.value || pwaInstall.showApkDownload.value)

function selectServer(srv) {
  if (srv.url === selectedServerUrl.value) return
  // Use native connectToServer for pre-auth, SSL handling, and error recovery
  if (window.AndroidNative?.connectToServer) {
    window.AndroidNative.connectToServer(srv.url, srv.password)
  } else {
    window.location.href = srv.url + '/login'
  }
}

function deleteServer(url) {
  if (!confirm(t('login.deleteServer'))) return
  removeServer(url)
}

async function handleLogin() {
  if (!password.value) return
  loading.value = true
  error.value = ''
  networkError.value = false
  try {
    const res = await fetch('/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: password.value })
    })
    if (res.ok) {
      // Save password to Android native layer
      if (window.AndroidNative?.isNativeApp?.()) {
        window.AndroidNative.setSSHPassword(password.value)
        // Also save to server list
        const currentUrl = window.location.origin
        saveServer(currentUrl, password.value)
      }
      emit('loginSuccess')
    } else if (res.status >= 500) {
      error.value = t('login.serverError')
    } else {
      error.value = t('login.wrongPassword')
    }
  } catch (_) {
    error.value = t('login.networkError')
    networkError.value = true
  } finally {
    loading.value = false
  }
}

async function handleAddServer() {
  if (!newServerUrl.value) return
  loading.value = true
  error.value = ''

  // Normalize URL
  let url = newServerUrl.value.trim()
  if (!/^https?:\/\//i.test(url)) {
    url = 'https://' + url
  }

  // Save to native server list first (so it persists even if connection fails later)
  saveServer(url, newServerPassword.value)

  // Use native connectToServer for pre-auth, CORS bypass, SSL handling, and error recovery
  if (window.AndroidNative?.connectToServer) {
    window.AndroidNative.connectToServer(url, newServerPassword.value)
    // connectToServer handles navigation, cookie injection, and error display
    // No need to handle response here — the native layer takes over
    return
  }

  // Fallback for non-APP mode (same-origin only)
  try {
    const res = await fetch(url + '/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: newServerPassword.value })
    })
    if (res.ok) {
      window.location.href = url + '/'
    } else if (res.status >= 500) {
      error.value = t('login.serverError')
    } else {
      error.value = t('login.wrongPassword')
    }
  } catch (_) {
    error.value = t('login.networkError')
  } finally {
    loading.value = false
  }
}

function handleReconfigure() {
  if (window.AndroidNative?.showServerDialog) {
    window.AndroidNative.showServerDialog()
  }
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

onMounted(() => {
  if (isAppMode.value) {
    loadServers()
    // Set current server URL
    selectedServerUrl.value = window.location.origin
    // Pre-fill password if available
    const savedPassword = getPassword(selectedServerUrl.value)
    if (savedPassword) {
      password.value = savedPassword
    }
  }
})
</script>

<style scoped>
.login-page {
    min-height: 100vh;
    min-height: 100dvh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-primary);
    position: relative;
    overflow: hidden;
}

/* Decorative background */
.login-bg-gradient {
    position: absolute;
    inset: 0;
    background:
        radial-gradient(ellipse 60% 50% at 20% 20%, color-mix(in srgb, var(--accent-color) 8%, transparent), transparent),
        radial-gradient(ellipse 50% 60% at 80% 80%, color-mix(in srgb, var(--accent-color) 6%, transparent), transparent);
    pointer-events: none;
}

.login-bg-grid {
    position: absolute;
    inset: 0;
    background-image:
        linear-gradient(color-mix(in srgb, var(--border-color) 30%, transparent) 1px, transparent 1px),
        linear-gradient(90deg, color-mix(in srgb, var(--border-color) 30%, transparent) 1px, transparent 1px);
    background-size: 48px 48px;
    mask-image: radial-gradient(ellipse 70% 70% at center, black, transparent);
    -webkit-mask-image: radial-gradient(ellipse 70% 70% at center, black, transparent);
    opacity: 0.4;
    pointer-events: none;
}

/* Content layout */
.login-content {
    position: relative;
    z-index: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    width: 100%;
    max-width: 380px;
    padding: 0 24px;
    gap: 36px;
}

/* Brand section */
.login-brand {
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
}

.login-logo-wrapper {
    position: relative;
    width: 96px;
    height: 96px;
    margin-bottom: 20px;
}

.login-logo {
    width: 96px;
    height: 96px;
    border-radius: 50%;
    display: block;
    position: relative;
    z-index: 1;
    box-shadow: var(--shadow-md);
}

.login-logo-ring {
    position: absolute;
    inset: -4px;
    border-radius: 50%;
    border: 2px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
    animation: ring-pulse 3s ease-in-out infinite;
}

@keyframes ring-pulse {
    0%, 100% { opacity: 0.4; transform: scale(1); }
    50% { opacity: 0.8; transform: scale(1.04); }
}

.login-title {
    font-size: 26px;
    font-weight: 700;
    color: var(--text-primary);
    letter-spacing: -0.02em;
    margin: 0 0 8px;
}

.login-slogan {
    font-size: 18px;
    font-weight: 500;
    color: var(--accent-color);
    margin: 0 0 4px;
    letter-spacing: 0.08em;
}

.login-subtitle {
    font-size: 13px;
    color: var(--text-muted);
    margin: 0;
}

/* Form card */
.login-form-card {
    width: 100%;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 14px;
    padding: 28px 24px;
    box-shadow: var(--shadow-sm);
}

/* Server selector */
.server-selector {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 16px;
    max-height: 160px;
    overflow-y: auto;
    scrollbar-width: thin;
}

.server-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 10px;
    border-radius: 8px;
    cursor: pointer;
    transition: background 0.1s;
    font-size: 13px;
}

.server-item:hover {
    background: var(--bg-tertiary);
}

.server-item.active {
    background: var(--accent-color);
    color: #fff;
}

.server-item.active .server-icon {
    color: #fff;
}

.server-item.active .server-delete {
    color: rgba(255,255,255,0.6);
}

.server-item.active .server-delete:hover {
    color: #fff;
    background: rgba(255,255,255,0.15);
}

.server-info {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex: 1;
}

.server-icon {
    flex-shrink: 0;
    color: var(--accent-color);
}

.server-url {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
}

.server-delete {
    flex-shrink: 0;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    padding: 2px;
    border-radius: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.1s, color 0.1s;
}

.server-delete:hover {
    background: var(--bg-tertiary);
    color: var(--color-red, #ef4444);
}

.input-group {
    position: relative;
    display: flex;
    align-items: center;
}

.input-icon {
    position: absolute;
    left: 14px;
    width: 18px;
    height: 18px;
    color: var(--text-muted);
    pointer-events: none;
    flex-shrink: 0;
}

input[type="password"],
input[type="url"] {
    width: 100%;
    padding: 13px 14px 13px 42px;
    border: 1.5px solid var(--border-color);
    border-radius: 10px;
    font-size: 15px;
    outline: none;
    background: var(--bg-primary);
    color: var(--text-primary);
    transition: border-color 0.2s, box-shadow 0.2s;
    box-sizing: border-box;
}

input:focus {
    border-color: var(--accent-color);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent-color) 12%, transparent);
}

.login-btn {
    width: 100%;
    padding: 13px;
    margin-top: 16px;
    border: none;
    border-radius: 10px;
    background: var(--accent-color);
    color: #fff;
    font-size: 15px;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s, transform 0.1s, box-shadow 0.2s;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
}

.login-btn:hover:not(:disabled) {
    background: var(--accent-hover);
    box-shadow: 0 4px 14px color-mix(in srgb, var(--accent-color) 30%, transparent);
}

.login-btn:active:not(:disabled) {
    transform: scale(0.98);
}

.login-btn:disabled {
    opacity: 0.6;
    cursor: default;
}

.btn-spinner {
    width: 16px;
    height: 16px;
    border: 2px solid rgba(255, 255, 255, 0.3);
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
}

@keyframes spin {
    to { transform: rotate(360deg); }
}

.cancel-btn {
    width: 100%;
    padding: 10px;
    margin-top: 8px;
    border: 1px solid var(--border-color);
    border-radius: 10px;
    background: transparent;
    color: var(--text-secondary);
    font-size: 14px;
    cursor: pointer;
    transition: background 0.15s;
}

.cancel-btn:hover {
    background: var(--bg-tertiary);
}

.error {
    margin-top: 14px;
    padding: 10px 14px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--color-red, #dc2626) 8%, var(--bg-primary));
    border: 1px solid color-mix(in srgb, var(--color-red, #dc2626) 20%, var(--border-color));
    color: var(--color-red, #dc2626);
    font-size: 13px;
    display: flex;
    align-items: center;
    gap: 8px;
}

.error svg {
    flex-shrink: 0;
}

.reconfigure-link {
    margin-left: auto;
    padding: 2px 8px;
    border: 1px solid color-mix(in srgb, var(--color-red, #dc2626) 40%, transparent);
    border-radius: 6px;
    background: color-mix(in srgb, var(--color-red, #dc2626) 10%, transparent);
    color: var(--color-red, #dc2626);
    font-size: 11px;
    font-weight: 500;
    cursor: pointer;
    white-space: nowrap;
    flex-shrink: 0;
    transition: background 0.15s;
}

.reconfigure-link:hover {
    background: color-mix(in srgb, var(--color-red, #dc2626) 20%, transparent);
}

/* Add server button */
.add-server-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    width: 100%;
    padding: 10px;
    margin-top: 12px;
    border: 1px dashed var(--border-color);
    border-radius: 10px;
    background: transparent;
    color: var(--text-muted);
    font-size: 13px;
    cursor: pointer;
    transition: background 0.15s, color 0.15s, border-color 0.15s;
}

.add-server-btn:hover {
    background: var(--bg-tertiary);
    color: var(--accent-color);
    border-color: var(--accent-color);
}

/* Install banner */
.install-banner {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.install-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
}

.install-row:hover {
    background: var(--bg-tertiary);
    border-color: var(--accent-color);
}

.install-icon {
    color: var(--accent-color);
    flex-shrink: 0;
}

.install-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
}

.install-label {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
}

.install-desc {
    font-size: 11px;
    color: var(--text-muted);
}

.install-arrow {
    color: var(--text-muted);
    flex-shrink: 0;
}
</style>
