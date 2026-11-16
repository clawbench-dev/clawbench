<template>
  <Transition name="welcome-fade">
    <div v-if="visible" class="welcome-overlay" @click.self="close">
      <div class="welcome-panel">
        <div class="welcome-header">
          <img class="welcome-logo" src="/logo-128.png" alt="ClawBench" />
          <div class="welcome-heading">
            <h3>{{ t('welcomeInfo.title') }}</h3>
            <p class="welcome-desc">
              {{ t('welcomeInfo.desc') }}<span class="desc-highlight">{{ t('welcomeInfo.descHighlight') }}</span>
            </p>
          </div>
          <button class="welcome-close" :aria-label="t('common.close')" :title="t('common.close')" @click="close">
            <X :size="16" />
          </button>
        </div>

        <div class="welcome-toolbar">
          <SegmentedTabs v-model="filter" class="welcome-filter" :options="filterOptions" />
          <div class="welcome-toolbar-right">
            <label class="welcome-search">
              <Search :size="14" class="welcome-search-icon" />
              <input
                v-model="query"
                type="search"
                class="welcome-search-input"
                :placeholder="t('welcomeInfo.searchPlaceholder')"
                :aria-label="t('welcomeInfo.searchPlaceholder')"
              />
            </label>
            <button
              class="btn-rescan refresh-spin"
              :class="{ 'refresh-spin--active': rescanning }"
              :disabled="rescanning"
              :title="t('welcomeInfo.rescan')"
              :aria-label="t('welcomeInfo.rescan')"
              @click="rescan"
            >
              <LoadingIndicator v-if="rescanning" class="rescan-spinner" size="sm" inline />
              <RefreshCw v-else :size="14" />
              <span>{{ rescanning ? t('welcomeInfo.rescanning') : t('welcomeInfo.rescan') }}</span>
            </button>
          </div>
        </div>

        <div class="welcome-grid-wrap">
          <!-- /api/agents runs a real detection scan that can take seconds. The
               grid stays hidden until BOTH fetches settle, so the panel never
               flashes a half-built catalogue. -->
          <LoadingIndicator
            v-if="loading"
            class="backends-loading"
            size="md"
            :label="t('welcomeInfo.detecting')"
          />
          <div v-else-if="visibleBackends.length > 0" class="welcome-grid">
            <button
              v-for="b in visibleBackends"
              :key="b.id"
              type="button"
              class="backend-item"
              :data-backend="b.id"
              :class="{
                'backend-item--installed': isInstalled(b),
                'backend-item--installable': canInstall(b),
              }"
              :disabled="!isInstalled(b) && !canInstall(b)"
              @click="onBackendClick(b)"
            >
              <span class="backend-icon"><AgentIcon :backend="b.id" :name="b.name" size="lg" /></span>
              <span class="backend-name-row">
                <span class="backend-name">{{ b.name }}</span>
                <span v-if="isInstalled(b)" class="backend-badge badge-installed">
                  <Check :size="12" />
                  {{ t('welcomeInfo.detected') }}
                </span>
              </span>
              <span class="backend-specialty">{{ b.specialty }}</span>
              <span v-if="isInstalled(b)" class="backend-action">
                {{ t('welcomeInfo.openChat') }} <ArrowRight :size="13" />
              </span>
              <span v-else-if="canInstall(b)" class="backend-action">
                {{ t('welcomeInfo.install') }} <ArrowRight :size="13" />
              </span>
              <span v-else class="backend-action backend-action--muted">
                {{ t('welcomeInfo.notDetected') }}
              </span>
            </button>
          </div>
          <div v-else class="welcome-empty">{{ t('welcomeInfo.noResults') }}</div>
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
          <div v-if="desktopDownload.isDesktop && desktopDownload.currentDownloadUrl()" class="welcome-install-row" role="button" tabindex="0" @click="desktopDownload.downloadDesktop()" @keydown.enter="desktopDownload.downloadDesktop()">
            <Monitor :size="16" />
            <span>{{ t('pwa.downloadDesktopApp') }}</span>
          </div>
        </div>

        <div class="welcome-footer">
          <button class="btn-dont-show" @click="dontShowAgain">
            {{ t('welcomeInfo.dontShowAgain') }}
          </button>
          <span class="footer-note">{{ t('welcomeInfo.serverNote') }}</span>
          <div class="footer-spacer"></div>
          <button class="btn-ok" @click="close">
            {{ t('welcomeInfo.start') }}
          </button>
        </div>
      </div>
    </div>
  </Transition>

  <!-- iOS install instructions sheet -->
  <IosInstallDrawer :open="showIosSheet" @close="showIosSheet = false" />

  <!-- Agent install dialog -->
  <AgentInstallDialog
    v-if="selectedBackend"
    :backend-id="selectedBackend.id"
    :backend-name="selectedBackend.name"
    :install-cmd="selectedBackend.install_cmd || ''"
    @close="selectedBackend = null"
    @installed="handleInstalled"
  />
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowRight, Check, Monitor, MonitorSmartphone, RefreshCw, Search, Smartphone, X } from 'lucide-vue-next'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { useDesktopDownload } from '@/composables/useDesktopDownload'
import { useAgents } from '@/composables/useAgents'
import { useBackendCatalog, type BackendInfo } from '@/composables/useBackendCatalog'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import IosInstallDrawer from './common/IosInstallDrawer.vue'
import AgentInstallDialog from './AgentInstallDialog.vue'
import AgentIcon from './common/AgentIcon.vue'
import LoadingIndicator from './common/LoadingIndicator.vue'
import SegmentedTabs from './common/SegmentedTabs.vue'
import { appLog } from '@/utils/appLog'
import { downloadByUrl } from '@/utils/download'

