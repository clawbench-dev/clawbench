<template>
  <div class="stats-tab-host">
    <!-- Sub-tab bar: switches between the two statistics panels. Each panel
         keeps its own full height, header and refresh button, so switching is
         cheap (v-show keeps scroll positions). -->
    <div class="stats-tab-bar">
      <button
        v-for="s in sections"
        :key="s.id"
        class="stats-subtab"
        :class="{ active: section === s.id }"
        @click="section = s.id"
      >
        {{ t(s.labelKey) }}
      </button>
    </div>
    <div class="stats-tab-body">
      <UsageStatsPanel :active="active && section === 'usage'" class="stats-pane" />
      <GitCodeStatsPanel :active="active && section === 'git'" class="stats-pane" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { defineAsyncComponent } from 'vue'
import AsyncComponentLoader from '@/components/common/AsyncComponentLoader.vue'

const props = defineProps<{
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

// `active` is only read to keep the per-panel activation fetches consistent;
// defineProps binds it reactively for the template children.
void props.active
</script>

<style scoped>
.stats-tab-host {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-primary, #fff);
}

/* Compact segmented bar — mirrors the panel header height so the whole tab
   reads as one unit. */
.stats-tab-bar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 38px;
  padding: 0 12px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}
.stats-subtab {
  border: 1px solid var(--border-color);
  background: var(--bg-elevated, var(--bg-primary));
  color: var(--text-secondary);
  border-radius: 999px;
  padding: 3px 14px;
  font-size: 12px;
  line-height: 20px;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
  -webkit-tap-highlight-color: transparent;
}
.stats-subtab.active {
  background: var(--accent-color, #4f8cff);
  border-color: var(--accent-color, #4f8cff);
  color: #fff;
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
