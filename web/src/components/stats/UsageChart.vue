<template>
  <div ref="chartEl" class="usage-chart"></div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import * as echarts from 'echarts/core'
import { BarChart, PieChart, LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent, TitleComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { EChartsCoreOption } from 'echarts/core'

// Register once at module load.
echarts.use([
  BarChart, PieChart, LineChart,
  GridComponent, TooltipComponent, LegendComponent, TitleComponent,
  CanvasRenderer,
])

const props = defineProps<{
  option: EChartsCoreOption
}>()

const chartEl = ref<HTMLDivElement | null>(null)
let chart: echarts.ECharts | null = null
let resizeObserver: ResizeObserver | null = null

function render() {
  if (!chart) return
  chart.setOption(props.option, { notMerge: true })
}

function initChart() {
  if (!chartEl.value) return
  chart = echarts.init(chartEl.value)
  render()
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      chart?.resize()
    })
    resizeObserver.observe(chartEl.value)
  }
}

function onThemeChange() {
  // Re-render with the palette colors; the parent listens to the same event
  // and rebuilds the option (reading CSS vars), then pushes it via the prop.
  render()
}

onMounted(() => {
  initChart()
  window.addEventListener('clawbench-theme-change', onThemeChange)
})

onBeforeUnmount(() => {
  window.removeEventListener('clawbench-theme-change', onThemeChange)
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  }
  if (chart) {
    chart.dispose()
    chart = null
  }
})

// Push new option data (rebuilt by the parent on filter/theme change).
watch(() => props.option, () => {
  if (!chart) return
  nextTick(render)
})
</script>

<style scoped>
.usage-chart {
  width: 100%;
  height: 100%;
  min-height: 0;
}
</style>
