<template>
  <div ref="pageEl" class="settings-page">
    <header class="settings-page__header">
      <!-- One header shape for both levels, matching the task panel's header:
           the root level shows the section glyph + title, a drilled-down level
           swaps in the breadcrumb. There is no back button — the root crumb
           ("设置") is the way back, exactly as the task breadcrumb's root is,
           and it runs through the same unsaved-changes guard. -->
      <SettingsBreadcrumb v-if="navStack.length > 0" :crumbs="breadcrumbs" @navigate="handleCrumbNavigate" />
      <template v-else>
        <Settings :size="14" class="panel-header-icon" />
        <span class="settings-page__title">{{ t('nav.settings') }}</span>
        <span v-if="serverVersion" class="settings-page__version">{{ serverVersion }}</span>
      </template>
    </header>
    <!-- The body is the scroll container for every level, so it is keyed by the
         current path: a level change rebuilds the element (default scrollTop 0)
         and `handleBodyScroll` then applies the offset saved for that level. -->
    <div :key="bodyKey" class="settings-page__body" @scroll="handleBodyScroll">
      <SettingsIndex v-if="navStack.length === 0" @navigate="pushNav" @open-setting="handleOpenSetting" />
      <SettingsCategory
        v-else
        :category-id="currentCategory!"
        :search-keys="searchKeys"
        :search-empty="searchEmpty"
        :search-query="searchQuery"
        @clear-search="clearSearch"
        @search-empty="searchEmpty = true"
        @navigate="pushNav"
        @back="handleCategoryBack"
        @deleted="handleCategoryDeleted"
        @navigate-replace="replaceTopNav"
        @restart-needed="handleRestartNeeded"
        @restart-requested="handleRestart"
      />
      <SettingsRestartDialog
        v-if="restartDialogVisible"
        :changed-fields="changedColdFields"
        @restart="handleRestart"
        @later="restartDialogVisible = false"
      />
    </div>
    <footer v-if="needsRestart" class="settings-page__footer">
      <button class="fbtn settings-restart-btn settings-restart-btn--pending refresh-spin" :class="{ 'refresh-spin--active': restarting }" :disabled="restarting" @click="handleRestart">
        <RefreshCw :size="14" class="settings-restart-btn__icon" />
        <span>{{ restarting ? t('settings.restarting') : t('settings.restartPending') }}</span>
      </button>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch, onMounted } from 'vue'
import { RefreshCw, Settings } from 'lucide-vue-next'
import SettingsIndex from './SettingsIndex.vue'
import SettingsCategory from './SettingsCategory.vue'
import SettingsRestartDialog from './SettingsRestartDialog.vue'
import SettingsBreadcrumb, { type BreadcrumbCrumb } from './SettingsBreadcrumb.vue'
import { useSettingsNavigation, consumePendingSettingsCategory, pendingSettingsCategory } from '@/composables/useSettingsNavigation'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import '@/assets/modal-footer-btn.css'
import { useAgents } from '@/composables/useAgents'
import { useDialog } from '@/composables/useDialog'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { isSubPageRoute, getSubPageTitleKey } from './settingsFieldMap'
import { dockCategoryTitleKey } from '@/composables/dockPluginRuntime'

const props = defineProps<{
  active?: boolean
}>()

const {
  t, loadConfig,
  navStack, currentCategory, pushNav, popNav, truncateNav, returnToCategory, replaceTopNav,
  restartDialogVisible, changedColdFields, needsRestart,
  restarting,
  handleRestartNeeded, handleRestart,
  checkAllGuards,
} = useSettingsNavigation()

const { serverConfig } = useSettingsConfig()
const dialog = useDialog()

// ── Back navigation with unsaved changes guard ──

/**
 * Confirm discarding unsaved panel edits. Returns false when a guard is
 * violated and the user chooses to keep editing.
 *
 * Shared by every way of leaving the current level (back button, breadcrumb
 * crumb, external deep-link) so none of them can slip past the guard.
 */
async function confirmDiscardIfDirty(): Promise<boolean> {
  if (checkAllGuards()) return true
  return dialog.confirm(
    t('settings.panel.unsavedMessage'),
    {
      title: t('settings.panel.unsavedTitle'),
      confirmText: t('settings.panel.discard'),
      cancelText: t('settings.panel.continueEditing'),
    },
  )
}

async function handleBack() {
  if (!(await confirmDiscardIfDirty())) return
  saveBodyScroll()
  popNav()
}

/**
 * Return from a detail page (project:<id> / agents:<id>) to its list.
 *
 * This is a POP, not a push: the detail was pushed on top of its list, so
 * pushing again would grow the breadcrumb (设置 › 项目 › 项目详情 › 项目).
 * The unsaved-changes guard still applies for symmetry with every other exit.
 */