const STORAGE_KEY = 'clawbench_welcome_dismissed'

const emit = defineEmits<{
  dismissed: []
  /** An installed card was clicked: open a chat with that agent. */
  startChat: [agentId: string]
}>()

const { t } = useI18n()
const pwaInstall = usePwaInstall()
const desktopDownload = useDesktopDownload()
const { rescanAgents } = useAgents()
const { backends, load: loadCatalog } = useBackendCatalog()
const visible = ref(false)
const detectedBackends = ref<Set<string>>(new Set())
const showIosSheet = ref(false)
const selectedBackend = ref<BackendInfo | null>(null)
const rescanning = ref(false)
const loading = ref(true)
const filter = ref<string>('all')
const query = ref('')
let unregisterBack: (() => void) | null = null

/**
 * backend id → the first installed agent id for it.
 *
 * The card is a backend, but starting a chat needs an agent row. A backend can
 * have several agents (copies); the first one is the one the user sees at the
 * top of every picker, so it is the least surprising target. Plain Map: it is
 * read on click only, never rendered.
 */
let agentByBackend = new Map<string, string>()

const installedCount = computed(() =>
  backends.value.filter(b => detectedBackends.value.has(b.id)).length,
)
const installableCount = computed(() =>
  backends.value.filter(b => !detectedBackends.value.has(b.id) && !!b.install_cmd).length,
)

// Installed first, not-installed last. Stable sort: the server's registry order
// is kept inside each half.
const sortedBackends = computed(() => {
  return [...backends.value].sort((a, b) => {
    const aDetected = detectedBackends.value.has(a.id)
    const bDetected = detectedBackends.value.has(b.id)
    if (aDetected !== bDetected) return aDetected ? -1 : 1
    return 0
  })
})

const visibleBackends = computed(() => {
  const q = query.value.trim().toLowerCase()
  return sortedBackends.value.filter((b) => {
    const installed = detectedBackends.value.has(b.id)
    if (filter.value === 'installed' && !installed) return false
    if (filter.value === 'avail' && (installed || !b.install_cmd)) return false
    if (q && !`${b.name} ${b.specialty}`.toLowerCase().includes(q)) return false
    return true
  })
})

const filterOptions = computed(() => [
  { value: 'all', label: `${t('welcomeInfo.filterAll')} ${backends.value.length}` },
  { value: 'installed', label: `${t('welcomeInfo.filterInstalled')} ${installedCount.value}` },
  { value: 'avail', label: `${t('welcomeInfo.filterAvailable')} ${installableCount.value}` },
])

