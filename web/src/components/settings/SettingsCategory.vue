<template>
  <!-- Category-specific pages (agents, the dock manager, a plugin's own custom
       settings page, a project's detail route) are looked up in a table in the
       script below instead of a hand-written if-chain: a plugin contributes its
       page through `settingsPage` in `plugin.ts`. -->
  <component
    :is="specialPage.component"
    v-if="specialPage"
    :key="categoryId"
    v-bind="specialPage.props"
    v-on="specialPage.on"
  />
  <!-- Sub-page routes (data-driven: any colon-separated ID except agents) -->
  <div v-else-if="subPagePanel" class="settings-category">
    <SettingsSearchFilter v-if="searchActive" :query="searchQuery!" @clear="$emit('clearSearch')" />
    <SettingsGroupPanel
      :config="subPagePanel"
      :show-title="false"
      :search-keys="panelSearchKeysFor(subPagePanel.panelId)"
      @restart-needed="(fields) => $emit('restartNeeded', fields)"
    />
  </div>
  <!-- Standard settings category with mixed items + panels -->
  <div v-else class="settings-category">
    <!-- Opened from a settings search: say which rows are left and how to get
         the whole page back. -->
    <SettingsSearchFilter v-if="searchActive" :query="searchQuery!" @clear="$emit('clearSearch')" />
    <div v-if="searchEmpty" class="settings-category__search-empty">
      {{ t('settings.search.noResults', { query: searchQuery ?? '' }) }}
    </div>
    <template v-if="!searchEmpty">
    <!-- Brand header for the About page: logo + name + slogan, above the
         version/actions cards. -->
    <SettingsAboutBrand v-if="categoryId === 'about'" />
    <template v-for="card in cards" :key="card.type === 'group' ? 'group-' + (card.title || 'basic') : card.config.panelId">
      <!-- Flat items grouped into a card -->
      <SettingsCard
        v-if="card.type === 'group'"
        :title="card.title"
      >
        <!-- Custom wallpaper block: dedicated component with thumbnail/upload/
             remove + its own panel-opacity slider. Rendered at the top of the
             wallpaper section card. -->
        <WallpaperSetting
          v-if="card.title === t('settings.items.wallpaperSection')"
          :description="t('settings.items.wallpaperDesc')"
        />
        <!-- HTTPS certificate: status + one-click self-signed generation. The
             generator also exists as `clawbench --gen-cert`, but the people who
             need HTTPS are exactly the ones who never open a terminal. -->
        <TlsCertPanel v-if="card.title === t('settings.items.tlsSection')" />
        <!-- Forge credentials: write-only tokens, so a dedicated row manages
             per-host tokens and never renders a stored value. -->
        <ForgeCredentialsRow
          v-if="card.title === t('settings.items.forgeSection')"
          :description="t('settings.items.forgeCredentialsDesc')"
        />
        <!-- Global MCP server list. One definition shared by every ACP agent;
             the per-agent enable policy lives on the agent page. -->
        <MCPSetting
          v-if="card.title === t('settings.items.mcpSection')"
          :description="t('settings.items.mcpDesc')"
        />
        <!-- Model providers: endpoint + credential shared by agents. The card
             body is a dedicated component (same pattern as MCPSetting). -->
        <!-- Mailbox accounts: the one place an IMAP/SMTP account is added or
             edited. The mail panel's gear sends the user here instead of
             growing a second copy of this form. -->
        <MailSetting
          v-if="card.title === t('settings.items.mailAccountsSection')"
        />
        <ProviderSetting
          v-if="card.title === t('settings.items.providerSection')"
          :description="t('settings.items.providerDesc')"
        />
        <!-- 中继服务: installation-wide LiveKit endpoint + write-only
             credential, with a test-then-save action. Lives here instead of the
             per-meeting create form, which keeps only its own switch. -->
        <MeetingRelaySetting v-if="card.title === t('settings.items.meetingRelaySection')" />
        <!-- Cross-agent skill discovery: directory + git repo lists with
             write-only tokens, plus the live discovered-skill listing. -->
        <SkillsSetting
          v-if="card.title === t('settings.items.skillsSection')"
          :description="t('settings.items.skillsCardDesc')"
        />
        <!-- Local directories and git repositories are separate cards: they
             differ in what they hold (paths vs cloned remotes with sync state),
             so a single blob made the two read as one. -->
        <SkillsDirsSetting v-if="card.title === t('settings.items.skillsDirs')" />
        <SkillsReposSetting v-if="card.title === t('settings.items.skillsRepos')" />
        <!-- The discovered listing is read-only output, so it gets its own card
             rather than being glued to the configuration controls. -->
        <SkillsDiscoveredSetting
          v-if="card.title === t('settings.items.skillsDiscoveredSection')"
        />
        <!-- Project registry listing: a live list fetched from the backend,
             rendered in place of the card's (placeholder-only) rows. Its row
             clicks drill into project:<id>, so the navigate event must be
             forwarded up to SettingsPage. -->
        <ProjectsSetting
          v-if="card.title === t('settings.items.allProjectsSection')"
          @navigate="(id: string) => $emit('navigate', id)"
        />
        <!-- Knowledge-base index status + rebuild action. A dedicated card body
             (counters and a destructive reindex button have no generic row), so
             the placeholder item only opens the card. -->
        <KbSetting v-if="card.title === t('settings.items.kbStatusSection')" />
        <!-- Feature master switches live in the Dock feature manager. These
             rows report the state and link there; the row itself carries no
             control (see FeatureStatusRow). -->
        <template
          v-for="item in card.items"
          :key="item.key"
        >
          <FeatureStatusRow
            v-if="item.statusFeature"
            :enable-key="item.statusFeature"
            :label="getItemLabel(item)"
            :description="item.descriptionKey ? t(item.descriptionKey) : ''"
            @navigate="$emit('navigate', 'dockPlugins')"
          />
          <!-- The summary model is chosen where it is referenced, not on a
               separate page: this row opens a provider → model picker. -->
          <SettingsItem
            v-else-if="item.type === 'summaryModel'"
            :setting-key="item.key"
            :label="getItemLabel(item)"
            :description="item.descriptionKey ? t(item.descriptionKey) : ''"
            type="action"
            :model-value="summaryModelLabel"
            :disabled="getItemDisabled(item)"
            :summary-model-status="resolveSummaryModelStatus(item)"
            @click="openSummaryModelPicker"
          />
          <SettingsItem
            v-else
            :setting-key="item.key"
            :label="getItemLabel(item)"
            :description="item.descriptionKey ? t(item.descriptionKey) : ''"
            :type="item.type"
            :model-value="getItemValue(item)"
            :disabled="getItemDisabled(item)"
            :options="resolveItemOptions(item)"
            :options-filter="resolveOptionsFilter(item)"
            :option-previews="item.key === 'theme' ? themePreviews : undefined"
            :reveal-on-select="item.key === 'theme'"
            :min="item.min"
            :max="getItemMax(item)"
            :max-length="item.maxLength"
            :step="item.step"
            :needs-restart="item.needsRestart"
            :summary-model-status="resolveSummaryModelStatus(item)"
            :force-close="activeKey !== null && activeKey !== item.key"
            :no-divider="false"
            :default-value="item.defaultValue"
            :display-format="item.displayFormat"
            :display-transform="item.displayTransform"
            @update:model-value="(v: unknown) => handleUpdate(item, v)"
            @click="handleClick(item)"
            @edit-toggle="(open: boolean) => handleEditToggle(item.key, open)"
            @discard="handleDiscard"
          />
        </template>
      </SettingsCard>
      <!-- Group panel — C2 fix: arrow closure passes panelId -->
      <SettingsGroupPanel
        v-else
        :config="card.config"
        :show-title="shouldShowPanelTitle(card.config)"
        :search-keys="panelSearchKeysFor(card.config.panelId)"
        @restart-needed="(fields) => $emit('restartNeeded', fields)"
      />
    </template>
    <!-- Summary model picker (provider → model) -->
    <SummaryModelPicker :open="summaryPickerOpen" @update:open="summaryPickerOpen = $event" />

    <!-- Password change dialog -->
    <PasswordChangeDialog
      :open="showPasswordDialog"
      @close="showPasswordDialog = false"
      @changed="handlePasswordChanged"
    />
    <!-- iOS install instructions sheet -->
    <IosInstallDrawer :open="showIosSheet" @close="showIosSheet = false" />
    <!-- Upgrade dialog -->
    <UpgradeDialog ref="upgradeDialogRef" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch, onMounted, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingsItem from './SettingsItem.vue'
import SettingsGroupPanel from './SettingsGroupPanel.vue'
import FeatureStatusRow from './FeatureStatusRow.vue'
import SettingsCard from './SettingsCard.vue'
import SettingsAboutBrand from './SettingsAboutBrand.vue'
import WallpaperSetting from './WallpaperSetting.vue'
import ForgeCredentialsRow from './ForgeCredentialsRow.vue'
import SkillsSetting from './SkillsSetting.vue'
import TlsCertPanel from './TlsCertPanel.vue'
import MCPSetting from './MCPSetting.vue'
import MailSetting from './MailSetting.vue'
import ProviderSetting from './ProviderSetting.vue'
import MeetingRelaySetting from './MeetingRelaySetting.vue'
import SummaryModelPicker from './SummaryModelPicker.vue'
import SkillsDirsSetting from './SkillsDirsSetting.vue'
import SkillsReposSetting from './SkillsReposSetting.vue'
import SkillsDiscoveredSetting from './SkillsDiscoveredSetting.vue'
import ProjectsSetting from './ProjectsSetting.vue'
import ProjectDetailSetting from './ProjectDetailSetting.vue'
import PasswordChangeDialog from './PasswordChangeDialog.vue'
import UpgradeDialog from './UpgradeDialog.vue'
import SettingsAgentsIndex from './SettingsAgentsIndex.vue'
import SettingsAgentDetail from './SettingsAgentDetail.vue'
import SettingsAgentSessions from './SettingsAgentSessions.vue'
import DockPluginSetting from './DockPluginSetting.vue'
import KbSetting from './KbSetting.vue'
import IosInstallDrawer from '@/components/common/IosInstallDrawer.vue'
import SettingsSearchFilter from './SettingsSearchFilter.vue'
import { useSettingsConfig, getEffectiveUIScale, autoFitUIScale } from '@/composables/useSettingsConfig'
import { useAgents } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { store } from '@/stores/app.ts'
import { appLog, setLogCaptureEnabled } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'
import { usePwaInstall } from '@/composables/usePwaInstall'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { useDesktopDownload } from '@/composables/useDesktopDownload'
import { managedByEmbedder as upgradeManagedByEmbedder } from '@/composables/useUpgrade'
import { downloadByUrl } from '@/utils/download'
import { openExternalUrl } from '@/utils/externalLink'
import { PROJECT_FEEDBACK_URL, PROJECT_GITHUB_URL, PROJECT_HOMEPAGE_URL } from '@/utils/projectLinks'
import { categoryItems, isPanelOnlyCategory, getCategoryPanels, isDependsOnMet, isSubPageRoute, getSubPagePanel, type ItemSpec, type CategoryEntry, type GroupPanelConfig } from './settingsFieldMap'
import { isSummaryModelConfigured } from '@/utils/aiSummaryModel'
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent'
import { DOCK_SETTINGS_PAGES } from '@/composables/dockPluginRuntime'
import { providerNameOf } from '@/utils/providerNames'
import { THEMES } from '@/utils/themeMeta'
import type { OptionPreview, SelectOption } from './SettingsItem.vue'
import { filterAvailableFonts, MONO_FONT_CHOICES, UI_FONT_CHOICES, MONO_FALLBACK_CHOICES, DEFAULT_MONO_STACK, DEFAULT_UI_STACK, buildFontStack, getCustomFontChoices, type FontChoice } from '@/utils/fontConfig'
import { loadCustomFonts } from '@/utils/customFonts'

const props = defineProps<{
  categoryId: string
  /**
   * Search filter (issue #691): the keys of the rows a settings-search hit
   * points at. Undefined renders the whole page; an array narrows it to those
   * rows (group panels render only the matching fields, see SettingsGroupPanel).
   */
  searchKeys?: string[]
  /** The query behind `searchKeys`, shown in the filter banner. */
  searchQuery?: string
  /** The filtered page has no matching row (a live-list page such as 智能体). */
  searchEmpty?: boolean
}>()

const emit = defineEmits<{
  navigate: [categoryId: string]
  // Return from a detail page (project:<id> / agents:<id>) to its list. Kept
  // separate from `navigate` because it is a stack POP, not a push.
  back: [categoryId: string]
  // The entity was DELETED: the detail page no longer exists, so return to the
  // list without the unsaved-changes guard (there is nothing to keep editing).
  deleted: [categoryId: string]
  // Swap the current detail entry for a sibling detail of the same kind (agent
  // copy → the new agent). A replace, not a push, so the breadcrumb does not
  // grow (设置 › 源智能体 › 副本).
  navigateReplace: [categoryId: string]
  restartNeeded: [changedFields: string[]]
  restartRequested: []
  /** Drop the search filter: the page shows everything again. */
  clearSearch: []
  /**
   * A search landed on this page but no row of it matches (the agent list and
   * the project list are live data, not settings rows). Only pages whose rows
   * live in `categoryItems` can report a real match.
   */
  searchEmpty: []
}>()

/**
 * Custom settings pages a plugin contributes, wrapped once at module load.
 *
 * `defineAsyncComponent` must not run inside the resolver below: it returns a
 * NEW component identity on every call, which remounts the page on every render.
 */
const PLUGIN_PAGES = new Map<string, Component>()
for (const page of DOCK_SETTINGS_PAGES) {
  if (page.kind === 'custom' && page.component) {
    PLUGIN_PAGES.set(
      page.categoryId,
      defineAsyncComponent(buildAsyncComponentOptions({ loader: page.component })),
    )
  }
}

/** One resolved page for the current category, or null for the normal layout. */
interface SpecialSettingsPage {
  component: Component
  props?: Record<string, unknown>
  on?: Record<string, (...args: never[]) => void>
}

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()
const { localConfig, serverConfig, setLocalConfig, getServerValueWithDefault, setServerValue } = useSettingsConfig()
const { loadAgents } = useAgents()
const pwaInstall = usePwaInstall()
// Host axis: which shell is running (see usePlatformDetect).
const { isNativeApp, isAndroidApp } = usePlatformDetect()
const desktopDownload = useDesktopDownload()
const activeKey = ref<string | null>(null)
const showPasswordDialog = ref(false)
const summaryPickerOpen = ref(false)

/** Current summary model, shown on the action row. Formatted like the picker's
 *  own rows ("provider · model"): two providers can serve the same model id, and
 *  the bare id made a switch between them look like nothing happened. The
 *  provider name comes from the shared cache (see utils/providerNames), so this
 *  row does not fire its own request; it falls back to the bare id until some
 *  screen has loaded the provider list. */
const summaryModelLabel = computed(() => {
  const cfg = serverConfig.value.ai_summary as { model?: string; provider_id?: number } | undefined
  const model = typeof cfg?.model === 'string' ? cfg.model : ''
  if (!model) return ''
  const provider = providerNameOf(cfg?.provider_id)
  return provider ? `${provider} · ${model}` : model
})

function openSummaryModelPicker() {
  summaryPickerOpen.value = true
}
const showIosSheet = ref(false)
const upgradeDialogRef = ref<InstanceType<typeof UpgradeDialog> | null>(null)
const nativeAppVersion = ref('')

onMounted(() => {
  void (async () => {
    try {
      const native = getNative()
      if (native?.getAppVersion) nativeAppVersion.value = (await native.getAppVersion()) ?? '-'
    } catch { /* not in app mode */ }
  })()
  // Resolve the desktop download URL so the "download desktop app" row knows
  // whether it has anything to offer on this platform.
  void desktopDownload.loadLatest()
})

// Load agents when chat or agents category is shown
watch(() => props.categoryId, (id) => {
  if (id === 'chat' || id === 'agents' || id.startsWith('agents:')) loadAgents(true)
}, { immediate: true })

// Re-scan the custom font directory every time the Appearance category opens
// (and whenever its configured directory changes), so newly-dropped font files
// become selectable without reloading the page. customFontTick bumps after each
// fresh successful scan to re-render the font option lists (the registry itself
// is not reactive). Failures surface a toast instead of failing silently; stale
// responses (a newer scan won the race) bump nothing.
const customFontTick = ref(0)
async function refreshCustomFonts() {
  const result = await loadCustomFonts()
  if (result.stale) return
  if (result.ok) {
    customFontTick.value++
  } else {
    toast.show(t('settings.items.fontDirScanError'), { icon: 'warning', type: 'error', duration: 4000 })
  }
}
watch(() => props.categoryId, (id) => {
  if (id === 'appearance') void refreshCustomFonts()
}, { immediate: true })
// fonts.dir is served nested under config response "fonts"; use a reactive
// deep read so a changed directory triggers a rescan.
watch(() => (serverConfig.value?.fonts as Record<string, unknown> | undefined)?.dir, () => {
  if (props.categoryId === 'appearance') void refreshCustomFonts()
})

function resolveConfigValue(key: string): unknown {
  if (key in localConfig) return localConfig[key]
  return getServerValueWithDefault(key)
}

// ── Shared AI-summary-model status ──

/**
 * Whether the shared AI summary model is configured, read from the live server
 * config so the row's status pill updates as soon as the base URL is saved
 * (patchConfig reloads the config, which flows back through this ref).
 */
const summaryModelConfigured = computed(() => isSummaryModelConfigured(serverConfig.value))

/** Status pill for a row, or undefined when the row does not opt in. */
function resolveSummaryModelStatus(item: ItemSpec): 'configured' | 'unconfigured' | undefined {
  if (!item.showSummaryModelStatus) return undefined
  return summaryModelConfigured.value ? 'configured' : 'unconfigured'
}

// ── Sub-page panel (data-driven) ──

/**
 * Category-specific pages, resolved from the id.
 *
 * A table (not a template if-chain): adding a page means adding one entry here
 * or — for a plugin — declaring `settingsPage` in its `plugin.ts`. The
 * colon-prefixed routes (`agents:<id>`, `agentSessions:<id>`, `project:<id>`)
 * stay explicit because they are App-level routes, not plugin contributions.
 */
const specialPage = computed<SpecialSettingsPage | null>(() => {
  const id = props.categoryId
  if (id === 'agents') {
    return {
      component: SettingsAgentsIndex,
      on: {
        navigate: ((cid: string) => emit('navigate', cid)),
        clearSearch: (() => emit('clearSearch')),
        searchEmpty: (() => emit('searchEmpty')),
      },
    } as SpecialSettingsPage
  }
  if (id.startsWith('agents:')) {
    return {
      component: SettingsAgentDetail,
      props: { agentId: id.slice(7) },
      on: {
        deleted: (() => emit('deleted', 'agents')),
        back: (() => emit('back', 'agents')),
        // v-on object keys are RUNTIME strings (no compiler normalization like
        // @navigate-replace), so they must match the child's declared emit names.
        navigateReplace: ((cid: string) => emit('navigateReplace', cid)),
        openSessions: ((aid: string) => emit('navigate', `agentSessions:${aid}`)),
      },
    } as SpecialSettingsPage
  }
  // "Conversations using this agent" (agentSessions:<id>). A dedicated route
  // rather than a card on the detail page: the list is paged and spans
  // projects, so it needs its own scroll surface. Like project:<id> it has no
  // back button — the breadcrumb crumb is the way back. The prefix is NOT
  // `agents:` on purpose: that prefix is parsed as `agents:<id>` everywhere
  // (slice(7)), so a third segment would be read as part of the agent id.
  if (id.startsWith('agentSessions:')) {
    return {
      component: SettingsAgentSessions,
      props: { agentId: id.slice('agentSessions:'.length) },
    } as SpecialSettingsPage
  }
  // The dock manager: a custom page (presets + visibility + order), not a flat
  // settings list. It emits `navigate` so its "feature settings" links can
  // drill into the plugin's own settings category.
  if (id === 'dockPlugins') {
    return {
      component: DockPluginSetting,
      on: { navigate: ((cid: string) => emit('navigate', cid)) },
    } as SpecialSettingsPage
  }
  // Project detail route (project:<id>): a read-only stats page, not a
  // batch-save panel. No back button — the breadcrumb is the way back — and on
  // delete it emits `deleted` so the parent pops the entry instead of pushing.
  if (id.startsWith('project:')) {
    return {
      component: ProjectDetailSetting,
      props: { projectId: Number(id.slice(8)) },
      on: { deleted: (() => emit('deleted', 'project')) },
    } as SpecialSettingsPage
  }
  const custom = PLUGIN_PAGES.get(id)
  return custom ? { component: custom } : null
})

const subPagePanel = computed((): GroupPanelConfig | undefined => {
  if (isSubPageRoute(props.categoryId)) {
    return getSubPagePanel(props.categoryId)
  }
  return undefined
})

// ── Search filter (issue #691) ──

const searchActive = computed(() => props.searchKeys !== undefined)
/** The own keys of the rows on this page: flat items, panel fields, enable switches. */
const ownKeys = computed(() => {
  const keys = new Set<string>()
  for (const entry of categoryItems[props.categoryId] ?? []) {
    if (entry.type === 'item') keys.add(entry.spec.key)
    else for (const k of panelFieldKeys(entry.config)) keys.add(k)
  }
  if (subPagePanel.value) for (const k of panelFieldKeys(subPagePanel.value)) keys.add(k)
  return keys
})

/** Keys of the matching rows that belong to a panel, or undefined when the
 *  page is not filtered. Panels with no match are dropped by `cards`. */
function panelSearchKeysFor(panelId: string): string[] | undefined {
  if (!props.searchKeys) return undefined
  const keys = new Set(props.searchKeys)
  return getCategoryPanels(props.categoryId)
    .concat(subPagePanel.value ? [subPagePanel.value] : [])
    .filter(p => p.panelId === panelId)
    .flatMap(p => panelFieldKeys(p).filter(k => keys.has(k)))
}

/** Every field key a panel can render, regardless of its live conditions. */
function panelFieldKeys(cfg: GroupPanelConfig): string[] {
  const keys: string[] = []
  if (cfg.enableKey) keys.push(cfg.enableKey)
  if (cfg.entrySelector) keys.push(cfg.entrySelector.key)
  for (const f of cfg.commonFields) keys.push(f.key)
  for (const sub of cfg.optionSubFields ?? []) for (const f of sub.fields) keys.push(f.key)
  return keys
}

// ── Render list: mixed items + panels with dependsOn filtering ──

/**
 * True only in the Android WebView shell. Both native hosts report
 * isAppMode() === true, so the desktop shell must be excluded explicitly —
 * `hideInAndroidApp` rows (e.g. auto UI scale) still apply on desktop.
 */

const renderList = computed(() => {
  const raw = categoryItems[props.categoryId] ?? []
  const result: CategoryEntry[] = []

  for (const entry of raw) {
    if (entry.type === 'item') {
      if (getNative()?.getHostPlatform?.() === 'harmonyos') {
        const capabilities = getNative()?.getCapabilities?.()
        if (entry.spec.key === 'floatingStatusWindow' && !capabilities?.floatingStatus) continue
        if (entry.spec.key === 'liveUpdate' && !capabilities?.liveUpdate) continue
        if (entry.spec.key === 'downloadAndroidApp' || entry.spec.key === 'downloadDesktopApp') continue
      }
      if (entry.spec.key === 'trustedCerts' && typeof getNative()?.forgetTrustedCerts !== 'function') continue
      // The close-behaviour row describes the frameless shell's own close
      // button. Only the Electron host on Windows/Linux has one — macOS keeps a
      // native frame and its red button already closes the window without
      // quitting — so the capability is what decides, not a platform flag. This
      // also keeps the row off Android, Harmony and the plain browser, where the
      // dialog can never appear.
      if (entry.spec.key === 'closeAction' && getNative()?.hasCustomWindowControls?.() !== true) continue
      // The desktop app owns the server it ships, so it refuses to restart it:
      // the row would only ever fail. See useUpgrade's managedByEmbedder.
      // "Check for updates" stays — a fused install upgrades as a whole package
      // (the desktop shell downloads and installs it).
      if (entry.spec.key === 'restartServer' && upgradeManagedByEmbedder.value) continue
      if (!isDependsOnMet(entry.spec.dependsOn, resolveConfigValue)) continue
      if (entry.spec.appOnly && !isNativeApp.value) continue
      if (entry.spec.androidOnly && !isAndroidApp.value) continue
      if (entry.spec.hideInAndroidApp && isAndroidApp.value) continue
      if (entry.spec.key === 'appVersion' && !isNativeApp.value) continue
      if (entry.spec.key === 'addToHomeScreen' && !pwaInstall.showPwaInstall.value) continue
      if (entry.spec.key === 'downloadAndroidApp' && !pwaInstall.showApkDownload.value) continue
      // Hide when this platform has no published desktop build (or the registry
      // query failed) — otherwise the row is a dead end that downloads nothing.
      if (entry.spec.key === 'downloadDesktopApp' && !(desktopDownload.isDesktop && !!desktopDownload.currentDownloadUrl())) continue
      result.push(entry)
    } else {
      // Panel entries always render
      result.push(entry)
    }
  }

  return result
})

// ── Group flat items into cards by section header ──

const basicGroupTitle = computed(() => t('settings.categories.basic'))

interface CardGroup { type: 'group'; title: string; items: ItemSpec[] }
interface CardPanel { type: 'panel'; config: GroupPanelConfig }
type RenderCard = CardGroup | CardPanel

const cards = computed<RenderCard[]>(() => {
  const out: RenderCard[] = []
  const otherItems: ItemSpec[] = []
  let cur: CardGroup | null = null
  const flush = () => { if (cur) { out.push(cur); cur = null } }
  // Custom-font scan count — read so a completed scan re-renders the font
  // option lists (the custom registry itself is not reactive).
  void customFontTick.value
  for (const entry of renderList.value) {
    if (entry.type === 'item') {
      // A filtered page keeps only the rows the search hit named. The cards
      // that hold them are created by their section header, so an unmatched
      // row's card disappears with it.
      if (searchActive.value && !props.searchKeys!.includes(entry.spec.key)) continue
      if (entry.spec.sectionHeader) {
        const header = t(entry.spec.sectionHeader)
        if (!cur || cur.title !== header) {
          flush()
          cur = { type: 'group', title: header, items: [] }
        }
        // Some section headers exist only to open a card that a dedicated
        // component fills. Their placeholder item must NOT also render as a
        // generic row: it has no control, and because the generic loop runs
        // after the custom components it would appear at the BOTTOM of the card
        // (the skills page showed a stray "cross-agent skills" row carrying a
        // duplicate of the description paragraph).
        //   - panelOpacity   → rendered inside WallpaperSetting
        //   - skillsCard     → supplies the description, rendered by SkillsSetting
        //   - skillsDiscovered → rendered by SkillsDiscoveredSetting
        //   - kbStatus       → rendered by KbSetting (counters + reindex)
        const PLACEHOLDER_ITEMS = new Set(['panelOpacity', 'skillsCard', 'skillsDirs', 'skillsRepos', 'skillsDiscovered', 'allProjectsList', 'mcpServers', 'providersCard', 'tlsCert', 'mailAccounts', 'meetingRelay', 'kbStatus'])
        if (!PLACEHOLDER_ITEMS.has(entry.spec.key)) {
          cur.items.push(entry.spec)
        }
      } else {
        // Header-less items all merge into a single "其他" group (at the end),
        // so a page can never have more than one "其他" card.
        flush()
        otherItems.push(entry.spec)
      }
    } else {
      flush()
      // A filtered page keeps only the panels that hold a matching row.
      if (searchActive.value && !ownKeys.value.has(entry.config.panelId) && panelSearchKeysFor(entry.config.panelId)!.length === 0) continue
      out.push({ type: 'panel', config: entry.config })
    }
  }
  flush()
  if (otherItems.length > 0) {
    out.push({ type: 'group', title: basicGroupTitle.value, items: otherItems })
  }
  // While filtering, a card with no rows left is dropped: either its rows were
  // all filtered out, or its body is a dedicated component (mail accounts, MCP
  // servers, the TLS panel…) that a search hit cannot point at. Those
  // components stay reachable by opening the page normally.
  if (searchActive.value) return out.filter(card => card.type !== 'group' || card.items.length > 0)
  return out
})

// ── Panel title visibility ──

function shouldShowPanelTitle(_config: GroupPanelConfig): boolean {
  // Single-panel-only category: panel title = category title, no redundant header
  if (isPanelOnlyCategory(props.categoryId)) {
    const panels = getCategoryPanels(props.categoryId)
    return panels.length > 1
  }
  // Mixed category: always show panel title to distinguish from flat items
  return true
}

// ── Flat item helpers ──

function getItemLabel(entry: ItemSpec): string {
  const extended = entry as ItemSpec & { label?: string }
  return extended.label || t(entry.labelKey)
}

function resolveItemOptions(item: ItemSpec): SelectOption[] | undefined {
  const resolvedOptions = item.options
  if (!resolvedOptions) return undefined
  const mapped: SelectOption[] = resolvedOptions.map((opt) => ({
    ...opt,
    label: (opt as { label?: string }).label || resolveOptionLabel(item.key, opt),
  }))
  // Append the runtime-scanned custom fonts (family = display label, no i18n key).
  const custom = customFontOptions(item.key)
  if (custom.length > 0) mapped.push(...custom)
  return mapped
}

/** Keys of the four font-select items that accept custom fonts. */
const FONT_SELECT_KEYS = ['fontMono', 'fontMonoFallback', 'fontUi', 'fontUiFallback'] as const

function isFontSelectKey(key: string): key is (typeof FONT_SELECT_KEYS)[number] {
  return (FONT_SELECT_KEYS as readonly string[]).includes(key)
}

/** Resolved SelectOption[] for the custom-font group, empty when none scanned. */
function customFontOptions(key: string): SelectOption[] {
  if (!isFontSelectKey(key)) return []
  const stack = key === 'fontUi' || key === 'fontUiFallback' ? DEFAULT_UI_STACK : DEFAULT_MONO_STACK
  return getCustomFontChoices().map(c => ({
    label: c.id,
    value: c.id,
    groupKey: 'settings.items.fontsGroup.custom',
    previewFont: buildFontStack(c.id, stack),
  }))
}

/**
 * Options availability filter for font select rows: drop system candidates the
 * current device does not have, so the picker only shows fonts that would
 * actually render (bundled + custom always shown). Applies to the four font
 * keys — primary + fallback for both mono and ui.
 */
function resolveOptionsFilter(item: ItemSpec): ((opts: SelectOption[]) => Promise<SelectOption[]>) | undefined {
  if (item.key === 'fontMono') return (opts) => filterFontOptions(opts, [...MONO_FONT_CHOICES, ...getCustomFontChoices()], String(localConfig.fontMono ?? 'default'))
  if (item.key === 'fontUi') return (opts) => filterFontOptions(opts, [...UI_FONT_CHOICES, ...getCustomFontChoices()], String(localConfig.fontUi ?? 'default'))
  if (item.key === 'fontMonoFallback') return (opts) => filterFontOptions(opts, [...MONO_FALLBACK_CHOICES, ...getCustomFontChoices()], String(localConfig.fontMonoFallback ?? 'default'))
  if (item.key === 'fontUiFallback') return (opts) => filterFontOptions(opts, [...UI_FONT_CHOICES, ...getCustomFontChoices()], String(localConfig.fontUiFallback ?? 'default'))
  return undefined
}

async function filterFontOptions(opts: SelectOption[], candidates: FontChoice[], selectedId: string): Promise<SelectOption[]> {
  const available = await filterAvailableFonts(candidates, selectedId)
  const availableIds = new Set(available.map(c => c.id))
  // Always keep the currently-selected option even if undetectable, so the
  // picker never renders an empty selection.
  return opts.filter(o => o.value === 'default' || o.value === selectedId || availableIds.has(String(o.value)))
}

function resolveOptionLabel(_itemKey: string, opt: { labelKey: string; value: unknown }): string {
  if (opt.labelKey) return t(opt.labelKey)
  return String(opt.value)
}

/** Color previews for the theme picker grid (includes a special 'auto' entry). */
const themePreviews = computed<Record<string, OptionPreview>>(() => {
  const map: Record<string, OptionPreview> = {}
  for (const t of THEMES) {
    map[t.id] = { type: 'color', bg: t.preview.bg, text: t.preview.text, accent: t.preview.accent, themeId: t.id }
  }
  // Auto option: neutral light/dark split handled by CSS, no real colors needed
  map.auto = { type: 'color', bg: '#ffffff', text: '#1a1a2e', accent: '#888888', themeId: 'auto' }
  return map
})

function getItemValue(item: ItemSpec): unknown {
  if (item.type === 'header') return undefined
  if ((item as ItemSpec & { modelValue?: unknown }).modelValue !== undefined && item.source === 'local' && item.type === 'info') {
    return (item as ItemSpec & { modelValue?: unknown }).modelValue
  }
  // Route the slider through the resolver rather than the raw stored value, so
  // an off-grid or out-of-range legacy value (e.g. 0.82) displays the same
  // number the applier uses and the thumb lands on the matching step.
  if (item.key === 'uiScale') return getEffectiveUIScale()
  if (item.key === 'serverVersion') {
    return serverConfig.value?.version ?? '-'
  }
  if (item.key === 'appVersion') {
    return nativeAppVersion.value || '-'
  }
  if (item.source === 'local') {
    return localConfig[item.key]
  }
  return getServerValueWithDefault(item.key)
}

/**
 * Widen the scale slider's track when the stored factor exceeds the slider's
 * own ceiling (auto fit caps at 200%, the slider at 150%). Without this the
 * thumb would pin to the right edge while the label read "200%", showing a
 * position that does not match the value.
 */
function getItemMax(item: ItemSpec): number | undefined {
  if (item.key === 'uiScale' && item.max !== undefined) {
    return Math.max(item.max, getEffectiveUIScale())
  }
  return item.max
}

/**
 * Gray out a flat item when its `disableUnless` condition is unmet.
 *
 * Mirrors SettingsGroupPanel's isFieldDisabled. The value resolver is the same
 * one used for visibility, so a condition on a local key reads the live config
 * rather than the server snapshot.
 */
function getItemDisabled(item: ItemSpec): boolean {
  if (!item.disableUnless) return false
  return !isDependsOnMet(item.disableUnless, resolveConfigValue)
}

async function handleUpdate(item: ItemSpec, value: unknown) {
  if (item.type === 'password') {
    if (!value) return
  }

  if (item.source === 'local') {
    setLocalConfig(item.key, value as string | number | boolean)
    if (item.key === 'logCapture') {
      // setLogCaptureEnabled arms/stands down the HTTP log relay AND starts/
      // stops the periodic flush timer accordingly.
      setLogCaptureEnabled(!!value)
      if (value) {
        try {
          getNative()?.startLogCapture?.()?.catch(() => {})
        } catch { /* not in app mode */ }
      } else {
        try {
          getNative()?.stopLogCapture?.()?.catch(() => {})
        } catch { /* not in app mode */ }
      }
    }
    return
  }
  try {
    const result = await setServerValue(item.key, value as string | number | boolean)
    // The session list is ordered server-side, so a saved order preference has
    // to be re-fetched: nothing else bumps this counter while the settings
    // overlay is open, and the list behind it would keep the old order until
    // the next project switch or WS event.
    if (item.key === 'chat.session_sort_recent') {
      store.state.sessionListVersion++
    }
    if (result.needsRestart && result.changedColdFields.length > 0) {
      emit('restartNeeded', result.changedColdFields)
    }
    // Server-side warnings are the ONLY channel for "the setting was saved but a
    // follow-up step failed". Ignoring them made one case invisible: disabling
    // the forge integration persists the switch and then clears the bindings; if
    // the clear fails the user is told nothing, sees the switch off, and later
    // re-enables it to find their bindings still there — the opposite of what
    // the dialog promised. Surfaced as an error toast because the user does have
    // to act (retry the disable, or unbind manually).
    for (const w of result.warnings ?? []) {
      appLog.w('SettingsCategory', `${item.key} saved with warning: ${w}`)
      toast.show(w, { icon: 'warning', type: 'error', duration: 8000 })
    }
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

/**
 * Desktop-only row. The shell remembers each accepted self-signed certificate
 * by origin + fingerprint; this is the only way to undo it without editing the
 * store by hand.
 *
 * It wipes EVERY remembered certificate, so every self-signed server has to be
 * re-accepted on its next connection — the same class of destructive action as
 * deleting an agent or a provider, and confirmed the same way.
 */
async function forgetTrustedCerts() {
  const native = getNative()
  if (!native?.forgetTrustedCerts) return
  const ok = await dialog.confirm(t('settings.items.trustedCertsConfirm'), { dangerous: true })
  if (!ok) return
  try {
    await native.forgetTrustedCerts()
    toast.show(t('settings.items.trustedCertsCleared'), { icon: 'success', type: 'success', duration: 3000 })
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

function handleClick(item: ItemSpec) {
  if (item.key === 'reconfigureServer') {
    try {
      getNative()?.showServerDialog?.()
    } catch { /* not in app mode */ }
  }
  if (item.key === 'changePassword') {
    showPasswordDialog.value = true
  }
  if (item.key === 'trustedCerts') {
    void forgetTrustedCerts()
  }
  if (item.key === 'restartServer') {
    handleRestartServer()
  }
  if (item.key === 'addToHomeScreen') {
    handleAddToHomeScreen()
  }
  if (item.key === 'downloadAndroidApp') {
    downloadByUrl('/api/apk', 'clawbench-android.apk')
  }
  if (item.key === 'downloadDesktopApp') {
    desktopDownload.downloadDesktop()
  }
  if (item.key === 'openProjectHomepage') {
    openExternalUrl(PROJECT_HOMEPAGE_URL)
  }
  if (item.key === 'openProjectGithub') {
    openExternalUrl(PROJECT_GITHUB_URL)
  }
  if (item.key === 'openProjectFeedback') {
    openExternalUrl(PROJECT_FEEDBACK_URL)
  }
  if (item.navigateTo) {
    emit('navigate', item.navigateTo)
  }
  if (item.key === 'showWelcome') {
    window.dispatchEvent(new CustomEvent('clawbench-show-welcome'))
  }
  if (item.key === 'checkUpgrade') {
    upgradeDialogRef.value?.show()
  }
  if (item.key === 'uiScaleAutoFit') {
    // Computes the factor once from the current screen and stores it, so the
    // scale becomes a stable value the slider below can then tweak.
    const factor = autoFitUIScale()
    toast.show(t('settings.items.uiScaleAutoFitDone', { pct: Math.round(factor * 100) }), { icon: 'success', type: 'success', duration: 2500 })
  }
}

async function handleAddToHomeScreen() {
  if (pwaInstall.canInstallPwa.value) {
    await pwaInstall.installPwa()
  } else if (pwaInstall.isIOS.value) {
    showIosSheet.value = true
  }
}

async function handleRestartServer() {
  const confirmed = await dialog.confirm(
    t('settings.items.restartServerConfirm'),
    { title: t('settings.items.restartServer'), dangerous: true }
  )
  if (confirmed) {
    emit('restartRequested')
  }
}

function handlePasswordChanged(needsRestart: boolean) {
  showPasswordDialog.value = false
  toast.show(t('settings.passwordChanged'), { icon: 'success', type: 'success', duration: 3000 })
  if (needsRestart) {
    emit('restartNeeded', ['password'])
  }
}

function handleEditToggle(key: string, open: boolean) {
  if (open) {
    activeKey.value = key
  } else if (activeKey.value === key) {
    activeKey.value = null
  }
}

function handleDiscard() {
  toast.show(t('settings.passwordDiscarded'), { icon: 'info', type: 'info', duration: 3000 })
}
</script>

<style scoped>
.settings-category {
  padding: var(--space-4);
  /* Shared page surface (see --panel-page-bg). */
  background: var(--panel-page-bg);
}

.settings-category__search-empty {
  padding: var(--space-8) var(--space-7);
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--text-muted);
}
</style>