async function handleCategoryBack(categoryId: string) {
  if (!(await confirmDiscardIfDirty())) return
  saveBodyScroll()
  returnToCategory(categoryId)
}

/**
 * A detail page reported its entity was deleted. Return to the list
 * unconditionally: the entity no longer exists, so the unsaved-changes guard
 * must not be able to strand the user on a page for something that is gone.
 */
function handleCategoryDeleted(categoryId: string) {
  saveBodyScroll()
  returnToCategory(categoryId)
}

/**
 * Breadcrumb crumb click — jump straight to an ancestor level (multi-level
 * pop). Depth 0 is the settings index, which is not a nav-stack entry.
 */
async function handleCrumbNavigate(depth: number) {
  if (!(await confirmDiscardIfDirty())) return
  saveBodyScroll()
  truncateNav(depth)
}

// Register back handler for settings navigation
useFeatureBackHandler(
  'settings',
  () => !!props.active && navStack.value.length > 0,
  () => handleBack(),
  PRIORITY_PAGE,
)

/**
 * Human-readable label for one nav-stack entry.
 *
 * Three id shapes are possible: a plain category (`appearance`), an agent
 * detail route (`agents:<id>`), and a data-driven sub-page (`tts:tts_engine`).
 */
function labelForCategory(cat: string): string {
  // For agent detail pages (agents:{id}), show the agent name as title. The
  // fallback must NOT be the parent's label: it would render
  // 设置 › 智能体 › 智能体, and the name is also unknown while the list is
  // still loading (or when the agent was deleted). Mirrors project:<id>, whose
  // crumb reads 项目详情 under 项目.
  if (cat.startsWith('agents:')) {
    const { getAgent } = useAgents()
    const agentId = cat.slice(7)
    const agent = getAgent(agentId)
    return agent ? agent.name : t('settings.items.agentDetailTitle')
  }
  // Agent-scoped sub-page (agentSessions:<id>): a fixed label, not the agent
  // name — the name is already the previous crumb, so repeating it would read
  // 设置 › Claude › Claude.
  if (cat.startsWith('agentSessions:')) {
    return t('settings.items.agentSessionsTitle')
  }
  // For sub-page routes (colon-separated, except agents), use data-driven title lookup
  if (isSubPageRoute(cat)) {
    const titleKey = getSubPageTitleKey(cat)
    return titleKey ? t(titleKey) : cat
  }
  // Project detail (project:<id>) is a fixed-label page: the crumb reads
  // "项目详情" rather than the project name, which is shown inside the page.
  if (cat.startsWith('project:')) {
    return t('settings.items.projectDetailTitle')
  }
  return t(dockCategoryTitleKey(cat) ?? `settings.categories.${cat}`)
}

/**
 * Every nav-stack entry needed to REACH `categoryId` from the settings index,
 * outermost first: a detail route contributes its parent list as well
 * (`agents:abc` → ['agents', 'agents:abc']), a top-level category just
 * itself.
 *
 * A detail page is a child of its list — that is how the in-tab path builds
 * the stack (index → list → detail). A deep-link must build the same shape,
 * otherwise the breadcrumb jumps straight from 设置 to the entity name and the
 * level in between is missing.
 */
function routeChain(categoryId: string): string[] {
  // A sub-page of an agent needs THREE levels: 设置 › 智能体 › <名字> › 会话.
  // Its id carries the agent id, so the middle level is `agents:<id>`.
  if (categoryId.startsWith('agentSessions:')) {
    return ['agents', 'agents:' + categoryId.slice('agentSessions:'.length), categoryId]
  }
  const sep = categoryId.indexOf(':')
  return sep > 0 ? [categoryId.slice(0, sep), categoryId] : [categoryId]
}

/**
 * Breadcrumb trail for the current position: the settings root followed by one
 * crumb per nav-stack entry. Each crumb's `depth` is the stack length to keep
 * when it is clicked, so the root crumb (depth 0) returns to the index.
 *
 * The root is always present (like TaskBreadcrumb/GitBreadcrumb/DirBreadcrumb),
 * even at a first-level category where the remaining trail equals the page
 * title — a breadcrumb whose root appears and disappears with depth reads as
 * inconsistent, and the root crumb is the only affordance that says "this whole
 * page lives under 设置".
 */
const breadcrumbs = computed<BreadcrumbCrumb[]>(() => {
  const trail: BreadcrumbCrumb[] = [{ depth: 0, label: t('nav.settings') }]
  navStack.value.forEach((cat, i) => {
    trail.push({ depth: i + 1, label: labelForCategory(cat) })
  })
  return trail
})

const serverVersion = computed(() => serverConfig.value?.version ?? '')

// ── Search jump (issue #691) ──