function isInstalled(b: BackendInfo): boolean {
  return detectedBackends.value.has(b.id)
}

function canInstall(b: BackendInfo): boolean {
  return !isInstalled(b) && !!b.install_cmd
}

const showInstallSection = computed(() => pwaInstall.showPwaInstall.value || pwaInstall.showApkDownload.value
  || (desktopDownload.isDesktop && !!desktopDownload.currentDownloadUrl()))

async function loadBackends() {
  loading.value = true
  try {
    const [, agentsResp] = await Promise.all([
      loadCatalog(),
      fetch('/api/agents'),
    ])
    if (agentsResp.ok) {
      const data = await agentsResp.json()
      const raw = data.agents || data
      const list = (Array.isArray(raw) ? raw : []) as Array<{ id?: string; backend?: string }>
      const detected = new Set<string>()
      const byBackend = new Map<string, string>()
      for (const a of list) {
        const backend = a.backend || a.id
        if (!backend) continue
        detected.add(backend)
        if (a.id && !byBackend.has(backend)) byBackend.set(backend, a.id)
      }
      detectedBackends.value = detected
      agentByBackend = byBackend
    }
  } catch (err) {
    // Keep whatever the previous scan found; the panel still renders the
    // catalogue with every card in its "installable" state.
    appLog.w('WelcomeOverlay', 'backend detection failed', err)
  } finally {
    loading.value = false
  }
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
  loading.value = true
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
  selectedBackend.value = b
}

function onBackendClick(b: BackendInfo) {
  if (isInstalled(b)) {
    const agentId = agentByBackend.get(b.id)
    if (!agentId) return
    visible.value = false
    emit('startChat', agentId)
    return
  }
  if (canInstall(b)) startInstall(b)
}

/**
 * A successful install means the backend is now usable — refresh the panel so
 * its card flips from "Install" to "Installed" without the user pressing
 * "Rescan". The server already ran its own rescan; this only re-reads the two
 * lists the panel renders.
 */
async function handleInstalled() {
  await loadBackends()
}

async function handlePwaInstall() {
  if (pwaInstall.canInstallPwa.value) {
    await pwaInstall.installPwa()
  } else if (pwaInstall.isIOS.value) {
    showIosSheet.value = true
  }
}

function handleApkDownload() {
  downloadByUrl('/api/apk', 'clawbench-android.apk')
}

function forceShow() {
  visible.value = true
}

// Register back handler when overlay opens, unregister on close
watch(visible, (v) => {
  if (v) {
    unregisterBack = registerBackHandler({
      id: 'welcome-overlay',
      canGoBack: () => visible.value,
      goBack: () => { visible.value = false },
      priority: PRIORITY_OVERLAY,
    })
  } else if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
})

onMounted(() => {
  loadBackends()
  void desktopDownload.loadLatest()
  window.addEventListener('clawbench-show-welcome', forceShow)
})

onUnmounted(() => {
  window.removeEventListener('clawbench-show-welcome', forceShow)
  if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
})

// `visible` is exposed so App.vue can hold the startup UI-scale prompt back
// until this first-run panel is closed — two full-screen modals must not stack.
defineExpose({ show, forceShow, visible })
</script>

<style scoped>
.welcome-overlay {
  position: fixed;
  inset: 0;
  z-index: var(--z-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--bg-primary) 80%, transparent);
  backdrop-filter: blur(calc(4 * var(--dp)));
  -webkit-backdrop-filter: blur(calc(4 * var(--dp)));
  padding: var(--space-7);
}

.welcome-panel {
  position: relative;
  width: min(var(--welcome-panel-width), 100%);
  /* A launcher, not a full-screen page: cap the height so the desktop keeps a
     visible margin instead of the panel growing to the whole viewport. The
     grid scrolls inside. */
  max-height: min(calc(780 * var(--dp)), 100%);
  display: flex;
  flex-direction: column;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}

/* One brand line: the only decoration on an otherwise quiet surface. */
.welcome-panel::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: calc(3 * var(--dp));
  background: linear-gradient(90deg, var(--accent-color), var(--brand-color));
  opacity: 0.85;
}

