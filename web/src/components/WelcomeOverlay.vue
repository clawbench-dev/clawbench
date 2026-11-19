<template>
  <!-- Teleported to <body>, like every other overlay shell. It has to sit
       outside `.app-container`: that container is position:fixed, which is a
       stacking context, so any z-index set here would lose to the teleported
       app header no matter how high it goes. -->
  <Teleport to="body">
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

        <!-- Two steps, not two pages: the first run has exactly two decisions
             (which agent, which feature layout), and the stepper is what makes
             the second one feel like part of the same setup instead of a
             settings page that popped up on its own. -->
        <nav class="welcome-steps">
          <button
            type="button"
            class="welcome-step"
            :class="{ 'is-active': step === 1, 'is-done': step > 1 }"
            :aria-current="step === 1 ? 'step' : undefined"
            @click="step = 1"
          >
            <span class="welcome-step-num">
              <Check v-if="step > 1" :size="11" />
              <template v-else>1</template>
            </span>
            <span class="welcome-step-label">{{ t('welcomeInfo.stepAgents') }}</span>
          </button>
          <span class="welcome-step-line"></span>
          <button
            type="button"
            class="welcome-step"
            :class="{ 'is-active': step === 2 }"
            :aria-current="step === 2 ? 'step' : undefined"
            @click="step = 2"
          >
            <span class="welcome-step-num">2</span>
            <span class="welcome-step-label">{{ t('welcomeInfo.stepPresets') }}</span>
          </button>
        </nav>

        <div class="welcome-body">
          <!-- ── Step 1: agents ─────────────────────────────────── -->
          <section v-if="step === 1" class="welcome-step-pane" data-step="1">
            <div class="welcome-step-head">
              <h4>{{ t('welcomeInfo.step1Title') }}</h4>
              <span class="welcome-step-hint">{{ t('welcomeInfo.step1Hint') }}</span>
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
                  <!-- The built-in agent is part of this process: it is
                       "installed" the moment the server runs, so calling it
                       that would imply the user did something. -->
                  <span
                    v-if="isInstalled(b)"
                    class="backend-badge"
                    :class="b.id === BUILTIN_BACKEND ? 'badge-builtin' : 'badge-installed'"
                  >
                    <Check :size="12" />
                    {{ b.id === BUILTIN_BACKEND ? t('welcomeInfo.noInstallNeeded') : t('welcomeInfo.detected') }}
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

            <!-- Install commands run on the server, not on this device. It is
                 easy to assume the opposite (the button is right here), so it
                 is stated where the install list is, not only in the footer. -->
            <div class="welcome-note">
              <Info :size="13" class="welcome-note-icon" />
              <div class="welcome-note-body">
                <b>{{ t('welcomeInfo.serverNote') }}</b>
                <span>{{ t('welcomeInfo.installRunsOnServer') }}</span>
              </div>
            </div>
          </section>

          <!-- ── Step 2: feature layout ─────────────────────────── -->
          <section v-if="step === 2" class="welcome-step-pane" data-step="2">
            <div class="welcome-step-head">
              <h4>{{ t('welcomeInfo.step2Title') }}</h4>
              <span class="welcome-step-hint">{{ t('welcomeInfo.step2Hint') }}</span>
              <span class="step-head-spacer"></span>
              <button type="button" class="welcome-adv" @click="openDockSettings">
                <SlidersHorizontal :size="13" />
                {{ t('welcomeInfo.adjust') }}
              </button>
            </div>

            <div class="preset-grid" role="radiogroup" :aria-label="t('welcomeInfo.step2Title')">
              <button
                v-for="p in presetCards"
                :key="p.key"
                type="button"
                class="preset-card"
                :class="{ 'is-selected': selectedPreset === p.key }"
                :data-preset="p.key"
                role="radio"
                :aria-checked="selectedPreset === p.key"
                @click="selectedPreset = p.key"
              >
                <span class="preset-check"><Check :size="11" /></span>
                <span class="preset-head">
                  <span class="preset-glyph"><component :is="p.icon" :size="14" /></span>
                  <span class="preset-name">{{ p.name }}</span>
                  <span v-if="p.recommended" class="preset-rec">{{ t('welcomeInfo.recommended') }}</span>
                </span>
                <span class="preset-desc">{{ p.desc }}</span>
                <span class="preset-block">
                  <span class="preset-block-title">
                    {{ t('welcomeInfo.dockTabsCount', { count: p.tabs.length }) }}
                  </span>
                  <span class="preset-chips">
                    <span v-for="tab in p.tabs" :key="tab.id" class="preset-tab">
                      <component :is="tab.icon" :size="11" />
                      {{ t(tab.titleKey) }}
                    </span>
                  </span>
                  <span v-if="p.hiddenTabs.length" class="preset-collapse">{{ collapseText(p.hiddenTabs) }}</span>
                </span>
              </button>
            </div>
          </section>
        </div>

        <div class="welcome-footer">
          <button class="btn-dont-show" @click="dontShowAgain">
            {{ t('welcomeInfo.dontShowAgain') }}
          </button>
          <span class="footer-note" :class="{ 'footer-note--warn': showNoAgentsHint }">{{ footerNote }}</span>
          <div class="footer-spacer"></div>
          <div class="footer-actions">
            <button v-if="step === 2" type="button" class="btn-back" @click="step = 1">
              <ArrowLeft :size="14" />
              {{ t('welcomeInfo.back') }}
            </button>
            <button class="btn-ok" :disabled="applying" @click="onNext">
              {{ step === 1 ? t('welcomeInfo.next') : t('welcomeInfo.start') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </Transition>
  </Teleport>

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
import { ref, computed, onMounted, onUnmounted, watch, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArrowLeft,
  ArrowRight,
  Briefcase,
  Check,
  CodeXml,
  Feather,
  Info,
  Monitor,
  MonitorSmartphone,
  RefreshCw,
  Search,
  SlidersHorizontal,
  Smartphone,
  X,
} from 'lucide-vue-next'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { useDesktopDownload } from '@/composables/useDesktopDownload'
import { useAgents } from '@/composables/useAgents'
import { useBackendCatalog, type BackendInfo } from '@/composables/useBackendCatalog'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import {
  DOCK_PRESET_PLANS,
  dockFeatureChangesToConfig,
  useDockPlugins,
  type DockPreset,
} from '@/composables/useDockPlugins'
import { DOCK_TABS_WITH_ICONS } from '@/composables/dockPluginRuntime'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'
import IosInstallDrawer from './common/IosInstallDrawer.vue'
import AgentInstallDialog from './AgentInstallDialog.vue'
import AgentIcon from './common/AgentIcon.vue'
import LoadingIndicator from './common/LoadingIndicator.vue'
import SegmentedTabs from './common/SegmentedTabs.vue'
import { appLog } from '@/utils/appLog'
import { downloadByUrl } from '@/utils/download'

const STORAGE_KEY = 'clawbench_welcome_dismissed'

/** The one backend that ships inside the server process. */
const BUILTIN_BACKEND = 'builtin'

const TAG = 'WelcomeOverlay'

const emit = defineEmits<{
  dismissed: []
  /** An installed card was clicked: open a chat with that agent. */
  startChat: [agentId: string]
  /** "Fine-tune" was clicked: leave the wizard and open that settings page. */
  openSettings: [categoryId: string]
}>()

const { t } = useI18n()
const pwaInstall = usePwaInstall()
const desktopDownload = useDesktopDownload()
const { rescanAgents } = useAgents()
const { backends, load: loadCatalog } = useBackendCatalog()
const { preset: currentPreset, applyPreset } = useDockPlugins()
const { patchConfig } = useSettingsConfig()
const toast = useToast()
const visible = ref(false)
const step = ref<1 | 2>(1)
const selectedPreset = ref<Exclude<DockPreset, 'custom'>>('standard')
const applying = ref(false)
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

/**
 * "Ready" rather than "installed": the built-in agent is usable without the
 * user installing anything, so an "installed" count would never match the
 * badges on the cards.
 */
const filterOptions = computed(() => [
  { value: 'all', label: `${t('welcomeInfo.filterAll')} ${backends.value.length}` },
  { value: 'installed', label: `${t('welcomeInfo.filterReady')} ${installedCount.value}` },
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
      const list = (Array.isArray(raw) ? raw : []) as Array<{ id?: string; backend?: string; name?: string; specialty?: string }>
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

      // The built-in agent is NOT in the catalogue: that catalogue lists
      // backends with a CLI to install, and the built-in agent IS the server
      // process. It is still an agent the user can chat with, so the welcome
      // list shows it — synthesised here rather than in the server's catalogue,
      // which every other surface (Settings › Agents) reads as "installable
      // backends".
      if (detected.has(BUILTIN_BACKEND) && !backends.value.some((b) => b.id === BUILTIN_BACKEND)) {
        const agent = list.find((a) => (a.backend || a.id) === BUILTIN_BACKEND)
        backends.value = [
          {
            id: BUILTIN_BACKEND,
            name: agent?.name || 'ClawBench Agent',
            specialty: agent?.specialty || '',
            default_cmd: '',
          },
          ...backends.value,
        ]
      }
    }
  } catch (err) {
    // Keep whatever the previous scan found; the panel still renders the
    // catalogue with every card in its "installable" state.
    appLog.w(TAG, 'backend detection failed', err)
  } finally {
    loading.value = false
  }
}

function open() {
  step.value = 1
  // First run: offer the recommended layout. `developer` is the factory
  // default, not a choice the user made, so pre-selecting it would make the
  // whole step a no-op. Re-opened later: start from what is in force, so
  // pressing "Start" cannot rewrite a dock the user left alone. `custom` is not
  // a card, so the recommended preset stands in for it.
  const firstRun = localStorage.getItem(STORAGE_KEY) !== 'true'
  selectedPreset.value = firstRun || currentPreset.value === 'custom' ? 'standard' : currentPreset.value
  visible.value = true
}

function show() {
  if (localStorage.getItem(STORAGE_KEY) === 'true') return
  open()
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
    appLog.w(TAG, 'rescan failed', e)
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
  downloadByUrl('/api/apk', 'clawbench-android.apk', { viaNativeApp: true })
}

function forceShow() {
  open()
}

/* ── Step 2: feature layout ─────────────────────────────────── */

const PRESET_KEYS = ['developer', 'standard', 'minimal'] as const

/** One icon per preset, matching the settings manager's cards. */
const PRESET_ICONS: Record<Exclude<DockPreset, 'custom'>, Component> = {
  developer: CodeXml,
  standard: Briefcase,
  minimal: Feather,
}

/**
 * The three cards, derived from the SAME plans the manager applies.
 *
 * The tab chips come from the plugin registry (icon + title key), so a new
 * plugin shows up in all three cards without touching this file — a hand-kept
 * list here would silently omit it.
 */
const presetCards = computed(() =>
  PRESET_KEYS.map((key) => {
    const hidden = new Set<string>(DOCK_PRESET_PLANS[key].hidden)
    return {
      key,
      icon: PRESET_ICONS[key],
      name: t(`settings.dockPlugins.presets.${key}.name`),
      desc: t(`settings.dockPlugins.presets.${key}.desc`),
      tabs: DOCK_TABS_WITH_ICONS.filter((tab) => !hidden.has(tab.id)),
      hiddenTabs: DOCK_TABS_WITH_ICONS.filter((tab) => hidden.has(tab.id)),
      recommended: key === 'standard',
    }
  }),
)

function collapseText(hiddenTabs: readonly { titleKey: string }[]): string {
  if (hiddenTabs.length > 4) {
    return t('welcomeInfo.presetCollapseRest', { count: hiddenTabs.length })
  }
  return t('welcomeInfo.presetCollapse', {
    names: hiddenTabs.map((tab) => t(tab.titleKey)).join(t('common.listSeparator')),
  })
}

const footerNote = computed(() => {
  if (step.value === 2) return t('welcomeInfo.presetNote')
  return installedCount.value === 0
    ? t('welcomeInfo.noneReady')
    : t('welcomeInfo.readyCount', { count: installedCount.value, total: backends.value.length })
})

const showNoAgentsHint = computed(() => step.value === 1 && installedCount.value === 0)

function onNext() {
  if (step.value === 1) {
    step.value = 2
    return
  }
  void finish()
}

/**
 * Apply the chosen layout and close.
 *
 * The write is the manager's: one batched PATCH for the server switches, then
 * visibility + order, and `patchConfig` refreshes the feature snapshot so the
 * dock reacts without a reload. Nothing is written when the selection already
 * matches what is in force — opening the wizard again must not rewrite a dock
 * the user left alone.
 *
 * No confirmation dialog here, deliberately: the card the user clicked spells
 * out which features disappear, and "Start" is the confirm. The manager keeps
 * its dialog because there a preset is one click away from a list they were
 * only browsing.
 */
async function finish() {
  if (applying.value) return
  applying.value = true
  try {
    if (selectedPreset.value !== currentPreset.value) {
      await applyPreset(selectedPreset.value, async (changes) => {
        await patchConfig(dockFeatureChangesToConfig(changes))
      })
    }
    visible.value = false
  } catch (err) {
    appLog.e(TAG, `failed to apply preset ${selectedPreset.value}`, err)
    toast.show(t('welcomeInfo.presetFailed'), { icon: 'warning', type: 'error' })
  } finally {
    applying.value = false
  }
}

/** Leave the wizard and open the manager for a per-row override. */
function openDockSettings() {
  visible.value = false
  emit('openSettings', 'dockPlugins')
}

// Register back handler when overlay opens, unregister on close
watch(visible, (v) => {
  if (v) {
    unregisterBack = registerBackHandler({
      id: 'welcome-overlay',
      canGoBack: () => visible.value,
      // Step 2 is a place inside the wizard: back returns to step 1 before it
      // leaves the overlay, otherwise a phone's back gesture would silently
      // discard the layout choice.
      goBack: () => {
        if (step.value === 2) step.value = 1
        else visible.value = false
      },
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
  /* Above the app header, not under it: the first run is a takeover of the
     whole window, and a live header strip showing through reads as the panel
     being stuck underneath the title bar. Still below anything opened FROM
     the panel (AgentInstallDialog uses --z-modal). */
  z-index: calc(var(--z-header) + 1);
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
     body scrolls inside. */
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
  padding: calc(18 * var(--dp)) var(--space-9) var(--space-4);
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

/* ── Stepper ── */

.welcome-steps {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-5);
  padding: 0 var(--space-9) var(--space-5);
}

.welcome-step {
  display: inline-flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-2);
  border: none;
  background: none;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  font-family: inherit;
  cursor: default;
  transition: color var(--duration-base);
}

.welcome-step.is-active {
  color: var(--text-primary);
  font-weight: var(--font-weight-semibold);
}

.welcome-step.is-done {
  color: var(--text-secondary);
  cursor: pointer;
}

.welcome-step-num {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(20 * var(--dp));
  height: calc(20 * var(--dp));
  flex-shrink: 0;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
  background: var(--bg-primary);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  line-height: 1;
  transition: background var(--duration-base), border-color var(--duration-base), color var(--duration-base);
}

.welcome-step.is-active .welcome-step-num {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
}

.welcome-step.is-done .welcome-step-num {
  background: color-mix(in srgb, var(--accent-color) 16%, transparent);
  border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
  color: var(--accent-color);
}

.welcome-step-line {
  width: calc(148 * var(--dp));
  height: 1px;
  background: var(--border-color);
}

/* ── Body ── */

.welcome-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--space-1) var(--space-9) var(--space-6);
  scrollbar-width: thin;
}

.welcome-step-head {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-5);
  padding: var(--space-3) 0 var(--space-5);
}

.welcome-step-head h4 {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.welcome-step-hint {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.step-head-spacer {
  flex: 1;
}

.welcome-adv {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0;
  border: none;
  background: none;
  color: var(--accent-color);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  font-family: inherit;
  cursor: pointer;
}

@media (hover: hover) {
  .welcome-adv:hover {
    text-decoration: underline;
  }
}

.welcome-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  flex-wrap: wrap;
  padding: 0 0 var(--space-5);
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

/* "No install needed" is a positive-but-different fact from "you installed
   this", so it gets its own hue rather than sharing the accent. */
.badge-builtin {
  background: color-mix(in srgb, var(--color-green) 15%, transparent);
  color: var(--color-green);
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
  padding: var(--space-5) 0 0;
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

/* ── Note under the install list ── */

.welcome-note {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  margin-top: var(--space-5);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--text-primary) 3%, transparent);
}

.welcome-note-icon {
  flex-shrink: 0;
  margin-top: calc(1 * var(--dp));
  color: var(--accent-color);
  opacity: 0.85;
}

.welcome-note-body b {
  display: block;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
}

.welcome-note-body span {
  font-size: var(--font-size-2xs);
  line-height: 1.55;
  color: var(--text-muted);
}

/* ── Step 2: preset cards ── */

.preset-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-5);
}

