<template>
  <div ref="scrollRef" class="git-graph-scroll" :class="{ 'collapsed-mode': collapsed }" @scroll="dismissTooltip">
    <svg
      :width="svgWidth"
      :height="svgHeight"
      class="git-graph-svg"
    >
      <!-- Connection lines (hidden when collapsed) -->
      <g v-if="!collapsed" class="git-graph-lines">
        <path
          v-for="(line, i) in lines"
          :key="'line-' + i"
          :d="line.path"
          :stroke="line.color"
          :stroke-width="2"
          fill="none"
          stroke-linecap="round"
          stroke-dasharray="none"
          :opacity="1"
          class="git-graph-line"
          @click.stop="onLineClick(line, $event)"
        />
      </g>
      <!-- Commit nodes -->
      <g class="git-graph-nodes">
        <g v-for="(node, i) in nodes" :key="'node-' + i">
          <!-- WT node: amber with glow -->
          <template v-if="node.isWT">
            <circle
              :cx="collapsed ? 10 : node.cx"
              :cy="node.cy"
              r="16"
              fill="transparent"
              class="git-graph-hitarea"
              @click.stop="onNodeClick(node, $event)"
            />
            <circle
              :cx="collapsed ? 10 : node.cx"
              :cy="node.cy"
              r="7"
              fill="none"
              stroke="#f59e0b"
              stroke-width="1.5"
              opacity="0.3"
            />
            <circle
              :cx="collapsed ? 10 : node.cx"
              :cy="node.cy"
              r="5"
              fill="#f59e0b"
            />
          </template>
          <!-- Node with refs: double ring style -->
          <template v-else-if="node.refs && node.refs.length">
            <circle
              :cx="collapsed ? 10 : node.cx"
              :cy="node.cy"
              r="16"
              fill="transparent"
              class="git-graph-hitarea"
              @click.stop="onNodeClick(node, $event)"
            />
            <circle
              :cx="collapsed ? 10 : node.cx"
              :cy="node.cy"
              r="6"
              fill="none"
              :stroke="node.color"
              stroke-width="1.5"
              opacity="0.4"
              class="git-graph-ref-node"
            />
            <circle
              :cx="collapsed ? 10 : node.cx"
              :cy="node.cy"
              r="3.5"
              :fill="node.color"
              stroke="var(--bg-primary, #fff)"
              stroke-width="1.5"
              class="git-graph-ref-node"
            />
          </template>
          <!-- Normal node: simple circle -->
          <template v-else>
            <circle
              :cx="collapsed ? 10 : node.cx"
              :cy="node.cy"
              r="16"
              fill="transparent"
              class="git-graph-hitarea"
              @click.stop="onNodeClick(node, $event)"
            />
            <circle
              :cx="collapsed ? 10 : node.cx"
              :cy="node.cy"
              r="4"
              :fill="node.color"
              stroke="var(--bg-primary, #fff)"
              stroke-width="1.5"
              class="git-graph-node"
            />
          </template>
        </g>
      </g>
    </svg>
    <!-- Tooltip (refs or branch name) -->
    <Teleport to="body">
      <div
        v-if="tooltip"
        ref="tooltipEl"
        class="git-graph-tooltip"
        :style="tooltipStyle"
        @click.stop
      >
        <span v-for="(item, idx) in tooltip.items" :key="idx" class="tooltip-ref-item">
          <span class="tooltip-ref-dot" :style="{ background: tooltip.color }"></span>
          {{ item }}
        </span>
      </div>
    </Teleport>
  </div>
</template>

<script setup>
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { computeGraphData, refLabelText } from '@/utils/gitGraph'
import { getZoomedViewport, toFixedCSS } from '@/composables/useSettingsConfig'
const { t } = useI18n()

const props = defineProps({
  commits: { type: Array, default: () => [] },
  rowHeight: { type: Number, default: 64 },
  collapsed: { type: Boolean, default: false },
})

const emit = defineEmits(['update:collapsed'])

const scrollRef = ref(null)

// ── Tooltip ──
const tooltip = ref(null)
const tooltipEl = ref(null)

// Dismiss tooltip on scroll or outside click
function dismissTooltip() { tooltip.value = null }

onMounted(() => {
  document.addEventListener('click', dismissTooltip, true)
})
onUnmounted(() => {
  document.removeEventListener('click', dismissTooltip, true)
})

// Persist lane assignments across lazy-load recomputations to prevent visual
// line splitting. When new commits are appended, previously-assigned SHAs
// keep their lanes. Cleared when the drawer closes (commits reset to []).
const persistedShaToLane = ref(new Map())