/**
 * Rows a settings-search hit landed on (`undefined` = the page is not filtered).
 * The category page renders only these rows; clearing re-renders it in full.
 *
 * Lives here rather than in the category page because the hit itself starts at
 * the index: the user picked a row there, and that intent has to survive the
 * index being unmounted.
 */
const searchKeys = ref<string[] | undefined>(undefined)
const searchQuery = ref('')
/**
 * The filtered page has no matching row: it is a live list (agents, projects),
 * whose rows are not part of the search index. The page is replaced by an
 * empty state rather than showing everything under a "filtered" banner.
 */
const searchEmpty = ref(false)

// ── Scroll position per level ──

/**
 * Scroll offset of every level the user has been on, keyed by its path.
 *
 * Drilling into a row and coming back (breadcrumb, edge-swipe, deleted entity)
 * must land where the user left, not at the top of a long page. All levels
 * share one scroll container — the page body, which is keyed by the path so a
 * level change starts it at 0 — and this map is what puts the offset back.
 */
const scrollOffsets = new Map<string, number>()

/** Path of the level being shown: '' for the index, else the nav stack joined. */
const levelKey = computed(() => navStack.value.join('/'))

/**
 * Ties the body element to the level it belongs to. It also changes when the
 * search filter does: a filtered page is a different document (the scroll-to-hit
 * lands mid-page), and both levels sit under one key otherwise.
 */
const bodyKey = computed(() => (searchKeys.value ? `${levelKey.value}#search` : levelKey.value))

/** Remember the level's offset as it is read. */
function handleBodyScroll(event: Event) {
  scrollOffsets.set(levelKey.value, (event.target as HTMLElement).scrollTop)
}

/**
 * Put the shown level back where it was left.
 *
 * Runs one frame after a level change, on the container that belongs to the
 * level being shown. A level entered for the first time has no entry and keeps
 * the fresh container's own 0. The offset is re-applied once more on the next
 * frame: the content between the two can still be empty (a panel loading its
 * snapshot), which clamps a scrollTop set too early back to 0.
 */
function restoreBodyScroll() {
  const body = pageBodyEl()
  const offset = scrollOffsets.get(levelKey.value) ?? 0
  if (!body || offset <= 0) return
  body.scrollTop = offset
  requestAnimationFrame(() => {
    const again = pageBodyEl()
    if (again && again.scrollTop !== offset) again.scrollTop = offset
  })
}

// Saved on the way out: the scroll container is rebuilt on a level change, so
// the leaving offset has to be taken by whoever starts the navigation.
function saveBodyScroll() {
  const body = pageBodyEl()
  if (body) scrollOffsets.set(levelKey.value, body.scrollTop)
  // The filtered view is transient — its offset belongs to that hit only.
  if (searchKeys.value) scrollOffsets.delete(bodyKey.value)
}

// Restore after the body belongs to a new level. Not gated on the search
// filter: a filtered page deliberately shows the top (the scroll-to-hit is what
// brings the row into view), and `bodyKey` above is what remounts it.
watch(() => navStack.value.length, () => {
  requestAnimationFrame(restoreBodyScroll)
})

/**
 * A settings-search hit was picked: open its category narrowed to that row.
 *
 * The filter is applied BEFORE the nav push, so the category mounts already
 * filtered (`:key` is the category, not the filter) and there is no flash of
 * the full page. A plain category click clears any filter left from an earlier
 * search — `pushNav` is still wired straight to the index for that.
 */
function handleOpenSetting(categoryId: string, key: string, query: string) {
  searchKeys.value = [key]
  searchQuery.value = query
  searchEmpty.value = false
  pushNav(categoryId)
  void scrollToHit(key)
}

/** Leave the filtered view: the page shows everything again. */
function clearSearch() {
  searchKeys.value = undefined
  searchQuery.value = ''
  searchEmpty.value = false
}

/**
 * Scroll the target row into view inside the page body and flash it.
 *
 * The category mounts already filtered, so the row usually exists on the first
 * frame; the retry covers a panel that re-renders once while its snapshot
 * loads. Gives up quietly — the page is still filtered, so the row is visible
 * even without the flash.
 */
async function scrollToHit(key: string, attempt = 0): Promise<void> {
  await nextTick()
  const body = pageBodyEl()
  if (!body) return
  // Scoped attributes are compiled onto the element itself, so the plain
  // attribute selector matches.
  const row = body.querySelector<HTMLElement>(`[data-settings-key="${cssEscape(key)}"]`)
  if (!row) {
    if (attempt < 20) requestAnimationFrame(() => void scrollToHit(key, attempt + 1))
    return
  }
  // Instant, not smooth: the filtered page is short, and a smooth scroll past
  // the flash's first frames made the landing look like nothing happened.
  row.scrollIntoView({ block: 'center' })
  row.classList.add('settings-search-flash')
  window.setTimeout(() => row.classList.remove('settings-search-flash'), 1600)
}