.preset-card {
  position: relative;
  display: flex;
  flex-direction: column;
  padding: var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
  color: var(--text-primary);
  text-align: left;
  font-family: inherit;
  cursor: pointer;
  transition: background var(--duration-base) ease, border-color var(--duration-base) ease,
    box-shadow var(--duration-base) ease;
}

@media (hover: hover) {
  .preset-card:hover {
    border-color: color-mix(in srgb, var(--accent-color) 34%, var(--border-color));
  }
}

.preset-card:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: calc(2 * var(--dp));
}

.preset-card.is-selected {
  border-color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 6%, var(--bg-primary));
  box-shadow: 0 0 0 calc(3 * var(--dp)) color-mix(in srgb, var(--accent-color) 14%, transparent);
}

.preset-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
}

.preset-glyph {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  flex-shrink: 0;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color);
}

.preset-name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
}

.preset-rec {
  padding: 0 var(--space-3);
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
  color: var(--accent-color);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-bold);
  letter-spacing: 0.02em;
  line-height: calc(16 * var(--dp));
}

.preset-check {
  position: absolute;
  top: var(--space-6);
  right: var(--space-6);
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(17 * var(--dp));
  height: calc(17 * var(--dp));
  border-radius: var(--radius-full);
  background: var(--accent-color);
  color: #fff;
  opacity: 0;
  transform: scale(0.7);
  transition: opacity var(--duration-base) ease, transform var(--duration-base) ease;
}