// When commits array is replaced (drawer reset), clear persisted lanes
watch(() => props.commits, (val) => {
  if (!val || val.length === 0) {
    persistedShaToLane.value = new Map()
  }
})

// Compute graph data, passing previous lane assignments for stability
const graphData = computed(() => {
  const result = computeGraphData(props.commits, props.rowHeight, persistedShaToLane.value)
  persistedShaToLane.value = result.shaToLane
  return result
})
const nodes = computed(() => graphData.value.nodes)
const lines = computed(() => graphData.value.lines)
const laneBranchName = computed(() => graphData.value.laneBranchName)
const svgHeight = computed(() => {
  const base = props.commits.length * props.rowHeight + 4
  // If there are continuation lines (fade), they extend below the last row
  const hasFade = lines.value.some(l => l.fade)
  return hasFade ? base + props.rowHeight : base
})

// SVG width: only lanes, no ref labels (refs shown via tooltip)
const svgWidth = computed(() => {
  if (props.collapsed) return 20
  return graphData.value.graphWidth || 40
})

// ── Tooltip positioning ──

const onNodeClick = (node, event) => {
  event.stopPropagation()

  // Use click event coordinates for reliable positioning regardless of scroll/collapsed state
  const x = toFixedCSS(event.clientX + 8)
  const y = toFixedCSS(event.clientY - 8)

  // Collect items: refs first, then branch names (deduplicated)
  const items = (node.refs || []).map(refLabelText)
  const refSet = new Set(items)
  for (const name of (node.branchNames || [])) {
    if (!refSet.has(name)) {
      items.push(name)
    }
  }

  tooltip.value = {
    x, y,
    items: items.length > 0 ? items : [node.isWT ? t('git.graph.workingTree') : props.commits[node.row]?.sha?.slice(0, 7)],
    color: node.color,
  }
}

const onLineClick = (line, event) => {
  event.stopPropagation()
  const branchName = laneBranchName.value.get(line.lane)
  if (!branchName) return

  tooltip.value = {
    x: toFixedCSS(event.clientX + 8),
    y: toFixedCSS(event.clientY - 8),
    items: [branchName],
    color: line.color,
  }
}

// Compute tooltip style with boundary detection
const tooltipStyle = computed(() => {
  if (!tooltip.value) return {}
  let x = tooltip.value.x
  let y = tooltip.value.y
  const vp = getZoomedViewport()
  const vpW = toFixedCSS(vp.width)
  const vpH = toFixedCSS(vp.height)
  // Account for iOS notch / safe area so tooltip isn't hidden behind it
  const safeTop = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--header-safe-area-top')) || 0
  // Use measured dimensions if available, fall back to estimates
  const el = tooltipEl.value
  const tw = el ? el.offsetWidth : Math.max(80, tooltip.value.items.length * 80)
  const th = el ? el.offsetHeight : 30 + tooltip.value.items.length * 18
  // Clamp right edge
  if (x + tw > vpW - 8) x = vpW - tw - 8
  // Clamp left edge
  if (x < 8) x = 8
  // Clamp bottom edge
  if (y + th > vpH - 8) y = y - th - 16
  // Clamp top edge — respect safe area
  const minTop = 8 + safeTop
  if (y < minTop) y = minTop
  return {
    left: x + 'px',
    top: y + 'px',
  }
})
</script>

<style scoped>
.git-graph-scroll {
  overflow-x: auto;
  overflow-y: hidden;
  flex-shrink: 0;
  min-width: 20px;
  max-width: 300px;
  position: relative;
}

.git-graph-scroll.collapsed-mode {
  max-width: 24px;
  overflow-x: hidden;
}

.git-graph-svg {
  display: block;
}

.git-graph-line {
  cursor: pointer;
}

.git-graph-line:hover {
  stroke-width: 3;
}

.git-graph-ref-node,
.git-graph-node {
  cursor: pointer;
}

.git-graph-ref-node:hover + circle {
  stroke-width: 2.5;
}

.git-graph-tooltip {
  position: fixed;
  background: var(--bg-primary, #fff);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: 6px;
  padding: 6px 10px;
  white-space: nowrap;
  pointer-events: none;
  z-index: 9999;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
  display: flex;
  flex-direction: column;
  gap: 2px;
  animation: tooltipFadeIn 0.15s ease;
}

@keyframes tooltipFadeIn {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: translateY(0); }
}

.tooltip-ref-item {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-primary, #212529);
  display: flex;
  align-items: center;
  gap: 5px;
}

.tooltip-ref-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
</style>
