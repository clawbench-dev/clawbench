<template>
  <div class="stats-tab-host">
    <!-- Tab bar: two rectangular page-tabs (usage / code). The bar follows the
         in-app terminal-tab pattern — connected tabs with a bottom accent line
         on the active one. Each child panel is kept mounted and its visibility
         is driven by the same `active` prop it already uses to fetch on
         activation, so switching preserves scroll positions and only the
         visible pane ever covers the body area. -->
    <div class="stats-tab-bar">
      <BarChart3 :size="14" class="panel-header-icon" />
      <!-- Horizontally scrollable strip. The three labels keep their natural
           width; a narrow panel scrolls them sideways instead of letting the
           row overflow its box (which painted the last tab on top of the
           refresh button and under the panel's window controls). Touch pans
           natively; a mouse drags and the wheel scrolls sideways, because the
           scrollbar is hidden — it would be taller than the 36px row. -->
      <div
        ref="tabStripRef"
        class="stats-tab-list"
        :class="{ 'is-overflowing': tabStripOverflow }"
        @wheel="onHorizontalWheel"
      >
        <button
          v-for="s in sections"
          :key="s.id"
          class="stats-tab"
          :class="{ active: section === s.id }"
          @click="section = s.id"
        >
          <component :is="s.icon" :size="13" />
          <span class="stats-tab-label">{{ t(s.labelKey) }}</span>
        </button>
      </div>
      <div class="stats-tab-actions">
        <RefreshButton
          class="stats-tab-refresh"
          :loading="refreshing"
          :disabled="refreshing"
          :title="t('nav.refresh')"
          @click="onRefresh"
        />
      </div>
    </div>

    <div class="stats-tab-body">
      <UsageStatsPanel :active="active && section === 'usage'" class="stats-pane" />
      <ClocStatsPanel :active="active && section === 'cloc'" class="stats-pane" />
      <GitCodeStatsPanel :active="active && section === 'git'" class="stats-pane" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { defineAsyncComponent } from 'vue'
import { BarChart3, Coins, Files, GitCommitHorizontal } from 'lucide-vue-next'
import RefreshButton from '@/components/common/RefreshButton.vue'
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent'
import { useUsageStats } from '@/composables/useUsageStats'
import { useGitCodeStats } from '@/composables/useGitCodeStats'
import { attachDragScroll, canDragScroll } from '@/utils/dragScroll'
import { onHorizontalWheel } from '@/utils/horizontalWheelScroll'
import { localConfig } from '@/composables/useSettingsConfig'
import { readPanelState, writePanelState } from '@/utils/panelState'

const props = defineProps<{
  active: boolean
}>()

const { t } = useI18n()

// Lazy-load the child panels so the echarts bundle only arrives when the
// stats tab is first opened (keeps the App.vue initial-chunk behaviour that
// used to lazy-load UsageStatsPanel on its own).
//
// These MUST go through buildAsyncComponentOptions, not a bare
// defineAsyncComponent: the panels are the one place where a failed chunk
// fetch left a permanently spinning pane. Vue renders `loadingComponent`
// forever once the loader rejects and no `errorComponent` is supplied, so an
// app left open across a rebuild (its old hashed chunks now 404) could never
// open the stats tab again — restarting the app was the only way out.
const UsageStatsPanel = defineAsyncComponent(
  buildAsyncComponentOptions({ loader: () => import('@/components/stats/UsageStatsPanel.vue') }),
)
const ClocStatsPanel = defineAsyncComponent(
  buildAsyncComponentOptions({ loader: () => import('@/components/stats/ClocStatsPanel.vue') }),
)
const GitCodeStatsPanel = defineAsyncComponent(
  buildAsyncComponentOptions({ loader: () => import('@/components/stats/GitCodeStatsPanel.vue') }),
)

type StatsSection = 'usage' | 'cloc' | 'git'
// Each tab carries its own glyph, following the forge tab bar. The icons name
// what the tab measures rather than reusing the panel-internal card icons:
// Coins = token spend, Files = code inventory on disk, GitCommitHorizontal =
// code arriving via commits.
const sections: { id: StatsSection; labelKey: string; icon: typeof Coins }[] = [
  { id: 'usage', labelKey: 'gitStats.tabUsage', icon: Coins },
  { id: 'cloc', labelKey: 'gitStats.tabCloc', icon: Files },
  { id: 'git', labelKey: 'gitStats.tabDelta', icon: GitCommitHorizontal },
]

/**
 * Which tab the panel opens on (设置 → 统计 → 默认页签). `cloc` is the "code
 * inventory" tab and `git` the "code arriving via commits" one; anything
 * unrecognised (an older/newer config) falls back to usage.
 */
function configuredSection(): StatsSection {
  const v = localConfig.statsDefaultTab
  return v === 'cloc' || v === 'git' ? v : 'usage'
}

/**
 * The tab a freshly loaded panel starts on.
 *
 * With 记住上次选择 ON this is the stored tab, which is what makes the switch
 * survive a page reload — the component instance is recreated on reload, so
 * "remember" has to come from storage, not from a ref.
 */
function initialSection(): StatsSection {
  if (localConfig.statsRememberLast === true) {
    const saved = readPanelState<string | null>('statsTab')
    if (saved === 'usage' || saved === 'cloc' || saved === 'git') return saved
  }
  return configuredSection()
}

const section = ref<StatsSection>(initialSection())

// Persist the user's tab while 记住上次选择 is on. Off means never write (and
// never read, see initialSection): the panel then always opens on the
// configured default.
watch(section, (next) => {
  if (localConfig.statsRememberLast !== true) return
  writePanelState('statsTab', next)
})

// ── Tab strip scrolling ──
// A narrow panel cannot fit all three labels. Before this the row simply
// overflowed its box, so the last tab was painted over the refresh button and
// under the panel's window controls. The strip now clips at its own edge and
// scrolls: touch pans natively, the mouse drags (attachDragScroll) and the
// wheel scrolls sideways — the scrollbar stays hidden because it is taller
// than the 36px row.
const tabStripRef = ref<HTMLElement | null>(null)
const tabStripOverflow = ref(false)
let stopTabDragScroll: (() => void) | null = null
let tabResizeObserver: ResizeObserver | null = null

/** Attach/detach drag-to-scroll to match whether the strip actually overflows. */
function syncTabDragScroll() {
  const el = tabStripRef.value
  if (!el) return
  const shouldAttach = canDragScroll('mouse', 0, el.scrollWidth, el.clientWidth)
  tabStripOverflow.value = shouldAttach
  if (shouldAttach && !stopTabDragScroll) {
    stopTabDragScroll = attachDragScroll(el)
  } else if (!shouldAttach && stopTabDragScroll) {
    stopTabDragScroll()
    stopTabDragScroll = null
  }
}

/** Keep the ACTIVE tab visible: after a split-ratio change the strip may sit
 *  scrolled away from it, and the accent line is the only selection cue. */
function scrollActiveTabIntoView() {
  const el = tabStripRef.value
  if (!el) return
  const active = el.querySelector('.stats-tab.active')
  if (!active || typeof active.scrollIntoView !== 'function') return
  active.scrollIntoView({ behavior: 'auto', block: 'nearest', inline: 'nearest' })
}

watch(section, () => {
  void nextTick(scrollActiveTabIntoView)
})

onMounted(() => {
  void nextTick(syncTabDragScroll)
  // The overflow depends on the rendered width: the split ratio changes it
  // continuously, and the panel is hidden (width 0) until the tab is opened.
  if (typeof ResizeObserver !== 'undefined') {
    tabResizeObserver = new ResizeObserver(() => {
      syncTabDragScroll()
      // A narrowing panel can push the selected tab out of view; the accent
      // line is the only cue for which pane is showing, so bring it back.
      if (tabStripOverflow.value) scrollActiveTabIntoView()
    })
    if (tabStripRef.value) tabResizeObserver.observe(tabStripRef.value)
  }
})

onBeforeUnmount(() => {
  stopTabDragScroll?.()
  stopTabDragScroll = null
  tabResizeObserver?.disconnect()
  tabResizeObserver = null
})

// The shared refresh button drives whichever panel is active.
const usageStats = useUsageStats()
const gitStats = useGitCodeStats()

/**
 * Apply the configured defaults each time the stats tab is opened.
 *
 * 记住上次选择 (statsRememberLast) decides which half of this runs: off means
 * every visit starts from the two defaults in 设置 → 统计; on means the panel
 * keeps whatever the user last chose (the composables' singleton state already
 * survives, so doing nothing is the whole implementation).
 */
watch(
  () => props.active,
  (nowActive) => {
    if (!nowActive || localConfig.statsRememberLast === true) return
    section.value = configuredSection()
    usageStats.resetRangeToDefault()
    gitStats.resetRangeToDefault()
  },
  { immediate: true },
)
const refreshing = computed(() => {
  if (section.value === 'usage') return usageStats.loading.value
  if (section.value === 'cloc') return gitStats.clocLoading.value
  return gitStats.loading.value
})

function onRefresh() {
  if (section.value === 'usage') {
    void usageStats.loadStats()
  } else if (section.value === 'cloc') {
    void gitStats.loadCloc()
  } else {
    void gitStats.loadGitStats()
  }
}
</script>

<style scoped>
.stats-tab-host {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--panel-page-bg);
}