function cssEscape(value: string): string {
  return typeof CSS !== 'undefined' && CSS.escape ? CSS.escape(value) : value
}

function pageBodyEl(): HTMLElement | null {
  return pageEl.value?.querySelector<HTMLElement>('.settings-page__body') ?? null
}

/** The page root, so the row lookup never escapes into another panel's DOM. */
const pageEl = ref<HTMLElement | null>(null)

// ── Deep-link into a category from OUTSIDE the settings tab ──
// (AppHeader logo → 关于, theme picker → "more appearance options", agent
// drawer → agent detail). The request is stored at module level by the caller.
// A deep-link is a fresh navigation to that category, NOT a descent from
// whatever happened to be open: the trail is replaced (truncate to the root,
// then push) so the breadcrumb reads 设置 › 关于 rather than
// 设置 › 外观 › 关于. A detail route also pushes its parent list (see
// routeChain) so 设置 › 智能体 › <名称> keeps the intermediate level that the
// in-tab path would have created. The request is consumed up front so a
// rejected deep-link (user cancels the unsaved-changes confirm) is dropped
// instead of lingering for the next activation.
async function openPendingDeepLink() {
  const categoryId = consumePendingSettingsCategory()
  if (!categoryId) return
  if (navStack.value[navStack.value.length - 1] === categoryId) return
  // Leaving a category with unsaved panel edits must confirm first (same as back).
  if (!(await confirmDiscardIfDirty())) return
  saveBodyScroll()
  truncateNav(0)
  for (const step of routeChain(categoryId)) pushNav(step)
}

// Case: the settings tab was ALREADY active when the deep-link fired and
// switchTab() no-ops — the module-level ref change is what triggers here.
watch(pendingSettingsCategory, (pending) => {
  if (pending && props.active) void openPendingDeepLink()
})

// First-ever open: SettingsPage is lazily mounted by the TabPanel with
// props.active already true, so the props.active watch never fires — the
// deep-link must be consumed on mount.
onMounted(() => {
  void openPendingDeepLink()
})

// Refresh config values when tab becomes active (preserve navigation state)
watch(() => props.active, (val) => {
  if (val) {
    loadConfig()
    void openPendingDeepLink()
  }
})
</script>

<style scoped>
/* The landing row (settings search): a short tint that fades, applied to the
   row element the child page rendered — hence :deep(), and not scoped to a
   child's `data-v` attribute. */
.settings-page__body :deep(.settings-search-flash) {
  animation: settings-search-flash 1.6s ease-out;
}

@keyframes settings-search-flash {
  0%, 30% {
    background: color-mix(in srgb, var(--accent-color) 18%, transparent);
  }
  100% {
    background: transparent;
  }
}

.settings-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

/* Same bar geometry as the task panel header (.list-header): --header-height,
   the same padding and gap, bg-primary with a bottom border. */
.settings-page__header {
  display: flex;
  align-items: center;
  height: var(--header-height);
  padding: 0 var(--space-2) 0 var(--space-6);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  background: var(--bg-primary);
  gap: var(--space-3);
}

/* Matches the task header's title (.drilldown-title / task breadcrumb): the
   panel title is body-sized, not a display heading — the bar is only 36px tall
   and a 16px title read as a second, competing page title. */
.settings-page__title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}


.settings-page__version {
  margin-left: auto;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-muted);
  background: var(--bg-tertiary);
  padding: var(--space-1) var(--space-4);
  border-radius: var(--radius-full);
  flex-shrink: 0;
}

.settings-page__body {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  position: relative;
}

.settings-page__footer {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  justify-content: flex-end;
  justify-content: safe flex-end;
  padding: var(--space-4) var(--space-6);
  border-top: 1px solid var(--border-color);
  flex-shrink: 0;
  gap: var(--space-4);
  padding-bottom: calc(8 * var(--dp) + env(safe-area-inset-bottom, 0));
  overflow-x: auto;
  overflow-y: hidden;
}

/* Restart footer button — layout + pulse only; pill visuals come from .fbtn. */
.settings-restart-btn {
  width: 100%;
}

.settings-restart-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

.settings-restart-btn--pending {
  background: var(--accent-color);
  color: #fff;
  animation: restart-pulse 0.8s ease-in-out infinite;
}

@keyframes restart-pulse {
  0%, 100% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-color, #0066cc) 0%, transparent); }
  50% { box-shadow: 0 0 8px 3px color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent); }
}

@media (hover: hover) {
  .settings-restart-btn.settings-restart-btn--pending:hover:not(:disabled) {
    background: var(--accent-hover);
  }
}

.settings-restart-btn:active.settings-restart-btn--pending:not(:disabled) {
  background: var(--accent-hover);
}
</style>
