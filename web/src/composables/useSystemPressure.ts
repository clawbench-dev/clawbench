import { ref, computed, watch, onUnmounted, getCurrentInstance, type Ref, type ComputedRef } from 'vue'
import { Cpu, Activity, MemoryStick, Database } from 'lucide-vue-next'
import type { SystemResources } from '@/composables/useSystemResources'

/**
 * System pressure alert for the header's server icon.
 *
 * Watches the shared system resources snapshot and reports whether any metric
 * (cpu / memory / disk / 1-min load normalized to core count) is at or above
 * the critical threshold. While under pressure the icon alternates between
 * the Server glyph and the critical metric's icon (1s blink), pausing when the
 * document is hidden and resuming on return.
 */

export const CRITICAL_THRESHOLD = 90

export type MetricKey = 'cpu' | 'memory' | 'disk' | 'load'

/** Metric icon lookup — the component binds `<component :is>` to these. */
export const metricIcons = { cpu: Cpu, memory: MemoryStick, disk: Database, load: Activity }

/**
 * The metric (if any) at or above the critical threshold. Picks the one with
 * the highest excess ratio when several are critical. Load percent is
 * `load1 / core_count`, capped at 100. Pure function — extracted for testing.
 */
export function criticalMetricOf(resources: SystemResources): MetricKey | null {
  const r = resources
  const cores = r.cpu.core_count || 1
  const loadPercent = (r.load.load1 / cores) * 100
  const metrics: { key: MetricKey; percent: number }[] = [
    { key: 'cpu', percent: r.cpu.percent },
    { key: 'memory', percent: r.memory.percent },
    { key: 'disk', percent: r.disk.percent },
    { key: 'load', percent: Math.min(loadPercent, 100) },
  ]
  // Filter to metrics at or above threshold
  const critical = metrics.filter(m => m.percent >= CRITICAL_THRESHOLD)
  if (critical.length === 0) return null
  // Pick the one with highest excess ratio (denominator is same, just sort by raw excess)
  critical.sort((a, b) => (b.percent - CRITICAL_THRESHOLD) - (a.percent - CRITICAL_THRESHOLD))
  return critical[0].key
}

export function useSystemPressure(options: {
  /** Shared system resources snapshot (from useSystemResources). */
  resources: Ref<SystemResources>
  /** Injectable document visibility probe — defaults to `document`. */
  isDocumentHidden?: () => boolean
}) {
  const { resources } = options
  const isDocumentHidden = options.isDocumentHidden ?? (() => document.hidden)

  const criticalMetric = computed<MetricKey | null>(() => criticalMetricOf(resources.value))
  const isUnderPressure = computed(() => criticalMetric.value !== null)

  // Blinking state: toggles between Server icon and the critical metric icon
  const showMetricIcon = ref(false)
  let blinkTimer: ReturnType<typeof setInterval> | null = null

  function startBlinking() {
    if (blinkTimer) return
    showMetricIcon.value = false
    blinkTimer = setInterval(() => {
      showMetricIcon.value = !showMetricIcon.value
    }, 1000)
  }

  function stopBlinking() {
    if (blinkTimer) {
      clearInterval(blinkTimer)
      blinkTimer = null
    }
    showMetricIcon.value = false
  }

  watch(isUnderPressure, (under) => {
    if (under) {
      startBlinking()
    } else {
      stopBlinking()
    }
  }, { immediate: true })

  // Pause blinking when tab is hidden, resume when visible
  function onVisibilityChange() {
    if (isDocumentHidden()) {
      stopBlinking()
    } else if (isUnderPressure.value) {
      startBlinking()
    }
  }

  // The critical metric's icon component (null when not under pressure).
  const PressureIcon: ComputedRef<typeof Cpu | null> = computed(() => {
    const key = criticalMetric.value
    return key ? metricIcons[key] : null
  })

  // Clean up the blink timer when the owning component unmounts. Skipped when
  // the composable is used outside a component (e.g. a plain unit test).
  if (getCurrentInstance()) {
    onUnmounted(() => {
      stopBlinking()
    })
  }

  return {
    criticalMetric,
    isUnderPressure,
    showMetricIcon,
    PressureIcon,
    startBlinking,
    stopBlinking,
    onVisibilityChange,
  }
}