/* ── Tab bar (connected rectangular tabs, like terminal tabs) ── */
.stats-tab-bar {
  display: flex;
  align-items: stretch;
  /* Panel-header row height, same token as every other page (the shared
     header rule enforces it in both layouts; declared here so the bar still
     has a definite height on its own). */
  height: var(--header-height);
  flex-shrink: 0;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  position: relative;
  z-index: 2;
}
/* The bar has no gap (the strip is meant to start flush at its edge), so the
   section glyph carries its own spacing. */
.stats-tab-bar > .panel-header-icon {
  margin-right: var(--space-3);
}
.stats-tab-list {
  display: flex;
  align-items: stretch;
  gap: 0;
  flex: 1;
  min-width: 0;
  /* Scroll sideways rather than squeezing the labels: `overflow-x: auto` also
     clips the strip at its own edge, so an overlong row can never paint over
     the refresh button or the window controls on the right. */
  overflow-x: auto;
  overflow-y: hidden;
  overscroll-behavior-x: contain;
  -webkit-overflow-scrolling: touch;
  /* Hidden scrollbar: it would be taller than the 36px row, and a visible bar
     under the labels reads as a second border. */
  scrollbar-width: none;
}
.stats-tab-list::-webkit-scrollbar {
  display: none;
}
/* Drag affordance only when the strip really overflows (class is measured). */
.stats-tab-list.is-overflowing {
  cursor: grab;
}
.stats-tab-list.is-overflowing.is-dragging,
.stats-tab-list.is-overflowing.is-dragging .stats-tab {
  cursor: grabbing;
  user-select: none;
  -webkit-user-select: none;
}
.stats-tab {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: calc(5 * var(--dp));
  padding: 0 var(--space-7);
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  user-select: none;
  -webkit-tap-highlight-color: transparent;
  position: relative;
  /* Natural width, never compressed — a squeezed label is worse than one the
     user scrolls to. The bar is a fixed-height strip, so the label must also
     stay on one line. */
  flex: 0 0 auto;
  white-space: nowrap;
}
.stats-tab > :deep(svg) {
  flex-shrink: 0;
}
@media (hover: hover) {
  .stats-tab:hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }
}
.stats-tab.active {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
}
.stats-tab.active::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: calc(2 * var(--dp));
  background: var(--accent-color, #4f8cff);
}

.stats-tab-actions {
  display: flex;
  align-items: center;
  padding: 0 var(--space-3);
  flex-shrink: 0;
}
/* Colour and hover come from the shared ActionBar language; the size stays 28
   because this bar is 36 tall and a 26 button would float inside it. */
.stats-tab-refresh {
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
@media (hover: hover) {
  .stats-tab-refresh:hover {
    background: color-mix(in srgb, var(--text-primary) 12%, transparent);
    color: var(--text-primary);
  }
}

.stats-tab-body {
  flex: 1;
  min-height: 0;
  position: relative;
}
.stats-pane {
  position: absolute;
  inset: 0;
}
</style>