.preset-card.is-selected .preset-check {
  opacity: 1;
  transform: none;
}

.preset-desc {
  margin: var(--space-3) 0 0;
  font-size: var(--font-size-xs);
  line-height: 1.55;
  color: var(--text-muted);
  /* Two lines on every card keeps the tab lists starting at the same height. */
  min-height: calc(34 * var(--dp));
}

.preset-block {
  margin-top: auto;
  padding-top: var(--space-5);
  border-top: 1px dashed var(--border-color);
}

.preset-block-title {
  display: block;
  margin-bottom: var(--space-3);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-hint);
}

.preset-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.preset-tab {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: calc(20 * var(--dp));
  padding: 0 var(--space-3);
  border-radius: var(--radius-xs);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
  white-space: nowrap;
}

.preset-tab svg {
  color: var(--text-muted);
}

.preset-collapse {
  display: block;
  margin-top: var(--space-4);
  font-size: var(--font-size-2xs);
  line-height: 1.5;
  color: var(--text-hint);
}

/* ── Footer ── */

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

.footer-note--warn {
  color: var(--accent-color);
}

.footer-spacer {
  flex: 1;
}

.footer-actions {
  display: inline-flex;
  align-items: center;
  gap: var(--space-5);
}

.btn-back {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: calc(34 * var(--dp));
  padding: 0 var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  font-family: inherit;
  white-space: nowrap;
  cursor: pointer;
  transition: border-color var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .btn-back:hover {
    border-color: var(--text-muted);
    color: var(--text-primary);
  }
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

.btn-ok:disabled {
  opacity: var(--opacity-muted);
  cursor: default;
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

  .preset-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .preset-desc {
    min-height: 0;
  }
}

@media (max-width: 620px) {
  .welcome-overlay {
    /* Full-bleed: the overlay now paints above the header, so the panel can
       start at the very top. Only the device's safe area is left clear — a
       notch is not something the panel should draw under. */
    padding: var(--header-safe-area-top) 0 0;
  }

  .welcome-panel {
    width: 100%;
    max-height: 100%;
    border: 0;
    border-radius: 0;
  }

  .welcome-header {
    gap: var(--space-4);
    padding: calc(14 * var(--dp)) var(--space-7) var(--space-3);
  }

  .welcome-logo {
    width: calc(36 * var(--dp));
    height: calc(36 * var(--dp));
  }

  .welcome-heading h3 {
    font-size: var(--font-size-xl);
  }

  .welcome-steps {
    padding: 0 var(--space-7) var(--space-4);
    gap: var(--space-3);
  }

  .welcome-step-line {
    width: calc(32 * var(--dp));
  }

  .welcome-step-label {
    white-space: nowrap;
  }

  .welcome-body {
    padding: var(--space-1) var(--space-7) var(--space-5);
  }

  .welcome-toolbar {
    padding: 0 0 var(--space-4);
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

  .welcome-install {
    padding: var(--space-4) 0 0;
  }

  .welcome-footer {
    flex-wrap: wrap;
    gap: var(--space-4);
    padding: var(--space-4) var(--space-7) var(--space-5);
  }

  .footer-note {
    display: none;
  }

  .footer-spacer {
    display: none;
  }

  .footer-actions {
    margin-left: auto;
  }

  .welcome-step-head {
    gap: var(--space-1) var(--space-4);
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