.welcome-header {
  display: flex;
  align-items: flex-start;
  gap: var(--space-6);
  padding: calc(18 * var(--dp)) var(--space-9) var(--space-5);
}

.welcome-logo {
  width: calc(44 * var(--dp));
  height: calc(44 * var(--dp));
  border-radius: var(--radius-md);
  flex-shrink: 0;
  object-fit: cover;
  box-shadow: var(--shadow-sm);
}

.welcome-heading {
  flex: 1;
  min-width: 0;
}

.welcome-heading h3 {
  margin: 0 0 var(--space-2);
  font-size: var(--font-size-2xl);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
}

.welcome-desc {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  line-height: var(--line-height-normal);
}

.desc-highlight {
  color: var(--accent-color);
  font-weight: var(--font-weight-semibold);
}

.welcome-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  flex-shrink: 0;
  border: none;
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  cursor: pointer;
  transition: background var(--duration-slow);
}

@media (hover: hover) {
  .welcome-close:hover {
    background: var(--border-color);
  }
}

.welcome-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  flex-wrap: wrap;
  padding: 0 var(--space-9) var(--space-5);
}

.welcome-toolbar-right {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-left: auto;
}

.welcome-search {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  height: calc(30 * var(--dp));
  padding: 0 var(--space-5);
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  transition: border-color var(--duration-base);
}

.welcome-search:focus-within {
  border-color: var(--accent-color);
}

.welcome-search-icon {
  flex-shrink: 0;
}

.welcome-search-input {
  width: calc(150 * var(--dp));
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font-size: var(--font-size-md);
  font-family: inherit;
}

.welcome-search-input::placeholder {
  color: var(--text-muted);
}

.btn-rescan {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: calc(30 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  font-family: inherit;
  white-space: nowrap;
  cursor: pointer;
  transition: all var(--duration-slow);
}

.btn-rescan:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
  border-color: var(--accent-color);
  color: var(--accent-color);
}

@media (hover: hover) {
  .btn-rescan:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}

.welcome-grid-wrap {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0 var(--space-9) var(--space-6);
  scrollbar-width: thin;
}

.welcome-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-4);
}

.backends-loading {
  display: flex;
  justify-content: center;
  padding: calc(40 * var(--dp)) 0;
  --li-color: var(--text-muted);
}

.backend-item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  grid-template-areas:
    "icon name action"
    "icon spec action";
  align-items: center;
  column-gap: var(--space-4);
  row-gap: calc(2 * var(--dp));
  padding: var(--space-4);
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  text-align: left;
  font-family: inherit;
  color: var(--text-primary);
  cursor: default;
  /* Flat by design: the card is a list row, not a raised surface. Hover only
     shifts the tint — no lift, no shadow. */
  transition: background var(--duration-base) ease, border-color var(--duration-base) ease;
}

.backend-item--installed,
.backend-item--installable {
  cursor: pointer;
}

.backend-item--installed {
  border-color: color-mix(in srgb, var(--accent-color) 30%, var(--border-color));
  background: color-mix(in srgb, var(--accent-color) 7%, var(--bg-primary));
}

.backend-item:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: calc(2 * var(--dp));
}

@media (hover: hover) {
  .backend-item--installable:hover {
    border-color: color-mix(in srgb, var(--accent-color) 30%, var(--border-color));
    background: color-mix(in srgb, var(--text-primary) 4%, var(--bg-primary));
  }

  .backend-item--installed:hover {
    border-color: color-mix(in srgb, var(--accent-color) 45%, var(--border-color));
    background: color-mix(in srgb, var(--accent-color) 11%, var(--bg-primary));
  }
}

.backend-icon {
  grid-area: icon;
  align-self: center;
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(32 * var(--dp));
  height: calc(32 * var(--dp));
  flex-shrink: 0;
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}

