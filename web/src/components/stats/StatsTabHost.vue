<template>
  <div class="stats-tab-host">
    <!-- Tab bar: two rectangular page-tabs (usage / code). The bar follows the
         in-app terminal-tab pattern — connected tabs with a bottom accent line
         on the active one. Each child panel is kept mounted and its visibility
         is driven by the same `active` prop it already uses to fetch on
         activation, so switching preserves scroll positions and only the
         visible pane ever covers the body area. -->
    <div class="stats-tab-bar">
      <div class="stats-tab-list">
        <button
          v-for="s in sections"
          :key="s.id"
          class="stats-tab"
          :class="{ active: section === s.id }"
          @click="section = s.id"
        >
          {{ t(s.labelKey) }}
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
      <GitCodeStatsPanel :active="active && section === 'git'" class="stats-pane" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { defineAsyncComponent } from 'vue'
import AsyncComponentLoader from '@/components/common/AsyncComponentLoader.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import { useUsageStats } from '@/composables/useUsageStats'
import { useGitCodeStats } from '@/composables/useGitCodeStats'

defineProps<{
  active: boolean
}>()

const { t } = useI18n()

// Lazy-load both child panels so the echarts bundle only arrives when the
// stats tab is first opened (keeps the App.vue initial-chunk behaviour that
// used to lazy-load UsageStatsPanel on its own).
const UsageStatsPanel = defineAsyncComponent({
  loader: () => import('@/components/stats/UsageStatsPanel.vue'),
  loadingComponent: AsyncComponentLoader,
})
const GitCodeStatsPanel = defineAsyncComponent({
  loader: () => import('@/components/stats/GitCodeStatsPanel.vue'),
  loadingComponent: AsyncComponentLoader,
})

type StatsSection = 'usage' | 'git'
const sections: { id: StatsSection; labelKey: string }[] = [
  { id: 'usage', labelKey: 'gitStats.tabUsage' },
  { id: 'git', labelKey: 'gitStats.tabCode' },
]

const section = ref<StatsSection>('usage')

// The shared refresh button drives whichever panel is active.
const usageStats = useUsageStats()
const gitStats = useGitCodeStats()
const refreshing = computed(() => (section.value === 'usage' ? usageStats.loading.value : gitStats.loading.value))

function onRefresh() {
  if (section.value === 'usage') {
    void usageStats.loadStats()
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
  background: var(--bg-primary, #fff);
}

/* ── Tab bar (connected rectangular tabs, like terminal tabs) ── */
.stats-tab-bar {
  display: flex;
  align-items: stretch;
  height: 34px;
  flex-shrink: 0;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  position: relative;
  z-index: 2;
}
.stats-tab-list {
  display: flex;
  align-items: stretch;
  gap: 0;
  flex: 1;
  min-width: 0;
}
.stats-tab {
  display: flex;
  align-items: center;
  padding: 0 16px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  user-select: none;
  -webkit-tap-highlight-color: transparent;
  position: relative;
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
  height: 2px;
  background: var(--accent-color, #4f8cff);
}

.stats-tab-actions {
  display: flex;
  align-items: center;
  padding: 0 6px;
  flex-shrink: 0;
}
.stats-tab-refresh {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 14px;
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
    background: var(--bg-tertiary);
    color: var(--accent-color);
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