.backend-name-row {
  grid-area: name;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.backend-name {
  min-width: 0;
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  line-height: 1.3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.backend-specialty {
  grid-area: spec;
  min-width: 0;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: 1.4;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.backend-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  padding: calc(2 * var(--dp)) var(--space-3);
  border-radius: var(--radius-full);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
}

.badge-installed {
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
  color: var(--accent-color);
}

.backend-action {
  grid-area: action;
  justify-self: end;
  align-self: center;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--accent-color);
  white-space: nowrap;
}

.backend-action--muted {
  color: var(--text-muted);
  font-weight: var(--font-weight-medium);
}

.welcome-empty {
  padding: calc(40 * var(--dp)) 0;
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-md);
}

/* Install section */
.welcome-install {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-4);
  padding: 0 var(--space-9) var(--space-5);
}

.welcome-install-row {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  flex: 1 1 auto;
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--accent-color) 8%, var(--bg-primary));
  border: 1px solid color-mix(in srgb, var(--accent-color) 20%, var(--border-color));
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .welcome-install-row:hover {
    background: color-mix(in srgb, var(--accent-color) 15%, var(--bg-primary));
  }
}

.welcome-footer {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  padding: var(--space-5) var(--space-9) var(--space-6);
  border-top: 1px solid var(--border-color);
  background: color-mix(in srgb, var(--text-primary) 3%, var(--bg-secondary));
}

.footer-note {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.footer-spacer {
  flex: 1;
}

.btn-ok {
  height: calc(34 * var(--dp));
  padding: 0 var(--space-8);
  border: none;
  border-radius: var(--radius-sm);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  font-family: inherit;
  white-space: nowrap;
  cursor: pointer;
  transition: opacity var(--duration-slow);
}

@media (hover: hover) {
  .btn-ok:hover {
    opacity: var(--opacity-hover);
  }
}

.btn-dont-show {
  padding: var(--space-2) var(--space-4);
  border: none;
  background: none;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  font-family: inherit;
  white-space: nowrap;
  cursor: pointer;
  transition: color var(--duration-slow);
}

@media (hover: hover) {
  .btn-dont-show:hover {
    color: var(--text-secondary);
  }
}

/* ── Narrow screens ── */

@media (max-width: 900px) {
  .welcome-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 620px) {
  .welcome-overlay {
    /* The app header sits above the overlay (--z-header > --z-overlay). A
       full-bleed panel would slide under it and hide the title, so start below
       the header instead of covering it. */
    padding: calc(var(--header-height) + var(--header-safe-area-top)) 0 0;
  }

  .welcome-panel {
    width: 100%;
    max-height: 100%;
    border: 0;
    border-radius: 0;
  }

  .welcome-header {
    gap: var(--space-4);
    padding: calc(14 * var(--dp)) var(--space-7) var(--space-4);
  }

  .welcome-logo {
    width: calc(36 * var(--dp));
    height: calc(36 * var(--dp));
  }

  .welcome-heading h3 {
    font-size: var(--font-size-xl);
  }

  .welcome-toolbar {
    padding: 0 var(--space-7) var(--space-4);
  }

  .welcome-filter {
    width: 100%;
  }

  .welcome-filter :deep(.segmented-tab) {
    flex: 1;
  }

  .welcome-toolbar-right {
    width: 100%;
    margin-left: 0;
  }

  .welcome-search {
    flex: 1;
    min-width: 0;
  }

  .welcome-search-input {
    width: 100%;
  }

  .btn-rescan {
    padding: 0 var(--space-4);
  }

  .btn-rescan span {
    display: none;
  }

  .welcome-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  /* One full-width row per agent: the compact card already reads as a list
     item, so trim its padding instead of keeping the desktop card's breathing
     room. */
  .backend-item {
    padding: var(--space-3) var(--space-4);
    column-gap: var(--space-3);
  }

  .welcome-grid-wrap {
    padding: 0 var(--space-7) var(--space-5);
  }

  .welcome-install {
    padding: 0 var(--space-7) var(--space-4);
  }

  .welcome-footer {
    flex-wrap: wrap;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-7) var(--space-5);
  }

  .footer-note {
    display: none;
  }
}

/* ── Transition ── */

.welcome-fade-enter-active {
  transition: opacity var(--duration-slow) ease;
}
.welcome-fade-leave-active {
  transition: opacity var(--duration-base) ease;
}
.welcome-fade-enter-from,
.welcome-fade-leave-to {
  opacity: 0;
}
</style>
